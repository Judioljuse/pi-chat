package server

// v0.5.0 Web Push: people subscribe their browsers (iPhone: the home screen web app); when a
// message @s them, replies to them, DMs them or asks them a decision and they haven't read it
// within PushDelay, every subscribed device gets a notification.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"pichat/internal/store"
	"pichat/internal/webpush"
)

const (
	maxPushSubs = 10
	pushTTL     = 24 * time.Hour
)

// vapid returns the instance's VAPID key, creating it on first use (sealed in tool_settings).
func (s *Server) vapid() (*webpush.VAPID, error) {
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	if s.vapidKey != nil {
		return s.vapidKey, nil
	}
	var sec []byte
	err := s.db.R.QueryRow(`SELECT secret FROM tool_settings WHERE key='push.vapid'`).Scan(&sec)
	var priv string
	if err == nil {
		if priv, err = s.unseal(sec, "tool_settings:push.vapid"); err != nil {
			return nil, err
		}
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	if priv == "" {
		k, err := webpush.GenerateVAPID()
		if err != nil {
			return nil, err
		}
		if priv, err = webpush.EncodePrivate(k); err != nil {
			return nil, err
		}
		sealed, err := s.seal(priv, "tool_settings:push.vapid")
		if err != nil {
			return nil, err
		}
		if err := s.setSetting(s.db.W, "push.vapid", nil, sealed); err != nil {
			return nil, err
		}
	}
	k, err := webpush.ParseVAPID(priv)
	if err != nil {
		return nil, err
	}
	sub := "mailto:pichat@localhost"
	if u, err := url.Parse(s.cfg.PublicURL); err == nil && u.Scheme == "https" {
		sub = s.cfg.PublicURL
	}
	s.vapidKey = &webpush.VAPID{Key: k, Subject: sub}
	return s.vapidKey, nil
}

func (s *Server) pushKey(c *ctx) (any, error) {
	v, err := s.vapid()
	if err != nil {
		return nil, errf(500, "push_unavailable", "推送不可用：%s", err.Error())
	}
	var n int
	s.db.R.QueryRow(`SELECT COUNT(*) FROM push_subs WHERE user_id=?`, c.a.ID).Scan(&n)
	return obj{"publicKey": v.PublicKey(), "devices": n}, nil
}

// pushEndpointOK: only https endpoints of public hosts (the push services of browsers).
func pushEndpointOK(e string) bool {
	u, err := url.Parse(e)
	return err == nil && u.Scheme == "https" && u.Host != "" && len(e) <= 1000 && !strings.ContainsAny(u.Host, "@")
}

func uaLabel(ua string) string {
	l := strings.ToLower(ua)
	dev := "电脑"
	switch {
	case strings.Contains(l, "iphone"):
		dev = "iPhone"
	case strings.Contains(l, "ipad"):
		dev = "iPad"
	case strings.Contains(l, "android"):
		dev = "安卓"
	case strings.Contains(l, "mac os"):
		dev = "Mac"
	case strings.Contains(l, "windows"):
		dev = "Windows"
	}
	br := "浏览器"
	switch {
	case strings.Contains(l, "edg/"):
		br = "Edge"
	case strings.Contains(l, "firefox"):
		br = "Firefox"
	case strings.Contains(l, "chrome") || strings.Contains(l, "crios"):
		br = "Chrome"
	case strings.Contains(l, "safari"):
		br = "Safari"
	}
	return dev + " · " + br
}

func (s *Server) postPushSub(c *ctx) (any, error) {
	var req struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	sub := webpush.Subscription{Endpoint: strings.TrimSpace(req.Endpoint), P256dh: strings.TrimSpace(req.Keys.P256dh), Auth: strings.TrimSpace(req.Keys.Auth)}
	if !pushEndpointOK(sub.Endpoint) {
		return nil, badRequest("推送地址无效（必须是 https）")
	}
	if err := sub.Validate(); err != nil {
		return nil, badRequest("订阅无效：%s", err.Error())
	}
	label := uaLabel(c.r.Header.Get("User-Agent"))
	id := store.NewID("ps_")
	err := s.tx(func(tx *sql.Tx, _ *[]func()) error {
		// The same browser re-subscribing (or a shared device after a different person logged in)
		// replaces the old row: an endpoint belongs to exactly one person.
		if _, err := tx.Exec(`DELETE FROM push_subs WHERE endpoint=?`, sub.Endpoint); err != nil {
			return err
		}
		var n int
		tx.QueryRow(`SELECT COUNT(*) FROM push_subs WHERE user_id=?`, c.a.ID).Scan(&n)
		if n >= maxPushSubs {
			// Drop the oldest device rather than refuse: the new one is the one in hand.
			if _, err := tx.Exec(`DELETE FROM push_subs WHERE id=(SELECT id FROM push_subs WHERE user_id=? ORDER BY created_at LIMIT 1)`, c.a.ID); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`INSERT INTO push_subs(id,user_id,endpoint,p256dh,auth,label,created_at) VALUES(?,?,?,?,?,?,?)`,
			id, c.a.ID, sub.Endpoint, sub.P256dh, sub.Auth, label, now())
		return err
	})
	if err != nil {
		return nil, err
	}
	return obj{"id": id, "label": label}, nil
}

func (s *Server) deletePushSub(c *ctx) (any, error) {
	var req struct {
		Endpoint string `json:"endpoint"`
		ID       string `json:"id"`
	}
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	_, err := s.db.W.Exec(`DELETE FROM push_subs WHERE user_id=? AND (endpoint=? OR id=?)`, c.a.ID, req.Endpoint, req.ID)
	return obj{}, err
}

func (s *Server) getPushSubs(c *ctx) (any, error) {
	rows, err := s.db.R.Query(`SELECT id,label,created_at,last_ok_at,fail_count,last_error FROM push_subs WHERE user_id=? ORDER BY created_at DESC`, c.a.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []obj{}
	for rows.Next() {
		var id, label, lastErr string
		var created int64
		var ok sql.NullInt64
		var fails int
		rows.Scan(&id, &label, &created, &ok, &fails, &lastErr)
		d := obj{"id": id, "label": label, "createdAt": created, "lastOkAt": nil, "failCount": fails, "lastError": lastErr}
		if ok.Valid {
			d["lastOkAt"] = ok.Int64
		}
		out = append(out, d)
	}
	return obj{"devices": out}, rows.Err()
}

// POST /push/test: a notification to every device of mine, now.
func (s *Server) pushTest(c *ctx) (any, error) {
	if !s.rl.allow("pushtest:"+c.a.ID, 10, time.Hour) {
		return nil, errf(429, "rate_limited", "测试太频繁，每小时最多 10 次")
	}
	res := s.pushTo(c.a.ID, pushPayload{Title: "pichat 推送测试", Body: "能看到这条，说明这台设备的推送已经开好了。", URL: "/#/me", Tag: "test"})
	return obj{"results": res}, nil
}

type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
	Group string `json:"groupId,omitempty"`
}

// pushTo sends p to every device of user and prunes dead subscriptions; it returns one result
// per device (for the test button).
func (s *Server) pushTo(user string, p pushPayload) []obj {
	v, err := s.vapid()
	if err != nil {
		s.log.Error("push key", "err", err)
		return nil
	}
	rows, err := s.db.R.Query(`SELECT id,endpoint,p256dh,auth,label FROM push_subs WHERE user_id=?`, user)
	if err != nil {
		return nil
	}
	type dev struct{ id, label string; sub webpush.Subscription }
	var devs []dev
	for rows.Next() {
		var d dev
		rows.Scan(&d.id, &d.sub.Endpoint, &d.sub.P256dh, &d.sub.Auth, &d.label)
		devs = append(devs, d)
	}
	rows.Close()
	body, _ := json.Marshal(p)
	out := []obj{}
	for _, d := range devs {
		r := v.Send(s.pushClient, d.sub, body, pushTTL, "high")
		res := obj{"id": d.id, "label": d.label, "ok": r.Err == nil, "status": r.Status}
		switch {
		case r.Gone:
			s.db.W.Exec(`DELETE FROM push_subs WHERE id=?`, d.id)
			res["error"] = "订阅已失效，已删除"
		case r.Err != nil:
			msg := truncRunes(r.Err.Error(), 300)
			res["error"] = msg
			s.db.W.Exec(`UPDATE push_subs SET fail_count=fail_count+1,last_error=? WHERE id=?`, msg, d.id)
			// A device that kept failing for a long time is gone in practice.
			s.db.W.Exec(`DELETE FROM push_subs WHERE id=? AND fail_count>=50`, d.id)
		default:
			s.db.W.Exec(`UPDATE push_subs SET fail_count=0,last_error='',last_ok_at=? WHERE id=?`, now(), d.id)
		}
		out = append(out, res)
	}
	return out
}

// schedulePush notifies the people a new message is aimed at, unless they read it within
// PushDelay. Agents never get pushes; the author never pushes to themselves.
func (s *Server) schedulePush(author *actor, gid string, seq int64, text string, targets []target, ms []gm) {
	kind := map[string]string{}
	for _, m := range ms {
		kind[m.id] = m.kind
	}
	var people []target
	for _, t := range targets {
		if kind[t.id] == "human" && t.id != author.ID {
			people = append(people, t)
		}
	}
	if len(people) == 0 {
		return
	}
	var gname, gkind string
	s.db.R.QueryRow(`SELECT name,kind FROM groups WHERE id=?`, gid).Scan(&gname, &gkind)
	who := s.principalName(s.db.R, gid, author.ID)
	if who == "" {
		who = "有人"
	}
	body := pushBody(text)
	time.AfterFunc(s.PushDelay, func() {
		for _, t := range people {
			var has bool
			s.db.R.QueryRow(`SELECT EXISTS(SELECT 1 FROM push_subs WHERE user_id=?)`, t.id).Scan(&has)
			if !has {
				continue
			}
			var read int64
			s.db.R.QueryRow(`SELECT last_read_seq FROM members WHERE group_id=? AND principal_id=?`, gid, t.id).Scan(&read)
			if read >= seq {
				continue
			}
			verb := map[string]string{"mention": "@了你", "reply": "回复了你", "dm": "私聊你"}[t.reason]
			if strings.Contains(text, "【待你决定】") {
				verb = "请你决定"
			}
			title := who + " " + verb
			if gkind != "dm" && gname != "" {
				title += " · " + gname
			}
			s.pushTo(t.id, pushPayload{Title: title, Body: body, URL: fmt.Sprintf("/#/g/%s?seq=%d", gid, seq), Tag: fmt.Sprintf("%s:%d", gid, seq), Group: gid})
		}
	})
}

// pushBody: a short plain-text preview (push payloads are small and shown on lock screens).
func pushBody(text string) string {
	t := strings.NewReplacer("**", "", "`", "", "#", "", ">", "").Replace(text)
	t = strings.Join(strings.Fields(t), " ")
	if utf8.RuneCountInString(t) > 140 {
		t = string([]rune(t)[:139]) + "…"
	}
	return t
}

package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"pichat/internal/store"
)

type pendingWake struct {
	agentID, groupID string
	reason           string
	seq, hop         int64
	commander        bool
	timer            *time.Timer
}

// waker coalesces wakes per (agent, group) and delivers them.
type waker struct {
	s       *Server
	mu      sync.Mutex
	pending map[string]*pendingWake
	stopped bool
}

func newWaker(s *Server) *waker { return &waker{s: s, pending: map[string]*pendingWake{}} }

// dispatch applies limits to targets and schedules wakes; it returns woken ids and skipped entries.
func (w *waker) dispatch(author *actor, gid string, seq, hop int64, targets []target, ms []gm) ([]string, []obj) {
	s := w.s
	woken, notWoken := []string{}, []obj{}
	byID := map[string]gm{}
	for _, m := range ms {
		byID[m.id] = m
	}
	hopNoticed := false
	maxHop, maxWakes := s.groupLimits(s.db.R, gid)
	for _, t := range targets {
		m, ok := byID[t.id]
		if !ok || m.kind != "agent" || t.id == author.ID {
			continue
		}
		skip := func(reason string) { notWoken = append(notWoken, obj{"id": t.id, "reason": reason}) }
		if m.disabled {
			skip("disabled")
			continue
		}
		if hop >= int64(maxHop) {
			skip("hop_limit")
			if !hopNoticed {
				hopNoticed = true
				s.hopLimitNotice(gid, maxHop)
			}
			continue
		}
		if author.isAgent() && !s.rl.allow(budgetKey(m.owner, gid), maxWakes, time.Hour) {
			skip("budget")
			continue
		}
		// Granted agents command too; their messages still count hops and the owner budget above.
		// A restricted run's message never commands: it may just relay a non-commander's words.
		commander := postCanCommand(author) && s.canCommand(s.db.R, author.ID, t.id, gid)
		if author.isHuman() && !commander && !s.rl.allow("sender:"+author.ID+":"+t.id, 10, time.Hour) {
			skip("sender_limit")
			continue
		}
		woken = append(woken, t.id)
		w.schedule(t.id, gid, t.reason, seq, hop, commander)
	}
	return woken, notWoken
}

func (w *waker) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	for k, p := range w.pending {
		p.timer.Stop()
		delete(w.pending, k)
	}
}

func (w *waker) schedule(agentID, gid, reason string, seq, hop int64, commander bool) {
	key := agentID + "|" + gid
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	if p := w.pending[key]; p != nil {
		p.hop = max(p.hop, hop)
		p.seq = max(p.seq, seq)
		p.commander = p.commander || commander
		if reason == "mention" || p.reason == "" {
			p.reason = reason
		}
		return
	}
	p := &pendingWake{agentID: agentID, groupID: gid, reason: reason, seq: seq, hop: hop, commander: commander}
	w.pending[key] = p
	p.timer = time.AfterFunc(w.s.WakeDelay, func() {
		w.mu.Lock()
		delete(w.pending, key)
		w.mu.Unlock()
		w.fire(p)
	})
}

func (w *waker) fire(p *pendingWake) {
	s := w.s
	id := store.NewID("w_")
	if _, err := s.db.W.Exec(`INSERT INTO wakes(id,agent_id,group_id,cause_hop,commander,created_at) VALUES(?,?,?,?,?,?)`,
		id, p.agentID, p.groupID, p.hop, b2i(p.commander), now()); err != nil {
		s.log.Error("insert wake", "err", err)
		return
	}
	var kind, url, secret string
	err := s.db.R.QueryRow(`SELECT kind,url,secret FROM wake_configs WHERE agent_id=?`, p.agentID).Scan(&kind, &url, &secret)
	if err == sql.ErrNoRows {
		kind = "listener"
	} else if err != nil {
		s.log.Error("load wake config", "err", err)
		return
	}
	payload := obj{"groupId": p.groupID, "wakeId": id, "reason": p.reason, "seq": p.seq, "hop": p.hop, "commander": p.commander}
	switch kind {
	case "listener":
		s.hub.send(p.agentID, encodeEvent(0, "wake", payload))
	case "webhook":
		payload["agentId"] = p.agentID
		payload["inboxUrl"] = s.cfg.PublicURL + "/api/v1/inbox"
		payload["text"] = "你在 pichat 有新的消息需要处理。用你的 agent token 调 GET " + s.cfg.PublicURL + "/api/v1/inbox 查看。"
		go s.deliverWebhook(p.agentID, url, secret, payload)
	}
}

func signWebhook(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) deliverOnce(url, secret string, body []byte) error {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-Pichat-Timestamp", ts)
	req.Header.Set("X-Pichat-Signature", signWebhook(secret, ts, body))
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func (s *Server) deliverWebhook(agentID, url, secret string, payload obj) {
	body, _ := json.Marshal(payload)
	try := func() error { return s.deliverOnce(url, secret, body) }
	err := try()
	if err != nil {
		time.Sleep(s.WebhookRetry)
		err = try()
	}
	if err == nil {
		s.db.W.Exec(`UPDATE wake_configs SET fail_streak=0 WHERE agent_id=?`, agentID)
		return
	}
	msg := truncRunes(err.Error(), 300)
	s.db.W.Exec(`UPDATE wake_configs SET fail_streak=fail_streak+1, last_error=?,
		kind=CASE WHEN fail_streak+1>=5 THEN 'none' ELSE kind END WHERE agent_id=?`, msg, agentID)
}

func (s *Server) hopLimitNotice(gid string, limit int) {
	s.hopMu.Lock()
	last, ok := s.hopNotice[gid]
	if ok && time.Since(last) < 10*time.Minute {
		s.hopMu.Unlock()
		return
	}
	s.hopNotice[gid] = time.Now()
	s.hopMu.Unlock()
	if err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		return s.systemMessage(tx, after, gid, fmt.Sprintf("agent 之间的接力已到上限（%d 跳），需要人类 @ 才能继续；群主或管理员可以在群设置里调整上限", limit))
	}); err != nil {
		s.log.Error("hop notice", "err", err)
	}
}

// Relay limits: groups may override the server defaults (群设置 → agent 接力).
const (
	maxHopLimit       = 50
	maxAgentWakesHour = 1000
)

// groupLimits returns the hop limit and the per-owner hourly agent-wake budget for gid.
func (s *Server) groupLimits(q querier, gid string) (hop, wakes int) {
	var h, w sql.NullInt64
	q.QueryRow(`SELECT hop_limit, agent_wakes_per_hour FROM groups WHERE id=?`, gid).Scan(&h, &w)
	hop, wakes = hopLimit, agentWakesPerHour
	if h.Valid && h.Int64 > 0 {
		hop = int(h.Int64)
	}
	if w.Valid && w.Int64 > 0 {
		wakes = int(w.Int64)
	}
	return hop, wakes
}

// budgetKey counts agent-triggered wakes per owner and group, so one busy group's limit
// doesn't starve the owner's agents elsewhere.
func budgetKey(owner, gid string) string { return "budget:" + owner + ":" + gid }

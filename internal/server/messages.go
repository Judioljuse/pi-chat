package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// canCommandSQL is true when author ?1 (a human) may command agent ?2 in group ?3.
const canCommandSQL = `EXISTS(SELECT 1 FROM principals ag JOIN principals au ON au.id=?1 WHERE ag.id=?2 AND au.kind='human' AND (ag.owner_id=au.id
	OR EXISTS(SELECT 1 FROM grants gr WHERE gr.agent_id=ag.id AND gr.user_id IN (au.id,'*') AND gr.group_id IN (?3,'*') AND NOT (gr.user_id='*' AND gr.group_id='*'))))`

func (s *Server) canCommand(q querier, authorID, agentID, gid string) bool {
	if authorID == "" {
		return false
	}
	var ok bool
	q.QueryRow(`SELECT `+canCommandSQL, authorID, agentID, gid).Scan(&ok)
	return ok
}

// emit stores an event and, after commit, pushes it to human members (and wakes inbox waiters for messages).
func (s *Server) emit(tx *sql.Tx, after *[]func(), gid, typ string, data any) error {
	b, _ := json.Marshal(data)
	var id int64
	if err := tx.QueryRow(`INSERT INTO events(group_id,type,data,created_at) VALUES(?,?,?,?) RETURNING id`, gid, typ, string(b), now()).Scan(&id); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT m.principal_id,p.kind FROM members m JOIN principals p ON p.id=m.principal_id WHERE m.group_id=?`, gid)
	if err != nil {
		return err
	}
	var humans, agents []string
	for rows.Next() {
		var pid, kind string
		rows.Scan(&pid, &kind)
		if kind == "human" {
			humans = append(humans, pid)
		} else {
			agents = append(agents, pid)
		}
	}
	rows.Close()
	f := encodeEvent(id, typ, json.RawMessage(b))
	*after = append(*after, func() {
		for _, pid := range humans {
			s.hub.send(pid, f)
			if typ == "message" {
				s.hub.notify(pid)
			}
		}
		if typ == "message" {
			for _, pid := range agents {
				s.hub.notify(pid)
			}
		}
	})
	return nil
}

func (s *Server) systemMessage(tx *sql.Tx, after *[]func(), gid, text string) error {
	var seq int64
	if err := tx.QueryRow(`UPDATE groups SET last_seq=last_seq+1 WHERE id=? RETURNING last_seq`, gid).Scan(&seq); err != nil {
		return err
	}
	t := now()
	if _, err := tx.Exec(`INSERT INTO messages(group_id,seq,kind,body,created_at) VALUES(?,?,?,?,?)`, gid, seq, "system", text, t); err != nil {
		return err
	}
	return s.emit(tx, after, gid, "message", obj{"seq": seq, "groupId": gid, "kind": "system", "author": nil, "text": text,
		"mentions": []string{}, "mentionAll": false, "replyTo": nil, "hop": 0, "files": []any{}, "createdAt": t, "recalled": false, "new": true})
}

type fileJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// fetchMessages loads messages of gid matching cond (SQL over alias m) as seen by reader.
func (s *Server) fetchMessages(q querier, reader *actor, gid, cond string, args ...any) ([]obj, error) {
	rows, err := q.Query(`SELECT m.seq,m.kind,m.body,m.reply_to,m.hop,m.mention_all,m.created_at,m.recalled_at,
		COALESCE(m.author_id,''),COALESCE(p.kind,''),COALESCE(mb.nick,p.display_name,''),COALESCE(p.owner_id,''),COALESCE(o.display_name,''),COALESCE(p.platform,'')
		FROM messages m LEFT JOIN principals p ON p.id=m.author_id LEFT JOIN members mb ON mb.group_id=m.group_id AND mb.principal_id=m.author_id
		LEFT JOIN principals o ON o.id=p.owner_id WHERE m.group_id=?1 AND (`+cond+`) ORDER BY m.seq`, append([]any{gid}, args...)...)
	if err != nil {
		return nil, err
	}
	var out []obj
	cmd := map[string]bool{}
	for rows.Next() {
		var seq, hop, created int64
		var kind, body, aid, akind, aname, aowner, aownerName, aplatform string
		var replyTo, recalled sql.NullInt64
		var mall int
		if err := rows.Scan(&seq, &kind, &body, &replyTo, &hop, &mall, &created, &recalled, &aid, &akind, &aname, &aowner, &aownerName, &aplatform); err != nil {
			rows.Close()
			return nil, err
		}
		m := obj{"seq": seq, "groupId": gid, "kind": kind, "author": nil, "text": body, "mentions": []string{}, "mentionAll": mall != 0,
			"replyTo": nil, "hop": hop, "files": []fileJSON{}, "createdAt": created, "recalled": recalled.Valid, "new": true}
		if replyTo.Valid {
			m["replyTo"] = replyTo.Int64
		}
		if recalled.Valid {
			m["text"] = ""
		}
		if aid != "" {
			au := obj{"id": aid, "kind": akind, "name": aname, "ownerId": aowner, "ownerName": aownerName, "isYourOwner": false, "canCommandYou": false}
			if aplatform != "" {
				au["platform"] = aplatform
			}
			m["author"] = au
		}
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}
	lo, hi := out[0]["seq"].(int64), out[len(out)-1]["seq"].(int64)
	bySeq := make(map[int64]obj, len(out))
	for _, m := range out {
		bySeq[m["seq"].(int64)] = m
		if au, ok := m["author"].(obj); ok && reader.isAgent() {
			aid := au["id"].(string)
			au["isYourOwner"] = aid == reader.OwnerID
			v, seen := cmd[aid]
			if !seen {
				v = s.canCommand(q, aid, reader.ID, gid)
				cmd[aid] = v
			}
			au["canCommandYou"] = v
		}
	}
	mrows, err := q.Query(`SELECT seq,target_id FROM mentions WHERE group_id=? AND seq BETWEEN ? AND ? AND reason='mention'`, gid, lo, hi)
	if err != nil {
		return nil, err
	}
	for mrows.Next() {
		var seq int64
		var t string
		mrows.Scan(&seq, &t)
		if m := bySeq[seq]; m != nil {
			m["mentions"] = append(m["mentions"].([]string), t)
		}
	}
	mrows.Close()
	frows, err := q.Query(`SELECT seq,id,name,mime,size FROM files WHERE group_id=? AND seq BETWEEN ? AND ? ORDER BY created_at,id`, gid, lo, hi)
	if err != nil {
		return nil, err
	}
	for frows.Next() {
		var seq int64
		var f fileJSON
		frows.Scan(&seq, &f.ID, &f.Name, &f.Mime, &f.Size)
		if m := bySeq[seq]; m != nil && !m["recalled"].(bool) {
			m["files"] = append(m["files"].([]fileJSON), f)
		}
	}
	frows.Close()
	return out, nil
}

func markNew(ms []obj, lastRead int64) {
	for _, m := range ms {
		m["new"] = m["seq"].(int64) > lastRead
	}
}

func (s *Server) advanceRead(tx *sql.Tx, after *[]func(), gid, pid string, seq int64) error {
	res, err := tx.Exec(`UPDATE members SET last_read_seq=? WHERE group_id=? AND principal_id=? AND last_read_seq<?`, seq, gid, pid, seq)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return s.emit(tx, after, gid, "read", obj{"groupId": gid, "memberId": pid, "seq": seq})
	}
	return nil
}

func qInt(c *ctx, k string) (int64, bool, error) {
	v := c.r.URL.Query().Get(k)
	if v == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, false, badRequest("%s 必须是非负整数", k)
	}
	return n, true, nil
}

func (s *Server) getMessages(c *ctx) (any, error) {
	gid := c.r.PathValue("id")
	until, hasUntil, err := qInt(c, "until")
	if err != nil {
		return nil, err
	}
	before, hasBefore, err := qInt(c, "before")
	if err != nil {
		return nil, err
	}
	afterSeq, hasAfter, err := qInt(c, "after")
	if err != nil {
		return nil, err
	}
	limit, hasLimit, err := qInt(c, "limit")
	if err != nil {
		return nil, err
	}
	if !hasLimit || limit == 0 || limit > 100 {
		limit = 50
	}
	if hasBefore || hasAfter {
		if _, err := s.role(s.db.R, c.a, gid); err != nil {
			return nil, err
		}
		var lastRead, lastSeq int64
		s.db.R.QueryRow(`SELECT m.last_read_seq,g.last_seq FROM members m JOIN groups g ON g.id=m.group_id WHERE m.group_id=? AND m.principal_id=?`, gid, c.a.ID).Scan(&lastRead, &lastSeq)
		var ms []obj
		if hasBefore {
			ms, err = s.fetchMessages(s.db.R, c.a, gid, `m.seq IN (SELECT seq FROM messages WHERE group_id=?1 AND seq<?2 ORDER BY seq DESC LIMIT ?3)`, before, limit)
		} else {
			ms, err = s.fetchMessages(s.db.R, c.a, gid, `m.seq IN (SELECT seq FROM messages WHERE group_id=?1 AND seq>?2 ORDER BY seq LIMIT ?3)`, afterSeq, limit)
		}
		if err != nil {
			return nil, err
		}
		markNew(ms, lastRead)
		return obj{"messages": nonNil(ms), "lastSeq": lastSeq, "skipped": 0}, nil
	}
	var res obj
	err = s.tx(func(tx *sql.Tx, after *[]func()) error {
		if _, err := s.role(tx, c.a, gid); err != nil {
			return err
		}
		var lastRead, lastSeq int64
		tx.QueryRow(`SELECT m.last_read_seq,g.last_seq FROM members m JOIN groups g ON g.id=m.group_id WHERE m.group_id=? AND m.principal_id=?`, gid, c.a.ID).Scan(&lastRead, &lastSeq)
		top := lastSeq
		if hasUntil && until < top {
			top = until
		}
		var total int64
		tx.QueryRow(`SELECT COUNT(*) FROM messages WHERE group_id=? AND seq>? AND seq<=?`, gid, lastRead, top).Scan(&total)
		skipped := int64(0)
		if total > 100 {
			skipped = total - 100
		}
		unread, err := s.fetchMessages(tx, c.a, gid, `m.seq IN (SELECT seq FROM messages WHERE group_id=?1 AND seq>?2 AND seq<=?3 ORDER BY seq DESC LIMIT 100)`, lastRead, top)
		if err != nil {
			return err
		}
		first := top + 1
		if len(unread) > 0 {
			first = unread[0]["seq"].(int64)
		}
		ctxMsgs, err := s.fetchMessages(tx, c.a, gid, `m.seq IN (SELECT seq FROM messages WHERE group_id=?1 AND seq<?2 ORDER BY seq DESC LIMIT 5)`, first)
		if err != nil {
			return err
		}
		markNew(unread, lastRead)
		for _, m := range ctxMsgs {
			m["new"] = false
		}
		if len(unread) > 0 {
			if err := s.advanceRead(tx, after, gid, c.a.ID, unread[len(unread)-1]["seq"].(int64)); err != nil {
				return err
			}
		}
		res = obj{"messages": nonNil(append(ctxMsgs, unread...)), "lastSeq": lastSeq, "skipped": skipped}
		return nil
	})
	return res, err
}

func nonNil(ms []obj) []obj {
	if ms == nil {
		return []obj{}
	}
	return ms
}

func (s *Server) markRead(c *ctx) (any, error) {
	gid := c.r.PathValue("id")
	var req struct{ Seq *int64 }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	var seq int64
	err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		if _, err := s.role(tx, c.a, gid); err != nil {
			return err
		}
		tx.QueryRow(`SELECT last_seq FROM groups WHERE id=?`, gid).Scan(&seq)
		if req.Seq != nil && *req.Seq < seq {
			seq = *req.Seq
		}
		return s.advanceRead(tx, after, gid, c.a.ID, seq)
	})
	if err != nil {
		return nil, err
	}
	return obj{"seq": seq}, nil
}

func (s *Server) recall(c *ctx) (any, error) {
	gid := c.r.PathValue("id")
	seq, err := strconv.ParseInt(c.r.PathValue("seq"), 10, 64)
	if err != nil {
		return nil, badRequest("seq 无效")
	}
	err = s.tx(func(tx *sql.Tx, after *[]func()) error {
		if _, err := s.role(tx, c.a, gid); err != nil {
			return err
		}
		var author sql.NullString
		var created int64
		var recalled sql.NullInt64
		if err := tx.QueryRow(`SELECT author_id,created_at,recalled_at FROM messages WHERE group_id=? AND seq=?`, gid, seq).Scan(&author, &created, &recalled); err != nil {
			return notFoundIf(err)
		}
		if author.String != c.a.ID {
			return errf(403, "forbidden", "只能撤回自己的消息")
		}
		if recalled.Valid {
			return nil
		}
		if now()-created > 2*time.Minute.Milliseconds() {
			return errf(400, "recall_expired", "超过 2 分钟，不能撤回")
		}
		if _, err := tx.Exec(`UPDATE messages SET recalled_at=? WHERE group_id=? AND seq=?`, now(), gid, seq); err != nil {
			return err
		}
		return s.emit(tx, after, gid, "recall", obj{"groupId": gid, "seq": seq})
	})
	if err != nil {
		return nil, err
	}
	return obj{}, nil
}

// ---- sending ----

type sendReq struct {
	Text       string   `json:"text"`
	Mentions   []string `json:"mentions"`
	ReplyTo    *int64   `json:"replyTo"`
	OpKey      string   `json:"opKey"`
	Files      []string `json:"files"`
	IgnoreNew  bool     `json:"ignoreNew"`
	Until      *int64   `json:"until"`
	MentionAll bool     `json:"mentionAll"`
}

type gm struct {
	id, kind, nick, owner string
	disabled              bool
}

// target is one principal the message is addressed to.
type target struct {
	id, nick, reason string
}

func isNickEnd(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// prefixFold reports the byte length of the prefix of s that case-insensitively
// equals p, or 0 when s does not start with p.
func prefixFold(s, p string) int {
	if p == "" {
		return 0
	}
	n := 0
	for _, pr := range p {
		if n >= len(s) {
			return 0
		}
		sr, w := utf8.DecodeRuneInString(s[n:])
		if sr != pr && !strings.EqualFold(string(sr), string(pr)) {
			return 0
		}
		n += w
	}
	return n
}

// parseMentions resolves explicit mentions and @nick in text against group members.
// Returns matched member ids (in order), whether @all appeared, and unmatched names.
func parseMentions(text string, explicit []string, ms []gm) (ids []string, all bool, unmatched []string) {
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, e := range explicit {
		e = strings.TrimPrefix(strings.TrimSpace(e), "@")
		if e == "" {
			continue
		}
		if strings.EqualFold(e, "all") {
			all = true
			continue
		}
		found := false
		for _, m := range ms {
			if m.id == e || strings.EqualFold(m.nick, e) {
				add(m.id)
				found = true
				break
			}
		}
		if !found {
			unmatched = append(unmatched, e)
		}
	}
	// Longest nick first so "CC2" wins over "CC".
	sorted := append([]gm(nil), ms...)
	sort.Slice(sorted, func(i, j int) bool {
		return utf8.RuneCountInString(sorted[i].nick) > utf8.RuneCountInString(sorted[j].nick)
	})
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		if i > 0 {
			if c := text[i-1]; c < 0x80 && (unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c))) {
				continue // looks like an email address
			}
		}
		rest := text[i+1:]
		boundary := func(n int) bool {
			if n == len(rest) {
				return true
			}
			r, _ := utf8.DecodeRuneInString(rest[n:])
			return isNickEnd(r)
		}
		matched := false
		for _, m := range sorted {
			if n := prefixFold(rest, m.nick); n > 0 && boundary(n) {
				add(m.id)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		if n := prefixFold(rest, "all"); n > 0 && boundary(n) {
			all = true
			continue
		}
		end := strings.IndexFunc(rest, isNickEnd)
		if end < 0 {
			end = len(rest)
		}
		if end > 0 {
			unmatched = append(unmatched, rest[:end])
		}
	}
	return
}

func (s *Server) sendMessage(c *ctx) (any, error) {
	gid := c.r.PathValue("id")
	var req sendReq
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	n := utf8.RuneCountInString(req.Text)
	if n > 10000 || (n == 0 && len(req.Files) == 0) || strings.TrimSpace(req.Text) == "" && len(req.Files) == 0 {
		return nil, badRequest("消息内容 1 到 10000 字")
	}
	if len(req.Files) > 8 {
		return nil, badRequest("一条消息最多 8 个附件")
	}
	if len(req.OpKey) > 128 {
		return nil, badRequest("opKey 太长")
	}
	if !s.rl.allow("send:"+string(c.a.TokenHash), 30, time.Minute) {
		return nil, errf(429, "rate_limited", "发送太频繁，每分钟最多 30 条")
	}
	a := c.a
	var (
		msg       obj
		dup       bool
		conflict  *apiError
		seq, hop  int64
		targets   []target
		mentioned = []obj{}
		hint      string
		ms        []gm
		role      string
		gkind     string
	)
	err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		var err error
		if role, err = s.role(tx, a, gid); err != nil {
			return err
		}
		if req.OpKey != "" {
			var oseq int64
			if tx.QueryRow(`SELECT seq FROM messages WHERE group_id=? AND author_id=? AND op_key=?`, gid, a.ID, req.OpKey).Scan(&oseq) == nil {
				list, err := s.fetchMessages(tx, a, gid, `m.seq=?2`, oseq)
				if err != nil || len(list) == 0 {
					return err
				}
				msg, dup = list[0], true
				msg["new"] = false
				return nil
			}
		}
		var lastRead, lastSeq int64
		tx.QueryRow(`SELECT m.last_read_seq,g.last_seq,g.kind FROM members m JOIN groups g ON g.id=m.group_id WHERE m.group_id=? AND m.principal_id=?`, gid, a.ID).Scan(&lastRead, &lastSeq, &gkind)
		var replyHop int64 = -1
		var replyAuthor, replyKind string
		if req.ReplyTo != nil {
			var rh int64
			var ra, rk sql.NullString
			var rec sql.NullInt64
			err := tx.QueryRow(`SELECT m.hop,m.author_id,p.kind,m.recalled_at FROM messages m LEFT JOIN principals p ON p.id=m.author_id WHERE m.group_id=? AND m.seq=?`, gid, *req.ReplyTo).Scan(&rh, &ra, &rk, &rec)
			if errors.Is(err, sql.ErrNoRows) {
				return errf(400, "reply_not_found", "要回复的消息不存在").with("reason", "not_found")
			} else if err != nil {
				return err
			}
			if rec.Valid {
				return errf(400, "reply_not_found", "要回复的消息已撤回").with("reason", "recalled")
			}
			replyHop, replyAuthor, replyKind = rh, ra.String, rk.String
		}
		if a.isAgent() && !req.IgnoreNew {
			top := lastSeq
			if req.Until != nil && *req.Until < top {
				top = *req.Until
			}
			var cnt int
			tx.QueryRow(`SELECT COUNT(*) FROM messages WHERE group_id=? AND seq>? AND seq<=? AND (author_id IS NULL OR author_id<>?)`, gid, lastRead, top, a.ID).Scan(&cnt)
			if cnt > 0 {
				var upto int64
				tx.QueryRow(`SELECT MAX(seq) FROM (SELECT seq FROM messages WHERE group_id=? AND seq>? AND seq<=? AND (author_id IS NULL OR author_id<>?) ORDER BY seq LIMIT 20)`, gid, lastRead, top, a.ID).Scan(&upto)
				list, err := s.fetchMessages(tx, a, gid, `m.seq IN (SELECT seq FROM messages WHERE group_id=?1 AND seq>?2 AND seq<=?3 AND (author_id IS NULL OR author_id<>?4) ORDER BY seq LIMIT 20)`, lastRead, top, a.ID)
				if err != nil {
					return err
				}
				if err := s.advanceRead(tx, after, gid, a.ID, upto); err != nil {
					return err
				}
				conflict = errf(409, "new_messages", "发送前群里有 %d 条你还没读的新消息，请先看完再决定是否发送（或带 ignoreNew:true 强制发送）", cnt).
					with("messages", list).with("lastSeq", lastSeq)
				return nil
			}
		}
		// Members for mention resolution.
		rows, err := tx.Query(`SELECT p.id,p.kind,m.nick,COALESCE(p.owner_id,''),p.disabled FROM members m JOIN principals p ON p.id=m.principal_id WHERE m.group_id=?`, gid)
		if err != nil {
			return err
		}
		for rows.Next() {
			var x gm
			var dis int
			rows.Scan(&x.id, &x.kind, &x.nick, &x.owner, &dis)
			x.disabled = dis != 0
			ms = append(ms, x)
		}
		rows.Close()
		byID := map[string]gm{}
		for _, m := range ms {
			byID[m.id] = m
		}
		ids, all, unmatched := parseMentions(req.Text, req.Mentions, ms)
		allOK := (all || req.MentionAll) && a.isHuman() && isManager(role) && gkind == "group"
		seen := map[string]bool{a.ID: true}
		addT := func(id, reason string) {
			if !seen[id] {
				seen[id] = true
				targets = append(targets, target{id: id, nick: byID[id].nick, reason: reason})
			}
		}
		for _, id := range ids {
			if id != a.ID {
				mentioned = append(mentioned, obj{"id": id, "nick": byID[id].nick})
			}
			addT(id, "mention")
		}
		if allOK {
			for _, m := range ms {
				if m.kind == "agent" {
					addT(m.id, "mention")
				}
			}
		}
		if replyKind == "agent" {
			addT(replyAuthor, "reply")
		}
		if gkind == "dm" {
			for _, m := range ms {
				addT(m.id, "dm")
			}
		}
		if len(unmatched) > 0 {
			var nicks []string
			for _, m := range ms {
				if m.id != a.ID {
					nicks = append(nicks, m.nick)
				}
			}
			hint = fmt.Sprintf("没有找到成员：%s。可用的昵称：%s", strings.Join(unmatched, "、"), strings.Join(nicks, "、"))
		}
		// hop
		if a.isAgent() {
			base := int64(-1)
			var ch sql.NullInt64
			if a.RunWake != "" {
				tx.QueryRow(`SELECT cause_hop FROM wakes WHERE id=? AND agent_id=?`, a.RunWake, a.ID).Scan(&ch)
			}
			if !ch.Valid {
				tx.QueryRow(`SELECT MAX(cause_hop) FROM wakes WHERE agent_id=? AND created_at>?`, a.ID, now()-10*time.Minute.Milliseconds()).Scan(&ch)
			}
			if ch.Valid {
				base = ch.Int64
			}
			hop = 1 + max(base, replyHop)
		}
		if err := tx.QueryRow(`UPDATE groups SET last_seq=last_seq+1 WHERE id=? RETURNING last_seq`, gid).Scan(&seq); err != nil {
			return err
		}
		var replyTo any
		if req.ReplyTo != nil {
			replyTo = *req.ReplyTo
		}
		if _, err := tx.Exec(`INSERT INTO messages(group_id,seq,author_id,kind,body,reply_to,hop,mention_all,op_key,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			gid, seq, a.ID, "text", req.Text, replyTo, hop, b2i(allOK), nullStr(req.OpKey), now()); err != nil {
			return err
		}
		for _, t := range targets {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO mentions(target_id,group_id,seq,reason) VALUES(?,?,?,?)`, t.id, gid, seq, t.reason); err != nil {
				return err
			}
		}
		for _, f := range req.Files {
			res, err := tx.Exec(`UPDATE files SET group_id=?,seq=? WHERE id=? AND uploader_id=? AND group_id IS NULL`, gid, seq, f, a.ID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n != 1 {
				return badRequest("附件 %s 不存在或已被使用", f)
			}
		}
		if _, err := tx.Exec(`UPDATE members SET last_read_seq=? WHERE group_id=? AND principal_id=?`, seq, gid, a.ID); err != nil {
			return err
		}
		list, err := s.fetchMessages(tx, a, gid, `m.seq=?2`, seq)
		if err != nil {
			return err
		}
		msg = list[0]
		human := cloneMsg(msg)
		return s.emit(tx, after, gid, "message", human)
	})
	if err != nil {
		return nil, err
	}
	if conflict != nil {
		return nil, conflict
	}
	if dup {
		return obj{"message": msg, "duplicate": true, "mentioned": []obj{}, "woken": []string{}, "notWoken": []obj{}}, nil
	}
	if a.isHuman() {
		s.rl.reset("budget:" + a.ID)
	}
	woken, notWoken := s.wk.dispatch(a, gid, seq, hop, targets, ms)
	res := obj{"message": msg, "mentioned": mentioned, "woken": woken, "notWoken": notWoken}
	if hint != "" {
		res["mentionHint"] = hint
	}
	return res, nil
}

// cloneMsg returns the human-perspective copy stored in events.
func cloneMsg(m obj) obj {
	out := obj{}
	for k, v := range m {
		out[k] = v
	}
	if au, ok := m["author"].(obj); ok {
		cp := obj{}
		for k, v := range au {
			cp[k] = v
		}
		cp["isYourOwner"], cp["canCommandYou"] = false, false
		out["author"] = cp
	}
	out["new"] = true
	return out
}

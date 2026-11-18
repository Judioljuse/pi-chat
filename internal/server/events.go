package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) maxEventID() int64 {
	var id sql.NullInt64
	s.db.R.QueryRow(`SELECT MAX(id) FROM events`).Scan(&id)
	return id.Int64
}

// inboxGroups lists groups with unread messages for the caller.
func (s *Server) inboxGroups(a *actor) ([]obj, error) {
	rows, err := s.db.R.Query(`SELECT g.id,g.kind,g.name,g.last_seq,m.last_read_seq,
		(SELECT COUNT(*) FROM messages x WHERE x.group_id=g.id AND x.seq>m.last_read_seq AND (x.author_id IS NULL OR x.author_id<>?1)),
		(SELECT COUNT(*) FROM mentions t WHERE t.target_id=?1 AND t.group_id=g.id AND t.seq>m.last_read_seq AND t.reason IN ('mention','dm')),
		(SELECT COUNT(*) FROM mentions t WHERE t.target_id=?1 AND t.group_id=g.id AND t.seq>m.last_read_seq AND t.reason='reply'),
		(SELECT COUNT(*) FROM messages x WHERE x.group_id=g.id AND x.seq>m.last_read_seq AND x.author_id IS NOT NULL AND x.author_id<>?1 AND `+
		replaceArgs(canCommandSQL)+`)
		FROM members m JOIN groups g ON g.id=m.group_id WHERE m.principal_id=?1 AND g.last_seq>m.last_read_seq ORDER BY g.id`, a.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []obj{}
	for rows.Next() {
		var id, kind, name string
		var lastSeq, lastRead, unread, ment, rep, cmd int64
		if err := rows.Scan(&id, &kind, &name, &lastSeq, &lastRead, &unread, &ment, &rep, &cmd); err != nil {
			return nil, err
		}
		if unread == 0 || !a.canGroup(id) {
			continue
		}
		if !a.isAgent() {
			cmd = 0
		}
		out = append(out, obj{"id": id, "name": name, "kind": kind, "unread": unread, "mentionsMe": ment, "repliesToMe": rep, "fromCommander": cmd, "lastSeq": lastSeq})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, g := range out {
		if g["kind"] == "dm" {
			var dn string
			s.db.R.QueryRow(`SELECT p.display_name FROM members m JOIN principals p ON p.id=m.principal_id WHERE m.group_id=? AND m.principal_id<>?`, g["id"], a.ID).Scan(&dn)
			g["name"] = dn
		}
	}
	return out, nil
}

// replaceArgs rewrites canCommandSQL for use inside the inbox query (author=x.author_id, agent=?1, group=g.id).
func replaceArgs(q string) string {
	r := []byte{}
	for i := 0; i < len(q); i++ {
		if q[i] == '?' && i+1 < len(q) {
			switch q[i+1] {
			case '1':
				r = append(r, "x.author_id"...)
				i++
				continue
			case '2':
				r = append(r, "?1"...)
				i++
				continue
			case '3':
				r = append(r, "g.id"...)
				i++
				continue
			}
		}
		r = append(r, q[i])
	}
	return string(r)
}

// relevantSince reports whether a message event newer than since exists in one of a's groups.
func (s *Server) relevantSince(a *actor, since int64) bool {
	var ok bool
	s.db.R.QueryRow(`SELECT EXISTS(SELECT 1 FROM events e JOIN members m ON m.group_id=e.group_id AND m.principal_id=?
		WHERE e.id>? AND e.type='message')`, a.ID, since).Scan(&ok)
	return ok
}

func (s *Server) inbox(c *ctx) (any, error) {
	since, hasSince, err := qInt(c, "since")
	if err != nil {
		return nil, err
	}
	wait, _, err := qInt(c, "wait")
	if err != nil {
		return nil, err
	}
	wait = min(wait, 25)
	if hasSince && wait > 0 {
		ch := s.hub.wait(c.a.ID)
		if !s.relevantSince(c.a, since) {
			t := time.NewTimer(time.Duration(wait) * time.Second)
			select {
			case <-ch:
			case <-t.C:
			case <-c.r.Context().Done():
			}
			t.Stop()
		}
		s.hub.unwait(c.a.ID, ch)
	}
	cursor := s.maxEventID()
	groups, err := s.inboxGroups(c.a)
	if err != nil {
		return nil, err
	}
	return obj{"you": obj{"id": c.a.ID, "name": c.a.DisplayName}, "cursor": cursor, "idle": len(groups) == 0, "groups": groups}, nil
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(w, r)
	if err == nil && a == nil {
		err = errUnauthorized()
	}
	if err == nil && (a.TokenKind == "run") {
		err = errf(403, "forbidden", "run token 不能订阅事件")
	}
	if err != nil {
		s.writeErr(w, err)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		s.writeErr(w, errf(500, "internal", "不支持流式响应"))
		return
	}
	var lastID int64 = -1
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			lastID = n
		}
	}
	sub := s.hub.subscribe(a.ID, a.isAgent())
	defer s.hub.unsubscribe(sub)
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	write := func(b []byte) bool {
		rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if _, err := w.Write(b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	cursor := s.maxEventID()
	var you any
	if a.isHuman() {
		you, _ = s.loadUser(s.db.R, a.ID)
	} else {
		you, _ = s.agentMe(a)
	}
	if !write(encodeEvent(0, "hello", obj{"you": you, "cursor": cursor}).data) {
		return
	}
	// Replay stored events for humans resuming with Last-Event-ID.
	replayed := cursor
	if lastID >= 0 && a.isHuman() && lastID != cursor {
		var minID sql.NullInt64
		s.db.R.QueryRow(`SELECT MIN(id) FROM events`).Scan(&minID)
		var frames []frame
		ok := lastID < cursor && minID.Valid && minID.Int64 <= lastID+1
		if ok {
			const maxReplay = 5000
			rows, err := s.db.R.Query(`SELECT e.id,e.type,e.data FROM events e JOIN members m ON m.group_id=e.group_id AND m.principal_id=?
				WHERE e.id>? AND e.id<=? ORDER BY e.id LIMIT ?`, a.ID, lastID, cursor, maxReplay+1)
			if err != nil {
				ok = false
			} else {
				for rows.Next() {
					var id int64
					var typ, data string
					rows.Scan(&id, &typ, &data)
					frames = append(frames, encodeEvent(id, typ, json.RawMessage(data)))
				}
				rows.Close()
				ok = len(frames) <= maxReplay
			}
		}
		if !ok {
			// Unknown, expired or too-far-behind cursor: the client must refetch.
			frames = []frame{encodeEvent(0, "resync", obj{})}
		}
		for _, f := range frames {
			if !write(f.data) {
				return
			}
		}
	}
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case f := <-sub.ch:
			if f.id > 0 && f.id <= replayed {
				continue
			}
			if !write(f.data) {
				return
			}
		case <-sub.done:
			// Flush anything queued (replaced / deleted) before closing.
			for {
				select {
				case f := <-sub.ch:
					write(f.data)
				default:
					return
				}
			}
		case <-ping.C:
			if !write([]byte(": ping\n\n")) {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

// Agent runtime: which local session the listener runs the agent in, and whether
// it is idle, running a job or attached to an interactive terminal.

var sessionRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// cleanSession returns s if it is a plausible session id, else "".
func cleanSession(s string) string {
	s = strings.TrimSpace(s)
	if sessionRe.MatchString(s) {
		return s
	}
	return ""
}

// cleanLabel keeps [A-Za-z0-9._-] of s and truncates to n bytes (host names, local names).
func cleanLabel(s string, n int) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 && (r == '.' || r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
			if b.Len() >= n {
				break
			}
		}
	}
	return b.String()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// viaFor returns the provenance JSON of a message posted by a (nil for humans).
func viaFor(a *actor, r *http.Request) any {
	if !a.isAgent() {
		return nil
	}
	var v obj
	if a.TokenKind == "run" {
		v = obj{"kind": "run", "session": nullable(a.RunSession), "tier": nullable(a.RunTier), "host": nullable(a.RunHost)}
	} else {
		v = obj{"kind": "direct", "session": nullable(cleanSession(r.Header.Get("X-Pichat-Session"))),
			"host": nullable(cleanLabel(r.Header.Get("X-Pichat-Host"), 64))}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// maxNotice is the longest runtime notice kept, in runes.
const maxNotice = 200

// cleanNotice strips control and format characters (line breaks, bidi overrides),
// collapses whitespace and truncates to maxNotice runes.
func cleanNotice(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s)
	return truncRunes(strings.Join(strings.Fields(s), " "), maxNotice)
}

var runtimeStates = map[string]bool{"idle": true, "running": true, "attached": true}

type runtimeInfo struct {
	State, Session, Host, Name, Notice string
	At, NoticeAt                       int64
}

func (s *Server) loadRuntime(id string) runtimeInfo {
	var st, sess, host, name, notice sql.NullString
	var at, noticeAt sql.NullInt64
	s.db.R.QueryRow(`SELECT runtime_state,main_session,runtime_host,runtime_name,runtime_at,runtime_notice,runtime_notice_at FROM principals WHERE id=?`, id).
		Scan(&st, &sess, &host, &name, &at, &notice, &noticeAt)
	return runtimeInfo{State: s.effectiveState(id, st.String), Session: sess.String, Host: host.String, Name: name.String, At: at.Int64,
		Notice: notice.String, NoticeAt: noticeAt.Int64}
}

// effectiveState maps the stored listener state to what clients see: offline without an SSE connection.
func (s *Server) effectiveState(id, stored string) string {
	if !s.hub.online(id) {
		return "offline"
	}
	if stored == "" {
		return "idle"
	}
	return stored
}

// publicNotice is what group members other than the agent's owner see: the full notice can hold a
// local command line (paths, prompt text) and pid.
func publicNotice(n string) string {
	switch {
	case n == "":
		return ""
	case strings.HasPrefix(n, "主会话正在被"):
		return "主会话正在被终端使用，群消息已排队"
	case strings.Contains(n, "分叉"):
		return "检测到会话分叉"
	default:
		return "agent 有一条运行提示"
	}
}

// noticeAt is null when there is no notice.
func (r runtimeInfo) noticeAt() any {
	if r.Notice == "" {
		return nil
	}
	return r.NoticeAt
}

func (r runtimeInfo) json() obj {
	return obj{"state": r.State, "session": nullable(r.Session), "host": nullable(r.Host), "name": nullable(r.Name), "at": r.At,
		"notice": nullable(r.Notice), "noticeAt": r.noticeAt()}
}

// broadcastAgent pushes the agent's runtime to its owner and the human members of its groups.
// Calls are serialized and each re-reads the state, so the last event sent is always the latest state.
func (s *Server) broadcastAgent(id string) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	humans, err := scanStrings(s.db.R.Query(`SELECT DISTINCT m2.principal_id FROM members m1 JOIN members m2 ON m2.group_id=m1.group_id
		JOIN principals p ON p.id=m2.principal_id WHERE m1.principal_id=?1 AND p.kind='human'
		UNION SELECT owner_id FROM principals WHERE id=?1 AND owner_id IS NOT NULL`, id))
	if err != nil {
		s.log.Error("agent broadcast", "err", err)
		return
	}
	rt := s.loadRuntime(id)
	var owner string
	s.db.R.QueryRow(`SELECT COALESCE(owner_id,'') FROM principals WHERE id=?`, id).Scan(&owner)
	ev := func(notice string) frame {
		return encodeEvent(0, "agent", obj{"agentId": id, "state": rt.State, "session": nullable(rt.Session), "host": nullable(rt.Host), "name": nullable(rt.Name),
			"notice": nullable(notice), "noticeAt": rt.noticeAt()})
	}
	full, public := ev(rt.Notice), ev(publicNotice(rt.Notice))
	for _, pid := range humans {
		if pid == owner {
			s.hub.sendHumans(pid, full)
		} else {
			s.hub.sendHumans(pid, public)
		}
	}
}

func (s *Server) agentSessions(id string) ([]obj, error) {
	rows, err := s.db.R.Query(`SELECT session,COALESCE(host,''),started_at FROM agent_sessions WHERE agent_id=? ORDER BY started_at DESC, id DESC LIMIT 20`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []obj{}
	for rows.Next() {
		var sess, host string
		var at int64
		if err := rows.Scan(&sess, &host, &at); err != nil {
			return nil, err
		}
		out = append(out, obj{"session": sess, "host": nullable(host), "startedAt": at})
	}
	return out, rows.Err()
}

func (s *Server) putRuntime(c *ctx) (any, error) {
	var req struct {
		State, Session, Host, Name string
		Notice                     *string // omitted: keep the current notice; "" clears it
	}
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if !runtimeStates[req.State] {
		return nil, badRequest("state 只能是 idle、running 或 attached")
	}
	sess, host, name := cleanSession(req.Session), cleanLabel(req.Host, 64), cleanLabel(req.Name, 40)
	t := now()
	err := s.tx(func(tx *sql.Tx, _ *[]func()) error {
		var cur sql.NullString
		if err := tx.QueryRow(`SELECT main_session FROM principals WHERE id=?`, c.a.ID).Scan(&cur); err != nil {
			return err
		}
		if sess != "" && sess != cur.String {
			if _, err := tx.Exec(`INSERT INTO agent_sessions(agent_id,session,host,started_at) VALUES(?,?,?,?)`, c.a.ID, sess, nullStr(host), t); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE principals SET runtime_state=?, main_session=COALESCE(?,main_session), runtime_host=COALESCE(?,runtime_host),
			runtime_name=COALESCE(?,runtime_name), runtime_at=? WHERE id=?`, req.State, nullStr(sess), nullStr(host), nullStr(name), t, c.a.ID); err != nil {
			return err
		}
		if req.Notice == nil {
			return nil
		}
		// Re-reporting the same notice keeps its original time.
		n := cleanNotice(*req.Notice)
		_, err := tx.Exec(`UPDATE principals SET runtime_notice=?1,
			runtime_notice_at=CASE WHEN ?1 IS NULL THEN NULL WHEN runtime_notice IS ?1 THEN runtime_notice_at ELSE ?2 END WHERE id=?3`,
			nullStr(n), t, c.a.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.broadcastAgent(c.a.ID)
	return obj{"runtime": s.loadRuntime(c.a.ID).json()}, nil
}

// dismissNotice clears the agent's runtime notice (owner only).
func (s *Server) dismissNotice(c *ctx) (any, error) {
	id := c.r.PathValue("id")
	if err := s.ownAgent(s.db.R, c.a, id); err != nil {
		return nil, err
	}
	err := s.tx(func(tx *sql.Tx, _ *[]func()) error {
		_, err := tx.Exec(`UPDATE principals SET runtime_notice=NULL, runtime_notice_at=NULL WHERE id=?`, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.broadcastAgent(id)
	return obj{"runtime": s.loadRuntime(id).json()}, nil
}

func (s *Server) resetSession(c *ctx) (any, error) {
	id := c.r.PathValue("id")
	if err := s.ownAgent(s.db.R, c.a, id); err != nil {
		return nil, err
	}
	// Not queued: an offline listener never sees it, so tell the caller.
	delivered := s.hub.online(id)
	s.hub.send(id, encodeEvent(0, "reset_session", obj{}))
	return obj{"delivered": delivered}, nil
}

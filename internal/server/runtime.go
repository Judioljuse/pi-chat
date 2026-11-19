package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
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

var runtimeStates = map[string]bool{"idle": true, "running": true, "attached": true}

type runtimeInfo struct {
	State, Session, Host, Name string
	At                         int64
}

func (s *Server) loadRuntime(id string) runtimeInfo {
	var st, sess, host, name sql.NullString
	var at sql.NullInt64
	s.db.R.QueryRow(`SELECT runtime_state,main_session,runtime_host,runtime_name,runtime_at FROM principals WHERE id=?`, id).
		Scan(&st, &sess, &host, &name, &at)
	return runtimeInfo{State: s.effectiveState(id, st.String), Session: sess.String, Host: host.String, Name: name.String, At: at.Int64}
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

func (r runtimeInfo) json() obj {
	return obj{"state": r.State, "session": nullable(r.Session), "host": nullable(r.Host), "name": nullable(r.Name), "at": r.At}
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
	f := encodeEvent(0, "agent", obj{"agentId": id, "state": rt.State, "session": nullable(rt.Session), "host": nullable(rt.Host), "name": nullable(rt.Name)})
	for _, pid := range humans {
		s.hub.sendHumans(pid, f)
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
	var req struct{ State, Session, Host, Name string }
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
		_, err := tx.Exec(`UPDATE principals SET runtime_state=?, main_session=COALESCE(?,main_session), runtime_host=COALESCE(?,runtime_host),
			runtime_name=COALESCE(?,runtime_name), runtime_at=? WHERE id=?`, req.State, nullStr(sess), nullStr(host), nullStr(name), t, c.a.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.broadcastAgent(c.a.ID)
	return obj{"runtime": s.loadRuntime(c.a.ID).json()}, nil
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

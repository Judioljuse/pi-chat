package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
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
	Listener                           *listenerReport
	Dirs                               *runtimeDirs // nil: the listener never reported them (older listener)
}

// Limits on the work dirs a listener reports.
const (
	maxDirLen  = 300
	maxDirBind = 50
)

// runtimeDirs is where the listener runs the agent: the main session's cwd, config.dir and per-group binds.
// Only the agent's owner sees it.
type runtimeDirs struct {
	Main    string            `json:"main,omitempty"`
	Default string            `json:"default,omitempty"`
	Binds   map[string]string `json:"binds,omitempty"`
	// Repo is the git checkout of main (or default when main has none), v0.2.7; nil when unknown.
	Repo *repoInfo `json:"repo,omitempty"`
	// Repos is the git checkout of each reported dir (main, default, binds), keyed by that dir.
	Repos map[string]repoInfo `json:"repos,omitempty"`
}

// repoInfo is the git checkout a dir is in, as the listener sees it. Root is the main repository's
// root (for a worktree: the checkout the worktree belongs to), Path the worktree's own top level.
type repoInfo struct {
	Root     string `json:"root"`
	Branch   string `json:"branch,omitempty"`
	Worktree bool   `json:"worktree"`
	Path     string `json:"path,omitempty"`
}

const maxBranchLen = 100

// cleanBranch accepts a branch name (or "HEAD" when detached) of at most maxBranchLen runes without
// control/format characters or whitespace; else "".
func cleanBranch(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxBranchLen {
		return ""
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) {
			return ""
		}
	}
	return s
}

// parseRepo sanitizes one reported repo object; nil when it has no valid root or a non-bool worktree.
func parseRepo(v any) *repoInfo {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	root := cleanDir(m["root"])
	if root == "" {
		return nil
	}
	wt, ok := m["worktree"].(bool)
	if !ok && m["worktree"] != nil {
		return nil
	}
	r := &repoInfo{Root: root, Branch: cleanBranch(m["branch"]), Worktree: wt}
	if wt {
		r.Path = cleanDir(m["path"])
	}
	return r
}

var bindKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// cleanDir returns s trimmed if it is a plausible absolute path (POSIX, C:\ or \\server) of at most
// maxDirLen characters without control or format characters, else "".
func cleanDir(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > maxDirLen || !utf8.ValidString(s) {
		return ""
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return ""
		}
	}
	abs := s[0] == '/' || strings.HasPrefix(s, `\\`) ||
		len(s) >= 3 && (s[0]|0x20) >= 'a' && (s[0]|0x20) <= 'z' && s[1] == ':' && (s[2] == '\\' || s[2] == '/')
	if !abs {
		return ""
	}
	return s
}

// parseDirs sanitizes a reported dirs object. ok is false when raw is absent or null (keep the stored value).
// Non-string values, relative paths and bad group ids are dropped; at most maxDirBind binds are kept.
func parseDirs(raw json.RawMessage) (d *runtimeDirs, ok bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var in struct {
		Main, Default, Repo any
		Binds, Repos        map[string]any
	}
	if json.Unmarshal(raw, &in) != nil {
		return nil, false
	}
	d = &runtimeDirs{Main: cleanDir(in.Main), Default: cleanDir(in.Default)}
	keys := make([]string, 0, len(in.Binds))
	for k := range in.Binds {
		if bindKeyRe.MatchString(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if len(d.Binds) >= maxDirBind {
			break
		}
		if dir := cleanDir(in.Binds[k]); dir != "" {
			if d.Binds == nil {
				d.Binds = map[string]string{}
			}
			d.Binds[k] = dir
		}
	}
	d.Repo = parseRepo(in.Repo)
	// Repos only for dirs kept above.
	for _, dir := range d.reported() {
		if r := parseRepo(in.Repos[dir]); r != nil {
			if d.Repos == nil {
				d.Repos = map[string]repoInfo{}
			}
			d.Repos[dir] = *r
		}
	}
	return d, true
}

// reported lists the distinct dirs in d (main, default, binds).
func (d *runtimeDirs) reported() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(d.Main)
	add(d.Default)
	for _, v := range d.Binds {
		add(v)
	}
	return out
}

func repoJSON(r *repoInfo) any {
	if r == nil {
		return nil
	}
	return obj{"root": r.Root, "branch": nullable(r.Branch), "worktree": r.Worktree, "path": nullable(r.Path)}
}

// dirsJSON is the owner-only view of the dirs (nil when never reported).
func (s *Server) dirsJSON(d *runtimeDirs) any {
	if d == nil {
		return nil
	}
	binds := d.Binds
	if binds == nil {
		binds = map[string]string{}
	}
	repos := obj{}
	for k, r := range d.Repos {
		repos[k] = repoJSON(&r)
	}
	return obj{"main": nullable(d.Main), "default": nullable(d.Default), "binds": binds, "repo": repoJSON(d.Repo), "repos": repos}
}

// listenerReport is the listener script version a listener reports (sha256 prefix, server version it came from).
type listenerReport struct {
	Sha256     string `json:"sha256"`
	Version    string `json:"version"`
	AutoUpdate bool   `json:"autoUpdate"`
	Skip       string `json:"skip,omitempty"` // why the listener won't update itself: git (work tree) | readonly
	latest     bool
}

// clean bounds the reported fields.
func (l *listenerReport) clean() *listenerReport {
	if l == nil {
		return nil
	}
	sha := strings.ToLower(cleanLabel(l.Sha256, 64))
	ver := cleanLabel(l.Version, 40)
	if ver == "" {
		ver = "unknown"
	}
	skip := l.Skip
	if skip != "git" && skip != "readonly" {
		skip = ""
	}
	return &listenerReport{Sha256: sha, Version: ver, AutoUpdate: l.AutoUpdate, Skip: skip}
}

func (s *Server) loadRuntime(id string) runtimeInfo {
	var st, sess, host, name, notice, lis, dirs sql.NullString
	var at, noticeAt sql.NullInt64
	s.db.R.QueryRow(`SELECT runtime_state,main_session,runtime_host,runtime_name,runtime_at,runtime_notice,runtime_notice_at,runtime_listener,runtime_dirs FROM principals WHERE id=?`, id).
		Scan(&st, &sess, &host, &name, &at, &notice, &noticeAt, &lis, &dirs)
	rt := runtimeInfo{State: s.effectiveState(id, st.String), Session: sess.String, Host: host.String, Name: name.String, At: at.Int64,
		Notice: notice.String, NoticeAt: noticeAt.Int64}
	if lis.Valid {
		var l listenerReport
		if json.Unmarshal([]byte(lis.String), &l) == nil {
			l.latest = len(l.Sha256) >= 12 && strings.HasPrefix(s.listenerSHA(), l.Sha256)
			rt.Listener = &l
		}
	}
	if dirs.Valid {
		var d runtimeDirs
		if json.Unmarshal([]byte(dirs.String), &d) == nil {
			rt.Dirs = &d
		}
	}
	return rt
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

// ownerJSON is the full runtime, for the agent's owner and the agent itself (includes the work dirs).
func (s *Server) ownerJSON(r runtimeInfo) obj {
	return obj{"state": r.State, "session": nullable(r.Session), "host": nullable(r.Host), "name": nullable(r.Name), "at": r.At,
		"notice": nullable(r.Notice), "noticeAt": r.noticeAt(), "listener": r.listenerJSON(), "dirs": s.dirsJSON(r.Dirs)}
}

// listenerJSON adds latest: whether the reported script matches the one this server serves (nil if never reported).
func (r runtimeInfo) listenerJSON() any {
	if r.Listener == nil {
		return nil
	}
	return obj{"sha256": r.Listener.Sha256, "version": r.Listener.Version, "autoUpdate": r.Listener.AutoUpdate, "skip": nullable(r.Listener.Skip), "latest": r.Listener.latest}
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
	// Host, machine name and work dirs are owner-only; other members get state, session and the public notice.
	full := encodeEvent(0, "agent", obj{"agentId": id, "state": rt.State, "session": nullable(rt.Session), "host": nullable(rt.Host), "name": nullable(rt.Name),
		"notice": nullable(rt.Notice), "noticeAt": rt.noticeAt(), "dirs": s.dirsJSON(rt.Dirs)})
	public := encodeEvent(0, "agent", obj{"agentId": id, "state": rt.State, "session": nullable(rt.Session),
		"notice": nullable(publicNotice(rt.Notice)), "noticeAt": rt.noticeAt()})
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
		Notice                     *string         // omitted: keep the current notice; "" clears it
		Listener                   *listenerReport // omitted: keep the stored one
		Dirs                       json.RawMessage // omitted or null: keep the stored one
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
		if l := req.Listener.clean(); l != nil {
			b, _ := json.Marshal(l)
			if _, err := tx.Exec(`UPDATE principals SET runtime_listener=? WHERE id=?`, string(b), c.a.ID); err != nil {
				return err
			}
		}
		if d, ok := parseDirs(req.Dirs); ok {
			b, _ := json.Marshal(d)
			if _, err := tx.Exec(`UPDATE principals SET runtime_dirs=? WHERE id=?`, string(b), c.a.ID); err != nil {
				return err
			}
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
	return obj{"runtime": s.ownerJSON(s.loadRuntime(c.a.ID))}, nil
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
	return obj{"runtime": s.ownerJSON(s.loadRuntime(id))}, nil
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

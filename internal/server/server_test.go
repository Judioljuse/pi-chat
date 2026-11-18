package server

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"pichat/internal/store"
)

type env struct {
	t     *testing.T
	s     *Server
	ts    *httptest.Server
	n     int
	admin *client
}

func newEnv(t *testing.T) *env {
	var db *store.DB
	var err error
	dir := t.TempDir()
	db, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(Config{PublicURL: "http://pichat.test", Data: dir}, db, Assets{Web: fstest.MapFS{"index.html": {Data: []byte("<html>")}}, Listener: []byte("//")})
	s.WakeDelay = 50 * time.Millisecond
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { s.Close(); ts.Close() })
	return &env{t: t, s: s, ts: ts}
}

// client is either a cookie-authenticated human or a bearer agent.
type client struct {
	e     *env
	hc    *http.Client
	token string
	id    string
}

type resp struct {
	status int
	body   map[string]any
	raw    string
}

func (r resp) errCode() string {
	if e, ok := r.body["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func (c *client) do(method, path string, body any) resp {
	c.e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.ts.URL+"/api/v1"+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	r, err := c.hc.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	out := resp{status: r.StatusCode, raw: string(raw)}
	json.Unmarshal(raw, &out.body)
	return out
}

func (c *client) ok(method, path string, body any) map[string]any {
	c.e.t.Helper()
	r := c.do(method, path, body)
	if r.status != 200 {
		c.e.t.Fatalf("%s %s -> %d %s", method, path, r.status, r.raw)
	}
	return r.body
}

func (e *env) anon() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, hc: &http.Client{Jar: jar}}
}

// human registers a new user; the first one is created via the admin link.
func (e *env) human(name string) *client {
	e.t.Helper()
	var code string
	if e.n == 0 {
		u, err := e.s.AdminLink()
		if err != nil {
			e.t.Fatal(err)
		}
		code = u[strings.LastIndex(u, "/")+1:]
		e.n++
		c := e.anon()
		r := c.ok("POST", "/auth/redeem", map[string]any{"code": code, "name": name, "password": "password1"})
		c.id = r["user"].(map[string]any)["id"].(string)
		e.admin = c
		return c
	}
	code = e.admin.ok("POST", "/admin/invites", nil)["code"].(string)
	e.n++
	c := e.anon()
	r := c.ok("POST", "/auth/redeem", map[string]any{"code": code, "name": name, "password": "password1"})
	c.id = r["user"].(map[string]any)["id"].(string)
	return c
}

// agent pairs a new agent owned by h, optionally into a group.
func (e *env) agent(h *client, name, gid string) *client {
	e.t.Helper()
	body := map[string]any{}
	if gid != "" {
		body["groupId"] = gid
	}
	code := h.ok("POST", "/pairing-codes", body)["code"].(string)
	r := e.anon().ok("POST", "/pair", map[string]any{"code": strings.ToLower(code), "name": name, "platform": "claude_code"})
	return &client{e: e, hc: http.DefaultClient, token: r["token"].(string), id: r["agent"].(map[string]any)["id"].(string)}
}

func (h *client) group(name string, members ...*client) string {
	gid := h.ok("POST", "/groups", map[string]any{"name": name})["group"].(map[string]any)["id"].(string)
	code := ""
	for _, m := range members {
		if m.token != "" {
			h.ok("POST", "/groups/"+gid+"/members", map[string]any{"agentId": m.id})
			continue
		}
		if code == "" {
			code = h.ok("POST", "/groups/"+gid+"/invite", nil)["code"].(string)
		}
		m.ok("POST", "/auth/redeem", map[string]any{"code": code})
	}
	return gid
}

func (c *client) send(gid string, body map[string]any) map[string]any {
	c.e.t.Helper()
	return c.ok("POST", "/groups/"+gid+"/messages", body)
}

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func notWokenReasons(r map[string]any) map[string]string {
	m := map[string]string{}
	for _, x := range r["notWoken"].([]any) {
		o := x.(map[string]any)
		m[o["id"].(string)] = o["reason"].(string)
	}
	return m
}

func TestPairingFlow(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("dev")
	cc := e.agent(alice, "CC", gid)
	me := cc.ok("GET", "/me", nil)
	if me["kind"] != "agent" || me["ownerId"] != alice.id || me["ownerName"] != "alice" || me["commanderTier"] != "project" {
		t.Fatalf("me: %v", me)
	}
	g := cc.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)
	if len(g["members"].([]any)) != 2 {
		t.Fatalf("agent should auto-join group: %v", g)
	}
	// Code is single-use.
	code := alice.ok("POST", "/pairing-codes", nil)["code"].(string)
	e.anon().ok("POST", "/pair", map[string]any{"code": code, "name": "X"})
	if r := e.anon().do("POST", "/pair", map[string]any{"code": code, "name": "Y"}); r.status != 404 {
		t.Fatalf("reuse: %d", r.status)
	}
	// Reset token: old token dies, new one maps to the same agent.
	rc := alice.ok("POST", "/agents/"+cc.id+"/reset-token", nil)["code"].(string)
	r := e.anon().ok("POST", "/pair", map[string]any{"code": rc})
	if r["agent"].(map[string]any)["id"] != cc.id {
		t.Fatalf("reset pair: %v", r)
	}
	if cc.do("GET", "/me", nil).status != 401 {
		t.Fatal("old token should be revoked")
	}
	agents := alice.ok("GET", "/agents", nil)["agents"].([]any)
	if len(agents) != 2 {
		t.Fatalf("agents: %v", agents)
	}
}

func TestSendReadUnread(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	gid := alice.group("g", bob)
	for i := 0; i < 3; i++ {
		alice.send(gid, map[string]any{"text": fmt.Sprintf("hi %d", i)})
	}
	groups := bob.ok("GET", "/groups", nil)["groups"].([]any)
	g := groups[0].(map[string]any)
	if g["unread"].(float64) != 3 || g["lastMessage"].(map[string]any)["text"] != "hi 2" {
		t.Fatalf("summary: %v", g)
	}
	r := bob.ok("GET", "/groups/"+gid+"/messages", nil)
	msgs := r["messages"].([]any)
	// 1 system "bob joined" message as context + 3 new.
	newCount := 0
	for _, m := range msgs {
		if m.(map[string]any)["new"] == true {
			newCount++
		}
	}
	if newCount != 3 {
		t.Fatalf("new: %d %v", newCount, msgs)
	}
	g = bob.ok("GET", "/groups", nil)["groups"].([]any)[0].(map[string]any)
	if g["unread"].(float64) != 0 {
		t.Fatalf("should be read: %v", g)
	}
	// Pagination doesn't change read state.
	alice.send(gid, map[string]any{"text": "more"})
	p := bob.ok("GET", "/groups/"+gid+"/messages?before=100&limit=2", nil)["messages"].([]any)
	if len(p) != 2 || p[1].(map[string]any)["text"] != "more" {
		t.Fatalf("page: %v", p)
	}
	if bob.ok("GET", "/groups", nil)["groups"].([]any)[0].(map[string]any)["unread"].(float64) != 1 {
		t.Fatal("before= must not mark read")
	}
	bob.ok("POST", "/groups/"+gid+"/read", map[string]any{})
	if bob.ok("GET", "/groups", nil)["groups"].([]any)[0].(map[string]any)["unread"].(float64) != 0 {
		t.Fatal("read failed")
	}
	// Non-member can't read.
	carol := e.human("carol")
	if r := carol.do("GET", "/groups/"+gid+"/messages", nil); r.status != 404 {
		t.Fatalf("non-member: %d", r.status)
	}
	// Recall.
	seq := alice.send(gid, map[string]any{"text": "oops"})["message"].(map[string]any)["seq"].(float64)
	if r := bob.do("DELETE", fmt.Sprintf("/groups/%s/messages/%d", gid, int(seq)), nil); r.status != 403 {
		t.Fatalf("recall by other: %d", r.status)
	}
	alice.ok("DELETE", fmt.Sprintf("/groups/%s/messages/%d", gid, int(seq)), nil)
	m := bob.ok("GET", fmt.Sprintf("/groups/%s/messages?after=%d", gid, int(seq)-1), nil)["messages"].([]any)[0].(map[string]any)
	if m["recalled"] != true || m["text"] != "" {
		t.Fatalf("recalled: %v", m)
	}
	// Reply to missing.
	if r := alice.do("POST", "/groups/"+gid+"/messages", map[string]any{"text": "x", "replyTo": 999}); r.errCode() != "reply_not_found" {
		t.Fatalf("reply: %s", r.raw)
	}
}

func TestNewMessagesConflictAndOpKey(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	cc := e.agent(alice, "CC", gid)
	cc.ok("GET", "/groups/"+gid+"/messages", nil)
	alice.send(gid, map[string]any{"text": "new stuff"})
	r := cc.do("POST", "/groups/"+gid+"/messages", map[string]any{"text": "reply"})
	if r.status != 409 || r.errCode() != "new_messages" {
		t.Fatalf("want 409: %s", r.raw)
	}
	if ms := r.body["error"].(map[string]any)["messages"].([]any); len(ms) != 1 || ms[0].(map[string]any)["text"] != "new stuff" {
		t.Fatalf("409 messages: %v", ms)
	}
	// Marked read: next send passes.
	first := cc.send(gid, map[string]any{"text": "reply", "opKey": "k1"})
	dup := cc.send(gid, map[string]any{"text": "reply", "opKey": "k1"})
	if dup["duplicate"] != true || dup["message"].(map[string]any)["seq"] != first["message"].(map[string]any)["seq"] {
		t.Fatalf("dedupe: %v", dup)
	}
	// ignoreNew bypasses; humans never get 409.
	alice.send(gid, map[string]any{"text": "again"})
	cc.send(gid, map[string]any{"text": "forced", "ignoreNew": true})
	alice.send(gid, map[string]any{"text": "human never 409"})
}

func TestParseMentions(t *testing.T) {
	ms := []gm{{id: "a_1", kind: "agent", nick: "CC"}, {id: "a_2", kind: "agent", nick: "CC2"}, {id: "u_1", kind: "human", nick: "Alice"}, {id: "a_3", kind: "agent", nick: "小派"}}
	cases := []struct {
		text     string
		explicit []string
		ids      []string
		all      bool
		unm      int
	}{
		{"@cc hi", nil, []string{"a_1"}, false, 0},
		{"@CC2, look", nil, []string{"a_2"}, false, 0},
		{"hey @CCX", nil, nil, false, 1},
		{"mail me at x@cc.com", nil, nil, false, 0},
		{"@小派。你好", nil, []string{"a_3"}, false, 0},
		{"@all 开会", nil, nil, true, 0},
		{"no text mention", []string{"u_1", "cc2", "nobody"}, []string{"u_1", "a_2"}, false, 1},
		{"@Alice@CC", nil, []string{"u_1"}, false, 0},
		{"请@CC 看一下", nil, []string{"a_1"}, false, 0},
		{"麻烦@小派", nil, []string{"a_3"}, false, 0},
		{"ȺȺȺȺ @x", nil, nil, false, 1},
		{"\u212a @cc ok", nil, []string{"a_1"}, false, 0},
		{"Ⱥ@ȺȺ", nil, nil, false, 1},
	}
	for _, c := range cases {
		ids, all, unm := parseMentions(c.text, c.explicit, ms)
		if fmt.Sprint(ids) != fmt.Sprint(c.ids) || all != c.all || len(unm) != c.unm {
			t.Errorf("%q %v: got %v %v %v", c.text, c.explicit, ids, all, unm)
		}
	}
}

func TestMentionsAndAll(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	gid := alice.group("g", bob)
	cc := e.agent(alice, "CC", gid)
	pi := e.agent(bob, "Pi", gid)
	r := bob.send(gid, map[string]any{"text": "@cc 看一下", "mentions": []string{pi.id}})
	if w := strs(r["woken"]); len(w) != 2 {
		t.Fatalf("woken: %v", r)
	}
	if m := r["message"].(map[string]any)["mentions"]; len(m.([]any)) != 2 {
		t.Fatalf("mentions: %v", m)
	}
	r = bob.send(gid, map[string]any{"text": "@nobody hi"})
	if r["mentionHint"] == nil {
		t.Fatalf("hint: %v", r)
	}
	// @all from member (bob) is ignored; from owner works.
	r = bob.send(gid, map[string]any{"text": "@all 起床"})
	if len(r["woken"].([]any)) != 0 || r["message"].(map[string]any)["mentionAll"] != false {
		t.Fatalf("member @all: %v", r)
	}
	r = alice.send(gid, map[string]any{"text": "@all 起床"})
	if len(r["woken"].([]any)) != 2 || r["message"].(map[string]any)["mentionAll"] != true {
		t.Fatalf("owner @all: %v", r)
	}
	// Agent @all doesn't wake.
	cc.ok("GET", "/groups/"+gid+"/messages", nil)
	r = cc.send(gid, map[string]any{"text": "@all hi", "mentionAll": true})
	if len(r["woken"].([]any)) != 0 {
		t.Fatalf("agent @all: %v", r)
	}
	// Reply to an agent counts as a mention.
	seq := r["message"].(map[string]any)["seq"]
	r = bob.send(gid, map[string]any{"text": "thanks", "replyTo": seq})
	if w := strs(r["woken"]); len(w) != 1 || w[0] != cc.id {
		t.Fatalf("reply wake: %v", r)
	}
}

func TestCanCommandAndGrants(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	gid := alice.group("g", bob)
	gid2 := alice.group("g2", bob)
	cc := e.agent(alice, "CC", gid)
	alice.ok("POST", "/groups/"+gid2+"/members", map[string]any{"agentId": cc.id})
	q := e.s.db.R
	if !e.s.canCommand(q, alice.id, cc.id, gid) {
		t.Fatal("owner can command")
	}
	if e.s.canCommand(q, bob.id, cc.id, gid) {
		t.Fatal("bob can't yet")
	}
	if e.s.canCommand(q, cc.id, cc.id, gid) {
		t.Fatal("agents never command")
	}
	alice.ok("PUT", "/agents/"+cc.id+"/grants", map[string]any{"userId": bob.id, "groupId": gid})
	if !e.s.canCommand(q, bob.id, cc.id, gid) || e.s.canCommand(q, bob.id, cc.id, gid2) {
		t.Fatal("grant scoped to group")
	}
	alice.ok("PUT", "/agents/"+cc.id+"/grants", map[string]any{"userId": "*", "groupId": gid2})
	if !e.s.canCommand(q, bob.id, cc.id, gid2) {
		t.Fatal("all humans grant")
	}
	if r := alice.do("PUT", "/agents/"+cc.id+"/grants", map[string]any{"userId": "*", "groupId": "*"}); r.status != 400 {
		t.Fatalf("*,* must be rejected: %d", r.status)
	}
	if r := bob.do("PUT", "/agents/"+cc.id+"/grants", map[string]any{"userId": bob.id, "groupId": "*"}); r.status != 404 {
		t.Fatalf("non-owner grant: %d", r.status)
	}
	gr := alice.ok("DELETE", "/agents/"+cc.id+"/grants", map[string]any{"userId": bob.id, "groupId": gid})["grants"].([]any)
	if len(gr) != 1 || e.s.canCommand(q, bob.id, cc.id, gid) {
		t.Fatalf("revoke: %v", gr)
	}
	// canCommandYou appears in the agent's view.
	alice.send(gid, map[string]any{"text": "@CC do it"})
	bob.send(gid, map[string]any{"text": "@CC do it too"})
	ms := cc.ok("GET", "/groups/"+gid+"/messages", nil)["messages"].([]any)
	var flags []bool
	for _, m := range ms {
		if a, ok := m.(map[string]any)["author"].(map[string]any); ok && m.(map[string]any)["new"] == true {
			flags = append(flags, a["canCommandYou"].(bool))
		}
	}
	if fmt.Sprint(flags) != "[true false]" {
		t.Fatalf("canCommandYou: %v", flags)
	}
	inbox := cc.ok("GET", "/inbox", nil)
	if inbox["idle"] != true {
		t.Fatalf("inbox after read: %v", inbox)
	}
}

func TestHopLimit(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	a1 := e.agent(alice, "A1", gid)
	a2 := e.agent(alice, "A2", gid)
	seq := alice.send(gid, map[string]any{"text": "@A1 start"})["message"].(map[string]any)["seq"]
	agents := []*client{a1, a2}
	var r map[string]any
	for i := 0; i < 6; i++ {
		from, to := agents[i%2], agents[(i+1)%2]
		_ = to
		r = from.send(gid, map[string]any{"text": "pass", "replyTo": seq, "ignoreNew": true})
		m := r["message"].(map[string]any)
		if m["hop"].(float64) != float64(i+1) {
			t.Fatalf("hop %d: %v", i, m["hop"])
		}
		seq = m["seq"]
	}
	if notWokenReasons(r)[a1.id] != "hop_limit" {
		t.Fatalf("expected hop_limit: %v", r)
	}
	ms := alice.ok("GET", "/groups/"+gid+"/messages?before=1000&limit=1", nil)["messages"].([]any)
	if last := ms[0].(map[string]any); last["kind"] != "system" || !strings.Contains(last["text"].(string), "上限") {
		t.Fatalf("system notice: %v", last)
	}
	// Human message resets hop to 0.
	r = alice.send(gid, map[string]any{"text": "@A1 go on"})
	if r["message"].(map[string]any)["hop"].(float64) != 0 || len(r["woken"].([]any)) != 1 {
		t.Fatalf("human reset: %v", r)
	}
}

func TestRunTokenScope(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	g1 := alice.group("g1", bob)
	g2 := alice.group("g2")
	cc := e.agent(alice, "CC", g1)
	alice.ok("POST", "/groups/"+g2+"/members", map[string]any{"agentId": cc.id})
	// Restricted: no wakeId.
	rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": g1})
	if rt["commander"] != false || rt["tier"] != "restricted" {
		t.Fatalf("restricted: %v", rt)
	}
	run := &client{e: e, hc: http.DefaultClient, token: rt["token"].(string), id: cc.id}
	run.ok("GET", "/groups/"+g1+"/messages", nil)
	if r := run.do("GET", "/groups/"+g2+"/messages", nil); r.status != 403 {
		t.Fatalf("restricted other group: %d", r.status)
	}
	if r := run.do("GET", "/groups", nil); r.status != 403 {
		t.Fatalf("restricted /groups: %d", r.status)
	}
	if r := run.do("POST", "/run-tokens", map[string]any{"groupId": g1}); r.status != 403 {
		t.Fatalf("run token can't mint: %d", r.status)
	}
	if r := run.do("POST", "/chats", map[string]any{"with": alice.id}); r.status != 403 {
		t.Fatalf("restricted /chats: %d", r.status)
	}
	// Non-commander wake (bob) stays restricted.
	bob.send(g1, map[string]any{"text": "@CC hi"})
	time.Sleep(150 * time.Millisecond)
	var bobWake, aliceWake string
	e.s.db.R.QueryRow(`SELECT id FROM wakes WHERE agent_id=? AND commander=0`, cc.id).Scan(&bobWake)
	if rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": g1, "wakeId": bobWake}); rt["commander"] != false {
		t.Fatalf("bob wake: %v", rt)
	}
	// Owner wake -> commander, tier from agent setting.
	alice.ok("PATCH", "/agents/"+cc.id, map[string]any{"commanderTier": "full"})
	alice.send(g1, map[string]any{"text": "@CC work"})
	time.Sleep(150 * time.Millisecond)
	e.s.db.R.QueryRow(`SELECT id FROM wakes WHERE agent_id=? AND commander=1`, cc.id).Scan(&aliceWake)
	rt = cc.ok("POST", "/run-tokens", map[string]any{"groupId": g1, "wakeId": aliceWake})
	if rt["commander"] != true || rt["tier"] != "full" {
		t.Fatalf("commander: %v", rt)
	}
	cmd := &client{e: e, hc: http.DefaultClient, token: rt["token"].(string), id: cc.id}
	cmd.ok("GET", "/groups/"+g2+"/messages", nil)
	cmd.ok("GET", "/groups", nil)
	cmd.ok("POST", "/chats", map[string]any{"with": alice.id})
	me := cmd.ok("GET", "/me", nil)
	if me["run"].(map[string]any)["groupId"] != g1 || me["run"].(map[string]any)["wakeId"] != aliceWake {
		t.Fatalf("me.run: %v", me)
	}
	// Hop from run token's wake.
	r := cmd.send(g1, map[string]any{"text": "done", "ignoreNew": true})
	if r["message"].(map[string]any)["hop"].(float64) != 1 {
		t.Fatalf("hop: %v", r)
	}
	cmd.ok("DELETE", "/run-tokens", nil)
	if cmd.do("GET", "/me", nil).status != 401 {
		t.Fatal("revoked run token still works")
	}
	// Wake of another agent can't be used.
	other := e.agent(alice, "Other", g1)
	if r := other.do("POST", "/run-tokens", map[string]any{"groupId": g1, "wakeId": aliceWake}); r.status != 404 {
		t.Fatalf("foreign wake: %d", r.status)
	}
}

func readEvent(t *testing.T, br *bufio.Reader) (string, string) {
	t.Helper()
	var typ, data string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("sse read: %v", err)
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case line == "":
			if typ != "" {
				return typ, data
			}
		case strings.HasPrefix(line, "event: "):
			typ = line[7:]
		case strings.HasPrefix(line, "data: "):
			data = line[6:]
		}
	}
}

func openSSE(t *testing.T, c *client, lastID string) (*bufio.Reader, func()) {
	req, _ := http.NewRequest("GET", c.e.ts.URL+"/api/v1/events", nil)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	r, err := c.hc.Do(req)
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("sse: %v %v", err, r)
	}
	return bufio.NewReader(r.Body), func() { r.Body.Close() }
}

func TestSSEHelloWakeReplaced(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	cc := e.agent(alice, "CC", gid)
	br, closeA := openSSE(t, cc, "")
	defer closeA()
	if typ, data := readEvent(t, br); typ != "hello" || !strings.Contains(data, cc.id) {
		t.Fatalf("hello: %s %s", typ, data)
	}
	// Two mentions within the coalescing window -> one wake.
	alice.send(gid, map[string]any{"text": "@CC one"})
	alice.send(gid, map[string]any{"text": "@CC two"})
	typ, data := readEvent(t, br)
	var w map[string]any
	json.Unmarshal([]byte(data), &w)
	if typ != "wake" || w["groupId"] != gid || w["commander"] != true || w["seq"].(float64) < 3 {
		t.Fatalf("wake: %s %s", typ, data)
	}
	var n int
	e.s.db.R.QueryRow(`SELECT COUNT(*) FROM wakes WHERE agent_id=?`, cc.id).Scan(&n)
	if n != 1 {
		t.Fatalf("coalesce: %d wakes", n)
	}
	if !e.s.hub.online(cc.id) {
		t.Fatal("online")
	}
	// New connection replaces the old one.
	br2, closeB := openSSE(t, cc, "")
	defer closeB()
	readEvent(t, br2)
	if typ, _ := readEvent(t, br); typ != "replaced" {
		t.Fatalf("want replaced, got %s", typ)
	}
	// Humans get message events with ids, and can resume.
	hb, closeH := openSSE(t, alice, "")
	readEvent(t, hb)
	alice.send(gid, map[string]any{"text": "plain"})
	if typ, _ := readEvent(t, hb); typ != "message" {
		t.Fatalf("human message: %s", typ)
	}
	closeH()
	cursor := e.s.maxEventID()
	alice.send(gid, map[string]any{"text": "while away"})
	hb, closeH = openSSE(t, alice, fmt.Sprint(cursor))
	defer closeH()
	readEvent(t, hb)
	typ, data = readEvent(t, hb)
	if typ != "message" || !strings.Contains(data, "while away") {
		t.Fatalf("resume: %s %s", typ, data)
	}
	// Deleting the agent sends `deleted`.
	alice.ok("DELETE", "/agents/"+cc.id, nil)
	if typ, _ := readEvent(t, br2); typ != "deleted" {
		t.Fatalf("deleted: %s", typ)
	}
}

func TestInboxLongPoll(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	cc := e.agent(alice, "CC", gid)
	in := cc.ok("GET", "/inbox", nil)
	if in["idle"] != true {
		t.Fatalf("idle: %v", in)
	}
	cursor := int(in["cursor"].(float64))
	done := make(chan map[string]any)
	start := time.Now()
	go func() { done <- cc.ok("GET", fmt.Sprintf("/inbox?since=%d&wait=10", cursor), nil) }()
	time.Sleep(200 * time.Millisecond)
	alice.send(gid, map[string]any{"text": "@CC ping"})
	select {
	case r := <-done:
		if time.Since(start) > 5*time.Second || r["idle"] != false {
			t.Fatalf("inbox: %v", r)
		}
		g := r["groups"].([]any)[0].(map[string]any)
		if g["mentionsMe"].(float64) != 1 || g["fromCommander"].(float64) != 1 || g["unread"].(float64) != 1 {
			t.Fatalf("counts: %v", g)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("long poll did not wake")
	}
	// since with already-new events returns immediately.
	t0 := time.Now()
	cc.ok("GET", fmt.Sprintf("/inbox?since=%d&wait=10", cursor), nil)
	if time.Since(t0) > 2*time.Second {
		t.Fatal("should return immediately")
	}
}

func TestSenderLimitAndBudget(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	gid := alice.group("g", bob)
	cc := e.agent(alice, "CC", gid)
	var r map[string]any
	for i := 0; i < 11; i++ {
		r = bob.send(gid, map[string]any{"text": "@CC hi"})
	}
	if notWokenReasons(r)[cc.id] != "sender_limit" {
		t.Fatalf("sender_limit: %v", r)
	}
	// Owner is never sender-limited.
	if r = alice.send(gid, map[string]any{"text": "@CC hi"}); len(r["woken"].([]any)) != 1 {
		t.Fatalf("owner: %v", r)
	}
}

func TestDMAndFiles(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	cc := e.agent(alice, "CC", "")
	if r := bob.do("POST", "/chats", map[string]any{"with": alice.id}); r.status != 403 {
		t.Fatalf("no shared group: %d", r.status)
	}
	d1 := alice.ok("POST", "/chats", map[string]any{"with": cc.id})["group"].(map[string]any)
	d2 := alice.ok("POST", "/chats", map[string]any{"with": cc.id})["group"].(map[string]any)
	if d1["id"] != d2["id"] || d1["name"] != "CC" {
		t.Fatalf("dm idempotent: %v %v", d1, d2)
	}
	r := alice.send(d1["id"].(string), map[string]any{"text": "hello"})
	if w := strs(r["woken"]); len(w) != 1 || w[0] != cc.id {
		t.Fatalf("dm wake: %v", r)
	}
	// Upload, attach, download.
	var buf bytes.Buffer
	buf.WriteString("--X\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\n\r\n")
	buf.Write([]byte("\x89PNG\r\n\x1a\n0000"))
	buf.WriteString("\r\n--X--\r\n")
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/v1/files", &buf)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=X")
	req.Header.Set("Origin", e.ts.URL)
	resp, err := alice.hc.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("upload %v %v", err, resp.Status)
	}
	var up map[string]any
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()
	f := up["file"].(map[string]any)
	if f["mime"] != "image/png" {
		t.Fatalf("mime: %v", f)
	}
	fid := f["id"].(string)
	if r := bob.do("GET", "/files/"+fid, nil); r.status != 404 {
		t.Fatalf("unbound file visible: %d", r.status)
	}
	m := alice.send(d1["id"].(string), map[string]any{"text": "", "files": []string{fid}})["message"].(map[string]any)
	if len(m["files"].([]any)) != 1 {
		t.Fatalf("files: %v", m)
	}
	dl, _ := http.NewRequest("GET", e.ts.URL+"/api/v1/files/"+fid, nil)
	dl.Header.Set("Authorization", "Bearer "+cc.token)
	resp, _ = http.DefaultClient.Do(dl)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "inline") || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download: %v", resp.Header)
	}
	resp.Body.Close()
}

func TestCSRFAndStatic(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/v1/groups", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "text/plain")
	resp, _ := alice.hc.Do(req)
	if resp.StatusCode != 400 {
		t.Fatalf("csrf: %d", resp.StatusCode)
	}
	for _, p := range []string{"/healthz", "/", "/skill.md", "/pichat-listen.mjs", "/api/v1"} {
		resp, err := http.Get(e.ts.URL + p)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %v %v", p, err, resp.Status)
		}
		resp.Body.Close()
	}
}

func TestWebhookWake(t *testing.T) {
	e := newEnv(t)
	e.s.WebhookRetry = 10 * time.Millisecond
	alice := e.human("alice")
	gid := alice.group("g")
	cc := e.agent(alice, "CC", gid)
	got := make(chan *http.Request, 10)
	bodies := make(chan []byte, 10)
	var fail atomic.Bool
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- r
		bodies <- b
		if fail.Load() {
			w.WriteHeader(500)
		}
	}))
	defer hook.Close()
	if r := alice.do("PUT", "/agents/"+cc.id+"/wake", map[string]any{"kind": "webhook", "url": "ftp://x"}); r.status != 400 {
		t.Fatalf("non-http url: %d", r.status)
	}
	res := alice.ok("PUT", "/agents/"+cc.id+"/wake", map[string]any{"kind": "webhook", "url": hook.URL})
	secret := res["secret"].(string)
	// Loopback is blocked by default (SSRF guard).
	if err := e.s.deliverOnce(hook.URL, secret, []byte("{}")); err == nil || !strings.Contains(err.Error(), "内部地址") {
		t.Fatalf("loopback should be blocked: %v", err)
	}
	e.s.client = webhookClient(mustCIDRs("127.0.0.1"))
	alice.send(gid, map[string]any{"text": "@CC hook"})
	var r *http.Request
	select {
	case r = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("no webhook")
	}
	body := <-bodies
	ts := r.Header.Get("X-Pichat-Timestamp")
	if r.Header.Get("Authorization") != "Bearer "+secret || r.Header.Get("X-Pichat-Signature") != signWebhook(secret, ts, body) {
		t.Fatalf("headers: %v", r.Header)
	}
	var p map[string]any
	json.Unmarshal(body, &p)
	if p["agentId"] != cc.id || p["groupId"] != gid || p["wakeId"] == nil || strings.Contains(p["text"].(string), "hook") {
		t.Fatalf("payload: %v", p)
	}
	// Five failed deliveries (each with one retry) switch the agent to none.
	time.Sleep(100 * time.Millisecond) // let the first delivery record its success
	fail.Store(true)
	e.s.db.W.Exec(`UPDATE wake_configs SET fail_streak=4 WHERE agent_id=?`, cc.id)
	e.s.deliverWebhook(cc.id, hook.URL, secret, obj{"groupId": gid})
	if len(got) != 2 {
		t.Fatalf("want 2 attempts (1 retry), got %d", len(got))
	}
	var kind, lastErr string
	e.s.db.R.QueryRow(`SELECT kind,last_error FROM wake_configs WHERE agent_id=?`, cc.id).Scan(&kind, &lastErr)
	if kind != "none" || !strings.Contains(lastErr, "500") {
		t.Fatalf("kind=%s err=%s", kind, lastErr)
	}
}

func TestOddRunesDoNotWedgeWriter(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	r := alice.send(gid, map[string]any{"text": "ȺȺȺȺ @x"})
	if r["mentionHint"] == nil {
		t.Fatalf("hint: %v", r)
	}
	// A panicking transaction must release the single writer connection.
	func() {
		defer func() { recover() }()
		e.s.tx(func(*sql.Tx, *[]func()) error { panic("boom") })
	}()
	done := make(chan bool)
	go func() { alice.send(gid, map[string]any{"text": "hello"}); done <- true }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("writer wedged")
	}
}

func TestConflictMarksOnlyReturned(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	cc := e.agent(alice, "CC", gid)
	cc.ok("GET", "/groups/"+gid+"/messages", nil)
	for i := 0; i < 25; i++ {
		alice.send(gid, map[string]any{"text": fmt.Sprint("m", i)})
	}
	r := cc.do("POST", "/groups/"+gid+"/messages", map[string]any{"text": "x"})
	ms := r.body["error"].(map[string]any)["messages"].([]any)
	if r.status != 409 || len(ms) != 20 || ms[0].(map[string]any)["text"] != "m0" {
		t.Fatalf("first 409: %d %d %v", r.status, len(ms), ms[0])
	}
	r = cc.do("POST", "/groups/"+gid+"/messages", map[string]any{"text": "x"})
	ms = r.body["error"].(map[string]any)["messages"].([]any)
	if r.status != 409 || len(ms) != 5 || ms[0].(map[string]any)["text"] != "m20" {
		t.Fatalf("second 409: %d %d", r.status, len(ms))
	}
	cc.send(gid, map[string]any{"text": "x"})
}

func TestSSEFutureCursorResync(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	alice.group("g")
	hb, closeH := openSSE(t, alice, "999999")
	defer closeH()
	readEvent(t, hb)
	if typ, _ := readEvent(t, hb); typ != "resync" {
		t.Fatalf("want resync, got %s", typ)
	}
}

func TestDisabledAgentAndPasswordChange(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	cc := e.agent(alice, "CC", gid)
	alice.ok("PATCH", "/agents/"+cc.id, map[string]any{"disabled": true})
	if r := cc.do("GET", "/me", nil); r.status != 403 || r.errCode() != "disabled" {
		t.Fatalf("disabled: %d %s", r.status, r.raw)
	}
	alice.ok("PATCH", "/agents/"+cc.id, map[string]any{"disabled": false})
	cc.ok("GET", "/me", nil)
	if r := alice.do("PATCH", "/me", map[string]any{"password": "newpass123"}); r.status != 403 {
		t.Fatalf("no current password: %d", r.status)
	}
	alice.ok("PATCH", "/me", map[string]any{"password": "newpass123", "currentPassword": "password1"})
	e.anon().ok("POST", "/auth/login", map[string]any{"name": "alice", "password": "newpass123"})
}

func TestClientIPTrustedProxies(t *testing.T) {
	s := &Server{cfg: Config{TrustedProxies: defaultProxies}}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.18.0.3:5555"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 100.101.1.2")
	if ip := s.clientIP(r); ip != "100.101.1.2" {
		t.Fatalf("docker proxy: %s", ip)
	}
	r.RemoteAddr = "100.101.1.2:5555"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if ip := s.clientIP(r); ip != "100.101.1.2" {
		t.Fatalf("untrusted peer: %s", ip)
	}
}

func TestRunTokenCommanderFromInbox(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	gid := alice.group("g", bob)
	cc := e.agent(alice, "CC", gid)
	cc.ok("GET", "/groups/"+gid+"/messages", nil)
	bob.send(gid, map[string]any{"text": "@CC hi"})
	if rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": gid}); rt["commander"] != false {
		t.Fatalf("non-commander mention: %v", rt)
	}
	alice.send(gid, map[string]any{"text": "@CC do it"})
	if rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": gid}); rt["commander"] != true || rt["tier"] != "project" {
		t.Fatalf("owner mention: %v", rt)
	}
	cc.ok("GET", "/groups/"+gid+"/messages", nil)
	if rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": gid}); rt["commander"] != false {
		t.Fatalf("after read: %v", rt)
	}
}

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func (c *client) doH(method, path string, body any, hdr map[string]string) map[string]any {
	c.e.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, c.e.ts.URL+"/api/v1"+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	r, err := c.hc.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 {
		c.e.t.Fatalf("%s %s -> %d %s", method, path, r.StatusCode, raw)
	}
	var out map[string]any
	json.Unmarshal(raw, &out)
	return out
}

func TestMessageVia(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	cc := e.agent(alice, "CC", gid)

	m := alice.send(gid, map[string]any{"text": "hi"})["message"].(map[string]any)
	if m["via"] != nil {
		t.Fatalf("human via: %v", m["via"])
	}
	// Direct post with the long-lived token: headers are validated.
	m = cc.doH("POST", "/groups/"+gid+"/messages", map[string]any{"text": "direct", "ignoreNew": true},
		map[string]string{"X-Pichat-Session": "00000000-0000-4000-8000-000000000001", "X-Pichat-Host": "box<1>"})["message"].(map[string]any)
	via := m["via"].(map[string]any)
	if via["kind"] != "direct" || via["session"] != "00000000-0000-4000-8000-000000000001" || via["host"] != "box1" {
		t.Fatalf("direct via: %v", via)
	}
	m = cc.doH("POST", "/groups/"+gid+"/messages", map[string]any{"text": "bad", "ignoreNew": true},
		map[string]string{"X-Pichat-Session": "has space"})["message"].(map[string]any)
	if v := m["via"].(map[string]any); v["kind"] != "direct" || v["session"] != nil || v["host"] != nil {
		t.Fatalf("invalid headers via: %v", v)
	}
	// Run token: session/host come from the token, headers are ignored.
	rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": gid, "session": "main-1", "host": "devbox"})
	run := &client{e: e, hc: http.DefaultClient, token: rt["token"].(string), id: cc.id}
	m = run.doH("POST", "/groups/"+gid+"/messages", map[string]any{"text": "from run", "ignoreNew": true},
		map[string]string{"X-Pichat-Session": "other"})["message"].(map[string]any)
	via = m["via"].(map[string]any)
	if via["kind"] != "run" || via["session"] != "main-1" || via["host"] != "devbox" || via["tier"] != "restricted" {
		t.Fatalf("run via: %v", via)
	}
	// Stored and returned on reads too.
	msgs := alice.ok("GET", "/groups/"+gid+"/messages?after=0", nil)["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["via"].(map[string]any)["session"] != "main-1" || msgs[0].(map[string]any)["via"] != nil {
		t.Fatalf("read via: %v", msgs)
	}
}

func TestAgentRuntime(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	bob := e.human("bob")
	gid := alice.group("g", bob)
	cc := e.agent(alice, "CC", gid)

	// Offline regardless of stored state.
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "running", "session": "s1", "host": "h1", "name": "cc"})
	ag := alice.ok("GET", "/agents", nil)["agents"].([]any)[0].(map[string]any)
	rt := ag["runtime"].(map[string]any)
	if rt["state"] != "offline" || rt["session"] != "s1" || rt["host"] != "h1" || rt["name"] != "cc" {
		t.Fatalf("runtime: %v", rt)
	}
	if r := cc.do("PUT", "/me/runtime", map[string]any{"state": "weird"}); r.status != 400 {
		t.Fatalf("bad state: %d", r.status)
	}
	// Bob (group member) gets agent events once the listener is connected.
	hb, closeB := openSSE(t, bob, "")
	defer closeB()
	readEvent(t, hb)
	ab, closeA := openSSE(t, cc, "")
	defer closeA()
	readEvent(t, ab)
	typ, data := readEvent(t, hb)
	if typ != "agent" || !strings.Contains(data, `"state":"running"`) {
		t.Fatalf("connect event: %s %s", typ, data)
	}
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "attached", "session": "s2", "host": "h1", "name": "cc"})
	typ, data = readEvent(t, hb)
	if typ != "agent" || !strings.Contains(data, `"state":"attached"`) || !strings.Contains(data, `"session":"s2"`) {
		t.Fatalf("attach event: %s %s", typ, data)
	}
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "session": "s2"})
	readEvent(t, hb)
	g := bob.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)
	for _, x := range g["members"].([]any) {
		m := x.(map[string]any)
		if m["id"] == cc.id && (m["state"] != "idle" || m["mainSession"] != "s2") {
			t.Fatalf("member: %v", m)
		}
		if m["kind"] == "human" && m["state"] != nil {
			t.Fatalf("human state: %v", m)
		}
	}
	ag = alice.ok("GET", "/agents", nil)["agents"].([]any)[0].(map[string]any)
	ss := ag["sessions"].([]any)
	if len(ss) != 2 || ss[0].(map[string]any)["session"] != "s2" || ss[1].(map[string]any)["session"] != "s1" {
		t.Fatalf("sessions: %v", ss)
	}
	// reset-session reaches the listener; only the owner may call it.
	if r := bob.do("POST", "/agents/"+cc.id+"/reset-session", nil); r.status != 404 {
		t.Fatalf("non-owner reset: %d", r.status)
	}
	alice.ok("POST", "/agents/"+cc.id+"/reset-session", nil)
	if typ, _ := readEvent(t, ab); typ != "reset_session" {
		t.Fatalf("reset: %s", typ)
	}
	// Run tokens can't set runtime.
	rtok := cc.ok("POST", "/run-tokens", map[string]any{"groupId": gid})["token"].(string)
	run := &client{e: e, hc: http.DefaultClient, token: rtok}
	if r := run.do("PUT", "/me/runtime", map[string]any{"state": "idle"}); r.status != 403 {
		t.Fatalf("run token runtime: %d", r.status)
	}
	// Disconnect -> offline event.
	closeA()
	typ, data = readEvent(t, hb)
	if typ != "agent" || !strings.Contains(data, `"state":"offline"`) {
		t.Fatalf("offline event: %s %s", typ, data)
	}
}

func TestReplyToHumanCountsAsMention(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	bob := e.human("bob")
	gid := alice.group("g", bob)
	cc := e.agent(alice, "CC", gid)
	seq := alice.send(gid, map[string]any{"text": "question"})["message"].(map[string]any)["seq"]
	bob.ok("POST", "/groups/"+gid+"/read", nil)
	cc.ok("GET", "/groups/"+gid+"/messages", nil)
	cc.send(gid, map[string]any{"text": "answer", "replyTo": seq})
	bob.send(gid, map[string]any{"text": "me too", "replyTo": seq})
	alice.send(gid, map[string]any{"text": "self", "replyTo": seq})

	var n int
	e.s.db.R.QueryRow(`SELECT COUNT(*) FROM mentions WHERE target_id=? AND reason='reply'`, alice.id).Scan(&n)
	if n != 2 {
		t.Fatalf("reply mentions for alice: %d (self-reply must not count)", n)
	}
	// alice already read up to her own last message, so make the replies unread again.
	e.s.db.W.Exec(`UPDATE members SET last_read_seq=1 WHERE group_id=? AND principal_id=?`, gid, alice.id)
	in := alice.ok("GET", "/inbox", nil)["groups"].([]any)[0].(map[string]any)
	if in["repliesToMe"].(float64) != 2 {
		t.Fatalf("inbox: %v", in)
	}
	sum := alice.ok("GET", "/groups", nil)["groups"].([]any)[0].(map[string]any)
	if sum["mentionsMe"].(float64) != 2 {
		t.Fatalf("summary: %v", sum)
	}
}

func TestRunTokenTierCeiling(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	cc := e.agent(alice, "CC", gid)
	alice.ok("PATCH", "/agents/"+cc.id, map[string]any{"commanderTier": "full"})
	alice.send(gid, map[string]any{"text": "@CC work"})
	// Commander without ceiling -> full; with ceiling project -> project, and via says so.
	if rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": gid}); rt["tier"] != "full" || rt["commander"] != true {
		t.Fatalf("no ceiling: %v", rt)
	}
	rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": gid, "maxTier": "project", "session": "m1"})
	if rt["tier"] != "project" {
		t.Fatalf("ceiling: %v", rt)
	}
	run := &client{e: e, hc: http.DefaultClient, token: rt["token"].(string), id: cc.id}
	m := run.ok("POST", "/groups/"+gid+"/messages", map[string]any{"text": "done", "ignoreNew": true})["message"].(map[string]any)
	if v := m["via"].(map[string]any); v["tier"] != "project" || v["kind"] != "run" {
		t.Fatalf("via: %v", v)
	}
	// Ceiling restricted -> restricted token, group-scoped (can't open DMs).
	rt = cc.ok("POST", "/run-tokens", map[string]any{"groupId": gid, "maxTier": "restricted"})
	if rt["tier"] != "restricted" {
		t.Fatalf("restricted ceiling: %v", rt)
	}
	run = &client{e: e, hc: http.DefaultClient, token: rt["token"].(string), id: cc.id}
	if r := run.do("POST", "/chats", map[string]any{"with": alice.id}); r.status != 403 {
		t.Fatalf("restricted ceiling /chats: %d", r.status)
	}
	// A ceiling never raises: non-commander stays restricted.
	alice.ok("POST", "/groups/"+gid+"/read", nil)
	cc.ok("GET", "/groups/"+gid+"/messages", nil)
	if rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": gid, "maxTier": "full"}); rt["tier"] != "restricted" {
		t.Fatalf("raise: %v", rt)
	}
	if r := cc.do("POST", "/run-tokens", map[string]any{"groupId": gid, "maxTier": "root"}); r.status != 400 {
		t.Fatalf("bad maxTier: %d", r.status)
	}
}

func TestRecallDropsMentions(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	cc := e.agent(alice, "CC", gid)
	seq := alice.send(gid, map[string]any{"text": "question"})["message"].(map[string]any)["seq"]
	cc.ok("GET", "/groups/"+gid+"/messages", nil)
	r := cc.send(gid, map[string]any{"text": "answer", "replyTo": seq})["message"].(map[string]any)
	if n := alice.ok("GET", "/groups", nil)["groups"].([]any)[0].(map[string]any)["mentionsMe"].(float64); n != 1 {
		t.Fatalf("before recall: %v", n)
	}
	cc.ok("DELETE", "/groups/"+gid+"/messages/"+strconv.Itoa(int(r["seq"].(float64))), nil)
	if n := alice.ok("GET", "/groups", nil)["groups"].([]any)[0].(map[string]any)["mentionsMe"].(float64); n != 0 {
		t.Fatalf("after recall: %v", n)
	}
	if in := alice.ok("GET", "/inbox", nil)["groups"].([]any); len(in) > 0 && in[0].(map[string]any)["repliesToMe"].(float64) != 0 {
		t.Fatalf("inbox after recall: %v", in)
	}
}

func TestAgentEventAudience(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	carol := e.human("carol")
	gid := alice.group("g")
	cg := carol.group("other")
	cc := e.agent(alice, "CC", gid)
	hc, closeC := openSSE(t, carol, "")
	defer closeC()
	readEvent(t, hc)
	ha, closeAl := openSSE(t, alice, "")
	defer closeAl()
	readEvent(t, ha)
	ab, closeA := openSSE(t, cc, "")
	defer closeA()
	readEvent(t, ab)
	readEvent(t, ha) // connect event for the owner
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "running"})
	if typ, _ := readEvent(t, ha); typ != "agent" {
		t.Fatalf("owner: %s", typ)
	}
	// Sentinel: carol's next event must be her own group's message, not an 'agent' event.
	carol.send(cg, map[string]any{"text": "sentinel"})
	for {
		typ, data := readEvent(t, hc)
		if typ == "agent" {
			t.Fatalf("carol got agent event: %s", data)
		}
		if strings.Contains(data, "sentinel") {
			break
		}
	}
	// The agent's own stream gets no 'agent' events either; its next event is the reset.
	alice.ok("POST", "/agents/"+cc.id+"/reset-session", nil)
	if typ, _ := readEvent(t, ab); typ != "reset_session" {
		t.Fatalf("agent stream: %s", typ)
	}
}

func TestRuntimeNotice(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	bob := e.human("bob")
	gid := alice.group("g", bob)
	cc := e.agent(alice, "CC", gid)
	hb, closeB := openSSE(t, bob, "")
	defer closeB()
	readEvent(t, hb)
	ab, closeA := openSSE(t, cc, "")
	defer closeA()
	readEvent(t, ab)
	if typ, _ := readEvent(t, hb); typ != "agent" {
		t.Fatalf("connect event: %s", typ)
	}

	long := strings.Repeat("长", 250)
	ha, closeO := openSSE(t, alice, "")
	defer closeO()
	readEvent(t, ha)
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "attached", "session": "s1", "notice": "主会话正在被\n‮终端进程 42 使用（claude --settings /home/x/secret.json） " + long})
	// The owner gets the full notice; other members only a generic text (no pid or command line).
	typ, data := readEvent(t, ha)
	if typ != "agent" || !strings.Contains(data, `"notice":"主会话正在被 终端进程 42 使用（claude --settings /home/x/secret.json） 长`) || !strings.Contains(data, `"noticeAt":`) {
		t.Fatalf("owner notice event: %s %s", typ, data)
	}
	typ, data = readEvent(t, hb)
	if typ != "agent" || !strings.Contains(data, `"notice":"主会话正在被终端使用，群消息已排队"`) || strings.Contains(data, "secret") {
		t.Fatalf("member notice event: %s %s", typ, data)
	}
	rt := alice.ok("GET", "/agents", nil)["agents"].([]any)[0].(map[string]any)["runtime"].(map[string]any)
	n, _ := rt["notice"].(string)
	if len([]rune(n)) != 200 || strings.ContainsAny(n, "\n‮") || rt["noticeAt"] == nil {
		t.Fatalf("stored notice: %q %v", n, rt["noticeAt"])
	}
	at := rt["noticeAt"]

	// A later report without notice keeps it; group members see the generic text, the owner all of it.
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle"})
	readEvent(t, hb)
	readEvent(t, ha)
	for _, x := range bob.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)["members"].([]any) {
		m := x.(map[string]any)
		if m["id"] == cc.id && m["notice"] != "主会话正在被终端使用，群消息已排队" {
			t.Fatalf("member notice: %v", m)
		}
		if m["kind"] == "human" && m["notice"] != nil {
			t.Fatalf("human notice: %v", m)
		}
	}
	for _, x := range alice.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)["members"].([]any) {
		if m := x.(map[string]any); m["id"] == cc.id && m["notice"] != n {
			t.Fatalf("owner member notice: %v", m)
		}
	}
	rt = alice.ok("GET", "/agents", nil)["agents"].([]any)[0].(map[string]any)["runtime"].(map[string]any)
	if rt["notice"] != n || rt["noticeAt"] != at {
		t.Fatalf("kept notice: %v", rt)
	}

	// Replaced by a new notice.
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "notice": "检测到会话分叉"})
	if _, data := readEvent(t, hb); !strings.Contains(data, `"notice":"检测到会话分叉"`) {
		t.Fatalf("replaced: %s", data)
	}

	// Only the owner can dismiss.
	if r := bob.do("POST", "/agents/"+cc.id+"/dismiss-notice", nil); r.status != 404 {
		t.Fatalf("non-owner dismiss: %d", r.status)
	}
	if r := cc.do("POST", "/agents/"+cc.id+"/dismiss-notice", nil); r.status != 403 {
		t.Fatalf("agent dismiss: %d", r.status)
	}
	res := alice.ok("POST", "/agents/"+cc.id+"/dismiss-notice", nil)["runtime"].(map[string]any)
	if res["notice"] != nil || res["noticeAt"] != nil {
		t.Fatalf("dismissed: %v", res)
	}
	if _, data := readEvent(t, hb); !strings.Contains(data, `"notice":null`) {
		t.Fatalf("dismiss event: %s", data)
	}
	// An empty notice from the listener also clears it.
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "notice": "x"})
	readEvent(t, hb)
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "notice": " \n "})
	if _, data := readEvent(t, hb); !strings.Contains(data, `"notice":null`) {
		t.Fatalf("cleared: %s", data)
	}
}

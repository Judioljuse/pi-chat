package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func contactIDs(t *testing.T, c *client) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, x := range c.ok("GET", "/contacts", nil)["contacts"].([]any) {
		m := x.(map[string]any)
		out[m["id"].(string)] = m
	}
	return out
}

func TestContactsMatchOpenChat(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	bob := e.human("bob")
	carol := e.human("carol") // shares no group with alice
	gid := alice.group("g", bob)
	cc := e.agent(alice, "CC", "")   // alice's agent, in no group
	cc2 := e.agent(alice, "CC2", "") // same owner as cc
	bb := e.agent(bob, "BB", gid)    // bob's agent, in alice's group
	cx := e.agent(carol, "CX", "")
	dis := e.agent(alice, "Off", "")
	alice.ok("PATCH", "/agents/"+dis.id, map[string]any{"disabled": true})

	got := contactIDs(t, alice)
	for _, want := range []string{bob.id, cc.id, cc2.id, bb.id} {
		if got[want] == nil {
			t.Fatalf("alice contacts missing %s: %v", want, got)
		}
	}
	for _, no := range []string{alice.id, carol.id, cx.id, dis.id} {
		if got[no] != nil {
			t.Fatalf("alice contacts has %s", no)
		}
	}
	if m := got[bb.id]; m["kind"] != "agent" || m["ownerName"] != "bob" || m["state"] != "offline" || m["platform"] != "claude_code" || m["online"] != false {
		t.Fatalf("agent contact: %v", m)
	}
	if m := got[bob.id]; m["kind"] != "human" || m["state"] != nil {
		t.Fatalf("human contact: %v", m)
	}
	// Humans come first.
	list := alice.ok("GET", "/contacts", nil)["contacts"].([]any)
	if list[0].(map[string]any)["kind"] != "human" {
		t.Fatalf("order: %v", list)
	}
	// An agent: its owner, sibling agents and group mates.
	got = contactIDs(t, cc)
	if got[alice.id] == nil || got[cc2.id] == nil || got[bob.id] != nil || got[bb.id] != nil {
		t.Fatalf("cc contacts: %v", got)
	}
	got = contactIDs(t, bb)
	if got[bob.id] == nil || got[alice.id] == nil || got[cc.id] != nil {
		t.Fatalf("bb contacts: %v", got)
	}
	// Restricted run tokens can't list contacts.
	rtok := bb.ok("POST", "/run-tokens", map[string]any{"groupId": gid})["token"].(string)
	if r := (&client{e: e, hc: http.DefaultClient, token: rtok}).do("GET", "/contacts", nil); r.status != 403 {
		t.Fatalf("run token contacts: %d", r.status)
	}

	// openChat agrees with the list for every principal.
	all := []*client{alice, bob, carol, cc, cc2, bb, cx, dis}
	for _, from := range []*client{alice, bob, carol, cc, bb} {
		list := contactIDs(t, from)
		for _, to := range all {
			if to == from {
				continue
			}
			r := from.do("POST", "/chats", map[string]any{"with": to.id})
			if (r.status == 200) != (list[to.id] != nil) {
				t.Fatalf("%s -> %s: chats %d, in contacts %v", from.id, to.id, r.status, list[to.id] != nil)
			}
		}
	}
}

func TestPrincipalProfile(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	bob := e.human("bob")
	carol := e.human("carol")
	gid := alice.group("Alpha", bob)
	alice.group("Beta", bob)
	cc := e.agent(alice, "CC", gid)
	cc.ok("PATCH", "/me", map[string]any{"intro": "hello"})
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "session": "s1", "host": "devbox", "notice": "主会话正在被 claude --resume s1 (pid 42) 使用",
		"dirs": map[string]any{"main": "/home/a/proj", "default": "/home/a", "binds": map[string]any{gid: "/home/a/proj"}}})

	// Owner: full fields.
	p := alice.ok("GET", "/principals/"+cc.id, nil)["principal"].(map[string]any)
	if p["displayName"] != "CC" || p["intro"] != "hello" || p["ownerName"] != "alice" || p["host"] != "devbox" || p["mainSession"] != "s1" || p["isOwner"] != true {
		t.Fatalf("owner view: %v", p)
	}
	d := p["dirs"].(map[string]any)
	if d["main"] != "/home/a/proj" || d["default"] != "/home/a" || d["binds"].(map[string]any)[gid] != "/home/a/proj" {
		t.Fatalf("owner dirs: %v", d)
	}
	if !strings.Contains(p["notice"].(string), "pid 42") {
		t.Fatalf("owner notice: %v", p["notice"])
	}
	if cg := p["commonGroups"].([]any); len(cg) != 1 || cg[0].(map[string]any)["name"] != "Alpha" {
		t.Fatalf("common groups: %v", cg)
	}

	// Group mate: public fields only.
	r := bob.do("GET", "/principals/"+cc.id, nil)
	if r.status != 200 {
		t.Fatalf("bob: %d %s", r.status, r.raw)
	}
	p = r.body["principal"].(map[string]any)
	for _, k := range []string{"host", "mainSession", "dirs"} {
		if _, ok := p[k]; ok {
			t.Fatalf("bob sees %s: %v", k, p)
		}
	}
	if strings.Contains(r.raw, "devbox") || strings.Contains(r.raw, "/home/a") || strings.Contains(r.raw, "pid 42") || p["isOwner"] != false {
		t.Fatalf("bob leak: %s", r.raw)
	}
	if p["state"] != "offline" || p["intro"] != "hello" {
		t.Fatalf("bob view: %v", p)
	}

	// Human profile with two groups in common.
	p = alice.ok("GET", "/principals/"+bob.id, nil)["principal"].(map[string]any)
	if p["kind"] != "human" || p["name"] != "bob" || len(p["commonGroups"].([]any)) != 2 {
		t.Fatalf("human view: %v", p)
	}
	// Outsider: 404 for both the agent and the human.
	for _, id := range []string{cc.id, bob.id, "a_nope"} {
		if r := carol.do("GET", "/principals/"+id, nil); r.status != 404 {
			t.Fatalf("carol -> %s: %d", id, r.status)
		}
	}
	// A DM is not a group in common.
	cc.ok("POST", "/chats", map[string]any{"with": alice.id})
	p = cc.ok("GET", "/principals/"+alice.id, nil)["principal"].(map[string]any)
	if len(p["commonGroups"].([]any)) != 1 {
		t.Fatalf("dm common: %v", p)
	}
	// The owner sees an agent that is in no group with them.
	solo := e.agent(alice, "Solo", "")
	p = alice.ok("GET", "/principals/"+solo.id, nil)["principal"].(map[string]any)
	if p["dirs"] != nil || len(p["commonGroups"].([]any)) != 0 {
		t.Fatalf("solo: %v", p)
	}
	// The agent can see itself; agents in the group see each other's public card.
	bb := e.agent(bob, "BB", gid)
	r = bb.do("GET", "/principals/"+cc.id, nil)
	if r.status != 200 || strings.Contains(r.raw, "devbox") {
		t.Fatalf("bb -> cc: %d %s", r.status, r.raw)
	}
}

func TestRuntimeDirs(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	bob := e.human("bob")
	gid := alice.group("g", bob)
	cc := e.agent(alice, "CC", gid)

	runtime := func() map[string]any {
		return alice.ok("GET", "/agents", nil)["agents"].([]any)[0].(map[string]any)["runtime"].(map[string]any)
	}
	// Older listener: no dirs.
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle"})
	if rt := runtime(); rt["dirs"] != nil {
		t.Fatalf("no dirs: %v", rt["dirs"])
	}
	binds := map[string]any{gid: "  /srv/proj  ", "g_bad id": "/x", "g_rel": "rel/path", "g_num": 5, "g_ctl": "/a\nb", "g_bidi": "/a\u202eb",
		"g_long": "/" + strings.Repeat("x", 300), "g_win": `C:\work\proj`}
	for i := 0; i < 60; i++ {
		binds["g_"+strings.Repeat("z", 2)+string(rune('a'+i%26))+string(rune('a'+i/26))] = "/d"
	}
	r := cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "dirs": map[string]any{"main": "/home/cc/main", "default": 7, "binds": binds}})
	if d := r["runtime"].(map[string]any)["dirs"].(map[string]any); d["main"] != "/home/cc/main" {
		t.Fatalf("put response dirs: %v", d)
	}
	d := runtime()["dirs"].(map[string]any)
	b := d["binds"].(map[string]any)
	if d["main"] != "/home/cc/main" || d["default"] != nil || b[gid] != "/srv/proj" || b[`g_win`] != `C:\work\proj` {
		t.Fatalf("dirs: %v", d)
	}
	for _, k := range []string{"g_bad id", "g_rel", "g_num", "g_ctl", "g_bidi", "g_long"} {
		if _, ok := b[k]; ok {
			t.Fatalf("kept %s: %v", k, b)
		}
	}
	if len(b) != maxDirBind {
		t.Fatalf("binds: %d", len(b))
	}
	// Omitting dirs keeps them; a new report replaces them.
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "running"})
	if runtime()["dirs"].(map[string]any)["main"] != "/home/cc/main" {
		t.Fatal("dirs dropped on report without dirs")
	}
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "dirs": map[string]any{"default": "/opt"}})
	d = runtime()["dirs"].(map[string]any)
	if d["main"] != nil || d["default"] != "/opt" || len(d["binds"].(map[string]any)) != 0 {
		t.Fatalf("replaced: %v", d)
	}
	// Not a JSON object: ignored, not an error.
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "dirs": "nope"})
	// Group members never see dirs.
	g := bob.ok("GET", "/groups/"+gid, nil)["group"]
	raw, _ := json.Marshal(g)
	if strings.Contains(string(raw), "/opt") {
		t.Fatalf("group leaks dirs: %s", raw)
	}
}

// Host, machine name and work dirs reach only the owner over SSE; other members get state and session.
func TestAgentEventFieldGating(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	bob := e.human("bob")
	gid := alice.group("g", bob)
	cc := e.agent(alice, "CC", gid)
	hb, closeB := openSSE(t, bob, "")
	defer closeB()
	readEvent(t, hb)
	ha, closeA := openSSE(t, alice, "")
	defer closeA()
	readEvent(t, ha)
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "session": "s1", "host": "box-secret", "name": "mac-secret",
		"dirs": map[string]any{"main": "/work/secret", "binds": map[string]any{}}})
	typ, data := readEvent(t, ha)
	if typ != "agent" || !strings.Contains(data, `"host":"box-secret"`) || !strings.Contains(data, `"/work/secret"`) {
		t.Fatalf("owner event: %s %s", typ, data)
	}
	typ, data = readEvent(t, hb)
	if typ != "agent" || !strings.Contains(data, `"session":"s1"`) || strings.Contains(data, "secret") || strings.Contains(data, `"host"`) || strings.Contains(data, `"dirs"`) {
		t.Fatalf("member event: %s %s", typ, data)
	}
}

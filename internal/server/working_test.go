package server

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWorkingState(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	other := alice.group("other")
	cc := e.agent(alice, "CC", gid)
	hb, closeH := openSSE(t, alice, "")
	defer closeH()
	readEvent(t, hb)

	if r := cc.do("POST", "/runs/start", map[string]any{"groupId": other}); r.status != 404 {
		t.Fatalf("non-member start: %d", r.status)
	}
	if r := alice.do("POST", "/runs/start", map[string]any{"groupId": gid}); r.status != 403 {
		t.Fatalf("human start: %d", r.status)
	}
	rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": gid})
	run := &client{e: e, hc: cc.hc, token: rt["token"].(string), id: cc.id}
	started := run.ok("POST", "/runs/start", map[string]any{"groupId": gid})["startedAt"].(float64)

	typ, data := readEvent(t, hb)
	var ev map[string]any
	json.Unmarshal([]byte(data), &ev)
	if typ != "working" || ev["groupId"] != gid || ev["agentId"] != cc.id || ev["startedAt"].(float64) != started {
		t.Fatalf("working start: %s %s", typ, data)
	}
	g := alice.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)
	w := g["working"].([]any)
	if len(w) != 1 || w[0].(map[string]any)["agentId"] != cc.id {
		t.Fatalf("group working: %v", g["working"])
	}
	for _, s := range alice.ok("GET", "/groups", nil)["groups"].([]any) {
		s := s.(map[string]any)
		ids := strs(s["working"])
		if (s["id"] == gid) != (len(ids) == 1 && ids[0] == cc.id) {
			t.Fatalf("summary working: %v", s)
		}
	}

	cc.ok("POST", "/runs", map[string]any{"groupId": gid, "ok": true, "detail": "x"})
	typ, data = readEvent(t, hb)
	json.Unmarshal([]byte(data), &ev)
	if typ != "working" || ev["startedAt"].(float64) != 0 {
		t.Fatalf("working end: %s %s", typ, data)
	}
	if w := alice.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)["working"].([]any); len(w) != 0 {
		t.Fatalf("still working: %v", w)
	}
}

func TestWorkingExpiry(t *testing.T) {
	w := newWorking()
	defer w.stop()
	fired := make(chan struct{}, 1)
	w.start("g", "a", 1, func() { fired <- struct{}{} })
	e := w.m["g"]["a"]
	// Restarting replaces the entry; the old timer must not clear the new one.
	w.start("g", "a", 2, func() { fired <- struct{}{} })
	if w.finishEntry("g", "a", e) {
		t.Fatal("stale entry cleared current run")
	}
	w.m["g"]["a"].timer.Reset(time.Millisecond)
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("expiry did not fire")
	}
	if len(w.list("g")) != 0 || w.finish("g", "a") {
		t.Fatal("expired entry still present")
	}
}

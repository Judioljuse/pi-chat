package server

import (
	"crypto/ecdh"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPushFlow(t *testing.T) {
	e := newEnv(t)
	e.s.PushDelay = 50 * time.Millisecond
	var mu sync.Mutex
	hits := map[string]int{}
	push := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") || r.Header.Get("Content-Encoding") != "aes128gcm" {
			w.WriteHeader(400)
			return
		}
		if r.URL.Path == "/gone" {
			w.WriteHeader(410)
			return
		}
		w.WriteHeader(201)
	}))
	defer push.Close()
	e.s.pushClient = push.Client()
	count := func(p string) int { mu.Lock(); defer mu.Unlock(); return hits[p] }

	alice, bob := e.human("alice"), e.human("bob")
	gid := alice.group("g", bob)
	a1 := e.agent(alice, "A1", gid)
	k := alice.ok("GET", "/push/key", nil)
	if len(k["publicKey"].(string)) != 87 {
		t.Fatalf("key: %v", k)
	}
	if k2 := bob.ok("GET", "/push/key", nil); k2["publicKey"] != k["publicKey"] {
		t.Fatal("key not stable")
	}
	ua, _ := ecdh.P256().GenerateKey(nil)
	keys := map[string]any{"p256dh": base64.RawURLEncoding.EncodeToString(ua.PublicKey().Bytes()), "auth": base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef"))}
	if r := alice.do("POST", "/push/subscriptions", map[string]any{"endpoint": "http://x.test/a", "keys": keys}); r.status != 400 {
		t.Fatalf("http endpoint: %d", r.status)
	}
	if r := alice.do("POST", "/push/subscriptions", map[string]any{"endpoint": push.URL + "/a", "keys": map[string]any{"p256dh": "AAAA", "auth": "x"}}); r.status != 400 {
		t.Fatalf("bad keys: %d", r.status)
	}
	alice.ok("POST", "/push/subscriptions", map[string]any{"endpoint": push.URL + "/alice", "keys": keys})
	alice.ok("POST", "/push/subscriptions", map[string]any{"endpoint": push.URL + "/gone", "keys": keys})
	if d := alice.ok("GET", "/push/subscriptions", nil)["devices"].([]any); len(d) != 2 {
		t.Fatalf("devices: %v", d)
	}

	// An agent @s alice: pushed after the delay because she hasn't read it; the dead device is pruned.
	a1.send(gid, map[string]any{"text": "@alice 看一下", "ignoreNew": true})
	time.Sleep(300 * time.Millisecond)
	if count("/alice") != 1 || count("/gone") != 1 {
		t.Fatalf("hits: %v", hits)
	}
	if d := alice.ok("GET", "/push/subscriptions", nil)["devices"].([]any); len(d) != 1 {
		t.Fatalf("gone not pruned: %v", d)
	}
	// Read in time: no push. A message not aimed at alice: no push. Her own message: no push.
	a1.send(gid, map[string]any{"text": "@alice 第二条", "ignoreNew": true})
	alice.ok("POST", "/groups/"+gid+"/read", map[string]any{})
	a1.send(gid, map[string]any{"text": "大家好", "ignoreNew": true})
	alice.send(gid, map[string]any{"text": "@alice 自己"})
	time.Sleep(300 * time.Millisecond)
	if count("/alice") != 1 {
		t.Fatalf("unexpected pushes: %v", hits)
	}
	// The same endpoint moving to bob (shared browser) leaves alice with none.
	bob.ok("POST", "/push/subscriptions", map[string]any{"endpoint": push.URL + "/alice", "keys": keys})
	if d := alice.ok("GET", "/push/subscriptions", nil)["devices"].([]any); len(d) != 0 {
		t.Fatalf("endpoint kept by alice: %v", d)
	}
	r := bob.ok("POST", "/push/test", nil)["results"].([]any)
	if len(r) != 1 || r[0].(map[string]any)["ok"] != true {
		t.Fatalf("test push: %v", r)
	}
	bob.ok("DELETE", "/push/subscriptions", map[string]any{"endpoint": push.URL + "/alice"})
	if d := bob.ok("GET", "/push/subscriptions", nil)["devices"].([]any); len(d) != 0 {
		t.Fatalf("not deleted: %v", d)
	}
	if r := a1.do("GET", "/push/key", nil); r.status != 403 {
		t.Fatalf("agent push key: %d", r.status)
	}
}

func TestUsage(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	gid := alice.group("g", bob)
	a1 := e.agent(alice, "A1", gid)
	b1 := e.agent(bob, "B1", gid)
	a1.ok("POST", "/runs", map[string]any{"groupId": gid, "ok": true, "detail": "x", "inputTokens": 1000, "outputTokens": 200, "cacheTokens": 5000, "costUsd": 0.12, "durationMs": 30000})
	a1.ok("POST", "/runs", map[string]any{"groupId": gid, "ok": false, "detail": "y", "inputTokens": -5, "durationMs": 1000})
	b1.ok("POST", "/runs", map[string]any{"groupId": gid, "ok": true, "inputTokens": 9})
	u := alice.ok("GET", "/usage?days=7", nil)
	tot := u["total"].(map[string]any)
	if tot["runs"].(float64) != 2 || tot["failed"].(float64) != 1 || tot["inputTokens"].(float64) != 1000 || tot["durationMs"].(float64) != 31000 {
		t.Fatalf("total: %v", tot)
	}
	if ag := u["agents"].([]any); len(ag) != 1 || ag[0].(map[string]any)["name"] != "A1" {
		t.Fatalf("agents: %v", ag)
	}
	if d := u["byDay"].([]any); len(d) != 1 || len(d[0].(map[string]any)["day"].(string)) != 10 {
		t.Fatalf("byDay: %v", d)
	}
	// The admin (alice is the first user) sees everyone's with scope=all; bob doesn't.
	if all := alice.ok("GET", "/usage?scope=all", nil)["total"].(map[string]any); all["runs"].(float64) != 3 {
		t.Fatalf("all: %v", all)
	}
	if b := bob.ok("GET", "/usage?scope=all", nil); b["scope"] != "mine" || b["total"].(map[string]any)["runs"].(float64) != 1 {
		t.Fatalf("bob: %v", b)
	}
}

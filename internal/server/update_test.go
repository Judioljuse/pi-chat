package server

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pichat/internal/update"
)

// fakeRelease serves /repos/o/r/releases/{latest,tags/v0.3.0} and the asset.
func fakeRelease(t *testing.T, asset []byte) *httptest.Server {
	sum := sha256.Sum256(asset)
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/releases/latest", "/repos/o/r/releases/tags/v0.3.0":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.3.0", "name": "v0.3.0", "body": "notes", "html_url": "https://x",
				"assets": []any{map[string]any{"name": "pichat_0.3.0_linux_amd64", "size": len(asset), "browser_download_url": ts.URL + "/dl/a",
					"digest": "sha256:" + hex.EncodeToString(sum[:])}}})
		case "/dl/a":
			w.Write(asset)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestAdminUpdate(t *testing.T) {
	e := newEnv(t)
	gh := fakeRelease(t, []byte("#!/bin/sh\necho 0.3.0\n"))
	restarted := make(chan [2]string, 1)
	e.s.Restart = func(p, v string) { restarted <- [2]string{p, v} }
	e.s.upd = update.New(update.Config{Enabled: true, Repo: "o/r", API: gh.URL, Data: e.s.cfg.Data, GOOS: "linux", GOARCH: "amd64",
		Self: update.Self{Version: "0.2.0", BuiltinVersion: "0.2.0"}, Log: e.s.log})
	admin := e.human("admin")
	bob := e.human("bob")

	if r := bob.do("GET", "/admin/update", nil); r.status != 403 {
		t.Fatalf("non-admin get: %d", r.status)
	}
	if r := bob.do("POST", "/admin/update", map[string]any{"tag": "v0.3.0", "password": "password1"}); r.status != 403 {
		t.Fatalf("non-admin post: %d", r.status)
	}
	st := admin.ok("GET", "/admin/update?refresh=1", nil)
	if st["available"] != true || st["current"] != "0.2.0" || st["running"] != "builtin" || st["latest"].(map[string]any)["tag"] != "v0.3.0" {
		t.Fatalf("status: %v", st)
	}
	if r := admin.do("POST", "/admin/update", map[string]any{"tag": "v0.3.0", "password": "wrong-password"}); r.status != 403 {
		t.Fatalf("wrong password: %d %s", r.status, r.raw)
	}
	// An agent token (even its owner's) can't install.
	cc := e.agent(admin, "CC", "")
	if r := cc.do("POST", "/admin/update", map[string]any{"tag": "v0.3.0", "password": "password1"}); r.status != 403 {
		t.Fatalf("agent: %d", r.status)
	}
	if r := admin.do("POST", "/admin/update", map[string]any{"tag": "v0.3.0", "password": "password1"}); r.status != 202 {
		t.Fatalf("install: %d %s", r.status, r.raw)
	}
	select {
	case got := <-restarted:
		if !strings.HasSuffix(got[0], "/bin/pichat-0.3.0") || got[1] != "0.3.0" {
			t.Fatalf("restart %v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no restart: %v", admin.ok("GET", "/admin/update", nil))
	}
	if st := admin.ok("GET", "/admin/update", nil)["state"].(map[string]any); st["phase"] != "restarting" {
		t.Fatalf("state %v", st)
	}
	// Rollback while running the builtin binary is refused.
	if r := admin.do("POST", "/admin/update/rollback", map[string]any{"password": "password1"}); r.status != 400 {
		t.Fatalf("rollback: %d %s", r.status, r.raw)
	}
}

func TestUpdateDisabledAndDescribe(t *testing.T) {
	e := newEnv(t)
	admin := e.human("admin")
	st := admin.ok("GET", "/admin/update", nil)
	if st["enabled"] != false || st["disabledReason"] == "" || st["latest"] != nil {
		t.Fatalf("disabled status %v", st)
	}
	if r := admin.do("POST", "/admin/update", map[string]any{"tag": "v9.0.0", "password": "password1"}); r.status != 409 {
		t.Fatalf("disabled install %d", r.status)
	}
	d := e.anon().ok("GET", "", nil)
	sum := sha256.Sum256([]byte("//"))
	if d["version"] != version || d["listenerSha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("describe %v", d)
	}
}

func TestListenerVersion(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	cc := e.agent(alice, "CC", "")
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+cc.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	typ, data := readEvent(t, bufio.NewReader(res.Body))
	sum := sha256.Sum256([]byte("//"))
	full := hex.EncodeToString(sum[:])
	if typ != "hello" || !strings.Contains(data, `"listener":{"sha256":"`+full+`","version":"`+version+`"}`) {
		t.Fatalf("hello %s", data)
	}
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "listener": map[string]any{"sha256": full[:12], "version": "0.2.0", "autoUpdate": true}})
	rt := alice.ok("GET", "/agents", nil)["agents"].([]any)[0].(map[string]any)["runtime"].(map[string]any)
	l := rt["listener"].(map[string]any)
	if l["latest"] != true || l["autoUpdate"] != true || l["version"] != "0.2.0" {
		t.Fatalf("listener %v", l)
	}
	// Omitted keeps it; an old script is not latest.
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle"})
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "listener": map[string]any{"sha256": "abcdefabcdef", "autoUpdate": false}})
	l = alice.ok("GET", "/agents", nil)["agents"].([]any)[0].(map[string]any)["runtime"].(map[string]any)["listener"].(map[string]any)
	if l["latest"] != false || l["version"] != "unknown" || l["autoUpdate"] != false {
		t.Fatalf("old listener %v", l)
	}
	if l["skip"] != nil {
		t.Fatalf("skip without report: %v", l["skip"])
	}
	// skip: only the known codes are kept.
	for in, want := range map[string]any{"git": "git", "readonly": "readonly", "/home/x 不可写": nil} {
		cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "listener": map[string]any{"sha256": "abcdefabcdef", "autoUpdate": true, "skip": in}})
		l = alice.ok("GET", "/agents", nil)["agents"].([]any)[0].(map[string]any)["runtime"].(map[string]any)["listener"].(map[string]any)
		if l["skip"] != want {
			t.Fatalf("skip %q -> %v, want %v", in, l["skip"], want)
		}
	}
}

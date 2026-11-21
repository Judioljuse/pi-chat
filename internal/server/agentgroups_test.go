package server

import (
	"strings"
	"testing"
)

func TestAgentGroupNames(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	bob := e.human("bob")
	cc := e.agent(alice, "CC", "")

	if n := alice.ok("GET", "/agent-groups", nil)["names"].(map[string]any); len(n) != 0 {
		t.Fatalf("empty: %v", n)
	}
	r := alice.ok("PUT", "/agent-groups", map[string]any{"key": "host:box-1", "name": "  我的 [Mac]  "})
	if n := r["names"].(map[string]any); n["host:box-1"] != "我的 Mac" {
		t.Fatalf("put: %v", n)
	}
	key := "proj:box-1|/home/a/my repo|main"
	alice.ok("PUT", "/agent-groups", map[string]any{"key": key, "name": "主项目"})
	alice.ok("PUT", "/agent-groups", map[string]any{"key": "host:box-1", "name": "办公室"})
	n := alice.ok("GET", "/agent-groups", nil)["names"].(map[string]any)
	if n["host:box-1"] != "办公室" || n[key] != "主项目" || len(n) != 2 {
		t.Fatalf("names: %v", n)
	}
	// Owner-only: bob sees his own (none) and cannot touch alice's.
	if n := bob.ok("GET", "/agent-groups", nil)["names"].(map[string]any); len(n) != 0 {
		t.Fatalf("bob sees: %v", n)
	}
	bob.ok("PUT", "/agent-groups", map[string]any{"key": "host:box-1", "name": ""})
	if n := alice.ok("GET", "/agent-groups", nil)["names"].(map[string]any); n["host:box-1"] != "办公室" {
		t.Fatalf("bob reset alice's: %v", n)
	}
	// Humans only.
	if r := cc.do("GET", "/agent-groups", nil); r.status != 403 && r.status != 401 {
		t.Fatalf("agent get: %d %s", r.status, r.raw)
	}
	if r := cc.do("PUT", "/agent-groups", map[string]any{"key": "host:x", "name": "y"}); r.status != 403 && r.status != 401 {
		t.Fatalf("agent put: %d %s", r.status, r.raw)
	}
	if r := e.anon().do("GET", "/agent-groups", nil); r.status != 401 {
		t.Fatalf("anon: %d", r.status)
	}
	// Validation.
	for _, body := range []map[string]any{
		{"key": "box-1", "name": "x"},
		{"key": "host:", "name": "x"},
		{"key": "host:a\nb", "name": "x"},
		{"key": "host:a\u202eb", "name": "x"},
		{"key": "host:" + strings.Repeat("x", maxGroupKeyLen), "name": "x"},
		{"key": "host:a", "name": strings.Repeat("名", maxGroupNameLen+1)},
		{"key": "host:a"},
	} {
		if r := alice.do("PUT", "/agent-groups", body); r.status != 400 {
			t.Fatalf("%v -> %d %s", body, r.status, r.raw)
		}
	}
	// Empty name resets; resetting an unknown key is fine.
	alice.ok("PUT", "/agent-groups", map[string]any{"key": "host:box-1", "name": "  "})
	alice.ok("PUT", "/agent-groups", map[string]any{"key": "host:never", "name": ""})
	if n := alice.ok("GET", "/agent-groups", nil)["names"].(map[string]any); len(n) != 1 || n[key] != "主项目" {
		t.Fatalf("after reset: %v", n)
	}
}

func TestRuntimeDirsRepo(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	bob := e.human("bob")
	gid := alice.group("g", bob)
	cc := e.agent(alice, "CC", gid)
	dirs := func() map[string]any {
		return alice.ok("GET", "/agents", nil)["agents"].([]any)[0].(map[string]any)["runtime"].(map[string]any)["dirs"].(map[string]any)
	}
	// Older listener: no repo info.
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "dirs": map[string]any{"main": "/w/a"}})
	if d := dirs(); d["repo"] != nil || len(d["repos"].(map[string]any)) != 0 {
		t.Fatalf("no repo: %v", d)
	}
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "dirs": map[string]any{
		"main": "/w/wt", "default": "/w/a", "binds": map[string]any{gid: "/w/b"},
		"repo": map[string]any{"root": "/w/a", "branch": "feat/x", "worktree": true, "path": "/w/wt"},
		"repos": map[string]any{
			"/w/wt":  map[string]any{"root": "/w/a", "branch": "feat/x", "worktree": true, "path": "/w/wt"},
			"/w/a":   map[string]any{"root": "/w/a", "branch": "main", "worktree": false},
			"/w/b":   map[string]any{"root": "rel", "branch": "main", "worktree": false},
			"/other": map[string]any{"root": "/other", "worktree": false},
		},
	}})
	d := dirs()
	repo := d["repo"].(map[string]any)
	if repo["root"] != "/w/a" || repo["branch"] != "feat/x" || repo["worktree"] != true || repo["path"] != "/w/wt" {
		t.Fatalf("repo: %v", repo)
	}
	repos := d["repos"].(map[string]any)
	if len(repos) != 2 || repos["/w/a"].(map[string]any)["branch"] != "main" || repos["/w/a"].(map[string]any)["path"] != nil {
		t.Fatalf("repos: %v", repos)
	}
	// Bad values: long branch dropped, non-bool worktree drops the repo, long root drops it.
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "dirs": map[string]any{
		"main": "/w/a", "repo": map[string]any{"root": "/w/a", "branch": strings.Repeat("b", maxBranchLen+1), "worktree": false},
		"repos": map[string]any{"/w/a": map[string]any{"root": "/w/a", "worktree": "yes"}},
	}})
	d = dirs()
	if r := d["repo"].(map[string]any); r["branch"] != nil || r["worktree"] != false {
		t.Fatalf("long branch: %v", r)
	}
	if len(d["repos"].(map[string]any)) != 0 {
		t.Fatalf("bad worktree kept: %v", d["repos"])
	}
	cc.ok("PUT", "/me/runtime", map[string]any{"state": "idle", "dirs": map[string]any{
		"main": "/w/a", "repo": map[string]any{"root": "/" + strings.Repeat("r", maxDirLen), "worktree": false}}})
	if d := dirs(); d["repo"] != nil {
		t.Fatalf("long root: %v", d["repo"])
	}
}

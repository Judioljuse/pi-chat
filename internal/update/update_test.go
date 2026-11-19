package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"0.2.0", "0.2.0", 0}, {"0.2.0", "0.3.0", -1}, {"0.10.0", "0.9.9", 1}, {"1.0.0", "0.99.99", 1},
		{"v0.3.0", "0.3.0", 0}, {"0.3.0-rc1", "0.3.0", -1}, {"0.3.0", "0.3.0-rc1", 1}, {"0.3.0-rc1", "0.2.9", 1},
		{"garbage", "0.0.1", -1}, {"0.3", "0.0.1", -1},
		{"0.3.0-rc.10", "0.3.0-rc.9", 1}, {"0.3.0-beta.2", "0.3.0-alpha.10", 1}, {"0.3.0-rc.1", "0.3.0-rc.1.1", -1},
		{"0.3.0-1", "0.3.0-alpha", -1}, {"0.3.0-rc.2", "0.3.0-rc.2", 0},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

// fakeGH serves a GitHub-like API and release assets.
type fakeGH struct {
	*httptest.Server
	mu       sync.Mutex
	tag      string
	asset    []byte
	digest   string // "" = omit
	sums     string // "" = no SHA256SUMS asset
	mirrored []string
	direct   []string
	apiCalls int
}

func newFake(t *testing.T, tag string, asset []byte) *fakeGH {
	f := &fakeGH{tag: tag, asset: asset}
	sum := sha256.Sum256(asset)
	f.digest = "sha256:" + hex.EncodeToString(sum[:])
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeGH) name() string { return "pichat_" + strings.TrimPrefix(f.tag, "v") + "_linux_amd64" }

func (f *fakeGH) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/mirror/"):
		f.mirrored = append(f.mirrored, p)
		// The mirror gets the full original URL appended to its prefix.
		orig := strings.TrimPrefix(r.URL.String(), "/mirror/")
		if !strings.HasPrefix(orig, f.URL+"/dl/") {
			http.Error(w, "bad mirror url "+orig, 400)
			return
		}
		p = strings.TrimPrefix(orig, f.URL)
		fallthrough
	case strings.HasPrefix(p, "/dl/"):
		if !strings.HasPrefix(r.URL.Path, "/mirror/") {
			f.direct = append(f.direct, p)
		}
		switch strings.TrimPrefix(p, "/dl/") {
		case f.name():
			w.Write(f.asset)
		case "SHA256SUMS":
			fmt.Fprint(w, f.sums)
		default:
			http.NotFound(w, r)
		}
	case p == "/repos/o/r/releases/latest" || p == "/repos/o/r/releases/tags/"+f.tag:
		f.apiCalls++
		if r.Header.Get("User-Agent") != "pichat/0.2.0" || r.Header.Get("Accept") != "application/vnd.github+json" {
			http.Error(w, "headers", 400)
			return
		}
		a := map[string]any{"name": f.name(), "size": len(f.asset), "browser_download_url": f.URL + "/dl/" + f.name()}
		if f.digest != "" {
			a["digest"] = f.digest
		}
		assets := []any{a, map[string]any{"name": "pichat_x_darwin_arm64", "size": 1, "browser_download_url": f.URL + "/dl/other"}}
		if f.sums != "" {
			assets = append(assets, map[string]any{"name": "SHA256SUMS", "size": len(f.sums), "browser_download_url": f.URL + "/dl/SHA256SUMS"})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "name": f.tag, "published_at": "2026-11-19T00:00:00Z",
			"body": "## 改动\n- x", "html_url": "https://github.com/o/r/releases/" + f.tag, "assets": assets})
	default:
		http.NotFound(w, r)
	}
}

type restartRec struct {
	ch chan [2]string
}

func newUpdater(t *testing.T, f *fakeGH, mirror string) (*Updater, string, *restartRec) {
	data := t.TempDir()
	rec := &restartRec{ch: make(chan [2]string, 1)}
	u := New(Config{Enabled: true, Repo: "o/r", API: f.URL, Mirror: mirror, Data: data, GOOS: "linux", GOARCH: "amd64",
		Self: Self{Version: "0.2.0", BuiltinVersion: "0.2.0"}, Restart: func(p, v string) { rec.ch <- [2]string{p, v} }})
	return u, data, rec
}

// wait returns the final state of an install.
func wait(t *testing.T, u *Updater, rec *restartRec) (State, [2]string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case r := <-rec.ch:
			return u.Status(context.Background(), false).State, r
		case <-deadline:
			t.Fatal("install timed out")
		case <-time.After(20 * time.Millisecond):
			if st := u.Status(context.Background(), false).State; st.Phase == "error" {
				return st, [2]string{}
			}
		}
	}
}

var script = []byte("#!/bin/sh\necho 0.3.0\n")

func TestCheckAvailable(t *testing.T) {
	f := newFake(t, "v0.3.0", script)
	u, _, _ := newUpdater(t, f, "")
	st := u.Status(context.Background(), false)
	if !st.Enabled || !st.Available || st.Latest == nil || st.Latest.Tag != "v0.3.0" || st.Latest.Asset.Name != "pichat_0.3.0_linux_amd64" || st.Running != "builtin" || st.Platform != "linux/amd64" {
		t.Fatalf("status: %+v", st)
	}
	// Cached for 10 minutes unless refreshed.
	u.Status(context.Background(), false)
	if f.apiCalls != 1 {
		t.Fatalf("api calls %d", f.apiCalls)
	}
	u.Status(context.Background(), true)
	if f.apiCalls != 2 {
		t.Fatalf("refresh api calls %d", f.apiCalls)
	}
	// Not newer.
	f2 := newFake(t, "v0.2.0", script)
	u2, _, _ := newUpdater(t, f2, "")
	if st := u2.Status(context.Background(), false); st.Available {
		t.Fatalf("same version available: %+v", st)
	}
	f3 := newFake(t, "v0.2.0-rc1", script)
	u3, _, _ := newUpdater(t, f3, "")
	if st := u3.Status(context.Background(), false); st.Available {
		t.Fatalf("prerelease available: %+v", st)
	}
	// Background notification fires once per release.
	n := 0
	u.checkAndNotify(context.Background(), func(*Release) { n++ })
	u.checkAndNotify(context.Background(), func(*Release) { n++ })
	if n != 1 {
		t.Fatalf("notified %d", n)
	}
	// Disabled and unsupported platforms.
	if d := New(Config{Enabled: false}); d.Enabled() || d.Status(context.Background(), false).DisabledReason == "" {
		t.Fatal("off not disabled")
	}
	if d := New(Config{Enabled: true, GOOS: "windows"}); d.Enabled() {
		t.Fatal("windows enabled")
	}
}

func TestInstallDigest(t *testing.T) {
	f := newFake(t, "v0.3.0", script)
	u, data, rec := newUpdater(t, f, "")
	if err := u.Install("v0.3.0"); err != nil {
		t.Fatal(err)
	}
	st, r := wait(t, u, rec)
	want := filepath.Join(data, "bin", "pichat-0.3.0")
	if r != [2]string{want, "0.3.0"} || st.Phase != "restarting" || st.Bytes != int64(len(script)) || st.Total != int64(len(script)) {
		t.Fatalf("restart %v state %+v", r, st)
	}
	c := ReadCurrent(data)
	sum := sha256.Sum256(script)
	if c == nil || c.Version != "0.3.0" || c.Path != want || c.Sha256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("current %+v", c)
	}
	if fi, err := os.Stat(want); err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("binary %v %v", fi, err)
	}
	// Older or equal versions are refused.
	if err := New(Config{Enabled: true, Self: Self{Version: "0.3.0"}}).Install("v0.3.0"); err == nil {
		t.Fatal("same version accepted")
	}
}

func TestInstallDigestMismatch(t *testing.T) {
	f := newFake(t, "v0.3.0", script)
	f.digest = "sha256:" + strings.Repeat("0", 64)
	u, data, rec := newUpdater(t, f, "")
	u.Install("v0.3.0")
	st, _ := wait(t, u, rec)
	if st.Phase != "error" || !strings.Contains(st.Error, "校验失败") {
		t.Fatalf("state %+v", st)
	}
	if ReadCurrent(data) != nil {
		t.Fatal("current.json written")
	}
	ents, _ := os.ReadDir(filepath.Join(data, "bin"))
	if len(ents) != 0 {
		t.Fatalf("leftovers %v", ents)
	}
}

func TestInstallSumsFallbackAndMirror(t *testing.T) {
	f := newFake(t, "v0.3.0", script)
	sum := sha256.Sum256(script)
	f.digest = ""
	f.sums = hex.EncodeToString(sum[:]) + "  pichat_0.3.0_linux_amd64\n" + strings.Repeat("1", 64) + "  pichat-listen.mjs\n"
	u, _, rec := newUpdater(t, f, f.URL+"/mirror/")
	u.Install("v0.3.0")
	st, r := wait(t, u, rec)
	if st.Phase != "restarting" || r[1] != "0.3.0" {
		t.Fatalf("state %+v %v", st, r)
	}
	// The binary came through the mirror, SHA256SUMS directly.
	if len(f.mirrored) != 1 || !strings.HasSuffix(f.mirrored[0], "/dl/pichat_0.3.0_linux_amd64") {
		t.Fatalf("mirrored %v", f.mirrored)
	}
	if len(f.direct) != 1 || f.direct[0] != "/dl/SHA256SUMS" {
		t.Fatalf("direct %v", f.direct)
	}
}

func TestInstallMirrorCannotForge(t *testing.T) {
	// The mirror serves other bytes: the digest from the API rejects them.
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("#!/bin/sh\necho 0.3.0 \n")) }))
	defer evil.Close()
	f := newFake(t, "v0.3.0", script)
	u, _, rec := newUpdater(t, f, evil.URL+"/")
	u.Install("v0.3.0")
	if st, _ := wait(t, u, rec); st.Phase != "error" || !strings.Contains(st.Error, "校验失败") {
		t.Fatalf("state %+v", st)
	}
}

func TestInstallNoDigest(t *testing.T) {
	f := newFake(t, "v0.3.0", script)
	f.digest = ""
	u, _, rec := newUpdater(t, f, "")
	u.Install("v0.3.0")
	if st, _ := wait(t, u, rec); st.Phase != "error" || st.Error != "无法校验下载内容" {
		t.Fatalf("state %+v", st)
	}
}

func TestInstallVersionMismatch(t *testing.T) {
	f := newFake(t, "v0.3.0", []byte("#!/bin/sh\necho 0.2.9\n"))
	u, data, rec := newUpdater(t, f, "")
	u.Install("v0.3.0")
	st, _ := wait(t, u, rec)
	if st.Phase != "error" || !strings.Contains(st.Error, "新版本无法在本机运行") {
		t.Fatalf("state %+v", st)
	}
	if ReadCurrent(data) != nil {
		t.Fatal("current.json written")
	}
}

func TestInstallKeepsTwo(t *testing.T) {
	f := newFake(t, "v0.3.0", script)
	u, data, rec := newUpdater(t, f, "")
	bin := filepath.Join(data, "bin")
	os.MkdirAll(bin, 0o700)
	for _, v := range []string{"0.2.1", "0.2.2"} {
		os.WriteFile(filepath.Join(bin, "pichat-"+v), []byte("x"), 0o755)
	}
	writeJSONAtomic(currentFile(data), Current{Version: "0.2.2", Path: filepath.Join(bin, "pichat-0.2.2")})
	u.Install("v0.3.0")
	wait(t, u, rec)
	var names []string
	ents, _ := os.ReadDir(bin)
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if got := strings.Join(names, ","); got != "boot.json,current.json,pichat-0.2.2,pichat-0.3.0" {
		t.Fatalf("bin: %s", got)
	}
}

func TestRollback(t *testing.T) {
	data := t.TempDir()
	if _, err := New(Config{Enabled: true, Data: data, Self: Self{Version: "0.3.0"}}).Rollback(); err == nil {
		t.Fatal("rollback while builtin")
	}
	builtin := filepath.Join(data, "pichat-builtin")
	os.WriteFile(builtin, []byte("x"), 0o755)
	writeJSONAtomic(currentFile(data), Current{Version: "0.3.0", Path: "/x"})
	u := New(Config{Enabled: true, Data: data, Self: Self{Version: "0.3.0", Installed: true, BuiltinPath: builtin, BuiltinVersion: "0.2.0"}})
	p, err := u.Rollback()
	if err != nil || p != builtin || ReadCurrent(data) != nil {
		t.Fatalf("rollback %q %v", p, err)
	}
	// Builtin running because the installed release failed: clear without restart.
	data2, bin := setup(t, "0.3.0", 0o755, true)
	writeJSONAtomic(bootFile(data2), Boot{Version: "0.3.0", Attempts: 3})
	u2 := New(Config{Enabled: true, Data: data2, Self: Self{Version: "0.2.0", BuiltinVersion: "0.2.0"}, Log: slogDiscard()})
	if p, err := u2.Rollback(); err != nil || p != "" || ReadCurrent(data2) != nil {
		t.Fatalf("clear failed: %q %v", p, err)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatalf("failed binary kept: %v", err)
	}
	// A newer installed release that did not fail is not cleared from the builtin.
	data3, _ := setup(t, "0.3.0", 0o755, false)
	u3 := New(Config{Enabled: true, Data: data3, Self: Self{Version: "0.2.0", BuiltinVersion: "0.2.0"}, Log: slogDiscard()})
	if _, err := u3.Rollback(); err == nil || ReadCurrent(data3) == nil {
		t.Fatalf("cleared a healthy install: %v", err)
	}
}

// ---- launcher ----

func setup(t *testing.T, ver string, mode os.FileMode, failed bool) (string, string) {
	data := t.TempDir()
	p := filepath.Join(data, "bin", "pichat-"+ver)
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, script, mode)
	sum := sha256.Sum256(script)
	writeJSONAtomic(currentFile(data), Current{Version: ver, Path: p, Sha256: hex.EncodeToString(sum[:]), Failed: failed})
	return data, p
}

func TestDecide(t *testing.T) {
	if d := Decide(t.TempDir(), "0.2.0", ""); d.Exec {
		t.Fatal("no current.json")
	}
	data, p := setup(t, "0.3.0", 0o755, false)
	if d := Decide(data, "0.2.0", ""); !d.Exec || d.Path != p || d.Version != "0.3.0" {
		t.Fatalf("newer: %+v", d)
	}
	// Builtin upgraded by hand to the same or a newer version.
	if d := Decide(data, "0.3.0", ""); d.Exec {
		t.Fatalf("equal: %+v", d)
	}
	if d := Decide(data, "0.4.0", ""); d.Exec {
		t.Fatalf("older installed: %+v", d)
	}
	// Loop guard.
	if d := Decide(data, "0.2.0", "0.3.0"); d.Exec {
		t.Fatalf("PICHAT_BOOT: %+v", d)
	}
	// Boot guard.
	writeJSONAtomic(bootFile(data), Boot{Version: "0.3.0", Attempts: 2})
	if d := Decide(data, "0.2.0", ""); !d.Exec {
		t.Fatalf("2 attempts: %+v", d)
	}
	writeJSONAtomic(bootFile(data), Boot{Version: "0.3.0", Attempts: 3})
	if d := Decide(data, "0.2.0", ""); d.Exec || !d.MarkFailed || !strings.Contains(d.Reason, "连续 3 次启动失败，改用内置版本 0.2.0") {
		t.Fatalf("3 attempts: %+v", d)
	}
	// Attempts of another version don't count.
	writeJSONAtomic(bootFile(data), Boot{Version: "0.2.5", Attempts: 9})
	if d := Decide(data, "0.2.0", ""); !d.Exec {
		t.Fatalf("other version attempts: %+v", d)
	}
	// Not executable / missing / failed.
	data2, _ := setup(t, "0.3.0", 0o644, false)
	if d := Decide(data2, "0.2.0", ""); d.Exec {
		t.Fatalf("not executable: %+v", d)
	}
	data3, p3 := setup(t, "0.3.0", 0o755, false)
	os.Remove(p3)
	if d := Decide(data3, "0.2.0", ""); d.Exec {
		t.Fatalf("missing: %+v", d)
	}
	data4, _ := setup(t, "0.3.0", 0o755, true)
	if d := Decide(data4, "0.2.0", ""); d.Exec {
		t.Fatalf("failed: %+v", d)
	}
	// Binary changed after install: not exec'd, marked failed.
	data5, p5 := setup(t, "0.3.0", 0o755, false)
	os.WriteFile(p5, []byte("#!/bin/sh\necho evil\n"), 0o755)
	if d := Decide(data5, "0.2.0", ""); d.Exec || !d.MarkFailed {
		t.Fatalf("sha mismatch: %+v", d)
	}
	// current.json pointing outside <data>/bin, or at a symlink.
	data6, p6 := setup(t, "0.3.0", 0o755, false)
	c := ReadCurrent(data6)
	other := filepath.Join(data6, "x")
	os.WriteFile(other, script, 0o755)
	c.Path = other
	writeJSONAtomic(currentFile(data6), c)
	if d := Decide(data6, "0.2.0", ""); d.Exec || !d.MarkFailed {
		t.Fatalf("outside bin: %+v", d)
	}
	os.Remove(p6)
	os.Symlink(other, p6)
	c.Path = p6
	writeJSONAtomic(currentFile(data6), c)
	if d := Decide(data6, "0.2.0", ""); d.Exec {
		t.Fatalf("symlink: %+v", d)
	}
}

func TestLaunch(t *testing.T) {
	var got []string
	var gotEnv []string
	old := ExecFunc
	ExecFunc = func(p string, argv, env []string) error { got = append(got, p); gotEnv = env; return nil }
	t.Cleanup(func() { ExecFunc = old })
	t.Setenv(EnvBoot, "")
	data, p := setup(t, "0.3.0", 0o755, false)
	self := Self{Version: "0.2.0", BuiltinPath: "/usr/local/bin/pichat", BuiltinVersion: "0.2.0"}
	for i := 0; i < 4; i++ {
		Launch(data, self, slogDiscard())
	}
	// Three attempts exec'd, the fourth trips the guard and marks current.json failed.
	if len(got) != 3 || got[0] != p {
		t.Fatalf("execs %v", got)
	}
	if c := ReadCurrent(data); c == nil || !c.Failed {
		t.Fatalf("current %+v", c)
	}
	env := strings.Join(gotEnv, "\n")
	if !strings.Contains(env, EnvBoot+"=0.3.0") || !strings.Contains(env, EnvBuiltin+"=/usr/local/bin/pichat") {
		t.Fatalf("env %s", env)
	}
	// Healthy run resets attempts.
	data2, _ := setup(t, "0.3.0", 0o755, false)
	got = nil
	Launch(data2, self, slogDiscard())
	MarkHealthy(data2, "0.3.0")
	if b := readBoot(data2); b.Attempts != 0 || len(got) != 1 {
		t.Fatalf("healthy: %+v", b)
	}
}

func slogDiscard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestCheckCancelNotCached(t *testing.T) {
	f := newFake(t, "v0.3.0", script)
	u, _, _ := newUpdater(t, f, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := u.Check(ctx, false); err == nil {
		t.Fatal("cancelled check succeeded")
	}
	if r, err := u.Check(context.Background(), false); err != nil || r == nil || r.Tag != "v0.3.0" {
		t.Fatalf("cancel was cached: %v %v", r, err)
	}
}

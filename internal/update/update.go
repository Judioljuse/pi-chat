// Package update checks GitHub releases of pichat, installs a newer server binary into
// <data>/bin and decides at startup which binary to run (launcher.go).
//
// Trust model: the server downloads and executes releases published in the configured GitHub
// repository. Integrity comes from the asset digest in the GitHub API response (or the release's
// SHA256SUMS fetched directly from GitHub), both over TLS; a download mirror only serves bytes.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultRepo = "Judioljuse/pi-chat"
	DefaultAPI  = "https://api.github.com"

	checkTimeout    = 15 * time.Second
	cacheFor        = 10 * time.Minute
	recheckEvery    = 12 * time.Hour
	maxAsset        = 200 << 20
	downloadTimeout = 10 * time.Minute
	versionTimeout  = 10 * time.Second
)

// Config configures the updater. Zero GOOS/GOARCH mean runtime values.
type Config struct {
	Enabled      bool
	Repo         string // owner/name
	API          string // GitHub API base
	Mirror       string // optional prefix for asset downloads
	Data         string // data directory (binaries go to <data>/bin)
	Self         Self
	GOOS, GOARCH string
	Client       *http.Client
	Log          *slog.Logger
	// Restart is called after a release is installed (path of the new binary, its version).
	Restart func(path, version string)
}

// Asset is a release file.
type Asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	URL    string `json:"-"`
	Digest string `json:"-"`
}

// Release is the subset of a GitHub release the UI needs.
type Release struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	PublishedAt string `json:"publishedAt"`
	Notes       string `json:"notes"`
	HTMLURL     string `json:"htmlUrl"`
	Asset       *Asset `json:"asset"`
	sums        string // browser_download_url of SHA256SUMS
}

// State is the progress of an install.
type State struct {
	Phase string `json:"phase"` // idle | downloading | verifying | restarting | error
	Bytes int64  `json:"bytes"`
	Total int64  `json:"total"`
	Error string `json:"error,omitempty"`
}

// Status is GET /admin/update.
type Status struct {
	Enabled        bool     `json:"enabled"`
	DisabledReason string   `json:"disabledReason,omitempty"`
	Current        string   `json:"current"`
	Builtin        string   `json:"builtin"`
	Running        string   `json:"running"` // builtin | installed
	Platform       string   `json:"platform"`
	Repo           string   `json:"repo"`
	Latest         *Release `json:"latest"`
	Available      bool     `json:"available"`
	CheckedAt      int64    `json:"checkedAt"`
	Error          string   `json:"error,omitempty"`
	State          State    `json:"state"`
	// Installed is the release recorded in current.json (failed: the boot guard gave up on it).
	Installed *Current `json:"installed"`
}

// Updater is safe for concurrent use.
type Updater struct {
	cfg    Config
	reason string

	mu        sync.Mutex
	latest    *Release
	checkedAt time.Time
	checkErr  string
	state     State
	notified  string
}

// ErrBusy is returned by Install while another install runs.
var ErrBusy = errors.New("正在更新，请稍候")

func New(cfg Config) *Updater {
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}
	if cfg.GOARCH == "" {
		cfg.GOARCH = runtime.GOARCH
	}
	if cfg.Repo == "" {
		cfg.Repo = DefaultRepo
	}
	if cfg.API == "" {
		cfg.API = DefaultAPI
	}
	cfg.API = strings.TrimRight(cfg.API, "/")
	if cfg.Client == nil {
		cfg.Client = &http.Client{}
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	u := &Updater{cfg: cfg, state: State{Phase: "idle"}}
	switch {
	case !cfg.Enabled:
		u.reason = "已用 PICHAT_UPDATE=off 关闭在线更新"
	case cfg.GOOS != "linux" && cfg.GOOS != "darwin":
		u.reason = "在线更新只支持 Linux 和 macOS（当前 " + cfg.GOOS + "）"
	case !validRepo(cfg.Repo):
		u.reason = "PICHAT_UPDATE_REPO 格式不对（应为 owner/name）：" + cfg.Repo
	}
	return u
}

func validRepo(r string) bool {
	o, n, ok := strings.Cut(r, "/")
	clean := func(s string) bool {
		if s == "" || len(s) > 100 || s == "." || s == ".." {
			return false
		}
		for _, c := range s {
			if !(c == '-' || c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
				return false
			}
		}
		return true
	}
	return ok && clean(o) && clean(n)
}

// Enabled reports whether checks and installs are allowed.
func (u *Updater) Enabled() bool { return u.reason == "" }

// SetRestart sets the function called after a release is installed.
func (u *Updater) SetRestart(f func(path, version string)) {
	u.mu.Lock()
	u.cfg.Restart = f
	u.mu.Unlock()
}

// Repo is the configured GitHub repository.
func (u *Updater) Repo() string { return u.cfg.Repo }

func (u *Updater) assetName(ver string) string {
	return fmt.Sprintf("pichat_%s_%s_%s", ver, u.cfg.GOOS, u.cfg.GOARCH)
}

// ---- GitHub API ----

type ghRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	Draft       bool   `json:"draft"`
	Assets      []struct {
		Name               string `json:"name"`
		Size               int64  `json:"size"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Digest             string `json:"digest"`
	} `json:"assets"`
}

func (u *Updater) get(ctx context.Context, rawURL string, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pichat/"+u.cfg.Self.Version)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	res, err := u.cfg.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, fmt.Errorf("%s 返回 %s", hostOf(rawURL), res.Status)
	}
	return res, nil
}

func hostOf(raw string) string {
	if p, err := url.Parse(raw); err == nil && p.Host != "" {
		return p.Host
	}
	return raw
}

func (u *Updater) fetchRelease(ctx context.Context, path string) (*Release, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	res, err := u.get(ctx, u.cfg.API+"/repos/"+u.cfg.Repo+path, "application/vnd.github+json")
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, fmt.Errorf("%s 还没有发布版本", u.cfg.Repo)
		}
		return nil, err
	}
	defer res.Body.Close()
	var g ghRelease
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&g); err != nil {
		return nil, fmt.Errorf("解析 GitHub 响应失败：%v", err)
	}
	ver := strings.TrimPrefix(g.TagName, "v")
	if !strings.HasPrefix(g.TagName, "v") || !ValidVersion(ver) {
		return nil, fmt.Errorf("发布的标签 %q 不是 vX.Y.Z", g.TagName)
	}
	r := &Release{Tag: g.TagName, Name: g.Name, PublishedAt: g.PublishedAt, Notes: g.Body, HTMLURL: g.HTMLURL}
	want := u.assetName(ver)
	for _, a := range g.Assets {
		switch a.Name {
		case want:
			r.Asset = &Asset{Name: a.Name, Size: a.Size, URL: a.BrowserDownloadURL, Digest: a.Digest}
		case "SHA256SUMS":
			r.sums = a.BrowserDownloadURL
		}
	}
	return r, nil
}

// Check returns the latest release, using the 10-minute cache unless force.
func (u *Updater) Check(ctx context.Context, force bool) (*Release, error) {
	if !u.Enabled() {
		return nil, errors.New(u.reason)
	}
	u.mu.Lock()
	if !force && !u.checkedAt.IsZero() && time.Since(u.checkedAt) < cacheFor {
		r, e := u.latest, u.checkErr
		u.mu.Unlock()
		if e != "" {
			return r, errors.New(e)
		}
		return r, nil
	}
	u.mu.Unlock()
	r, err := u.fetchRelease(ctx, "/releases/latest")
	u.mu.Lock()
	defer u.mu.Unlock()
	if err != nil && ctx.Err() != nil {
		// The caller went away (or its deadline passed): don't cache that for 10 minutes.
		return u.latest, err
	}
	u.checkedAt = time.Now()
	if err != nil {
		u.checkErr = err.Error()
		return u.latest, err
	}
	u.latest, u.checkErr = r, ""
	return r, nil
}

func (u *Updater) available(r *Release) bool {
	return r != nil && r.Asset != nil && Compare(strings.TrimPrefix(r.Tag, "v"), u.cfg.Self.Version) > 0
}

// Status reports versions, the cached latest release and install progress.
func (u *Updater) Status(ctx context.Context, refresh bool) Status {
	self := u.cfg.Self
	st := Status{Enabled: u.Enabled(), DisabledReason: u.reason, Current: self.Version, Builtin: self.BuiltinVersion,
		Running: "builtin", Platform: u.cfg.GOOS + "/" + u.cfg.GOARCH, Repo: u.cfg.Repo, Installed: ReadCurrent(u.cfg.Data)}
	if self.Installed {
		st.Running = "installed"
	}
	if st.Enabled {
		r, err := u.Check(ctx, refresh)
		st.Latest = r
		if err != nil {
			st.Error = err.Error()
		} else if r != nil && r.Asset == nil {
			st.Error = "最新版本 " + r.Tag + " 没有 " + st.Platform + " 的程序"
		}
		st.Available = u.available(r)
	}
	u.mu.Lock()
	if !u.checkedAt.IsZero() {
		st.CheckedAt = u.checkedAt.UnixMilli()
	}
	st.State = u.state
	u.mu.Unlock()
	return st
}

// Loop re-checks every 12 hours (first check after a minute) and calls onNew once per newer release.
func (u *Updater) Loop(ctx context.Context, onNew func(*Release)) {
	if !u.Enabled() {
		return
	}
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		u.checkAndNotify(ctx, onNew)
		t.Reset(recheckEvery)
	}
}

func (u *Updater) checkAndNotify(ctx context.Context, onNew func(*Release)) {
	r, err := u.Check(ctx, true)
	if err != nil {
		u.cfg.Log.Warn("检查更新失败", "err", err)
		return
	}
	if !u.available(r) {
		return
	}
	u.mu.Lock()
	fresh := u.notified != r.Tag
	u.notified = r.Tag
	u.mu.Unlock()
	if fresh {
		u.cfg.Log.Info("有新版本", "latest", r.Tag, "current", u.cfg.Self.Version)
		onNew(r)
	}
}

// ---- install ----

func (u *Updater) setState(f func(s *State)) {
	u.mu.Lock()
	f(&u.state)
	u.mu.Unlock()
}

// Install validates tag and starts installing it in the background; progress is in Status().State.
func (u *Updater) Install(tag string) error {
	if !u.Enabled() {
		return errors.New(u.reason)
	}
	ver := strings.TrimPrefix(tag, "v")
	if !strings.HasPrefix(tag, "v") || !ValidVersion(ver) {
		return fmt.Errorf("版本号格式不对：%q", tag)
	}
	if Compare(ver, u.cfg.Self.Version) <= 0 {
		return fmt.Errorf("当前已经是 %s，不需要更新到 %s", u.cfg.Self.Version, tag)
	}
	u.mu.Lock()
	switch u.state.Phase {
	case "downloading", "verifying", "restarting":
		u.mu.Unlock()
		return ErrBusy
	}
	u.state = State{Phase: "downloading"}
	u.mu.Unlock()
	go func() {
		path, err := u.install(context.Background(), tag, ver)
		if err != nil {
			u.cfg.Log.Error("更新失败", "tag", tag, "err", err)
			u.setState(func(s *State) { s.Phase, s.Error = "error", err.Error() })
			return
		}
		u.cfg.Log.Info("新版本已安装，重启", "version", ver, "path", path)
		u.mu.Lock()
		u.state.Phase = "restarting"
		restart := u.cfg.Restart
		u.mu.Unlock()
		if restart != nil {
			restart(path, ver)
		}
	}()
	return nil
}

// progress counts downloaded bytes into the state.
type progress struct{ u *Updater }

func (p progress) Write(b []byte) (int, error) {
	p.u.setState(func(s *State) { s.Bytes += int64(len(b)) })
	return len(b), nil
}

func (u *Updater) install(ctx context.Context, tag, ver string) (string, error) {
	r, err := u.fetchRelease(ctx, "/releases/tags/"+url.PathEscape(tag))
	if err != nil {
		return "", fmt.Errorf("读取发布信息失败：%v", err)
	}
	if r.Asset == nil {
		return "", fmt.Errorf("%s 没有 %s/%s 的程序（%s）", tag, u.cfg.GOOS, u.cfg.GOARCH, u.assetName(ver))
	}
	a := r.Asset
	if a.Size > maxAsset {
		return "", fmt.Errorf("程序太大（%d 字节）", a.Size)
	}
	want, err := u.expectedDigest(ctx, r)
	if err != nil {
		return "", err
	}
	u.setState(func(s *State) { s.Total = a.Size })

	dir := binDir(u.cfg.Data)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// MkdirAll keeps the mode of an existing directory; only pichat should write here.
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".dl-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			os.Remove(tmpName)
		}
	}()
	dl := a.URL
	if u.cfg.Mirror != "" {
		dl = strings.TrimRight(u.cfg.Mirror, "/") + "/" + a.URL
	}
	dctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	res, err := u.get(dctx, dl, "application/octet-stream")
	if err != nil {
		tmp.Close()
		return "", fmt.Errorf("下载失败：%v", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h, progress{u}), io.LimitReader(res.Body, maxAsset+1))
	res.Body.Close()
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("下载失败：%v", err)
	}
	if n > maxAsset {
		return "", errors.New("下载内容超过 200 MB")
	}
	u.setState(func(s *State) { s.Phase = "verifying" })
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return "", fmt.Errorf("校验失败：下载内容的 sha256 是 %s，应为 %s", got, want)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return "", err
	}
	if err := checkRuns(tmpName, ver); err != nil {
		return "", err
	}
	final := filepath.Join(dir, "pichat-"+ver)
	if err := os.Rename(tmpName, final); err != nil {
		return "", err
	}
	keep = true
	prev := ReadCurrent(u.cfg.Data)
	if err := writeJSONAtomic(currentFile(u.cfg.Data), Current{Version: ver, Path: final, Sha256: got, InstalledAt: time.Now().UnixMilli()}); err != nil {
		return "", err
	}
	// The restart into it counts as the first start attempt (see the boot guard in launcher.go).
	writeJSONAtomic(bootFile(u.cfg.Data), Boot{Version: ver, Attempts: 1})
	u.prune(final, prev)
	return final, nil
}

// expectedDigest is the asset's sha256 from the API, else from SHA256SUMS fetched directly from GitHub.
func (u *Updater) expectedDigest(ctx context.Context, r *Release) (string, error) {
	if d, ok := strings.CutPrefix(r.Asset.Digest, "sha256:"); ok && isHex64(d) {
		return strings.ToLower(d), nil
	}
	if r.sums == "" {
		return "", errors.New("无法校验下载内容")
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	res, err := u.get(ctx, r.sums, "application/octet-stream") // never through the mirror
	if err != nil {
		return "", fmt.Errorf("无法校验下载内容：读取 SHA256SUMS 失败：%v", err)
	}
	defer res.Body.Close()
	sc := bufio.NewScanner(io.LimitReader(res.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == r.Asset.Name && isHex64(f[0]) {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", errors.New("无法校验下载内容：SHA256SUMS 里没有 " + r.Asset.Name)
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// checkRuns runs '<file> version' and requires it to print ver.
func checkRuns(file, ver string) error {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, file, "version")
	cmd.Env = append(os.Environ(), EnvBoot+"=", EnvBuiltin+"=")
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, syscall.EACCES) || errors.Is(err, os.ErrPermission) {
			return errors.New("数据目录不允许执行程序（noexec），请用重新构建镜像/替换程序的方式更新")
		}
		if ctx.Err() != nil {
			return errors.New("新版本无法在本机运行：10 秒内没有响应")
		}
		return fmt.Errorf("新版本无法在本机运行：%v", err)
	}
	if got := strings.TrimSpace(string(out)); got != ver {
		if len(got) > 80 {
			got = got[:80]
		}
		return fmt.Errorf("新版本无法在本机运行：version 输出 %q，应为 %s", got, ver)
	}
	return nil
}

// prune keeps only the new binary, the previous installed one and the running executable.
func (u *Updater) prune(final string, prev *Current) {
	keep := map[string]bool{final: true}
	if prev != nil {
		keep[prev.Path] = true
	}
	if exe, err := os.Executable(); err == nil {
		keep[exe] = true
	}
	dir := binDir(u.cfg.Data)
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		p := filepath.Join(dir, e.Name())
		if (strings.HasPrefix(e.Name(), "pichat-") || strings.HasPrefix(e.Name(), ".dl-")) && !keep[p] {
			if err := os.Remove(p); err == nil {
				u.cfg.Log.Info("删除旧版本程序", "path", p)
			}
		}
	}
}

// Rollback removes current.json and returns the builtin binary to restart into.
// When the builtin already runs because the installed release failed to start or is not newer,
// it only clears current.json, boot.json and that binary, and returns "" (no restart needed).
func (u *Updater) Rollback() (string, error) {
	self := u.cfg.Self
	if !self.Installed {
		if c := ReadCurrent(u.cfg.Data); c != nil && (c.Failed || Compare(self.Version, c.Version) >= 0) {
			return "", u.clearInstalled(c)
		}
	}
	if !self.Installed || self.BuiltinPath == "" {
		return "", errors.New("当前运行的就是内置版本")
	}
	if fi, err := os.Stat(self.BuiltinPath); err != nil || fi.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("找不到内置程序 %s", self.BuiltinPath)
	}
	u.mu.Lock()
	switch u.state.Phase {
	case "downloading", "verifying", "restarting":
		u.mu.Unlock()
		return "", ErrBusy
	}
	u.state = State{Phase: "restarting"}
	u.mu.Unlock()
	if err := os.Remove(currentFile(u.cfg.Data)); err != nil && !os.IsNotExist(err) {
		u.setState(func(s *State) { *s = State{Phase: "error", Error: err.Error()} })
		return "", err
	}
	os.Remove(bootFile(u.cfg.Data))
	return self.BuiltinPath, nil
}

// clearInstalled forgets an installed release the builtin runs instead of (failed or not newer).
func (u *Updater) clearInstalled(c *Current) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch u.state.Phase {
	case "downloading", "verifying", "restarting":
		return ErrBusy
	}
	if err := os.Remove(currentFile(u.cfg.Data)); err != nil && !os.IsNotExist(err) {
		return err
	}
	os.Remove(bootFile(u.cfg.Data))
	// Only remove a binary pichat installed itself, never the running one.
	if want := filepath.Join(binDir(u.cfg.Data), "pichat-"+c.Version); samePath(c.Path, want) {
		if exe, err := os.Executable(); err != nil || !samePath(exe, want) {
			os.Remove(want)
		}
	}
	u.cfg.Log.Info("已清除失败或过时的已安装版本", "version", c.Version)
	return nil
}

package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Environment variables shared between the launcher and the binary it starts.
const (
	EnvBoot           = "PICHAT_BOOT"            // version the launcher / restart exec'd into
	EnvBuiltin        = "PICHAT_BUILTIN"         // path of the builtin (image / installed by hand) binary
	EnvBuiltinVersion = "PICHAT_BUILTIN_VERSION" // its version
	maxBootAttempts   = 3
)

// Current is <data>/bin/current.json: the installed release the launcher should run.
type Current struct {
	Version     string `json:"version"`
	Path        string `json:"path"`
	Sha256      string `json:"sha256"`
	InstalledAt int64  `json:"installedAt"`
	Failed      bool   `json:"failed,omitempty"`
}

// Boot is <data>/bin/boot.json: start attempts of an installed version that did not yet run 60 s.
type Boot struct {
	Version  string `json:"version"`
	Attempts int    `json:"attempts"`
}

func binDir(data string) string      { return filepath.Join(data, "bin") }
func currentFile(data string) string { return filepath.Join(binDir(data), "current.json") }
func bootFile(data string) string    { return filepath.Join(binDir(data), "boot.json") }

// ReadCurrent returns current.json, or nil if absent or unreadable.
func ReadCurrent(data string) *Current {
	var c Current
	b, err := os.ReadFile(currentFile(data))
	if err != nil || json.Unmarshal(b, &c) != nil || c.Version == "" {
		return nil
	}
	return &c
}

func readBoot(data string) Boot {
	var b Boot
	if raw, err := os.ReadFile(bootFile(data)); err == nil {
		json.Unmarshal(raw, &b)
	}
	return b
}

// writeJSONAtomic writes v to f through a temp file and rename.
func writeJSONAtomic(f string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), f); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Decision is what the launcher does at the start of 'serve'.
type Decision struct {
	Exec       bool   // exec Path
	Path       string // installed binary
	Version    string // installed version
	MarkFailed bool   // record failed:true in current.json (boot guard tripped)
	Reason     string // log text
}

// Decide inspects <data>/bin and decides whether to exec the installed release.
// builtin is this binary's version and boot the PICHAT_BOOT environment value.
func Decide(data, builtin, boot string) Decision {
	c := ReadCurrent(data)
	if c == nil {
		return Decision{Reason: "没有已安装的新版本，运行内置版本 " + builtin}
	}
	d := Decision{Path: c.Path, Version: c.Version}
	switch {
	case !ValidVersion(c.Version):
		d.Reason = fmt.Sprintf("current.json 里的版本 %q 无效，运行内置版本 %s", c.Version, builtin)
	case Compare(builtin, c.Version) >= 0:
		d.Reason = fmt.Sprintf("内置版本 %s 不低于已安装的 %s，运行内置版本", builtin, c.Version)
	case boot == c.Version:
		d.Reason = fmt.Sprintf("已经从 %s 启动过（%s=%s），不再切换，运行内置版本 %s", c.Version, EnvBoot, boot, builtin)
	case c.Failed:
		d.Reason = fmt.Sprintf("新版本 %s 之前连续启动失败，运行内置版本 %s", c.Version, builtin)
	case !samePath(c.Path, filepath.Join(binDir(data), "pichat-"+c.Version)):
		d.MarkFailed = true
		d.Reason = fmt.Sprintf("current.json 指向的程序 %s 不在 %s 里，不运行它，改用内置版本 %s", c.Path, binDir(data), builtin)
	default:
		fi, err := os.Lstat(c.Path)
		switch {
		case err != nil:
			d.Reason = fmt.Sprintf("已安装的程序 %s 不存在（%v），运行内置版本 %s", c.Path, err, builtin)
		case !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0:
			d.Reason = fmt.Sprintf("已安装的程序 %s 不是可执行的普通文件，运行内置版本 %s", c.Path, builtin)
		case !shaMatches(c.Path, c.Sha256):
			d.MarkFailed = true
			d.Reason = fmt.Sprintf("已安装的程序 %s 的 sha256 和安装时不一致（文件被改动或损坏），不运行它，改用内置版本 %s", c.Path, builtin)
		default:
			if b := readBoot(data); b.Version == c.Version && b.Attempts >= maxBootAttempts {
				d.MarkFailed = true
				d.Reason = fmt.Sprintf("新版本 %s 连续 %d 次启动失败，改用内置版本 %s", c.Version, maxBootAttempts, builtin)
			} else {
				d.Exec = true
				d.Reason = fmt.Sprintf("启动已安装的新版本 %s（%s），内置版本 %s", c.Version, c.Path, builtin)
			}
		}
	}
	return d
}

// samePath compares two paths after making them absolute and clean.
func samePath(a, b string) bool {
	x, err1 := filepath.Abs(a)
	y, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && x == y
}

// shaMatches reports whether file's sha256 (hex) equals want; an empty want never matches.
func shaMatches(file, want string) bool {
	if want == "" {
		return false
	}
	f, err := os.Open(file)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == strings.ToLower(want)
}

// ExecFunc replaces the process; tests swap it.
var ExecFunc = syscall.Exec

// Self describes the running process: its version, and whether the launcher (or a restart) started it as an installed release.
type Self struct {
	Version        string // version of this binary
	Installed      bool   // started as an installed release (PICHAT_BOOT == Version)
	BuiltinPath    string // the builtin binary (rollback target)
	BuiltinVersion string
}

// Detect reads the environment set by the launcher.
func Detect(ver string) Self {
	s := Self{Version: ver, BuiltinVersion: ver}
	if os.Getenv(EnvBoot) == ver && os.Getenv(EnvBuiltin) != "" {
		s.Installed = true
		s.BuiltinPath = os.Getenv(EnvBuiltin)
		if v := os.Getenv(EnvBuiltinVersion); v != "" {
			s.BuiltinVersion = v
		}
		return s
	}
	if exe, err := os.Executable(); err == nil {
		s.BuiltinPath = exe
	}
	return s
}

// Launch runs the launcher at the start of 'serve': it returns if this binary should keep running,
// otherwise it execs the installed release.
func Launch(data string, self Self, log *slog.Logger) {
	if self.Installed {
		log.Info(fmt.Sprintf("pichat %s（已安装的版本，内置版本 %s 在 %s）", self.Version, self.BuiltinVersion, self.BuiltinPath), "pid", os.Getpid())
		return
	}
	d := Decide(data, self.Version, os.Getenv(EnvBoot))
	if d.MarkFailed {
		log.Error(d.Reason)
		if c := ReadCurrent(data); c != nil {
			c.Failed = true
			if err := writeJSONAtomic(currentFile(data), c); err != nil {
				log.Error("写 current.json 失败", "err", err)
			}
		}
	}
	if !d.Exec {
		log.Info("pichat "+self.Version+"（内置版本）："+d.Reason, "pid", os.Getpid())
		return
	}
	n, err := countBoot(data, d.Version)
	if err != nil {
		log.Error("写 boot.json 失败，运行内置版本", "err", err)
		return
	}
	log.Info(d.Reason, "attempt", n)
	if err := ExecFunc(d.Path, os.Args, ExecEnv(d.Version, self)); err != nil {
		log.Error("启动新版本失败，运行内置版本 "+self.Version, "path", d.Path, "err", err)
	}
}

// countBoot increments the start attempts of version in boot.json.
func countBoot(data, version string) (int, error) {
	b := readBoot(data)
	if b.Version != version {
		b = Boot{Version: version}
	}
	b.Attempts++
	return b.Attempts, writeJSONAtomic(bootFile(data), b)
}

// ExecEnv is os.Environ() with PICHAT_BOOT=boot (removed when boot is "") and, when boot is set,
// PICHAT_BUILTIN / PICHAT_BUILTIN_VERSION pointing at self's builtin binary.
func ExecEnv(boot string, self Self) []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == EnvBoot || k == EnvBuiltin || k == EnvBuiltinVersion {
			continue
		}
		out = append(out, kv)
	}
	if boot != "" {
		out = append(out, EnvBoot+"="+boot, EnvBuiltin+"="+self.BuiltinPath, EnvBuiltinVersion+"="+self.BuiltinVersion)
	}
	return out
}

// MarkHealthy resets the boot attempts of version after it has served for a while.
func MarkHealthy(data, version string) {
	if b := readBoot(data); b.Version == version && b.Attempts != 0 {
		b.Attempts = 0
		writeJSONAtomic(bootFile(data), b)
	}
}

// HealthyAfter calls MarkHealthy after d unless done is closed first.
func HealthyAfter(data, version string, d time.Duration, done <-chan struct{}) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		MarkHealthy(data, version)
	case <-done:
	}
}

// ExecRestart replaces the process with path. boot is the installed version it runs as, or "" for the builtin binary.
func ExecRestart(path, boot string, self Self) error {
	return ExecFunc(path, os.Args, ExecEnv(boot, self))
}

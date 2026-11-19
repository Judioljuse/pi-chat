package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"pichat/internal/update"
	pversion "pichat/internal/version"
)

// version is the running server's version (internal/version/VERSION, overridable at build time).
var version = pversion.Version

// listenerSHA is the sha256 (hex) of the embedded listener script.
func (s *Server) listenerSHA() string {
	s.listenerOnce.Do(func() {
		sum := sha256.Sum256(s.assets.Listener)
		s.listenerSum = hex.EncodeToString(sum[:])
	})
	return s.listenerSum
}

// listenerInfo is sent to agents in the SSE hello so listeners can update themselves.
func (s *Server) listenerInfo() obj {
	return obj{"sha256": s.listenerSHA(), "version": version}
}

// updater returns the configured updater, or a disabled one.
func (s *Server) updater() *update.Updater {
	s.updOnce.Do(func() {
		if s.upd == nil {
			s.upd = update.New(update.Config{Enabled: false, Data: s.cfg.Data, Self: update.Detect(version), Log: s.log})
		}
		s.upd.SetRestart(s.restartInto)
	})
	return s.upd
}

// startUpdates begins the background release check (every 12 h).
func (s *Server) startUpdates(ctx context.Context) {
	go s.updater().Loop(ctx, func(r *update.Release) {
		admins, err := scanStrings(s.db.R.Query(`SELECT id FROM principals WHERE kind='human' AND is_admin=1 AND disabled=0`))
		if err != nil {
			s.log.Error("update notify", "err", err)
			return
		}
		f := encodeEvent(0, "update", obj{"available": true, "latest": r.Tag})
		for _, id := range admins {
			s.hub.sendHumans(id, f)
		}
	})
}

// checkPassword verifies the caller's password with the same rate limiting as PATCH /me.
func (s *Server) checkPassword(c *ctx, pw string) error {
	if c.a.TokenKind != "session" {
		return errForbidden()
	}
	if err := s.limited(c, "login"); err != nil {
		return err
	}
	var cur sql.NullString
	s.db.R.QueryRow(`SELECT password_hash FROM principals WHERE id=?`, c.a.ID).Scan(&cur)
	if pw == "" || bcrypt.CompareHashAndPassword([]byte(cur.String), []byte(pw)) != nil {
		s.failed(c, "login")
		return errf(403, "forbidden", "密码不对")
	}
	return nil
}

func (s *Server) getUpdate(c *ctx) (any, error) {
	ctx, cancel := context.WithTimeout(c.r.Context(), 20*time.Second)
	defer cancel()
	return s.updater().Status(ctx, c.r.URL.Query().Get("refresh") == "1"), nil
}

func (s *Server) postUpdate(c *ctx) (any, error) {
	var req struct{ Tag, Password string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if err := s.checkPassword(c, req.Password); err != nil {
		return nil, err
	}
	u := s.updater()
	if !u.Enabled() {
		return nil, errf(409, "update_disabled", "%s", u.Status(c.r.Context(), false).DisabledReason)
	}
	if err := u.Install(strings.TrimSpace(req.Tag)); err != nil {
		if errors.Is(err, update.ErrBusy) {
			return nil, errf(409, "busy", "%s", err.Error())
		}
		return nil, badRequest("%s", err.Error())
	}
	s.log.Info("管理员开始在线更新", "by", c.a.Name, "tag", req.Tag)
	writeJSON(c.w, 202, u.Status(c.r.Context(), false))
	return nil, nil
}

func (s *Server) postRollback(c *ctx) (any, error) {
	var req struct{ Password string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if err := s.checkPassword(c, req.Password); err != nil {
		return nil, err
	}
	path, err := s.updater().Rollback()
	if err != nil {
		if errors.Is(err, update.ErrBusy) {
			return nil, errf(409, "busy", "%s", err.Error())
		}
		return nil, badRequest("%s", err.Error())
	}
	if path == "" {
		s.log.Info("管理员清除了没有运行的已安装版本", "by", c.a.Name)
		return obj{"ok": true, "cleared": true}, nil
	}
	s.log.Info("管理员恢复内置版本", "by", c.a.Name, "path", path)
	go s.restartInto(path, "")
	writeJSON(c.w, 202, obj{"ok": true})
	return nil, nil
}

// restartInto restarts the server as path. boot is the installed version it runs as ("" = builtin).
func (s *Server) restartInto(path, boot string) {
	if s.Restart != nil {
		s.Restart(path, boot)
		return
	}
	s.restarting.Store(true)
	ver := boot
	if ver == "" {
		ver = "builtin"
	}
	s.hub.sendAll(encodeEvent(0, "restarting", obj{"version": ver}))
	time.Sleep(300 * time.Millisecond)
	s.Close()
	if srv := s.httpSrv; srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := srv.Shutdown(ctx); err != nil {
			s.log.Warn("关闭 HTTP 服务超时", "err", err)
		}
		cancel()
	}
	s.db.W.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	if boot != "" {
		s.snapshotDB(boot)
	}
	if err := s.db.Close(); err != nil {
		s.log.Warn("关闭数据库", "err", err)
	}
	s.log.Info("重启", "path", path, "version", ver)
	if err := update.ExecRestart(path, boot, update.Detect(version)); err != nil {
		s.log.Error("重启失败，退出（由 supervisor 重新启动）", "path", path, "err", err)
		os.Exit(1)
	}
}

// snapshotDB writes <data>/bin/pre-<ver>.db (VACUUM INTO) before a newer release migrates the schema,
// so a rollback to the builtin version can restore it. Only the latest snapshot is kept.
func (s *Server) snapshotDB(ver string) {
	dir := filepath.Join(s.cfg.Data, "bin")
	old, _ := filepath.Glob(filepath.Join(dir, "pre-*.db"))
	for _, f := range old {
		os.Remove(f)
	}
	out := filepath.Join(dir, "pre-"+ver+".db")
	if _, err := s.db.W.Exec(`VACUUM INTO ?`, out); err != nil {
		s.log.Warn("更新前备份数据库失败（继续更新）", "path", out, "err", err)
		os.Remove(out)
		return
	}
	s.log.Info("更新前已备份数据库", "path", out)
}

// sendAll delivers f to every connection (humans and agents).
func (h *hub) sendAll(f frame) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.subs {
		for s := range m {
			select {
			case s.ch <- f:
			default:
			}
		}
	}
}

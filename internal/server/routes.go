package server

import (
	"context"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"
)

func fileModTime(f *os.File) time.Time {
	if st, err := f.Stat(); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

func init() { mime.AddExtensionType(".webmanifest", "application/manifest+json") }

func (s *Server) routes() {
	const p = "/api/v1"
	h := s.handle
	h("POST "+p+"/auth/login", pub, s.login)
	h("POST "+p+"/auth/logout", human, s.logout)
	h("GET "+p+"/auth/invite/{code}", pub, s.getInvite)
	h("POST "+p+"/auth/redeem", pub, s.redeem)
	h("GET "+p+"/me", anyone, s.me)
	h("PATCH "+p+"/me", human|agentTok, s.patchMe)
	h("POST "+p+"/admin/invites", human|adminOnly, s.adminInvite)
	h("GET "+p+"/admin/users", human|adminOnly, s.adminUsers)

	h("POST "+p+"/pairing-codes", human, s.createPairCode)
	h("GET "+p+"/agents", human, s.listAgents)
	h("PATCH "+p+"/agents/{id}", human, s.patchAgent)
	h("DELETE "+p+"/agents/{id}", human, s.deleteAgent)
	h("POST "+p+"/agents/{id}/reset-token", human, s.resetToken)
	h("PUT "+p+"/agents/{id}/grants", human, s.changeGrant)
	h("DELETE "+p+"/agents/{id}/grants", human, s.changeGrant)
	h("PUT "+p+"/agents/{id}/wake", human, s.setWake)
	h("POST "+p+"/agents/{id}/reset-session", human, s.resetSession)

	h("POST "+p+"/pair", pub, s.pair)
	h("POST "+p+"/run-tokens", agentTok, s.createRunToken)
	h("DELETE "+p+"/run-tokens", runAny, s.deleteRunToken)
	h("POST "+p+"/runs/start", agentTok|runAny, s.startRun)
	h("POST "+p+"/runs", agentTok|runAny, s.reportRun)
	h("DELETE "+p+"/token", anyone, s.deleteToken)
	h("PUT "+p+"/me/runtime", agentTok, s.putRuntime)

	h("GET "+p+"/groups", human|agentTok|runCmd, s.listGroups)
	h("POST "+p+"/groups", human|agentTok, s.createGroup)
	h("GET "+p+"/groups/{id}", anyone, s.getGroup)
	h("PATCH "+p+"/groups/{id}", human, s.patchGroup)
	h("DELETE "+p+"/groups/{id}", human, s.deleteGroup)
	h("POST "+p+"/groups/{id}/invite", human, s.groupInvite)
	h("POST "+p+"/groups/{id}/members", human, s.addAgentMember)
	h("PATCH "+p+"/groups/{id}/members/{pid}", human, s.patchMember)
	h("DELETE "+p+"/groups/{id}/members/{pid}", human|agentTok, s.deleteMember)
	h("POST "+p+"/chats", human|agentTok|runCmd, s.openChat)

	h("GET "+p+"/groups/{id}/messages", anyone, s.getMessages)
	h("POST "+p+"/groups/{id}/messages", anyone, s.sendMessage)
	h("POST "+p+"/groups/{id}/read", anyone, s.markRead)
	h("DELETE "+p+"/groups/{id}/messages/{seq}", anyone, s.recall)

	h("GET "+p+"/inbox", human|agentTok|runCmd, s.inbox)
	s.mux.HandleFunc("GET "+p+"/events", s.events)
	h("POST "+p+"/files", anyone, s.upload)
	h("GET "+p+"/files/{id}", anyone, s.download)
	h("GET "+p, pub, s.describe)
	h("GET "+p+"/", pub, func(c *ctx) (any, error) { return nil, errNotFound() })

	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.db.R.PingContext(r.Context()); err != nil {
			http.Error(w, "db", 503)
			return
		}
		w.Write([]byte("ok"))
	})
	s.mux.HandleFunc("GET /skill.md", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write([]byte(strings.ReplaceAll(skillTemplate, "{{URL}}", s.cfg.PublicURL)))
	})
	s.mux.HandleFunc("GET /pichat-listen.mjs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(s.assets.Listener)
	})
	if s.assets.Web != nil {
		files := http.FileServerFS(s.assets.Web)
		static := http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Vite puts content-hashed bundles under assets/; everything else may change in place.
			if strings.HasPrefix(r.URL.Path, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			files.ServeHTTP(w, r)
		}))
		s.mux.Handle("GET /static/", secHeaders(static))
		s.mux.Handle("GET /{$}", secHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, err := fs.ReadFile(s.assets.Web, "index.html")
			if err != nil {
				http.Error(w, "index.html missing", 500)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(b)
		})))
	}
}

func secHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob:; style-src 'self' 'unsafe-inline'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) describe(c *ctx) (any, error) {
	return obj{"name": "pichat", "version": version, "skill": s.cfg.PublicURL + "/skill.md",
		"endpoints": []string{
			"POST /pair {code,name,platform}",
			"GET /me", "PATCH /me {intro}",
			"GET /inbox?since=&wait=",
			"GET /events (SSE)",
			"GET /groups", "POST /groups {name,topic}", "GET /groups/{id}",
			"GET /groups/{id}/messages[?until=|before=|after=&limit=]",
			"POST /groups/{id}/messages {text,mentions,replyTo,opKey,files,ignoreNew,until}",
			"POST /groups/{id}/read {seq}", "DELETE /groups/{id}/messages/{seq}",
			"POST /chats {with}", "POST /files (multipart file)", "GET /files/{id}",
			"POST /run-tokens {groupId,wakeId,session,host}", "PUT /me/runtime {state,session,host,name}", "DELETE /run-tokens", "POST /runs/start {groupId}", "POST /runs {groupId,ok,detail}", "DELETE /token",
		},
		"limits": obj{"sendPerMinute": 30, "hopLimit": hopLimit, "agentWakesPerOwnerPerHour": 30, "humanWakesPerAgentPerHour": 10,
			"groupMembers": maxMembers, "fileBytes": maxFile, "filesPerMessage": 8, "textChars": 10000, "runTokenMinutes": int(runTTL.Minutes())}}, nil
}

// ---- cleanup ----

func (s *Server) cleanupLoop(ctx context.Context) {
	s.cleanup()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.cleanup()
		}
	}
}

func (s *Server) cleanup() {
	t := now()
	day := 24 * time.Hour.Milliseconds()
	for _, q := range []struct {
		sql string
		arg int64
	}{
		{`DELETE FROM events WHERE created_at<?`, t - 7*day},
		{`DELETE FROM wakes WHERE created_at<?`, t - 2*time.Hour.Milliseconds()},
		{`DELETE FROM invites WHERE expires_at<?`, t},
		{`DELETE FROM tokens WHERE expires_at IS NOT NULL AND expires_at<?`, t},
		{`DELETE FROM files WHERE group_id IS NULL AND created_at<?`, t - day},
		{`DELETE FROM runs WHERE created_at<?`, t - 30*day},
	} {
		if _, err := s.db.W.Exec(q.sql, q.arg); err != nil {
			s.log.Error("cleanup", "err", err)
		}
	}
	// Remove blobs no longer referenced.
	s.blobMu.Lock()
	defer s.blobMu.Unlock()
	sums, err := scanStrings(s.db.R.Query(`SELECT DISTINCT sha256 FROM files`))
	if err == nil {
		keep := make(map[string]bool, len(sums))
		for _, x := range sums {
			keep[x] = true
		}
		root := s.cfg.Data + "/files"
		dirs, _ := os.ReadDir(root)
		for _, d := range dirs {
			if !d.IsDir() {
				if strings.HasPrefix(d.Name(), ".up-") {
					if fi, err := d.Info(); err == nil && time.Since(fi.ModTime()) > time.Hour {
						os.Remove(root + "/" + d.Name())
					}
				}
				continue
			}
			ents, _ := os.ReadDir(root + "/" + d.Name())
			for _, e := range ents {
				if !keep[e.Name()] {
					os.Remove(root + "/" + d.Name() + "/" + e.Name())
				}
			}
		}
	}
	s.rl.sweep()
	s.hopMu.Lock()
	for k, v := range s.hopNotice {
		if time.Since(v) > 10*time.Minute {
			delete(s.hopNotice, k)
		}
	}
	s.hopMu.Unlock()
	s.db.W.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
}

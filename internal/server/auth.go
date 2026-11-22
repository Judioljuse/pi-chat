package server

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"pichat/internal/store"
)

// actor is the authenticated caller of a request.
type actor struct {
	ID, Kind, Name, DisplayName string
	OwnerID                     string
	IsAdmin                     bool
	TokenHash                   []byte
	TokenKind                   string // session | agent | run
	RunGroup, RunWake           string
	RunLabel                    string // run token: the group it was issued for
	Commander                   bool   // run token issued for a commander wake
	RunSession, RunHost         string // run token: listener main session and host (for message provenance)
	RunTier                     string // run token: tier it was issued with
}

func (a *actor) isHuman() bool { return a.Kind == "human" }
func (a *actor) isAgent() bool { return a.Kind == "agent" }

func (a *actor) allowed(access int) bool {
	switch {
	case a.isHuman():
		return access&human != 0 && (access&adminOnly == 0 || a.IsAdmin)
	case a.TokenKind == "agent":
		return access&agentTok != 0
	case a.Commander:
		return access&runCmd != 0
	default:
		return access&runGroup != 0
	}
}

// canGroup reports whether the token scope allows touching group gid at all.
func (a *actor) canGroup(gid string) bool {
	return a.TokenKind != "run" || a.Commander || a.RunGroup == gid
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*actor, error) {
	var tok string
	cookie := false
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = strings.TrimSpace(h[7:])
	} else if c, err := r.Cookie(cookieName); err == nil {
		tok, cookie = c.Value, true
	}
	if tok == "" {
		return nil, nil
	}
	hash := store.Hash(tok)
	a := &actor{TokenHash: hash}
	var exp, lastUsed sql.NullInt64
	var owner, runGroup, runWake, label, runSession, runHost, runTier sql.NullString
	var admin, disabled int
	var deleted sql.NullInt64
	err := s.db.R.QueryRow(`SELECT p.id,p.kind,p.name,p.display_name,p.owner_id,p.is_admin,p.disabled,p.deleted_at,t.kind,t.label,t.run_group,t.run_wake,t.expires_at,t.last_used_at,t.run_session,t.run_host,t.run_tier
		FROM tokens t JOIN principals p ON p.id=t.principal_id WHERE t.hash=?`, hash).
		Scan(&a.ID, &a.Kind, &a.Name, &a.DisplayName, &owner, &admin, &disabled, &deleted, &a.TokenKind, &label, &runGroup, &runWake, &exp, &lastUsed, &runSession, &runHost, &runTier)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errUnauthorized()
		}
		return nil, err
	}
	t := now()
	if (exp.Valid && exp.Int64 < t) || deleted.Valid || (disabled != 0 && a.Kind != "agent") {
		return nil, errUnauthorized()
	}
	if disabled != 0 {
		// Reversible: listeners keep retrying instead of exiting.
		return nil, errf(403, "disabled", "agent 已被 owner 停用")
	}
	a.OwnerID, a.IsAdmin, a.RunGroup, a.RunWake, a.RunLabel = owner.String, admin != 0, runGroup.String, runWake.String, label.String
	if a.TokenKind == "run" {
		a.Commander = a.RunGroup == "*"
		a.RunSession, a.RunHost, a.RunTier = runSession.String, runHost.String, runTier.String
	}
	// Touch at most once a minute to keep writes rare; sessions slide their expiry.
	if !lastUsed.Valid || t-lastUsed.Int64 > 60_000 {
		if a.TokenKind == "session" {
			newExp := t + sessionTTL.Milliseconds()
			s.db.W.Exec(`UPDATE tokens SET last_used_at=?, expires_at=? WHERE hash=?`, t, newExp, hash)
			if cookie {
				s.setCookie(w, r, tok, time.UnixMilli(newExp))
			}
		} else {
			s.db.W.Exec(`UPDATE tokens SET last_used_at=? WHERE hash=?`, t, hash)
		}
		s.db.W.Exec(`UPDATE principals SET last_seen_at=? WHERE id=?`, t, a.ID)
	}
	return a, nil
}

func secureReq(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setCookie(w http.ResponseWriter, r *http.Request, val string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: val, Path: "/", Expires: exp, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: secureReq(r)})
}

func (s *Server) newToken(q querier, pid, kind, label, runGroup, runWake string, ttl time.Duration) (string, int64, error) {
	tok := "pc_" + store.RandString(20)
	var exp any
	var expMs int64
	if ttl > 0 {
		expMs = now() + ttl.Milliseconds()
		exp = expMs
	}
	_, err := q.Exec(`INSERT INTO tokens(hash,principal_id,kind,label,run_group,run_wake,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?)`,
		store.Hash(tok), pid, kind, nullStr(label), nullStr(runGroup), nullStr(runWake), now(), exp)
	return tok, expMs, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Server) startSession(c *ctx, q querier, uid string) error {
	tok, exp, err := s.newToken(q, uid, "session", "web", "", "", sessionTTL)
	if err != nil {
		return err
	}
	s.setCookie(c.w, c.r, tok, time.UnixMilli(exp))
	return nil
}

var nameRe = regexp.MustCompile(`^[a-z0-9_-]{3,32}$`)

type userJSON struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	IsAdmin     bool   `json:"isAdmin"`
	CreatedAt   int64  `json:"createdAt,omitempty"`
}

func (s *Server) loadUser(q querier, id string) (*userJSON, error) {
	u := &userJSON{Kind: "human"}
	var admin int
	err := q.QueryRow(`SELECT id,name,display_name,is_admin FROM principals WHERE id=? AND kind='human'`, id).Scan(&u.ID, &u.Name, &u.DisplayName, &admin)
	u.IsAdmin = admin != 0
	return u, notFoundIf(err)
}

func (s *Server) limited(c *ctx, key string) error {
	if !s.rl.peek("fail:"+key+":"+s.clientIP(c.r), 20, 15*time.Minute) {
		return errf(429, "rate_limited", "失败次数太多，请 15 分钟后再试")
	}
	return nil
}

func (s *Server) failed(c *ctx, key string) {
	s.rl.allow("fail:"+key+":"+s.clientIP(c.r), 20, 15*time.Minute)
}

func (s *Server) login(c *ctx) (any, error) {
	var req struct{ Name, Password string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	name := strings.ToLower(strings.TrimSpace(req.Name))
	nameKey := "fail:login-name:" + name
	if err := s.limited(c, "login"); err != nil {
		return nil, err
	}
	if !s.rl.peek(nameKey, 20, 15*time.Minute) {
		return nil, errf(429, "rate_limited", "该账号失败次数太多，请 15 分钟后再试")
	}
	var id string
	var hash sql.NullString
	var disabled int
	err := s.db.R.QueryRow(`SELECT id,password_hash,disabled FROM principals WHERE kind='human' AND name=?`, name).Scan(&id, &hash, &disabled)
	if err != nil || disabled != 0 || bcrypt.CompareHashAndPassword([]byte(hash.String), []byte(req.Password)) != nil {
		s.failed(c, "login")
		s.rl.allow(nameKey, 20, 15*time.Minute)
		return nil, errf(401, "unauthorized", "用户名或密码错误")
	}
	if err := s.startSession(c, s.db.W, id); err != nil {
		return nil, err
	}
	u, err := s.loadUser(s.db.R, id)
	return obj{"user": u}, err
}

func (s *Server) logout(c *ctx) (any, error) {
	s.db.W.Exec(`DELETE FROM tokens WHERE hash=?`, c.a.TokenHash)
	http.SetCookie(c.w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secureReq(c.r)})
	return obj{}, nil
}

type invite struct {
	Kind, GroupID, CreatedBy, AgentID string
	MaxUses, Uses                     int
	ExpiresAt                         int64
}

func (s *Server) loadInvite(q querier, code string) (*invite, error) {
	inv := &invite{}
	var g, cb, ag sql.NullString
	err := q.QueryRow(`SELECT kind,group_id,created_by,agent_id,max_uses,uses,expires_at FROM invites WHERE hash=?`, store.Hash(normCode(code))).
		Scan(&inv.Kind, &g, &cb, &ag, &inv.MaxUses, &inv.Uses, &inv.ExpiresAt)
	if err != nil || inv.Uses >= inv.MaxUses || inv.ExpiresAt < now() {
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, errf(404, "not_found", "邀请链接无效或已过期")
	}
	inv.GroupID, inv.CreatedBy, inv.AgentID = g.String, cb.String, ag.String
	return inv, nil
}

// normCode upper-cases pairing codes (which contain a dash) so users can type them in any case.
func normCode(code string) string {
	code = strings.TrimSpace(code)
	if len(code) == 9 && code[4] == '-' {
		return strings.ToUpper(code)
	}
	return code
}

func (s *Server) getInvite(c *ctx) (any, error) {
	inv, err := s.loadInvite(s.db.R, c.r.PathValue("code"))
	if err != nil || inv.Kind == "pair" {
		return nil, errf(404, "not_found", "邀请链接无效或已过期")
	}
	res := obj{"kind": inv.Kind, "valid": true}
	if inv.Kind == "group" {
		var id, name, topic string
		if err := s.db.R.QueryRow(`SELECT id,name,topic FROM groups WHERE id=?`, inv.GroupID).Scan(&id, &name, &topic); err != nil {
			return nil, errf(404, "not_found", "群已不存在")
		}
		res["group"] = obj{"id": id, "name": name, "topic": topic}
	}
	return res, nil
}

func (s *Server) redeem(c *ctx) (any, error) {
	var req struct{ Code, Name, DisplayName, Password string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if err := s.limited(c, "redeem"); err != nil {
		return nil, err
	}
	a := c.a
	if a != nil && !a.isHuman() {
		a = nil
	}
	var uid, gid string
	var pwHash []byte
	if a == nil {
		req.Name = strings.ToLower(strings.TrimSpace(req.Name))
		if !nameRe.MatchString(req.Name) {
			return nil, badRequest("用户名只能用小写字母、数字、_ 和 -，3 到 32 位")
		}
		if utf8.RuneCountInString(req.Password) < 8 || len(req.Password) > 72 {
			return nil, badRequest("密码 8 到 72 位")
		}
		var err error
		if pwHash, err = bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost); err != nil {
			return nil, err
		}
	}
	err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		inv, err := s.loadInvite(tx, req.Code)
		if err != nil || inv.Kind == "pair" {
			s.failed(c, "redeem")
			return errf(404, "not_found", "邀请链接无效或已过期")
		}
		if a != nil && inv.Kind != "group" {
			return errf(409, "conflict", "你已经登录，不能再用注册链接")
		}
		if a == nil {
			uid = store.NewID("u_")
			dn := cleanName(req.DisplayName)
			if dn == "" {
				dn = req.Name
			}
			if utf8.RuneCountInString(dn) > 32 {
				return badRequest("显示名最多 32 字")
			}
			_, err := tx.Exec(`INSERT INTO principals(id,kind,name,display_name,is_admin,password_hash,created_at) VALUES(?,?,?,?,?,?,?)`,
				uid, "human", req.Name, dn, b2i(inv.Kind == "admin"), string(pwHash), now())
			if err != nil {
				if strings.Contains(err.Error(), "UNIQUE") {
					return errf(409, "conflict", "用户名已被占用")
				}
				return err
			}
			if err := s.startSession(c, tx, uid); err != nil {
				return err
			}
		} else {
			uid = a.ID
		}
		if _, err := tx.Exec(`UPDATE invites SET uses=uses+1 WHERE hash=?`, store.Hash(normCode(req.Code))); err != nil {
			return err
		}
		if inv.Kind == "group" {
			gid = inv.GroupID
			if _, err := s.addMember(tx, after, gid, uid, "member"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	u, err := s.loadUser(s.db.R, uid)
	res := obj{"user": u}
	if gid != "" {
		res["groupId"] = gid
	}
	return res, err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Server) me(c *ctx) (any, error) {
	if c.a.isHuman() {
		return s.loadUser(s.db.R, c.a.ID)
	}
	return s.agentMe(c.a)
}

func (s *Server) agentMe(a *actor) (obj, error) {
	var platform, ownerName sql.NullString
	var intro, tier, notes string
	var runMax sql.NullInt64
	err := s.db.R.QueryRow(`SELECT p.platform,p.intro,p.commander_tier,p.owner_notes,o.display_name,p.run_max_min FROM principals p LEFT JOIN principals o ON o.id=p.owner_id WHERE p.id=?`, a.ID).
		Scan(&platform, &intro, &tier, &notes, &ownerName, &runMax)
	if err != nil {
		return nil, err
	}
	m := obj{"id": a.ID, "kind": "agent", "displayName": a.DisplayName, "ownerId": a.OwnerID, "ownerName": ownerName.String,
		"platform": platform.String, "intro": intro, "commanderTier": tier, "ownerNotes": notes, "runMaxMinutes": runMaxOf(runMax)}
	if a.TokenKind == "run" {
		m["run"] = obj{"groupId": a.RunLabel, "wakeId": a.RunWake}
	}
	return m, nil
}

func (s *Server) patchMe(c *ctx) (any, error) {
	var req struct {
		DisplayName, Password, Intro *string
		CurrentPassword              string `json:"currentPassword"`
	}
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if c.a.isHuman() {
		if req.DisplayName != nil {
			dn := cleanName(*req.DisplayName)
			if dn == "" || utf8.RuneCountInString(dn) > 32 {
				return nil, badRequest("显示名 1 到 32 字")
			}
			if _, err := s.db.W.Exec(`UPDATE principals SET display_name=? WHERE id=?`, dn, c.a.ID); err != nil {
				return nil, err
			}
			if err := s.syncNicks(c.a.ID, dn); err != nil {
				return nil, err
			}
		}
		if req.Password != nil {
			if utf8.RuneCountInString(*req.Password) < 8 || len(*req.Password) > 72 {
				return nil, badRequest("密码 8 到 72 位")
			}
			if err := s.limited(c, "login"); err != nil {
				return nil, err
			}
			var cur sql.NullString
			s.db.R.QueryRow(`SELECT password_hash FROM principals WHERE id=?`, c.a.ID).Scan(&cur)
			if bcrypt.CompareHashAndPassword([]byte(cur.String), []byte(req.CurrentPassword)) != nil {
				s.failed(c, "login")
				return nil, errf(403, "forbidden", "当前密码不对")
			}
			h, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
			if err != nil {
				return nil, err
			}
			// Changing the password signs out every other session.
			if err := s.tx(func(tx *sql.Tx, _ *[]func()) error {
				if _, err := tx.Exec(`UPDATE principals SET password_hash=? WHERE id=?`, string(h), c.a.ID); err != nil {
					return err
				}
				_, err := tx.Exec(`DELETE FROM tokens WHERE principal_id=? AND kind='session' AND hash<>?`, c.a.ID, c.a.TokenHash)
				return err
			}); err != nil {
				return nil, err
			}
		}
		u, err := s.loadUser(s.db.R, c.a.ID)
		return obj{"me": u}, err
	}
	if req.Intro != nil {
		if utf8.RuneCountInString(*req.Intro) > 500 {
			return nil, badRequest("简介最多 500 字")
		}
		if _, err := s.db.W.Exec(`UPDATE principals SET intro=? WHERE id=?`, *req.Intro, c.a.ID); err != nil {
			return nil, err
		}
	}
	m, err := s.agentMe(c.a)
	return obj{"me": m}, err
}

func (s *Server) adminInvite(c *ctx) (any, error) {
	code := store.RandString(15)
	if _, err := s.db.W.Exec(`INSERT INTO invites(hash,kind,created_by,max_uses,expires_at,created_at) VALUES(?,?,?,1,?,?)`,
		store.Hash(code), "user", c.a.ID, now()+24*time.Hour.Milliseconds(), now()); err != nil {
		return nil, err
	}
	return obj{"code": code, "url": s.cfg.PublicURL + "/#/redeem/" + code}, nil
}

func (s *Server) adminUsers(c *ctx) (any, error) {
	rows, err := s.db.R.Query(`SELECT id,name,display_name,is_admin,created_at FROM principals WHERE kind='human' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []userJSON{}
	for rows.Next() {
		u := userJSON{Kind: "human"}
		var admin int
		if err := rows.Scan(&u.ID, &u.Name, &u.DisplayName, &admin, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.IsAdmin = admin != 0
		users = append(users, u)
	}
	return obj{"users": users}, rows.Err()
}

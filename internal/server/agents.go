package server

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"pichat/internal/store"
)

const pairAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func pairCode() string {
	b := make([]byte, 8)
	rand.Read(b)
	out := make([]byte, 0, 9)
	for i, c := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, pairAlphabet[int(c)%len(pairAlphabet)])
	}
	return string(out)
}

func (s *Server) pairInstruction(code string, reset bool) string {
	u := s.cfg.PublicURL
	what := "接入"
	if reset {
		what = "重新接入（换新 token，名字不用改）"
	}
	return fmt.Sprintf(`请读 %s/skill.md，用配对码 %s %s pichat 群聊。

如果要在本机常驻（被 @ 时自动唤醒），下载监听脚本并配对：
curl -fsSL %s/pichat-listen.mjs -o ~/.pichat/pichat-listen.mjs --create-dirs
node ~/.pichat/pichat-listen.mjs pair %s --server %s --name <名字> --agent claude|pi|codex [--dir <项目目录>]

配对码 30 分钟内有效，只能用一次，不要发到任何群里。`, u, code, what, u, code, u)
}

func (s *Server) createPairCode(c *ctx) (any, error) {
	var req struct{ GroupID string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if req.GroupID != "" {
		if _, err := s.role(s.db.R, c.a, req.GroupID); err != nil {
			return nil, err
		}
		if g, err := s.loadGroup(s.db.R, req.GroupID); err != nil || g.Kind != "group" {
			return nil, badRequest("只能把 agent 拉进群，不能拉进私聊")
		}
	}
	return s.issuePairCode(c.a.ID, req.GroupID, "")
}

func (s *Server) issuePairCode(owner, gid, agentID string) (obj, error) {
	code := pairCode()
	exp := now() + 30*time.Minute.Milliseconds()
	if _, err := s.db.W.Exec(`INSERT INTO invites(hash,kind,group_id,created_by,agent_id,max_uses,expires_at,created_at) VALUES(?,?,?,?,?,1,?,?)`,
		store.Hash(code), "pair", nullStr(gid), owner, nullStr(agentID), exp, now()); err != nil {
		return nil, err
	}
	return obj{"code": code, "expiresAt": exp, "instruction": s.pairInstruction(code, agentID != "")}, nil
}

var platforms = map[string]bool{"claude_code": true, "pi": true, "codex": true, "other": true}

func (s *Server) pair(c *ctx) (any, error) {
	var req struct{ Code, Name, Platform string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if err := s.limited(c, "pair"); err != nil {
		return nil, err
	}
	var tok, aid, dn, ownerName, gid string
	err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		inv, err := s.loadInvite(tx, req.Code)
		if err != nil || inv.Kind != "pair" {
			s.failed(c, "pair")
			return errf(404, "not_found", "配对码无效或已过期")
		}
		if _, err := tx.Exec(`UPDATE invites SET uses=uses+1 WHERE hash=?`, store.Hash(normCode(req.Code))); err != nil {
			return err
		}
		tx.QueryRow(`SELECT display_name FROM principals WHERE id=?`, inv.CreatedBy).Scan(&ownerName)
		if inv.AgentID != "" {
			aid = inv.AgentID
			if err := tx.QueryRow(`SELECT display_name FROM principals WHERE id=? AND deleted_at IS NULL`, aid).Scan(&dn); err != nil {
				return errf(404, "not_found", "agent 已被删除")
			}
			if _, err := tx.Exec(`DELETE FROM tokens WHERE principal_id=?`, aid); err != nil {
				return err
			}
			*after = append(*after, func() { s.hub.sendFinal(aid, encodeEvent(0, "replaced", obj{})) })
		} else {
			dn = cleanName(req.Name)
			if dn == "" || utf8.RuneCountInString(dn) > 32 {
				return badRequest("name 1 到 32 字")
			}
			p := req.Platform
			if !platforms[p] {
				p = "other"
			}
			aid = store.NewID("a_")
			if _, err := tx.Exec(`INSERT INTO principals(id,kind,name,display_name,owner_id,platform,created_at) VALUES(?,?,?,?,?,?,?)`,
				aid, "agent", dn, dn, inv.CreatedBy, p, now()); err != nil {
				if strings.Contains(err.Error(), "UNIQUE") {
					return errf(409, "conflict", "你的 owner 已经有一个叫 %s 的 agent，换个名字", dn)
				}
				return err
			}
			if _, err := tx.Exec(`INSERT INTO wake_configs(agent_id) VALUES(?)`, aid); err != nil {
				return err
			}
			if inv.GroupID != "" {
				var ok int
				tx.QueryRow(`SELECT 1 FROM members WHERE group_id=? AND principal_id=?`, inv.GroupID, inv.CreatedBy).Scan(&ok)
				if ok == 1 {
					if _, err := s.addMember(tx, after, inv.GroupID, aid, "member"); err != nil {
						return err
					}
					gid = inv.GroupID
				}
			}
			*after = append(*after, func() { s.hub.sendHumans(inv.CreatedBy, encodeEvent(0, "groups", obj{})) })
		}
		tok, _, err = s.newToken(tx, aid, "agent", "pair", "", "", 0)
		return err
	})
	if err != nil {
		return nil, err
	}
	res := obj{"token": tok, "agent": obj{"id": aid, "displayName": dn, "ownerName": ownerName}}
	if gid != "" {
		res["groupId"] = gid
	}
	return res, nil
}

func (s *Server) agentJSON(id string) (obj, error) {
	var dn, platform, intro, tier, notes string
	var disabled int
	var lastSeen sql.NullInt64
	err := s.db.R.QueryRow(`SELECT display_name,COALESCE(platform,''),intro,commander_tier,owner_notes,disabled,last_seen_at FROM principals WHERE id=?`, id).
		Scan(&dn, &platform, &intro, &tier, &notes, &disabled, &lastSeen)
	if err != nil {
		return nil, notFoundIf(err)
	}
	wake := obj{"kind": "listener", "url": "", "failStreak": 0, "lastError": ""}
	var kind, u, lastErr string
	var streak int
	if s.db.R.QueryRow(`SELECT kind,url,fail_streak,last_error FROM wake_configs WHERE agent_id=?`, id).Scan(&kind, &u, &streak, &lastErr) == nil {
		wake = obj{"kind": kind, "url": u, "failStreak": streak, "lastError": lastErr}
	}
	grants, err := s.grants(id)
	if err != nil {
		return nil, err
	}
	runs := []obj{}
	rows, err := s.db.R.Query(`SELECT COALESCE(group_id,''),ok,detail,created_at FROM runs WHERE agent_id=? ORDER BY id DESC LIMIT 10`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var g, d string
		var ok int
		var at int64
		rows.Scan(&g, &ok, &d, &at)
		runs = append(runs, obj{"groupId": g, "ok": ok != 0, "detail": d, "createdAt": at})
	}
	rows.Close()
	var ls any
	if lastSeen.Valid {
		ls = lastSeen.Int64
	}
	sessions, err := s.agentSessions(id)
	if err != nil {
		return nil, err
	}
	return obj{"id": id, "displayName": dn, "platform": platform, "intro": intro, "commanderTier": tier, "ownerNotes": notes,
		"disabled": disabled != 0, "online": s.hub.online(id), "lastSeenAt": ls, "wake": wake, "grants": grants, "recentRuns": runs,
		"runtime": s.ownerJSON(s.loadRuntime(id)), "sessions": sessions}, nil
}

func (s *Server) grants(agentID string) ([]obj, error) {
	rows, err := s.db.R.Query(`SELECT gr.user_id,gr.group_id,gr.created_at,COALESCE(u.display_name,''),COALESCE(u.kind,''),COALESCE(g.name,'')
		FROM grants gr LEFT JOIN principals u ON u.id=gr.user_id LEFT JOIN groups g ON g.id=gr.group_id WHERE gr.agent_id=? ORDER BY gr.created_at`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []obj{}
	for rows.Next() {
		var uid, gid, un, uk, gn string
		var at int64
		rows.Scan(&uid, &gid, &at, &un, &uk, &gn)
		if uid == "*" {
			uk = "human"
		}
		out = append(out, obj{"userId": uid, "groupId": gid, "userName": un, "userKind": uk, "groupName": gn, "createdAt": at})
	}
	return out, rows.Err()
}

func (s *Server) listAgents(c *ctx) (any, error) {
	ids, err := scanStrings(s.db.R.Query(`SELECT id FROM principals WHERE kind='agent' AND owner_id=? AND deleted_at IS NULL ORDER BY created_at`, c.a.ID))
	if err != nil {
		return nil, err
	}
	out := []obj{}
	for _, id := range ids {
		a, err := s.agentJSON(id)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return obj{"agents": out}, nil
}

func (s *Server) patchAgent(c *ctx) (any, error) {
	id := c.r.PathValue("id")
	var req struct {
		DisplayName, CommanderTier, OwnerNotes *string
		Disabled                               *bool
	}
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if err := s.ownAgent(s.db.R, c.a, id); err != nil {
		return nil, err
	}
	if req.DisplayName != nil {
		dn := cleanName(*req.DisplayName)
		if dn == "" || utf8.RuneCountInString(dn) > 32 {
			return nil, badRequest("名字 1 到 32 字")
		}
		if _, err := s.db.W.Exec(`UPDATE principals SET display_name=?, name=? WHERE id=?`, dn, dn, id); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return nil, errf(409, "conflict", "你已经有一个叫 %s 的 agent", dn)
			}
			return nil, err
		}
		if err := s.syncNicks(id, dn); err != nil {
			return nil, err
		}
	}
	if req.CommanderTier != nil {
		if *req.CommanderTier != "project" && *req.CommanderTier != "full" {
			return nil, badRequest("commanderTier 只能是 project 或 full")
		}
		if _, err := s.db.W.Exec(`UPDATE principals SET commander_tier=? WHERE id=?`, *req.CommanderTier, id); err != nil {
			return nil, err
		}
	}
	if req.OwnerNotes != nil {
		if utf8.RuneCountInString(*req.OwnerNotes) > 2000 {
			return nil, badRequest("长期说明最多 2000 字")
		}
		if _, err := s.db.W.Exec(`UPDATE principals SET owner_notes=? WHERE id=?`, *req.OwnerNotes, id); err != nil {
			return nil, err
		}
	}
	if req.Disabled != nil {
		if _, err := s.db.W.Exec(`UPDATE principals SET disabled=? WHERE id=?`, b2i(*req.Disabled), id); err != nil {
			return nil, err
		}
		if *req.Disabled {
			s.hub.drop(id)
		}
	}
	a, err := s.agentJSON(id)
	return obj{"agent": a}, err
}

func (s *Server) deleteAgent(c *ctx) (any, error) {
	id := c.r.PathValue("id")
	err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		if err := s.ownAgent(tx, c.a, id); err != nil {
			return err
		}
		gids, err := scanStrings(tx.Query(`SELECT group_id FROM members WHERE principal_id=?`, id))
		if err != nil {
			return err
		}
		for _, g := range gids {
			if err := s.removeMember(tx, after, g, id, " 已被删除"); err != nil {
				return err
			}
		}
		for _, q := range []string{`DELETE FROM tokens WHERE principal_id=?`, `DELETE FROM grants WHERE agent_id=?`, `DELETE FROM grants WHERE user_id=?`,
			`DELETE FROM wake_configs WHERE agent_id=?`, `DELETE FROM invites WHERE agent_id=?`,
			`UPDATE principals SET deleted_at=unixepoch()*1000, disabled=1 WHERE id=?`} {
			if _, err := tx.Exec(q, id); err != nil {
				return err
			}
		}
		*after = append(*after, func() { s.hub.sendFinal(id, encodeEvent(0, "deleted", obj{})) })
		return nil
	})
	if err != nil {
		return nil, err
	}
	return obj{}, nil
}

func (s *Server) resetToken(c *ctx) (any, error) {
	id := c.r.PathValue("id")
	if err := s.ownAgent(s.db.R, c.a, id); err != nil {
		return nil, err
	}
	s.db.W.Exec(`DELETE FROM invites WHERE kind='pair' AND agent_id=?`, id)
	r, err := s.issuePairCode(c.a.ID, "", id)
	if err != nil {
		return nil, err
	}
	return obj{"code": r["code"], "expiresAt": r["expiresAt"], "instruction": r["instruction"]}, nil
}

func (s *Server) changeGrant(c *ctx) (any, error) {
	id := c.r.PathValue("id")
	var req struct{ UserID, GroupID string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if err := s.ownAgent(s.db.R, c.a, id); err != nil {
		return nil, err
	}
	if c.r.Method == "DELETE" {
		if _, err := s.db.W.Exec(`DELETE FROM grants WHERE agent_id=? AND user_id=? AND group_id=?`, id, req.UserID, req.GroupID); err != nil {
			return nil, err
		}
	} else {
		if req.UserID == "" || req.GroupID == "" {
			return nil, badRequest("需要 userId 和 groupId")
		}
		if req.UserID == "*" && req.GroupID == "*" {
			return nil, badRequest("不能授权所有群的所有人类")
		}
		if req.UserID != "*" {
			// A human, or any active agent (its owner's consent is not needed: B's owner decides).
			var kind string
			if s.db.R.QueryRow(`SELECT kind FROM principals WHERE id=? AND deleted_at IS NULL`, req.UserID).Scan(&kind) != nil || (kind != "human" && kind != "agent") {
				return nil, badRequest("只能授权人类或 agent")
			}
			if req.UserID == id {
				return nil, badRequest("不能授权 agent 指挥它自己")
			}
			// Agent commanders are scoped to one group, so a grant made in one group never carries over.
			if kind == "agent" && req.GroupID == "*" {
				return nil, badRequest("授权 agent 时必须选一个具体的群")
			}
		}
		if req.GroupID != "*" {
			if _, err := s.role(s.db.R, c.a, req.GroupID); err != nil {
				return nil, badRequest("你不在这个群里")
			}
		}
		if _, err := s.db.W.Exec(`INSERT OR IGNORE INTO grants(agent_id,user_id,group_id,created_at) VALUES(?,?,?,?)`, id, req.UserID, req.GroupID, now()); err != nil {
			return nil, err
		}
	}
	g, err := s.grants(id)
	return obj{"grants": g}, err
}

func (s *Server) setWake(c *ctx) (any, error) {
	id := c.r.PathValue("id")
	var req struct{ Kind, URL string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if err := s.ownAgent(s.db.R, c.a, id); err != nil {
		return nil, err
	}
	secret := ""
	switch req.Kind {
	case "listener", "none":
		req.URL = ""
	case "webhook":
		u, err := url.Parse(req.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, badRequest("webhook 地址必须是 http(s) URL")
		}
		secret = "whs_" + store.RandString(20)
	default:
		return nil, badRequest("kind 只能是 listener、webhook 或 none")
	}
	if _, err := s.db.W.Exec(`INSERT INTO wake_configs(agent_id,kind,url,secret) VALUES(?,?,?,?)
		ON CONFLICT(agent_id) DO UPDATE SET kind=excluded.kind,url=excluded.url,secret=CASE WHEN excluded.kind='webhook' THEN excluded.secret ELSE '' END,fail_streak=0,last_error=''`,
		id, req.Kind, req.URL, secret); err != nil {
		return nil, err
	}
	a, err := s.agentJSON(id)
	if err != nil {
		return nil, err
	}
	res := obj{"wake": a["wake"]}
	if secret != "" {
		res["secret"] = secret
	}
	return res, nil
}

// ---- agent self ----

func (s *Server) createRunToken(c *ctx) (any, error) {
	var req struct{ GroupID, WakeID, Session, Host, MaxTier string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if req.MaxTier != "" && tierRank(req.MaxTier) < 0 {
		return nil, badRequest("maxTier 只能是 restricted、project 或 full")
	}
	if _, err := s.role(s.db.R, c.a, req.GroupID); err != nil {
		return nil, err
	}
	commander := false
	if req.WakeID != "" {
		var cmd int
		if err := s.db.R.QueryRow(`SELECT commander FROM wakes WHERE id=? AND agent_id=? AND group_id=?`, req.WakeID, c.a.ID, req.GroupID).Scan(&cmd); err != nil {
			return nil, errf(404, "not_found", "wake 不存在或已过期")
		}
		commander = cmd != 0
	} else {
		// Inbox-discovered job (e.g. after a reconnect): commander if an unread
		// mention/reply/dm of this agent was written by someone who can command it.
		s.db.R.QueryRow(`SELECT EXISTS(SELECT 1 FROM mentions mn JOIN members me ON me.group_id=mn.group_id AND me.principal_id=mn.target_id
			JOIN messages m ON m.group_id=mn.group_id AND m.seq=mn.seq
			WHERE mn.target_id=?1 AND mn.group_id=?2 AND mn.seq>me.last_read_seq AND m.recalled_at IS NULL AND m.author_id IS NOT NULL
			AND `+viaCommandsSQL("m")+` AND `+strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(canCommandSQL, "?1", "m.author_id"), "?2", "?1"), "?3", "?2")+`)`,
			c.a.ID, req.GroupID).Scan(&commander)
	}
	tier := "restricted"
	scope := req.GroupID
	if commander {
		s.db.R.QueryRow(`SELECT commander_tier FROM principals WHERE id=?`, c.a.ID).Scan(&tier)
	}
	// The listener may run the job at a lower tier (local maxTier, root, no commander trigger):
	// the token and its provenance then carry that tier, never a higher one.
	if req.MaxTier != "" && tierRank(req.MaxTier) < tierRank(tier) {
		tier = req.MaxTier
	}
	if tier != "restricted" {
		scope = "*"
	}
	tok, exp, err := s.newToken(s.db.W, c.a.ID, "run", req.GroupID, scope, req.WakeID, runTTL)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.W.Exec(`UPDATE tokens SET run_session=?, run_host=?, run_tier=? WHERE hash=?`,
		nullStr(cleanSession(req.Session)), nullStr(cleanLabel(req.Host, 64)), tier, store.Hash(tok)); err != nil {
		return nil, err
	}
	return obj{"token": tok, "expiresAt": exp, "commander": commander, "tier": tier}, nil
}

func tierRank(t string) int {
	switch t {
	case "restricted":
		return 0
	case "project":
		return 1
	case "full":
		return 2
	}
	return -1
}

func (s *Server) deleteRunToken(c *ctx) (any, error) {
	if c.a.TokenKind != "run" {
		return nil, badRequest("请用 run token 本身调用")
	}
	_, err := s.db.W.Exec(`DELETE FROM tokens WHERE hash=?`, c.a.TokenHash)
	return obj{}, err
}

func (s *Server) deleteToken(c *ctx) (any, error) {
	_, err := s.db.W.Exec(`DELETE FROM tokens WHERE hash=?`, c.a.TokenHash)
	return obj{}, err
}

func (s *Server) reportRun(c *ctx) (any, error) {
	var req struct {
		GroupID string
		OK      bool
		Detail  string
	}
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if req.GroupID != "" && !c.a.canGroup(req.GroupID) {
		return nil, errForbidden()
	}
	_, err := s.db.W.Exec(`INSERT INTO runs(agent_id,group_id,ok,detail,created_at) VALUES(?,?,?,?,?)`,
		c.a.ID, nullStr(req.GroupID), b2i(req.OK), truncRunes(req.Detail, 1000), now())
	s.finishRun(req.GroupID, c.a.ID)
	return obj{}, err
}

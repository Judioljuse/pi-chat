package server

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"pichat/internal/store"
)

type memberJSON struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Nick        string `json:"nick"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	OwnerID     string `json:"ownerId,omitempty"`
	OwnerName   string `json:"ownerName,omitempty"`
	Platform    string `json:"platform,omitempty"`
	Online      bool   `json:"online"`
	State       string `json:"state,omitempty"`       // agent: offline / idle / running / attached
	MainSession string `json:"mainSession,omitempty"` // agent: the listener's current main session
	Notice      string `json:"notice,omitempty"`      // agent: listener runtime notice (external use, fork)
	LastReadSeq int64  `json:"lastReadSeq"`
	joinedAt    int64
}

type groupRow struct {
	ID, Kind, Name, Topic, OwnerID string
	LastSeq, CreatedAt             int64
}

func (s *Server) loadGroup(q querier, gid string) (*groupRow, error) {
	g := &groupRow{}
	var owner sql.NullString
	err := q.QueryRow(`SELECT id,kind,name,topic,owner_id,last_seq,created_at FROM groups WHERE id=?`, gid).
		Scan(&g.ID, &g.Kind, &g.Name, &g.Topic, &owner, &g.LastSeq, &g.CreatedAt)
	g.OwnerID = owner.String
	return g, notFoundIf(err)
}

func (s *Server) members(q querier, gid string) ([]memberJSON, error) {
	rows, err := q.Query(`SELECT p.id,p.kind,m.nick,p.display_name,m.role,COALESCE(p.owner_id,''),COALESCE(o.display_name,''),COALESCE(p.platform,''),m.last_read_seq,m.joined_at,
		COALESCE(p.runtime_state,''),COALESCE(p.main_session,''),COALESCE(p.runtime_notice,'')
		FROM members m JOIN principals p ON p.id=m.principal_id LEFT JOIN principals o ON o.id=p.owner_id
		WHERE m.group_id=? ORDER BY m.joined_at, p.id`, gid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []memberJSON{}
	for rows.Next() {
		var m memberJSON
		var state string
		if err := rows.Scan(&m.ID, &m.Kind, &m.Nick, &m.DisplayName, &m.Role, &m.OwnerID, &m.OwnerName, &m.Platform, &m.LastReadSeq, &m.joinedAt, &state, &m.MainSession, &m.Notice); err != nil {
			return nil, err
		}
		if m.Kind == "agent" {
			m.Online = s.hub.online(m.ID)
			m.State = s.effectiveState(m.ID, state)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Server) memberIDs(q querier, gid string) ([]string, error) {
	return scanStrings(q.Query(`SELECT principal_id FROM members WHERE group_id=?`, gid))
}

// role returns the caller's role in gid, or a 404/403 if not a member or out of token scope.
func (s *Server) role(q querier, a *actor, gid string) (string, error) {
	if !a.canGroup(gid) {
		return "", errf(403, "forbidden", "这个 token 只能访问它所属的群")
	}
	var role string
	err := q.QueryRow(`SELECT role FROM members WHERE group_id=? AND principal_id=?`, gid, a.ID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errf(404, "not_found", "群不存在或你不是成员")
	}
	return role, err
}

func isManager(role string) bool { return role == "owner" || role == "admin" }

func (s *Server) groupJSON(q querier, a *actor, gid string) (obj, error) {
	g, err := s.loadGroup(q, gid)
	if err != nil {
		return nil, err
	}
	ms, err := s.members(q, gid)
	if err != nil {
		return nil, err
	}
	myRole := ""
	name := g.Name
	for i := range ms {
		// Only the agent's owner sees the full runtime notice.
		if ms[i].Notice != "" && ms[i].OwnerID != a.ID {
			ms[i].Notice = publicNotice(ms[i].Notice)
		}
	}
	for _, m := range ms {
		if m.ID == a.ID {
			myRole = m.Role
		} else if g.Kind == "dm" {
			name = m.DisplayName
		}
	}
	hop, wakes := s.groupLimits(q, gid)
	return obj{"id": g.ID, "kind": g.Kind, "name": name, "topic": g.Topic, "ownerId": g.OwnerID, "lastSeq": g.LastSeq,
		"myRole": myRole, "members": ms, "working": s.work.list(gid), "createdAt": g.CreatedAt, "openTasks": s.openTaskCount(q, gid),
		"limits": obj{"hopLimit": hop, "agentWakesPerHour": wakes, "default": hop == hopLimit && wakes == agentWakesPerHour}}, nil
}

// addMember inserts pid into gid with a unique nick; existing members are returned unchanged.
func (s *Server) addMember(tx *sql.Tx, after *[]func(), gid, pid, role string) (*memberJSON, error) {
	return s.addMemberBy(tx, after, gid, pid, role, "")
}

// addMemberBy is addMember with the inviter's nick for the join system message
// ("<by> 把 <nick> 拉进了群"); an empty by gives "<nick> 加入了群".
func (s *Server) addMemberBy(tx *sql.Tx, after *[]func(), gid, pid, role, by string) (*memberJSON, error) {
	var exists int
	tx.QueryRow(`SELECT 1 FROM members WHERE group_id=? AND principal_id=?`, gid, pid).Scan(&exists)
	if exists == 0 {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM members WHERE group_id=?`, gid).Scan(&n); err != nil {
			return nil, err
		}
		if n >= maxMembers {
			return nil, errf(409, "conflict", "群成员已满（%d 人）", maxMembers)
		}
		var dn string
		if err := tx.QueryRow(`SELECT display_name FROM principals WHERE id=?`, pid).Scan(&dn); err != nil {
			return nil, notFoundIf(err)
		}
		nicks, err := scanStrings(tx.Query(`SELECT lower(nick) FROM members WHERE group_id=?`, gid))
		if err != nil {
			return nil, err
		}
		nick := uniqueNick(dn, nicks)
		var lastSeq int64
		tx.QueryRow(`SELECT last_seq FROM groups WHERE id=?`, gid).Scan(&lastSeq)
		if _, err := tx.Exec(`INSERT INTO members(group_id,principal_id,nick,role,last_read_seq,joined_at) VALUES(?,?,?,?,?,?)`,
			gid, pid, nick, role, lastSeq, now()); err != nil {
			return nil, err
		}
		var kind string
		tx.QueryRow(`SELECT kind FROM groups WHERE id=?`, gid).Scan(&kind)
		if kind == "group" {
			text := nick + " 加入了群"
			if by != "" {
				text = by + " 把 " + nick + " 拉进了群"
			}
			if err := s.systemMessage(tx, after, gid, text); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(`UPDATE members SET last_read_seq=(SELECT last_seq FROM groups WHERE id=?1) WHERE group_id=?1 AND principal_id=?2`, gid, pid); err != nil {
				return nil, err
			}
		}
		s.groupChanged(tx, after, gid)
		*after = append(*after, func() { s.hub.sendHumans(pid, encodeEvent(0, "groups", obj{})) })
	}
	ms, err := s.members(tx, gid)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		if m.ID == pid {
			return &m, nil
		}
	}
	return nil, errNotFound()
}

// cleanName strips characters that could forge mentions or prompt markers
// ("@", brackets, "#", control characters and line breaks) from a display name or nick.
func cleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case strings.ContainsRune("@[]【】#", r), unicode.IsControl(r), r == '\u2028', r == '\u2029':
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func uniqueNick(base string, taken []string) string {
	base = cleanName(base)
	if base == "" {
		base = "member"
	}
	set := map[string]bool{}
	for _, t := range taken {
		set[t] = true
	}
	nick := base
	for i := 2; set[strings.ToLower(nick)] || strings.EqualFold(nick, "all"); i++ {
		nick = fmt.Sprintf("%s%d", base, i)
	}
	return nick
}

// syncNicks gives pid the nick dn in every group it is in (made unique per group, keeping the
// current nick when it already fits), so a rename shows up in all groups' member lists and @s.
func (s *Server) syncNicks(pid, dn string) error {
	return s.tx(func(tx *sql.Tx, after *[]func()) error {
		gids, err := scanStrings(tx.Query(`SELECT group_id FROM members WHERE principal_id=?`, pid))
		if err != nil {
			return err
		}
		for _, gid := range gids {
			var cur string
			tx.QueryRow(`SELECT nick FROM members WHERE group_id=? AND principal_id=?`, gid, pid).Scan(&cur)
			others, err := scanStrings(tx.Query(`SELECT lower(nick) FROM members WHERE group_id=? AND principal_id<>?`, gid, pid))
			if err != nil {
				return err
			}
			nick := uniqueNick(dn, others)
			if nick == cur {
				continue
			}
			if _, err := tx.Exec(`UPDATE members SET nick=? WHERE group_id=? AND principal_id=?`, nick, gid, pid); err != nil {
				return err
			}
			s.groupChanged(tx, after, gid)
		}
		return nil
	})
}

// removeMember removes pid (and, for humans, their agents) from gid.
func (s *Server) removeMember(tx *sql.Tx, after *[]func(), gid, pid, why string) error {
	ids := []string{pid}
	agents, err := scanStrings(tx.Query(`SELECT m.principal_id FROM members m JOIN principals p ON p.id=m.principal_id WHERE m.group_id=? AND p.owner_id=?`, gid, pid))
	if err != nil {
		return err
	}
	ids = append(ids, agents...)
	var nicks []string
	for _, id := range ids {
		var nick string
		if err := tx.QueryRow(`DELETE FROM members WHERE group_id=? AND principal_id=? RETURNING nick`, gid, id).Scan(&nick); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
		nicks = append(nicks, nick)
		if err := s.unassignTasks(tx, after, gid, id); err != nil {
			return err
		}
		*after = append(*after, func() { s.hub.sendHumans(id, encodeEvent(0, "groups", obj{})) })
	}
	if len(nicks) > 0 && why != "" {
		if err := s.systemMessage(tx, after, gid, strings.Join(nicks, "、")+why); err != nil {
			return err
		}
	}
	s.groupChanged(tx, after, gid)
	return nil
}

// groupChanged records a `group` event and pushes it to human members after commit.
func (s *Server) groupChanged(tx *sql.Tx, after *[]func(), gid string) {
	s.emit(tx, after, gid, "group", obj{"groupId": gid})
}

func (s *Server) listGroups(c *ctx) (any, error) {
	rows, err := s.db.R.Query(`SELECT g.id,g.kind,g.name,g.topic,g.last_seq,m.last_read_seq,g.created_at,
		(SELECT COUNT(*) FROM messages x WHERE x.group_id=g.id AND x.seq>m.last_read_seq AND (x.author_id IS NULL OR x.author_id<>?)),
		(SELECT COUNT(*) FROM mentions t WHERE t.target_id=? AND t.group_id=g.id AND t.seq>m.last_read_seq AND t.reason IN ('mention','dm','reply'))
		FROM members m JOIN groups g ON g.id=m.group_id WHERE m.principal_id=?`, c.a.ID, c.a.ID, c.a.ID)
	if err != nil {
		return nil, err
	}
	type row struct {
		sum      obj
		id, kind string
		lastSeq  int64
		sortKey  int64
	}
	var list []row
	for rows.Next() {
		var id, kind, name, topic string
		var lastSeq, lastRead, created, unread, ment int64
		if err := rows.Scan(&id, &kind, &name, &topic, &lastSeq, &lastRead, &created, &unread, &ment); err != nil {
			rows.Close()
			return nil, err
		}
		if !c.a.canGroup(id) {
			continue
		}
		list = append(list, row{sum: obj{"id": id, "kind": kind, "name": name, "topic": topic, "lastSeq": lastSeq,
			"unread": unread, "mentionsMe": ment, "working": s.work.ids(id), "lastMessage": nil}, id: id, kind: kind, lastSeq: lastSeq, sortKey: created})
	}
	rows.Close()
	out := make([]obj, 0, len(list))
	for i := range list {
		r := &list[i]
		if r.lastSeq > 0 {
			var author, kind sql.NullString
			var body string
			var at int64
			var recalled sql.NullInt64
			err := s.db.R.QueryRow(`SELECT m.author_id,m.kind,m.body,m.created_at,m.recalled_at FROM messages m WHERE m.group_id=? AND m.seq=?`, r.id, r.lastSeq).
				Scan(&author, &kind, &body, &at, &recalled)
			if err == nil {
				name := "系统"
				if author.Valid {
					name = s.principalName(s.db.R, r.id, author.String)
				}
				text := body
				if recalled.Valid {
					text = "[已撤回]"
				} else if text == "" {
					text = "[附件]"
				}
				r.sum["lastMessage"] = obj{"seq": r.lastSeq, "authorName": name, "text": truncRunes(text, 80), "createdAt": at}
				r.sortKey = at
			}
		}
		if r.kind == "dm" {
			var pid, dn, kind, platform string
			if err := s.db.R.QueryRow(`SELECT p.id,p.display_name,p.kind,COALESCE(p.platform,'') FROM members m JOIN principals p ON p.id=m.principal_id WHERE m.group_id=? AND m.principal_id<>? LIMIT 1`, r.id, c.a.ID).Scan(&pid, &dn, &kind, &platform); err == nil {
				r.sum["name"] = dn
				peer := obj{"id": pid, "displayName": dn, "kind": kind}
				if platform != "" {
					peer["platform"] = platform
				}
				r.sum["peer"] = peer
			}
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].sortKey > list[j].sortKey })
	for _, r := range list {
		out = append(out, r.sum)
	}
	return obj{"groups": out}, nil
}

func (s *Server) principalName(q querier, gid, pid string) string {
	var name string
	if q.QueryRow(`SELECT nick FROM members WHERE group_id=? AND principal_id=?`, gid, pid).Scan(&name) != nil {
		q.QueryRow(`SELECT display_name FROM principals WHERE id=?`, pid).Scan(&name)
	}
	return name
}

func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

func checkName(name string, max int, what string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > max {
		return "", badRequest("%s 1 到 %d 字", what, max)
	}
	return name, nil
}

func (s *Server) createGroup(c *ctx) (any, error) {
	var req struct{ Name, Topic string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	name, err := checkName(req.Name, 64, "群名")
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(req.Topic) > 500 {
		return nil, badRequest("群简介最多 500 字")
	}
	owner := c.a.ID
	if c.a.isAgent() {
		owner = c.a.OwnerID
	}
	gid := store.NewID("g_")
	err = s.tx(func(tx *sql.Tx, after *[]func()) error {
		if _, err := tx.Exec(`INSERT INTO groups(id,kind,name,topic,owner_id,created_at) VALUES(?,?,?,?,?,?)`, gid, "group", name, req.Topic, owner, now()); err != nil {
			return err
		}
		if _, err := s.addMember(tx, after, gid, owner, "owner"); err != nil {
			return err
		}
		if c.a.isAgent() {
			_, err := s.addMember(tx, after, gid, c.a.ID, "member")
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	g, err := s.groupJSON(s.db.R, c.a, gid)
	return obj{"group": g}, err
}

func (s *Server) getGroup(c *ctx) (any, error) {
	gid := c.r.PathValue("id")
	if _, err := s.role(s.db.R, c.a, gid); err != nil {
		return nil, err
	}
	g, err := s.groupJSON(s.db.R, c.a, gid)
	return obj{"group": g}, err
}

func (s *Server) patchGroup(c *ctx) (any, error) {
	gid := c.r.PathValue("id")
	var req struct {
		Name, Topic                 *string
		HopLimit, AgentWakesPerHour *int
	}
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		role, err := s.role(tx, c.a, gid)
		if err != nil {
			return err
		}
		g, err := s.loadGroup(tx, gid)
		if err != nil {
			return err
		}
		if !isManager(role) || g.Kind == "dm" {
			return errForbidden()
		}
		if req.Name != nil {
			n, err := checkName(*req.Name, 64, "群名")
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE groups SET name=? WHERE id=?`, n, gid); err != nil {
				return err
			}
		}
		if req.Topic != nil {
			if utf8.RuneCountInString(*req.Topic) > 500 {
				return badRequest("群简介最多 500 字")
			}
			if _, err := tx.Exec(`UPDATE groups SET topic=? WHERE id=?`, *req.Topic, gid); err != nil {
				return err
			}
		}
		// 0 resets to the server default.
		if req.HopLimit != nil {
			v := *req.HopLimit
			if v < 0 || v > maxHopLimit {
				return badRequest("接力上限是 1 到 %d 跳（0 恢复默认 %d）", maxHopLimit, hopLimit)
			}
			if _, err := tx.Exec(`UPDATE groups SET hop_limit=? WHERE id=?`, nullPositive(v), gid); err != nil {
				return err
			}
		}
		if req.AgentWakesPerHour != nil {
			v := *req.AgentWakesPerHour
			if v < 0 || v > maxAgentWakesHour {
				return badRequest("每小时唤醒次数是 1 到 %d（0 恢复默认 %d）", maxAgentWakesHour, agentWakesPerHour)
			}
			if _, err := tx.Exec(`UPDATE groups SET agent_wakes_per_hour=? WHERE id=?`, nullPositive(v), gid); err != nil {
				return err
			}
		}
		s.groupChanged(tx, after, gid)
		return nil
	})
	if err != nil {
		return nil, err
	}
	g, err := s.groupJSON(s.db.R, c.a, gid)
	return obj{"group": g}, err
}

func (s *Server) deleteGroup(c *ctx) (any, error) {
	gid := c.r.PathValue("id")
	err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		role, err := s.role(tx, c.a, gid)
		if err != nil {
			return err
		}
		if role != "owner" {
			return errForbidden()
		}
		return s.dissolve(tx, after, gid)
	})
	if err != nil {
		return nil, err
	}
	return obj{}, nil
}

func (s *Server) dissolve(tx *sql.Tx, after *[]func(), gid string) error {
	ids, err := s.memberIDs(tx, gid)
	if err != nil {
		return err
	}
	for _, q := range []string{`DELETE FROM mentions WHERE group_id=?`, `DELETE FROM files WHERE group_id=?`,
		`DELETE FROM invites WHERE group_id=?`, `DELETE FROM grants WHERE group_id=?`, `DELETE FROM groups WHERE id=?`} {
		if _, err := tx.Exec(q, gid); err != nil {
			return err
		}
	}
	*after = append(*after, func() {
		for _, id := range ids {
			s.hub.sendHumans(id, encodeEvent(0, "groups", obj{}))
		}
	})
	return nil
}

func (s *Server) groupInvite(c *ctx) (any, error) {
	gid := c.r.PathValue("id")
	code := store.RandString(15)
	err := s.tx(func(tx *sql.Tx, _ *[]func()) error {
		role, err := s.role(tx, c.a, gid)
		if err != nil {
			return err
		}
		g, err := s.loadGroup(tx, gid)
		if err != nil {
			return err
		}
		if !isManager(role) || g.Kind == "dm" {
			return errForbidden()
		}
		if _, err := tx.Exec(`DELETE FROM invites WHERE kind='group' AND group_id=?`, gid); err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO invites(hash,kind,group_id,created_by,max_uses,expires_at,created_at) VALUES(?,?,?,?,20,?,?)`,
			store.Hash(code), "group", gid, c.a.ID, now()+7*24*time.Hour.Milliseconds(), now())
		return err
	})
	if err != nil {
		return nil, err
	}
	return obj{"code": code, "url": s.cfg.PublicURL + "/#/redeem/" + code}, nil
}

// addGroupMember handles POST /groups/{id}/members: {agentId} pulls in one of
// the caller's own agents; {userId} lets an owner/admin add an existing human.
func (s *Server) addGroupMember(c *ctx) (any, error) {
	gid := c.r.PathValue("id")
	var req struct{ AgentID, UserID string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if (req.AgentID == "") == (req.UserID == "") {
		return nil, badRequest("agentId 和 userId 必须二选一")
	}
	var m *memberJSON
	err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		role, err := s.role(tx, c.a, gid)
		if err != nil {
			return err
		}
		g, err := s.loadGroup(tx, gid)
		if err != nil {
			return err
		}
		if req.AgentID != "" {
			if g.Kind == "dm" {
				return badRequest("不能把 agent 拉进私聊")
			}
			if err := s.ownAgent(tx, c.a, req.AgentID); err != nil {
				return err
			}
			m, err = s.addMember(tx, after, gid, req.AgentID, "member")
			return err
		}
		if g.Kind == "dm" {
			return badRequest("不能把人拉进私聊")
		}
		if !isManager(role) {
			return errf(403, "forbidden", "只有群主和管理员能拉人进群")
		}
		var disabled int
		if err := tx.QueryRow(`SELECT disabled FROM principals WHERE id=? AND kind='human' AND deleted_at IS NULL`, req.UserID).Scan(&disabled); err != nil || disabled != 0 {
			return errf(404, "not_found", "没有这个用户")
		}
		var exists int
		tx.QueryRow(`SELECT 1 FROM members WHERE group_id=? AND principal_id=?`, gid, req.UserID).Scan(&exists)
		if exists != 0 {
			return errf(409, "conflict", "已经在群里")
		}
		m, err = s.addMemberBy(tx, after, gid, req.UserID, "member", s.principalName(tx, gid, c.a.ID))
		return err
	})
	if err != nil {
		return nil, err
	}
	return obj{"member": m}, nil
}

// listUsers is the directory of active humans; with ?groupId= (caller must be
// a member) each entry also says whether that user is in the group.
func (s *Server) listUsers(c *ctx) (any, error) {
	gid := c.r.URL.Query().Get("groupId")
	if gid != "" {
		if _, err := s.role(s.db.R, c.a, gid); err != nil {
			return nil, err
		}
	}
	rows, err := s.db.R.Query(`SELECT p.id,p.name,p.display_name,
		EXISTS(SELECT 1 FROM members m WHERE m.group_id=? AND m.principal_id=p.id)
		FROM principals p WHERE p.kind='human' AND p.deleted_at IS NULL AND p.disabled=0
		ORDER BY lower(p.display_name), p.name`, gid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []obj{}
	for rows.Next() {
		var id, name, dn string
		var in int
		if err := rows.Scan(&id, &name, &dn, &in); err != nil {
			return nil, err
		}
		u := obj{"id": id, "name": name, "displayName": dn}
		if gid != "" {
			u["inGroup"] = in != 0
		}
		users = append(users, u)
	}
	return obj{"users": users}, rows.Err()
}

func (s *Server) ownAgent(q querier, a *actor, agentID string) error {
	var owner string
	var disabled int
	err := q.QueryRow(`SELECT COALESCE(owner_id,''),disabled FROM principals WHERE id=? AND kind='agent' AND deleted_at IS NULL`, agentID).Scan(&owner, &disabled)
	if err != nil || owner != a.ID {
		return errf(404, "not_found", "没有这个 agent")
	}
	return nil
}

func (s *Server) patchMember(c *ctx) (any, error) {
	gid, pid := c.r.PathValue("id"), c.r.PathValue("pid")
	var req struct{ Role string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if req.Role != "admin" && req.Role != "member" {
		return nil, badRequest("role 只能是 admin 或 member")
	}
	var m *memberJSON
	err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		role, err := s.role(tx, c.a, gid)
		if err != nil {
			return err
		}
		if role != "owner" || pid == c.a.ID {
			return errForbidden()
		}
		var kind string
		if err := tx.QueryRow(`SELECT p.kind FROM members m JOIN principals p ON p.id=m.principal_id WHERE m.group_id=? AND m.principal_id=?`, gid, pid).Scan(&kind); err != nil {
			return notFoundIf(err)
		}
		if kind != "human" {
			return badRequest("只有人类可以当管理员")
		}
		if _, err := tx.Exec(`UPDATE members SET role=? WHERE group_id=? AND principal_id=?`, req.Role, gid, pid); err != nil {
			return err
		}
		s.groupChanged(tx, after, gid)
		ms, err := s.members(tx, gid)
		for _, x := range ms {
			if x.ID == pid {
				m = &x
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return obj{"member": m}, nil
}

func (s *Server) deleteMember(c *ctx) (any, error) {
	gid, pid := c.r.PathValue("id"), c.r.PathValue("pid")
	err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		role, err := s.role(tx, c.a, gid)
		if err != nil {
			return err
		}
		g, err := s.loadGroup(tx, gid)
		if err != nil {
			return err
		}
		var trole, tkind, towner string
		err = tx.QueryRow(`SELECT m.role,p.kind,COALESCE(p.owner_id,'') FROM members m JOIN principals p ON p.id=m.principal_id WHERE m.group_id=? AND m.principal_id=?`, gid, pid).Scan(&trole, &tkind, &towner)
		if err != nil {
			return notFoundIf(err)
		}
		if g.Kind == "dm" {
			if pid != c.a.ID {
				return errForbidden()
			}
			return s.dissolve(tx, after, gid)
		}
		switch {
		case pid == c.a.ID: // leave
			if role == "owner" {
				var next string
				err := tx.QueryRow(`SELECT m.principal_id FROM members m JOIN principals p ON p.id=m.principal_id
					WHERE m.group_id=? AND p.kind='human' AND m.principal_id<>? ORDER BY m.joined_at LIMIT 1`, gid, pid).Scan(&next)
				if errors.Is(err, sql.ErrNoRows) {
					return s.dissolve(tx, after, gid)
				}
				if err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE members SET role='owner' WHERE group_id=? AND principal_id=?`, gid, next); err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE groups SET owner_id=? WHERE id=?`, next, gid); err != nil {
					return err
				}
			}
			return s.removeMember(tx, after, gid, pid, " 退出了群")
		case tkind == "agent" && towner == c.a.ID: // owner removes own agent
			return s.removeMember(tx, after, gid, pid, " 被移出了群")
		case c.a.isHuman() && (role == "owner" || (role == "admin" && trole == "member")):
			return s.removeMember(tx, after, gid, pid, " 被移出了群")
		}
		return errForbidden()
	})
	if err != nil {
		return nil, err
	}
	return obj{}, nil
}

func (s *Server) openChat(c *ctx) (any, error) {
	var req struct{ With string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if req.With == "" || req.With == c.a.ID {
		return nil, badRequest("with 无效")
	}
	ids := []string{c.a.ID, req.With}
	sort.Strings(ids)
	key := strings.Join(ids, "|")
	var gid string
	err := s.tx(func(tx *sql.Tx, after *[]func()) error {
		if tx.QueryRow(`SELECT id FROM groups WHERE dm_key=?`, key).Scan(&gid) == nil {
			return nil
		}
		var disabled int
		if err := tx.QueryRow(`SELECT disabled FROM principals WHERE id=? AND deleted_at IS NULL`, req.With).Scan(&disabled); err != nil || disabled != 0 {
			return errf(404, "not_found", "没有这个人")
		}
		ok, err := s.canDM(tx, c.a, req.With)
		if err != nil {
			return err
		}
		if !ok {
			return errf(403, "forbidden", "你们没有共同的群，不能私聊")
		}
		gid = store.NewID("g_")
		if _, err := tx.Exec(`INSERT INTO groups(id,kind,dm_key,created_at) VALUES(?,?,?,?)`, gid, "dm", key, now()); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := s.addMember(tx, after, gid, id, "member"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	g, err := s.groupJSON(s.db.R, c.a, gid)
	return obj{"group": g}, err
}

func nullPositive(v int) any {
	if v <= 0 {
		return nil
	}
	return v
}

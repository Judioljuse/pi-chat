package server

import "database/sql"

// dmPredicate is the rule for who caller ?1 may open a DM with, as a condition on principals p.
// ?1 caller id, ?2 1 if the caller is human, ?3 the caller's owner (agents; ” for humans).
// Allowed: a human's own agents; an agent's owner; agents with the same owner; anyone sharing a group.
// Deleted and disabled principals and the caller itself never match.
const dmPredicate = `p.deleted_at IS NULL AND p.disabled=0 AND p.id<>?1 AND (
	(?2=1 AND p.kind='agent' AND p.owner_id=?1)
	OR (?2=0 AND ?3<>'' AND (p.id=?3 OR (p.kind='agent' AND p.owner_id=?3)))
	OR EXISTS (SELECT 1 FROM members a JOIN members b ON b.group_id=a.group_id JOIN groups g ON g.id=a.group_id
		WHERE g.kind='group' AND a.principal_id=?1 AND b.principal_id=p.id))`

func dmArgs(a *actor) []any {
	h := 0
	if a.isHuman() {
		h = 1
	}
	return []any{a.ID, h, a.OwnerID}
}

// canDM reports whether a may open a DM with principal with (see dmPredicate).
func (s *Server) canDM(q querier, a *actor, with string) (bool, error) {
	var n int
	err := q.QueryRow(`SELECT COUNT(*) FROM principals p WHERE p.id=?4 AND `+dmPredicate, append(dmArgs(a), with)...).Scan(&n)
	return n > 0, err
}

// listContacts is GET /contacts: everyone the caller may open a DM with, humans first, then by name.
func (s *Server) listContacts(c *ctx) (any, error) {
	rows, err := s.db.R.Query(`SELECT p.id,p.kind,p.name,p.display_name,COALESCE(p.platform,''),COALESCE(p.owner_id,''),COALESCE(o.display_name,''),COALESCE(p.runtime_state,'')
		FROM principals p LEFT JOIN principals o ON o.id=p.owner_id WHERE `+dmPredicate+`
		ORDER BY CASE p.kind WHEN 'human' THEN 0 ELSE 1 END, p.display_name COLLATE NOCASE, p.id`, dmArgs(c.a)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []obj{}
	for rows.Next() {
		var id, kind, name, dn, platform, owner, ownerName, state string
		if err := rows.Scan(&id, &kind, &name, &dn, &platform, &owner, &ownerName, &state); err != nil {
			return nil, err
		}
		x := obj{"id": id, "kind": kind, "displayName": dn, "platform": nullable(platform), "ownerId": nullable(owner), "ownerName": ownerName, "online": false}
		if kind == "human" {
			x["name"] = name
		} else {
			x["online"] = s.hub.online(id)
			x["state"] = s.effectiveState(id, state)
		}
		out = append(out, x)
	}
	return obj{"contacts": out}, rows.Err()
}

// getPrincipal is GET /principals/{id}: a profile card. Visible to the principal itself, its owner, an
// agent's view of its own owner and anyone sharing a group or DM with it; otherwise 404. Host, main session and work dirs go only to
// the agent's owner; others see the public form of the runtime notice.
func (s *Server) getPrincipal(c *ctx) (any, error) {
	id := c.r.PathValue("id")
	var kind, name, dn, platform, owner, ownerName, intro string
	var disabled int
	var lastSeen sql.NullInt64
	err := s.db.R.QueryRow(`SELECT p.kind,p.name,p.display_name,COALESCE(p.platform,''),COALESCE(p.owner_id,''),COALESCE(o.display_name,''),p.intro,p.disabled,p.last_seen_at
		FROM principals p LEFT JOIN principals o ON o.id=p.owner_id WHERE p.id=? AND p.deleted_at IS NULL`, id).
		Scan(&kind, &name, &dn, &platform, &owner, &ownerName, &intro, &disabled, &lastSeen)
	if err != nil {
		return nil, notFoundIf(err)
	}
	isOwner := kind == "agent" && owner == c.a.ID
	if id != c.a.ID && !isOwner && id != c.a.OwnerID {
		var n int
		if err := s.db.R.QueryRow(`SELECT COUNT(*) FROM members a JOIN members b ON b.group_id=a.group_id WHERE a.principal_id=? AND b.principal_id=?`, c.a.ID, id).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, errf(404, "not_found", "没有这个人，或你们没有共同的群")
		}
	}
	common := []obj{}
	rows, err := s.db.R.Query(`SELECT g.id,g.name FROM groups g JOIN members a ON a.group_id=g.id JOIN members b ON b.group_id=g.id
		WHERE g.kind='group' AND a.principal_id=? AND b.principal_id=? ORDER BY g.name COLLATE NOCASE, g.id`, c.a.ID, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var gid, gname string
		if err := rows.Scan(&gid, &gname); err != nil {
			rows.Close()
			return nil, err
		}
		common = append(common, obj{"id": gid, "name": gname})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	p := obj{"id": id, "kind": kind, "displayName": dn, "intro": intro, "commonGroups": common, "isOwner": isOwner}
	if kind == "human" {
		p["name"] = name
		return obj{"principal": p}, nil
	}
	rt := s.loadRuntime(id)
	var ls any
	if lastSeen.Valid {
		ls = lastSeen.Int64
	}
	p["platform"] = nullable(platform)
	p["ownerId"] = nullable(owner)
	p["ownerName"] = ownerName
	p["online"] = s.hub.online(id)
	p["state"] = rt.State
	p["lastSeenAt"] = ls
	p["disabled"] = disabled != 0
	if isOwner {
		p["host"] = nullable(rt.Host)
		p["mainSession"] = nullable(rt.Session)
		p["dirs"] = s.dirsJSON(rt.Dirs)
		p["notice"] = nullable(rt.Notice)
	} else {
		p["notice"] = nullable(publicNotice(rt.Notice))
	}
	return obj{"principal": p}, nil
}

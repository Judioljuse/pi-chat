package server

import (
	"database/sql"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Custom names for the agent groups on the owner's agents page. The page groups agents by machine
// ("host:<host>") and, inside a machine, by git checkout ("proj:<host>|<repo root>|<branch or worktree path>").

const (
	maxGroupKeyLen  = 800 // host (64) + two paths (300 each) + branch and separators
	maxGroupNameLen = 40
	maxGroupNames   = 500 // per owner
)

// validGroupKey accepts "host:..." or "proj:..." keys without control or format characters (paths may hold spaces).
func validGroupKey(k string) bool {
	if !(strings.HasPrefix(k, "host:") && len(k) > 5 || strings.HasPrefix(k, "proj:") && len(k) > 5) {
		return false
	}
	if len(k) > maxGroupKeyLen || !utf8.ValidString(k) {
		return false
	}
	for _, r := range k {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func (s *Server) agentGroupNames(q querier, owner string) (map[string]string, error) {
	rows, err := q.Query(`SELECT key,name FROM agent_group_names WHERE owner_id=? ORDER BY key`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, n string
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// getAgentGroups: GET /agent-groups -> {names:{key:name}} (the caller's own).
func (s *Server) getAgentGroups(c *ctx) (any, error) {
	names, err := s.agentGroupNames(s.db.R, c.a.ID)
	if err != nil {
		return nil, err
	}
	return obj{"names": names}, nil
}

// putAgentGroup: PUT /agent-groups {key,name}; an empty name resets the group to its default label.
func (s *Server) putAgentGroup(c *ctx) (any, error) {
	var req struct {
		Key  string
		Name *string
	}
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if !validGroupKey(req.Key) {
		return nil, badRequest("key 必须以 host: 或 proj: 开头，不超过 %d 字节，不含控制字符", maxGroupKeyLen)
	}
	if req.Name == nil {
		return nil, badRequest("缺少 name（空字符串表示恢复默认名称）")
	}
	name := cleanName(*req.Name)
	if utf8.RuneCountInString(name) > maxGroupNameLen {
		return nil, badRequest("名称最多 %d 个字", maxGroupNameLen)
	}
	err := s.tx(func(tx *sql.Tx, _ *[]func()) error {
		if name == "" {
			_, err := tx.Exec(`DELETE FROM agent_group_names WHERE owner_id=? AND key=?`, c.a.ID, req.Key)
			return err
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM agent_group_names WHERE owner_id=? AND key<>?`, c.a.ID, req.Key).Scan(&n); err != nil {
			return err
		}
		if n >= maxGroupNames {
			return badRequest("自定义分组名称最多 %d 个", maxGroupNames)
		}
		_, err := tx.Exec(`INSERT INTO agent_group_names(owner_id,key,name,updated_at) VALUES(?,?,?,?)
			ON CONFLICT(owner_id,key) DO UPDATE SET name=excluded.name, updated_at=excluded.updated_at`, c.a.ID, req.Key, name, now())
		return err
	})
	if err != nil {
		return nil, err
	}
	names, err := s.agentGroupNames(s.db.R, c.a.ID)
	if err != nil {
		return nil, err
	}
	return obj{"names": names}, nil
}

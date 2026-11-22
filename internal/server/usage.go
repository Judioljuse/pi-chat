package server

import (
	"strconv"
	"time"
)

// GET /usage?days=7: runs, time and tokens of my agents (admins: all agents), per agent, per
// group and per day, from the listener's run reports (v0.5.0). Days are in the alarm time zone.
func (s *Server) getUsage(c *ctx) (any, error) {
	days := 7
	if v, err := strconv.Atoi(c.r.URL.Query().Get("days")); err == nil && v > 0 {
		days = min(v, 30)
	}
	all := c.r.URL.Query().Get("scope") == "all" && c.a.IsAdmin
	nowT := time.Now().In(s.loc)
	start := time.Date(nowT.Year(), nowT.Month(), nowT.Day(), 0, 0, 0, 0, s.loc).AddDate(0, 0, -(days - 1))
	_, off := nowT.Zone()
	cond, args := `r.created_at>=?`, []any{start.UnixMilli()}
	if !all {
		cond, args = cond+` AND p.owner_id=?`, append(args, c.a.ID)
	}
	const agg = `COUNT(*),SUM(1-r.ok),SUM(r.duration_ms),SUM(r.input_tokens),SUM(r.output_tokens),SUM(r.cache_tokens),SUM(r.cost_usd)`
	row := func(scan func(...any) error, extra ...any) (obj, error) {
		var runs, failed, dur, in, out, cache int64
		var cost float64
		if err := scan(append(extra, &runs, &failed, &dur, &in, &out, &cache, &cost)...); err != nil {
			return nil, err
		}
		return obj{"runs": runs, "failed": failed, "durationMs": dur, "inputTokens": in, "outputTokens": out, "cacheTokens": cache, "costUsd": cost}, nil
	}
	q := func(sel, group string) ([]obj, error) {
		rows, err := s.db.R.Query(`SELECT `+sel+`,`+agg+` FROM runs r JOIN principals p ON p.id=r.agent_id LEFT JOIN groups g ON g.id=r.group_id
			WHERE `+cond+` GROUP BY `+group+` ORDER BY 1`, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []obj{}
		for rows.Next() {
			var k1, k2 string
			o, err := row(rows.Scan, &k1, &k2)
			if err != nil {
				return nil, err
			}
			o["id"], o["name"] = k1, k2
			out = append(out, o)
		}
		return out, rows.Err()
	}
	byAgent, err := q(`r.agent_id, p.display_name`, `r.agent_id`)
	if err != nil {
		return nil, err
	}
	byGroup, err := q(`COALESCE(r.group_id,''), COALESCE(CASE WHEN g.kind='dm' THEN '私聊' ELSE g.name END,'')`, `r.group_id`)
	if err != nil {
		return nil, err
	}
	byDay, err := q(`strftime('%Y-%m-%d', (r.created_at/1000)+`+strconv.Itoa(off)+`, 'unixepoch'), ''`, `1`)
	if err != nil {
		return nil, err
	}
	for _, d := range byDay {
		d["day"] = d["id"]
		delete(d, "id")
		delete(d, "name")
	}
	total, err := row(s.db.R.QueryRow(`SELECT `+agg+` FROM runs r JOIN principals p ON p.id=r.agent_id WHERE `+cond, args...).Scan)
	if err != nil {
		return nil, err
	}
	return obj{"days": days, "since": start.UnixMilli(), "total": total, "agents": byAgent, "groups": byGroup, "byDay": byDay, "scope": map[bool]string{true: "all", false: "mine"}[all]}, nil
}

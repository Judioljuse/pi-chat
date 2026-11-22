package server

import (
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"pichat/internal/store"
	"pichat/internal/tools"
)

// Tool settings set on the admin page (table tool_settings). Precedence for web search: the
// settings saved on the admin page when there are any, else the PICHAT_SEARCH_* env vars, else
// DuckDuckGo. Built-in tools are on unless switched off. Changes apply at once (the searcher is
// rebuilt in-process).

// builtinTools are the server's own MCP tools, in tools/list order (all read-only except
// task_add / task_update, which only write pichat's own task records).
var builtinTools = []string{"view_attachment", "web_fetch", "web_search", "search_messages", "task_list", "task_add", "task_update", "wake_me", "ask_decision"}

var builtinDesc = map[string]string{
	"view_attachment": "查看群消息里的附件（图片、文本文件等）",
	"web_fetch":       "读取公开网页的正文",
	"web_search":      "用配置的搜索服务搜索网页",
	"search_messages": "搜索群里以前的消息（只能搜 agent 所在的群）",
	"task_list":       "查看群任务（非指挥者 @ 的受限运行只有这一个任务工具）",
	"task_add":        "新建群任务",
	"task_update":     "更新群任务的状态、备注和负责人",
	"wake_me":         "agent 定时叫醒自己（最早 10 分钟后，每个 agent 最多 5 个）",
	"ask_decision":    "agent 请人在几个选项里做决定，答案回到群里并叫醒它",
}

func isBuiltin(name string) bool {
	for _, n := range builtinTools {
		if n == name {
			return true
		}
	}
	return false
}

// searchState is the search config in use and where it came from.
type searchState struct {
	cfg    tools.SearchConfig
	source string // db | env | default
}

// loadTools reads tool settings from the database and rebuilds the searcher.
func (s *Server) loadTools() error {
	rows, err := s.db.R.Query(`SELECT key,COALESCE(value,''),secret FROM tool_settings`)
	if err != nil {
		return err
	}
	vals, secrets := map[string]string{}, map[string][]byte{}
	for rows.Next() {
		var k, v string
		var sec []byte
		if err := rows.Scan(&k, &v, &sec); err != nil {
			rows.Close()
			return err
		}
		vals[k] = v
		if len(sec) > 0 {
			secrets[k] = sec
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	disabled := map[string]bool{}
	for _, n := range builtinTools {
		disabled[n] = vals["builtin."+n] == "off"
	}
	st := searchState{source: "default"}
	var keyErr string
	if p, ok := vals["search.provider"]; ok && p != "" {
		st.source = "db"
		st.cfg = tools.SearchConfig{Provider: p, URL: vals["search.url"]}
		if sec := secrets["search.key"]; sec != nil {
			k, err := s.unseal(sec, "tool_settings:search.key")
			if err != nil {
				keyErr = "搜索服务的 key 无法解密（" + err.Error() + "），请在管理页重新填写"
			}
			st.cfg.Key = k
		}
	} else if e := s.cfg.Search; e.Provider != "" || e.Key != "" || e.URL != "" {
		st.source, st.cfg = "env", e
	}
	sr, serr := tools.NewSearcher(st.cfg)
	s.toolMu.Lock()
	defer s.toolMu.Unlock()
	s.disabled, s.search = disabled, st
	if serr != nil {
		s.searcher, s.searchErr = nil, serr.Error()
		if keyErr != "" {
			s.searchErr = keyErr
		}
	} else {
		s.searcher, s.searchErr = sr, ""
	}
	return nil
}

func (s *Server) toolEnabled(name string) bool {
	s.toolMu.RLock()
	defer s.toolMu.RUnlock()
	return !s.disabled[name]
}

func (s *Server) currentSearcher() (*tools.Searcher, string) {
	s.toolMu.RLock()
	defer s.toolMu.RUnlock()
	return s.searcher, s.searchErr
}

func (s *Server) seal(plain, aad string) ([]byte, error) {
	if s.secretErr != "" {
		return nil, errf(500, "secret_unavailable", "实例密钥 secret.key 不可用：%s", s.secretErr)
	}
	return store.Seal(s.secret, plain, aad)
}

func (s *Server) unseal(blob []byte, aad string) (string, error) {
	if len(blob) == 0 {
		return "", nil
	}
	if s.secretErr != "" {
		return "", errors.New("secret.key 不可用：" + s.secretErr)
	}
	return store.Unseal(s.secret, blob, aad)
}

// keyHint shows the last 4 characters of a secret, and nothing for short ones.
func keyHint(secret string) string {
	if utf8.RuneCountInString(secret) < 8 {
		return ""
	}
	r := []rune(secret)
	return "…" + string(r[len(r)-4:])
}

func (s *Server) setSetting(tx querier, key string, value *string, secret []byte) error {
	var v any
	if value != nil {
		v = *value
	}
	var sec any
	if secret != nil {
		sec = secret
	}
	_, err := tx.Exec(`INSERT INTO tool_settings(key,value,secret,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value,secret=excluded.secret,updated_at=excluded.updated_at`, key, v, sec, now())
	return err
}

// ---- admin API ----

func (s *Server) adminTools(c *ctx) (any, error) {
	return s.adminToolsJSON()
}

func (s *Server) adminToolsJSON() (obj, error) {
	s.toolMu.RLock()
	st, disabled := s.search, s.disabled
	s.toolMu.RUnlock()
	var builtin []obj
	for _, n := range builtinTools {
		builtin = append(builtin, obj{"name": n, "enabled": !disabled[n], "description": builtinDesc[n]})
	}
	provider := strings.ToLower(strings.TrimSpace(st.cfg.Provider))
	if provider == "" {
		provider = "duckduckgo"
	}
	search := obj{"provider": provider, "keySet": st.cfg.Key != "", "url": st.cfg.URL, "source": st.source}
	if h := keyHint(st.cfg.Key); h != "" {
		search["keyHint"] = h
	}
	if _, e := s.currentSearcher(); e != "" {
		search["error"] = e
	}
	servers, err := s.listUpstreams(false)
	if err != nil {
		return nil, err
	}
	out := []obj{}
	for _, u := range servers {
		out = append(out, u.adminJSON(s))
	}
	return obj{"builtin": builtin, "search": search, "servers": out}, nil
}

func (s *Server) putBuiltin(c *ctx) (any, error) {
	var req struct {
		Name    string `json:"name"`
		Enabled *bool  `json:"enabled"`
	}
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if !isBuiltin(req.Name) {
		return nil, badRequest("没有叫 %s 的内置工具（可选：%s）", req.Name, strings.Join(builtinTools, "、"))
	}
	if req.Enabled == nil {
		return nil, badRequest("缺少 enabled")
	}
	v := "on"
	if !*req.Enabled {
		v = "off"
	}
	if err := s.setSetting(s.db.W, "builtin."+req.Name, &v, nil); err != nil {
		return nil, err
	}
	if err := s.loadTools(); err != nil {
		return nil, err
	}
	return s.adminToolsJSON()
}

func (s *Server) putSearch(c *ctx) (any, error) {
	var req struct {
		Provider *string `json:"provider"`
		Key      *string `json:"key"`
		URL      *string `json:"url"`
	}
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if req.Provider == nil {
		return nil, badRequest("缺少 provider")
	}
	p := strings.ToLower(strings.TrimSpace(*req.Provider))
	if p == "" {
		// Back to the env vars / default.
		if _, err := s.db.W.Exec(`DELETE FROM tool_settings WHERE key LIKE 'search.%'`); err != nil {
			return nil, err
		}
		if err := s.loadTools(); err != nil {
			return nil, err
		}
		return s.adminToolsJSON()
	}
	known := false
	for _, x := range tools.Providers {
		known = known || x == p
	}
	if !known {
		return nil, badRequest("provider 只能是 %s", strings.Join(tools.Providers, "、"))
	}
	// An omitted key or url keeps the value in use now (saved on the admin page, or from the
	// PICHAT_SEARCH_* env vars, which are then copied into the database), but only while the
	// provider stays the same: a Brave key must never be sent to Tavily, nor to a SearXNG host.
	s.toolMu.RLock()
	cur := s.search.cfg
	s.toolMu.RUnlock()
	if !strings.EqualFold(strings.TrimSpace(cur.Provider), p) {
		cur.Key, cur.URL = "", ""
	}
	u := cur.URL
	if req.URL != nil {
		u = strings.TrimSpace(*req.URL)
	}
	if len(u) > 500 {
		return nil, badRequest("url 太长")
	}
	key := cur.Key
	if req.Key != nil {
		key = strings.TrimSpace(*req.Key)
		if len(key) > 500 {
			return nil, badRequest("key 太长")
		}
	}
	if _, err := tools.NewSearcher(tools.SearchConfig{Provider: p, Key: key, URL: u}); err != nil {
		msg := strings.NewReplacer("PICHAT_SEARCH_PROVIDER=", "", "PICHAT_SEARCH_KEY", "key", "PICHAT_SEARCH_URL", "url").Replace(err.Error())
		return nil, badRequest("%s", msg)
	}
	var sealed []byte
	if key != "" {
		var err error
		if sealed, err = s.seal(key, "tool_settings:search.key"); err != nil {
			return nil, err
		}
	}
	err := s.tx(func(tx *sql.Tx, _ *[]func()) error {
		if err := s.setSetting(tx, "search.provider", &p, nil); err != nil {
			return err
		}
		if err := s.setSetting(tx, "search.url", &u, nil); err != nil {
			return err
		}
		if sealed == nil {
			_, err := tx.Exec(`DELETE FROM tool_settings WHERE key='search.key'`)
			return err
		}
		return s.setSetting(tx, "search.key", nil, sealed)
	})
	if err != nil {
		return nil, err
	}
	if err := s.loadTools(); err != nil {
		return nil, err
	}
	return s.adminToolsJSON()
}

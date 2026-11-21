package server

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

func searchSeqs(t *testing.T, c *client, gid, q string, extra string) []int64 {
	t.Helper()
	r := c.ok("GET", "/groups/"+gid+"/search?q="+url.QueryEscape(q)+extra, nil)
	var out []int64
	for _, x := range r["results"].([]any) {
		out = append(out, int64(x.(map[string]any)["seq"].(float64)))
	}
	return out
}

func TestSearchMessages(t *testing.T) {
	e := newEnv(t)
	alice, bob, carol := e.human("alice"), e.human("bob"), e.human("carol")
	g := alice.group("g", bob)
	other := carol.group("other")
	cc := e.agent(alice, "CC", g)
	// Group creation and joins add system messages, so seqs are mapped: n1..n5 -> real seq.
	sq := func(m map[string]any) int64 { return int64(m["message"].(map[string]any)["seq"].(float64)) }
	s1 := sq(alice.send(g, map[string]any{"text": "我们明天讨论数据库迁移方案", "ignoreNew": true}))
	s2 := sq(bob.send(g, map[string]any{"text": "数据库备份放在 NAS 上", "ignoreNew": true}))
	s3 := sq(alice.send(g, map[string]any{"text": "Hello World of Databases", "ignoreNew": true}))
	s4 := sq(bob.send(g, map[string]any{"text": "这条数据库消息会被撤回", "ignoreNew": true}))
	s5 := sq(alice.send(g, map[string]any{"text": "100%_完成 了", "ignoreNew": true}))
	carol.send(other, map[string]any{"text": "别的群也谈数据库", "ignoreNew": true})
	bob.ok("DELETE", fmt.Sprintf("/groups/%s/messages/%d", g, s4), nil)
	f := func(xs ...int64) string { return fmt.Sprint(xs) }

	// Chinese substring (trigram), newest first; recalled excluded; other groups excluded.
	if got := searchSeqs(t, alice, g, "数据库", ""); fmt.Sprint(got) != f(s2, s1) {
		t.Fatalf("数据库: %v", got)
	}
	if got := searchSeqs(t, alice, g, "迁移方案", ""); fmt.Sprint(got) != f(s1) {
		t.Fatalf("迁移方案: %v", got)
	}
	// Two-character Chinese query: below trigram length, LIKE fallback.
	if got := searchSeqs(t, alice, g, "备份", ""); fmt.Sprint(got) != f(s2) {
		t.Fatalf("备份: %v", got)
	}
	// Case-insensitive for ASCII; LIKE wildcards are literal.
	if got := searchSeqs(t, alice, g, "databases", ""); fmt.Sprint(got) != f(s3) {
		t.Fatalf("case: %v", got)
	}
	if got := searchSeqs(t, alice, g, "%_", ""); fmt.Sprint(got) != f(s5) {
		t.Fatalf("wildcards: %v", got)
	}
	if got := searchSeqs(t, alice, g, "_", ""); fmt.Sprint(got) != f(s5) {
		t.Fatalf("underscore: %v", got)
	}
	// from: by nick, display name or id; before/after by seq.
	if got := searchSeqs(t, alice, g, "数据库", "&from=bob"); fmt.Sprint(got) != f(s2) {
		t.Fatalf("from nick: %v", got)
	}
	if got := searchSeqs(t, alice, g, "数据库", "&from="+alice.id); fmt.Sprint(got) != f(s1) {
		t.Fatalf("from id: %v", got)
	}
	if got := searchSeqs(t, alice, g, "数据库", fmt.Sprintf("&before=%d", s2)); fmt.Sprint(got) != f(s1) {
		t.Fatalf("before: %v", got)
	}
	if got := searchSeqs(t, alice, g, "数据库", fmt.Sprintf("&after=%d", s1)); fmt.Sprint(got) != f(s2) {
		t.Fatalf("after: %v", got)
	}
	if got := searchSeqs(t, alice, g, "数据库", "&limit=1"); fmt.Sprint(got) != f(s2) {
		t.Fatalf("limit: %v", got)
	}
	r := alice.ok("GET", "/groups/"+g+"/search?q="+url.QueryEscape("数据库"), nil)["results"].([]any)[0].(map[string]any)
	if r["author"] != "bob" || r["snippet"] != "数据库备份放在 NAS 上" || r["createdAt"].(float64) == 0 {
		t.Fatalf("hit: %v", r)
	}
	// Authz: non-members get 404; bad input 400.
	if r := carol.do("GET", "/groups/"+g+"/search?q=x", nil); r.status != 404 {
		t.Fatalf("non-member: %d", r.status)
	}
	if r := alice.do("GET", "/groups/"+g+"/search?q=", nil); r.status != 400 {
		t.Fatalf("empty: %d", r.status)
	}
	if r := alice.do("GET", "/groups/"+g+"/search?q="+strings.Repeat("长", 101), nil); r.status != 400 {
		t.Fatalf("too long: %d", r.status)
	}

	// MCP tool: run token defaults to its group and is limited to it.
	run := runToken(t, e, cc, g, "restricted")
	res := callTool(t, e, run.token, "search_messages", map[string]any{"query": "数据库"})
	text := resultText(res)
	if isErr(res) || !strings.HasPrefix(text, searchMsgPrefix+"数据库") || !strings.Contains(text, fmt.Sprintf("#%d bob", s2)) || !strings.Contains(text, fmt.Sprintf("#%d alice", s1)) || strings.Contains(text, "撤回") || strings.Contains(text, "别的群") {
		t.Fatalf("tool: %q", text)
	}
	if res := callTool(t, e, run.token, "search_messages", map[string]any{"query": "数据库", "groupId": other}); !isErr(res) {
		t.Fatalf("run token other group: %v", res)
	}
	// Commander run tokens are still limited to their own group for search.
	cmd := runToken(t, e, cc, g, "full")
	alice.ok("POST", "/groups/"+g+"/members", map[string]any{"agentId": cc.id}) // no-op if member
	g2 := alice.group("g2")
	alice.ok("POST", "/groups/"+g2+"/members", map[string]any{"agentId": cc.id})
	if res := callTool(t, e, cmd.token, "search_messages", map[string]any{"query": "数据库", "groupId": g2}); !isErr(res) {
		t.Fatalf("commander other group: %v", res)
	}
	// Agent token: groupId required, membership checked.
	if res := callTool(t, e, cc.token, "search_messages", map[string]any{"query": "数据库"}); !isErr(res) || !strings.Contains(resultText(res), "groupId") {
		t.Fatalf("agent no group: %v", res)
	}
	if res := callTool(t, e, cc.token, "search_messages", map[string]any{"query": "数据库", "groupId": other}); !isErr(res) {
		t.Fatalf("agent non-member: %v", res)
	}
	res = callTool(t, e, cc.token, "search_messages", map[string]any{"query": "数据库", "groupId": g, "from": "alice", "limit": "5"})
	if isErr(res) || !strings.Contains(resultText(res), fmt.Sprintf("#%d alice", s1)) || strings.Contains(resultText(res), fmt.Sprintf("#%d ", s2)) {
		t.Fatalf("agent: %q", resultText(res))
	}
	if res := callTool(t, e, cc.token, "search_messages", map[string]any{"query": "", "groupId": g}); !isErr(res) {
		t.Fatalf("empty query: %v", res)
	}
	if res := callTool(t, e, cc.token, "search_messages", map[string]any{"query": "不存在的内容啊", "groupId": g}); isErr(res) || !strings.Contains(resultText(res), "没有找到") {
		t.Fatalf("no hit: %v", res)
	}
}

// TestSearchLikeFallback: without the FTS index, search still works over the newest messages.
func TestSearchLikeFallback(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	g := alice.group("g")
	m := alice.send(g, map[string]any{"text": "数据库迁移", "ignoreNew": true})
	alice.send(g, map[string]any{"text": "无关", "ignoreNew": true})
	e.s.db.FTS = false
	if got := searchSeqs(t, alice, g, "数据库迁移", ""); fmt.Sprint(got) != fmt.Sprint([]int64{int64(m["message"].(map[string]any)["seq"].(float64))}) {
		t.Fatalf("fallback: %v", got)
	}
}

func TestSnippet(t *testing.T) {
	long := strings.Repeat("前", 300) + "关键词" + strings.Repeat("后", 300)
	s := snippet(long, "关键词", 200)
	if utf8.RuneCountInString(s) != 200 || !strings.Contains(s, "关键词") || !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") {
		t.Fatalf("snippet: %d %q", utf8.RuneCountInString(s), s)
	}
	if s := snippet("短\n消息", "消息", 200); s != "短 消息" {
		t.Fatalf("short: %q", s)
	}
	head := snippet("关键词"+strings.Repeat("x", 400), "关键词", 200)
	if !strings.HasPrefix(head, "关键词") || utf8.RuneCountInString(head) != 200 {
		t.Fatalf("head: %q", head)
	}
}

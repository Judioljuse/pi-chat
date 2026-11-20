package server

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pichat/internal/tools"
)

// mcpPost sends one JSON-RPC message to /mcp with tok and returns status and decoded body.
func mcpPost(t *testing.T, e *env, tok string, msg any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(msg)
	req, _ := http.NewRequest("POST", e.ts.URL+"/mcp", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return r.StatusCode, out
}

var rpcID int

// callTool runs tools/call and returns the result object.
func callTool(t *testing.T, e *env, tok, name string, args map[string]any) map[string]any {
	t.Helper()
	rpcID++
	st, out := mcpPost(t, e, tok, map[string]any{"jsonrpc": "2.0", "id": rpcID, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	if st != 200 || out["result"] == nil {
		t.Fatalf("tools/call %s -> %d %v", name, st, out)
	}
	return out["result"].(map[string]any)
}

func resultText(r map[string]any) string {
	var b strings.Builder
	for _, c := range r["content"].([]any) {
		if m := c.(map[string]any); m["type"] == "text" {
			b.WriteString(m["text"].(string))
		}
	}
	return b.String()
}

func isErr(r map[string]any) bool { return r["isError"] == true }

// uploadAs uploads a file with the given bytes and returns its id.
func uploadAs(t *testing.T, c *client, name string, data []byte) string {
	t.Helper()
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "--X\r\nContent-Disposition: form-data; name=\"file\"; filename=%q\r\n\r\n", name)
	buf.Write(data)
	buf.WriteString("\r\n--X--\r\n")
	req, _ := http.NewRequest("POST", c.e.ts.URL+"/api/v1/files", &buf)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=X")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else {
		req.Header.Set("Origin", c.e.ts.URL)
	}
	resp, err := c.hc.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("upload %v %v", err, resp.Status)
	}
	defer resp.Body.Close()
	var up map[string]any
	json.NewDecoder(resp.Body).Decode(&up)
	return up["file"].(map[string]any)["id"].(string)
}

func TestMCPHandshake(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	cc := e.agent(alice, "CC", "")
	// No token / session cookie: 401.
	if st, _ := mcpPost(t, e, "", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}); st != 401 {
		t.Fatalf("anon: %d", st)
	}
	st, out := mcpPost(t, e, cc.token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}})
	res, _ := out["result"].(map[string]any)
	if st != 200 || res["protocolVersion"] != "2025-06-18" || res["capabilities"].(map[string]any)["tools"] == nil {
		t.Fatalf("initialize: %d %v", st, out)
	}
	// Unknown version negotiates to the latest supported.
	_, out = mcpPost(t, e, cc.token, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "initialize", "params": map[string]any{"protocolVersion": "2099-01-01"}})
	if out["result"].(map[string]any)["protocolVersion"] != mcpLatest {
		t.Fatalf("negotiate: %v", out)
	}
	// Notification: 202, no body.
	if st, _ := mcpPost(t, e, cc.token, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); st != 202 {
		t.Fatalf("notification: %d", st)
	}
	_, out = mcpPost(t, e, cc.token, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/list"})
	var names []string
	for _, x := range out["result"].(map[string]any)["tools"].([]any) {
		tl := x.(map[string]any)
		names = append(names, tl["name"].(string))
		if tl["inputSchema"].(map[string]any)["type"] != "object" {
			t.Fatalf("schema: %v", tl)
		}
	}
	if strings.Join(names, ",") != "view_attachment,web_fetch,web_search" {
		t.Fatalf("tools: %v", names)
	}
	_, out = mcpPost(t, e, cc.token, map[string]any{"jsonrpc": "2.0", "id": 4, "method": "ping"})
	if out["result"] == nil {
		t.Fatalf("ping: %v", out)
	}
	_, out = mcpPost(t, e, cc.token, map[string]any{"jsonrpc": "2.0", "id": 5, "method": "nope"})
	if out["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatalf("unknown method: %v", out)
	}
	_, out = mcpPost(t, e, cc.token, map[string]any{"jsonrpc": "2.0", "id": 6, "method": "tools/call", "params": map[string]any{"name": "bash"}})
	if out["error"].(map[string]any)["code"].(float64) != -32602 {
		t.Fatalf("unknown tool: %v", out)
	}
	// GET -> 405; bad protocol version header -> 400; foreign Origin -> 403.
	r, _ := http.Get(e.ts.URL + "/mcp")
	if r.StatusCode != 405 {
		t.Fatalf("GET: %d", r.StatusCode)
	}
	req, _ := http.NewRequest("POST", e.ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer "+cc.token)
	req.Header.Set("MCP-Protocol-Version", "1999-01-01")
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 400 {
		t.Fatalf("bad version: %d", r.StatusCode)
	}
	req, _ = http.NewRequest("POST", e.ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer "+cc.token)
	req.Header.Set("Origin", "http://evil.example")
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 403 {
		t.Fatalf("origin: %d", r.StatusCode)
	}
	// Parse error.
	req, _ = http.NewRequest("POST", e.ts.URL+"/mcp", strings.NewReader(`{nope`))
	req.Header.Set("Authorization", "Bearer "+cc.token)
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 400 {
		t.Fatalf("parse: %d", r.StatusCode)
	}
	// GET /api/v1 describes the tools.
	d := e.anon().ok("GET", "", nil)
	ti := d["tools"].(map[string]any)
	if ti["searchProvider"] != "duckduckgo" || len(ti["available"].([]any)) != 3 || ti["mcp"] != "http://pichat.test/mcp" {
		t.Fatalf("describe tools: %v", ti)
	}
}

// hugePNG is a valid PNG header claiming w x h pixels with no image data.
func hugePNG(w, h uint32) []byte {
	ihdr := []byte("IHDR")
	ihdr = binary.BigEndian.AppendUint32(ihdr, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, 8, 2, 0, 0, 0)
	b := []byte("\x89PNG\r\n\x1a\n")
	b = binary.BigEndian.AppendUint32(b, 13)
	b = append(b, ihdr...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(ihdr))
}

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestMCPViewAttachment(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	g1 := alice.group("g1")
	g2 := alice.group("g2")
	g3 := bob.group("g3")
	cc := e.agent(alice, "CC", g1)
	alice.ok("POST", "/groups/"+g2+"/members", map[string]any{"agentId": cc.id})

	img := uploadAs(t, alice, "a.png", pngBytes(3, 2))
	alice.send(g1, map[string]any{"text": "pic", "files": []string{img}})
	long := strings.Repeat("中", 100_000) // 300 KB of UTF-8
	txt := uploadAs(t, alice, "notes.md", []byte(long))
	alice.send(g1, map[string]any{"text": "doc", "files": []string{txt}})
	other := uploadAs(t, bob, "secret.txt", []byte("bob only"))
	bob.send(g3, map[string]any{"text": "s", "files": []string{other}})
	inG2 := uploadAs(t, alice, "g2.txt", []byte("hello g2"))
	alice.send(g2, map[string]any{"text": "x", "files": []string{inG2}})
	bin := uploadAs(t, alice, "data.bin", []byte{0, 1, 2, 3, 0xff, 0xfe})
	alice.send(g1, map[string]any{"text": "bin", "files": []string{bin}})

	r := callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": img})
	cs := r["content"].([]any)
	if isErr(r) || len(cs) != 2 {
		t.Fatalf("image: %v", r)
	}
	im := cs[1].(map[string]any)
	data, _ := base64.StdEncoding.DecodeString(im["data"].(string))
	if im["type"] != "image" || im["mimeType"] != "image/png" || !bytes.Equal(data, pngBytes(3, 2)) {
		t.Fatalf("image content: %v", im["type"])
	}
	if !strings.HasPrefix(cs[0].(map[string]any)["text"].(string), attachPrefix) || !strings.Contains(resultText(r), "3x2") {
		t.Fatalf("image text: %v", cs[0])
	}

	huge := uploadAs(t, alice, "big.png", hugePNG(10000, 5000))
	alice.send(g1, map[string]any{"text": "big", "files": []string{huge}})
	if r := callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": huge}); !isErr(r) || !strings.Contains(resultText(r), "10000x5000") {
		t.Fatalf("huge image: %v", r)
	}
	fake := uploadAs(t, alice, "fake.png", []byte("\x89PNG\r\n\x1a\nnot really"))
	alice.send(g1, map[string]any{"text": "fake", "files": []string{fake}})
	if r := callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": fake}); !isErr(r) || !strings.Contains(resultText(r), "无法解析") {
		t.Fatalf("fake image: %v", r)
	}

	r = callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": txt})
	text := resultText(r)
	if isErr(r) || !strings.HasPrefix(text, attachPrefix) || !strings.Contains(text, "已截断") || len(text) > maxAttachText+1000 || !strings.Contains(text, "中中中") {
		t.Fatalf("text: err=%v len=%d", isErr(r), len(text))
	}
	if !strings.Contains(text, "中\n") && strings.ContainsRune(text, '�') {
		t.Fatal("split rune at truncation")
	}

	r = callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": bin})
	if isErr(r) || !strings.Contains(resultText(r), "data.bin") || strings.Contains(resultText(r), "\x00") {
		t.Fatalf("binary: %v", r)
	}

	// Another group's file: same error as a missing file.
	r = callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": other})
	if !isErr(r) || strings.Contains(resultText(r), "bob only") {
		t.Fatalf("foreign file visible: %v", r)
	}
	if r := callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": "f_missing"}); !isErr(r) {
		t.Fatalf("missing: %v", r)
	}

	// Agent token sees both its groups; a run token only its own group, at any tier.
	if r := callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": inG2}); isErr(r) {
		t.Fatalf("agent token g2: %v", r)
	}
	rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": g1})["token"].(string)
	if r := callTool(t, e, rt, "view_attachment", map[string]any{"fileId": txt}); isErr(r) {
		t.Fatalf("run token own group: %v", r)
	}
	if r := callTool(t, e, rt, "view_attachment", map[string]any{"fileId": inG2}); !isErr(r) {
		t.Fatalf("restricted run token saw g2: %v", r)
	}
	alice.ok("PATCH", "/agents/"+cc.id, map[string]any{"commanderTier": "full"})
	alice.send(g1, map[string]any{"text": "@CC look"})
	time.Sleep(150 * time.Millisecond)
	var wake string
	e.s.db.R.QueryRow(`SELECT id FROM wakes WHERE agent_id=? AND commander=1`, cc.id).Scan(&wake)
	full := cc.ok("POST", "/run-tokens", map[string]any{"groupId": g1, "wakeId": wake})
	if full["tier"] != "full" {
		t.Fatalf("tier: %v", full)
	}
	if r := callTool(t, e, full["token"].(string), "view_attachment", map[string]any{"fileId": inG2}); !isErr(r) {
		t.Fatalf("full run token saw another group: %v", r)
	}
	// Human session cookies can't use MCP.
	req, _ := http.NewRequest("POST", e.ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if r, _ := alice.hc.Do(req); r.StatusCode != 401 {
		t.Fatalf("cookie: %d", r.StatusCode)
	}
}

func TestMCPWebFetch(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	cc := e.agent(alice, "CC", "")
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			if r.Header.Get("Cookie") != "" || !strings.HasPrefix(r.UserAgent(), "pichat-fetch/") {
				w.WriteHeader(400)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, `<html><head><title>测试 页面</title><style>.x{}</style><script>alert(1)</script></head>
				<body><nav>菜单</nav><article><h1>标题</h1><p>正文   第一段</p><p>第二段</p></article><footer>版权</footer></body></html>`)
		case "/meta":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"a":1}`)
		case "/zip":
			w.Header().Set("Content-Type", "application/zip")
			w.Write([]byte("PK\x03\x04"))
		case "/captcha":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<html><body>请完成安全验证 captcha</body></html>`)
		case "/404/sec_abc":
			io.WriteString(w, "x")
		case "/sec":
			http.Redirect(w, r, "/404/sec_abc?x=1", http.StatusFound)
		}
	}))
	defer page.Close()

	// Default blocklist: loopback, private and link-local destinations are refused.
	for _, u := range []string{page.URL + "/page", "http://10.1.2.3/", "http://[::1]/", "http://169.254.169.254/", "http://100.100.100.100/", "http://localhost:1/"} {
		r := callTool(t, e, cc.token, "web_fetch", map[string]any{"url": u})
		if !isErr(r) || !strings.Contains(resultText(r), "内部地址") {
			t.Fatalf("%s not blocked: %v", u, r)
		}
	}
	for _, u := range []string{"file:///etc/passwd", "ftp://x/", "javascript:alert(1)"} {
		if r := callTool(t, e, cc.token, "web_fetch", map[string]any{"url": u}); !isErr(r) {
			t.Fatalf("%s allowed", u)
		}
	}

	// Let the test server through (loopback) but keep the rest of the blocklist.
	var nets = tools.MustCIDRs("10.0.0.0/8,100.64.0.0/10,169.254.0.0/16,172.16.0.0/12,192.168.0.0/16,::1/128,fc00::/7,fe80::/10")
	e.s.fetcher = tools.NewFetcher(nets)
	r := callTool(t, e, cc.token, "web_fetch", map[string]any{"url": page.URL + "/page"})
	text := resultText(r)
	if isErr(r) || !strings.HasPrefix(text, tools.FetchPrefix+page.URL+"/page") || !strings.Contains(text, "标题：测试 页面") ||
		!strings.Contains(text, "正文 第一段\n第二段") || strings.Contains(text, "alert") || strings.Contains(text, "菜单") || strings.Contains(text, "版权") {
		t.Fatalf("page: %v %q", isErr(r), text)
	}
	if r := callTool(t, e, cc.token, "web_fetch", map[string]any{"url": page.URL + "/meta"}); !isErr(r) || !strings.Contains(resultText(r), "169.254.169.254") {
		t.Fatalf("redirect to metadata: %v", r)
	}
	if r := callTool(t, e, cc.token, "web_fetch", map[string]any{"url": page.URL + "/loop"}); !isErr(r) || !strings.Contains(resultText(r), "跳转") {
		t.Fatalf("redirect loop: %v", r)
	}
	if r := callTool(t, e, cc.token, "web_fetch", map[string]any{"url": page.URL + "/json"}); !strings.Contains(resultText(r), `{"a":1}`) {
		t.Fatalf("json: %v", r)
	}
	if r := callTool(t, e, cc.token, "web_fetch", map[string]any{"url": page.URL + "/zip"}); isErr(r) || !strings.Contains(resultText(r), "application/zip") {
		t.Fatalf("zip: %v", r)
	}
	for _, p := range []string{"/captcha", "/sec"} {
		if r := callTool(t, e, cc.token, "web_fetch", map[string]any{"url": page.URL + p}); resultText(r) != tools.LoginWallText {
			t.Fatalf("%s: %v", p, r)
		}
	}

	// Rate limit: 60 fetches per agent per hour (the calls above count too).
	e.s.rl.reset("tool:fetch:" + cc.id)
	for i := 0; i < fetchesPerHour; i++ {
		e.s.rl.allow("tool:fetch:"+cc.id, fetchesPerHour, time.Hour)
	}
	if r := callTool(t, e, cc.token, "web_fetch", map[string]any{"url": page.URL + "/json"}); !isErr(r) || !strings.Contains(resultText(r), "太频繁") {
		t.Fatalf("fetch limit: %v", r)
	}
	// The limit is per agent, shared by its run tokens.
	g := alice.group("g", cc)
	rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": g})["token"].(string)
	if r := callTool(t, e, rt, "web_fetch", map[string]any{"url": page.URL + "/json"}); !isErr(r) {
		t.Fatalf("run token bypassed limit: %v", r)
	}
}

func TestMCPWebSearch(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	cc := e.agent(alice, "CC", "")
	var gotKey, gotQ string
	brave := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotQ = r.Header.Get("X-Subscription-Token"), r.URL.Query().Get("q")
		var res []map[string]string
		for i := 0; i < 12; i++ {
			res = append(res, map[string]string{"title": fmt.Sprintf("<strong>结果</strong> %d", i), "url": fmt.Sprintf("https://example.com/%d", i), "description": "摘要 <b>粗</b>"})
		}
		json.NewEncoder(w).Encode(map[string]any{"web": map[string]any{"results": res}})
	}))
	defer brave.Close()
	sr, err := tools.NewSearcher(tools.SearchConfig{Provider: "brave", Key: "k1", URL: brave.URL})
	if err != nil {
		t.Fatal(err)
	}
	e.s.searcher = sr
	r := callTool(t, e, cc.token, "web_search", map[string]any{"query": "pichat 测试"})
	text := resultText(r)
	if isErr(r) || gotKey != "k1" || gotQ != "pichat 测试" || !strings.HasPrefix(text, tools.SearchPrefix+"pichat 测试") ||
		!strings.Contains(text, "5. 结果 4") || strings.Contains(text, "6. ") || strings.Contains(text, "<b>") {
		t.Fatalf("search: %v %q", isErr(r), text)
	}
	if r := callTool(t, e, cc.token, "web_search", map[string]any{"query": "x", "count": 50}); !strings.Contains(resultText(r), "10. ") || strings.Contains(resultText(r), "11. ") {
		t.Fatalf("count cap: %q", resultText(r))
	}
	if r := callTool(t, e, cc.token, "web_search", map[string]any{"query": " "}); !isErr(r) {
		t.Fatalf("empty query: %v", r)
	}
	// Rate limit: 30 searches per agent per hour.
	for i := 0; i < searchesPerHour; i++ {
		e.s.rl.allow("tool:search:"+cc.id, searchesPerHour, time.Hour)
	}
	if r := callTool(t, e, cc.token, "web_search", map[string]any{"query": "x"}); !isErr(r) || !strings.Contains(resultText(r), "太频繁") {
		t.Fatalf("search limit: %v", r)
	}
	// Misconfigured provider: a clear error, not a crash.
	e.s.searcher, e.s.searchErr = nil, "PICHAT_SEARCH_PROVIDER=brave 需要 PICHAT_SEARCH_KEY"
	other := e.agent(alice, "Other", "")
	if r := callTool(t, e, other.token, "web_search", map[string]any{"query": "x"}); !isErr(r) || !strings.Contains(resultText(r), "PICHAT_SEARCH_KEY") {
		t.Fatalf("unconfigured: %v", r)
	}
}

func rawMCP(t *testing.T, e *env, tok, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", e.ts.URL+"/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var out map[string]any
	json.NewDecoder(r.Body).Decode(&out)
	return r.StatusCode, out
}

// Batches are refused (removed in MCP 2025-06-18); malformed messages get -32600.
func TestMCPInvalidRequests(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	cc := e.agent(alice, "CC", "")
	code := func(out map[string]any) float64 {
		if er, ok := out["error"].(map[string]any); ok {
			return er["code"].(float64)
		}
		return 0
	}
	if st, out := rawMCP(t, e, cc.token, `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`); st != 400 || code(out) != -32600 {
		t.Fatalf("batch: %d %v", st, out)
	}
	if st, out := rawMCP(t, e, cc.token, `{"jsonrpc":"2.0","id":7,"params":{}}`); st != 400 || code(out) != -32600 || out["id"].(float64) != 7 {
		t.Fatalf("no method: %d %v", st, out)
	}
	if st, _ := rawMCP(t, e, cc.token, `{"jsonrpc":"1.0","method":"notifications/initialized"}`); st != 400 {
		t.Fatalf("bad version notification: %d", st)
	}
	// A client response (id + result) is still accepted.
	if st, _ := rawMCP(t, e, cc.token, `{"jsonrpc":"2.0","id":3,"result":{}}`); st != 202 {
		t.Fatalf("client response: %d", st)
	}
}

func TestMCPViewAttachmentLimits(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	g1 := alice.group("g1")
	cc := e.agent(alice, "CC", g1)

	// Unattached file uploaded by the agent: its own token sees it, a run token doesn't.
	up := uploadAs(t, cc, "draft.txt", []byte("secret draft"))
	if r := callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": up}); isErr(r) {
		t.Fatalf("own unattached: %v", r)
	}
	rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": g1})["token"].(string)
	if r := callTool(t, e, rt, "view_attachment", map[string]any{"fileId": up}); !isErr(r) || strings.Contains(resultText(r), "secret") {
		t.Fatalf("run token saw unattached file: %v", r)
	}

	// Too wide for the model API: downscaled to a JPEG.
	wide := uploadAs(t, alice, "wide.png", pngBytes(9000, 20))
	alice.send(g1, map[string]any{"text": "wide", "files": []string{wide}})
	r := callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": wide})
	cs := r["content"].([]any)
	if isErr(r) || len(cs) != 2 || cs[1].(map[string]any)["mimeType"] != "image/jpeg" || !strings.Contains(resultText(r), "已缩小") {
		t.Fatalf("downscale: %v", resultText(r))
	}
	data, _ := base64.StdEncoding.DecodeString(cs[1].(map[string]any)["data"].(string))
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != scaledSide || cfg.Height < 1 {
		t.Fatalf("scaled size: %v %v", cfg, err)
	}

	// Per-agent rate limit.
	n := 3 // calls so far
	for ; n < viewsPerHour; n++ {
		callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": "f_missing"})
	}
	if r := callTool(t, e, cc.token, "view_attachment", map[string]any{"fileId": up}); !isErr(r) || !strings.Contains(resultText(r), "太频繁") {
		t.Fatalf("rate limit: %v", r)
	}
}

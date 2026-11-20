package tools

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const ddgPage = `<html><body><form action="/html/" method="post"><input name="q"></form>
<div class="result results_links results_links_deep web-result">
 <h2 class="result__title"><a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F&amp;rut=abc">The <b>Go</b> Programming Language</a></h2>
 <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F">Documentation for <b>Go</b>.</a>
</div>
<div class="result results_links web-result result--ad">
 <h2 class="result__title"><a class="result__a" href="https://duckduckgo.com/y.js?ad_domain=x">Ad</a></h2>
</div>
<div class="result results_links web-result">
 <h2 class="result__title"><a class="result__a" href="https://example.org/a">Example</a></h2>
 <a class="result__snippet">Second   snippet</a>
</div>
</body></html>`

func TestDuckDuckGo(t *testing.T) {
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		form = r.PostForm.Get("q")
		if form == "blocked" {
			w.Write([]byte(`<html><body><div class="anomaly-modal__modal">Unfortunately, bots use DuckDuckGo too.</div><form id="challenge-form"></form></body></html>`))
			return
		}
		w.Write([]byte(ddgPage))
	}))
	defer srv.Close()
	s, err := NewSearcher(SearchConfig{URL: srv.URL})
	if err != nil || s.Provider() != "duckduckgo" {
		t.Fatal(err)
	}
	rs, err := s.Search(context.Background(), "go 语言", 5)
	if err != nil || form != "go 语言" {
		t.Fatal(err, form)
	}
	if len(rs) != 2 || rs[0].URL != "https://go.dev/doc/" || rs[0].Title != "The Go Programming Language" || rs[0].Snippet != "Documentation for Go." ||
		rs[1].URL != "https://example.org/a" || rs[1].Snippet != "Second snippet" {
		t.Fatalf("%+v", rs)
	}
	if _, err := s.Search(context.Background(), "blocked", 5); err == nil || !strings.Contains(err.Error(), "PICHAT_SEARCH_PROVIDER") {
		t.Fatalf("blocked: %v", err)
	}
}

func TestSearcherConfig(t *testing.T) {
	for _, c := range []SearchConfig{{Provider: "brave"}, {Provider: "tavily"}, {Provider: "bocha"}, {Provider: "searxng"}, {Provider: "google"}, {Provider: "searxng", URL: "ftp://x"}} {
		if _, err := NewSearcher(c); err == nil {
			t.Fatalf("%+v accepted", c)
		}
	}
	if s, err := NewSearcher(SearchConfig{Provider: "SearXNG", URL: "http://127.0.0.1:8888/"}); err != nil || s.Provider() != "searxng" {
		t.Fatal(err)
	}
}

func TestProvidersJSON(t *testing.T) {
	var path, auth, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path+"?"+r.URL.RawQuery, r.Header.Get("Authorization")
		b := make([]byte, 1024)
		n, _ := r.Body.Read(b)
		body = string(b[:n])
		switch {
		case strings.HasPrefix(path, "/search?"):
			w.Write([]byte(`{"results":[{"title":"S","url":"https://s.example/","content":"c"}]}`))
		case strings.Contains(body, `"max_results"`):
			w.Write([]byte(`{"results":[{"title":"T","url":"https://t.example/","content":"c"}]}`))
		default:
			w.Write([]byte(`{"code":200,"data":{"webPages":{"value":[{"name":"B","url":"https://b.example/","snippet":"","summary":"sum"}]}}}`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	s, _ := NewSearcher(SearchConfig{Provider: "searxng", URL: srv.URL})
	if rs, err := s.Search(ctx, "q", 3); err != nil || len(rs) != 1 || rs[0].Title != "S" || !strings.Contains(path, "format=json") {
		t.Fatal(rs, err, path)
	}
	s, _ = NewSearcher(SearchConfig{Provider: "tavily", Key: "tk", URL: srv.URL})
	if rs, err := s.Search(ctx, "q", 3); err != nil || len(rs) != 1 || rs[0].Title != "T" || auth != "Bearer tk" {
		t.Fatal(rs, err, auth)
	}
	s, _ = NewSearcher(SearchConfig{Provider: "bocha", Key: "bk", URL: srv.URL})
	if rs, err := s.Search(ctx, "q", 3); err != nil || len(rs) != 1 || rs[0].Snippet != "sum" || auth != "Bearer bk" {
		t.Fatal(rs, err, auth)
	}
}

func TestExtractHTML(t *testing.T) {
	title, text := ExtractHTML(strings.NewReader(`<!doctype html><title> A  &amp; B </title><body><header>Site</header>
<nav><a>Home</a></nav><main><h1>Head</h1><p>one<br>two &lt;3</p><img src=x><ul><li>x</li><li>y</li></ul></main>
<aside>ads</aside><noscript>enable js</noscript><footer>c</footer></body>`))
	if title != "A & B" || text != "Site\nHead\none\ntwo <3\nx\ny" {
		t.Fatalf("%q %q", title, text)
	}
}

func TestLooksBlocked(t *testing.T) {
	if !looksBlocked(200, "请输入验证码") || looksBlocked(200, "normal article") || looksBlocked(200, strings.Repeat("长文 ", 400)+"captcha") {
		t.Fatal("looksBlocked")
	}
}

func TestDefaultBlocked(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.0.1", "::1", "fd00::1", "2002:0a00:0001::1", "2001:0:4136:e378::1", "192.88.99.1", "100::1"} {
		if !blockedIP(net.ParseIP(s), DefaultBlocked) {
			t.Errorf("%s should be blocked", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700::1111"} {
		if blockedIP(net.ParseIP(s), DefaultBlocked) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Client side of MCP Streamable HTTP, used to proxy tools of external MCP servers that the
// admin configured. JSON-RPC over POST; the response may be application/json or a
// text/event-stream carrying the response message.

// MCPTimeout bounds one upstream operation (a var so tests can shorten it; read when a client is built).
var MCPTimeout = 30 * time.Second

const (
	// MCPMaxBody caps the bytes read from one upstream response.
	MCPMaxBody     = 2 << 20
	mcpClientProto = "2025-06-18"
	mcpMaxPages    = 10
	mcpMaxTools    = 200
)

// MCPUpstream is where and how to reach one external MCP server.
type MCPUpstream struct {
	URL         string
	HeaderName  string // e.g. Authorization; empty: no auth header
	HeaderValue string // secret, e.g. "Bearer xxx"
}

// MCPSession is the result of initialize: the negotiated version and the optional session id.
type MCPSession struct {
	ID, Version string
}

// UpstreamTool is one tool from an upstream tools/list.
type UpstreamTool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Annotations json.RawMessage `json:"annotations,omitempty"`
}

// RPCError is a JSON-RPC error returned by the upstream.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("上游返回错误 %d：%s", e.Code, e.Message)
}

// ErrSessionExpired: the upstream answered 404 to a request carrying a session id; initialize again.
var ErrSessionExpired = errors.New("上游会话已过期")

// MCPClient talks to external MCP servers. Connections to blocked addresses are refused on
// the address actually dialed (like web_fetch); redirects are not followed and no
// environment proxy is used.
type MCPClient struct {
	client  *http.Client
	blocked []*net.IPNet
	id      atomic.Int64
}

// NewMCPClient returns a client refusing addresses in blocked (nil: no address restriction).
func NewMCPClient(blocked []*net.IPNet) *MCPClient {
	d := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || blockedIP(ip, blocked) {
			return &blockedError{host}
		}
		return nil
	}}
	return &MCPClient{blocked: blocked, client: &http.Client{
		Timeout: MCPTimeout,
		Transport: &http.Transport{DialContext: d.DialContext, Proxy: nil, MaxIdleConns: 8, IdleConnTimeout: time.Minute,
			TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: MCPTimeout},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Initialize runs initialize + notifications/initialized.
func (c *MCPClient) Initialize(ctx context.Context, up MCPUpstream) (*MCPSession, error) {
	params := map[string]any{"protocolVersion": mcpClientProto, "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "pichat", "version": strings.TrimPrefix(UserAgent(), "pichat-fetch/")}}
	res, hdr, err := c.rpc(ctx, up, nil, "initialize", params)
	if err != nil {
		return nil, err
	}
	var r struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, errors.New("上游 initialize 的返回格式不对")
	}
	s := &MCPSession{ID: hdr.Get("Mcp-Session-Id"), Version: r.ProtocolVersion}
	if s.Version == "" {
		s.Version = mcpClientProto
	}
	c.notify(ctx, up, s, "notifications/initialized")
	return s, nil
}

// ListTools runs tools/list, following nextCursor (at most 10 pages, 200 tools).
func (c *MCPClient) ListTools(ctx context.Context, up MCPUpstream, s *MCPSession) ([]UpstreamTool, error) {
	var out []UpstreamTool
	cursor := ""
	for page := 0; page < mcpMaxPages; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		res, _, err := c.rpc(ctx, up, s, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var r struct {
			Tools      []UpstreamTool `json:"tools"`
			NextCursor string         `json:"nextCursor"`
		}
		if err := json.Unmarshal(res, &r); err != nil {
			return nil, errors.New("上游 tools/list 的返回格式不对")
		}
		out = append(out, r.Tools...)
		if len(out) >= mcpMaxTools {
			return out[:mcpMaxTools], nil
		}
		if r.NextCursor == "" {
			break
		}
		cursor = r.NextCursor
	}
	return out, nil
}

// CallTool runs tools/call and returns the raw result object.
func (c *MCPClient) CallTool(ctx context.Context, up MCPUpstream, s *MCPSession, name string, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	res, _, err := c.rpc(ctx, up, s, "tools/call", map[string]any{"name": name, "arguments": args})
	return res, err
}

func (c *MCPClient) request(ctx context.Context, up MCPUpstream, s *MCPSession, body []byte) (*http.Request, error) {
	u, err := url.Parse(up.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("上游地址无效")
	}
	if err := literalBlocked(u, c.blocked); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", up.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", UserAgent())
	if up.HeaderName != "" && up.HeaderValue != "" {
		req.Header.Set(up.HeaderName, up.HeaderValue)
	}
	if s != nil {
		req.Header.Set("MCP-Protocol-Version", s.Version)
		if s.ID != "" {
			req.Header.Set("Mcp-Session-Id", s.ID)
		}
	}
	return req, nil
}

func (c *MCPClient) notify(ctx context.Context, up MCPUpstream, s *MCPSession, method string) {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	req, err := c.request(ctx, up, s, b)
	if err != nil {
		return
	}
	if resp, err := c.client.Do(req); err == nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
	}
}

func (c *MCPClient) rpc(ctx context.Context, up MCPUpstream, s *MCPSession, method string, params any) (json.RawMessage, http.Header, error) {
	id := c.id.Add(1)
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	req, err := c.request(ctx, up, s, b)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, mcpErr(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound && s != nil && s.ID != "" {
		return nil, nil, ErrSessionExpired
	}
	if resp.StatusCode/100 != 2 {
		return nil, nil, fmt.Errorf("上游返回 HTTP %d", resp.StatusCode)
	}
	lr := &capReader{r: resp.Body, n: MCPMaxBody}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	var msg []byte
	if ct == "text/event-stream" {
		msg, err = sseResponse(lr, id)
	} else {
		msg, err = io.ReadAll(lr)
	}
	if lr.over {
		return nil, nil, errors.New("上游返回内容超过 2 MB")
	}
	if err != nil {
		return nil, nil, mcpErr(ctx, err)
	}
	var r struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(msg, &r); err != nil {
		return nil, nil, errors.New("上游返回的不是 JSON-RPC 消息")
	}
	if r.Error != nil {
		if len(r.Error.Message) > 500 {
			r.Error.Message, _ = truncRunes(r.Error.Message, 500)
		}
		return nil, nil, r.Error
	}
	if len(r.Result) == 0 || string(r.Result) == "null" {
		return nil, nil, errors.New("上游的返回里没有 result")
	}
	return r.Result, resp.Header, nil
}

// sseResponse reads SSE events until the JSON-RPC response with id arrives.
func sseResponse(r io.Reader, id int64) ([]byte, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), MCPMaxBody)
	var data []string
	want := fmt.Sprint(id)
	flush := func() []byte {
		if len(data) == 0 {
			return nil
		}
		msg := []byte(strings.Join(data, "\n"))
		data = data[:0]
		var probe struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(msg, &probe) == nil && probe.Method == "" && strings.Trim(string(probe.ID), `"`) == want {
			return msg
		}
		return nil // a request or notification from the server: ignored
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if m := flush(); m != nil {
				return m, nil
			}
			continue
		}
		if v, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(v, " "))
		}
	}
	if m := flush(); m != nil {
		return m, nil
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("上游的事件流里没有对应的返回")
}

// capReader reads at most n bytes and records whether there was more.
type capReader struct {
	r    io.Reader
	n    int64
	over bool
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.n <= 0 {
		var one [1]byte
		if k, _ := c.r.Read(one[:]); k > 0 {
			c.over = true
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.n {
		p = p[:c.n]
	}
	k, err := c.r.Read(p)
	c.n -= int64(k)
	return k, err
}

func mcpErr(ctx context.Context, err error) error {
	var be *blockedError
	if errors.As(err, &be) {
		return be
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("上游超时（%d 秒）", int(MCPTimeout.Seconds()))
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return fmt.Errorf("上游超时（%d 秒）", int(MCPTimeout.Seconds()))
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return errors.New("域名解析失败：" + dnsErr.Name)
		}
		if ue.Err != nil {
			return errors.New("连接上游失败：" + ue.Err.Error())
		}
	}
	return errors.New("连接上游失败：" + err.Error())
}

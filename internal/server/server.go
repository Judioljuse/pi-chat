// Package server implements the pichat HTTP API, SSE hub and wake dispatcher.
package server

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"pichat/internal/store"
	"pichat/internal/update"
)

//go:embed skill.md
var skillTemplate string

const (
	cookieName = "pichat_session"
	sessionTTL = 30 * 24 * time.Hour
	runTTL     = 60 * time.Minute
	maxMembers = 100
	hopLimit   = 6
)

// Config holds runtime settings.
type Config struct {
	Listen, Data, PublicURL string
	// TrustedProxies are peers whose X-Forwarded-For is honored (default: loopback and private ranges).
	TrustedProxies []*net.IPNet
	// WebhookAllow are extra destination ranges webhooks may reach (internal ranges are blocked by default).
	WebhookAllow []*net.IPNet
	// Updater checks and installs releases (nil: online update disabled).
	Updater *update.Updater
}

// ParseCIDRs parses a comma-separated list of CIDRs or bare IPs.
func ParseCIDRs(list string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, x := range strings.Split(list, ",") {
		if x = strings.TrimSpace(x); x == "" {
			continue
		}
		if !strings.Contains(x, "/") {
			if ip := net.ParseIP(x); ip != nil && ip.To4() != nil {
				x += "/32"
			} else {
				x += "/128"
			}
		}
		_, n, err := net.ParseCIDR(x)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func mustCIDRs(list string) []*net.IPNet {
	n, err := ParseCIDRs(list)
	if err != nil {
		panic(err)
	}
	return n
}

var (
	defaultProxies = mustCIDRs("127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7")
	// internalNets are blocked as webhook destinations unless allow-listed.
	internalNets = mustCIDRs("0.0.0.0/8,10.0.0.0/8,100.64.0.0/10,127.0.0.0/8,169.254.0.0/16,172.16.0.0/12,192.168.0.0/16,224.0.0.0/4,240.0.0.0/4,::/128,::1/128,fc00::/7,fe80::/10,ff00::/8")
)

func inNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Assets are the embedded files served to browsers and agents.
type Assets struct {
	Web      fs.FS
	Listener []byte
}

type Server struct {
	cfg    Config
	db     *store.DB
	assets Assets
	mux    *http.ServeMux
	hub    *hub
	wk     *waker
	work   *working
	rl     *limiter
	log    *slog.Logger
	client *http.Client

	// WakeDelay is the coalescing window; WebhookRetry the delay before the single retry.
	WakeDelay    time.Duration
	WebhookRetry time.Duration

	hopMu     sync.Mutex
	blobMu    sync.Mutex // serializes blob creation with the blob sweep
	agentMu   sync.Mutex // serializes 'agent' runtime broadcasts
	hopNotice map[string]time.Time

	// Restart replaces the default restart (SSE notice, shutdown, exec) in tests.
	Restart      func(path, boot string)
	upd          *update.Updater
	updOnce      sync.Once
	httpSrv      *http.Server
	restarting   atomic.Bool
	listenerOnce sync.Once
	listenerSum  string
}

func New(cfg Config, db *store.DB, assets Assets) *Server {
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	if cfg.TrustedProxies == nil {
		cfg.TrustedProxies = defaultProxies
	}
	s := &Server{
		cfg: cfg, db: db, assets: assets, mux: http.NewServeMux(),
		hub: newHub(), work: newWorking(), rl: newLimiter(), log: slog.Default(),
		client:       webhookClient(cfg.WebhookAllow),
		WakeDelay:    2 * time.Second,
		WebhookRetry: 30 * time.Second,
		hopNotice:    map[string]time.Time{},
	}
	s.wk = newWaker(s)
	s.upd = cfg.Updater
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

// Close stops pending wakes and drops SSE connections.
func (s *Server) Close() {
	s.wk.stop()
	s.work.stop()
	s.hub.closeAll()
}

// Run serves HTTP until ctx is cancelled, with the cleanup loop in the background.
func (s *Server) Run(ctx context.Context) error {
	if err := s.firstStart(); err != nil {
		return err
	}
	s.checkFilesDir()
	go s.cleanupLoop(ctx)
	srv := &http.Server{Addr: s.cfg.Listen, Handler: s.mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	s.httpSrv = srv
	s.startUpdates(ctx)
	if self := update.Detect(version); self.Installed {
		go update.HealthyAfter(s.cfg.Data, version, time.Minute, ctx.Done())
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	s.log.Info("pichat listening", "addr", s.cfg.Listen, "url", s.cfg.PublicURL, "version", version)
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) && s.restarting.Load() {
			select {} // restartInto shuts down and execs the new binary
		}
		return err
	case <-ctx.Done():
		s.Close()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := srv.Shutdown(sctx)
		// A clean stop (docker restart, SIGTERM) is not a failed start: don't let it trip the boot guard.
		if self := update.Detect(version); self.Installed {
			update.MarkHealthy(s.cfg.Data, version)
		}
		return err
	}
}

func (s *Server) firstStart() error {
	var n int
	if err := s.db.R.QueryRow(`SELECT COUNT(*) FROM principals WHERE kind='human'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	url, err := s.AdminLink()
	if err != nil {
		return err
	}
	s.log.Info("还没有任何用户。用这个一次性链接注册管理员（1 小时内有效）", "url", url)
	fmt.Println("管理员注册链接：" + url)
	return nil
}

// AdminLink creates a one-time admin invite valid for one hour.
func (s *Server) AdminLink() (string, error) {
	code := store.RandString(15)
	if _, err := s.db.W.Exec(`INSERT INTO invites(hash,kind,max_uses,expires_at,created_at) VALUES(?,?,1,?,?)`,
		store.Hash(code), "admin", now()+time.Hour.Milliseconds(), now()); err != nil {
		return "", err
	}
	return s.cfg.PublicURL + "/#/redeem/" + code, nil
}

func now() int64 { return time.Now().UnixMilli() }

// ---- errors and JSON ----

type apiError struct {
	status    int
	code, msg string
	extra     map[string]any
}

func (e *apiError) Error() string { return e.code + ": " + e.msg }

func errf(status int, code, format string, args ...any) *apiError {
	return &apiError{status: status, code: code, msg: fmt.Sprintf(format, args...)}
}

func (e *apiError) with(k string, v any) *apiError {
	if e.extra == nil {
		e.extra = map[string]any{}
	}
	e.extra[k] = v
	return e
}

var (
	errUnauthorized = func() *apiError { return errf(401, "unauthorized", "未登录或 token 无效") }
	errForbidden    = func() *apiError { return errf(403, "forbidden", "没有权限") }
	errNotFound     = func() *apiError { return errf(404, "not_found", "不存在") }
)

func badRequest(format string, args ...any) *apiError {
	return errf(400, "bad_request", format, args...)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) writeErr(w http.ResponseWriter, err error) {
	var ae *apiError
	if !errors.As(err, &ae) {
		s.log.Error("internal error", "err", err)
		ae = errf(500, "internal", "服务器内部错误")
	}
	body := map[string]any{"code": ae.code, "message": ae.msg}
	for k, v := range ae.extra {
		body[k] = v
	}
	writeJSON(w, ae.status, map[string]any{"error": body})
}

type obj = map[string]any

// ---- routing ----

const (
	pub = 1 << iota
	human
	agentTok // long-lived agent token
	runGroup // restricted run token (scoped to one group)
	runCmd   // commander run token
	adminOnly
	runAny = runGroup | runCmd
	anyone = human | agentTok | runAny
)

type ctx struct {
	w http.ResponseWriter
	r *http.Request
	a *actor
}

func (c *ctx) decode(v any) error {
	if c.r.Body == nil {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(c.r.Body, 1<<20))
	if err != nil {
		return badRequest("读取请求失败")
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return badRequest("JSON 格式错误：%v", err)
	}
	return nil
}

type hfunc func(c *ctx) (any, error)

func (s *Server) handle(pattern string, access int, f hfunc) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		a, err := s.authenticate(w, r)
		if err != nil && access&pub == 0 {
			s.writeErr(w, err)
			return
		}
		if a == nil && access&pub == 0 {
			s.writeErr(w, errUnauthorized())
			return
		}
		if a != nil && access&pub == 0 && !a.allowed(access) {
			s.writeErr(w, errForbidden())
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			if err := checkCSRF(r); err != nil {
				s.writeErr(w, err)
				return
			}
		}
		res, err := f(&ctx{w: w, r: r, a: a})
		if err != nil {
			s.writeErr(w, err)
			return
		}
		if res != nil {
			writeJSON(w, 200, res)
		}
	})
}

func checkCSRF(r *http.Request) error {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") && r.URL.Path == "/api/v1/files" {
		if o := r.Header.Get("Origin"); o != "" {
			if i := strings.Index(o, "://"); i < 0 || o[i+3:] != r.Host {
				return errf(403, "forbidden", "Origin 不匹配")
			}
		}
		return nil
	}
	if !strings.HasPrefix(ct, "application/json") {
		return errf(400, "bad_request", "需要 Content-Type: application/json")
	}
	return nil
}

// clientIP returns the peer address, or for trusted proxies the rightmost
// X-Forwarded-For entry that is not itself a trusted proxy.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !inNets(ip, s.cfg.TrustedProxies) {
		return host
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		pip := net.ParseIP(p)
		if pip == nil {
			break
		}
		host = p
		if !inNets(pip, s.cfg.TrustedProxies) {
			break
		}
	}
	return host
}

// webhookClient refuses redirects and connections to internal addresses not in allow.
func webhookClient(allow []*net.IPNet) *http.Client {
	d := &net.Dialer{Timeout: 5 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || (inNets(ip, internalNets) && !inNets(ip, allow)) {
			return fmt.Errorf("不允许访问内部地址 %s（可用 PICHAT_WEBHOOK_ALLOW 放行）", host)
		}
		return nil
	}}
	return &http.Client{
		Timeout:       10 * time.Second,
		Transport:     &http.Transport{DialContext: d.DialContext, Proxy: nil, MaxIdleConns: 4, IdleConnTimeout: time.Minute, TLSHandshakeTimeout: 5 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// ---- small db helpers ----

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

// tx runs f in a write transaction; post-commit actions are run afterwards in order.
func (s *Server) tx(f func(tx *sql.Tx, after *[]func()) error) error {
	t, err := s.db.W.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback() // no-op after Commit; releases the writer on error or panic
	var after []func()
	if err := f(t, &after); err != nil {
		return err
	}
	if err := t.Commit(); err != nil {
		return err
	}
	for _, fn := range after {
		fn()
	}
	return nil
}

func scanStrings(rows *sql.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func notFoundIf(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound()
	}
	return err
}

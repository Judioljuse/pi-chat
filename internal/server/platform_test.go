package server

import (
	"strings"
	"testing"
)

// Codex agents keep their platform; unknown platforms become "other"; the pairing text lists codex.
func TestPairPlatformCodex(t *testing.T) {
	e := newEnv(t)
	h := e.human("alice")
	for _, c := range []struct{ in, want string }{{"codex", "codex"}, {"pi", "pi"}, {"gpt", "other"}} {
		pc := h.ok("POST", "/pairing-codes", map[string]any{})
		if ins, _ := pc["instruction"].(string); !strings.Contains(ins, "--agent claude|pi|codex") {
			t.Fatalf("instruction does not mention codex: %q", ins)
		}
		r := e.anon().ok("POST", "/pair", map[string]any{"code": pc["code"], "name": "bot-" + c.in, "platform": c.in})
		a := &client{e: e, hc: h.hc, token: r["token"].(string)}
		if got := a.ok("GET", "/me", nil)["platform"]; got != c.want {
			t.Fatalf("platform %q: got %v, want %s", c.in, got, c.want)
		}
	}
}

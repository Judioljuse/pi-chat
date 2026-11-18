package server

import (
	"encoding/json"
	"fmt"
	"sync"
)

// frame is one pre-encoded SSE event.
type frame struct {
	id   int64
	data []byte
}

type sub struct {
	pid   string
	agent bool
	ch    chan frame
	done  chan struct{}
	once  sync.Once
}

func (s *sub) close() { s.once.Do(func() { close(s.done) }) }

// hub tracks SSE subscribers and inbox long-poll waiters per principal.
type hub struct {
	mu      sync.Mutex
	subs    map[string]map[*sub]struct{}
	waiters map[string]map[chan struct{}]struct{}
}

func newHub() *hub {
	return &hub{subs: map[string]map[*sub]struct{}{}, waiters: map[string]map[chan struct{}]struct{}{}}
}

func encodeEvent(id int64, typ string, data any) frame {
	b, _ := json.Marshal(data)
	var out []byte
	if id > 0 {
		out = fmt.Appendf(out, "id: %d\n", id)
	}
	out = fmt.Appendf(out, "event: %s\ndata: %s\n\n", typ, b)
	return frame{id: id, data: out}
}

// subscribe registers a connection. For agents, existing connections get `replaced` and are closed.
func (h *hub) subscribe(pid string, agent bool) *sub {
	s := &sub{pid: pid, agent: agent, ch: make(chan frame, 256), done: make(chan struct{})}
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.subs[pid]
	if m == nil {
		m = map[*sub]struct{}{}
		h.subs[pid] = m
	}
	if agent {
		for old := range m {
			select {
			case old.ch <- encodeEvent(0, "replaced", obj{}):
			default:
			}
			old.close()
			delete(m, old)
		}
	}
	m[s] = struct{}{}
	return s
}

func (h *hub) unsubscribe(s *sub) {
	h.mu.Lock()
	if m := h.subs[s.pid]; m != nil {
		delete(m, s)
		if len(m) == 0 {
			delete(h.subs, s.pid)
		}
	}
	h.mu.Unlock()
	s.close()
}

// send delivers f to all connections of pid; slow subscribers are dropped.
func (h *hub) send(pid string, f frame) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[pid] {
		select {
		case s.ch <- f:
		default:
			s.close()
			delete(h.subs[pid], s)
		}
	}
}

// sendFinal delivers f and then closes the connections of pid (deleted, disabled).
func (h *hub) sendFinal(pid string, f frame) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[pid] {
		select {
		case s.ch <- f:
		default:
		}
		s.close()
	}
	delete(h.subs, pid)
}

func (h *hub) online(pid string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[pid]) > 0
}

func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for pid, m := range h.subs {
		for s := range m {
			s.close()
		}
		delete(h.subs, pid)
	}
}

func (h *hub) wait(pid string) chan struct{} {
	c := make(chan struct{}, 1)
	h.mu.Lock()
	m := h.waiters[pid]
	if m == nil {
		m = map[chan struct{}]struct{}{}
		h.waiters[pid] = m
	}
	m[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *hub) unwait(pid string, c chan struct{}) {
	h.mu.Lock()
	if m := h.waiters[pid]; m != nil {
		delete(m, c)
		if len(m) == 0 {
			delete(h.waiters, pid)
		}
	}
	h.mu.Unlock()
}

func (h *hub) notify(pid string) {
	h.mu.Lock()
	for c := range h.waiters[pid] {
		select {
		case c <- struct{}{}:
		default:
		}
	}
	h.mu.Unlock()
}

// drop closes all connections of pid without a final event.
func (h *hub) drop(pid string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[pid] {
		s.close()
	}
	delete(h.subs, pid)
}

// sendHumans is send restricted to human connections (agents only get wake/deleted/replaced).
func (h *hub) sendHumans(pid string, f frame) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[pid] {
		if s.agent {
			continue
		}
		select {
		case s.ch <- f:
		default:
			s.close()
			delete(h.subs[pid], s)
		}
	}
}

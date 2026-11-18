package server

import (
	"sync"
	"time"
)

// limiter is a fixed-window counter keyed by string; small and in memory.
type limiter struct {
	mu sync.Mutex
	m  map[string]*window
}

type window struct {
	start time.Time
	per   time.Duration
	n     int
}

func newLimiter() *limiter { return &limiter{m: map[string]*window{}} }

func (l *limiter) get(key string, per time.Duration) *window {
	w := l.m[key]
	if w == nil || time.Since(w.start) >= per {
		w = &window{start: time.Now(), per: per}
		l.m[key] = w
	}
	return w
}

// allow counts one event and reports whether it is within max per window.
func (l *limiter) allow(key string, max int, per time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.get(key, per)
	if w.n >= max {
		return false
	}
	w.n++
	return true
}

// peek reports whether one more event would be allowed, without counting it.
func (l *limiter) peek(key string, max int, per time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.get(key, per).n < max
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	delete(l.m, key)
	l.mu.Unlock()
}

func (l *limiter) sweep() {
	l.mu.Lock()
	for k, w := range l.m {
		if time.Since(w.start) >= w.per {
			delete(l.m, k)
		}
	}
	l.mu.Unlock()
}

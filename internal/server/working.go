package server

import (
	"sort"
	"sync"
	"time"
)

// workingTTL bounds how long a run is shown as in progress without a /runs report.
const workingTTL = 65 * time.Minute

type workEntry struct {
	startedAt int64
	timer     *time.Timer
}

// working tracks in-memory "agent is running" state per group; it is not persisted.
type working struct {
	mu sync.Mutex
	m  map[string]map[string]*workEntry // group -> agent -> entry
}

func newWorking() *working { return &working{m: map[string]map[string]*workEntry{}} }

// start marks agent as working in gid; onExpire runs if no finish arrives within workingTTL.
func (w *working) start(gid, agent string, at int64, onExpire func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	g := w.m[gid]
	if g == nil {
		g = map[string]*workEntry{}
		w.m[gid] = g
	}
	if old := g[agent]; old != nil {
		old.timer.Stop()
	}
	e := &workEntry{startedAt: at}
	e.timer = time.AfterFunc(workingTTL, func() {
		if w.finishEntry(gid, agent, e) {
			onExpire()
		}
	})
	g[agent] = e
}

// finish clears the entry and reports whether there was one.
func (w *working) finish(gid, agent string) bool { return w.finishEntry(gid, agent, nil) }

func (w *working) finishEntry(gid, agent string, only *workEntry) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	e := w.m[gid][agent]
	if e == nil || (only != nil && e != only) {
		return false
	}
	e.timer.Stop()
	delete(w.m[gid], agent)
	if len(w.m[gid]) == 0 {
		delete(w.m, gid)
	}
	return true
}

// list returns the working agents of gid ordered by start time.
func (w *working) list(gid string) []obj {
	w.mu.Lock()
	out := []obj{}
	for id, e := range w.m[gid] {
		out = append(out, obj{"agentId": id, "startedAt": e.startedAt})
	}
	w.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i]["startedAt"].(int64), out[j]["startedAt"].(int64)
		return a < b || (a == b && out[i]["agentId"].(string) < out[j]["agentId"].(string))
	})
	return out
}

func (w *working) ids(gid string) []string {
	l := w.list(gid)
	out := make([]string, len(l))
	for i, x := range l {
		out[i] = x["agentId"].(string)
	}
	return out
}

func (w *working) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, g := range w.m {
		for _, e := range g {
			e.timer.Stop()
		}
	}
	w.m = map[string]map[string]*workEntry{}
}

// broadcastWorking pushes a `working` event to the human members of gid (startedAt 0 = finished).
func (s *Server) broadcastWorking(gid, agent string, startedAt int64) {
	humans, err := scanStrings(s.db.R.Query(`SELECT m.principal_id FROM members m JOIN principals p ON p.id=m.principal_id WHERE m.group_id=? AND p.kind='human'`, gid))
	if err != nil {
		s.log.Error("working broadcast", "err", err)
		return
	}
	f := encodeEvent(0, "working", obj{"groupId": gid, "agentId": agent, "startedAt": startedAt})
	for _, pid := range humans {
		s.hub.sendHumans(pid, f)
	}
}

func (s *Server) startRun(c *ctx) (any, error) {
	var req struct{ GroupID string }
	if err := c.decode(&req); err != nil {
		return nil, err
	}
	if _, err := s.role(s.db.R, c.a, req.GroupID); err != nil {
		return nil, err
	}
	gid, agent, at := req.GroupID, c.a.ID, now()
	s.work.start(gid, agent, at, func() { s.broadcastWorking(gid, agent, 0) })
	s.broadcastWorking(gid, agent, at)
	return obj{"startedAt": at}, nil
}

// finishRun clears the working state of the caller in gid, if any.
func (s *Server) finishRun(gid, agent string) {
	if gid != "" && s.work.finish(gid, agent) {
		s.broadcastWorking(gid, agent, 0)
	}
}

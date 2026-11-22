package server

import (
	"strings"
	"testing"
	"time"
)

// An agent's reply to another agent wakes it only when the reply asks something (v0.4.0).
func TestQuestionReplyWakes(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	a1, a2 := e.agent(alice, "A1", gid), e.agent(alice, "A2", gid)
	m := a1.send(gid, map[string]any{"text": "我做完了第一步", "ignoreNew": true})["message"].(map[string]any)
	seq := m["seq"]

	for _, c := range []struct {
		text string
		wake bool
	}{
		{"好的", false},
		{"已合入 master，见 `a?b` 和 https://x.test/p?q=1", false},
		{"10:30 后直接跑全量，是否就是这个安排？", true},
		{"请确认阈值改成 17", true},
		{"Is that the plan?", true},
	} {
		r := a2.send(gid, map[string]any{"text": c.text, "replyTo": seq, "ignoreNew": true})
		woke := len(r["woken"].([]any)) == 1
		if woke != c.wake {
			t.Fatalf("%q: woken=%v want %v (%v)", c.text, woke, c.wake, r)
		}
		if c.wake {
			got := r["message"].(map[string]any)["replyWakes"]
			if rw := strs(got); len(rw) != 1 || rw[0] != a1.id {
				t.Fatalf("%q: replyWakes %v", c.text, got)
			}
		}
	}
	if isQuestionText("```\nwhat?\n```") || !isQuestionText("要不要先停") {
		t.Fatal("isQuestionText")
	}
}

func TestAlarms(t *testing.T) {
	e := newEnv(t)
	e.s.AlarmMinDelay = 0
	alice := e.human("alice")
	gid := alice.group("g")
	a1 := e.agent(alice, "A1", gid)

	if r := a1.do("POST", "/alarms", map[string]any{"groupId": gid}); r.status != 400 {
		t.Fatalf("no time: %d", r.status)
	}
	if r := a1.do("POST", "/alarms", map[string]any{"groupId": gid, "inMinutes": 8 * 24 * 60}); r.status != 400 {
		t.Fatalf("too far: %d", r.status)
	}
	if r := alice.do("POST", "/alarms", map[string]any{"groupId": gid, "inMinutes": 5}); r.status != 403 {
		t.Fatalf("human: %d", r.status)
	}
	al := a1.ok("POST", "/alarms", map[string]any{"groupId": gid, "inMinutes": 30, "note": "查进度"})["alarm"].(map[string]any)
	if l := a1.ok("GET", "/alarms", nil)["alarms"].([]any); len(l) != 1 {
		t.Fatalf("list: %v", l)
	}
	for i := 0; i < maxAlarmsPerAgent-1; i++ {
		a1.ok("POST", "/alarms", map[string]any{"groupId": gid, "at": "23:59"})
	}
	if r := a1.do("POST", "/alarms", map[string]any{"groupId": gid, "inMinutes": 5}); r.status != 409 {
		t.Fatalf("cap: %d", r.status)
	}
	a1.ok("DELETE", "/alarms/"+al["id"].(string), nil)

	// A restricted run may not set alarms.
	rt := a1.ok("POST", "/run-tokens", map[string]any{"groupId": gid})
	if rt["tier"] != "restricted" {
		t.Fatalf("tier: %v", rt)
	}
	rc := &client{e: e, hc: a1.hc, token: rt["token"].(string), id: a1.id}
	if r := rc.do("POST", "/alarms", map[string]any{"inMinutes": 5}); r.status != 403 {
		t.Fatalf("restricted alarm: %d", r.status)
	}

	// Due alarm: a system message addressed to the agent, the alarm row gone, the agent's inbox hit.
	e.s.db.W.Exec(`UPDATE alarms SET fire_at=? WHERE agent_id=?`, now()-1000, a1.id)
	e.s.db.W.Exec(`DELETE FROM alarms WHERE id IN (SELECT id FROM alarms WHERE agent_id=? ORDER BY id LIMIT 3)`, a1.id)
	e.s.fireAlarms()
	if l := a1.ok("GET", "/alarms", nil)["alarms"].([]any); len(l) != 0 {
		t.Fatalf("left: %v", l)
	}
	last := lastMessage(t, alice, gid)
	if last["kind"] != "system" || !strings.Contains(last["text"].(string), "定时到了") || strs(last["mentions"])[0] != a1.id {
		t.Fatalf("alarm message: %v", last)
	}
	if meta := last["meta"].(map[string]any); meta["type"] != "alarm" || meta["agentId"] != a1.id {
		t.Fatalf("meta: %v", meta)
	}
	in := a1.ok("GET", "/inbox", nil)["groups"].([]any)
	if len(in) != 1 || in[0].(map[string]any)["mentionsMe"].(float64) < 1 {
		t.Fatalf("inbox: %v", in)
	}
	// Firing again does nothing.
	n := alice.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)["lastSeq"]
	e.s.fireAlarms()
	if alice.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)["lastSeq"] != n {
		t.Fatal("fired twice")
	}
}

// An alarm set during a commander run keeps commander authority for the run it wakes.
func TestAlarmCommander(t *testing.T) {
	e := newEnv(t)
	e.s.AlarmMinDelay = 0
	alice := e.human("alice")
	gid := alice.group("g")
	a1 := e.agent(alice, "A1", gid)
	alice.send(gid, map[string]any{"text": "@A1 go"})
	time.Sleep(150 * time.Millisecond)
	var wid string
	e.s.db.R.QueryRow(`SELECT id FROM wakes WHERE agent_id=? ORDER BY created_at DESC LIMIT 1`, a1.id).Scan(&wid)
	rt := a1.ok("POST", "/run-tokens", map[string]any{"groupId": gid, "wakeId": wid})
	if rt["commander"] != true {
		t.Fatalf("rt: %v", rt)
	}
	rc := &client{e: e, hc: a1.hc, token: rt["token"].(string), id: a1.id}
	rc.ok("POST", "/alarms", map[string]any{"inMinutes": 1})
	a1.ok("POST", "/groups/"+gid+"/read", map[string]any{"seq": 100})
	e.s.db.W.Exec(`UPDATE alarms SET fire_at=?`, now()-1)
	e.s.fireAlarms()
	time.Sleep(150 * time.Millisecond)
	if r := a1.ok("POST", "/run-tokens", map[string]any{"groupId": gid}); r["commander"] != true {
		t.Fatalf("alarm run not commander: %v", r)
	}
}

func TestJobs(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	a1 := e.agent(alice, "A1", gid)
	r := a1.ok("POST", "/jobs", map[string]any{"groupId": gid, "label": "全量筛选"})
	job, secret := r["job"].(map[string]any), r["secret"].(string)
	id := job["id"].(string)
	if r := e.anon().do("POST", "/jobs/"+id+"/done", map[string]any{"secret": "wrong", "exitCode": 0}); r.status != 404 {
		t.Fatalf("bad secret: %d", r.status)
	}
	res := e.anon().ok("POST", "/jobs/"+id+"/done", map[string]any{"secret": secret, "exitCode": 2, "tail": "line1\n```evil```\nTraceback: boom"})
	if res["status"] != "failed" {
		t.Fatalf("status: %v", res)
	}
	last := lastMessage(t, alice, gid)
	txt := last["text"].(string)
	if !strings.Contains(txt, "后台任务失败") || !strings.Contains(txt, "退出码 2") || !strings.Contains(txt, "boom") || strings.Count(txt, "```") != 2 {
		t.Fatalf("job message: %q", txt)
	}
	if strs(last["mentions"])[0] != a1.id || last["meta"].(map[string]any)["type"] != "job" {
		t.Fatalf("job message meta: %v", last)
	}
	if r := e.anon().ok("POST", "/jobs/"+id+"/done", map[string]any{"secret": secret, "exitCode": 0}); r["duplicate"] != true {
		t.Fatalf("second report: %v", r)
	}
	// A job that never reports is marked lost after its timeout.
	j2 := a1.ok("POST", "/jobs", map[string]any{"groupId": gid, "label": "x", "timeoutMin": 1})["job"].(map[string]any)
	e.s.db.W.Exec(`UPDATE jobs SET started_at=? WHERE id=?`, now()-2*60000, j2["id"])
	e.s.expireJobs()
	if !strings.Contains(lastMessage(t, alice, gid)["text"].(string), "超时没有回报") {
		t.Fatal("lost job not reported")
	}
	jl := a1.ok("GET", "/jobs", nil)["jobs"].([]any)
	st := map[string]any{}
	for _, x := range jl {
		st[x.(map[string]any)["id"].(string)] = x.(map[string]any)["status"]
	}
	if len(jl) != 2 || st[j2["id"].(string)] != "lost" || st[id] != "failed" {
		t.Fatalf("jobs: %v", jl)
	}
}

func TestDecisions(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	bob := e.human("bob")
	gid := alice.group("g", bob)
	a1 := e.agent(alice, "A1", gid)

	if r := a1.do("POST", "/groups/"+gid+"/decisions", map[string]any{"question": "q", "options": []string{"only"}}); r.status != 400 {
		t.Fatalf("one option: %d", r.status)
	}
	if r := a1.do("POST", "/groups/"+gid+"/decisions", map[string]any{"question": "q", "options": []string{"a", "b"}, "to": "A1"}); r.status != 400 {
		t.Fatalf("agent decides: %d", r.status)
	}
	r := a1.ok("POST", "/groups/"+gid+"/decisions", map[string]any{"question": "内存阈值提到 17 GiB？", "options": []string{"提到 17", "维持 15"}, "refs": []int{3}})
	d := r["decision"].(map[string]any)
	msg := r["message"].(map[string]any)
	if d["status"] != "open" || d["askTo"].(map[string]any)["id"] != alice.id || !strings.Contains(msg["text"].(string), "【待你决定】") {
		t.Fatalf("decision: %v %v", d, msg)
	}
	if strs(msg["mentions"])[0] != alice.id {
		t.Fatalf("not @'d: %v", msg)
	}
	// The message carries its decision; alice's list has it, bob's (mine) doesn't.
	if got := lastMessage(t, alice, gid)["decision"].(map[string]any); got["id"] != d["id"] {
		t.Fatalf("message decision: %v", got)
	}
	if l := alice.ok("GET", "/decisions", nil)["decisions"].([]any); len(l) != 1 {
		t.Fatalf("alice list: %v", l)
	}
	if l := bob.ok("GET", "/decisions", nil)["decisions"].([]any); len(l) != 0 {
		t.Fatalf("bob list: %v", l)
	}
	if l := bob.ok("GET", "/decisions?scope=all", nil)["decisions"].([]any); len(l) != 1 {
		t.Fatalf("bob all: %v", l)
	}
	if r := bob.do("POST", "/decisions/"+d["id"].(string)+"/answer", map[string]any{"option": 0}); r.status != 403 {
		t.Fatalf("bob answers: %d", r.status)
	}
	if r := alice.do("POST", "/decisions/"+d["id"].(string)+"/answer", map[string]any{"option": 5}); r.status != 400 {
		t.Fatalf("bad option: %d", r.status)
	}
	a1.ok("POST", "/groups/"+gid+"/read", map[string]any{"seq": 1000})
	ans := alice.ok("POST", "/decisions/"+d["id"].(string)+"/answer", map[string]any{"option": 0, "text": "跑完告诉我"})
	am := ans["message"].(map[string]any)
	if am["replyTo"].(float64) != msg["seq"].(float64) || !strings.Contains(am["text"].(string), "决定：提到 17。跑完告诉我") {
		t.Fatalf("answer message: %v", am)
	}
	if ad := ans["decision"].(map[string]any); ad["status"] != "answered" || ad["answer"] != "提到 17。跑完告诉我" {
		t.Fatalf("answered: %v", ad)
	}
	time.Sleep(150 * time.Millisecond)
	if rt := a1.ok("POST", "/run-tokens", map[string]any{"groupId": gid}); rt["commander"] != true {
		t.Fatalf("answer doesn't command: %v", rt)
	}
	if r := alice.do("POST", "/decisions/"+d["id"].(string)+"/answer", map[string]any{"option": 1}); r.status != 409 {
		t.Fatalf("answer twice: %d", r.status)
	}
	// Cancel: the agent's own open decision.
	d2 := a1.ok("POST", "/groups/"+gid+"/decisions", map[string]any{"question": "q2", "options": []string{"a", "b"}})["decision"].(map[string]any)
	if r := bob.do("DELETE", "/decisions/"+d2["id"].(string), nil); r.status != 403 {
		t.Fatalf("bob cancels: %d", r.status)
	}
	if c := a1.ok("DELETE", "/decisions/"+d2["id"].(string), nil)["decision"].(map[string]any); c["status"] != "canceled" {
		t.Fatalf("cancel: %v", c)
	}
}

func TestRunMaxMinutes(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	a1 := e.agent(alice, "A1", "")
	if r := alice.do("PATCH", "/agents/"+a1.id, map[string]any{"runMaxMinutes": 5}); r.status != 400 {
		t.Fatalf("too short: %d", r.status)
	}
	ag := alice.ok("PATCH", "/agents/"+a1.id, map[string]any{"runMaxMinutes": 180})["agent"].(map[string]any)
	if ag["runMaxMinutes"].(float64) != 180 {
		t.Fatalf("agent: %v", ag)
	}
	if me := a1.ok("GET", "/me", nil); me["runMaxMinutes"].(float64) != 180 {
		t.Fatalf("me: %v", me)
	}
	if ag := alice.ok("PATCH", "/agents/"+a1.id, map[string]any{"runMaxMinutes": 0})["agent"].(map[string]any); ag["runMaxMinutes"].(float64) != defaultRunMaxMin {
		t.Fatalf("reset: %v", ag)
	}
}

func TestParseAlarmTime(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	n := time.Date(2026, 11, 22, 9, 0, 0, 0, loc)
	if at, _ := parseAlarmTime(alarmReq{At: "10:30"}, n); !at.Equal(time.Date(2026, 11, 22, 10, 30, 0, 0, loc)) {
		t.Fatalf("today: %v", at)
	}
	if at, _ := parseAlarmTime(alarmReq{At: "08:00"}, n); !at.Equal(time.Date(2026, 11, 23, 8, 0, 0, 0, loc)) {
		t.Fatalf("tomorrow: %v", at)
	}
	if _, err := parseAlarmTime(alarmReq{At: "10点半"}, n); err == nil {
		t.Fatal("bad format accepted")
	}
}

// An agent's ignoreNew reply must not mark messages that arrived during its run as read (a job
// result or an @ posted mid-run would otherwise never reach it).
func TestReplyKeepsMidRunUnread(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	a1 := e.agent(alice, "A1", gid)
	alice.send(gid, map[string]any{"text": "@A1 first"})
	a1.ok("GET", "/groups/"+gid+"/messages", nil) // the run reads #3
	alice.send(gid, map[string]any{"text": "@A1 second, during the run"})
	a1.send(gid, map[string]any{"text": "done with first", "replyTo": 3, "ignoreNew": true})
	in := a1.ok("GET", "/inbox", nil)["groups"].([]any)
	if len(in) != 1 || in[0].(map[string]any)["mentionsMe"].(float64) != 1 {
		t.Fatalf("mid-run @ lost: %v", in)
	}
	// With nothing unread from others, sending marks read as before.
	a1.ok("GET", "/groups/"+gid+"/messages", nil)
	a1.send(gid, map[string]any{"text": "done with second"})
	if in := a1.ok("GET", "/inbox", nil)["groups"].([]any); len(in) != 0 {
		t.Fatalf("still unread: %v", in)
	}
}

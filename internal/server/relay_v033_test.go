package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func limitsOf(r map[string]any) map[string]any {
	return r["group"].(map[string]any)["limits"].(map[string]any)
}

func lastMessage(t *testing.T, h *client, gid string) map[string]any {
	t.Helper()
	ms := h.ok("GET", "/groups/"+gid+"/messages?before=100000&limit=1", nil)["messages"].([]any)
	return ms[0].(map[string]any)
}

func countSystem(t *testing.T, h *client, gid, substr string) int {
	t.Helper()
	n := 0
	for _, x := range h.ok("GET", "/groups/"+gid+"/messages?before=100000&limit=100", nil)["messages"].([]any) {
		m := x.(map[string]any)
		if m["kind"] == "system" && strings.Contains(m["text"].(string), substr) {
			n++
		}
	}
	return n
}

// hopLimit -1 lets agents relay without a cap; hops keep counting for display (v0.3.3).
func TestUnlimitedHops(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	a1, a2 := e.agent(alice, "A1", gid), e.agent(alice, "A2", gid)

	if r := alice.do("PATCH", "/groups/"+gid, map[string]any{"hopLimit": -2}); r.status != 400 {
		t.Fatalf("-2: %d", r.status)
	}
	l := limitsOf(alice.ok("PATCH", "/groups/"+gid, map[string]any{"hopLimit": -1}))
	if l["hopLimit"].(float64) != -1 || l["unlimitedHops"] != true || l["default"] != false {
		t.Fatalf("unlimited limits: %v", l)
	}
	l = limitsOf(alice.ok("GET", "/groups/"+gid, nil))
	if l["unlimitedHops"] != true || l["agentWakesPerAgentHour"].(float64) != agentWakesPerAgentHour {
		t.Fatalf("GET limits: %v", l)
	}

	alice.send(gid, map[string]any{"text": "@A1 start"})
	pair, nicks := []*client{a1, a2}, []string{"@A2", "@A1"}
	for i := 0; i < hopLimit+4; i++ {
		r := pair[i%2].send(gid, map[string]any{"text": nicks[i%2] + " pass", "ignoreNew": true})
		if why := notWokenReasons(r); len(why) != 0 {
			t.Fatalf("hop %d not woken: %v", i+1, why)
		}
		if len(r["woken"].([]any)) != 1 {
			t.Fatalf("hop %d: %v", i+1, r)
		}
		time.Sleep(80 * time.Millisecond) // let the wake fire so the next message's hop builds on it
		if hop := r["message"].(map[string]any)["hop"].(float64); hop < float64(i) {
			t.Fatalf("hop %d counted as %v", i+1, hop)
		}
	}
	if n := countSystem(t, alice, gid, "接力已到上限"); n != 0 {
		t.Fatalf("hop notice with unlimited hops: %d", n)
	}

	// Back to the default: the cap applies again.
	l = limitsOf(alice.ok("PATCH", "/groups/"+gid, map[string]any{"hopLimit": 0}))
	if l["hopLimit"].(float64) != hopLimit || l["unlimitedHops"] != false || l["default"] != true {
		t.Fatalf("reset: %v", l)
	}
	r := a1.send(gid, map[string]any{"text": "@A2 more", "ignoreNew": true})
	if notWokenReasons(r)[a2.id] != "hop_limit" {
		t.Fatalf("hop cap back: %v", r)
	}
}

// Each agent has an hourly budget of wakes caused by agent messages, per group (v0.3.3).
func TestPerAgentWakeBudget(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	gid := alice.group("g", bob)
	a1, a2, a3 := e.agent(alice, "A1", gid), e.agent(alice, "A2", gid), e.agent(bob, "B3", gid)
	other := alice.group("g2")
	alice.ok("POST", "/groups/"+other+"/members", map[string]any{"agentId": a1.id})
	alice.ok("POST", "/groups/"+other+"/members", map[string]any{"agentId": a2.id})

	for _, v := range []int{-1, maxAgentWakesPerAgentHour + 1} {
		if r := alice.do("PATCH", "/groups/"+gid, map[string]any{"agentWakesPerAgentHour": v}); r.status != 400 {
			t.Fatalf("bad budget %d: %d", v, r.status)
		}
	}
	if r := bob.do("PATCH", "/groups/"+gid, map[string]any{"agentWakesPerAgentHour": 5}); r.status != 403 {
		t.Fatalf("member patch: %d", r.status)
	}
	l := limitsOf(alice.ok("PATCH", "/groups/"+gid, map[string]any{"agentWakesPerAgentHour": 3, "hopLimit": -1}))
	if l["agentWakesPerAgentHour"].(float64) != 3 || l["default"] != false {
		t.Fatalf("patched: %v", l)
	}

	// A2 can be woken 3 times by agents (A1 and B3 share A2's budget), then agent_budget.
	senders := []*client{a1, a3, a1}
	for i, s := range senders {
		r := s.send(gid, map[string]any{"text": "@A2 ping", "ignoreNew": true})
		if len(notWokenReasons(r)) != 0 {
			t.Fatalf("wake %d: %v", i+1, r)
		}
	}
	r := a3.send(gid, map[string]any{"text": "@A2 ping", "ignoreNew": true})
	if notWokenReasons(r)[a2.id] != "agent_budget" {
		t.Fatalf("expected agent_budget: %v", r)
	}
	last := lastMessage(t, alice, gid)
	want := "A2 这一小时被其他 agent 唤醒的次数到上限了（3 次），需要人类 @ 才能继续；群主或管理员可以在群设置里调整"
	if last["kind"] != "system" || last["text"] != want {
		t.Fatalf("notice: %v", last)
	}
	// The notice is posted at most once per 30 minutes per agent.
	r = a1.send(gid, map[string]any{"text": "@A2 again", "ignoreNew": true})
	if notWokenReasons(r)[a2.id] != "agent_budget" {
		t.Fatalf("still over budget: %v", r)
	}
	if n := countSystem(t, alice, gid, "这一小时被其他 agent 唤醒"); n != 1 {
		t.Fatalf("notices: %d", n)
	}
	// Other agents and other groups keep their own budgets; humans are not limited by it.
	if r := a2.send(gid, map[string]any{"text": "@A1 hi", "ignoreNew": true}); len(notWokenReasons(r)) != 0 {
		t.Fatalf("A1 budget separate: %v", r)
	}
	if r := a1.send(other, map[string]any{"text": "@A2 hi", "ignoreNew": true}); len(notWokenReasons(r)) != 0 {
		t.Fatalf("other group budget separate: %v", r)
	}
	if r := bob.send(gid, map[string]any{"text": "@A2 hi"}); len(r["woken"].([]any)) != 1 {
		t.Fatalf("human wake: %v", r)
	}
	// Owner budget stays the outer cap: per-agent budget high, owner budget 2.
	alice.ok("PATCH", "/groups/"+other, map[string]any{"agentWakesPerHour": 2, "hopLimit": -1})
	e.s.rl.reset(budgetKey(alice.id, other))
	a1.send(other, map[string]any{"text": "@A2 x", "ignoreNew": true})
	a2.send(other, map[string]any{"text": "@A1 x", "ignoreNew": true})
	if r := a1.send(other, map[string]any{"text": "@A2 x", "ignoreNew": true}); notWokenReasons(r)[a2.id] != "budget" {
		t.Fatalf("owner budget: %v", r)
	}
	if !e.s.rl.peek(agentBudgetKey(a2.id, other), agentWakesPerAgentHour, time.Hour) {
		t.Fatal("per-agent budget exhausted unexpectedly")
	}
}

// v0.3.3: an agent's reply no longer wakes the agent it replies to; @, human replies and DMs do.
func TestAgentReplyDoesNotWake(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	gid := alice.group("g", bob)
	a1, a2 := e.agent(alice, "A1", gid), e.agent(alice, "A2", gid)

	s1 := a1.send(gid, map[string]any{"text": "a1 says", "ignoreNew": true})["message"].(map[string]any)["seq"]
	// Agent replying to an agent: not a target at all (no wake, no reply mention).
	r := a2.send(gid, map[string]any{"text": "a2 answers", "replyTo": s1, "ignoreNew": true})
	if len(r["woken"].([]any)) != 0 || len(r["notWoken"].([]any)) != 0 {
		t.Fatalf("agent reply woke: %v", r)
	}
	var n int
	e.s.db.R.QueryRow(`SELECT COUNT(*) FROM mentions WHERE target_id=? AND group_id=?`, a1.id, gid).Scan(&n)
	if n != 0 {
		t.Fatalf("reply mention rows for agent: %d", n)
	}
	// Explicit @ still wakes, also together with replyTo (in text or mentions[]).
	r = a2.send(gid, map[string]any{"text": "@A1 please check", "replyTo": s1, "ignoreNew": true})
	if w := strs(r["woken"]); len(w) != 1 || w[0] != a1.id {
		t.Fatalf("@ wake: %v", r)
	}
	r = a2.send(gid, map[string]any{"text": "check", "mentions": []string{"A1"}, "replyTo": s1, "ignoreNew": true})
	if w := strs(r["woken"]); len(w) != 1 || w[0] != a1.id {
		t.Fatalf("mentions[] wake: %v", r)
	}
	// A human's reply to an agent still wakes it.
	r = bob.send(gid, map[string]any{"text": "thanks", "replyTo": s1})
	if w := strs(r["woken"]); len(w) != 1 || w[0] != a1.id {
		t.Fatalf("human reply: %v", r)
	}
	// An agent's reply to a human still notifies the human (mentionsMe).
	hs := bob.send(gid, map[string]any{"text": "question"})["message"].(map[string]any)["seq"]
	a1.send(gid, map[string]any{"text": "answer", "replyTo": hs, "ignoreNew": true})
	var reason string
	e.s.db.R.QueryRow(`SELECT reason FROM mentions WHERE target_id=? AND group_id=? ORDER BY seq DESC LIMIT 1`, bob.id, gid).Scan(&reason)
	if reason != "reply" {
		t.Fatalf("human reply mention: %q", reason)
	}
	// DMs between agents still wake.
	dm := a1.ok("POST", "/chats", map[string]any{"with": a2.id})["group"].(map[string]any)["id"].(string)
	r = a1.send(dm, map[string]any{"text": "hi", "ignoreNew": true})
	if w := strs(r["woken"]); len(w) != 1 || w[0] != a2.id {
		t.Fatalf("dm wake: %v", r)
	}
	ds := r["message"].(map[string]any)["seq"]
	r = a2.send(dm, map[string]any{"text": "re", "replyTo": ds, "ignoreNew": true})
	if w := strs(r["woken"]); len(w) != 1 || w[0] != a1.id {
		t.Fatalf("dm reply wake: %v", r)
	}
}

// Group collaboration rules: default text, owner/admin edits, reset (v0.3.3).
func TestGroupRules(t *testing.T) {
	e := newEnv(t)
	alice, bob, carol := e.human("alice"), e.human("bob"), e.human("carol")
	gid := alice.group("g", bob, carol)
	cc := e.agent(alice, "CC", gid)

	g := alice.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)
	if g["rules"] != defaultGroupRules || g["rulesDefault"] != true {
		t.Fatalf("default rules: %v %v", g["rules"], g["rulesDefault"])
	}
	if g = cc.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any); g["rules"] != defaultGroupRules {
		t.Fatalf("agent sees rules: %v", g["rules"])
	}
	for _, c := range []*client{bob, cc} {
		if r := c.do("PATCH", "/groups/"+gid, map[string]any{"rules": "x"}); r.status != 403 {
			t.Fatalf("non-manager patch: %d", r.status)
		}
	}
	if r := alice.do("PATCH", "/groups/"+gid, map[string]any{"rules": strings.Repeat("规", maxRulesChars+1)}); r.status != 400 {
		t.Fatalf("too long: %d", r.status)
	}
	g = alice.ok("PATCH", "/groups/"+gid, map[string]any{"rules": "  只说结论。\r\n不要互相致谢。 "})["group"].(map[string]any)
	if g["rules"] != "只说结论。\n不要互相致谢。" || g["rulesDefault"] != false {
		t.Fatalf("custom: %q %v", g["rules"], g["rulesDefault"])
	}
	// Admins may edit too; the max length is accepted.
	alice.ok("PATCH", "/groups/"+gid+"/members/"+bob.id, map[string]any{"role": "admin"})
	long := strings.Repeat("规", maxRulesChars)
	if g = bob.ok("PATCH", "/groups/"+gid, map[string]any{"rules": long})["group"].(map[string]any); g["rules"] != long {
		t.Fatal("admin edit")
	}
	// Other PATCH fields leave rules alone.
	if g = alice.ok("PATCH", "/groups/"+gid, map[string]any{"topic": "t"})["group"].(map[string]any); g["rules"] != long {
		t.Fatal("rules changed by topic patch")
	}
	// Empty string (恢复默认) resets; sending the default text also stores NULL.
	g = alice.ok("PATCH", "/groups/"+gid, map[string]any{"rules": ""})["group"].(map[string]any)
	if g["rules"] != defaultGroupRules || g["rulesDefault"] != true {
		t.Fatalf("reset: %v", g["rulesDefault"])
	}
	alice.ok("PATCH", "/groups/"+gid, map[string]any{"rules": defaultGroupRules})
	var n int
	e.s.db.R.QueryRow(`SELECT COUNT(*) FROM groups WHERE id=? AND rules IS NULL`, gid).Scan(&n)
	if n != 1 {
		t.Fatal("default text stored")
	}
}

// GET /groups/{id} tells an agent who may command it in that group (v0.3.3).
func TestGroupCommanders(t *testing.T) {
	e := newEnv(t)
	alice, bob, carol := e.human("alice"), e.human("bob"), e.human("carol")
	gid := alice.group("g", bob, carol)
	other := alice.group("g2", bob)
	cc := e.agent(alice, "CC", gid)
	dd := e.agent(bob, "DD", gid)
	ee := e.agent(carol, "EE", gid)
	// bob commands CC in gid, the agent DD too; carol only in the other group, EE nowhere.
	alice.ok("PUT", "/agents/"+cc.id+"/grants", map[string]any{"userId": bob.id, "groupId": gid})
	alice.ok("PUT", "/agents/"+cc.id+"/grants", map[string]any{"userId": dd.id, "groupId": gid})
	alice.ok("PUT", "/agents/"+cc.id+"/grants", map[string]any{"userId": carol.id, "groupId": other})

	got := map[string]string{}
	for _, x := range cc.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)["commanders"].([]any) {
		c := x.(map[string]any)
		got[c["id"].(string)] = c["nick"].(string) + "/" + c["kind"].(string)
	}
	want := map[string]string{alice.id: "alice/human", bob.id: "bob/human", dd.id: "DD/agent"}
	if len(got) != len(want) {
		t.Fatalf("commanders: %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("commanders: %v", got)
		}
	}
	// Humans don't get the field; an agent with no grants sees only its owner.
	if _, ok := alice.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)["commanders"]; ok {
		t.Fatal("human sees commanders")
	}
	cs := ee.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)["commanders"].([]any)
	if len(cs) != 1 || cs[0].(map[string]any)["id"] != carol.id {
		t.Fatalf("EE commanders: %v", cs)
	}
	// Run tokens see the same list.
	rt := cc.ok("POST", "/run-tokens", map[string]any{"groupId": gid})["token"].(string)
	run := &client{e: e, hc: http.DefaultClient, token: rt, id: cc.id}
	if cs := run.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)["commanders"].([]any); len(cs) != 3 {
		t.Fatalf("run commanders: %v", cs)
	}
	// A disabled commanding agent drops out.
	bob.ok("PATCH", "/agents/"+dd.id, map[string]any{"disabled": true})
	if cs := cc.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)["commanders"].([]any); len(cs) != 2 {
		t.Fatalf("after disable: %v", cs)
	}
}

// Agents can't post their [SILENT] marker; humans can, and agents may still confirm (v0.3.3).
func TestSilentRejected(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	gid := alice.group("g")
	cc := e.agent(alice, "CC", gid)
	for _, text := range []string{"[SILENT]", "  [silent] nothing to add", "[静默] 没有新信息", "【静默】", "SILENT",
		"（回复 #12）[SILENT]", "（回复 #3）（回复 #4） 【静默】 无需回复"} {
		r := cc.do("POST", "/groups/"+gid+"/messages", map[string]any{"text": text, "ignoreNew": true})
		if r.status != 400 || r.errCode() != "silent" {
			t.Fatalf("%q: %d %s", text, r.status, r.raw)
		}
		if !strings.Contains(r.raw, "以 [SILENT] 开头的回复不会发出") {
			t.Fatalf("message: %s", r.raw)
		}
	}
	for _, text := range []string{"收到", "SILENT mode is off", "我觉得 [SILENT] 用得太多", "（回复 #x）[SILENT]"} {
		cc.send(gid, map[string]any{"text": text, "ignoreNew": true})
	}
	alice.send(gid, map[string]any{"text": "[SILENT]"})
}

func TestIsSilentText(t *testing.T) {
	cases := map[string]bool{
		"[SILENT]": true, "\n[SILENT]\n": true, "[silent]": true, "[静默] 因为": true, "【静默】": true, "SILENT": true,
		"(回复 #7) [SILENT]": true, "（ 回复 #7 ）[SILENT]": true, "（回复 #1）（回复 #2）SILENT": true,
		"": false, "silent": false, "SILENT.": false, "[Silent]": false, "好的 [SILENT]": false, "（回复 #）[SILENT]": false, "（回复 #1）": false,
	}
	for in, want := range cases {
		if got := isSilentText(in); got != want {
			t.Errorf("isSilentText(%q) = %v", in, got)
		}
	}
}

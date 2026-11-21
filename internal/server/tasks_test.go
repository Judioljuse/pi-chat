package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func taskOf(r map[string]any) map[string]any { return r["task"].(map[string]any) }

func taskIDs(r map[string]any) []string {
	var out []string
	for _, x := range r["tasks"].([]any) {
		out = append(out, x.(map[string]any)["id"].(string))
	}
	return out
}

func TestTasksHTTPPermissions(t *testing.T) {
	e := newEnv(t)
	alice, bob, carol, dave := e.human("alice"), e.human("bob"), e.human("carol"), e.human("dave")
	g := alice.group("g", bob, dave)
	alice.ok("PATCH", "/groups/"+g+"/members/"+dave.id, map[string]any{"role": "admin"})
	cc := e.agent(alice, "CC", g)
	bobAgent := e.agent(bob, "BB", g)
	other := carol.group("other")

	// Human member creates; defaults, cleaning, JSON shape.
	m := alice.send(g, map[string]any{"text": "请修一下登录页", "ignoreNew": true})
	seq := m["message"].(map[string]any)["seq"].(float64)
	t1 := taskOf(alice.ok("POST", "/groups/"+g+"/tasks", map[string]any{"title": "  修\n登录页  ", "note": "细节\x07", "assigneeId": cc.id, "sourceSeq": seq}))
	if t1["title"] != "修 登录页" || t1["note"] != "细节" || t1["status"] != "todo" || t1["groupId"] != g || t1["groupName"] != "g" ||
		t1["sourceSeq"].(float64) != seq || t1["doneAt"] != nil || !strings.HasPrefix(t1["id"].(string), "t_") {
		t.Fatalf("create: %v", t1)
	}
	if as := t1["assignee"].(map[string]any); as["id"] != cc.id || as["kind"] != "agent" || as["name"] != "CC" {
		t.Fatalf("assignee: %v", as)
	}
	if cb := t1["createdBy"].(map[string]any); cb["id"] != alice.id || cb["name"] != "alice" {
		t.Fatalf("createdBy: %v", cb)
	}
	id1 := t1["id"].(string)

	// Validation: empty/long title, bad status, bad sourceSeq, assignee not a member.
	for _, body := range []map[string]any{
		{"title": " \n "},
		{"title": strings.Repeat("长", 201)},
		{"title": "x", "note": strings.Repeat("n", 4001)},
		{"title": "x", "status": "later"},
		{"title": "x", "sourceSeq": 9999},
		{"title": "x", "assigneeId": carol.id},
	} {
		if r := alice.do("POST", "/groups/"+g+"/tasks", body); r.status != 400 {
			t.Fatalf("%v -> %d %s", body, r.status, r.raw)
		}
	}
	// Non-members can't see or touch anything.
	if r := carol.do("POST", "/groups/"+g+"/tasks", map[string]any{"title": "x"}); r.status != 404 {
		t.Fatalf("non-member create: %d", r.status)
	}
	if r := carol.do("GET", "/groups/"+g+"/tasks", nil); r.status != 404 {
		t.Fatalf("non-member list: %d", r.status)
	}
	if r := carol.do("PATCH", "/tasks/"+id1, map[string]any{"status": "done"}); r.status != 404 {
		t.Fatalf("non-member patch: %d", r.status)
	}
	if r := carol.do("DELETE", "/tasks/"+id1, nil); r.status != 404 {
		t.Fatalf("non-member delete: %d", r.status)
	}

	// Any human member edits any task; done sets doneAt, reopening clears it; null unassigns.
	t1 = taskOf(bob.ok("PATCH", "/tasks/"+id1, map[string]any{"status": "done", "title": "修登录页"}))
	if t1["status"] != "done" || t1["doneAt"] == nil || t1["title"] != "修登录页" || t1["updatedBy"].(map[string]any)["id"] != bob.id {
		t.Fatalf("bob patch: %v", t1)
	}
	t1 = taskOf(bob.ok("PATCH", "/tasks/"+id1, map[string]any{"status": "doing", "assigneeId": nil}))
	if t1["doneAt"] != nil || t1["assignee"] != nil {
		t.Fatalf("reopen/unassign: %v", t1)
	}
	if r := bob.do("PATCH", "/tasks/"+id1, map[string]any{"assigneeId": carol.id}); r.status != 400 {
		t.Fatalf("reassign to non-member: %d", r.status)
	}
	if r := bob.do("PATCH", "/tasks/"+id1, map[string]any{"status": "x"}); r.status != 400 {
		t.Fatalf("bad status: %d", r.status)
	}

	// Agents on their own token: create; edit own / unassigned / created by them; not others'.
	tb := taskOf(alice.ok("POST", "/groups/"+g+"/tasks", map[string]any{"title": "bob 的事", "assigneeId": bob.id}))
	if r := cc.do("PATCH", "/tasks/"+tb["id"].(string), map[string]any{"status": "done"}); r.status != 403 {
		t.Fatalf("agent edits other's task: %d", r.status)
	}
	cc.ok("PATCH", "/tasks/"+id1, map[string]any{"status": "blocked"}) // unassigned
	tc := taskOf(cc.ok("POST", "/groups/"+g+"/tasks", map[string]any{"title": "CC 自己建的", "assigneeId": bob.id}))
	cc.ok("PATCH", "/tasks/"+tc["id"].(string), map[string]any{"status": "doing"}) // created by CC
	tmine := taskOf(alice.ok("POST", "/groups/"+g+"/tasks", map[string]any{"title": "给 CC", "assigneeId": cc.id}))
	cc.ok("PATCH", "/tasks/"+tmine["id"].(string), map[string]any{"status": "doing"})
	if r := bobAgent.do("PATCH", "/tasks/"+tmine["id"].(string), map[string]any{"status": "done"}); r.status != 403 {
		t.Fatalf("other agent edits CC's task: %d", r.status)
	}

	// Restricted run token: read-only. Commander run token: may create.
	rr := runToken(t, e, cc, g, "restricted")
	if r := rr.do("GET", "/groups/"+g+"/tasks", nil); r.status != 200 || len(taskIDs(r.body)) != 4 {
		t.Fatalf("restricted list: %d %s", r.status, r.raw)
	}
	if r := rr.do("POST", "/groups/"+g+"/tasks", map[string]any{"title": "x"}); r.status != 403 {
		t.Fatalf("restricted create: %d", r.status)
	}
	if r := rr.do("PATCH", "/tasks/"+tmine["id"].(string), map[string]any{"status": "done"}); r.status != 403 {
		t.Fatalf("restricted patch: %d", r.status)
	}
	if r := rr.do("DELETE", "/tasks/"+tc["id"].(string), nil); r.status != 403 {
		t.Fatalf("restricted delete: %d", r.status)
	}
	cmd := runToken(t, e, cc, g, "project")
	cmd.ok("POST", "/groups/"+g+"/tasks", map[string]any{"title": "指挥者让建的"})
	// Commander run tokens stay inside the run's group: tasks of g2 are invisible to it.
	g2 := alice.group("g2", cc)
	t2 := taskOf(alice.ok("POST", "/groups/"+g2+"/tasks", map[string]any{"title": "别的群", "assigneeId": cc.id}))
	if r := cmd.do("PATCH", "/tasks/"+t2["id"].(string), map[string]any{"status": "done"}); r.status != 404 {
		t.Fatalf("commander patches other group's task: %d", r.status)
	}
	if r := cmd.do("DELETE", "/tasks/"+t2["id"].(string), nil); r.status != 404 {
		t.Fatalf("commander deletes other group's task: %d", r.status)
	}
	if r := cmd.do("GET", "/tasks?assignee=me&status=all", nil); r.status != 200 || strings.Contains(r.raw, t2["id"].(string)) {
		t.Fatalf("commander global list leaks other group: %d %s", r.status, r.raw)
	}
	cc.ok("PATCH", "/tasks/"+t2["id"].(string), map[string]any{"status": "doing"}) // the agent token itself still can

	// Delete: member who is not the creator 403; agent creator ok; admin ok; owner ok.
	if r := bob.do("DELETE", "/tasks/"+id1, nil); r.status != 403 {
		t.Fatalf("plain member deletes other's task: %d", r.status)
	}
	if r := bobAgent.do("DELETE", "/tasks/"+tc["id"].(string), nil); r.status != 403 {
		t.Fatalf("agent deletes other's task: %d", r.status)
	}
	cc.ok("DELETE", "/tasks/"+tc["id"].(string), nil)
	dave.ok("DELETE", "/tasks/"+tb["id"].(string), nil)
	alice.ok("DELETE", "/tasks/"+id1, nil)
	tbob := taskOf(bob.ok("POST", "/groups/"+g+"/tasks", map[string]any{"title": "bob 自己建"}))
	bob.ok("DELETE", "/tasks/"+tbob["id"].(string), nil)
	if r := alice.do("PATCH", "/tasks/"+id1, map[string]any{"status": "done"}); r.status != 404 {
		t.Fatalf("deleted task: %d", r.status)
	}
	_ = other
}

func TestTasksFiltersAndGlobal(t *testing.T) {
	e := newEnv(t)
	alice, bob, carol := e.human("alice"), e.human("bob"), e.human("carol")
	g1 := alice.group("g1", bob)
	g2 := bob.group("g2", alice)
	g3 := carol.group("g3")
	a1 := taskOf(alice.ok("POST", "/groups/"+g1+"/tasks", map[string]any{"title": "a1", "assigneeId": alice.id}))
	b1 := taskOf(alice.ok("POST", "/groups/"+g1+"/tasks", map[string]any{"title": "b1", "assigneeId": bob.id, "status": "blocked"}))
	u1 := taskOf(bob.ok("POST", "/groups/"+g1+"/tasks", map[string]any{"title": "u1"}))
	a2 := taskOf(bob.ok("POST", "/groups/"+g2+"/tasks", map[string]any{"title": "a2", "assigneeId": alice.id, "status": "done"}))
	a3 := taskOf(bob.ok("POST", "/groups/"+g2+"/tasks", map[string]any{"title": "a3", "assigneeId": alice.id, "status": "doing"}))
	carol.ok("POST", "/groups/"+g3+"/tasks", map[string]any{"title": "c", "assigneeId": carol.id})
	id := func(m map[string]any) string { return m["id"].(string) }

	// Group list: status and assignee filters, ordering doing > blocked > todo > done.
	if got := taskIDs(alice.ok("GET", "/groups/"+g1+"/tasks", nil)); fmt.Sprint(got) != fmt.Sprint([]string{id(b1), id(u1), id(a1)}) {
		t.Fatalf("g1 all: %v", got)
	}
	if got := taskIDs(alice.ok("GET", "/groups/"+g1+"/tasks?assignee=me", nil)); fmt.Sprint(got) != fmt.Sprint([]string{id(a1)}) {
		t.Fatalf("g1 me: %v", got)
	}
	if got := taskIDs(alice.ok("GET", "/groups/"+g1+"/tasks?assignee=none", nil)); fmt.Sprint(got) != fmt.Sprint([]string{id(u1)}) {
		t.Fatalf("g1 none: %v", got)
	}
	if got := taskIDs(alice.ok("GET", "/groups/"+g1+"/tasks?status=blocked", nil)); fmt.Sprint(got) != fmt.Sprint([]string{id(b1)}) {
		t.Fatalf("g1 blocked: %v", got)
	}
	if r := alice.do("GET", "/groups/"+g1+"/tasks?status=nope", nil); r.status != 400 {
		t.Fatalf("bad status: %d", r.status)
	}

	// Global: default open across my groups only; all includes done; by person.
	if got := taskIDs(alice.ok("GET", "/tasks?assignee=me", nil)); fmt.Sprint(got) != fmt.Sprint([]string{id(a3), id(a1)}) {
		t.Fatalf("me open: %v", got)
	}
	if got := taskIDs(alice.ok("GET", "/tasks?assignee=me&status=all", nil)); fmt.Sprint(got) != fmt.Sprint([]string{id(a3), id(a1), id(a2)}) {
		t.Fatalf("me all: %v", got)
	}
	if got := taskIDs(alice.ok("GET", "/tasks?assignee="+bob.id, nil)); fmt.Sprint(got) != fmt.Sprint([]string{id(b1)}) {
		t.Fatalf("by bob: %v", got)
	}
	all := alice.ok("GET", "/tasks?status=all", nil)
	if len(taskIDs(all)) != 5 {
		t.Fatalf("all mine: %v", taskIDs(all))
	}
	for _, x := range all["tasks"].([]any) {
		tk := x.(map[string]any)
		if tk["groupName"] != "g1" && tk["groupName"] != "g2" {
			t.Fatalf("group name: %v", tk)
		}
	}
	if got := taskIDs(carol.ok("GET", "/tasks?status=all", nil)); len(got) != 1 {
		t.Fatalf("carol sees: %v", got)
	}
	// openTasks count on the group.
	if n := alice.ok("GET", "/groups/"+g2, nil)["group"].(map[string]any)["openTasks"].(float64); n != 1 {
		t.Fatalf("openTasks: %v", n)
	}
	// Leaving the group unassigns the member's tasks there.
	bob.ok("DELETE", "/groups/"+g1+"/members/"+bob.id, nil)
	if got := taskIDs(alice.ok("GET", "/groups/"+g1+"/tasks?assignee=none", nil)); len(got) != 2 {
		t.Fatalf("after leave: %v", got)
	}
	// Dissolving the group deletes its tasks.
	bob.ok("DELETE", "/groups/"+g2, nil)
	var n int
	e.s.db.R.QueryRow(`SELECT COUNT(*) FROM tasks WHERE group_id=?`, g2).Scan(&n)
	if n != 0 {
		t.Fatalf("tasks after dissolve: %d", n)
	}
}

func TestTaskSSEAudience(t *testing.T) {
	e := newEnv(t)
	alice, bob, carol := e.human("alice"), e.human("bob"), e.human("carol")
	g := alice.group("g", bob)
	carol.group("other")
	cc := e.agent(alice, "CC", g)

	hb, closeB := openSSE(t, bob, "")
	defer closeB()
	readEvent(t, hb)
	cb, closeC := openSSE(t, carol, "")
	defer closeC()
	readEvent(t, cb)
	ab, closeA := openSSE(t, cc, "")
	defer closeA()
	readEvent(t, ab)

	tk := taskOf(alice.ok("POST", "/groups/"+g+"/tasks", map[string]any{"title": "看 SSE"}))
	nextTask := func() map[string]any {
		t.Helper()
		for {
			typ, data := readEvent(t, hb)
			if typ == "task" {
				var ev map[string]any
				json.Unmarshal([]byte(data), &ev)
				return ev
			}
		}
	}
	ev := nextTask()
	if ev["groupId"] != g || ev["task"].(map[string]any)["id"] != tk["id"] || ev["deleted"] != nil {
		t.Fatalf("create event: %v", ev)
	}
	bob.ok("PATCH", "/tasks/"+tk["id"].(string), map[string]any{"status": "doing"})
	if ev := nextTask(); ev["task"].(map[string]any)["status"] != "doing" {
		t.Fatalf("patch event: %v", ev)
	}
	alice.ok("DELETE", "/tasks/"+tk["id"].(string), nil)
	if ev := nextTask(); ev["deleted"] != true {
		t.Fatalf("delete event: %v", ev)
	}
	// No chat messages were written for any of it.
	var n int
	e.s.db.R.QueryRow(`SELECT COUNT(*) FROM messages WHERE group_id=? AND body LIKE '%SSE%'`, g).Scan(&n)
	if n != 0 {
		t.Fatalf("chat messages: %d", n)
	}
	// Non-member carol and the agent get nothing: a group message to carol's own group shows
	// her stream is live and that no task event came before it.
	alice.send(g, map[string]any{"text": "@CC ping"})
	if typ, _ := readEvent(t, ab); typ != "wake" {
		t.Fatalf("agent got %s before its wake", typ)
	}
	carol.send(carol.ok("GET", "/groups", nil)["groups"].([]any)[0].(map[string]any)["id"].(string), map[string]any{"text": "x"})
	if typ, _ := readEvent(t, cb); typ != "message" {
		t.Fatalf("non-member got %s", typ)
	}
	// Task events are stored: a resuming human gets them replayed.
	cursor := e.s.maxEventID()
	closeB()
	alice.ok("POST", "/groups/"+g+"/tasks", map[string]any{"title": "离线时建的"})
	hb2, close2 := openSSE(t, bob, fmt.Sprint(cursor))
	defer close2()
	readEvent(t, hb2)
	if typ, data := readEvent(t, hb2); typ != "task" || !strings.Contains(data, "离线时建的") {
		t.Fatalf("replay: %s %s", typ, data)
	}
}

func TestTaskMCPTools(t *testing.T) {
	e := newEnv(t)
	alice, bob, carol := e.human("alice"), e.human("bob"), e.human("carol")
	g := alice.group("g", bob)
	other := carol.group("other")
	cc := e.agent(alice, "CC", g)
	m := alice.send(g, map[string]any{"text": "帮我写周报", "ignoreNew": true})
	seq := int64(m["message"].(map[string]any)["seq"].(float64))

	cmd := runToken(t, e, cc, g, "project")
	// task_add: default assignee is the caller and status doing; sourceSeq accepted as string.
	res := callTool(t, e, cmd.token, "task_add", map[string]any{"title": "写周报", "sourceSeq": fmt.Sprint(seq)})
	if isErr(res) || !strings.Contains(resultText(res), "[进行中] 写周报｜负责人：CC(agent)") || !strings.Contains(resultText(res), fmt.Sprintf("来自 #%d", seq)) {
		t.Fatalf("add: %q", resultText(res))
	}
	var id string
	e.s.db.R.QueryRow(`SELECT id FROM tasks WHERE title='写周报'`).Scan(&id)
	// Assigned to someone else by nick: todo.
	res = callTool(t, e, cmd.token, "task_add", map[string]any{"title": "审核周报", "assignee": "bob"})
	if isErr(res) || !strings.Contains(resultText(res), "[待办] 审核周报｜负责人：bob") {
		t.Fatalf("add bob: %q", resultText(res))
	}
	if res := callTool(t, e, cmd.token, "task_add", map[string]any{"title": "x", "assignee": "carol"}); !isErr(res) || !strings.Contains(resultText(res), "不是这个群的成员") {
		t.Fatalf("add non-member assignee: %v", res)
	}
	if res := callTool(t, e, cmd.token, "task_add", map[string]any{"title": "x", "groupId": other}); !isErr(res) {
		t.Fatalf("add other group: %v", res)
	}
	res = callTool(t, e, cmd.token, "task_add", map[string]any{"title": "没人管", "assignee": "none"})
	if isErr(res) || !strings.Contains(resultText(res), "[待办] 没人管｜负责人：未分配") {
		t.Fatalf("add unassigned: %q", resultText(res))
	}

	// task_update: note appended with a timestamp; replace:true overwrites; status done.
	callTool(t, e, cmd.token, "task_update", map[string]any{"taskId": id, "note": "第一段"})
	res = callTool(t, e, cmd.token, "task_update", map[string]any{"taskId": id, "note": "第二段", "status": "blocked"})
	var note, status string
	e.s.db.R.QueryRow(`SELECT note,status FROM tasks WHERE id=?`, id).Scan(&note, &status)
	lines := strings.Split(note, "\n")
	if isErr(res) || status != "blocked" || len(lines) != 2 || !strings.HasSuffix(lines[0], " CC] 第一段") || !strings.HasSuffix(lines[1], " CC] 第二段") ||
		!strings.HasPrefix(lines[1], "["+time.Now().Format("2006-01-02")) {
		t.Fatalf("append: %v %q %q", res, status, note)
	}
	callTool(t, e, cmd.token, "task_update", map[string]any{"taskId": id, "note": "重写", "replace": true, "status": "done"})
	e.s.db.R.QueryRow(`SELECT note,status FROM tasks WHERE id=?`, id).Scan(&note, &status)
	if note != "重写" || status != "done" {
		t.Fatalf("replace: %q %q", note, status)
	}
	if res := callTool(t, e, cmd.token, "task_update", map[string]any{"taskId": id}); !isErr(res) {
		t.Fatalf("empty update: %v", res)
	}
	// Agent can't update a task assigned to someone else that it didn't create.
	tb := taskOf(alice.ok("POST", "/groups/"+g+"/tasks", map[string]any{"title": "alice 给 bob", "assigneeId": bob.id}))
	if res := callTool(t, e, cmd.token, "task_update", map[string]any{"taskId": tb["id"], "status": "done"}); !isErr(res) || !strings.Contains(resultText(res), "只能修改") {
		t.Fatalf("other's task: %v", res)
	}
	// Reassign own task to bob, then unassign.
	tm := taskOf(alice.ok("POST", "/groups/"+g+"/tasks", map[string]any{"title": "给 CC 的", "assigneeId": cc.id}))
	// Empty assignee means "unchanged", not "unassign".
	if res := callTool(t, e, cmd.token, "task_update", map[string]any{"taskId": tm["id"], "assignee": "", "status": "doing"}); isErr(res) || !strings.Contains(resultText(res), "负责人：CC(agent)") || !strings.HasPrefix(resultText(res), taskListPrefix) {
		t.Fatalf("empty assignee: %q", resultText(res))
	}
	if res := callTool(t, e, cmd.token, "task_update", map[string]any{"taskId": tm["id"], "assignee": "none"}); isErr(res) || !strings.Contains(resultText(res), "未分配") {
		t.Fatalf("unassign: %v", res)
	}
	if res := callTool(t, e, cmd.token, "task_add", map[string]any{"title": "空负责人", "assignee": ""}); isErr(res) || !strings.Contains(resultText(res), "[进行中] 空负责人｜负责人：CC(agent)") || !strings.HasPrefix(resultText(res), taskListPrefix) {
		t.Fatalf("add empty assignee: %q", resultText(res))
	}
	// Commander run token: other groups are off limits even if the agent is a member.
	g2 := alice.group("g2", cc)
	if res := callTool(t, e, cmd.token, "task_add", map[string]any{"title": "x", "groupId": g2}); !isErr(res) {
		t.Fatalf("commander adds in other group: %v", res)
	}
	if res := callTool(t, e, cmd.token, "task_list", map[string]any{"groupId": g2}); !isErr(res) {
		t.Fatalf("commander lists other group: %v", res)
	}

	// task_list: run token defaults to its group, untrusted prefix, open by default; mine filter.
	res = callTool(t, e, cmd.token, "task_list", nil)
	text := resultText(res)
	if isErr(res) || !strings.HasPrefix(text, taskListPrefix) || !strings.Contains(text, "审核周报") || strings.Contains(text, "写周报｜") {
		t.Fatalf("list: %q", text)
	}
	res = callTool(t, e, cmd.token, "task_list", map[string]any{"status": "all", "mine": true})
	if text := resultText(res); isErr(res) || !strings.Contains(text, "写周报") || strings.Contains(text, "审核周报") || !strings.Contains(text, "备注：重写") {
		t.Fatalf("list mine all: %q", text)
	}
	// Agent token: groupId or mine required; mine works across groups.
	if res := callTool(t, e, cc.token, "task_list", nil); !isErr(res) || !strings.Contains(resultText(res), "groupId") {
		t.Fatalf("agent no group: %v", res)
	}
	if res := callTool(t, e, cc.token, "task_list", map[string]any{"groupId": other}); !isErr(res) {
		t.Fatalf("agent non-member: %v", res)
	}
	if res := callTool(t, e, cc.token, "task_list", map[string]any{"mine": true, "status": "all"}); isErr(res) || !strings.Contains(resultText(res), "「g」") {
		t.Fatalf("agent mine: %q", resultText(res))
	}

	// Restricted run token: only task_list is listed and callable.
	rr := runToken(t, e, cc, g, "restricted")
	names := toolNames(t, e, rr.token)
	if !has(names, "task_list") || has(names, "task_add") || has(names, "task_update") {
		t.Fatalf("restricted tools: %v", names)
	}
	if res := callTool(t, e, rr.token, "task_list", nil); isErr(res) {
		t.Fatalf("restricted list: %v", res)
	}
	_, out := mcpPost(t, e, rr.token, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "task_add", "arguments": map[string]any{"title": "x"}}})
	if out["error"] == nil {
		t.Fatalf("restricted add: %v", out)
	}
	if names := toolNames(t, e, cmd.token); !has(names, "task_add") || !has(names, "task_update") {
		t.Fatalf("commander tools: %v", names)
	}
}

func TestTaskToolsAdminToggle(t *testing.T) {
	e := newEnv(t)
	admin := e.human("admin")
	g := admin.group("g")
	cc := e.agent(admin, "CC", g)
	admin.ok("PUT", "/admin/tools/builtin", map[string]any{"name": "task_add", "enabled": false})
	if names := toolNames(t, e, cc.token); has(names, "task_add") || !has(names, "task_update") || !has(names, "task_list") {
		t.Fatalf("toggle: %v", names)
	}
	_, out := mcpPost(t, e, cc.token, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "task_add", "arguments": map[string]any{"title": "x", "groupId": g}}})
	if out["error"] == nil {
		t.Fatalf("disabled task_add callable: %v", out)
	}
	admin.ok("PUT", "/admin/tools/builtin", map[string]any{"name": "task_list", "enabled": false})
	if names := toolNames(t, e, cc.token); has(names, "task_list") {
		t.Fatalf("task_list still listed: %v", names)
	}
	// The HTTP API is not an MCP tool: disabling the tools doesn't block the web.
	admin.ok("POST", "/groups/"+g+"/tasks", map[string]any{"title": "网页上建"})
	admin.ok("PUT", "/admin/tools/builtin", map[string]any{"name": "task_add", "enabled": true})
	if names := toolNames(t, e, cc.token); !has(names, "task_add") {
		t.Fatalf("re-enabled: %v", names)
	}
}

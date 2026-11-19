package server

import (
	"strings"
	"testing"
)

func TestAddExistingUser(t *testing.T) {
	e := newEnv(t)
	alice := e.human("alice")
	bob := e.human("bob")
	carol := e.human("carol")
	dave := e.human("dave")
	erin := e.human("erin")
	cc := e.agent(alice, "CC", "")
	gid := alice.group("g", bob, cc)

	// Directory: humans only, sorted, inGroup per user.
	us := alice.ok("GET", "/users?groupId="+gid, nil)["users"].([]any)
	var names []string
	in := map[string]bool{}
	for _, x := range us {
		u := x.(map[string]any)
		names = append(names, u["name"].(string))
		in[u["name"].(string)] = u["inGroup"].(bool)
	}
	if strings.Join(names, ",") != "alice,bob,carol,dave,erin" {
		t.Fatalf("directory: %v", names)
	}
	if !in["alice"] || !in["bob"] || in["carol"] {
		t.Fatalf("inGroup: %v", in)
	}
	if _, ok := alice.ok("GET", "/users", nil)["users"].([]any)[0].(map[string]any)["inGroup"]; ok {
		t.Fatal("inGroup without groupId")
	}
	if r := carol.do("GET", "/users?groupId="+gid, nil); r.status != 404 {
		t.Fatalf("non-member directory: %d", r.status)
	}
	if r := cc.do("GET", "/users", nil); r.status != 403 && r.status != 401 {
		t.Fatalf("agent directory: %d", r.status)
	}

	// Plain member and agent token cannot add users.
	if r := bob.do("POST", "/groups/"+gid+"/members", map[string]any{"userId": carol.id}); r.status != 403 {
		t.Fatalf("member add: %d %s", r.status, r.raw)
	}
	if r := cc.do("POST", "/groups/"+gid+"/members", map[string]any{"userId": carol.id}); r.status != 403 && r.status != 401 {
		t.Fatalf("agent add: %d %s", r.status, r.raw)
	}
	if r := alice.do("POST", "/groups/"+gid+"/members", map[string]any{"userId": cc.id}); r.status != 404 {
		t.Fatalf("add agent as user: %d", r.status)
	}

	// Owner adds carol; carol's SSE gets a groups event and GET /groups lists it.
	br, closeC := openSSE(t, carol, "")
	defer closeC()
	readEvent(t, br) // hello
	m := alice.ok("POST", "/groups/"+gid+"/members", map[string]any{"userId": carol.id})["member"].(map[string]any)
	if m["id"] != carol.id || m["role"] != "member" {
		t.Fatalf("member: %v", m)
	}
	// Carol is a member by commit time, so the message/group events reach her too.
	for i := 0; ; i++ {
		typ, _ := readEvent(t, br)
		if typ == "groups" {
			break
		}
		if i > 5 {
			t.Fatalf("carol never got groups, last %s", typ)
		}
	}
	found := false
	for _, x := range carol.ok("GET", "/groups", nil)["groups"].([]any) {
		if x.(map[string]any)["id"] == gid {
			found = true
		}
	}
	if !found {
		t.Fatal("carol does not see group")
	}
	msgs := alice.ok("GET", "/groups/"+gid+"/messages", nil)["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["kind"] != "system" || last["text"] != "alice 把 carol 拉进了群" {
		t.Fatalf("system message: %v", last)
	}

	// Duplicate -> 409.
	if r := alice.do("POST", "/groups/"+gid+"/members", map[string]any{"userId": carol.id}); r.status != 409 || !strings.Contains(r.raw, "已经在群里") {
		t.Fatalf("duplicate: %d %s", r.status, r.raw)
	}

	// Admin adds dave.
	alice.ok("PATCH", "/groups/"+gid+"/members/"+bob.id, map[string]any{"role": "admin"})
	bob.ok("POST", "/groups/"+gid+"/members", map[string]any{"userId": dave.id})
	if in := bob.ok("GET", "/users?groupId="+gid, nil)["users"].([]any); !in[3].(map[string]any)["inGroup"].(bool) {
		t.Fatalf("dave inGroup: %v", in[3])
	}

	// DM -> 400.
	dm := alice.ok("POST", "/chats", map[string]any{"with": bob.id})["group"].(map[string]any)["id"].(string)
	if r := alice.do("POST", "/groups/"+dm+"/members", map[string]any{"userId": erin.id}); r.status != 400 {
		t.Fatalf("dm add: %d %s", r.status, r.raw)
	}
	// Neither / both ids -> 400.
	if r := alice.do("POST", "/groups/"+gid+"/members", map[string]any{}); r.status != 400 {
		t.Fatalf("empty: %d", r.status)
	}
}

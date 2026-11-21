package server

import "testing"

// Group owners/admins can raise the agent relay limits for one group (v0.2.8).
func TestGroupRelayLimits(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	gid := alice.group("g", bob)
	b := e.agent(alice, "BB", gid)
	a := e.agent(bob, "AA", gid)
	alice.ok("PUT", "/agents/"+b.id+"/grants", map[string]any{"userId": a.id, "groupId": gid})
	bob.ok("PUT", "/agents/"+a.id+"/grants", map[string]any{"userId": b.id, "groupId": gid})

	g := alice.ok("GET", "/groups/"+gid, nil)["group"].(map[string]any)["limits"].(map[string]any)
	if g["hopLimit"].(float64) != hopLimit || g["default"] != true {
		t.Fatalf("default limits: %v", g)
	}
	// Plain members and agents can't change it; bad values are rejected.
	if r := bob.do("PATCH", "/groups/"+gid, map[string]any{"hopLimit": 20}); r.status != 403 {
		t.Fatalf("member patch: %d", r.status)
	}
	if r := alice.do("PATCH", "/groups/"+gid, map[string]any{"hopLimit": maxHopLimit + 1}); r.status != 400 {
		t.Fatalf("too high: %d", r.status)
	}
	g = alice.ok("PATCH", "/groups/"+gid, map[string]any{"hopLimit": 20, "agentWakesPerHour": 100})["group"].(map[string]any)["limits"].(map[string]any)
	if g["hopLimit"].(float64) != 20 || g["agentWakesPerHour"].(float64) != 100 || g["default"] != false {
		t.Fatalf("patched limits: %v", g)
	}

	// Agents now relay past the old 6-hop default, up to 20.
	seq := alice.send(gid, map[string]any{"text": "@BB start"})["message"].(map[string]any)["seq"]
	pair := []*client{b, a}
	nicks := []string{"@AA", "@BB"}
	var r map[string]any
	for i := 0; i < 20; i++ {
		r = pair[i%2].send(gid, map[string]any{"text": nicks[i%2] + " pass", "replyTo": seq, "ignoreNew": true})
		if i < 19 {
			if why := notWokenReasons(r); len(why) != 0 {
				t.Fatalf("hop %d not woken: %v", i+1, why)
			}
		}
		seq = r["message"].(map[string]any)["seq"]
	}
	if notWokenReasons(r)[b.id] != "hop_limit" {
		t.Fatalf("expected hop_limit at 20: %v", r)
	}

	// 0 resets to the default.
	g = alice.ok("PATCH", "/groups/"+gid, map[string]any{"hopLimit": 0, "agentWakesPerHour": 0})["group"].(map[string]any)["limits"].(map[string]any)
	if g["hopLimit"].(float64) != hopLimit || g["default"] != true {
		t.Fatalf("reset limits: %v", g)
	}
}

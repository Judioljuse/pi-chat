package server

import "testing"

// Renaming an agent or a human updates its nick in every group (v0.2.4).
func TestRenameSyncsNicks(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.human("alice"), e.human("bob")
	g1 := alice.group("g1", bob)
	g2 := alice.group("g2", bob)
	cc := e.agent(alice, "CC", g1)
	alice.ok("POST", "/groups/"+g2+"/members", map[string]any{"agentId": cc.id})
	nickIn := func(gid, id string) string {
		var n string
		e.s.db.R.QueryRow(`SELECT nick FROM members WHERE group_id=? AND principal_id=?`, gid, id).Scan(&n)
		return n
	}
	alice.ok("PATCH", "/agents/"+cc.id, map[string]any{"displayName": "部署方"})
	if nickIn(g1, cc.id) != "部署方" || nickIn(g2, cc.id) != "部署方" {
		t.Fatalf("agent nick not synced: %q %q", nickIn(g1, cc.id), nickIn(g2, cc.id))
	}
	// A clash with another member gets a suffix in that group only.
	bob.ok("PATCH", "/me", map[string]any{"displayName": "部署方"})
	if n := nickIn(g1, bob.id); n == "部署方" || n == "" {
		t.Fatalf("human nick should be made unique, got %q", n)
	}
	// @ by the new nick reaches the agent.
	m := alice.send(g1, map[string]any{"text": "@部署方 在吗"})["message"].(map[string]any)
	if ms, _ := m["mentions"].([]any); len(ms) != 1 || ms[0] != cc.id {
		t.Fatalf("mention by new nick: %v", m["mentions"])
	}
}

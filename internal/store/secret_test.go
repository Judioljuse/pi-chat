package store

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestSecretKeyAndSeal(t *testing.T) {
	dir := t.TempDir()
	k1, err := LoadOrCreateSecret(dir)
	if err != nil || len(k1) != 32 {
		t.Fatalf("create: %v %d", err, len(k1))
	}
	st, err := os.Stat(filepath.Join(dir, SecretFile))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", st.Mode(), err)
	}
	k2, err := LoadOrCreateSecret(dir)
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatal("reload gave a different key")
	}
	a, err := Seal(k1, "Bearer sk-秘密-1234", "mcp_servers:x")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Seal(k1, "Bearer sk-秘密-1234", "mcp_servers:x")
	if bytes.Equal(a, b) || bytes.Contains(a, []byte("sk-")) {
		t.Fatal("nonce reused or plaintext visible")
	}
	if p, err := Unseal(k1, a, "mcp_servers:x"); err != nil || p != "Bearer sk-秘密-1234" {
		t.Fatalf("round trip: %q %v", p, err)
	}
	if _, err := Unseal(k1, a, "mcp_servers:y"); err == nil {
		t.Fatal("wrong aad accepted")
	}
	other := bytes.Repeat([]byte{7}, 32)
	if _, err := Unseal(other, a, "mcp_servers:x"); err == nil {
		t.Fatal("wrong key accepted")
	}
	a[len(a)-1] ^= 1
	if _, err := Unseal(k1, a, "mcp_servers:x"); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err := Unseal(k1, []byte{1, 2}, ""); err == nil {
		t.Fatal("short blob accepted")
	}
	if e, _ := Seal(k1, "", "k"); e == nil {
		t.Fatal("empty plaintext")
	} else if p, err := Unseal(k1, e, "k"); err != nil || p != "" {
		t.Fatal("empty round trip")
	}
	// A truncated secret file is an error, not silently replaced.
	os.WriteFile(filepath.Join(dir, SecretFile), []byte("short"), 0o600)
	if _, err := LoadOrCreateSecret(dir); err == nil {
		t.Fatal("bad secret file accepted")
	}
}

// TestFTSBackfillAndTriggers: a database from before migration 8 gets the search index built
// from its existing messages; recalled/system messages are left out and triggers keep it in sync.
func TestFTSBackfillAndTriggers(t *testing.T) {
	dir := t.TempDir()
	old, err := sql.Open("sqlite", dsn(filepath.Join(dir, "pichat.db")))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range migrations[:7] {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{`PRAGMA user_version=7`,
		`INSERT INTO principals(id,kind,name,display_name,created_at) VALUES('u_a','human','alice','Alice',1)`,
		`INSERT INTO groups(id,kind,name,owner_id,last_seq,created_at) VALUES('g_1','group','g','u_a',3,1)`,
		`INSERT INTO messages(group_id,seq,author_id,kind,body,created_at) VALUES('g_1',1,'u_a','text','明天讨论数据库迁移方案',1)`,
		`INSERT INTO messages(group_id,seq,author_id,kind,body,created_at,recalled_at) VALUES('g_1',2,'u_a','text','撤回的数据库消息',1,2)`,
		`INSERT INTO messages(group_id,seq,kind,body,created_at) VALUES('g_1',3,'system','系统 数据库提示',1)`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	old.Close()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !db.FTS {
		t.Fatal("FTS index not created")
	}
	match := func(q string) []int64 {
		t.Helper()
		rows, err := db.R.Query(`SELECT m.seq FROM messages_fts f JOIN messages m ON m.id=f.rowid WHERE f.messages_fts MATCH ? ORDER BY m.seq`, q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []int64
		for rows.Next() {
			var s int64
			rows.Scan(&s)
			out = append(out, s)
		}
		return out
	}
	if got := match(`"数据库"`); len(got) != 1 || got[0] != 1 {
		t.Fatalf("backfill: %v", got)
	}
	db.W.Exec(`INSERT INTO messages(group_id,seq,author_id,kind,body,created_at) VALUES('g_1',4,'u_a','text','新的数据库问题',5)`)
	if got := match(`"数据库"`); len(got) != 2 {
		t.Fatalf("insert trigger: %v", got)
	}
	db.W.Exec(`UPDATE messages SET recalled_at=9 WHERE seq=4`)
	if got := match(`"数据库"`); len(got) != 1 {
		t.Fatalf("recall trigger: %v", got)
	}
	db.W.Exec(`UPDATE messages SET body='改过的内容' WHERE seq=1`)
	if got := match(`"数据库"`); len(got) != 0 {
		t.Fatalf("update trigger: %v", got)
	}
	if got := match(`"改过的"`); len(got) != 1 {
		t.Fatalf("update reindex: %v", got)
	}
	db.W.Exec(`DELETE FROM groups WHERE id='g_1'`)
	if got := match(`"改过的"`); len(got) != 0 {
		t.Fatalf("cascade delete: %v", got)
	}
	var n int
	db.R.QueryRow(`SELECT COUNT(*) FROM tool_settings`).Scan(&n)
	db.R.QueryRow(`SELECT COUNT(*) FROM mcp_servers`).Scan(&n)
	db.Close()
	db2, err := Open(dir) // reopening keeps the index
	if err != nil || !db2.FTS {
		t.Fatalf("reopen: %v", err)
	}
	db2.Close()
}

func TestBackupCopiesSecret(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	key, _ := LoadOrCreateSecret(dir)
	dst := filepath.Join(t.TempDir(), "bk")
	if err := Backup(dir, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, SecretFile))
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("secret not backed up: %v", err)
	}
	if st, _ := os.Stat(filepath.Join(dst, SecretFile)); st.Mode().Perm() != 0o600 {
		t.Fatalf("backup secret mode %v", st.Mode())
	}
}

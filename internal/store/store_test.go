package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrateFromV1 builds a database with only the first migration (the schema
// shipped before message provenance / agent runtime), fills it, and opens it with
// the current code: data must survive and the new columns must exist.
func TestMigrateFromV1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pichat.db")
	old, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{migrations[0], `PRAGMA user_version=1`,
		`INSERT INTO principals(id,kind,name,display_name,created_at) VALUES('u_a','human','alice','Alice',1)`,
		`INSERT INTO principals(id,kind,name,display_name,owner_id,platform,created_at) VALUES('a_c','agent','CC','CC','u_a','claude_code',1)`,
		`INSERT INTO groups(id,kind,name,owner_id,last_seq,created_at) VALUES('g_1','group','g','u_a',1,1)`,
		`INSERT INTO members(group_id,principal_id,nick,role,joined_at) VALUES('g_1','u_a','Alice','owner',1),('g_1','a_c','CC','member',1)`,
		`INSERT INTO messages(group_id,seq,author_id,kind,body,created_at) VALUES('g_1',1,'a_c','text','hello',1)`,
		`INSERT INTO tokens(hash,principal_id,kind,created_at) VALUES(x'01','a_c','agent',1)`,
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
	var v int
	db.R.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != len(migrations) {
		t.Fatalf("user_version %d", v)
	}
	var body string
	var via sql.NullString
	if err := db.R.QueryRow(`SELECT body,via FROM messages WHERE group_id='g_1' AND seq=1`).Scan(&body, &via); err != nil || body != "hello" || via.Valid {
		t.Fatalf("message: %q %v %v", body, via, err)
	}
	var state, sess sql.NullString
	if err := db.R.QueryRow(`SELECT runtime_state,main_session FROM principals WHERE id='a_c'`).Scan(&state, &sess); err != nil || state.Valid || sess.Valid {
		t.Fatalf("principal: %v %v %v", state, sess, err)
	}
	var n int
	db.R.QueryRow(`SELECT COUNT(*) FROM tokens WHERE run_session IS NULL`).Scan(&n)
	if n != 1 {
		t.Fatalf("tokens %d", n)
	}
	if _, err := db.W.Exec(`INSERT INTO agent_sessions(agent_id,session,host,started_at) VALUES('a_c','s1','h',2)`); err != nil {
		t.Fatal(err)
	}
	// Re-opening is a no-op.
	db.Close()
	db2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	db2.Close()
}

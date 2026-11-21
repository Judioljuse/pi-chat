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
	var notice sql.NullString
	if err := db.R.QueryRow(`SELECT runtime_notice FROM principals WHERE id='a_c'`).Scan(&notice); err != nil || notice.Valid {
		t.Fatalf("notice: %v %v", notice, err)
	}
	var n int
	db.R.QueryRow(`SELECT COUNT(*) FROM tokens WHERE run_session IS NULL`).Scan(&n)
	if n != 1 {
		t.Fatalf("tokens %d", n)
	}
	if _, err := db.W.Exec(`INSERT INTO agent_sessions(agent_id,session,host,started_at) VALUES('a_c','s1','h',2)`); err != nil {
		t.Fatal(err)
	}
	// Migration 6: agent group names, keyed per owner.
	if _, err := db.W.Exec(`INSERT INTO agent_group_names(owner_id,key,name,updated_at) VALUES('u_a','host:box','办公室',3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO agent_group_names(owner_id,key,name,updated_at) VALUES('u_a','host:box','x',4)`); err == nil {
		t.Fatal("duplicate (owner,key) accepted")
	}
	var dirs sql.NullString
	if err := db.R.QueryRow(`SELECT runtime_dirs FROM principals WHERE id='a_c'`).Scan(&dirs); err != nil || dirs.Valid {
		t.Fatalf("runtime_dirs: %v %v", dirs, err)
	}
	// Migration 9: tasks, with status checked and rows cascading with their group.
	if _, err := db.W.Exec(`INSERT INTO tasks(id,group_id,title,status,assignee_id,source_seq,created_by,created_at,updated_by,updated_at)
		VALUES('t_1','g_1','修 bug','doing','a_c',1,'u_a',5,'u_a',5)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO tasks(id,group_id,title,status,created_by,created_at,updated_by,updated_at) VALUES('t_2','g_1','x','later','u_a',5,'u_a',5)`); err == nil {
		t.Fatal("bad status accepted")
	}
	if _, err := db.W.Exec(`INSERT INTO tasks(id,group_id,title,created_by,created_at,updated_by,updated_at) VALUES('t_3','g_nope','x','u_a',5,'u_a',5)`); err == nil {
		t.Fatal("task in a missing group accepted")
	}
	// Re-opening is a no-op.
	db.Close()
	db2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db2.R.QueryRow(`SELECT name FROM agent_group_names WHERE owner_id='u_a'`).Scan(&name); err != nil || name != "办公室" {
		t.Fatalf("group name after reopen: %q %v", name, err)
	}
	var title string
	if err := db2.R.QueryRow(`SELECT title FROM tasks WHERE id='t_1'`).Scan(&title); err != nil || title != "修 bug" {
		t.Fatalf("task after reopen: %q %v", title, err)
	}
	if _, err := db2.W.Exec(`DELETE FROM groups WHERE id='g_1'`); err != nil {
		t.Fatal(err)
	}
	var n2 int
	db2.R.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n2)
	if n2 != 0 {
		t.Fatalf("tasks not cascaded: %d", n2)
	}
	db2.Close()
}

// TestMigrateFromV8 opens a v0.3.1 database (migrations 1-8) with data: migration 9 adds tasks
// and leaves everything else as it was.
func TestMigrateFromV8(t *testing.T) {
	dir := t.TempDir()
	old, err := sql.Open("sqlite", dsn(filepath.Join(dir, "pichat.db")))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := old.Exec(migrations[i]); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	for _, q := range []string{`PRAGMA user_version=8`,
		`INSERT INTO principals(id,kind,name,display_name,created_at) VALUES('u_a','human','alice','Alice',1)`,
		`INSERT INTO groups(id,kind,name,owner_id,last_seq,created_at,hop_limit) VALUES('g_1','group','g','u_a',1,1,9)`,
		`INSERT INTO members(group_id,principal_id,nick,role,joined_at) VALUES('g_1','u_a','Alice','owner',1)`,
		`INSERT INTO messages(group_id,seq,author_id,kind,body,created_at) VALUES('g_1',1,'u_a','text','hello',1)`,
		`INSERT INTO tool_settings(key,value,updated_at) VALUES('builtin.web_fetch','off',1)`,
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
	var v, hop int
	var body, setting string
	db.R.QueryRow(`PRAGMA user_version`).Scan(&v)
	db.R.QueryRow(`SELECT hop_limit FROM groups WHERE id='g_1'`).Scan(&hop)
	db.R.QueryRow(`SELECT body FROM messages WHERE seq=1`).Scan(&body)
	db.R.QueryRow(`SELECT value FROM tool_settings WHERE key='builtin.web_fetch'`).Scan(&setting)
	if v != len(migrations) || hop != 9 || body != "hello" || setting != "off" {
		t.Fatalf("after migrate: v=%d hop=%d body=%q setting=%q", v, hop, body, setting)
	}
	var n int
	if err := db.R.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("tasks: %d %v", n, err)
	}
}

// TestMigrateFromV9 opens a v0.3.2 database (migrations 1-9) with group limits set: migration 10
// adds the per-agent budget and rules columns (NULL) and keeps the existing values.
func TestMigrateFromV9(t *testing.T) {
	dir := t.TempDir()
	old, err := sql.Open("sqlite", dsn(filepath.Join(dir, "pichat.db")))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		if _, err := old.Exec(migrations[i]); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	for _, q := range []string{`PRAGMA user_version=9`,
		`INSERT INTO principals(id,kind,name,display_name,created_at) VALUES('u_a','human','alice','Alice',1)`,
		`INSERT INTO groups(id,kind,name,owner_id,last_seq,created_at,hop_limit,agent_wakes_per_hour) VALUES('g_1','group','g','u_a',1,1,12,90)`,
		`INSERT INTO messages(group_id,seq,author_id,kind,body,created_at) VALUES('g_1',1,'u_a','text','hello',1)`,
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
	var v, hop, wakes int
	var perAgent sql.NullInt64
	var rules sql.NullString
	var body string
	db.R.QueryRow(`PRAGMA user_version`).Scan(&v)
	if err := db.R.QueryRow(`SELECT hop_limit,agent_wakes_per_hour,agent_wakes_per_agent_hour,rules FROM groups WHERE id='g_1'`).Scan(&hop, &wakes, &perAgent, &rules); err != nil {
		t.Fatal(err)
	}
	db.R.QueryRow(`SELECT body FROM messages WHERE seq=1`).Scan(&body)
	if v != len(migrations) || hop != 12 || wakes != 90 || perAgent.Valid || rules.Valid || body != "hello" {
		t.Fatalf("after migrate: v=%d hop=%d wakes=%d perAgent=%v rules=%v body=%q", v, hop, wakes, perAgent, rules, body)
	}
}

// Package store opens the SQLite database and applies migrations.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DB holds a single-connection writer pool and a small reader pool.
type DB struct {
	W, R *sql.DB
	Path string
	// FTS is true when the messages_fts trigram index exists and is kept in sync.
	FTS bool
}

func dsn(path string) string {
	return "file:" + path + "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
}

// Open opens <dataDir>/pichat.db, creating the directory layout and running migrations.
func Open(dataDir string) (*DB, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "files"), 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "pichat.db")
	w, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	w.SetConnMaxLifetime(0)
	if err := migrate(w); err != nil {
		w.Close()
		return nil, err
	}
	fts := ensureFTS(w)
	r, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)
	r.SetMaxIdleConns(2)
	return &DB{W: w, R: r, Path: path, FTS: fts}, nil
}

func (d *DB) Close() error {
	d.R.Close()
	return d.W.Close()
}

func migrate(db *sql.DB) error {
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		slog.Warn("数据库结构比这个程序新（可能是从在线更新的版本回退的），新增的表或列不会被用到", "dbVersion", v, "known", len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Backup writes a consistent copy of the database, the instance secret (secret.key, needed to
// decrypt the tool secrets stored in the database) and all attachment blobs into dst.
func Backup(dataDir, dst string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	out := filepath.Join(dst, "pichat.db")
	if _, err := os.Stat(out); err == nil {
		return fmt.Errorf("%s already exists", out)
	}
	db, err := sql.Open("sqlite", dsn(filepath.Join(dataDir, "pichat.db")))
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), "VACUUM INTO ?", out); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dataDir, SecretFile)); err == nil {
		if err := copyFile(filepath.Join(dataDir, SecretFile), filepath.Join(dst, SecretFile)); err != nil {
			return err
		}
	}
	src := filepath.Join(dataDir, "files")
	return filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || strings.HasPrefix(fi.Name(), ".") {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, "files", rel)
		if st, err := os.Stat(target); err == nil && st.Size() == fi.Size() {
			return nil
		}
		return copyFile(p, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

var b32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewID returns prefix + 16 lowercase base32 chars.
func NewID(prefix string) string { return prefix + RandString(10) }

// RandString returns base32 of n random bytes.
func RandString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return b32.EncodeToString(b)
}

// Hash returns sha256 of s.
func Hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

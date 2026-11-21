package store

import (
	"database/sql"
	"log/slog"
)

// Message search index: an FTS5 table with the trigram tokenizer (substring matching, so
// Chinese works without word segmentation), contentless with delete support so it only holds
// the index; bodies are read from messages. Only searchable rows are indexed: text messages
// that are not recalled. Triggers keep it in sync on insert, update (recall, body change) and
// delete (including ON DELETE CASCADE from groups).
const ftsSchema = `
CREATE VIRTUAL TABLE messages_fts USING fts5(body, content='', contentless_delete=1, tokenize='trigram');
CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages WHEN new.kind='text' AND new.recalled_at IS NULL AND new.body<>'' BEGIN
  INSERT INTO messages_fts(rowid, body) VALUES (new.id, new.body);
END;
CREATE TRIGGER messages_fts_au AFTER UPDATE OF body, kind, recalled_at ON messages BEGIN
  DELETE FROM messages_fts WHERE rowid=old.id;
  INSERT INTO messages_fts(rowid, body) SELECT new.id, new.body WHERE new.kind='text' AND new.recalled_at IS NULL AND new.body<>'';
END;
CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
  DELETE FROM messages_fts WHERE rowid=old.id;
END;
INSERT INTO messages_fts(rowid, body) SELECT id, body FROM messages WHERE kind='text' AND recalled_at IS NULL AND body<>'';
`

// ensureFTS creates and backfills the search index once. It reports whether the index is
// usable; false (SQLite without fts5/trigram) means search falls back to a LIKE scan.
func ensureFTS(db *sql.DB) bool {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='messages_fts'`).Scan(&n); err != nil {
		return false
	}
	if n > 0 {
		return true
	}
	tx, err := db.Begin()
	if err != nil {
		return false
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ftsSchema); err != nil {
		slog.Warn("SQLite 不支持 FTS5 trigram，消息搜索改用逐条匹配（只搜每个群最新的 5000 条）", "err", err)
		return false
	}
	return tx.Commit() == nil
}

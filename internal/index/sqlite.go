// Package index manages the SQLite cache of Claude Code sessions.
package index

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/parser"
	_ "modernc.org/sqlite"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS sessions (
	session_id TEXT PRIMARY KEY,
	project_dir TEXT NOT NULL,
	jsonl_path TEXT NOT NULL UNIQUE,
	jsonl_mtime INTEGER NOT NULL,
	start_time INTEGER NOT NULL,
	end_time INTEGER NOT NULL,
	message_count INTEGER NOT NULL,
	user_messages INTEGER NOT NULL,
	assistant_messages INTEGER NOT NULL,
	first_user_msg TEXT,
	last_user_msg TEXT,
	git_branch TEXT,
	claude_version TEXT,
	model TEXT,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sessions_start ON sessions(start_time DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_dir);

CREATE TABLE IF NOT EXISTS tool_uses (
	session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
	tool_name TEXT NOT NULL,
	count INTEGER NOT NULL,
	PRIMARY KEY (session_id, tool_name)
);

CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
	session_id UNINDEXED,
	role,
	content,
	tokenize = 'porter unicode61'
);

CREATE TABLE IF NOT EXISTS last_index_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_cache (
	session_id TEXT PRIMARY KEY REFERENCES sessions(session_id) ON DELETE CASCADE,
	jsonl_mtime INTEGER NOT NULL,
	summary TEXT,
	embedding BLOB,
	topic_cluster INTEGER NOT NULL DEFAULT -1,
	topic_label TEXT,
	generated_at INTEGER NOT NULL
);
`

const currentSchemaVersion = "1"

// DB wraps a *sql.DB for the index store.
type DB struct {
	conn *sql.DB
	path string
}

// Open opens (or creates) the SQLite database at path and ensures the schema is current.
// It also creates the parent directory if missing.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	conn, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	if _, err := conn.Exec(schemaSQL); err != nil {
		conn.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if _, err := conn.Exec(`INSERT OR IGNORE INTO last_index_meta(key, value) VALUES('schema_version', ?)`, currentSchemaVersion); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set schema version: %w", err)
	}
	return &DB{conn: conn, path: path}, nil
}

// Close closes the underlying connection.
func (db *DB) Close() error {
	if db.conn == nil {
		return nil
	}
	return db.conn.Close()
}

const upsertSessionSQL = `
INSERT INTO sessions (
	session_id, project_dir, jsonl_path, jsonl_mtime,
	start_time, end_time, message_count, user_messages, assistant_messages,
	first_user_msg, last_user_msg, git_branch, claude_version, model,
	input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(session_id) DO UPDATE SET
	project_dir=excluded.project_dir,
	jsonl_path=excluded.jsonl_path,
	jsonl_mtime=excluded.jsonl_mtime,
	start_time=excluded.start_time,
	end_time=excluded.end_time,
	message_count=excluded.message_count,
	user_messages=excluded.user_messages,
	assistant_messages=excluded.assistant_messages,
	first_user_msg=excluded.first_user_msg,
	last_user_msg=excluded.last_user_msg,
	git_branch=excluded.git_branch,
	claude_version=excluded.claude_version,
	model=excluded.model,
	input_tokens=excluded.input_tokens,
	output_tokens=excluded.output_tokens,
	cache_creation_tokens=excluded.cache_creation_tokens,
	cache_read_tokens=excluded.cache_read_tokens
`

// Upsert inserts or updates a session and replaces its tool_uses.
func (db *DB) Upsert(s *model.Session) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(upsertSessionSQL,
		s.SessionID, s.ProjectDir, s.JSONLPath, s.JSONLMtime.UnixNano(),
		s.StartTime.UnixNano(), s.EndTime.UnixNano(),
		s.MessageCount, s.UserMessages, s.AssistantMessages,
		s.FirstUserMsg, s.LastUserMsg, s.GitBranch, s.ClaudeVersion, s.Model,
		s.InputTokens, s.OutputTokens, s.CacheCreationTokens, s.CacheReadTokens,
	); err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM tool_uses WHERE session_id = ?`, s.SessionID); err != nil {
		return fmt.Errorf("clear tool_uses: %w", err)
	}
	for name, count := range s.ToolCalls {
		if _, err := tx.Exec(`INSERT INTO tool_uses (session_id, tool_name, count) VALUES (?,?,?)`, s.SessionID, name, count); err != nil {
			return fmt.Errorf("insert tool_use: %w", err)
		}
	}
	return tx.Commit()
}

const selectSessionSQL = `
SELECT session_id, project_dir, jsonl_path, jsonl_mtime,
	start_time, end_time, message_count, user_messages, assistant_messages,
	first_user_msg, last_user_msg, git_branch, claude_version, model,
	input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens
FROM sessions`

// GetByID returns a single session by ID, or sql.ErrNoRows if not found.
func (db *DB) GetByID(id string) (*model.Session, error) {
	rows, err := db.conn.Query(selectSessionSQL+` WHERE session_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	s, err := scanSession(rows)
	if err != nil {
		return nil, err
	}
	if err := db.loadToolCalls(s); err != nil {
		return nil, err
	}
	return s, nil
}

// ListSessions returns all sessions ordered by start_time desc.
func (db *DB) ListSessions() ([]*model.Session, error) {
	rows, err := db.conn.Query(selectSessionSQL + ` ORDER BY start_time DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, s := range out {
		if err := db.loadToolCalls(s); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func scanSession(rows *sql.Rows) (*model.Session, error) {
	var s model.Session
	var mtime, start, end int64
	if err := rows.Scan(
		&s.SessionID, &s.ProjectDir, &s.JSONLPath, &mtime,
		&start, &end, &s.MessageCount, &s.UserMessages, &s.AssistantMessages,
		&s.FirstUserMsg, &s.LastUserMsg, &s.GitBranch, &s.ClaudeVersion, &s.Model,
		&s.InputTokens, &s.OutputTokens, &s.CacheCreationTokens, &s.CacheReadTokens,
	); err != nil {
		return nil, err
	}
	s.JSONLMtime = time.Unix(0, mtime)
	s.StartTime = time.Unix(0, start)
	s.EndTime = time.Unix(0, end)
	s.ToolCalls = map[string]int{}
	return &s, nil
}

func (db *DB) loadToolCalls(s *model.Session) error {
	rows, err := db.conn.Query(`SELECT tool_name, count FROM tool_uses WHERE session_id = ?`, s.SessionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return err
		}
		s.ToolCalls[name] = count
	}
	return rows.Err()
}

// AICache representa o cache de geração AI de uma session.
type AICache struct {
	SessionID    string
	JSONLMtime   int64
	Summary      string
	Embedding    []byte
	TopicCluster int
	TopicLabel   string
	GeneratedAt  int64
}

// AICacheGet busca o cache pra uma session.
func (db *DB) AICacheGet(sessionID string) (*AICache, error) {
	row := db.conn.QueryRow(`SELECT session_id, jsonl_mtime, summary, embedding, topic_cluster, topic_label, generated_at FROM ai_cache WHERE session_id = ?`, sessionID)
	var c AICache
	var label, summary sql.NullString
	if err := row.Scan(&c.SessionID, &c.JSONLMtime, &summary, &c.Embedding, &c.TopicCluster, &label, &c.GeneratedAt); err != nil {
		return nil, err
	}
	c.Summary = summary.String
	c.TopicLabel = label.String
	return &c, nil
}

// AICacheUpsert grava ou atualiza o cache.
func (db *DB) AICacheUpsert(c *AICache) error {
	const q = `
INSERT INTO ai_cache (session_id, jsonl_mtime, summary, embedding, topic_cluster, topic_label, generated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
	jsonl_mtime=excluded.jsonl_mtime,
	summary=COALESCE(NULLIF(excluded.summary,''), ai_cache.summary),
	embedding=COALESCE(excluded.embedding, ai_cache.embedding),
	topic_cluster=excluded.topic_cluster,
	topic_label=COALESCE(NULLIF(excluded.topic_label,''), ai_cache.topic_label),
	generated_at=excluded.generated_at
`
	_, err := db.conn.Exec(q, c.SessionID, c.JSONLMtime, c.Summary, c.Embedding, c.TopicCluster, c.TopicLabel, c.GeneratedAt)
	return err
}

// AICacheList retorna todos os caches existentes.
func (db *DB) AICacheList() ([]*AICache, error) {
	rows, err := db.conn.Query(`SELECT session_id, jsonl_mtime, summary, embedding, topic_cluster, topic_label, generated_at FROM ai_cache`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AICache
	for rows.Next() {
		var c AICache
		var label, summary sql.NullString
		if err := rows.Scan(&c.SessionID, &c.JSONLMtime, &summary, &c.Embedding, &c.TopicCluster, &label, &c.GeneratedAt); err != nil {
			return nil, err
		}
		c.Summary = summary.String
		c.TopicLabel = label.String
		out = append(out, &c)
	}
	return out, rows.Err()
}

// AICacheUpdateCluster grava cluster + label num batch eficiente.
func (db *DB) AICacheUpdateCluster(sessionID string, cluster int, label string) error {
	_, err := db.conn.Exec(`UPDATE ai_cache SET topic_cluster = ?, topic_label = ? WHERE session_id = ?`, cluster, label, sessionID)
	return err
}

// SearchResult é um match retornado por SearchFTS ou SearchLike.
type SearchResult struct {
	SessionID string
	Role      string
	Snippet   string
	Rank      float64
}

// HasFTS5 returns true if the loaded SQLite was built with FTS5 support.
func (db *DB) HasFTS5() bool {
	rows, err := db.conn.Query(`PRAGMA compile_options`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var opt string
		if err := rows.Scan(&opt); err == nil && opt == "ENABLE_FTS5" {
			return true
		}
	}
	// modernc/sqlite always ships FTS5; a more robust probe is to try a query
	if _, err := db.conn.Exec(`SELECT 1 FROM messages_fts LIMIT 0`); err == nil {
		return true
	}
	return false
}

// IndexMessages inserts message rows into the FTS5 virtual table.
// Existing rows for the same session_id are deleted first.
func (db *DB) IndexMessages(msgs []parser.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	bySession := map[string]bool{}
	for _, m := range msgs {
		bySession[m.SessionID] = true
	}
	for sid := range bySession {
		if _, err := tx.Exec(`DELETE FROM messages_fts WHERE session_id = ?`, sid); err != nil {
			return fmt.Errorf("clear fts: %w", err)
		}
	}
	for _, m := range msgs {
		if _, err := tx.Exec(`INSERT INTO messages_fts(session_id, role, content) VALUES (?,?,?)`,
			m.SessionID, m.Role, m.Content,
		); err != nil {
			return fmt.Errorf("insert fts: %w", err)
		}
	}
	return tx.Commit()
}

// SearchFTS runs an FTS5 MATCH query and returns ranked results.
func (db *DB) SearchFTS(query string) ([]SearchResult, error) {
	const sql = `
		SELECT session_id, role,
			snippet(messages_fts, 2, '[', ']', '…', 16) AS snippet,
			rank
		FROM messages_fts
		WHERE messages_fts MATCH ?
		ORDER BY rank
		LIMIT 100
	`
	rows, err := db.conn.Query(sql, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.SessionID, &r.Role, &r.Snippet, &r.Rank); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SearchLike is a fallback for when FTS5 isn't available.
func (db *DB) SearchLike(query string) ([]SearchResult, error) {
	const sql = `
		SELECT session_id, role, content, 0.0
		FROM messages_fts
		WHERE content LIKE ?
		LIMIT 100
	`
	rows, err := db.conn.Query(sql, "%"+query+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.SessionID, &r.Role, &r.Snippet, &r.Rank); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

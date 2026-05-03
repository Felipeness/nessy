package index

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/felipeness/nessy/internal/parser"
)

// ReindexStats reports counts from a reindex pass.
type ReindexStats struct {
	Scanned int
	New     int
	Updated int
	Removed int
}

// parserVersion bumpa quando extractText muda — invalida FTS de todas sessions
// e força repopular. Vai pra last_index_meta.parser_version.
//   v2: original
//   v3: adiciona sidechain_turns/sidechain_agents (re-Upsert metadata)
//   v4: tool_events table populada pra loop detection
//   v5: tool_events ganha input_preview pra UI
//   v6: session_files + resolved_at_turn (Studio Meta tab)
const parserVersion = "6"

// IngestFilter define quais sessions skipar durante reindex. Zero-value =
// indexa tudo (back-compat com chamadas antigas Reindex).
type IngestFilter struct {
	SkipWarmup      bool     // skip "I am Claude Code..."
	SkipClearOnly   bool     // skip sessions /clear-only
	MinMessages     int      // skip < N msgs (0 = sem filtro)
	ExcludeProjects []string // path substrings — qualquer match skipa
}

func (f IngestFilter) shouldSkip(s *parser.Session) bool {
	if f.MinMessages > 0 && s.MessageCount < f.MinMessages {
		return true
	}
	if f.SkipWarmup && parser.IsWarmup(s) {
		return true
	}
	if f.SkipClearOnly && parser.IsClearOnly(s) {
		return true
	}
	for _, ex := range f.ExcludeProjects {
		if ex == "" {
			continue
		}
		if strings.Contains(s.ProjectDir, ex) {
			return true
		}
	}
	return false
}

// Reindex walks root looking for *.jsonl files (excluding subagents/),
// re-parsing only those whose mtime is newer than the cached value.
// Sessions whose JSONL no longer exists on disk are deleted.
// Sem filter — back-compat com callers antigos.
func (db *DB) Reindex(root string) (ReindexStats, error) {
	return db.ReindexFiltered(root, IngestFilter{})
}

// ReindexFiltered roda reindex aplicando o filter — skipped sessions são
// nem indexadas nem mantidas (deletadas se já existiam).
func (db *DB) ReindexFiltered(root string, filter IngestFilter) (ReindexStats, error) {
	var stats ReindexStats
	seen := map[string]bool{}

	// Se parser_version mudou, nuke FTS + força re-Upsert da metadata pra
	// popular colunas novas (ex: sidechain_*).
	var stored string
	_ = db.conn.QueryRow(`SELECT value FROM last_index_meta WHERE key = 'parser_version'`).Scan(&stored)
	parserVersionChanged := stored != parserVersion
	if parserVersionChanged {
		if _, err := db.conn.Exec(`DELETE FROM messages_fts`); err != nil {
			return stats, err
		}
		_, _ = db.conn.Exec(
			`INSERT OR REPLACE INTO last_index_meta(key, value) VALUES('parser_version', ?)`,
			parserVersion,
		)
	}

	// Preload (path → session_id, mtime) num map único pra evitar N queries
	// SQLite no hot path do walk. Em repos com milhares de JSONLs, esse
	// preload sozinho corta segundos do startup.
	type cached struct {
		sid   string
		mtime int64
	}
	cache := map[string]cached{}
	if rows, err := db.conn.Query(`SELECT jsonl_path, session_id, jsonl_mtime FROM sessions`); err == nil {
		for rows.Next() {
			var p, sid string
			var mtime int64
			if rows.Scan(&p, &sid, &mtime) == nil {
				cache[p] = cached{sid, mtime}
			}
		}
		rows.Close()
	}

	// FTS counts por session — também preloaded pra evitar query por arquivo.
	ftsCounts := map[string]int{}
	if rows, err := db.conn.Query(`SELECT session_id, COUNT(*) FROM messages_fts GROUP BY session_id`); err == nil {
		for rows.Next() {
			var sid string
			var n int
			if rows.Scan(&sid, &n) == nil {
				ftsCounts[sid] = n
			}
		}
		rows.Close()
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"subagents"+string(filepath.Separator)) {
			return nil
		}
		stats.Scanned++
		seen[path] = true

		info, err := d.Info()
		if err != nil {
			return nil
		}
		mtime := info.ModTime().UnixNano()

		c, hit := cache[path]
		if hit && c.mtime == mtime && !parserVersionChanged {
			if ftsCounts[c.sid] > 0 {
				return nil // up-to-date completo
			}
			// Re-indexa só as messages, mantém metadata
			if msgs, err := parser.ParseMessages(path); err == nil {
				_ = db.IndexMessages(msgs)
			}
			return nil
		}
		if hit {
			stats.Updated++
		} else {
			stats.New++
		}

		s, err := parser.ParseSession(path)
		if err != nil || s == nil || s.MessageCount == 0 {
			return nil
		}
		// Aplica ingest filter — skipa warmup/clear/excluded paths
		if filter.shouldSkip(s) {
			// Se já existia no DB, deleta (config mudou e sessão virou ineligível)
			_, _ = db.conn.Exec(`DELETE FROM sessions WHERE jsonl_path = ?`, path)
			return nil
		}
		s.JSONLMtime = info.ModTime()
		if err := db.Upsert(s); err != nil {
			return nil
		}
		msgs, err := parser.ParseMessages(path)
		if err == nil {
			_ = db.IndexMessages(msgs)
		}
		// tool_events pra loop detection retroativa
		if events, err := parser.ParseToolEvents(path); err == nil && len(events) > 0 {
			_ = db.IndexToolEvents(s.SessionID, events)
		}
		// session_files pra Studio Meta tab (retrabalho rate, cost per feature)
		if ops, err := parser.ParseFileOps(path); err == nil && len(ops) > 0 {
			_ = db.IndexFileOps(s.SessionID, ops)
		}
		return nil
	})
	if walkErr != nil {
		return stats, walkErr
	}

	rows, err := db.conn.Query(`SELECT jsonl_path FROM sessions`)
	if err != nil {
		return stats, err
	}
	var stale []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil && !seen[p] {
			stale = append(stale, p)
		}
	}
	rows.Close()
	for _, p := range stale {
		if _, err := db.conn.Exec(`DELETE FROM sessions WHERE jsonl_path = ?`, p); err == nil {
			stats.Removed++
		}
	}
	return stats, nil
}

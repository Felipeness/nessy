package index

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/felipeness/claude-history/internal/parser"
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
const parserVersion = "4"

// Reindex walks root looking for *.jsonl files (excluding subagents/),
// re-parsing only those whose mtime is newer than the cached value.
// Sessions whose JSONL no longer exists on disk are deleted.
func (db *DB) Reindex(root string) (ReindexStats, error) {
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

		var cachedMtime int64
		var cachedSID string
		row := db.conn.QueryRow(`SELECT session_id, jsonl_mtime FROM sessions WHERE jsonl_path = ?`, path)
		scanErr := row.Scan(&cachedSID, &cachedMtime)
		if scanErr == nil && cachedMtime == mtime && !parserVersionChanged {
			// Cache de metadata bate, mas o FTS pode estar subpopulado por
			// versão antiga do parser. Verifica e força reindex de msgs se
			// necessário (sem regravar metadata da session).
			var ftsCount int
			_ = db.conn.QueryRow(`SELECT COUNT(*) FROM messages_fts WHERE session_id = ?`, cachedSID).Scan(&ftsCount)
			if ftsCount > 0 {
				return nil // up-to-date completo
			}
			// Re-indexa só as messages, mantém metadata
			if msgs, err := parser.ParseMessages(path); err == nil {
				_ = db.IndexMessages(msgs)
			}
			return nil
		}
		if scanErr == nil {
			stats.Updated++
		} else {
			stats.New++
		}

		s, err := parser.ParseSession(path)
		if err != nil || s == nil || s.MessageCount == 0 {
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

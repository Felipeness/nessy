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

// Reindex walks root looking for *.jsonl files (excluding subagents/),
// re-parsing only those whose mtime is newer than the cached value.
// Sessions whose JSONL no longer exists on disk are deleted.
func (db *DB) Reindex(root string) (ReindexStats, error) {
	var stats ReindexStats
	seen := map[string]bool{}

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
		row := db.conn.QueryRow(`SELECT jsonl_mtime FROM sessions WHERE jsonl_path = ?`, path)
		switch err := row.Scan(&cachedMtime); {
		case err == nil && cachedMtime == mtime:
			return nil // up-to-date
		case err == nil:
			stats.Updated++
		default:
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

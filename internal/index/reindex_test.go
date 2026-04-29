package index

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleJSONL = `{"type":"user","sessionId":"reidx-1","cwd":"/tmp/reidx","timestamp":"2026-04-28T10:00:00Z","message":{"role":"user","content":"primeira pergunta"}}
{"type":"assistant","sessionId":"reidx-1","cwd":"/tmp/reidx","timestamp":"2026-04-28T10:00:01Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"primeira resposta"}],"usage":{"input_tokens":10,"output_tokens":5}}}
`

func TestReindex_indexesNewFiles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	projDir := t.TempDir()
	jsonlPath := filepath.Join(projDir, "reidx-1.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(sampleJSONL), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := db.Reindex(projDir)
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if stats.New != 1 {
		t.Errorf("stats.New = %d, want 1", stats.New)
	}
	if stats.Updated != 0 {
		t.Errorf("stats.Updated = %d, want 0", stats.Updated)
	}

	stats, err = db.Reindex(projDir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.New != 0 || stats.Updated != 0 {
		t.Errorf("idempotent reindex changed: new=%d updated=%d", stats.New, stats.Updated)
	}
}

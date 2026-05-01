package index

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/parser"
)

func TestOpen_createsSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var version string
	err = db.conn.QueryRow(`SELECT value FROM last_index_meta WHERE key = 'schema_version'`).Scan(&version)
	if err != nil {
		t.Fatalf("schema_version not set: %v", err)
	}
	if version != "1" {
		t.Errorf("schema_version = %q, want 1", version)
	}

	var name string
	err = db.conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='sessions'`).Scan(&name)
	if err != nil {
		t.Fatalf("table sessions not created: %v", err)
	}
}

func TestOpen_isIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db1, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db1.Close()

	db2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer db2.Close()

	var version string
	err = db2.conn.QueryRow(`SELECT value FROM last_index_meta WHERE key = 'schema_version'`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != "1" {
		t.Errorf("re-open schema_version = %q, want 1", version)
	}
}

func TestUpsertAndList(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := &model.Session{
		SessionID:         "abc-123",
		ProjectDir:        "/tmp/proj",
		JSONLPath:         "/tmp/proj/abc-123.jsonl",
		JSONLMtime:        time.Unix(1700000000, 0),
		StartTime:         time.Unix(1700000010, 0),
		EndTime:           time.Unix(1700000900, 0),
		MessageCount:      10,
		UserMessages:      4,
		AssistantMessages: 6,
		FirstUserMsg:      "hello",
		LastUserMsg:       "thanks",
		GitBranch:         "main",
		Model:             "claude-sonnet-4-6",
		InputTokens:       100,
		OutputTokens:      50,
		ToolCalls:         map[string]int{"Bash": 3, "Edit": 2},
	}
	if err := db.Upsert(s); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := db.GetByID("abc-123")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.FirstUserMsg != "hello" {
		t.Errorf("FirstUserMsg = %q, want hello", got.FirstUserMsg)
	}
	if got.ToolCalls["Bash"] != 3 {
		t.Errorf("Bash count = %d, want 3", got.ToolCalls["Bash"])
	}

	s.MessageCount = 20
	if err := db.Upsert(s); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetByID("abc-123")
	if got.MessageCount != 20 {
		t.Errorf("MessageCount após re-upsert = %d, want 20", got.MessageCount)
	}

	all, err := db.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("ListSessions = %d sessions, want 1", len(all))
	}
}

func TestIndexMessagesAndSearch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if !db.HasFTS5() {
		t.Skip("FTS5 não disponível, pulando teste")
	}

	msgs := []parser.Message{
		{SessionID: "s1", Role: "user", Content: "como configurar postgres triggers"},
		{SessionID: "s1", Role: "assistant", Content: "voce pode criar trigger antes do insert"},
		{SessionID: "s2", Role: "user", Content: "qual o comando docker compose down"},
	}
	if err := db.IndexMessages(msgs); err != nil {
		t.Fatalf("IndexMessages: %v", err)
	}

	results, err := db.SearchFTS("postgres trigger")
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("SearchFTS retornou 0 resultados")
	}
	if results[0].SessionID != "s1" {
		t.Errorf("SessionID = %q, want s1", results[0].SessionID)
	}
}

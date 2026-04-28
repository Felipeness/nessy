# TUI base — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Adicionar TUI Bubble Tea com tabs Search/Recent/Stats ao `claude-history`, evoluindo o indexer pra cache SQLite com FTS5 e métricas de tokens/custo.

**Architecture:** Indexer Go expõe Sessions via SQLite (com FTS5). TUI Bubble Tea consome o cache, com layout adaptativo (multi-pane ≥120 cols, single <120). Refresh manual via `r`. CLI `list/show/fzf` da Fase 1 mantida sem regressão.

**Tech Stack:** Go 1.26, Bubble Tea, Lipgloss, Bubbles, modernc.org/sqlite (FTS5), BurntSushi/toml.

**Spec:** [`docs/superpowers/specs/2026-04-28-tui-base-design.md`](../specs/2026-04-28-tui-base-design.md)

---

## File Structure

```
claude-history/
├── main.go                       # MODIFY — adiciona subcomando "tui"
├── go.mod                        # MODIFY — adiciona deps
├── internal/
│   ├── model/
│   │   └── session.go            # NEW — Session struct compartilhada (movida de parser)
│   ├── parser/
│   │   └── jsonl.go              # MODIFY — extrai usage tokens, model
│   ├── pricing/
│   │   ├── pricing.go            # NEW — TOML loader + cost calculator
│   │   └── pricing_test.go       # NEW
│   └── index/
│       ├── sqlite.go             # NEW — open/migrate/upsert/query
│       ├── sqlite_test.go        # NEW
│       ├── reindex.go            # NEW — scanner mtime-based
│       └── reindex_test.go       # NEW
└── tui/
    ├── app.go                    # NEW — Bubble Tea root + tab routing + adaptive layout
    ├── search.go                 # NEW — tab Search (metadata + FTS)
    ├── recent.go                 # NEW — tab Recent
    ├── stats.go                  # NEW — tab Stats
    ├── detail.go                 # NEW — detail panel reusável
    ├── style.go                  # NEW — lipgloss styles centralizados
    └── keys.go                   # NEW — keybind definitions
```

**Decomposição por milestone**: cada milestone produz software funcional e testável.

- **A. Foundation** (Tasks 1-3): refactor + tokens + pricing
- **B. SQLite cache** (Tasks 4-7): schema, ops, FTS5, reindex
- **C. TUI shell** (Tasks 8, 14, 15): Bubble Tea scaffold + adaptive layout + keybinds
- **D. TUI tabs** (Tasks 9-13): Recent, Search (2 modos), Stats (global + local)
- **E. Integration** (Tasks 16-20): resume, refresh, subcommand, golden tests, README

---

## Milestone A — Foundation

### Task 1: Mover Session struct pra `internal/model/session.go`

**Files:**
- Create: `internal/model/session.go`
- Modify: `internal/parser/jsonl.go` — remover struct local, importar `model.Session`

- [ ] **Step 1: Criar `internal/model/session.go` com a struct e helper**

```go
// Package model contains domain types shared across parser, index, pricing, and tui.
package model

import "time"

// Session is the indexed view of one Claude Code conversation.
type Session struct {
	SessionID            string         `json:"session_id"`
	ProjectDir           string         `json:"project_dir"`
	JSONLPath            string         `json:"jsonl_path"`
	JSONLMtime           time.Time      `json:"jsonl_mtime"`
	StartTime            time.Time      `json:"start_time"`
	EndTime              time.Time      `json:"end_time"`
	MessageCount         int            `json:"message_count"`
	UserMessages         int            `json:"user_messages"`
	AssistantMessages    int            `json:"assistant_messages"`
	FirstUserMsg         string         `json:"first_user_msg"`
	LastUserMsg          string         `json:"last_user_msg"`
	GitBranch            string         `json:"git_branch"`
	ClaudeVersion        string         `json:"claude_version"`
	Model                string         `json:"model"`
	InputTokens          int64          `json:"input_tokens"`
	OutputTokens         int64          `json:"output_tokens"`
	CacheCreationTokens  int64          `json:"cache_creation_tokens"`
	CacheReadTokens      int64          `json:"cache_read_tokens"`
	ToolCalls            map[string]int `json:"tool_calls"`
}

// Duration returns the session wall-clock duration.
func (s Session) Duration() time.Duration {
	return s.EndTime.Sub(s.StartTime)
}

// TotalTokens returns input+output+cache_creation+cache_read.
func (s Session) TotalTokens() int64 {
	return s.InputTokens + s.OutputTokens + s.CacheCreationTokens + s.CacheReadTokens
}
```

- [ ] **Step 2: Atualizar `internal/parser/jsonl.go` pra usar `model.Session`**

Remover a struct `Session` definida no parser. Trocar tipo de retorno de `ParseSession` e `ListSessions` pra `*model.Session` / `[]*model.Session`. Adicionar import `github.com/felipeness/claude-history/internal/model`.

- [ ] **Step 3: Atualizar `main.go` pra usar `model.Session`**

Trocar `parser.Session` por `model.Session` em todas as referências (loadSorted, printTable, cmdShow, cmdList, cmdFzf).

- [ ] **Step 4: Build e smoke test**

```bash
cd ~/Desktop/Projects/claude-history
go build -o /tmp/ch-test .
/tmp/ch-test list | head -3
```

Expected: tabela com 4+ sessions, sem erro.

- [ ] **Step 5: Commit**

```bash
git add internal/model/session.go internal/parser/jsonl.go main.go
git commit -m "refactor: move Session struct para internal/model"
```

---

### Task 2: Estender parser pra extrair usage tokens e model

**Files:**
- Modify: `internal/parser/jsonl.go`
- Create: `internal/parser/jsonl_test.go`

- [ ] **Step 1: Escrever teste falhando**

```go
// internal/parser/jsonl_test.go
package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSession_extractsTokensAndModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	const fixture = `{"type":"user","sessionId":"abc","cwd":"/tmp","timestamp":"2026-04-28T10:00:00Z","message":{"role":"user","content":"oi"}}
{"type":"assistant","sessionId":"abc","cwd":"/tmp","timestamp":"2026-04-28T10:00:01Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"oi de volta"}],"usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":200,"cache_read_input_tokens":1000}}}
`
	if err := os.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}
	if s.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want claude-sonnet-4-6", s.Model)
	}
	if s.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", s.InputTokens)
	}
	if s.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", s.OutputTokens)
	}
	if s.CacheCreationTokens != 200 {
		t.Errorf("CacheCreationTokens = %d, want 200", s.CacheCreationTokens)
	}
	if s.CacheReadTokens != 1000 {
		t.Errorf("CacheReadTokens = %d, want 1000", s.CacheReadTokens)
	}
}
```

- [ ] **Step 2: Rodar teste pra confirmar que falha**

```bash
go test ./internal/parser/ -run TestParseSession_extractsTokens -v
```
Expected: FAIL (campos novos não existem ainda — não, espera, eles já existem na struct; o que falha é a extração no parser).

Na verdade vai falhar com `Model = "", want claude-sonnet-4-6` porque o parser não lê esses campos ainda.

- [ ] **Step 3: Adicionar parsing de usage e model**

Modificar `internal/parser/jsonl.go`:

1. Estender `rawMessage` pra incluir `Model` e `Usage`:

```go
type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model,omitempty"`
	Usage   *rawUsage       `json:"usage,omitempty"`
}

type rawUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}
```

2. No bloco `case "assistant":` dentro de `ParseSession`, somar tokens e capturar model:

```go
case "assistant":
	s.AssistantMessages++
	s.MessageCount++
	if ev.Message != nil {
		if ev.Message.Model != "" && s.Model == "" {
			s.Model = ev.Message.Model
		}
		if ev.Message.Usage != nil {
			s.InputTokens += ev.Message.Usage.InputTokens
			s.OutputTokens += ev.Message.Usage.OutputTokens
			s.CacheCreationTokens += ev.Message.Usage.CacheCreationInputTokens
			s.CacheReadTokens += ev.Message.Usage.CacheReadInputTokens
		}
		countToolUses(ev.Message.Content, s.ToolCalls)
	}
```

(Note: o campo da struct se chama `AssistantMessages` agora — ajustar em qualquer outro lugar que usava `AssistantMsgs` da Task 1.)

- [ ] **Step 4: Rodar teste pra verificar que passa**

```bash
go test ./internal/parser/ -run TestParseSession_extractsTokens -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/parser/jsonl.go internal/parser/jsonl_test.go
git commit -m "feat: parser extrai tokens e model de assistant messages"
```

---

### Task 3: Pricing TOML loader + cost calculator

**Files:**
- Create: `internal/pricing/pricing.go`
- Create: `internal/pricing/pricing_test.go`
- Modify: `go.mod` — add `github.com/BurntSushi/toml`

- [ ] **Step 1: Adicionar dependência**

```bash
go get github.com/BurntSushi/toml
```

- [ ] **Step 2: Escrever teste falhando**

```go
// internal/pricing/pricing_test.go
package pricing

import (
	"path/filepath"
	"testing"

	"github.com/felipeness/claude-history/internal/model"
)

func TestLoadAndCalculate(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "pricing.toml")
	const fixture = `default_currency = "USD"
brl_rate = 5.0

[[models]]
name = "claude-sonnet-4-6"
input_per_mtok = 3.00
output_per_mtok = 15.00
cache_creation_per_mtok = 3.75
cache_read_per_mtok = 0.30
`
	if err := writeFile(tomlPath, fixture); err != nil {
		t.Fatal(err)
	}

	p, err := Load(tomlPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	s := &model.Session{
		Model:               "claude-sonnet-4-6",
		InputTokens:         1_000_000,
		OutputTokens:        100_000,
		CacheCreationTokens: 50_000,
		CacheReadTokens:     500_000,
	}

	cost, ok := p.Cost(s)
	if !ok {
		t.Fatal("Cost returned ok=false for known model")
	}
	want := 3.00 + 1.50 + 0.1875 + 0.15 // = 4.8375 USD
	if abs(cost.USD-want) > 0.0001 {
		t.Errorf("cost.USD = %.4f, want %.4f", cost.USD, want)
	}
	if abs(cost.BRL-want*5.0) > 0.0001 {
		t.Errorf("cost.BRL = %.4f, want %.4f", cost.BRL, want*5.0)
	}
}

func TestCost_unknownModelReturnsFalse(t *testing.T) {
	p := &Pricing{Models: map[string]Model{}}
	_, ok := p.Cost(&model.Session{Model: "claude-future-99"})
	if ok {
		t.Error("expected ok=false for unknown model")
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
```

(Adicionar import `os` no topo.)

- [ ] **Step 3: Rodar teste pra confirmar que falha**

```bash
go test ./internal/pricing/ -v
```
Expected: FAIL (package não existe).

- [ ] **Step 4: Implementar `internal/pricing/pricing.go`**

```go
// Package pricing carrega snapshot de preços por modelo do TOML e calcula
// custo de uma Session somando input/output/cache tokens.
package pricing

import (
	"github.com/BurntSushi/toml"
	"github.com/felipeness/claude-history/internal/model"
)

type Model struct {
	Name                 string  `toml:"name"`
	InputPerMTok         float64 `toml:"input_per_mtok"`
	OutputPerMTok        float64 `toml:"output_per_mtok"`
	CacheCreationPerMTok float64 `toml:"cache_creation_per_mtok"`
	CacheReadPerMTok     float64 `toml:"cache_read_per_mtok"`
}

type Pricing struct {
	DefaultCurrency string             `toml:"default_currency"`
	BRLRate         float64            `toml:"brl_rate"`
	ModelsList      []Model            `toml:"models"`
	Models          map[string]Model   `toml:"-"`
}

type Cost struct {
	USD float64
	BRL float64 // 0 if BRLRate not set
}

// Load reads a pricing TOML file and indexes models by name.
func Load(path string) (*Pricing, error) {
	var p Pricing
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return nil, err
	}
	if p.DefaultCurrency == "" {
		p.DefaultCurrency = "USD"
	}
	p.Models = make(map[string]Model, len(p.ModelsList))
	for _, m := range p.ModelsList {
		p.Models[m.Name] = m
	}
	return &p, nil
}

// Cost returns the USD (and optionally BRL) cost of the given session based on
// its model and token counts. Returns ok=false if the model is unknown.
func (p *Pricing) Cost(s *model.Session) (Cost, bool) {
	m, ok := p.Models[s.Model]
	if !ok {
		return Cost{}, false
	}
	usd := (float64(s.InputTokens)*m.InputPerMTok +
		float64(s.OutputTokens)*m.OutputPerMTok +
		float64(s.CacheCreationTokens)*m.CacheCreationPerMTok +
		float64(s.CacheReadTokens)*m.CacheReadPerMTok) / 1_000_000.0
	out := Cost{USD: usd}
	if p.BRLRate > 0 {
		out.BRL = usd * p.BRLRate
	}
	return out, true
}
```

- [ ] **Step 5: Rodar teste**

```bash
go test ./internal/pricing/ -v
```
Expected: PASS, dois testes verdes.

- [ ] **Step 6: Commit**

```bash
git add internal/pricing/ go.mod go.sum
git commit -m "feat: pricing loader e cost calculator com USD/BRL"
```

---

## Milestone B — SQLite cache

### Task 4: SQLite open + schema migration

**Files:**
- Create: `internal/index/sqlite.go`
- Create: `internal/index/sqlite_test.go`
- Modify: `go.mod` — add `modernc.org/sqlite`

- [ ] **Step 1: Adicionar dependência**

```bash
go get modernc.org/sqlite
```

- [ ] **Step 2: Escrever teste falhando**

```go
// internal/index/sqlite_test.go
package index

import (
	"path/filepath"
	"testing"
)

func TestOpen_createsSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// schema_version deve ser 1
	var version string
	err = db.conn.QueryRow(`SELECT value FROM last_index_meta WHERE key = 'schema_version'`).Scan(&version)
	if err != nil {
		t.Fatalf("schema_version not set: %v", err)
	}
	if version != "1" {
		t.Errorf("schema_version = %q, want 1", version)
	}

	// tabela sessions deve existir
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
```

- [ ] **Step 3: Rodar teste**

```bash
go test ./internal/index/ -v
```
Expected: FAIL (package não existe).

- [ ] **Step 4: Implementar `internal/index/sqlite.go`**

```go
// Package index manages the SQLite cache of Claude Code sessions.
package index

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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
```

- [ ] **Step 5: Rodar teste**

```bash
go test ./internal/index/ -v
```
Expected: PASS, dois testes verdes.

- [ ] **Step 6: Commit**

```bash
git add internal/index/sqlite.go internal/index/sqlite_test.go go.mod go.sum
git commit -m "feat: sqlite open com schema migration idempotente"
```

---

### Task 5: Upsert + Query de sessions e tool_uses

**Files:**
- Modify: `internal/index/sqlite.go` — adicionar `Upsert`, `ListSessions`, `GetByID`
- Modify: `internal/index/sqlite_test.go`

- [ ] **Step 1: Escrever teste falhando**

Adicionar ao `sqlite_test.go`:

```go
func TestUpsertAndList(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := &model.Session{
		SessionID:    "abc-123",
		ProjectDir:   "/tmp/proj",
		JSONLPath:    "/tmp/proj/abc-123.jsonl",
		JSONLMtime:   time.Unix(1700000000, 0),
		StartTime:    time.Unix(1700000010, 0),
		EndTime:      time.Unix(1700000900, 0),
		MessageCount: 10,
		UserMessages: 4,
		AssistantMessages: 6,
		FirstUserMsg: "hello",
		LastUserMsg:  "thanks",
		GitBranch:    "main",
		Model:        "claude-sonnet-4-6",
		InputTokens:  100,
		OutputTokens: 50,
		ToolCalls:    map[string]int{"Bash": 3, "Edit": 2},
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

	// idempotência: upsert again com message_count atualizado
	s.MessageCount = 20
	if err := db.Upsert(s); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetByID("abc-123")
	if got.MessageCount != 20 {
		t.Errorf("MessageCount após re-upsert = %d, want 20", got.MessageCount)
	}

	// list ordenado desc por start_time
	all, err := db.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("ListSessions = %d sessions, want 1", len(all))
	}
}
```

(Adicionar imports `time` e `github.com/felipeness/claude-history/internal/model`.)

- [ ] **Step 2: Rodar teste**

```bash
go test ./internal/index/ -run TestUpsertAndList -v
```
Expected: FAIL (métodos não existem).

- [ ] **Step 3: Implementar `Upsert`, `GetByID`, `ListSessions`**

Adicionar ao `internal/index/sqlite.go`:

```go
import (
	"github.com/felipeness/claude-history/internal/model"
)

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
	for _, s := range out {
		if err := db.loadToolCalls(s); err != nil {
			return nil, err
		}
	}
	return out, rows.Err()
}

const selectSessionSQL = `
SELECT session_id, project_dir, jsonl_path, jsonl_mtime,
	start_time, end_time, message_count, user_messages, assistant_messages,
	first_user_msg, last_user_msg, git_branch, claude_version, model,
	input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens
FROM sessions`

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
```

(Adicionar import `time` no topo.)

- [ ] **Step 4: Rodar teste**

```bash
go test ./internal/index/ -v
```
Expected: PASS, todos os testes verdes.

- [ ] **Step 5: Commit**

```bash
git add internal/index/sqlite.go internal/index/sqlite_test.go
git commit -m "feat: upsert/list/get sessions com tool_uses transacional"
```

---

### Task 6: FTS5 — index e search com fallback LIKE

**Files:**
- Modify: `internal/index/sqlite.go` — adicionar `IndexMessages`, `SearchFTS`, `SearchLike`, `HasFTS5`
- Modify: `internal/index/sqlite_test.go`
- Modify: `internal/parser/jsonl.go` — exportar mensagens individuais (novo método `ParseMessages`)

- [ ] **Step 1: Estender parser pra retornar lista de mensagens**

Adicionar a `internal/parser/jsonl.go`:

```go
// Message é uma user/assistant message individual extraída do JSONL.
type Message struct {
	SessionID string
	Role      string
	Content   string
}

// ParseMessages reads the JSONL and returns flat user/assistant messages
// for FTS indexing. Tool result blocks are excluded.
func ParseMessages(path string) ([]Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Message
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var ev rawEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != "user" && ev.Type != "assistant" {
			continue
		}
		if ev.Message == nil {
			continue
		}
		text := extractText(ev.Message.Content)
		if text == "" {
			continue
		}
		out = append(out, Message{
			SessionID: ev.SessionID,
			Role:      ev.Type,
			Content:   text,
		})
	}
	return out, scanner.Err()
}
```

- [ ] **Step 2: Escrever teste falhando**

Adicionar a `sqlite_test.go`:

```go
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
```

(Import: `github.com/felipeness/claude-history/internal/parser`.)

- [ ] **Step 3: Rodar teste pra confirmar que falha**

```bash
go test ./internal/index/ -run TestIndexMessages -v
```
Expected: FAIL (métodos não existem).

- [ ] **Step 4: Implementar HasFTS5, IndexMessages, SearchFTS, SearchLike**

Adicionar ao `internal/index/sqlite.go`:

```go
// SearchResult é um match retornado por SearchFTS ou SearchLike.
type SearchResult struct {
	SessionID string
	Role      string
	Snippet   string
	Rank      float64 // BM25 score; menor = mais relevante (FTS5 retorna negativo)
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
```

(Import: `github.com/felipeness/claude-history/internal/parser`.)

- [ ] **Step 5: Rodar teste**

```bash
go test ./internal/index/ -v
```
Expected: PASS (incluindo o novo) ou SKIP se FTS5 não tiver — modernc/sqlite vem com FTS5 ativo by default, então deve PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/index/sqlite.go internal/index/sqlite_test.go internal/parser/jsonl.go
git commit -m "feat: indexa mensagens em fts5 e suporta search com fallback like"
```

---

### Task 7: Reindex scanner mtime-based

**Files:**
- Create: `internal/index/reindex.go`
- Create: `internal/index/reindex_test.go`

- [ ] **Step 1: Escrever teste falhando**

```go
// internal/index/reindex_test.go
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

	// segunda chamada sem mudança — não re-indexa
	stats, err = db.Reindex(projDir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.New != 0 || stats.Updated != 0 {
		t.Errorf("idempotent reindex changed: new=%d updated=%d", stats.New, stats.Updated)
	}
}
```

- [ ] **Step 2: Rodar teste**

```bash
go test ./internal/index/ -run TestReindex -v
```
Expected: FAIL (método Reindex não existe).

- [ ] **Step 3: Implementar `internal/index/reindex.go`**

```go
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

	// remove sessions cujo jsonl sumiu
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
```

- [ ] **Step 4: Rodar teste**

```bash
go test ./internal/index/ -v
```
Expected: PASS, todos verdes.

- [ ] **Step 5: Commit**

```bash
git add internal/index/reindex.go internal/index/reindex_test.go
git commit -m "feat: reindex scanner com invalidacao por mtime"
```

---

## Milestone C — TUI shell

### Task 8: Bubble Tea scaffold + tab routing

**Files:**
- Create: `tui/app.go`
- Create: `tui/keys.go`
- Create: `tui/style.go`
- Modify: `go.mod`

- [ ] **Step 1: Adicionar dependências**

```bash
go get github.com/charmbracelet/bubbletea github.com/charmbracelet/lipgloss github.com/charmbracelet/bubbles
```

- [ ] **Step 2: Criar `tui/keys.go`**

```go
package tui

import "github.com/charmbracelet/bubbles/key"

type keymap struct {
	NextTab  key.Binding
	PrevTab  key.Binding
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Search   key.Binding
	Refresh  key.Binding
	Help     key.Binding
	Quit     key.Binding
	Group    key.Binding
	Stats    key.Binding
	OpenDir  key.Binding
}

var keys = keymap{
	NextTab:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
	PrevTab:  key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev tab")),
	Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k", "up")),
	Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "down")),
	Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume")),
	Search:   key.NewBinding(key.WithKeys("/", "f"), key.WithHelp("/", "search")),
	Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:     key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "quit")),
	Group:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "group toggle")),
	Stats:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "stats toggle")),
	OpenDir:  key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "open dir")),
}
```

- [ ] **Step 3: Criar `tui/style.go`**

```go
package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorActive   = lipgloss.Color("46")  // green
	colorPaused   = lipgloss.Color("220") // yellow
	colorInactive = lipgloss.Color("245") // grey
	colorAccent   = lipgloss.Color("39")  // blue
	colorMuted    = lipgloss.Color("241")
)

var (
	tabBarStyle      = lipgloss.NewStyle().Padding(0, 1)
	tabActiveStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Underline(true)
	tabInactiveStyle = lipgloss.NewStyle().Foreground(colorMuted)
	statusBarStyle   = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Padding(0, 1)
	leftPaneStyle    = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, true, false, false).BorderForeground(colorMuted)
	rightPaneStyle   = lipgloss.NewStyle().Padding(0, 1)
)
```

- [ ] **Step 4: Criar `tui/app.go` — root model com tab routing**

```go
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/felipeness/claude-history/internal/index"
	"github.com/felipeness/claude-history/internal/pricing"
)

type tabID int

const (
	tabSearch tabID = iota
	tabRecent
	tabStats
)

var tabNames = []string{"Search", "Recent", "Stats"}

const wideCols = 120

// Model é o root da TUI.
type Model struct {
	db       *index.DB
	pricing  *pricing.Pricing
	width    int
	height   int
	activeTab tabID
	status    string
}

// New cria o root model.
func New(db *index.DB, p *pricing.Pricing) Model {
	return Model{db: db, pricing: p, activeTab: tabRecent, status: "ready"}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		switch {
		case keyMatch(msg, keys.Quit):
			return m, tea.Quit
		case keyMatch(msg, keys.NextTab):
			m.activeTab = (m.activeTab + 1) % 3
			return m, nil
		case keyMatch(msg, keys.PrevTab):
			m.activeTab = (m.activeTab + 2) % 3
			return m, nil
		}
	}
	return m, nil
}

func (m Model) View() string {
	tabBar := m.renderTabBar()
	body := m.renderBody()
	status := m.renderStatusBar()
	return lipgloss.JoinVertical(lipgloss.Left, tabBar, body, status)
}

func (m Model) renderTabBar() string {
	parts := make([]string, 0, len(tabNames))
	for i, name := range tabNames {
		if tabID(i) == m.activeTab {
			parts = append(parts, tabActiveStyle.Render(name))
		} else {
			parts = append(parts, tabInactiveStyle.Render(name))
		}
	}
	return tabBarStyle.Render(lipgloss.JoinHorizontal(lipgloss.Left, parts...))
}

func (m Model) renderBody() string {
	bodyHeight := m.height - 2
	switch m.activeTab {
	case tabSearch:
		return lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Render("(search tab — task 10)")
	case tabRecent:
		return lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Render("(recent tab — task 9)")
	case tabStats:
		return lipgloss.NewStyle().Width(m.width).Height(bodyHeight).Render("(stats tab — task 12)")
	}
	return ""
}

func (m Model) renderStatusBar() string {
	return statusBarStyle.Width(m.width).Render(fmt.Sprintf(" %s ", m.status))
}

func keyMatch(msg tea.KeyMsg, b interface{ Keys() []string }) bool {
	for _, k := range b.Keys() {
		if msg.String() == k {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Build e smoke test**

```bash
go build -o /tmp/ch-test .
# Roda só pra ver que compila — sem subcomando "tui" ainda, então o build basta
```
Expected: build OK.

- [ ] **Step 6: Commit**

```bash
git add tui/ go.mod go.sum
git commit -m "feat: bubble tea scaffold com tab routing"
```

---

### Task 9: Tab Recent — lista cronológica

**Files:**
- Create: `tui/recent.go`
- Modify: `tui/app.go` — substituir placeholder pela render real

- [ ] **Step 1: Implementar `tui/recent.go`**

```go
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/model"
)

const (
	activityActive = 5 * time.Minute
	activityPaused = time.Hour
)

type recentView struct {
	sessions []*model.Session
	cursor   int
	groupByProject bool
}

func newRecentView(sessions []*model.Session) recentView {
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].EndTime.After(sessions[j].EndTime)
	})
	return recentView{sessions: sessions}
}

func (v recentView) View(width, height int) string {
	if len(v.sessions) == 0 {
		return lipgloss.NewStyle().Width(width).Render("(nenhuma session encontrada)")
	}
	if v.groupByProject {
		return v.viewByProject(width)
	}
	return v.viewByTime(width)
}

func (v recentView) viewByTime(width int) string {
	now := time.Now()
	var b strings.Builder
	var lastBucket string
	for i, s := range v.sessions {
		bucket := timeBucket(now, s.EndTime)
		if bucket != lastBucket {
			fmt.Fprintf(&b, "─── %s ─────────────\n", bucket)
			lastBucket = bucket
		}
		marker := " "
		if i == v.cursor {
			marker = "▶"
		}
		fmt.Fprintf(&b, "%s %s\n", marker, formatRow(s, now, width-2))
	}
	return b.String()
}

func (v recentView) viewByProject(width int) string {
	groups := map[string][]*model.Session{}
	for _, s := range v.sessions {
		groups[s.ProjectDir] = append(groups[s.ProjectDir], s)
	}
	type entry struct {
		dir  string
		list []*model.Session
	}
	flat := make([]entry, 0, len(groups))
	for d, l := range groups {
		flat = append(flat, entry{d, l})
	}
	sort.Slice(flat, func(i, j int) bool {
		return flat[i].list[0].EndTime.After(flat[j].list[0].EndTime)
	})
	now := time.Now()
	var b strings.Builder
	for _, e := range flat {
		fmt.Fprintf(&b, "%s (%d sessions)\n", e.dir, len(e.list))
		for _, s := range e.list {
			fmt.Fprintf(&b, "  %s\n", formatRow(s, now, width-4))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatRow(s *model.Session, now time.Time, width int) string {
	icon := activityIcon(now.Sub(s.EndTime))
	dir := s.ProjectDir
	if len(dir) > 30 {
		dir = "…" + dir[len(dir)-29:]
	}
	preview := s.FirstUserMsg
	if len(preview) > 50 {
		preview = preview[:49] + "…"
	}
	return fmt.Sprintf("%s %s  %s  %d msg  %s",
		icon,
		s.EndTime.Local().Format("15:04"),
		lipgloss.NewStyle().Width(30).Render(dir),
		s.MessageCount,
		preview,
	)
}

func activityIcon(since time.Duration) string {
	switch {
	case since < activityActive:
		return "🟢"
	case since < activityPaused:
		return "🟡"
	default:
		return "⚪"
	}
}

func timeBucket(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 24*time.Hour && now.Day() == t.Day():
		return "Today"
	case d < 48*time.Hour:
		return "Yesterday"
	case d < 7*24*time.Hour:
		return "This week"
	default:
		return "Older"
	}
}
```

- [ ] **Step 2: Conectar Recent view ao Model**

Modificar `tui/app.go`:

1. Adicionar campo `recent recentView` ao Model
2. No `New`, carregar sessions: `sessions, _ := db.ListSessions(); m.recent = newRecentView(sessions)`
3. No `Update` da `tea.KeyMsg`, adicionar:
```go
case keyMatch(msg, keys.Up):
	if m.recent.cursor > 0 { m.recent.cursor-- }
	return m, nil
case keyMatch(msg, keys.Down):
	if m.recent.cursor < len(m.recent.sessions)-1 { m.recent.cursor++ }
	return m, nil
case keyMatch(msg, keys.Group):
	m.recent.groupByProject = !m.recent.groupByProject
	return m, nil
```
4. No `renderBody`, substituir o placeholder do `tabRecent`:
```go
case tabRecent:
	return m.recent.View(m.width, bodyHeight)
```

- [ ] **Step 3: Smoke test manual**

```bash
go build -o /tmp/ch-test .
# Roda só compila por enquanto. Subcomando vem em Task 18.
```
Expected: build OK.

- [ ] **Step 4: Commit**

```bash
git add tui/recent.go tui/app.go
git commit -m "feat: tab recent com agrupamento tempo/projeto"
```

---

### Task 10: Tab Search — modo metadata

**Files:**
- Create: `tui/search.go`
- Modify: `tui/app.go`

- [ ] **Step 1: Implementar `tui/search.go`**

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/model"
)

type searchMode int

const (
	modeMetadata searchMode = iota
	modeFullText
)

type searchView struct {
	input    textinput.Model
	mode     searchMode
	all      []*model.Session
	results  []*model.Session
	cursor   int
}

func newSearchView(all []*model.Session) searchView {
	ti := textinput.New()
	ti.Placeholder = "Filtrar por cwd, branch ou primeira msg…"
	ti.Focus()
	return searchView{input: ti, all: all, results: all}
}

func (v *searchView) Filter(query string) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		v.results = v.all
		return
	}
	v.results = v.results[:0]
	for _, s := range v.all {
		if metadataMatch(s, q) {
			v.results = append(v.results, s)
		}
	}
}

func metadataMatch(s *model.Session, q string) bool {
	for _, h := range []string{s.ProjectDir, s.GitBranch, s.FirstUserMsg, s.LastUserMsg, s.SessionID} {
		if strings.Contains(strings.ToLower(h), q) {
			return true
		}
	}
	return false
}

func (v searchView) View(width, height int) string {
	header := lipgloss.NewStyle().Foreground(colorMuted).Render(
		"mode: " + map[searchMode]string{modeMetadata: "metadata", modeFullText: "full-text"}[v.mode],
	)
	var rows []string
	for i, s := range v.results {
		marker := "  "
		if i == v.cursor {
			marker = "▶ "
		}
		rows = append(rows, marker+s.ProjectDir+"  "+s.FirstUserMsg)
	}
	body := strings.Join(rows, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, v.input.View(), header, body)
}
```

- [ ] **Step 2: Conectar Search view ao Model**

Em `tui/app.go`:

1. Adicionar `search searchView` ao Model.
2. No `New`: `m.search = newSearchView(sessions)`.
3. No `renderBody` para `tabSearch`: `return m.search.View(m.width, bodyHeight)`.
4. No `Update`, quando `m.activeTab == tabSearch`, propagar mensagens pro textinput:
```go
if m.activeTab == tabSearch {
	var cmd tea.Cmd
	m.search.input, cmd = m.search.input.Update(msg)
	m.search.Filter(m.search.input.Value())
	return m, cmd
}
```

- [ ] **Step 3: Build**

```bash
go build -o /tmp/ch-test .
```
Expected: build OK.

- [ ] **Step 4: Commit**

```bash
git add tui/search.go tui/app.go
git commit -m "feat: tab search modo metadata"
```

---

### Task 11: Search — modo full-text via FTS5

**Files:**
- Modify: `tui/search.go` — detectar prefixo `:body` e rodar `db.SearchFTS`
- Modify: `tui/app.go` — passar `db` pro searchView

- [ ] **Step 1: Estender searchView pra ter referência ao DB**

```go
// tui/search.go
type searchView struct {
	db       *index.DB
	input    textinput.Model
	mode     searchMode
	all      []*model.Session
	results  []*model.Session
	snippets map[string]string // sessionID → snippet do FTS, opcional
	cursor   int
}

func newSearchView(db *index.DB, all []*model.Session) searchView {
	// ... (igual antes mas guardando db)
}

func (v *searchView) Filter(query string) {
	q := strings.TrimSpace(query)
	if strings.HasPrefix(q, ":body ") {
		v.mode = modeFullText
		v.runFullText(strings.TrimPrefix(q, ":body "))
		return
	}
	v.mode = modeMetadata
	// ... lógica metadata igual antes
}

func (v *searchView) runFullText(q string) {
	if v.db == nil { return }
	results, err := v.db.SearchFTS(q)
	if err != nil { return }
	byID := map[string]*model.Session{}
	for _, s := range v.all { byID[s.SessionID] = s }
	v.results = v.results[:0]
	v.snippets = map[string]string{}
	for _, r := range results {
		if s, ok := byID[r.SessionID]; ok {
			v.results = append(v.results, s)
			v.snippets[r.SessionID] = r.Snippet
		}
	}
}
```

(Adicionar import `github.com/felipeness/claude-history/internal/index`.)

- [ ] **Step 2: Atualizar render pra mostrar snippet quando full-text**

No `View` da `searchView`, ao montar `rows`:
```go
extra := ""
if v.mode == modeFullText {
	if sn, ok := v.snippets[s.SessionID]; ok {
		extra = " — " + sn
	}
}
rows = append(rows, marker+s.ProjectDir+"  "+s.FirstUserMsg+extra)
```

- [ ] **Step 3: Atualizar `New` no app.go**

```go
m.search = newSearchView(db, sessions)
```

- [ ] **Step 4: Build**

```bash
go build -o /tmp/ch-test .
```
Expected: build OK.

- [ ] **Step 5: Commit**

```bash
git add tui/search.go tui/app.go
git commit -m "feat: search modo full-text via fts5 com prefixo :body"
```

---

### Task 12: Tab Stats — global aggregate

**Files:**
- Create: `tui/stats.go`
- Modify: `tui/app.go`

- [ ] **Step 1: Implementar `tui/stats.go`**

```go
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
)

type statsView struct {
	sessions []*model.Session
	pricing  *pricing.Pricing
	cursor   int
	showLocal bool // <120 cols toggle
}

func newStatsView(sessions []*model.Session, p *pricing.Pricing) statsView {
	return statsView{sessions: sessions, pricing: p}
}

func (v statsView) renderGlobal(width int) string {
	var b strings.Builder
	totalMsgs := 0
	totalCostUSD := 0.0
	costByProject := map[string]float64{}
	toolGlobal := map[string]int{}
	for _, s := range v.sessions {
		totalMsgs += s.MessageCount
		if cost, ok := v.pricing.Cost(s); ok {
			totalCostUSD += cost.USD
			costByProject[s.ProjectDir] += cost.USD
		}
		for t, c := range s.ToolCalls {
			toolGlobal[t] += c
		}
	}
	fmt.Fprintf(&b, "TOTAL geral\n")
	fmt.Fprintf(&b, "─────────────\n")
	fmt.Fprintf(&b, "Sessions: %d   Msgs: %d   Custo total: $%.2f USD\n",
		len(v.sessions), totalMsgs, totalCostUSD)
	if v.pricing.BRLRate > 0 {
		fmt.Fprintf(&b, "(~R$ %.2f a câmbio %.2f)\n", totalCostUSD*v.pricing.BRLRate, v.pricing.BRLRate)
	}
	fmt.Fprintf(&b, "\nTop projetos por custo\n────────────\n")
	type kv struct{ k string; v float64 }
	var pairs []kv
	for k, c := range costByProject {
		pairs = append(pairs, kv{k, c})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
	for i, p := range pairs {
		if i >= 5 { break }
		fmt.Fprintf(&b, "  $%-8.2f %s\n", p.v, p.k)
	}
	fmt.Fprintf(&b, "\nTop tools globais\n──────\n")
	var toolPairs []kv
	for k, c := range toolGlobal {
		toolPairs = append(toolPairs, kv{k, float64(c)})
	}
	sort.Slice(toolPairs, func(i, j int) bool { return toolPairs[i].v > toolPairs[j].v })
	for i, p := range toolPairs {
		if i >= 8 { break }
		fmt.Fprintf(&b, "  %-15s %d\n", p.k, int(p.v))
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

// renderSparkline produz string de 7 chars representando sessions/dia dos últimos 7 dias.
func renderSparkline(sessions []*model.Session) string {
	now := time.Now()
	bins := make([]int, 7)
	for _, s := range sessions {
		days := int(now.Sub(s.StartTime).Hours() / 24)
		if days >= 0 && days < 7 {
			bins[6-days]++
		}
	}
	chars := []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	max := 1
	for _, c := range bins {
		if c > max { max = c }
	}
	var sb strings.Builder
	for _, c := range bins {
		idx := c * (len(chars) - 1) / max
		sb.WriteString(chars[idx])
	}
	return sb.String()
}
```

- [ ] **Step 2: Conectar ao Model**

Em `tui/app.go`:
1. Adicionar `stats statsView`.
2. No `New`: `m.stats = newStatsView(sessions, p)`.
3. No `renderBody` `tabStats`: `return m.stats.renderGlobal(m.width)`.

- [ ] **Step 3: Build**

```bash
go build -o /tmp/ch-test .
```
Expected: build OK.

- [ ] **Step 4: Commit**

```bash
git add tui/stats.go tui/app.go
git commit -m "feat: tab stats global aggregate com sparkline"
```

---

### Task 13: Detail panel + Stats local

**Files:**
- Create: `tui/detail.go`
- Modify: `tui/stats.go`, `tui/recent.go`, `tui/app.go`

- [ ] **Step 1: Implementar `tui/detail.go`**

```go
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
)

func renderDetail(s *model.Session, p *pricing.Pricing) string {
	if s == nil {
		return "(nenhuma session selecionada)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Session: %s\n", s.SessionID)
	fmt.Fprintf(&b, "Pasta: %s\n", s.ProjectDir)
	fmt.Fprintf(&b, "Branch: %s\n", orDash(s.GitBranch))
	fmt.Fprintf(&b, "Início: %s\n", s.StartTime.Local().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "Duração: %s\n", s.Duration().Round(1e9))
	fmt.Fprintf(&b, "Modelo: %s\n", orDash(s.Model))
	fmt.Fprintf(&b, "\nTokens\n──────\n")
	fmt.Fprintf(&b, "Input:    %s\n", fmtInt(s.InputTokens))
	fmt.Fprintf(&b, "Output:   %s\n", fmtInt(s.OutputTokens))
	fmt.Fprintf(&b, "Cache cr: %s\n", fmtInt(s.CacheCreationTokens))
	fmt.Fprintf(&b, "Cache rd: %s\n", fmtInt(s.CacheReadTokens))
	if cost, ok := p.Cost(s); ok {
		if p.BRLRate > 0 {
			fmt.Fprintf(&b, "Custo: $%.2f USD (~R$ %.2f)\n", cost.USD, cost.BRL)
		} else {
			fmt.Fprintf(&b, "Custo: $%.2f USD\n", cost.USD)
		}
	} else {
		fmt.Fprintf(&b, "Custo: ? (modelo %q sem entry no pricing.toml)\n", s.Model)
	}
	if len(s.ToolCalls) > 0 {
		fmt.Fprintf(&b, "\nTools\n─────\n")
		type kv struct{ k string; v int }
		var pairs []kv
		for k, v := range s.ToolCalls { pairs = append(pairs, kv{k, v}) }
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
		for _, p := range pairs {
			fmt.Fprintf(&b, "  %-15s %d\n", p.k, p.v)
		}
	}
	return b.String()
}

func orDash(s string) string { if s == "" { return "-" }; return s }
func fmtInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
```

- [ ] **Step 2: No app.go, renderizar multi-pane se largura ≥ 120**

```go
func (m Model) renderBody() string {
	bodyHeight := m.height - 2
	if m.width >= wideCols {
		return m.renderWide(bodyHeight)
	}
	return m.renderNarrow(bodyHeight)
}

func (m Model) renderWide(h int) string {
	leftW := m.width * 4 / 10
	rightW := m.width - leftW
	left := lipgloss.NewStyle().Width(leftW).Height(h)
	right := lipgloss.NewStyle().Width(rightW).Height(h).Padding(0, 1)
	switch m.activeTab {
	case tabSearch:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			left.Render(m.search.View(leftW, h)),
			right.Render(renderDetail(m.search.selected(), m.pricing)),
		)
	case tabRecent:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			left.Render(m.recent.View(leftW, h)),
			right.Render(renderDetail(m.recent.selected(), m.pricing)),
		)
	case tabStats:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			left.Render(m.stats.renderGlobal(leftW)),
			right.Render(renderDetail(m.recent.selected(), m.pricing)),
		)
	}
	return ""
}

func (m Model) renderNarrow(h int) string {
	switch m.activeTab {
	case tabSearch:
		return m.search.View(m.width, h)
	case tabRecent:
		return m.recent.View(m.width, h)
	case tabStats:
		if m.stats.showLocal {
			return renderDetail(m.recent.selected(), m.pricing)
		}
		return m.stats.renderGlobal(m.width)
	}
	return ""
}
```

E adicionar métodos `selected()` em recent.go e search.go:
```go
// recent.go
func (v recentView) selected() *model.Session {
	if v.cursor < 0 || v.cursor >= len(v.sessions) { return nil }
	return v.sessions[v.cursor]
}

// search.go
func (v searchView) selected() *model.Session {
	if v.cursor < 0 || v.cursor >= len(v.results) { return nil }
	return v.results[v.cursor]
}
```

- [ ] **Step 3: Tratar key `s` em Stats narrow**

No `Update`:
```go
case keyMatch(msg, keys.Stats):
	if m.activeTab == tabStats && m.width < wideCols {
		m.stats.showLocal = !m.stats.showLocal
	}
	return m, nil
```

- [ ] **Step 4: Build**

```bash
go build -o /tmp/ch-test .
```
Expected: build OK.

- [ ] **Step 5: Commit**

```bash
git add tui/detail.go tui/recent.go tui/search.go tui/stats.go tui/app.go
git commit -m "feat: detail panel reusavel e layout adaptativo"
```

---

### Task 14: Adaptive layout testado em runtime

**Files:**
- Modify: `tui/app.go` — confirmar `tea.WindowSizeMsg` está propagado

- [ ] **Step 1: Verificar handler do WindowSizeMsg**

No `Update`, o branch `case tea.WindowSizeMsg:` já seta `m.width, m.height`. Confirmar que renderBody usa esses valores. Já feito.

- [ ] **Step 2: Smoke test com terminal redimensionado**

Pular pra Task 18 onde rodaremos o subcomando real e confirmaremos. Esta task fica como verificação cross-cutting.

- [ ] **Step 3: Commit**

(Sem mudança — pular se nada mudou.)

---

### Task 15: Help overlay

**Files:**
- Modify: `tui/app.go` — overlay com lista de keybinds em `?`

- [ ] **Step 1: Adicionar campo `showHelp bool` ao Model**

- [ ] **Step 2: Tratar key `?`**

```go
case keyMatch(msg, keys.Help):
	m.showHelp = !m.showHelp
	return m, nil
```

- [ ] **Step 3: Renderizar overlay quando showHelp**

```go
func (m Model) View() string {
	tabBar := m.renderTabBar()
	body := m.renderBody()
	status := m.renderStatusBar()
	out := lipgloss.JoinVertical(lipgloss.Left, tabBar, body, status)
	if m.showHelp {
		help := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(1, 2).
			Render(helpText())
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, help)
	}
	return out
}

func helpText() string {
	return `KEYBINDS

Tab / Shift+Tab    trocar tab
j / k              navegar lista
Enter              retomar session
/ ou f             search box
:body <q>          full-text search
g                  toggle agrupamento (Recent)
s                  toggle stats local (narrow)
d                  toggle filtro 7d/all
r                  refresh
?                  help
q ou Esc           quit
Ctrl+O             abrir pasta no Finder

Pressiona ? de novo pra fechar.`
}
```

- [ ] **Step 4: Build + commit**

```bash
go build -o /tmp/ch-test .
git add tui/app.go
git commit -m "feat: help overlay com lista de keybinds"
```

---

## Milestone E — Integration

### Task 16: Enter retoma session

**Files:**
- Modify: `tui/app.go` — handler de Enter chama `claude --resume`

- [ ] **Step 1: Implementar resume Cmd**

Adicionar em `tui/app.go`:

```go
import (
	"os"
	"os/exec"
)

type resumeMsg struct{ err error }

func resumeCmd(s *model.Session) tea.Cmd {
	return func() tea.Msg {
		if s == nil { return resumeMsg{} }
		claude, err := exec.LookPath("claude")
		if err != nil { return resumeMsg{err: err} }
		c := exec.Command(claude, "--resume", s.SessionID)
		c.Dir = s.ProjectDir
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return resumeMsg{err: c.Run()}
	}
}
```

No `Update`:
```go
case keyMatch(msg, keys.Enter):
	var s *model.Session
	switch m.activeTab {
	case tabRecent:
		s = m.recent.selected()
	case tabSearch:
		s = m.search.selected()
	}
	if s != nil {
		return m, tea.Batch(tea.ExitAltScreen, resumeCmd(s), tea.Quit)
	}
	return m, nil
```

- [ ] **Step 2: Build + commit**

```bash
go build -o /tmp/ch-test .
git add tui/app.go
git commit -m "feat: enter retoma session via claude --resume"
```

---

### Task 17: Refresh action

**Files:**
- Modify: `tui/app.go` — `r` chama `db.Reindex` async

- [ ] **Step 1: Implementar refresh Cmd**

```go
type refreshDoneMsg struct {
	stats index.ReindexStats
	err error
}

func refreshCmd(db *index.DB, root string) tea.Cmd {
	return func() tea.Msg {
		stats, err := db.Reindex(root)
		return refreshDoneMsg{stats: stats, err: err}
	}
}
```

No `Update`:
```go
case keyMatch(msg, keys.Refresh):
	m.status = "refreshing…"
	return m, refreshCmd(m.db, claudeProjectsRoot())
case refreshDoneMsg:
	if msg.err != nil {
		m.status = "refresh error: " + msg.err.Error()
	} else {
		m.status = fmt.Sprintf("refresh: +%d new, %d updated, %d removed", msg.stats.New, msg.stats.Updated, msg.stats.Removed)
		// recarrega sessions
		sessions, _ := m.db.ListSessions()
		m.recent = newRecentView(sessions)
		m.search = newSearchView(m.db, sessions)
		m.stats = newStatsView(sessions, m.pricing)
	}
	return m, nil
```

E helper:
```go
func claudeProjectsRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}
```

- [ ] **Step 2: Build + commit**

```bash
go build -o /tmp/ch-test .
git add tui/app.go
git commit -m "feat: r dispara reindex async com status no rodape"
```

---

### Task 18: Subcomando `tui` no main.go

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Adicionar handler do subcomando**

Em `main.go`, no switch:
```go
case "tui":
	cmdTui(os.Args[2:])
```

E a função:
```go
func cmdTui(args []string) {
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".claude-history")
	os.MkdirAll(cacheDir, 0755)
	dbPath := filepath.Join(cacheDir, "index.db")
	pricingPath := filepath.Join(cacheDir, "pricing.toml")

	if _, err := os.Stat(pricingPath); errors.Is(err, os.ErrNotExist) {
		// seed default pricing.toml
		const seed = `default_currency = "USD"
brl_rate = 5.20

[[models]]
name = "claude-sonnet-4-6"
input_per_mtok = 3.00
output_per_mtok = 15.00
cache_creation_per_mtok = 3.75
cache_read_per_mtok = 0.30

[[models]]
name = "claude-opus-4-7"
input_per_mtok = 15.00
output_per_mtok = 75.00
cache_creation_per_mtok = 18.75
cache_read_per_mtok = 1.50
`
		os.WriteFile(pricingPath, []byte(seed), 0644)
	}

	db, err := index.Open(dbPath)
	if err != nil { fatal(err) }
	defer db.Close()

	if _, err := db.Reindex(filepath.Join(home, ".claude", "projects")); err != nil {
		fmt.Fprintln(os.Stderr, "reindex error:", err)
	}

	p, err := pricing.Load(pricingPath)
	if err != nil { fatal(err) }

	prog := tea.NewProgram(tui.New(db, p), tea.WithAltScreen())
	if _, err := prog.Run(); err != nil { fatal(err) }
}
```

(Imports: `errors`, `path/filepath`, `tea "github.com/charmbracelet/bubbletea"`, `internal/index`, `internal/pricing`, `tui`.)

E atualizar a `usage` no topo do `main.go`:
```go
const usage = `claude-history — busca todas as suas conversas do Claude Code

USAGE:
  claude-history list [--json|--tsv]
  claude-history fzf
  claude-history show <session-id>
  claude-history tui                       NOVO — TUI Bubble Tea com 3 tabs
  claude-history serve [--port N]          Fase 3 (web UI)
`
```

- [ ] **Step 2: Build e teste real**

```bash
go build -o ~/.local/bin/claude-history .
claude-history tui
```
Expected: TUI abre, navega entre tabs, mostra suas 4 sessions reais. `q` sai.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat: subcomando claude-history tui inicializa db e roda bubble tea"
```

---

### Task 19: Golden test com fixture JSONL

**Files:**
- Create: `internal/parser/testdata/sample-session.jsonl`
- Create: `internal/parser/golden_test.go`

- [ ] **Step 1: Criar fixture**

Em `internal/parser/testdata/sample-session.jsonl`:
```
{"type":"user","sessionId":"golden-1","cwd":"/tmp/golden","timestamp":"2026-04-28T10:00:00Z","gitBranch":"main","version":"2.1.121","message":{"role":"user","content":"oi"}}
{"type":"assistant","sessionId":"golden-1","cwd":"/tmp/golden","timestamp":"2026-04-28T10:00:01Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"oi de volta"},{"type":"tool_use","name":"Bash","id":"x"}],"usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":200,"cache_read_input_tokens":1000}}}
{"type":"user","sessionId":"golden-1","cwd":"/tmp/golden","timestamp":"2026-04-28T10:01:00Z","message":{"role":"user","content":"obrigado"}}
```

- [ ] **Step 2: Escrever golden test**

```go
// internal/parser/golden_test.go
package parser

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseSession_golden(t *testing.T) {
	s, err := ParseSession(filepath.Join("testdata", "sample-session.jsonl"))
	if err != nil { t.Fatal(err) }
	if s.SessionID != "golden-1" { t.Errorf("SessionID = %q", s.SessionID) }
	if s.UserMessages != 2 { t.Errorf("UserMessages = %d, want 2", s.UserMessages) }
	if s.AssistantMessages != 1 { t.Errorf("AssistantMessages = %d, want 1", s.AssistantMessages) }
	if s.GitBranch != "main" { t.Errorf("GitBranch = %q", s.GitBranch) }
	if s.ToolCalls["Bash"] != 1 { t.Errorf("Bash count = %d", s.ToolCalls["Bash"]) }
	if s.InputTokens != 100 { t.Errorf("InputTokens = %d", s.InputTokens) }
	if s.FirstUserMsg != "oi" { t.Errorf("FirstUserMsg = %q", s.FirstUserMsg) }
	if s.LastUserMsg != "obrigado" { t.Errorf("LastUserMsg = %q", s.LastUserMsg) }
	if !s.StartTime.Equal(time.Date(2026,4,28,10,0,0,0,time.UTC)) {
		t.Errorf("StartTime = %v", s.StartTime)
	}
}
```

- [ ] **Step 3: Rodar**

```bash
go test ./internal/parser/ -v
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/parser/testdata/ internal/parser/golden_test.go
git commit -m "test: golden fixture para ParseSession"
```

---

### Task 20: README atualizado

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Atualizar seção "Uso"**

Substituir bloco de uso por:

```markdown
## Uso

```bash
# CLI clássica (Fase 1)
claude-history list                      # tabela
claude-history list --json | jq '...'    # JSON pra script
claude-history show <id>                 # detalhes
claude-history fzf                       # fzf interativo

# TUI (Fase 2 — NOVO)
claude-history tui                       # 3 tabs: Search/Recent/Stats
```

## TUI — keybinds

```
Tab / Shift+Tab    trocar tab
j / k              navegar lista
Enter              retomar session no cwd certo
/ ou f             search por metadata
:body <query>      search full-text via FTS5
g                  toggle agrupamento (Recent: tempo ↔ projeto)
s                  toggle stats local em terminal pequeno
d                  filtro temporal Recent (7d ↔ all)
r                  refresh (reindex)
?                  help overlay
q ou Esc           sair
Ctrl+O             abrir pasta no Finder
```

## Pricing

`~/.claude-history/pricing.toml` é seedado automaticamente no primeiro launch da TUI.
Edite pra ajustar custos por modelo ou setar `brl_rate` pra display dual USD/BRL.
```

- [ ] **Step 2: Atualizar seção Roadmap**

```markdown
## Roadmap

- [x] Fase 1 — indexer + `list/show/fzf`
- [x] Fase 2 — `tui` Bubble Tea com tabs Search/Recent/Stats + tokens/custo
- [ ] Fase 3 — `serve` HTTP + UI web (Vite/React)
- [ ] Fase 4 — Behavioral analytics (heurísticas determinísticas)
- [ ] Fase 5 — AI-powered profiling (LLM local + embeddings)
- [ ] Fase 6 — Code mining
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: atualiza README com uso da tui e roadmap"
```

---

## Self-Review

### Spec coverage

- ✅ Tabs Search/Recent/Stats — Tasks 9, 10, 11, 12, 13
- ✅ Layout adaptativo (multi-pane / single) — Task 13
- ✅ Busca híbrida metadata + FTS — Tasks 10, 11
- ✅ Tokens, custo, modelo — Tasks 2, 3, 13
- ✅ SQLite cache com FTS5 — Tasks 4, 5, 6
- ✅ Reindex mtime-based — Task 7
- ✅ Refresh manual `r` — Task 17
- ✅ Enter retoma session — Task 16
- ✅ Help overlay — Task 15
- ✅ Sem regressão CLI Fase 1 — preservado em todas as tasks (refactor isolado em Task 1)
- ✅ Pricing TOML auto-seedado — Task 18
- ✅ Critérios de aceitação cobertos — todos (validar manualmente em Task 18)

### Placeholder scan

- ✅ Sem TODO/TBD
- ✅ Sem "implement later" / "fill in details"
- ✅ Cada step de código tem código completo
- ✅ Comandos exatos com expected output

### Type consistency

- `Session.AssistantMessages` (não `AssistantMsgs`) — uniformizado a partir da Task 1
- `Cost{USD, BRL}` consistente entre pricing e detail
- `SearchResult{SessionID, Role, Snippet, Rank}` consistente entre fts/like
- `ReindexStats{Scanned, New, Updated, Removed}` consistente
- Métodos `selected()` adicionados nas views — consistentes em assinatura

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-04-28-tui-base-implementation.md`.**

Two execution options:

**1. Subagent-Driven (recommended)** — Felipe (ou outro agent) dispara um subagent fresco por task, revisa entre tasks, iteração rápida e auditável.

**2. Inline Execution** — eu executo as tasks nesta sessão em batches, com checkpoints depois de cada milestone (A → revisão → B → revisão → ...).

**Qual abordagem?**

Recomendação: **(2) inline com checkpoints por milestone**. A primeira sessão já tem o contexto carregado, e checkpoints por milestone (A=foundation, B=sqlite, C=shell, D=tabs, E=integration) são os pontos naturais de revisão.

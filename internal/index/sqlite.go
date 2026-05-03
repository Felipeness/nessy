// Package index manages the SQLite cache of Claude Code sessions.
package index

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/parser"
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
	cache_read_tokens INTEGER NOT NULL DEFAULT 0,
	sidechain_turns INTEGER NOT NULL DEFAULT 0,
	sidechain_agents INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sessions_start ON sessions(start_time DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_dir);

CREATE TABLE IF NOT EXISTS tool_uses (
	session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
	tool_name TEXT NOT NULL,
	count INTEGER NOT NULL,
	PRIMARY KEY (session_id, tool_name)
);

-- tool_events: 1 row por tool_use individual (não agregado). Habilita
-- loop detection retroativa: GROUP BY (session_id, tool_name, input_hash)
-- HAVING count >= N AND maxts - mints < window_ns.
-- input_preview: primeiros ~100 chars do input pra UI debug.
CREATE TABLE IF NOT EXISTS tool_events (
	session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
	ts INTEGER NOT NULL,
	tool_name TEXT NOT NULL,
	input_hash TEXT NOT NULL,
	input_preview TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE INDEX IF NOT EXISTS idx_tool_events_loop
	ON tool_events(session_id, tool_name, input_hash, ts);
CREATE INDEX IF NOT EXISTS idx_tool_events_session
	ON tool_events(session_id);

-- session_files: arquivos tocados por cada session (Edit/Write/Read/...).
-- Habilita métricas de retrabalho (mesmo arquivo aberto em N sessions
-- distintas em janela curta = sinal de iteração frequente / instabilidade).
CREATE TABLE IF NOT EXISTS session_files (
	session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
	file_path TEXT NOT NULL,
	op_count INTEGER NOT NULL DEFAULT 0,
	first_op_ts INTEGER NOT NULL,
	PRIMARY KEY (session_id, file_path)
) STRICT;
CREATE INDEX IF NOT EXISTS idx_session_files_path
	ON session_files(file_path);

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

CREATE TABLE IF NOT EXISTS ai_insights (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	type TEXT NOT NULL,
	title TEXT NOT NULL,
	description TEXT NOT NULL,
	evidence TEXT,
	suggested_action TEXT,
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_profile (
	id INTEGER PRIMARY KEY,
	content TEXT NOT NULL,
	generated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS session_knowledge (
	session_id TEXT PRIMARY KEY REFERENCES sessions(session_id) ON DELETE CASCADE,
	jsonl_mtime INTEGER NOT NULL,
	problem TEXT,
	solution TEXT,
	decisions TEXT,        -- JSON array de {decision, rationale}
	learnings TEXT,        -- JSON array de strings
	code_patterns TEXT,    -- JSON array de strings
	tech_used TEXT,        -- JSON array de strings
	open_questions TEXT,   -- JSON array de strings
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
	// Split em statements individuais — modernc.org/sqlite às vezes só roda
	// o primeiro statement num Exec multi-statement, deixando tabelas
	// silenciosamente faltando.
	for _, stmt := range splitSQL(schemaSQL) {
		if _, err := conn.Exec(stmt); err != nil {
			conn.Close()
			return nil, fmt.Errorf("create schema (%s): %w", firstLine(stmt), err)
		}
	}
	// Migrations idempotentes — schemaSQL só cria tables/cols pra DBs novos.
	// Pra DBs existentes precisamos ALTER TABLE on demand.
	if err := runMigrations(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}
	if _, err := conn.Exec(`INSERT OR IGNORE INTO last_index_meta(key, value) VALUES('schema_version', ?)`, currentSchemaVersion); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set schema version: %w", err)
	}
	return &DB{conn: conn, path: path}, nil
}

// splitSQL quebra um bloco SQL em statements individuais, separando por ';'
// no nível top (ignora ';' dentro de strings). Skipa whitespace + comments.
func splitSQL(s string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(s, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "--") {
			continue
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
		if strings.HasSuffix(trim, ";") {
			st := strings.TrimSpace(cur.String())
			if st != "" {
				out = append(out, st)
			}
			cur.Reset()
		}
	}
	if rest := strings.TrimSpace(cur.String()); rest != "" {
		out = append(out, rest)
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}

// runMigrations aplica ALTER TABLE pras colunas que vieram depois de v1.
// Cada migration checa se a coluna já existe via PRAGMA antes de adicionar
// — rodar 2× é no-op.
func runMigrations(conn *sql.DB) error {
	addColIfMissing := func(table, col, ddl string) error {
		rows, err := conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				return err
			}
			if name == col {
				return nil // já existe
			}
		}
		_, err = conn.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, ddl))
		return err
	}
	if err := addColIfMissing("sessions", "sidechain_turns",
		"sidechain_turns INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColIfMissing("sessions", "sidechain_agents",
		"sidechain_agents INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColIfMissing("tool_events", "input_preview",
		"input_preview TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// resolved_at_turn: NULL se session não convergiu (sem msg final positiva).
	// Inteiro 1-based pra "turno onde o user demonstrou que tava resolvido".
	if err := addColIfMissing("sessions", "resolved_at_turn",
		"resolved_at_turn INTEGER"); err != nil {
		return err
	}
	return nil
}

// Close closes the underlying connection.
func (db *DB) Close() error {
	if db.conn == nil {
		return nil
	}
	return db.conn.Close()
}

// Conn devolve o handle pra queries ad-hoc (advisor, debug, scripts).
// Não usar pra escrita — Upsert/IndexX já encapsulam transações.
func (db *DB) Conn() *sql.DB { return db.conn }

const upsertSessionSQL = `
INSERT INTO sessions (
	session_id, project_dir, jsonl_path, jsonl_mtime,
	start_time, end_time, message_count, user_messages, assistant_messages,
	first_user_msg, last_user_msg, git_branch, claude_version, model,
	input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
	sidechain_turns, sidechain_agents, resolved_at_turn
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
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
	cache_read_tokens=excluded.cache_read_tokens,
	sidechain_turns=excluded.sidechain_turns,
	sidechain_agents=excluded.sidechain_agents,
	resolved_at_turn=excluded.resolved_at_turn
`

// Upsert inserts or updates a session and replaces its tool_uses.
func (db *DB) Upsert(s *model.Session) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// resolved_at_turn é nullable em SQL — mapeia 0 → NULL pra busca SQL ficar limpa
	var resolvedArg any
	if s.ResolvedAtTurn > 0 {
		resolvedArg = s.ResolvedAtTurn
	}
	if _, err := tx.Exec(upsertSessionSQL,
		s.SessionID, s.ProjectDir, s.JSONLPath, s.JSONLMtime.UnixNano(),
		s.StartTime.UnixNano(), s.EndTime.UnixNano(),
		s.MessageCount, s.UserMessages, s.AssistantMessages,
		s.FirstUserMsg, s.LastUserMsg, s.GitBranch, s.ClaudeVersion, s.Model,
		s.InputTokens, s.OutputTokens, s.CacheCreationTokens, s.CacheReadTokens,
		s.SidechainTurns, s.SidechainAgents, resolvedArg,
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

// IndexToolEvents substitui os tool_events de uma session pelos da lista.
// Operação atômica em transação. Idempotente: chamar 2× é no-op.
func (db *DB) IndexToolEvents(sessionID string, events []parser.ToolEvent) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM tool_events WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("clear tool_events: %w", err)
	}
	stmt, err := tx.Prepare(
		`INSERT INTO tool_events (session_id, ts, tool_name, input_hash, input_preview) VALUES (?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range events {
		if _, err := stmt.Exec(sessionID, e.Timestamp.UnixNano(), e.ToolName, e.InputHash, e.InputPreview); err != nil {
			return fmt.Errorf("insert tool_event: %w", err)
		}
	}
	return tx.Commit()
}

// IndexFileOps substitui as file ops de uma session pelas da lista.
// Idempotente: chamar 2× é no-op.
func (db *DB) IndexFileOps(sessionID string, ops []parser.FileOp) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM session_files WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("clear session_files: %w", err)
	}
	stmt, err := tx.Prepare(
		`INSERT INTO session_files (session_id, file_path, op_count, first_op_ts) VALUES (?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, op := range ops {
		if _, err := stmt.Exec(sessionID, op.FilePath, op.OpCount, op.FirstOpAt.UnixNano()); err != nil {
			return fmt.Errorf("insert session_file: %w", err)
		}
	}
	return tx.Commit()
}

// FileReuse mostra arquivos tocados em ≥ minSessions sessions distintas.
// Sinal de iteração frequente / instabilidade naquele arquivo.
type FileReuse struct {
	FilePath     string `json:"file_path"`
	SessionCount int    `json:"session_count"`
	TotalOps     int    `json:"total_ops"`
}

// FileReuseTop devolve top arquivos por nº de sessions que os tocaram.
// Filtra arquivos com ≥ minSessions e retorna os top limit.
func (db *DB) FileReuseTop(minSessions, limit int) ([]FileReuse, error) {
	if minSessions < 2 {
		minSessions = 2
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.conn.Query(`
		SELECT file_path,
		       COUNT(DISTINCT session_id) AS session_count,
		       SUM(op_count) AS total_ops
		FROM session_files
		GROUP BY file_path
		HAVING session_count >= ?
		ORDER BY session_count DESC, total_ops DESC
		LIMIT ?
	`, minSessions, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileReuse
	for rows.Next() {
		var f FileReuse
		if err := rows.Scan(&f.FilePath, &f.SessionCount, &f.TotalOps); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CostByTicket extrai pattern tipo CC-1234 do branch e agrega custo.
type CostByTicket struct {
	Ticket   string
	Sessions int
	CostUSD  float64 // requer pricing externo — preencher depois
	Branches []string
}

// CostByTicketRows devolve rows brutas (Ticket, Sessions, Branches) — caller
// computa CostUSD via pricing depois pra evitar dependência cruzada.
func (db *DB) CostByTicketRows() (map[string]*CostByTicket, error) {
	rows, err := db.conn.Query(`SELECT session_id, git_branch FROM sessions WHERE git_branch IS NOT NULL AND git_branch != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*CostByTicket{}
	for rows.Next() {
		var sid, branch string
		if err := rows.Scan(&sid, &branch); err != nil {
			return nil, err
		}
		ticket := ExtractTicket(branch)
		if ticket == "" {
			continue
		}
		t, ok := out[ticket]
		if !ok {
			t = &CostByTicket{Ticket: ticket}
			out[ticket] = t
		}
		t.Sessions++
		// Dedup branches
		seen := false
		for _, b := range t.Branches {
			if b == branch {
				seen = true
				break
			}
		}
		if !seen {
			t.Branches = append(t.Branches, branch)
		}
	}
	return out, rows.Err()
}

// ExtractTicket pega XX-NNNN de uma branch tipo "feat/CC-1234-foo".
// Devolve string vazia se não casar pattern. Exposed pra uso externo.
func ExtractTicket(branch string) string {
	for i := 0; i < len(branch)-3; i++ {
		// procura por LETTER+ - DIGIT+
		j := i
		for j < len(branch) && branch[j] >= 'A' && branch[j] <= 'Z' {
			j++
		}
		if j == i || j >= len(branch) || branch[j] != '-' {
			continue
		}
		k := j + 1
		for k < len(branch) && branch[k] >= '0' && branch[k] <= '9' {
			k++
		}
		if k > j+1 && k-j >= 2 && j-i >= 2 {
			return branch[i:k]
		}
	}
	return ""
}

// ConvergenceStats agrega resolved_at_turn por algum group key.
type ConvergenceStats struct {
	Group    string `json:"group"`
	Count    int    `json:"count"`
	P50Turns int    `json:"p50_turns"` // mediana de resolved_at_turn (apenas sessions resolvidas)
	P90Turns int    `json:"p90_turns"`
	Resolved int    `json:"resolved"` // count das que tem resolved_at_turn > 0
	Total    int    `json:"total"`    // total de sessions no group
}

// ConvergenceByModel agrupa convergence por modelo.
func (db *DB) ConvergenceByModel() ([]ConvergenceStats, error) {
	return db.convergenceBy("COALESCE(model, '(unknown)')")
}

// convergenceBy é o helper genérico — group expr é injetado direto, então
// só usar com strings hardcoded.
func (db *DB) convergenceBy(groupExpr string) ([]ConvergenceStats, error) {
	q := fmt.Sprintf(`
		WITH grouped AS (
			SELECT %s AS grp, resolved_at_turn FROM sessions
		)
		SELECT grp,
		       COUNT(*) AS total,
		       SUM(CASE WHEN resolved_at_turn IS NOT NULL AND resolved_at_turn > 0 THEN 1 ELSE 0 END) AS resolved
		FROM grouped
		GROUP BY grp
		HAVING total > 0
		ORDER BY total DESC
	`, groupExpr)
	rows, err := db.conn.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConvergenceStats
	for rows.Next() {
		var c ConvergenceStats
		if err := rows.Scan(&c.Group, &c.Total, &c.Resolved); err != nil {
			return nil, err
		}
		c.Count = c.Resolved
		// Computa percentis em segunda passada
		c.P50Turns, c.P90Turns = db.percentilesOfResolved(groupExpr, c.Group)
		out = append(out, c)
	}
	return out, rows.Err()
}

// percentilesOfResolved devolve p50 e p90 de resolved_at_turn pra um group.
func (db *DB) percentilesOfResolved(groupExpr, group string) (p50, p90 int) {
	q := fmt.Sprintf(`
		SELECT resolved_at_turn FROM sessions
		WHERE %s = ? AND resolved_at_turn IS NOT NULL AND resolved_at_turn > 0
		ORDER BY resolved_at_turn ASC
	`, groupExpr)
	rows, err := db.conn.Query(q, group)
	if err != nil {
		return 0, 0
	}
	defer rows.Close()
	var values []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err == nil {
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return 0, 0
	}
	p50 = values[len(values)/2]
	p90Idx := (len(values) * 9) / 10
	if p90Idx >= len(values) {
		p90Idx = len(values) - 1
	}
	p90 = values[p90Idx]
	return
}

// LoopHit representa um padrão de tool repetido suspeito.
type LoopHit struct {
	SessionID    string    `json:"session_id"`
	ToolName     string    `json:"tool_name"`
	InputHash    string    `json:"input_hash"`
	InputPreview string    `json:"input_preview"`
	Count        int       `json:"count"`
	SpanSecs     float64   `json:"span_secs"` // tempo entre primeiro e último
	FirstAt      time.Time `json:"first_at"`
}

// DetectLoops devolve padrões `count >= minCount` de mesmo (tool, input_hash)
// numa janela <= windowSecs. Útil pra identificar agente preso em retry.
//
// Default: minCount=3, windowSecs=300 (3 calls iguais em ≤5min). Janela
// curta (60s) é estrita demais pra dados retroativos — agentes lentos com
// pause humana entre retries são "loops" práticos mesmo com gap >60s.
// Ordena por count desc, span asc — mais "apertados" primeiro.
func (db *DB) DetectLoops(minCount int, windowSecs float64) ([]LoopHit, error) {
	if minCount < 2 {
		minCount = 3
	}
	if windowSecs <= 0 {
		windowSecs = 300
	}
	windowNs := int64(windowSecs * 1e9)
	// Pega max(input_preview) — qualquer linha do grupo serve, todas têm
	// mesmo hash logo mesmo preview (assumindo hash determinístico).
	rows, err := db.conn.Query(`
		SELECT session_id, tool_name, input_hash,
		       MAX(input_preview) AS preview,
		       COUNT(*) AS cnt,
		       MIN(ts) AS first_ts,
		       MAX(ts) - MIN(ts) AS span_ns
		FROM tool_events
		GROUP BY session_id, tool_name, input_hash
		HAVING cnt >= ? AND span_ns <= ? AND span_ns > 0
		ORDER BY cnt DESC, span_ns ASC
		LIMIT 50
	`, minCount, windowNs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoopHit
	for rows.Next() {
		var h LoopHit
		var firstTs, spanNs int64
		if err := rows.Scan(&h.SessionID, &h.ToolName, &h.InputHash, &h.InputPreview, &h.Count, &firstTs, &spanNs); err != nil {
			return nil, err
		}
		h.FirstAt = time.Unix(0, firstTs)
		h.SpanSecs = float64(spanNs) / 1e9
		out = append(out, h)
	}
	return out, rows.Err()
}

const selectSessionSQL = `
SELECT session_id, project_dir, jsonl_path, jsonl_mtime,
	start_time, end_time, message_count, user_messages, assistant_messages,
	first_user_msg, last_user_msg, git_branch, claude_version, model,
	input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
	sidechain_turns, sidechain_agents,
	COALESCE(resolved_at_turn, 0)
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
	// Carrega tool_uses de TODAS as sessions numa unica query — antes era N+1
	// (1 query por session). Em DBs com 100+ sessions isso somava centenas de
	// ms no startup do TUI.
	toolRows, err := db.conn.Query(`SELECT session_id, tool_name, count FROM tool_uses`)
	if err != nil {
		return out, nil // tool_calls é opcional, não falha o list
	}
	allTools := map[string]map[string]int{}
	for toolRows.Next() {
		var sid, name string
		var count int
		if err := toolRows.Scan(&sid, &name, &count); err == nil {
			if allTools[sid] == nil {
				allTools[sid] = map[string]int{}
			}
			allTools[sid][name] = count
		}
	}
	toolRows.Close()
	for _, s := range out {
		if t, ok := allTools[s.SessionID]; ok {
			s.ToolCalls = t
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
		&s.SidechainTurns, &s.SidechainAgents, &s.ResolvedAtTurn,
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

// Insight é um card sugerido pelo advisor AI.
type Insight struct {
	ID              int64
	Type            string
	Title           string
	Description     string
	Evidence        string
	SuggestedAction string
	CreatedAt       int64
}

func (db *DB) InsightsList() ([]*Insight, error) {
	rows, err := db.conn.Query(`SELECT id, type, title, description, evidence, suggested_action, created_at FROM ai_insights ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Insight
	for rows.Next() {
		var i Insight
		var ev, sug sql.NullString
		if err := rows.Scan(&i.ID, &i.Type, &i.Title, &i.Description, &ev, &sug, &i.CreatedAt); err != nil {
			return nil, err
		}
		i.Evidence = ev.String
		i.SuggestedAction = sug.String
		out = append(out, &i)
	}
	return out, rows.Err()
}

func (db *DB) InsightsReplaceAll(insights []*Insight) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM ai_insights`); err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, ins := range insights {
		if _, err := tx.Exec(
			`INSERT INTO ai_insights (type, title, description, evidence, suggested_action, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			ins.Type, ins.Title, ins.Description, ins.Evidence, ins.SuggestedAction, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) ProfileGet() (string, int64, error) {
	row := db.conn.QueryRow(`SELECT content, generated_at FROM ai_profile WHERE id = 1`)
	var content string
	var ts int64
	if err := row.Scan(&content, &ts); err != nil {
		if err == sql.ErrNoRows {
			return "", 0, nil
		}
		return "", 0, err
	}
	return content, ts, nil
}

func (db *DB) ProfileSet(content string) error {
	_, err := db.conn.Exec(`
INSERT INTO ai_profile (id, content, generated_at) VALUES (1, ?, ?)
ON CONFLICT(id) DO UPDATE SET content=excluded.content, generated_at=excluded.generated_at`,
		content, time.Now().Unix())
	return err
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

// SearchFTS roda FTS5 MATCH com Porter stemmer (fuzzy: 'docker' → casa
// 'dock', 'docked', 'docks', 'docker'). Bom pra natural language.
func (db *DB) SearchFTS(query string) ([]SearchResult, error) {
	return db.searchFTSImpl(query, false)
}

// SearchFTSExact roda FTS5 + post-filtra com LIKE pra exigir match literal
// da palavra. Bom pra brand names ('Docker', 'Postgres') e termos técnicos
// onde stem produz falsos positivos.
func (db *DB) SearchFTSExact(query string) ([]SearchResult, error) {
	return db.searchFTSImpl(query, true)
}

func (db *DB) searchFTSImpl(query string, exact bool) ([]SearchResult, error) {
	sql := `
		SELECT session_id, role,
			snippet(messages_fts, 2, '[', ']', '…', 16) AS snippet,
			rank
		FROM messages_fts
		WHERE messages_fts MATCH ?
	`
	args := []any{query}
	if exact {
		// LIKE filtra resultados onde o conteúdo contém a query literal
		// (case-insensitive). FTS5 usa o índice primeiro e LIKE só roda
		// no result set pequeno — sem custo significativo.
		sql += ` AND lower(content) LIKE ?`
		args = append(args, "%"+strings.ToLower(query)+"%")
	}
	sql += ` ORDER BY rank LIMIT 100`

	rows, err := db.conn.Query(sql, args...)
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

// Knowledge é o "segundo cérebro" extraído de uma session — problema,
// solução, decisões, learnings, padrões de código e tech usada.
// Cada session pode ter zero ou um Knowledge associado.
type Knowledge struct {
	SessionID      string
	JSONLMtime     int64
	Problem        string
	Solution       string
	Decisions      string // JSON array de {decision, rationale}
	Learnings      string // JSON array de strings
	CodePatterns   string // JSON array
	TechUsed       string // JSON array
	OpenQuestions  string // JSON array
	GeneratedAt    int64
}

// KnowledgeGet busca a entrada de uma session.
func (db *DB) KnowledgeGet(sessionID string) (*Knowledge, error) {
	row := db.conn.QueryRow(`
		SELECT session_id, jsonl_mtime, problem, solution, decisions,
		       learnings, code_patterns, tech_used, open_questions, generated_at
		FROM session_knowledge WHERE session_id = ?`, sessionID)
	var k Knowledge
	if err := row.Scan(&k.SessionID, &k.JSONLMtime, &k.Problem, &k.Solution,
		&k.Decisions, &k.Learnings, &k.CodePatterns, &k.TechUsed,
		&k.OpenQuestions, &k.GeneratedAt); err != nil {
		return nil, err
	}
	return &k, nil
}

// KnowledgeUpsert grava ou atualiza.
func (db *DB) KnowledgeUpsert(k *Knowledge) error {
	const q = `
INSERT INTO session_knowledge (session_id, jsonl_mtime, problem, solution,
	decisions, learnings, code_patterns, tech_used, open_questions, generated_at)
VALUES (?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(session_id) DO UPDATE SET
	jsonl_mtime=excluded.jsonl_mtime,
	problem=excluded.problem,
	solution=excluded.solution,
	decisions=excluded.decisions,
	learnings=excluded.learnings,
	code_patterns=excluded.code_patterns,
	tech_used=excluded.tech_used,
	open_questions=excluded.open_questions,
	generated_at=excluded.generated_at`
	_, err := db.conn.Exec(q, k.SessionID, k.JSONLMtime, k.Problem, k.Solution,
		k.Decisions, k.Learnings, k.CodePatterns, k.TechUsed, k.OpenQuestions,
		k.GeneratedAt)
	return err
}

// KnowledgeList devolve todas entradas (pra agregações cross-session).
func (db *DB) KnowledgeList() ([]*Knowledge, error) {
	rows, err := db.conn.Query(`
		SELECT session_id, jsonl_mtime, problem, solution, decisions,
		       learnings, code_patterns, tech_used, open_questions, generated_at
		FROM session_knowledge ORDER BY generated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Knowledge
	for rows.Next() {
		var k Knowledge
		if err := rows.Scan(&k.SessionID, &k.JSONLMtime, &k.Problem, &k.Solution,
			&k.Decisions, &k.Learnings, &k.CodePatterns, &k.TechUsed,
			&k.OpenQuestions, &k.GeneratedAt); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

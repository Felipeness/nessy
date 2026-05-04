# Code Analysis (Phase 2 — Mapper)

Per-package deep analysis. Format follows the `nessy-mapper` skill spec: purpose,
public API, internal patterns, dependencies, data flow, side effects, open questions.

---

## `internal/parser`

**Purpose**: Parse Claude Code's session JSONL files (one session per file, append-only,
each line a turn message or tool event). Produces `model.Session` (metadata aggregated)
and `[]Message` (transcripts), plus auxiliary extractors for tool events and file
operations. 🟢 (`internal/parser/jsonl.go` package comment + structure)

**Public API**:
- `ParseSession(path string) (*Session, error)` — `jsonl.go` 🟢. Parses metadata
  (start/end time, message count, tokens, branch, model, sidechain stats).
- `ParseMessages(path string) ([]Message, error)` — `jsonl.go` 🟢. Full transcript
  for indexing/embedding.
- `LastUserMessages(path string, n int) ([]Message, error)` — `jsonl.go` 🟢.
  Used by detail panel to show last N user turns without loading full transcript.
- `ParseToolEvents(path string) ([]ToolEvent, error)` — `jsonl.go` 🟢. Extracts
  `tool_use` events for loop-detection.
- `ParseFileOps(path string) ([]FileOp, error)` — `jsonl.go` 🟢. File reads/edits
  for retrabalho-rate stats.
- `ParseLedger(path string) ([]LedgerEntry, error)` — `ledger.go` 🟢. Parses Claude's
  ledger format (token-by-token cost log).
- `ListSessions() ([]*Session, error)` — `jsonl.go` 🟢. Walks `~/.claude/projects/`,
  parses each, returns flat list. Used by `nessy list` CLI (NOT by TUI which uses
  the indexed DB).
- `IsWarmup(s *Session) bool`, `IsClearOnly(s *Session) bool` — `jsonl.go` 🟢.
  Filter predicates for ingest config.
- `DecodeProjectDir(name string) string` — `jsonl.go` 🟢. Reverses Claude's path
  encoding (slashes→dashes).

**Patterns**:
- Pure functions over file paths — no DB coupling 🟢. Makes parser testable in isolation.
- Re-exports `model.Session` as `parser.Session` (alias) — convenience for callers
  that don't want to import `internal/model` separately. 🟢 `jsonl.go` (`type Session = model.Session`)
- Golden tests against `internal/parser/testdata/` 🟢 `golden_test.go`

**Dependencies**:
- → `internal/model` (Session struct)
- → external: `encoding/json` stdlib only 🟢

**Side effects**: None in main API. `ListSessions` reads filesystem (`~/.claude/projects`). 🟢

---

## `internal/index`

**Purpose**: SQLite-backed index of parsed sessions + FTS5 messages + tool events +
AI cache (summaries, embeddings, knowledge). The hot data layer for everything except
streaming AI responses. 🟢

**Public API**:
- `Open(path string) (*DB, error)` — `sqlite.go` 🟢. Opens WAL-mode SQLite, runs
  migrations, returns `*DB`.
- `(*DB).Close()` — 🟢
- `(*DB).Conn() *sql.DB` — escape hatch for ad-hoc queries 🟢
- `(*DB).Reindex(root string)` / `ReindexFiltered(root, filter)` —
  `reindex.go` 🟢. Walks filesystem, parses new/changed JSONL files, upserts.
  Returns `ReindexStats{Scanned, New, Updated, Removed}`.
- `(*DB).Upsert(*Session) error` — single-session insert/update 🟢
- `(*DB).IndexMessages([]Message)` — populates `messages_fts` (BM25 search) 🟢
- `(*DB).IndexToolEvents(sid, []ToolEvent)` — for loop detection 🟢
- `(*DB).IndexFileOps(sid, []FileOp)` — for file-reuse / retrabalho stats 🟢
- `(*DB).GetByID(id string) (*Session, error)` 🟢
- `(*DB).ListSessions() ([]*Session, error)` 🟢. **PERF:** carrega `tool_uses`
  numa unica query agregada (não N+1). Recente fix `commit 95a7310` 🟢
- AI cache: `AICacheGet/Upsert/List` — `sqlite.go` 🟢
- Knowledge: `KnowledgeGet/Upsert/List` 🟢
- Insights: `InsightsList` 🟢
- Profile: `ProfileGet/Set` 🟢
- Search: `SearchHybrid(...)` (delegates to `internal/search`) 🟢
- Stats helpers: `FileReuseTop`, `CostByTicketRows`, `ConvergenceByModel`,
  `DetectLoops` 🟢

**Patterns**:
- Migrations inline em `Open()` — `sqlite.go:206-…` usa PRAGMA table_info pra
  detectar schema antes de adicionar coluna 🟢. Idempotente.
- WAL mode habilitado via DSN `?_pragma=journal_mode(WAL)` 🟢 `sqlite.go:146`
- `parser_version` armazenado em `last_index_meta` table — quando muda, FTS é
  truncado e re-indexado pra popular colunas novas (ex: sidechain) 🟢 `reindex.go:72-85`
- Mtime cache pra skip arquivos não-modificados 🟢. **PERF:** preload mtime + fts
  count maps no inicio do reindex (`commit f7ea99f`) 🟢
- Pure-Go SQLite driver (`modernc.org/sqlite`) — sem CGO, mas ~2x slower que
  mattn/go-sqlite3 nos benchmarks 🟡 (web-known tradeoff, escolha intencional pra
  cross-compile sem C toolchain)

**Dependencies**:
- → `internal/model`, `internal/parser`
- → external: `database/sql` + `modernc.org/sqlite` 🟢

**Side effects**:
- Cria/escreve em `<cacheDir>/index.db` (WAL files também) 🟢
- Walk filesystem em `~/.claude/projects/` 🟢

**Open questions**: 🔴 Veja `questions.md` § index — `parser_version` está hardcoded
em qual file? Não achei via grep. 🔴

---

## `internal/ai`

**Purpose**: Tudo de AI local — Ollama HTTP client (chat/generate/embedding),
worker que processa background tasks (gerar summaries, embeddings, knowledge),
funções de high-level que orquestram (RAG chat, clustering, profile gen, knowledge
aggregation). 🟢

**Public API**:
- `Client` (`ollama.go`):
  - `NewClient(baseURL string) *Client` 🟢
  - `Health(ctx) bool` (timeout 2s) 🟢. **NOTE:** chamava Health em todo render do TUI
    travando 2s/keystroke quando Ollama offline; agora cacheado em `aiView.reachable`
    via `aiHealthCmd` (`commit 3f33588`) 🟢
  - `Generate(ctx, model, prompt) (string, error)` 🟢
  - `GenerateLong(ctx, model, prompt) (string, error)` — output 8192 tokens 🟢
  - `Chat(ctx, model, []ChatMessage) (string, error)` 🟢
  - `Embedding(ctx, model, text) ([]float32, error)` 🟢
- High-level (cada função recebe `*DB, *Client, model, ...`):
  - `BuildTranscript(s *Session) string` (`summary.go`) 🟢
  - `GenerateSummary(...)` 🟢
  - `GenerateInsights(...)` (`insights.go`) 🟢
  - `GenerateProfile(...)` (`profile`/`tech.go`) 🟢
  - `GenerateKnowledge(...)`, `GenerateKnowledgeAll(...)` 🟢
  - `RecomputeClusters(...)` (`clustering.go`, KMeans interno) 🟢
  - `ChatWithContext(...)` (`chat.go`) 🟢. RAG: query → embed → top-K
    similar sessions → inject snippets → chat com contexto.
  - `AggregateKnowledge(db) (*KnowledgeAggregate, error)` (`aggregate.go`) 🟢
- Embedding utils:
  - `Cosine([]float32, []float32) float64` (`similarity.go`) 🟢
  - `EncodeEmbedding/DecodeEmbedding` (`floatbits.go`) — float32 ↔ blob 🟢
- `Worker` (`worker.go`):
  - `NewWorker(db, client, gen, emb, hub) *Worker` 🟢
  - `(*Worker).Run(ctx)` — loop de background, processa queue 🟢
  - `(*Worker).Enqueue(sessionID)` 🟢

**Patterns**:
- Strategy: gen vs embed model passados como string em todas funções — usuario configura
  via `cfg.AI.GenModel/EmbedModel` 🟢
- Health check sempre com `context.WithTimeout(2s)` — non-blocking 🟢 `ollama.go:34`
- KMeans implementação inline (`clustering.go`) — sem dependência externa 🟢
- Retorno opcional de cluster info (nullable) — pra caso AI esteja desabilitada 🟡

**Dependencies**:
- → `internal/index`, `internal/model`, `internal/parser`
- → external: `net/http` stdlib 🟢

**Side effects**:
- HTTP requests pra Ollama (`localhost:11434` default) 🟢
- DB writes (cache de summaries, embeddings, knowledge) via passed `*DB` 🟢

**Open questions**: 🟡 Worker error handling — failures vão pra `genStatus` mas não tem
backoff explícito. Investigar.

---

## `internal/search`

**Purpose**: Busca híbrida (BM25 full-text + dense embedding similarity + metadata
filters) com Reciprocal Rank Fusion. 🟢

**Public API**:
- `SearchHybrid(db, query, opts)` (chamado de `internal/index`) 🟢
- `hybrid.go` contém RRF fusion + result type
- Modes: `metadata`, `body` (full-text only), `hybrid`, `semantic`

**Patterns**:
- RRF (Reciprocal Rank Fusion) clássico — combina rankings 🟢 (`hybrid_test.go`)
- Filtros parsed inline: `project:X`, `branch:Y`, `since:7d`, `cost:>1` 🟢

**Dependencies**: → `internal/index`, `internal/model`

**Side effects**: SQL queries (read-only) 🟢

---

## `internal/server`

**Purpose**: Web Studio HTTP server. Serve React SPA (embedded via `embed.go`),
expose REST API pra dados de sessions, SSE pra eventos live. 🟢

**Public API**:
- `Run(s *Server, listen, openBrowser bool) error` (`server.go`) 🟢
- Handlers em `handlers.go` (REST endpoints)
- SSE em `sse.go` — broadcast de refresh events
- Statusline editor endpoints — `statusline.go`, `statusline_studio.go`

**Patterns**:
- Hub + EventBroadcaster interface — desacopla broadcast logic do worker AI 🟢
- Embedded SPA via `webDist` em `embed.go` (root-level) 🟢
- SPA fallback — paths sem extensão servem `index.html` 🟢

**Dependencies**: → `internal/index`, `internal/ai`, `internal/parser`, `internal/model`,
`internal/stats`, `internal/statusline`

**Side effects**: HTTP listener (default `:5555`), file I/O quando edita statusline
config 🟢

---

## `internal/mcp`

**Purpose**: MCP (Model Context Protocol) server stdio — permite outros Claudes
consultarem teu histórico via tools registradas. Mais um entry point pra os mesmos
dados (search/knowledge/etc), exposto a Claudes que rodam fora do Claude Code do user. 🟢

**Public API**:
- `NewServer(name, version) *Server` (`server.go`) 🟢
- `(*Server).Register(t Tool, h Handler)` — registra tool callable 🟢
- `(*Server).Run(ctx) error` — loop stdio 🟢
- `Install(opts) (*InstallResult, error)` (`install.go`) — adiciona entry no
  `~/.claude/settings.json` mcpServers 🟢
- `Uninstall(settingsPath, name)` 🟢

**Patterns**:
- JSON-RPC 2.0 protocol no stdio (`protocol.go`) 🟢
- Tools registradas estaticamente em `mcp_tools.go` (root) — search, ask, knowledge,
  insights, etc. 🟢

**Dependencies**: → `internal/index`, `internal/ai`, `internal/parser`, `internal/stats`

**Side effects**: Reads stdin, writes stdout (JSON-RPC). 🟢

---

## `tui/`

**Purpose**: Bubble Tea TUI com 10 tabs sobre os mesmos dados. Frontend rico pra
exploração interativa. ~22.6k LOC, maior single file `threads.go` (~1700 LOC). 🟢

**Estrutura por tab**:
- `app.go` — root Model + Update/View dispatch + key handling
- `search.go`, `recent.go`, `stats.go`, `costs.go`, `timeline.go`, `tools.go`,
  `behavior.go`, `ai.go`, `ness.go`, `threads.go` — tab-per-file
- `viewer.go` — modal session viewer (overlay)
- `widgets.go` — helpers compartilhados (`scrollWindow`, `scrollByOffset`,
  `padLinesToWidth`, `branchColor`, `breadcrumb`)
- `chart.go` — BarChart, Gauge, Sparkline, Heatmap
- `style.go` — color constants + tab styles
- `keys.go` — keybindings
- `detail.go` — detail panel (right-side em split layout)

**Public API** (do package `tui`, exported):
- `New(db, p, cfg, state, statePath, aiDeps) Model` 🟢
- `(*Model).SetInitialIngest(cmd tea.Cmd)` — async ingest setup 🟢
- `MakeIngestCmd(db, root, filter) tea.Cmd` 🟢
- `(*Model).PendingResume() *Session` — usado por main.go pra invocar `claude --resume`
  depois que TUI sai 🟢
- `AIDeps` struct 🟢

**Patterns**:
- Bubble Tea elm-style (Model/Update/View) 🟢
- Lazy init de views pesadas — `behaviorView` agora computa stats so quando
  user entra na tab (era 52s eager no startup) 🟢 `commit 3f33588`
- Async via tea.Cmd: refresh ingest, AI health tick, behavior compute 🟢
- Scroll viewport: `scrollWindow` (centra cursor), `scrollByOffset` (offset
  explicito sem cursor) 🟢
- Full-width vs split layout: `threads.IsFullWidth()` decide se renderWide
  do app.go usa split (40/60) ou full-width 🟢
- Ghost render fix: `padLinesToWidth` + `tea.ClearScreen` no toggle 🟢

**Dependencies**: → `internal/index`, `internal/ai`, `internal/config`, `internal/model`,
`internal/parser`, `internal/pricing`, `internal/stats`, `internal/sysutil`,
`internal/viewer`, charmbracelet/bubbletea, lipgloss, bubbles

**Side effects**:
- Terminal alt-screen (via `tea.WithAltScreen()`) 🟢
- Disparar `claude --resume` subprocess depois do prog.Run() retornar 🟢

**Open questions**: 🟡 Galaxy renderGalaxy — coordenação cluster radius vs star size
ainda tem ajuste fino possível.

---

## `skills/` (NOVO)

**Purpose**: Skills bundled embedded no binário — instaláveis em engines de AI
(Claude Code/Codex/Cursor) via `nessy install`. Habilita o modo delegated do
spec generation (`/nessy`). 🟢

**Public API**:
- `FS() fs.FS` — embedded filesystem 🟢
- `Names() []string` — lista de skills bundled 🟢

**Conteúdo**:
- `nessy/` — orchestrator (5-phase pipeline coordinator)
- `nessy-mapper/` — Phase 2 module analysis
- `nessy-decoder/` — Phase 3 implicit knowledge
- `nessy-blueprint/` — Phase 4 architecture synthesis
- `nessy-scribe/` — Phase 5 operational specs

Cada skill é `<name>/SKILL.md` com frontmatter `name + description` e prompt body.

**Patterns**: Confidence labels 🟢🟡🔴 obrigatórios em todos outputs. Read-only por
default. State em `.nessy/state.json` pra resume. 🟢

**Side effects**: None ate user invoke. Outputs do skill execution vão pra `_nessy_atlas/`.

---

## Root files

- **`main.go`** — CLI dispatcher (~25 subcommands), TUI bootstrap, AI worker setup
  com Ollama health check assincrono. Profile flag `NESSY_PROFILE=1` pra timing logs. 🟢
- **`cli.go`** — implementations dos CLI commands (list/search/ask/etc) 🟢
- **`mcp_tools.go`** — registra MCP tools (search/ask/knowledge/insights/etc) 🟢
- **`embed.go`** — embeds `web/dist` no binário pra Web Studio 🟢
- **`cmd_install.go`** — `nessy install` + `nessy uninstall` (NOVO) 🟢

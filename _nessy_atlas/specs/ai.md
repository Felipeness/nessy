# Spec: ai (Ollama + worker + RAG + KMeans)

**Source**: `internal/ai/`
**Last updated**: 2026-05-04
**Confidence overall**: 🟢

## Purpose

Toda inteligência local. Wraps Ollama HTTP API e provê high-level operations
(summary, knowledge extraction, profile gen, RAG chat, embedding similarity,
clustering). Worker em background processa fila de sessions a serem analisadas.

🟢 Opcional — ativado via `cfg.AI.Enabled = true`. Nessy funciona 100% sem AI
(busca FTS, stats, threads, etc.).

## Public interface

### `Client` (Ollama HTTP)

```go
func NewClient(baseURL string) *Client
func (c *Client) Health(ctx context.Context) bool
func (c *Client) Generate(ctx, model, prompt) (string, error)
func (c *Client) GenerateLong(ctx, model, prompt) (string, error)
func (c *Client) Chat(ctx, model, []ChatMessage) (string, error)
func (c *Client) Embedding(ctx, model, text) ([]float32, error)
```

- **Health timeout**: hard 2s 🟢 `ollama.go:34`. Cached caller-side em
  `aiView.reachable` pra evitar HTTP per render (commit 3f33588).
- **Generate timeout**: parent ctx; sem clamp interno. Caller responsável por
  `context.WithTimeout`.

### High-level operations

```go
func GenerateSummary(ctx, c *Client, model, transcript) (string, error)
func GenerateInsights(ctx, *DB, *Client, genModel) ([]*Insight, error)
func GenerateProfile(ctx, *DB, *Client, genModel) (string, error)
func GenerateKnowledge(ctx, *DB, *Client, genModel, *Session) (*Knowledge, error)
func GenerateKnowledgeAll(ctx, *DB, *Client, genModel) (gen, cached int, err error)
func RecomputeClusters(ctx, *DB, *Client, genModel) ([]ClusterInfo, error)
func ChatWithContext(ctx, *DB, *Client, gen, embed, []ChatMsg) (*ChatResponse, error)
func AggregateKnowledge(*DB) (*KnowledgeAggregate, error)
```

- 🟢 Cada função recebe `*DB` pra cache lookup/save.
- 🟢 Responsável por upsert em `ai_cache`/`ai_insights`/`ai_profile`/
  `session_knowledge` tables.

### Worker (background)

```go
type Worker struct{...}
func NewWorker(*DB, *Client, gen, embed, EventBroadcaster) *Worker
func (*Worker) Run(ctx context.Context)
func (*Worker) Enqueue(sessionID string)
func (*Worker) QueuedCount() int
```

- **Run**: loop processing queue. Pra cada session, gera summary + embedding +
  knowledge se não cached.
- **Enqueue**: thread-safe push.
- 🟢 Started por `main.go` quando `cfg.AI.Enabled`. `defer cancel()` no main.

## Required invariants

- 🟢 **INV-1**: Cache lookup por session_id + jsonl_mtime — se mtime mudou,
  cache invalidated. `worker.go` ou `GenerateSummary` checa antes de chamar
  Ollama.
- 🟢 **INV-2**: Embedding dimension consistente per model. Caller deve usar
  mesmo embed_model em ingest e query (cosine só faz sentido se vetores no
  mesmo espaço).
- 🟡 **INV-3**: Worker drops failed jobs sem retry. Failures vão pra
  `genStatus` field. Se Ollama OOM, próximas sessions enqueued continuam
  tentando — pode haver thrash. **Investigate**.

## Error model

| Erro | Causa | Ação |
|---|---|---|
| `ctx.DeadlineExceeded` | timeout Ollama (lento ou OOM) | retorno; caller decide |
| HTTP 500 (Ollama) | model não carregado / OOM | log + skip session |
| HTTP 404 (Ollama) | model name errado | fatal config error |
| Connection refused | Ollama não rodando | Health check pega antes (cached) |

## Health check pattern (importante!)

```go
// ❌ NUNCA chame de View() — bloqueia render por 2s
reachable := client.Health(context.Background())

// ✅ Cache + tea.Cmd (padrão atual)
// 1. Init() dispatcha aiHealthCmd(client)
// 2. aiHealthMsg → Update grava em m.ai.reachable + reschedule via aiHealthTickCmd
// 3. View() lê m.ai.reachable instantâneo
```

🟢 Lessons learned em `commit 3f33588` — antes do fix, cada keystroke do TUI
travava 2s quando Ollama offline. Brutal.

## Dependencies

- → `internal/index`, `internal/model`, `internal/parser`
- → external: `net/http` stdlib + `encoding/json`

## Canonical paths

### Generate knowledge for a single session
```go
client := ai.NewClient(cfg.AI.OllamaURL)
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()
sess, _ := db.GetByID(sessionID)
k, err := ai.GenerateKnowledge(ctx, db, client, "llama3.2", sess)
// k written to session_knowledge table; returned for inline use
```

### RAG chat
```go
resp, _ := ai.ChatWithContext(ctx, db, client, "llama3.2", "nomic-embed-text",
    []ai.ChatMsg{{Role: "user", Content: "como resolvi o bug de auth?"}})
fmt.Println(resp.Content)
for _, src := range resp.Sources {
    fmt.Printf("  [%s] %.0f%%\n", src.SessionID[:8], src.Similarity*100)
}
```

## Modification guide

- 🟢 Adicionar nova generation function: criar arquivo `internal/ai/<x>.go`,
  função recebe `(ctx, *DB, *Client, model, ...)`, escreve em table dedicada.
- 🟡 Mudar prompt — versionar via constante e migrar cache. Senão prompts
  novos batem em cache antigo (resultado stale).
- 🔴 NÃO chame Ollama de hot path do TUI (View, render). Sempre via tea.Cmd.

## Test coverage

- 🟡 Zero tests no `internal/ai/`. Testar Ollama precisa fixture HTTP server
  ou mock. Recomendado: pelo menos prompt-formatting tests + cache invalidation
  tests.

## Related specs

- See also: `specs/index.md` (cache table schemas)
- See also: `specs/tui-integration.md` (health check cache pattern)

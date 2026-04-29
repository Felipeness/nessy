# AI profiling — Implementation Plan (Fase 5)

> Inline execution. Sub-skill: superpowers:executing-plans.

**Goal:** Adicionar AI-powered profiling (resumos, clustering, similar search) usando Ollama local. Toggle pra desativar.

**Spec:** [`../specs/2026-04-29-ai-profiling-design.md`](../specs/2026-04-29-ai-profiling-design.md)

## Sub-milestones

- **5.a**: Ollama client + config (Tasks 91-93)
- **5.b**: AI cache + summary (Tasks 94-96)
- **5.c**: Embeddings + similar (Tasks 97-99)
- **5.d**: Clustering + topic labels (Tasks 100-102)
- **5.e**: REST + TUI + Web tabs (Tasks 103-106)
- **5.f**: Smoke + push (Task 107)

---

### Task 91: Ollama HTTP client

**Files:** Create `internal/ai/ollama.go`

- [ ] Cliente Go pra Ollama: `Generate(model, prompt) (string, error)`, `Embedding(model, text) ([]float32, error)`, `Health() bool` (GET `/api/tags` com 2s timeout).
- [ ] Commit `feat: ollama HTTP client`.

### Task 92: AI config

**Files:** Modify `internal/config/config.go`

- [ ] Adiciona `AI` section: `Enabled bool`, `OllamaURL string`, `GenModel string`, `EmbedModel string`, `AutoGenerate bool`.
- [ ] Defaults: `enabled=true`, `url="http://localhost:11434"`, `gen_model="qwen2.5:7b"`, `embed_model="nomic-embed-text"`, `auto_generate=true`.
- [ ] Commit `feat: AI config com toggle e modelos`.

### Task 93: CLI flag --no-ai

**Files:** Modify `main.go`

- [ ] `cmdServe`/`cmdTui` aceitam `--no-ai` que sobrescreve `cfg.AI.Enabled = false`.
- [ ] `--ai-model NAME` opcional pra trocar gen model em runtime.
- [ ] Commit `feat: CLI flags --no-ai e --ai-model`.

### Task 94: AI cache schema

**Files:** Modify `internal/index/sqlite.go`

- [ ] Adiciona tabela `ai_cache` na schema SQL.
- [ ] Métodos: `AICacheGet(sessionID) (*AICache, error)`, `AICacheUpsert(c *AICache) error`, `AICacheList() ([]*AICache, error)`.
- [ ] Commit `feat: ai_cache table e CRUD`.

### Task 95: Summary generation

**Files:** Create `internal/ai/summary.go`

- [ ] `BuildTranscript(s *model.Session) string` — concat user/assistant msgs até 8000 chars.
- [ ] `GenerateSummary(client, model, transcript) (string, error)` — chama Ollama com prompt em pt-BR, retorna 1ª linha.
- [ ] Test simples (mock client).
- [ ] Commit `feat: gerador de resumo de session via LLM`.

### Task 96: Background generation worker

**Files:** Create `internal/ai/worker.go`

- [ ] `Worker` struct com queue chan string, db, client. Método `Enqueue(id)` e `Run(ctx)`.
- [ ] Quando processado: gera summary + embedding, persist no `ai_cache`, broadcast SSE.
- [ ] Commit `feat: background worker pra geração AI`.

### Task 97: Embeddings

**Files:** Modify `internal/ai/summary.go`

- [ ] `GenerateEmbedding(client, model, text) ([]float32, error)` chamando `/api/embeddings`.
- [ ] `EncodeEmbedding(emb []float32) []byte` (gob ou binary.Write); `DecodeEmbedding(blob []byte) []float32`.
- [ ] Commit `feat: embeddings via nomic-embed-text`.

### Task 98: Cosine similarity + FindSimilar

**Files:** Create `internal/ai/similarity.go`

- [ ] `Cosine(a, b []float32) float64`.
- [ ] `FindSimilar(db, sessionID, n) ([]SimilarResult, error)` — load all embeddings, computa cosine, top N.
- [ ] Commit `feat: cosine similarity e FindSimilar`.

### Task 99: REST `/api/ai/similar/:id`

**Files:** Modify `internal/server/handlers.go`

- [ ] Handler que registra rota e retorna array.
- [ ] Commit `feat: endpoint /api/ai/similar`.

### Task 100: K-means clustering

**Files:** Create `internal/ai/clustering.go`

- [ ] `KMeans(embeddings [][]float32, k, maxIter int, seed int64) ([]int, [][]float32)` — retorna cluster ids e centroids.
- [ ] k-means++ init + loop até convergência.
- [ ] Commit `feat: k-means clustering puro Go`.

### Task 101: Topic labels via LLM

**Files:** Modify `internal/ai/clustering.go`

- [ ] Pra cada cluster: pega 5 sessions mais próximas do centroide, monta prompt "dê 2-3 palavras de label pra esse grupo: {sample summaries}", LLM gera label.
- [ ] Persist em `ai_cache.topic_label` + `topic_cluster`.
- [ ] Commit `feat: topic labels via LLM por cluster`.

### Task 102: REST `/api/ai/clusters` + `/api/ai/health` + `/api/ai/summaries` + `/api/ai/generate-all`

- [ ] `/api/ai/health` retorna `{enabled, ollama_reachable, gen_model, embed_model, cached_count, total_count}`.
- [ ] `/api/ai/clusters` agrupa sessions com `topic_cluster != -1`.
- [ ] `/api/ai/summaries` lista todas em cache.
- [ ] `POST /api/ai/generate-all` enqueue todas as sessions sem cache.
- [ ] `POST /api/ai/generate/:id` enqueue 1 (force regen).
- [ ] Commit `feat: endpoints ai/health, clusters, summaries, generate-all`.

### Task 103: TUI tab AI

**Files:** Create `tui/ai.go`, modify `tui/app.go`, `tui/keys.go`

- [ ] `aiView` com seções: header status, clusters list, similar to selected, queue progress.
- [ ] tab `tabAI` adicionado, keybind `8`.
- [ ] Commit `feat: tab AI na TUI com clusters e similar`.

### Task 104: TUI integra summary em Recent + Detail

**Files:** Modify `tui/recent.go`, `tui/detail.go`

- [ ] Recent row: usa summary se disponível em `ai_cache`, fallback first_user_msg.
- [ ] Detail panel: seção "🧠 Resumo" + comando `R` regenera.
- [ ] Commit `feat: TUI Recent/Detail mostram AI summary`.

### Task 105: Web tab AI

**Files:** Create `web/src/tabs/AITab.tsx`, modify `web/src/App.tsx`, `web/src/components/Layout.tsx`, `web/src/types.ts`, `web/src/api.ts`

- [ ] `AITab` com 3 sub-tabs internas (Clusters/Similar/Summaries) ou 3 seções stacked.
- [ ] Status banner topo, botão "Generate all".
- [ ] Listener SSE `summary_done` atualiza UI em tempo real.
- [ ] Commit `feat: tab AI na web com clusters, similar, summaries`.

### Task 106: Web Recent/Detail integram summary

**Files:** Modify `web/src/components/SessionRow.tsx`, `web/src/components/DetailPanel.tsx`

- [ ] Mostra summary no preview/header quando disponível.
- [ ] DetailPanel: nova seção "🧠 Resumo" + botão "Find similar".
- [ ] Commit `feat: web Recent/Detail mostram AI summary`.

### Task 107: Smoke + README + push

- [ ] Smoke: `claude-history serve` com Ollama rodando vê tab AI funcional.
- [ ] Smoke: `--no-ai` desativa.
- [ ] README: seção "AI" com instalação Ollama.
- [ ] Push.
- [ ] Commit `docs: README com fase 5 + smoke`.

---

## Self-review

- ✅ Toggle: 92, 93
- ✅ Health: 91, 102
- ✅ Cache: 94
- ✅ Summary: 95, 96
- ✅ Embedding: 97
- ✅ Similar: 98, 99
- ✅ Clusters: 100, 101, 102
- ✅ TUI: 103, 104
- ✅ Web: 105, 106
- ✅ Polish: 107

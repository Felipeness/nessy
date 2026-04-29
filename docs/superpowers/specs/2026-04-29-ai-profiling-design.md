---
title: AI-powered profiling — Fase 5
status: approved
date: 2026-04-29
phase: 5
parent: 2026-04-29-behavioral-advanced-design
---

# AI-powered profiling — Design

## Goals

1. Resumo automático de cada session (1 linha em pt-BR)
2. Clustering temático automático via embeddings + K-means
3. Find similar sessions (cosine similarity em embeddings)
4. AI insights: tópicos da semana, padrões emergentes
5. **Toggle pra desativar AI** (config + CLI flag)
6. **TUI igual Web** — sem perder features

## Non-goals

- Cloud LLM (Anthropic API direto) — só local
- Fine-tuning
- Code mining (Fase 6)

## Tech stack

| Componente | Escolha |
|---|---|
| LLM runtime | Ollama HTTP API local em `localhost:11434` |
| Modelo geração | `qwen2.5:7b` (default, configurável) |
| Modelo embedding | `nomic-embed-text` (137M, dim 768) |
| Cache | SQLite tabela `ai_cache(session_id, mtime, summary, embedding BLOB, topic_label)` |
| Clustering | K-means Go puro, K=10, max_iters=50 |
| Linguagem | Go puro pra HTTP client e math |

## Toggle

`~/.claude-history/config.toml`:
```toml
[ai]
enabled = true
ollama_url = "http://localhost:11434"
gen_model = "qwen2.5:7b"
embed_model = "nomic-embed-text"
auto_generate = true   # background fill on launch
```

CLI flags (override temporário):
- `--no-ai` em `serve`/`tui` desativa run-time
- `--ai-model qwen2.5:14b` permite trocar modelo

Ordem de prioridade: CLI flag > config TOML > defaults.

Quando desabilitado:
- Endpoints `/api/ai/*` retornam 503 com `{"error": "ai disabled"}`
- TUI/Web tabs AI mostram badge "AI off" + instrução pra ativar
- Background generation suspensa
- Recent/Detail mostram preview tradicional (first user msg)

## Detecção automática

Mesmo com `enabled=true`, se Ollama não responder em `localhost:11434/api/tags` (timeout 2s), degrade silently — UI mostra "Ollama unreachable, run `ollama serve`".

## Cache schema

```sql
CREATE TABLE IF NOT EXISTS ai_cache (
  session_id TEXT PRIMARY KEY REFERENCES sessions(session_id) ON DELETE CASCADE,
  jsonl_mtime INTEGER NOT NULL,
  summary TEXT,
  embedding BLOB,            -- gob-encoded []float32 dim=768
  topic_cluster INTEGER,     -- -1 se não clusterizado ainda
  topic_label TEXT,
  generated_at INTEGER NOT NULL
);
```

Invalidação: se `sessions.jsonl_mtime != ai_cache.jsonl_mtime`, regen.

## Geração de resumo

Prompt template (pt-BR):

```
Resuma a conversa abaixo entre um dev e Claude Code em UMA frase de no máximo
20 palavras, em português brasileiro, focando no que foi feito (não no que foi
discutido). Não use markdown, só texto puro.

CONVERSA:
{transcript}

RESUMO (1 frase):
```

`{transcript}`: concat de user + assistant msgs separados por `---`, truncado em 8000 chars (cabe em 7B context).

## Embedding

Concatena user msgs (até 4000 chars) → POST `/api/embeddings` `{model, prompt}` → recebe `[]float64`. Convertemos pra `[]float32`, gob-encode em BLOB.

## Clustering

K-means simples:
1. Inicialização k-means++ (escolhe centroides longe entre si)
2. Iteração: assign + update até convergência ou 50 iters
3. K=10 fixo

Após clustering, pra cada cluster, pega 5 sessions mais próximas do centroide, manda pro LLM pedindo: "dê um label de 2-3 palavras pra esse grupo". Cache em `topic_label`.

## Find similar

`GET /api/ai/similar/:id?n=10` → cosine similarity entre embedding da session e todos os outros, retorna top N.

## REST endpoints

| Endpoint | Método | Retorno |
|---|---|---|
| `/api/ai/health` | GET | `{enabled: bool, ollama_reachable: bool, model: string}` |
| `/api/ai/summary/:id` | GET | `{summary: string, generated_at: ts}` (gera se não tem cache) |
| `/api/ai/summaries` | GET | array `{session_id, summary}` (todas em cache) |
| `/api/ai/similar/:id?n=` | GET | array `{session_id, similarity: float}` |
| `/api/ai/clusters` | GET | array `{cluster_id, label, sessions: [...]}` |
| `/api/ai/generate-all` | POST | dispara background fill, retorna `{queued: N}` |
| `/api/ai/generate/:id` | POST | gera/regenera 1 session, retorna summary |

## Background generation

Worker goroutine no server. Queue chan ID. Consumer pega 1 por vez, gera, persiste em cache, broadcast SSE `summary_done` com `{session_id, summary}`.

## Frontend Web — nova tab "AI" (8ª)

- Status header: "Ollama: ✓ qwen2.5:7b · 12/47 sessions resumidas"
- Botão "Generate all" (mostra progress)
- Tab interno "Clusters": cards dos 10 grupos com label + count + top sessions
- Tab interno "Similar": input search by session ID, mostra top 10 similar
- Tab interno "Summaries": lista todas com summary

Modificações fora da tab AI:
- **Recent**: preview muda pra summary quando disponível (fallback first user msg)
- **DetailPanel**: nova seção "🧠 Resumo" + buttons "Find similar" / "Regenerate"

## TUI — nova tab "AI" (8ª)

Layout textual:
- Header status: `Ollama: ✓ qwen2.5:7b · 12/47 cached`
- Subseções:
  - **Clusters** (lista textual):
    ```
    Cluster 0 [setup mac]                 8 sessions
      • a007e9ad  ~/Desktop/...  baixa para mim o jogo magic arena
      • 19a6a4ba  ~/             tudo certo agora?
      • ...
    ```
  - **Similar to selected** (cursor session na Recent):
    ```
    Top 10 similar to {selected.id}
      0.92  fca83e96  tem todas umas config de monitoramento...
      0.81  ...
    ```
  - **Generation queue**: progress visual

Keybind:
- Tab8 (`8`): jump to AI
- `G`: dispara generate-all
- `R`: regenera summary da session selecionada (em qualquer tab)

## Critérios de aceitação

- [ ] Toggle on/off via TOML + CLI flag
- [ ] Detect Ollama down, degrade gracefully
- [ ] `qwen2.5:7b` + `nomic-embed-text` setup defaults
- [ ] SQLite cache com invalidation por mtime
- [ ] Resumos 1-line gerados em pt-BR
- [ ] Embeddings persisted como BLOB
- [ ] K-means clustering com 10 grupos
- [ ] Topic labels via LLM
- [ ] Find similar via cosine
- [ ] Background generate-all
- [ ] SSE broadcast `summary_done` em tempo real
- [ ] Tab AI na Web e TUI
- [ ] Recent/DetailPanel integram summary
- [ ] Sem regressão se AI desabilitado

## Riscos

| Risco | Mitigação |
|---|---|
| Ollama não está rodando | Health check, degrade gracefully com instrução |
| Modelo não baixado | Endpoint health detecta, mostra `ollama pull` no UI |
| Geração lenta (5-10s/session) | Background queue, SSE progress, eager-cache |
| Embedding model diff retorna dim diferente | Lock em config, recomputa todos se mudar |
| Cluster ruim com poucas sessions (<10) | Fallback: mostra "poucas sessions, clustering desabilitado" |
| BLOB embedding cresce muito | 768 floats × 4 bytes = 3KB/session — desprezível |
| K-means não-determinístico | Seed fixo + tiebreaker em sort |

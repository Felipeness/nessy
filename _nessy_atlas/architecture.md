# Architecture (Phase 4 — Blueprint)

## In one sentence

**Nessy** é um indexer + explorer local do histórico Claude Code escrito em Go,
distribuído como single binary cross-platform (npm wrapper esbuild-style),
estruturado em layers limpas (frontends → services → data → core), com **4
frontends** sobre o mesmo dataset SQLite e **2 modos de execução AI** (Ollama
local + skills delegáveis pra qualquer engine AI).

## Top-level layout

```
~/.claude/projects/<encoded>/<uuid>.jsonl   ← input (Claude Code grava aqui)
                ↓
        nessy ingest (filewalk + parser)
                ↓
        ~/.claude-history/index.db          ← SQLite WAL (sessions, msgs FTS,
                                              tool events, AI cache, knowledge)
                ↓
        4 frontends sobre os mesmos dados:
        ┌──────────┬────────────┬────────────┬────────────┐
        │   TUI    │  Web SPA   │    CLI     │ MCP server │
        │ (10 tabs)│ (statusline│ (~25 cmds) │ (stdio,    │
        │          │  studio)   │            │  tool exp) │
        └──────────┴────────────┴────────────┴────────────┘
                                              ↑
        Outro Claude consulta via MCP ────────┘
```

## Major flows

### Flow 1 — Cold start ingest 🟢

1. `main.cmdTuiInternal` chama `index.Open()` (SQLite WAL setup, migrations).
2. `tui.New(...)` constrói Model com `db.ListSessions()` (cached). Tabs lazy-init.
3. `prog.Run()` inicia bubbletea loop; `Init()` retorna `tea.Batch(spin.Tick,
   initialIngest)`.
4. `initialIngest` (goroutine) chama `db.ReindexFiltered()` — walk filesystem,
   diff por mtime, parse + upsert novos/mudados.
5. `refreshDoneMsg` → `m.reload()` → views recarregam dados frescos.
6. **Cold start total** ~20ms até primeiro paint (era 52s antes do fix
   `behaviorView` lazy compute).
🟢 `commit 3f33588`, `commit f7ea99f`

### Flow 2 — Search query (TUI) 🟢

1. User digita query em search input.
2. Enter → `searchView.search()` → `db.SearchHybrid(query, opts)`.
3. `internal/search/hybrid.go` faz BM25 (FTS) + dense embedding cosine + metadata
   filters → RRF fusion → top-N.
4. Results renderizados com snippets, agrupados por session (default) ou
   expanded (ctrl+t).
5. Cursor selectable; Enter retoma session via `claude --resume <id>` depois
   prog.Run() retornar (race-free).

### Flow 3 — `/nessy` spec generation (NOVO, delegated mode) 🟢

1. User instala via `nessy install` no projeto target.
2. Skills copiados pra `.claude/skills/nessy*/SKILL.md`.
3. User digita `/nessy` no Claude Code.
4. Orchestrator skill ativa → Phase 1 recon → escreve `_nessy_atlas/inventory.md`,
   `.nessy/state.json`.
5. Para, pede confirmação. User confirma.
6. Invoca sub-skills sequencialmente: mapper (P2), decoder (P3), blueprint (P4),
   scribe (P5).
7. Resultado: `_nessy_atlas/` com toda a documentação SDD com confidence labels.

### Flow 4 — `nessy spec` local mode (FUTURO Phase 2) 🟡

1. User roda `nessy spec ~/projeto`.
2. Mesmo pipeline 5-fases mas executado pelo binário Go usando Ollama local.
3. Mesmo output em `_nessy_atlas/`.
4. **Vantagem**: zero token cost, total privacy, batch-able em CI.

### Flow 5 — MCP query (cross-Claude) 🟢

1. User configura `nessy mcp-install` → adiciona entry em `~/.claude/settings.json`.
2. Outro Claude Code (pode ser session diferente) chama tool `nessy.search` via
   MCP protocol.
3. `internal/mcp/server.go` roda em stdio, dispatcha pra handlers em `mcp_tools.go`.
4. Handler chama `db.Search...` etc, retorna resultado JSON.
5. Outro Claude usa resultado pra responder pergunta do user-2.

## External integrations

| System | Purpose | Module | Auth | Failure mode | Confidence |
|---|---|---|---|---|---|
| Ollama | Local LLM (gen + embed) | `internal/ai/ollama.go` | none (localhost) | timeout 2s, cache offline status | 🟢 |
| Claude CLI | `--resume <session>` | `tui/app.go cmdResume` | none (subprocess) | exits if `claude` not on PATH | 🟢 |
| GitHub Releases | Binary distribution | `.github/workflows/release.yml` (goreleaser) | GITHUB_TOKEN | CI fail, no auto-rollback | 🟢 |
| npm registry | npm wrapper distribution | `.github/workflows/npm.yml` | NPM_TOKEN secret | CI fail | 🟢 |
| AI engines (Claude Code/Codex/Cursor/Gemini) | Skill delegation | `cmd_install.go` | none (file copy) | engine ignora skill se SKILL.md inválido 🟡 | 🟢 |

## Tech debt callouts

1. 🟡 **Worker error handling** — `internal/ai/worker.go` errors vão pra
   `genStatus` mas sem backoff. Se Ollama OOM, worker pode burnar tentando.
2. 🟡 **Pricing rate** — `pricing.BRLRate` hardcoded, sem refresh live. User
   precisa atualizar manual quando câmbio se move.
3. 🟢 **Tests TUI = zero** — `tui/` package tem 0 `*_test.go`. Refactors
   recentes (scroll viewport, ghost render fix) foram via dump-tests ad-hoc.
   Snapshot tests recomendados.
4. 🟡 **`scripts/install.sh`** — curl install legado; quebra se repo for
   privado. Considerar deprecar agora que npm wrapper funciona.
5. 🟢 **Storage path inconsistency** — `~/.claude-history/` (cache) vs
   `_nessy_atlas/` (per-project) vs `.nessy/` (state). Documentado mas
   confuso pro user. Consolidar guideline em README.
6. 🔴 **Phase 2 do roadmap não implementado** — `nessy spec` CLI command
   ainda é placeholder. TUI tab `Specs` ainda não existe. MCP server não
   expõe specs.

# Code → Spec Matrix

Mapeamento file → spec. Útil pra próximo agent perguntar "que spec cobre `internal/foo`?".

| File / dir | Spec | Confidence |
|---|---|---|
| `internal/index/sqlite.go` | `specs/index.md` | 🟢 |
| `internal/index/reindex.go` | `specs/ingest.md` (+ `specs/index.md`) | 🟢 |
| `internal/parser/jsonl.go` | `specs/ingest.md` | 🟢 |
| `internal/parser/ledger.go` | `specs/ingest.md` (mencionado, sem spec dedicado) | 🟡 |
| `internal/ai/ollama.go` | `specs/ai.md` | 🟢 |
| `internal/ai/worker.go` | `specs/ai.md` | 🟢 |
| `internal/ai/chat.go` | `specs/ai.md` (RAG) | 🟢 |
| `internal/ai/clustering.go` | `specs/ai.md` (KMeans) | 🟢 |
| `internal/ai/aggregate.go`, `summary.go`, `insights.go`, `knowledge.go`, `tech.go` | `specs/ai.md` | 🟢 |
| `internal/ai/floatbits.go`, `similarity.go` | `specs/ai.md` (utility) | 🟢 |
| `internal/search/hybrid.go` | (encapsulado em `specs/index.md`) | 🟡 |
| `internal/server/*.go` | NOT COVERED — Web Studio merece spec dedicado | 🔴 GAP |
| `internal/mcp/*.go` | NOT COVERED — MCP server merece spec dedicado | 🔴 GAP |
| `internal/stats/*.go` | parcial em `domain.md` (BR-002 thread merging); spec dedicado faltando | 🟡 |
| `internal/config/config.go` | mencionado em `domain.md`; sem spec dedicado | 🟡 |
| `internal/pricing/pricing.go` | mencionado em `domain.md` (BR-003); sem spec dedicado | 🟡 |
| `internal/model/session.go` | core types; documentado em `domain.md` glossary | 🟢 |
| `internal/branding/*.go` | utility, sem spec | 🟡 (intencional — trivial) |
| `internal/statusline/*.go` | NOT COVERED — feature inteira (statusline editor) merece spec | 🔴 GAP |
| `internal/sysutil/*.go` | utility | 🟡 (intencional) |
| `internal/viewer/*.go` | parcial em `state-machines.md` § Viewer | 🟡 |
| `internal/watch/*.go` | NOT COVERED — investigar uso (file watcher?) | 🔴 GAP |
| `internal/advisor/*.go` | mencionado em `domain.md` (Insight); sem spec dedicado | 🟡 |
| `tui/app.go` | `specs/tui.md` | 🟢 |
| `tui/threads.go` | `specs/tui.md` (galaxy renderGalaxy redesign documented em changelog) | 🟢 |
| `tui/search.go`, `recent.go`, `stats.go`, `costs.go`, `timeline.go`, `tools.go`, `behavior.go`, `ai.go`, `ness.go` | `specs/tui.md` (per-tab brief, full deep-dive faltando) | 🟡 |
| `tui/widgets.go`, `chart.go`, `style.go`, `keys.go`, `viewer.go`, `detail.go` | `specs/tui.md` (helpers/widgets) | 🟢 |
| `skills/embed.go` | `specs/skills-install.md` | 🟢 |
| `skills/nessy*/SKILL.md` | self-documenting (são prompts); meta-doc em `specs/skills-install.md` | 🟢 |
| `cmd_install.go` | `specs/skills-install.md` | 🟢 |
| `main.go` | parcial — `specs/tui.md` (boot sequence), `specs/ingest.md` (filter setup); main flow não tem spec dedicado | 🟡 |
| `cli.go` | NOT COVERED — ~25 subcommands sem spec | 🔴 GAP |
| `mcp_tools.go` | NOT COVERED — junto com `internal/mcp/` | 🔴 GAP |
| `embed.go` (root) | mencionado em `architecture.md` (web SPA embedding); trivial | 🟡 |
| `web/src/**` | NOT COVERED — frontend React merece análise separada | 🔴 GAP |
| `npm/` | mencionado em `adrs/0004-scoped-npm-package.md`; sem spec dedicado | 🟡 |
| `docs/superpowers/**` | meta — vision/plans, não código | N/A |
| `scripts/install.sh` | flagged tech debt em `architecture.md` | 🟡 |

## Summary

- **Files com spec dedicado**: 25 arquivos → 5 specs (`index`, `ingest`, `ai`,
  `tui`, `skills-install`)
- **Files com partial coverage** (mencionado em domain/architecture/etc): ~30
- **Files NOT COVERED (🔴 GAP)**: `internal/server`, `internal/mcp`,
  `internal/statusline`, `internal/watch`, `cli.go`, `mcp_tools.go`, `web/src`
- **Files trivial (🟡 intencional)**: utility packages (`branding`, `sysutil`,
  embed.go root)

## Próximo round (se reapply pipeline)

Specs dedicados pra preencher gaps:
- `specs/server.md` — Web Studio HTTP API + SSE
- `specs/mcp.md` — MCP protocol + tools registered
- `specs/statusline.md` — statusline editor + render
- `specs/cli.md` — ~25 subcommands documentados
- `specs/web-studio.md` — React SPA structure (precisaria recursar)

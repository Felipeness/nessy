# Inventory

## Stack

- **Languages**: Go 1.26.2 🟢 (`go.mod:3`) + TypeScript/React 19 🟢 (`web/package.json:14`) + Node.js wrapper 🟢 (`npm/package.json`)
- **Frameworks**:
  - Backend (Go): Bubble Tea v1.3.10 (TUI), Lipgloss v1.1.0 (styling), Bubbles v1.0.0 (TUI components), modernc.org/sqlite v1.50.0 (pure-Go SQLite, no CGO) 🟢 `go.mod:7-10`
  - Frontend (React): Vite + React 19 + Tailwind v4 + dnd-kit + recharts 🟢 `web/package.json`
- **Build**:
  - Go: standard `go build` (CGO disabled in `.goreleaser.yaml:23`, ldflags pra version/commit/date) 🟢
  - Web: `bun install --frozen-lockfile && bun run build` (vite + tsc) — embedded no Go binary via `embed.go` 🟢 `embed.go:13`
  - Release: GoReleaser cross-compile (darwin/linux/windows × arm64/amd64) → tar.gz/zip → GitHub Release. NPM workflow publica platform packages com binarios. 🟢 `.goreleaser.yaml`, `.github/workflows/`

## Structure

```
nessy/
├── main.go                    # CLI entry — dispatches subcommands
├── cli.go                     # CLI command implementations (list, show, search, ask, etc.)
├── cmd_install.go             # `nessy install` — copia skills pra engines
├── mcp_tools.go               # MCP tool handlers (servidor consultavel por outros Claudes)
├── embed.go                   # web/dist embedded via go:embed
├── go.mod, go.sum             # deps
├── internal/                  # 16 pacotes internos (advisor, ai, branding, config,
│                              #   index, mcp, model, parser, pricing, search, server,
│                              #   stats, statusline, sysutil, viewer, watch)
├── tui/                       # Bubble Tea TUI: 10 tabs em ~22k LOC total
│                              #   (app, search, recent, stats, costs, timeline, tools,
│                              #    behavior, ai, ness, threads, viewer, etc.)
├── skills/                    # Skills bundled embedded — 5 nessy-* + embed.go
├── web/                       # React/Vite Studio — embedded no binario
├── npm/                       # Wrapper npm (main + 6 platform packages)
├── docs/superpowers/          # Vision docs, plans, gates, audits
├── scripts/                   # install.sh (curl install legado)
└── .github/workflows/         # release.yml (goreleaser) + npm.yml (publish)
```

## Entry points

- **`main.go:cmdMain`** — CLI dispatcher 🟢 `main.go:87-130`. Subcommands: `tui`, `serve`, `list`, `show`, `search`, `ask`, `insights`, `knowledge`, `aggregated`, `project`, `standup`, `advise`, `mcp`, `mcp-install`, `install`, `uninstall`, `daemon-*`, `statusline-*`, `similar`, `fzf`.
- **`tui.New(...)` → `prog.Run()`** — TUI loop 🟢 `main.go:446-449`
- **`internal/server/server.go`** — Web Studio HTTP server (port 5555 default, embeds React build via `embed.go`) 🟢 `embed.go:13`
- **`internal/mcp/server.go`** — MCP stdio server (consultable via Claude Code MCP integration) 🟢 `mcp_tools.go`

## External dependencies

### Go (`go.mod` direct)

| Package | Version | Purpose | Confidence |
|---|---|---|---|
| `BurntSushi/toml` | v1.6.0 | Config parsing (pricing.toml, config.toml) | 🟢 |
| `charmbracelet/bubbletea` | v1.3.10 | TUI framework | 🟢 |
| `charmbracelet/bubbles` | v1.0.0 | TUI widgets (spinner, etc.) | 🟢 |
| `charmbracelet/lipgloss` | v1.1.0 | Terminal styling | 🟢 |
| `modernc.org/sqlite` | v1.50.0 | Pure-Go SQLite (no CGO needed) | 🟢 |

### Web (`web/package.json`)

- React 19, Vite 8, TypeScript 6, Tailwind 4, recharts 3.8, @dnd-kit 6/10/3 — todos 🟢

### Runtime / external services (opcionais)

- **Ollama** (HTTP local em `localhost:11434`) — usado se `cfg.AI.Enabled = true`. Roda summaries/embeddings/chat localmente, não obrigatório. 🟢 `internal/ai/ollama.go:24`
- **Claude CLI** (`claude --resume <id>`) — invocado pelo TUI quando user pressiona Enter pra retomar uma session. 🟢 `tui/app.go` (cmdResume)

## Estimated complexity

- **LOC (Go only)**: ~22.6k 🟢 (`wc -l *.go internal/*/*.go tui/*.go` → 22596 total)
- **Files (Go)**: 83 🟢 (`find . -name "*.go" | wc -l`)
- **Top-level packages (Go)**: 16 internal + 4 root (main, tui, skills, npm)
- **TUI tabs**: 10 (search/recent/stats/costs/timeline/tools/behavior/ai/ness/threads)
- **Web frontend**: Vite SPA com Recharts (vários componentes), embedded
- **CLI subcommands**: ~25
- **Largest single file**: `tui/threads.go` (~1700 lines) — 6 sub-views (tree/cards/miller/graph/timeline/galaxy) 🟢
- **Test coverage**: tests em `internal/index`, `internal/parser`, `internal/pricing`, `internal/search` (4 packages com `_test.go`); zero tests no `tui/` 🟡

## Notes

- Projeto **ativo, em desenvolvimento rápido** — git log mostra commits diários
  (refactors, features, perf fixes ainda sendo entregues). É **legacy em devir**
  — codebase grande o suficiente pra ter knowledge implícita acumulada.
- Multi-output: TUI + Web Studio + CLI + MCP server — 4 frontends pro mesmo data model (sessions indexadas em SQLite).
- Plataforma cross-platform (darwin/linux/windows × arm64/amd64) já pipelined via goreleaser.

# Spec Impact Matrix

Reverse: dado um arquivo modificado, quais specs precisam ser revisados/
atualizados.

| If you modify... | These specs apply | Why |
|---|---|---|
| `internal/index/*.go` | `specs/index.md`, `specs/ingest.md` | Storage + ingest pipeline acoplados |
| `internal/parser/*.go` | `specs/ingest.md` | Parser é input do ingest |
| `internal/ai/*.go` | `specs/ai.md`, `specs/tui.md` (health pattern) | AI worker + Ollama; TUI tem cache especial |
| `internal/search/*.go` | `specs/index.md` | Search é encapsulado em index API |
| `internal/server/*.go` | (criar `specs/server.md` 🔴 GAP) | Web Studio sem spec hoje |
| `internal/mcp/*.go` | (criar `specs/mcp.md` 🔴 GAP) | MCP server sem spec |
| `internal/stats/*.go` | `domain.md` (BR-002 thread merge), `specs/tui.md` | Stats consumido pelas tabs Stats/Threads |
| `internal/pricing/pricing.go` | `domain.md` (BR-003, BR-004) | Cost calc rules |
| `internal/config/config.go` | `domain.md` (BR-001 ingest filter), `specs/ingest.md` | Config define filter behavior |
| `internal/model/session.go` | `domain.md` (glossary), `erd-complete.md` (estrutura ↔ schema) | Core type, propaga pra TUDO |
| `internal/statusline/*.go` | (criar `specs/statusline.md` 🔴 GAP) | Statusline editor + render |
| `internal/viewer/*.go` | `state-machines.md` (Viewer modal), `specs/tui.md` | Modal overlay no TUI |
| `tui/app.go` | `specs/tui.md`, `state-machines.md` | Root model + dispatch |
| `tui/threads.go` | `specs/tui.md` | Tab mais complexa (6 sub-views, galaxy graph) |
| `tui/widgets.go` | `specs/tui.md` (scrollWindow/scrollByOffset/padLinesToWidth invariants) | Helpers críticos pra ghost-render fix |
| `tui/<other-tab>.go` | `specs/tui.md` (per-tab section) | Layouts decisions |
| `skills/nessy*/SKILL.md` | `specs/skills-install.md` | Prompt content é o spec |
| `skills/embed.go` | `specs/skills-install.md` | Embed glue |
| `cmd_install.go` | `specs/skills-install.md` | Install logic |
| `main.go` | `specs/tui.md` (boot), `specs/ingest.md` (filter setup), `architecture.md` | Entry point afeta startup flows |
| `cli.go` | (criar `specs/cli.md` 🔴 GAP) | CLI commands sem spec |
| `mcp_tools.go` | (criar `specs/mcp.md` 🔴 GAP) | Tool handlers |
| Any `migrations/*.sql` | `specs/index.md`, `erd-complete.md` (schema cascade) | Schema change → propaga em queries + ingest |
| `pricing.toml` defaults | `domain.md` (BR-003) | User-overridable, mas defaults documentados |
| `.goreleaser.yaml` | `architecture.md` (deploy units), `adrs/0001-modernc-sqlite-no-cgo.md` | CGO flag, build matrix |
| `.github/workflows/release.yml` | `architecture.md` (deploy units) | Release pipeline |
| `.github/workflows/npm.yml` | `adrs/0004-scoped-npm-package.md`, `architecture.md` | npm distribution |
| `npm/package.json` | `adrs/0004-scoped-npm-package.md` | optionalDependencies pattern |
| `web/**` | (criar `specs/web-studio.md` 🔴 GAP) | React SPA front-end |
| Adicionar novo skill em `skills/<name>/SKILL.md` | `specs/skills-install.md` (lista skills), `skills/embed.go` (`go:embed` directive + `Names()`) | Embed precisa update |

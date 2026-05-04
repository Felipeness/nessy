# Spec: tui (Bubble Tea TUI, 10 tabs)

**Source**: `tui/`
**Last updated**: 2026-05-04
**Confidence overall**: 🟢

## Purpose

Frontend interativo principal. 10 tabs (Search, Recent, Stats, Costs, Timeline,
Tools, Behavior, AI, 🧠 Ness, Threads) sobre os mesmos dados SQLite. Sub-views
em alguns tabs (Threads tem 6: tree/cards/miller/graph/timeline/galaxy; Stats
tem 3: overview/models/detailed × 3 períodos).

🟢 ~22.6k LOC, maior single file `threads.go` (~1700 LOC). Architecture é
elm-style (Bubble Tea) com `Model + Update + View`.

## Public interface

```go
func New(db *index.DB, p *pricing.Pricing, cfg *config.Config,
         state *config.State, statePath string, aiDeps AIDeps) Model

func (*Model) SetInitialIngest(cmd tea.Cmd)
func MakeIngestCmd(db *index.DB, root string, filter index.IngestFilter) tea.Cmd

func (Model) PendingResume() *model.Session  // pra main.go invocar `claude --resume`
                                              // depois de prog.Run() retornar

type AIDeps struct {
    Enabled    bool
    Client     *ai.Client
    Worker     *ai.Worker
    GenModel   string
    EmbedModel string
}
```

## Required invariants

- 🟢 **INV-1**: View() output sempre tem exatas `m.width × m.height` cells.
  Garantido por `lipgloss.NewStyle().Width(m.width).Height(h).Render(...)` +
  `padLinesToWidth` quando layout mudou. Sem isso = ghost render no Windows
  Terminal (commit 6e93338).
- 🟢 **INV-2**: Cursor sempre visível na viewport. Garantido por `scrollWindow`
  (helper em `widgets.go`) que centraliza cursor em listas. Sem isso = user
  perde cursor ao passar do primeiro frame (commit f7ea99f).
- 🟢 **INV-3**: AI Health check NUNCA executa em hot path (View, Update). Cache
  em `aiView.reachable`, atualizado por `aiHealthCmd` async. Sem isso = freeze
  2s/keystroke (commit 3f33588).
- 🟢 **INV-4**: Ingest é assíncrono. Init() retorna `tea.Cmd` que dispara
  reindex em goroutine. First paint <100ms. Sem isso = startup 20-60s
  (commit f7ea99f, 3f33588).
- 🟢 **INV-5**: Lazy compute pra views pesadas (`behaviorView` com TopBigrams/
  TopTrigrams/CoOccurrences). Construtor só guarda inputs; compute via
  `behaviorComputeCmd` quando user entra na tab.

## Sub-view layouts

### Wide mode (m.width >= 120)

| Tab | Layout |
|---|---|
| Search/Recent | split 40/60 (left: list, right: detail) |
| Stats (overview/models) | split 40/60 (left: stats, right: detail) |
| Stats (detailed) | full-width |
| Costs/Timeline | full-width |
| Tools | split 40/60 (left: tool list, right: drilldown) |
| Behavior/AI/Ness | full-width |
| Threads | depende do sub-view: tree/cards/timeline = split, miller/graph/galaxy = full-width |

🟢 Decision em `app.go renderWide` + `threadsView.IsFullWidth()`.

### Narrow mode (m.width < 120)

Single-pane fallback. Detail é replaced by stats (s key alterna).

## Error model

| Caso | Comportamento |
|---|---|
| DB query fails durante refresh | status bar mostra erro, views ficam stale (não crash) |
| Ollama unreachable | `m.ai.reachable = false`, "❌" no status do AI tab, sem freeze |
| Viewer modal opened on session sem JSONL | mostra erro inline no modal, q fecha |
| Width=0 (nunca acontece exceto bug) | bodyHeight clampado em 5, layouts degradam graciosamente |

## Profile flag

`NESSY_PROFILE=1 nessy tui 2>profile.log` ativa timing logs em main.go +
tui.New per-construtor. Útil pra diagnosticar regressões de startup.

🟢 Adicionado em `commit 3f33588`. Format: `[Nms] <label>`.

## Canonical paths

### Daily browse session
1. `nessy tui` → cold start ~50ms
2. Tab 9 (Recent) — j/k navegar
3. Enter → exit TUI, `claude --resume <id>` invocado pelo main.go
4. (Sai do Claude) → próxima `nessy tui` mostra a session updated

### Search across history
1. Tab 1 (Search), digite query (ex: "auth middleware")
2. Modes: ctrl+y (fuzzy), ctrl+t (expand hits), ctrl+f
3. Enter pra retomar selected

### Threads exploration
1. Tab 0 (Threads), v cicla view (tree → cards → ... → galaxy → tree)
2. Em galaxy: scatter plot tempo×custo mas formato grafo (constelações por
   projeto), bolinhas multi-cell escaladas por session count
3. j/k move cursor entre threads/sessions
4. enter retoma a session selecionada

## Modification guide

- 🟢 Adicionar nova tab: const novo em `tabID iota`, append em `tabNames`,
  case em `renderTabBar`, `renderWide`, `renderNarrow`, `tabHint`. Bump
  `numTabs`. Adicione campo em `Model`. Construtor.
- 🟢 Adicionar scroll-via-↑↓ pra tab nova: campo `scroll int` na view, método
  `Scroll(delta int)`, despachar em `app.go moveCursor()`, aplicar
  `scrollByOffset` em View().
- 🟢 Mudança visual: edite `style.go` (cores) ou `widgets.go` (helpers
  compartilhados como branchColor, branchPill).
- 🟡 Mudança que adiciona work em hot path: SEMPRE async via `tea.Cmd`.
  Veja `behaviorComputeCmd` ou `aiHealthCmd` como template.
- 🔴 NÃO chame DB query síncrona em View(). NÃO chame HTTP em View(). NÃO
  chame `parser.ListSessions` em View() (faz filewalk).

## Test coverage

- 🟡 ZERO tests no `tui/`. Refactors recentes (scroll, ghost, lazy compute)
  validados via dump-tests ad-hoc:
  - Cria Model com WindowSizeMsg, simula keypresses, dump View() output
  - Verifica linha-por-linha
- Recomendado: snapshot tests pra views, table-driven pra layout decisions.

## Related specs

- See also: `specs/ai.md` (health check cache pattern)
- See also: `specs/ingest.md` (Init startup flow)
- See also: `state-machines.md` (tab/view transitions)

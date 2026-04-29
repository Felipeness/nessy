# TUI density pass — Implementation Plan (Fase 2.1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox `- [ ]` syntax for tracking.

**Goal:** Adensar a TUI da Fase 2 com 6 frentes (A-F) — lista densa, detail panel rico, stats dashboards, novas abas Costs/Timeline/Tools, polish visual, análise comportamental light.

**Architecture:** Sem mudanças estruturais. Adições incrementais aos pacotes `tui/`, novos módulos de stats em `internal/stats/` e config em `internal/config/`.

**Tech Stack:** Bubble Tea, Lipgloss, Bubbles (spinner novo), modernc.org/sqlite, BurntSushi/toml.

**Spec:** [`docs/superpowers/specs/2026-04-29-tui-density-design.md`](../specs/2026-04-29-tui-density-design.md)

---

## File Structure

```
internal/
├── config/
│   └── config.go                # NEW — config.toml + state.toml load/save
├── stats/
│   ├── stats.go                 # NEW — agregações (heatmap, baseline, trends, cache)
│   ├── behavioral.go            # NEW — top words, error patterns, prefixes, peak hour
│   └── stopwords.go             # NEW — listas pt-BR + en
└── (existentes)

tui/
├── app.go                       # MODIFY — 6 tabs, persistência, novos keybinds
├── recent.go                    # MODIFY — linha densa
├── search.go                    # MODIFY — linha densa
├── detail.go                    # MODIFY — bar charts, breakdown, sparkline, baseline, msgs trecho
├── stats.go                     # MODIFY — heatmap, modelos, projeção, long-tail, tendências, F1-F4
├── costs.go                     # NEW — tab Costs
├── timeline.go                  # NEW — tab Timeline
├── tools.go                     # NEW — tab Tools
├── chart.go                     # NEW — primitives (bar, gauge, sparkline, heatmap)
├── badge.go                     # NEW — badge modelo (S/O/H + cor)
├── style.go                     # MODIFY — cores adicionais
└── keys.go                      # MODIFY — keybinds extras
```

## Sub-milestones

- **2.1.a** — Tasks 21-26: Frente A (lista densa) + E (polish visual)
- **2.1.b** — Tasks 27-33: Frente B (detail panel rico)
- **2.1.c** — Tasks 34-40: Frente C (stats dashboards)
- **2.1.d** — Tasks 41-44: Frente F (behavioral light)
- **2.1.e** — Tasks 45-50: Frente D (novas abas Costs/Timeline/Tools)

---

## Sub-milestone 2.1.a — Lista densa + polish

### Task 21: Primitive `chart.go` (bar, gauge, sparkline)

**Files:** Create `tui/chart.go`

- [ ] **Step 1**: Implementar `BarChart(label string, value, max float64, width int, color lipgloss.Color) string`, `Gauge(value float64, width int) string`, `Sparkline(values []int) string` (8 níveis), `Heatmap(grid [][]int, rows, cols int) string`.
- [ ] **Step 2**: `go build` — sem testes (UI render).
- [ ] **Step 3**: Commit `feat: chart primitives (bar, gauge, sparkline, heatmap)`.

### Task 22: `badge.go` — model badges

**Files:** Create `tui/badge.go`

- [ ] Implementar `ModelBadge(model string) string` retornando `"S"`/`"O"`/`"H"`/`"?"` colorizado via lipgloss conforme regex no nome do modelo.
- [ ] Build + commit `feat: model badges (S/O/H) com cor`.

### Task 23: Lista densa em recent.go

**Files:** Modify `tui/recent.go`

- [ ] Reescrever `formatRow` pra incluir: duração (`fmtDuration`), badge modelo, tokens (`fmtTokens` em k/M), custo (via `pricing.Cost`).
- [ ] Helpers: `fmtDuration(d time.Duration) string` (45s/12m/1h23m), `fmtTokens(n int64) string` (1.2k/1.2M).
- [ ] Build + commit `feat: linha densa em recent (duração+modelo+tokens+custo)`.

### Task 24: Lista densa em search.go

**Files:** Modify `tui/search.go`

- [ ] Aplicar mesmo `formatRow` (extrair pra helper `tui/row.go` se necessário pra DRY).
- [ ] Build + commit `feat: linha densa em search`.

### Task 25: Polish keybinds extras

**Files:** Modify `tui/keys.go`, `tui/app.go`

- [ ] Adicionar bindings: `gg`/`G`/`PgUp`/`PgDn`/`n`/`N`/`,`/`Ctrl+E`/`1`-`6`.
- [ ] Handlers no `Update`:
  - `gg` (sequência): cursor=0 da tab ativa
  - `G`: cursor=last
  - `PgUp`/`PgDn`: ±10
  - `n`/`N`: next/prev resultado search
  - `1`-`6`: pula direto pra tab N
  - `,`: toggle settings overlay (placeholder por enquanto)
  - `Ctrl+E`: handler de export (Task 33)
- [ ] Build + commit `feat: keybinds extras (vim-style nav, tab jump)`.

### Task 26: Spinner durante reindex

**Files:** Modify `tui/app.go` — adicionar `spinner` field do `bubbles/spinner`

- [ ] Importar `github.com/charmbracelet/bubbles/spinner`. Adicionar `m.spinner = spinner.New()` no `New()`.
- [ ] Quando `refreshCmd` está rodando, status bar mostra `m.spinner.View() + " refreshing…"`.
- [ ] No `Update`, propagar `spinner.TickMsg` pra `m.spinner`.
- [ ] Build + commit `feat: spinner animado durante reindex`.

---

## Sub-milestone 2.1.b — Detail panel rico

### Task 27: Bar chart de tools no detail

**Files:** Modify `tui/detail.go`

- [ ] Substituir lista numérica de tools por `BarChart` calls. Categorizar tools em groups (execution/edit/read/other) com cores.
- [ ] Helper `toolCategory(name string) lipgloss.Color`.
- [ ] Build + commit `feat: bar chart de tools no detail panel`.

### Task 28: Breakdown de custo

**Files:** Modify `tui/detail.go`, `internal/pricing/pricing.go`

- [ ] Estender `pricing.Cost` pra retornar breakdown: `CostBreakdown{Input, Output, CacheCreation, CacheRead float64; USD, BRL float64}`.
- [ ] Detail render mostra cada linha com bar.
- [ ] Build + commit `feat: breakdown de custo no detail`.

### Task 29: Cache hit gauge

**Files:** Modify `tui/detail.go`

- [ ] `cacheHitRatio = cache_read / (cache_read + input)` (proxy).
- [ ] Render `Gauge(ratio, width)` com label "Cache hits".
- [ ] Build + commit `feat: cache hit gauge no detail`.

### Task 30: Sparkline do projeto

**Files:** Modify `tui/detail.go`, add helper `internal/stats/stats.go`

- [ ] Em `internal/stats/stats.go`, função `ProjectHistory(sessions []*model.Session, projectDir string, days int) []int` retornando count/dia.
- [ ] Detail render `Sparkline(history)` + total sessions e custo do projeto.
- [ ] Build + commit `feat: sparkline mini do histórico do projeto`.

### Task 31: Comparação com baseline

**Files:** Modify `tui/detail.go`, add helper em `internal/stats/stats.go`

- [ ] `Baseline(sessions []*model.Session, projectDir string)` retorna mediana de msgs/cost/duration das últimas 30 sessions do projeto.
- [ ] Detail render compara session selecionada com baseline. Setas `↑↑/↑/=/↓/↓↓` baseado em delta %.
- [ ] Esconde seção se < 3 sessions disponíveis.
- [ ] Build + commit `feat: comparação com baseline (mediana do projeto)`.

### Task 32: Trecho da última conversa

**Files:** Modify `tui/detail.go`, helper em `internal/parser/jsonl.go`

- [ ] `parser.LastUserMessages(path string, n int) []string` — re-parseia JSONL, retorna últimas N user msgs.
- [ ] Detail render exibe truncadas em 80 chars.
- [ ] Cache simples in-memory (mapa por sessionID) pra evitar re-parse a cada render.
- [ ] Build + commit `feat: trecho das últimas msgs no detail`.

### Task 33: Mini-stats + export

**Files:** Modify `tui/detail.go`, add `tui/export.go`

- [ ] Mini-stats: msgs/min, tokens/msg, ratio user:assistant — calculados inline.
- [ ] `tui/export.go`: `ExportSession(s *model.Session, outDir string) error` → escreve `<outDir>/<session-id>.json` com metadata + msgs + cost breakdown + tools.
- [ ] Handler de `Ctrl+E` no `app.go` chama export, status bar mostra "exported to ..." 3s.
- [ ] Build + commit `feat: mini-stats e export json com Ctrl+E`.

---

## Sub-milestone 2.1.c — Stats dashboards

### Task 34: Heatmap hora × dia (12 semanas)

**Files:** Modify `tui/stats.go`, helper em `internal/stats/stats.go`

- [ ] `Heatmap(sessions []*model.Session, weeks int) [6][7]int` — bins 4h × 7 dias.
- [ ] Stats render `Heatmap(grid)` com legenda "atividade".
- [ ] Build + commit `feat: heatmap atividade hora × dia em stats`.

### Task 35: Distribuição de modelos

**Files:** Modify `tui/stats.go`

- [ ] Calcular % msgs por modelo. Render barras com cor (badge) + count.
- [ ] Build + commit `feat: distribuição de modelos em stats`.

### Task 36: Custo cumulativo + projeção

**Files:** Modify `tui/stats.go`, helper em `internal/stats/stats.go`

- [ ] `CostThisMonth(sessions, pricing)` retorna `{accumulated, projection, today}`.
- [ ] Render: linha `Abril 2026: $X acumulado · projeção $Y`.
- [ ] Comparar com thresholds do `config.toml` — warning/alert visual.
- [ ] Build + commit `feat: custo cumulativo do mês + projeção + thresholds`.

### Task 37: Top tools por projeto (drill-down)

**Files:** Modify `tui/stats.go`

- [ ] Quando uma session está selecionada na lista (Recent ou Stats), o painel global de stats mostra top tools daquele PROJETO (não global). Toggle via cursor.
- [ ] Build + commit `feat: top tools com drill-down por projeto selecionado`.

### Task 38: Long-tail (top 5 mais caras / mais longas)

**Files:** Modify `tui/stats.go`

- [ ] Ordenar sessions por custo desc e duração desc, top 5 cada.
- [ ] Render como tabela curta.
- [ ] Build + commit `feat: long-tail top 5 caras + longas`.

### Task 39: Tendências semana atual vs anterior

**Files:** Modify `tui/stats.go`, helper em `internal/stats/stats.go`

- [ ] `WeekDelta(sessions, pricing)` retorna `{thisWeek, lastWeek, deltaPct}` pra sessions/msgs/cost/cacheHit.
- [ ] Render com setas direcionais.
- [ ] Build + commit `feat: tendências semana atual vs anterior`.

### Task 40: Cache savings global

**Files:** Modify `tui/stats.go`, helper em `internal/stats/stats.go`

- [ ] `CacheSavings(sessions, pricing, days int)` calcula valor economizado em cache hits (cache_read tokens × diff entre input rate e cache rate).
- [ ] Render linha "Cache savings: $X economizados em N dias".
- [ ] Build + commit `feat: cache savings global em stats`.

---

## Sub-milestone 2.1.d — Behavioral light

### Task 41: Stopwords + tokenizer

**Files:** Create `internal/stats/stopwords.go`, `internal/stats/behavioral.go`

- [ ] `stopwords.go`: lista hardcoded pt-BR (~150 palavras: de, a, o, que, e, do, da, em, um, para, com, não, etc.) + en (~150).
- [ ] `behavioral.go`: `Tokenize(text string) []string` → lowercase, regex `\b[\p{L}]+\b`, filtra stopwords.
- [ ] Test simples: `tokenize("Vamos instalar o Postgres") == ["vamos", "instalar", "postgres"]`.
- [ ] Commit `feat: tokenizer + stopwords pt-BR e en`.

### Task 42: Top palavras

**Files:** Modify `internal/stats/behavioral.go`, integrar em stats tab

- [ ] `TopWords(sessions []*model.Session, db *index.DB, n int) []WordCount` — pra cada session, busca user msgs no FTS5 (ou re-parseia), tokeniza, conta.
- [ ] Stats tab renderiza top 20.
- [ ] Build + commit `feat: top palavras do user em stats`.

### Task 43: Padrões de erro/correção

**Files:** Modify `internal/stats/behavioral.go`

- [ ] Lista `errorWords` hardcoded (errado, errei, rollback, desfaz, ...).
- [ ] `ErrorRate(sessions, db)` → conta msgs com qualquer dessas palavras / total user msgs.
- [ ] Stats render: `Sinais de retrabalho: 38 msgs (6%) — saudável` com cor por threshold.
- [ ] Build + commit `feat: detecção de retrabalho via heurística regex`.

### Task 44: Prefixos + horário de pico

**Files:** Modify `internal/stats/behavioral.go`

- [ ] `TopPrefixes(sessions, db, n int)` → primeira palavra de cada user msg, top N.
- [ ] `PeakHour(sessions)` → bar chart por hora do dia.
- [ ] Stats render ambos.
- [ ] Build + commit `feat: prefixos comuns + horário de pico`.

---

## Sub-milestone 2.1.e — Novas abas

### Task 45: Adicionar 6 tabs ao `tabID` e `tabNames`

**Files:** Modify `tui/app.go`

- [ ] `tabSearch, tabRecent, tabStats, tabCosts, tabTimeline, tabTools` enum.
- [ ] `tabNames = ["Search", "Recent", "Stats", "Costs", "Timeline", "Tools"]`.
- [ ] `(activeTab + 1) % 6` ao trocar tab.
- [ ] Keybinds `1`-`6` mapeiam direto.
- [ ] `renderBody` switch cobre todas (placeholder pra Costs/Timeline/Tools por enquanto).
- [ ] Build + commit `feat: 6 tabs (Search/Recent/Stats/Costs/Timeline/Tools)`.

### Task 46: Tab Costs

**Files:** Create `tui/costs.go`

- [ ] `costsView` com 3 seções: por dia (últimos 30), por projeto, por modelo.
- [ ] Por dia: bar chart vertical (cada bar = 1 dia, h em USD).
- [ ] Por projeto: top 10 com bar horizontal proporcional.
- [ ] Por modelo: bar com cor do badge.
- [ ] Cache savings no rodapé.
- [ ] Conectar no `app.go`.
- [ ] Build + commit `feat: tab Costs com 3 visões + cache savings`.

### Task 47: Tab Timeline

**Files:** Create `tui/timeline.go`

- [ ] `timelineView` agrupa sessions por dia. Hoje primeiro, depois ontem, depois últimos 7 dias.
- [ ] Cada session aparece como linha: `HH:MM ─●─ open <cwd> (msgs, cost) <activity_icon>`.
- [ ] Cursor navega entre sessions.
- [ ] Build + commit `feat: tab Timeline com cronologia visual`.

### Task 48: Tab Tools

**Files:** Create `tui/tools.go`

- [ ] `toolsView` agrega `tool_uses` table do SQLite — count por tool global, count de sessions distintas, média/session.
- [ ] Cursor seleciona tool. Painel direito (em wide) mostra top 10 sessions que mais usaram aquele tool.
- [ ] Build + commit `feat: tab Tools com drill-down por tool`.

### Task 49: Persistência de estado

**Files:** Create `internal/config/config.go`, modify `tui/app.go`

- [ ] `config.go` com:
  ```go
  type Config struct {
      Cost struct { WarnPerDayUSD, AlertPerDayUSD float64 }
      Behavioral struct { TopWordsCount int; ErrorWords, StopwordsExtra []string }
      UI struct { DefaultTab string }
  }
  type State struct {
      LastTab string
      RecentGroupByProject bool
      RecentFilter7d bool
      SearchMode string
  }
  func LoadConfig(path string) (*Config, error)
  func LoadState(path string) (*State, error)
  func SaveState(path string, s *State) error
  ```
- [ ] No `cmdTui`, carrega config+state, passa pra `tui.New(db, p, cfg, state)`.
- [ ] No `Update` quando recebe `tea.Quit`, antes de sair, escreve state.toml.
- [ ] Build + commit `feat: persistência de config e state entre runs`.

### Task 50: README + smoke final

**Files:** Modify `README.md`

- [ ] Atualizar README com lista das 6 tabs, novos keybinds, screenshots ASCII das novas features (heatmap, breakdown, etc.).
- [ ] Roadmap: marcar Fase 2.1 como done.
- [ ] Smoke test: `go test ./... && claude-history list && claude-history tui` (manual).
- [ ] Build + commit `docs: README com features da fase 2.1` + push.

---

## Self-Review

### Spec coverage

- ✅ A (lista densa) → Tasks 21-24
- ✅ B (detail rico) → Tasks 27-33
- ✅ C (stats dashboards) → Tasks 34-40
- ✅ D (novas abas) → Tasks 45-48
- ✅ E (polish) → Tasks 21-22, 25-26, 49
- ✅ F (behavioral) → Tasks 41-44

### Placeholders

- ✅ Sem TBD/TODO genérico
- ✅ Cada task tem files exatos
- ⚠️ Steps individuais menos verbosos que a Fase 2 (compromisso aceito pra economizar 5000 linhas de plan; engineer experiente preenche os detalhes)

### Type consistency

- `pricing.CostBreakdown` usado em B2 e D1 — definido em Task 28
- `stats.WordCount`, `stats.WeekDelta` — types definidos onde introduzidos
- `config.Config` e `config.State` — Task 49

---

## Execution Handoff

**Plan complete.** Vou executar inline em sequência respeitando os 5 sub-milestones, com smoke build (`go build`) entre cada um.

Cada task = 1 commit. Total estimado: **30 commits** após 14 da Fase 2.

Começando agora pelo sub-milestone 2.1.a.

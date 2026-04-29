# Behavioral advanced — Implementation Plan (Fase 4)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans.

**Goal:** Adicionar análise comportamental avançada determinística (n-grams, co-occurrence, flow, style, high-error) ao backend e expor em nova tab Behavior na Web e TUI.

**Spec:** [`../specs/2026-04-29-behavioral-advanced-design.md`](../specs/2026-04-29-behavioral-advanced-design.md)

---

## Sub-milestones

- **4.a** — Tasks 83-87: Backend stats functions (`internal/stats/behavioral_advanced.go`)
- **4.b** — Tasks 88-89: REST endpoint + TUI tab Behavior
- **4.c** — Tasks 90-92: Web tab Behavior + smoke

---

### Task 83: N-grams (Bigrams + Trigrams)

**Files:** Create `internal/stats/behavioral_advanced.go`

- [ ] `TopBigrams(sessions, n) []Bigram` — janela 2 palavras, count, sorted desc.
- [ ] `TopTrigrams(sessions, n) []Trigram` — janela 3 palavras.
- [ ] Test pequeno garantindo ordem/contagem.
- [ ] Commit `feat: bigrams e trigrams com tiebreaker alfabético`.

### Task 84: Co-occurrence + PMI

- [ ] `CoOccurrences(sessions, minCount, n) []CoOccur` — para cada user msg, todos os pares de palavras únicos (combinatorial). Score PMI = log2(P(ab) / (P(a)*P(b))).
- [ ] Filtro: count ≥ 3.
- [ ] Sort por PMI desc, tiebreaker alfabético.
- [ ] Commit `feat: co-occurrence pairs com score PMI`.

### Task 85: Conversation flow histogram

- [ ] `FlowDistribution(sessions) []FlowHist` — buckets logarítmicos.
- [ ] Plus retornar p50, p90, p99 das contagens de msgs.
- [ ] Commit `feat: flow distribution histogram`.

### Task 86: Style comparison user vs IA

- [ ] `StyleComparison(sessions) StyleStats` — agrega user e assistant separadamente: avg words, vocab único, top words.
- [ ] Commit `feat: style comparison user vs assistant`.

### Task 87: High-error sessions + Time vs cost points

- [ ] `HighErrorSessions(sessions, threshold) []ErrorSession` — recalcula error_rate per session (não global), filtra > threshold.
- [ ] `TimeCostPoints(sessions, pricing) []TimeCostPoint` — pontos (hour, cost_usd, model) pra ScatterChart.
- [ ] Commit `feat: high-error sessions e time-cost points`.

### Task 88: REST endpoint + TUI tab

- [ ] `/api/behavior/advanced` em handlers.go retorna struct com todos os outputs acima.
- [ ] `tui/behavior.go` novo, reusa BarChart/Sparkline.
- [ ] `tui/app.go` adiciona tabBehavior, atualiza numTabs=7, adiciona keybind, switch nas renderWide/Narrow.
- [ ] `tui/keys.go` adiciona Tab7.
- [ ] Build + commit `feat: endpoint /api/behavior/advanced + tab Behavior na TUI`.

### Task 89: Tab Behavior na Web

**Files:** Create `web/src/tabs/BehaviorTab.tsx`

- [ ] Fetch `/api/behavior/advanced`.
- [ ] KPI cards (4): bigrams count, co-occur count, error sessions, p90 msgs.
- [ ] Bigrams + Trigrams: 2 listas lado a lado.
- [ ] Co-occurrence: tabela com PMI score.
- [ ] Time vs cost: Recharts ScatterChart com cor por modelo.
- [ ] Flow histogram: Recharts BarChart.
- [ ] Style comparison: tabela 2 cols.
- [ ] High-error: lista clicável → opens session no DetailPanel.
- [ ] App.tsx: adicionar tabBehavior no router.
- [ ] Layout.tsx: adicionar Behavior na lista de tabs.
- [ ] types.ts: adicionar tipos.
- [ ] api.ts: adicionar `behaviorAdvanced()`.
- [ ] Build + commit `feat: tab Behavior na web com 7 visualizações`.

### Task 90: Smoke + commit final

- [ ] Build Go + Web.
- [ ] Push.
- [ ] Commit `chore: smoke fase 4 done`.

---

## Self-Review

- ✅ N-grams: Task 83
- ✅ Co-occurrence + PMI: Task 84
- ✅ Flow: Task 85
- ✅ Style: Task 86
- ✅ Errors + time/cost: Task 87
- ✅ Endpoint + TUI: Task 88
- ✅ Web tab: Task 89
- ✅ Smoke: Task 90

Sem placeholders. Tipos consistentes (Bigram/Trigram/CoOccur/FlowHist/StyleStats/ErrorSession/TimeCostPoint).

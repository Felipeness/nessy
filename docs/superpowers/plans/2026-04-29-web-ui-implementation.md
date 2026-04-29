# Web UI — Implementation Plan (Fase 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans.

**Goal:** Web UI local consumindo o indexer existente via REST + SSE, frontend React/Vite/Tailwind/Recharts bundlado via go:embed no mesmo binário Go.

**Architecture:** Backend HTTP em Go stdlib, ServeMux, handlers reutilizam internal/{index,stats,pricing,parser}. SSE pra push de reindex events. Frontend SPA React, hash routing, EventSource pra live updates. Single binary final.

**Tech stack:** Go 1.26 stdlib http, Vite 7, React 19, TypeScript, Tailwind v4, Recharts, TanStack Table.

**Spec:** [`../specs/2026-04-29-web-ui-design.md`](../specs/2026-04-29-web-ui-design.md)

---

## File Structure

```
internal/
└── server/
    ├── server.go                # NEW — HTTP server + auto-open
    ├── handlers.go              # NEW — REST endpoints
    ├── sse.go                   # NEW — SSE hub + broadcast
    └── server_test.go           # NEW — handler tests

web/
├── package.json
├── vite.config.ts
├── tailwind.config.ts
├── tsconfig.json
├── postcss.config.js
├── index.html
├── src/
│   ├── main.tsx
│   ├── App.tsx
│   ├── api.ts
│   ├── sse.ts
│   ├── types.ts
│   ├── components/{Tabs,SessionRow,DetailPanel,ModelBadge,Heatmap,Sparkline,CostBars,SearchResults,ExportButton}.tsx
│   ├── tabs/{Recent,Search,Stats,Costs,Timeline,Tools,Compare}Tab.tsx
│   └── styles.css
└── dist/                       # output, gitignored

main.go                          # MODIFY — adiciona "serve" subcomando
embed.go                         # NEW — go:embed do web/dist
```

## Sub-milestones

- **3.a** — Tasks 51-54: Backend HTTP + SSE skeleton
- **3.b** — Tasks 55-60: REST endpoints (sessions/stats/costs/timeline/tools/search/export)
- **3.c** — Tasks 61-65: Frontend scaffold (Vite/Tailwind/types/api/SSE)
- **3.d** — Tasks 66-72: Tabs (Recent/Search/Stats/Costs/Timeline/Tools/Compare)
- **3.e** — Tasks 73-77: Visualizações interativas (heatmap clicável, time series brush, search highlight, compare diff)
- **3.f** — Tasks 78-82: Polish (export CSV/PDF, go:embed, auto-open, README, smoke)

---

## Sub-milestone 3.a — Backend HTTP + SSE

### Task 51: HTTP server skeleton + listen flag

**Files:** Create `internal/server/server.go`, modify `main.go`

- [ ] **Step 1**: `internal/server/server.go` exporta `Run(db *index.DB, p *pricing.Pricing, listen string, openBrowser bool) error`. Usa `http.NewServeMux`. Registra rotas placeholder. Logs "listening on http://...".
- [ ] **Step 2**: `main.go` adiciona case `"serve"` que aceita flags `--port N`, `--listen ADDR`, `--no-open`.
- [ ] **Step 3**: Build, smoke test (`claude-history serve` → curl http://localhost:5555/health → 200 ok).
- [ ] **Step 4**: Commit `feat: subcomando serve com http server skeleton`.

### Task 52: SSE hub + broadcast

**Files:** Create `internal/server/sse.go`

- [ ] **Step 1**: `Hub` struct com map de clients (channels). Métodos `Subscribe(w http.ResponseWriter)` e `Broadcast(event string, data any)`.
- [ ] **Step 2**: Handler `/api/events` que mantém connection aberta, faz Subscribe, escreve eventos em formato `event: NAME\ndata: JSON\n\n`.
- [ ] **Step 3**: Test manual: `curl -N http://localhost:5555/api/events` mantém conexão aberta.
- [ ] **Step 4**: Commit `feat: SSE hub para push de eventos pro frontend`.

### Task 53: Auto-open browser

**Files:** Modify `internal/server/server.go`

- [ ] **Step 1**: Função `openBrowser(url string)` — exec `open` (darwin), `xdg-open` (linux), `start` (windows). Erro silencioso.
- [ ] **Step 2**: Após `ln, _ := net.Listen()`, dispara goroutine que sleep 100ms + `openBrowser` se flag setada.
- [ ] **Step 3**: Smoke: `claude-history serve` deve abrir Chrome.
- [ ] **Step 4**: Commit `feat: auto-open browser no claude-history serve`.

### Task 54: LAN warning prompt

**Files:** Modify `main.go`

- [ ] **Step 1**: Se `--listen` começa com `0.0.0.0` ou IP não-loopback, print warning + prompt `[y/N]`. Se não confirmar, abort.
- [ ] **Step 2**: Smoke: `claude-history serve --listen 0.0.0.0:5555` → prompt aparece.
- [ ] **Step 3**: Commit `feat: warning interativo ao expor na LAN`.

---

## Sub-milestone 3.b — REST endpoints

### Task 55: `/api/sessions` + `/api/sessions/:id`

**Files:** Create `internal/server/handlers.go`

- [ ] **Step 1**: Handler `handleSessions(db)` retorna array de `*model.Session` em JSON (use `json.NewEncoder(w).Encode`).
- [ ] **Step 2**: Handler `handleSessionByID(db)` parseia path, retorna 1 session ou 404.
- [ ] **Step 3**: Test: `curl http://localhost:5555/api/sessions | jq '.[0]'` → JSON válido com fields.
- [ ] **Step 4**: Commit `feat: REST sessions list e by-id`.

### Task 56: `/api/sessions/:id/messages?n=N`

- [ ] Handler reusa `parser.LastUserMessages(path, n)`. Default n=10. Retorna `[{role,content}]`.
- [ ] Test: `curl ".../sessions/abc-123/messages?n=5"` retorna 5 ou menos.
- [ ] Commit `feat: REST messages endpoint`.

### Task 57: `/api/stats` + `/api/stats/behavioral`

- [ ] `/api/stats` retorna struct com: `heatmap` (grid), `model_distribution`, `month_cost`, `week_delta`, `top_projects`, `cache_savings`.
- [ ] `/api/stats/behavioral` retorna `top_words`, `top_prefixes`, `error_rate`, `peak_hour`.
- [ ] Reusa `internal/stats` package.
- [ ] Commit `feat: REST stats endpoints`.

### Task 58: `/api/costs`

- [ ] Retorna `{by_day:[{date,usd}], by_project:[{dir,usd}], by_model:[{name,usd}], cache_savings, projection}`.
- [ ] Commit `feat: REST costs breakdown`.

### Task 59: `/api/timeline?from=&to=`

- [ ] Parsea query params como ISO dates. Default últimos 7 dias.
- [ ] Retorna `{date: [sessions]}` map agrupado.
- [ ] Commit `feat: REST timeline com range filter`.

### Task 60: `/api/tools` + `/api/search` + `/api/refresh` + `/api/export/:id`

- [ ] `/api/tools` retorna `[{name, total_calls, num_sessions}]` ordenado por calls.
- [ ] `/api/tools/:name/sessions` retorna top sessions que usaram aquela tool.
- [ ] `/api/search?q=&mode=metadata|fts` retorna `[{session_id, role, snippet, rank}]`.
- [ ] `POST /api/refresh` chama `db.Reindex()` e broadcast SSE `reindex_done`. Retorna `ReindexStats`.
- [ ] `GET /api/export/:id` retorna JSON detalhado da session (stream).
- [ ] Commit `feat: REST endpoints tools/search/refresh/export`.

---

## Sub-milestone 3.c — Frontend scaffold

### Task 61: Vite + React + TS + Tailwind

**Files:** Create `web/`

- [ ] **Step 1**: `cd web && bun create vite . --template react-ts` (se Bun reclamar do dir, usar `bun create vite tmp --template react-ts && mv tmp/* . && rmdir tmp`).
- [ ] **Step 2**: Tailwind v4: `bun add -D tailwindcss @tailwindcss/vite`. Config em `vite.config.ts` plugin.
- [ ] **Step 3**: `src/styles.css` com `@import "tailwindcss";`. `App.tsx` minimal "Hello".
- [ ] **Step 4**: `bun dev` → http://localhost:5173 mostra "Hello" estilizado com Tailwind.
- [ ] **Step 5**: `web/.gitignore` com `node_modules/`, `dist/`.
- [ ] **Step 6**: Commit `feat: vite scaffold com tailwind v4`.

### Task 62: Tipos compartilhados + api.ts

**Files:** Create `web/src/types.ts`, `web/src/api.ts`

- [ ] `types.ts` com `Session`, `Stats`, `Behavioral`, `Costs`, `Timeline`, `ToolStat`, `SearchResult`, `Cost` — espelho dos JSONs do backend.
- [ ] `api.ts` com helpers tipados: `getSessions(): Promise<Session[]>`, `getStats()`, `getCosts()`, etc. base URL via env var.
- [ ] Commit `feat: tipos compartilhados e api client`.

### Task 63: SSE wrapper (hooks/useSSE)

**Files:** Create `web/src/sse.ts`

- [ ] Hook `useSSE<T>(url, eventName)` retorna estado atualizado quando event chega. Usa `EventSource` nativa.
- [ ] Cleanup on unmount.
- [ ] Commit `feat: hook useSSE com auto-reconnect`.

### Task 64: Tabs router (hash-based)

**Files:** Create `web/src/App.tsx`, `web/src/components/Tabs.tsx`

- [ ] `App.tsx`: lê `window.location.hash`, mantém estado `activeTab`. Listener `hashchange`.
- [ ] `Tabs.tsx` componente puro: array de tabs + active + onChange (atualiza hash).
- [ ] Tabs hardcoded: Recent, Search, Stats, Costs, Timeline, Tools, Compare.
- [ ] Commit `feat: SPA router hash-based com tabs`.

### Task 65: Layout base + dark theme

**Files:** Create `web/src/components/Layout.tsx`

- [ ] Layout com header (logo "claude-history" + status/refresh button) + tabs + main content + footer (count sessions, last refresh).
- [ ] Dark theme via Tailwind `dark:` classes.
- [ ] Responsive: tabs no topo desktop, bottom nav mobile.
- [ ] Commit `feat: layout base com header/footer/tabs`.

---

## Sub-milestone 3.d — Tabs

### Task 66: RecentTab

**Files:** Create `web/src/tabs/RecentTab.tsx`, `web/src/components/SessionRow.tsx`

- [ ] Fetch `/api/sessions`, render lista densa: ModelBadge + dur + tokens + custo + dir + preview.
- [ ] Activity icon (🟢🟡⚪) computado client-side.
- [ ] Group by project toggle.
- [ ] Click numa row abre DetailPanel.
- [ ] Commit `feat: RecentTab com lista densa`.

### Task 67: SearchTab

**Files:** Create `web/src/tabs/SearchTab.tsx`, `web/src/components/SearchResults.tsx`

- [ ] Input box, prefix `:body ` switcha pra full-text.
- [ ] Highlights `<mark>` no snippet do FTS.
- [ ] Click no result abre DetailPanel scrollado até msg.
- [ ] Commit `feat: SearchTab com highlight visual`.

### Task 68: StatsTab

**Files:** Create `web/src/tabs/StatsTab.tsx`, `web/src/components/Heatmap.tsx`

- [ ] Cards: total sessions, custo mês, projeção, cache savings.
- [ ] `<Heatmap>` interativo (CSS grid + hover tooltip + click filter).
- [ ] Recharts `<PieChart>` distribuição modelos.
- [ ] Recharts `<BarChart>` top projetos.
- [ ] Behavioral: top words, error rate, prefixes, peak hour (todos via Recharts ou listas).
- [ ] Commit `feat: StatsTab com heatmap interativo e charts`.

### Task 69: CostsTab

**Files:** Create `web/src/tabs/CostsTab.tsx`, `web/src/components/CostBars.tsx`

- [ ] Recharts `<LineChart>` 30d com `<Brush>` zoom.
- [ ] `<BarChart>` por projeto e por modelo.
- [ ] Card com cache savings + ratio.
- [ ] Threshold lines (warn/alert) horizontais.
- [ ] Commit `feat: CostsTab com time series brush`.

### Task 70: TimelineTab

**Files:** Create `web/src/tabs/TimelineTab.tsx`

- [ ] Date range picker (últimos N dias).
- [ ] Sessions agrupadas por dia, vertical timeline.
- [ ] Click numa session abre Detail.
- [ ] Commit `feat: TimelineTab com date range`.

### Task 71: ToolsTab

**Files:** Create `web/src/tabs/ToolsTab.tsx`

- [ ] Lista esquerda: tools globais com bar chart inline.
- [ ] Click numa tool: painel direito mostra top sessions com aquela tool (drill-down).
- [ ] Commit `feat: ToolsTab com drill-down clicável`.

### Task 72: CompareTab

**Files:** Create `web/src/tabs/CompareTab.tsx`

- [ ] Inputs: 2 session IDs (autocomplete da lista).
- [ ] Side-by-side: tools (bar charts mesma scale), custo, duração, msgs.
- [ ] Commit `feat: CompareTab side-by-side de 2 sessions`.

---

## Sub-milestone 3.e — Visualizações interativas

### Task 73: Heatmap interativo

- [ ] Hover: tooltip com `weekday HH-HH: N sessions`.
- [ ] Click bin: navega `#search?from=<ts>&to=<ts>` (filtro temporal).
- [ ] Commit `feat: heatmap clicável com filter por bin`.

### Task 74: Search com highlight visual

- [ ] Snippet do FTS já vem com `[match]` markers; converte pra `<mark>` JSX.
- [ ] No DetailPanel, scroll automático até o match.
- [ ] Commit `feat: search highlight visual com scroll-into-view`.

### Task 75: Time series brush

- [ ] CostsTab `<LineChart>` com `<Brush>`. Brush emit onChange com from/to.
- [ ] Title atualiza com range selecionado.
- [ ] Commit `feat: brush selector pra zoom em time series`.

### Task 76: Compare diff visual

- [ ] Tools side-by-side: 2 BarCharts com domínio Y igualado pra comparação justa.
- [ ] Diff de custos mostra delta % colorido (verde/vermelho).
- [ ] Commit `feat: compare visual com diff colorido`.

### Task 77: Live update via SSE

- [ ] App.tsx assina `/api/events`, em `reindex_done` invalida cache de tabs.
- [ ] Status bar mostra "🔵 last refresh: HH:MM (+N novos)" em tempo real.
- [ ] Commit `feat: live update via SSE em todas as tabs`.

---

## Sub-milestone 3.f — Polish

### Task 78: Export CSV / PDF

**Files:** Create `web/src/components/ExportButton.tsx`

- [ ] Button na CostsTab e RecentTab.
- [ ] CSV: client-side via `papaparse` (inline gen).
- [ ] PDF: `jsPDF` ou print-to-pdf via `window.print()` com CSS @media print.
- [ ] Commit `feat: export CSV e PDF nas tabs principais`.

### Task 79: go:embed + serve dist

**Files:** Create `embed.go`, modify `internal/server/server.go`

- [ ] `embed.go` raiz: `//go:embed all:web/dist` `var WebFS embed.FS`.
- [ ] Server serve estaticamente via `http.FS(fs.Sub(WebFS, "web/dist"))` em `/`.
- [ ] Caso fallback (SPA routing): se path não existe, servir `index.html`.
- [ ] Commit `feat: go embed do frontend bundle no binario`.

### Task 80: Build script

**Files:** Create `Makefile`

- [ ] Target `web`: `cd web && bun install && bun run build`.
- [ ] Target `build`: depende de `web` + `go build -o bin/claude-history .`.
- [ ] Commit `chore: makefile com target build (web + go)`.

### Task 81: README atualizado

- [ ] Adicionar seção Web UI: como rodar, screenshots, features.
- [ ] Roadmap: marca Fase 3 como done.
- [ ] Commit `docs: README com fase 3 e seção web ui`.

### Task 82: Smoke final

- [ ] `make build && bin/claude-history serve` abre browser, testa todas as tabs.
- [ ] `claude-history list` continua funcionando (no regression CLI).
- [ ] `claude-history tui` continua funcionando (no regression TUI).
- [ ] Push.
- [ ] Commit smoke (vazio ou docs minor).

---

## Self-Review

### Coverage spec

- ✅ HTTP server + SSE: 51-54
- ✅ REST endpoints: 55-60
- ✅ Frontend scaffold: 61-65
- ✅ 7 tabs: 66-72
- ✅ Visualizações interativas: 73-77
- ✅ Polish: 78-82

### Placeholders / consistency

- Todos endpoints REST mapeados pra handlers concretos
- `useSSE<T>` com type parameter consistente entre tabs
- `Session` type idêntico TS ↔ Go (atenção: campos `snake_case` no JSON, conversão no frontend ou config tags Go)

---

## Execution Handoff

Inline execution em sub-milestones. Smoke build entre cada um.

Pode começar.

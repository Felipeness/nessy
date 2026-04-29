---
title: Web UI local — Fase 3
status: approved
date: 2026-04-29
phase: 3
parent: 2026-04-28-tui-base-design
---

# Web UI local — Design

## Contexto

Fases 1-2.1 entregaram CLI + TUI rica sobre indexer Go/SQLite/FTS5/stats. A TUI é ótima pra fluxo "retomar conversa", mas tem limites visuais (gráficos só ASCII, sem interatividade mouse, sem zoom/pan, sem highlight visual em search results).

Fase 3 entrega **Web UI local** que reusa 100% do backend existente, expondo via REST + SSE, com frontend React/Vite consumindo. Mantém o ethos "single binary" via `go:embed`.

## Goals

1. Visualizações interativas que TUI não permite (heatmap clicável, time-series com zoom).
2. Search com highlight visual de matches no texto das mensagens.
3. Side-by-side comparação de 2 sessions.
4. Export CSV/PDF.
5. Acesso de outras máquinas da LAN (opcional, via flag).
6. Live updates real-time via SSE (push quando reindex roda).
7. Single binary continua — frontend bundled via `go:embed`.

## Non-goals

- Multi-user / auth (localhost only por default)
- Editar sessions (read-only por design — herdado das fases anteriores)
- Replace TUI (coexistem; user escolhe qual prefere)
- Cloud / sync (continua local)
- AI summaries (Fase 5)

## Tech stack

| Camada | Tech |
|---|---|
| Backend | Go 1.26 (mesmo binário) — `net/http` stdlib, sem framework |
| Frontend build | Vite 7 |
| Frontend lang | React 19 + TypeScript |
| Styling | Tailwind v4 (alinha com `portfolio-v2-react`) |
| Charts | Recharts (idiomatic React, leve) |
| Tables | TanStack Table v8 (sortable/filterable) |
| Live updates | Server-Sent Events (`text/event-stream`) |
| Distribuição | `go:embed` empacota `web/dist/` no binário |

Justificativa SSE vs WebSocket: pra updates server→client only, SSE é simpler (HTTP normal, auto-reconnect), suficiente.

## API REST

| Endpoint | Método | Retorno |
|---|---|---|
| `/api/sessions` | GET | array de Session JSON (todos os campos do struct) |
| `/api/sessions/:id` | GET | session detalhada com tools breakdown e custo |
| `/api/sessions/:id/messages?n=N` | GET | últimas N user messages |
| `/api/stats` | GET | aggregate global (heatmap, modelos, projeção, long-tail, trends, cache savings) |
| `/api/stats/behavioral` | GET | top words, error rate, prefixes, peak hour |
| `/api/costs` | GET | breakdown por dia (30d) / projeto / modelo |
| `/api/timeline?from=&to=` | GET | sessions agrupadas por dia |
| `/api/tools` | GET | ranking + drill-down (por tool: top sessions) |
| `/api/search?q=&mode=metadata\|fts` | GET | results com snippets |
| `/api/refresh` | POST | dispara reindex, retorna ReindexStats |
| `/api/export/:id` | GET | session JSON streamed |
| `/api/export.csv?type=sessions\|costs` | GET | dump CSV |
| `/api/events` | GET (SSE) | push de `reindex_done` quando novo state |

CORS: liberado pra `localhost:*` only.

## SSE — protocolo

Servidor mantém set de clients conectados. Quando `Reindex()` termina:

```
event: reindex_done
data: {"new":2,"updated":1,"removed":0,"total":47}
```

Frontend ouve e refetch. Auto-reconnect built-in do `EventSource`.

## Frontend — estrutura

```
web/
├── package.json
├── vite.config.ts
├── tailwind.config.ts
├── postcss.config.js
├── index.html
├── src/
│   ├── main.tsx                  # entry
│   ├── App.tsx                   # router de tabs (hash-based)
│   ├── api.ts                    # fetch helpers tipados
│   ├── sse.ts                    # EventSource wrapper
│   ├── types.ts                  # tipos compartilhados (Session, Stats, etc.)
│   ├── components/
│   │   ├── Tabs.tsx
│   │   ├── SessionRow.tsx
│   │   ├── DetailPanel.tsx
│   │   ├── ModelBadge.tsx
│   │   ├── Heatmap.tsx
│   │   ├── Sparkline.tsx
│   │   ├── CostBars.tsx
│   │   ├── SearchResults.tsx
│   │   └── ExportButton.tsx
│   ├── tabs/
│   │   ├── RecentTab.tsx
│   │   ├── SearchTab.tsx
│   │   ├── StatsTab.tsx
│   │   ├── CostsTab.tsx
│   │   ├── TimelineTab.tsx
│   │   ├── ToolsTab.tsx
│   │   └── CompareTab.tsx        # NOVO — side-by-side de 2 sessions
│   └── styles.css
└── dist/                         # build output, embeddado via go:embed
```

## Routing

SPA com URL hash:

- `#recent` (default)
- `#search?q=...`
- `#stats`
- `#costs`
- `#timeline`
- `#tools`
- `#compare?a=ID1&b=ID2` (Fase 3 plus — adiciona depois das outras tabs)

URL bookmarkable. Browser back/forward funciona.

## Layout

Mobile: tabs viram bottom nav (responsivo via Tailwind).
Desktop: tabs no topo + sidebar opcional pra navegação rápida (largura ≥ 1024px).

Detail panel:
- Desktop ≥ 1280px: lista esquerda 40% + detail direita 60%
- Desktop < 1280px: lista cheia, click numa session abre modal cheio

## Visualizações interativas (highlights)

### Heatmap em Stats
Recharts não tem heatmap nativo — vou usar grid CSS manual com cores (bg via Tailwind dynamic). Hover mostra tooltip com `weekday HH-HH: N sessions`. Click filtra Recent/Search por aquele bin de tempo.

### Time series em Costs
`<LineChart>` com `<Brush>` pra recortar período. Tooltip mostra session count + total USD + projects ativos no dia.

### Search highlight
Search results mostra snippet com match wrapping (`<mark>` styled via Tailwind). Click no result abre modal com a session message inteira, scroll até o match com `scrollIntoView`.

### Compare tab
Seleciona 2 sessions (via input ou drag-drop da Recent). Mostra:
- Diff visual de tools (mesma scale lado a lado)
- Comparação de custo total
- Comparação de duração
- Heatmap de quando cada uma rodou

## Distribuição

```bash
go install ./...                  # builds binary
# Mas pra dev:
cd web && bun dev                 # vite dev em :5173 com proxy pra :5555
# Pra prod:
cd web && bun run build           # gera dist/
go build .                        # bundla via go:embed
```

`go:embed`:

```go
//go:embed all:web/dist
var webFS embed.FS
```

Subcomando `serve`:

```bash
claude-history serve              # default localhost:5555 + auto-open
claude-history serve --port 8080
claude-history serve --listen 0.0.0.0:5555  # LAN exposure (com warning)
claude-history serve --no-open    # não abre browser
```

## Auto-open

Após `http.ListenAndServe` start, dispara `open <URL>` (macOS) / `xdg-open` (linux) async.
Detect se browser deu erro: ignora silencioso (user pode abrir manual).

## CORS / segurança

- Default bind: `127.0.0.1:5555` (não acessível externamente)
- Flag `--listen 0.0.0.0:PORT` requer confirm interativo: "Expose on LAN? Anyone on your network can read your sessions. [y/N]"
- Sem auth nesta fase. Binding pra LAN é responsabilidade do user.

## Critérios de aceitação

- [ ] `claude-history serve` sobe HTTP em :5555 + auto-open Chrome
- [ ] Tab Recent lista sessions com filtro, ordenação, agrupamento
- [ ] Tab Search com FTS5 + highlight visual
- [ ] Tab Stats com heatmap interativo, charts Recharts
- [ ] Tab Costs com time series + brush
- [ ] Tab Timeline cronológica com filtros de data
- [ ] Tab Tools com drill-down clicável
- [ ] Tab Compare side-by-side de 2 sessions
- [ ] SSE atualiza UI quando refresh acontece
- [ ] Export CSV/PDF funciona
- [ ] `go:embed` empacota frontend no binário
- [ ] CLI/TUI continuam funcionando idênticos (zero regressão)

## Riscos & edge cases

| Risco | Mitigação |
|---|---|
| `go:embed` não pega `dist/` se Vite não rodou | Build script `make build` que roda `bun run build` antes de `go build` |
| SSE conexão drop em proxy/network | EventSource tem auto-reconnect built-in; backend toleranterá clients sumindo |
| Sessões com 1000+ msgs lag o detail | Lazy-load: detail mostra metadata + 50 últimas msgs, scroll infinito |
| Conflito entre TUI rodando + Web | Ambos usam SQLite WAL — concorrência OK; só não rodam reindex simultâneo (lock simples no DB) |
| Bundle size do React+deps | Vite tree-shake + minify; alvo <500KB gzipped |
| Mobile touch (heatmap pequeno) | Responsivo: heatmap em mobile vira lista vertical em vez de grid 2D |

## Out-of-scope (futuras fases)

- Editar sessions (read-only por design)
- Auth/multi-user (Fase 6+ se relevante)
- Sync entre máquinas (não pretendido)
- LLM-powered features (Fase 5)

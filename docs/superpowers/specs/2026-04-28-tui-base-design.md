---
title: TUI base para claude-history (Fase 2)
status: approved
date: 2026-04-28
author: Felipe (brainstormed com Claude)
phase: 2
---

# TUI base do `claude-history` — Design

## Contexto

A **Fase 1** entregou `claude-history list` + `claude-history fzf` — um indexer Go que parseia `~/.claude/projects/**/*.jsonl`, ignora subagents, extrai metadados (sessionId, cwd, branch, msg counts, tools, primeira/última msg, duração) e expõe via CLI/fzf.

A **Fase 2** entrega uma TUI Bubble Tea como segundo frontend sobre o mesmo indexer, evoluindo o indexer pra suportar:

- Cache SQLite com FTS5 (até hoje, in-memory)
- Métricas determinísticas extras: tokens (input/output/cache), custo USD/BRL, modelo
- Layout adaptativo, busca híbrida, refresh manual

Esta spec define o **escopo, comportamento e estrutura** da Fase 2. Análises comportamentais, código, e AI-powered profiling ficam em fases posteriores (4-6) — ver `## Decomposição em fases`.

## Decomposição em fases

| Fase  | Sub-projeto                                                          | Status       |
| ----- | -------------------------------------------------------------------- | ------------ |
| 1     | Indexer Go + CLI `list/show/fzf`                                     | ✅ Entregue  |
| **2** | **TUI base** com tabs Search/Recent/Stats + métricas determinísticas | ⏳ Esta spec |
| 3     | Web UI + dashboard temporal                                          | Backlog      |
| 4     | Behavioral analytics via heurísticas (regex/stats)                   | Backlog      |
| 5     | AI-powered profiling (LLM local + embeddings)                        | Backlog      |
| 6     | Code mining (extração + análise de snippets gerados)                 | Backlog      |

Cada fase = 1 spec + 1 plan + 1 ciclo de implementação. Esta spec cobre **somente a Fase 2**.

## Goals

1. Permitir ao Felipe encontrar qualquer conversa rapidamente (busca por metadata ou full-text).
2. Mostrar contexto de retomada — preview rico, indicadores de atividade.
3. Visibilidade financeira — saber quanto cada session/projeto custou em tokens.
4. UX híbrida (lista + detail simultâneos quando o terminal permite, modal quando não).
5. Sem regressão — `claude-history list/fzf` da Fase 1 continuam funcionando idênticos.

## Non-goals

- Análise comportamental (estilo de mensagem, personalidade) → Fase 4
- Detecção semântica de erros/acertos → Fase 4 (regex) e 5 (LLM)
- Code style mining → Fase 6
- AI summaries via Ollama → Fase 5
- Live filesystem watcher (`fsnotify`) — refresh manual com `r` é suficiente nesta fase
- Auto-detect de novo modelo Anthropic e update do pricing
- Edit/delete de sessions (read-only por design)
- Comparação entre sessions (diff/branching)
- Export PDF/CSV (vai pra Web na Fase 3)

## Tech stack

| Camada         | Tech                                                                                   |
| -------------- | -------------------------------------------------------------------------------------- |
| Linguagem      | Go 1.26 (mantém consistência com Fase 1)                                               |
| TUI framework  | [Bubble Tea](https://github.com/charmbracelet/bubbletea)                               |
| Styling        | [Lipgloss](https://github.com/charmbracelet/lipgloss)                                  |
| Componentes    | [Bubbles](https://github.com/charmbracelet/bubbles) (table, textinput, viewport, help) |
| DB             | SQLite via `modernc.org/sqlite` (CGO-free) com FTS5 ativado                            |
| Pricing config | TOML via `github.com/BurntSushi/toml`                                                  |

Justificativa Bubble Tea: ecossistema Charm é o mais maduro pra TUI Go em 2026, suporta layout adaptativo e cobre todos os componentes necessários. `modernc.org/sqlite` evita CGO e mantém o binário portátil (single-binary cross-compile).

## UI/UX

### Layout adaptativo

**≥120 colunas (multi-pane)**:

```
┌─[Search│Recent│Stats]────────────────────────────────────────────────┐
│ Lista (40-50% width)        │ Detail panel (50-60% width)            │
│ ─────────────────           │ ──────────────────────                 │
│ • 2026-04-28 16:34 …        │ Session: 6df22c8d                      │
│ ▶ 2026-04-28 11:18 …        │ Início: 2026-04-28 16:34:34            │
│ • 2026-04-28 10:57 …        │ Duração: 41m 36s                       │
│ • 2026-04-28 10:53 …        │ Modelo: claude-sonnet-4-6              │
│                             │ Tokens: in 1.2M · out 98K · cache 90%  │
│                             │ Custo:  $4.32 USD (R$ 22,46)           │
│                             │                                        │
│                             │ Tools (top 5):                         │
│                             │   Bash    73                           │
│                             │   Edit     3                           │
│                             │   ...                                  │
└─────────────────────────────┴────────────────────────────────────────┘
status: 4 sessions │ $87.43 mês │ last refresh: 16:34 │ r refresh ? help
```

**<120 colunas (full-screen)**:

- Ocupa tela toda com a lista
- `Enter` abre modal Detail full-screen (mesmo conteúdo do painel direito)
- `Esc` volta pra lista
- Em tab Stats: `s` toggle entre **global aggregate** e **session local**

### Tabs

3 tabs no topo, Tab/Shift+Tab pra trocar:

1. **Search** — input box + lista filtrada
2. **Recent** — lista cronológica (default) ou agrupada por projeto (`g` toggle)
3. **Stats** — multi-pane (global + local) ou toggle (`s`) em tela pequena

### Keybinds (globais)

```
Tab / Shift-Tab    trocar tab (Search/Recent/Stats)
j / k              navegar lista (down/up)
g g                ir pro topo
G                  ir pro fim
/ ou f             abrir search box (modo metadata)
:body <query>      switch pra full-text search (FTS5)
Enter              retomar session (cd no cwd + claude --resume)
Ctrl-O             abrir pasta da session no Finder (open <cwd>)
g                  toggle agrupamento na tab Recent (tempo ↔ projeto)
s                  toggle em Stats single-pane (global ↔ local)
d                  toggle filtro temporal em Recent (7-day ↔ all-time)
r                  refresh (re-index)
?                  help (overlay com lista de keybinds)
q ou Esc           sair
```

### Search tab

Default: **metadata** (cwd, branch, primeira/última msg, data) — busca substring case-insensitive sobre cache em memória, **alvo <50ms**.

Full-text: ativado via prefixo `:body <query>` ou `Ctrl-F`. Query vai pro SQLite FTS5 com BM25 ranking. Mostra snippet com highlight do match.

Resultado: lista padrão com indicador no header (`mode: metadata` ou `mode: full-text`).

### Recent tab

Default: cronológico descending por última atividade.

Headers de tempo (visual separator):

```
─── Today ──────────────
2026-04-28 16:34  ~/Desktop/Projects/claude-history    250 msg  $4.32   🟢 ATIVA
...
─── Yesterday ──────────
...
─── This week ──────────
...
─── Older ──────────────
...
```

`g` toggle pra agrupar por projeto:

```
~/Desktop/Projects/claude-history (3 sessions, $36.20)
  16:34  250 msg  $4.32   🟢
  10:53    7 msg  $0.12
  10:18    3 msg  $0.05

~/obsidian-vault (5 sessions, $12.45)
  ...
```

**Indicadores de atividade** (calculados do `EndTime` da session):

- 🟢 ATIVA → última msg < 5min
- 🟡 Pausada → 5min ≤ última msg < 1h
- ⚪ Antiga → ≥ 1h

`d` toggle entre "últimos 7 dias" e "all time".

### Stats tab

**Multi-pane (≥120 cols)**:

- **Esquerda — Global aggregate**:
  - Sessions count, msgs total, tempo total, custo mês
  - Top 5 projetos por custo (com %)
  - Sparkline ASCII dos últimos 7 dias (sessions/dia)
  - Top tools globais (count e %)

- **Direita — Session local** (da session selecionada na lista):
  - Mesmo conteúdo do detail panel da tab Recent
  - Tokens detalhados (input/output/cache breakdown)
  - Custo USD + BRL (se configurado)
  - Tools per-session

**Single (<120 cols)**: `s` toggle entre global e local.

## Cost calculation

### Pricing snapshot

Arquivo `~/.claude-history/pricing.toml`:

```toml
default_currency = "USD"
brl_rate = 5.20  # opcional; quando setado, mostra dual display

[[models]]
name = "claude-sonnet-4-6"
input_per_mtok = 3.00
output_per_mtok = 15.00
cache_creation_per_mtok = 3.75
cache_read_per_mtok = 0.30

[[models]]
name = "claude-opus-4-7"
input_per_mtok = 15.00
output_per_mtok = 75.00
cache_creation_per_mtok = 18.75
cache_read_per_mtok = 1.50

# adicionar mais modelos conforme necessário
```

### Cálculo

Por sessão, soma de cada `assistant` message no JSONL:

```
input_tokens         = sum(message.usage.input_tokens)
output_tokens        = sum(message.usage.output_tokens)
cache_creation_input = sum(message.usage.cache_creation_input_tokens)
cache_read_input     = sum(message.usage.cache_read_input_tokens)

cost_usd = (
  input_tokens         * model.input_per_mtok +
  output_tokens        * model.output_per_mtok +
  cache_creation_input * model.cache_creation_per_mtok +
  cache_read_input     * model.cache_read_per_mtok
) / 1_000_000

cost_brl = cost_usd * brl_rate  # se brl_rate setado
```

### Display

- USD only: `$4.32 USD`
- Dual: `$4.32 USD (~R$ 22,46)`
- Modelo não mapeado: `?` no campo de custo + warning no status bar

## Storage

```
~/.claude-history/
├── index.db        SQLite (modernc.org/sqlite)
├── pricing.toml    snapshot de pricing por modelo
└── config.toml     prefs do user (opcional)
```

### Schema SQLite

```sql
CREATE TABLE sessions (
  session_id TEXT PRIMARY KEY,
  project_dir TEXT NOT NULL,
  jsonl_path TEXT NOT NULL UNIQUE,
  jsonl_mtime INTEGER NOT NULL,    -- unix epoch ns; usado pra invalidação de cache
  start_time INTEGER NOT NULL,     -- unix epoch ns
  end_time INTEGER NOT NULL,
  message_count INTEGER NOT NULL,
  user_messages INTEGER NOT NULL,
  assistant_messages INTEGER NOT NULL,
  first_user_msg TEXT,
  last_user_msg TEXT,
  git_branch TEXT,
  claude_version TEXT,
  model TEXT,                      -- modelo último observado
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_sessions_start ON sessions(start_time DESC);
CREATE INDEX idx_sessions_project ON sessions(project_dir);

CREATE TABLE tool_uses (
  session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
  tool_name TEXT NOT NULL,
  count INTEGER NOT NULL,
  PRIMARY KEY (session_id, tool_name)
);

-- FTS5 virtual table pra full-text search
CREATE VIRTUAL TABLE messages_fts USING fts5(
  session_id UNINDEXED,
  role,        -- 'user' ou 'assistant'
  content,
  tokenize = 'porter unicode61'
);

CREATE TABLE last_index_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
-- ex: ('schema_version', '1'), ('last_full_scan_at', '...')
```

### Reindex strategy

1. Ao launch, scanner walk em `~/.claude/projects/**/*.jsonl`
2. Pra cada arquivo, compara `mtime` com `sessions.jsonl_mtime`
3. Se mudou OU não existe → re-parseia + UPDATE/INSERT
4. Sessions que sumiram do disco → DELETE FROM sessions (cascata pra tool_uses; FTS5 limpo separado)
5. Status bar mostra `last refresh: HH:MM (+N new, M updated)`

Primeiro launch: full index (~2-5s pra 100 sessions). Subsequentes: ~50ms (mostly mtime checks).

## Refresh

Sem auto-refresh (decidido — KISS). `r` re-roda scanner + re-render. Status bar reflete.

## Estrutura do código

```
claude-history/
├── main.go                       # router de subcomandos (existente; adiciona "tui")
├── go.mod
├── go.sum
├── internal/
│   ├── parser/
│   │   └── jsonl.go              # existente; adicionar parsing de usage tokens
│   ├── index/
│   │   ├── sqlite.go             # NOVO: open/migrate/upsert/query
│   │   └── reindex.go            # NOVO: scanner mtime-based
│   ├── pricing/
│   │   └── pricing.go            # NOVO: carrega pricing.toml + calcula custo
│   └── model/
│       └── session.go            # NOVO: structs compartilhadas (movidas de parser)
└── tui/
    ├── app.go                    # NOVO: Bubble Tea root model, layout adaptativo
    ├── search.go                 # NOVO: tab Search
    ├── recent.go                 # NOVO: tab Recent
    ├── stats.go                  # NOVO: tab Stats
    ├── detail.go                 # NOVO: detail panel (compartilhado por tabs)
    └── style.go                  # NOVO: lipgloss styles centralizados
```

## Dependências novas

```
github.com/charmbracelet/bubbletea
github.com/charmbracelet/lipgloss
github.com/charmbracelet/bubbles
modernc.org/sqlite
github.com/BurntSushi/toml
```

Todas têm binários arm64 nativos e zero CGO (com `modernc.org/sqlite` em vez de `mattn/go-sqlite3`). Isso preserva cross-compile e mantém o binário simples.

## Riscos & edge cases

| Risco                                                | Probabilidade                       | Mitigação                                                                                                |
| ---------------------------------------------------- | ----------------------------------- | -------------------------------------------------------------------------------------------------------- |
| JSONL com linha JSON inválida                        | Alta (já visto em produção)         | `Skip + continue` — Fase 1 já faz isso                                                                   |
| `cwd` apontando pra pasta deletada                   | Média                               | Mostra session greyed-out + erro ao Enter                                                                |
| Modelo novo (ex: sonnet-4-7) sem entry no pricing    | Alta (Anthropic libera modelo novo) | Custo `?` + warning não-bloqueante; usuário edita TOML manual                                            |
| Terminal redimensionado em runtime                   | Alta                                | Bubble Tea trata nativamente (`tea.WindowSizeMsg`); refluí o layout                                      |
| 1000+ sessions (escala futura)                       | Média (em 6 meses)                  | SQLite + FTS5 cobrem; UI vira viewport scrollable                                                        |
| FTS5 não disponível na build do `modernc.org/sqlite` | Baixa                               | Fallback pra `LIKE %query%` (mais lento mas funciona); detectado em runtime via `PRAGMA compile_options` |
| Refresh durante load → race                          | Média                               | Usar `tea.Cmd` pra reindex async; lock global no DB                                                      |
| BRL rate desatualizado                               | Alta                                | Doc explícita que é manual; não tentar puxar de API externa nesta fase                                   |
| Subagents jsonl (do task tool)                       | N/A — já filtrados na Fase 1        | Mantém filtro `subagents/`                                                                               |

## Critérios de aceitação

- [ ] `claude-history tui` abre TUI funcional
- [ ] Tabs Search/Recent/Stats navegáveis com Tab
- [ ] Layout muda entre multi-pane e single em runtime ao redimensionar
- [ ] Search metadata <50ms em 100+ sessions
- [ ] Search full-text via `:body` retorna snippets com highlight
- [ ] Recent mostra indicadores 🟢🟡⚪ corretos (testado em fixtures)
- [ ] Stats global mostra sparkline 7 dias e top projetos
- [ ] Stats local mostra tokens detalhados + custo USD+BRL
- [ ] `r` re-indexa e atualiza UI
- [ ] `Enter` retoma session no cwd correto via `claude --resume`
- [ ] `Ctrl-O` abre pasta no Finder
- [ ] CLI `list/show/fzf` da Fase 1 continua funcionando idêntico (sem regressão)
- [ ] Cache SQLite criado em `~/.claude-history/index.db` no primeiro run
- [ ] Pricing TOML é carregado de `~/.claude-history/pricing.toml`; se ausente, criado com defaults

## Métricas de sucesso (post-launch)

- Tempo médio pra encontrar uma session específica < 10s (vs ~30s no fzf da Fase 1 quando esquecimento o tópico)
- Custo total mensal visível em <2s após launch
- Zero crashes em 1 semana de uso real

## Referências

- [Spec da Fase 1 (implícita)](../../README.md)
- [Bubble Tea tutorial](https://github.com/charmbracelet/bubbletea/tree/master/tutorials)
- [Anthropic pricing reference (snapshot 2026-04)](https://www.anthropic.com/pricing)
- [SQLite FTS5 docs](https://sqlite.org/fts5.html)

# claude-history

Indexa, busca, analisa e dá feedback sobre **todas** as suas conversas do Claude Code num só lugar — independente da pasta onde você abriu cada uma.

Lê os JSONLs que o Claude Code grava em `~/.claude/projects/<encoded-cwd>/*.jsonl`, indexa em SQLite com FTS5, e expõe **3 frontends** sobre o mesmo backend:

- **CLI**: `claude-history list/show/fzf`
- **TUI**: `claude-history tui` — Bubble Tea com 6 tabs
- **Web UI + Studio**: `claude-history serve` — Vite/React com tabs de Stats, Costs, Behavior, AI insights e **Statusline Studio** (editor visual do statusline do Claude Code)
- **Statusline**: `claude-history statusline-render` — binário que se pluga no `statusLine` do Claude Code e mostra cost/context/burn-rate/cluster live

## Instalação

### 1. Build

```bash
git clone git@github.com:Felipeness/claude-history ~/Desktop/Projects/claude-history
cd ~/Desktop/Projects/claude-history
cd web && bun install && bun run build && cd ..
go build -o ~/.local/bin/claude-history .
```

Garante que `~/.local/bin` está no seu PATH. Bun é necessário só pra buildar o frontend uma vez (depois fica embedded no binário Go).

### 2. Subir o daemon

Necessário pra Web UI, statusline live (cost/p90/burn-rate) e SSE updates:

```bash
claude-history serve --no-open    # http://localhost:5555
```

Roda em foreground. Pra deixar sempre ativo no boot via launchd, veja [Daemon persistente](#daemon-persistente) abaixo.

### 3. Plugar o statusline no Claude Code (opcional mas recomendado)

```bash
claude-history statusline-install --preset compact
# Se você já tem outro statusline (ccstatusline, etc) instalado:
claude-history statusline-install --preset compact --force
```

Isso:
- Cria backup automático: `~/.claude/settings.json.bak.YYYYMMDD-HHMMSS`
- Faz merge atômico no `~/.claude/settings.json` — preserva `permissions`, `hooks`, e qualquer outra key que já exista
- Escreve `~/.claude-history/statusline.toml` com o preset escolhido
- Sem `--force`, recusa se já existe `statusLine` apontando pra outro tool

Depois **reinicia o Claude Code** — o `statusLine` só carrega no boot. Pronto, vai aparecer assim no terminal:

```
~/Desktop/Projects/claude-history │ main↑11 │ Opus 4.7 │ ▓▓░░░░ 42% │ $0.32 │ 850 t/m
```

### 4. Customizar o statusline visualmente

Abra `http://localhost:5555/#studio` no browser — drag-drop dos components, escolha de tema (graphite/nord/dracula/sakura/mono), 3 styles (plain/powerline/capsule), thresholds (warn/critical) por component, mock data pra simular cenários. Salvar persiste em `~/.claude-history/statusline.toml`.

Ou edite o TOML direto:

```toml
theme = "graphite"
style = "plain"

[[lines]]
components = ["cwd", "git", "model", "context_pct", "cost_session", "burn_rate"]
separator = " │ "

[components.context_pct]
warn_at = 50
critical_at = 80

[components.cost_session]
warn_at = 0.8     # × p90 → amarelo
critical_at = 1.2 # × p90 → vermelho
```

Reinicia o Claude Code pra aplicar (não tem hot-reload do config — Claude Code lê 1x).

### Desinstalar o statusline

```bash
claude-history statusline-install --uninstall
# OU restaurar do backup:
cp ~/.claude/settings.json.bak.YYYYMMDD-HHMMSS ~/.claude/settings.json
```

### 5. (opcional) AI features — Ollama

```bash
ollama pull qwen2.5:7b
ollama pull nomic-embed-text
ollama serve
```

Sem isso a tab AI fica vazia, mas todo o resto (statusline, TUI, costs) funciona normal.

### Daemon persistente

Pra `claude-history serve` rodar sempre no login (assim o statusline sempre tem dados), crie `~/Library/LaunchAgents/com.claude-history.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>com.claude-history</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/SEU_USER/.local/bin/claude-history</string>
        <string>serve</string>
        <string>--no-open</string>
    </array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>StandardOutPath</key><string>/tmp/claude-history.log</string>
    <key>StandardErrorPath</key><string>/tmp/claude-history.err</string>
</dict>
</plist>
```

Ativa: `launchctl load ~/Library/LaunchAgents/com.claude-history.plist`

## Visão geral das fases

| Fase | Status | Resumo |
|---|---|---|
| 1 — Indexer + CLI | ✅ | parser JSONL, SQLite, `list/show/fzf` |
| 2 — TUI Bubble Tea | ✅ | 9 tabs, layout adaptativo, detail panel rico |
| 3 — Web UI (Vite/React) | ✅ | mesmo backend via REST + SSE; tabs Stats/Costs/Timeline/Tools/Behavior |
| 4 — Behavioral avançado | ✅ | n-grams, bigrams, co-ocorrência PMI, scatter time×cost, style stats |
| 5 — AI profiling (Ollama) | ✅ | summaries, embeddings, K-means clustering, similarity |
| 5.1 — AI Insights advisor | ✅ | detecta padrões, repetições, anti-patterns, dicas de economia de token |
| 5.2 — AI Knowledge (segundo cérebro) | ✅ | tabela `session_knowledge` com problem/solution/decisions/learnings/patterns/tech/open_questions extraídos por LLM |
| 5.3 — Knowledge agregado cross-session | ✅ | top patterns/decisões/problemas recorrentes/tech/open_questions agregados |
| 6 — Statusline + Studio | ✅ | binário live + editor visual web com drag-drop, themes, mock data — também spin-off público em [`claude-statusline`](https://github.com/Felipeness/claude-statusline) |
| 7 — Ness IA (chat com RAG) | ✅ | tab "🧠 Ness" no Web/TUI: pergunta direto pro segundo cérebro, RAG sobre summaries+knowledge, fontes citadas em `[session_id]`, fallback `[geral]` quando histórico não cobre |
| 7a — CLI extension | ✅ | 8 subcomandos query (`similar/search/ask/insights/knowledge/aggregated/project/standup`) com `--json` pra Claude e scripts consumirem via Bash |
| 8 — MCP server | 🟡 planned | wrapper fino mapeando tools MCP → calls de CLI |
| 9 — Sistema (launchd + menu bar + notif) | 🟡 planned | daemon persistente, menu bar Mac, notificações p90/burn |
| 10 — TUI session tree | 🟡 planned | detectar continuações por cwd+branch+gap, view tree |

## CLI

### Comandos básicos

```bash
claude-history list                       # tabela
claude-history list --json | jq '.[]'     # JSON pra script
claude-history list --tsv                 # TSV
claude-history show 6df22c8d              # detalhes (aceita ID curto)
claude-history fzf                        # fzf interativo, Enter retoma
claude-history tui                        # TUI Bubble Tea (9 tabs)
claude-history serve                      # Web UI em http://localhost:5555
claude-history statusline-install         # instala statusline no Claude Code
claude-history statusline-preview --all   # preview no terminal (5 themes × 3 styles)
```

### Query (Fase 7a) — pra scripts e Claude consumirem via Bash

Todos retornam human-readable por default, JSON com `--json`. Lêem DB direto, **sem precisar daemon**. Falhas graceful (Ollama down → JSON com `error` ao invés de quebrar).

```bash
# Similar — top sessions parecidas via embedding+cosine
claude-history similar "auth bug com middleware" --n 5 --json

# Search — hybrid (default) / body / meta / sim
claude-history search docker --mode body --json
claude-history search "react auth" --all     # sem dedup, todos hits

# Ask — chat Ness IA na CLI (RAG completo, fontes citadas)
claude-history ask "como uso docker no mac?"
# → "Você usa Docker via Colima... [6df22c8d]"
# → fontes listadas com session_id + similarity %

# Insights — advisor results (token_waste, anti_pattern, etc.)
claude-history insights --type token_waste --json

# Knowledge de 1 session (problem/solution/decisions/learnings/patterns/tech/open)
claude-history knowledge 6df22c8d
claude-history knowledge 6df22c8d --json | jq '.solution'

# Aggregated cross-session — top patterns, decisões, problemas recorrentes, em aberto
claude-history aggregated

# Project stats — p90, tech, top tools, ticket pattern
claude-history project ~/Desktop/Projects/claude-history --json

# Standup markdown pra Slack/daily
claude-history standup --since 7d                    # editorial (default — usa knowledge)
claude-history standup --since 7d --format timeline  # cronológico por dia/hora
claude-history standup --since 14d --format project  # agrupado por projeto + cost
```

### Use cases

**Claude consultando seu próprio histórico mid-session** (o killer use case):
```bash
# No meio de outra session do Claude Code, ele pode rodar via Bash:
claude-history ask "como resolvi auth bug 3 meses atrás?" --json
# → contexto rico citando session_ids reais
```

**Standup automatizado**:
```bash
claude-history standup --since 7d | pbcopy
# cola no Slack/daily
```

**CI/CD insight check**:
```bash
# script verifica se há open questions críticas em aberto
COUNT=$(claude-history aggregated --json | jq '.open_questions | length')
[ "$COUNT" -gt 5 ] && echo "⚠ $COUNT pendências"
```

**Drill-down num projeto antes de decidir refactor**:
```bash
claude-history project ~/projects/my-app --json | jq '.tech_stack, .top_tools'
```

## TUI

### 6 tabs

| Tab | Pra que serve |
|---|---|
| **Search** | Busca metadata default · `:body <q>` switcha pra full-text via FTS5 |
| **Recent** | Lista cronológica densa: badge modelo, duração, tokens, custo, preview · `g` agrupa por projeto |
| **Stats** | Heatmap 12 sem · distribuição modelos · projeção custo do mês · long-tail · top palavras · sinais de retrabalho · prefixos · horário de pico |
| **Costs** | Custo/dia (30d) · custo por projeto · custo por modelo · cache savings global |
| **Timeline** | Sessions agrupadas por dia |
| **Tools** | Top 25 tools globais + drill-down das sessions que mais usaram a tool selecionada |

### Detail panel (multi-pane ≥ 120 colunas)

- Header: id, pasta, branch, modelo, duração
- Custo total + breakdown (input/output/cache create/cache read) com bars
- Tokens detalhados + cache hit gauge
- Mini-stats: msgs/min, tokens/msg, ratio user:assistant
- Bar chart de tools (cores por categoria)
- Sparkline 14d do projeto
- Comparação com mediana do projeto (setas)
- Trecho das últimas 3 user messages

### Keybinds (TUI)

`Tab`/`Shift+Tab` próxima/anterior tab · `1-6` pula direto · `j/k` ou setas naveg · `Enter` retoma session · `/` ou `f` search · `:body <q>` FTS5 · `g` agrupa Recent · `r` reindex · `Ctrl+E` export JSON · `Ctrl+O` abre pasta · `?` help · `q` sair

## Web UI

```bash
claude-history serve --no-open
# abre em http://localhost:5555
```

**Tabs**:

- **Recent / Search / Stats / Costs / Timeline / Tools / Behavior / Compare** — espelhos web dos da TUI, com gráficos Recharts
- **AI** — summaries por session, clusters K-means, busca por similaridade, insights advisor, profile pessoal gerado por LLM
- **Studio** — editor visual do statusline (descrito abaixo)

Live updates via SSE (Server-Sent Events) — quando você reindexar pelo botão Refresh, todas as tabs abertas atualizam.

## AI (Fase 5 + 5.1)

Requer [Ollama](https://ollama.com) rodando local com 2 modelos:

```bash
ollama pull qwen2.5:7b           # geração (summaries, insights, profile)
ollama pull nomic-embed-text     # embeddings (clusters, similarity)
ollama serve
```

Sem internet — tudo roda local. O claude-history gera (sob demanda):

- **Summaries** — 1 parágrafo por session, cacheado por mtime do JSONL
- **Clusters** — K-means sobre embeddings, com label gerado por LLM (ex: "auth-refactor", "config-tweaks")
- **Similar** — top-N sessions com cosine similarity à atual
- **Insights** — advisor que detecta `repeated_task`, `chronic_problem`, `script_opportunity`, `token_waste`, `performance_hint`, `anti_pattern`, `personal_pattern`. Cada um com evidência concreta (session ids) e ação sugerida.
- **Profile** — perfil pessoal em pt-BR gerado a partir de summaries + tech detectada (regex sobre msgs) + insights. Honra `~/.claude-history/about.txt` como ground truth pra identidade.

## Statusline (Fase 6)

Binário `claude-history statusline-render` que o Claude Code chama via stdin a cada turno. Recebe um JSON com `cwd`, `model`, `context_window`, `cost`, `rate_limits`, `worktree`, etc., consulta o daemon claude-history pra dados históricos (p90, daily, project, cluster) com timeout 80ms, e devolve uma linha ANSI colorida.

### Setup

```bash
claude-history serve --no-open    # daemon roda em :5555 com cache 5s
claude-history statusline-install --preset compact
# reinicia o Claude Code (statusLine só carrega no boot)
```

`statusline-install` faz: backup de `~/.claude/settings.json`, merge atômico só na chave `statusLine` (preserva `permissions`, `hooks`, etc.), escreve config TOML default em `~/.claude-history/statusline.toml`. `--uninstall` reverte.

### 16 components disponíveis

| Component | Categoria | O que mostra |
|---|---|---|
| `cwd` | path | Caminho atual encurtado com `~` |
| `git` | git | Branch + dirty marker (`✱`) + ahead/behind (`↑1↓2`) |
| `ticket` | git | Auto-extrai `TICKET-NNNN` da branch |
| `worktree` | git | Nome do worktree (se ativo) |
| `model` | model | Display name (ex: "Opus 4.7") |
| `vim_mode` | system | NORMAL / INSERT |
| `context_pct` | context | Bar `▓▓░░░░ 42%` com cor por severity |
| `cost_session` | cost | `$X.XX` + badge `(N×p90)` quando acima do normal |
| `burn_rate` | cost | Tokens/min com seta `⬆` em rajadas |
| `cost_today` | cost | Soma do dia (requer daemon) |
| `cost_month` | cost | Acumulado + projeção (requer daemon) |
| `lines_changed` | git | `+45/-12` linhas |
| `rate_5h` | limits | % do bloco de 5h + countdown reset |
| `rate_7d` | limits | % do bloco semanal |
| `cluster` | history | `~auth-refactor` — cluster AI da session (requer daemon) |
| `time` | system | `hh:mm` |
| `mcp_status` | system | Placeholder pra MCP server health |

### 5 themes × 3 styles = 15 visuais

`themes`: graphite (default), nord, dracula, sakura, mono
`styles`:
- `plain` — separator entre segments (`│`)
- `powerline` — segments em pílulas com transição de cor (precisa Nerd Font pro arrow ``)
- `capsule` — pílulas independentes com bordas arredondadas (` `)

### Severities

Components com `has_warn_at: true` reagem com cor:

| Severity | Cor | Quando |
|---|---|---|
| **OK** | verde | valor < `warn_at` |
| **Warn** | amarelo | `warn_at` ≤ valor < `critical_at` |
| **Crit** | vermelho | valor ≥ `critical_at` |

Defaults (configuráveis no Studio ⚙):
- `context_pct`: warn=50, critical=80 (% do context window)
- `cost_session`: warn=0.8, critical=1.2 (multiplicador de p90 — 1.2 = 20% acima do seu p90)
- `burn_rate`: warn=1500, critical=3000 (tokens/min)
- `rate_5h` / `rate_7d`: warn=70, critical=90 (% do bloco)

### Conceito-chave: p90

`cost_session` compara seu cost atual com o **p90 histórico desse projeto** — o 90º percentil de custo de todas suas sessions desse projeto. Se p90 = $0.50 e session atual = $1.50, o badge vira `(3.0×p90)` e fica vermelho. Funciona como alerta defensivo: "você está gastando 3x o normal — algo deu errado, vou parar".

## Statusline Studio (no Web UI, tab `#studio`)

Editor visual do statusline. **Single source of truth**: o engine de render é em Go, o Studio web só envia config + mock data via POST e exibe o HTML pronto (Go converte ANSI → HTML).

### Painel esquerdo

- **Theme picker** — 5 cards com sample text + 3 indicadores (ok/warn/crit)
- **Style picker** — botões plain/powerline/capsule
- **Lines** — cada linha é um drag-drop horizontal de chips (components)
  - `+ add` abre modal com filtro fuzzy + agrupamento por categoria
  - `⚙` em chips com `has_warn_at` abre editor de threshold (warn_at / critical_at)
  - `×` remove
- **Resetar pra preset** — `↺ compact`, `↺ max`, `↺ powerline`
- **Salvar** — POST `/api/statusline/config` → grava em `~/.claude-history/statusline.toml`

### Painel direito

- **Preview live** — debounce 150ms, ANSI renderizado como HTML colorido
- **Mock data editor** — divididon em 2 sections:
  - **Input (stdin)** — cwd, branch, model, vim_mode, context %, cost USD, rate 5h/7d %, lines added/removed
  - **History (simula daemon)** — burn rate, cost p90, cost today, cluster name
- **ActiveDot** — bolinha verde/cinza ao lado de cada label do mock indica se aquele campo afeta a preview atual (depende do component estar em alguma linha)
- **Catálogo de components** — cards com label, descrição, badge "requer daemon"

## Como funciona

Cada sessão do Claude Code vira um `.jsonl` em `~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl`. O `cwd-encoded` é o caminho original com `/` → `-`. Cada linha é um evento (user/assistant/tool_use/tool_result).

O parser faz uma única passada streaming por arquivo, extrai metadados (sessionId, cwd, branch, msgs, tools, **tokens do `usage` field**, modelo) — sub-agents (`subagents/*.jsonl`) são ignorados pra não duplicar.

Cache SQLite (`~/.claude-history/index.db`) com FTS5 pra busca textual. Reindex incremental via `mtime`. Primeiro launch ~2-5s pra ~100 sessions, subsequentes ~50ms.

## Configuração

Diretório de runtime: `~/.claude-history/`

| Arquivo | Pra que serve |
|---|---|
| `index.db` | cache SQLite + FTS5 |
| `pricing.toml` | preços por modelo (input/output/cache) — edite quando Anthropic mudar preços |
| `config.toml` | preferências da TUI/serve (default tab, ai enabled, ollama url, alerts) |
| `state.toml` | estado da TUI entre runs (última tab, agrupamento) |
| `statusline.toml` | config do statusline (theme, style, lines, components, thresholds) |
| `about.txt` | (opcional) sua auto-descrição — usada como ground truth pelo profile generator |

`pricing.toml` exemplo (seedado no primeiro launch):

```toml
default_currency = "USD"
brl_rate = 5.20

[[models]]
name = "claude-opus-4-7"
input_per_mtok = 15.00
output_per_mtok = 75.00
cache_creation_per_mtok = 18.75
cache_read_per_mtok = 1.50
```

## Endpoints HTTP

```
GET  /api/sessions              # lista todas
GET  /api/sessions/<id>         # detalhe
GET  /api/sessions/<id>/messages
GET  /api/stats                 # heatmap, modelos, mês, week-delta, top projects
GET  /api/stats/behavioral      # top words, error rate, peak hour
GET  /api/behavior/advanced     # bigrams, trigrams, PMI, flow, style, scatter
GET  /api/costs                 # por dia/projeto/modelo + month projection
GET  /api/timeline              # sessions agrupadas por dia
GET  /api/tools                 # top tools globais
GET  /api/tools/<name>/sessions # drill-down
GET  /api/search?q=...&mode=metadata|fts
POST /api/refresh               # reindex
GET  /api/export/<id>           # session JSON

# AI
GET  /api/ai/health
GET  /api/ai/summaries
GET  /api/ai/clusters
POST /api/ai/clusters/recompute
GET  /api/ai/similar/<id>?n=10
POST /api/ai/generate-all       # gera summaries + embeddings em background
GET  /api/ai/insights
POST /api/ai/insights/generate
GET  /api/ai/profile
POST /api/ai/profile/generate

# Statusline
GET  /api/statusline?session_id=X&project_dir=Y    # agregados live (cache 5s)
GET  /api/statusline/components                     # catálogo dos 16 components
GET  /api/statusline/themes                         # 5 themes + 3 styles com cores
GET  /api/statusline/presets                        # compact/max/powerline
GET  /api/statusline/config                         # TOML atual como JSON
POST /api/statusline/config                         # salva
POST /api/statusline/render                         # {config, mock_input, mock_history} → {ansi, html}
```

## Arquitetura

```
claude-history/
├── main.go                            # router de subcomandos
├── embed.go                           # //go:embed all:web/dist
├── internal/
│   ├── model/session.go               # Session struct compartilhada
│   ├── parser/jsonl.go                # streaming JSONL → Session
│   ├── pricing/pricing.go             # TOML loader + cost breakdown
│   ├── config/config.go               # config.toml + state.toml
│   ├── stats/
│   │   ├── stats.go                   # heatmap, baseline, trends, cache savings
│   │   ├── behavioral.go              # top words, error patterns, peak hour
│   │   ├── advanced.go                # n-grams, PMI, flow, style, scatter
│   │   └── stopwords.go               # listas pt-BR + en
│   ├── ai/
│   │   ├── ollama.go                  # cliente HTTP Ollama
│   │   ├── worker.go                  # fila de geração de summaries+embeddings
│   │   ├── cluster.go                 # K-means++ sobre embeddings
│   │   ├── similar.go                 # cosine similarity
│   │   ├── insights.go                # advisor + profile generation
│   │   └── tech.go                    # regex de detecção de tech stack
│   ├── statusline/
│   │   ├── input.go                   # tipos do JSON stdin do Claude Code
│   │   ├── config.go                  # TOML config + defaults + load/save
│   │   ├── theme.go                   # 5 themes embedded
│   │   ├── ansi.go                    # color helpers (truecolor)
│   │   ├── components.go              # 16 components + metadata
│   │   ├── render.go                  # plain/powerline/capsule
│   │   ├── html.go                    # ANSI → HTML pra Studio
│   │   ├── history.go                 # fetch de /api/statusline (best-effort)
│   │   ├── presets.go                 # compact/max/powerline
│   │   └── install.go                 # merge atômico em settings.json
│   ├── server/
│   │   ├── server.go                  # http.Server + SSE Hub
│   │   ├── handlers.go                # rotas /api/*
│   │   ├── statusline.go              # /api/statusline (cache 5s + cache project 60s)
│   │   └── statusline_studio.go       # /api/statusline/{components,themes,config,render,presets}
│   └── index/
│       ├── sqlite.go                  # SQLite + FTS5 + ai_cache + insights + profile
│       └── reindex.go                 # scanner mtime-based
├── tui/
│   ├── app.go, recent.go, search.go, stats.go, costs.go, timeline.go,
│   ├── tools.go, behavior.go, ai.go, compare.go    # 9 tabs
│   ├── detail.go, chart.go, badge.go, export.go, style.go, keys.go
└── web/
    ├── package.json, vite.config.ts, tsconfig.json
    └── src/
        ├── App.tsx, api.ts, sse.ts, types.ts, styles.css
        ├── components/Layout.tsx
        └── tabs/{Recent,Search,Stats,Costs,Timeline,Tools,Behavior,AI,Compare,Studio}Tab.tsx
```

## Tech stack

**Backend**: Go 1.26 · [BurntSushi/toml](https://github.com/BurntSushi/toml) · [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) (CGO-free, FTS5 ativado) · [Bubble Tea](https://github.com/charmbracelet/bubbletea) · [Lipgloss](https://github.com/charmbracelet/lipgloss) · [Bubbles](https://github.com/charmbracelet/bubbles)

**Frontend**: Vite 8 · React 19 · TypeScript · Tailwind v4 · [Recharts](https://recharts.org/) · [@dnd-kit](https://dndkit.com/) (drag-drop)

**AI (opcional)**: [Ollama](https://ollama.com) com `qwen2.5:7b` (gen) + `nomic-embed-text` (embeddings) — tudo local, sem internet

## Privacidade

Tudo roda local. Nada sai da sua máquina:
- Index e cache no `~/.claude-history/`
- AI via Ollama localhost
- Web UI bind padrão `127.0.0.1:5555` (warning explícito se você passar `--listen 0.0.0.0`)
- Statusline endpoint cacheia 5s, não loga conteúdo

## Licença

MIT

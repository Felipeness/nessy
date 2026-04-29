# claude-history

Busca, retoma e analisa **todas** as suas conversas do Claude Code num só lugar — independente da pasta onde você abriu cada uma.

Lê os JSONLs que o Claude Code grava em `~/.claude/projects/<encoded-cwd>/*.jsonl`, indexa em SQLite com FTS5, e expõe **2 frontends** sobre o mesmo backend:

- **CLI clássica**: `claude-history list/show/fzf`
- **TUI**: `claude-history tui` — Bubble Tea com **6 tabs** (Search · Recent · Stats · Costs · Timeline · Tools), layout adaptativo, busca híbrida metadata+FTS5, métricas determinísticas (tokens/custo/duração) e análise comportamental light

## Instalação

```bash
git clone git@github.com:Felipeness/claude-history ~/Desktop/Projects/claude-history
cd ~/Desktop/Projects/claude-history
go build -o ~/.local/bin/claude-history .
```

Garante que `~/.local/bin` está no seu PATH.

## CLI

```bash
claude-history list                       # tabela
claude-history list --json | jq '.[]'     # JSON pra script
claude-history list --tsv                 # TSV
claude-history show 6df22c8d              # detalhes (aceita ID curto)
claude-history fzf                        # fzf interativo, Enter retoma
claude-history tui                        # TUI Bubble Tea (recomendado)
```

## TUI

### 6 tabs

| Tab | Pra que serve |
|---|---|
| **Search** | Busca metadata default · `:body <q>` switcha pra full-text via FTS5 |
| **Recent** | Lista cronológica densa: badge modelo, duração, tokens, custo, preview · `g` agrupa por projeto · indicadores 🟢🟡⚪ de atividade |
| **Stats** | Heatmap 12 sem · distribuição modelos · projeção custo do mês · long-tail · tendências · top palavras suas · sinais de retrabalho · prefixos · horário de pico |
| **Costs** | Custo/dia (30d) · custo por projeto · custo por modelo · cache savings global |
| **Timeline** | Cronologia visual: sessions agrupadas por dia, linha do tempo |
| **Tools** | Top 25 tools globais (esquerda) + drill-down das sessions que mais usaram a tool selecionada (direita) |

### Layout adaptativo

- **≥ 120 colunas**: multi-pane — lista esquerda + detail panel direita (em Tools: lista de tools + sessions que usam)
- **< 120 colunas**: full-screen single-pane

### Detail panel (painel direito em multi-pane)

Quando você seleciona uma session, o detail panel renderiza:

- Header: id, pasta, branch, modelo (badge colorido), duração
- 💰 Custo total + breakdown (input/output/cache create/cache read) com bars %
- 🔢 Tokens detalhados
- Cache hit gauge (`██████████░░░░ 67%`)
- ⚡ Mini-stats: msgs/min, tokens/msg, ratio user:assistant
- 🔧 Bar chart de tools usadas (cores por categoria: execução azul, edit verde, read amarelo)
- 📊 Sparkline 14d do histórico do projeto
- 📐 Comparação com mediana do projeto (setas ↑↑ / ↑ / = / ↓ / ↓↓)
- 💬 Trecho das últimas 3 user messages

### Keybinds

| Combo | Ação |
|---|---|
| `Tab` / `Shift+Tab` | Próxima/anterior tab |
| `1` `2` `3` `4` `5` `6` | Pula direto pra Search/Recent/Stats/Costs/Timeline/Tools |
| `j` `k` ou `↑` `↓` | Navegar lista |
| `Home` / `G` ou `End` | Topo / Fim |
| `PgUp` / `PgDn` | Página acima/abaixo (10 linhas) |
| `Enter` | Retomar session no cwd correto (`claude --resume`) |
| `/` ou `f` | Search box (modo metadata) |
| `:body <query>` | Switch pra full-text search via FTS5 |
| `g` | Agrupar Recent por tempo ↔ projeto |
| `s` | Toggle Stats local em terminal pequeno |
| `r` | Refresh (re-indexa) |
| `Ctrl+E` | Exporta session selecionada como JSON |
| `Ctrl+O` | Abre pasta da session no Finder |
| `?` | Help overlay |
| `q` ou `Esc` | Sair (salva state) |

### Persistência entre runs

Ao sair, a TUI grava `~/.claude-history/state.toml` com:
- Última tab ativa
- Modo de agrupamento da Recent
- Modo de search (metadata vs full-text)

Próximo launch carrega esse state — você cai exatamente onde estava.

### Configuração

`~/.claude-history/config.toml` (criado se você quiser editar):

```toml
[cost]
warn_per_day_usd = 5.00
alert_per_day_usd = 10.00

[ui]
default_tab = "Recent"
```

`~/.claude-history/pricing.toml` (seedado automático no primeiro launch):

```toml
default_currency = "USD"
brl_rate = 5.20

[[models]]
name = "claude-sonnet-4-6"
input_per_mtok = 3.00
output_per_mtok = 15.00
cache_creation_per_mtok = 3.75
cache_read_per_mtok = 0.30
```

Anthropic muda preços ocasionalmente — edita esse arquivo pra atualizar.

## Como funciona

Cada sessão do Claude Code vira um `.jsonl` em `~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl`. O `cwd-encoded` é o caminho original com `/` → `-`. Cada linha é um evento (user/assistant/tool_use/tool_result).

O parser faz uma única passada streaming por arquivo, extrai metadados (sessionId, cwd, branch, msgs, tools, **tokens do `usage` field**, modelo) — sub-agents (`subagents/*.jsonl`) são ignorados pra não duplicar.

A TUI usa um cache SQLite (`~/.claude-history/index.db`) com FTS5 pra busca textual. Reindex é incremental: compara `mtime` de cada `.jsonl` com o cache e só re-parseia o que mudou. Primeiro launch ~2-5s pra ~100 sessions, subsequentes ~50ms.

Sem live filesystem watcher — refresh é manual via `r`.

## Arquitetura

```
claude-history/
├── main.go                          # router de subcomandos
├── internal/
│   ├── model/session.go             # Session struct compartilhada
│   ├── parser/jsonl.go              # streaming JSONL → Session
│   ├── pricing/pricing.go           # TOML loader + cost breakdown
│   ├── config/config.go             # config.toml + state.toml
│   ├── stats/
│   │   ├── stats.go                 # heatmap, baseline, trends, cache savings, long-tail
│   │   ├── behavioral.go            # top words, error patterns, prefixes, peak hour
│   │   └── stopwords.go             # listas pt-BR + en
│   └── index/
│       ├── sqlite.go                # SQLite + FTS5 (open/upsert/get/list/search)
│       └── reindex.go               # scanner mtime-based
└── tui/
    ├── app.go                       # Bubble Tea root + tab routing + adaptive layout + state save
    ├── recent.go                    # tab Recent com agrupamento tempo/projeto
    ├── search.go                    # tab Search (metadata + :body full-text)
    ├── stats.go                     # tab Stats — dashboard
    ├── costs.go                     # tab Costs — financeiro
    ├── timeline.go                  # tab Timeline — cronologia
    ├── tools.go                     # tab Tools — drill-down por tool
    ├── detail.go                    # detail panel reusável (rico)
    ├── chart.go                     # primitives: BarChart/Gauge/Sparkline/Heatmap
    ├── badge.go                     # badge modelo (S/O/H + cor)
    ├── export.go                    # Ctrl+E exporta session JSON
    ├── style.go                     # lipgloss styles
    └── keys.go                      # keybinds centralizados
```

## Tech stack

Go 1.26 · [Bubble Tea](https://github.com/charmbracelet/bubbletea) · [Lipgloss](https://github.com/charmbracelet/lipgloss) · [Bubbles](https://github.com/charmbracelet/bubbles) (spinner) · [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) (CGO-free, FTS5 ativado) · [BurntSushi/toml](https://github.com/BurntSushi/toml)

## Roadmap

- [x] **Fase 1** — indexer + `list/show/fzf`
- [x] **Fase 2** — TUI Bubble Tea com 3 tabs + tokens/custo + cache SQLite
- [x] **Fase 2.1** — density pass: 6 tabs, detail panel rico, dashboards, behavioral light, polish
- [ ] **Fase 3** — `serve` HTTP + UI web (Vite/React)
- [ ] **Fase 4** — Behavioral analytics avançado (heurísticas + clusterização)
- [ ] **Fase 5** — AI-powered profiling (LLM local + embeddings via Ollama/MLX)
- [ ] **Fase 6** — Code mining (extração + análise de snippets gerados)

## Licença

MIT

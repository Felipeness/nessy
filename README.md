# claude-history

Busca e retoma conversas do Claude Code em todas as pastas de uma vez.

Lê os arquivos `~/.claude/projects/<encoded-cwd>/*.jsonl` que o Claude Code grava por sessão e expõe **2 frontends** sobre o mesmo indexer (Fase 3 traz o terceiro):

- **CLI clássica**: `claude-history list/show/fzf`
- **TUI**: `claude-history tui` — Bubble Tea com 3 tabs (Search / Recent / Stats), layout adaptativo, busca híbrida metadata+FTS5

## Instalação

```bash
cd ~/Desktop/Projects/claude-history
go build -o ~/.local/bin/claude-history .
```

## Uso

```bash
# CLI clássica
claude-history list                      # tabela
claude-history list --json | jq '.[]'    # JSON
claude-history list --tsv                # TSV pra script
claude-history show 6df22c8d             # detalhes (aceita ID curto)
claude-history fzf                       # fzf interativo

# TUI
claude-history tui                       # 3 tabs: Search/Recent/Stats
```

## TUI — keybinds

```
Tab / Shift+Tab    trocar tab
j / k              navegar lista
Enter              retomar session no cwd certo (claude --resume)
/ ou f             search box (modo metadata default)
:body <query>      switch pra full-text search via FTS5
g                  toggle agrupamento (Recent: tempo ↔ projeto)
s                  toggle stats local em terminal pequeno
r                  refresh (re-indexa)
?                  help overlay
q ou Esc           sair
Ctrl+O             abrir pasta no Finder
```

## Layout adaptativo

- **≥ 120 colunas**: multi-pane (lista esquerda + detail direita)
- **< 120 colunas**: full-screen com modal de detalhes

## Pricing

`~/.claude-history/pricing.toml` é seedado automaticamente no primeiro launch da TUI com snapshot atual dos preços Anthropic. Edite pra ajustar custos por modelo ou setar `brl_rate` pra display dual USD/BRL na tab Stats.

```toml
default_currency = "USD"
brl_rate = 5.20  # opcional

[[models]]
name = "claude-sonnet-4-6"
input_per_mtok = 3.00
output_per_mtok = 15.00
cache_creation_per_mtok = 3.75
cache_read_per_mtok = 0.30
```

## Como funciona

Cada sessão do Claude Code vira um `.jsonl` em:

```
~/.claude/projects/<cwd-encoded>/<session-uuid>.jsonl
```

O `cwd-encoded` é o caminho original com `/` substituído por `-`. Cada linha é um evento (user msg, assistant response, tool call, etc.).

O parser faz uma única passada streaming por arquivo extraindo metadados (sessionId, cwd, branch, primeira/última msg, contagem de tools, **tokens do `usage` field**, modelo). Sub-agents (`<session>/subagents/*.jsonl`) são ignorados porque herdam o `sessionId` do pai.

A TUI usa um cache SQLite (`~/.claude-history/index.db`) com FTS5 pra busca textual. Reindex é incremental: compara `mtime` de cada `.jsonl` com o que está no cache e só re-parseia o que mudou.

## Roadmap

- [x] **Fase 1** — indexer + `list` + `show` + `fzf`
- [x] **Fase 2** — `tui` Bubble Tea com tabs Search/Recent/Stats + tokens/custo
- [ ] **Fase 3** — `serve` HTTP + UI web (Vite/React) com dashboard temporal
- [ ] **Fase 4** — Behavioral analytics via heurísticas (regex/stats)
- [ ] **Fase 5** — AI-powered profiling (LLM local + embeddings via Ollama/MLX)
- [ ] **Fase 6** — Code mining (extração + análise de snippets gerados)

## Arquitetura

```
claude-history/
├── main.go                       # router de subcomandos (list/show/fzf/tui)
├── internal/
│   ├── model/session.go          # Session struct compartilhada
│   ├── parser/jsonl.go           # streaming JSONL → Session
│   ├── pricing/pricing.go        # TOML loader + cost calculator
│   └── index/
│       ├── sqlite.go             # SQLite + FTS5 (open/upsert/get/list/search)
│       └── reindex.go            # scanner mtime-based
└── tui/
    ├── app.go                    # Bubble Tea root + tab routing + adaptive layout
    ├── recent.go                 # tab Recent
    ├── search.go                 # tab Search (metadata + :body full-text)
    ├── stats.go                  # tab Stats (global aggregate + sparkline)
    ├── detail.go                 # detail panel reusável
    ├── style.go                  # lipgloss styles
    └── keys.go                   # keybinds centralizados
```

## Tech stack

Go 1.26 · [Bubble Tea](https://github.com/charmbracelet/bubbletea) · [Lipgloss](https://github.com/charmbracelet/lipgloss) · [Bubbles](https://github.com/charmbracelet/bubbles) · [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) (CGO-free, FTS5 ativado) · [BurntSushi/toml](https://github.com/BurntSushi/toml)

## Licença

MIT

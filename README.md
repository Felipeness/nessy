# claude-history

Busca e retoma conversas do Claude Code em todas as pastas de uma vez.

Lê os arquivos `~/.claude/projects/<encoded-cwd>/*.jsonl` que o Claude Code grava por sessão e expõe 3 frontends sobre o mesmo indexer:

- **Fase 1** (atual): `claude-history list` + `claude-history fzf` — TSV/tabela + integração fzf
- **Fase 2** (próxima): `claude-history tui` — TUI Bubble Tea
- **Fase 3** (próxima): `claude-history serve` — HTTP + web UI local

## Instalação

```bash
cd ~/Desktop/Projects/claude-history
go build -o ~/.local/bin/claude-history .
```

## Uso

```bash
# Lista todas as sessions (tabela)
claude-history list

# JSON (pra scripting)
claude-history list --json | jq '.[] | select(.message_count > 100)'

# TSV (pra outras tools)
claude-history list --tsv

# Detalhes de uma session (aceita ID curto)
claude-history show 6df22c8d

# fzf interativo: navega + Enter retoma
claude-history fzf
```

## Como funciona

Cada sessão do Claude Code vira um `.jsonl` em:

```
~/.claude/projects/<cwd-encoded>/<session-uuid>.jsonl
```

O `cwd-encoded` é o caminho original com `/` substituído por `-` (ex: `/Users/felipe/foo` → `-Users-felipe-foo`). Cada linha do JSONL é um evento (mensagem do user, resposta do assistant, tool call, etc.).

O parser faz uma única passada streaming por arquivo, extrai metadados (sessionId, cwd, timestamp, branch, primeira/última mensagem do user, contagem de tool calls) e devolve uma `Session`. Sub-agents (em `<session>/subagents/*.jsonl`) são ignorados porque herdam o `sessionId` do pai e duplicariam linhas.

## Roadmap

- [x] Fase 1 — indexer + `list` + `show` + `fzf`
- [ ] Fase 2 — `tui` Bubble Tea com filtros (data/branch/projeto/busca textual)
- [ ] Fase 3 — `serve` HTTP + UI web (Vite/React) com search full-text + AI summaries via Ollama

## Arquitetura

```
claude-history/
├── main.go                       # router de subcomandos
├── internal/
│   └── parser/
│       └── jsonl.go              # streaming JSONL → Session
└── (futuro)
    ├── internal/index/sqlite.go  # cache local
    ├── tui/                      # Bubble Tea
    └── web/                      # static + server
```

## Licença

MIT

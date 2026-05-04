# ADR-0005: Hybrid distribution — Go binary + AI skills bundled

**Status**: Active (estabelecido 2026-05-03 via `commit 811ee7d`)
**Confidence**: 🟢

## Context

Nessy precisa rodar em dois cenários distintos:
1. **Local mode** — features que dependem de código nativo (parser JSONL,
   SQLite index, embedding similarity, TUI, MCP server) precisam de binário.
2. **Delegated mode** — gerar specs invocando o AI engine que o user já tem
   (Claude Code, Codex, Cursor) sem dependência de Ollama nem cold start.

## Decision

**Fazer ambos**. Go binary distribuído via npm wrapper (esbuild-style platform
packages) + skills bundled via `go:embed` no binário, instaláveis em engines
AI via `nessy install`.

## Rationale 🟢

1. **Local AI mode** (binário) — `nessy spec` roda pipeline com Ollama local,
   gratuito, privado.
2. **Delegated AI mode** (skills) — `/nessy` no Claude Code/Cursor/etc roda
   pipeline com LLM do engine do user (mais capaz mas paga em tokens).
3. **Storage compartilhado** — ambos modos escrevem em `_nessy_atlas/` no projeto.
   TUI e MCP server lêem do mesmo lugar.
4. **MCP exposure** — outro Claude pode perguntar "o que esse repo faz?" via
   `nessy mcp` server, que serve specs gerados em qualquer modo.

## Trade-offs

- 🟢 Mais código pra manter (binário Go + skills MD), mas reusa muito (storage,
  MCP, TUI).
- 🟢 Distribuição mais complexa (precisa Go binary cross-platform via goreleaser
  + npm wrapper). Mitigado por boa CI (release.yml + npm.yml workflows).
- 🟡 Ambiguidade pro user: "qual modo eu uso?" — precisa docs claras. Default
  recomendado: skills (delegated) pra explorar; CLI (local) pra produção/CI/
  privacidade.

## Why not just one mode?

- **Só binário (sem skills)** — perde acesso fácil dentro do flow do Claude
  Code/Cursor, perde entrada no ecosystem multi-engine.
- **Só skills (sem binário)** — perde TUI, parser JSONL, indexing SQLite, MCP
  server, statusline, web Studio, todos os CLI commands existentes. Vira só
  um pacote de prompts, sem unique value.

Hybrid é o sweet spot. 🟢

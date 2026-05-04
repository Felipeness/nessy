# Permissions (Phase 3 — Decoder)

## Modelo de permissão: nenhum

Nessy é **single-user, local-only**. Não tem auth, login, RBAC, multi-tenancy.

🟢 Verified by absence:
- Sem package `auth/` em `internal/`
- Sem middleware HTTP de auth em `internal/server/handlers.go`
- Sem coluna `user_id` em qualquer table do schema (`internal/index/sqlite.go`)
- MCP server (`internal/mcp/server.go`) roda em stdio — herda permissão do
  processo pai (Claude Code do user)
- Web Studio HTTP (`internal/server`) listen em `localhost:5555` por default
  — não exposed externamente, sem TLS, sem auth header check

## Implicações

| Quem pode... | Resposta | Source |
|---|---|---|
| Ler suas sessions? | Você (process owner) | filesystem ACL — `~/.claude/projects/` 🟢 |
| Modificar o index DB? | Mesmo | `<cacheDir>/index.db` 🟢 |
| Acessar Web Studio? | Você + qualquer processo local que conecte em :5555 | `internal/server/server.go` 🟢 |
| Chamar tools MCP? | Outro Claude rodando no mesmo terminal/usuario que tem `nessy mcp-install` configurado | `~/.claude/settings.json` 🟢 |
| Modificar specs em `_nessy_atlas/`? | Você (mesmo) | filesystem ACL 🟢 |

## Quando isso muda?

🔴 Se algum dia Nessy virar SaaS (multi-user), tudo isso vira gap — precisaria
de auth model novo. Por enquanto, escopo single-user é decisão consciente.

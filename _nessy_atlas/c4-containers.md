# C4 Containers

```mermaid
graph TB
    subgraph Nessy [Nessy distribution]
        BinGo["nessy binary<br/>Go cross-platform<br/>(darwin/linux/win × arm64/amd64)"]
        SkillsDir["skills/<br/>5 SKILL.md embedded<br/>(go:embed)"]
    end

    subgraph Local FS
        SQLite[(index.db<br/>SQLite WAL<br/>~/.claude-history/)]
        AtlasDir[(_nessy_atlas/<br/>per-project specs<br/>output)]
        StateDir[(.nessy/state.json<br/>per-project pipeline state)]
        CCSkills[(.claude/skills/<br/>installed by 'nessy install')]
    end

    subgraph Web Studio [optional, on-demand]
        WebHTTP["HTTP server :5555<br/>Go net/http"]
        SPA["React SPA<br/>(embedded via go:embed web/dist)"]
    end

    subgraph MCP Bridge [stdio]
        MCPSrv["MCP server<br/>JSON-RPC stdio"]
    end

    BinGo -->|reads/writes| SQLite
    BinGo -->|writes| AtlasDir
    BinGo -->|writes| StateDir
    BinGo -->|nessy install copies| CCSkills
    SkillsDir -.->|source for install| CCSkills

    BinGo -->|nessy serve| WebHTTP
    WebHTTP -->|serves| SPA
    SPA -->|XHR/SSE| WebHTTP

    BinGo -->|nessy mcp| MCPSrv
```

🟢 Containers identificados de:
- `.goreleaser.yaml` (binário matrix)
- `embed.go` (SPA embedded)
- `internal/server/server.go` (HTTP listener)
- `internal/mcp/server.go` (stdio JSON-RPC)
- `cmd_install.go` (skills copy logic)

## Deploy units

- **`nessy` binary** — single executable per platform (~21 MB), distribuído via:
  - GitHub Release (tar.gz/zip) 🟢 `goreleaser`
  - npm wrapper (`@felipeness/nessy` + 6 platform packages) 🟢
- **Web Studio SPA** — embedded no binário (não deploy separate). React 19 build via
  bun → Vite → embedded em Go. 🟢
- **Skills** — embedded no binário. Instaláveis on-demand em projetos via
  `nessy install`. 🟢

## Storage

- `~/.claude-history/index.db` — central cache (sessions, FTS, AI cache).
  Single-user assumption. 🟢
- `~/.claude-history/config.toml`, `pricing.toml`, `state.toml` — config + state. 🟢
- `_nessy_atlas/` (per project) — specs gerados pelo `/nessy`. Committable
  ou .gitignore (decisão do user). 🟢
- `.nessy/state.json` (per project) — pipeline state. Pequeno, normalmente
  committable. 🟢

🟡 Storage decentralization é proposital pra evitar single-DB lock contention
quando múltiplos `nessy *` rodam concorrentes. Mas adiciona complexidade —
backup precisa cobrir 3+ paths.

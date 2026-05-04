# C4 Components — `nessy` binary

```mermaid
graph TB
    subgraph Frontends
        Main["main.go<br/>CLI dispatcher"]
        TUI["tui/<br/>Bubble Tea, 10 tabs<br/>~22.6k LOC"]
        Server["internal/server<br/>Web Studio HTTP"]
        MCP["internal/mcp<br/>JSON-RPC stdio"]
        CLI["cli.go + mcp_tools.go<br/>~25 subcommands"]
        Install["cmd_install.go<br/>nessy install/uninstall"]
    end

    subgraph Domain
        AI["internal/ai<br/>Ollama client + worker<br/>RAG, KMeans, summary, knowledge"]
        Search["internal/search<br/>BM25+dense+meta hybrid"]
        Stats["internal/stats<br/>threads, behavioral, overview"]
        Skills["skills/<br/>embedded SKILL.md (5)"]
    end

    subgraph Data
        Index["internal/index<br/>SQLite + FTS + migrations"]
        Parser["internal/parser<br/>JSONL parser, ledger"]
    end

    subgraph Core
        Model["internal/model<br/>Session struct"]
        Pricing["internal/pricing<br/>cost calc"]
        Config["internal/config<br/>TOML config + state"]
        Branding["internal/branding<br/>cache dir, colors"]
    end

    Main --> TUI
    Main --> Server
    Main --> MCP
    Main --> CLI
    Main --> Install
    Main --> AI
    Main --> Index
    Main --> Config
    Main --> Branding
    Main --> Pricing

    TUI --> Index
    TUI --> AI
    TUI --> Stats
    TUI --> Pricing
    TUI --> Config
    TUI --> Model

    Server --> Index
    Server --> AI
    Server --> Stats

    MCP --> Index
    MCP --> AI
    MCP --> Stats

    CLI --> Index
    CLI --> AI
    CLI --> Stats

    Install --> Skills

    AI --> Index
    AI --> Model
    AI --> Parser

    Index --> Search
    Index --> Model
    Index --> Parser

    Search --> Index
    Stats --> Model
    Stats --> Pricing

    Parser --> Model
```

🟢 Verified via `go list -deps` per package + grep `^import`.

## Component responsibilities

### Frontends

| Component | Owns | Doesn't own |
|---|---|---|
| `tui/` | Visual rendering, navigation, keybindings, scroll viewport | DB schema, parser, AI prompts |
| `internal/server` | HTTP routing, SPA serving, SSE | Business rules, cache invalidation logic |
| `internal/mcp` | JSON-RPC protocol, tool registration | Tool implementations (em `mcp_tools.go` root) |
| `main.go + cli.go` | Subcommand dispatch, output formatting (table/json/tsv) | Implementations (delegate pra `internal/`) |

### Domain services

| Component | Owns | Patterns |
|---|---|---|
| `internal/ai` | Ollama client, worker queue, RAG orchestration, KMeans | Strategy (gen vs embed model), context-cancellation timeouts |
| `internal/search` | RRF fusion, query parsing (`since:`, `cost:>`, etc), mode dispatch | Pure functions over DB queries |
| `internal/stats` | Thread building, behavioral n-grams, baseline calc | Aggregation pipelines |
| `skills/` | Embedded prompt files, install metadata | (leaf — no logic) |

### Data layer

| Component | Owns | Notes |
|---|---|---|
| `internal/index` | SQLite schema, migrations, all CRUD, FTS, encapsulates `internal/search` | WAL mode, mtime-based incremental reindex |
| `internal/parser` | JSONL → Session/Message/ToolEvent/FileOp/LedgerEntry | Pure functions, no DB |

### Core

| Component | Owns | Notes |
|---|---|---|
| `internal/model` | `Session` struct (canonical) | Re-exported via `parser.Session = model.Session` |
| `internal/pricing` | TOML loader, cost formulas | Default rates hardcoded, user-overridable |
| `internal/config` | Config + state TOML | Single-file each |
| `internal/branding` | Cache dir resolution, color constants | Cross-platform `os.UserCacheDir`-derived |

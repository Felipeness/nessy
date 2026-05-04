# ERD — index.db schema

🟢 Source: `internal/index/sqlite.go` migrations (grep `CREATE TABLE|VIRTUAL`).

```mermaid
erDiagram
    sessions ||--o{ tool_uses : has
    sessions ||--o{ tool_events : has
    sessions ||--o{ session_files : has
    sessions ||--o{ messages_fts : indexes
    sessions ||--o| ai_cache : caches
    sessions ||--o| session_knowledge : extracts

    sessions {
        text session_id PK "uuid from JSONL"
        text project_dir "decoded path"
        text jsonl_path UNIQUE "absolute path"
        int  jsonl_mtime "ns since epoch — used for diff"
        int  start_time "ns"
        int  end_time "ns"
        int  message_count
        int  user_messages
        int  assistant_messages
        text first_user_msg "snippet pra preview"
        text last_user_msg "snippet"
        text git_branch
        text claude_version
        text model "ex: claude-opus-4-7"
        int  input_tokens
        int  output_tokens
        int  cache_creation_tokens
        int  cache_read_tokens
        int  sidechain_turns
        int  sidechain_agents
        int  resolved_at_turn
    }

    tool_uses {
        text session_id FK
        text tool_name "Bash, Read, Edit, ..."
        int  count
        composite PK "(session_id, tool_name)"
    }

    tool_events {
        text session_id FK
        text tool_name
        int  ts "ns timestamp"
        text input_hash "SHA-256 do input — pra loop detection"
        composite indexed "(session_id, tool_name, ts)"
    }

    session_files {
        text session_id FK
        text path
        text op "read | write | edit"
        int  ts
        composite indexed "(path)"
    }

    messages_fts {
        text session_id "virtual FK"
        text role "user | assistant | system"
        text content "FTS5 indexed"
        notes "VIRTUAL TABLE FTS5"
    }

    last_index_meta {
        text key PK "ex: 'parser_version'"
        text value
    }

    ai_cache {
        text session_id PK
        text summary "LLM-generated"
        blob embedding "[]float32 encoded"
        int  cluster "K-means cluster index"
        text cluster_label "human-readable label"
        int  jsonl_mtime "invalidação via mtime match"
    }

    ai_insights {
        int    id PK
        text   kind "token_waste | loop | retrabalho | ..."
        text   severity "info | warn | crit"
        text   message
        text   session_id FK "nullable — pode ser cross-session"
        int    generated_at
    }

    ai_profile {
        int    id PK
        text   profile "LLM-generated user style description"
        int    generated_at
    }

    session_knowledge {
        text session_id PK
        text problem
        text solution
        text decisions "JSON array"
        text learnings "JSON array"
        text tech_used "JSON array"
        text open_questions "JSON array"
        int  generated_at
    }
```

## Indexes

- `idx_sessions_start ON sessions(start_time DESC)` 🟢 — list by recency
- `idx_sessions_project ON sessions(project_dir)` 🟢 — group by project
- `idx_tool_events_loop ON tool_events(session_id, tool_name, input_hash, ts)` 🟢
  — loop detection
- `idx_tool_events_session ON tool_events(session_id, ts)` 🟢
- `idx_session_files_path ON session_files(path)` 🟢 — file reuse stats
- FTS5 inherent index em `messages_fts(content)` 🟢

## Migrations

🟢 Idempotent via `CREATE TABLE IF NOT EXISTS` + PRAGMA table_info checks pra
adicionar columns em tables existentes. Versão tracked via `last_index_meta`
key `parser_version` — quando muda, FTS é truncado pra re-index.

## Foreign keys

🟡 PRAGMA `foreign_keys=1` está ON via DSN (`sqlite.go:146`), mas nem todas
relations têm FK constraint declarada. Composite PKs e unique indexes substituem
maior parte das garantias. Cleanup orphans é manual no reindex (`stale[]` loop).

## Storage size estimates

- Per-session row: ~2-5 KB (depende de tamanho dos snippets)
- FTS: ~2x message body size
- Embedding: ~3 KB per session (768 dim × float32)
- Knowledge: ~1-3 KB por session

🟢 Para ~1k sessions: ~10-20 MB. Para 10k: ~100-200 MB. Tranquilo pra SQLite.

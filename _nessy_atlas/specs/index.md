# Spec: index (data layer)

**Source**: `internal/index/`
**Last updated**: 2026-05-04
**Confidence overall**: 🟢 (95% claims cited file:line)

## Purpose

Camada de persistência: SQLite WAL com schema próprio (sessions, FTS messages,
tool events, AI cache, knowledge, insights, profile). Responsável por:
- migrações idempotentes no `Open()`
- ingest filewalk-based com diff por mtime
- queries de leitura otimizadas (N+1 eliminado em `ListSessions`)
- encapsulamento do `internal/search` (BM25 + dense fusion)

🟢 É o "single source of truth" runtime — todos frontends (TUI/Web/CLI/MCP) leem
daqui, nunca diretamente dos JSONLs.

## Public interface

### `Open(path string) (*DB, error)`
```go
func Open(path string) (*DB, error)
```
- **Behavior**: abre SQLite em WAL mode, roda migrations idempotentes, retorna
  `*DB` ready-to-use 🟢 `sqlite.go:144-205`
- **Inputs**: `path` — caminho do .db file (criado se não existe)
- **Outputs**: `*DB` (caller deve `defer db.Close()`); error se path inválido ou
  schema migration falha
- **Side effects**: cria `<path>` + `<path>-wal` + `<path>-shm` files. PRAGMA
  `journal_mode=WAL` + `foreign_keys=1` 🟢
- **Invariants preserved**:
  - 🟢 INV-1: `parser_version` em `last_index_meta` reflete versão atual; se
    diferente, FTS é truncado e re-indexado.

### `(*DB).ReindexFiltered(root string, filter IngestFilter) (ReindexStats, error)`
```go
type IngestFilter struct {
    SkipWarmup      bool
    SkipClearOnly   bool
    MinMessages     int
    ExcludeProjects []string
}

type ReindexStats struct {
    Scanned int
    New     int
    Updated int
    Removed int
}
```
- **Behavior**: walks `root` recursivamente, encontra `*.jsonl`, parseia
  novos/modificados (mtime check), upserts. Remove sessions cujo arquivo
  desapareceu. 🟢 `reindex.go`
- **Performance optimizations**:
  - 🟢 Preload de `(path → session_id, mtime)` map único pra evitar N queries
    SQLite no walk (`commit f7ea99f`)
  - 🟢 Preload de `messages_fts` count agregado em map (mesma ideia)
  - 🟢 Skip de `/subagents/` paths (`reindex.go:94`)
- **Errors**: walk error returned; per-file errors são silently swallowed
  (continue) 🟡
- **Side effects**: writes a múltiplas tables (`sessions`, `messages_fts`,
  `tool_uses`, `tool_events`, `session_files`)

### `(*DB).ListSessions() ([]*Session, error)`
```go
func (db *DB) ListSessions() ([]*Session, error)
```
- **Behavior**: retorna todas sessions ordenadas por `start_time DESC` 🟢
- **Performance**: 1 query principal + 1 query agregada pra `tool_uses` (não N+1).
  Foi otimizado em `commit 95a7310` 🟢
- **Errors**: query error
- **Side effects**: read-only

### `(*DB).Search...` family
Vários métodos pra busca: hybrid (FTS+dense+meta), metadata-only, body-only,
similar (cosine sobre embeddings). Encapsula `internal/search` package.

🟢 Detalhes em `internal/search/hybrid.go`.

### AI cache, knowledge, insights, profile
- `AICacheGet/Upsert/List/UpdateCluster` — `sqlite.go` 🟢
- `KnowledgeGet/Upsert/List` 🟢
- `InsightsList/Upsert` 🟢
- `ProfileGet/Set` 🟢

Padrão único: cada um tem `Get/Upsert/List`. Embedded blobs encoded via
`internal/ai/floatbits.go`.

## Required invariants

- 🟢 **INV-1**: `parser_version` em `last_index_meta` ≡ versão atual do parser.
  Quando diverge, FTS é truncado em `ReindexFiltered:73-85`.
- 🟢 **INV-2**: `messages_fts.session_id` corresponde a `sessions.session_id`
  válido. Garantido por reindex que insere FTS junto com session.
- 🟢 **INV-3**: `aiCache.embedding`, se presente, decodifica pra `[]float32`
  com tamanho consistente. Caller é responsável por dimensão (depende do embed
  model usado).
- 🟡 **INV-4**: `sessions.jsonl_path` é único (UNIQUE constraint). Reindex
  upsert via `ON CONFLICT REPLACE` — pode mascarar duplicatas se mesma session
  aparecer em 2 paths (raro mas possível com symlinks). 🟡

## Error model

| Error | Cause | Caller action |
|---|---|---|
| `sql.ErrNoRows` | session_id não existe | retornar nil, é OK |
| `sqlite ... locked` | outra escrita concorrente | retry com backoff (caller) |
| Schema migration error | Open falhou | fatal — DB corrompido, restaurar backup |

## Dependencies

- Internal: `internal/model`, `internal/parser`, `internal/search`
- External: `database/sql` stdlib + `modernc.org/sqlite v1.50.0`

## Examples / canonical paths

```go
// Bootstrap
db, err := index.Open("~/.claude-history/index.db")
if err != nil { fatal(err) }
defer db.Close()

// Initial ingest
stats, _ := db.ReindexFiltered("~/.claude/projects", index.IngestFilter{
    SkipWarmup: true,
})
fmt.Printf("indexed: +%d new, %d updated\n", stats.New, stats.Updated)

// Read
sessions, _ := db.ListSessions()
```

## Modification guide

- 🟢 Se adicionar nova column em `sessions`: incremente `parserVersion`
  constant + adicione `ALTER TABLE` idempotente em `Open()` migration block.
  FTS NÃO precisa truncar (só adicionou metadata).
- 🟢 Se mudar schema FTS (adicionar column indexada): incremente
  `parserVersion` — vai disparar reindex automatico.
- 🟡 Se quebrar backward compat de algum field: bump `parserVersion` e
  documentar em ADR. User vai pagar 1x reindex completo.
- 🔴 NÃO toque em foreign key constraints sem cuidado — vários joins assumem
  cascade behavior implícito.

## Test coverage

- Unit: `sqlite_test.go`, `reindex_test.go` 🟢
- Coverage areas: Open + migration, basic Upsert + Get, Reindex with filter,
  diff por mtime
- Gaps: 🟡 Concurrent access (multiple writers), failure recovery, FTS
  edge cases (queries com chars especiais)

## Related specs

- See also: `specs/ingest.md` for parser→index pipeline
- See also: `specs/ai.md` for cache lifecycle

# Spec: ingest (parser + index pipeline)

**Source**: `internal/parser/`, `internal/index/reindex.go`
**Last updated**: 2026-05-04
**Confidence overall**: 🟢

## Purpose

Pipeline filewalk-based que transforma JSONL files (Claude Code session logs) em
rows na DB SQLite indexada. Roda no startup do TUI (async, não bloqueia paint)
e quando user pressiona `r` (refresh).

🟢 É o input principal de TUDO no Nessy. Sem ingest não há dados.

## Public interface

### Async ingest no TUI startup
```go
tuiModel.SetInitialIngest(tui.MakeIngestCmd(db, ingestRoot, ingestFilter))
```
- **Behavior**: configura uma `tea.Cmd` que vai rodar reindex em goroutine quando
  TUI Init() executar. Resultado vem como `refreshDoneMsg`. 🟢 `tui/app.go`
- **Side effects**: spawn goroutine, eventually muta DB

### Manual refresh (TUI)
- User aperta `r` → `refreshing = true`, dispara mesma `refreshCmd` 🟢
  `tui/app.go:432-434`

### CLI `nessy list` etc.
- CLI commands que precisam de dados frescos NÃO disparam reindex —
  trabalham com cache atual da DB. User deve rodar `nessy tui` ou `nessy serve`
  pra reindex quando suspeitar de staleness. 🟡

## Required invariants

- 🟢 **INV-1**: Toda session em DB tem JSONL file existente. Stale removal loop
  no fim de `ReindexFiltered:163-179` enforces.
- 🟢 **INV-2**: Mtime check é fonte de verdade pra "session mudou". Se mtime
  do file ≡ mtime cached na DB E `parser_version` ≡ stored, reindex skipa.
- 🟡 **INV-3**: `IngestFilter.MinMessages = 0` (default) inclui warmup-only
  sessions se `SkipWarmup = false`. Pra suprimir totalmente, ambos flags devem
  ser ON.

## Filter semantics

```go
filter := index.IngestFilter{
    SkipWarmup:      true,  // skip sessions cujo único turn é setup boilerplate
    SkipClearOnly:   true,  // skip sessions com /clear sem mensagens reais depois
    MinMessages:     2,     // min total turns
    ExcludeProjects: []string{"~/scratch", "**/node_modules/**"},
}
```

🟢 Filter é applied PER session — sessions filtered são deletadas se já estavam
em DB (config mudou e sessão virou ineligible). `reindex.go:136-140`

## Error model

| Erro | Causa | Ação |
|---|---|---|
| `walkErr` | filesystem inacessível | propagated, ingest aborta |
| Per-file parse error | JSONL malformado | silently swallowed (continue), file count Scanned still incremented 🟡 |
| DB write error | disk full, permission | propagated, ingest pode estar parcial |

🟡 Per-file errors deveriam logar em `ai_insights` ou warn na status bar — hoje
ficam invisíveis. **Modification guide**: se adicionar logging, manter o
"continue on error" pra não fail no primeiro file ruim de 1000.

## Dependencies

- → `internal/parser` (ParseSession, ParseMessages, ParseToolEvents, ParseFileOps)
- → `internal/index` (Upsert, IndexMessages, IndexToolEvents, IndexFileOps)
- → `internal/model.Session`

## Canonical paths

### Cold start (TUI)
1. `tui.New` lê cache DB (`db.ListSessions()`) → render imediato
2. `Init()` retorna ingest Cmd → goroutine paralela
3. Goroutine: walk → diff → parse modificados → upsert
4. `refreshDoneMsg` → `m.reload()` → views atualizam

### Refresh manual (`r`)
1. User keypress `r` → `m.refreshing = true`, status "refreshing…"
2. Spawn `refreshCmd` Cmd
3. `refreshDoneMsg` → reload + status "refresh: +N new, M updated"

## Modification guide

- 🟢 Pra adicionar novo extractor (ex: gather user message embeddings during
  ingest): adicionar parser function em `internal/parser/`, chamar dentro de
  `ReindexFiltered` na seção "if !cached or different mtime", upsert em nova
  table.
- 🟡 Mudanças que aumentam tempo do ingest: cuidado, é cold-start. Se >1s
  agregado, considere fazer assíncrono num worker separado.
- 🟢 Pra novos filter criteria: adicionar campo em `IngestFilter`, adicionar
  check em `(filter).shouldSkip(s)`, adicionar parsing em `internal/config`.
- 🔴 NÃO mude `parser_version` por features triviais — força reindex full
  (caro pra users com 10k+ sessions).

## Test coverage

- Unit: `internal/parser/jsonl_test.go`, `golden_test.go` (golden file fixtures
  em `testdata/`) 🟢
- Unit: `internal/index/reindex_test.go` (mtime diff, filter) 🟢
- Integration gaps: 🟡 não há test que valida o pipeline TUI Init → Ingest →
  Reload end-to-end.

## Related specs

- See also: `specs/index.md` (storage layer details)
- See also: `state-machines.md` (Init / Refresh state transitions)

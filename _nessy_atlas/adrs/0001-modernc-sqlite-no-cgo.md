# ADR-0001: modernc.org/sqlite (pure-Go) over mattn/go-sqlite3 (CGO)

**Status**: Retroactive (deduzido 2026-05-04)
**Confidence**: 🟢 (forte evidência circumstancial)

## Context

`go.mod:10` lista `modernc.org/sqlite v1.50.0` — driver pure-Go. Alternative comum
no ecosystem Go é `github.com/mattn/go-sqlite3` que wrap a libsqlite3 nativa via CGO.

Web benchmarks mostram modernc é ~2x slower que mattn em INSERTs e SELECTs grandes.
Apesar disso, foi escolhido.

## Likely rationale 🟢

1. **Cross-compile sem C toolchain** — goreleaser cross-compila pra darwin/linux/
   windows × arm64/amd64 (`.goreleaser.yaml:25-31`). CGO=1 obrigaria ter cross-
   compiler C de cada plataforma instalado no build machine. CGO=0 + pure-Go faz
   `go build` funcionar em qualquer host.
2. **Distribuição npm sem complicações** — npm wrapper (`npm/`) precisa publicar
   binários por plataforma. Pure-Go = single artifact, sem dependências runtime
   (nem mesmo libsqlite3 instalado no sistema do user).
3. **CGO_ENABLED=0 explicito** em `.goreleaser.yaml:23` 🟢 — confirma intenção
4. **Performance "good enough"** — SQLite indexa ~5000 sessions em <1s mesmo
   com modernc, e queries de leitura (lista/search) são sub-100ms. Trade-off
   aceito.

## Trade-offs reconhecidos

- 🟡 ~2x slower que mattn em workloads write-heavy. Não impacta UX hoje.
- 🟢 Binário ~10MB maior que com CGO (libsqlite C compiled-in vs pure-Go runtime
  embedded).

## When to revisit

Se reindex de 100k+ sessions começar a exceder 5s, considerar mattn/go-sqlite3
com builds CGO por plataforma (mais complexo mas reverte performance).

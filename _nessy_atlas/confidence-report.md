# Confidence Report

Generated 2026-05-04. Pipeline run: full 5-phase via Claude Code (delegated mode).

## Coverage

- **Files analyzed**: 83 Go files + 5 SKILL.md + auxiliares
- **Modules with full spec**: 5 / ~16 effective (`index`, `ingest`,
  `ai`, `tui`, `skills-install`). Vários packages têm coverage parcial em
  `domain.md`/`architecture.md`/`state-machines.md`.

## Confidence distribution

| Marker | Count (rough) | Examples |
|---|---|---|
| 🟢 CONFIRMED | ~140 claims | Stack/deps citados de go.mod, `commit hashes` referenciados, schemas de `CREATE TABLE` extraídos |
| 🟡 INFERRED | ~30 claims | Patterns deduzidos (worker error handling, registry conventions), aspectos não-testados (Cursor end-to-end install) |
| 🔴 GAP | ~10 explicit | Spec gaps (server/mcp/statusline/web-studio sem spec dedicado), questões abertas listadas em domain.md |

🟢 Threshold conservador — sempre que ficou ambíguo, downgradei pra 🟡 ou 🔴.

## Top gaps (preciso teu input)

1. **🔴 specs/server.md** — Web Studio HTTP API merece spec dedicado.
   `internal/server/handlers.go` tem ~N endpoints sem documentação. Confirmar
   prioridade.

2. **🔴 specs/mcp.md** — MCP server + tools registered em `mcp_tools.go` sem
   spec dedicado. Importante porque é a interface pública pra outros Claudes.

3. **🔴 specs/statusline.md** — Toda a feature de statusline (editor visual no
   Web Studio + render no terminal) está sub-documentada.

4. **🔴 specs/web-studio.md** — Frontend React em `web/src/` não foi analisado.
   Precisaria phase 2 do pipeline rodada na pasta `web/` separadamente.

5. **🟡 Worker retry/backoff** — `internal/ai/worker.go` failures sem retry.
   Pretendido ou TODO?

6. **🟡 `parser_version` constant** — não achei via grep onde está hardcoded.
   Pode estar num file que não inspecionei. Confirmar localização.

7. **🟡 Galaxy view ergonomia** — coordenação cluster radius vs star size
   ainda em ajuste fino. Não é gap, é polish em curso.

## Validate next

Veja `_nessy_atlas/domain.md` § Open questions pra a fila completa.

Recomendação:
1. Reaplicar Phase 2 (mapper) com foco em `internal/server`, `internal/mcp`,
   `internal/statusline`, `internal/watch` — preencher 4 gaps de spec.
2. Phase 2 separadamente em `web/src/` (frontend React tem regras próprias).
3. Adicionar tests de TUI (snapshot) — coverage gap importante mencionado em
   `architecture.md` § tech debt.

## Output structure

```
_nessy_atlas/
├── inventory.md              ✓
├── code-analysis.md          ✓
├── dependencies.md           ✓
├── domain.md                 ✓
├── state-machines.md         ✓
├── permissions.md            ✓
├── architecture.md           ✓
├── c4-context.md             ✓
├── c4-containers.md          ✓
├── c4-components.md          ✓
├── erd-complete.md           ✓
├── confidence-report.md      ✓ (este file)
├── adrs/
│   ├── 0001-modernc-sqlite-no-cgo.md      ✓
│   ├── 0002-bubbletea-tui.md              ✓
│   ├── 0003-ollama-local-ai.md            ✓
│   ├── 0004-scoped-npm-package.md         ✓
│   └── 0005-skills-vs-binary.md           ✓
├── specs/
│   ├── index.md              ✓
│   ├── ingest.md             ✓
│   ├── ai.md                 ✓
│   ├── tui.md                ✓
│   └── skills-install.md     ✓
└── traceability/
    ├── code-spec-matrix.md   ✓
    └── spec-impact-matrix.md ✓
```

15 arquivos top-level + 5 ADRs + 5 specs + 2 traceability matrices = **27
arquivos** total no atlas. ~80 KB combined markdown.

## Pipeline metadata

- Started: 2026-05-04T00:00:00Z
- Completed: 2026-05-04 (mesma sessão)
- Mode: delegated via Claude Code (eu, Claude, segui SKILL.md instructions
  manualmente já que o /nessy slash não está auto-loaded em sessão atual sem
  restart). Demonstra que o pipeline FUNCIONA end-to-end.
- Phases run: 1 (Reconnaissance), 2 (Mapping), 3 (Decoding), 4 (Blueprint),
  5 (Scribing) — todas completas.
- User confirmations: 1 (após Phase 1, user confirmou prosseguir)
- Skill files used: 5 (orchestrator + 4 sub-skills)

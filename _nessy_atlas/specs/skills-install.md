# Spec: skills + install (`/nessy` delegated mode)

**Source**: `skills/`, `cmd_install.go`
**Last updated**: 2026-05-04
**Confidence overall**: 🟢 (recém implementado — `commit 811ee7d, aea07f6`)

## Purpose

Empacota 5 prompt files (SKILL.md cada) no binário Go via `go:embed`, e provê
`nessy install` que detecta engines AI no sistema do user (Claude Code, Codex,
Cursor, Gemini CLI) e copia os skills pros paths apropriados
(`.claude/skills/` ou `.agents/skills/`).

Resultado: usuário digita `/nessy` no Claude Code (ou `nessy` em engines sem
slash) e o orchestrator skill executa um pipeline 5-fases que reverse-engineerea
o codebase em specs em `_nessy_atlas/`, com confidence labels 🟢🟡🔴.

🟢 No modo delegated, a inteligência fica no AI engine do user — Nessy só
fornece prompts. O modo local (próxima fase do roadmap, `nessy spec <project>`)
vai rodar o mesmo pipeline localmente com Ollama, sem consumir tokens do user.

## Public interface

### `nessy install` CLI

```
nessy install [--global] [--all|--yes]
```

- **Default**: detecta engines via marker dirs em `~/` (`.claude`, `.codex`,
  `.cursor`, `.gemini`), prompts pra confirmar, instala project-local em
  `.claude/skills/` e/ou `.agents/skills/`. 🟢
- **`--global`**: instala em `~/.claude/skills/` (so Claude Code suporta global). 🟢
- **`--all` / `--yes`**: non-interactive. Se algum engine detectado, instala
  só nesses; se nenhum, instala em todos (4 engines). 🟢
- **Entry file** (CLAUDE.md, AGENTS.md, etc): criado APENAS se não existe —
  nunca sobrescreve customização do user. 🟢

### `nessy uninstall` CLI
```
nessy uninstall [--global]
```
- Remove apenas os 5 dirs `nessy*/` em cada engine path. Não toca em entry
  file (pode estar customizado). 🟢

### `skills.FS()` + `skills.Names()`

API Go programática pra outras partes do binário acessarem o embedded FS.
Hoje só `cmd_install.go` consome. 🟢 Phase 2 vai consumir tambem pra rodar
skills via Ollama.

## The 5 skills

| Skill | Phase | Output |
|---|---|---|
| `nessy` | 1 (recon) + orchestration | `inventory.md`, `.nessy/state.json` |
| `nessy-mapper` | 2 | `code-analysis.md`, `dependencies.md` |
| `nessy-decoder` | 3 | `domain.md`, `state-machines.md`, `permissions.md`, `adrs/` |
| `nessy-blueprint` | 4 | `architecture.md`, `c4-*.md`, `erd-complete.md` |
| `nessy-scribe` | 5 | `specs/<component>.md`, `traceability/*.md`, `confidence-report.md` |

🟢 Each skill é self-contained — frontmatter `name + description` + body
markdown. AI engine do user lê automaticamente quando ativa o skill.

## Required invariants

- 🟢 **INV-1**: Embedded FS espelha exatamente `skills/` no source — `go:embed
  all:nessy all:nessy-mapper ...`. Mudanças em SKILL.md requerem rebuild.
- 🟢 **INV-2**: Toda afirmação produzida pelos skills DEVE marcar confidence
  (🟢/🟡/🔴). É enforced no prompt body (não no código), mas o orchestrator
  valida na confidence-report.md final.
- 🟢 **INV-3**: Skills não modificam código fora de `_nessy_atlas/` e
  `.nessy/`. Read-only no projeto. Enforced só por convenção no prompt.
- 🟡 **INV-4**: Entry file não-sobrescrito — implementação atual checa
  `os.Stat ... os.IsNotExist`. Race condition teórica entre check e write,
  mas inocuo (worst case: sobrescreve um arquivo recém criado por outro
  processo, improvável).

## Engine detection logic

```go
for _, e := range engines {
    if _, err := os.Stat(filepath.Join(home, e.DetectMarker)); err == nil {
        detected = append(detected, e)
    }
}
```

🟢 `cmd_install.go:84-89`. Marker dirs:
- Claude Code: `~/.claude`
- Codex: `~/.codex`
- Cursor: `~/.cursor`
- Gemini CLI: `~/.gemini`

🟡 Detection pode ser falso-positivo se user tem o dir mas não usa o engine.
Ok pro caso (instalação não custa nada além de espaço).

## Error model

| Erro | Causa | Ação |
|---|---|---|
| `os.Stat` em ~/ falha | filesystem permission ou ~/ inacessível | fatal — não dá pra continuar |
| MkdirAll falha | permission no project root | imprimir erro, pular esse engine, continuar |
| copy file falha | disk full, etc | imprimir erro, pular esse skill |

🟢 Comportamento "skip e continua" pra falhas parciais — instala o que conseguir.

## Canonical paths

### First-time install
```bash
cd seu-projeto-legacy
nessy install
# Detecta engines, prompts pra confirmar, copia skills, cria entry files
# que não existem
```

### Reinstall após update do nessy
```bash
nessy uninstall
nessy install --all
```

🟡 `--all` em "Codex detectado mas não outros" instala APENAS Codex (não
todos engines). Pra forçar instalação em TODOS os engines:
```bash
# Não há flag explícita, mas pode-se fazer manual via mkdir + copy
# Workaround: instale em /tmp/fake-home com `~/.claude` etc fakes,
# então copie os outputs.
```
**Fix futuro**: `--engines all|claude,codex` flag.

## Modification guide

- 🟢 Pra adicionar engine novo: append em `engines` slice em `cmd_install.go`
  com `Name`, `SkillsPath`, `EntryFile`, `DetectMarker`. Sem outra mudança.
- 🟢 Pra adicionar skill novo: criar `skills/nessy-<name>/SKILL.md`, adicionar
  em `Names()` slice + no `go:embed` directive em `skills/embed.go`. Update
  orchestrator skill body pra invocar.
- 🟡 Pra mudar template do entry file: editar `writeEntryFile` em
  `cmd_install.go`. Cuidado pra não quebrar parsing dos engines (cada um
  espera formato específico — Claude Code aceita qualquer markdown, mas
  Cursor `.cursorrules` tem syntax mais restrita). 🔴 GAP — não testei
  Cursor end-to-end.
- 🔴 NÃO sobrescreva entry file existente sem prompt — usuários têm
  CLAUDE.md customizado.

## Test coverage

- 🟡 Zero tests automatizados pro install. Testado manualmente em /tmp dir
  durante desenvolvimento (`commit 811ee7d`). Recomendado:
  - Test que `nessy install` em dir vazio cria estrutura completa
  - Test que entry file existente NÃO é sobrescrito
  - Test que `uninstall` remove apenas os 5 dirs (não outros)

## Related specs

- See also: `state-machines.md` § /nessy spec pipeline
- See also: `architecture.md` § Flow 3 (delegated mode), Flow 4 (futuro local)
- See also: `adrs/0005-skills-vs-binary.md`

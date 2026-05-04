# ADR-0002: Bubble Tea (Charm) for TUI framework

**Status**: Retroactive (deduzido 2026-05-04)
**Confidence**: 🟡 INFERRED

## Context

`go.mod:8` usa `github.com/charmbracelet/bubbletea v1.3.10`. Alternativas comuns:
- `tview` (rivo) — mais widget-rich, modal-based
- `tcell` (gdamore) — lower-level, monta UI manual
- `cobra` + simple stdout — sem TUI, só CLI

## Likely rationale 🟡

1. **Estilo elm-architecture** (Model/Update/View) encaixa bem com event-driven
   tabs — Bubble Tea async via `tea.Cmd` permite ingest/AI/refresh sem bloquear
   UI thread.
2. **Estética Charm** (lipgloss styling, bubbles widgets, smooth animations)
   alinha com a vibe "polished local tool" — competição visual com Claude Code's
   own TUI presumivelmente.
3. **Comunidade ativa** — Charm tem múltiplos projetos famosos (gh-dash, glow,
   wishlist), exemplos abundantes, docs decentes.

## Trade-offs

- 🟡 No Windows tem quirks (alt-screen flicker em transições de layout — vimos
  ghost render bug recente, fix via `tea.ClearScreen`).
- 🟡 Cell-based diff renderer não pad linhas individualmente — exige
  `padLinesToWidth` workaround pra evitar leak entre frames.
- 🟢 Performance ótima pra terminais modernos. 60fps quando precisa.

## When to revisit

Se quirks Windows persistirem ou se precisar de widgets que Bubble Tea não tem
nativo (ex: complex tables com sort/filter inline), considerar `tview` que tem
biblioteca de widgets mais completa.

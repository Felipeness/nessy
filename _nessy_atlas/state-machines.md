# State Machines (Phase 3 — Decoder)

## Threads view (TUI tab)

User cicla com `v` entre 6 sub-views. ToggleView é módulo 6.

```mermaid
stateDiagram-v2
    [*] --> tree
    tree --> cards : v 🟢 tui/threads.go:188
    cards --> miller : v 🟢
    miller --> graph : v 🟢
    graph --> timeline : v 🟢
    timeline --> galaxy : v 🟢
    galaxy --> tree : v (mod 6) 🟢
```

🟢 Layout muda na transição: tree/cards/timeline = split (40/60 com detail panel),
miller/graph/galaxy = full-width (sem detail). Decisão em `IsFullWidth()`.

🟢 `tea.ClearScreen` disparado em ToggleView pra evitar ghost render no Windows
Terminal — `commit 6e93338`.

---

## Stats view (TUI tab)

```mermaid
stateDiagram-v2
    [*] --> overview
    overview --> models : m 🟢 tui/app.go (StatsMode)
    models --> detailed : m
    detailed --> overview : m

    state period {
        [*] --> all_time
        all_time --> last_7d : p 🟢
        last_7d --> last_30d : p
        last_30d --> all_time : p
    }
```

`m` cicla mode (overview/models/detailed), `p` cicla period (all/7d/30d).
Independentes: 3×3 = 9 combinações. 🟢

---

## Search input (TUI tab)

```mermaid
stateDiagram-v2
    [*] --> typing
    typing --> typing : qualquer tecla → input.View() captura
    typing --> results : enter → executa busca
    typing --> mode_change : ctrl+y/t/f → toggle mode/expand/fuzzy
    results --> typing : / ou f → reset query (NÃO automatico — manual)
```

🟡 Modos: `hybrid` (default) | `body` (FTS only) | `meta` (no body) | `sim` (semantic).
Toggle `ctrl+y` alterna fuzzy. `ctrl+t` alterna expand (mostra todos hits vs
agrupado por session).

---

## Viewer modal (overlay)

```mermaid
stateDiagram-v2
    [*] --> closed
    closed --> opened : V (uppercase) — abre viewer da session selecionada 🟢
    opened --> opened : navigation (j/k/g/G/etc)
    opened --> closed : q ou esc 🟢 tui/viewer.go
```

Quando `viewer.active = true`, captura TODAS as teclas (não vazam pra app).
🟢 `tui/app.go:253-258`

---

## AI worker (background)

```mermaid
stateDiagram-v2
    [*] --> idle
    idle --> processing : Enqueue(sessionID)
    processing --> generating_summary
    generating_summary --> generating_embedding
    generating_embedding --> writing_db
    writing_db --> idle
    processing --> error_state : Ollama unreachable / timeout
    error_state --> idle : drop and continue (no retry hoje 🟡)
```

🟢 Worker.Run loop em goroutine, started por main.go quando `cfg.AI.Enabled`.
🟡 Sem backoff explicito em failures — investigar `internal/ai/worker.go`.

---

## /nessy spec pipeline (NEW skill)

```mermaid
stateDiagram-v2
    [*] --> reconnaissance
    reconnaissance --> awaiting_user : write inventory.md, ask "ok to proceed?"
    awaiting_user --> mapping : user confirms
    mapping --> awaiting_user2 : write code-analysis.md, deps.md
    awaiting_user2 --> decoding : user confirms
    decoding --> blueprint : write domain/state-machines/permissions/adrs
    blueprint --> scribing : write architecture/c4-*/erd
    scribing --> [*] : write specs/* + traceability + confidence-report
```

🟢 State em `.nessy/state.json` (versão 1) — `phase`, `completed_phases`,
`next_action`. Permite resume de qualquer ponto.

🟢 Após Phase 1 (recon), orchestrator PARA e pede confirmação. Outras pausas
são opcionais (user pode interromper a qualquer momento).

---

## Session lifecycle (Claude Code, externo a Nessy)

Não controlado por nós, mas relevante pra parsing:

```mermaid
stateDiagram-v2
    [*] --> active : nova session via `claude` CLI
    active --> active : turns user/assistant/tool_use/tool_result
    active --> compacted : context limit hit → /compact ↻
    active --> cleared : /clear → session vira "clear-only" se nada vem depois 🟢
    active --> ended : user fecha terminal / process exit
    compacted --> active : continua na mesma session
    ended --> resumed : `claude --resume <id>` → cria nova session linkando original 🟡
```

🟡 Resume cria NEW session JSONL com referência à original, mas semantica
exata varia por versão do Claude. Nessy detecta via parser kind = "resumed".

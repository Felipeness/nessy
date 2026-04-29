---
title: TUI density pass — Fase 2.1
status: approved
date: 2026-04-29
phase: 2.1
parent: 2026-04-28-tui-base-design
---

# TUI density pass — Design

## Contexto

A Fase 2 entregou TUI funcional mas "espartana". Esta Fase 2.1 adensa a UX em 6 frentes (A-F) sem fragmentar em fases novas. É um pass de density/polish, não muda arquitetura.

## Goals

1. Cada linha de lista mostra mais info útil sem virar ruído.
2. Detail panel vira pequeno dashboard por session.
3. Stats tab vira analytics real (não só agregados).
4. Novas tabs Costs/Timeline/Tools pra drill-down focado.
5. UX polish: cores semânticas, progress bars, persistência.
6. Análise comportamental light determinística (regex/stats — sem LLM).

## Non-goals

- LLM-powered profiling (continua na Fase 5)
- Code mining (continua na Fase 6)
- Web UI (continua na Fase 3)
- Live filesystem watcher (segue manual)

## Frente A — Lista densa

Linha atual: `🟢 16:34  ~/Desktop/...  250 msg  preview…`

Linha nova: `🟢 16:34  41m  📊S  1.2M  $4.32  ~/Desktop/…  preview…`

Decomposição:
- `🟢` activity icon (mantido)
- `16:34` última atividade (mantido)
- `41m` duração (NOVO, formato curto: `45s`/`12m`/`1h23m`)
- `📊S/O/H` badge do modelo (NOVO — `S`=sonnet azul, `O`=opus roxo, `H`=haiku verde)
- `1.2M` total tokens (NOVO — `Xk`/`X.YM`)
- `$4.32` custo USD (NOVO — `?` se modelo não mapeado)
- pasta + preview (mantidos)

Em multi-pane, dir trunca pra 24 chars (deixa preview mais longo). Em single, dir 30 + preview 40.

## Frente B — Detail panel rico

Adições ao painel direito:

### B1 — Bar chart de tools

Em vez de:
```
Bash    73
Edit     3
Read     3
```
Renderizar:
```
Bash   ████████████████████ 73 (90%)
Edit   █                    3
Read   █                    3
```

Largura proporcional ao painel, max 30 chars de barra. Cores por categoria:
- Execution (Bash, Task, Skill) — `#39` azul
- Edit (Edit, Write, NotebookEdit) — `#46` verde
- Read (Read, Grep, Glob, ToolSearch, WebFetch, WebSearch) — `#220` amarelo
- Schedule/Wakeup — `#241` cinza

### B2 — Breakdown de custo

```
Custo: $4.32 USD (R$ 22,46)
  Input         $0.30 (7%)   ████
  Output        $0.75 (17%)  ████████
  Cache create  $1.13 (26%)  █████████████
  Cache read    $2.14 (50%)  █████████████████████████
```

### B3 — Cache hit ratio gauge

```
Cache hits:  ████████████████░░░░  82%
```

### B4 — Mini sparkline desse projeto

```
Histórico do projeto (12 dias): ▁▂▅▇█▇▅▂▁▃▆▇  (12 sessions, $22.40)
```

### B5 — Comparação com baseline

Baseline = mediana das últimas 30 sessions do mesmo projeto (ou todas se <30).

```
vs mediana do projeto:
  msgs        250  (mediana 80, +212%)  ↑
  custo       $4.32 (mediana $1.20, +260%)  ↑
  duração     41m   (mediana 18m, +128%)  ↑
```

Setas: `↑↑` >+50%, `↑` >+10%, `=` ±10%, `↓` >-10%, `↓↓` >-50%.

### B6 — Trecho da última conversa

Últimas 3 user msgs preview (truncadas em 80 chars cada):
```
Última conversa:
  user    14:32  "ja que voce vai criar um projeto nao esqueca…"
  user    14:35  "B nao e endurecer e ampliar se ele nao trigar…"
  user    14:38  "opcao A, mas ja atualize logo antes de continuar"
```

### B7 — Mini-stats

```
msgs/min:   6.0      tokens/msg:  4,940     user:assistant ratio:  0.67
```

## Frente C — Stats tab dashboards

### C1 — Heatmap hora × dia (últimas 12 semanas)

```
Atividade (12 semanas)
       Mon Tue Wed Thu Fri Sat Sun
00-04   ·   ·   ·   ·   ·   ·   ·
04-08   ·   ·   ·   ·   ·   ·   ·
08-12   ▁   ▂   ▅   ▃   ▂   ·   ·
12-16   ▅   ▇   █   ▆   ▅   ▂   ▁
16-20   ▆   █   █   ▇   ▆   ▃   ▂
20-24   ▂   ▃   ▂   ▂   ▁   ·   ·
```

8 níveis (`·▁▂▃▄▅▆▇█`), normalizado por max bin.

### C2 — Distribuição de modelos

```
Modelos usados:
  sonnet-4-6   ████████████████████  82%  (1,234 msgs)
  opus-4-7     ████                  15%  (228 msgs)
  haiku-4-5    █                      3%  (42 msgs)
```

### C3 — Custo cumulativo do mês + projeção

```
Abril 2026: $87.43 acumulado · projeção fim do mês: $145 (29 dias × média $4.83/dia)
  Hoje: $5.40   Limite warn: $5    Limite alert: $10
  ⚠ Hoje passou do warning threshold
```

### C4 — Top tools por projeto

Drill-down: ao selecionar projeto na lista, painel direito mostra tools daquele projeto. Em narrow mode, `s` toggle habilita "stats local" que já existe.

### C5 — Long-tail

```
Top 5 mais caras:
  $18.40  19a6a4ba  ~                            939 msgs   3h 12m
  $4.32   6df22c8d  ~                            250 msgs    41m 36s
  ...

Top 5 mais longas:
  3h 12m  19a6a4ba  ~                            939 msgs
  ...
```

### C6 — Tendências

```
Esta semana vs anterior:
  Sessions    8 (+33%)   ↑
  Msgs    1,234 (+18%)   ↑
  Custo  $24.50 (-12%)   ↓
  Cache hit  82% (+5pp)  ↑
```

### C7 — Cache efficiency global

```
Cache savings: $145.20 economizados em 30 dias (87.4M tokens lidos do cache)
```

## Frente D — Novas abas

### D1 — Tab Costs

Dedicado ao financeiro. Independente das outras tabs.

```
┌─[Search│Recent│Stats│Costs│Timeline│Tools]──────┐
│ Custo no mês: $87.43 (proj. $145)               │
│                                                 │
│ Por dia (últimos 30):                           │
│   Apr 01  $0.50  █                              │
│   Apr 02  $1.20  ███                            │
│   ...                                           │
│   Apr 28  $4.32  ███████████                    │
│   Apr 29  $5.40  █████████████                  │
│                                                 │
│ Por projeto (últimos 30 dias):                  │
│   ~/Desktop/Projects/claude-history  $32 (37%)  │
│   ~                                  $28 (32%)  │
│   ~/obsidian-vault                   $15 (17%)  │
│                                                 │
│ Por modelo:                                     │
│   sonnet-4-6   $65 (74%)                        │
│   opus-4-7     $20 (23%)                        │
│   haiku-4-5    $2  (3%)                         │
│                                                 │
│ Cache savings: $145.20 (3.3x return)            │
└─────────────────────────────────────────────────┘
```

### D2 — Tab Timeline

Cronologia visual do dia/semana (não da session — do calendar).

```
Hoje (2026-04-29)
  10:43  ─●─  open ~/Desktop/Projects/claude-history
                "criou o repo? de commit e push?"
  11:12  ─●─  open ~                  ⚪ pausada 5min
  13:55  ─●─  open ~/obsidian-vault   🟢 ATIVA

Ontem (2026-04-28)
  10:53  ─●─  open ~  (7 msgs, $0.12)
  10:57  ─●─  open ~  (1140 msgs, $20+)
  ...
```

### D3 — Tab Tools

Drill-down por tool. Default ranking global, Enter num tool abre lista de sessions que mais usaram aquele tool.

```
Top tools globais
  Bash       2,340 calls (45 sessions, média 52/session)
  Edit         480 calls (38 sessions)
  Read         320 calls (29 sessions)
  TaskCreate   180 calls (12 sessions)
  ...

(Enter num tool abre detail das sessions que mais usaram)
```

## Frente E — UX polish

### E1 — Cores/badges nos modelos

| Modelo | Cor | Badge |
|---|---|---|
| claude-sonnet-* | `#39` azul | `S` |
| claude-opus-* | `#129` roxo | `O` |
| claude-haiku-* | `#46` verde | `H` |
| outro | `#241` cinza | `?` |

### E2 — Progress bars / gauges

Reutilizar a primitive em B1 (bar chart) pra: cache hit, % spending vs threshold, cache savings ratio. Lipgloss `lipgloss.NewStyle().Width(N)` com fundo + foreground.

### E3 — Animação de loading

Durante reindex, status bar mostra spinner via `bubbles/spinner`:
```
status: ⠋ refreshing… (12/45)
```

### E4 — Keybinds extras

```
gg            ir pro topo da lista
G             ir pro fim
PgUp / PgDn   página acima/abaixo (10 linhas)
n / N         next/prev no resultado de search
,             abrir settings overlay
Ctrl+E        export session JSON
1 / 2 / 3 / 4 / 5 / 6   ir direto pra tab N
```

### E5 — Persistência de estado

`~/.claude-history/state.toml`:
```toml
last_tab = "Recent"
recent_group_by_project = false
recent_filter_7d = false
search_mode = "metadata"
```

Lê no startup, escreve no quit.

### E6 — Export

`Ctrl+E` na session selecionada → escreve `~/.claude-history/exports/<session-id>.json` com:
- Metadata da Session (tudo do struct)
- Lista de mensagens user/assistant (do FTS5 cache)
- Custo calculado
- Tools breakdown

Status bar mostra "exported to ..." durante 3s.

## Frente F — Análise comportamental light

### F1 — Top palavras suas

Em Stats tab, nova seção "Sua linguagem":
```
Suas palavras mais usadas (top 20, excl. stopwords):
  preciso (45)   instala (38)   cria (32)   vamos (28)
  configurar (24)   ...
```

Stopwords: lista hardcoded em pt-BR (`de, a, o, que, e, do, da, em, um, para, com, não, ...`) + en (`the, of, and, to, a, in, ...`).

Tokenização: `\b[a-záéíóúâêôãõç]+\b` lowercased.

### F2 — Padrões de erro/correção

Heurística determinística — count msgs com:
```
errado, errei, errou, no, nao, nao funciona, fail,
rollback, desfaz, ignora, esqueci, mudei de ideia,
cancela, para, stop
```

Métrica: `error_rate = msgs com sinais de erro / total user msgs`.

Mostrado em Stats:
```
Sinais de retrabalho: 38 msgs (6% do total) — saudável
```
Threshold: <5% verde, 5-15% amarelo, >15% vermelho.

### F3 — Prefixos comuns

Top 10 primeiras palavras das suas msgs:
```
Como você inicia mensagens:
  vamos (45)   instala (38)   cria (32)   pode (28)   isso (22)
  agora (18)   blz (15)   opcao (14)   ...
```

### F4 — Horário de pico

Bar chart por hora do dia (msgs do user agregadas):
```
Quando você usa Claude Code:
  00 ·   01 ·   02 ·   ...
  09 ▁   10 ▃   11 ▆   12 █  ← pico
  13 ▆   14 ▇   15 █   16 █
  20 ▃   21 ▂   ...
```

## Storage / config

Novos arquivos em `~/.claude-history/`:

```
~/.claude-history/
├── index.db          (existente)
├── pricing.toml      (existente)
├── config.toml       NOVO — thresholds, stopwords customizadas, BRL rate
├── state.toml        NOVO — última tab, grouping, filtros
└── exports/          NOVO
    └── <session-id>.json   gerado por Ctrl+E
```

`config.toml` schema:
```toml
[cost]
warn_per_day_usd = 5.00
alert_per_day_usd = 10.00

[behavioral]
top_words_count = 20
error_words = ["errado", "errei", "rollback", ...]  # opcional, override do default
stopwords_extra = []  # opcional

[ui]
default_tab = "Recent"
```

## Critérios de aceitação

- [ ] Lista mostra duração + badge modelo + tokens + custo (Frente A)
- [ ] Detail panel mostra bar chart de tools (B1)
- [ ] Detail panel mostra breakdown de custo (B2) com %
- [ ] Detail panel mostra cache hit gauge (B3)
- [ ] Detail panel mostra mini-sparkline do projeto (B4)
- [ ] Detail panel mostra comparação vs baseline (B5)
- [ ] Detail panel mostra trecho últimas msgs (B6)
- [ ] Detail panel mostra mini-stats (B7)
- [ ] Stats tab tem heatmap (C1), distribuição modelos (C2), projeção (C3), long-tail (C5), tendências (C6)
- [ ] Tab Costs funciona com 3 visões (por dia, projeto, modelo)
- [ ] Tab Timeline mostra cronologia
- [ ] Tab Tools mostra ranking + drill-down
- [ ] Cores/badges aplicados aos modelos
- [ ] Spinner durante refresh
- [ ] Persistência de estado entre runs
- [ ] Ctrl+E exporta JSON
- [ ] Top palavras + erros + prefixos + horário (F1-F4) em Stats

## Riscos & edge cases

| Risco | Mitigação |
|---|---|
| Heatmap com poucos dados (< 1 semana de uso) | Mostra tabela mesmo zerada com hint "use por mais 1 semana pra ver padrões" |
| Modelo desconhecido sem badge | Fallback `?` cinza |
| Stopwords não cobre tokens compostos ("não" vs "n") | Aceita imperfeição na Fase 2.1; Fase 5 melhora com NLP |
| Bar chart estoura largura em narrow | `min(painel-margem, 30)` |
| Comparação vs baseline com <3 sessions | Esconde a seção, mostra "baseline insuficiente" |
| Persistência corrompe TOML | Try/recover: deleta state.toml e segue com defaults |
| Export gigante (sessions com 1000+ msgs) | Streaming write, sem load inteiro em memória |

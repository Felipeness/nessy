---
title: Behavioral analytics avançado — Fase 4
status: approved
date: 2026-04-29
phase: 4
parent: 2026-04-29-tui-density-design
---

# Behavioral analytics avançado — Design

## Contexto

Fase 2.1 entregou behavioral light (top words, error rate, prefixes, peak hour). Fase 4 aprofunda com análise multi-dimensional **determinística** (sem LLM — Fase 5 cobre isso).

## Goals

1. N-grams (bigrams/trigrams) revelam padrões que palavras isoladas escondem
2. Co-occurrence mostra "ecossistemas de tópicos" implícitos
3. Time vs custo correlation responde "quando eu gasto mais?"
4. Conversation flow caracteriza estilo de uso (telegráfico vs verbose)
5. User vs IA comparativo mostra simetria/assimetria de vocabulário
6. High-error drill-down detecta sessions de retrabalho

## Non-goals

- LLM-powered clustering (continua Fase 5)
- Sentiment analysis (continua Fase 5)
- Code mining (continua Fase 6)

## Features

### F1 — N-grams

Top 20 bigrams + top 10 trigrams (ambos só user msgs, lowercased, stopwords filtradas, hifens preservados).

Tokenização: já existente em `internal/stats/behavioral.go`. Adicionar `Bigrams()` e `Trigrams()` que percorrem janela deslizante de 2/3 palavras.

### F2 — Co-occurrence

Top 30 pares de palavras que aparecem na mesma user message (excluindo bigrams adjacentes pra não duplicar com F1). Score = **pointwise mutual information**:

```
PMI(a,b) = log2(P(a,b) / (P(a) * P(b)))
```

onde `P(a,b)` = frequência do par dividida por total de mensagens, `P(a)` = frequência da palavra `a`. Filtro: pares com count ≥ 3.

### F3 — Time × cost correlation

Para cada session, ponto `(hora_do_dia, cost_usd)`. Apresentação:
- **Web**: Recharts ScatterChart, cor do ponto = badge color do modelo
- **TUI**: tabela agregada por bin de hora (00-04, 04-08, ...) com custo médio + count

### F4 — Conversation flow

Histogram de msgs/session em buckets logarítmicos (`<10`, `10-30`, `30-100`, `100-300`, `300-1000`, `>1000`):
- **Web**: Recharts BarChart
- **TUI**: ASCII bars usando Sparkline existente

Plus métrica: msgs/session mediana, p90, p99.

### F5 — User vs IA style

Tabela comparativa entre user msgs e assistant msgs:

| Métrica | Você | IA |
|---|---|---|
| Avg word count | 12.3 | 87.5 |
| Vocabulário único | 1240 | 8923 |
| Top 5 palavras | preciso, vamos… | tab, files… |
| Avg sentence length | 3.2 | 12.1 |

### F6 — High-error sessions

Sessions com `error_signal_rate > 15%` (heurística F2 da Fase 2.1, recalculada per-session):
- Lista clicável (Enter/click → DetailPanel)
- Coluna de error_rate visível

## Backend

```
internal/stats/
├── behavioral.go            (existente, estender)
└── behavioral_advanced.go   (novo)
```

Novas funções em `behavioral_advanced.go`:

```go
type Bigram struct { A, B string; Count int }
type Trigram struct { A, B, C string; Count int }
type CoOccur struct { A, B string; Count int; PMI float64 }
type FlowHist struct { Bucket string; Count int }
type StyleStats struct {
  AvgWordsUser, AvgWordsAssistant      float64
  UniqueWordsUser, UniqueWordsAssistant int
  TopWordsUser, TopWordsAssistant       []WordCount
  AvgSentencesUser, AvgSentencesAssistant float64
}
type ErrorSession struct {
  Session   *model.Session
  ErrorRate float64
  Hits      int
  Total     int
}

func TopBigrams(sessions []*model.Session, n int) []Bigram
func TopTrigrams(sessions []*model.Session, n int) []Trigram
func CoOccurrences(sessions []*model.Session, minCount int, n int) []CoOccur
func FlowDistribution(sessions []*model.Session) []FlowHist
func StyleComparison(sessions []*model.Session) StyleStats
func HighErrorSessions(sessions []*model.Session, threshold float64) []ErrorSession
func TimeCostPoints(sessions []*model.Session, p *pricing.Pricing) []TimeCostPoint
```

## REST endpoint

`GET /api/behavior/advanced` retorna struct com todos os campos acima.

## Frontend Web

Nova aba **Behavior** (`#behavior`) entre Tools e Compare.

Layout:
- 4 KPI cards: total bigrams, total co-occur pairs, error sessions count, p90 msgs
- Bigrams + Trigrams lado a lado
- Co-occurrence top 30 (lista com PMI score)
- Time vs cost ScatterChart
- Conversation flow histogram BarChart
- Style comparison table
- High-error sessions list (clicável)

## TUI

Nova tab `Behavior` (8ª tab — keybind `7` redirecionado pra Behavior, atual ordem fica `8` Tools):

Wait — tabs atuais: Search/Recent/Stats/Costs/Timeline/Tools (6). Adicionar Behavior fica:
Search/Recent/Stats/Costs/Timeline/Tools/Behavior = 7. Web tem +Compare = 8. Pra simplicidade: TUI fica com 7 (sem Compare), Web tem 8.

Layout TUI da Behavior:
- Bigrams top 15 + Trigrams top 8 lado a lado
- Co-occurrence top 20 com bar chart inline
- Flow histogram Sparkline + p50/p90/p99
- Style comparison: 2 colunas
- High-error sessions: lista (sem drill-down — só info)

Reusa primitives `BarChart`, `Sparkline` do `tui/chart.go`.

## Critérios de aceitação

- [ ] N-grams (bigrams + trigrams) calculados e exibidos em ambos
- [ ] Co-occurrence com PMI funciona em ambos
- [ ] Time vs cost: Web tem ScatterChart, TUI tem tabela
- [ ] Conversation flow histogram em ambos
- [ ] Style comparison user vs IA em ambos
- [ ] High-error sessions identificados (threshold 15%)
- [ ] Endpoint `/api/behavior/advanced` retorna JSON
- [ ] Sem regressão no resto

## Riscos

| Risco | Mitigação |
|---|---|
| Cálculo lento com milhares de sessions | Faz uma única passada por session, cache de tokenização |
| Co-occurrence ruidoso (palavras random pareadas) | Filtro `minCount=3` + PMI score ranqueia pares semanticamente fortes |
| Bigrams dominados por stopwords sobrando | Já filtramos stopwords no Tokenize() |
| TUI fica muito densa | Mostra só top N reduzido; Web mostra mais |

# claude-history — Roadmap & Assessment

Avaliação de um brief externo sobre próximos passos. Validado contra o
código real em 2026-05-01.

## O que o brief errou

- **"77 commits"** — são 79.
- **"F8 pendente"** — F8 (MCP) já tá implementada. Tem
  `internal/mcp/{server,protocol,install}.go` + `mcp_tools.go` com 8 tools.
- **"`internal/index/migrations/002_bitemporal.sql`"** — esse path nem existe;
  o schema é inline em `sqlite.go`. Migration system não existe ainda.
- **"lógica duplicada chat.go:108-132 e similarity.go"** — meio mítico; tem
  `cosineSimChat` em `chat.go:246` e `similarity.go` separados, mas não é
  duplicação 1:1. Refactor pra `internal/search/hybrid.go` faz sentido **só
  se** RRF for adicionado, não preventivamente.
- **"continueSession via tea.ExecProcess"** — já funciona, mas usa abordagem
  diferente (`tea.Quit` + `exec.Command` no `main.go`). Comment em
  `app.go:72` explica que ExecProcess teve race com TTY no claude. Sugestão
  ignora essa decisão já tomada.

## Confirmado que é verdade

- Schema atual: `sessions, tool_uses, messages_fts, ai_cache, ai_insights,
  ai_profile, session_knowledge` (sem migration system).
- 8 MCP tools: `similar, search, ask, insights, knowledge, aggregated,
  project, standup`.
- `tui/threads.go` 1967 linhas, 6 views.
- Falta forks/sidechains (`parentUuid`, `isSidechain` do JSONL não são
  extraídos).
- F9 (launchd + menu bar) realmente pendente.

---

## Tier S — implementar logo

### S.1 — Forks/sidechains nativo
**Gap real, fácil.** JSONL já tem `parentUuid` e `isSidechain`.
Extrai em parse-time, exibe como sub-tree no Threads tab. Diferencial óbvio
vs ccusage/claude-mem.

```go
type Turn struct {
    ParentUUID  string  // novo
    IsSidechain bool    // novo
    // ...
}

type ThreadSession struct {
    *model.Session
    GapFromPrev time.Duration
    Kind        string
    Forks       []*ForkBranch  // sub-trees de Task spawns
}
```

### S.2 — `tool_events` + loop detection
Adiciona 1 tabela `(session_id, ts, tool_name, input_hash)` com SHA-256 do
`tool_use.input`. Habilita detecção retroativa + telemetria de retrabalho.
Custo baixo, valor alto.

```sql
CREATE TABLE tool_events (
    session_id TEXT NOT NULL,
    ts INTEGER NOT NULL,
    tool_name TEXT NOT NULL,
    input_hash TEXT NOT NULL,
    PRIMARY KEY (session_id, ts, tool_name)
) STRICT;
```

Loop detection: `group by (session_id, input_hash) having count>=3 AND
maxts-mints<60s`.

### S.3 — MCP tool descriptions revistas
Anthropic recomenda 80-150 palavras com exemplos. Template:
- propósito (o que faz, em 1 frase)
- quando usar
- exemplos (1-2)
- quando NÃO usar
- limites (max items, formatos retornados)

Cap. 24 do livro: <50 palavras causa 30% erro de seleção, >150+exemplos cai
pra <5%.

---

## Tier A — próximas Phases

### A.4 — RRF híbrido
Extrai score combinando FTS rank + dense rank. ~30 linhas, melhora recall
mensuravelmente. Tipo de query (regex pra identifier vs prosa) routing é
cereja.

```go
const (rrfK = 60; finalTop = 10)

func rrfMerge(bm25 []SearchResult, dense []SimilarResult) []Hit {
    scores := map[string]*Hit{}
    add := func(id, src string, rank int) {
        h, ok := scores[id]
        if !ok { h = &Hit{SessionID: id}; scores[id] = h }
        h.Score += 1.0 / float64(rrfK+rank)
        h.Sources = append(h.Sources, src)
    }
    for i, r := range bm25  { add(r.SessionID, "bm25",  i+1) }
    for i, r := range dense { add(r.SessionID, "dense", i+1) }
    out := make([]Hit, 0, len(scores))
    for _, h := range scores { out = append(out, *h) }
    sort.Slice(out, func(i,j int) bool { return out[i].Score > out[j].Score })
    if len(out) > finalTop { out = out[:finalTop] }
    return out
}
```

Routing por tipo:
```go
var identRe = regexp.MustCompile(`[A-Z][a-z]+[A-Z]|_[a-z]|::|\.[a-z]+\(`)

func detectQueryType(q string) QueryType {
    if identRe.MatchString(q) { return BM25Heavy }
    if len(strings.Fields(q)) >= 6 { return DenseHeavy }
    return Hybrid
}
```

### A.5 — `session_files` + `resolved_at_turn`
Habilita Studio Meta tab (cost per feature, retrabalho, convergence).
2 colunas/tabelas baratas.

```sql
CREATE TABLE session_files (
    session_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    PRIMARY KEY (session_id, file_path)
) STRICT;

ALTER TABLE sessions ADD COLUMN resolved_at_turn INTEGER;
```

Studio Meta tab — 6 charts SQL-driven:

| Métrica           | Query SQL                                                                          | Insight                                |
|-------------------|------------------------------------------------------------------------------------|----------------------------------------|
| Convergence speed | `SELECT skill, percentile(resolved_at_turn, 0.5) FROM sessions GROUP BY skill`     | Turnos até primeira solução por skill  |
| Token efficiency  | `SUM(success)/SUM(tokens)*1e6`                                                     | Skills mais eficientes                 |
| Retrabalho rate   | self-join `session_files` em janela 7d                                             | % sessões que reabrem mesmo arquivo    |
| Cost per feature  | `REGEXP_EXTRACT(git_branch, 'CC-\d+')`                                             | Custo agregado por ticket Jira         |
| Loop detection    | `tool_events group by (session_id, input_hash) having count>=3 AND maxts-mints<60s`| Agente repetindo                       |
| Drift mensal      | `strftime('%Y-%m', start_time)` time-series                                        | Workflow melhorando ou piorando?       |

### A.6 — F9 menu bar
ROI alto pro user (visão passiva enquanto Claude roda em outro terminal).

Stack:
- `~/Library/LaunchAgents/com.felipe-coelho.claude-history.plist` (sem sudo,
  JumpCloud-friendly)
- bind `127.0.0.1:7531` + socket Unix `/tmp/claude-history.sock`
- `getlantern/systray` pra menu bar
- `osascript` pra notifications com batching (max 1/30s por AlertKey)

Detectores (goroutines lendo do JSONL tail):

| Detector          | Sinal                                  | Janela     | Limiar      |
|-------------------|----------------------------------------|------------|-------------|
| loopDetector      | tool.name + input_hash                 | 60s        | ≥3          |
| costSpikeDetector | sessão vs mediana últimas 20 same-skill| sessão     | >2×         |
| latencyDetector   | p95 vs baseline 7d                     | 1h rolling | desvio >50% |

---

## Tier B — avaliar quando bater necessidade

### B.7 — Bi-temporal facts table
Bonito academicamente, mas é Phase inteira só pra isso. Prematuro até ter
use-case concreto que `session_knowledge` não resolve.

Quando bater: build `entities + facts + reflections` com tiers raw →
consolidada → semântica e reflection job nightly via Ollama.

### B.8 — Reflection job
Só faz sentido depois de B.7.

Cluster por `(subject, predicate)`, threshold ≥3 episódios, dedup por
`prompt_hash` ANTES do LLM (evita custo redundante), Ollama
`qwen2.5:7b` com prompt structured. Importance via heurística:

```go
func scoreHeuristic(f Fact, eps []Episode) float64 {
    base := 0.3 + 0.1*math.Log1p(float64(len(eps)))
    if f.Predicate == "DECIDED" || f.Predicate == "PREFERS" { base += 0.2 }
    if hasCrossProject(eps) { base += 0.15 }
    return math.Min(base, 1.0)
}
```

### B.9 — Contextual Retrieval
Promissor mas precisa benchmark antes (é caro re-embeddar tudo). Adiciona
coluna `context_text` em `ai_cache`. Job batch pré-processa cada session
com prompt local Haiku-equivalente (`qwen2.5:3b` ou `llama3.2`) → 80-100
tokens situacionais. Re-embedar uma vez, guardar.

---

## Tier C — descartar

- **Sugiyama/dagre layout** — overkill. Tree simples já funciona.
- **ML refinement opt-in** (`--smart-threads`) — hipótese sem evidência de
  que melhora versus heurística atual de gap.
- **bge-reranker-v2-m3** — não tá no Ollama, ONNX puro Go é trabalho
  não-trivial pra +5pp nDCG. Adicionar quando RRF baseline tiver telemetria
  de ruído mensurável.
- **3 correções no BuildThreads (cwd moda, branch retroativa via reflog,
  ML)** — premature optimization sem bug report concreto.

---

## Plano de execução

### Phase 12 — Forks & Telemetria (Tier S)

1. **S.1** — Extrai `parentUuid`/`isSidechain` no parser → renderiza forks
   no Threads tab.
2. **S.2** — Tabela `tool_events` populada via parser → habilita loop
   detection retroativo.
3. **S.3** — Revisa descriptions dos 8 MCP tools.

### Phase 13 — Search & Meta (Tier A)

4. **A.4** — RRF merge entre FTS + similarity (única função, sem virar
   microservice). Routing por query type.
5. **A.5** — `session_files` + `resolved_at_turn` + Studio Meta tab.
6. **A.6** — F9: launchd + menu bar + detectores + notifications.

### Phase 14+ — Memória semântica (Tier B, sob demanda)

Só quando bater use-case concreto que justifique a complexidade.

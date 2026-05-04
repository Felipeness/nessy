# Domain (Phase 3 — Decoder)

## Glossary

### Session
Uma conversa unitária com Claude Code, persistida como JSONL em
`~/.claude/projects/<encoded-project>/<uuid>.jsonl`. Cada linha é um turno
(user/assistant/tool_use/tool_result/system).
🟢 `internal/model/session.go`, `internal/parser/jsonl.go`

### Thread
Sequência cronológica de sessions que compartilham `(project_dir, branch)` e
estão temporalmente próximas (gap < `gap` config, default 30min). Representa
"trabalho contínuo" mesmo que partido em N sessions.
🟢 `internal/stats/threads.go:BuildThreads`

### Gap
Intervalo de tempo entre fim de uma session e início da próxima do mesmo
(project, branch). Usado pra agrupar/quebrar threads.
🟢 default: `30 * time.Minute` em `tui/threads.go:74` `newThreadsView`

### Kind (de session)
Classifica a session em:
- `normal` — execução standalone 🟢
- `compact` — session foi compactada por hit-context (icon ↻) 🟢
- `resumed` — continuou de uma session anterior via `--resume` 🟢
🟢 `internal/parser/jsonl.go` (extraído do JSONL header/metadata)

### Sidechain
Sub-agentes disparados via Task tool. Cada sub-agent tem seu próprio fluxo de
mensagens dentro da session pai. `SidechainTurns` conta turnos sidechain;
`SidechainAgents` conta agentes únicos. Visível no TUI como pílula `↳ N subs`.
🟢 `internal/model/session.go`

### Knowledge
Estrutura extraída por AI de uma session: `problem`, `solution`, `decisions`
(com rationale), `learnings`, `tech_used`, `open_questions`. Persistida em
`knowledge` table. Usada pra RAG, aggregation, standup gen.
🟢 `internal/index/sqlite.go:Knowledge` + `internal/ai/knowledge.go`

### Insight
Output do advisor — anti-pattern detectado, sugestão de melhoria. Tem `kind`
(token_waste / loop / retrabalho / etc) e `severity`.
🟢 `internal/index/sqlite.go:Insight`, `internal/ai/insights.go`

### Profile
Perfil do user gerado por AI sumarizando seu estilo de trabalho (idiomas
preferidos, anti-patterns recorrentes, áreas de força). Single-blob TEXT.
🟢 `internal/index/sqlite.go:ProfileGet/Set`

### Cluster
Grupo de sessions semanticamente similares (KMeans sobre embeddings). Cada
session pode pertencer a um cluster com label gerada por AI.
🟢 `internal/ai/clustering.go`, `aiCache.cluster + cluster_label`

### Tier / Cohort / User
N/A — não existem. Nessy é single-user local-only. Termos típicos de SaaS
não se aplicam. Não há permissões nem auth.

### Project Dir
Encoded path do projeto onde Claude rodou — slashes substituídos por dashes
(Claude convention). Decoded back via `parser.DecodeProjectDir`.
🟢 `internal/parser/jsonl.go:DecodeProjectDir`

### Atlas
Output dir do skill `/nessy` — `_nessy_atlas/`. Coleção de mapas (inventory,
code-analysis, dependencies, domain, state-machines, c4, erd, specs).
🟢 NOVO em `commit aea07f6`

---

## Business rules

### BR-001: Session filtering (ingest)
Antes de indexar, sessions podem ser puladas se:
- `IsWarmup(s)` — primeiro turn é só `<system-reminder>` ou setup boilerplate 🟢
  `internal/parser/jsonl.go:IsWarmup`
- `IsClearOnly(s)` — session inteira foi `/clear` sem mensagens reais 🟢
- `MessageCount < cfg.Ingest.MinMessages` (default não setado) 🟢
- Path matches `cfg.Ingest.ExcludeProjects` glob 🟢
- Caminho contém `/subagents/` (sub-agent JSONL ignorado — pertence à session pai) 🟢
  `internal/index/reindex.go:94`

🟢 `internal/index/reindex.go:filter.shouldSkip(s)`

### BR-002: Thread merging by gap
Sessions adjacentes do mesmo (project_dir, branch) são merged em um Thread
se `next.start_time - prev.end_time < gap` (default 30min). Se passa do gap,
nova thread.
🟢 `internal/stats/threads.go:BuildThreads`

### BR-003: Cost calculation
Custo USD por session = sum de (tokens × price_per_token) por modelo, por kind
de token (input/output/cache_create/cache_read). Pricing carregado de
`pricing.toml` (modelo → 4 prices).
🟢 `internal/pricing/pricing.go`

🟡 Pricing.toml é gerado automaticamente em `<cacheDir>/pricing.toml` se não
existir, com valores default hardcoded em `defaultPricingTOML` (root). Updates
manuais sobrevivem a re-runs. 🟢 `main.go:388-392`

### BR-004: BRL conversion
Custos exibidos em USD + BRL via `pricing.BRLRate`. Default rate hardcoded;
não fetched live. 🟡 user precisa atualizar manualmente em pricing.toml
quando câmbio se move.

### BR-005: Loop detection
Detecta calls Bash repetidas com mesmo input hash dentro de janela. Default:
`minCount=3, windowSecs` config. Marca como insight `kind=loop`.
🟢 `internal/index/sqlite.go:DetectLoops`

### BR-006: FTS reindex on parser_version change
Se `parser_version` constant muda, FTS é truncado e re-indexado. Garante que
schema mudanças (ex: nova column `sidechain_turns`) sejam back-filled.
🟢 `internal/index/reindex.go:73-85`

### BR-007: Health check timeout
Toda chamada Ollama Health usa `context.WithTimeout(2*time.Second)`. Resultado
agora cacheado em `aiView.reachable`, atualizado por `aiHealthTickCmd` a cada
30s — não bloqueia render.
🟢 `internal/ai/ollama.go:34`, `tui/ai.go` (cache), `tui/app.go` (handler)

### BR-008: Async startup ingest
Reindex roda async via `tea.Cmd` no `Init()` do TUI Model — não bloqueia
primeiro paint. Cache do SQLite mostra dados imediatos, refresh chega depois.
🟢 `commit f7ea99f`, `tui/app.go:Init`

### BR-009: Skill install scope
`nessy install` (sem flag) instala project-local em `.claude/skills/` ou
`.agents/skills/`. `--global` instala em `~/.claude/skills/` (Claude Code only).
Entry file (CLAUDE.md/AGENTS.md) só é criado se NÃO existe — nunca sobrescreve
customização do user.
🟢 `cmd_install.go:installEngine`

---

## Invariants

- 🟢 **INV-1**: Toda session em DB tem JSONL file correspondente em disk. Reindex
  remove orphans (sessions sem file).
  Enforced: `internal/index/reindex.go:163-179` (stale removal loop)
- 🟢 **INV-2**: `messages_fts` table é populada se e somente se `messages` table
  é populada pra mesma `session_id`. Reindex re-popula FTS quando count=0.
  Enforced: `internal/index/reindex.go:114-122`
- 🟢 **INV-3**: `aiCache.embedding` blob, se presente, decodifica pra `[]float32`
  com tamanho determinado pelo embed model. Sem checksum — confia no encoder/decoder.
  Enforced: `internal/ai/floatbits.go:EncodeEmbedding/DecodeEmbedding`
- 🟡 **INV-4**: `Session.SessionID` é UUID válido. Não validado explicitamente
  — confia no Claude Code naming.
- 🟡 **INV-5**: `Thread.Sessions` é cronologicamente ordenado (start_time asc).
  Garantido pelo `BuildThreads` mas não enforced em mutações posteriores.

---

## Open questions → `questions.md`

- 🔴 Q1: Onde está hardcoded `parserVersion`? Grep não acha em `internal/index/`
  nem em `internal/parser/`. Pode estar em algum outro file.
- 🔴 Q2: `cfg.Ingest.MinMessages` tem default explícito? Aparenta ser 0 (zero
  value), mas deveria ser ≥1 pra evitar warmup-only sessions.
- 🟡 Q3: Comportamento de re-install — `nessy install` em diretório que já tem
  `.claude/skills/nessy/` faz overwrite ou skip? Atualmente overwrite (porque
  `os.Create` no `copyEmbeddedSkill` truncate). Ok, mas não documentado.

# AI Insights — Implementation Plan (Fase 5.1)

> Inline execution. Sub-skill: superpowers:executing-plans.

**Goal:** Adicionar advisor proativo (insights + profile) sobre Fase 5, integrar resumos no preview de Recent.

**Spec:** [`../specs/2026-04-29-ai-insights-design.md`](../specs/2026-04-29-ai-insights-design.md)

## Tasks

### Task 108: Schema + helpers SQLite

**Files:** Modify `internal/index/sqlite.go`

- [ ] Adiciona `ai_insights` e `ai_profile` no schema.
- [ ] Métodos: `InsightsList`, `InsightsReplaceAll(insights)`, `InsightForSession(sid)`, `ProfileGet`, `ProfileSet`.
- [ ] Commit `feat: tabelas ai_insights e ai_profile com CRUD`.

### Task 109: Generator de insights

**Files:** Create `internal/ai/insights.go`

- [ ] `GenerateInsights(ctx, db, client, model) ([]Insight, error)`.
- [ ] Carrega summaries + tools + behavioral, monta prompt agregado, parse JSON resposta.
- [ ] Persist via `InsightsReplaceAll`.
- [ ] Commit `feat: generator de insights via LLM com prompt agregado`.

### Task 110: Generator de profile

**Files:** Modify `internal/ai/insights.go`

- [ ] `GenerateProfile(ctx, db, client, model) (string, error)`.
- [ ] Prompt textual baseado em summaries + insights existentes.
- [ ] Persist via `ProfileSet`.
- [ ] Commit `feat: generator de personal profile`.

### Task 111: REST endpoints

**Files:** Modify `internal/server/handlers.go`

- [ ] `/api/ai/insights` GET, `/api/ai/insights/generate` POST.
- [ ] `/api/ai/profile` GET, `/api/ai/profile/generate` POST.
- [ ] SSE broadcast `insights_done` e `profile_done`.
- [ ] Commit `feat: endpoints insights e profile com SSE`.

### Task 112: Frontend tipos + api

**Files:** Modify `web/src/types.ts`, `web/src/api.ts`

- [ ] Adiciona `Insight`, `Profile` em types.
- [ ] Adiciona `aiInsights`, `aiInsightsGenerate`, `aiProfile`, `aiProfileGenerate` em api.
- [ ] Commit `feat: tipos e client de insights/profile`.

### Task 113: Recent preview com summary

**Files:** Modify `web/src/components/SessionRow.tsx`, `web/src/tabs/RecentTab.tsx`, `tui/recent.go`, `tui/row.go`

- [ ] Web: SessionRow aceita prop `summary?` que substitui `first_user_msg`.
- [ ] Web: RecentTab fetch /api/ai/summaries e passa pra rows.
- [ ] TUI: similar — recent.go busca AICache e usa summary se existir.
- [ ] Commit `feat: Recent preview mostra resumo AI quando disponivel`.

### Task 114: AI tab com Insights + Profile

**Files:** Modify `web/src/tabs/AITab.tsx`, `tui/ai.go`

- [ ] Web: novas seções "💡 Insights" (cards por type) e "🧠 Profile" (texto + botão copiar).
- [ ] Web: botões "Gerar insights" / "Gerar profile" + SSE listeners.
- [ ] TUI: idem em texto, comandos `I` e `P` no app.go.
- [ ] Commit `feat: tab AI ganha Insights e Profile sections`.

### Task 115: DetailPanel mostra padrão

**Files:** Modify `web/src/components/DetailPanel.tsx`, `tui/detail.go`

- [ ] Se selected.session_id aparece em algum insight, mostra card.
- [ ] Commit `feat: DetailPanel exibe padrao detectado da session`.

### Task 116: Smoke + push

- [ ] Build + smoke + push.
- [ ] Commit `chore: smoke fase 5.1`.

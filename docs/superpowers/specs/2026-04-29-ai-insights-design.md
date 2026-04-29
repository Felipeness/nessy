---
title: AI Insights & Personalization — Fase 5.1
status: approved
date: 2026-04-29
phase: 5.1
parent: 2026-04-29-ai-profiling-design
---

# AI Insights & Personalization — Design

## Goals

Estender Fase 5 (busca/summaries/clusters mantidos) com **advisor proativo** que:

1. Detecta tarefas repetitivas → sugere skills
2. Detecta problemas crônicos → flagra bugs recorrentes
3. Detecta sequências de comandos → sugere aliases/scripts
4. Mantém **personal profile** ("um pouco de mim") — perfil textual atualizado periodicamente, injetável em outros prompts
5. Integra resumo da IA no preview de Recent + DetailPanel (polish faltante da F5)

## Non-goals

- Substituir busca/clusters/similar (continuam intactos)
- Auto-gerar skills (sugere, não cria)
- Profile compartilhável online (continua local)

## Insight types

| type | Exemplo |
|---|---|
| `repeated_task` | "instalei CLI via mise 8x → skill `install-cli`" |
| `chronic_problem` | "hook design-first deu falso positivo 4x" |
| `script_opportunity` | "git add+commit+push em 70% sessions → alias `gp`" |
| `personal_pattern` | "você prefere TUI > Web; design-first; commits curtos" |

## Backend

### Schema

```sql
CREATE TABLE IF NOT EXISTS ai_insights (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  evidence TEXT,           -- JSON array de session_ids ou refs
  suggested_action TEXT,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_profile (
  id INTEGER PRIMARY KEY,  -- sempre 1, single-row
  content TEXT NOT NULL,
  generated_at INTEGER NOT NULL
);
```

### Geração de insights

Função `GenerateInsights(ctx, db, client, model)`:

1. Carrega: todos summaries, top words/bigrams, top tools, top error_words, prefixes
2. Monta prompt agregado com TODA essa info
3. LLM gera JSON com array de insights (5-10 cards)
4. Limpa tabela, INSERT em massa
5. Broadcast SSE `insights_done`

Prompt template:
```
Você é um advisor analisando o histórico de uso de Claude Code de um dev brasileiro.

CONTEXTO:
- {N} sessions resumidas:
{summaries_list}

- Tools mais usados (top 10):
{tools_list}

- Palavras frequentes:
{words_list}

- Sinais de erro/retrabalho (% por session):
{errors_summary}

GERE 5-10 INSIGHTS úteis em formato JSON estrito (array de objetos), categorizando como:
- repeated_task — tarefas que aparecem 3+ vezes
- chronic_problem — bugs/erros que voltam
- script_opportunity — sequências repetitivas que viram alias
- personal_pattern — preferências de estilo/workflow

Cada insight com {type, title, description, evidence (string com session_ids ou stats), suggested_action}.

RESPONDA SÓ COM O JSON, SEM MARKDOWN.
```

### Geração de profile

Função `GenerateProfile(ctx, db, client, model)`:

1. Carrega summaries + insights + behavioral stats
2. Prompt: "Resuma quem é esse usuário em parágrafos curtos: stack preferida, workflow, anti-patterns evitados, frustrações recorrentes."
3. Salva em `ai_profile`
4. Acessível via `GET /api/ai/profile` pra **injetar em outros prompts**

## REST endpoints

| Endpoint | Método | Retorno |
|---|---|---|
| `/api/ai/insights` | GET | array de Insight |
| `/api/ai/insights/generate` | POST | 202 (worker enqueue) |
| `/api/ai/profile` | GET | `{content, generated_at}` |
| `/api/ai/profile/generate` | POST | 202 |

SSE events: `insights_done`, `profile_done`.

## Frontend

### Recent preview integration

`SessionRow` recebe `summary` opcional. Se tem, usa `summary` em vez de `first_user_msg`. Mesma fonte de dados — só fetcha do `/api/ai/summaries` e mapeia `session_id → summary`.

### DetailPanel — seção "🔍 Padrão"

Se a session selecionada participa de algum insight, mostra o card:
```
🔍 Padrão detectado: "Você instalou CLI via mise 8x"
   Sugestão: criar skill `install-cli`
```

### Tab AI — novas seções

Após "Clusters", adiciona:

**💡 Insights**: cards organizados por type, com badge colorido:
```
💡 [repeated_task] "Você instalou CLI via mise 8x"
   Sessions: a007e9ad, 6df22c8d, …
   Sugestão: criar skill install-cli
```

**🧠 Personal profile**: parágrafo do perfil + botão "📋 Copiar" pra pegar o texto e injetar em outros prompts (ChatGPT, Claude.ai, etc).

Botões topo: "🧠 Gerar insights" / "🧠 Gerar profile".

## TUI

`tui/ai.go` ganha:
- Seção "💡 Insights" (lista textual)
- Seção "🧠 Profile" (parágrafo)
- Comando `I` dispara generate insights, `P` dispara profile

## Critérios de aceitação

- [ ] Insights gerados via LLM call com contexto agregado
- [ ] Profile gerado em parágrafos descrevendo o user
- [ ] Recent mostra summary no preview (substitui first_user_msg)
- [ ] DetailPanel mostra padrão quando aplicável
- [ ] Tab AI tem seções Insights + Profile + botões
- [ ] SSE `insights_done` / `profile_done` atualizam UI live
- [ ] Endpoint `/api/ai/profile` retorna texto puro pra injection externa

## Riscos

| Risco | Mitigação |
|---|---|
| LLM retorna JSON inválido | Parser tolerante, retry 1x, fallback `[]` |
| Insights ruins com poucos dados | Threshold mínimo: 5 sessions; abaixo disso mostra "precisa mais histórico" |
| Profile longo demais (> 1k tokens) | Cap em ~500 palavras |
| Contexto LLM estoura (8k tokens) | Truncar summaries pra <100 chars cada |

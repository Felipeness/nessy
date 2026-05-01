package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/felipeness/nessy/internal/index"
	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/parser"
)

// knowledgePromptPT é o prompt que extrai o "segundo cérebro" — problema,
// solução, decisões, learnings, padrões e tech a partir de uma session.
const knowledgePromptPT = `Você é um analista do histórico de desenvolvimento de um dev brasileiro.
Extraia em JSON ESTRITO o conhecimento útil dessa session do Claude Code.

CONTEXTO DA SESSION:
%s

PRIMEIRAS USER MSGS (intenção inicial):
%s

ÚLTIMAS USER MSGS (estado final):
%s

TOOLS USADOS (top 8):
%s

EXTRAIA o JSON com este SCHEMA EXATO:
{
  "problem": "1 frase — o que o dev estava tentando resolver/fazer",
  "solution": "1-2 frases — o que funcionou ou o estado final alcançado",
  "decisions": [
    {"decision": "decisão tomada", "rationale": "por quê"}
  ],
  "learnings": ["insight ou aha-moment 1", "insight 2"],
  "code_patterns": ["padrão de código usado/aprendido (ex: 'error wrapping com %%w em Go')"],
  "tech_used": ["Go", "TypeScript", "Tailwind", ...],
  "open_questions": ["coisa que ficou em aberto pra resolver depois"]
}

REGRAS:
- Seja específico e útil, não genérico ("usou git" → ruim; "rebase --onto pra mover commits entre branches" → bom)
- Se não houver decisions/learnings/code_patterns relevantes, devolve array vazio []
- tech_used: APENAS o que aparece nas msgs ou tools, nunca invente
- problem e solution sempre preenchidos (se não dá pra extrair, escreva "não identificado")
- Idioma: pt-BR
- RESPONDA SÓ COM JSON, SEM MARKDOWN, SEM TEXTO AO REDOR

Exemplo de response esperada:
{"problem":"...","solution":"...","decisions":[{"decision":"...","rationale":"..."}],"learnings":["..."],"code_patterns":["..."],"tech_used":["Go"],"open_questions":[]}`

// rawKnowledge é o shape JSON que o LLM devolve.
type rawKnowledge struct {
	Problem       string             `json:"problem"`
	Solution      string             `json:"solution"`
	Decisions     []rawDecision      `json:"decisions"`
	Learnings     []string           `json:"learnings"`
	CodePatterns  []string           `json:"code_patterns"`
	TechUsed      []string           `json:"tech_used"`
	OpenQuestions []string           `json:"open_questions"`
}

type rawDecision struct {
	Decision  string `json:"decision"`
	Rationale string `json:"rationale"`
}

// GenerateKnowledge produz e persiste o Knowledge pra uma session específica.
// Reusa o summary do AICache se já existe (evita re-summarizar a JSONL toda).
// Pula geração se Knowledge já existe e jsonl_mtime bate com a session atual.
func GenerateKnowledge(ctx context.Context, db *index.DB, client *Client, genModel string, sess *model.Session) (*index.Knowledge, error) {
	// Skip se cache válido
	if existing, err := db.KnowledgeGet(sess.SessionID); err == nil && existing != nil {
		if existing.JSONLMtime == sess.JSONLMtime.UnixNano() {
			return existing, nil
		}
	}

	// Reaproveita summary se temos
	summary := ""
	if c, err := db.AICacheGet(sess.SessionID); err == nil && c != nil {
		summary = c.Summary
	}
	if summary == "" {
		summary = fmt.Sprintf("Session em %s, branch %s, %d msgs",
			sess.ProjectDir, sess.GitBranch, sess.MessageCount)
	}

	// Pega user msgs
	msgs, err := parser.ParseMessages(sess.JSONLPath)
	if err != nil {
		return nil, fmt.Errorf("parse messages: %w", err)
	}
	var userMsgs []string
	for _, m := range msgs {
		if m.Role == "user" {
			t := strings.TrimSpace(m.Content)
			if t != "" && !strings.HasPrefix(t, "<") {
				userMsgs = append(userMsgs, t)
			}
		}
	}
	first, last := slice5(userMsgs, true), slice5(userMsgs, false)

	// Top tools
	type kv struct {
		k string
		v int
	}
	var pairs []kv
	for k, v := range sess.ToolCalls {
		pairs = append(pairs, kv{k, v})
	}
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j].v > pairs[j-1].v; j-- {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
		}
	}
	var toolsB strings.Builder
	for i, p := range pairs {
		if i >= 8 {
			break
		}
		fmt.Fprintf(&toolsB, "- %s: %d\n", p.k, p.v)
	}

	prompt := fmt.Sprintf(knowledgePromptPT,
		summary, formatMsgs(first), formatMsgs(last), toolsB.String())

	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	out, err := client.GenerateLong(cctx, genModel, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}

	raw, err := parseKnowledgeJSON(out)
	if err != nil {
		return nil, fmt.Errorf("parse: %w (raw: %s)", err, truncStr(out, 400))
	}

	k := &index.Knowledge{
		SessionID:     sess.SessionID,
		JSONLMtime:    sess.JSONLMtime.UnixNano(),
		Problem:       raw.Problem,
		Solution:      raw.Solution,
		Decisions:     mustJSON(raw.Decisions),
		Learnings:     mustJSON(raw.Learnings),
		CodePatterns:  mustJSON(raw.CodePatterns),
		TechUsed:      mustJSON(raw.TechUsed),
		OpenQuestions: mustJSON(raw.OpenQuestions),
		GeneratedAt:   time.Now().Unix(),
	}
	if err := db.KnowledgeUpsert(k); err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}
	return k, nil
}

// slice5 devolve as primeiras (head=true) ou últimas (head=false) 5 strings.
func slice5(xs []string, head bool) []string {
	const n = 5
	if len(xs) <= n {
		return xs
	}
	if head {
		return xs[:n]
	}
	return xs[len(xs)-n:]
}

// formatMsgs junta msgs com "- " prefix e tronca cada uma a 200 chars.
func formatMsgs(xs []string) string {
	if len(xs) == 0 {
		return "(nenhuma)"
	}
	var b strings.Builder
	for _, x := range xs {
		t := x
		if len(t) > 200 {
			t = t[:200] + "…"
		}
		fmt.Fprintf(&b, "- %s\n", t)
	}
	return b.String()
}

// parseKnowledgeJSON aceita JSON puro, code-fence, ou texto antes/depois.
func parseKnowledgeJSON(s string) (*rawKnowledge, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i > 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var k rawKnowledge
	if err := json.Unmarshal([]byte(s), &k); err != nil {
		return nil, err
	}
	return &k, nil
}

// mustJSON serializa, devolve "[]" em caso de erro (campo não-nullable).
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return "[]"
	}
	return string(b)
}

// GenerateKnowledgeAll roda em background pra todas sessions sem cache atual.
// Devolve número gerado, número que já estava em cache, e erros (mas não falha).
func GenerateKnowledgeAll(ctx context.Context, db *index.DB, client *Client, genModel string) (generated, cached int, lastErr error) {
	sessions, err := db.ListSessions()
	if err != nil {
		return 0, 0, err
	}
	for _, sess := range sessions {
		select {
		case <-ctx.Done():
			return generated, cached, ctx.Err()
		default:
		}
		existing, _ := db.KnowledgeGet(sess.SessionID)
		if existing != nil && existing.JSONLMtime == sess.JSONLMtime.UnixNano() {
			cached++
			continue
		}
		if _, err := GenerateKnowledge(ctx, db, client, genModel, sess); err != nil {
			lastErr = err
			continue
		}
		generated++
	}
	return generated, cached, lastErr
}

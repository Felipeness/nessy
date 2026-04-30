package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/felipeness/claude-history/internal/index"
)

// ChatMsg é uma mensagem da conversa user↔Ness IA. Espelha o shape do Ollama
// /api/chat (mesma struct ChatMessage), mas com tag json em snake pra REST.
type ChatMsg struct {
	Role    string `json:"role"`    // "user" | "assistant" | "system"
	Content string `json:"content"`
}

// ChatSource é uma session_id citada como fonte da resposta. A UI mostra
// embaixo da resposta como pílulas clicáveis.
type ChatSource struct {
	SessionID  string  `json:"session_id"`
	Similarity float64 `json:"similarity"`
	Summary    string  `json:"summary"`
	Snippet    string  `json:"snippet"`
}

// ChatResponse é o que o handler /api/ai/chat retorna.
type ChatResponse struct {
	Response string       `json:"response"`
	Sources  []ChatSource `json:"sources"`
}

const chatSystemPrompt = `Você é "Ness IA" — o segundo cérebro técnico do Luis Felipe Coelho.

Sua ÚNICA fonte de verdade são as FONTES abaixo (sessions passadas dele). Você NÃO tem permissão de usar conhecimento geral.

REGRAS RÍGIDAS — não negociáveis:

1. RESPOSTA SÓ COM BASE NAS FONTES. Se a info está nas fontes, sintetize. Se não está, responda EXATAMENTE: "Não encontrei isso no seu histórico. As sessions mais próximas que achei foram [sid1] [sid2] mas não tratam disso." E PARE. Não complete com conhecimento geral.

2. NUNCA dê tutorial genérico. NUNCA explique conceitos do zero. NUNCA sugira "passos comuns" tipo "instale o Docker Desktop" se isso não está na fonte. Felipe é senior, ele já sabe — ele quer saber O QUE ELE FEZ, não o que existe no mundo.

3. CITE session_ids em [bracket] ao usar uma fonte. Ex: "Você usa Docker via Colima [6df22c8d]." Sem citação = sem afirmação.

4. Se há conflito entre fontes (decisões opostas em sessions diferentes), mostre os 2 com session_ids.

5. Tom: pt-BR, direto, técnico. Sem rodeios, sem "talvez", sem "você poderia". Frases curtas.

6. Markdown leve OK (bullets), nunca H1/H2.

EXEMPLOS DE RESPOSTAS BOAS:
- "Docker no seu Mac roda via Colima (não Docker Desktop) — você instalou via mise sem sudo [6df22c8d]. Decisão: Docker Desktop precisava de admin, Colima não [6df22c8d]."
- "Não encontrei isso no seu histórico. Sessions próximas: [a8d4aa0c] [c7bd912a] mas tratam de outra coisa."

EXEMPLOS DE RESPOSTAS RUINS (NÃO FAZER):
- "Você pode instalar o Docker Desktop..." ❌ (genérico, sem fonte)
- "Geralmente devs fazem X..." ❌ (não é sobre o Felipe)
- Fechar com "se quiser mais detalhes me diga" ❌ (poluição)

FONTES (top %d sessions por similaridade — única coisa que você sabe):
%s`

// ChatWithContext faz RAG: embedda a última msg do user, busca top-K sessions
// por cosine similarity, monta um system prompt com o knowledge dessas
// sessions, manda pro LLM. Retorna a resposta + lista de fontes citadas.
func ChatWithContext(ctx context.Context, db *index.DB, client *Client, genModel, embedModel string, messages []ChatMsg) (*ChatResponse, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages")
	}
	last := messages[len(messages)-1]
	if last.Role != "user" {
		return nil, fmt.Errorf("last message must be from user")
	}

	// Embed query (timeout curto — embedding é rápido)
	embCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	queryEmb, err := client.Embedding(embCtx, embedModel, last.Content)
	if err != nil {
		return nil, fmt.Errorf("embedding: %w", err)
	}
	if len(queryEmb) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}

	// Pega caches com embedding e ranqueia
	caches, err := db.AICacheList()
	if err != nil {
		return nil, err
	}
	type ranked struct {
		cache *index.AICache
		sim   float64
	}
	var hits []ranked
	for _, c := range caches {
		if len(c.Embedding) == 0 {
			continue
		}
		emb := DecodeEmbedding(c.Embedding)
		s := cosineSimChat(queryEmb, emb)
		if s > 0 {
			hits = append(hits, ranked{c, s})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].sim > hits[j].sim })
	const topK = 8
	if len(hits) > topK {
		hits = hits[:topK]
	}

	// Monta sources + context block
	var sources []ChatSource
	var ctxB strings.Builder
	for _, h := range hits {
		snippet := buildKnowledgeSnippet(db, h.cache.SessionID)
		sources = append(sources, ChatSource{
			SessionID:  h.cache.SessionID,
			Similarity: h.sim,
			Summary:    h.cache.Summary,
			Snippet:    snippet,
		})
		sid := h.cache.SessionID[:8]
		fmt.Fprintf(&ctxB, "\n[%s] (similaridade: %.2f)\n", sid, h.sim)
		if h.cache.Summary != "" {
			fmt.Fprintf(&ctxB, "  resumo: %s\n", h.cache.Summary)
		}
		if snippet != "" {
			fmt.Fprintf(&ctxB, "  conhecimento extraído:\n%s", indent(snippet, "    "))
		}
	}
	if ctxB.Len() == 0 {
		// Sem fontes — chat sem RAG, ainda funciona mas avisa o LLM
		ctxB.WriteString("(nenhuma fonte similar encontrada no histórico)\n")
	}

	sysPrompt := fmt.Sprintf(chatSystemPrompt, len(hits), ctxB.String())

	// Conversa completa: system + histórico (limitado pros últimos 10 turnos
	// pra não estourar context)
	convo := []ChatMessage{{Role: "system", Content: sysPrompt}}
	startIdx := 0
	if len(messages) > 10 {
		startIdx = len(messages) - 10
	}
	for _, m := range messages[startIdx:] {
		convo = append(convo, ChatMessage{Role: m.Role, Content: m.Content})
	}

	chatCtx, cancel2 := context.WithTimeout(ctx, 120*time.Second)
	defer cancel2()
	answer, err := client.Chat(chatCtx, genModel, convo)
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}

	return &ChatResponse{Response: answer, Sources: sources}, nil
}

// buildKnowledgeSnippet monta um texto compacto com problem/solution/decisions/
// learnings/patterns de uma session, se ela tiver entry em session_knowledge.
// Vazio se não tem.
func buildKnowledgeSnippet(db *index.DB, sessionID string) string {
	k, err := db.KnowledgeGet(sessionID)
	if err != nil || k == nil {
		return ""
	}
	var b strings.Builder
	if k.Problem != "" {
		fmt.Fprintf(&b, "- problema: %s\n", k.Problem)
	}
	if k.Solution != "" {
		fmt.Fprintf(&b, "- solução: %s\n", k.Solution)
	}
	type rawDec struct {
		Decision  string `json:"decision"`
		Rationale string `json:"rationale"`
	}
	var decs []rawDec
	if err := json.Unmarshal([]byte(k.Decisions), &decs); err == nil {
		for _, d := range decs {
			fmt.Fprintf(&b, "- decisão: %s", d.Decision)
			if d.Rationale != "" {
				fmt.Fprintf(&b, " — %s", d.Rationale)
			}
			b.WriteByte('\n')
		}
	}
	var learnings, patterns, tech, open []string
	_ = json.Unmarshal([]byte(k.Learnings), &learnings)
	_ = json.Unmarshal([]byte(k.CodePatterns), &patterns)
	_ = json.Unmarshal([]byte(k.TechUsed), &tech)
	_ = json.Unmarshal([]byte(k.OpenQuestions), &open)
	for _, l := range learnings {
		fmt.Fprintf(&b, "- learning: %s\n", l)
	}
	for _, p := range patterns {
		fmt.Fprintf(&b, "- pattern: %s\n", p)
	}
	if len(tech) > 0 {
		fmt.Fprintf(&b, "- tech: %s\n", strings.Join(tech, ", "))
	}
	for _, q := range open {
		fmt.Fprintf(&b, "- em aberto: %s\n", q)
	}
	return b.String()
}

// indent prefixa cada linha de s com prefix. Útil pra hierarquia visual no
// system prompt.
func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n") + "\n"
}

// cosineSimChat — duplicado do server pra evitar import cycle. Move pra util
// shared depois se virar incômodo.
func cosineSimChat(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

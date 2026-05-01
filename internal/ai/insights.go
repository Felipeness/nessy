package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/felipeness/nessy/internal/index"
	"github.com/felipeness/nessy/internal/stats"
)

const insightsPromptPT = `Você é um advisor analisando o histórico de uso de Claude Code de um dev brasileiro.
Seu papel: detectar padrões e dar conselhos PRÁTICOS, incluindo economia de tokens, performance e anti-patterns.

CONTEXTO AGREGADO:

Sessions resumidas (%d):
%s

Tools mais usados:
%s

Palavras frequentes:
%s

Sinais de retrabalho/erros:
%s

Bigrams comuns:
%s

TAREFA: gere 6-12 INSIGHTS úteis em JSON estrito (array de objetos). Cada insight é um card pra mostrar ao dev. Categorias possíveis no campo "type":

- "repeated_task" — tarefas que aparecem 3+ vezes (ex: "instalou X 4 vezes" → vire skill)
- "chronic_problem" — bugs/erros que voltam (ex: "hook X falha em N sessions")
- "script_opportunity" — sequências de comandos que merecem alias/script
- "token_waste" — padrões que GASTAM tokens à toa. EXEMPLOS:
    - "você re-explica contexto X em cada session — vire CLAUDE.md"
    - "muitos Read+Read+Read em vez de Grep targeted"
    - "histórico longo sem /compact — context inflado"
    - "repete 'agora faça Y' depois de já dito — split em sub-tasks com fresh context"
- "performance_hint" — formas de melhorar velocidade/qualidade (ex: "use plan mode em tasks longas")
- "anti_pattern" — pior forma de usar (ex: "interrompe execução pra dar instrução conflitante" / "pede pra refazer sem dar contexto novo")
- "personal_pattern" — preferências de estilo/workflow do dev (ex: "prefere TUI sobre Web")

Pra "token_waste", "performance_hint" e "anti_pattern" — sempre dê EXEMPLO CONCRETO no description (cite session id ou trecho do que aconteceu) + ação específica em "suggested_action".

Cada insight com os campos:
- type: uma das categorias acima
- title: curta, ≤80 chars, declarativa (NÃO faça pergunta)
- description: 1-2 frases mostrando o padrão com exemplo (cita session id se aplicável)
- evidence: string com session ids, contagens, ou stats que sustentam
- suggested_action: 1 frase de ação concreta (não "considere X", mas "faça X")

RESPONDA SÓ COM O JSON ARRAY, SEM MARKDOWN, SEM TEXTO AO REDOR.

Exemplo de formato:
[{"type":"token_waste","title":"...","description":"...","evidence":"...","suggested_action":"..."}]`

const profilePromptPT = `Escreva um perfil em pt-BR desse dev com base nas evidências abaixo.

%sTECNOLOGIAS DETECTADAS (frequência de menções no histórico):
%s

TOOLS MAIS USADOS:
%s

PALAVRAS/BIGRAMS RECORRENTES:
%s

RESUMOS DAS SESSIONS:
%s

INSIGHTS DETECTADOS:
%s

REGRAS RÍGIDAS:
- Stack técnica: cite APENAS tecnologias que aparecem em "TECNOLOGIAS DETECTADAS". NUNCA invente ou infira (ex: não cite Ruby, Java, .NET se não estiverem listados).
- Se a seção "SOBRE O DEV" estiver presente acima, ela tem PRIORIDADE sobre o resto pra dados pessoais (nome, cargo, stack).
- Se faltar evidência pra alguma seção (ex: workflow), omita em vez de inventar.
- Não cite ferramentas de setup pessoal (Homebrew, mise, Hammerspoon) como "stack técnica" — elas vão na seção de ambiente, se mencionar.

ESTRUTURA do perfil (parágrafos curtos):
- Stack técnica (APENAS o que está em TECNOLOGIAS DETECTADAS)
- Workflow / metodologia (design-first, TDD, conventional commits, etc — só se houver evidência)
- Anti-patterns que evita
- Frustrações recorrentes
- Estilo de comunicação

Limite: ~300 palavras. Em pt-BR. Sem markdown. Tom direto e factual.`

// GenerateInsights monta prompt agregado, chama LLM, parseia JSON e persiste.
func GenerateInsights(ctx context.Context, db *index.DB, client *Client, genModel string) ([]*index.Insight, error) {
	caches, err := db.AICacheList()
	if err != nil {
		return nil, err
	}
	sessions, err := db.ListSessions()
	if err != nil {
		return nil, err
	}
	if len(sessions) < 3 {
		return nil, fmt.Errorf("histórico insuficiente (need ≥3 sessions, have %d)", len(sessions))
	}

	// Resumos
	var summariesB strings.Builder
	for _, c := range caches {
		if c.Summary == "" {
			continue
		}
		short := c.Summary
		if len(short) > 120 {
			short = short[:120] + "…"
		}
		fmt.Fprintf(&summariesB, "- %s: %s\n", c.SessionID[:8], short)
	}

	// Tools agregados
	toolFreq := map[string]int{}
	for _, s := range sessions {
		for t, n := range s.ToolCalls {
			toolFreq[t] += n
		}
	}
	type kv struct {
		k string
		v int
	}
	var toolPairs []kv
	for k, v := range toolFreq {
		toolPairs = append(toolPairs, kv{k, v})
	}
	sort.Slice(toolPairs, func(i, j int) bool {
		if toolPairs[i].v != toolPairs[j].v {
			return toolPairs[i].v > toolPairs[j].v
		}
		return toolPairs[i].k < toolPairs[j].k
	})
	var toolsB strings.Builder
	for i, p := range toolPairs {
		if i >= 10 {
			break
		}
		fmt.Fprintf(&toolsB, "- %s: %d\n", p.k, p.v)
	}

	// Words / bigrams / errors via stats package
	words := stats.TopWords(sessions, 15)
	var wordsB strings.Builder
	for _, w := range words {
		fmt.Fprintf(&wordsB, "- %s: %d\n", w.Word, w.Count)
	}

	bigrams := stats.TopBigrams(sessions, 10)
	var bigramsB strings.Builder
	for _, b := range bigrams {
		fmt.Fprintf(&bigramsB, "- %s %s: %d\n", b.A, b.B, b.Count)
	}

	rate, hits, total := stats.ErrorRate(sessions)
	highErr := stats.HighErrorSessions(sessions, 0.15)
	var errorsB strings.Builder
	fmt.Fprintf(&errorsB, "Error rate global: %.0f%% (%d/%d)\n", rate*100, hits, total)
	for _, h := range highErr {
		fmt.Fprintf(&errorsB, "- session %s: %.0f%% retrabalho\n", h.Session.SessionID[:8], h.ErrorRate*100)
	}

	prompt := fmt.Sprintf(insightsPromptPT,
		len(caches), summariesB.String(),
		toolsB.String(), wordsB.String(),
		errorsB.String(), bigramsB.String(),
	)

	cctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	out, err := client.GenerateLong(cctx, genModel, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}

	insights, err := parseInsights(out)
	if err != nil {
		return nil, fmt.Errorf("parse: %w (raw: %s)", err, truncStr(out, 500))
	}
	if err := db.InsightsReplaceAll(insights); err != nil {
		return nil, fmt.Errorf("persist: %w", err)
	}
	return insights, nil
}

// parseInsights aceita JSON puro ou JSON envolto em ```json ... ``` ou texto.
func parseInsights(raw string) ([]*index.Insight, error) {
	raw = strings.TrimSpace(raw)
	// strip code fences se houver
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	// extrai array se tem texto antes
	if i := strings.Index(raw, "["); i > 0 {
		if j := strings.LastIndex(raw, "]"); j > i {
			raw = raw[i : j+1]
		}
	}

	type rawIns struct {
		Type             string `json:"type"`
		Title            string `json:"title"`
		Description      string `json:"description"`
		Evidence         any    `json:"evidence"`
		SuggestedAction  string `json:"suggested_action"`
	}
	var arr []rawIns
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil, err
	}
	out := make([]*index.Insight, 0, len(arr))
	for _, r := range arr {
		ev := ""
		switch v := r.Evidence.(type) {
		case string:
			ev = v
		case []any:
			parts := make([]string, 0, len(v))
			for _, x := range v {
				parts = append(parts, fmt.Sprintf("%v", x))
			}
			ev = strings.Join(parts, ", ")
		default:
			ev = fmt.Sprintf("%v", r.Evidence)
		}
		out = append(out, &index.Insight{
			Type:            r.Type,
			Title:           r.Title,
			Description:     r.Description,
			Evidence:        ev,
			SuggestedAction: r.SuggestedAction,
		})
	}
	return out, nil
}

// GenerateProfile carrega summaries + insights + tech detectada e gera perfil.
func GenerateProfile(ctx context.Context, db *index.DB, client *Client, genModel string) (string, error) {
	caches, err := db.AICacheList()
	if err != nil {
		return "", err
	}
	sessions, err := db.ListSessions()
	if err != nil {
		return "", err
	}
	insights, _ := db.InsightsList()

	var summariesB strings.Builder
	for _, c := range caches {
		if c.Summary == "" {
			continue
		}
		fmt.Fprintf(&summariesB, "- %s\n", c.Summary)
	}
	var insightsB strings.Builder
	for _, ins := range insights {
		fmt.Fprintf(&insightsB, "- [%s] %s — %s\n", ins.Type, ins.Title, ins.Description)
	}
	if insightsB.Len() == 0 {
		insightsB.WriteString("(insights ainda não gerados)")
	}

	// Tech detection
	techs := DetectTech(sessions)
	var techB strings.Builder
	if len(techs) == 0 {
		techB.WriteString("(nenhuma tecnologia conhecida detectada)\n")
	}
	for _, t := range techs {
		fmt.Fprintf(&techB, "- %s: %d menções\n", t.Name, t.Count)
	}

	// Tools
	toolFreq := map[string]int{}
	for _, s := range sessions {
		for t, n := range s.ToolCalls {
			toolFreq[t] += n
		}
	}
	type kv struct {
		k string
		v int
	}
	var toolPairs []kv
	for k, v := range toolFreq {
		toolPairs = append(toolPairs, kv{k, v})
	}
	sort.Slice(toolPairs, func(i, j int) bool {
		if toolPairs[i].v != toolPairs[j].v {
			return toolPairs[i].v > toolPairs[j].v
		}
		return toolPairs[i].k < toolPairs[j].k
	})
	var toolsB strings.Builder
	for i, p := range toolPairs {
		if i >= 8 {
			break
		}
		fmt.Fprintf(&toolsB, "- %s: %d\n", p.k, p.v)
	}

	// Words/bigrams
	words := stats.TopWords(sessions, 10)
	bigrams := stats.TopBigrams(sessions, 8)
	var wordsB strings.Builder
	for _, w := range words {
		fmt.Fprintf(&wordsB, "- %s (%d)\n", w.Word, w.Count)
	}
	for _, b := range bigrams {
		fmt.Fprintf(&wordsB, "- %s %s (%d)\n", b.A, b.B, b.Count)
	}

	// About override (~/.claude-history/about.txt)
	about := loadAboutOverride()
	aboutSection := ""
	if about != "" {
		aboutSection = "SOBRE O DEV (auto-descrição, prioridade máxima):\n" + about + "\n\n"
	}

	prompt := fmt.Sprintf(profilePromptPT,
		aboutSection,
		techB.String(),
		toolsB.String(),
		wordsB.String(),
		summariesB.String(),
		insightsB.String(),
	)
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	out, err := client.GenerateLong(cctx, genModel, prompt)
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if err := db.ProfileSet(out); err != nil {
		return "", err
	}
	return out, nil
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

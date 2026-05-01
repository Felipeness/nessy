// Package advisor gera recomendações deterministas (rule-based, sem LLM)
// pra melhorar uso do Claude Code. Complementa o `internal/ai/insights`
// que usa LLM — advisor roda sempre, é rápido, e cita evidência concreta.
//
// Tipos de recomendação:
//
//	skill          — padrão de uso vira candidato a skill
//	hook           — comando repetido depois de tool específico
//	cli            — Bash com CLI nativo melhor disponível (gh, fd, rg, etc)
//	model_downgrade— sessions simples usando modelo caro
//	cache          — arquivo tocado em N sessions vira CLAUDE.md/skill
//	subagent       — paralelismo perdido (N reads em janela curta)
//	claude_md      — contexto repetido cross-session
//
// Política de hierarquia (importante):
//
//	CLI > skill > hook > MCP server
//
// Sempre que detectar padrão de uso de Bash que TEM um CLI nativo melhor,
// recomende o CLI primeiro. Só sugere MCP server se NENHUM CLI cobre o
// caso (ex: queries SQL ad-hoc num DB, navegação simbólica em código).
// Razão: CLI funciona em qualquer contexto (pipe, scripts, terminal),
// MCP só funciona dentro do Claude Code e exige setup extra.
//
// Cada regra é independente e pode ser adicionada/desativada.
package advisor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/felipeness/nessy/internal/index"
	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/pricing"
)

// Recommendation é uma sugestão concreta pra melhorar um aspecto do uso.
type Recommendation struct {
	Type        string  `json:"type"`        // "skill", "hook", "model_downgrade", etc
	Title       string  `json:"title"`       // "Skill recomendada: git-commit-flow"
	Description string  `json:"description"` // contexto do padrão observado
	Evidence    string  `json:"evidence"`    // session_ids/contagens
	Action      string  `json:"action"`      // ação concreta
	Savings     string  `json:"savings"`     // estimativa de economia
	Confidence  string  `json:"confidence"`  // "high", "medium", "low"
	Score       float64 `json:"score"`       // ranking (alto = priorizar)
}

// Run aplica todas as regras e devolve recommendations ranqueadas por score.
func Run(db *index.DB, p *pricing.Pricing, sessions []*model.Session) ([]Recommendation, error) {
	var out []Recommendation

	// Cada regra é defensiva — se falhar, segue.
	if recs, err := ruleSkillFromFileReuse(db); err == nil {
		out = append(out, recs...)
	}
	if recs, err := ruleHookFromToolSequence(db); err == nil {
		out = append(out, recs...)
	}
	if recs, err := ruleModelDowngrade(sessions, p); err == nil {
		out = append(out, recs...)
	}
	if recs, err := ruleSubagentFromBurstReads(db); err == nil {
		out = append(out, recs...)
	}
	if recs, err := ruleClaudeMDFromRepeatedContext(sessions); err == nil {
		out = append(out, recs...)
	}
	if recs, err := ruleSkillFromLoopDetected(db); err == nil {
		out = append(out, recs...)
	}
	if recs, err := ruleCLIAlternative(db); err == nil {
		out = append(out, recs...)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

// =============================================================================
// Rule: arquivos tocados em N+ sessions = candidato a skill/CLAUDE.md
// =============================================================================

func ruleSkillFromFileReuse(db *index.DB) ([]Recommendation, error) {
	reuse, err := db.FileReuseTop(3, 10) // ≥3 sessions
	if err != nil {
		return nil, err
	}
	var out []Recommendation
	for _, f := range reuse {
		// Skipa caches/artifacts/dotfiles internos do Claude Code
		if isNoiseFile(f.FilePath) {
			continue
		}
		base := basename(f.FilePath)
		out = append(out, Recommendation{
			Type:  "skill",
			Title: fmt.Sprintf("Skill ou referência fixa pra: %s", base),
			Description: fmt.Sprintf(
				"%s foi tocado em %d sessions distintas (%d ops totais). "+
					"Provavelmente é arquivo de referência ou config recorrente.",
				base, f.SessionCount, f.TotalOps),
			Evidence: fmt.Sprintf("file_path=%s · sessions=%d · ops=%d",
				f.FilePath, f.SessionCount, f.TotalOps),
			Action: fmt.Sprintf(
				"Considere: (1) adicionar conteúdo/regras de '%s' ao CLAUDE.md, "+
					"(2) criar skill que sempre lê esse arquivo no setup, "+
					"ou (3) marcar pra cache_control no prompt.",
				base),
			Savings: fmt.Sprintf("~%d Read calls evitáveis por session futura",
				f.TotalOps/maxInt(1, f.SessionCount)),
			Confidence: confidenceFromCount(f.SessionCount),
			Score:      float64(f.SessionCount) * 10,
		})
	}
	return out, nil
}

// =============================================================================
// Rule: tool específico chamado N+ vezes com mesmo input = candidato a hook
// (loops também sinalizam isso, mas advisor sugere AÇÃO em vez de "loop")
// =============================================================================

func ruleHookFromToolSequence(db *index.DB) ([]Recommendation, error) {
	loops, err := db.DetectLoops(3, 3600) // ≥3× em ≤60min
	if err != nil {
		return nil, err
	}
	var out []Recommendation
	for _, h := range loops {
		// Bash repetido com mesmo input em mesma session: claro sinal de hook ou skill
		if h.ToolName == "Bash" && h.Count >= 3 {
			preview := h.InputPreview
			if len(preview) > 80 {
				preview = preview[:79] + "…"
			}
			out = append(out, Recommendation{
				Type:  "hook",
				Title: "Comando Bash repetido — automatizar via hook ou skill",
				Description: fmt.Sprintf(
					"Mesmo Bash chamado %d× em [%s]: %s",
					h.Count, h.SessionID[:8], preview),
				Evidence: fmt.Sprintf("session=%s · count=%d · span=%.0fs",
					h.SessionID[:8], h.Count, h.SpanSecs),
				Action: "Ou crie um PostToolUse hook que dispara automaticamente, " +
					"ou empacote num skill que o Claude pode invocar de uma só vez.",
				Savings:    fmt.Sprintf("~%d turnos extras por ocorrência futura", h.Count-1),
				Confidence: confidenceFromCount(h.Count),
				Score:      float64(h.Count) * 8,
			})
		}
	}
	return out, nil
}

// =============================================================================
// Rule: sessions curtas usando modelo caro = downgrade pra Sonnet/Haiku
// =============================================================================

func ruleModelDowngrade(sessions []*model.Session, p *pricing.Pricing) ([]Recommendation, error) {
	if p == nil {
		return nil, nil
	}
	type smallSession struct {
		s    *model.Session
		cost float64
	}
	var smalls []smallSession
	for _, s := range sessions {
		// Heurística: session curta = ≤10 turns + ≤50k tokens output
		if s.MessageCount > 10 || s.OutputTokens > 50_000 {
			continue
		}
		// Só conta se modelo é Opus (caro)
		if !strings.Contains(strings.ToLower(s.Model), "opus") {
			continue
		}
		c, ok := p.Cost(s)
		if !ok || c.USD < 0.10 {
			continue // ignora sessions triviais
		}
		smalls = append(smalls, smallSession{s, c.USD})
	}
	if len(smalls) < 3 {
		return nil, nil // dados insuficientes
	}
	totalCost := 0.0
	var ids []string
	for _, x := range smalls {
		totalCost += x.cost
		if len(ids) < 5 {
			ids = append(ids, x.s.SessionID[:8])
		}
	}
	// Sonnet ~5× barato que Opus, Haiku ~20×
	estSavings := totalCost * 0.8 // assume sonnet
	return []Recommendation{{
		Type:  "model_downgrade",
		Title: fmt.Sprintf("%d sessions curtas em Opus — Sonnet seria suficiente", len(smalls)),
		Description: fmt.Sprintf(
			"Você tem %d sessions com ≤10 turns e ≤50k output tokens rodando em Opus. "+
				"Tarefas dessa escala raramente exigem Opus.",
			len(smalls)),
		Evidence: fmt.Sprintf("total_cost=$%.2f · sessions=%s%s",
			totalCost, strings.Join(ids, ","), ifThen(len(smalls) > 5, "...")),
		Action: "Pra tarefas curtas (resumir, refatorar pequeno arquivo, perguntar API), " +
			"setar `model=claude-sonnet-4-6` ou usar `/model haiku`.",
		Savings:    fmt.Sprintf("~$%.2f USD economizáveis (≈80%% downgrade Opus→Sonnet)", estSavings),
		Confidence: "medium",
		Score:      totalCost * 5, // prioriza por economia
	}}, nil
}

// =============================================================================
// Rule: muitos Read calls em janela curta = oportunidade de subagent paralelo
// =============================================================================

func ruleSubagentFromBurstReads(db *index.DB) ([]Recommendation, error) {
	// Query: por session, conta Reads em janelas de 5min
	rows, err := db.Conn().Query(`
		WITH read_events AS (
			SELECT session_id, ts FROM tool_events WHERE tool_name = 'Read'
		)
		SELECT a.session_id, COUNT(*) AS bursts
		FROM read_events a
		JOIN read_events b
		  ON a.session_id = b.session_id
		 AND b.ts > a.ts AND b.ts - a.ts <= 300000000000
		GROUP BY a.session_id
		HAVING bursts >= 30
		ORDER BY bursts DESC
		LIMIT 5
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recommendation
	for rows.Next() {
		var sid string
		var bursts int
		if err := rows.Scan(&sid, &bursts); err != nil {
			continue
		}
		out = append(out, Recommendation{
			Type:  "subagent",
			Title: "Muitos Reads em sequência — paralelizar via subagent",
			Description: fmt.Sprintf(
				"Session [%s] teve %d pares de Reads em janela ≤5min. "+
					"Reads sequenciais que poderiam rodar em paralelo via Task tool.",
				sid[:8], bursts),
			Evidence: fmt.Sprintf("session=%s · read_pairs_in_5min=%d",
				sid[:8], bursts),
			Action: "Quando precisar ler 3+ arquivos pra entender contexto, " +
				"dispara um subagent (Task tool) com prompt 'leia X, Y, Z e sumarize'. " +
				"Usa metade do tempo e mantém o context principal limpo.",
			Savings:    fmt.Sprintf("~%d turnos sequenciais → 1 spawn paralelo", bursts/3),
			Confidence: "medium",
			Score:      float64(bursts),
		})
	}
	return out, nil
}

// =============================================================================
// Rule: mesma frase de abertura em múltiplas sessions = candidato CLAUDE.md
// =============================================================================

func ruleClaudeMDFromRepeatedContext(sessions []*model.Session) ([]Recommendation, error) {
	// Conta primeiras 60 chars normalizadas. ≥3 sessions com abertura similar.
	seen := map[string][]string{}
	for _, s := range sessions {
		key := normalizePrefix(s.FirstUserMsg)
		if key == "" {
			continue
		}
		seen[key] = append(seen[key], s.SessionID[:8])
	}
	var out []Recommendation
	for prefix, ids := range seen {
		if len(ids) < 3 {
			continue
		}
		out = append(out, Recommendation{
			Type:  "claude_md",
			Title: "Contexto repetido em múltiplas sessions",
			Description: fmt.Sprintf(
				"Você abre %d sessions com a mesma frase: \"%s…\". "+
					"Provavelmente está re-explicando contexto a cada vez.",
				len(ids), truncate(prefix, 70)),
			Evidence: fmt.Sprintf("sessions=%s · prefix_repeated=%d×",
				strings.Join(ids[:minInt(5, len(ids))], ","), len(ids)),
			Action: "Move esse contexto pro CLAUDE.md do projeto " +
				"(ou pra um skill genérico) — Claude vai ler 1× e usar em todas.",
			Savings: fmt.Sprintf("~%d×%d≈%d tokens evitáveis (cada repetição)",
				len(ids), len(prefix)/4, len(ids)*len(prefix)/4),
			Confidence: confidenceFromCount(len(ids)),
			Score:      float64(len(ids)) * 6,
		})
	}
	return out, nil
}

// =============================================================================
// Rule: loops já detectados = explicit skill recommendation
// =============================================================================

func ruleSkillFromLoopDetected(db *index.DB) ([]Recommendation, error) {
	loops, err := db.DetectLoops(5, 3600) // ≥5× — só os mais óbvios
	if err != nil {
		return nil, err
	}
	var out []Recommendation
	for _, h := range loops {
		if h.ToolName == "Bash" {
			continue // já coberto por ruleHookFromToolSequence
		}
		out = append(out, Recommendation{
			Type:  "skill",
			Title: fmt.Sprintf("Skill recomendada: pattern de %s repetido", h.ToolName),
			Description: fmt.Sprintf(
				"Tool %s foi chamada %d× com mesmo input em [%s]. "+
					"Padrão maduro pra encapsular num skill.",
				h.ToolName, h.Count, h.SessionID[:8]),
			Evidence: fmt.Sprintf("tool=%s · count=%d · session=%s",
				h.ToolName, h.Count, h.SessionID[:8]),
			Action: fmt.Sprintf(
				"Crie um skill que combine %s + lógica de checagem/idempotência. "+
					"Claude vai usar isso em vez de retentar o mesmo call.",
				h.ToolName),
			Savings:    fmt.Sprintf("~%d tool calls eliminados", h.Count-1),
			Confidence: "high",
			Score:      float64(h.Count) * 7,
		})
	}
	return out, nil
}

// =============================================================================
// Rule: padrões de Bash que têm CLI nativo melhor
// =============================================================================

// cliPattern descreve um padrão suspeito num Bash input + a CLI mais adequada.
// Use o CLI primeiro; se não cobre o caso, aí sim discutir MCP server.
type cliPattern struct {
	// matches são substrings que disparam a recomendação (case-insensitive).
	matches []string
	// cli é o nome do binário recomendado.
	cli string
	// rationale explica POR QUE o CLI é melhor.
	rationale string
	// example é uma linha de uso simples.
	example string
	// minOccurrences pra disparar (evita falso positivo em uso esporádico).
	minOccurrences int
}

var cliPatterns = []cliPattern{
	{
		matches:        []string{"api.github.com", "raw.githubusercontent.com"},
		cli:            "gh",
		rationale:      "gh já faz auth automático (gh auth login), respeita rate limits, e tem subcomandos pra issues/PRs/repos.",
		example:        "gh api repos/{owner}/{repo} · gh pr view 123 · gh issue list",
		minOccurrences: 3,
	},
	{
		matches:        []string{"git diff --name-only", "git log --pretty="},
		cli:            "gh + git aliases",
		rationale:      "Pra inspeção de PRs/branches, gh é mais alto nível. Pra git puro, define alias em ~/.gitconfig em vez de invocar Bash sempre.",
		example:        "gh pr diff · gh pr checks · git config --global alias.lg 'log --oneline'",
		minOccurrences: 5,
	},
	{
		matches:        []string{`find . -name "*`, `find . -type f`},
		cli:            "fd",
		rationale:      "fd é 5-10× mais rápido, syntax mais natural, respeita .gitignore por default.",
		example:        "fd '\\.go$' · fd -e ts components/",
		minOccurrences: 4,
	},
	{
		matches:        []string{"grep -r", "grep -R"},
		cli:            "rg (ripgrep)",
		rationale:      "ripgrep é muito mais rápido que grep -r, formato de output melhor, suporta tipos (--type go), respeita .gitignore.",
		example:        "rg 'pattern' · rg --type go 'func' · rg -l 'TODO'",
		minOccurrences: 4,
	},
	{
		matches:        []string{"jq -r", "jq '.["},
		cli:            "jq (já é CLI)",
		rationale:      "Tu já usa jq — bom! Mas se padrões se repetem, vira candidato a alias ou função shell pra reduzir digitação.",
		example:        "function gh-prs() { gh pr list --json number,title | jq -r '.[]|\"\\(.number) \\(.title)\"' }",
		minOccurrences: 8,
	},
	{
		matches:        []string{"curl http", "wget http"},
		cli:            "httpie ou xh",
		rationale:      "Pra queries HTTP exploratórias: httpie/xh tem syntax mais legível, pretty-print JSON, default reasonável (Accept: application/json).",
		example:        "http GET api.example.com/users · xh post api.example.com email=a@b.com",
		minOccurrences: 5,
	},
	{
		matches:        []string{"docker run -it", "docker exec -it"},
		cli:            "lazydocker",
		rationale:      "Pra inspeção interativa de containers/imagens, TUI é mais ergonômico que CLI flags.",
		example:        "lazydocker (binding inicial vai pros containers ativos)",
		minOccurrences: 4,
	},
	{
		matches:        []string{"kubectl get", "kubectl describe"},
		cli:            "k9s",
		rationale:      "Pra exploração de pods/deployments, k9s é TUI live com filtros e logs streaming. kubectl puro fica pra scripts.",
		example:        "k9s (navega namespaces, segue logs, restart pods, tudo via teclado)",
		minOccurrences: 5,
	},
}

func ruleCLIAlternative(db *index.DB) ([]Recommendation, error) {
	rows, err := db.Conn().Query(`
		SELECT input_preview, COUNT(*) AS n
		FROM tool_events
		WHERE tool_name = 'Bash' AND input_preview != ''
		GROUP BY input_preview
		ORDER BY n DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Conta ocorrências por pattern
	type hit struct {
		pattern *cliPattern
		count   int
		samples []string // primeiros previews que casaram
	}
	hits := map[string]*hit{}
	for rows.Next() {
		var preview string
		var n int
		if err := rows.Scan(&preview, &n); err != nil {
			continue
		}
		low := strings.ToLower(preview)
		for i := range cliPatterns {
			pat := &cliPatterns[i]
			matched := false
			for _, m := range pat.matches {
				if strings.Contains(low, strings.ToLower(m)) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			h, ok := hits[pat.cli]
			if !ok {
				h = &hit{pattern: pat}
				hits[pat.cli] = h
			}
			h.count += n
			if len(h.samples) < 3 {
				sample := preview
				if len(sample) > 60 {
					sample = sample[:59] + "…"
				}
				h.samples = append(h.samples, sample)
			}
		}
	}

	var out []Recommendation
	for cli, h := range hits {
		if h.count < h.pattern.minOccurrences {
			continue
		}
		out = append(out, Recommendation{
			Type:  "cli",
			Title: fmt.Sprintf("Usa %s — CLI '%s' é alternativa direta", strings.Join(h.pattern.matches[:1], ""), cli),
			Description: fmt.Sprintf(
				"Detectei %d Bash calls com padrão de %s. %s",
				h.count, strings.Join(h.pattern.matches, "/"), h.pattern.rationale),
			Evidence: fmt.Sprintf("count=%d · samples=%s",
				h.count, strings.Join(h.samples, " | ")),
			Action: fmt.Sprintf(
				"Substitui por: %s. Exemplo: %s",
				cli, h.pattern.example),
			Savings:    fmt.Sprintf("~%d Bash invocations × menos digitação/parsing", h.count),
			Confidence: confidenceFromCount(h.count / 2),
			Score:      float64(h.count) * 4,
		})
	}
	return out, nil
}

// =============================================================================
// Helpers
// =============================================================================

func basename(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func isNoiseFile(p string) bool {
	noise := []string{"/dist/", "/build/", "/node_modules/", "/.git/",
		"/.cache/", "/.next/", "/.DS_Store"}
	for _, n := range noise {
		if strings.Contains(p, n) {
			return true
		}
	}
	return false
}

func confidenceFromCount(n int) string {
	switch {
	case n >= 7:
		return "high"
	case n >= 4:
		return "medium"
	default:
		return "low"
	}
}

func normalizePrefix(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 60 {
		s = s[:60]
	}
	s = strings.ToLower(s)
	// Remove pontuação trailing
	s = strings.TrimRight(s, " .,!?;:")
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func ifThen(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

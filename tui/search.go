package tui

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/nessy/internal/index"
	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/pricing"
)

// searchMode espelha os 4 modos do backend HTTP. Default é hybrid.
type searchMode int

const (
	modeHybrid searchMode = iota
	modeMetadata
	modeFullText
	modeSemantic // não-implementado em TUI (precisa Ollama, complica), prefixo redireciona pra :body
)

// searchHit é um match individual — pode ter múltiplos por session quando
// expand=true. Snippet vem com [bracket] highlight nos termos.
type searchHit struct {
	Session *model.Session
	Snippet string
	Role    string
}

// searchView é a aba Search da TUI. Default mostra TODOS os hits (cada match
// individual) — toggle 'e' agrupa por session.
type searchView struct {
	db        *index.DB
	pricing   *pricing.Pricing
	summaries map[string]string // session_id → AI summary (pra busca)
	input     textinput.Model
	mode      searchMode
	all       []*model.Session
	results   []searchHit
	cursor    int
	expand    bool // false (default) = 1 por session; true = todos hits
	fuzzy     bool // false (default) = exact LIKE filter; true = Porter stemmer
}

func newSearchView(db *index.DB, p *pricing.Pricing, all []*model.Session) searchView {
	ti := textinput.New()
	ti.Placeholder = "ex: docker · :sim auth · project:claude cost:>1 since:7d"
	ti.Focus()
	return searchView{
		db:        db,
		pricing:   p,
		summaries: loadSummaries(db),
		input:     ti,
		all:       all,
		results:   sessionsToHits(all),
		expand:    false, // default = agrupado (1 por session); ctrl+t expande
	}
}

func sessionsToHits(sessions []*model.Session) []searchHit {
	out := make([]searchHit, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, searchHit{Session: s})
	}
	return out
}

// ToggleExpand alterna entre modo agrupado e todos hits, recomputando.
func (v *searchView) ToggleExpand() {
	v.expand = !v.expand
	v.Filter(v.input.Value())
}

// ToggleFuzzy alterna entre busca exata (default) e fuzzy (Porter stemmer).
// Recomputa imediatamente.
func (v *searchView) ToggleFuzzy() {
	v.fuzzy = !v.fuzzy
	v.Filter(v.input.Value())
}

// Filter parseia prefixes (:body / :meta / :sim / :all) + filtros
// (project: branch: cost: since:) e roda o modo apropriado.
func (v *searchView) Filter(query string) {
	q := strings.TrimSpace(query)

	// :all força hybrid + expand
	expand := v.expand
	if strings.HasPrefix(q, ":all ") {
		expand = true
		q = strings.TrimSpace(q[5:])
	}
	// Detect modo via prefixo
	mode := modeHybrid
	switch {
	case strings.HasPrefix(q, ":body "):
		mode = modeFullText
		q = strings.TrimSpace(q[6:])
	case strings.HasPrefix(q, ":meta "):
		mode = modeMetadata
		q = strings.TrimSpace(q[6:])
	case strings.HasPrefix(q, ":sim "):
		// TUI não tem semantic — fallback pra body (sinaliza no header)
		mode = modeFullText
		q = strings.TrimSpace(q[5:])
	}
	v.mode = mode

	// Parse filtros (project:, branch:, cost:, since:, model:)
	filters, residual := parseSearchFilters(q)
	q = residual

	// Aplica filtros — corta o universo de busca
	candidates := v.applyFilters(v.all, filters)

	if q == "" {
		v.results = sessionsToHits(candidates)
		v.clampCursor()
		return
	}

	switch mode {
	case modeMetadata:
		v.results = v.searchMetadata(q, candidates)
	case modeFullText:
		v.results = v.searchFTS(q, candidates, expand)
	default: // hybrid
		v.results = v.searchHybridTUI(q, candidates, expand)
	}
	v.clampCursor()
}

// runFTS escolhe entre exact (LIKE filter) e fuzzy (Porter stem) baseado
// em v.fuzzy. Usado por searchFTS e searchHybridTUI.
func (v *searchView) runFTS(q string) []index.SearchResult {
	if v.db == nil {
		return nil
	}
	var results []index.SearchResult
	var err error
	if v.fuzzy {
		results, err = v.db.SearchFTS(q)
	} else {
		results, err = v.db.SearchFTSExact(q)
	}
	if err != nil {
		results, _ = v.db.SearchLike(q)
	}
	return results
}

func (v *searchView) clampCursor() {
	if v.cursor >= len(v.results) {
		v.cursor = len(v.results) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

// searchMetadata busca substring em path, branch, msgs, model, tools, summary.
func (v *searchView) searchMetadata(q string, sessions []*model.Session) []searchHit {
	lower := strings.ToLower(q)
	out := []searchHit{}
	for _, s := range sessions {
		if hit, snippet, role := metaMatchExtTUI(s, lower, v.summaries); hit {
			out = append(out, searchHit{Session: s, Snippet: snippet, Role: role})
		}
	}
	return out
}

// searchFTS roda FTS5. Quando expand=true, cada match vira hit. Quando false,
// dedupa por session.
func (v *searchView) searchFTS(q string, sessions []*model.Session, expand bool) []searchHit {
	results := v.runFTS(q)
	byID := map[string]*model.Session{}
	for _, s := range sessions {
		byID[s.SessionID] = s
	}
	out := []searchHit{}
	seen := map[string]bool{}
	for _, r := range results {
		s, ok := byID[r.SessionID]
		if !ok {
			continue
		}
		if !expand {
			if seen[r.SessionID] {
				continue
			}
			seen[r.SessionID] = true
		}
		out = append(out, searchHit{Session: s, Snippet: r.Snippet, Role: "body:" + r.Role})
	}
	return out
}

// searchHybridTUI = metadata + FTS combinados, com dedup opcional.
func (v *searchView) searchHybridTUI(q string, sessions []*model.Session, expand bool) []searchHit {
	out := []searchHit{}
	seen := map[string]bool{}

	// Metadata first
	lower := strings.ToLower(q)
	for _, s := range sessions {
		if hit, snippet, role := metaMatchExtTUI(s, lower, v.summaries); hit {
			seen[s.SessionID] = true
			out = append(out, searchHit{Session: s, Snippet: snippet, Role: role})
		}
	}
	// FTS — todos hits ou só novos sessions
	results := v.runFTS(q)
	byID := map[string]*model.Session{}
	for _, s := range sessions {
		byID[s.SessionID] = s
	}
	matchCount := map[string]int{}
	for _, r := range results {
		matchCount[r.SessionID]++
		s, ok := byID[r.SessionID]
		if !ok {
			continue
		}
		if expand {
			out = append(out, searchHit{Session: s, Snippet: r.Snippet, Role: "body:" + r.Role})
			continue
		}
		if seen[r.SessionID] {
			continue
		}
		seen[r.SessionID] = true
		out = append(out, searchHit{Session: s, Snippet: r.Snippet, Role: "body:" + r.Role})
	}
	// Anota +N badge no role pra modo agrupado
	if !expand {
		for i := range out {
			if c := matchCount[out[i].Session.SessionID]; c > 1 {
				out[i].Role = out[i].Role + " (+" + strconv.Itoa(c-1) + ")"
			}
		}
	}
	return out
}

// metaMatchExtTUI = mesma lógica do server, busca em vários fields + summary.
func metaMatchExtTUI(s *model.Session, q string, summaries map[string]string) (bool, string, string) {
	if s == nil {
		return false, "", ""
	}
	checks := []struct {
		field, text string
	}{
		{"path", s.ProjectDir},
		{"branch", s.GitBranch},
		{"first_msg", s.FirstUserMsg},
		{"last_msg", s.LastUserMsg},
		{"id", s.SessionID},
		{"model", s.Model},
	}
	for _, c := range checks {
		if c.text == "" {
			continue
		}
		if i := strings.Index(strings.ToLower(c.text), q); i >= 0 {
			return true, makeSnippetTUI(c.text, i, len(q)), c.field
		}
	}
	for tool := range s.ToolCalls {
		if strings.Contains(strings.ToLower(tool), q) {
			return true, "tool: " + tool, "tool"
		}
	}
	if sum := summaries[s.SessionID]; sum != "" {
		if i := strings.Index(strings.ToLower(sum), q); i >= 0 {
			return true, makeSnippetTUI(sum, i, len(q)), "summary"
		}
	}
	return false, "", ""
}

func makeSnippetTUI(text string, idx, qLen int) string {
	if idx < 0 || idx >= len(text) {
		return text
	}
	start := idx - 30
	if start < 0 {
		start = 0
	}
	end := idx + qLen + 30
	if end > len(text) {
		end = len(text)
	}
	prefix, suffix := "", ""
	if start > 0 {
		prefix = "…"
	}
	if end < len(text) {
		suffix = "…"
	}
	return prefix + text[start:idx] + "[" + text[idx:idx+qLen] + "]" + text[idx+qLen:end] + suffix
}

// searchFiltersTUI mapeia os filtros parseados.
type searchFiltersTUI struct {
	project, branch, modelN string
	since                   time.Duration
	costMin, costMax        float64
	hasFilter               bool
}

func parseSearchFilters(q string) (searchFiltersTUI, string) {
	f := searchFiltersTUI{costMin: -1, costMax: -1}
	parts := strings.Fields(q)
	var rest []string
	for _, p := range parts {
		colon := strings.IndexByte(p, ':')
		if colon < 1 {
			rest = append(rest, p)
			continue
		}
		key := strings.ToLower(p[:colon])
		val := p[colon+1:]
		switch key {
		case "project":
			f.project, f.hasFilter = strings.ToLower(val), true
		case "branch":
			f.branch, f.hasFilter = strings.ToLower(val), true
		case "model":
			f.modelN, f.hasFilter = strings.ToLower(val), true
		case "since":
			if d, err := parseDurAliasTUI(val); err == nil {
				f.since, f.hasFilter = d, true
			} else {
				rest = append(rest, p)
			}
		case "cost":
			if strings.HasPrefix(val, ">") {
				if n, err := strconv.ParseFloat(val[1:], 64); err == nil {
					f.costMin, f.hasFilter = n, true
					continue
				}
			}
			if strings.HasPrefix(val, "<") {
				if n, err := strconv.ParseFloat(val[1:], 64); err == nil {
					f.costMax, f.hasFilter = n, true
					continue
				}
			}
			rest = append(rest, p)
		default:
			rest = append(rest, p)
		}
	}
	return f, strings.Join(rest, " ")
}

func parseDurAliasTUI(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func (v *searchView) applyFilters(all []*model.Session, f searchFiltersTUI) []*model.Session {
	if !f.hasFilter {
		return all
	}
	cutoff := time.Time{}
	if f.since > 0 {
		cutoff = time.Now().Add(-f.since)
	}
	out := make([]*model.Session, 0, len(all))
	for _, s := range all {
		if f.project != "" && !strings.Contains(strings.ToLower(s.ProjectDir), f.project) {
			continue
		}
		if f.branch != "" && !strings.Contains(strings.ToLower(s.GitBranch), f.branch) {
			continue
		}
		if f.modelN != "" && !strings.Contains(strings.ToLower(s.Model), f.modelN) {
			continue
		}
		if !cutoff.IsZero() && s.StartTime.Before(cutoff) {
			continue
		}
		if f.costMin >= 0 || f.costMax >= 0 {
			if v.pricing != nil {
				if c, ok := v.pricing.Cost(s); ok {
					if f.costMin >= 0 && c.USD < f.costMin {
						continue
					}
					if f.costMax >= 0 && c.USD > f.costMax {
						continue
					}
				}
			}
		}
		out = append(out, s)
	}
	return out
}

func (v searchView) selected() *model.Session {
	if v.cursor < 0 || v.cursor >= len(v.results) {
		return nil
	}
	return v.results[v.cursor].Session
}

func (v searchView) View(width, height int) string {
	modeNames := map[searchMode]string{
		modeHybrid:   "hybrid",
		modeMetadata: "meta",
		modeFullText: "body",
		modeSemantic: "sim",
	}
	expandLabel := "agrupado"
	if v.expand {
		expandLabel = "todos hits"
	}
	matchLabel := "exato"
	if v.fuzzy {
		matchLabel = "fuzzy"
	}
	header := lipgloss.NewStyle().Foreground(colorMuted).Render(
		"mode: " + modeNames[v.mode] + " · " + expandLabel + " · " + matchLabel +
			" · " + strconv.Itoa(len(v.results)) + " results · [ctrl+t] expand · [ctrl+f] fuzzy · [↑↓] nav · [enter] retomar",
	)
	now := time.Now()
	var rows []string
	for i, h := range v.results {
		marker := "  "
		if i == v.cursor {
			marker = "▶ "
		}
		row := formatDenseRow(h.Session, v.pricing, now, width-3)
		if h.Snippet != "" {
			snippet := h.Snippet
			if len(snippet) > width-7 {
				snippet = snippet[:width-8] + "…"
			}
			row += "\n     " + lipgloss.NewStyle().Foreground(colorMuted).Render(
				"["+h.Role+"] "+highlightBrackets(snippet),
			)
		}
		rows = append(rows, marker+row)
	}
	body := strings.Join(rows, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, v.input.View(), header, body)
}

// highlightBrackets pinta termos em [bracket] com accent. lipgloss não tem
// markdown — fazemos manualmente.
var bracketRE = regexp.MustCompile(`\[([^\]]+)\]`)

func highlightBrackets(s string) string {
	accent := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	return bracketRE.ReplaceAllStringFunc(s, func(m string) string {
		inner := m[1 : len(m)-1]
		return accent.Render(inner)
	})
}

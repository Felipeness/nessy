package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/index"
	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
)

type searchMode int

const (
	modeMetadata searchMode = iota
	modeFullText
)

type searchView struct {
	db       *index.DB
	pricing  *pricing.Pricing
	input    textinput.Model
	mode     searchMode
	all      []*model.Session
	results  []*model.Session
	snippets map[string]string
	cursor   int
}

func newSearchView(db *index.DB, p *pricing.Pricing, all []*model.Session) searchView {
	ti := textinput.New()
	ti.Placeholder = "Filtrar por cwd, branch ou primeira msg…  (use :body <q> pra full-text)"
	ti.Focus()
	return searchView{db: db, pricing: p, input: ti, all: all, results: all, snippets: map[string]string{}}
}

func (v *searchView) Filter(query string) {
	q := strings.TrimSpace(query)
	if strings.HasPrefix(q, ":body ") {
		v.mode = modeFullText
		v.runFullText(strings.TrimPrefix(q, ":body "))
		return
	}
	v.mode = modeMetadata
	v.snippets = map[string]string{}
	if q == "" {
		v.results = v.all
		return
	}
	lower := strings.ToLower(q)
	v.results = v.results[:0]
	for _, s := range v.all {
		if metadataMatch(s, lower) {
			v.results = append(v.results, s)
		}
	}
}

func metadataMatch(s *model.Session, q string) bool {
	for _, h := range []string{s.ProjectDir, s.GitBranch, s.FirstUserMsg, s.LastUserMsg, s.SessionID} {
		if strings.Contains(strings.ToLower(h), q) {
			return true
		}
	}
	return false
}

func (v *searchView) runFullText(q string) {
	v.snippets = map[string]string{}
	if v.db == nil || strings.TrimSpace(q) == "" {
		v.results = nil
		return
	}
	results, err := v.db.SearchFTS(q)
	if err != nil {
		results, err = v.db.SearchLike(q)
		if err != nil {
			v.results = nil
			return
		}
	}
	byID := map[string]*model.Session{}
	for _, s := range v.all {
		byID[s.SessionID] = s
	}
	out := make([]*model.Session, 0, len(results))
	seen := map[string]bool{}
	for _, r := range results {
		if seen[r.SessionID] {
			continue
		}
		seen[r.SessionID] = true
		if s, ok := byID[r.SessionID]; ok {
			out = append(out, s)
			v.snippets[r.SessionID] = r.Snippet
		}
	}
	v.results = out
}

func (v searchView) selected() *model.Session {
	if v.cursor < 0 || v.cursor >= len(v.results) {
		return nil
	}
	return v.results[v.cursor]
}

func (v searchView) View(width, height int) string {
	header := lipgloss.NewStyle().Foreground(colorMuted).Render(
		"mode: " + map[searchMode]string{modeMetadata: "metadata", modeFullText: "full-text"}[v.mode],
	)
	now := time.Now()
	var rows []string
	for i, s := range v.results {
		marker := "  "
		if i == v.cursor {
			marker = "▶ "
		}
		row := formatDenseRow(s, v.pricing, now, width-3)
		if v.mode == modeFullText {
			if sn, ok := v.snippets[s.SessionID]; ok {
				row += "\n     " + lipgloss.NewStyle().Foreground(colorMuted).Render(sn)
			}
		}
		rows = append(rows, marker+row)
	}
	body := strings.Join(rows, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, v.input.View(), header, body)
}

package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/model"
)

type toolStat struct {
	Name        string
	TotalCalls  int
	NumSessions int
}

type toolsView struct {
	sessions []*model.Session
	stats    []toolStat
	cursor   int
}

func newToolsView(sessions []*model.Session) toolsView {
	calls := map[string]int{}
	sess := map[string]map[string]bool{}
	for _, s := range sessions {
		for t, c := range s.ToolCalls {
			calls[t] += c
			if sess[t] == nil {
				sess[t] = map[string]bool{}
			}
			sess[t][s.SessionID] = true
		}
	}
	out := make([]toolStat, 0, len(calls))
	for t, c := range calls {
		out = append(out, toolStat{Name: t, TotalCalls: c, NumSessions: len(sess[t])})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalCalls != out[j].TotalCalls {
			return out[i].TotalCalls > out[j].TotalCalls
		}
		return out[i].Name < out[j].Name
	})
	return toolsView{sessions: sessions, stats: out}
}

func (v toolsView) selectedTool() string {
	if v.cursor < 0 || v.cursor >= len(v.stats) {
		return ""
	}
	return v.stats[v.cursor].Name
}

// View renderiza só a lista de tools (lado esquerdo em multi-pane, único em narrow).
func (v toolsView) View(width int) string {
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	var b strings.Builder
	fmt.Fprintln(&b, header.Render("🔧 Top tools globais"))
	max := 1
	for _, t := range v.stats {
		if t.TotalCalls > max {
			max = t.TotalCalls
		}
	}
	for i, t := range v.stats {
		if i >= 25 {
			break
		}
		marker := "  "
		if i == v.cursor {
			marker = "▶ "
		}
		avg := 0
		if t.NumSessions > 0 {
			avg = t.TotalCalls / t.NumSessions
		}
		bar := BarChart(fmt.Sprintf("%-15s", t.Name), float64(t.TotalCalls), float64(max), 16, toolColor(t.Name))
		fmt.Fprintf(&b, "%s%s  %5d  (%d sess, méd %d/sess)\n",
			marker, bar, t.TotalCalls, t.NumSessions, avg)
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

// renderDrillDown renderiza top sessions que mais usaram a tool selecionada.
func (v toolsView) renderDrillDown(width int) string {
	tool := v.selectedTool()
	if tool == "" {
		return "(nenhuma tool selecionada)"
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	type sessUse struct {
		s     *model.Session
		count int
	}
	var hits []sessUse
	for _, s := range v.sessions {
		if c, ok := s.ToolCalls[tool]; ok && c > 0 {
			hits = append(hits, sessUse{s, c})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].count != hits[j].count {
			return hits[i].count > hits[j].count
		}
		return hits[i].s.SessionID < hits[j].s.SessionID
	})
	var b strings.Builder
	fmt.Fprintln(&b, header.Render(fmt.Sprintf("📊 Top sessions usando %s", tool)))
	if len(hits) == 0 {
		fmt.Fprintln(&b, "(nenhuma session usou essa tool)")
		return lipgloss.NewStyle().Width(width).Render(b.String())
	}
	for i, h := range hits {
		if i >= 15 {
			break
		}
		dir := h.s.ProjectDir
		if len(dir) > 35 {
			dir = "…" + dir[len(dir)-34:]
		}
		fmt.Fprintf(&b, "  %5d × %s  %s  %s\n",
			h.count,
			h.s.SessionID[:8],
			h.s.StartTime.Local().Format("2006-01-02 15:04"),
			dir,
		)
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

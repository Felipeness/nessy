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
		if i >= 20 {
			break
		}
		avg := 0
		if t.NumSessions > 0 {
			avg = t.TotalCalls / t.NumSessions
		}
		bar := BarChart(fmt.Sprintf("%-18s", t.Name), float64(t.TotalCalls), float64(max), 22, toolColor(t.Name))
		fmt.Fprintf(&b, "%s  %5d  (%d sessions, média %d/sess)\n",
			bar, t.TotalCalls, t.NumSessions, avg)
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

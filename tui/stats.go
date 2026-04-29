package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
)

type statsView struct {
	sessions  []*model.Session
	pricing   *pricing.Pricing
	showLocal bool
}

func newStatsView(sessions []*model.Session, p *pricing.Pricing) statsView {
	return statsView{sessions: sessions, pricing: p}
}

func (v statsView) renderGlobal(width int) string {
	var b strings.Builder
	totalMsgs := 0
	totalCostUSD := 0.0
	costByProject := map[string]float64{}
	toolGlobal := map[string]int{}
	for _, s := range v.sessions {
		totalMsgs += s.MessageCount
		if cost, ok := v.pricing.Cost(s); ok {
			totalCostUSD += cost.USD
			costByProject[s.ProjectDir] += cost.USD
		}
		for t, c := range s.ToolCalls {
			toolGlobal[t] += c
		}
	}
	fmt.Fprintf(&b, "TOTAL geral\n─────────────\n")
	fmt.Fprintf(&b, "Sessions: %d   Msgs: %d   Custo total: $%.2f USD\n",
		len(v.sessions), totalMsgs, totalCostUSD)
	if v.pricing != nil && v.pricing.BRLRate > 0 {
		fmt.Fprintf(&b, "(~R$ %.2f a câmbio %.2f)\n", totalCostUSD*v.pricing.BRLRate, v.pricing.BRLRate)
	}
	fmt.Fprintf(&b, "\n7 dias: %s\n", renderSparkline(v.sessions))
	fmt.Fprintf(&b, "\nTop projetos\n────────────\n")
	type kv struct {
		k string
		v float64
	}
	var pairs []kv
	for k, c := range costByProject {
		pairs = append(pairs, kv{k, c})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
	for i, p := range pairs {
		if i >= 5 {
			break
		}
		fmt.Fprintf(&b, "  $%-8.2f %s\n", p.v, p.k)
	}
	fmt.Fprintf(&b, "\nTop tools\n──────\n")
	var toolPairs []kv
	for k, c := range toolGlobal {
		toolPairs = append(toolPairs, kv{k, float64(c)})
	}
	sort.Slice(toolPairs, func(i, j int) bool { return toolPairs[i].v > toolPairs[j].v })
	for i, p := range toolPairs {
		if i >= 8 {
			break
		}
		fmt.Fprintf(&b, "  %-15s %d\n", p.k, int(p.v))
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

// renderSparkline produz string de 7 chars representando sessions/dia dos últimos 7 dias.
func renderSparkline(sessions []*model.Session) string {
	now := time.Now()
	bins := make([]int, 7)
	for _, s := range sessions {
		days := int(now.Sub(s.StartTime).Hours() / 24)
		if days >= 0 && days < 7 {
			bins[6-days]++
		}
	}
	chars := []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	max := 1
	for _, c := range bins {
		if c > max {
			max = c
		}
	}
	var sb strings.Builder
	for _, c := range bins {
		idx := c * (len(chars) - 1) / max
		sb.WriteString(chars[idx])
	}
	return sb.String()
}

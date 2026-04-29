package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
	"github.com/felipeness/claude-history/internal/stats"
)

type behaviorView struct {
	bigrams   []stats.Bigram
	trigrams  []stats.Trigram
	coOccur   []stats.CoOccur
	flow      stats.FlowSummary
	style     stats.StyleStats
	highErr   []stats.ErrorSession
	timeCost  []stats.TimeCostPoint
}

func newBehaviorView(sessions []*model.Session, p *pricing.Pricing) behaviorView {
	return behaviorView{
		bigrams:  stats.TopBigrams(sessions, 15),
		trigrams: stats.TopTrigrams(sessions, 8),
		coOccur:  stats.CoOccurrences(sessions, 3, 20),
		flow:     stats.FlowDistribution(sessions),
		style:    stats.StyleComparison(sessions),
		highErr:  stats.HighErrorSessions(sessions, 0.15),
		timeCost: stats.TimeCostPoints(sessions, p),
	}
}

func (v behaviorView) View(width int) string {
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	var b strings.Builder

	// Bigrams + Trigrams lado a lado (texto)
	fmt.Fprintln(&b, header.Render("🔗 Bigrams (top 15)"))
	for _, bg := range v.bigrams {
		fmt.Fprintf(&b, "  %-25s %d\n", bg.A+" "+bg.B, bg.Count)
	}
	b.WriteByte('\n')

	fmt.Fprintln(&b, header.Render("🔗🔗 Trigrams (top 8)"))
	for _, tg := range v.trigrams {
		fmt.Fprintf(&b, "  %-32s %d\n", tg.A+" "+tg.B+" "+tg.C, tg.Count)
	}
	b.WriteByte('\n')

	// Co-occurrence
	fmt.Fprintln(&b, header.Render("🕸️ Co-occurrence (top 20 — PMI)"))
	for _, c := range v.coOccur {
		fmt.Fprintf(&b, "  %-30s n=%-3d  PMI=%.2f\n", c.A+" ↔ "+c.B, c.Count, c.PMI)
	}
	b.WriteByte('\n')

	// Conversation flow
	fmt.Fprintln(&b, header.Render("💬 Conversation flow"))
	fmt.Fprintf(&b, "p50: %d  p90: %d  p99: %d msgs/session\n", v.flow.P50, v.flow.P90, v.flow.P99)
	maxFlow := 1
	for _, h := range v.flow.Hist {
		if h.Count > maxFlow {
			maxFlow = h.Count
		}
	}
	for _, h := range v.flow.Hist {
		bar := BarChart(fmt.Sprintf("%-10s", h.Bucket), float64(h.Count), float64(maxFlow), 22, colorAccent)
		fmt.Fprintf(&b, "%s %d\n", bar, h.Count)
	}
	b.WriteByte('\n')

	// Style comparison
	fmt.Fprintln(&b, header.Render("🆚 Você vs IA — estilo"))
	fmt.Fprintf(&b, "  %-20s %15s  %15s\n", "métrica", "você", "IA")
	fmt.Fprintf(&b, "  %s\n", strings.Repeat("─", 55))
	fmt.Fprintf(&b, "  %-20s %15.1f  %15.1f\n", "avg palavras/msg", v.style.AvgWordsUser, v.style.AvgWordsAssistant)
	fmt.Fprintf(&b, "  %-20s %15d  %15d\n", "vocabulário único", v.style.UniqueWordsUser, v.style.UniqueWordsAssistant)
	fmt.Fprintf(&b, "  %-20s %15.1f  %15.1f\n", "avg sentenças/msg", v.style.AvgSentencesUser, v.style.AvgSentencesAssistant)
	b.WriteByte('\n')

	if len(v.style.TopWordsUser) > 0 || len(v.style.TopWordsAssistant) > 0 {
		fmt.Fprintln(&b, "  Top palavras:")
		max := len(v.style.TopWordsUser)
		if len(v.style.TopWordsAssistant) > max {
			max = len(v.style.TopWordsAssistant)
		}
		for i := 0; i < max && i < 8; i++ {
			u := ""
			a := ""
			if i < len(v.style.TopWordsUser) {
				u = fmt.Sprintf("%s (%d)", v.style.TopWordsUser[i].Word, v.style.TopWordsUser[i].Count)
			}
			if i < len(v.style.TopWordsAssistant) {
				a = fmt.Sprintf("%s (%d)", v.style.TopWordsAssistant[i].Word, v.style.TopWordsAssistant[i].Count)
			}
			fmt.Fprintf(&b, "    %-25s %-25s\n", u, a)
		}
	}
	b.WriteByte('\n')

	// High-error
	fmt.Fprintln(&b, header.Render("🔁 High-error sessions (>15% retrabalho)"))
	if len(v.highErr) == 0 {
		fmt.Fprintln(&b, "  (nenhuma — saudável)")
	} else {
		for i, e := range v.highErr {
			if i >= 10 {
				break
			}
			fmt.Fprintf(&b, "  %.0f%%  %s  %d/%d msgs  %s\n",
				e.ErrorRate*100,
				e.Session.SessionID[:8],
				e.Hits, e.Total,
				e.Session.ProjectDir,
			)
		}
	}

	return lipgloss.NewStyle().Width(width).Render(b.String())
}

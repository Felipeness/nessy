package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/pricing"
	"github.com/felipeness/nessy/internal/stats"
)

// behaviorView guarda input pro lazy compute. As stats pesadas (n-grams,
// co-occurrence, flow distribution) custavam ~50s no startup com 60+ sessions,
// quase 100% do cold start — agora rodam async via behaviorComputeCmd quando
// usuario entra na tab pela primeira vez.
type behaviorView struct {
	sessions []*model.Session
	pricing  *pricing.Pricing

	bigrams  []stats.Bigram
	trigrams []stats.Trigram
	coOccur  []stats.CoOccur
	flow     stats.FlowSummary
	style    stats.StyleStats
	highErr  []stats.ErrorSession
	timeCost []stats.TimeCostPoint

	loading  bool
	computed bool

	// scroll offset em linhas — ↑↓ ajustam pra rolar o body inteiro,
	// que tem ~80 linhas e nao cabe num terminal padrao.
	scroll int
}

// Scroll ajusta o offset por delta linhas; clamping fica a cargo de View.
func (v *behaviorView) Scroll(delta int) { v.scroll += delta }

func newBehaviorView(sessions []*model.Session, p *pricing.Pricing) behaviorView {
	return behaviorView{sessions: sessions, pricing: p}
}

// behaviorComputedMsg carrega o resultado do compute async pra o Update aplicar.
type behaviorComputedMsg struct {
	bigrams  []stats.Bigram
	trigrams []stats.Trigram
	coOccur  []stats.CoOccur
	flow     stats.FlowSummary
	style    stats.StyleStats
	highErr  []stats.ErrorSession
	timeCost []stats.TimeCostPoint
}

// behaviorComputeCmd dispara as stats pesadas em goroutine. Devolve um msg
// pra Update aplicar; nunca bloqueia o render loop.
func behaviorComputeCmd(sessions []*model.Session, p *pricing.Pricing) tea.Cmd {
	return func() tea.Msg {
		return behaviorComputedMsg{
			bigrams:  stats.TopBigrams(sessions, 15),
			trigrams: stats.TopTrigrams(sessions, 8),
			coOccur:  stats.CoOccurrences(sessions, 3, 20),
			flow:     stats.FlowDistribution(sessions),
			style:    stats.StyleComparison(sessions),
			highErr:  stats.HighErrorSessions(sessions, 0.15),
			timeCost: stats.TimeCostPoints(sessions, p),
		}
	}
}

func (v behaviorView) View(width, height int) string {
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	var b strings.Builder

	if !v.computed {
		fmt.Fprintln(&b, header.Render("🔬 Behavior"))
		if v.loading {
			fmt.Fprintln(&b, muted.Render("computando n-grams, co-occurrence, flow distribution…"))
			fmt.Fprintln(&b, muted.Render("(primeira vez nessa tab — pode levar até ~1min com muitas sessions)"))
		} else {
			fmt.Fprintln(&b, muted.Render("aperte qualquer tecla pra começar a computar."))
		}
		return lipgloss.NewStyle().Width(width).Padding(1, 2).Render(b.String())
	}

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

	rendered := lipgloss.NewStyle().Width(width).Render(b.String())
	lines := strings.Split(rendered, "\n")
	return scrollByOffset(lines, v.scroll, height)
}

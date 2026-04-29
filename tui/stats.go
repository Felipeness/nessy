package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
	"github.com/felipeness/claude-history/internal/stats"
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
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	totalMsgs := 0
	totalCostUSD := 0.0
	costByProject := map[string]float64{}
	toolGlobal := map[string]int{}
	for _, s := range v.sessions {
		totalMsgs += s.MessageCount
		if v.pricing != nil {
			if cost, ok := v.pricing.Cost(s); ok {
				totalCostUSD += cost.USD
				costByProject[s.ProjectDir] += cost.USD
			}
		}
		for t, c := range s.ToolCalls {
			toolGlobal[t] += c
		}
	}

	// C3 — Custo cumulativo + projeção
	mc := stats.CostThisMonth(v.sessions, v.pricing)
	fmt.Fprintln(&b, header.Render("📅 Mês atual"))
	fmt.Fprintf(&b, "Acumulado: $%.2f · Hoje: $%.2f · Projeção fim mês: $%.2f\n",
		mc.Accumulated, mc.Today, mc.Projection)
	if mc.Today > 10 {
		fmt.Fprintln(&b, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("⚠ Hoje passou do alert threshold ($10/dia)"))
	} else if mc.Today > 5 {
		fmt.Fprintln(&b, lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("⚠ Hoje passou do warn threshold ($5/dia)"))
	}
	b.WriteByte('\n')

	// C7 — Cache savings
	savings := stats.CacheSavings(v.sessions, v.pricing, 30)
	if savings > 0 {
		fmt.Fprintln(&b, header.Render("💾 Cache savings (30d)"))
		fmt.Fprintf(&b, "$%.2f economizados em cache hits\n\n", savings)
	}

	// C1 — Heatmap hora × dia
	fmt.Fprintln(&b, header.Render("🔥 Atividade (12 semanas)"))
	grid := stats.HeatmapGrid(v.sessions, 12)
	hourLabels := []string{"00-04", "04-08", "08-12", "12-16", "16-20", "20-24"}
	dayLabels := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	fmt.Fprintf(&b, "       %s\n", strings.Join(dayLabels, " "))
	max := 1
	for _, row := range grid {
		for _, v := range row {
			if v > max {
				max = v
			}
		}
	}
	chars := []string{"·", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	for i, row := range grid {
		fmt.Fprintf(&b, "%s ", hourLabels[i])
		for _, v := range row {
			idx := v * (len(chars) - 1) / max
			fmt.Fprintf(&b, " %s ", chars[idx])
		}
		b.WriteByte('\n')
	}
	b.WriteByte('\n')

	// C2 — Distribuição modelos
	dist := stats.ModelDistribution(v.sessions)
	fmt.Fprintln(&b, header.Render("🤖 Distribuição de modelos"))
	type kv struct {
		k string
		v int
	}
	var modelPairs []kv
	totalModelMsgs := 0
	for k, c := range dist {
		modelPairs = append(modelPairs, kv{k, c})
		totalModelMsgs += c
	}
	sort.Slice(modelPairs, func(i, j int) bool {
		if modelPairs[i].v != modelPairs[j].v {
			return modelPairs[i].v > modelPairs[j].v
		}
		return modelPairs[i].k < modelPairs[j].k
	})
	for _, p := range modelPairs {
		bar := BarChart(fmt.Sprintf("%-20s", modelShort(p.k)), float64(p.v), float64(totalModelMsgs), 18, ModelColor(p.k))
		fmt.Fprintf(&b, "%s %d\n", bar, p.v)
	}
	b.WriteByte('\n')

	// Sessions / Msgs / Custo
	fmt.Fprintln(&b, header.Render("📊 Total"))
	fmt.Fprintf(&b, "Sessions: %d · Msgs: %d · Custo: $%.2f USD\n",
		len(v.sessions), totalMsgs, totalCostUSD)
	if v.pricing != nil && v.pricing.BRLRate > 0 {
		fmt.Fprintf(&b, "(~R$ %.2f a câmbio %.2f)\n", totalCostUSD*v.pricing.BRLRate, v.pricing.BRLRate)
	}
	b.WriteByte('\n')

	// Tendências
	wd := stats.WeekDeltaFor(v.sessions, v.pricing)
	fmt.Fprintln(&b, header.Render("📈 Esta semana vs anterior"))
	fmt.Fprintf(&b, "Sessions  %d → %d  %s\n", wd.LastWeek.Sessions, wd.ThisWeek.Sessions,
		deltaArrow(float64(wd.ThisWeek.Sessions), float64(wd.LastWeek.Sessions)))
	fmt.Fprintf(&b, "Msgs    %d → %d  %s\n", wd.LastWeek.Msgs, wd.ThisWeek.Msgs,
		deltaArrow(float64(wd.ThisWeek.Msgs), float64(wd.LastWeek.Msgs)))
	fmt.Fprintf(&b, "Custo  $%.2f → $%.2f  %s\n\n", wd.LastWeek.CostUSD, wd.ThisWeek.CostUSD,
		deltaArrow(wd.ThisWeek.CostUSD, wd.LastWeek.CostUSD))

	// Top projetos
	fmt.Fprintln(&b, header.Render("📁 Top projetos"))
	var projPairs []struct {
		k string
		v float64
	}
	for k, c := range costByProject {
		projPairs = append(projPairs, struct {
			k string
			v float64
		}{k, c})
	}
	sort.Slice(projPairs, func(i, j int) bool {
		if projPairs[i].v != projPairs[j].v {
			return projPairs[i].v > projPairs[j].v
		}
		return projPairs[i].k < projPairs[j].k
	})
	for i, p := range projPairs {
		if i >= 5 {
			break
		}
		fmt.Fprintf(&b, "  $%-8.2f %s\n", p.v, p.k)
	}
	b.WriteByte('\n')

	// C5 — Long-tail
	fmt.Fprintln(&b, header.Render("🐢 Top 5 mais caras"))
	for _, s := range stats.LongTailByCost(append([]*model.Session{}, v.sessions...), v.pricing, 5) {
		c, _ := v.pricing.Cost(s)
		fmt.Fprintf(&b, "  $%-7.2f %s  %d msgs  %s\n",
			c.USD, s.SessionID[:8], s.MessageCount, fmtDuration(s.Duration()))
	}
	b.WriteByte('\n')

	// F1 — Top palavras
	fmt.Fprintln(&b, header.Render("🗣️ Suas palavras mais usadas"))
	words := stats.TopWords(v.sessions, 15)
	for i, w := range words {
		if i >= 15 {
			break
		}
		fmt.Fprintf(&b, "  %-18s %d\n", w.Word, w.Count)
		if i == 4 {
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')

	// F2 — Padrões de retrabalho
	rate, hits, totalMsgs := stats.ErrorRate(v.sessions)
	rateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	rateLabel := "saudável"
	switch {
	case rate > 0.15:
		rateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		rateLabel = "alto"
	case rate > 0.05:
		rateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
		rateLabel = "moderado"
	}
	fmt.Fprintln(&b, header.Render("🔁 Sinais de retrabalho"))
	fmt.Fprintf(&b, "%d msgs (%.0f%% de %d total) — %s\n\n",
		hits, rate*100, totalMsgs, rateStyle.Render(rateLabel))

	// F3 — Prefixos comuns
	fmt.Fprintln(&b, header.Render("✏️ Como você inicia mensagens"))
	prefs := stats.TopPrefixes(v.sessions, 8)
	for _, p := range prefs {
		fmt.Fprintf(&b, "  %-15s %d\n", p.Word, p.Count)
	}
	b.WriteByte('\n')

	// F4 — Horário de pico
	fmt.Fprintln(&b, header.Render("⏰ Quando você usa Claude Code"))
	bins := stats.PeakHour(v.sessions)
	binsSlice := bins[:]
	fmt.Fprintf(&b, "%s\n", Sparkline(binsSlice))
	fmt.Fprintln(&b, "0h──────6h──────12h──────18h──────24h")
	b.WriteByte('\n')

	// Top tools
	fmt.Fprintln(&b, header.Render("🔧 Top tools globais"))
	var toolPairs []kv
	for k, c := range toolGlobal {
		toolPairs = append(toolPairs, kv{k, c})
	}
	sort.Slice(toolPairs, func(i, j int) bool {
		if toolPairs[i].v != toolPairs[j].v {
			return toolPairs[i].v > toolPairs[j].v
		}
		return toolPairs[i].k < toolPairs[j].k
	})
	maxTool := 1
	for _, p := range toolPairs {
		if p.v > maxTool {
			maxTool = p.v
		}
	}
	for i, p := range toolPairs {
		if i >= 8 {
			break
		}
		bar := BarChart(fmt.Sprintf("%-15s", p.k), float64(p.v), float64(maxTool), 18, toolColor(p.k))
		fmt.Fprintf(&b, "%s %d\n", bar, p.v)
	}

	return lipgloss.NewStyle().Width(width).Render(b.String())
}

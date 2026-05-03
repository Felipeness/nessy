package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/pricing"
	"github.com/felipeness/nessy/internal/stats"
)

type costsView struct {
	sessions []*model.Session
	pricing  *pricing.Pricing

	scroll int
}

func (v *costsView) Scroll(delta int) { v.scroll += delta }

func newCostsView(sessions []*model.Session, p *pricing.Pricing) costsView {
	return costsView{sessions: sessions, pricing: p}
}

func (v costsView) View(width, height int) string {
	if v.pricing == nil {
		return "(pricing.toml não carregado)"
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	var b strings.Builder

	// Mês
	mc := stats.CostThisMonth(v.sessions, v.pricing)
	fmt.Fprintln(&b, header.Render("📅 Mês atual"))
	fmt.Fprintf(&b, "Acumulado: $%.2f · Hoje: $%.2f · Projeção: $%.2f (final do mês, %d dias)\n\n",
		mc.Accumulated, mc.Today, mc.Projection, mc.Days)

	// Por dia (últimos 30)
	fmt.Fprintln(&b, header.Render("📊 Custo por dia (últimos 30)"))
	now := time.Now()
	days := 30
	bins := make([]float64, days)
	for _, s := range v.sessions {
		d := int(now.Sub(s.StartTime).Hours() / 24)
		if d >= 0 && d < days {
			c, ok := v.pricing.Cost(s)
			if ok {
				bins[days-1-d] += c.USD
			}
		}
	}
	maxDay := 0.01
	for _, c := range bins {
		if c > maxDay {
			maxDay = c
		}
	}
	for i := 0; i < days; i++ {
		dt := now.AddDate(0, 0, -(days - 1 - i))
		bar := BarChart("", bins[i], maxDay, 25, lipgloss.Color("39"))
		fmt.Fprintf(&b, "  %s  %s $%.2f\n", dt.Format("Jan 02"), bar, bins[i])
	}
	b.WriteByte('\n')

	// Por projeto (todos)
	fmt.Fprintln(&b, header.Render("📁 Custo por projeto"))
	costByProj := map[string]float64{}
	totalProj := 0.0
	for _, s := range v.sessions {
		if c, ok := v.pricing.Cost(s); ok {
			costByProj[s.ProjectDir] += c.USD
			totalProj += c.USD
		}
	}
	type kv struct {
		k string
		v float64
	}
	var pairs []kv
	for k, c := range costByProj {
		pairs = append(pairs, kv{k, c})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	maxProj := 0.01
	if len(pairs) > 0 {
		maxProj = pairs[0].v
	}
	for i, p := range pairs {
		if i >= 10 {
			break
		}
		bar := BarChart("", p.v, maxProj, 25, lipgloss.Color("39"))
		dir := p.k
		if len(dir) > 35 {
			dir = "…" + dir[len(dir)-34:]
		}
		fmt.Fprintf(&b, "  %s $%-7.2f  %s\n", bar, p.v, dir)
	}
	b.WriteByte('\n')

	// Por modelo
	fmt.Fprintln(&b, header.Render("🤖 Custo por modelo"))
	costByModel := map[string]float64{}
	for _, s := range v.sessions {
		if c, ok := v.pricing.Cost(s); ok {
			costByModel[s.Model] += c.USD
		}
	}
	var mPairs []kv
	maxModel := 0.01
	for k, c := range costByModel {
		mPairs = append(mPairs, kv{k, c})
		if c > maxModel {
			maxModel = c
		}
	}
	sort.Slice(mPairs, func(i, j int) bool {
		if mPairs[i].v != mPairs[j].v {
			return mPairs[i].v > mPairs[j].v
		}
		return mPairs[i].k < mPairs[j].k
	})
	for _, p := range mPairs {
		bar := BarChart("", p.v, maxModel, 25, ModelColor(p.k))
		fmt.Fprintf(&b, "  %s $%-7.2f  %s %s\n", bar, p.v, ModelBadge(p.k), modelShort(p.k))
	}
	b.WriteByte('\n')

	// Cache savings
	savings := stats.CacheSavings(v.sessions, v.pricing, 30)
	fmt.Fprintln(&b, header.Render("💾 Cache savings"))
	ratio := 0.0
	if totalProj > 0 {
		ratio = savings / totalProj
	}
	fmt.Fprintf(&b, "$%.2f economizados em 30d (%.1fx return vs gasto)\n", savings, ratio)

	rendered := lipgloss.NewStyle().Width(width).Render(b.String())
	lines := strings.Split(rendered, "\n")
	return scrollByOffset(lines, v.scroll, height)
}

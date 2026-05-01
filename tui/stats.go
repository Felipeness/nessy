package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/index"
	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
	"github.com/felipeness/claude-history/internal/stats"
)

type statsMode int

const (
	statsModeOverview statsMode = iota // dashboard (heatmap + cards)
	statsModeModels                    // line chart por modelo + breakdown
	statsModeDetailed                  // analytics detalhada (renderGlobal antigo)
)

type statsPeriod int

const (
	periodAll statsPeriod = iota
	period7d
	period30d
)

func (p statsPeriod) label() string {
	switch p {
	case period7d:
		return "Last 7 days"
	case period30d:
		return "Last 30 days"
	default:
		return "All time"
	}
}

func (p statsPeriod) days() int {
	switch p {
	case period7d:
		return 7
	case period30d:
		return 30
	default:
		return 0 // 0 = sem filtro
	}
}

type statsView struct {
	sessions  []*model.Session
	pricing   *pricing.Pricing
	db        *index.DB // pra DetectLoops e queries SQL diretas
	showLocal bool
	mode      statsMode
	period    statsPeriod

	// scroll é o offset (em linhas) do início do body. ↑↓ ajustam.
	// Necessário porque o body do Detailed é longo e não cabe num terminal
	// padrão; lipgloss.Height clampa truncando o final.
	scroll int

	// Caches por (mode, period, width). Evita re-render a cada keystroke.
	cache map[string]string
}

// Scroll move o offset por delta linhas, clamping em [0, +∞). Saturação
// do limite superior fica a cargo do dispatcher (que conhece o nº de linhas
// renderizadas).
func (v *statsView) Scroll(delta int) {
	v.scroll += delta
	if v.scroll < 0 {
		v.scroll = 0
	}
}

// ResetScroll volta pro topo. Útil ao mudar de modo/período.
func (v *statsView) ResetScroll() { v.scroll = 0 }

func newStatsView(sessions []*model.Session, p *pricing.Pricing, db *index.DB) statsView {
	return statsView{
		sessions: sessions, pricing: p, db: db,
		mode: statsModeOverview, period: periodAll,
		cache: map[string]string{},
	}
}

// invalidate limpa o cache (chame quando sessions mudam ou flag relevante muda).
func (v *statsView) invalidate() {
	v.cache = map[string]string{}
}

// ToggleMode cicla overview → models → detailed → overview.
func (v *statsView) ToggleMode() {
	v.mode = (v.mode + 1) % 3
	v.scroll = 0
}

// TogglePeriod cicla all → 7d → 30d → all.
func (v *statsView) TogglePeriod() {
	v.period = (v.period + 1) % 3
	v.scroll = 0
	v.invalidate()
}

// filteredSessions aplica o filtro de período ao slice. Retorna slice filtrado
// (pode ser o original se period == all).
func (v statsView) filteredSessions() []*model.Session {
	d := v.period.days()
	if d == 0 {
		return v.sessions
	}
	cutoff := time.Now().Add(-time.Duration(d) * 24 * time.Hour)
	var out []*model.Session
	for _, s := range v.sessions {
		if s.StartTime.After(cutoff) {
			out = append(out, s)
		}
	}
	return out
}

// renderGlobal é o dispatcher: header com modos + filtro + corpo do modo selecionado.
// Body tem cache por (mode, period, width) — header sempre rerenderiza pra
// refletir destaque das sub-tabs/período. Aplica scroll por skip de linhas
// no início do body (lipgloss.Height clampa truncando o final naturalmente).
func (v statsView) renderGlobal(width int) string {
	header := v.renderModeHeader(width) + "\n"
	cacheKey := fmt.Sprintf("%d:%d:%d", v.mode, v.period, width)
	body, ok := v.cache[cacheKey]
	if !ok {
		switch v.mode {
		case statsModeOverview:
			body = v.renderOverview(width)
		case statsModeModels:
			body = v.renderModels(width)
		default:
			body = v.renderDetailed(width)
		}
		v.cache[cacheKey] = body
	}
	if v.scroll > 0 {
		lines := strings.Split(body, "\n")
		if v.scroll >= len(lines) {
			body = ""
		} else {
			body = strings.Join(lines[v.scroll:], "\n")
		}
	}
	return header + body
}

// renderModeHeader desenha sub-tabs Overview/Models/Detailed e filtro de período.
func (v statsView) renderModeHeader(width int) string {
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	active := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Underline(true)
	inactive := muted

	mkTab := func(label string, mine statsMode) string {
		if v.mode == mine {
			return active.Render(label)
		}
		return inactive.Render(label)
	}
	tabs := mkTab("Overview", statsModeOverview) + "  " +
		mkTab("Models", statsModeModels) + "  " +
		mkTab("Detailed", statsModeDetailed)

	mkPeriod := func(label string, mine statsPeriod) string {
		if v.period == mine {
			return active.Render(label)
		}
		return inactive.Render(label)
	}
	periodStrip := mkPeriod("All time", periodAll) + "  " +
		mkPeriod("Last 7 days", period7d) + "  " +
		mkPeriod("Last 30 days", period30d)

	hint := muted.Render("[m] modo  [p] período")
	border := muted.Render(strings.Repeat("─", maxInt(0, width)))
	return tabs + "\n" + periodStrip + "    " + hint + "\n" + border
}

// renderDetailed é o renderGlobal antigo — análise extensa.
func (v statsView) renderDetailed(width int) string {
	var b strings.Builder
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	sessions := v.filteredSessions()

	totalMsgs := 0
	totalCostUSD := 0.0
	costByProject := map[string]float64{}
	toolGlobal := map[string]int{}
	for _, s := range sessions {
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
	mc := stats.CostThisMonth(sessions, v.pricing)
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
	savings := stats.CacheSavings(sessions, v.pricing, 30)
	if savings > 0 {
		fmt.Fprintln(&b, header.Render("💾 Cache savings (30d)"))
		fmt.Fprintf(&b, "$%.2f economizados em cache hits\n\n", savings)
	}

	// C1 — Heatmap hora × dia
	fmt.Fprintln(&b, header.Render("🔥 Atividade (12 semanas)"))
	grid := stats.HeatmapGrid(sessions, 12)
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
	dist := stats.ModelDistribution(sessions)
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
		len(sessions), totalMsgs, totalCostUSD)
	if v.pricing != nil && v.pricing.BRLRate > 0 {
		fmt.Fprintf(&b, "(~R$ %.2f a câmbio %.2f)\n", totalCostUSD*v.pricing.BRLRate, v.pricing.BRLRate)
	}
	b.WriteByte('\n')

	// Tendências
	wd := stats.WeekDeltaFor(sessions, v.pricing)
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
	for _, s := range stats.LongTailByCost(append([]*model.Session{}, sessions...), v.pricing, 5) {
		c, _ := v.pricing.Cost(s)
		fmt.Fprintf(&b, "  $%-7.2f %s  %d msgs  %s\n",
			c.USD, s.SessionID[:8], s.MessageCount, fmtDuration(s.Duration()))
	}
	b.WriteByte('\n')

	// F1 — Top palavras
	fmt.Fprintln(&b, header.Render("🗣️ Suas palavras mais usadas"))
	words := stats.TopWords(sessions, 15)
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
	rate, hits, totalMsgs := stats.ErrorRate(sessions)
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
	prefs := stats.TopPrefixes(sessions, 8)
	for _, p := range prefs {
		fmt.Fprintf(&b, "  %-15s %d\n", p.Word, p.Count)
	}
	b.WriteByte('\n')

	// F4 — Horário de pico
	fmt.Fprintln(&b, header.Render("⏰ Quando você usa Claude Code"))
	bins := stats.PeakHour(sessions)
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

	// 🔁 Repetições suspeitas — mesmo (tool, input) ≥3× em ≤30min.
	// Janela larga porque dados retroativos incluem pausa humana.
	if v.db != nil {
		loops, err := v.db.DetectLoops(3, 1800)
		if err == nil && len(loops) > 0 {
			b.WriteByte('\n')
			fmt.Fprintln(&b, header.Render("🔁 Repetições suspeitas (≥3× mesma input ≤30min)"))
			warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
			critStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
			muted := lipgloss.NewStyle().Foreground(colorMuted)
			for i, h := range loops {
				if i >= 8 {
					break
				}
				countStyle := warnStyle
				if h.Count >= 5 {
					countStyle = critStyle
				}
				when := h.FirstAt.Local().Format("02/01 15:04")
				fmt.Fprintf(&b, "  %s %s %s %s %s\n",
					countStyle.Bold(true).Render(fmt.Sprintf("%d×", h.Count)),
					lipgloss.NewStyle().Foreground(colorFg).Render(fmt.Sprintf("%-12s", h.ToolName)),
					muted.Render(fmt.Sprintf("[%s]", h.SessionID[:8])),
					muted.Render(when),
					muted.Render(fmt.Sprintf("· span %s", fmtSecs(h.SpanSecs))))
			}
		}
	}

	return lipgloss.NewStyle().Width(width).Render(b.String())
}

// fmtSecs formata segundos em "Xs", "Xm Ys" ou "Xh Ym".
func fmtSecs(s float64) string {
	if s < 60 {
		return fmt.Sprintf("%.0fs", s)
	}
	if s < 3600 {
		return fmt.Sprintf("%dm%ds", int(s)/60, int(s)%60)
	}
	return fmt.Sprintf("%dh%dm", int(s)/3600, (int(s)%3600)/60)
}

// =============================================================================
// renderOverview — dashboard estilo /status do Claude Code:
//   1. Calendar heatmap GitHub-style (Mon-Sun × 12 meses)
//   2. Cards com métricas resumidas
// =============================================================================

func (v statsView) renderOverview(width int) string {
	sessions := v.filteredSessions()
	if len(sessions) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render(
			"(nenhuma session no período " + v.period.label() + ")")
	}
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	var b strings.Builder

	// Heatmap calendário — 12 meses, Mon..Sun rows × N weeks cols
	months := 12
	if v.period == period7d {
		months = 1
	} else if v.period == period30d {
		months = 2
	}
	grid, firstMonday, weeks := stats.CalendarHeatmap(sessions, months)
	maxV := 1
	for _, row := range grid {
		for _, c := range row {
			if c > maxV {
				maxV = c
			}
		}
	}
	chars := []string{"·", "░", "▒", "▓", "█"}
	colors := []lipgloss.Color{colorMuted, "#16573a", "#1f7a4d", "#2e9d61", "#3fbf76"}

	// Header com mês labels (uma label por coluna onde o mês vira)
	b.WriteString("       ") // 7 espaços pra alinhar com day labels (4) + 3 padding
	monthLine := []rune(strings.Repeat(" ", weeks*2))
	prevMonth := ""
	for w := 0; w < weeks; w++ {
		day := firstMonday.AddDate(0, 0, w*7)
		m := day.Format("Jan")
		if m != prevMonth {
			for i, r := range []rune(m) {
				if w*2+i < len(monthLine) {
					monthLine[w*2+i] = r
				}
			}
			prevMonth = m
		}
	}
	b.WriteString(muted.Render(string(monthLine)) + "\n")

	// Day rows (Mon..Sun, label só pra Mon/Wed/Fri)
	dayLabels := []string{"Mon", "   ", "Wed", "   ", "Fri", "   ", "   "}
	for r := 0; r < 7; r++ {
		b.WriteString("   " + muted.Render(dayLabels[r]) + " ")
		for w := 0; w < weeks; w++ {
			val := grid[r][w]
			idx := 0
			if maxV > 0 {
				idx = val * (len(chars) - 1) / maxV
			}
			ch := chars[idx]
			b.WriteString(lipgloss.NewStyle().Foreground(colors[idx]).Render(ch + " "))
		}
		b.WriteString("\n")
	}

	// Legend
	b.WriteString("\n   " + muted.Render("Less "))
	for i := 0; i < len(chars); i++ {
		b.WriteString(lipgloss.NewStyle().Foreground(colors[i]).Render(chars[i] + " "))
	}
	b.WriteString(muted.Render("More") + "\n\n")

	// Cards de métricas
	ov := stats.BuildOverview(sessions, v.pricing)
	cardL := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	val := lipgloss.NewStyle().Foreground(colorFg)

	twoCol := func(l1, v1, l2, v2 string) string {
		left := fmt.Sprintf("   %s %s",
			cardL.Render(fmt.Sprintf("%-18s", l1+":")),
			val.Render(v1))
		right := fmt.Sprintf("%s %s",
			cardL.Render(fmt.Sprintf("%-18s", l2+":")),
			val.Render(v2))
		// padding entre colunas
		leftW := lipgloss.Width(stripAnsi(left))
		pad := maxInt(2, 44-leftW)
		return left + strings.Repeat(" ", pad) + right + "\n"
	}

	mostActive := "—"
	if !ov.MostActiveDay.IsZero() {
		mostActive = fmt.Sprintf("%s (%d sessions)",
			ov.MostActiveDay.Format("Jan 02"), ov.MostActiveCount)
	}
	streakRange := fmt.Sprintf("%d/%d", ov.ActiveDays, ov.TotalDays)

	b.WriteString(twoCol("Favorite model", modelShort(ov.FavoriteModel),
		"Total tokens", humanizeTokens(ov.TotalTokens)))
	b.WriteString(twoCol("Sessions", fmt.Sprintf("%d", ov.TotalSessions),
		"Longest session", fmtDuration(ov.LongestSession)))
	b.WriteString(twoCol("Active days", streakRange,
		"Longest streak", fmt.Sprintf("%d days", ov.LongestStreak)))
	b.WriteString(twoCol("Most active day", mostActive,
		"Current streak", fmt.Sprintf("%d days", ov.CurrentStreak)))
	b.WriteString(twoCol("Total cost", fmt.Sprintf("$%.2f USD", ov.TotalCostUSD),
		"Total messages", fmt.Sprintf("%d", ov.TotalMessages)))

	// Comparação divertida — tokens equivalentes a livros
	// "To Kill a Mockingbird" tem ~100k tokens; usa essa unidade
	b.WriteString("\n")
	if ov.TotalTokens > 100_000 {
		books := float64(ov.TotalTokens) / 100_000
		b.WriteString("   " + muted.Render(fmt.Sprintf(
			"Você usou ~%.1fx mais tokens que To Kill a Mockingbird (~100k tokens/livro).",
			books)) + "\n")
	}

	return lipgloss.NewStyle().Width(width).Render(b.String())
}

// humanizeTokens formata número de tokens em k/m/b com 1 casa decimal.
func humanizeTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fb", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// =============================================================================
// renderModels — line chart tokens/dia por modelo + breakdown por modelo
// =============================================================================

func (v statsView) renderModels(width int) string {
	sessions := v.filteredSessions()
	if len(sessions) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render(
			"(nenhuma session no período " + v.period.label() + ")")
	}
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	var b strings.Builder

	// Agrega tokens por dia × modelo
	type dayKey struct {
		day   string
		model string
	}
	tokens := map[dayKey]int64{}
	dayList := []string{}
	daySeen := map[string]bool{}
	modelSet := map[string]bool{}

	// Agrega tokens por modelo pra filtrar os com 0 antes de iterar dias
	modelTotal := map[string]int64{}
	for _, s := range sessions {
		m := s.Model
		if m == "" || m == "<synthetic>" {
			continue // skip placeholders
		}
		modelTotal[m] += s.TotalTokens()
	}
	for _, s := range sessions {
		m := s.Model
		if m == "" || m == "<synthetic>" {
			continue
		}
		if modelTotal[m] == 0 {
			continue
		}
		k := s.StartTime.Local().Format("2006-01-02")
		tokens[dayKey{k, m}] += s.TotalTokens()
		modelSet[m] = true
		if !daySeen[k] {
			daySeen[k] = true
			dayList = append(dayList, k)
		}
	}
	sort.Strings(dayList)
	if len(dayList) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render(
			"(sem tokens registrados)")
	}
	// Limita a 30 dias mais recentes pra caber no chart
	if len(dayList) > 30 {
		dayList = dayList[len(dayList)-30:]
	}

	var models []string
	for m := range modelSet {
		models = append(models, m)
	}
	sort.Strings(models)

	// Compute total per day (somando models) pro chart "stacked"
	total := make([]int64, len(dayList))
	for i, d := range dayList {
		var sum int64
		for _, m := range models {
			sum += tokens[dayKey{d, m}]
		}
		total[i] = sum
	}
	var maxTokens int64
	for _, v := range total {
		if v > maxTokens {
			maxTokens = v
		}
	}

	// Header
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorAccent).
		Render(" Tokens per Day") + "\n\n")

	// Line chart simples — vertical bars proporcional ao total
	chartH := 8
	chartW := width - 12
	if chartW < 30 {
		chartW = 30
	}
	cellW := chartW / maxInt(1, len(dayList))
	if cellW < 1 {
		cellW = 1
	}

	// Y-axis labels
	yLabels := []string{}
	for i := chartH - 1; i >= 0; i-- {
		v := float64(maxTokens) * float64(i) / float64(maxInt(1, chartH-1))
		yLabels = append(yLabels, humanizeTokens(int64(v)))
	}

	// Render rows
	for row := 0; row < chartH; row++ {
		threshold := float64(maxTokens) * float64(chartH-1-row) / float64(maxInt(1, chartH-1))
		b.WriteString(fmt.Sprintf("%6s ┤", yLabels[row]))
		for _, t := range total {
			ch := " "
			if float64(t) >= threshold && threshold > 0 {
				ch = "▮"
			}
			styled := lipgloss.NewStyle().Foreground(colorAccent).Render(ch)
			b.WriteString(styled + strings.Repeat(" ", maxInt(0, cellW-1)))
		}
		b.WriteString("\n")
	}
	// X-axis
	b.WriteString("       └" + strings.Repeat("─", cellW*len(dayList)) + "\n")
	// X-axis labels (every ~5 days)
	b.WriteString("        ")
	for i, d := range dayList {
		t, _ := time.Parse("2006-01-02", d)
		if i%5 == 0 || i == len(dayList)-1 {
			lbl := t.Format("Jan 02")
			b.WriteString(muted.Render(lbl))
			pad := maxInt(0, cellW*5-len(lbl))
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	b.WriteString("\n\n")

	// Legend de modelos (chips coloridos)
	for _, m := range models {
		b.WriteString(lipgloss.NewStyle().Foreground(ModelColor(m)).Render("●") +
			" " + modelShort(m) + "  ")
	}
	b.WriteString("\n\n")

	// Breakdown por modelo: tokens in/out + percentual
	// Pula placeholders ("" e "<synthetic>") — não fazem sentido no breakdown
	var totalAll int64
	perModel := map[string]struct {
		in, out, total int64
		sessions       int
	}{}
	for _, s := range sessions {
		m := s.Model
		if m == "" || m == "<synthetic>" {
			continue
		}
		x := perModel[m]
		x.in += s.InputTokens + s.CacheCreationTokens + s.CacheReadTokens
		x.out += s.OutputTokens
		x.total += s.TotalTokens()
		x.sessions++
		perModel[m] = x
		totalAll += s.TotalTokens()
	}
	// Sort por total desc, com tiebreaker por nome (deterministic — sem flicker
	// entre modelos zerados como "<synthetic>" e "(unknown)").
	type modelStat struct {
		m              string
		in, out, total int64
		sessions       int
	}
	var stats2 []modelStat
	for m, x := range perModel {
		// Filtra modelos com 0 tokens — são placeholders que poluem
		if x.total == 0 {
			continue
		}
		stats2 = append(stats2, modelStat{m, x.in, x.out, x.total, x.sessions})
	}
	sort.Slice(stats2, func(i, j int) bool {
		if stats2[i].total != stats2[j].total {
			return stats2[i].total > stats2[j].total
		}
		return stats2[i].m < stats2[j].m
	})

	// 2 colunas se couber
	cols := 1
	if width >= 80 {
		cols = 2
	}
	for i, s := range stats2 {
		pct := 0.0
		if totalAll > 0 {
			pct = float64(s.total) / float64(totalAll) * 100
		}
		dot := lipgloss.NewStyle().Foreground(ModelColor(s.m)).Render("●")
		title := fmt.Sprintf("%s %s %s", dot,
			lipgloss.NewStyle().Bold(true).Render(modelShort(s.m)),
			muted.Render(fmt.Sprintf("(%.1f%%)", pct)))
		body := fmt.Sprintf("    In: %s · Out: %s · %d sessions",
			humanizeTokens(s.in), humanizeTokens(s.out), s.sessions)
		b.WriteString(title + "\n" + muted.Render(body))
		if cols == 2 && i%2 == 0 && i+1 < len(stats2) {
			b.WriteString("    ")
		} else {
			b.WriteString("\n")
		}
	}

	return lipgloss.NewStyle().Width(width).Render(b.String())
}

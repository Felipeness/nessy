package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/index"
	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
	"github.com/felipeness/claude-history/internal/stats"
)

// threadView é a tab "Threads". Agrupa sessions em threads (project+branch+gap)
// e renderiza em uma de N views: tree, cards, miller, graph, timeline, galaxy.
// Toggle via tecla 'v' cicla entre elas.
type threadView int

const (
	threadViewTree threadView = iota
	threadViewCards
	threadViewMiller
	threadViewGraph
	threadViewTimeline
	threadViewGalaxy
)

func (v threadView) String() string {
	switch v {
	case threadViewTree:
		return "tree"
	case threadViewCards:
		return "cards"
	case threadViewMiller:
		return "miller"
	case threadViewGraph:
		return "graph"
	case threadViewTimeline:
		return "timeline"
	case threadViewGalaxy:
		return "galaxy"
	}
	return "?"
}

type threadsView struct {
	db        *index.DB
	pricing   *pricing.Pricing
	threads   []*stats.Thread
	summaries map[string]string
	view      threadView
	cursor    int // sessão selecionada (índice na flat list)
	flat      []*flatRow
	gap       time.Duration
}

// flatRow é uma linha "navegável" — pode ser thread header (não-selecionável)
// ou session selecionável. Permite cursor único 1D.
type flatRow struct {
	threadIdx  int
	sessionIdx int // -1 = header da thread
	thread     *stats.Thread
	session    *stats.ThreadSession
}

func newThreadsView(db *index.DB, p *pricing.Pricing, sessions []*model.Session, summaries map[string]string) threadsView {
	gap := 30 * time.Minute
	threads := stats.BuildThreads(sessions, gap)
	for _, t := range threads {
		t.CalcTotals(func(s *model.Session) (float64, bool) {
			if p == nil {
				return 0, false
			}
			c, ok := p.Cost(s)
			return c.USD, ok
		})
	}
	v := threadsView{
		db:        db,
		pricing:   p,
		threads:   threads,
		summaries: summaries,
		view:      threadViewTree,
		gap:       gap,
	}
	v.rebuildFlat()
	return v
}

// rebuildFlat reconstrói a flat list de rows baseada na view atual.
// Pra tree, cada thread vira [header + sessions]. Pra cards, cada thread = 1 row.
// Pra miller, é tratado fora (3 colunas).
func (v *threadsView) rebuildFlat() {
	v.flat = nil
	for ti, t := range v.threads {
		v.flat = append(v.flat, &flatRow{threadIdx: ti, sessionIdx: -1, thread: t})
		for si, s := range t.Sessions {
			v.flat = append(v.flat, &flatRow{
				threadIdx:  ti,
				sessionIdx: si,
				thread:     t,
				session:    s,
			})
		}
	}
}

func (v *threadsView) ToggleView() {
	v.view = (v.view + 1) % 6
	v.rebuildFlat()
}

func (v *threadsView) selected() *model.Session {
	if v.cursor < 0 || v.cursor >= len(v.flat) {
		return nil
	}
	row := v.flat[v.cursor]
	if row.session == nil {
		return nil
	}
	return row.session.Session
}

// MoveCursor avança/recua skipando headers (só para em sessions).
func (v *threadsView) MoveCursor(delta int) {
	if len(v.flat) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
		delta = -delta
	}
	for delta > 0 {
		v.cursor += step
		if v.cursor < 0 {
			v.cursor = 0
			return
		}
		if v.cursor >= len(v.flat) {
			v.cursor = len(v.flat) - 1
			return
		}
		// Skip headers
		if v.flat[v.cursor].session != nil {
			delta--
		}
	}
}

func (v threadsView) View(width, height int) string {
	switch v.view {
	case threadViewTree:
		return v.renderTree(width)
	case threadViewCards:
		return v.renderCards(width)
	case threadViewMiller:
		return v.renderMiller(width)
	case threadViewGraph:
		return v.renderGraph(width)
	case threadViewTimeline:
		return v.renderTimeline(width)
	case threadViewGalaxy:
		return v.renderGalaxy(width, 24)
	}
	return ""
}

// =============================================================================
// View 1 — Tree polido
// =============================================================================

func (v threadsView) renderTree(width int) string {
	if len(v.threads) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Render(
			"  (nenhuma thread — sem sessions indexadas?)")
	}

	now := time.Now()
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	header := lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	cardBorder := lipgloss.NewStyle().Foreground(colorBorder)

	grouped := stats.GroupByProject(v.threads)
	dirs := stats.SortedProjectDirs(grouped)

	var b strings.Builder
	for _, dir := range dirs {
		short := shortPath(dir, mustHomeTUI())
		// Project header
		fmt.Fprintf(&b, "\n%s %s %s\n",
			header.Render("───"),
			lipgloss.NewStyle().Foreground(colorFg).Bold(true).Render(short),
			muted.Render(strings.Repeat("─", maxInt(0, width-len(short)-8))))

		for _, t := range grouped[dir] {
			v.renderThreadCard(&b, t, now, width, cardBorder, muted)
		}
	}
	return b.String()
}

// renderThreadCard renderiza uma thread como "card" com borda arredondada,
// header com pílula da branch + sparkline + stats, sessions indentadas com
// status dots e selection bar full-width quando cursor.
func (v threadsView) renderThreadCard(b *strings.Builder, t *stats.Thread, now time.Time, width int, borderStyle, muted lipgloss.Style) {
	branchStr := branchPill(t.Branch)
	count := len(t.Sessions)
	spark := stats.SparklineFromThread(t)
	cost := fmt.Sprintf("$%.2f", t.TotalCost)
	dur := fmtDuration(t.TotalDur)

	// Header line: ╭─ feat/CC-1234 ─...─╮  ▁▂▅█  3 sessions · 2h15m · $5.20
	headerInner := fmt.Sprintf(" %s ", branchStr)
	headerLeftLen := lipgloss.Width(headerInner)
	// Se branch line for muito longo, cortamos via truncRight
	if headerLeftLen > width-30 {
		// degrada
		branchStr = branchPill(truncRight(t.Branch, 18))
		headerInner = fmt.Sprintf(" %s ", branchStr)
		headerLeftLen = lipgloss.Width(headerInner)
	}

	// Right side metadata
	right := fmt.Sprintf(" %s  %d sess · %s · %s",
		lipgloss.NewStyle().Foreground(colorAccent).Render(spark),
		count, dur, lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(cost))
	rightLen := lipgloss.Width(right)

	// Top border
	dashes := width - headerLeftLen - rightLen - 6
	if dashes < 0 {
		dashes = 0
	}
	top := borderStyle.Render("╭─") + headerInner +
		borderStyle.Render(strings.Repeat("─", dashes)+"╮") +
		right
	b.WriteString(top + "\n")

	// Sessions
	for _, s := range t.Sessions {
		// row index pra detect cursor
		isCursor := false
		idx := v.flatRowIndex(t, s)
		if idx >= 0 && idx == v.cursor {
			isCursor = true
		}

		dot := threadDot(s.Kind, "")
		when := s.StartTime.Local().Format("Mon 15:04")
		dr := fmtDuration(s.Duration())
		title := v.titleFor(s.Session)
		titleMax := width - 50
		if titleMax > 0 && len(title) > titleMax {
			title = title[:titleMax-1] + "…"
		}
		sid := s.SessionID[:8]
		sidStyled := muted.Render("[" + sid + "]")

		// Compact marker
		gapStr := ""
		if s.Kind == "compact" {
			gapStr = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).
				Render(fmt.Sprintf(" ↻ compact (%s)", humanDurShort(s.GapFromPrev)))
		} else if s.Kind == "resumed" {
			gapStr = muted.Render(fmt.Sprintf(" ↻ +%s", humanDurShort(s.GapFromPrev)))
		}

		line := fmt.Sprintf("%s  %s  %s  %-5s  %s%s  %s",
			borderStyle.Render("│"),
			dot,
			muted.Render(when),
			dr,
			lipgloss.NewStyle().Foreground(colorFg).Render(title),
			gapStr,
			sidStyled)

		if isCursor {
			// Full-width selection bar
			plain := stripAnsi(line)
			pad := width - len(plain)
			if pad < 0 {
				pad = 0
			}
			sel := lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(colorAccent).
				Bold(true)
			line = sel.Render("▶ ") + line[2:] + sel.Render(strings.Repeat(" ", pad))
		}
		b.WriteString(line + "\n")
		_ = now
	}

	// Bottom border
	b.WriteString(borderStyle.Render("╰"+strings.Repeat("─", width-1)) + "\n")
}

// flatRowIndex acha índice de uma session na flat list (pra detecção do cursor).
func (v threadsView) flatRowIndex(t *stats.Thread, s *stats.ThreadSession) int {
	for i, row := range v.flat {
		if row.thread == t && row.session == s {
			return i
		}
	}
	return -1
}

// titleFor escolhe AI summary > FirstUserMsg > "(sem mensagens)".
func (v threadsView) titleFor(s *model.Session) string {
	if v.summaries != nil {
		if sum, ok := v.summaries[s.SessionID]; ok && sum != "" {
			return firstSentence(sum)
		}
	}
	if s.FirstUserMsg != "" {
		return s.FirstUserMsg
	}
	return "(sem mensagens)"
}

// =============================================================================
// View 2 — Cards (Pinterest-style)
// =============================================================================

func (v threadsView) renderCards(width int) string {
	if len(v.threads) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render(
			"(nenhuma thread)")
	}

	// Cards 1 por linha pra widths < 80, 2 por linha pra wide.
	cols := 1
	cardW := width - 2
	if width >= 80 {
		cols = 2
		cardW = (width - 4) / 2
	}

	muted := lipgloss.NewStyle().Foreground(colorMuted)
	border := lipgloss.NewStyle().Foreground(colorBorder)
	focused := lipgloss.NewStyle().Foreground(colorAccent)

	var b strings.Builder
	var rowCards []string
	for ti, t := range v.threads {
		rowCards = append(rowCards, v.renderSingleCard(t, cardW, ti))
		if len(rowCards) == cols {
			b.WriteString(joinCards(rowCards) + "\n")
			rowCards = rowCards[:0]
		}
	}
	if len(rowCards) > 0 {
		b.WriteString(joinCards(rowCards) + "\n")
	}
	_ = muted
	_ = border
	_ = focused
	return b.String()
}

// renderSingleCard renderiza UMA thread como card multi-line.
func (v threadsView) renderSingleCard(t *stats.Thread, w, threadIdx int) string {
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	subdued := lipgloss.NewStyle().Foreground(colorMuted)
	bord := colorBorder
	bordStyle := lipgloss.NewStyle().Foreground(bord)

	// Detect cursor — se algum row do flat dessa thread está selecionado
	isCursor := false
	for i, row := range v.flat {
		if row.threadIdx == threadIdx && i == v.cursor {
			isCursor = true
			break
		}
	}
	if isCursor {
		bord = colorAccent
		bordStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	}

	var lines []string

	// Top border
	lines = append(lines, bordStyle.Render("╭"+strings.Repeat("─", w-2)+"╮"))

	// Project line
	short := shortPath(t.ProjectDir, mustHomeTUI())
	if len(short) > w-6 {
		short = "…" + short[len(short)-(w-7):]
	}
	projLine := bordStyle.Render("│ ") +
		muted.Render("📁 "+short) +
		strings.Repeat(" ", maxInt(0, w-2-3-len(short)-1)) +
		bordStyle.Render("│")
	lines = append(lines, projLine)

	// Branch pill line
	branch := t.Branch
	if branch == "" {
		branch = "(no branch)"
	}
	pill := branchPill(branch)
	pillW := lipgloss.Width(pill)
	branchLine := bordStyle.Render("│ ") +
		pill +
		strings.Repeat(" ", maxInt(0, w-2-pillW-1)) +
		bordStyle.Render("│")
	lines = append(lines, branchLine)

	// Stats line: ●●●○○ 5 sessions · $5.20 · 2h15m
	dotsRendered := renderSessionDots(t.Sessions)
	statsTxt := fmt.Sprintf(" %d sess · %s · %s",
		len(t.Sessions),
		fmtDuration(t.TotalDur),
		fmt.Sprintf("$%.2f", t.TotalCost))
	statsLine := bordStyle.Render("│ ") +
		dotsRendered +
		muted.Render(statsTxt) +
		strings.Repeat(" ", maxInt(0, w-2-len(t.Sessions)-len(statsTxt)-1)) +
		bordStyle.Render("│")
	lines = append(lines, statsLine)

	// Sparkline
	spark := stats.SparklineFromThread(t)
	sparkLine := bordStyle.Render("│ ") +
		lipgloss.NewStyle().Foreground(colorAccent).Render(spark) +
		strings.Repeat(" ", maxInt(0, w-2-len(spark)-1)) +
		bordStyle.Render("│")
	lines = append(lines, sparkLine)

	// Latest session
	if len(t.Sessions) > 0 {
		latest := t.Sessions[len(t.Sessions)-1]
		title := v.titleFor(latest.Session)
		titleMax := w - 8
		if titleMax > 0 && len(title) > titleMax {
			title = title[:titleMax-1] + "…"
		}
		latestLine := bordStyle.Render("│ ") +
			subdued.Render("↳ ") +
			lipgloss.NewStyle().Foreground(colorFg).Render(title) +
			strings.Repeat(" ", maxInt(0, w-2-2-len(title)-1)) +
			bordStyle.Render("│")
		lines = append(lines, latestLine)
	}

	// Bottom border
	lines = append(lines, bordStyle.Render("╰"+strings.Repeat("─", w-2)+"╯"))

	return strings.Join(lines, "\n")
}

// renderSessionDots desenha ● ● ◉ ● ● — colored por kind.
func renderSessionDots(sessions []*stats.ThreadSession) string {
	var b strings.Builder
	for _, s := range sessions {
		b.WriteString(threadDot(s.Kind, "") + " ")
	}
	return b.String()
}

// joinCards cola N cards lado-a-lado linha-a-linha.
func joinCards(cards []string) string {
	if len(cards) == 1 {
		return cards[0]
	}
	rows := make([][]string, len(cards))
	maxLines := 0
	for i, c := range cards {
		rows[i] = strings.Split(c, "\n")
		if len(rows[i]) > maxLines {
			maxLines = len(rows[i])
		}
	}
	var out []string
	for i := 0; i < maxLines; i++ {
		var line strings.Builder
		for ci, r := range rows {
			if i < len(r) {
				line.WriteString(r[i])
			} else {
				// pad com espaços do tamanho do primeiro card
				if len(r) > 0 {
					line.WriteString(strings.Repeat(" ", lipgloss.Width(stripAnsi(r[0]))))
				}
			}
			if ci < len(rows)-1 {
				line.WriteString("  ")
			}
		}
		out = append(out, line.String())
	}
	return strings.Join(out, "\n")
}

// humanDurShort: 30s, 5m, 2h, 1d.
func humanDurShort(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func mustHomeTUI() string {
	h, _ := os.UserHomeDir()
	return h
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// =============================================================================
// View 4 — Miller columns (3-pane Finder-style)
// Cursor navega no pane das sessions (rightmost). Alt+h/l mudaria o foco
// pra outras panes mas pra v1 simplifico — projects/branches escolhidos
// automaticamente baseado no que tem cursor selecionado.
// =============================================================================

func (v threadsView) renderMiller(width int) string {
	if len(v.threads) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render("(nenhuma thread)")
	}
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	bord := lipgloss.NewStyle().Foreground(colorBorder)
	header := lipgloss.NewStyle().Foreground(colorMuted).Bold(true).Padding(0, 1)
	sel := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	// 3 cols: 22, 26, rest
	colW := []int{22, 26, width - 22 - 26 - 4}
	if colW[2] < 30 {
		colW[2] = 30
	}

	// Determine selection from current cursor row
	selProject, selBranch := "", ""
	var selSession *model.Session
	if v.cursor < len(v.flat) {
		row := v.flat[v.cursor]
		selProject = row.thread.ProjectDir
		selBranch = row.thread.Branch
		if row.session != nil {
			selSession = row.session.Session
		}
	}

	// Pane 1: projects
	grouped := stats.GroupByProject(v.threads)
	dirs := stats.SortedProjectDirs(grouped)
	var p1 strings.Builder
	p1.WriteString(header.Render("PROJECTS") + "\n")
	for _, d := range dirs {
		short := shortPath(d, mustHomeTUI())
		short = truncRight(short, colW[0]-6)
		count := 0
		for _, t := range grouped[d] {
			count += len(t.Sessions)
		}
		line := fmt.Sprintf(" %-*s %3d", colW[0]-6, short, count)
		if d == selProject {
			line = sel.Render("▶" + line)
		} else {
			line = " " + line
		}
		p1.WriteString(line + "\n")
	}

	// Pane 2: branches do projeto selecionado
	var p2 strings.Builder
	p2.WriteString(header.Render("BRANCHES") + "\n")
	if branches, ok := grouped[selProject]; ok {
		for _, t := range branches {
			b := t.Branch
			if b == "" {
				b = "(no branch)"
			}
			b = truncRight(b, colW[1]-6)
			line := fmt.Sprintf(" %-*s %3d", colW[1]-6, b, len(t.Sessions))
			if t.Branch == selBranch {
				line = sel.Render("▶" + line)
			} else {
				line = " " + line
			}
			p2.WriteString(line + "\n")
		}
	}

	// Pane 3: sessions da branch selecionada
	var p3 strings.Builder
	p3.WriteString(header.Render("SESSIONS") + "\n")
	for _, t := range v.threads {
		if t.ProjectDir != selProject || t.Branch != selBranch {
			continue
		}
		for _, s := range t.Sessions {
			when := s.StartTime.Local().Format("Mon 15:04")
			dur := fmtDuration(s.Duration())
			cost := "?"
			if v.pricing != nil {
				if c, ok := v.pricing.Cost(s.Session); ok {
					cost = fmt.Sprintf("$%.2f", c.USD)
				}
			}
			marker := " "
			line := fmt.Sprintf("%s %s · %s · %s · %s",
				marker, when, dur, cost, threadDot(s.Kind, ""))
			if selSession != nil && s.SessionID == selSession.SessionID {
				line = sel.Render("▶ " + when + " · " + dur + " · " + cost + " ●")
			}
			p3.WriteString(line + "\n")
		}
	}

	// Join 3 panes side-by-side
	lines1 := strings.Split(p1.String(), "\n")
	lines2 := strings.Split(p2.String(), "\n")
	lines3 := strings.Split(p3.String(), "\n")
	maxLines := len(lines1)
	if len(lines2) > maxLines {
		maxLines = len(lines2)
	}
	if len(lines3) > maxLines {
		maxLines = len(lines3)
	}

	var out strings.Builder
	for i := 0; i < maxLines; i++ {
		l1, l2, l3 := "", "", ""
		if i < len(lines1) {
			l1 = lines1[i]
		}
		if i < len(lines2) {
			l2 = lines2[i]
		}
		if i < len(lines3) {
			l3 = lines3[i]
		}
		// Pad to colW
		l1 = padRight(l1, colW[0])
		l2 = padRight(l2, colW[1])
		out.WriteString(l1 + bord.Render("│") + l2 + bord.Render("│") + l3 + "\n")
	}

	// Preview pane embaixo
	out.WriteString(bord.Render(strings.Repeat("─", width)) + "\n")
	if selSession != nil {
		summary := v.titleFor(selSession)
		summary = truncRight(summary, width-4)
		out.WriteString(" " + lipgloss.NewStyle().Foreground(colorFg).Render(summary) + "\n")
		// breadcrumb
		crumb := breadcrumb(
			shortPath(selProject, mustHomeTUI()),
			selBranch,
			selSession.SessionID[:8],
		)
		out.WriteString(" " + crumb + "\n")
	} else {
		out.WriteString(" " + muted.Render("(nenhuma session selecionada)") + "\n")
	}

	return out.String()
}

func padRight(s string, n int) string {
	w := lipgloss.Width(stripAnsi(s))
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// =============================================================================
// View 3 — Graph DAG (lazygit-style lanes paralelas)
// =============================================================================

func (v threadsView) renderGraph(width int) string {
	if len(v.threads) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render("(nenhuma thread)")
	}
	// Cada thread vira uma "lane" com cor própria. Sessions de threads diferentes
	// aparecem em ordem cronológica global, com símbolos `●` na lane correspondente
	// e `│` nas outras lanes pra mostrar continuidade.

	// Cores rotativas pra lanes
	laneColors := []lipgloss.Color{
		lipgloss.Color("#7dd3fc"), // cyan
		lipgloss.Color("#f472b6"), // magenta
		lipgloss.Color("#fbbf24"), // yellow
		lipgloss.Color("#34d399"), // green
		lipgloss.Color("#60a5fa"), // blue
		lipgloss.Color("#f87171"), // red
	}

	// Assign lane index to each thread
	threadLane := map[*stats.Thread]int{}
	for i, t := range v.threads {
		threadLane[t] = i % len(laneColors)
	}

	// Flat list of all sessions globally sorted by start time
	type globalEntry struct {
		t *stats.Thread
		s *stats.ThreadSession
	}
	var all []globalEntry
	for _, t := range v.threads {
		for _, s := range t.Sessions {
			all = append(all, globalEntry{t, s})
		}
	}
	sortByTime := func(a, b globalEntry) bool {
		return a.s.StartTime.Before(b.s.StartTime)
	}
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && sortByTime(all[j], all[j-1]); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}

	// Track which threads are "alive" at each row — first session = appears,
	// last session = disappears, intermediate = `│`. Determine first/last:
	firstIdx := map[*stats.Thread]int{}
	lastIdx := map[*stats.Thread]int{}
	for i, e := range all {
		if _, ok := firstIdx[e.t]; !ok {
			firstIdx[e.t] = i
		}
		lastIdx[e.t] = i
	}

	muted := lipgloss.NewStyle().Foreground(colorMuted)
	var b strings.Builder

	for i, e := range all {
		// Build the lane prefix
		var lanePrefix strings.Builder
		for j, t := range v.threads {
			lane := threadLane[t]
			col := laneColors[lane]
			active := i >= firstIdx[t] && i <= lastIdx[t]
			isThis := t == e.t
			ch := " "
			if isThis {
				if e.s.Kind == "compact" {
					ch = lipgloss.NewStyle().Foreground(colorWarn).Render("◉")
				} else {
					ch = lipgloss.NewStyle().Foreground(col).Render("●")
				}
			} else if active {
				ch = lipgloss.NewStyle().Foreground(col).Render("│")
			}
			lanePrefix.WriteString(ch + "  ")
			_ = j
		}

		// Right-side: branch pill + session info
		isCursor := false
		idx := v.flatRowIndex(e.t, e.s)
		if idx == v.cursor {
			isCursor = true
		}
		when := e.s.StartTime.Local().Format("Mon 15:04")
		title := v.titleFor(e.s.Session)
		title = truncRight(title, 50)
		gapStr := ""
		if e.s.Kind == "compact" {
			gapStr = lipgloss.NewStyle().Foreground(colorWarn).Render(fmt.Sprintf(" ↻ %s", humanDurShort(e.s.GapFromPrev)))
		}

		row := lanePrefix.String() +
			"  " + branchPill(e.t.Branch) +
			"  " + muted.Render(when) +
			"  " + lipgloss.NewStyle().Foreground(colorFg).Render(title) +
			gapStr +
			"  " + muted.Render("["+e.s.SessionID[:8]+"]")
		if isCursor {
			row = lipgloss.NewStyle().Background(lipgloss.Color("237")).Render(row)
		}
		b.WriteString(row + "\n")
	}
	return b.String()
}

// =============================================================================
// View 5 — Timeline horizontal (eixo tempo)
// Cada thread = linha horizontal, sessions = ● ao longo do eixo X (tempo).
// =============================================================================

func (v threadsView) renderTimeline(width int) string {
	if len(v.threads) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render("(nenhuma thread)")
	}
	muted := lipgloss.NewStyle().Foreground(colorMuted)

	// Find global time range
	var minT, maxT time.Time
	for _, t := range v.threads {
		for _, s := range t.Sessions {
			if minT.IsZero() || s.StartTime.Before(minT) {
				minT = s.StartTime
			}
			if s.EndTime.After(maxT) {
				maxT = s.EndTime
			}
		}
	}
	if minT.IsZero() || maxT.IsZero() {
		return ""
	}

	// Label column: 22 chars
	labelW := 22
	timelineW := width - labelW - 2
	if timelineW < 30 {
		timelineW = 30
	}
	totalSpan := maxT.Sub(minT)
	if totalSpan == 0 {
		totalSpan = time.Hour
	}

	// Helper: time → column position
	timeToX := func(t time.Time) int {
		ratio := float64(t.Sub(minT)) / float64(totalSpan)
		x := int(ratio * float64(timelineW-1))
		if x < 0 {
			x = 0
		}
		if x >= timelineW {
			x = timelineW - 1
		}
		return x
	}

	var b strings.Builder

	// Header com day labels
	header := strings.Repeat(" ", labelW+2)
	dayMarkers := []time.Time{}
	day := time.Date(minT.Year(), minT.Month(), minT.Day(), 0, 0, 0, 0, minT.Location())
	for ; !day.After(maxT); day = day.AddDate(0, 0, 1) {
		dayMarkers = append(dayMarkers, day)
	}
	headerLine := []rune(strings.Repeat(" ", timelineW))
	for _, d := range dayMarkers {
		x := timeToX(d)
		label := d.Format("Mon 02")
		for i, r := range []rune(label) {
			if x+i < len(headerLine) {
				headerLine[x+i] = r
			}
		}
	}
	b.WriteString(header + muted.Render(string(headerLine)) + "\n")

	// Separator
	b.WriteString(strings.Repeat(" ", labelW+2) + muted.Render(strings.Repeat("─", timelineW)) + "\n")

	// Thread rows
	for _, t := range v.threads {
		branch := t.Branch
		if branch == "" {
			branch = "(no branch)"
		}
		// Label
		label := truncRight(branch, labelW-1)
		b.WriteString(branchPill(label) + strings.Repeat(" ", maxInt(0, labelW-lipgloss.Width(branchPill(label)))) + "  ")

		// Build line of sessions
		line := []rune(strings.Repeat(" ", timelineW))
		col := branchColor(t.Branch)
		_ = col
		for i, s := range t.Sessions {
			x := timeToX(s.StartTime)
			ch := '●'
			if s.Kind == "compact" {
				ch = '◉'
			}
			if x < len(line) {
				line[x] = ch
			}
			// Connect to previous session in same thread with ─
			if i > 0 {
				prev := t.Sessions[i-1]
				px := timeToX(prev.EndTime)
				for j := px + 1; j < x; j++ {
					if j < len(line) && line[j] == ' ' {
						line[j] = '─'
					}
				}
			}
		}
		// Color the line
		colored := lipgloss.NewStyle().Foreground(branchColor(t.Branch)).Render(string(line))
		b.WriteString(colored)

		// Right-side: session count + cost
		b.WriteString("  " + muted.Render(fmt.Sprintf("%d · $%.2f", len(t.Sessions), t.TotalCost)) + "\n")
	}

	// Footer separator
	b.WriteString(strings.Repeat(" ", labelW+2) + muted.Render(strings.Repeat("─", timelineW)) + "\n")

	// Hour markers (only if span < 7d)
	if totalSpan < 7*24*time.Hour {
		hourLine := []rune(strings.Repeat(" ", timelineW))
		hr := time.Date(minT.Year(), minT.Month(), minT.Day(), minT.Hour(), 0, 0, 0, minT.Location())
		for ; !hr.After(maxT); hr = hr.Add(6 * time.Hour) {
			x := timeToX(hr)
			label := hr.Format("15:04")
			for i, r := range []rune(label) {
				if x+i < len(hourLine) {
					hourLine[x+i] = r
				}
			}
		}
		b.WriteString(strings.Repeat(" ", labelW+2) + muted.Render(string(hourLine)) + "\n")
	}

	return b.String()
}

// =============================================================================
// View 6 — Galaxy (Braille canvas, drawille-inspired)
// Force-directed layout simples + render em chars Braille (2x4 pixels/char).
// =============================================================================

// brailleCanvas inspirado em asciimoo/drawille.
// Cada char Braille (U+2800..U+28FF) representa 2×4 pixels.
// Bit positions: col 0 → bits 0,1,2,6 (rows 0,1,2,3) · col 1 → bits 3,4,5,7
type brailleCanvas struct {
	pixels [][]uint8 // pixel grid (h*4 rows × w*2 cols)
	colors [][]lipgloss.Color
	w, h   int // chars
}

func newBraille(w, h int) *brailleCanvas {
	pixels := make([][]uint8, h*4)
	for i := range pixels {
		pixels[i] = make([]uint8, w*2)
	}
	colors := make([][]lipgloss.Color, h)
	for i := range colors {
		colors[i] = make([]lipgloss.Color, w)
	}
	return &brailleCanvas{pixels: pixels, colors: colors, w: w, h: h}
}

func (c *brailleCanvas) set(x, y int, color lipgloss.Color) {
	if x < 0 || y < 0 || y >= c.h*4 || x >= c.w*2 {
		return
	}
	c.pixels[y][x] = 1
	cx, cy := x/2, y/4
	c.colors[cy][cx] = color
}

func (c *brailleCanvas) circle(cx, cy, r int, color lipgloss.Color) {
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy <= r*r {
				c.set(cx+dx, cy+dy, color)
			}
		}
	}
}

func (c *brailleCanvas) line(x0, y0, x1, y1 int, color lipgloss.Color, dashed bool) {
	dx, dy := abs(x1-x0), abs(y1-y0)
	sx, sy := 1, 1
	if x0 >= x1 {
		sx = -1
	}
	if y0 >= y1 {
		sy = -1
	}
	err := dx - dy
	count := 0
	for {
		if !dashed || (count%4 < 2) {
			c.set(x0, y0, color)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
		count++
	}
}

func (c *brailleCanvas) render() string {
	// Bit positions matching Unicode Braille:
	// dots: 1=0x01 2=0x02 3=0x04 4=0x08 5=0x10 6=0x20 7=0x40 8=0x80
	// layout in 2x4:
	//   1 4
	//   2 5
	//   3 6
	//   7 8
	bits := [4][2]uint8{
		{0x01, 0x08},
		{0x02, 0x10},
		{0x04, 0x20},
		{0x40, 0x80},
	}
	var out strings.Builder
	for cy := 0; cy < c.h; cy++ {
		var line strings.Builder
		var curColor lipgloss.Color = ""
		var buf strings.Builder
		flush := func() {
			if buf.Len() == 0 {
				return
			}
			if curColor != "" {
				line.WriteString(lipgloss.NewStyle().Foreground(curColor).Render(buf.String()))
			} else {
				line.WriteString(buf.String())
			}
			buf.Reset()
		}
		for cx := 0; cx < c.w; cx++ {
			var b uint8
			for row := 0; row < 4; row++ {
				for col := 0; col < 2; col++ {
					px, py := cx*2+col, cy*4+row
					if c.pixels[py][px] != 0 {
						b |= bits[row][col]
					}
				}
			}
			ch := ' '
			if b != 0 {
				ch = rune(0x2800 + int(b))
			}
			col := c.colors[cy][cx]
			if col != curColor {
				flush()
				curColor = col
			}
			buf.WriteRune(ch)
		}
		flush()
		out.WriteString(line.String() + "\n")
	}
	return out.String()
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// renderGalaxy faz force-directed layout simples + braille canvas.
func (v threadsView) renderGalaxy(width, height int) string {
	if len(v.threads) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render("(nenhuma thread)")
	}
	if width < 60 || height < 12 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render(
			"galaxy view requer terminal ≥ 60×12 — use outra view")
	}
	canvas := newBraille(width, height-2) // leave room for legend
	pixW, pixH := width*2, (height-2)*4

	// Assign cores por projeto
	projectColors := map[string]lipgloss.Color{}
	colors := []lipgloss.Color{
		"#7dd3fc", "#fbbf24", "#f472b6", "#34d399", "#60a5fa", "#a78bfa",
	}
	for i, t := range v.threads {
		if _, ok := projectColors[t.ProjectDir]; !ok {
			projectColors[t.ProjectDir] = colors[len(projectColors)%len(colors)]
		}
		_ = i
	}

	// Build flat node list (1 per session)
	type node struct {
		s     *stats.ThreadSession
		t     *stats.Thread
		x, y  float64
		vx,vy float64
		r     int
	}
	var nodes []*node
	for _, t := range v.threads {
		for _, s := range t.Sessions {
			r := 2
			cost := 0.0
			if v.pricing != nil {
				if c, ok := v.pricing.Cost(s.Session); ok {
					cost = c.USD
				}
			}
			if cost > 1 {
				r = 3
			}
			if cost > 3 {
				r = 4
			}
			if cost > 10 {
				r = 5
			}
			nodes = append(nodes, &node{
				s: s, t: t,
				x: float64(pixW)/2 + float64(len(nodes)%5)*8 - 16,
				y: float64(pixH)/2 + float64(len(nodes)/5)*8 - 16,
				r: r,
			})
		}
	}

	// Edges: continuação dentro da mesma thread
	type edge struct{ a, b *node; dashed bool }
	var edges []edge
	for _, t := range v.threads {
		for i := 1; i < len(t.Sessions); i++ {
			var pn, cn *node
			for _, n := range nodes {
				if n.s == t.Sessions[i-1] {
					pn = n
				}
				if n.s == t.Sessions[i] {
					cn = n
				}
			}
			if pn != nil && cn != nil {
				edges = append(edges, edge{pn, cn, t.Sessions[i].Kind == "compact"})
			}
		}
	}

	// Force-directed: 80 iterations
	cx, cy := float64(pixW)/2, float64(pixH)/2
	k := 12.0
	for iter := 0; iter < 80; iter++ {
		for _, a := range nodes {
			a.vx, a.vy = 0, 0
			// Repulsion between all
			for _, b := range nodes {
				if a == b {
					continue
				}
				dx := a.x - b.x
				dy := a.y - b.y
				d := dx*dx + dy*dy
				if d < 0.01 {
					d = 0.01
				}
				dn := sqrt(d)
				f := (k * k) / dn
				a.vx += (dx / dn) * f
				a.vy += (dy / dn) * f
			}
			// Cluster attraction within same project
			for _, b := range nodes {
				if a == b || a.t.ProjectDir != b.t.ProjectDir {
					continue
				}
				dx := a.x - b.x
				dy := a.y - b.y
				d := sqrt(dx*dx + dy*dy)
				if d < 0.01 {
					continue
				}
				f := (d * d) / k * 0.5
				a.vx -= (dx / d) * f
				a.vy -= (dy / d) * f
			}
			// Pull to center
			a.vx += (cx - a.x) * 0.005
			a.vy += (cy - a.y) * 0.005
		}
		// Edge attraction
		for _, e := range edges {
			dx := e.a.x - e.b.x
			dy := e.a.y - e.b.y
			d := sqrt(dx*dx + dy*dy)
			if d < 0.01 {
				continue
			}
			f := (d * d) / k * 0.8
			e.a.vx -= (dx / d) * f
			e.a.vy -= (dy / d) * f
			e.b.vx += (dx / d) * f
			e.b.vy += (dy / d) * f
		}
		// Apply with damping + clamp
		for _, n := range nodes {
			speed := sqrt(n.vx*n.vx + n.vy*n.vy)
			if speed < 0.01 {
				speed = 0.01
			}
			cap := speed
			if cap > 5 {
				cap = 5
			}
			n.x += (n.vx / speed) * cap * 0.3
			n.y += (n.vy / speed) * cap * 0.3
			if n.x < 8 {
				n.x = 8
			}
			if n.x > float64(pixW)-8 {
				n.x = float64(pixW) - 8
			}
			if n.y < 4 {
				n.y = 4
			}
			if n.y > float64(pixH)-4 {
				n.y = float64(pixH) - 4
			}
		}
	}

	// Draw edges first
	for _, e := range edges {
		col := projectColors[e.a.t.ProjectDir]
		canvas.line(int(e.a.x), int(e.a.y), int(e.b.x), int(e.b.y), col, e.dashed)
	}
	// Draw nodes
	for _, n := range nodes {
		col := projectColors[n.t.ProjectDir]
		canvas.circle(int(n.x), int(n.y), n.r, col)
	}

	// Render + legend
	out := canvas.render()

	muted := lipgloss.NewStyle().Foreground(colorMuted)
	var legend strings.Builder
	legend.WriteString(muted.Render("─── projetos ───") + "\n")
	for proj, col := range projectColors {
		short := shortPath(proj, mustHomeTUI())
		short = truncRight(short, 40)
		legend.WriteString("  " +
			lipgloss.NewStyle().Foreground(col).Render("●") +
			" " + lipgloss.NewStyle().Foreground(col).Render(short) + "\n")
	}
	return out + legend.String()
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x / 2
	for i := 0; i < 8; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

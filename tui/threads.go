package tui

import (
	"fmt"
	"os"
	"sort"
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
	// millerPane: 0=projects, 1=branches, 2=sessions. Setas ←→ alternam.
	millerPane int
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

// rebuildFlat reconstrói a flat list baseada na ORDEM VISUAL da view atual.
// Cada view tem sua própria ordem natural — pra cursor bater com o que aparece
// na tela:
//   tree/cards/miller: hierárquico (project > thread > sessions)
//   graph/timeline/galaxy: cronológico global (start_time asc)
func (v *threadsView) rebuildFlat() {
	prevSessionID := ""
	if v.cursor < len(v.flat) && v.flat[v.cursor].session != nil {
		prevSessionID = v.flat[v.cursor].session.SessionID
	}
	v.flat = nil

	switch v.view {
	case threadViewGraph, threadViewTimeline, threadViewGalaxy:
		// Cronológico global — mesma ordem que essas views renderizam
		type entry struct {
			t *stats.Thread
			s *stats.ThreadSession
		}
		var all []entry
		for _, t := range v.threads {
			for _, s := range t.Sessions {
				all = append(all, entry{t, s})
			}
		}
		sort.Slice(all, func(i, j int) bool {
			return all[i].s.StartTime.Before(all[j].s.StartTime)
		})
		for _, e := range all {
			ti := indexOfThread(v.threads, e.t)
			si := indexOfSession(e.t, e.s)
			v.flat = append(v.flat, &flatRow{
				threadIdx:  ti,
				sessionIdx: si,
				thread:     e.t,
				session:    e.s,
			})
		}
	default:
		// Hierárquico — tree, cards, miller. Header (sessionIdx=-1) skipável.
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

	// Reposiciona cursor na mesma session que estava antes do rebuild
	if prevSessionID != "" {
		for i, row := range v.flat {
			if row.session != nil && row.session.SessionID == prevSessionID {
				v.cursor = i
				return
			}
		}
	}
	// fallback: primeiro row com session
	for i, row := range v.flat {
		if row.session != nil {
			v.cursor = i
			return
		}
	}
}

func indexOfThread(threads []*stats.Thread, t *stats.Thread) int {
	for i, x := range threads {
		if x == t {
			return i
		}
	}
	return -1
}

func indexOfSession(t *stats.Thread, s *stats.ThreadSession) int {
	for i, x := range t.Sessions {
		if x == s {
			return i
		}
	}
	return -1
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
	if v.view == threadViewGalaxy {
		v.moveCursorGalaxy(0, delta) // delta > 0 = baixo, < 0 = cima
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
		if v.flat[v.cursor].session != nil {
			delta--
		}
	}
}

// MoveCursorH é nav horizontal — setas esquerda/direita. Comportamento por view:
//   miller: alterna pane (projects ↔ branches ↔ sessions)
//   galaxy: navega pro node mais próximo na direção
//   outras views: fallback pra MoveCursor (delta menor — pula 1 dentro do mesmo group)
func (v *threadsView) MoveCursorH(delta int) {
	switch v.view {
	case threadViewMiller:
		v.millerPane += delta
		if v.millerPane < 0 {
			v.millerPane = 0
		}
		if v.millerPane > 2 {
			v.millerPane = 2
		}
	case threadViewGalaxy:
		v.moveCursorGalaxy(delta, 0) // delta > 0 = direita, < 0 = esquerda
	default:
		// Em outras views, faz nothing — esquerda/direita não tem sentido
	}
}

// moveCursorGalaxy escolhe o próximo node mais próximo na direção dada.
// dx > 0 = direita, dx < 0 = esquerda; dy > 0 = baixo, dy < 0 = cima.
func (v *threadsView) moveCursorGalaxy(dx, dy int) {
	// Galaxy precisa ter coords salvas — usa positions do último render.
	// Como o force-directed roda a cada render, snapshotamos posições aqui
	// rodando um layout determinístico rápido baseado em índice.
	if v.cursor >= len(v.flat) {
		return
	}
	curRow := v.flat[v.cursor]
	if curRow.session == nil {
		return
	}
	// Coordenadas heurísticas: usa start_time como x, hash do project como y.
	// Ordem cronológica natural já tá na flat list.
	// Pra simplificar: ←→ = sessão anterior/próxima cronologicamente,
	// ↑↓ = sessão da thread anterior/próxima na lista.
	if dx != 0 {
		// flat já é cronológico em galaxy view
		next := v.cursor + sign(dx)
		for next >= 0 && next < len(v.flat) && v.flat[next].session == nil {
			next += sign(dx)
		}
		if next >= 0 && next < len(v.flat) {
			v.cursor = next
		}
	}
	if dy != 0 {
		// Acha próxima session de OUTRA thread na direção
		curThread := curRow.thread
		next := v.cursor + sign(dy)
		for next >= 0 && next < len(v.flat) {
			if v.flat[next].session != nil && v.flat[next].thread != curThread {
				v.cursor = next
				return
			}
			next += sign(dy)
		}
		// Fallback: primeiro/último válido
		if dy > 0 {
			for i := len(v.flat) - 1; i >= 0; i-- {
				if v.flat[i].session != nil && v.flat[i].thread != curThread {
					v.cursor = i
					return
				}
			}
		} else {
			for i := 0; i < len(v.flat); i++ {
				if v.flat[i].session != nil && v.flat[i].thread != curThread {
					v.cursor = i
					return
				}
			}
		}
	}
}

func sign(x int) int {
	if x > 0 {
		return 1
	}
	if x < 0 {
		return -1
	}
	return 0
}

func (v threadsView) View(width, height int) string {
	header := v.renderStatusHeader(width)
	var body string
	switch v.view {
	case threadViewTree:
		body = v.renderTree(width)
	case threadViewCards:
		body = v.renderCards(width)
	case threadViewMiller:
		body = v.renderMiller(width)
	case threadViewGraph:
		body = v.renderGraph(width)
	case threadViewTimeline:
		body = v.renderTimeline(width)
	case threadViewGalaxy:
		body = v.renderGalaxy(width, height-3)
	}
	return header + "\n" + body
}

// renderStatusHeader é uma barra que aparece em TODAS as views mostrando:
//   view name · session N/M · branch · timestamp · cost
// Garante que o user sempre saiba ONDE está, independente da view.
func (v threadsView) renderStatusHeader(width int) string {
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	accent := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	// View badges — todas, com a atual destacada
	views := []string{"tree", "cards", "miller", "graph", "timeline", "galaxy"}
	var badges []string
	for i, name := range views {
		if i == int(v.view) {
			badges = append(badges, accent.Render("◉ "+name))
		} else {
			badges = append(badges, muted.Render("  "+name))
		}
	}
	viewStrip := strings.Join(badges, "  ")

	// Cursor position info
	totalSessions := 0
	currentIdx := 0
	for _, row := range v.flat {
		if row.session != nil {
			totalSessions++
		}
	}
	for i := 0; i <= v.cursor && i < len(v.flat); i++ {
		if v.flat[i].session != nil {
			currentIdx++
		}
	}

	posInfo := ""
	if v.cursor < len(v.flat) && v.flat[v.cursor].session != nil {
		row := v.flat[v.cursor]
		s := row.session
		when := s.StartTime.Local().Format("Mon 02 · 15:04")
		cost := ""
		if v.pricing != nil {
			if c, ok := v.pricing.Cost(s.Session); ok {
				cost = fmt.Sprintf("$%.2f", c.USD)
			}
		}
		posInfo = fmt.Sprintf("  %s session %d/%d  %s  %s  %s  %s",
			muted.Render("·"),
			currentIdx, totalSessions,
			branchPill(row.thread.Branch),
			muted.Render(when),
			lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(cost),
			muted.Render("["+s.SessionID[:8]+"]"))
	} else {
		posInfo = "  " + muted.Render(fmt.Sprintf("· %d threads · %d sessions total",
			len(v.threads), totalSessions))
	}

	// Combina em 2 linhas: views badges + position
	line1 := viewStrip
	line2 := muted.Render("[v] alterna view  [↑↓ j/k] navega  [enter] retoma") + posInfo

	// Trunca pra width
	if lipgloss.Width(stripAnsi(line1)) > width {
		line1 = line1[:maxInt(0, width)]
	}

	border := lipgloss.NewStyle().Foreground(colorBorder).
		Render(strings.Repeat("─", maxInt(0, width)))

	return line1 + "\n" + line2 + "\n" + border
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
			muted.Render(strings.Repeat("─", maxInt(0, width-lipgloss.Width(short)-8))))

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
	dashes := maxInt(0, width-headerLeftLen-rightLen-6)
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
			pad := maxInt(0, width-lipgloss.Width(plain))
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
	dashCount := maxInt(0, width-1)
	b.WriteString(borderStyle.Render("╰"+strings.Repeat("─", dashCount)) + "\n")
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
	bord := colorBorder
	bordStyle := lipgloss.NewStyle().Foreground(bord)

	// Detecta se cursor tá em alguma session dessa thread
	cursorInThread := false
	cursorSession := -1
	for i, row := range v.flat {
		if row.threadIdx == threadIdx && i == v.cursor && row.session != nil {
			cursorInThread = true
			cursorSession = row.sessionIdx
			break
		}
	}
	if cursorInThread {
		bord = colorAccent
		bordStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	}

	var lines []string
	innerW := maxInt(0, w-2)

	// Top border com branch pill embutida no título
	branch := t.Branch
	if branch == "" {
		branch = "(no branch)"
	}
	pill := branchPill(branch)
	pillW := lipgloss.Width(pill)
	titleSpace := maxInt(0, innerW-pillW-3)
	lines = append(lines, bordStyle.Render("╭─ ")+pill+
		bordStyle.Render(" "+strings.Repeat("─", titleSpace)+"╮"))

	// Project line
	short := shortPath(t.ProjectDir, mustHomeTUI())
	if lipgloss.Width(short) > w-6 {
		short = "…" + truncRight(short, w-7)
	}
	lines = append(lines, padCardLine("│ "+muted.Render("📁 "+short), w, bordStyle))

	// Stats: ●●●●● N sessions · dur · cost
	dotsRendered := renderSessionDots(t.Sessions)
	statsTxt := fmt.Sprintf(" %d sess · %s · $%.2f",
		len(t.Sessions), fmtDuration(t.TotalDur), t.TotalCost)
	lines = append(lines, padCardLine("│ "+dotsRendered+muted.Render(statsTxt), w, bordStyle))

	// Sparkline
	spark := stats.SparklineFromThread(t)
	if spark != "" {
		sparkColored := lipgloss.NewStyle().Foreground(colorAccent).Render(spark)
		lines = append(lines, padCardLine("│ "+sparkColored, w, bordStyle))
	}

	// Separator
	sepInner := maxInt(0, w-4)
	lines = append(lines, padCardLine("│ "+muted.Render(strings.Repeat("─", sepInner)), w, bordStyle))

	// Lista as últimas N sessions com cursor highlight
	maxSess := 5
	startIdx := 0
	if len(t.Sessions) > maxSess {
		startIdx = len(t.Sessions) - maxSess
	}
	if startIdx > 0 {
		lines = append(lines, padCardLine("│ "+muted.Render(fmt.Sprintf("  +%d anteriores ⌃", startIdx)), w, bordStyle))
	}
	for si := startIdx; si < len(t.Sessions); si++ {
		s := t.Sessions[si]
		dot := threadDot(s.Kind, "")
		when := s.StartTime.Local().Format("Mon 15:04")
		title := v.titleFor(s.Session)
		titleMax := maxInt(8, w-22)
		if lipgloss.Width(title) > titleMax {
			title = truncRight(title, titleMax)
		}
		isCursorRow := cursorInThread && si == cursorSession

		var rowContent string
		if isCursorRow {
			rowContent = lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(colorAccent).Bold(true).
				Render(fmt.Sprintf("▶ %s %s  %s", "●", when, title))
		} else {
			rowContent = "  " + dot + " " + muted.Render(when) + "  " +
				lipgloss.NewStyle().Foreground(colorFg).Render(title)
		}
		lines = append(lines, padCardLine("│ "+rowContent, w, bordStyle))
	}

	// Bottom border
	lines = append(lines, bordStyle.Render("╰"+strings.Repeat("─", innerW)+"╯"))

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

// padCardLine recebe linha "│ <content>" e adiciona "│" no fim com spacing.
func padCardLine(prefix string, w int, bordStyle lipgloss.Style) string {
	plain := stripAnsi(prefix)
	currentW := lipgloss.Width(plain)
	pad := maxInt(0, w-1-currentW)
	return prefix + strings.Repeat(" ", pad) + bordStyle.Render("│")
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
	selBg := lipgloss.NewStyle().Background(lipgloss.Color("237")).
		Foreground(colorAccent).Bold(true)
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
			isSel := selSession != nil && s.SessionID == selSession.SessionID
			content := fmt.Sprintf("%s · %s · %s",
				when, dur, cost)
			if s.Kind == "compact" {
				content += " ↻"
			}
			var line string
			if isSel {
				line = selBg.Render("▶ " + content)
			} else {
				line = "  " + muted.Render(content)
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
	out.WriteString(bord.Render(strings.Repeat("─", maxInt(0, width))) + "\n")
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
	muted := lipgloss.NewStyle().Foreground(colorMuted)

	// Cores rotativas pra lanes
	laneColors := []lipgloss.Color{
		"#7dd3fc", "#f472b6", "#fbbf24", "#34d399", "#60a5fa", "#f87171",
	}
	threadLane := map[*stats.Thread]int{}
	for i, t := range v.threads {
		threadLane[t] = i % len(laneColors)
	}

	// Flat list ordenada cronologicamente
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
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].s.StartTime.Before(all[j-1].s.StartTime); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}

	// Identifica primeiro e último idx de cada thread
	firstIdx := map[*stats.Thread]int{}
	lastIdx := map[*stats.Thread]int{}
	for i, e := range all {
		if _, ok := firstIdx[e.t]; !ok {
			firstIdx[e.t] = i
		}
		lastIdx[e.t] = i
	}

	// Header com lane labels
	var b strings.Builder
	var laneHeader strings.Builder
	for _, t := range v.threads {
		col := laneColors[threadLane[t]]
		laneHeader.WriteString(lipgloss.NewStyle().Foreground(col).Bold(true).Render("│") + " ")
	}
	b.WriteString(laneHeader.String() + "  " + muted.Render("legenda das lanes →  ") +
		laneLegend(v.threads, laneColors, threadLane) + "\n")
	b.WriteString(muted.Render(strings.Repeat("─", maxInt(0, width))) + "\n")

	// Cada session = 1 row
	for i, e := range all {
		var lanes strings.Builder
		for _, t := range v.threads {
			col := laneColors[threadLane[t]]
			active := i >= firstIdx[t] && i <= lastIdx[t]
			isThis := t == e.t
			ch := " "
			if isThis {
				if e.s.Kind == "compact" {
					ch = lipgloss.NewStyle().Foreground(colorWarn).Bold(true).Render("◉")
				} else {
					ch = lipgloss.NewStyle().Foreground(col).Bold(true).Render("●")
				}
			} else if active {
				ch = lipgloss.NewStyle().Foreground(col).Render("│")
			}
			lanes.WriteString(ch + " ")
		}

		isCursor := v.flatRowIndex(e.t, e.s) == v.cursor
		when := e.s.StartTime.Local().Format("Mon 15:04")
		title := v.titleFor(e.s.Session)
		titleMax := maxInt(20, width-len(v.threads)*2-50)
		title = truncRight(title, titleMax)
		gapStr := ""
		if e.s.Kind == "compact" {
			gapStr = " " + lipgloss.NewStyle().Foreground(colorWarn).Render(
				fmt.Sprintf("↻%s", humanDurShort(e.s.GapFromPrev)))
		}

		var row string
		if isCursor {
			// Selection bar full width
			marker := lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(colorAccent).Bold(true).
				Render(fmt.Sprintf("▶ %s · %s · %s · [%s]",
					e.t.Branch, when, title, e.s.SessionID[:8]))
			plain := stripAnsi(marker)
			pad := maxInt(0, width-lipgloss.Width(plain)-lipgloss.Width(stripAnsi(lanes.String()))-2)
			row = lanes.String() + " " + marker +
				lipgloss.NewStyle().Background(lipgloss.Color("237")).Render(strings.Repeat(" ", pad))
		} else {
			row = lanes.String() + " " +
				branchPill(e.t.Branch) + " " +
				muted.Render(when) + " " +
				lipgloss.NewStyle().Foreground(colorFg).Render(title) +
				gapStr + " " +
				muted.Render("["+e.s.SessionID[:8]+"]")
		}
		b.WriteString(row + "\n")
	}
	return b.String()
}

// laneLegend retorna "feat/CC-1234 · main · feat/auth ..." colorido.
func laneLegend(threads []*stats.Thread, colors []lipgloss.Color, lane map[*stats.Thread]int) string {
	var parts []string
	for _, t := range threads {
		col := colors[lane[t]]
		b := t.Branch
		if b == "" {
			b = "(no branch)"
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(col).Render(b))
	}
	return strings.Join(parts, " · ")
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
		// Terminal muito estreito — degrada graciosamente
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render(
			"timeline view requer terminal ≥ 60 cols — use outra view ('v')")
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

	// Identifica session selecionada (pra highlight)
	var selSession *model.Session
	if v.cursor < len(v.flat) && v.flat[v.cursor].session != nil {
		selSession = v.flat[v.cursor].session.Session
	}

	// Thread rows
	for _, t := range v.threads {
		branch := t.Branch
		if branch == "" {
			branch = "(no branch)"
		}
		// Label com cursor marker se cursor estiver em session dessa thread
		threadHasCursor := false
		for _, s := range t.Sessions {
			if selSession != nil && s.SessionID == selSession.SessionID {
				threadHasCursor = true
				break
			}
		}
		marker := "  "
		if threadHasCursor {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("▶ ")
		}
		label := truncRight(branch, labelW-3)
		labelStr := marker + branchPill(label)
		b.WriteString(labelStr + strings.Repeat(" ", maxInt(0, labelW-lipgloss.Width(stripAnsi(labelStr)))) + "  ")

		// Build line de chars com posições
		line := []rune(strings.Repeat(" ", timelineW))
		// Track quais positions são selected
		selX := -1
		for i, s := range t.Sessions {
			x := timeToX(s.StartTime)
			ch := '●'
			if s.Kind == "compact" {
				ch = '◉'
			}
			if x < len(line) {
				line[x] = ch
			}
			if selSession != nil && s.SessionID == selSession.SessionID {
				selX = x
			}
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
		// Renderiza com cores
		col := branchColor(t.Branch)
		var colored strings.Builder
		for i, r := range line {
			s := string(r)
			switch {
			case i == selX:
				// Highlight selected session
				colored.WriteString(lipgloss.NewStyle().
					Background(lipgloss.Color("237")).
					Foreground(colorAccent).Bold(true).Render("◉"))
			case r == '◉':
				colored.WriteString(lipgloss.NewStyle().Foreground(colorWarn).Render(s))
			default:
				colored.WriteString(lipgloss.NewStyle().Foreground(col).Render(s))
			}
		}
		b.WriteString(colored.String())
		b.WriteString("  " + muted.Render(fmt.Sprintf("%d · $%.2f",
			len(t.Sessions), t.TotalCost)) + "\n")
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

	// Build flat node list (1 per session) — posicoes iniciais em CÍRCULO ao
	// redor do centro pra evitar convergir em borrão.
	type node struct {
		s     *stats.ThreadSession
		t     *stats.Thread
		x, y  float64
		vx,vy float64
		r     int
	}
	var nodes []*node
	totalSess := 0
	for _, t := range v.threads {
		totalSess += len(t.Sessions)
	}
	cx0, cy0 := float64(pixW)/2, float64(pixH)/2
	rInit := minFloat(float64(pixW), float64(pixH))/2 - 12
	if rInit < 20 {
		rInit = 20
	}
	idx := 0
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
			// Distribute em círculo igualmente
			theta := 2 * 3.14159265 * float64(idx) / float64(maxInt(1, totalSess))
			nodes = append(nodes, &node{
				s: s, t: t,
				x: cx0 + rInit*cosApprox(theta),
				y: cy0 + rInit*sinApprox(theta),
				r: r,
			})
			idx++
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

	// Force-directed mais agressivo — k baseado em area + node count, repulsão
	// reforçada, atração intra-projeto reduzida. 150 iters pra convergir.
	cx, cy := cx0, cy0
	k := sqrt(float64(pixW*pixH)/float64(maxInt(1, totalSess))) * 0.85

	for iter := 0; iter < 150; iter++ {
		// Cooling: força reduz com tempo pra estabilizar
		coolFactor := 1.0 - float64(iter)/200.0
		if coolFactor < 0.2 {
			coolFactor = 0.2
		}

		for _, a := range nodes {
			a.vx, a.vy = 0, 0
			// Repulsão forte entre todos
			for _, b := range nodes {
				if a == b {
					continue
				}
				dx := a.x - b.x
				dy := a.y - b.y
				d := sqrt(dx*dx + dy*dy)
				if d < 0.5 {
					d = 0.5
				}
				f := (k * k) / d * 1.5
				a.vx += (dx / d) * f
				a.vy += (dy / d) * f
			}
			// Atração intra-projeto SUAVE (cluster mas não colado)
			for _, b := range nodes {
				if a == b || a.t.ProjectDir != b.t.ProjectDir {
					continue
				}
				dx := a.x - b.x
				dy := a.y - b.y
				d := sqrt(dx*dx + dy*dy)
				if d < 0.5 {
					continue
				}
				f := (d * d) / k * 0.15
				a.vx -= (dx / d) * f
				a.vy -= (dy / d) * f
			}
			// Pull suave pro centro
			a.vx += (cx - a.x) * 0.003
			a.vy += (cy - a.y) * 0.003
		}
		// Edge attraction
		for _, e := range edges {
			dx := e.a.x - e.b.x
			dy := e.a.y - e.b.y
			d := sqrt(dx*dx + dy*dy)
			if d < 0.5 {
				continue
			}
			f := (d * d) / k * 0.5
			e.a.vx -= (dx / d) * f
			e.a.vy -= (dy / d) * f
			e.b.vx += (dx / d) * f
			e.b.vy += (dy / d) * f
		}
		// Apply velocity com damping + clamp
		for _, n := range nodes {
			speed := sqrt(n.vx*n.vx + n.vy*n.vy)
			if speed < 0.01 {
				speed = 0.01
			}
			cap := speed
			if cap > 8 {
				cap = 8
			}
			n.x += (n.vx / speed) * cap * coolFactor * 0.4
			n.y += (n.vy / speed) * cap * coolFactor * 0.4
			margin := 6.0
			if n.x < margin {
				n.x = margin
			}
			if n.x > float64(pixW)-margin {
				n.x = float64(pixW) - margin
			}
			if n.y < margin {
				n.y = margin
			}
			if n.y > float64(pixH)-margin {
				n.y = float64(pixH) - margin
			}
		}
	}

	// Draw edges first
	for _, e := range edges {
		col := projectColors[e.a.t.ProjectDir]
		canvas.line(int(e.a.x), int(e.a.y), int(e.b.x), int(e.b.y), col, e.dashed)
	}
	// Identifica session selecionada
	var selSession *model.Session
	var selNode *node
	if v.cursor < len(v.flat) && v.flat[v.cursor].session != nil {
		selSession = v.flat[v.cursor].session.Session
		for _, n := range nodes {
			if n.s.SessionID == selSession.SessionID {
				selNode = n
				break
			}
		}
	}

	// Draw nodes
	for _, n := range nodes {
		col := projectColors[n.t.ProjectDir]
		canvas.circle(int(n.x), int(n.y), n.r, col)
	}
	// Draw cursor anel + crosshair sobre nó selecionado (último, fica em cima)
	if selNode != nil {
		accentCol := lipgloss.Color("#a78bfa")
		// Anel maior accent (apenas borda — pixels no perímetro)
		ringR := selNode.r + 3
		for theta := 0.0; theta < 6.28; theta += 0.05 {
			x := int(selNode.x + float64(ringR)*cosApprox(theta))
			y := int(selNode.y + float64(ringR)*sinApprox(theta))
			canvas.set(x, y, accentCol)
			canvas.set(x+1, y, accentCol)
		}
		// Crosshair: linhas curtas saindo do node
		cx0 := int(selNode.x)
		cy0 := int(selNode.y)
		canvas.line(cx0-ringR-3, cy0, cx0-ringR-1, cy0, accentCol, false)
		canvas.line(cx0+ringR+1, cy0, cx0+ringR+3, cy0, accentCol, false)
		canvas.line(cx0, cy0-ringR-3, cx0, cy0-ringR-1, accentCol, false)
		canvas.line(cx0, cy0+ringR+1, cx0, cy0+ringR+3, accentCol, false)
	}

	// Render + legend + selected info
	out := canvas.render()

	muted := lipgloss.NewStyle().Foreground(colorMuted)
	var legend strings.Builder
	if selSession != nil {
		// Highlight da session selecionada (acima da legenda)
		legend.WriteString(lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Foreground(colorAccent).Bold(true).
			Render(fmt.Sprintf("▶ session selecionada: [%s] %s",
				selSession.SessionID[:8],
				truncRight(v.titleFor(selSession), 60))) + "\n")
	}
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
	for i := 0; i < 12; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// cosApprox/sinApprox — Taylor series simple. Sem importar math.
func cosApprox(x float64) float64 {
	// Reduz pra [-π, π]
	for x > 3.14159265 {
		x -= 2 * 3.14159265
	}
	for x < -3.14159265 {
		x += 2 * 3.14159265
	}
	x2 := x * x
	return 1 - x2/2 + x2*x2/24 - x2*x2*x2/720
}

func sinApprox(x float64) float64 {
	for x > 3.14159265 {
		x -= 2 * 3.14159265
	}
	for x < -3.14159265 {
		x += 2 * 3.14159265
	}
	x2 := x * x
	return x - x*x2/6 + x*x2*x2/120 - x*x2*x2*x2/5040
}

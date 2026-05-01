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
	// Miller: 3 panes navegáveis. millerPane indica qual está focado.
	// Cada pane tem índice próprio.
	millerPane     int // 0=projects, 1=branches, 2=sessions
	millerProjIdx  int
	millerBranchIdx int
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
	if v.view == threadViewMiller {
		v.syncMillerFromCursor()
	}
}

// syncMillerFromCursor posiciona millerProjIdx/millerBranchIdx baseado em qual
// thread o cursor atual aponta. Usado quando user entra na view Miller vinda
// de outra — pra que os panes mostrem o contexto correto.
func (v *threadsView) syncMillerFromCursor() {
	if v.cursor >= len(v.flat) {
		return
	}
	curThread := v.flat[v.cursor].thread
	if curThread == nil {
		return
	}
	grouped := stats.GroupByProject(v.threads)
	dirs := stats.SortedProjectDirs(grouped)
	for i, d := range dirs {
		if d == curThread.ProjectDir {
			v.millerProjIdx = i
			break
		}
	}
	branches := grouped[curThread.ProjectDir]
	for i, t := range branches {
		if t == curThread {
			v.millerBranchIdx = i
			break
		}
	}
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
// Em Miller, ↑↓ navega DENTRO do pane focado.
func (v *threadsView) MoveCursor(delta int) {
	if len(v.flat) == 0 {
		return
	}
	if v.view == threadViewMiller {
		v.moveCursorMiller(delta)
		return
	}
	if v.view == threadViewGalaxy {
		v.moveCursorGalaxy(0, delta)
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

// moveCursorMiller — pane focado decide o que mexe.
//   pane 0 (projects):  muda projeto selecionado
//   pane 1 (branches):  muda branch dentro do projeto
//   pane 2 (sessions):  muda session (igual MoveCursor padrão)
func (v *threadsView) moveCursorMiller(delta int) {
	grouped := stats.GroupByProject(v.threads)
	dirs := stats.SortedProjectDirs(grouped)

	switch v.millerPane {
	case 0:
		v.millerProjIdx = clampMiller(v.millerProjIdx+delta, 0, len(dirs)-1)
		v.millerBranchIdx = 0 // reseta branch ao trocar projeto
		v.syncCursorFromMiller(dirs, grouped)
	case 1:
		if v.millerProjIdx < len(dirs) {
			branches := grouped[dirs[v.millerProjIdx]]
			v.millerBranchIdx = clampMiller(v.millerBranchIdx+delta, 0, len(branches)-1)
			v.syncCursorFromMiller(dirs, grouped)
		}
	case 2:
		// Move dentro das sessions da branch selecionada
		if v.millerProjIdx < len(dirs) {
			branches := grouped[dirs[v.millerProjIdx]]
			if v.millerBranchIdx < len(branches) {
				thread := branches[v.millerBranchIdx]
				// Acha cursor atual e move
				curIdx := -1
				for i, row := range v.flat {
					if row.thread == thread && row.session != nil {
						if i == v.cursor {
							curIdx = i
							break
						}
					}
				}
				if curIdx == -1 {
					// posiciona na primeira da branch
					for i, row := range v.flat {
						if row.thread == thread && row.session != nil {
							v.cursor = i
							return
						}
					}
					return
				}
				// Move dentro dessa thread só
				next := curIdx + sign(delta)
				for next >= 0 && next < len(v.flat) {
					if v.flat[next].thread == thread && v.flat[next].session != nil {
						v.cursor = next
						return
					}
					next += sign(delta)
				}
			}
		}
	}
}

// syncCursorFromMiller posiciona o cursor na primeira session do projeto+branch
// selecionados nas panes 0/1.
func (v *threadsView) syncCursorFromMiller(dirs []string, grouped map[string][]*stats.Thread) {
	if v.millerProjIdx >= len(dirs) {
		return
	}
	branches := grouped[dirs[v.millerProjIdx]]
	if v.millerBranchIdx >= len(branches) {
		v.millerBranchIdx = 0
	}
	if len(branches) == 0 {
		return
	}
	thread := branches[v.millerBranchIdx]
	for i, row := range v.flat {
		if row.thread == thread && row.session != nil {
			v.cursor = i
			return
		}
	}
}

func clampMiller(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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

// moveCursorGalaxy navega pelos nodes do galaxy.
//   ←→ = sessão anterior/próxima cronologicamente
//   ↑↓ = pula pra próxima/anterior thread (cicla pelas threads, lands na 1a session)
// Sem loop: cada press de ↑/↓ avança thread index, não vai e volta.
func (v *threadsView) moveCursorGalaxy(dx, dy int) {
	if v.cursor >= len(v.flat) {
		return
	}
	curRow := v.flat[v.cursor]
	if curRow.session == nil {
		return
	}
	if dx != 0 {
		next := v.cursor + sign(dx)
		for next >= 0 && next < len(v.flat) && v.flat[next].session == nil {
			next += sign(dx)
		}
		if next >= 0 && next < len(v.flat) {
			v.cursor = next
		}
	}
	if dy != 0 {
		// Cicla por threads. Acha índice atual, soma delta, wrap, pula pra
		// 1a session da nova thread.
		curIdx := indexOfThread(v.threads, curRow.thread)
		if curIdx < 0 {
			return
		}
		n := len(v.threads)
		newIdx := (curIdx + sign(dy) + n) % n
		newThread := v.threads[newIdx]
		for i, row := range v.flat {
			if row.thread == newThread && row.session != nil {
				v.cursor = i
				return
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
//   linha 1: view badges (◉ active)
//   linha 2: posição/contexto da seleção (compacta pra caber em terminais ~80c)
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
	viewStrip := strings.Join(badges, " ")

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

	// Linha 2: posição compacta. Em terminais largos, mostra mais; em estreitos
	// degrada graciosamente removendo campos da direita.
	var line2 string
	if v.cursor < len(v.flat) && v.flat[v.cursor].session != nil {
		row := v.flat[v.cursor]
		s := row.session
		when := s.StartTime.Local().Format("02/01 15:04")
		cost := "—"
		if v.pricing != nil {
			if c, ok := v.pricing.Cost(s.Session); ok {
				cost = fmt.Sprintf("$%.2f", c.USD)
			}
		}
		costStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		sep := muted.Render(" · ")
		// Constrói por partes; pode tirar campos se passar de width.
		parts := []string{
			accent.Render(fmt.Sprintf("session %d/%d", currentIdx, totalSessions)),
			branchPill(row.thread.Branch),
			muted.Render(when),
			costStyle.Render(cost),
			muted.Render("["+s.SessionID[:6]+"]"),
		}
		line2 = strings.Join(parts, sep)
		// Hint à esquerda — só mostra se sobrar espaço
		hint := muted.Render("[v] view  [↑↓ ←→] nav  [enter] resume")
		if lipgloss.Width(stripAnsi(line2))+lipgloss.Width(stripAnsi(hint))+3 <= width {
			line2 = hint + sep + line2
		}
	} else {
		line2 = muted.Render(fmt.Sprintf("%d threads · %d sessions",
			len(v.threads), totalSessions))
	}

	border := lipgloss.NewStyle().Foreground(colorBorder).
		Render(strings.Repeat("─", maxInt(0, width)))

	return viewStrip + "\n" + line2 + "\n" + border
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

		// Sidechain badge — só aparece se session teve subagents
		subStr := ""
		if s.SidechainTurns > 0 {
			subStr = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa")).
				Render(fmt.Sprintf("↳%d", s.SidechainAgents))
		}

		line := fmt.Sprintf("%s  %s  %s  %-5s  %s%s%s  %s",
			borderStyle.Render("│"),
			dot,
			muted.Render(when),
			dr,
			lipgloss.NewStyle().Foreground(colorFg).Render(title),
			gapStr,
			subStr,
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
	sel := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	// 3 cols: 22, 26, rest
	colW := []int{22, 26, width - 22 - 26 - 4}
	if colW[2] < 30 {
		colW[2] = 30
	}

	// Estado vem dos pane indices, NÃO do cursor flat (que pode estar
	// dessincronizado quando user navega só pelos panes 0/1).
	grouped := stats.GroupByProject(v.threads)
	dirs := stats.SortedProjectDirs(grouped)
	if len(dirs) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render("(nenhuma thread)")
	}
	if v.millerProjIdx >= len(dirs) {
		v.millerProjIdx = 0
	}
	selProject := dirs[v.millerProjIdx]
	branches := grouped[selProject]
	if v.millerBranchIdx >= len(branches) {
		v.millerBranchIdx = 0
	}
	var selThread *stats.Thread
	if len(branches) > 0 {
		selThread = branches[v.millerBranchIdx]
	}
	selBranch := ""
	if selThread != nil {
		selBranch = selThread.Branch
	}
	var selSession *model.Session
	if v.cursor < len(v.flat) && v.flat[v.cursor].session != nil {
		selSession = v.flat[v.cursor].session.Session
	}

	// Header com pane focado destacado
	paneHeader := func(title string, focused bool) string {
		if focused {
			return lipgloss.NewStyle().Foreground(colorAccent).Bold(true).
				Padding(0, 1).Render("◉ " + title)
		}
		return lipgloss.NewStyle().Foreground(colorMuted).Bold(true).
			Padding(0, 1).Render("  " + title)
	}

	// Pane 1: projects
	var p1 strings.Builder
	p1.WriteString(paneHeader("PROJECTS", v.millerPane == 0) + "\n")
	for i, d := range dirs {
		short := shortPath(d, mustHomeTUI())
		short = truncRight(short, colW[0]-6)
		count := 0
		for _, t := range grouped[d] {
			count += len(t.Sessions)
		}
		body := fmt.Sprintf(" %-*s %3d", colW[0]-6, short, count)
		switch {
		case i == v.millerProjIdx && v.millerPane == 0:
			p1.WriteString(sel.Render("▶"+body) + "\n")
		case i == v.millerProjIdx:
			p1.WriteString(lipgloss.NewStyle().Foreground(colorFg).Render("▶"+body) + "\n")
		default:
			p1.WriteString(" " + muted.Render(body) + "\n")
		}
	}

	// Pane 2: branches do projeto selecionado
	var p2 strings.Builder
	p2.WriteString(paneHeader("BRANCHES", v.millerPane == 1) + "\n")
	for i, t := range branches {
		b := t.Branch
		if b == "" {
			b = "(no branch)"
		}
		b = truncRight(b, colW[1]-6)
		body := fmt.Sprintf(" %-*s %3d", colW[1]-6, b, len(t.Sessions))
		switch {
		case i == v.millerBranchIdx && v.millerPane == 1:
			p2.WriteString(sel.Render("▶"+body) + "\n")
		case i == v.millerBranchIdx:
			p2.WriteString(lipgloss.NewStyle().Foreground(colorFg).Render("▶"+body) + "\n")
		default:
			p2.WriteString(" " + muted.Render(body) + "\n")
		}
	}

	// Pane 3: sessions da branch selecionada
	var p3 strings.Builder
	p3.WriteString(paneHeader("SESSIONS", v.millerPane == 2) + "\n")
	selBg := lipgloss.NewStyle().Background(lipgloss.Color("237")).
		Foreground(colorAccent).Bold(true)
	if selThread != nil {
		for _, s := range selThread.Sessions {
			when := s.StartTime.Local().Format("Mon 15:04")
			dur := fmtDuration(s.Duration())
			cost := "?"
			if v.pricing != nil {
				if c, ok := v.pricing.Cost(s.Session); ok {
					cost = fmt.Sprintf("$%.2f", c.USD)
				}
			}
			isSel := selSession != nil && s.SessionID == selSession.SessionID
			content := fmt.Sprintf("%s · %s · %s", when, dur, cost)
			if s.Kind == "compact" {
				content += " ↻"
			}
			var line string
			switch {
			case isSel && v.millerPane == 2:
				line = selBg.Render("▶ " + content)
			case isSel:
				line = lipgloss.NewStyle().Foreground(colorFg).Render("▶ " + content)
			default:
				line = "  " + muted.Render(content)
			}
			p3.WriteString(line + "\n")
		}
	}

	// Join 3 panes side-by-side com border colorida no pane focado
	focusedBord := lipgloss.NewStyle().Foreground(colorAccent)
	sep1 := bord.Render("│")
	sep2 := bord.Render("│")
	if v.millerPane == 0 || v.millerPane == 1 {
		sep1 = focusedBord.Render("┃")
	}
	if v.millerPane == 1 || v.millerPane == 2 {
		sep2 = focusedBord.Render("┃")
	}

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
		l1 = padRight(l1, colW[0])
		l2 = padRight(l2, colW[1])
		out.WriteString(l1 + sep1 + l2 + sep2 + l3 + "\n")
	}

	// Preview pane embaixo
	out.WriteString(bord.Render(strings.Repeat("─", maxInt(0, width))) + "\n")
	hint := muted.Render("[← →] muda pane  [↑ ↓] move dentro do pane")
	out.WriteString(" " + hint + "\n")
	if selSession != nil {
		summary := v.titleFor(selSession)
		summary = truncRight(summary, width-4)
		out.WriteString(" " + lipgloss.NewStyle().Foreground(colorFg).Render(summary) + "\n")
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

// renderTimeline mostra threads como rows horizontais ao longo do tempo.
// Layout simples: [label branch] [gráfico ●─●] [count · cost].
// Datas no topo, horas no rodapé. Cursor highlight no ◉ accent.
func (v threadsView) renderTimeline(width int) string {
	if len(v.threads) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render("(nenhuma thread)")
	}
	muted := lipgloss.NewStyle().Foreground(colorMuted)

	// Range global
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
	totalSpan := maxT.Sub(minT)
	if totalSpan == 0 {
		totalSpan = time.Hour
	}

	// Layout: label esquerdo, gráfico, info direita
	labelW := 22
	infoW := 14
	timelineW := width - labelW - infoW - 4
	if timelineW < 24 {
		return lipgloss.NewStyle().Foreground(colorMuted).Padding(2).Render(
			"timeline view requer terminal ≥ 70 cols — use outra view ('v')")
	}

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

	// Cursor
	var selSession *model.Session
	var selThread *stats.Thread
	if v.cursor < len(v.flat) && v.flat[v.cursor].session != nil {
		selSession = v.flat[v.cursor].session.Session
		selThread = v.flat[v.cursor].thread
	}

	var b strings.Builder
	leftPad := strings.Repeat(" ", labelW+2)

	// Day markers (topo) — DD/MM em cada dia do span
	dayLine := []rune(strings.Repeat(" ", timelineW))
	day := time.Date(minT.Year(), minT.Month(), minT.Day(), 0, 0, 0, 0, minT.Location())
	for ; !day.After(maxT); day = day.AddDate(0, 0, 1) {
		x := timeToX(day)
		label := day.Format("02/01")
		for i, r := range []rune(label) {
			if x+i < len(dayLine) {
				dayLine[x+i] = r
			}
		}
	}
	b.WriteString(leftPad + muted.Render(string(dayLine)) + "\n")
	b.WriteString(leftPad + muted.Render(strings.Repeat("─", timelineW)) + "\n")

	// Thread rows
	for _, t := range v.threads {
		branch := t.Branch
		if branch == "" {
			branch = "(no branch)"
		}
		threadHasCursor := selThread == t
		marker := "  "
		if threadHasCursor {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("▶ ")
		}
		labelStr := marker + branchPill(truncRight(branch, labelW-3))
		b.WriteString(labelStr +
			strings.Repeat(" ", maxInt(0, labelW-lipgloss.Width(stripAnsi(labelStr)))) +
			"  ")

		line := []rune(strings.Repeat(" ", timelineW))
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
		col := branchColor(t.Branch)
		var colored strings.Builder
		for i, r := range line {
			s := string(r)
			switch {
			case i == selX:
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
		info := fmt.Sprintf("  %3d · $%5.2f", len(t.Sessions), t.TotalCost)
		b.WriteString(muted.Render(info) + "\n")
	}

	// Footer: hour markers se span pequeno
	b.WriteString(leftPad + muted.Render(strings.Repeat("─", timelineW)) + "\n")
	if totalSpan < 7*24*time.Hour {
		hourLine := []rune(strings.Repeat(" ", timelineW))
		hr := time.Date(minT.Year(), minT.Month(), minT.Day(), minT.Hour(), 0, 0, 0, minT.Location())
		step := 6 * time.Hour
		if totalSpan < 12*time.Hour {
			step = time.Hour
		} else if totalSpan < 48*time.Hour {
			step = 3 * time.Hour
		}
		for ; !hr.After(maxT); hr = hr.Add(step) {
			x := timeToX(hr)
			label := hr.Format("15h")
			for i, r := range []rune(label) {
				if x+i < len(hourLine) {
					hourLine[x+i] = r
				}
			}
		}
		b.WriteString(leftPad + muted.Render(string(hourLine)) + "\n")
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
// labels é um grid de runes em char-coords que sobrescreve o braille no render
// final (usado pra colocar texto sobre nodes do galaxy).
type brailleCanvas struct {
	pixels      [][]uint8 // pixel grid (h*4 rows × w*2 cols)
	colors      [][]lipgloss.Color
	labels      [][]rune          // char grid — rune != 0 sobrescreve braille
	labelColors [][]lipgloss.Color
	labelBold   [][]bool
	w, h        int // chars
}

func newBraille(w, h int) *brailleCanvas {
	pixels := make([][]uint8, h*4)
	for i := range pixels {
		pixels[i] = make([]uint8, w*2)
	}
	colors := make([][]lipgloss.Color, h)
	labels := make([][]rune, h)
	labelColors := make([][]lipgloss.Color, h)
	labelBold := make([][]bool, h)
	for i := range colors {
		colors[i] = make([]lipgloss.Color, w)
		labels[i] = make([]rune, w)
		labelColors[i] = make([]lipgloss.Color, w)
		labelBold[i] = make([]bool, w)
	}
	return &brailleCanvas{
		pixels: pixels, colors: colors,
		labels: labels, labelColors: labelColors, labelBold: labelBold,
		w: w, h: h,
	}
}

// label coloca string s a partir de (cx, cy) em coords de char.
// Sobrescreve qualquer braille daquele char durante o render. Trunca em
// limites do canvas.
func (c *brailleCanvas) label(cx, cy int, s string, color lipgloss.Color, bold bool) {
	if cy < 0 || cy >= c.h {
		return
	}
	for i, r := range []rune(s) {
		x := cx + i
		if x < 0 || x >= c.w {
			continue
		}
		c.labels[cy][x] = r
		c.labelColors[cy][x] = color
		c.labelBold[cy][x] = bold
	}
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
		// Track style "key" for run-coalescing (color + bold).
		type styleKey struct {
			col  lipgloss.Color
			bold bool
		}
		var curStyle styleKey
		var buf strings.Builder
		flush := func() {
			if buf.Len() == 0 {
				return
			}
			st := lipgloss.NewStyle()
			if curStyle.col != "" {
				st = st.Foreground(curStyle.col)
			}
			if curStyle.bold {
				st = st.Bold(true)
			}
			line.WriteString(st.Render(buf.String()))
			buf.Reset()
		}
		for cx := 0; cx < c.w; cx++ {
			var ch rune = ' '
			var col lipgloss.Color
			var bold bool
			// Label overrides braille
			if lbl := c.labels[cy][cx]; lbl != 0 {
				ch = lbl
				col = c.labelColors[cy][cx]
				bold = c.labelBold[cy][cx]
			} else {
				var b uint8
				for row := 0; row < 4; row++ {
					for cc := 0; cc < 2; cc++ {
						px, py := cx*2+cc, cy*4+row
						if c.pixels[py][px] != 0 {
							b |= bits[row][cc]
						}
					}
				}
				if b != 0 {
					ch = rune(0x2800 + int(b))
				}
				col = c.colors[cy][cx]
			}
			st := styleKey{col: col, bold: bold}
			if st != curStyle {
				flush()
				curStyle = st
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

	// Labels: número sequencial em cada node (#01, #02...). A legenda embaixo
	// mapeia número → branch · projeto · time. Selected node mostra a branch
	// inline pra contexto rápido.
	type charBox struct{ cx, cy, w int }
	var occupied []charBox
	overlaps := func(b charBox) bool {
		for _, o := range occupied {
			if b.cy == o.cy && b.cx < o.cx+o.w && b.cx+b.w > o.cx {
				return true
			}
		}
		return false
	}
	// Atribui número por ordem de aparição nos nodes (cronológica nessa view)
	for i, n := range nodes {
		num := i + 1
		isSel := selNode != nil && n == selNode
		var lbl string
		if isSel {
			br := n.t.Branch
			if br == "" {
				br = "(no-br)"
			}
			lbl = fmt.Sprintf(" #%02d %s", num, truncRight(br, 14))
		} else {
			lbl = fmt.Sprintf(" #%02d", num)
		}
		// Char coords: tenta direita, depois esquerda, depois baixo
		cx := int(n.x)/2 + n.r + 1
		cy := int(n.y) / 4
		w := lipgloss.Width(lbl)
		positions := []charBox{
			{cx, cy, w},
			{int(n.x)/2 - n.r - w, cy, w},
			{int(n.x) / 2, cy + 1, w},
			{int(n.x) / 2, cy - 1, w},
		}
		var placed *charBox
		for _, p := range positions {
			if p.cx < 0 || p.cy < 0 || p.cy >= height-2 || p.cx+p.w > width {
				continue
			}
			if overlaps(p) {
				continue
			}
			placed = &p
			break
		}
		if placed == nil {
			continue
		}
		col := projectColors[n.t.ProjectDir]
		if isSel {
			col = lipgloss.Color("#a78bfa")
		}
		canvas.label(placed.cx, placed.cy, lbl, col, isSel)
		occupied = append(occupied, *placed)
	}

	// Render + legend
	out := canvas.render()

	muted := lipgloss.NewStyle().Foreground(colorMuted)
	var legend strings.Builder

	// Highlight da session selecionada
	if selNode != nil {
		num := 0
		for i, n := range nodes {
			if n == selNode {
				num = i + 1
				break
			}
		}
		when := selSession.StartTime.Local().Format("02/01 15:04")
		legend.WriteString(lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Foreground(colorAccent).Bold(true).
			Render(fmt.Sprintf(" ▶ #%02d  %s  %s ",
				num, when,
				truncRight(v.titleFor(selSession), maxInt(20, width-30)))) + "\n")
	}

	// Legenda: mapeia # → contexto. 2 colunas se couber.
	legend.WriteString(muted.Render("─── nodes ───") + "\n")
	colW := 38
	cols := width / colW
	if cols < 1 {
		cols = 1
	}
	for i, n := range nodes {
		num := i + 1
		br := n.t.Branch
		if br == "" {
			br = "(no-br)"
		}
		br = truncRight(br, 18)
		when := n.s.StartTime.Local().Format("02/01 15:04")
		col := projectColors[n.t.ProjectDir]
		marker := lipgloss.NewStyle().Foreground(col).Render("●")
		isSel := selNode != nil && n == selNode
		numStyle := lipgloss.NewStyle().Foreground(col)
		if isSel {
			numStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		entry := fmt.Sprintf("%s %s %-18s %s",
			marker,
			numStyle.Render(fmt.Sprintf("#%02d", num)),
			lipgloss.NewStyle().Foreground(colorFg).Render(br),
			muted.Render(when))
		legend.WriteString(entry)
		if (i+1)%cols == 0 {
			legend.WriteString("\n")
		} else {
			legend.WriteString("  ")
		}
	}
	if len(nodes)%cols != 0 {
		legend.WriteString("\n")
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

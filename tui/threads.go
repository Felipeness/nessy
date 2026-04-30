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
	case threadViewMiller, threadViewGraph, threadViewTimeline, threadViewGalaxy:
		// Placeholders pras outras views — implementar nas próximas fases
		return v.renderTodo(width, v.view.String())
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
// View 2 — Cards (placeholder, próxima fase)
// =============================================================================

func (v threadsView) renderCards(width int) string {
	return v.renderTodo(width, "cards")
}

func (v threadsView) renderTodo(width int, name string) string {
	return lipgloss.NewStyle().
		Foreground(colorMuted).
		Padding(2, 4).
		Render(fmt.Sprintf("(%s view — próxima fase. Aperte 'v' pra voltar pro tree.)", name))
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

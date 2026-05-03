package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/pricing"
)

const (
	activityActive = 5 * time.Minute
	activityPaused = time.Hour
)

type recentView struct {
	sessions       []*model.Session
	pricing        *pricing.Pricing
	summaries      map[string]string // session_id → AI summary, populado quando AI tá ativo
	cursor         int
	groupByProject bool
}

func newRecentView(sessions []*model.Session, p *pricing.Pricing, summaries map[string]string) recentView {
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].EndTime.After(sessions[j].EndTime)
	})
	return recentView{sessions: sessions, pricing: p, summaries: summaries}
}

// titleFor escolhe o melhor "nome" pra mostrar primeiro: AI summary se
// existir, senão primeira user msg, senão "(sem mensagens)".
func (v recentView) titleFor(s *model.Session) string {
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

// firstSentence pega o primeiro período da summary (até "." ou "\n"),
// pra encurtar pra title de 1 linha.
func firstSentence(s string) string {
	for _, sep := range []string{"\n", ". ", " — "} {
		if i := strings.Index(s, sep); i > 0 && i < 200 {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

func (v recentView) selected() *model.Session {
	if v.cursor < 0 || v.cursor >= len(v.sessions) {
		return nil
	}
	return v.sessions[v.cursor]
}

func (v recentView) View(width, height int) string {
	if len(v.sessions) == 0 {
		return lipgloss.NewStyle().Width(width).Render("(nenhuma session encontrada)")
	}
	if v.groupByProject {
		return v.viewByProject(width, height)
	}
	return v.viewByTime(width, height)
}

func (v recentView) viewByTime(width, height int) string {
	now := time.Now()
	var lines []string
	cursorLine := 0
	var lastBucket string
	for i, s := range v.sessions {
		bucket := timeBucket(now, s.EndTime)
		if bucket != lastBucket {
			lines = append(lines, fmt.Sprintf("─── %s ─────────────", bucket))
			lastBucket = bucket
		}
		if i == v.cursor {
			cursorLine = len(lines)
		}
		lines = append(lines, recentEntryLines(s, v, now, width, i == v.cursor)...)
	}
	return scrollWindow(lines, cursorLine, height)
}

// recentEntryLines renderiza uma session em 2 linhas:
//   ▶ 🟢 Implementing statusline studio with drag-drop
//        16:34 · 41m · Opus · 1.2M · $4.32 · ~/Desktop/projects/claude-history
//
// Linha 1: title (AI summary > FirstUserMsg) — destaque cromado quando
// for o cursor. Linha 2: metadata em muted.
func recentEntryLines(s *model.Session, v recentView, now time.Time, width int, selected bool) []string {
	icon := activityIcon(now.Sub(s.EndTime))
	marker := "  "
	if selected {
		marker = "▶ "
	}
	title := v.titleFor(s)
	titleMax := width - 6
	if len(title) > titleMax && titleMax > 10 {
		title = title[:titleMax-1] + "…"
	}
	titleStyled := title
	if selected {
		titleStyled = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title)
	}

	dur := fmtDuration(s.Duration())
	badge := ModelBadge(s.Model)
	tokens := fmtTokens(s.TotalTokens())
	cost := "?"
	if v.pricing != nil {
		if c, ok := v.pricing.Cost(s); ok {
			cost = fmt.Sprintf("$%.2f", c.USD)
		}
	}
	dir := truncatePath(s.ProjectDir, 40)
	branch := ""
	if s.GitBranch != "" {
		branch = " · " + s.GitBranch
	}
	meta := fmt.Sprintf("%s · %s · %s · %s · %s · %s%s",
		s.EndTime.Local().Format("Mon 15:04"),
		dur, badge, tokens, cost, dir, branch,
	)
	muted := lipgloss.NewStyle().Foreground(colorMuted).Render(meta)

	return []string{
		fmt.Sprintf("%s%s %s", marker, icon, titleStyled),
		fmt.Sprintf("     %s", muted),
	}
}

func (v recentView) viewByProject(width, height int) string {
	groups := map[string][]*model.Session{}
	for _, s := range v.sessions {
		groups[s.ProjectDir] = append(groups[s.ProjectDir], s)
	}
	type entry struct {
		dir  string
		list []*model.Session
	}
	flat := make([]entry, 0, len(groups))
	for d, l := range groups {
		flat = append(flat, entry{d, l})
	}
	sort.Slice(flat, func(i, j int) bool {
		return flat[i].list[0].EndTime.After(flat[j].list[0].EndTime)
	})
	// Mapeia cursor (índice em v.sessions) -> linha visível pra manter selecionado
	// no viewport quando user navega pelo modo agrupado.
	cursorSessionID := ""
	if v.cursor >= 0 && v.cursor < len(v.sessions) {
		cursorSessionID = v.sessions[v.cursor].SessionID
	}
	now := time.Now()
	var lines []string
	cursorLine := 0
	for _, e := range flat {
		lines = append(lines, fmt.Sprintf("%s (%d sessions)", e.dir, len(e.list)))
		for _, s := range e.list {
			if s.SessionID == cursorSessionID {
				cursorLine = len(lines)
			}
			lines = append(lines, fmt.Sprintf("  %s", formatDenseRow(s, v.pricing, now, width-4)))
		}
		lines = append(lines, "")
	}
	return scrollWindow(lines, cursorLine, height)
}

func activityIcon(since time.Duration) string {
	switch {
	case since < activityActive:
		return "🟢"
	case since < activityPaused:
		return "🟡"
	default:
		return "⚪"
	}
}

func timeBucket(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 24*time.Hour && now.Day() == t.Day():
		return "Today"
	case d < 48*time.Hour:
		return "Yesterday"
	case d < 7*24*time.Hour:
		return "This week"
	default:
		return "Older"
	}
}

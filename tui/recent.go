package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
)

const (
	activityActive = 5 * time.Minute
	activityPaused = time.Hour
)

type recentView struct {
	sessions       []*model.Session
	pricing        *pricing.Pricing
	cursor         int
	groupByProject bool
}

func newRecentView(sessions []*model.Session, p *pricing.Pricing) recentView {
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].EndTime.After(sessions[j].EndTime)
	})
	return recentView{sessions: sessions, pricing: p}
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
		return v.viewByProject(width)
	}
	return v.viewByTime(width)
}

func (v recentView) viewByTime(width int) string {
	now := time.Now()
	var b strings.Builder
	var lastBucket string
	for i, s := range v.sessions {
		bucket := timeBucket(now, s.EndTime)
		if bucket != lastBucket {
			fmt.Fprintf(&b, "─── %s ─────────────\n", bucket)
			lastBucket = bucket
		}
		marker := " "
		if i == v.cursor {
			marker = "▶"
		}
		fmt.Fprintf(&b, "%s %s\n", marker, formatDenseRow(s, v.pricing, now, width-2))
	}
	return b.String()
}

func (v recentView) viewByProject(width int) string {
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
	now := time.Now()
	var b strings.Builder
	for _, e := range flat {
		fmt.Fprintf(&b, "%s (%d sessions)\n", e.dir, len(e.list))
		for _, s := range e.list {
			fmt.Fprintf(&b, "  %s\n", formatDenseRow(s, v.pricing, now, width-4))
		}
		b.WriteString("\n")
	}
	return b.String()
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

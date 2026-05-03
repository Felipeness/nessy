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

type timelineView struct {
	sessions []*model.Session
	pricing  *pricing.Pricing

	scroll int
}

func (v *timelineView) Scroll(delta int) { v.scroll += delta }

func newTimelineView(sessions []*model.Session, p *pricing.Pricing) timelineView {
	return timelineView{sessions: sessions, pricing: p}
}

func (v timelineView) View(width, height int) string {
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	var b strings.Builder

	now := time.Now()
	sorted := append([]*model.Session{}, v.sessions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartTime.After(sorted[j].StartTime) })

	var currentBucket string
	for _, s := range sorted {
		bucket := timeBucket(now, s.StartTime)
		if bucket != currentBucket {
			b.WriteByte('\n')
			fmt.Fprintln(&b, header.Render(bucket+" — "+s.StartTime.Format("2006-01-02")))
			currentBucket = bucket
		}
		icon := activityIcon(now.Sub(s.EndTime))
		costStr := "?"
		if v.pricing != nil {
			if c, ok := v.pricing.Cost(s); ok {
				costStr = fmt.Sprintf("$%.2f", c.USD)
			}
		}
		dir := s.ProjectDir
		if len(dir) > 50 {
			dir = "…" + dir[len(dir)-49:]
		}
		fmt.Fprintf(&b, "  %s %s ─●─  %s  (%d msg, %s, %s)  %s\n",
			s.StartTime.Local().Format("15:04"),
			icon,
			dir,
			s.MessageCount,
			fmtDuration(s.Duration()),
			costStr,
			ModelBadge(s.Model),
		)
	}

	rendered := lipgloss.NewStyle().Width(width).Render(b.String())
	lines := strings.Split(rendered, "\n")
	return scrollByOffset(lines, v.scroll, height)
}

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
)

// formatDenseRow renderiza uma linha de session no formato denso:
// 🟢 16:34  41m  S  1.2M  $4.32  ~/Desktop/...  preview
func formatDenseRow(s *model.Session, p *pricing.Pricing, now time.Time, maxWidth int) string {
	icon := activityIcon(now.Sub(s.EndTime))
	dur := fmtDuration(s.Duration())
	badge := ModelBadge(s.Model)
	tokens := fmtTokens(s.TotalTokens())
	cost := "?"
	if p != nil {
		if c, ok := p.Cost(s); ok {
			cost = fmt.Sprintf("$%.2f", c.USD)
		}
	}
	dirWidth := 24
	if maxWidth > 100 {
		dirWidth = 30
	}
	dir := truncatePath(s.ProjectDir, dirWidth)
	previewMax := maxWidth - 80
	if previewMax < 10 {
		previewMax = 20
	}
	preview := s.FirstUserMsg
	if len(preview) > previewMax {
		preview = preview[:previewMax-1] + "…"
	}
	return fmt.Sprintf("%s %s  %5s  %s  %6s  %7s  %-*s  %s",
		icon,
		s.EndTime.Local().Format("15:04"),
		dur,
		badge,
		tokens,
		cost,
		dirWidth, dir,
		preview,
	)
}

func fmtDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

func fmtTokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
}

func truncatePath(p string, max int) string {
	if len(p) <= max {
		return p
	}
	if max < 4 {
		return p[:max]
	}
	return "…" + p[len(p)-(max-1):]
}

// stripAnsi remove escape codes pra calcular largura visível.
func stripAnsi(s string) string {
	out := s
	for {
		i := strings.Index(out, "\x1b[")
		if i < 0 {
			return out
		}
		j := strings.IndexByte(out[i:], 'm')
		if j < 0 {
			return out
		}
		out = out[:i] + out[i+j+1:]
	}
}

package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/parser"
	"github.com/felipeness/nessy/internal/pricing"
	"github.com/felipeness/nessy/internal/stats"
)

// detailContext carrega tudo que detail panel precisa pra render rico.
type detailContext struct {
	allSessions []*model.Session
	pricing     *pricing.Pricing
	msgsCache   map[string][]parser.Message // sessionID → últimas N user msgs
}

func newDetailContext(all []*model.Session, p *pricing.Pricing) *detailContext {
	return &detailContext{allSessions: all, pricing: p, msgsCache: map[string][]parser.Message{}}
}

// renderDetail aceita paneWidth pra dimensionar bar charts/sparklines ao
// pane real do detail. Antes era hardcoded 50, deixando metade do pane vazio
// em terminais largos.
func (d *detailContext) renderDetail(s *model.Session, paneWidth int) string {
	if s == nil {
		return "(nenhuma session selecionada)"
	}
	// bars precisam caber em "label(13) + ' ' + bar + ' $X.XX (XX%)'(~13)".
	// Reserva 30 pra label + suffix; resto é bar. Clampa pra 20-80.
	barWidth := paneWidth - 30
	if barWidth < 20 {
		barWidth = 20
	}
	if barWidth > 80 {
		barWidth = 80
	}

	var b strings.Builder
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	// Header
	fmt.Fprintln(&b, headerStyle.Render(s.SessionID))
	fmt.Fprintf(&b, "%s\n", lipgloss.NewStyle().Foreground(colorMuted).Render(s.ProjectDir))
	fmt.Fprintf(&b, "Branch: %s · Modelo: %s %s\n",
		orDash(s.GitBranch), ModelBadge(s.Model), modelShort(s.Model))
	fmt.Fprintf(&b, "Início: %s · Duração: %s\n",
		s.StartTime.Local().Format("2006-01-02 15:04"), fmtDuration(s.Duration()))
	fmt.Fprintf(&b, "Msgs: %d (user: %d · assistant: %d)\n\n",
		s.MessageCount, s.UserMessages, s.AssistantMessages)

	// B2 — Custo breakdown
	if d.pricing != nil {
		if c, ok := d.pricing.Cost(s); ok {
			fmt.Fprintln(&b, headerStyle.Render("💰 Custo"))
			brl := ""
			if d.pricing.BRLRate > 0 {
				brl = fmt.Sprintf(" (R$ %.2f)", c.BRL)
			}
			fmt.Fprintf(&b, "Total: $%.2f USD%s\n", c.USD, brl)
			renderCostBar(&b, "Input        ", c.InputUSD, c.USD, barWidth)
			renderCostBar(&b, "Output       ", c.OutputUSD, c.USD, barWidth)
			renderCostBar(&b, "Cache create ", c.CacheCreationUSD, c.USD, barWidth)
			renderCostBar(&b, "Cache read   ", c.CacheReadUSD, c.USD, barWidth)
			b.WriteByte('\n')
		} else {
			fmt.Fprintf(&b, "Custo: ? (modelo %q sem entry no pricing.toml)\n\n", s.Model)
		}
	}

	// Tokens
	fmt.Fprintln(&b, headerStyle.Render("🔢 Tokens"))
	fmt.Fprintf(&b, "Input:    %s\n", fmtIntComma(s.InputTokens))
	fmt.Fprintf(&b, "Output:   %s\n", fmtIntComma(s.OutputTokens))
	fmt.Fprintf(&b, "Cache cr: %s\n", fmtIntComma(s.CacheCreationTokens))
	fmt.Fprintf(&b, "Cache rd: %s\n", fmtIntComma(s.CacheReadTokens))

	// B3 — Cache hit gauge
	cacheHit := 0.0
	if total := s.CacheReadTokens + s.InputTokens; total > 0 {
		cacheHit = float64(s.CacheReadTokens) / float64(total)
	}
	fmt.Fprintf(&b, "Cache hits: %s\n\n", Gauge(cacheHit, 20))

	// B7 — Mini-stats
	mins := s.Duration().Minutes()
	msgsPerMin := 0.0
	if mins > 0 {
		msgsPerMin = float64(s.UserMessages) / mins
	}
	tokensPerMsg := int64(0)
	if s.MessageCount > 0 {
		tokensPerMsg = s.TotalTokens() / int64(s.MessageCount)
	}
	ratio := 0.0
	if s.AssistantMessages > 0 {
		ratio = float64(s.UserMessages) / float64(s.AssistantMessages)
	}
	fmt.Fprintf(&b, "msgs/min: %.1f · tokens/msg: %s · u:a ratio: %.2f\n\n",
		msgsPerMin, fmtIntComma(tokensPerMsg), ratio)

	// B1 — Tools bar chart
	if len(s.ToolCalls) > 0 {
		fmt.Fprintln(&b, headerStyle.Render("🔧 Tools"))
		type kv struct {
			k string
			v int
		}
		var pairs []kv
		max := 0
		for k, v := range s.ToolCalls {
			pairs = append(pairs, kv{k, v})
			if v > max {
				max = v
			}
		}
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i].v != pairs[j].v {
				return pairs[i].v > pairs[j].v
			}
			return pairs[i].k < pairs[j].k
		})
		shown := pairs
		if len(shown) > 8 {
			shown = shown[:8]
		}
		for _, p := range shown {
			color := toolColor(p.k)
			label := fmt.Sprintf("%-15s", p.k)
			bar := BarChart(label, float64(p.v), float64(max), 18, color)
			fmt.Fprintf(&b, "%s %d\n", bar, p.v)
		}
		b.WriteByte('\n')
	}

	// B4 — Sparkline do projeto
	hist := stats.ProjectHistory(d.allSessions, s.ProjectDir, 14)
	totalSess := 0
	for _, c := range hist {
		totalSess += c
	}
	fmt.Fprintf(&b, "📊 Histórico do projeto (14d): %s (%d sessions)\n\n",
		Sparkline(hist), totalSess)

	// B5 — Comparação com baseline
	bl := stats.Baseline(d.allSessions, s.ProjectDir, d.pricing)
	if bl.Available {
		fmt.Fprintln(&b, headerStyle.Render("📐 vs mediana do projeto"))
		fmt.Fprintf(&b, "msgs:    %d (mediana %d) %s\n",
			s.MessageCount, bl.MsgsMedian, deltaArrow(float64(s.MessageCount), float64(bl.MsgsMedian)))
		if d.pricing != nil {
			if c, ok := d.pricing.Cost(s); ok && bl.CostMedian > 0 {
				fmt.Fprintf(&b, "custo:   $%.2f (mediana $%.2f) %s\n",
					c.USD, bl.CostMedian, deltaArrow(c.USD, bl.CostMedian))
			}
		}
		fmt.Fprintf(&b, "duração: %s (mediana %s) %s\n\n",
			fmtDuration(s.Duration()), fmtDuration(bl.DurMedian),
			deltaArrow(s.Duration().Seconds(), bl.DurMedian.Seconds()))
	}

	// B6 — Trecho últimas msgs
	if msgs, ok := d.msgsCache[s.SessionID]; ok && len(msgs) > 0 {
		renderLastMsgs(&b, msgs, headerStyle)
	} else if s.JSONLPath != "" {
		if msgs, err := parser.LastUserMessages(s.JSONLPath, 3); err == nil {
			d.msgsCache[s.SessionID] = msgs
			renderLastMsgs(&b, msgs, headerStyle)
		}
	}

	return b.String()
}

func renderCostBar(b *strings.Builder, label string, value, total float64, width int) {
	pct := 0.0
	if total > 0 {
		pct = value / total
	}
	bar := BarChart(label, value, total, width, lipgloss.Color("39"))
	fmt.Fprintf(b, "%s $%.2f (%.0f%%)\n", bar, value, pct*100)
}

func renderLastMsgs(b *strings.Builder, msgs []parser.Message, h lipgloss.Style) {
	fmt.Fprintln(b, h.Render("💬 Últimas mensagens"))
	for _, m := range msgs {
		text := m.Content
		if len(text) > 80 {
			text = text[:79] + "…"
		}
		fmt.Fprintf(b, "  %q\n", text)
	}
}

func deltaArrow(now, baseline float64) string {
	if baseline == 0 {
		return ""
	}
	delta := (now - baseline) / baseline * 100
	switch {
	case delta > 50:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(fmt.Sprintf("↑↑ +%.0f%%", delta))
	case delta > 10:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render(fmt.Sprintf("↑ +%.0f%%", delta))
	case delta < -50:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Render(fmt.Sprintf("↓↓ %.0f%%", delta))
	case delta < -10:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Render(fmt.Sprintf("↓ %.0f%%", delta))
	default:
		return lipgloss.NewStyle().Foreground(colorMuted).Render("=")
	}
}

func toolColor(name string) lipgloss.Color {
	switch name {
	case "Bash", "Task", "Skill":
		return lipgloss.Color("39") // azul (execution)
	case "Edit", "Write", "NotebookEdit":
		return lipgloss.Color("46") // verde (edit)
	case "Read", "Grep", "Glob", "ToolSearch", "WebFetch", "WebSearch":
		return lipgloss.Color("220") // amarelo (read)
	default:
		return colorMuted
	}
}

func modelShort(m string) string {
	if m == "" {
		return "?"
	}
	parts := strings.Split(m, "-")
	if len(parts) >= 3 {
		return strings.Join(parts[1:], "-") // sem prefixo "claude-"
	}
	return m
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func fmtIntComma(n int64) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

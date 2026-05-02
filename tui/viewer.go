package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/nessy/internal/parser"
	"github.com/felipeness/nessy/internal/sysutil"
	"github.com/felipeness/nessy/internal/viewer"
)

// viewerState é um modal fullscreen pra ler uma session em ledger format.
// Aberto com 'v' em qualquer tab que tenha cursor sobre session.
//
// Inspirado raine/claude-history. Comandos:
//
//	j/k    scroll 1 linha
//	d/u    half-page
//	g/G    top/bottom
//	t      cycle tool mode (Hidden→Truncated→Full→Hidden)
//	T      toggle thinking blocks
//	[/]    nav anterior/próxima entry (com gutter ▌)
//	y      yank entry focada como markdown
//	Y      yank file path
//	I      yank session UUID
//	p      mostra path no status (sem clipboard)
//	q/esc  fecha viewer
type viewerState struct {
	active       bool
	sessionID    string
	jsonlPath    string
	entries      []parser.LedgerEntry
	lines        []string // pre-rendered cache
	scroll       int
	maxScroll    int
	width        int
	height       int
	opts         viewer.Options
	lastErr      string
	statusMsg    string
	navMode      bool
	focusedEntry int
}

// Open carrega o JSONL em ledger format e ativa o modal.
func (v *viewerState) Open(jsonlPath, sessionID string, width, height int) {
	entries, err := parser.ParseLedger(jsonlPath)
	if err != nil {
		v.lastErr = "erro lendo session: " + err.Error()
		return
	}
	v.active = true
	v.sessionID = sessionID
	v.jsonlPath = jsonlPath
	v.entries = entries
	v.scroll = 0
	v.width = width
	v.height = height
	v.opts = viewer.DefaultOptions(width)
	v.opts.NavMode = false
	v.focusedEntry = 0
	v.rerender()
}

// Close fecha o viewer.
func (v *viewerState) Close() {
	v.active = false
	v.entries = nil
	v.lines = nil
	v.lastErr = ""
	v.statusMsg = ""
}

// rerender pre-computa as linhas com opts atual. Chamar depois de toggle.
func (v *viewerState) rerender() {
	if len(v.entries) == 0 {
		v.lines = []string{"(nenhum conteúdo)"}
		v.maxScroll = 0
		return
	}
	v.opts.HighlightLine = -1
	if v.navMode && v.focusedEntry < len(v.entries) {
		// HighlightLine no Render é por entry index — mas o output é multi-linha
		// por entry. Pra simplificar, render guarda em qual entry tá cada linha.
		// Truque: ative NavMode no opts, render decide gutter.
		v.opts.NavMode = true
		v.opts.HighlightLine = v.focusedEntry
	} else {
		v.opts.NavMode = false
	}
	v.lines = viewer.Render(v.entries, v.opts)
	v.maxScroll = len(v.lines) - (v.height - 4) // header + footer reserved
	if v.maxScroll < 0 {
		v.maxScroll = 0
	}
}

// HandleKey processa input. Devolve true se consumiu (não passa pra app).
func (v *viewerState) HandleKey(key string) (consumed bool, statusMsg string) {
	if !v.active {
		return false, ""
	}
	switch key {
	case "q", "esc":
		v.Close()
		return true, ""
	case "j", "down":
		v.scroll = clampInt(v.scroll+1, 0, v.maxScroll)
		return true, ""
	case "k", "up":
		v.scroll = clampInt(v.scroll-1, 0, v.maxScroll)
		return true, ""
	case "d":
		v.scroll = clampInt(v.scroll+v.height/2, 0, v.maxScroll)
		return true, ""
	case "u":
		v.scroll = clampInt(v.scroll-v.height/2, 0, v.maxScroll)
		return true, ""
	case "g", "home":
		v.scroll = 0
		return true, ""
	case "G", "end":
		v.scroll = v.maxScroll
		return true, ""
	case "t":
		v.opts.ToolMode = (v.opts.ToolMode + 1) % 3
		v.rerender()
		labels := []string{"Hidden", "Truncated", "Full"}
		return true, "tool mode: " + labels[v.opts.ToolMode]
	case "T":
		v.opts.ShowThinking = !v.opts.ShowThinking
		v.rerender()
		if v.opts.ShowThinking {
			return true, "thinking blocks: visíveis"
		}
		return true, "thinking blocks: ocultos"
	case "[", "K":
		v.navMode = true
		v.focusedEntry = clampInt(v.focusedEntry-1, 0, len(v.entries)-1)
		v.rerender()
		return true, fmt.Sprintf("entry %d/%d", v.focusedEntry+1, len(v.entries))
	case "]", "J":
		v.navMode = true
		v.focusedEntry = clampInt(v.focusedEntry+1, 0, len(v.entries)-1)
		v.rerender()
		return true, fmt.Sprintf("entry %d/%d", v.focusedEntry+1, len(v.entries))
	case "y":
		// Yank focused entry as markdown
		if v.navMode && v.focusedEntry < len(v.entries) {
			e := v.entries[v.focusedEntry]
			md := entryToMarkdown(e)
			if err := sysutil.Clipboard(md); err != nil {
				return true, "yank err: " + err.Error()
			}
			return true, "✓ entry copiada (md)"
		}
		// Sem nav: yank conversa toda
		var b strings.Builder
		for _, e := range v.entries {
			b.WriteString(entryToMarkdown(e))
			b.WriteString("\n\n")
		}
		if err := sysutil.Clipboard(b.String()); err != nil {
			return true, "yank err: " + err.Error()
		}
		return true, "✓ conversa toda copiada (md)"
	case "Y":
		if err := sysutil.Clipboard(v.jsonlPath); err != nil {
			return true, "yank err: " + err.Error()
		}
		return true, "✓ path copiado"
	case "I":
		if err := sysutil.Clipboard(v.sessionID); err != nil {
			return true, "yank err: " + err.Error()
		}
		return true, "✓ session id copiado"
	case "p":
		return true, v.jsonlPath
	}
	return false, ""
}

// View renderiza o modal completo (header + body + footer).
func (v viewerState) View(width, height int) string {
	if !v.active {
		return ""
	}
	if v.lastErr != "" {
		return lipgloss.NewStyle().Foreground(colorCrit).Padding(2).Render(v.lastErr)
	}
	header := lipgloss.NewStyle().
		Background(lipgloss.Color("#1f2937")).
		Foreground(colorAccent).Bold(true).Padding(0, 1).
		Render(fmt.Sprintf(" 📖 Viewer · %s · %d entries ", v.sessionID[:8], len(v.entries)))
	footer := lipgloss.NewStyle().
		Foreground(colorMuted).Padding(0, 1).
		Render(footerText(v.opts, v.navMode))

	bodyHeight := height - 3
	if bodyHeight < 5 {
		bodyHeight = 5
	}
	end := v.scroll + bodyHeight
	if end > len(v.lines) {
		end = len(v.lines)
	}
	start := v.scroll
	if start > end {
		start = end
	}
	body := strings.Join(v.lines[start:end], "\n")

	statusLine := ""
	if v.statusMsg != "" {
		statusLine = lipgloss.NewStyle().Foreground(colorAccent).Render(v.statusMsg)
	}
	out := header + "\n" + body
	if statusLine != "" {
		out += "\n" + statusLine
	}
	out += "\n" + footer
	return lipgloss.NewStyle().Width(width).MaxHeight(height).Render(out)
}

func footerText(opts viewer.Options, nav bool) string {
	tool := []string{"hidden", "truncated", "full"}[opts.ToolMode]
	thinking := "off"
	if opts.ShowThinking {
		thinking = "on"
	}
	navIcon := ""
	if nav {
		navIcon = " [nav]"
	}
	return fmt.Sprintf(
		"[j/k] scroll  [d/u] page  [g/G] top/bot  [t]ool=%s  [T]hink=%s  [/]nav  [y]ank  [Y]path  [I]d  [q]uit%s",
		tool, thinking, navIcon)
}

// entryToMarkdown serializa uma entry pra clipboard friendly.
func entryToMarkdown(e parser.LedgerEntry) string {
	when := ""
	if !e.Timestamp.IsZero() {
		when = e.Timestamp.Local().Format("2006-01-02 15:04:05")
	}
	role := e.Role
	if e.Role == "tool_use" {
		role = "tool: " + e.ToolName
	}
	header := fmt.Sprintf("**[%s] %s**", when, role)
	body := e.Text
	if e.Role == "tool_use" {
		body = "```json\n" + e.ToolInput + "\n```"
	}
	return header + "\n\n" + body
}

func clampInt(v, lo, hi int) int {
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

// Package viewer renderiza sessions em "ledger format" — uma tabela
// HH:MM | Role(9) | Body com tool blocks colapsáveis.
//
// Inspirado no raine/claude-history (Rust), com 3 melhorias:
//
//  1. Tool blocks sempre coloridos por nome (Bash=ciano, Edit=verde, etc),
//     não só dim
//  2. Sidechain blocks têm marker `↳N` no role label (não só dim)
//  3. Modo "Auto" pra tool display: full em tool_use, truncated em
//     tool_result com >10 linhas
//
// Pure functions — não depende de bubble tea, retorna strings com ANSI
// codes via lipgloss.
package viewer

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/nessy/internal/parser"
)

const (
	timestampWidth = 7 // " HH:MM "
	nameWidth      = 9 // role label right-padded
	truncatedLines = 6 // tool blocks no modo truncated
)

// ToolMode controla como tool_use/tool_result são renderizados.
type ToolMode int

const (
	// ToolHidden: oculta tool blocks completamente.
	ToolHidden ToolMode = iota
	// ToolTruncated: mostra header + primeiras linhas (default).
	ToolTruncated
	// ToolFull: mostra integralmente.
	ToolFull
)

// Options controla o render.
type Options struct {
	ToolMode      ToolMode
	ShowThinking  bool
	HighlightLine int  // index da entry focada (-1 = nenhuma); usa gutter accent
	NavMode       bool // true = mostra gutter
	Width         int  // largura disponível
	IsLight       bool // theme detection (futuro)
}

// Default Options pra "primeira abertura".
func DefaultOptions(width int) Options {
	return Options{
		ToolMode:      ToolTruncated,
		ShowThinking:  false,
		HighlightLine: -1,
		Width:         width,
	}
}

// Render gera as linhas pro viewer. Cada string é uma linha já formatada
// com ANSI. Caller só precisa join("\n").
func Render(entries []parser.LedgerEntry, opts Options) []string {
	var out []string
	if opts.Width < 40 {
		opts.Width = 80
	}
	bodyWidth := opts.Width - timestampWidth - 3 - nameWidth - 3 // separators
	if bodyWidth < 20 {
		bodyWidth = 20
	}

	for i, e := range entries {
		if e.Role == "thinking" && !opts.ShowThinking {
			continue
		}
		if (e.Role == "tool_use" || e.Role == "tool_result") && opts.ToolMode == ToolHidden {
			continue
		}

		gutter := "  "
		if opts.NavMode {
			if i == opts.HighlightLine {
				gutter = lipgloss.NewStyle().Foreground(lipgloss.Color("#7dd3fc")).Render("▌ ")
			} else {
				gutter = "  "
			}
		}

		ts := formatTimestamp(e)
		role := formatRole(e)

		// Body é multi-linha — primeira linha tem ts+role, restantes têm padding
		body := bodyFor(e, opts, bodyWidth)
		if body == "" {
			continue
		}
		bodyLines := strings.Split(body, "\n")
		for li, ln := range bodyLines {
			var prefix string
			if li == 0 {
				prefix = gutter + ts + sep() + role + sep()
			} else {
				prefix = gutter + strings.Repeat(" ", timestampWidth) + sep() +
					strings.Repeat(" ", nameWidth) + sep()
			}
			out = append(out, prefix+ln)
		}
		// Adiciona linha em branco entre entries pra respiro visual
		if i < len(entries)-1 {
			out = append(out, "")
		}
	}
	return out
}

// formatTimestamp devolve "HH:MM " (7 chars total — 5 + 2 spaces).
func formatTimestamp(t parser.LedgerEntry) string {
	if t.Timestamp.IsZero() {
		return strings.Repeat(" ", timestampWidth)
	}
	s := t.Timestamp.Local().Format(" 15:04 ")
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af")).Render(s)
}

// formatRole devolve role label colorido + padded.
func formatRole(e parser.LedgerEntry) string {
	var label string
	var color lipgloss.Color
	switch e.Role {
	case "user":
		label = "You"
		color = "#fbbf24"
	case "assistant":
		label = "Claude"
		color = "#7dd3fc"
	case "tool_use":
		label = e.ToolName
		color = toolColor(e.ToolName)
	case "tool_result":
		label = "↳ Result"
		color = "#a78bfa"
	case "thinking":
		label = "thinking"
		color = "#6b7280"
	default:
		label = e.Role
		color = "#9ca3af"
	}
	if e.IsSidechain {
		label = "↳ " + label
	}
	if len(label) > nameWidth {
		label = label[:nameWidth]
	}
	padded := fmt.Sprintf("%*s", nameWidth, label) // right-align
	return lipgloss.NewStyle().Foreground(color).Bold(true).Render(padded)
}

func toolColor(name string) lipgloss.Color {
	switch name {
	case "Bash":
		return "#7dd3fc" // cyan
	case "Edit", "MultiEdit":
		return "#34d399" // green
	case "Write":
		return "#fbbf24" // amber
	case "Read":
		return "#60a5fa" // blue
	case "Task", "Agent":
		return "#a78bfa" // purple
	case "Grep", "Glob":
		return "#f87171" // red
	case "WebFetch", "WebSearch":
		return "#f472b6" // pink
	default:
		return "#cbd5e1" // light gray
	}
}

func sep() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#374151")).Render(" │ ")
}

// bodyFor gera o conteúdo da entry, aplicando ToolMode quando relevante.
func bodyFor(e parser.LedgerEntry, opts Options, width int) string {
	switch e.Role {
	case "user":
		return wrap(e.Text, width)
	case "assistant":
		return wrap(e.Text, width)
	case "thinking":
		t := wrap(e.Text, width)
		return lipgloss.NewStyle().Italic(true).Faint(true).Render(t)
	case "tool_use":
		return formatToolUse(e, opts.ToolMode, width)
	case "tool_result":
		return formatToolResult(e, opts.ToolMode, width)
	}
	return ""
}

func formatToolUse(e parser.LedgerEntry, mode ToolMode, width int) string {
	if mode == ToolHidden {
		return ""
	}
	input := e.ToolInput
	if input == "" {
		return lipgloss.NewStyle().Faint(true).Render("(no input)")
	}
	// Pretty-print: se for JSON, indenta. Se não, usa raw.
	pretty := prettyJSON(input)
	if mode == ToolTruncated {
		pretty = truncateLines(pretty, truncatedLines)
	}
	return wrap(pretty, width)
}

func formatToolResult(e parser.LedgerEntry, mode ToolMode, width int) string {
	if mode == ToolHidden {
		return ""
	}
	text := e.Text
	if mode == ToolTruncated {
		text = truncateLines(text, truncatedLines)
	}
	return wrap(text, width)
}

// truncateLines mostra primeiras N linhas + sumário "(... X more lines)".
func truncateLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	more := len(lines) - n
	head := strings.Join(lines[:n], "\n")
	footer := lipgloss.NewStyle().Faint(true).
		Render(fmt.Sprintf("(... %d more lines, press 't' to expand)", more))
	return head + "\n" + footer
}

// wrap quebra string em width preservando palavras quando possível.
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if lipgloss.Width(line) <= width {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		// quebra simples por chars (preservar palavras é mais complexo,
		// fazer simples agora — TODO: word-aware)
		runes := []rune(line)
		for i := 0; i < len(runes); i += width {
			end := i + width
			if end > len(runes) {
				end = len(runes)
			}
			out.WriteString(string(runes[i:end]))
			out.WriteByte('\n')
		}
	}
	return strings.TrimRight(out.String(), "\n")
}

// prettyJSON tenta parsear como JSON e re-encodar com indent. Se falhar,
// devolve raw.
func prettyJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || (s[0] != '{' && s[0] != '[') {
		return s
	}
	// Tenta indentar via json.RawMessage roundtrip — barato.
	var v any
	if err := jsonUnmarshal(s, &v); err != nil {
		return s
	}
	out, err := jsonMarshalIndent(v, "  ")
	if err != nil {
		return s
	}
	return string(out)
}

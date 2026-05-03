package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Helpers de UI compartilhados entre tabs — pílulas, barras, separadores.
// Mantém estilo coerente em toda TUI sem repetir lipgloss.NewStyle() inline.

// branchColor escolhe uma cor baseada no prefix da branch (feat/, fix/,
// chore/, etc). Usado no Threads tab pras pílulas + lanes do graph DAG.
func branchColor(branch string) lipgloss.Color {
	prefix := branchPrefix(branch)
	switch prefix {
	case "feat":
		return lipgloss.Color("#7dd3fc") // cyan
	case "fix":
		return lipgloss.Color("#f87171") // red
	case "chore":
		return lipgloss.Color("#9ca3af") // gray
	case "refactor":
		return lipgloss.Color("#a78bfa") // purple
	case "docs":
		return lipgloss.Color("#34d399") // green
	case "perf":
		return lipgloss.Color("#fbbf24") // amber
	case "test":
		return lipgloss.Color("#60a5fa") // blue
	}
	if branch == "main" || branch == "master" {
		return lipgloss.Color("#fbbf24") // gold
	}
	return lipgloss.Color("#cbd5e1") // light gray default
}

func branchPrefix(branch string) string {
	if i := strings.IndexByte(branch, '/'); i > 0 {
		return branch[:i]
	}
	if i := strings.IndexByte(branch, '-'); i > 0 {
		return branch[:i]
	}
	return branch
}

// branchPill renderiza uma pílula colorida com a branch. Usa fg da
// branchColor e bg cinza-escuro pra contraste sutil.
func branchPill(branch string) string {
	if branch == "" {
		branch = "(no branch)"
	}
	style := lipgloss.NewStyle().
		Foreground(branchColor(branch)).
		Bold(true).
		Padding(0, 1)
	return style.Render(branch)
}

// dot devolve um símbolo colorido por kind: ● normal · ◉ compact · ●(red) crit.
func threadDot(kind, status string) string {
	switch kind {
	case "compact":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24")).Render("◉")
	default:
		col := lipgloss.Color("#34d399") // green default
		if status == "error" {
			col = lipgloss.Color("#f87171")
		}
		return lipgloss.NewStyle().Foreground(col).Render("●")
	}
}

// breadcrumb renderiza "a › b › c" com separadores accent.
func breadcrumb(items ...string) string {
	sep := lipgloss.NewStyle().Foreground(colorAccent).Render(" › ")
	main := lipgloss.NewStyle().Foreground(colorMuted).Render
	parts := make([]string, len(items))
	for i, s := range items {
		if i == len(items)-1 {
			parts[i] = lipgloss.NewStyle().Foreground(colorFg).Bold(true).Render(s)
		} else {
			parts[i] = main(s)
		}
	}
	return strings.Join(parts, sep)
}

// shortPath substitui $HOME por ~ e trunca prefix se passar de maxLen.
func shortPath(p, home string) string {
	if home != "" && strings.HasPrefix(p, home) {
		p = "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// truncRight trunca string à direita com elipse se passar de n.
func truncRight(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 2 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// scrollWindow devolve uma janela de `lines` com altura `height` que mantém
// `cursorLine` visível. Quando o cursor passa do meio da janela, faz scroll
// pra cima; quando chega no fim, clampa no último frame.
//
// Sem essa função, listas longas (recent/search/threads tree) ficam com o
// cursor invisível ao passar do número de linhas que cabem na tela.
func scrollWindow(lines []string, cursorLine, height int) string {
	if height <= 0 || len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	start := cursorLine - height/2
	if start < 0 {
		start = 0
	}
	if start+height > len(lines) {
		start = len(lines) - height
	}
	return strings.Join(lines[start:start+height], "\n")
}

// scrollByOffset corta `lines` numa janela [offset, offset+height], clampando
// offset em [0, len(lines)-height]. Diferente de scrollWindow (que centraliza
// um cursor), esse usa offset explicito — pra views sem cursor onde ↑↓ rola
// o body inteiro (behavior/costs/ai/etc).
func scrollByOffset(lines []string, offset, height int) string {
	if height <= 0 || len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	if offset < 0 {
		offset = 0
	}
	maxOffset := len(lines) - height
	if offset > maxOffset {
		offset = maxOffset
	}
	return strings.Join(lines[offset:offset+height], "\n")
}

// clampScrollOffset normaliza um offset para a faixa valida dado total/height.
func clampScrollOffset(offset, total, height int) int {
	if offset < 0 {
		return 0
	}
	max := total - height
	if max < 0 {
		max = 0
	}
	if offset > max {
		return max
	}
	return offset
}

// padLinesToWidth garante que CADA linha tem exatamente `width` celulas
// visiveis, preenchendo com espacos. Sem isso, lipgloss.Width() so estende
// linhas mais curtas que a maior linha, deixando o resto do row vazio — em
// transicoes de layout (split <-> fullwidth), o frame anterior leak nas
// celulas a direita do conteudo curto. (Ghost render de viewStrip quando
// user pressiona V em sequencia.)
func padLinesToWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w < width {
			lines[i] = line + strings.Repeat(" ", width-w)
		}
	}
	return strings.Join(lines, "\n")
}

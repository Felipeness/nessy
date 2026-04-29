package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorSonnet = lipgloss.Color("39")  // azul
	colorOpus   = lipgloss.Color("129") // roxo
	colorHaiku  = lipgloss.Color("46")  // verde
	colorUnknown = lipgloss.Color("241") // cinza
)

// ModelBadge retorna 1-letter colorido (S/O/H/?) baseado no nome do modelo.
func ModelBadge(model string) string {
	letter, color := badgeOf(model)
	return lipgloss.NewStyle().Foreground(color).Bold(true).Render(letter)
}

// ModelColor retorna a cor associada ao modelo (pra reuso fora do badge).
func ModelColor(model string) lipgloss.Color {
	_, color := badgeOf(model)
	return color
}

func badgeOf(model string) (string, lipgloss.Color) {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "sonnet"):
		return "S", colorSonnet
	case strings.Contains(m, "opus"):
		return "O", colorOpus
	case strings.Contains(m, "haiku"):
		return "H", colorHaiku
	default:
		return "?", colorUnknown
	}
}

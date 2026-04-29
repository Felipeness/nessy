package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent = lipgloss.Color("39")
	colorMuted  = lipgloss.Color("241")
)

var (
	tabBarStyle      = lipgloss.NewStyle().Padding(0, 1)
	tabActiveStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Underline(true).Padding(0, 1)
	tabInactiveStyle = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
	statusBarStyle   = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Padding(0, 1)
)

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	barFilled  = '█'
	barEmpty   = '░'
	heatChars  = " ·▁▂▃▄▅▆▇█"
	sparkChars = "▁▂▃▄▅▆▇█"
)

// BarChart renderiza uma barra horizontal estilo "label   ████░░  42 (61%)".
func BarChart(label string, value, max float64, width int, color lipgloss.Color) string {
	if width < 5 {
		width = 5
	}
	if max <= 0 {
		max = 1
	}
	ratio := value / max
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	bar := strings.Repeat(string(barFilled), filled) + strings.Repeat(string(barEmpty), width-filled)
	colored := lipgloss.NewStyle().Foreground(color).Render(bar)
	return label + " " + colored
}

// Gauge renderiza percentual 0-1 como barra visual `████░░░░░░ 42%`.
func Gauge(value float64, width int) string {
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	filled := int(value * float64(width))
	bar := strings.Repeat(string(barFilled), filled) + strings.Repeat(string(barEmpty), width-filled)
	pct := int(value * 100)
	color := lipgloss.Color("46") // green default
	switch {
	case value < 0.3:
		color = lipgloss.Color("196") // red
	case value < 0.6:
		color = lipgloss.Color("220") // yellow
	}
	return lipgloss.NewStyle().Foreground(color).Render(bar) + " " + lipgloss.NewStyle().Foreground(colorMuted).Render(itoa(pct)+"%")
}

// Sparkline produz string de 8 níveis representando a série numérica.
func Sparkline(values []int) string {
	if len(values) == 0 {
		return ""
	}
	max := 1
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	var b strings.Builder
	for _, v := range values {
		idx := v * (len(sparkChars) - 1) / max
		b.WriteByte(sparkChars[idx])
	}
	return b.String()
}

// Heatmap renderiza grid 2D de intensidades (0..max) com 10 níveis.
func Heatmap(grid [][]int) string {
	if len(grid) == 0 {
		return ""
	}
	max := 1
	for _, row := range grid {
		for _, v := range row {
			if v > max {
				max = v
			}
		}
	}
	var b strings.Builder
	for _, row := range grid {
		for _, v := range row {
			idx := v * (len(heatChars) - 1) / max
			b.WriteByte(heatChars[idx])
			b.WriteByte(' ')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// itoa pequeno pra evitar import de strconv pesado em utility.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

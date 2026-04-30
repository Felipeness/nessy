package statusline

import "fmt"

// Reset, Bold, BoldOff são escape codes ANSI básicos.
const (
	Reset   = "\x1b[0m"
	Bold    = "\x1b[1m"
	BoldOff = "\x1b[22m"
	Dim     = "\x1b[2m"
)

// Color é uma cor RGB 0-255. Resolvida em runtime pra truecolor ou 256.
type Color struct {
	R, G, B uint8
}

// Hex parseia "#RRGGBB" → Color (panic em malformed; pra constantes em theme.go).
func Hex(s string) Color {
	if len(s) == 7 && s[0] == '#' {
		s = s[1:]
	}
	if len(s) != 6 {
		return Color{}
	}
	var r, g, b uint8
	fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	return Color{r, g, b}
}

// Empty retorna true pra Color{0,0,0} (tratamos como "transparente"/sem cor).
func (c Color) Empty() bool { return c == Color{} }

// FG retorna o escape ANSI truecolor pra foreground.
func (c Color) FG() string {
	if c.Empty() {
		return ""
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B)
}

// BG retorna o escape ANSI truecolor pra background.
func (c Color) BG() string {
	if c.Empty() {
		return ""
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.R, c.G, c.B)
}

// Severity classifica um valor em ok/warn/crit. Usado nos components.
type Severity int

const (
	SevOK Severity = iota
	SevWarn
	SevCrit
)

// Classify devolve a severidade dado os thresholds em ComponentOpts.
// Se warn==0 e crit==0, retorna sempre OK.
func Classify(value, warn, crit float64) Severity {
	if warn == 0 && crit == 0 {
		return SevOK
	}
	if crit > 0 && value >= crit {
		return SevCrit
	}
	if warn > 0 && value >= warn {
		return SevWarn
	}
	return SevOK
}

package statusline

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// AnsiToHTML converte uma string com escape codes ANSI em HTML com <span>s.
// Suporta truecolor (38;2;r;g;b / 48;2;r;g;b), 256-color (38;5;n / 48;5;n),
// reset (0), bold (1), bold-off (22), dim (2). Saída pronta pra inserir
// num <pre> ou <code> com font-family: monospace.
//
// Mantemos a conversão em Go (mesmo código que renderiza pra terminal) pra
// evitar duplicar a lib ansi_up no frontend — single source of truth.
func AnsiToHTML(ansi string) string {
	var b strings.Builder
	open := false
	current := spanState{}

	for {
		idx := strings.Index(ansi, "\x1b[")
		if idx < 0 {
			if open {
				b.WriteString(html.EscapeString(ansi))
				b.WriteString("</span>")
			} else if ansi != "" {
				b.WriteString(html.EscapeString(ansi))
			}
			break
		}
		// flush text antes do escape
		if idx > 0 {
			b.WriteString(html.EscapeString(ansi[:idx]))
		}
		// parse "\x1b[ ... m"
		end := strings.IndexByte(ansi[idx+2:], 'm')
		if end < 0 {
			break // malformed, abort
		}
		code := ansi[idx+2 : idx+2+end]
		ansi = ansi[idx+2+end+1:]

		next := applyAnsiCodes(current, code)
		// reset → fecha span
		if next.isReset() {
			if open {
				b.WriteString("</span>")
				open = false
			}
			current = spanState{}
			continue
		}
		if open {
			b.WriteString("</span>")
		}
		b.WriteString(next.openTag())
		current = next
		open = true
	}
	return b.String()
}

// spanState carrega cor/atributos correntes pra construir <span style="...">.
type spanState struct {
	fg     string // "rgb(r,g,b)" ou ""
	bg     string
	bold   bool
	dim    bool
	reset  bool // true = só recebemos "0", próximo passo deve fechar
}

func (s spanState) isReset() bool {
	return s.reset && s.fg == "" && s.bg == "" && !s.bold && !s.dim
}

func (s spanState) openTag() string {
	var styles []string
	if s.fg != "" {
		styles = append(styles, "color:"+s.fg)
	}
	if s.bg != "" {
		styles = append(styles, "background:"+s.bg)
	}
	if s.bold {
		styles = append(styles, "font-weight:bold")
	}
	if s.dim {
		styles = append(styles, "opacity:0.6")
	}
	if len(styles) == 0 {
		return "<span>"
	}
	return `<span style="` + strings.Join(styles, ";") + `">`
}

// applyAnsiCodes parseia "38;2;r;g;b" / "48;5;n" / "0" / "1" / "22" sobre
// o estado anterior. Múltiplos codes separados por ";" são aplicados em ordem.
func applyAnsiCodes(prev spanState, code string) spanState {
	if code == "" || code == "0" {
		return spanState{reset: true}
	}
	out := prev
	out.reset = false
	parts := strings.Split(code, ";")
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			continue
		}
		switch n {
		case 0:
			out = spanState{reset: true}
		case 1:
			out.bold = true
		case 2:
			out.dim = true
		case 22:
			out.bold = false
			out.dim = false
		case 38:
			// foreground — consome 2 mais (truecolor) ou 1 (256)
			if i+1 < len(parts) {
				mode, _ := strconv.Atoi(parts[i+1])
				if mode == 2 && i+4 < len(parts) {
					r, _ := strconv.Atoi(parts[i+2])
					g, _ := strconv.Atoi(parts[i+3])
					bl, _ := strconv.Atoi(parts[i+4])
					out.fg = fmt.Sprintf("rgb(%d,%d,%d)", r, g, bl)
					i += 4
				} else if mode == 5 && i+2 < len(parts) {
					n2, _ := strconv.Atoi(parts[i+2])
					out.fg = ansi256ToRGB(n2)
					i += 2
				}
			}
		case 48:
			if i+1 < len(parts) {
				mode, _ := strconv.Atoi(parts[i+1])
				if mode == 2 && i+4 < len(parts) {
					r, _ := strconv.Atoi(parts[i+2])
					g, _ := strconv.Atoi(parts[i+3])
					bl, _ := strconv.Atoi(parts[i+4])
					out.bg = fmt.Sprintf("rgb(%d,%d,%d)", r, g, bl)
					i += 4
				} else if mode == 5 && i+2 < len(parts) {
					n2, _ := strconv.Atoi(parts[i+2])
					out.bg = ansi256ToRGB(n2)
					i += 2
				}
			}
		case 39:
			out.fg = ""
		case 49:
			out.bg = ""
		}
	}
	return out
}

// ansi256ToRGB converte código 256 (0-255) em "rgb(r,g,b)".
// 0-15 = cores base, 16-231 = cubo 6×6×6, 232-255 = grayscale.
var basic16 = [16][3]int{
	{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
	{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
	{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

func ansi256ToRGB(n int) string {
	if n < 0 || n > 255 {
		return "rgb(0,0,0)"
	}
	if n < 16 {
		c := basic16[n]
		return fmt.Sprintf("rgb(%d,%d,%d)", c[0], c[1], c[2])
	}
	if n < 232 {
		// 6×6×6 cube: levels = [0, 95, 135, 175, 215, 255]
		levels := []int{0, 95, 135, 175, 215, 255}
		idx := n - 16
		r := levels[idx/36]
		g := levels[(idx%36)/6]
		bl := levels[idx%6]
		return fmt.Sprintf("rgb(%d,%d,%d)", r, g, bl)
	}
	// grayscale
	gray := 8 + (n-232)*10
	return fmt.Sprintf("rgb(%d,%d,%d)", gray, gray, gray)
}

// stripUnused é um marker pra evitar warning de import não-usado em dev.
// Removemos quando todos os imports forem usados de fato.
var _ = regexp.MustCompile

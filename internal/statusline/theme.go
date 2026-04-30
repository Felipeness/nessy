package statusline

// Theme define as cores que cada component recebe. SegmentColors mapeia
// component name → BG/FG. Status colors são pros indicadores ok/warn/crit.
type Theme struct {
	Name    string
	Default ThemeSegment            // fallback pra components sem entry
	Segs    map[string]ThemeSegment // por component name
	Status  StatusColors            // 3-tier: ok/warn/crit
	Muted   Color                   // pra texto secundário/dim
}

type ThemeSegment struct {
	BG Color
	FG Color
}

type StatusColors struct {
	OK   Color
	Warn Color
	Crit Color
}

// SegOf devolve a entrada do theme pra um component, com fallback no Default.
func (t *Theme) SegOf(name string) ThemeSegment {
	if s, ok := t.Segs[name]; ok {
		return s
	}
	return t.Default
}

// SeverityFG mapeia severity → cor de foreground apropriada.
func (t *Theme) SeverityFG(s Severity) Color {
	switch s {
	case SevWarn:
		return t.Status.Warn
	case SevCrit:
		return t.Status.Crit
	default:
		return t.Status.OK
	}
}

// Themes registry.
var Themes = map[string]*Theme{
	"graphite": graphiteTheme(),
	"nord":     nordTheme(),
	"dracula":  draculaTheme(),
	"sakura":   sakuraTheme(),
	"mono":     monoTheme(),
}

// ThemeNames em ordem fixa pra UI (não-aleatória como mapa).
var ThemeNames = []string{"graphite", "nord", "dracula", "sakura", "mono"}

// StyleNames em ordem fixa pra UI.
var StyleNames = []string{"plain", "powerline", "capsule"}

// GetTheme com fallback graphite.
func GetTheme(name string) *Theme {
	if t, ok := Themes[name]; ok {
		return t
	}
	return Themes["graphite"]
}

func graphiteTheme() *Theme {
	return &Theme{
		Name:    "graphite",
		Default: ThemeSegment{BG: Hex("#2d2d2d"), FG: Hex("#cccccc")},
		Segs: map[string]ThemeSegment{
			"cwd":          {BG: Hex("#3d3d3d"), FG: Hex("#fafafa")},
			"git":          {BG: Hex("#4a4a4a"), FG: Hex("#7dd3fc")},
			"model":        {BG: Hex("#2d2d2d"), FG: Hex("#a5b4fc")},
			"context_pct":  {BG: Hex("#2d2d2d"), FG: Hex("#cccccc")},
			"cost_session": {BG: Hex("#2d2d2d"), FG: Hex("#fbbf24")},
			"burn_rate":    {BG: Hex("#2d2d2d"), FG: Hex("#cccccc")},
			"rate_5h":      {BG: Hex("#2d2d2d"), FG: Hex("#cccccc")},
			"rate_7d":      {BG: Hex("#2d2d2d"), FG: Hex("#cccccc")},
			"ticket":       {BG: Hex("#2d2d2d"), FG: Hex("#34d399")},
			"cluster":      {BG: Hex("#2d2d2d"), FG: Hex("#c4b5fd")},
			"vim_mode":     {BG: Hex("#2d2d2d"), FG: Hex("#fbbf24")},
		},
		Status: StatusColors{
			OK:   Hex("#4ade80"),
			Warn: Hex("#fbbf24"),
			Crit: Hex("#f87171"),
		},
		Muted: Hex("#737373"),
	}
}

func nordTheme() *Theme {
	return &Theme{
		Name:    "nord",
		Default: ThemeSegment{BG: Hex("#2e3440"), FG: Hex("#d8dee9")},
		Segs: map[string]ThemeSegment{
			"cwd":          {BG: Hex("#3b4252"), FG: Hex("#eceff4")},
			"git":          {BG: Hex("#434c5e"), FG: Hex("#88c0d0")},
			"model":        {BG: Hex("#2e3440"), FG: Hex("#81a1c1")},
			"cost_session": {BG: Hex("#2e3440"), FG: Hex("#ebcb8b")},
		},
		Status: StatusColors{
			OK:   Hex("#a3be8c"),
			Warn: Hex("#ebcb8b"),
			Crit: Hex("#bf616a"),
		},
		Muted: Hex("#4c566a"),
	}
}

func draculaTheme() *Theme {
	return &Theme{
		Name:    "dracula",
		Default: ThemeSegment{BG: Hex("#282a36"), FG: Hex("#f8f8f2")},
		Segs: map[string]ThemeSegment{
			"cwd":          {BG: Hex("#44475a"), FG: Hex("#f8f8f2")},
			"git":          {BG: Hex("#6272a4"), FG: Hex("#8be9fd")},
			"model":        {BG: Hex("#282a36"), FG: Hex("#bd93f9")},
			"cost_session": {BG: Hex("#282a36"), FG: Hex("#ffb86c")},
		},
		Status: StatusColors{
			OK:   Hex("#50fa7b"),
			Warn: Hex("#f1fa8c"),
			Crit: Hex("#ff5555"),
		},
		Muted: Hex("#6272a4"),
	}
}

func sakuraTheme() *Theme {
	return &Theme{
		Name:    "sakura",
		Default: ThemeSegment{BG: Hex("#fdf2f8"), FG: Hex("#831843")},
		Segs: map[string]ThemeSegment{
			"cwd":          {BG: Hex("#fce7f3"), FG: Hex("#831843")},
			"git":          {BG: Hex("#fbcfe8"), FG: Hex("#9d174d")},
			"model":        {BG: Hex("#fdf2f8"), FG: Hex("#9333ea")},
			"cost_session": {BG: Hex("#fdf2f8"), FG: Hex("#c2410c")},
		},
		Status: StatusColors{
			OK:   Hex("#16a34a"),
			Warn: Hex("#d97706"),
			Crit: Hex("#dc2626"),
		},
		Muted: Hex("#a78bfa"),
	}
}

func monoTheme() *Theme {
	return &Theme{
		Name:    "mono",
		Default: ThemeSegment{BG: Color{}, FG: Hex("#cccccc")},
		Segs: map[string]ThemeSegment{
			"cwd":          {BG: Color{}, FG: Hex("#ffffff")},
			"git":          {BG: Color{}, FG: Hex("#999999")},
			"model":        {BG: Color{}, FG: Hex("#aaaaaa")},
			"cost_session": {BG: Color{}, FG: Hex("#dddddd")},
		},
		Status: StatusColors{
			OK:   Hex("#cccccc"),
			Warn: Hex("#eeeeee"),
			Crit: Hex("#ffffff"),
		},
		Muted: Hex("#666666"),
	}
}

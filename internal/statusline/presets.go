package statusline

// Presets são configs prontas pra users escolherem no install.
var Presets = map[string]*Config{
	"compact":   compactPreset(),
	"max":       maxPreset(),
	"powerline": powerlinePreset(),
}

// PresetNames lista os nomes (ordem fixa pra UI/help).
var PresetNames = []string{"compact", "max", "powerline"}

func compactPreset() *Config {
	c := DefaultConfig()
	c.Style = "plain"
	c.Lines = []Line{
		{
			Components: []string{"cwd", "git", "model", "context_pct", "cost_session"},
			Separator:  " │ ",
		},
	}
	return c
}

func maxPreset() *Config {
	c := DefaultConfig()
	c.Style = "plain"
	c.Lines = []Line{
		{
			Components: []string{"cwd", "git", "ticket", "model", "context_pct", "cost_session", "burn_rate", "rate_5h"},
			Separator:  " │ ",
		},
		{
			Components: []string{"cost_today", "cost_month", "lines_changed", "cluster", "time"},
			Separator:  " · ",
		},
	}
	return c
}

func powerlinePreset() *Config {
	c := DefaultConfig()
	c.Style = "powerline"
	c.Theme = "graphite"
	c.Lines = []Line{
		{
			Components: []string{"cwd", "git", "model", "context_pct", "cost_session"},
		},
	}
	return c
}

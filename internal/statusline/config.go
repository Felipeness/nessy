package statusline

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Config é a config persistente em ~/.claude-history/statusline.toml.
// Estrutura inspirada em ccstatusline (lines + components) com TOML
// (tipado e legível) ao invés de JSON.
type Config struct {
	Theme    string `toml:"theme" json:"theme"`         // graphite|nord|dracula|sakura|mono
	Style    string `toml:"style" json:"style"`         // plain|powerline|capsule
	Charset  string `toml:"charset" json:"charset"`     // unicode|ascii
	AutoWrap bool   `toml:"auto_wrap" json:"auto_wrap"`

	Lines      []Line                   `toml:"lines" json:"lines"`
	Components map[string]ComponentOpts `toml:"components" json:"components"`
	History    HistoryConfig            `toml:"history" json:"history"`
}

// Line é uma linha do statusline — array ordenado de component names.
type Line struct {
	Components []string `toml:"components" json:"components"`
	Separator  string   `toml:"separator" json:"separator"`
}

// ComponentOpts são overrides por-component (cores, thresholds, format).
type ComponentOpts struct {
	WarnAt     float64 `toml:"warn_at,omitempty" json:"warn_at,omitempty"`
	CriticalAt float64 `toml:"critical_at,omitempty" json:"critical_at,omitempty"`
	Format     string  `toml:"format,omitempty" json:"format,omitempty"`
	Hide       bool    `toml:"hide,omitempty" json:"hide,omitempty"`
}

// HistoryConfig diz onde achar o daemon claude-history.
type HistoryConfig struct {
	Endpoint string `toml:"endpoint" json:"endpoint"`
	Timeout  string `toml:"timeout" json:"timeout"` // ex: "80ms"
}

// TimeoutDuration parseia HistoryConfig.Timeout com fallback de 80ms.
func (h HistoryConfig) TimeoutDuration() time.Duration {
	if h.Timeout == "" {
		return 80 * time.Millisecond
	}
	d, err := time.ParseDuration(h.Timeout)
	if err != nil || d <= 0 {
		return 80 * time.Millisecond
	}
	return d
}

// DefaultConfig é o ponto de partida quando ~/.claude-history/statusline.toml
// não existe. 2 linhas, 7 components, tema graphite, style plain.
func DefaultConfig() *Config {
	return &Config{
		Theme:    "graphite",
		Style:    "plain",
		Charset:  "unicode",
		AutoWrap: true,
		Lines: []Line{
			{
				Components: []string{"cwd", "git", "model", "context_pct", "cost_session", "burn_rate"},
				Separator:  " │ ",
			},
		},
		Components: map[string]ComponentOpts{
			"context_pct":  {WarnAt: 50, CriticalAt: 80},
			"cost_session": {WarnAt: 0.8, CriticalAt: 1.2}, // multiplicador de p90
			"burn_rate":    {WarnAt: 1500, CriticalAt: 3000},
			"rate_5h":      {WarnAt: 70, CriticalAt: 90},
			"rate_7d":      {WarnAt: 70, CriticalAt: 90},
		},
		History: HistoryConfig{
			Endpoint: "http://localhost:5555",
			Timeout:  "80ms",
		},
	}
}

// LoadConfig lê do disco (TOML), aplica defaults onde campos faltam.
// Se o arquivo não existe, retorna DefaultConfig() sem erro.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	user := &Config{}
	if _, err := toml.Decode(string(data), user); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	mergeConfig(cfg, user)
	return cfg, nil
}

// SaveConfig escreve o config como TOML em path. Cria parent dir se preciso.
func SaveConfig(path string, cfg *Config) error {
	if err := os.MkdirAll(parentDir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// mergeConfig sobrepõe campos não-zero do user em cfg (defaults).
func mergeConfig(cfg, user *Config) {
	if user.Theme != "" {
		cfg.Theme = user.Theme
	}
	if user.Style != "" {
		cfg.Style = user.Style
	}
	if user.Charset != "" {
		cfg.Charset = user.Charset
	}
	if len(user.Lines) > 0 {
		cfg.Lines = user.Lines
	}
	for k, v := range user.Components {
		cfg.Components[k] = v
	}
	if user.History.Endpoint != "" {
		cfg.History.Endpoint = user.History.Endpoint
	}
	if user.History.Timeout != "" {
		cfg.History.Timeout = user.History.Timeout
	}
}

func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

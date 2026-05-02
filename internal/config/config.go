// Package config carrega e persiste preferências do usuário (config.toml estático)
// e estado entre runs (state.toml dinâmico — escrito ao sair).
package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

// Config — preferências fixas, editáveis pelo usuário.
type Config struct {
	Cost struct {
		WarnPerDayUSD  float64 `toml:"warn_per_day_usd" json:"warn_per_day_usd"`
		AlertPerDayUSD float64 `toml:"alert_per_day_usd" json:"alert_per_day_usd"`
	} `toml:"cost" json:"cost"`
	UI struct {
		DefaultTab string `toml:"default_tab" json:"default_tab"`
	} `toml:"ui" json:"ui"`
	AI struct {
		Enabled      bool   `toml:"enabled" json:"enabled"`
		OllamaURL    string `toml:"ollama_url" json:"ollama_url"`
		GenModel     string `toml:"gen_model" json:"gen_model"`
		EmbedModel   string `toml:"embed_model" json:"embed_model"`
		AutoGenerate bool   `toml:"auto_generate" json:"auto_generate"`
	} `toml:"ai" json:"ai"`
	// Ingest — filtros aplicados durante reindex pra excluir lixo.
	Ingest struct {
		SkipWarmup     bool     `toml:"skip_warmup" json:"skip_warmup"`         // sessions com primeira msg "I am Claude Code..."
		SkipClearOnly  bool     `toml:"skip_clear_only" json:"skip_clear_only"` // só /clear msgs
		MinMessages    int      `toml:"min_messages" json:"min_messages"`       // skip < N msgs
		ExcludeProjects []string `toml:"exclude_projects" json:"exclude_projects"` // path substrings a ignorar
	} `toml:"ingest" json:"ingest"`
	// Notify — controla notificações do watcher de loops.
	Notify struct {
		Enabled      bool     `toml:"enabled" json:"enabled"`
		MinCount     int      `toml:"min_count" json:"min_count"`
		WindowSecs   int      `toml:"window_secs" json:"window_secs"`
		DebounceSecs int      `toml:"debounce_secs" json:"debounce_secs"`
		IncludeTools []string `toml:"include_tools" json:"include_tools"`
		ExcludeTools []string `toml:"exclude_tools" json:"exclude_tools"`
		PollSecs     int      `toml:"poll_secs" json:"poll_secs"`
	} `toml:"notify" json:"notify"`
}

// State — estado dinâmico, persistido entre runs.
type State struct {
	LastTab              string `toml:"last_tab"`
	RecentGroupByProject bool   `toml:"recent_group_by_project"`
	SearchMode           string `toml:"search_mode"`
}

// DefaultConfig devolve config com defaults sensatos.
func DefaultConfig() *Config {
	c := &Config{}
	c.Cost.WarnPerDayUSD = 5.00
	c.Cost.AlertPerDayUSD = 10.00
	c.UI.DefaultTab = "Recent"
	c.AI.Enabled = true
	c.AI.OllamaURL = "http://localhost:11434"
	c.AI.GenModel = "qwen2.5:7b"
	c.AI.EmbedModel = "nomic-embed-text"
	c.AI.AutoGenerate = true
	// Ingest: defaults razoáveis pra reduzir lixo no índice.
	c.Ingest.SkipWarmup = true
	c.Ingest.SkipClearOnly = true
	c.Ingest.MinMessages = 1
	// Notify: opt-in. Por default não notifica — ligar via Studio quando
	// quiser. Default era true mas ficava barulhento demais.
	c.Notify.Enabled = false
	c.Notify.MinCount = 3
	c.Notify.WindowSecs = 60
	c.Notify.DebounceSecs = 30
	c.Notify.PollSecs = 10
	return c
}

// LoadConfig lê o TOML; se ausente, devolve defaults sem erro.
func LoadConfig(path string) (*Config, error) {
	c := DefaultConfig()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return c, nil
	}
	if _, err := toml.DecodeFile(path, c); err != nil {
		return c, err
	}
	if c.Cost.WarnPerDayUSD == 0 {
		c.Cost.WarnPerDayUSD = 5.00
	}
	if c.Cost.AlertPerDayUSD == 0 {
		c.Cost.AlertPerDayUSD = 10.00
	}
	if c.UI.DefaultTab == "" {
		c.UI.DefaultTab = "Recent"
	}
	if c.AI.OllamaURL == "" {
		c.AI.OllamaURL = "http://localhost:11434"
	}
	if c.AI.GenModel == "" {
		c.AI.GenModel = "qwen2.5:7b"
	}
	if c.AI.EmbedModel == "" {
		c.AI.EmbedModel = "nomic-embed-text"
	}
	if c.Notify.MinCount == 0 {
		c.Notify.MinCount = 3
	}
	if c.Notify.WindowSecs == 0 {
		c.Notify.WindowSecs = 60
	}
	if c.Notify.DebounceSecs == 0 {
		c.Notify.DebounceSecs = 30
	}
	if c.Notify.PollSecs == 0 {
		c.Notify.PollSecs = 10
	}
	return c, nil
}

// SaveConfig grava o Config no path. Usado pelo endpoint POST /api/config.
func SaveConfig(path string, c *Config) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

// LoadState lê state.toml; se ausente ou corrompido, devolve State zerado.
func LoadState(path string) *State {
	s := &State{LastTab: "Recent", SearchMode: "metadata"}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return s
	}
	if _, err := toml.DecodeFile(path, s); err != nil {
		return s
	}
	return s
}

// SaveState grava o State no path indicado. Erros silenciosos — state é nice-to-have.
func SaveState(path string, s *State) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(s)
}

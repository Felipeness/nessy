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
		WarnPerDayUSD  float64 `toml:"warn_per_day_usd"`
		AlertPerDayUSD float64 `toml:"alert_per_day_usd"`
	} `toml:"cost"`
	UI struct {
		DefaultTab string `toml:"default_tab"`
	} `toml:"ui"`
	AI struct {
		Enabled      bool   `toml:"enabled"`
		OllamaURL    string `toml:"ollama_url"`
		GenModel     string `toml:"gen_model"`
		EmbedModel   string `toml:"embed_model"`
		AutoGenerate bool   `toml:"auto_generate"`
	} `toml:"ai"`
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
	return c, nil
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

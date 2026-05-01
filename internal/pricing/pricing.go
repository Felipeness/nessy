// Package pricing carrega snapshot de preços por modelo do TOML e calcula
// custo de uma Session somando input/output/cache tokens.
package pricing

import (
	"github.com/BurntSushi/toml"
	"github.com/felipeness/nessy/internal/model"
)

// Model mapeia o pricing de um único modelo (preços por 1M tokens).
type Model struct {
	Name                 string  `toml:"name"`
	InputPerMTok         float64 `toml:"input_per_mtok"`
	OutputPerMTok        float64 `toml:"output_per_mtok"`
	CacheCreationPerMTok float64 `toml:"cache_creation_per_mtok"`
	CacheReadPerMTok     float64 `toml:"cache_read_per_mtok"`
}

// Pricing é o snapshot inteiro carregado do TOML, indexado por nome do modelo.
type Pricing struct {
	DefaultCurrency string           `toml:"default_currency"`
	BRLRate         float64          `toml:"brl_rate"`
	ModelsList      []Model          `toml:"models"`
	Models          map[string]Model `toml:"-"`
}

// Cost expressa o custo de uma session.
type Cost struct {
	USD              float64
	BRL              float64 // 0 se BRLRate não estiver setado
	InputUSD         float64
	OutputUSD        float64
	CacheCreationUSD float64
	CacheReadUSD     float64
}

// Load reads a pricing TOML file and indexes models by name.
func Load(path string) (*Pricing, error) {
	var p Pricing
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return nil, err
	}
	if p.DefaultCurrency == "" {
		p.DefaultCurrency = "USD"
	}
	p.Models = make(map[string]Model, len(p.ModelsList))
	for _, m := range p.ModelsList {
		p.Models[m.Name] = m
	}
	return &p, nil
}

// Cost returns the USD (and optionally BRL) cost of the given session, broken
// down per token category. Returns ok=false if the model is unknown.
func (p *Pricing) Cost(s *model.Session) (Cost, bool) {
	m, ok := p.Models[s.Model]
	if !ok {
		return Cost{}, false
	}
	in := float64(s.InputTokens) * m.InputPerMTok / 1_000_000.0
	out := float64(s.OutputTokens) * m.OutputPerMTok / 1_000_000.0
	cc := float64(s.CacheCreationTokens) * m.CacheCreationPerMTok / 1_000_000.0
	cr := float64(s.CacheReadTokens) * m.CacheReadPerMTok / 1_000_000.0
	total := in + out + cc + cr
	c := Cost{
		USD:              total,
		InputUSD:         in,
		OutputUSD:        out,
		CacheCreationUSD: cc,
		CacheReadUSD:     cr,
	}
	if p.BRLRate > 0 {
		c.BRL = total * p.BRLRate
	}
	return c, true
}

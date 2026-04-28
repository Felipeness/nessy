package pricing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/felipeness/claude-history/internal/model"
)

func TestLoadAndCalculate(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "pricing.toml")
	const fixture = `default_currency = "USD"
brl_rate = 5.0

[[models]]
name = "claude-sonnet-4-6"
input_per_mtok = 3.00
output_per_mtok = 15.00
cache_creation_per_mtok = 3.75
cache_read_per_mtok = 0.30
`
	if err := os.WriteFile(tomlPath, []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := Load(tomlPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	s := &model.Session{
		Model:               "claude-sonnet-4-6",
		InputTokens:         1_000_000,
		OutputTokens:        100_000,
		CacheCreationTokens: 50_000,
		CacheReadTokens:     500_000,
	}

	cost, ok := p.Cost(s)
	if !ok {
		t.Fatal("Cost returned ok=false for known model")
	}
	want := 3.00 + 1.50 + 0.1875 + 0.15 // = 4.8375 USD
	if abs(cost.USD-want) > 0.0001 {
		t.Errorf("cost.USD = %.4f, want %.4f", cost.USD, want)
	}
	if abs(cost.BRL-want*5.0) > 0.0001 {
		t.Errorf("cost.BRL = %.4f, want %.4f", cost.BRL, want*5.0)
	}
}

func TestCost_unknownModelReturnsFalse(t *testing.T) {
	p := &Pricing{Models: map[string]Model{}}
	_, ok := p.Cost(&model.Session{Model: "claude-future-99"})
	if ok {
		t.Error("expected ok=false for unknown model")
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

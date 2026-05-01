package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/felipeness/nessy/internal/branding"
	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/pricing"
)

type exportDoneMsg struct {
	path string
	err  error
}

type exportPayload struct {
	Session *model.Session   `json:"session"`
	Cost    *exportCost      `json:"cost,omitempty"`
}

type exportCost struct {
	USD float64 `json:"usd"`
	BRL float64 `json:"brl,omitempty"`
}

func exportCmd(s *model.Session, p *pricing.Pricing) tea.Cmd {
	return func() tea.Msg {
		dir := filepath.Join(branding.CacheDir(), "exports")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return exportDoneMsg{err: err}
		}
		out := filepath.Join(dir, fmt.Sprintf("%s.json", s.SessionID))
		payload := exportPayload{Session: s}
		if p != nil {
			if c, ok := p.Cost(s); ok {
				payload.Cost = &exportCost{USD: c.USD, BRL: c.BRL}
			}
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return exportDoneMsg{err: err}
		}
		if err := os.WriteFile(out, data, 0644); err != nil {
			return exportDoneMsg{err: err}
		}
		return exportDoneMsg{path: out}
	}
}

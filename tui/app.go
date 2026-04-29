// Package tui implements the Bubble Tea front-end for claude-history.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/felipeness/claude-history/internal/index"
	"github.com/felipeness/claude-history/internal/pricing"
)

type tabID int

const (
	tabSearch tabID = iota
	tabRecent
	tabStats
)

var tabNames = []string{"Search", "Recent", "Stats"}

const wideCols = 120

// Model é o root da TUI.
type Model struct {
	db        *index.DB
	pricing   *pricing.Pricing
	width     int
	height    int
	activeTab tabID
	status    string
}

// New cria o root model. Tab default é Recent (caso de uso mais comum).
func New(db *index.DB, p *pricing.Pricing) Model {
	return Model{db: db, pricing: p, activeTab: tabRecent, status: "ready"}
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		k := msg.String()
		switch {
		case keyMatches(k, keys.Quit):
			return m, tea.Quit
		case keyMatches(k, keys.NextTab):
			m.activeTab = (m.activeTab + 1) % 3
			return m, nil
		case keyMatches(k, keys.PrevTab):
			m.activeTab = (m.activeTab + 2) % 3
			return m, nil
		}
	}
	return m, nil
}

// View renders.
func (m Model) View() string {
	tabBar := m.renderTabBar()
	body := m.renderBody()
	status := m.renderStatusBar()
	return lipgloss.JoinVertical(lipgloss.Left, tabBar, body, status)
}

func (m Model) renderTabBar() string {
	parts := make([]string, 0, len(tabNames))
	for i, name := range tabNames {
		if tabID(i) == m.activeTab {
			parts = append(parts, tabActiveStyle.Render(name))
		} else {
			parts = append(parts, tabInactiveStyle.Render(name))
		}
	}
	return tabBarStyle.Render(lipgloss.JoinHorizontal(lipgloss.Left, parts...))
}

func (m Model) renderBody() string {
	bodyHeight := m.height - 2
	if bodyHeight < 5 {
		bodyHeight = 5
	}
	style := lipgloss.NewStyle().Width(m.width).Height(bodyHeight)
	switch m.activeTab {
	case tabSearch:
		return style.Render("(search tab — task 10)")
	case tabRecent:
		return style.Render("(recent tab — task 9)")
	case tabStats:
		return style.Render("(stats tab — task 12)")
	}
	return ""
}

func (m Model) renderStatusBar() string {
	return statusBarStyle.Width(m.width).Render(fmt.Sprintf(" %s ", m.status))
}

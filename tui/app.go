// Package tui implements the Bubble Tea front-end for claude-history.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/felipeness/claude-history/internal/index"
	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
)

type tabID int

const (
	tabSearch tabID = iota
	tabRecent
	tabStats
)

var tabNames = []string{"Search", "Recent", "Stats"}

const numTabs = 3

const wideCols = 120

// Model é o root da TUI.
type Model struct {
	db          *index.DB
	pricing     *pricing.Pricing
	width       int
	height      int
	activeTab   tabID
	status      string
	statusUntil time.Time
	showHelp    bool
	refreshing  bool
	spin        spinner.Model
	detailCtx   *detailContext
	recent      recentView
	search      searchView
	stats       statsView
}

// New cria o root model carregando sessions do cache.
func New(db *index.DB, p *pricing.Pricing) Model {
	sessions, _ := db.ListSessions()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorAccent)
	return Model{
		db:        db,
		pricing:   p,
		activeTab: tabRecent,
		status:    "ready",
		spin:      sp,
		detailCtx: newDetailContext(sessions, p),
		recent:    newRecentView(sessions, p),
		search:    newSearchView(db, p, sessions),
		stats:     newStatsView(sessions, p),
	}
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return m.spin.Tick }

func (m *Model) reload() {
	sessions, _ := m.db.ListSessions()
	m.detailCtx = newDetailContext(sessions, m.pricing)
	m.recent = newRecentView(sessions, m.pricing)
	m.search = newSearchView(m.db, m.pricing, sessions)
	m.stats = newStatsView(sessions, m.pricing)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case refreshDoneMsg:
		m.refreshing = false
		if msg.err != nil {
			m.status = "refresh error: " + msg.err.Error()
		} else {
			m.status = fmt.Sprintf("refresh: +%d new, %d updated, %d removed",
				msg.stats.New, msg.stats.Updated, msg.stats.Removed)
			m.reload()
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case exportDoneMsg:
		if msg.err != nil {
			m.status = "export error: " + msg.err.Error()
		} else {
			m.status = "exported to " + msg.path
		}
		m.statusUntil = time.Now().Add(3 * time.Second)
		return m, nil

	case tea.KeyMsg:
		k := msg.String()

		// Help overlay sempre captura ?
		if keyMatches(k, keys.Help) {
			m.showHelp = !m.showHelp
			return m, nil
		}
		if m.showHelp {
			// qualquer tecla fecha help
			m.showHelp = false
			return m, nil
		}

		// Search tab quando ativa: input box recebe a maioria das teclas
		if m.activeTab == tabSearch && !isGlobalKey(k) {
			var cmd tea.Cmd
			m.search.input, cmd = m.search.input.Update(msg)
			m.search.Filter(m.search.input.Value())
			return m, cmd
		}

		switch {
		case keyMatches(k, keys.Quit):
			return m, tea.Quit
		case keyMatches(k, keys.NextTab):
			m.activeTab = (m.activeTab + 1) % numTabs
			return m, nil
		case keyMatches(k, keys.PrevTab):
			m.activeTab = (m.activeTab + numTabs - 1) % numTabs
			return m, nil
		case keyMatches(k, keys.Tab1):
			m.activeTab = tabSearch
			return m, nil
		case keyMatches(k, keys.Tab2):
			m.activeTab = tabRecent
			return m, nil
		case keyMatches(k, keys.Tab3):
			m.activeTab = tabStats
			return m, nil
		case keyMatches(k, keys.Up):
			m.moveCursor(-1)
			return m, nil
		case keyMatches(k, keys.Down):
			m.moveCursor(+1)
			return m, nil
		case keyMatches(k, keys.PageUp):
			m.moveCursor(-10)
			return m, nil
		case keyMatches(k, keys.PageDn):
			m.moveCursor(+10)
			return m, nil
		case keyMatches(k, keys.Top):
			m.cursorTo(0)
			return m, nil
		case keyMatches(k, keys.Bottom):
			m.cursorTo(99999)
			return m, nil
		case keyMatches(k, keys.Group):
			if m.activeTab == tabRecent {
				m.recent.groupByProject = !m.recent.groupByProject
			}
			return m, nil
		case keyMatches(k, keys.Stats):
			if m.activeTab == tabStats && m.width < wideCols {
				m.stats.showLocal = !m.stats.showLocal
			}
			return m, nil
		case keyMatches(k, keys.Refresh):
			m.status = "refreshing…"
			m.refreshing = true
			return m, refreshCmd(m.db, claudeProjectsRoot())
		case keyMatches(k, keys.Enter):
			s := m.selectedForActiveTab()
			if s != nil {
				return m, tea.Batch(tea.ExitAltScreen, resumeCmd(s), tea.Quit)
			}
			return m, nil
		case keyMatches(k, keys.OpenDir):
			s := m.selectedForActiveTab()
			if s != nil {
				_ = exec.Command("open", s.ProjectDir).Start()
			}
			return m, nil
		case keyMatches(k, keys.Export):
			s := m.selectedForActiveTab()
			if s != nil {
				return m, exportCmd(s, m.pricing)
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	switch m.activeTab {
	case tabRecent:
		m.recent.cursor = clamp(m.recent.cursor+delta, 0, len(m.recent.sessions)-1)
	case tabSearch:
		m.search.cursor = clamp(m.search.cursor+delta, 0, len(m.search.results)-1)
	}
}

func (m *Model) cursorTo(pos int) {
	switch m.activeTab {
	case tabRecent:
		m.recent.cursor = clamp(pos, 0, len(m.recent.sessions)-1)
	case tabSearch:
		m.search.cursor = clamp(pos, 0, len(m.search.results)-1)
	}
}

func (m Model) selectedForActiveTab() *model.Session {
	switch m.activeTab {
	case tabRecent:
		return m.recent.selected()
	case tabSearch:
		return m.search.selected()
	case tabStats:
		return m.recent.selected()
	}
	return nil
}

// View renders.
func (m Model) View() string {
	tabBar := m.renderTabBar()
	body := m.renderBody()
	status := m.renderStatusBar()
	out := lipgloss.JoinVertical(lipgloss.Left, tabBar, body, status)
	if m.showHelp {
		help := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(1, 2).
			Render(helpText())
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, help)
	}
	return out
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
	if m.width >= wideCols {
		return m.renderWide(bodyHeight)
	}
	return m.renderNarrow(bodyHeight)
}

func (m Model) renderWide(h int) string {
	leftW := m.width * 4 / 10
	rightW := m.width - leftW
	left := lipgloss.NewStyle().Width(leftW).Height(h)
	right := lipgloss.NewStyle().Width(rightW).Height(h).Padding(0, 1)
	switch m.activeTab {
	case tabSearch:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			left.Render(m.search.View(leftW, h)),
			right.Render(m.detailCtx.renderDetail(m.search.selected())),
		)
	case tabRecent:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			left.Render(m.recent.View(leftW, h)),
			right.Render(m.detailCtx.renderDetail(m.recent.selected())),
		)
	case tabStats:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			left.Render(m.stats.renderGlobal(leftW)),
			right.Render(m.detailCtx.renderDetail(m.recent.selected())),
		)
	}
	return ""
}

func (m Model) renderNarrow(h int) string {
	switch m.activeTab {
	case tabSearch:
		return m.search.View(m.width, h)
	case tabRecent:
		return m.recent.View(m.width, h)
	case tabStats:
		if m.stats.showLocal {
			return m.detailCtx.renderDetail(m.recent.selected())
		}
		return m.stats.renderGlobal(m.width)
	}
	return ""
}

func (m Model) renderStatusBar() string {
	prefix := ""
	if m.refreshing {
		prefix = m.spin.View() + " "
	}
	if !m.statusUntil.IsZero() && time.Now().After(m.statusUntil) {
		// status temporário expirou
	}
	return statusBarStyle.Width(m.width).Render(fmt.Sprintf(" %s%s ", prefix, m.status))
}

func helpText() string {
	return `KEYBINDS

Tab / Shift+Tab    trocar tab
j / k              navegar lista
Enter              retomar session
/ ou f             search box
:body <q>          full-text search
g                  toggle agrupamento (Recent)
s                  toggle stats local (narrow)
r                  refresh
?                  toggle help (esta tela)
q ou Esc           quit
Ctrl+O             abrir pasta no Finder

Pressiona qualquer tecla pra fechar.`
}

func isGlobalKey(k string) bool {
	switch k {
	case "tab", "shift+tab", "esc", "enter", "ctrl+c", "ctrl+o":
		return true
	}
	return false
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// resumeMsg is the result of trying to launch claude --resume.
type resumeMsg struct{ err error }

func resumeCmd(s *model.Session) tea.Cmd {
	return func() tea.Msg {
		if s == nil {
			return resumeMsg{}
		}
		claude, err := exec.LookPath("claude")
		if err != nil {
			return resumeMsg{err: err}
		}
		c := exec.Command(claude, "--resume", s.SessionID)
		c.Dir = s.ProjectDir
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return resumeMsg{err: c.Run()}
	}
}

type refreshDoneMsg struct {
	stats index.ReindexStats
	err   error
}

func refreshCmd(db *index.DB, root string) tea.Cmd {
	return func() tea.Msg {
		stats, err := db.Reindex(root)
		return refreshDoneMsg{stats: stats, err: err}
	}
}

func claudeProjectsRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

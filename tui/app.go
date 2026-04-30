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

	"github.com/felipeness/claude-history/internal/ai"
	"github.com/felipeness/claude-history/internal/config"
	"github.com/felipeness/claude-history/internal/index"
	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
)

type tabID int

const (
	tabSearch tabID = iota
	tabRecent
	tabStats
	tabCosts
	tabTimeline
	tabTools
	tabBehavior
	tabAI
)

var tabNames = []string{"Search", "Recent", "Stats", "Costs", "Timeline", "Tools", "Behavior", "AI"}

const numTabs = 8

const wideCols = 120

// Model é o root da TUI.
type Model struct {
	db          *index.DB
	pricing     *pricing.Pricing
	cfg         *config.Config
	statePath   string
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
	costs       costsView
	timeline    timelineView
	tools       toolsView
	behavior    behaviorView
	ai          aiView
	aiClient    *ai.Client
	aiWorker    *ai.Worker

	// pendingResume é setado quando user pressiona Enter — main.go executa
	// `claude --resume` depois de prog.Run() retornar. Evita race com
	// tea.Quit terminando o subprocess antes dele herdar o TTY.
	pendingResume *model.Session
}

// PendingResume devolve a session que o user quis retomar (nil se nenhuma).
func (m Model) PendingResume() *model.Session { return m.pendingResume }

// AIDeps agrupa client e worker de AI (opcionais — nil se desabilitado).
type AIDeps struct {
	Enabled    bool
	Client     *ai.Client
	Worker     *ai.Worker
	GenModel   string
	EmbedModel string
}

// New cria o root model carregando sessions do cache + state persistido.
func New(db *index.DB, p *pricing.Pricing, cfg *config.Config, state *config.State, statePath string, aiDeps AIDeps) Model {
	sessions, _ := db.ListSessions()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorAccent)

	tab := tabFromName(state.LastTab)
	if tab < 0 {
		tab = tabFromName(cfg.UI.DefaultTab)
		if tab < 0 {
			tab = tabRecent
		}
	}

	recent := newRecentView(sessions, p, loadSummaries(db))
	recent.groupByProject = state.RecentGroupByProject

	search := newSearchView(db, p, sessions)
	if state.SearchMode == "full-text" {
		search.mode = modeFullText
	}

	return Model{
		db:        db,
		pricing:   p,
		cfg:       cfg,
		statePath: statePath,
		activeTab: tab,
		status:    "ready",
		spin:      sp,
		detailCtx: newDetailContext(sessions, p),
		recent:    recent,
		search:    search,
		stats:     newStatsView(sessions, p),
		costs:     newCostsView(sessions, p),
		timeline:  newTimelineView(sessions, p),
		tools:     newToolsView(sessions),
		behavior:  newBehaviorView(sessions, p),
		ai:        newAIView(aiDeps.Enabled, aiDeps.Client, aiDeps.Worker, aiDeps.GenModel, aiDeps.EmbedModel, db, sessions),
		aiClient:  aiDeps.Client,
		aiWorker:  aiDeps.Worker,
	}
}

func tabFromName(name string) tabID {
	for i, n := range tabNames {
		if n == name {
			return tabID(i)
		}
	}
	return -1
}

func (m Model) currentState() *config.State {
	mode := "metadata"
	if m.search.mode == modeFullText {
		mode = "full-text"
	}
	return &config.State{
		LastTab:              tabNames[m.activeTab],
		RecentGroupByProject: m.recent.groupByProject,
		SearchMode:           mode,
	}
}

// loadSummaries lê AICacheList do db e devolve session_id → summary.
// Erros silenciados — recent ainda funciona com fallback FirstUserMsg.
func loadSummaries(db *index.DB) map[string]string {
	out := map[string]string{}
	caches, err := db.AICacheList()
	if err != nil {
		return out
	}
	for _, c := range caches {
		if c.Summary != "" {
			out[c.SessionID] = c.Summary
		}
	}
	return out
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return m.spin.Tick }

func (m *Model) reload() {
	sessions, _ := m.db.ListSessions()
	m.detailCtx = newDetailContext(sessions, m.pricing)
	m.recent = newRecentView(sessions, m.pricing, loadSummaries(m.db))
	m.search = newSearchView(m.db, m.pricing, sessions)
	m.stats = newStatsView(sessions, m.pricing)
	m.costs = newCostsView(sessions, m.pricing)
	m.timeline = newTimelineView(sessions, m.pricing)
	m.tools = newToolsView(sessions)
	m.behavior = newBehaviorView(sessions, m.pricing)
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

	case aiGeneratedMsg:
		// Geração assíncrona terminou — recarrega state da aiView
		m.ai.genStatus = ""
		m.ai.reload()
		if msg.err != nil {
			m.status = "ai " + msg.kind + " erro: " + msg.err.Error()
		} else {
			m.status = "ai " + msg.kind + " ✓"
		}
		m.statusUntil = time.Now().Add(4 * time.Second)
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
			if m.statePath != "" {
				_ = config.SaveState(m.statePath, m.currentState())
			}
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
		case keyMatches(k, keys.Tab4):
			m.activeTab = tabCosts
			return m, nil
		case keyMatches(k, keys.Tab5):
			m.activeTab = tabTimeline
			return m, nil
		case keyMatches(k, keys.Tab6):
			m.activeTab = tabTools
			return m, nil
		case keyMatches(k, keys.Tab7):
			m.activeTab = tabBehavior
			return m, nil
		case keyMatches(k, keys.Tab8):
			m.activeTab = tabAI
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
		case keyMatches(k, keys.Expand):
			if m.activeTab == tabSearch {
				m.search.ToggleExpand()
			}
			return m, nil
		case keyMatches(k, keys.Fuzzy):
			if m.activeTab == tabSearch {
				m.search.ToggleFuzzy()
			}
			return m, nil
		// AI tab actions — só fazem algo quando estamos no tab AI
		case keyMatches(k, keys.GenSummaries):
			if m.activeTab == tabAI {
				return m, m.ai.enqueueAllSummariesCmd()
			}
			return m, nil
		case keyMatches(k, keys.GenClusters):
			if m.activeTab == tabAI {
				return m, m.ai.genClustersCmd()
			}
			return m, nil
		case keyMatches(k, keys.GenInsights):
			if m.activeTab == tabAI {
				return m, m.ai.genInsightsCmd()
			}
			return m, nil
		case keyMatches(k, keys.GenProfile):
			if m.activeTab == tabAI {
				return m, m.ai.genProfileCmd()
			}
			return m, nil
		case keyMatches(k, keys.GenKnowledge):
			if m.activeTab == tabAI {
				return m, m.ai.genKnowledgeCmd(m.recent.selected())
			}
			return m, nil
		case keyMatches(k, keys.GenKnowledgeAll):
			if m.activeTab == tabAI {
				return m, m.ai.genKnowledgeAllCmd()
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
				m.pendingResume = s
				return m, tea.Quit
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
	case tabTools:
		m.tools.cursor = clamp(m.tools.cursor+delta, 0, len(m.tools.stats)-1)
	}
}

func (m *Model) cursorTo(pos int) {
	switch m.activeTab {
	case tabRecent:
		m.recent.cursor = clamp(pos, 0, len(m.recent.sessions)-1)
	case tabSearch:
		m.search.cursor = clamp(pos, 0, len(m.search.results)-1)
	case tabTools:
		m.tools.cursor = clamp(pos, 0, len(m.tools.stats)-1)
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
	bar := tabBarStyle.Render(lipgloss.JoinHorizontal(lipgloss.Left, parts...))
	// Hint contextual da tab ativa — atalhos relevantes só pra ela.
	hint := lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1).Render(m.tabHint())
	return lipgloss.JoinVertical(lipgloss.Left, bar, hint)
}

// tabHint devolve uma linha curta de atalhos relevantes à tab ativa.
// Sempre inclui [?] help no fim pra lembrar onde achar a referência completa.
func (m Model) tabHint() string {
	const tail = "  ·  [?] ajuda  [r] refresh  [q] sair"
	switch m.activeTab {
	case tabSearch:
		return "[ctrl+t] agrupar/all  [ctrl+y] fuzzy/exato  [↑↓] nav  [enter] retomar" + tail
	case tabRecent:
		return "[g] agrupar tempo/projeto  [↑↓] nav  [enter] retomar  [ctrl+e] export  [ctrl+o] abrir" + tail
	case tabStats:
		return "[s] toggle stats local (narrow)  [↑↓] nav" + tail
	case tabCosts:
		return "custos por dia/projeto/modelo  [↑↓] nav" + tail
	case tabTimeline:
		return "sessions agrupadas por dia  [↑↓] nav" + tail
	case tabTools:
		return "tools globais + drill-down  [↑↓] nav" + tail
	case tabBehavior:
		return "n-grams · co-occurrence · style stats" + tail
	case tabAI:
		return "[S] summaries  [C] clusters  [I] insights  [P] profile  [K] knowledge  [ctrl+k] all" + tail
	}
	return tail
}

func (m Model) renderBody() string {
	// 3 linhas reservadas: tab bar (1) + hint contextual (1) + status bar (1).
	bodyHeight := m.height - 3
	if bodyHeight < 5 {
		bodyHeight = 5
	}
	var body string
	if m.width >= wideCols {
		body = m.renderWide(bodyHeight)
	} else {
		body = m.renderNarrow(bodyHeight)
	}
	// Clamp pra bodyHeight × m.width: garante que tab bar (linha 0) e status
	// bar (última linha) sempre apareçam, mesmo que tabs como Stats/Behavior/AI
	// produzam conteúdo mais alto que o terminal.
	return lipgloss.NewStyle().Width(m.width).Height(bodyHeight).MaxHeight(bodyHeight).Render(body)
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
	case tabCosts:
		// full-width — costs/timeline/behavior/ai não tem detail panel
		return lipgloss.NewStyle().Width(m.width).MaxHeight(h).Render(m.costs.View(m.width))
	case tabTimeline:
		return lipgloss.NewStyle().Width(m.width).MaxHeight(h).Render(m.timeline.View(m.width))
	case tabTools:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			left.Render(m.tools.View(leftW)),
			right.Render(m.tools.renderDrillDown(rightW)),
		)
	case tabBehavior:
		return lipgloss.NewStyle().Width(m.width).MaxHeight(h).Render(m.behavior.View(m.width))
	case tabAI:
		return lipgloss.NewStyle().Width(m.width).MaxHeight(h).Render(m.ai.View(m.width, m.recent.selected()))
	}
	return ""
}

func (m Model) renderNarrow(h int) string {
	clamp := lipgloss.NewStyle().Width(m.width).MaxHeight(h)
	switch m.activeTab {
	case tabSearch:
		return m.search.View(m.width, h)
	case tabRecent:
		return m.recent.View(m.width, h)
	case tabStats:
		if m.stats.showLocal {
			return clamp.Render(m.detailCtx.renderDetail(m.recent.selected()))
		}
		return clamp.Render(m.stats.renderGlobal(m.width))
	case tabCosts:
		return clamp.Render(m.costs.View(m.width))
	case tabTimeline:
		return clamp.Render(m.timeline.View(m.width))
	case tabTools:
		return clamp.Render(m.tools.View(m.width))
	case tabBehavior:
		return clamp.Render(m.behavior.View(m.width))
	case tabAI:
		return clamp.Render(m.ai.View(m.width, m.recent.selected()))
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
	return `KEYBINDS — claude-history TUI

NAVEGAÇÃO ENTRE TABS
  Tab / Shift+Tab    próxima/anterior tab
  1 2 3 4 5 6 7 8    pula direto pra Search/Recent/Stats/Costs/Timeline/Tools/Behavior/AI

NAVEGAÇÃO NA LISTA (qualquer tab com lista)
  ↑ ↓ ou j k         linha acima/abaixo
  PgUp / PgDn        página (10 linhas)
  Home / G ou End    topo / fim
  Enter              retomar session selecionada (claude --resume)

GLOBAL
  r                  refresh (re-indexa novas sessions)
  ?                  toggle help (esta tela)
  q ou Esc           sair (salva state)
  Ctrl+E             exportar session selecionada como JSON
  Ctrl+O             abrir pasta da session no Finder

TAB RECENT
  g                  toggle agrupamento por tempo ↔ projeto

TAB SEARCH
  Digite pra filtrar — default = hybrid (metadata + body)
  Prefixos:
    :body <q>        só full-text via FTS5
    :meta <q>        só metadata (path/branch/msg)
    :sim <q>         semantic via embeddings (Ollama)
    :all <q>         alias pra "todos hits"
  Filtros (qualquer modo):
    project:<x>      filtra por substring no path
    branch:<x>       filtra por branch git
    model:<x>        opus, sonnet, haiku
    since:<dur>      7d, 24h, 30m
    cost:>N / cost:<N
  Toggles:
    Ctrl+T           agrupar ↔ todos hits
    Ctrl+Y           exato ↔ fuzzy (Porter stemmer)

TAB STATS (terminal narrow < 120 cols)
  s                  toggle stats global ↔ detail da session selecionada

TAB AI (Ollama)
  S                  enfileirar summaries de todas sessions
  C                  recompute clusters K-means + LLM labels
  I                  gerar insights (advisor)
  P                  gerar profile (perfil pt-BR)
  K                  gerar knowledge da session selecionada (Recent)
  Ctrl+K             gerar knowledge de TODAS sessions (5-10 min)

Pressiona qualquer tecla pra fechar.`
}

func isGlobalKey(k string) bool {
	switch k {
	case "tab", "shift+tab", "esc", "enter", "ctrl+c", "ctrl+o",
		"ctrl+e", "ctrl+t", "ctrl+y", "ctrl+k", "ctrl+f", "ctrl+b",
		"up", "down", "home", "end", "pgup", "pgdown":
		// Setas + Ctrl+* nunca são caracteres digitáveis, então sempre
		// passam por cima da input pra navegar/atalhos globais funcionarem
		// na tab Search.
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

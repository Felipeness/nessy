package statusline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// RenderCtx carrega tudo que um component precisa pra renderizar.
type RenderCtx struct {
	In      *Input       // stdin do Claude Code
	History *HistoryData // do daemon (pode ser nil — fallback graceful)
	Theme   *Theme
	Now     time.Time
}

// Segment é a unidade de output do component: texto + cores que serão
// resolvidas pelo renderer (plain / powerline / capsule).
type Segment struct {
	Name string // component name (pra debug e per-component theming)
	Text string
	FG   Color
	BG   Color
	Bold bool
}

// Empty marca segments vazios pra serem dropados sem aparecer.
func (s Segment) Empty() bool { return strings.TrimSpace(s.Text) == "" }

// Component é a interface que todos componentes implementam.
type Component interface {
	Name() string
	Render(c *RenderCtx, opts ComponentOpts) Segment
}

// ComponentMeta descreve um component pra UI (catálogo do Studio).
type ComponentMeta struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Category    string `json:"category"` // path|git|model|context|cost|limits|history|system
	Description string `json:"description"`
	NeedsHist   bool   `json:"needs_history"` // true → só funciona com daemon up
	HasWarnAt   bool   `json:"has_warn_at"`   // true → aceita warn_at/critical_at
}

// componentMetas catalogo manual — fonte de verdade pra UI.
var componentMetas = map[string]ComponentMeta{
	"cwd":           {Name: "cwd", Label: "Pasta atual", Category: "path", Description: "Caminho da pasta encurtado com ~"},
	"git":           {Name: "git", Label: "Git branch", Category: "git", Description: "Branch + dirty marker (✱) + ahead/behind"},
	"model":         {Name: "model", Label: "Modelo", Category: "model", Description: "Display name do modelo atual"},
	"context_pct":   {Name: "context_pct", Label: "Context %", Category: "context", Description: "Bar + percentual com cor por severity", HasWarnAt: true},
	"cost_session":  {Name: "cost_session", Label: "Cost session", Category: "cost", Description: "$ atual com badge vs p90 (se daemon up)", HasWarnAt: true},
	"burn_rate":     {Name: "burn_rate", Label: "Burn rate", Category: "cost", Description: "Tokens/min — requer daemon", NeedsHist: true, HasWarnAt: true},
	"cost_today":    {Name: "cost_today", Label: "Cost hoje", Category: "cost", Description: "Soma do dia inteiro — requer daemon", NeedsHist: true},
	"cost_month":    {Name: "cost_month", Label: "Cost mês", Category: "cost", Description: "Acumulado + projeção — requer daemon", NeedsHist: true},
	"rate_5h":       {Name: "rate_5h", Label: "Rate 5h", Category: "limits", Description: "% do bloco de 5h + countdown", HasWarnAt: true},
	"rate_7d":       {Name: "rate_7d", Label: "Rate 7d", Category: "limits", Description: "% do bloco semanal", HasWarnAt: true},
	"ticket":        {Name: "ticket", Label: "Ticket", Category: "git", Description: "Auto-extrai TICKET-NNNN da branch"},
	"cluster":       {Name: "cluster", Label: "Cluster AI", Category: "history", Description: "Label do cluster AI desse projeto — requer daemon", NeedsHist: true},
	"vim_mode":      {Name: "vim_mode", Label: "Vim mode", Category: "system", Description: "NORMAL/INSERT (se vim ativado)"},
	"lines_changed": {Name: "lines_changed", Label: "Linhas +/-", Category: "git", Description: "Linhas adicionadas/removidas na session"},
	"time":          {Name: "time", Label: "Hora", Category: "system", Description: "hh:mm atual"},
	"mcp_status":    {Name: "mcp_status", Label: "MCP status", Category: "system", Description: "Status dos MCP servers (placeholder)"},
}

// Metas devolve o catálogo de components em ordem alfabética.
func Metas() []ComponentMeta {
	out := make([]ComponentMeta, 0, len(componentMetas))
	for _, m := range componentMetas {
		out = append(out, m)
	}
	// ordena por categoria depois nome pra UI ficar previsível.
	sortMetas(out)
	return out
}

func sortMetas(out []ComponentMeta) {
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			less := out[j].Category < out[j-1].Category ||
				(out[j].Category == out[j-1].Category && out[j].Name < out[j-1].Name)
			if !less {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
}

// registry global, populado nos init() dos arquivos de cada component.
var registry = map[string]Component{}

// Register adiciona um component ao registry. Chamado nos init() abaixo.
func Register(c Component) { registry[c.Name()] = c }

// Get devolve um component por nome (nil se não existe).
func Get(name string) Component { return registry[name] }

// AllNames lista todos components registrados (ordem não-determinística;
// usar pra catálogo na UI/preview).
func AllNames() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// helper: encurta path home com ~ + corta com … se passar de maxLen.
func shortenPath(p string, maxLen int) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		p = "~" + strings.TrimPrefix(p, home)
	}
	if maxLen > 0 && len(p) > maxLen {
		// mantém o final (último diretório é o mais relevante)
		p = "…" + p[len(p)-(maxLen-1):]
	}
	return p
}

// helper: formata bar de progresso unicode (0-100).
func progressBar(pct float64, width int) string {
	if width <= 0 {
		width = 8
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100.0 * float64(width))
	return strings.Repeat("▓", filled) + strings.Repeat("░", width-filled)
}

// =============================================================================
// CWD — caminho da pasta atual, encurtado.
// =============================================================================

type cwdComp struct{}

func (cwdComp) Name() string { return "cwd" }
func (cwdComp) Render(c *RenderCtx, _ ComponentOpts) Segment {
	dir := c.In.Workspace.CurrentDir
	if dir == "" {
		dir = c.In.CWD
	}
	if dir == "" {
		return Segment{}
	}
	short := shortenPath(dir, 40)
	seg := c.Theme.SegOf("cwd")
	return Segment{Name: "cwd", Text: short, FG: seg.FG, BG: seg.BG}
}

// =============================================================================
// Git — branch + dirty marker + ahead/behind. Usa info do worktree do stdin
// quando disponível, fallback pra git CLI no diretório.
// =============================================================================

type gitComp struct{}

func (gitComp) Name() string { return "git" }
func (gitComp) Render(c *RenderCtx, _ ComponentOpts) Segment {
	branch := ""
	if c.In.Worktree != nil && c.In.Worktree.Branch != "" {
		branch = c.In.Worktree.Branch
	}
	dir := c.In.Workspace.CurrentDir
	if dir == "" {
		dir = c.In.CWD
	}
	if branch == "" && dir != "" {
		branch = gitBranch(dir)
	}
	if branch == "" {
		return Segment{}
	}
	dirty := dir != "" && gitDirty(dir)
	ahead, behind := gitAheadBehind(dir)

	var b strings.Builder
	b.WriteString(branch)
	if dirty {
		b.WriteString("✱")
	}
	if ahead > 0 {
		fmt.Fprintf(&b, "↑%d", ahead)
	}
	if behind > 0 {
		fmt.Fprintf(&b, "↓%d", behind)
	}
	seg := c.Theme.SegOf("git")
	return Segment{Name: "git", Text: b.String(), FG: seg.FG, BG: seg.BG}
}

func gitBranch(dir string) string {
	if dir == "" || !isGitRepo(dir) {
		return ""
	}
	out, err := runGit(dir, 200*time.Millisecond, "branch", "--show-current")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func gitDirty(dir string) bool {
	if dir == "" || !isGitRepo(dir) {
		return false
	}
	out, _ := runGit(dir, 200*time.Millisecond, "status", "--porcelain")
	return strings.TrimSpace(out) != ""
}

func gitAheadBehind(dir string) (ahead, behind int) {
	if dir == "" || !isGitRepo(dir) {
		return 0, 0
	}
	out, err := runGit(dir, 200*time.Millisecond, "rev-list", "--left-right", "--count", "@{u}...HEAD")
	if err != nil {
		return 0, 0
	}
	parts := strings.Fields(out)
	if len(parts) != 2 {
		return 0, 0
	}
	fmt.Sscanf(parts[0], "%d", &behind)
	fmt.Sscanf(parts[1], "%d", &ahead)
	return ahead, behind
}

func isGitRepo(dir string) bool {
	cur := dir
	for cur != "/" && cur != "" {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return true
		}
		cur = filepath.Dir(cur)
	}
	return false
}

func runGit(dir string, timeout time.Duration, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// timeout via canal
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.Output()
		close(done)
	}()
	select {
	case <-done:
		return string(out), err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return "", fmt.Errorf("git timeout")
	}
}

// =============================================================================
// Model — display name do modelo atual.
// =============================================================================

type modelComp struct{}

func (modelComp) Name() string { return "model" }
func (modelComp) Render(c *RenderCtx, _ ComponentOpts) Segment {
	name := c.In.Model.DisplayName
	if name == "" {
		name = c.In.Model.ID
	}
	if name == "" {
		return Segment{}
	}
	seg := c.Theme.SegOf("model")
	return Segment{Name: "model", Text: name, FG: seg.FG, BG: seg.BG}
}

// =============================================================================
// Context % — bar + percentual com cor por severity.
// =============================================================================

type contextPctComp struct{}

func (contextPctComp) Name() string { return "context_pct" }
func (contextPctComp) Render(c *RenderCtx, opts ComponentOpts) Segment {
	pct := c.In.Context.UsedPercentage
	if pct == 0 {
		return Segment{}
	}
	sev := Classify(pct, opts.WarnAt, opts.CriticalAt)
	bar := progressBar(pct, 6)
	text := fmt.Sprintf("%s %.0f%%", bar, pct)
	seg := c.Theme.SegOf("context_pct")
	fg := seg.FG
	if sev != SevOK {
		fg = c.Theme.SeverityFG(sev)
	}
	return Segment{Name: "context_pct", Text: text, FG: fg, BG: seg.BG}
}

// =============================================================================
// Cost session — $ atual, com badge vs p90 quando histórico disponível.
// =============================================================================

type costSessionComp struct{}

func (costSessionComp) Name() string { return "cost_session" }
func (costSessionComp) Render(c *RenderCtx, opts ComponentOpts) Segment {
	cost := c.In.Cost.TotalCostUSD
	if cost == 0 && c.History != nil {
		cost = c.History.Session.CostUSD
	}
	if cost == 0 {
		return Segment{}
	}
	text := fmt.Sprintf("$%.2f", cost)
	sev := SevOK
	if c.History != nil && c.History.Project.P90Cost > 0 {
		ratio := cost / c.History.Project.P90Cost
		sev = Classify(ratio, opts.WarnAt, opts.CriticalAt)
		if sev != SevOK {
			text = fmt.Sprintf("$%.2f (%.1f×p90)", cost, ratio)
		}
	}
	seg := c.Theme.SegOf("cost_session")
	fg := seg.FG
	if sev != SevOK {
		fg = c.Theme.SeverityFG(sev)
	}
	return Segment{Name: "cost_session", Text: text, FG: fg, BG: seg.BG}
}

// =============================================================================
// Burn rate — tokens/min, do daemon (stdin não tem essa info).
// =============================================================================

type burnRateComp struct{}

func (burnRateComp) Name() string { return "burn_rate" }
func (burnRateComp) Render(c *RenderCtx, opts ComponentOpts) Segment {
	if c.History == nil || c.History.Session.BurnRateTPM == 0 {
		return Segment{}
	}
	tpm := c.History.Session.BurnRateTPM
	sev := Classify(tpm, opts.WarnAt, opts.CriticalAt)
	arrow := ""
	if tpm > 1000 {
		arrow = "⬆"
	}
	text := fmt.Sprintf("%.0f t/m%s", tpm, arrow)
	seg := c.Theme.SegOf("burn_rate")
	fg := seg.FG
	if sev != SevOK {
		fg = c.Theme.SeverityFG(sev)
	}
	return Segment{Name: "burn_rate", Text: text, FG: fg, BG: seg.BG}
}

// =============================================================================
// Cost today — soma do dia, do daemon.
// =============================================================================

type costTodayComp struct{}

func (costTodayComp) Name() string { return "cost_today" }
func (costTodayComp) Render(c *RenderCtx, _ ComponentOpts) Segment {
	if c.History == nil || c.History.Daily.CostUSD == 0 {
		return Segment{}
	}
	text := fmt.Sprintf("today $%.2f", c.History.Daily.CostUSD)
	seg := c.Theme.SegOf("cost_today")
	if seg.FG.Empty() {
		seg = c.Theme.Default
	}
	return Segment{Name: "cost_today", Text: text, FG: seg.FG, BG: seg.BG}
}

// =============================================================================
// Cost month — accum + projeção.
// =============================================================================

type costMonthComp struct{}

func (costMonthComp) Name() string { return "cost_month" }
func (costMonthComp) Render(c *RenderCtx, _ ComponentOpts) Segment {
	if c.History == nil || c.History.Monthly.Accumulated == 0 {
		return Segment{}
	}
	text := fmt.Sprintf("month $%.2f→$%.2f", c.History.Monthly.Accumulated, c.History.Monthly.Projection)
	seg := c.Theme.SegOf("cost_month")
	if seg.FG.Empty() {
		seg = c.Theme.Default
	}
	return Segment{Name: "cost_month", Text: text, FG: seg.FG, BG: seg.BG}
}

// =============================================================================
// Rate limits 5h / 7d — % + countdown pro reset.
// =============================================================================

type rate5hComp struct{}

func (rate5hComp) Name() string { return "rate_5h" }
func (rate5hComp) Render(c *RenderCtx, opts ComponentOpts) Segment {
	if c.In.RateLimits == nil || c.In.RateLimits.FiveHour == nil {
		return Segment{}
	}
	w := c.In.RateLimits.FiveHour
	if w.UsedPercentage == 0 && w.ResetsAt == 0 {
		return Segment{}
	}
	sev := Classify(w.UsedPercentage, opts.WarnAt, opts.CriticalAt)
	text := fmt.Sprintf("5h %.0f%%", w.UsedPercentage)
	if w.ResetsAt > 0 {
		left := time.Until(time.Unix(w.ResetsAt, 0))
		if left > 0 {
			text += fmt.Sprintf(" (%s)", humanDur(left))
		}
	}
	seg := c.Theme.SegOf("rate_5h")
	fg := seg.FG
	if sev != SevOK {
		fg = c.Theme.SeverityFG(sev)
	}
	return Segment{Name: "rate_5h", Text: text, FG: fg, BG: seg.BG}
}

type rate7dComp struct{}

func (rate7dComp) Name() string { return "rate_7d" }
func (rate7dComp) Render(c *RenderCtx, opts ComponentOpts) Segment {
	if c.In.RateLimits == nil || c.In.RateLimits.SevenDay == nil {
		return Segment{}
	}
	w := c.In.RateLimits.SevenDay
	if w.UsedPercentage == 0 {
		return Segment{}
	}
	sev := Classify(w.UsedPercentage, opts.WarnAt, opts.CriticalAt)
	text := fmt.Sprintf("7d %.0f%%", w.UsedPercentage)
	seg := c.Theme.SegOf("rate_7d")
	fg := seg.FG
	if sev != SevOK {
		fg = c.Theme.SeverityFG(sev)
	}
	return Segment{Name: "rate_7d", Text: text, FG: fg, BG: seg.BG}
}

func humanDur(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

// =============================================================================
// Ticket — auto-extrai TICKET-NNNN da branch (Jira/Linear style).
// =============================================================================

var ticketRE = regexp.MustCompile(`([A-Z]{2,8}-\d{1,6})`)

type ticketComp struct{}

func (ticketComp) Name() string { return "ticket" }
func (ticketComp) Render(c *RenderCtx, _ ComponentOpts) Segment {
	branch := ""
	if c.In.Worktree != nil {
		branch = c.In.Worktree.Branch
	}
	if branch == "" {
		dir := c.In.Workspace.CurrentDir
		if dir == "" {
			dir = c.In.CWD
		}
		branch = gitBranch(dir)
	}
	m := ticketRE.FindString(branch)
	if m == "" && c.History != nil {
		m = c.History.Project.Ticket
	}
	if m == "" {
		return Segment{}
	}
	seg := c.Theme.SegOf("ticket")
	return Segment{Name: "ticket", Text: m, FG: seg.FG, BG: seg.BG}
}

// =============================================================================
// Cluster — label do cluster AI desse projeto/session, do daemon.
// =============================================================================

type clusterComp struct{}

func (clusterComp) Name() string { return "cluster" }
func (clusterComp) Render(c *RenderCtx, _ ComponentOpts) Segment {
	if c.History == nil || c.History.Project.ClusterName == "" {
		return Segment{}
	}
	seg := c.Theme.SegOf("cluster")
	return Segment{Name: "cluster", Text: "~" + c.History.Project.ClusterName, FG: seg.FG, BG: seg.BG}
}

// =============================================================================
// Vim mode — NORMAL/INSERT.
// =============================================================================

type vimComp struct{}

func (vimComp) Name() string { return "vim_mode" }
func (vimComp) Render(c *RenderCtx, _ ComponentOpts) Segment {
	if c.In.Vim == nil || c.In.Vim.Mode == "" {
		return Segment{}
	}
	seg := c.Theme.SegOf("vim_mode")
	return Segment{Name: "vim_mode", Text: c.In.Vim.Mode, FG: seg.FG, BG: seg.BG, Bold: true}
}

// =============================================================================
// Lines changed — +X/-Y do stdin.
// =============================================================================

type linesChangedComp struct{}

func (linesChangedComp) Name() string { return "lines_changed" }
func (linesChangedComp) Render(c *RenderCtx, _ ComponentOpts) Segment {
	added := c.In.Cost.TotalLinesAdded
	removed := c.In.Cost.TotalLinesRemoved
	if added == 0 && removed == 0 {
		return Segment{}
	}
	text := fmt.Sprintf("+%d/-%d", added, removed)
	seg := c.Theme.Default
	return Segment{Name: "lines_changed", Text: text, FG: seg.FG, BG: seg.BG}
}

// =============================================================================
// Time — hh:mm.
// =============================================================================

type timeComp struct{}

func (timeComp) Name() string { return "time" }
func (timeComp) Render(c *RenderCtx, _ ComponentOpts) Segment {
	t := c.Now
	if t.IsZero() {
		t = time.Now()
	}
	return Segment{Name: "time", Text: t.Format("15:04"), FG: c.Theme.Muted}
}

// =============================================================================
// MCP status — count de servers conectados (placeholder até integrarmos).
// =============================================================================

type mcpComp struct{}

func (mcpComp) Name() string { return "mcp_status" }
func (mcpComp) Render(c *RenderCtx, _ ComponentOpts) Segment {
	// TODO: integrar com `claude mcp list` ou stdin quando disponível.
	return Segment{}
}

// =============================================================================
// init — registro de tudo.
// =============================================================================

func init() {
	Register(cwdComp{})
	Register(gitComp{})
	Register(modelComp{})
	Register(contextPctComp{})
	Register(costSessionComp{})
	Register(burnRateComp{})
	Register(costTodayComp{})
	Register(costMonthComp{})
	Register(rate5hComp{})
	Register(rate7dComp{})
	Register(ticketComp{})
	Register(clusterComp{})
	Register(vimComp{})
	Register(linesChangedComp{})
	Register(timeComp{})
	Register(mcpComp{})
}

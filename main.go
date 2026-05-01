package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"context"

	"github.com/felipeness/nessy/internal/ai"
	"github.com/felipeness/nessy/internal/branding"
	"github.com/felipeness/nessy/internal/config"
	"github.com/felipeness/nessy/internal/index"
	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/parser"
	"github.com/felipeness/nessy/internal/pricing"
	"github.com/felipeness/nessy/internal/server"
	"github.com/felipeness/nessy/internal/statusline"
	"github.com/felipeness/nessy/internal/watch"
	"github.com/felipeness/nessy/tui"
)

const usage = `nessy — busca todas as suas conversas do Claude Code

USAGE:
  nessy list [--json|--tsv]   lista todas as sessions (default: tabela)
  nessy fzf                    abre fzf interativo, Enter retoma a session
  nessy show <session-id>      mostra detalhes de uma session
  nessy tui                    TUI Bubble Tea com tabs Search/Recent/Stats
  nessy serve [--port N]       sobe Web UI local em http://localhost:5555
  nessy statusline-render      consome stdin do Claude Code, escreve linha ANSI
  nessy statusline-install     escreve statusLine no ~/.claude/settings.json
  nessy statusline-preview     mostra o statusline com mock data no terminal

QUERY (Fase 7a — saída human-readable, ou JSON com --json):
  nessy similar <q> [--n 5]    top sessions parecidas via embedding
  nessy search <q> [--mode]    busca hybrid/body/meta (--all sem dedup)
  nessy ask <q>                pergunta pro Ness IA (RAG completo)
  nessy insights [--type X]    lista insights gerados (advisor)
  nessy knowledge <id>         problem/solution/decisions de 1 session
  nessy aggregated             top patterns/decisions/problemas cross-session
  nessy project <path>         p90, tech, top tools de 1 projeto
  nessy standup [--since 7d]   markdown timeline|project|editorial

MCP (Fase 8):
  nessy mcp                    sobe MCP server em stdio (chamado pelo Claude Code)
  nessy mcp-install [--force]  registra em ~/.claude/settings.json mcpServers
                  [--uninstall]

DAEMON (Fase 9):
  nessy daemon-install         instala launchd plist pra subir 'nessy serve' no login
  nessy daemon-uninstall       remove launchd plist
  nessy daemon-status          mostra estado do daemon

EXAMPLES:
  nessy list
  nessy list --json | jq '.[] | select(.git_branch == "main")'
  nessy fzf
  nessy tui
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
	switch os.Args[1] {
	case "list":
		cmdList(os.Args[2:])
	case "fzf":
		cmdFzf()
	case "show":
		cmdShow(os.Args[2:])
	case "tui":
		cmdTui(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "statusline-render":
		cmdStatuslineRender()
	case "statusline-install":
		cmdStatuslineInstall(os.Args[2:])
	case "statusline-preview":
		cmdStatuslinePreview(os.Args[2:])
	case "similar":
		cmdSimilar(os.Args[2:])
	case "search":
		cmdSearchCLI(os.Args[2:])
	case "ask":
		cmdAsk(os.Args[2:])
	case "insights":
		cmdInsightsCLI(os.Args[2:])
	case "knowledge":
		cmdKnowledgeCLI(os.Args[2:])
	case "aggregated":
		cmdAggregatedCLI(os.Args[2:])
	case "project":
		cmdProjectCLI(os.Args[2:])
	case "standup":
		cmdStandupCLI(os.Args[2:])
	case "mcp":
		cmdMCPServe()
	case "mcp-install":
		cmdMCPInstall(os.Args[2:])
	case "daemon-install":
		cmdDaemonInstall()
	case "daemon-uninstall":
		cmdDaemonUninstall()
	case "daemon-status":
		cmdDaemonStatus()
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}
}

func cmdList(args []string) {
	format := "table"
	for _, a := range args {
		switch a {
		case "--json":
			format = "json"
		case "--tsv":
			format = "tsv"
		}
	}
	sessions, err := loadSorted()
	if err != nil {
		fatal(err)
	}
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(sessions)
	case "tsv":
		for _, s := range sessions {
			home, _ := os.UserHomeDir()
			dir := strings.Replace(s.ProjectDir, home, "~", 1)
			fmt.Printf("%s\t%s\t%s\t%s\t%d\t%s\n",
				s.StartTime.Format("2006-01-02 15:04"),
				s.SessionID,
				dir,
				orDash(s.GitBranch),
				s.MessageCount,
				s.FirstUserMsg,
			)
		}
	default:
		printTable(sessions)
	}
}

func cmdShow(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nessy show <session-id>")
		os.Exit(1)
	}
	id := args[0]
	sessions, err := loadSorted()
	if err != nil {
		fatal(err)
	}
	for _, s := range sessions {
		if s.SessionID == id || strings.HasPrefix(s.SessionID, id) {
			home, _ := os.UserHomeDir()
			dir := strings.Replace(s.ProjectDir, home, "~", 1)
			fmt.Printf("Session:    %s\n", s.SessionID)
			fmt.Printf("Pasta:      %s\n", dir)
			fmt.Printf("Branch:     %s\n", orDash(s.GitBranch))
			fmt.Printf("Versão:     %s\n", orDash(s.ClaudeVersion))
			fmt.Printf("Início:     %s\n", s.StartTime.Local().Format("2006-01-02 15:04:05"))
			fmt.Printf("Fim:        %s\n", s.EndTime.Local().Format("2006-01-02 15:04:05"))
			dur := s.EndTime.Sub(s.StartTime)
			fmt.Printf("Duração:    %s\n", dur.Round(1e9))
			fmt.Printf("Msgs:       %d total (user: %d, assistant: %d)\n",
				s.MessageCount, s.UserMessages, s.AssistantMessages)
			if len(s.ToolCalls) > 0 {
				fmt.Printf("\nTools usados:\n")
				type kv struct{ k string; v int }
				var pairs []kv
				for k, v := range s.ToolCalls {
					pairs = append(pairs, kv{k, v})
				}
				sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
				for _, p := range pairs {
					fmt.Printf("  %-20s %d\n", p.k, p.v)
				}
			}
			fmt.Printf("\nPrimeira msg do user:\n  %s\n", s.FirstUserMsg)
			if s.LastUserMsg != "" && s.LastUserMsg != s.FirstUserMsg {
				fmt.Printf("\nÚltima msg do user:\n  %s\n", s.LastUserMsg)
			}
			fmt.Printf("\nPara retomar:\n  cd %q && claude --resume %s\n", s.ProjectDir, s.SessionID)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "session %q não encontrada\n", id)
	os.Exit(1)
}

func cmdFzf() {
	if _, err := exec.LookPath("fzf"); err != nil {
		fatal(fmt.Errorf("fzf não encontrado no PATH — instala via mise: mise use -g fzf"))
	}
	self, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	sessions, err := loadSorted()
	if err != nil {
		fatal(err)
	}
	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, "nenhuma session encontrada em ~/.claude/projects/")
		return
	}
	home, _ := os.UserHomeDir()

	// build fzf input: tab-separated, sessionId hidden in column 2
	var lines []string
	for _, s := range sessions {
		dir := strings.Replace(s.ProjectDir, home, "~", 1)
		if len(dir) > 40 {
			dir = "…" + dir[len(dir)-39:]
		}
		preview := s.FirstUserMsg
		if len(preview) > 70 {
			preview = preview[:69] + "…"
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%-40s\t%-12s\t%4d msg\t%s",
			s.StartTime.Local().Format("2006-01-02 15:04"),
			s.SessionID,
			dir,
			orDash(s.GitBranch),
			s.MessageCount,
			preview,
		))
	}

	cmd := exec.Command("fzf",
		"--ansi",
		"--delimiter", "\t",
		"--with-nth", "1,3,4,5,6",
		"--header", "Enter: retomar | Ctrl-O: abrir pasta | Ctrl-D: ver detalhes",
		"--preview", fmt.Sprintf("%s show {2}", self),
		"--preview-window", "right:55%:wrap",
		"--bind", fmt.Sprintf("ctrl-d:execute(%s show {2} | less -R)", self),
		"--bind", "ctrl-o:execute-silent(open $(echo {3} | sed 's|~|'$HOME'|'))",
		"--height", "90%",
		"--layout", "reverse",
	)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		// user pressed Esc → exit code 130, silent
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
			return
		}
		fatal(err)
	}
	selected := strings.TrimSpace(string(out))
	if selected == "" {
		return
	}
	parts := strings.Split(selected, "\t")
	if len(parts) < 3 {
		return
	}
	sessionID := parts[1]
	// expand ~
	dir := parts[2]
	dir = strings.Replace(dir, "~", home, 1)
	dir = strings.TrimLeft(dir, "…")
	// find original full path from sessions
	for _, s := range sessions {
		if s.SessionID == sessionID {
			dir = s.ProjectDir
			break
		}
	}
	resume(dir, sessionID)
}

func resume(dir, sessionID string) {
	if _, err := os.Stat(dir); err != nil {
		fmt.Fprintf(os.Stderr, "pasta %q não existe mais (%v)\n", dir, err)
		os.Exit(1)
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(os.Stderr, "binário 'claude' não encontrado no PATH")
		fmt.Fprintf(os.Stderr, "tente manualmente:\n  cd %q && claude --resume %s\n", dir, sessionID)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "→ cd %s && claude --resume %s\n", dir, sessionID)
	cmd := exec.Command(claude, "--resume", sessionID)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}

const defaultPricingTOML = `default_currency = "USD"
brl_rate = 5.20

[[models]]
name = "claude-sonnet-4-6"
input_per_mtok = 3.00
output_per_mtok = 15.00
cache_creation_per_mtok = 3.75
cache_read_per_mtok = 0.30

[[models]]
name = "claude-opus-4-7"
input_per_mtok = 15.00
output_per_mtok = 75.00
cache_creation_per_mtok = 18.75
cache_read_per_mtok = 1.50

[[models]]
name = "claude-haiku-4-5"
input_per_mtok = 1.00
output_per_mtok = 5.00
cache_creation_per_mtok = 1.25
cache_read_per_mtok = 0.10
`

func cmdTui(args []string) {
	noAI := false
	aiModelOverride := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--no-ai":
			noAI = true
		case "--ai-model":
			if i+1 < len(args) {
				aiModelOverride = args[i+1]
				i++
			}
		}
	}
	_ = noAI
	_ = aiModelOverride
	cmdTuiInternal(noAI, aiModelOverride)
}

func cmdTuiInternal(noAI bool, aiModelOverride string) {
	home, _ := os.UserHomeDir()
	cacheDir := branding.CacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		fatal(err)
	}
	dbPath := filepath.Join(cacheDir, "index.db")
	pricingPath := filepath.Join(cacheDir, "pricing.toml")
	configPath := filepath.Join(cacheDir, "config.toml")
	statePath := filepath.Join(cacheDir, "state.toml")

	if _, err := os.Stat(pricingPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(pricingPath, []byte(defaultPricingTOML), 0644); err != nil {
			fatal(err)
		}
	}

	db, err := index.Open(dbPath)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	if _, err := db.Reindex(filepath.Join(home, ".claude", "projects")); err != nil {
		fmt.Fprintln(os.Stderr, "reindex error:", err)
	}

	p, err := pricing.Load(pricingPath)
	if err != nil {
		fatal(err)
	}

	cfg, _ := config.LoadConfig(configPath)
	state := config.LoadState(statePath)

	aiDeps := tui.AIDeps{}
	if cfg.AI.Enabled {
		client := ai.NewClient(cfg.AI.OllamaURL)
		worker := ai.NewWorker(db, client, cfg.AI.GenModel, cfg.AI.EmbedModel, nil)
		aiDeps = tui.AIDeps{
			Enabled:    true,
			Client:     client,
			Worker:     worker,
			GenModel:   cfg.AI.GenModel,
			EmbedModel: cfg.AI.EmbedModel,
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go worker.Run(ctx)
		if cfg.AI.AutoGenerate && client.Health(ctx) {
			go func() {
				all, _ := db.ListSessions()
				for _, sess := range all {
					c, err := db.AICacheGet(sess.SessionID)
					if err == nil && c.Summary != "" && c.JSONLMtime == sess.JSONLMtime.UnixNano() {
						continue
					}
					worker.Enqueue(sess.SessionID)
				}
			}()
		}
	}

	prog := tea.NewProgram(tui.New(db, p, cfg, state, statePath, aiDeps), tea.WithAltScreen())
	finalModel, err := prog.Run()
	if err != nil {
		fatal(err)
	}
	// Se user apertou Enter pra retomar uma session, executa AGORA — depois
	// que Bubble Tea liberou o TTY. Bubble Tea + subprocess simultâneos
	// brigavam pelo terminal e o claude --resume saía vazio.
	if final, ok := finalModel.(tui.Model); ok {
		if s := final.PendingResume(); s != nil {
			resume(s.ProjectDir, s.SessionID)
		}
	}
}

func cmdServe(args []string) {
	port := 5555
	listenFlag := ""
	openBrowser := true
	noAI := false
	aiModelOverride := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port", "-p":
			if i+1 < len(args) {
				p, err := strconv.Atoi(args[i+1])
				if err == nil {
					port = p
				}
				i++
			}
		case "--listen":
			if i+1 < len(args) {
				listenFlag = args[i+1]
				i++
			}
		case "--no-open":
			openBrowser = false
		case "--no-ai":
			noAI = true
		case "--ai-model":
			if i+1 < len(args) {
				aiModelOverride = args[i+1]
				i++
			}
		}
	}
	listen := listenFlag
	if listen == "" {
		listen = fmt.Sprintf("127.0.0.1:%d", port)
	}
	// LAN warning
	if !strings.HasPrefix(listen, "127.") && !strings.HasPrefix(listen, "localhost") {
		fmt.Fprintf(os.Stderr, "⚠ %s expõe na rede. Qualquer um na sua LAN poderá ler suas conversas.\nContinuar? [y/N] ", listen)
		var resp string
		fmt.Scanln(&resp)
		if strings.ToLower(strings.TrimSpace(resp)) != "y" {
			fmt.Fprintln(os.Stderr, "abortado")
			return
		}
	}

	home, _ := os.UserHomeDir()
	cacheDir := branding.CacheDir()
	_ = os.MkdirAll(cacheDir, 0755)
	dbPath := filepath.Join(cacheDir, "index.db")
	pricingPath := filepath.Join(cacheDir, "pricing.toml")

	if _, err := os.Stat(pricingPath); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile(pricingPath, []byte(defaultPricingTOML), 0644)
	}

	db, err := index.Open(dbPath)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	if _, err := db.Reindex(filepath.Join(home, ".claude", "projects")); err != nil {
		fmt.Fprintln(os.Stderr, "reindex error:", err)
	}

	p, err := pricing.Load(pricingPath)
	if err != nil {
		fatal(err)
	}

	cfg, _ := config.LoadConfig(filepath.Join(cacheDir, "config.toml"))
	if noAI {
		cfg.AI.Enabled = false
	}
	if aiModelOverride != "" {
		cfg.AI.GenModel = aiModelOverride
	}

	hub := server.NewHub()
	srv := &server.Server{
		DB:      db,
		Pricing: p,
		Hub:     hub,
		Static:  staticHandler(),
	}

	if cfg.AI.Enabled {
		client := ai.NewClient(cfg.AI.OllamaURL)
		worker := ai.NewWorker(db, client, cfg.AI.GenModel, cfg.AI.EmbedModel, hub)
		srv.AIEnabled = true
		srv.AIClient = client
		srv.AIWorker = worker
		srv.GenModel = cfg.AI.GenModel
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go worker.Run(ctx)

		// kick off auto-generate em background se Ollama estiver up
		if cfg.AI.AutoGenerate && client.Health(ctx) {
			go func() {
				all, _ := db.ListSessions()
				for _, sess := range all {
					c, err := db.AICacheGet(sess.SessionID)
					if err == nil && c.Summary != "" && c.JSONLMtime == sess.JSONLMtime.UnixNano() {
						continue
					}
					worker.Enqueue(sess.SessionID)
				}
			}()
		}
	}

	// Watcher de background — detectores de loop em sessions ativas.
	// Roda em goroutine; ctx é cancelado se o serve sair.
	watcherCtx, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	watcher := watch.New(filepath.Join(home, ".claude", "projects"), watch.Config{}, nil)
	go watcher.Run(watcherCtx)

	if err := server.Run(srv, listen, openBrowser); err != nil {
		fatal(err)
	}
}

// staticHandler retorna o handler do frontend embeddado, ou nil se ainda não
// existe (dev mode — vite dev é proxy externo).
func staticHandler() http.Handler {
	return webStatic // declared in embed.go (build tag dependent)
}

func loadSorted() ([]*model.Session, error) {
	sessions, err := parser.ListSessions()
	if err != nil {
		return nil, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartTime.After(sessions[j].StartTime)
	})
	return sessions, nil
}

func printTable(sessions []*model.Session) {
	if len(sessions) == 0 {
		fmt.Println("Nenhuma session em ~/.claude/projects/")
		return
	}
	home, _ := os.UserHomeDir()
	fmt.Printf("%-16s  %-8s  %-40s  %-12s  %5s  %s\n",
		"DATA", "ID", "PASTA", "BRANCH", "MSGS", "PREVIEW")
	fmt.Println(strings.Repeat("─", 140))
	for _, s := range sessions {
		dir := strings.Replace(s.ProjectDir, home, "~", 1)
		if len(dir) > 40 {
			dir = "…" + dir[len(dir)-39:]
		}
		preview := s.FirstUserMsg
		if len(preview) > 60 {
			preview = preview[:59] + "…"
		}
		fmt.Printf("%-16s  %-8s  %-40s  %-12s  %5d  %s\n",
			s.StartTime.Local().Format("2006-01-02 15:04"),
			s.SessionID[:8],
			dir,
			orDash(s.GitBranch),
			s.MessageCount,
			preview,
		)
	}
	_ = filepath.Separator
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// cmdStatuslineRender é o handler chamado pelo Claude Code via settings.json:
//   { "statusLine": { "type": "command", "command": "nessy statusline-render" } }
//
// Lê o JSON do stdin, carrega ~/.nessy/statusline.toml (ou default),
// renderiza, escreve uma linha em stdout. Falha silenciosa em qualquer erro
// pra não quebrar o terminal do user.
func cmdStatuslineRender() {
	cfgPath := filepath.Join(branding.CacheDir(), "statusline.toml")
	cfg, err := statusline.LoadConfig(cfgPath)
	if err != nil {
		// silencia — statusline não deve nunca quebrar a UX do Claude Code.
		return
	}

	var in statusline.Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		// stdin malformed → não imprime nada (Claude Code mostra blank).
		return
	}

	out := statusline.Render(&in, cfg)
	fmt.Println(out)
}

// cmdStatuslineInstall escreve a entrada statusLine no settings.json e cria
// o config TOML default em ~/.nessy/statusline.toml.
//
// Flags:
//   --preset <name>      compact | max | powerline (default compact)
//   --refresh <seconds>  1-60 pra interval-driven (default event-driven)
//   --force              sobrescreve statusLine existente sem perguntar
//   --uninstall          remove a entrada statusLine
func cmdStatuslineInstall(args []string) {
	preset := "compact"
	refresh := 0
	force := false
	uninstall := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--preset":
			if i+1 < len(args) {
				preset = args[i+1]
				i++
			}
		case "--refresh":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil {
					refresh = n
				}
				i++
			}
		case "--force", "-f":
			force = true
		case "--uninstall":
			uninstall = true
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fatal(err)
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	if uninstall {
		removed, backup, err := statusline.Uninstall(settingsPath)
		if err != nil {
			fatal(err)
		}
		if !removed {
			fmt.Println("settings.json não tinha statusLine — nada a remover")
			return
		}
		fmt.Printf("✓ statusLine removido de %s\n", settingsPath)
		fmt.Printf("  backup: %s\n", backup)
		return
	}

	// Resolve binary path absoluto pra colocar no command
	self, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	cmd := self + " statusline-render"

	// Escreve config TOML default se não existir
	cfgPath := filepath.Join(branding.CacheDir(), "statusline.toml")
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		cfg := statusline.Presets[preset]
		if cfg == nil {
			cfg = statusline.DefaultConfig()
		}
		if err := statusline.SaveConfig(cfgPath, cfg); err != nil {
			fatal(fmt.Errorf("save config: %w", err))
		}
		fmt.Printf("✓ config criado em %s (preset: %s)\n", cfgPath, preset)
	} else {
		fmt.Printf("✓ config já existe em %s — preservado\n", cfgPath)
	}

	res, err := statusline.Install(statusline.InstallOptions{
		SettingsPath:    settingsPath,
		Command:         cmd,
		RefreshInterval: refresh,
		Force:           force,
	})
	if err != nil {
		fatal(err)
	}
	if res.Backup != "" {
		fmt.Printf("✓ backup: %s\n", res.Backup)
	}
	if res.Replaced {
		fmt.Println("⚠ statusLine anterior foi sobrescrito")
	}
	fmt.Printf("✓ statusLine instalado em %s\n", settingsPath)
	fmt.Printf("  command: %s\n", cmd)
	if refresh > 0 {
		fmt.Printf("  refresh: %ds\n", refresh)
	}
	fmt.Println()
	fmt.Println("Próximos passos:")
	fmt.Println("  1. Suba o daemon (em outra aba):  nessy serve --no-open")
	fmt.Println("  2. Reinicie o Claude Code         (statusLine só carrega no boot)")
	fmt.Println("  3. Edite o config em:             " + cfgPath)
}

// cmdStatuslinePreview renderiza o statusline com dados mock pra ver no terminal.
// Útil pra testar themes/components sem precisar reiniciar Claude Code.
//
// Flags:
//   --theme <name>     graphite|nord|dracula|sakura|mono
//   --style <name>     plain|powerline|capsule
//   --all              renderiza TODAS combinações theme×style
func cmdStatuslinePreview(args []string) {
	theme := ""
	style := ""
	all := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--theme":
			if i+1 < len(args) {
				theme = args[i+1]
				i++
			}
		case "--style":
			if i+1 < len(args) {
				style = args[i+1]
				i++
			}
		case "--all":
			all = true
		}
	}

	mock := mockInput()
	cfgPath := filepath.Join(branding.CacheDir(), "statusline.toml")
	cfg, _ := statusline.LoadConfig(cfgPath)

	if all {
		styles := []string{"plain", "powerline", "capsule"}
		themes := []string{"graphite", "nord", "dracula", "sakura", "mono"}
		for _, t := range themes {
			for _, st := range styles {
				cfg.Theme = t
				cfg.Style = st
				fmt.Printf("─ %s · %s\n", t, st)
				fmt.Println(statusline.Render(mock, cfg))
				fmt.Println()
			}
		}
		return
	}
	if theme != "" {
		cfg.Theme = theme
	}
	if style != "" {
		cfg.Style = style
	}
	fmt.Println(statusline.Render(mock, cfg))
}

// mockInput devolve um Input plausível pra preview/testes.
func mockInput() *statusline.Input {
	return &statusline.Input{
		CWD:       "/Users/dev/projects/my-app",
		SessionID: "preview-mock",
		Model: statusline.ModelInfo{
			DisplayName: "Opus 4.7",
			ID:          "claude-opus-4-7",
		},
		Workspace: statusline.Workspace{
			CurrentDir: "/Users/dev/projects/my-app",
			ProjectDir: "/Users/dev/projects/my-app",
		},
		Context: func() statusline.ContextWindow {
			c := statusline.ContextWindow{
				UsedPercentage:    42,
				TotalInputTokens:  18432,
				TotalOutputTokens: 4521,
			}
			return c
		}(),
		Cost: statusline.CostInfo{
			TotalCostUSD:      0.32,
			TotalLinesAdded:   45,
			TotalLinesRemoved: 12,
		},
		RateLimits: &statusline.RateLimits{
			FiveHour: &statusline.RateLimitWindow{UsedPercentage: 73, ResetsAt: 0},
			SevenDay: &statusline.RateLimitWindow{UsedPercentage: 18},
		},
		Worktree: &statusline.WorktreeInfo{Branch: "feat/CC-1234-statusline"},
	}
}

// =============================================================================
// Daemon — launchd integration pra macOS
// =============================================================================

const launchdLabel = "com.felipe-coelho.nessy"

func launchdPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

func renderPlist(binPath string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + launchdLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + binPath + `</string>
		<string>serve</string>
		<string>--no-open</string>
	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>/tmp/nessy.log</string>
	<key>StandardErrorPath</key><string>/tmp/nessy.err</string>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key><string>/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin</string>
	</dict>
</dict>
</plist>
`
}

func cmdDaemonInstall() {
	binPath, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	plistPath := launchdPlistPath()
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(plistPath, []byte(renderPlist(binPath)), 0644); err != nil {
		fatal(err)
	}
	// Carrega no launchd. Tolera "already loaded" — re-carrega via unload+load.
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "launchctl load: %v\n%s\n", err, out)
		os.Exit(1)
	}
	fmt.Printf("✓ daemon instalado: %s\n", plistPath)
	fmt.Println("  binário:", binPath)
	fmt.Println("  logs:    /tmp/nessy.log /tmp/nessy.err")
	fmt.Println("  Web UI:  http://localhost:5555")
	fmt.Println()
	fmt.Println("Reinicia agora:")
	fmt.Println("  launchctl kickstart -k gui/$UID/" + launchdLabel)
}

func cmdDaemonUninstall() {
	plistPath := launchdPlistPath()
	if _, err := os.Stat(plistPath); errors.Is(err, os.ErrNotExist) {
		fmt.Println("daemon não está instalado")
		return
	}
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := os.Remove(plistPath); err != nil {
		fatal(err)
	}
	fmt.Printf("✓ daemon removido: %s\n", plistPath)
}

func cmdDaemonStatus() {
	plistPath := launchdPlistPath()
	if _, err := os.Stat(plistPath); errors.Is(err, os.ErrNotExist) {
		fmt.Println("daemon: NÃO INSTALADO")
		fmt.Println("  rode: nessy daemon-install")
		return
	}
	out, _ := exec.Command("launchctl", "list", launchdLabel).CombinedOutput()
	fmt.Printf("plist: %s\n\n", plistPath)
	fmt.Print(string(out))
	// Bate na porta pra ver se tá saudável
	resp, err := http.Get("http://127.0.0.1:5555/health")
	if err != nil {
		fmt.Println("\nhealth: SEM RESPOSTA (daemon pode não estar pronto)")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		fmt.Println("\nhealth: OK (http://127.0.0.1:5555)")
	} else {
		fmt.Printf("\nhealth: status %d\n", resp.StatusCode)
	}
}

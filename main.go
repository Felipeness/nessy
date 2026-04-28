package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/felipeness/claude-history/internal/parser"
)

const usage = `claude-history — busca todas as suas conversas do Claude Code

USAGE:
  claude-history list [--json|--tsv]   lista todas as sessions (default: tabela)
  claude-history fzf                    abre fzf interativo, Enter retoma a session
  claude-history show <session-id>      mostra detalhes de uma session
  claude-history serve [--port N]       sobe API HTTP + web UI (próxima fase)
  claude-history tui                    TUI Bubble Tea (próxima fase)

EXAMPLES:
  claude-history list
  claude-history list --json | jq '.[] | select(.git_branch == "main")'
  claude-history fzf
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
		fmt.Fprintln(os.Stderr, "usage: claude-history show <session-id>")
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
				s.MessageCount, s.UserMessages, s.AssistantMsgs)
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

func loadSorted() ([]*parser.Session, error) {
	sessions, err := parser.ListSessions()
	if err != nil {
		return nil, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartTime.After(sessions[j].StartTime)
	})
	return sessions, nil
}

func printTable(sessions []*parser.Session) {
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

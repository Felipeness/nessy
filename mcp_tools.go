// Tools MCP que wrappa as funcs do cli.go pra responder via Model Context
// Protocol. Cada tool tem description afiada (use case + when-to-use) — é
// o que o LLM lê pra decidir quando invocar. Tools mal-descritas = LLM ignora.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/felipeness/claude-history/internal/ai"
	"github.com/felipeness/claude-history/internal/index"
	"github.com/felipeness/claude-history/internal/mcp"
	"github.com/felipeness/claude-history/internal/model"
)

// registerTools registra as 8 tools no server MCP. Cada handler abre o DB
// (loadCLICtx), executa, fecha. Não compartilha estado entre calls — MCP
// requests são curtos e independentes.
func registerTools(s *mcp.Server) {
	s.Register(mcp.Tool{
		Name: "claude_history_similar",
		Description: "Find past Claude Code sessions similar to a query, ranked by " +
			"semantic similarity. USE WHEN: the user mentions 'doing similar work before', " +
			"asks 'how did I solve X last time', or starts a task that resembles past work. " +
			"Returns top-N session_ids with summary, project_dir, branch, similarity score.",
		InputSchema: schemaObject(map[string]any{
			"query": schemaString("Description of the task or topic to find similar past work."),
			"n":     schemaInt("Max results", 5),
		}, "query"),
	}, handleSimilar)

	s.Register(mcp.Tool{
		Name: "claude_history_search",
		Description: "Search the user's Claude Code history. USE WHEN: looking for sessions " +
			"with specific keywords (path, branch, message content, tool names). Modes: " +
			"'hybrid' (default) combines metadata + full-text; 'body' only message content " +
			"via FTS5; 'meta' only path/branch/msg; 'sim' semantic via embeddings.",
		InputSchema: schemaObject(map[string]any{
			"query": schemaString("Search query — supports filters like 'project:X branch:Y since:7d cost:>1'."),
			"mode":  schemaStringEnum("Search mode", []string{"hybrid", "body", "meta", "sim"}, "hybrid"),
		}, "query"),
	}, handleSearchTool)

	s.Register(mcp.Tool{
		Name: "claude_history_ask",
		Description: "Ask 'Ness IA' — the user's second brain — any question about their " +
			"development history. Uses RAG over all sessions with extracted knowledge. " +
			"USE WHEN: user asks how they solved something before, what decisions they " +
			"made, what patterns they use, or what's still unfinished. Returns answer " +
			"citing session_ids in [brackets] from the user's actual history; falls back " +
			"to general knowledge marked [geral] only when history doesn't cover.",
		InputSchema: schemaObject(map[string]any{
			"question": schemaString("Question about the user's coding history."),
		}, "question"),
	}, handleAskTool)

	s.Register(mcp.Tool{
		Name: "claude_history_insights",
		Description: "List automated insights about the user's coding patterns: repeated " +
			"tasks, chronic problems, script opportunities, token waste, anti-patterns, " +
			"personal patterns. USE WHEN: user wants to understand their own workflow, " +
			"asks 'what should I improve', or wants automation suggestions.",
		InputSchema: schemaObject(map[string]any{
			"type": schemaStringEnum("Filter by insight type",
				[]string{"repeated_task", "chronic_problem", "script_opportunity",
					"token_waste", "performance_hint", "anti_pattern", "personal_pattern"}, ""),
		}),
	}, handleInsightsTool)

	s.Register(mcp.Tool{
		Name: "claude_history_knowledge",
		Description: "Get the extracted 'knowledge' from a specific session: problem, " +
			"solution, decisions with rationale, learnings, code patterns, tech used, " +
			"open questions. USE WHEN: drilling into a specific session referenced by " +
			"session_id (8-char prefix or full UUID).",
		InputSchema: schemaObject(map[string]any{
			"session_id": schemaString("Session ID, full UUID or 8-char prefix."),
		}, "session_id"),
	}, handleKnowledgeTool)

	s.Register(mcp.Tool{
		Name: "claude_history_aggregated",
		Description: "Cross-session knowledge aggregate: top code patterns, decision " +
			"timeline, recurring problems, tech frequency, open questions across all " +
			"sessions. USE WHEN: user wants overview of their work patterns, recurring " +
			"problems they keep hitting, or what's still in their backlog.",
		InputSchema: schemaObject(nil),
	}, handleAggregatedTool)

	s.Register(mcp.Tool{
		Name: "claude_history_project",
		Description: "Get stats for a specific project directory: total sessions, total " +
			"cost, p90 cost/tokens, detected tech stack, top tools used, most-recent " +
			"ticket pattern. USE WHEN: about to start work on a project, want to " +
			"understand its history before refactoring, or estimate effort.",
		InputSchema: schemaObject(map[string]any{
			"path": schemaString("Absolute path to the project directory."),
		}, "path"),
	}, handleProjectTool)

	s.Register(mcp.Tool{
		Name: "claude_history_standup",
		Description: "Generate a standup-style markdown summary of recent work. Three " +
			"formats: 'editorial' (default — Concluído/Decisões/Em aberto sections using " +
			"extracted knowledge), 'timeline' (chronological by day/hour), 'project' " +
			"(grouped by project_dir with cost). USE WHEN: user asks 'what did I do " +
			"last week', preparing for daily/weekly standups.",
		InputSchema: schemaObject(map[string]any{
			"since":  schemaString("Time window: '7d', '24h', '30m'."),
			"format": schemaStringEnum("Output format", []string{"editorial", "timeline", "project"}, "editorial"),
		}),
	}, handleStandupTool)
}

// =============================================================================
// Schema helpers — reduzem boilerplate de JSON Schema.
// =============================================================================

func schemaObject(props map[string]any, required ...string) map[string]any {
	out := map[string]any{"type": "object"}
	if props == nil {
		props = map[string]any{}
	}
	out["properties"] = props
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func schemaString(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func schemaStringEnum(desc string, values []string, def string) map[string]any {
	out := map[string]any{
		"type":        "string",
		"description": desc,
		"enum":        values,
	}
	if def != "" {
		out["default"] = def
	}
	return out
}

func schemaInt(desc string, def int) map[string]any {
	return map[string]any{
		"type":        "integer",
		"description": desc,
		"default":     def,
	}
}

// =============================================================================
// Tool handlers — cada um abre cliCtx, executa, devolve texto humano-legível.
// =============================================================================

func handleSimilar(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Query string `json:"query"`
		N     int    `json:"n"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Query == "" {
		return "", fmt.Errorf("query required")
	}
	if args.N == 0 {
		args.N = 5
	}
	cli, err := loadCLICtx(false)
	if err != nil {
		return "", err
	}
	defer cli.db.Close()
	if cli.aiClient == nil {
		return "", fmt.Errorf("ollama not available — start with `ollama serve`")
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	queryEmb, err := cli.aiClient.Embedding(cctx, cli.embedModel, args.Query)
	if err != nil {
		return "", err
	}
	caches, _ := cli.db.AICacheList()
	type ranked struct {
		c   *index.AICache
		sim float64
	}
	var hits []ranked
	for _, c := range caches {
		if len(c.Embedding) == 0 {
			continue
		}
		sim := cosineSim32(queryEmb, ai.DecodeEmbedding(c.Embedding))
		if sim > 0 {
			hits = append(hits, ranked{c, sim})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].sim > hits[j].sim })
	if len(hits) > args.N {
		hits = hits[:args.N]
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "Top %d sessions similar to %q:\n\n", len(hits), args.Query)
	for _, h := range hits {
		s, _ := cli.db.GetByID(h.c.SessionID)
		dir, br := "", ""
		if s != nil {
			dir = strings.Replace(s.ProjectDir, mustHome(), "~", 1)
			br = s.GitBranch
		}
		fmt.Fprintf(&b, "[%s] similarity=%.2f branch=%s dir=%s\n  %s\n\n",
			h.c.SessionID[:8], h.sim, br, dir, h.c.Summary)
	}
	return b.String(), nil
}

func handleSearchTool(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Query string `json:"query"`
		Mode  string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Query == "" {
		return "", fmt.Errorf("query required")
	}
	if args.Mode == "" {
		args.Mode = "hybrid"
	}
	cli, err := loadCLICtx(true)
	if err != nil {
		return "", err
	}
	defer cli.db.Close()
	all, _ := cli.db.ListSessions()
	caches, _ := cli.db.AICacheList()
	summaries := map[string]string{}
	for _, c := range caches {
		if c.Summary != "" {
			summaries[c.SessionID] = c.Summary
		}
	}
	var results []searchHitCLI
	switch args.Mode {
	case "body":
		results = searchFTSCli(cli.db, args.Query, all, false)
	case "meta":
		results = searchMetaCli(args.Query, all, summaries)
	default:
		results = append(searchMetaCli(args.Query, all, summaries),
			searchFTSCli(cli.db, args.Query, all, false)...)
	}
	if len(results) == 0 {
		return fmt.Sprintf("No matches for %q in mode=%s.", args.Query, args.Mode), nil
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "Search %q (mode=%s, %d hits):\n\n", args.Query, args.Mode, len(results))
	for i, r := range results {
		if i >= 20 {
			fmt.Fprintf(&b, "+%d more (truncated).\n", len(results)-i)
			break
		}
		fmt.Fprintf(&b, "[%s] %s\n  %s\n\n", r.SessionID[:8], r.Role, r.Snippet)
	}
	return b.String(), nil
}

func handleAskTool(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Question == "" {
		return "", fmt.Errorf("question required")
	}
	cli, err := loadCLICtx(false)
	if err != nil {
		return "", err
	}
	defer cli.db.Close()
	if cli.aiClient == nil {
		return "", fmt.Errorf("ollama not available")
	}
	cctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	resp, err := ai.ChatWithContext(cctx, cli.db, cli.aiClient, cli.genModel, cli.embedModel,
		[]ai.ChatMsg{{Role: "user", Content: args.Question}})
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	b.WriteString(resp.Response)
	if len(resp.Sources) > 0 {
		b.WriteString("\n\n--- sources ---\n")
		for _, s := range resp.Sources {
			fmt.Fprintf(&b, "[%s] %.0f%%  %s\n", s.SessionID[:8], s.Similarity*100, s.Summary)
		}
	}
	return b.String(), nil
}

func handleInsightsTool(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &args)
	cli, err := loadCLICtx(true)
	if err != nil {
		return "", err
	}
	defer cli.db.Close()
	list, err := cli.db.InsightsList()
	if err != nil {
		return "", err
	}
	if args.Type != "" {
		filtered := []*index.Insight{}
		for _, i := range list {
			if i.Type == args.Type {
				filtered = append(filtered, i)
			}
		}
		list = filtered
	}
	if len(list) == 0 {
		return "No insights generated yet — run `claude-history` AI tab and click 'Generate insights'.", nil
	}
	var b bytes.Buffer
	for _, i := range list {
		fmt.Fprintf(&b, "[%s] %s\n  %s\n", i.Type, i.Title, i.Description)
		if i.SuggestedAction != "" {
			fmt.Fprintf(&b, "  → %s\n", i.SuggestedAction)
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func handleKnowledgeTool(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.SessionID == "" {
		return "", fmt.Errorf("session_id required")
	}
	cli, err := loadCLICtx(true)
	if err != nil {
		return "", err
	}
	defer cli.db.Close()
	all, _ := cli.db.ListSessions()
	full := args.SessionID
	for _, s := range all {
		if strings.HasPrefix(s.SessionID, args.SessionID) {
			full = s.SessionID
			break
		}
	}
	k, err := cli.db.KnowledgeGet(full)
	if err != nil {
		return fmt.Sprintf("Knowledge not generated for session %s yet.", args.SessionID), nil
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "Session: %s\n\n", k.SessionID[:8])
	if k.Problem != "" {
		fmt.Fprintf(&b, "Problem: %s\n", k.Problem)
	}
	if k.Solution != "" {
		fmt.Fprintf(&b, "Solution: %s\n", k.Solution)
	}
	dumpJSONListMCP(&b, "Decisions", k.Decisions, true)
	dumpJSONListMCP(&b, "Learnings", k.Learnings, false)
	dumpJSONListMCP(&b, "Code patterns", k.CodePatterns, false)
	dumpJSONListMCP(&b, "Tech used", k.TechUsed, false)
	dumpJSONListMCP(&b, "Open questions", k.OpenQuestions, false)
	return b.String(), nil
}

func handleAggregatedTool(ctx context.Context, raw json.RawMessage) (string, error) {
	cli, err := loadCLICtx(true)
	if err != nil {
		return "", err
	}
	defer cli.db.Close()
	agg, err := ai.AggregateKnowledge(cli.db)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "Aggregate (%d sessions):\n\n", agg.SessionsAnalyzed)
	if len(agg.TechFrequency) > 0 {
		b.WriteString("Tech: ")
		var parts []string
		for i, t := range agg.TechFrequency {
			if i >= 8 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s(%d)", t.Name, t.Count))
		}
		b.WriteString(strings.Join(parts, " "))
		b.WriteByte('\n')
	}
	if len(agg.TopPatterns) > 0 {
		b.WriteString("\nTop patterns:\n")
		for i, p := range agg.TopPatterns {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&b, "  ×%d %s\n", p.Count, p.Pattern)
		}
	}
	if len(agg.RecurringProblems) > 0 {
		b.WriteString("\nRecurring problems:\n")
		for i, p := range agg.RecurringProblems {
			if i >= 5 {
				break
			}
			fmt.Fprintf(&b, "  ×%d %s\n", p.Count, p.Representative)
		}
	}
	if len(agg.OpenQuestions) > 0 {
		b.WriteString("\nOpen questions:\n")
		for i, q := range agg.OpenQuestions {
			if i >= 8 {
				break
			}
			ageStr := "today"
			if q.AgeDays > 0 {
				ageStr = fmt.Sprintf("%dd", q.AgeDays)
			}
			fmt.Fprintf(&b, "  [%s] %s [%s]\n", ageStr, q.Question, q.SessionID[:8])
		}
	}
	return b.String(), nil
}

func handleProjectTool(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Path == "" {
		return "", fmt.Errorf("path required")
	}
	if abs, err := filepath.Abs(args.Path); err == nil {
		args.Path = abs
	}
	cli, err := loadCLICtx(true)
	if err != nil {
		return "", err
	}
	defer cli.db.Close()
	all, _ := cli.db.ListSessions()
	var projSessions []*model.Session
	for _, s := range all {
		if s.ProjectDir == args.Path {
			projSessions = append(projSessions, s)
		}
	}
	if len(projSessions) == 0 {
		return fmt.Sprintf("No sessions indexed for %s.", args.Path), nil
	}
	var costs []float64
	var tokens []int
	totalCost := 0.0
	tools := map[string]int{}
	for _, s := range projSessions {
		if c, ok := cli.pricing.Cost(s); ok {
			costs = append(costs, c.USD)
			totalCost += c.USD
		}
		tokens = append(tokens, int(s.InputTokens+s.OutputTokens))
		for t, n := range s.ToolCalls {
			tools[t] += n
		}
	}
	techs := ai.DetectTech(projSessions)
	var techNames []string
	for i, t := range techs {
		if i >= 8 {
			break
		}
		techNames = append(techNames, t.Name)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "Project: %s\n", args.Path)
	fmt.Fprintf(&b, "Sessions: %d · Total cost: $%.2f\n", len(projSessions), totalCost)
	fmt.Fprintf(&b, "p90: $%.2f · %d tokens\n", percentileCLI(costs, 0.9), percentileIntCLI(tokens, 0.9))
	if len(techNames) > 0 {
		fmt.Fprintf(&b, "Tech: %s\n", strings.Join(techNames, ", "))
	}
	if len(tools) > 0 {
		type kv struct {
			k string
			v int
		}
		var pairs []kv
		for k, v := range tools {
			pairs = append(pairs, kv{k, v})
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
		b.WriteString("Top tools:\n")
		for i, p := range pairs {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&b, "  %s · %d\n", p.k, p.v)
		}
	}
	return b.String(), nil
}

func handleStandupTool(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Since  string `json:"since"`
		Format string `json:"format"`
	}
	_ = json.Unmarshal(raw, &args)
	since := 7 * 24 * time.Hour
	if args.Since != "" {
		if d, err := parseDur(args.Since); err == nil {
			since = d
		}
	}
	if args.Format == "" {
		args.Format = "editorial"
	}
	cli, err := loadCLICtx(true)
	if err != nil {
		return "", err
	}
	defer cli.db.Close()
	cutoff := time.Now().Add(-since)
	all, _ := cli.db.ListSessions()
	var recent []*model.Session
	for _, s := range all {
		if s.StartTime.After(cutoff) {
			recent = append(recent, s)
		}
	}
	return renderStandup(cli, recent, args.Format), nil
}

func dumpJSONListMCP(b *bytes.Buffer, label, raw string, structured bool) {
	if raw == "" || raw == "[]" {
		return
	}
	if structured {
		var ds []struct {
			Decision  string `json:"decision"`
			Rationale string `json:"rationale"`
		}
		if err := json.Unmarshal([]byte(raw), &ds); err == nil && len(ds) > 0 {
			fmt.Fprintln(b, label+":")
			for _, d := range ds {
				fmt.Fprintf(b, "  · %s", d.Decision)
				if d.Rationale != "" {
					fmt.Fprintf(b, " — %s", d.Rationale)
				}
				b.WriteByte('\n')
			}
		}
		return
	}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(arr) > 0 {
		fmt.Fprintln(b, label+":")
		for _, x := range arr {
			fmt.Fprintf(b, "  · %s\n", x)
		}
	}
}

// cmdMCPServe é o subcomando "mcp" — sobe servidor MCP em stdio.
func cmdMCPServe() {
	srv := mcp.NewServer("claude-history", "0.1.0")
	registerTools(srv)
	if err := srv.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "mcp server error:", err)
		os.Exit(1)
	}
}

// cmdMCPInstall escreve a entrada mcpServers.claude-history em settings.json.
func cmdMCPInstall(args []string) {
	force := false
	uninstall := false
	for _, a := range args {
		switch a {
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
		removed, backup, err := mcp.Uninstall(settingsPath, "claude-history")
		if err != nil {
			fatal(err)
		}
		if !removed {
			fmt.Println("settings.json não tinha mcpServers.claude-history — nada a remover")
			return
		}
		fmt.Printf("✓ mcpServers.claude-history removido de %s\n  backup: %s\n", settingsPath, backup)
		return
	}
	self, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	res, err := mcp.Install(mcp.InstallOptions{
		SettingsPath: settingsPath,
		Name:         "claude-history",
		Command:      self,
		Args:         []string{"mcp"},
		Force:        force,
	})
	if err != nil {
		fatal(err)
	}
	if res.Backup != "" {
		fmt.Printf("✓ backup: %s\n", res.Backup)
	}
	if res.Replaced {
		fmt.Println("⚠ mcpServers.claude-history anterior foi sobrescrito")
	}
	fmt.Printf("✓ MCP server instalado em %s\n  command: %s mcp\n", settingsPath, self)
	fmt.Println("\nPróximo passo: reinicia o Claude Code (mcpServers só carrega no boot).")
	fmt.Println("As 8 tools 'claude_history_*' vão aparecer auto-discovered.")
}

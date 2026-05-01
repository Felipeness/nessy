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
		Description: "Find past Claude Code sessions semantically similar to a query, " +
			"ranked by embedding cosine similarity. Returns top-N (default 5) with " +
			"session_id, summary (1-line), project_dir, branch, and similarity score 0-1.\n\n" +
			"USE WHEN: user says 'I did something like this before', 'how did I solve X', " +
			"'find that session where I…', or you're starting a task that may resemble past work. " +
			"Useful pre-flight check before big refactors or debugging.\n\n" +
			"EXAMPLE: query='migrate sqlite to postgres' → returns 5 sessions about DB migrations " +
			"with scores 0.7-0.9.\n\n" +
			"DO NOT USE for: keyword/literal search (use claude_history_search instead), or " +
			"questions needing synthesis across sessions (use claude_history_ask). Returns " +
			"empty if AI features disabled (no embeddings indexed).",
		InputSchema: schemaObject(map[string]any{
			"query": schemaString("Natural-language description of the task or topic. Sentences work better than keywords."),
			"n":     schemaInt("Max results (1-20)", 5),
		}, "query"),
	}, handleSimilar)

	s.Register(mcp.Tool{
		Name: "claude_history_search",
		Description: "Keyword/filter search over Claude Code session metadata + message bodies. " +
			"Returns sessions matching the query with first_msg, project, branch, model, cost, " +
			"start_time. Supports filters inline: 'project:claude-history', 'branch:feat/CC-1234', " +
			"'since:7d', 'cost:>1', 'msgs:>100'.\n\n" +
			"MODES: 'hybrid' (default, metadata+FTS5), 'body' (only message text via FTS5), " +
			"'meta' (only project/branch/first_msg fields), 'sim' (semantic via embeddings — " +
			"prefer claude_history_similar for that).\n\n" +
			"USE WHEN: user has specific keywords/filters in mind ('find sessions on the auth " +
			"refactor branch from last week with cost > $5'). Faster and more precise than " +
			"semantic for literal terms.\n\n" +
			"DO NOT USE for: vague conceptual queries (use claude_history_similar), or for " +
			"questions needing answers/synthesis (use claude_history_ask).",
		InputSchema: schemaObject(map[string]any{
			"query": schemaString("Keywords + optional inline filters: 'project:X branch:Y since:7d cost:>1 msgs:>100'."),
			"mode":  schemaStringEnum("Search mode (default 'hybrid')", []string{"hybrid", "body", "meta", "sim"}, "hybrid"),
		}, "query"),
	}, handleSearchTool)

	s.Register(mcp.Tool{
		Name: "claude_history_ask",
		Description: "RAG-powered Q&A over the user's entire Claude Code history. Uses " +
			"extracted knowledge (decisions, learnings, code patterns) + embeddings to " +
			"answer in natural language with [session_id] citations.\n\n" +
			"USE WHEN: user wants a SYNTHESIZED answer rather than a list. Examples: " +
			"'how did I solve the auth bug?', 'what databases have I used?', 'what's still " +
			"unfinished in the migration project?', 'what decisions did I make about caching?'.\n\n" +
			"Returns prose answer with [abc12345] markers linking to actual sessions. Falls " +
			"back to general knowledge with [geral] tag only when history is silent on the topic.\n\n" +
			"DO NOT USE for: just listing/finding sessions (use search/similar — they're " +
			"faster and don't run the LLM). Returns empty if AI features disabled.",
		InputSchema: schemaObject(map[string]any{
			"question": schemaString("Natural-language question about your coding history. Phrasing as a question helps."),
		}, "question"),
	}, handleAskTool)

	s.Register(mcp.Tool{
		Name: "claude_history_insights",
		Description: "Surface AI-generated insights about the user's coding patterns. Each " +
			"insight has type, title, description, evidence (session_ids), and suggested action.\n\n" +
			"INSIGHT TYPES:\n" +
			"  repeated_task     — task done 3+× across sessions (script candidate)\n" +
			"  chronic_problem   — same bug/issue keeps coming back\n" +
			"  script_opportunity— manual workflow that should be automated\n" +
			"  token_waste       — sessions burning tokens with low output\n" +
			"  performance_hint  — slow loops or inefficient patterns\n" +
			"  anti_pattern      — code smell repeated across sessions\n" +
			"  personal_pattern  — workflow quirks specific to this user\n\n" +
			"USE WHEN: user asks 'what should I improve', 'what am I doing wrong', 'what " +
			"could be automated', or wants a workflow review. Without 'type' filter returns all.\n\n" +
			"DO NOT USE for: looking up specific sessions (use search). Returns empty if " +
			"insights haven't been generated yet (run TUI's [I] action first).",
		InputSchema: schemaObject(map[string]any{
			"type": schemaStringEnum("Filter by insight type (omit for all)",
				[]string{"repeated_task", "chronic_problem", "script_opportunity",
					"token_waste", "performance_hint", "anti_pattern", "personal_pattern"}, ""),
		}),
	}, handleInsightsTool)

	s.Register(mcp.Tool{
		Name: "claude_history_knowledge",
		Description: "Get the AI-extracted structured knowledge from ONE specific session. " +
			"Returns: problem statement, solution, decisions (each with rationale), learnings " +
			"(takeaways), code_patterns (reusable techniques), tech_used (libs/tools), " +
			"open_questions (loose ends).\n\n" +
			"USE WHEN: drilling into a session you already know the ID of, often after using " +
			"claude_history_search or claude_history_similar to find candidates. Examples: " +
			"'tell me what I learned in [abc12345]', 'what decisions were made in that " +
			"session about caching?'.\n\n" +
			"EXAMPLE: session_id='abc12345' → returns JSON with all 7 fields.\n\n" +
			"DO NOT USE for: cross-session aggregates (use claude_history_aggregated), or " +
			"when you don't have a session_id yet. Returns empty if knowledge not extracted " +
			"for this session (run TUI's [K] action first).",
		InputSchema: schemaObject(map[string]any{
			"session_id": schemaString("Session ID — full UUID (36 chars) or 8-char prefix accepted."),
		}, "session_id"),
	}, handleKnowledgeTool)

	s.Register(mcp.Tool{
		Name: "claude_history_aggregated",
		Description: "Cross-session aggregate of all extracted knowledge. Returns: top " +
			"code_patterns (with frequency), decision_timeline (chronological), recurring " +
			"problems (count + last_seen), tech frequency map, open_questions across all " +
			"sessions.\n\n" +
			"USE WHEN: user wants the BIG PICTURE — 'what patterns do I use most', 'what " +
			"are my open loose ends across all projects', 'what recurring problems keep " +
			"coming up', 'show me my decision history'. Read-only summary, no per-session drill.\n\n" +
			"No input parameters — operates on the entire history.\n\n" +
			"DO NOT USE for: questions about a single session (use claude_history_knowledge), " +
			"or when looking for specific text/keywords (use claude_history_search). Returns " +
			"empty arrays if no sessions have extracted knowledge yet.",
		InputSchema: schemaObject(nil),
	}, handleAggregatedTool)

	s.Register(mcp.Tool{
		Name: "claude_history_project",
		Description: "Profile a single project directory across all sessions that ran in " +
			"it. Returns: session count, total cost, p90 cost/tokens (catch outliers), " +
			"detected tech stack (langs/frameworks), top tools used (Bash/Edit/Read freq), " +
			"most-recent ticket pattern (CC-1234 from branch names).\n\n" +
			"USE WHEN: about to start work on a project ('what have I done in /path/to/repo " +
			"before?'), planning a refactor and want history context, estimating effort " +
			"based on similar past work, or onboarding to an unfamiliar local project.\n\n" +
			"EXAMPLE: path='/Users/me/work/auth-service' → returns 23 sessions, $45 total, " +
			"p90=$3.20, tech=[Go, NestJS, Postgres], top tools=[Edit:120, Bash:80].\n\n" +
			"DO NOT USE for: searching across projects (use claude_history_search). Path " +
			"must be absolute and match the cwd recorded in sessions exactly.",
		InputSchema: schemaObject(map[string]any{
			"path": schemaString("Absolute project path — must match session cwd exactly (e.g. /Users/me/repo, not ~/repo)."),
		}, "path"),
	}, handleProjectTool)

	s.Register(mcp.Tool{
		Name: "claude_history_standup",
		Description: "Generate a standup-style markdown summary of recent Claude Code work. " +
			"Pre-formatted for daily/weekly reports.\n\n" +
			"FORMATS:\n" +
			"  editorial (default) — narrative with sections: Concluído / Decisões / Em aberto\n" +
			"  timeline            — chronological by day/hour\n" +
			"  project             — grouped by project_dir with cost subtotals\n\n" +
			"USE WHEN: user asks 'what did I do last week', 'summary for daily standup', " +
			"'prepare my weekly report', or wants to copy-paste into Slack/Jira. The " +
			"editorial format is best for human reading; timeline is best for activity " +
			"reconstruction; project is best when reporting per-client.\n\n" +
			"EXAMPLE: since='7d', format='editorial' → markdown with last-week summary.\n\n" +
			"DO NOT USE for: questions about specific sessions (use claude_history_ask) or " +
			"raw lists (use claude_history_search). Default since='24h' if omitted.",
		InputSchema: schemaObject(map[string]any{
			"since":  schemaString("Time window like '7d', '24h', '30m', '14d'. Default '24h'."),
			"format": schemaStringEnum("Output format (default 'editorial')", []string{"editorial", "timeline", "project"}, "editorial"),
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

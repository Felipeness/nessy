// CLI subcommands novos pra Fase 7a — `similar/search/ask/insights/knowledge/
// aggregated/project/standup`. Saída JSON com `--json`, human-readable default.
//
// Ler DB direto (sem precisar daemon rodando). Comandos AI usam Ollama direto.
// Falhas graceful — Ollama down emite JSON com {"error": "..."} ao invés de
// quebrar o caller.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/felipeness/nessy/internal/advisor"
	"github.com/felipeness/nessy/internal/ai"
	"github.com/felipeness/nessy/internal/branding"
	"github.com/felipeness/nessy/internal/config"
	"github.com/felipeness/nessy/internal/index"
	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/parser"
	"github.com/felipeness/nessy/internal/pricing"
)

// cliCtx carrega tudo que os subcomandos novos precisam — DB, pricing, config,
// e cliente Ollama opcional. Centraliza setup que antes estava em cmdTui/serve.
type cliCtx struct {
	db        *index.DB
	pricing   *pricing.Pricing
	cfg       *config.Config
	aiClient  *ai.Client // pode ser nil (--no-ai ou ollama down)
	genModel  string
	embedModel string
}

// loadCLICtx abre DB, pricing e config. Não inicia daemon nem worker.
// Reusa caminhos padrão de ~/.nessy/.
func loadCLICtx(noAI bool) (*cliCtx, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cacheDir := branding.CacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(cacheDir, "index.db")
	pricingPath := filepath.Join(cacheDir, "pricing.toml")

	if _, err := os.Stat(pricingPath); os.IsNotExist(err) {
		_ = os.WriteFile(pricingPath, []byte(defaultPricingTOML), 0644)
	}

	db, err := index.Open(dbPath)
	if err != nil {
		return nil, err
	}
	// Reindex incremental no boot — comandos CLI sempre veem dado fresco.
	_, _ = db.Reindex(filepath.Join(home, ".claude", "projects"))

	p, err := pricing.Load(pricingPath)
	if err != nil {
		return nil, err
	}
	cfg, _ := config.LoadConfig(filepath.Join(cacheDir, "config.toml"))

	c := &cliCtx{
		db:         db,
		pricing:    p,
		cfg:        cfg,
		genModel:   cfg.AI.GenModel,
		embedModel: cfg.AI.EmbedModel,
	}
	if !noAI && cfg.AI.Enabled {
		client := ai.NewClient(cfg.AI.OllamaURL)
		// Health check — se ollama down, deixa nil mas não falha
		if client.Health(context.Background()) {
			c.aiClient = client
		}
	}
	return c, nil
}

// emitJSON escreve v como JSON indentado em stdout. Usado em todo --json output.
func emitJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// emitJSONError emite {"error": msg} e exit code 0 (graceful).
func emitJSONError(msg string) {
	emitJSON(map[string]string{"error": msg})
}

// parseFlags simples pra args estilo POSIX: --flag, --flag value, --flag=value.
// Retorna mapa flag→valor (string vazio = boolean true) e residuais (positional).
func parseFlags(args []string, knownBool map[string]bool) (flags map[string]string, positional []string) {
	flags = map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			positional = append(positional, a)
			continue
		}
		key := a[2:]
		val := ""
		if eq := strings.IndexByte(key, '='); eq >= 0 {
			val = key[eq+1:]
			key = key[:eq]
		} else if !knownBool[key] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			val = args[i+1]
			i++
		}
		flags[key] = val
	}
	return
}

// =============================================================================
// similar <query> — top-N sessions parecidas via cosine sim sobre embeddings.
// =============================================================================

func cmdSimilar(args []string) {
	flags, positional := parseFlags(args, map[string]bool{"json": true})
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nessy similar <query> [--n 5] [--json]")
		os.Exit(1)
	}
	query := strings.Join(positional, " ")
	n := 5
	if v := flags["n"]; v != "" {
		if x, err := strconv.Atoi(v); err == nil {
			n = x
		}
	}
	asJSON := flags["json"] != "_unset" && (flags["json"] == "" || flags["json"] == "true")
	if _, ok := flags["json"]; !ok {
		asJSON = false
	}

	ctx, err := loadCLICtx(false)
	if err != nil {
		fatal(err)
	}
	defer ctx.db.Close()

	if ctx.aiClient == nil {
		if asJSON {
			emitJSONError("ollama not available — start with `ollama serve`")
		} else {
			fmt.Fprintln(os.Stderr, "ollama not reachable")
		}
		return
	}

	cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	queryEmb, err := ctx.aiClient.Embedding(cctx, ctx.embedModel, query)
	if err != nil {
		if asJSON {
			emitJSONError("embedding: " + err.Error())
		} else {
			fmt.Fprintln(os.Stderr, "embedding error:", err)
		}
		return
	}

	caches, _ := ctx.db.AICacheList()
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
	if len(hits) > n {
		hits = hits[:n]
	}

	if asJSON {
		type out struct {
			SessionID  string  `json:"session_id"`
			Similarity float64 `json:"similarity"`
			Summary    string  `json:"summary"`
			ProjectDir string  `json:"project_dir"`
			GitBranch  string  `json:"git_branch"`
		}
		var arr []out
		for _, h := range hits {
			s, _ := ctx.db.GetByID(h.c.SessionID)
			pd, br := "", ""
			if s != nil {
				pd, br = s.ProjectDir, s.GitBranch
			}
			arr = append(arr, out{h.c.SessionID, h.sim, h.c.Summary, pd, br})
		}
		emitJSON(arr)
		return
	}
	fmt.Printf("query: %q\n\n", query)
	for _, h := range hits {
		s, _ := ctx.db.GetByID(h.c.SessionID)
		dir, br := "", ""
		if s != nil {
			dir = strings.Replace(s.ProjectDir, mustHome(), "~", 1)
			br = s.GitBranch
		}
		fmt.Printf("  %.2f  %s  %s  %s\n        %s\n\n", h.sim, h.c.SessionID[:8], dir, br, h.c.Summary)
	}
}

// =============================================================================
// search <q> — busca via metadata + FTS hybrid (mesmo do Web/TUI).
// =============================================================================

func cmdSearchCLI(args []string) {
	flags, positional := parseFlags(args, map[string]bool{"json": true, "all": true})
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nessy search <query> [--mode hybrid|body|meta|sim] [--all] [--json]")
		os.Exit(1)
	}
	query := strings.Join(positional, " ")
	mode := flags["mode"]
	if mode == "" {
		mode = "hybrid"
	}
	all := flags["all"] != "_unset"
	if _, ok := flags["all"]; !ok {
		all = false
	}
	_, asJSON := flags["json"]

	ctx, err := loadCLICtx(false)
	if err != nil {
		fatal(err)
	}
	defer ctx.db.Close()

	// metadata = substring em vários campos; body = FTS5
	all_, _ := ctx.db.ListSessions()
	caches, _ := ctx.db.AICacheList()
	summaries := map[string]string{}
	for _, c := range caches {
		if c.Summary != "" {
			summaries[c.SessionID] = c.Summary
		}
	}

	var results []searchHitCLI

	switch mode {
	case "body":
		results = searchFTSCli(ctx.db, query, all_, all)
	case "meta":
		results = searchMetaCli(query, all_, summaries)
	default: // hybrid
		results = append(searchMetaCli(query, all_, summaries), searchFTSCli(ctx.db, query, all_, all)...)
		// dedupe por session+role+snippet
		seen := map[string]bool{}
		uniq := []searchHitCLI{}
		for _, r := range results {
			k := r.SessionID + "|" + r.Role + "|" + r.Snippet
			if seen[k] {
				continue
			}
			seen[k] = true
			uniq = append(uniq, r)
		}
		results = uniq
	}

	if asJSON {
		emitJSON(results)
		return
	}
	fmt.Printf("query: %q · mode: %s · %d hits\n\n", query, mode, len(results))
	for _, r := range results {
		fmt.Printf("  [%s] %s\n  %s\n\n", r.SessionID[:8], r.Role, r.Snippet)
	}
}

type searchHitCLI struct {
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Snippet   string `json:"snippet"`
}

func searchMetaCli(q string, sessions []*model.Session, summaries map[string]string) []searchHitCLI {
	lower := strings.ToLower(q)
	var out []searchHitCLI
	for _, s := range sessions {
		for _, c := range []struct{ field, text string }{
			{"path", s.ProjectDir}, {"branch", s.GitBranch},
			{"first_msg", s.FirstUserMsg}, {"last_msg", s.LastUserMsg},
			{"model", s.Model},
		} {
			if c.text != "" && strings.Contains(strings.ToLower(c.text), lower) {
				out = append(out, searchHitCLI{s.SessionID, c.field, c.text})
				break
			}
		}
		if sum := summaries[s.SessionID]; sum != "" && strings.Contains(strings.ToLower(sum), lower) {
			out = append(out, searchHitCLI{s.SessionID, "summary", sum})
		}
	}
	return out
}

func searchFTSCli(db *index.DB, q string, sessions []*model.Session, all bool) []searchHitCLI {
	results, err := db.SearchFTSExact(q)
	if err != nil {
		results, _ = db.SearchLike(q)
	}
	byID := map[string]*model.Session{}
	for _, s := range sessions {
		byID[s.SessionID] = s
	}
	var out []searchHitCLI
	seen := map[string]bool{}
	for _, r := range results {
		if !all && seen[r.SessionID] {
			continue
		}
		seen[r.SessionID] = true
		if _, ok := byID[r.SessionID]; ok {
			out = append(out, searchHitCLI{r.SessionID, "body:" + r.Role, r.Snippet})
		}
	}
	return out
}

// =============================================================================
// ask <question> — chat Ness IA com RAG, single-turn.
// =============================================================================

func cmdAsk(args []string) {
	flags, positional := parseFlags(args, map[string]bool{"json": true})
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nessy ask <question> [--json]")
		os.Exit(1)
	}
	question := strings.Join(positional, " ")
	_, asJSON := flags["json"]

	ctx, err := loadCLICtx(false)
	if err != nil {
		fatal(err)
	}
	defer ctx.db.Close()
	if ctx.aiClient == nil {
		if asJSON {
			emitJSONError("ollama not available")
		} else {
			fmt.Fprintln(os.Stderr, "ollama not reachable — `ollama serve`")
		}
		return
	}

	cctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	resp, err := ai.ChatWithContext(cctx, ctx.db, ctx.aiClient, ctx.genModel, ctx.embedModel,
		[]ai.ChatMsg{{Role: "user", Content: question}})
	if err != nil {
		if asJSON {
			emitJSONError(err.Error())
		} else {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		return
	}
	if asJSON {
		emitJSON(resp)
		return
	}
	fmt.Println(resp.Response)
	fmt.Println()
	fmt.Println("--- fontes ---")
	for _, s := range resp.Sources {
		fmt.Printf("  [%s] %.0f%%  %s\n", s.SessionID[:8], s.Similarity*100, s.Summary)
	}
}

// =============================================================================
// insights — lista insights existentes na DB.
// =============================================================================

func cmdInsightsCLI(args []string) {
	flags, _ := parseFlags(args, map[string]bool{"json": true})
	_, asJSON := flags["json"]
	typeFilter := flags["type"]

	ctx, err := loadCLICtx(true) // não precisa Ollama
	if err != nil {
		fatal(err)
	}
	defer ctx.db.Close()
	list, err := ctx.db.InsightsList()
	if err != nil {
		fatal(err)
	}
	if typeFilter != "" {
		filtered := []*index.Insight{}
		for _, i := range list {
			if i.Type == typeFilter {
				filtered = append(filtered, i)
			}
		}
		list = filtered
	}
	if asJSON {
		emitJSON(list)
		return
	}
	for _, i := range list {
		fmt.Printf("[%s] %s\n  %s\n", i.Type, i.Title, i.Description)
		if i.SuggestedAction != "" {
			fmt.Printf("  → %s\n", i.SuggestedAction)
		}
		fmt.Println()
	}
}

// =============================================================================
// knowledge <session_id> — devolve knowledge de 1 session.
// =============================================================================

func cmdKnowledgeCLI(args []string) {
	flags, positional := parseFlags(args, map[string]bool{"json": true})
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nessy knowledge <session_id> [--json]")
		os.Exit(1)
	}
	id := positional[0]
	_, asJSON := flags["json"]

	ctx, err := loadCLICtx(true)
	if err != nil {
		fatal(err)
	}
	defer ctx.db.Close()

	// aceita prefix
	all, _ := ctx.db.ListSessions()
	full := id
	for _, s := range all {
		if strings.HasPrefix(s.SessionID, id) {
			full = s.SessionID
			break
		}
	}
	k, err := ctx.db.KnowledgeGet(full)
	if err != nil {
		if asJSON {
			emitJSONError("not generated yet — run `nessy` and gen knowledge")
		} else {
			fmt.Fprintln(os.Stderr, "knowledge not generated for", id)
		}
		return
	}
	if asJSON {
		emitJSON(k)
		return
	}
	fmt.Printf("session: %s\n\n", k.SessionID[:8])
	fmt.Printf("🎯 problem:  %s\n", k.Problem)
	fmt.Printf("✅ solution: %s\n", k.Solution)
	printJSONList("⚖️  decisions:", k.Decisions, true)
	printJSONList("💡 learnings:", k.Learnings, false)
	printJSONList("⚙️  patterns:", k.CodePatterns, false)
	printJSONList("🔧 tech:", k.TechUsed, false)
	printJSONList("❓ open:", k.OpenQuestions, false)
}

func printJSONList(label, raw string, structured bool) {
	if raw == "" || raw == "[]" {
		return
	}
	if structured {
		var ds []struct {
			Decision  string `json:"decision"`
			Rationale string `json:"rationale"`
		}
		if err := json.Unmarshal([]byte(raw), &ds); err == nil && len(ds) > 0 {
			fmt.Println(label)
			for _, d := range ds {
				fmt.Printf("  · %s", d.Decision)
				if d.Rationale != "" {
					fmt.Printf(" — %s", d.Rationale)
				}
				fmt.Println()
			}
		}
		return
	}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(arr) > 0 {
		fmt.Println(label)
		for _, x := range arr {
			fmt.Printf("  · %s\n", x)
		}
	}
}

// =============================================================================
// aggregated — KnowledgeAggregate cross-session.
// =============================================================================

func cmdAggregatedCLI(args []string) {
	flags, _ := parseFlags(args, map[string]bool{"json": true})
	_, asJSON := flags["json"]

	ctx, err := loadCLICtx(true)
	if err != nil {
		fatal(err)
	}
	defer ctx.db.Close()
	agg, err := ai.AggregateKnowledge(ctx.db)
	if err != nil {
		fatal(err)
	}
	if asJSON {
		emitJSON(agg)
		return
	}
	fmt.Printf("knowledge agregado · %d sessions analisadas\n\n", agg.SessionsAnalyzed)
	if len(agg.TechFrequency) > 0 {
		fmt.Println("🔧 tech:")
		for i, t := range agg.TechFrequency {
			if i >= 10 {
				break
			}
			fmt.Printf("  %s · %d\n", t.Name, t.Count)
		}
		fmt.Println()
	}
	if len(agg.TopPatterns) > 0 {
		fmt.Println("⚙️ top patterns:")
		for i, p := range agg.TopPatterns {
			if i >= 8 {
				break
			}
			fmt.Printf("  ×%-2d %s\n", p.Count, p.Pattern)
		}
		fmt.Println()
	}
	if len(agg.RecurringProblems) > 0 {
		fmt.Println("🔁 problemas recorrentes:")
		for i, p := range agg.RecurringProblems {
			if i >= 5 {
				break
			}
			fmt.Printf("  ×%-2d %s\n", p.Count, p.Representative)
		}
		fmt.Println()
	}
	if len(agg.OpenQuestions) > 0 {
		fmt.Println("❓ em aberto:")
		for i, q := range agg.OpenQuestions {
			if i >= 8 {
				break
			}
			ageStr := "hoje"
			if q.AgeDays > 0 {
				ageStr = fmt.Sprintf("%dd", q.AgeDays)
			}
			fmt.Printf("  [%4s] %s\n", ageStr, q.Question)
		}
	}
}

// =============================================================================
// project <path> — stats de 1 projeto: p90, tech, top tools, ticket pattern.
// =============================================================================

var ticketBranchRECLI = regexp.MustCompile(`([A-Z]{2,8}-\d{1,6})`)

func cmdProjectCLI(args []string) {
	flags, positional := parseFlags(args, map[string]bool{"json": true})
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nessy project <path> [--json]")
		os.Exit(1)
	}
	path := positional[0]
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	_, asJSON := flags["json"]

	ctx, err := loadCLICtx(true)
	if err != nil {
		fatal(err)
	}
	defer ctx.db.Close()

	all, _ := ctx.db.ListSessions()
	var projSessions []*model.Session
	for _, s := range all {
		if s.ProjectDir == path {
			projSessions = append(projSessions, s)
		}
	}

	type out struct {
		Dir          string         `json:"dir"`
		Sessions     int            `json:"sessions"`
		TotalCostUSD float64        `json:"total_cost_usd"`
		P90Cost      float64        `json:"p90_cost"`
		P90Tokens    int            `json:"p90_tokens"`
		TopTools     map[string]int `json:"top_tools"`
		TechStack    []string       `json:"tech_stack"`
		LastTicket   string         `json:"last_ticket"`
	}
	o := out{Dir: path, Sessions: len(projSessions), TopTools: map[string]int{}}

	var costs []float64
	var tokens []int
	for _, s := range projSessions {
		if c, ok := ctx.pricing.Cost(s); ok {
			costs = append(costs, c.USD)
			o.TotalCostUSD += c.USD
		}
		tokens = append(tokens, int(s.InputTokens+s.OutputTokens))
		for t, n := range s.ToolCalls {
			o.TopTools[t] += n
		}
	}
	o.P90Cost = percentileCLI(costs, 0.9)
	o.P90Tokens = percentileIntCLI(tokens, 0.9)

	techs := ai.DetectTech(projSessions)
	for i, t := range techs {
		if i >= 8 {
			break
		}
		o.TechStack = append(o.TechStack, t.Name)
	}
	// Ticket: extrai da branch da session mais recente
	if len(projSessions) > 0 {
		newest := projSessions[0]
		for _, s := range projSessions[1:] {
			if s.StartTime.After(newest.StartTime) {
				newest = s
			}
		}
		if m := ticketBranchRECLI.FindString(newest.GitBranch); m != "" {
			o.LastTicket = m
		}
	}

	if asJSON {
		emitJSON(o)
		return
	}
	if o.Sessions == 0 {
		fmt.Println("nenhuma session indexada nesse projeto")
		return
	}
	fmt.Printf("projeto: %s\n", path)
	fmt.Printf("sessions: %d · cost total: $%.2f\n", o.Sessions, o.TotalCostUSD)
	fmt.Printf("p90: $%.2f · %d tokens\n", o.P90Cost, o.P90Tokens)
	if len(o.TechStack) > 0 {
		fmt.Printf("tech: %s\n", strings.Join(o.TechStack, ", "))
	}
	if o.LastTicket != "" {
		fmt.Printf("último ticket: %s\n", o.LastTicket)
	}
	if len(o.TopTools) > 0 {
		fmt.Println("\ntop tools:")
		type kv struct {
			k string
			v int
		}
		var pairs []kv
		for k, v := range o.TopTools {
			pairs = append(pairs, kv{k, v})
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
		for i, p := range pairs {
			if i >= 8 {
				break
			}
			fmt.Printf("  %-15s %d\n", p.k, p.v)
		}
	}
}

func percentileCLI(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]float64{}, xs...)
	sort.Float64s(sorted)
	return sorted[int(float64(len(sorted)-1)*p)]
}

func percentileIntCLI(xs []int, p float64) int {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]int{}, xs...)
	sort.Ints(sorted)
	return sorted[int(float64(len(sorted)-1)*p)]
}

// =============================================================================
// standup --since 7d [--format timeline|project|editorial]
// =============================================================================

func cmdStandupCLI(args []string) {
	flags, _ := parseFlags(args, map[string]bool{"json": true})
	since := 7 * 24 * time.Hour
	if v := flags["since"]; v != "" {
		if d, err := parseDur(v); err == nil {
			since = d
		}
	}
	format := flags["format"]
	if format == "" {
		format = "editorial"
	}
	_, asJSON := flags["json"]

	ctx, err := loadCLICtx(true)
	if err != nil {
		fatal(err)
	}
	defer ctx.db.Close()

	cutoff := time.Now().Add(-since)
	all, _ := ctx.db.ListSessions()
	var recent []*model.Session
	for _, s := range all {
		if s.StartTime.After(cutoff) {
			recent = append(recent, s)
		}
	}

	if asJSON {
		out := map[string]any{
			"since":    since.String(),
			"sessions": len(recent),
			"format":   format,
		}
		// pra json, sempre devolve dado bruto + format renderizado
		out["markdown"] = renderStandup(ctx, recent, format)
		emitJSON(out)
		return
	}
	fmt.Println(renderStandup(ctx, recent, format))
}

func renderStandup(ctx *cliCtx, sessions []*model.Session, format string) string {
	switch format {
	case "timeline":
		return renderStandupTimeline(ctx, sessions)
	case "project":
		return renderStandupProject(ctx, sessions)
	default:
		return renderStandupEditorial(ctx, sessions)
	}
}

func renderStandupTimeline(ctx *cliCtx, sessions []*model.Session) string {
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].StartTime.Before(sessions[j].StartTime) })
	var b strings.Builder
	b.WriteString("# Standup — últimos dias\n\n")
	currentDay := ""
	for _, s := range sessions {
		day := s.StartTime.Local().Format("Mon Jan 02")
		if day != currentDay {
			fmt.Fprintf(&b, "\n## %s\n", day)
			currentDay = day
		}
		summary := getSummaryCLI(ctx, s.SessionID)
		fmt.Fprintf(&b, "- [%s] %s [%s]\n",
			s.StartTime.Local().Format("15:04"),
			truncate(summary, 80),
			s.SessionID[:8])
	}
	return b.String()
}

func renderStandupProject(ctx *cliCtx, sessions []*model.Session) string {
	groups := map[string][]*model.Session{}
	for _, s := range sessions {
		groups[s.ProjectDir] = append(groups[s.ProjectDir], s)
	}
	type entry struct {
		dir   string
		list  []*model.Session
		cost  float64
	}
	var ordered []entry
	for d, l := range groups {
		var cost float64
		for _, s := range l {
			if c, ok := ctx.pricing.Cost(s); ok {
				cost += c.USD
			}
		}
		ordered = append(ordered, entry{d, l, cost})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].cost > ordered[j].cost })

	var b strings.Builder
	b.WriteString("# Standup — últimos dias por projeto\n\n")
	for _, e := range ordered {
		dir := strings.Replace(e.dir, mustHome(), "~", 1)
		fmt.Fprintf(&b, "## %s (%d sessions, $%.2f)\n", dir, len(e.list), e.cost)
		for _, s := range e.list {
			fmt.Fprintf(&b, "- %s [%s]\n", truncate(getSummaryCLI(ctx, s.SessionID), 90), s.SessionID[:8])
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// renderStandupEditorial usa knowledge (decisions/open_questions) pra fazer
// um standup com sections de Concluído / Decisões / Em aberto.
func renderStandupEditorial(ctx *cliCtx, sessions []*model.Session) string {
	var b strings.Builder
	b.WriteString("# Standup — últimos dias\n\n")

	// ✅ Concluído — solutions de cada session com knowledge
	b.WriteString("## ✅ Concluído\n\n")
	for _, s := range sessions {
		k, _ := ctx.db.KnowledgeGet(s.SessionID)
		if k != nil && k.Solution != "" && k.Solution != "Não foi possível concluir." {
			fmt.Fprintf(&b, "- %s [%s]\n", k.Solution, s.SessionID[:8])
		} else {
			summary := getSummaryCLI(ctx, s.SessionID)
			if summary != "" {
				fmt.Fprintf(&b, "- %s [%s]\n", truncate(summary, 100), s.SessionID[:8])
			}
		}
	}
	b.WriteByte('\n')

	// ⚖️ Decisões importantes
	b.WriteString("## ⚖️ Decisões importantes\n\n")
	hasDecisions := false
	for _, s := range sessions {
		k, _ := ctx.db.KnowledgeGet(s.SessionID)
		if k == nil || k.Decisions == "" {
			continue
		}
		var decs []struct {
			Decision  string `json:"decision"`
			Rationale string `json:"rationale"`
		}
		if err := json.Unmarshal([]byte(k.Decisions), &decs); err != nil {
			continue
		}
		for _, d := range decs {
			fmt.Fprintf(&b, "- %s", d.Decision)
			if d.Rationale != "" {
				fmt.Fprintf(&b, " — %s", d.Rationale)
			}
			fmt.Fprintf(&b, " [%s]\n", s.SessionID[:8])
			hasDecisions = true
		}
	}
	if !hasDecisions {
		b.WriteString("- (sem decisões registradas no período)\n")
	}
	b.WriteByte('\n')

	// ❓ Em aberto
	b.WriteString("## ❓ Em aberto\n\n")
	hasOpen := false
	for _, s := range sessions {
		k, _ := ctx.db.KnowledgeGet(s.SessionID)
		if k == nil || k.OpenQuestions == "" {
			continue
		}
		var qs []string
		if err := json.Unmarshal([]byte(k.OpenQuestions), &qs); err != nil {
			continue
		}
		for _, q := range qs {
			fmt.Fprintf(&b, "- %s [%s]\n", q, s.SessionID[:8])
			hasOpen = true
		}
	}
	if !hasOpen {
		b.WriteString("- (nada em aberto registrado)\n")
	}
	return b.String()
}

func getSummaryCLI(ctx *cliCtx, sessionID string) string {
	c, err := ctx.db.AICacheGet(sessionID)
	if err != nil || c == nil {
		// fallback pra first_user_msg
		s, _ := ctx.db.GetByID(sessionID)
		if s != nil {
			return s.FirstUserMsg
		}
		return ""
	}
	return c.Summary
}

func parseDur(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func mustHome() string {
	h, _ := os.UserHomeDir()
	return h
}

func cosineSim32(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (sqrtFCli(na) * sqrtFCli(nb))
}

func sqrtFCli(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x / 2
	for i := 0; i < 8; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

// parser usage ref pra evitar unused import quando compila
var _ = parser.ListSessions

// cmdAdviseCLI roda o advisor (rule-based) e formata pra terminal/JSON.
func cmdAdviseCLI(args []string) {
	flags, _ := parseFlags(args, map[string]bool{"json": true})
	_, asJSON := flags["json"]

	ctx, err := loadCLICtx(false) // não precisa AI
	if err != nil {
		fatal(err)
	}
	defer ctx.db.Close()
	all, err := ctx.db.ListSessions()
	if err != nil {
		fatal(err)
	}
	recs, err := advisor.Run(ctx.db, ctx.pricing, all)
	if err != nil {
		fatal(err)
	}
	if asJSON {
		emitJSON(map[string]any{
			"recommendations": recs,
			"count":           len(recs),
		})
		return
	}
	if len(recs) == 0 {
		fmt.Println("✓ Nenhuma recomendação no momento — uso parece bom!")
		return
	}
	fmt.Printf("💡 %d recomendações (ordenadas por impacto)\n\n", len(recs))
	icons := map[string]string{
		"skill":           "🛠️ ",
		"hook":            "🪝",
		"cli":             "⚡",
		"model_downgrade": "💸",
		"cache":           "💾",
		"subagent":        "🌳",
		"claude_md":       "📝",
	}
	for i, r := range recs {
		icon := icons[r.Type]
		if icon == "" {
			icon = "•"
		}
		fmt.Printf("%d. %s %s  [%s · confidence:%s]\n", i+1, icon, r.Title, r.Type, r.Confidence)
		fmt.Printf("   %s\n", r.Description)
		fmt.Printf("   → %s\n", r.Action)
		if r.Savings != "" {
			fmt.Printf("   💰 %s\n", r.Savings)
		}
		fmt.Printf("   📎 %s\n\n", r.Evidence)
	}
}

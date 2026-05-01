package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/felipeness/nessy/internal/advisor"
	"github.com/felipeness/nessy/internal/ai"
	"github.com/felipeness/nessy/internal/config"
	"github.com/felipeness/nessy/internal/index"
	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/parser"
	"github.com/felipeness/nessy/internal/pricing"
	"github.com/felipeness/nessy/internal/search"
	"github.com/felipeness/nessy/internal/stats"
)

func registerAPI(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("/api/meta", s.handleMeta)
	mux.HandleFunc("/api/advise", s.handleAdvise)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionByID) // /api/sessions/<id> + /api/sessions/<id>/messages
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/stats/behavioral", s.handleBehavioral)
	mux.HandleFunc("/api/behavior/advanced", s.handleBehaviorAdvanced)
	mux.HandleFunc("/api/costs", s.handleCosts)
	mux.HandleFunc("/api/timeline", s.handleTimeline)
	mux.HandleFunc("/api/tools", s.handleTools)
	mux.HandleFunc("/api/tools/", s.handleToolDrill) // /api/tools/<name>/sessions
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/refresh", s.handleRefresh)
	mux.HandleFunc("/api/export/", s.handleExport) // /api/export/<id>
	mux.HandleFunc("/api/ai/health", s.handleAIHealth)
	mux.HandleFunc("/api/ai/summaries", s.handleAISummaries)
	mux.HandleFunc("/api/ai/summary/", s.handleAISummary)
	mux.HandleFunc("/api/ai/similar/", s.handleAISimilar)
	mux.HandleFunc("/api/ai/clusters", s.handleAIClusters)
	mux.HandleFunc("/api/ai/clusters/recompute", s.handleAIRecomputeClusters)
	mux.HandleFunc("/api/ai/generate-all", s.handleAIGenerateAll)
	mux.HandleFunc("/api/ai/generate/", s.handleAIGenerateOne)
	mux.HandleFunc("/api/ai/insights", s.handleAIInsights)
	mux.HandleFunc("/api/ai/insights/generate", s.handleAIGenerateInsights)
	mux.HandleFunc("/api/ai/profile", s.handleAIProfile)
	mux.HandleFunc("/api/ai/profile/generate", s.handleAIGenerateProfile)
	mux.HandleFunc("/api/ai/knowledge", s.handleAIKnowledgeList)
	mux.HandleFunc("/api/ai/knowledge/aggregated", s.handleAIKnowledgeAggregated)
	mux.HandleFunc("/api/ai/knowledge/generate-all", s.handleAIGenerateKnowledgeAll)
	mux.HandleFunc("/api/ai/knowledge/", s.handleAIKnowledgeOne) // /api/ai/knowledge/<session_id>
	mux.HandleFunc("/api/ai/chat", s.handleAIChat)
	mux.HandleFunc("/api/statusline", s.handleStatusline)
	mux.HandleFunc("/api/statusline/components", s.handleStatuslineComponents)
	mux.HandleFunc("/api/statusline/themes", s.handleStatuslineThemes)
	mux.HandleFunc("/api/statusline/presets", s.handleStatuslinePresets)
	mux.HandleFunc("/api/statusline/config", s.handleStatuslineConfig)
	mux.HandleFunc("/api/statusline/render", s.handleStatuslineRender)
}

func (s *Server) handleAIInsights(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w) {
		return
	}
	list, err := s.DB.InsightsList()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) handleAIGenerateInsights(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST required")
		return
	}
	if !s.requireAI(w) {
		return
	}
	go func() {
		out, err := ai.GenerateInsights(context.Background(), s.DB, s.AIClient, s.GenModel)
		if err != nil {
			s.Hub.Broadcast("insights_done", map[string]any{"error": err.Error()})
			return
		}
		s.Hub.Broadcast("insights_done", map[string]any{"count": len(out)})
	}()
	writeJSON(w, 202, map[string]string{"status": "running"})
}

func (s *Server) handleAIProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w) {
		return
	}
	content, ts, err := s.DB.ProfileGet()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"content": content, "generated_at": ts})
}

func (s *Server) handleAIGenerateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST required")
		return
	}
	if !s.requireAI(w) {
		return
	}
	go func() {
		out, err := ai.GenerateProfile(context.Background(), s.DB, s.AIClient, s.GenModel)
		if err != nil {
			s.Hub.Broadcast("profile_done", map[string]any{"error": err.Error()})
			return
		}
		s.Hub.Broadcast("profile_done", map[string]any{"length": len(out)})
	}()
	writeJSON(w, 202, map[string]string{"status": "running"})
}

// withSessions é um helper que carrega sessions e responde com JSON ou erro.
func (s *Server) sessionsAll() ([]*model.Session, error) {
	return s.DB.ListSessions()
}

// knowledgeOut decodifica os JSON arrays de string pra resposta legível.
type knowledgeOut struct {
	SessionID     string             `json:"session_id"`
	Problem       string             `json:"problem"`
	Solution      string             `json:"solution"`
	Decisions     []decisionOut      `json:"decisions"`
	Learnings     []string           `json:"learnings"`
	CodePatterns  []string           `json:"code_patterns"`
	TechUsed      []string           `json:"tech_used"`
	OpenQuestions []string           `json:"open_questions"`
	GeneratedAt   int64              `json:"generated_at"`
}

type decisionOut struct {
	Decision  string `json:"decision"`
	Rationale string `json:"rationale"`
}

func knowledgeToOut(k *index.Knowledge) knowledgeOut {
	out := knowledgeOut{
		SessionID:   k.SessionID,
		Problem:     k.Problem,
		Solution:    k.Solution,
		GeneratedAt: k.GeneratedAt,
	}
	_ = json.Unmarshal([]byte(k.Decisions), &out.Decisions)
	_ = json.Unmarshal([]byte(k.Learnings), &out.Learnings)
	_ = json.Unmarshal([]byte(k.CodePatterns), &out.CodePatterns)
	_ = json.Unmarshal([]byte(k.TechUsed), &out.TechUsed)
	_ = json.Unmarshal([]byte(k.OpenQuestions), &out.OpenQuestions)
	return out
}

func (s *Server) handleAIKnowledgeList(w http.ResponseWriter, r *http.Request) {
	all, err := s.DB.KnowledgeList()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := make([]knowledgeOut, 0, len(all))
	for _, k := range all {
		out = append(out, knowledgeToOut(k))
	}
	writeJSON(w, 200, out)
}

// handleAIKnowledgeOne devolve a entrada de uma session específica, ou 404
// se não existe. POST gera sob demanda pra essa session.
func (s *Server) handleAIKnowledgeOne(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ai/knowledge/")
	id = strings.TrimSuffix(id, "/")
	if id == "" || strings.Contains(id, "/") {
		writeErr(w, 400, "session id required")
		return
	}
	if r.Method == http.MethodPost {
		if !s.requireAI(w) {
			return
		}
		sess, err := s.DB.GetByID(id)
		if err != nil {
			writeErr(w, 404, "session not found")
			return
		}
		go func() {
			k, err := ai.GenerateKnowledge(context.Background(), s.DB, s.AIClient, s.GenModel, sess)
			if err != nil {
				s.Hub.Broadcast("knowledge_done", map[string]any{"session_id": id, "error": err.Error()})
				return
			}
			s.Hub.Broadcast("knowledge_done", map[string]any{"session_id": id, "ok": k != nil})
		}()
		writeJSON(w, 202, map[string]string{"status": "running"})
		return
	}
	k, err := s.DB.KnowledgeGet(id)
	if err != nil {
		writeErr(w, 404, "knowledge not generated yet")
		return
	}
	writeJSON(w, 200, knowledgeToOut(k))
}

// handleAIKnowledgeAggregated devolve as 5 visões cross-session: top patterns,
// decision history, recurring problems, tech frequency, open questions.
func (s *Server) handleAIKnowledgeAggregated(w http.ResponseWriter, r *http.Request) {
	agg, err := ai.AggregateKnowledge(s.DB)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, agg)
}

// handleAIChat — chat "Ness IA" com RAG sobre session_knowledge + summaries.
// Body: {"messages": [{"role": "user|assistant", "content": "..."}, ...]}
// Resposta: {"response": "...", "sources": [{"session_id", "similarity", ...}]}
func (s *Server) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST required")
		return
	}
	if !s.requireAI(w) {
		return
	}
	var body struct {
		Messages []ai.ChatMsg `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid json: "+err.Error())
		return
	}
	if len(body.Messages) == 0 {
		writeErr(w, 400, "messages required")
		return
	}
	embedModel := "nomic-embed-text"
	resp, err := ai.ChatWithContext(r.Context(), s.DB, s.AIClient, s.GenModel, embedModel, body.Messages)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, resp)
}

func (s *Server) handleAIGenerateKnowledgeAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST required")
		return
	}
	if !s.requireAI(w) {
		return
	}
	go func() {
		gen, cached, err := ai.GenerateKnowledgeAll(context.Background(), s.DB, s.AIClient, s.GenModel)
		payload := map[string]any{"generated": gen, "cached": cached}
		if err != nil {
			payload["error"] = err.Error()
		}
		s.Hub.Broadcast("knowledge_all_done", payload)
	}()
	writeJSON(w, 202, map[string]string{"status": "running"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// --- Sessions ---

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	all, err := s.sessionsAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, all)
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	// path: /api/sessions/<id> ou /api/sessions/<id>/messages
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeErr(w, 400, "session id required")
		return
	}
	id := parts[0]

	if len(parts) > 1 && parts[1] == "messages" {
		s.handleSessionMessages(w, r, id)
		return
	}

	sess, err := s.DB.GetByID(id)
	if err != nil {
		writeErr(w, 404, "session not found")
		return
	}
	writeJSON(w, 200, sess)
}

func (s *Server) handleSessionMessages(w http.ResponseWriter, r *http.Request, id string) {
	n := 10
	if v := r.URL.Query().Get("n"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 && i <= 200 {
			n = i
		}
	}
	sess, err := s.DB.GetByID(id)
	if err != nil {
		writeErr(w, 404, "session not found")
		return
	}
	msgs, err := parser.LastUserMessages(sess.JSONLPath, n)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, msgs)
}

// --- Stats ---

type statsResponse struct {
	Heatmap         [][]int                  `json:"heatmap"`
	HeatmapWeeks    int                      `json:"heatmap_weeks"`
	ModelDist       []kv                     `json:"model_distribution"`
	MonthCost       stats.MonthCost          `json:"month_cost"`
	WeekDelta       stats.WeekDelta          `json:"week_delta"`
	TopProjects     []projectCost            `json:"top_projects"`
	CacheSavingsUSD float64                  `json:"cache_savings_usd"`
	LongTailCost    []sessionSummary         `json:"long_tail_cost"`
	LongTailDur     []sessionSummary         `json:"long_tail_duration"`
	TotalSessions   int                      `json:"total_sessions"`
	TotalMsgs       int                      `json:"total_msgs"`
	TotalCostUSD    float64                  `json:"total_cost_usd"`
}

type kv struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type projectCost struct {
	ProjectDir string  `json:"project_dir"`
	CostUSD    float64 `json:"cost_usd"`
}

type sessionSummary struct {
	SessionID   string        `json:"session_id"`
	ProjectDir  string        `json:"project_dir"`
	StartTime   time.Time     `json:"start_time"`
	MessageCnt  int           `json:"message_count"`
	Duration    time.Duration `json:"duration_ns"`
	CostUSD     float64       `json:"cost_usd"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	all, err := s.sessionsAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	resp := statsResponse{
		Heatmap:         stats.HeatmapGrid(all, 12),
		HeatmapWeeks:    12,
		MonthCost:       stats.CostThisMonth(all, s.Pricing),
		WeekDelta:       stats.WeekDeltaFor(all, s.Pricing),
		CacheSavingsUSD: stats.CacheSavings(all, s.Pricing, 30),
		TotalSessions:   len(all),
	}

	dist := stats.ModelDistribution(all)
	for k, c := range dist {
		resp.ModelDist = append(resp.ModelDist, kv{Name: k, Count: c})
	}
	sort.Slice(resp.ModelDist, func(i, j int) bool {
		if resp.ModelDist[i].Count != resp.ModelDist[j].Count {
			return resp.ModelDist[i].Count > resp.ModelDist[j].Count
		}
		return resp.ModelDist[i].Name < resp.ModelDist[j].Name
	})

	costByProj := map[string]float64{}
	totalCost := 0.0
	for _, sess := range all {
		resp.TotalMsgs += sess.MessageCount
		if c, ok := s.Pricing.Cost(sess); ok {
			totalCost += c.USD
			costByProj[sess.ProjectDir] += c.USD
		}
	}
	resp.TotalCostUSD = totalCost
	for k, v := range costByProj {
		resp.TopProjects = append(resp.TopProjects, projectCost{ProjectDir: k, CostUSD: v})
	}
	sort.Slice(resp.TopProjects, func(i, j int) bool {
		if resp.TopProjects[i].CostUSD != resp.TopProjects[j].CostUSD {
			return resp.TopProjects[i].CostUSD > resp.TopProjects[j].CostUSD
		}
		return resp.TopProjects[i].ProjectDir < resp.TopProjects[j].ProjectDir
	})
	if len(resp.TopProjects) > 10 {
		resp.TopProjects = resp.TopProjects[:10]
	}

	for _, sess := range stats.LongTailByCost(append([]*model.Session{}, all...), s.Pricing, 5) {
		c, _ := s.Pricing.Cost(sess)
		resp.LongTailCost = append(resp.LongTailCost, sessionSummary{
			SessionID: sess.SessionID, ProjectDir: sess.ProjectDir,
			StartTime: sess.StartTime, MessageCnt: sess.MessageCount,
			Duration: sess.Duration(), CostUSD: c.USD,
		})
	}
	for _, sess := range stats.LongTailByDuration(all, 5) {
		c, _ := s.Pricing.Cost(sess)
		resp.LongTailDur = append(resp.LongTailDur, sessionSummary{
			SessionID: sess.SessionID, ProjectDir: sess.ProjectDir,
			StartTime: sess.StartTime, MessageCnt: sess.MessageCount,
			Duration: sess.Duration(), CostUSD: c.USD,
		})
	}

	writeJSON(w, 200, resp)
}

type behavioralResponse struct {
	TopWords    []stats.WordCount `json:"top_words"`
	TopPrefixes []stats.WordCount `json:"top_prefixes"`
	ErrorRate   float64           `json:"error_rate"`
	ErrorHits   int               `json:"error_hits"`
	ErrorTotal  int               `json:"error_total"`
	PeakHour    [24]int           `json:"peak_hour"`
}

func (s *Server) handleBehavioral(w http.ResponseWriter, r *http.Request) {
	all, err := s.sessionsAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rate, hits, total := stats.ErrorRate(all)
	writeJSON(w, 200, behavioralResponse{
		TopWords:    stats.TopWords(all, 25),
		TopPrefixes: stats.TopPrefixes(all, 15),
		ErrorRate:   rate,
		ErrorHits:   hits,
		ErrorTotal:  total,
		PeakHour:    stats.PeakHour(all),
	})
}

// --- Behavior advanced ---

type behaviorAdvancedResp struct {
	Bigrams       []stats.Bigram        `json:"bigrams"`
	Trigrams      []stats.Trigram       `json:"trigrams"`
	CoOccurrences []stats.CoOccur       `json:"co_occurrences"`
	Flow          stats.FlowSummary     `json:"flow"`
	Style         stats.StyleStats      `json:"style"`
	HighError     []stats.ErrorSession  `json:"high_error_sessions"`
	TimeCost      []stats.TimeCostPoint `json:"time_cost_points"`
}

func (s *Server) handleBehaviorAdvanced(w http.ResponseWriter, r *http.Request) {
	all, err := s.sessionsAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	resp := behaviorAdvancedResp{
		Bigrams:       stats.TopBigrams(all, 20),
		Trigrams:      stats.TopTrigrams(all, 12),
		CoOccurrences: stats.CoOccurrences(all, 3, 30),
		Flow:          stats.FlowDistribution(all),
		Style:         stats.StyleComparison(all),
		HighError:     stats.HighErrorSessions(all, 0.15),
		TimeCost:      stats.TimeCostPoints(all, s.Pricing),
	}
	writeJSON(w, 200, resp)
}

// --- Costs ---

type costsResponse struct {
	ByDay           []dailyCost     `json:"by_day"`
	ByProject       []projectCost   `json:"by_project"`
	ByModel         []modelCost     `json:"by_model"`
	CacheSavingsUSD float64         `json:"cache_savings_usd"`
	MonthCost       stats.MonthCost `json:"month_cost"`
}

type dailyCost struct {
	Date    string  `json:"date"` // YYYY-MM-DD
	CostUSD float64 `json:"cost_usd"`
}

type modelCost struct {
	Model   string  `json:"model"`
	CostUSD float64 `json:"cost_usd"`
}

func (s *Server) handleCosts(w http.ResponseWriter, r *http.Request) {
	all, err := s.sessionsAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	now := time.Now()
	days := 30
	dayBins := map[string]float64{}
	byProj := map[string]float64{}
	byModel := map[string]float64{}
	for _, sess := range all {
		c, ok := s.Pricing.Cost(sess)
		if !ok {
			continue
		}
		byProj[sess.ProjectDir] += c.USD
		byModel[sess.Model] += c.USD
		d := now.Sub(sess.StartTime)
		if d >= 0 && d < time.Duration(days)*24*time.Hour {
			date := sess.StartTime.Format("2006-01-02")
			dayBins[date] += c.USD
		}
	}
	resp := costsResponse{
		CacheSavingsUSD: stats.CacheSavings(all, s.Pricing, 30),
		MonthCost:       stats.CostThisMonth(all, s.Pricing),
	}
	// fill all 30 days, mesmo que zero
	for i := days - 1; i >= 0; i-- {
		dt := now.AddDate(0, 0, -i).Format("2006-01-02")
		resp.ByDay = append(resp.ByDay, dailyCost{Date: dt, CostUSD: dayBins[dt]})
	}
	for k, v := range byProj {
		resp.ByProject = append(resp.ByProject, projectCost{ProjectDir: k, CostUSD: v})
	}
	sort.Slice(resp.ByProject, func(i, j int) bool {
		if resp.ByProject[i].CostUSD != resp.ByProject[j].CostUSD {
			return resp.ByProject[i].CostUSD > resp.ByProject[j].CostUSD
		}
		return resp.ByProject[i].ProjectDir < resp.ByProject[j].ProjectDir
	})
	for k, v := range byModel {
		resp.ByModel = append(resp.ByModel, modelCost{Model: k, CostUSD: v})
	}
	sort.Slice(resp.ByModel, func(i, j int) bool {
		if resp.ByModel[i].CostUSD != resp.ByModel[j].CostUSD {
			return resp.ByModel[i].CostUSD > resp.ByModel[j].CostUSD
		}
		return resp.ByModel[i].Model < resp.ByModel[j].Model
	})
	writeJSON(w, 200, resp)
}

// --- Timeline ---

type timelineResponse struct {
	Days []dayBucket `json:"days"`
}

type dayBucket struct {
	Date     string             `json:"date"`
	Sessions []*model.Session   `json:"sessions"`
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	all, err := s.sessionsAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	from, to := timeRange(r, 7) // default últimos 7 dias
	groups := map[string][]*model.Session{}
	for _, sess := range all {
		if sess.StartTime.Before(from) || sess.StartTime.After(to) {
			continue
		}
		date := sess.StartTime.Format("2006-01-02")
		groups[date] = append(groups[date], sess)
	}
	dates := make([]string, 0, len(groups))
	for d := range groups {
		dates = append(dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	resp := timelineResponse{}
	for _, d := range dates {
		sort.Slice(groups[d], func(i, j int) bool {
			return groups[d][i].StartTime.After(groups[d][j].StartTime)
		})
		resp.Days = append(resp.Days, dayBucket{Date: d, Sessions: groups[d]})
	}
	writeJSON(w, 200, resp)
}

func timeRange(r *http.Request, defaultDays int) (time.Time, time.Time) {
	now := time.Now()
	from := now.AddDate(0, 0, -defaultDays)
	to := now
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t.AddDate(0, 0, 1) // inclusive
		}
	}
	return from, to
}

// --- Tools ---

type toolStatResp struct {
	Name        string `json:"name"`
	TotalCalls  int    `json:"total_calls"`
	NumSessions int    `json:"num_sessions"`
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	all, err := s.sessionsAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	calls := map[string]int{}
	sess := map[string]map[string]bool{}
	for _, ss := range all {
		for t, c := range ss.ToolCalls {
			calls[t] += c
			if sess[t] == nil {
				sess[t] = map[string]bool{}
			}
			sess[t][ss.SessionID] = true
		}
	}
	out := make([]toolStatResp, 0, len(calls))
	for t, c := range calls {
		out = append(out, toolStatResp{Name: t, TotalCalls: c, NumSessions: len(sess[t])})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalCalls != out[j].TotalCalls {
			return out[i].TotalCalls > out[j].TotalCalls
		}
		return out[i].Name < out[j].Name
	})
	writeJSON(w, 200, out)
}

func (s *Server) handleToolDrill(w http.ResponseWriter, r *http.Request) {
	// /api/tools/<name>/sessions
	path := strings.TrimPrefix(r.URL.Path, "/api/tools/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "sessions" {
		writeErr(w, 400, "expected /api/tools/<name>/sessions")
		return
	}
	tool := parts[0]
	all, err := s.sessionsAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	type hit struct {
		Session *model.Session `json:"session"`
		Count   int            `json:"count"`
	}
	var hits []hit
	for _, ss := range all {
		if c, ok := ss.ToolCalls[tool]; ok && c > 0 {
			hits = append(hits, hit{ss, c})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Count != hits[j].Count {
			return hits[i].Count > hits[j].Count
		}
		return hits[i].Session.SessionID < hits[j].Session.SessionID
	})
	if len(hits) > 50 {
		hits = hits[:50]
	}
	writeJSON(w, 200, hits)
}

// --- Search ---

type searchResp struct {
	Mode    string                `json:"mode"`
	Results []searchResultEntry   `json:"results"`
}

type searchResultEntry struct {
	Session *model.Session `json:"session"`
	Snippet string         `json:"snippet,omitempty"`
	Role    string         `json:"role,omitempty"`
	Rank    float64        `json:"rank,omitempty"`
}

// metaResp é o payload do Studio Meta tab. Cada bloco é uma "card" de chart.
type metaResp struct {
	GeneratedAt   int64                  `json:"generated_at"`
	FileReuse     []index.FileReuse      `json:"file_reuse"`     // top arquivos tocados em N+ sessions
	CostByTicket  []costByTicketEntry    `json:"cost_by_ticket"` // CC-1234 → custo agregado
	Convergence   []index.ConvergenceStats `json:"convergence_by_model"` // resolved_at_turn p50/p90 por modelo
	LoopsDetected []index.LoopHit        `json:"loops_detected"` // top N loops (já existia em S.2)
}

type costByTicketEntry struct {
	Ticket   string   `json:"ticket"`
	Sessions int      `json:"sessions"`
	CostUSD  float64  `json:"cost_usd"`
	Branches []string `json:"branches"`
}

// handleMeta agrega métricas de meta-análise pro Studio Meta tab.
// Cada bloco é uma query SQL independente — render frontend escolhe quais
// renderizar. Não é cacheado server-side ainda — o dataset é pequeno (≤ N
// rows por bloco).
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	resp := metaResp{GeneratedAt: time.Now().Unix()}

	// File reuse top
	if reuse, err := s.DB.FileReuseTop(2, 20); err == nil {
		resp.FileReuse = reuse
	}

	// Cost by ticket — precisa pricing pra computar custo total
	if rows, err := s.DB.CostByTicketRows(); err == nil {
		all, _ := s.sessionsAll()
		byID := map[string]*model.Session{}
		for _, sess := range all {
			byID[sess.SessionID] = sess
		}
		// re-walk pra somar custo das sessions de cada ticket
		costByTicket := map[string]float64{}
		sessionsByTicket := map[string][]string{}
		for _, sess := range all {
			t := index.ExtractTicket(sess.GitBranch)
			if t == "" {
				continue
			}
			sessionsByTicket[t] = append(sessionsByTicket[t], sess.SessionID)
			if s.Pricing != nil {
				if c, ok := s.Pricing.Cost(sess); ok {
					costByTicket[t] += c.USD
				}
			}
		}
		for t, info := range rows {
			resp.CostByTicket = append(resp.CostByTicket, costByTicketEntry{
				Ticket:   t,
				Sessions: info.Sessions,
				CostUSD:  costByTicket[t],
				Branches: info.Branches,
			})
		}
		// Sort por cost desc
		sort.Slice(resp.CostByTicket, func(i, j int) bool {
			return resp.CostByTicket[i].CostUSD > resp.CostByTicket[j].CostUSD
		})
		if len(resp.CostByTicket) > 20 {
			resp.CostByTicket = resp.CostByTicket[:20]
		}
	}

	// Convergence speed por modelo
	if conv, err := s.DB.ConvergenceByModel(); err == nil {
		resp.Convergence = conv
	}

	// Loops detected (≥3× em ≤60min — mesmo default do TUI Detailed)
	if loops, err := s.DB.DetectLoops(3, 3600); err == nil {
		resp.LoopsDetected = loops
	}

	writeJSON(w, 200, resp)
}

// handleConfig: GET retorna config atual; POST sobrescreve.
// Body do POST espelha o struct Config (apenas seção [notify] mexível por agora).
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, 200, s.Config)
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, 405, "GET or POST only")
		return
	}
	var body config.Config
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	// Só aceita atualização da seção Notify por enquanto — outras seções
	// exigem reload do servidor (Cost, AI) e merecem flow próprio.
	s.Config.Notify = body.Notify
	if err := config.SaveConfig(s.ConfigPath, s.Config); err != nil {
		writeErr(w, 500, "save: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"saved": true,
		"note":  "Config persistida. Reinicie o daemon (nessy serve) pra aplicar.",
	})
}

// handleAdvise devolve recomendações deterministas do advisor.
// GET /api/advise — sem params. Lazy: sempre re-roda (queries são leves).
func (s *Server) handleAdvise(w http.ResponseWriter, r *http.Request) {
	all, err := s.sessionsAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	recs, err := advisor.Run(s.DB, s.Pricing, all)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"recommendations": recs,
		"count":           len(recs),
	})
}

// handleSearch suporta 4 modos:
//
//   - hybrid  (DEFAULT) — metadata + full-text via FTS5, deduplicado por session.
//     Acha tanto match em path/branch/msg/summary quanto dentro do conteúdo
//     das mensagens. Tipicamente o que o user quer.
//   - meta    (`:meta <q>`) — só metadata (path/branch/msg/summary/model/tools)
//   - body    (`:body <q>`) — só FTS5 sobre messages
//   - semantic (`:sim <q>`) — cosine similarity sobre embeddings
//
// Filtros parseados do query (em qualquer modo):
//
//   project:<substr>, branch:<substr>, model:<substr>, since:<dur> (ex 7d, 24h),
//   cost:>N, cost:<N
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	modeFlag := r.URL.Query().Get("mode")

	// Default = mostrar TODOS os hits (expand). Frontend pode mandar
	// ?group=true pra agrupar/dedup por session. Mantemos :all como alias
	// histórico (compatibilidade), mas não precisa mais.
	expandFlag := r.URL.Query().Get("group") != "true"
	if strings.HasPrefix(q, ":all ") {
		q = strings.TrimSpace(q[5:])
		expandFlag = true
		modeFlag = "hybrid"
	}
	if r.URL.Query().Get("expand") == "true" {
		expandFlag = true
	}
	if r.URL.Query().Get("expand") == "false" {
		expandFlag = false
	}
	// fuzzy=true ativa Porter stemmer (FTS5 default). Default é exact —
	// faz post-filter LIKE pra não casar 'dock' quando user busca 'docker'.
	fuzzyFlag := r.URL.Query().Get("fuzzy") == "true"

	// Default agora é hybrid (metadata + FTS combinados), não só metadata.
	mode := modeFlag
	if mode == "" || mode == "metadata" {
		mode = "hybrid"
	}
	switch {
	case strings.HasPrefix(q, ":body "):
		mode = "fts"
		q = strings.TrimSpace(q[6:])
	case strings.HasPrefix(q, ":meta "):
		mode = "metadata"
		q = strings.TrimSpace(q[6:])
	case strings.HasPrefix(q, ":sim "):
		mode = "semantic"
		q = strings.TrimSpace(q[5:])
	}

	// Parse filtros do query
	filters, residual := parseSearchFilters(q)
	q = residual

	all, err := s.sessionsAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	caches, _ := s.DB.AICacheList()
	summaryByID := map[string]string{}
	embeddingByID := map[string][]float32{}
	for _, c := range caches {
		if c.Summary != "" {
			summaryByID[c.SessionID] = c.Summary
		}
		if len(c.Embedding) > 0 {
			embeddingByID[c.SessionID] = ai.DecodeEmbedding(c.Embedding)
		}
	}

	// Aplica filtros (date, cost, project, etc) — pré-corta o universo de busca
	candidates := s.applySearchFilters(all, filters)

	resp := searchResp{Mode: mode}
	if q == "" {
		for _, sess := range candidates {
			resp.Results = append(resp.Results, searchResultEntry{Session: sess})
		}
		writeJSON(w, 200, resp)
		return
	}

	switch mode {
	case "fts":
		s.searchFTS(q, candidates, &resp, fuzzyFlag)
	case "semantic":
		s.searchSemantic(r.Context(), q, candidates, embeddingByID, &resp)
	case "metadata":
		s.searchMetadata(q, candidates, summaryByID, &resp)
	case "rrf":
		// RRF explícito — sempre tenta usar dense + bm25
		s.searchRRF(r.Context(), q, candidates, summaryByID, embeddingByID, &resp)
	default: // hybrid — auto-upgrade pra RRF se AI tá enabled e há embeddings
		if s.AIEnabled && len(embeddingByID) > 0 {
			resp.Mode = "hybrid+rrf"
			s.searchRRF(r.Context(), q, candidates, summaryByID, embeddingByID, &resp)
		} else {
			s.searchHybrid(q, candidates, summaryByID, &resp, expandFlag, fuzzyFlag)
		}
	}
	writeJSON(w, 200, resp)
}

// searchRRF combina BM25 (FTS5) + dense (embeddings) + metadata via Reciprocal
// Rank Fusion. Detecta query type (identifier vs prose) pra ajustar pesos.
//
// Pipeline:
//  1. FTS5 → ranked list por BM25 score (top 50)
//  2. Embed query → cosine vs todos embeddings → top 50
//  3. Metadata match → ordem de match (se houver)
//  4. RRF merge com pesos derivados de DetectQueryType
//  5. Map back pra session entries com role="rrf:..." mostrando fontes
//
// Fallback elegante: se embed falhar (Ollama down), só BM25 + metadata.
func (s *Server) searchRRF(
	ctx context.Context,
	q string,
	sessions []*model.Session,
	summaries map[string]string,
	embeddingByID map[string][]float32,
	resp *searchResp,
) {
	byID := map[string]*model.Session{}
	for _, sess := range sessions {
		byID[sess.SessionID] = sess
	}
	candidates := map[string]bool{}
	for id := range byID {
		candidates[id] = true
	}

	rankings := map[string][]string{}

	// 1. BM25 via FTS5
	if ftsResults, err := s.DB.SearchFTSExact(q); err == nil {
		seen := map[string]bool{}
		for _, r := range ftsResults {
			if !candidates[r.SessionID] || seen[r.SessionID] {
				continue
			}
			seen[r.SessionID] = true
			rankings["bm25"] = append(rankings["bm25"], r.SessionID)
		}
	}

	// 2. Dense — embed query, cosine vs todos
	if s.AIClient != nil && len(embeddingByID) > 0 {
		embCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		queryEmb, err := s.AIClient.Embedding(embCtx, "nomic-embed-text", q)
		if err == nil && len(queryEmb) > 0 {
			type scored struct {
				id  string
				sim float64
			}
			var scoredList []scored
			for id, emb := range embeddingByID {
				if !candidates[id] {
					continue
				}
				sim := ai.Cosine(queryEmb, emb)
				if sim > 0 {
					scoredList = append(scoredList, scored{id, sim})
				}
			}
			sort.Slice(scoredList, func(i, j int) bool {
				if scoredList[i].sim != scoredList[j].sim {
					return scoredList[i].sim > scoredList[j].sim
				}
				return scoredList[i].id < scoredList[j].id
			})
			// Cap em top 50 — RRF não precisa do tail
			if len(scoredList) > 50 {
				scoredList = scoredList[:50]
			}
			for _, s := range scoredList {
				rankings["dense"] = append(rankings["dense"], s.id)
			}
		}
	}

	// 3. Metadata — ordem natural (start_time desc geralmente)
	lower := strings.ToLower(q)
	for _, sess := range sessions {
		if hit, _, _ := metaMatchExt(sess, lower, summaries); hit {
			rankings["metadata"] = append(rankings["metadata"], sess.SessionID)
		}
	}

	// 4. RRF merge
	weights := search.WeightsFor(search.DetectQueryType(q))
	weights["metadata"] = 0.7 // metadata é tiebreaker, não primary signal
	hits := search.MergeRRF(rankings, weights)

	// 5. Map back. Snippet/role do FTS quando disponível, senão metadata.
	ftsSnippets := map[string]index.SearchResult{}
	if results, err := s.DB.SearchFTSExact(q); err == nil {
		for _, r := range results {
			if _, ok := ftsSnippets[r.SessionID]; !ok {
				ftsSnippets[r.SessionID] = r
			}
		}
	}
	for _, h := range hits {
		sess, ok := byID[h.SessionID]
		if !ok {
			continue
		}
		entry := searchResultEntry{
			Session: sess,
			Role:    fmt.Sprintf("rrf:%s", strings.Join(h.Sources, "+")),
		}
		if ftsHit, ok := ftsSnippets[h.SessionID]; ok {
			entry.Snippet = ftsHit.Snippet
		} else if hit, snippet, _ := metaMatchExt(sess, lower, summaries); hit {
			entry.Snippet = snippet
		}
		resp.Results = append(resp.Results, entry)
	}
}

// searchHybrid roda metadata + FTS5 e combina. expand controla dedup,
// fuzzy controla Porter stemmer.
func (s *Server) searchHybrid(q string, sessions []*model.Session, summaries map[string]string, resp *searchResp, expand bool, fuzzy bool) {
	matchCount := map[string]int{} // por session, pra count badge
	seen := map[string]bool{}

	// Metadata primeiro
	lower := strings.ToLower(q)
	for _, sess := range sessions {
		if hit, snippet, role := metaMatchExt(sess, lower, summaries); hit {
			seen[sess.SessionID] = true
			resp.Results = append(resp.Results, searchResultEntry{
				Session: sess, Snippet: snippet, Role: role,
			})
		}
	}

	// FTS5 — exato (LIKE post-filter) ou fuzzy (Porter stem)
	var results []index.SearchResult
	var err error
	if fuzzy {
		results, err = s.DB.SearchFTS(q)
	} else {
		results, err = s.DB.SearchFTSExact(q)
	}
	if err != nil {
		results, _ = s.DB.SearchLike(q)
	}
	byID := map[string]*model.Session{}
	for _, sess := range sessions {
		byID[sess.SessionID] = sess
	}

	for _, r := range results {
		matchCount[r.SessionID]++
		sess, ok := byID[r.SessionID]
		if !ok {
			continue
		}
		if expand {
			// Cada hit vira entry separada
			resp.Results = append(resp.Results, searchResultEntry{
				Session: sess, Snippet: r.Snippet, Role: "body:" + r.Role, Rank: r.Rank,
			})
			continue
		}
		// Deduplicado: só primeiro hit da session
		if seen[r.SessionID] {
			continue
		}
		seen[r.SessionID] = true
		resp.Results = append(resp.Results, searchResultEntry{
			Session: sess, Snippet: r.Snippet, Role: "body:" + r.Role, Rank: r.Rank,
		})
	}

	// Anota match count na role pra UI mostrar "+15 matches" badge
	if !expand {
		for i, e := range resp.Results {
			if c := matchCount[e.Session.SessionID]; c > 1 {
				resp.Results[i].Role = fmt.Sprintf("%s (+%d)", e.Role, c-1)
			}
		}
	}
}

// searchMetadata busca substring case-insensitive em vários campos +
// AI summary + tools usados. Devolve snippet do campo que matchou.
func (s *Server) searchMetadata(q string, sessions []*model.Session, summaries map[string]string, resp *searchResp) {
	lower := strings.ToLower(q)
	for _, sess := range sessions {
		if hit, snippet, role := metaMatchExt(sess, lower, summaries); hit {
			resp.Results = append(resp.Results, searchResultEntry{
				Session: sess, Snippet: snippet, Role: role,
			})
		}
	}
}

// searchFTS faz query FTS5 e mapeia pra sessions. exact=true post-filtra
// com LIKE pra não pegar matches só pelo Porter stem.
func (s *Server) searchFTS(q string, sessions []*model.Session, resp *searchResp, fuzzy bool) {
	var results []index.SearchResult
	var err error
	if fuzzy {
		results, err = s.DB.SearchFTS(q)
	} else {
		results, err = s.DB.SearchFTSExact(q)
	}
	if err != nil {
		results, _ = s.DB.SearchLike(q)
	}
	byID := map[string]*model.Session{}
	for _, sess := range sessions {
		byID[sess.SessionID] = sess
	}
	seen := map[string]bool{}
	for _, r := range results {
		if seen[r.SessionID] {
			continue
		}
		seen[r.SessionID] = true
		if sess, ok := byID[r.SessionID]; ok {
			resp.Results = append(resp.Results, searchResultEntry{
				Session: sess, Snippet: r.Snippet, Role: r.Role, Rank: r.Rank,
			})
		}
	}
}

// searchSemantic embeda a query e ranqueia sessions por cosine similarity.
func (s *Server) searchSemantic(ctx context.Context, q string, sessions []*model.Session, embByID map[string][]float32, resp *searchResp) {
	if !s.AIEnabled || s.AIClient == nil {
		// fallback: vira metadata
		s.searchMetadata(q, sessions, nil, resp)
		return
	}
	queryEmb, err := s.AIClient.Embedding(ctx, "nomic-embed-text", q)
	if err != nil || len(queryEmb) == 0 {
		s.searchMetadata(q, sessions, nil, resp)
		return
	}
	type ranked struct {
		sess *model.Session
		sim  float64
	}
	var hits []ranked
	for _, sess := range sessions {
		emb, ok := embByID[sess.SessionID]
		if !ok {
			continue
		}
		hits = append(hits, ranked{sess, cosineSim(queryEmb, emb)})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].sim > hits[j].sim })
	for i, h := range hits {
		if i >= 30 {
			break
		}
		resp.Results = append(resp.Results, searchResultEntry{
			Session: h.sess, Rank: h.sim,
			Snippet: fmt.Sprintf("similarity: %.2f", h.sim),
		})
	}
}

func cosineSim(a, b []float32) float64 {
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
	return dot / (sqrtF(na) * sqrtF(nb))
}

func sqrtF(x float64) float64 {
	if x < 0 {
		return 0
	}
	return math.Sqrt(x)
}

// metaMatchExt expande metaMatch pra cobrir AI summary, model, tools e
// devolve qual campo matchou pro snippet.
func metaMatchExt(sess *model.Session, q string, summaries map[string]string) (bool, string, string) {
	if sess == nil {
		return false, "", ""
	}
	checks := []struct {
		field string
		text  string
	}{
		{"path", sess.ProjectDir},
		{"branch", sess.GitBranch},
		{"first_msg", sess.FirstUserMsg},
		{"last_msg", sess.LastUserMsg},
		{"id", sess.SessionID},
		{"model", sess.Model},
	}
	for _, c := range checks {
		if c.text == "" {
			continue
		}
		if i := strings.Index(strings.ToLower(c.text), q); i >= 0 {
			return true, makeSnippet(c.text, i, len(q)), c.field
		}
	}
	for tool := range sess.ToolCalls {
		if strings.Contains(strings.ToLower(tool), q) {
			return true, fmt.Sprintf("tool: %s", tool), "tool"
		}
	}
	if sum := summaries[sess.SessionID]; sum != "" {
		if i := strings.Index(strings.ToLower(sum), q); i >= 0 {
			return true, makeSnippet(sum, i, len(q)), "summary"
		}
	}
	return false, "", ""
}

// makeSnippet retorna texto curto ao redor do match com [highlight] no termo.
func makeSnippet(text string, idx, qLen int) string {
	if idx < 0 || idx >= len(text) {
		return text
	}
	start := idx - 30
	if start < 0 {
		start = 0
	}
	end := idx + qLen + 30
	if end > len(text) {
		end = len(text)
	}
	prefix := ""
	suffix := ""
	if start > 0 {
		prefix = "…"
	}
	if end < len(text) {
		suffix = "…"
	}
	return prefix + text[start:idx] + "[" + text[idx:idx+qLen] + "]" + text[idx+qLen:end] + suffix
}

// searchFilters carrega filtros parseados do query.
type searchFilters struct {
	project   string
	branch    string
	model     string
	since     time.Duration
	costMin   float64
	costMax   float64
	hasFilter bool // pelo menos 1 filtro setado
}

// parseSearchFilters extrai `field:value` tokens. Tokens não-filtro voltam em residual.
func parseSearchFilters(q string) (searchFilters, string) {
	f := searchFilters{costMin: -1, costMax: -1}
	parts := strings.Fields(q)
	var rest []string
	for _, p := range parts {
		colon := strings.IndexByte(p, ':')
		if colon < 1 {
			rest = append(rest, p)
			continue
		}
		key := strings.ToLower(p[:colon])
		val := p[colon+1:]
		switch key {
		case "project":
			f.project = strings.ToLower(val)
			f.hasFilter = true
		case "branch":
			f.branch = strings.ToLower(val)
			f.hasFilter = true
		case "model":
			f.model = strings.ToLower(val)
			f.hasFilter = true
		case "since":
			d, err := parseDurAlias(val)
			if err == nil {
				f.since = d
				f.hasFilter = true
			} else {
				rest = append(rest, p)
			}
		case "cost":
			if strings.HasPrefix(val, ">") {
				if n, err := strconv.ParseFloat(val[1:], 64); err == nil {
					f.costMin = n
					f.hasFilter = true
					continue
				}
			}
			if strings.HasPrefix(val, "<") {
				if n, err := strconv.ParseFloat(val[1:], 64); err == nil {
					f.costMax = n
					f.hasFilter = true
					continue
				}
			}
			rest = append(rest, p)
		default:
			rest = append(rest, p)
		}
	}
	return f, strings.Join(rest, " ")
}

// parseDurAlias aceita "7d", "24h", "30m" — go time.ParseDuration não tem "d".
func parseDurAlias(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// applySearchFilters reduz o universo de sessions baseado nos filtros parseados.
func (s *Server) applySearchFilters(all []*model.Session, f searchFilters) []*model.Session {
	if !f.hasFilter {
		return all
	}
	cutoff := time.Time{}
	if f.since > 0 {
		cutoff = time.Now().Add(-f.since)
	}
	out := make([]*model.Session, 0, len(all))
	for _, sess := range all {
		if f.project != "" && !strings.Contains(strings.ToLower(sess.ProjectDir), f.project) {
			continue
		}
		if f.branch != "" && !strings.Contains(strings.ToLower(sess.GitBranch), f.branch) {
			continue
		}
		if f.model != "" && !strings.Contains(strings.ToLower(sess.Model), f.model) {
			continue
		}
		if !cutoff.IsZero() && sess.StartTime.Before(cutoff) {
			continue
		}
		if f.costMin >= 0 || f.costMax >= 0 {
			c, _ := s.Pricing.Cost(sess)
			if f.costMin >= 0 && c.USD < f.costMin {
				continue
			}
			if f.costMax >= 0 && c.USD > f.costMax {
				continue
			}
		}
		out = append(out, sess)
	}
	return out
}

func metaMatch(sess *model.Session, q string) bool {
	hit, _, _ := metaMatchExt(sess, q, nil)
	return hit
}

// --- AI ---

type aiHealthResp struct {
	Enabled         bool   `json:"enabled"`
	OllamaReachable bool   `json:"ollama_reachable"`
	GenModel        string `json:"gen_model"`
	EmbedModel      string `json:"embed_model"`
	Cached          int    `json:"cached"`
	Total           int    `json:"total"`
	Queued          int    `json:"queued"`
}

func (s *Server) handleAIHealth(w http.ResponseWriter, r *http.Request) {
	resp := aiHealthResp{Enabled: s.AIEnabled, GenModel: s.GenModel}
	if s.AIClient != nil {
		resp.OllamaReachable = s.AIClient.Health(r.Context())
	}
	if s.AIWorker != nil {
		resp.EmbedModel = s.AIWorker.EmbedModel
		resp.Queued = s.AIWorker.QueuedCount()
	}
	all, _ := s.DB.ListSessions()
	resp.Total = len(all)
	caches, _ := s.DB.AICacheList()
	for _, c := range caches {
		if c.Summary != "" {
			resp.Cached++
		}
	}
	writeJSON(w, 200, resp)
}

func (s *Server) requireAI(w http.ResponseWriter) bool {
	if !s.AIEnabled || s.AIClient == nil {
		writeErr(w, 503, "ai disabled")
		return false
	}
	return true
}

func (s *Server) handleAISummaries(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w) {
		return
	}
	caches, err := s.DB.AICacheList()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	type entry struct {
		SessionID string `json:"session_id"`
		Summary   string `json:"summary"`
		Cluster   int    `json:"cluster"`
		Label     string `json:"label"`
	}
	out := make([]entry, 0, len(caches))
	for _, c := range caches {
		out = append(out, entry{
			SessionID: c.SessionID,
			Summary:   c.Summary,
			Cluster:   c.TopicCluster,
			Label:     c.TopicLabel,
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleAISummary(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/ai/summary/")
	if id == "" {
		writeErr(w, 400, "session id required")
		return
	}
	cache, err := s.DB.AICacheGet(id)
	if err == nil && cache.Summary != "" {
		writeJSON(w, 200, map[string]any{
			"session_id":   id,
			"summary":      cache.Summary,
			"generated_at": cache.GeneratedAt,
			"cached":       true,
		})
		return
	}
	if s.AIWorker != nil {
		s.AIWorker.Enqueue(id)
	}
	writeJSON(w, 202, map[string]any{"session_id": id, "status": "queued"})
}

func (s *Server) handleAISimilar(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/ai/similar/")
	if id == "" {
		writeErr(w, 400, "session id required")
		return
	}
	n := 10
	if v := r.URL.Query().Get("n"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 && i <= 50 {
			n = i
		}
	}
	results, err := ai.FindSimilar(s.DB, id, n)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, results)
}

func (s *Server) handleAIClusters(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w) {
		return
	}
	caches, err := s.DB.AICacheList()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	groups := map[int]*ai.ClusterInfo{}
	for _, c := range caches {
		if c.TopicCluster < 0 {
			continue
		}
		ci, ok := groups[c.TopicCluster]
		if !ok {
			ci = &ai.ClusterInfo{ClusterID: c.TopicCluster, Label: c.TopicLabel}
			groups[c.TopicCluster] = ci
		}
		ci.SessionIDs = append(ci.SessionIDs, c.SessionID)
	}
	out := make([]ai.ClusterInfo, 0, len(groups))
	for _, ci := range groups {
		out = append(out, *ci)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].SessionIDs) != len(out[j].SessionIDs) {
			return len(out[i].SessionIDs) > len(out[j].SessionIDs)
		}
		return out[i].ClusterID < out[j].ClusterID
	})
	writeJSON(w, 200, out)
}

func (s *Server) handleAIRecomputeClusters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST required")
		return
	}
	if !s.requireAI(w) {
		return
	}
	go func() {
		out, err := ai.RecomputeClusters(context.Background(), s.DB, s.AIClient, s.GenModel)
		if err == nil {
			s.Hub.Broadcast("clusters_done", map[string]any{"clusters": out})
		}
	}()
	writeJSON(w, 202, map[string]string{"status": "running"})
}

func (s *Server) handleAIGenerateAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST required")
		return
	}
	if !s.requireAI(w) {
		return
	}
	all, err := s.DB.ListSessions()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	queued := 0
	for _, sess := range all {
		c, err := s.DB.AICacheGet(sess.SessionID)
		if err == nil && c.Summary != "" && c.JSONLMtime == sess.JSONLMtime.UnixNano() {
			continue
		}
		s.AIWorker.Enqueue(sess.SessionID)
		queued++
	}
	writeJSON(w, 200, map[string]int{"queued": queued})
}

func (s *Server) handleAIGenerateOne(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST required")
		return
	}
	if !s.requireAI(w) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/ai/generate/")
	if id == "" {
		writeErr(w, 400, "session id required")
		return
	}
	s.AIWorker.Enqueue(id)
	writeJSON(w, 202, map[string]string{"status": "queued", "session_id": id})
}

var _ = index.AICache{}

// --- Refresh ---

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST required")
		return
	}
	root := claudeProjectsRoot()
	st, err := s.DB.Reindex(root)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.Hub.Broadcast("reindex_done", st)
	writeJSON(w, 200, st)
}

func claudeProjectsRoot() string {
	home := homedir()
	return filepath.Join(home, ".claude", "projects")
}

func homedir() string {
	if h, err := userHomeDir(); err == nil {
		return h
	}
	return "."
}

// userHomeDir é alias pra os.UserHomeDir, separado pra facilitar test.
func userHomeDir() (string, error) {
	return userHomeImpl()
}

// --- Export ---

type exportPayload struct {
	Session *model.Session         `json:"session"`
	Cost    *pricing.Cost          `json:"cost,omitempty"`
	Messages []parser.Message      `json:"messages,omitempty"`
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/export/")
	if id == "" {
		writeErr(w, 400, "session id required")
		return
	}
	sess, err := s.DB.GetByID(id)
	if err != nil {
		writeErr(w, 404, "session not found")
		return
	}
	payload := exportPayload{Session: sess}
	if c, ok := s.Pricing.Cost(sess); ok {
		payload.Cost = &c
	}
	if msgs, err := parser.ParseMessages(sess.JSONLPath); err == nil {
		payload.Messages = msgs
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, id))
	writeJSON(w, 200, payload)
}

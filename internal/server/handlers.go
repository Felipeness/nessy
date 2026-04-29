package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/parser"
	"github.com/felipeness/claude-history/internal/pricing"
	"github.com/felipeness/claude-history/internal/stats"
)

func registerAPI(mux *http.ServeMux, s *Server) {
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
}

// withSessions é um helper que carrega sessions e responde com JSON ou erro.
func (s *Server) sessionsAll() ([]*model.Session, error) {
	return s.DB.ListSessions()
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

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "metadata"
	}
	all, err := s.sessionsAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	resp := searchResp{Mode: mode}
	if q == "" {
		// retorna todas
		for _, sess := range all {
			resp.Results = append(resp.Results, searchResultEntry{Session: sess})
		}
		writeJSON(w, 200, resp)
		return
	}
	if mode == "fts" {
		results, err := s.DB.SearchFTS(q)
		if err != nil {
			results, err = s.DB.SearchLike(q)
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
		}
		byID := map[string]*model.Session{}
		for _, sess := range all {
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
	} else {
		lower := strings.ToLower(q)
		for _, sess := range all {
			if metaMatch(sess, lower) {
				resp.Results = append(resp.Results, searchResultEntry{Session: sess})
			}
		}
	}
	writeJSON(w, 200, resp)
}

func metaMatch(sess *model.Session, q string) bool {
	for _, h := range []string{sess.ProjectDir, sess.GitBranch, sess.FirstUserMsg, sess.LastUserMsg, sess.SessionID} {
		if strings.Contains(strings.ToLower(h), q) {
			return true
		}
	}
	return false
}

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

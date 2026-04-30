package server

import (
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/felipeness/claude-history/internal/ai"
	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
	"github.com/felipeness/claude-history/internal/stats"
)

// statuslineCache cacheia respostas de /api/statusline por session+project.
// Statusline é chamado a cada turno do Claude Code — sem cache, DetectTech
// reparseia JSONL de todas sessions a cada hit (200ms+). TTL 5s mantém UX
// "live" enquanto evita o I/O.
var (
	statuslineCacheMu sync.RWMutex
	statuslineCache   = map[string]statuslineCacheEntry{}
)

type statuslineCacheEntry struct {
	resp   statuslineResponse
	expiry time.Time
}

const statuslineTTL = 5 * time.Second

// projectAggCache vive mais (1min) — tech stack/p90 mudam devagar.
var (
	projectAggCacheMu sync.RWMutex
	projectAggCache   = map[string]projectAggCacheEntry{}
)

type projectAggCacheEntry struct {
	agg    projectAgg
	expiry time.Time
}

const projectAggTTL = 60 * time.Second

// statuslineResponse é o payload de GET /api/statusline. Schema espelhado
// em internal/statusline/history.go (HistoryData).
type statuslineResponse struct {
	Session sessionLive     `json:"session"`
	Daily   dailyAgg        `json:"daily"`
	Monthly stats.MonthCost `json:"monthly"`
	Project projectAgg      `json:"project"`
}

type sessionLive struct {
	ID           string  `json:"id"`
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	BurnRateTPM  float64 `json:"burn_rate_tpm"`
	Messages     int     `json:"messages"`
	ErrorCount   int     `json:"error_count"`
	StartUnix    int64   `json:"start_unix"`
}

type dailyAgg struct {
	CostUSD       float64 `json:"cost_usd"`
	SessionsCount int     `json:"sessions_count"`
}

type projectAgg struct {
	Name        string   `json:"name"`
	Dir         string   `json:"dir"`
	P90Cost     float64  `json:"p90_cost"`
	P90Tokens   int      `json:"p90_tokens"`
	Ticket      string   `json:"ticket"`
	ClusterName string   `json:"cluster_name"`
	TechStack   []string `json:"tech_stack"`
}

var ticketBranchRE = regexp.MustCompile(`([A-Z]{2,8}-\d{1,6})`)

// handleStatusline retorna agregados (live + histórico) pra session corrente.
// Otimizado pra ser chamado a cada turno do Claude Code (target <50ms).
//
// Query params: ?session_id=X&project_dir=Y. Ambos opcionais — sem session_id,
// retorna só os agregados de daily/monthly/project; sem project_dir, project
// agg fica vazio.
func (s *Server) handleStatusline(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	projectDir := r.URL.Query().Get("project_dir")

	cacheKey := sessionID + "|" + projectDir
	statuslineCacheMu.RLock()
	if entry, ok := statuslineCache[cacheKey]; ok && time.Now().Before(entry.expiry) {
		statuslineCacheMu.RUnlock()
		writeJSON(w, 200, entry.resp)
		return
	}
	statuslineCacheMu.RUnlock()

	all, err := s.DB.ListSessions()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	resp := statuslineResponse{
		Monthly: stats.CostThisMonth(all, s.Pricing),
		Daily:   computeDaily(all, s.Pricing),
		Session: computeSessionLive(all, sessionID, s.Pricing),
		Project: cachedProject(all, projectDir, s.Pricing),
	}

	statuslineCacheMu.Lock()
	statuslineCache[cacheKey] = statuslineCacheEntry{resp: resp, expiry: time.Now().Add(statuslineTTL)}
	statuslineCacheMu.Unlock()

	writeJSON(w, 200, resp)
}

// cachedProject usa cache 60s — DetectTech parseia JSONL e é caro.
func cachedProject(all []*model.Session, projectDir string, p *pricing.Pricing) projectAgg {
	if projectDir == "" {
		return projectAgg{}
	}
	projectAggCacheMu.RLock()
	if entry, ok := projectAggCache[projectDir]; ok && time.Now().Before(entry.expiry) {
		projectAggCacheMu.RUnlock()
		return entry.agg
	}
	projectAggCacheMu.RUnlock()

	agg := computeProject(all, projectDir, p)
	projectAggCacheMu.Lock()
	projectAggCache[projectDir] = projectAggCacheEntry{agg: agg, expiry: time.Now().Add(projectAggTTL)}
	projectAggCacheMu.Unlock()
	return agg
}

// computeDaily soma cost de todas sessions cuja StartTime cai em hoje (local).
func computeDaily(all []*model.Session, p *pricing.Pricing) dailyAgg {
	today := time.Now().Local().Format("2006-01-02")
	out := dailyAgg{}
	for _, sess := range all {
		if sess.StartTime.Local().Format("2006-01-02") != today {
			continue
		}
		out.SessionsCount++
		if c, ok := p.Cost(sess); ok {
			out.CostUSD += c.USD
		}
	}
	return out
}

// computeSessionLive acha a session por ID (ou prefix) e calcula cost + burn rate.
// Burn rate = total tokens / minutes since start (capped a 1 min minimum).
func computeSessionLive(all []*model.Session, sessionID string, p *pricing.Pricing) sessionLive {
	if sessionID == "" {
		return sessionLive{}
	}
	var found *model.Session
	for _, sess := range all {
		if sess.SessionID == sessionID {
			found = sess
			break
		}
	}
	if found == nil {
		return sessionLive{ID: sessionID}
	}
	out := sessionLive{
		ID:           found.SessionID,
		Model:        found.Model,
		InputTokens:  int(found.InputTokens),
		OutputTokens: int(found.OutputTokens),
		Messages:     found.MessageCount,
		StartUnix:    found.StartTime.Unix(),
	}
	if c, ok := p.Cost(found); ok {
		out.CostUSD = c.USD
	}
	totalTokens := float64(found.InputTokens + found.OutputTokens)
	mins := time.Since(found.StartTime).Minutes()
	if mins < 1 {
		mins = 1
	}
	out.BurnRateTPM = totalTokens / mins
	return out
}

// computeProject acha todas sessions desse project_dir, calcula p90 de cost
// e tokens, extrai ticket da branch corrente, detecta tech stack.
func computeProject(all []*model.Session, projectDir string, p *pricing.Pricing) projectAgg {
	if projectDir == "" {
		return projectAgg{}
	}
	out := projectAgg{
		Dir:  projectDir,
		Name: filepath.Base(projectDir),
	}
	var costs []float64
	var tokens []int
	var projSessions []*model.Session
	var latest *model.Session
	for _, sess := range all {
		if sess.ProjectDir != projectDir {
			continue
		}
		projSessions = append(projSessions, sess)
		if c, ok := p.Cost(sess); ok {
			costs = append(costs, c.USD)
		}
		tokens = append(tokens, int(sess.InputTokens+sess.OutputTokens))
		if latest == nil || sess.StartTime.After(latest.StartTime) {
			latest = sess
		}
	}
	out.P90Cost = percentile(costs, 0.9)
	out.P90Tokens = percentileInt(tokens, 0.9)

	// ticket: branch da session mais recente.
	if latest != nil {
		if m := ticketBranchRE.FindString(latest.GitBranch); m != "" {
			out.Ticket = m
		}
	}

	// tech stack via DetectTech (regex sobre msgs).
	techs := ai.DetectTech(projSessions)
	for i, t := range techs {
		if i >= 5 {
			break
		}
		out.TechStack = append(out.TechStack, t.Name)
	}
	return out
}

// percentile devolve o p-ésimo percentil (0-1) de uma slice de floats. Vazio → 0.
func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]float64{}, xs...)
	sort.Float64s(sorted)
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func percentileInt(xs []int, p float64) int {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]int{}, xs...)
	sort.Ints(sorted)
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

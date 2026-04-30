package statusline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// HistoryData é o payload que GET /api/statusline retorna do daemon.
// Tudo opcional — quando o daemon não responde a tempo, History fica nil
// e os components renderizam só com o que veio do stdin do Claude Code.
type HistoryData struct {
	Session SessionLive `json:"session"`
	Daily   DailyAgg    `json:"daily"`
	Monthly MonthlyAgg  `json:"monthly"`
	Project ProjectAgg  `json:"project"`
}

type SessionLive struct {
	ID           string  `json:"id"`
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	BurnRateTPM  float64 `json:"burn_rate_tpm"` // tokens per minute
	Messages     int     `json:"messages"`
	ErrorCount   int     `json:"error_count"`
	StartUnix    int64   `json:"start_unix"`
}

type DailyAgg struct {
	CostUSD       float64 `json:"cost_usd"`
	SessionsCount int     `json:"sessions_count"`
}

type MonthlyAgg struct {
	Accumulated float64 `json:"accumulated"`
	Today       float64 `json:"today"`
	Projection  float64 `json:"projection"`
	DayOfMonth  int     `json:"day_of_month"`
	Days        int     `json:"days"`
}

type ProjectAgg struct {
	Name        string   `json:"name"`
	Dir         string   `json:"dir"`
	P90Cost     float64  `json:"p90_cost"`
	P90Tokens   int      `json:"p90_tokens"`
	Ticket      string   `json:"ticket"`
	ClusterName string   `json:"cluster_name"`
	TechStack   []string `json:"tech_stack"`
}

// FetchHistory faz GET no daemon com timeout curto. Devolve nil em qualquer
// erro (graceful fallback — statusline ainda renderiza com dados do stdin).
func FetchHistory(endpoint, sessionID, projectDir string, timeout time.Duration) *HistoryData {
	if endpoint == "" || sessionID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	q := url.Values{}
	q.Set("session_id", sessionID)
	if projectDir != "" {
		q.Set("project_dir", projectDir)
	}
	full := endpoint + "/api/statusline?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var d HistoryData
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil
	}
	return &d
}

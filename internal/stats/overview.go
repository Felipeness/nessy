package stats

import (
	"sort"
	"time"

	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/pricing"
)

// OverviewMetrics agrega métricas pra "dashboard" estilo /status do Claude Code.
// Representa a saúde / hábito de uso ao longo do tempo.
type OverviewMetrics struct {
	TotalSessions  int
	TotalMessages  int
	TotalTokens    int64
	TotalCostUSD   float64
	FavoriteModel  string         // modelo com mais sessions
	ModelCounts    map[string]int // model → session count
	LongestSession time.Duration
	LongestSID     string

	// Activity/streak
	ActiveDays      int       // dias únicos com >= 1 session no range
	TotalDays       int       // dias do range observado
	CurrentStreak   int       // dias consecutivos terminando hoje
	LongestStreak   int       // maior sequência consecutiva
	MostActiveDay   time.Time // dia com mais sessions
	MostActiveCount int       // sessions naquele dia
	FirstSession    time.Time
	LastSession     time.Time
}

// BuildOverview computa todas as métricas em uma passada.
// Retorna OverviewMetrics zerado se não houver sessions.
func BuildOverview(sessions []*model.Session, p *pricing.Pricing) OverviewMetrics {
	if len(sessions) == 0 {
		return OverviewMetrics{ModelCounts: map[string]int{}}
	}
	ov := OverviewMetrics{
		TotalSessions: len(sessions),
		ModelCounts:   map[string]int{},
	}

	// Conta sessions por dia (YYYY-MM-DD local) pra streak/active days
	dayCount := map[string]int{}
	dayKey := func(t time.Time) string {
		return t.Local().Format("2006-01-02")
	}

	for _, s := range sessions {
		ov.TotalMessages += s.MessageCount
		ov.TotalTokens += s.TotalTokens()
		if p != nil {
			if c, ok := p.Cost(s); ok {
				ov.TotalCostUSD += c.USD
			}
		}
		if s.Model != "" && s.Model != "<synthetic>" {
			ov.ModelCounts[s.Model]++
		}
		if d := s.Duration(); d > ov.LongestSession {
			ov.LongestSession = d
			ov.LongestSID = s.SessionID
		}
		if ov.FirstSession.IsZero() || s.StartTime.Before(ov.FirstSession) {
			ov.FirstSession = s.StartTime
		}
		if s.StartTime.After(ov.LastSession) {
			ov.LastSession = s.StartTime
		}
		dayCount[dayKey(s.StartTime)]++
	}

	// Favorite model (maior count, tiebreak por nome estável)
	type kv struct {
		k string
		v int
	}
	var pairs []kv
	for k, v := range ov.ModelCounts {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	if len(pairs) > 0 {
		ov.FavoriteModel = pairs[0].k
	}

	// Most active day
	for k, v := range dayCount {
		if v > ov.MostActiveCount {
			ov.MostActiveCount = v
			ov.MostActiveDay, _ = time.ParseInLocation("2006-01-02", k, time.Local)
		}
	}
	ov.ActiveDays = len(dayCount)
	if !ov.FirstSession.IsZero() {
		ov.TotalDays = int(ov.LastSession.Sub(ov.FirstSession)/(24*time.Hour)) + 1
		if ov.TotalDays < ov.ActiveDays {
			ov.TotalDays = ov.ActiveDays
		}
	}

	// Streaks: itera dia-a-dia da primeira até hoje
	ov.LongestStreak, ov.CurrentStreak = computeStreaks(dayCount, ov.FirstSession)
	return ov
}

// computeStreaks devolve longest streak e current streak (terminando hoje, 0 se hoje sem session).
func computeStreaks(dayCount map[string]int, first time.Time) (longest, current int) {
	if first.IsZero() {
		return 0, 0
	}
	cur := 0
	day := first.Local().Truncate(24 * time.Hour)
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	today := time.Now().Local()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local)

	for !day.After(today) {
		if _, ok := dayCount[day.Format("2006-01-02")]; ok {
			cur++
			if cur > longest {
				longest = cur
			}
		} else {
			cur = 0
		}
		day = day.AddDate(0, 0, 1)
	}
	// current streak = sequência terminando em hoje (ou ontem se hoje sem session)
	cs := 0
	probe := today
	for {
		if _, ok := dayCount[probe.Format("2006-01-02")]; ok {
			cs++
			probe = probe.AddDate(0, 0, -1)
		} else {
			break
		}
	}
	if cs == 0 {
		// se hoje vazio, tenta terminar em ontem (current streak ainda "vivo")
		probe = today.AddDate(0, 0, -1)
		for {
			if _, ok := dayCount[probe.Format("2006-01-02")]; ok {
				cs++
				probe = probe.AddDate(0, 0, -1)
			} else {
				break
			}
		}
	}
	current = cs
	return
}

// CalendarHeatmap monta um grid GitHub-style: 7 rows (Mon..Sun) × N colunas (semanas).
// Retorna (grid, firstMonday, weeks). Cada célula = nº sessions do dia. weeks = nº de
// colunas (≈ months × 4.3, arredondado pra cima).
func CalendarHeatmap(sessions []*model.Session, months int) (grid [][]int, firstMonday time.Time, weeks int) {
	if months <= 0 {
		months = 12
	}
	now := time.Now().Local()
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	start := end.AddDate(0, -months, 0)
	// Recua até a segunda-feira pra alinhar a primeira coluna
	for start.Weekday() != time.Monday {
		start = start.AddDate(0, 0, -1)
	}
	firstMonday = start

	totalDays := int(end.Sub(start)/(24*time.Hour)) + 1
	weeks = (totalDays + 6) / 7
	grid = make([][]int, 7)
	for i := range grid {
		grid[i] = make([]int, weeks)
	}

	dayCount := map[string]int{}
	for _, s := range sessions {
		k := s.StartTime.Local().Format("2006-01-02")
		dayCount[k]++
	}

	for d := 0; d < totalDays; d++ {
		day := start.AddDate(0, 0, d)
		row := int(day.Weekday()+6) % 7 // Mon=0..Sun=6
		col := d / 7
		if col >= weeks {
			break
		}
		grid[row][col] = dayCount[day.Format("2006-01-02")]
	}
	return
}

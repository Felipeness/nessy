// Package stats agrega métricas determinísticas sobre uma coleção de sessions.
package stats

import (
	"sort"
	"time"

	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
)

// Baseline retorna mediana de msgs/cost/duration nas últimas 30 sessions do projeto.
type BaselineSummary struct {
	Available  bool          // true se ≥3 sessions disponíveis
	Count      int
	MsgsMedian int
	CostMedian float64
	DurMedian  time.Duration
}

// Baseline calcula mediana das últimas 30 sessions do mesmo projeto.
func Baseline(sessions []*model.Session, projectDir string, p *pricing.Pricing) BaselineSummary {
	var same []*model.Session
	for _, s := range sessions {
		if s.ProjectDir == projectDir {
			same = append(same, s)
		}
	}
	if len(same) < 3 {
		return BaselineSummary{}
	}
	sort.Slice(same, func(i, j int) bool { return same[i].EndTime.After(same[j].EndTime) })
	if len(same) > 30 {
		same = same[:30]
	}

	msgs := make([]int, len(same))
	costs := make([]float64, 0, len(same))
	durs := make([]time.Duration, len(same))
	for i, s := range same {
		msgs[i] = s.MessageCount
		durs[i] = s.Duration()
		if p != nil {
			if c, ok := p.Cost(s); ok {
				costs = append(costs, c.USD)
			}
		}
	}
	sort.Ints(msgs)
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	sort.Float64s(costs)
	costMed := 0.0
	if len(costs) > 0 {
		costMed = costs[len(costs)/2]
	}
	return BaselineSummary{
		Available:  true,
		Count:      len(same),
		MsgsMedian: msgs[len(msgs)/2],
		CostMedian: costMed,
		DurMedian:  durs[len(durs)/2],
	}
}

// ProjectHistory retorna count de sessions por dia (últimos N dias) no projeto.
func ProjectHistory(sessions []*model.Session, projectDir string, days int) []int {
	now := time.Now()
	bins := make([]int, days)
	for _, s := range sessions {
		if s.ProjectDir != projectDir {
			continue
		}
		d := int(now.Sub(s.StartTime).Hours() / 24)
		if d >= 0 && d < days {
			bins[days-1-d]++
		}
	}
	return bins
}

// HeatmapGrid produz [6][7]int — bins de 4h × dia da semana — pras últimas N semanas.
func HeatmapGrid(sessions []*model.Session, weeks int) [][]int {
	cutoff := time.Now().Add(-time.Duration(weeks) * 7 * 24 * time.Hour)
	grid := make([][]int, 6)
	for i := range grid {
		grid[i] = make([]int, 7)
	}
	for _, s := range sessions {
		if s.StartTime.Before(cutoff) {
			continue
		}
		hour := s.StartTime.Hour()
		row := hour / 4
		if row > 5 {
			row = 5
		}
		col := int(s.StartTime.Weekday()) // Sun=0..Sat=6
		// Reorder pra Mon=0..Sun=6
		col = (col + 6) % 7
		grid[row][col]++
	}
	return grid
}

// CostThisMonth calcula custo acumulado e projeção pro fim do mês.
type MonthCost struct {
	Accumulated float64
	Today       float64
	Projection  float64
	Days        int
	DayOfMonth  int
}

func CostThisMonth(sessions []*model.Session, p *pricing.Pricing) MonthCost {
	if p == nil {
		return MonthCost{}
	}
	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	var acc, today float64
	for _, s := range sessions {
		if s.StartTime.Before(startOfMonth) {
			continue
		}
		c, ok := p.Cost(s)
		if !ok {
			continue
		}
		acc += c.USD
		if s.StartTime.After(startOfDay) || s.StartTime.Equal(startOfDay) {
			today += c.USD
		}
	}
	day := now.Day()
	avgPerDay := acc / float64(day)
	totalDays := daysInMonth(now)
	return MonthCost{
		Accumulated: acc,
		Today:       today,
		Projection:  avgPerDay * float64(totalDays),
		Days:        totalDays,
		DayOfMonth:  day,
	}
}

func daysInMonth(t time.Time) int {
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	next := first.AddDate(0, 1, 0)
	return next.AddDate(0, 0, -1).Day()
}

// WeekDelta compara semana atual vs anterior.
type WeekDelta struct {
	ThisWeek struct {
		Sessions int
		Msgs     int
		CostUSD  float64
	}
	LastWeek struct {
		Sessions int
		Msgs     int
		CostUSD  float64
	}
}

func WeekDeltaFor(sessions []*model.Session, p *pricing.Pricing) WeekDelta {
	now := time.Now()
	startWeek := now.AddDate(0, 0, -int(now.Weekday()))
	startWeek = time.Date(startWeek.Year(), startWeek.Month(), startWeek.Day(), 0, 0, 0, 0, startWeek.Location())
	startLast := startWeek.AddDate(0, 0, -7)

	var d WeekDelta
	for _, s := range sessions {
		if s.StartTime.Before(startLast) {
			continue
		}
		var bucket *struct {
			Sessions int
			Msgs     int
			CostUSD  float64
		}
		switch {
		case s.StartTime.Before(startWeek):
			bucket = &d.LastWeek
		default:
			bucket = &d.ThisWeek
		}
		bucket.Sessions++
		bucket.Msgs += s.MessageCount
		if p != nil {
			if c, ok := p.Cost(s); ok {
				bucket.CostUSD += c.USD
			}
		}
	}
	return d
}

// Pct calcula delta percentual.
func Pct(now, prev float64) float64 {
	if prev == 0 {
		return 0
	}
	return (now - prev) / prev * 100
}

// CacheSavings estima quanto foi economizado em cache hits (vs preço de input).
func CacheSavings(sessions []*model.Session, p *pricing.Pricing, days int) float64 {
	if p == nil {
		return 0
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	saved := 0.0
	for _, s := range sessions {
		if s.StartTime.Before(cutoff) {
			continue
		}
		m, ok := p.Models[s.Model]
		if !ok {
			continue
		}
		// Saving = cache_read tokens × (input_per_mtok - cache_read_per_mtok)
		saved += float64(s.CacheReadTokens) * (m.InputPerMTok - m.CacheReadPerMTok) / 1_000_000.0
	}
	return saved
}

// LongTail retorna top N sessions por critério (cost ou duration).
func LongTailByCost(sessions []*model.Session, p *pricing.Pricing, n int) []*model.Session {
	if p == nil {
		return nil
	}
	sort.Slice(sessions, func(i, j int) bool {
		ci, _ := p.Cost(sessions[i])
		cj, _ := p.Cost(sessions[j])
		if ci.USD != cj.USD {
			return ci.USD > cj.USD
		}
		return sessions[i].SessionID < sessions[j].SessionID
	})
	if len(sessions) > n {
		return sessions[:n]
	}
	return sessions
}

func LongTailByDuration(sessions []*model.Session, n int) []*model.Session {
	out := make([]*model.Session, len(sessions))
	copy(out, sessions)
	sort.Slice(out, func(i, j int) bool {
		di, dj := out[i].Duration(), out[j].Duration()
		if di != dj {
			return di > dj
		}
		return out[i].SessionID < out[j].SessionID
	})
	if len(out) > n {
		return out[:n]
	}
	return out
}

// ModelDistribution retorna count de msgs por modelo.
func ModelDistribution(sessions []*model.Session) map[string]int {
	m := map[string]int{}
	for _, s := range sessions {
		key := s.Model
		if key == "" {
			key = "unknown"
		}
		m[key] += s.MessageCount
	}
	return m
}

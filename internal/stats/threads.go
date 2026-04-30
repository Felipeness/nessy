package stats

import (
	"sort"
	"strings"
	"time"

	"github.com/felipeness/claude-history/internal/model"
)

// Thread é um grupo de sessions consecutivas no mesmo project+branch dentro
// do gap threshold. "Continuação" = mesmo dev voltando ao trabalho recente.
// "Compact" = continuação muito rápida (gap < 1min), provavelmente fork
// automático do Claude Code via /compact.
type Thread struct {
	ProjectDir string
	Branch     string
	Sessions   []*ThreadSession
	StartTime  time.Time
	EndTime    time.Time
	TotalCost  float64
	TotalDur   time.Duration
}

// ThreadSession decora model.Session com info de continuidade.
type ThreadSession struct {
	*model.Session
	// GapFromPrev — tempo desde a session anterior na mesma thread. 0 se primeira.
	GapFromPrev time.Duration
	// Kind classifica o tipo de transição:
	//   "first"   — primeira session da thread
	//   "compact" — gap < 1min (provável /compact ou crash+resume rápido)
	//   "resumed" — gap < gapThreshold (continuação manual)
	Kind string
}

// BuildThreads agrupa sessions em threads usando heurística:
//   mesmo project_dir + mesma branch + gap_from_prev_end <= gapThreshold
//
// Sessions sem branch ainda são agrupadas (branch="" como key).
// gapThreshold default razoável: 30 minutos.
func BuildThreads(sessions []*model.Session, gapThreshold time.Duration) []*Thread {
	if gapThreshold <= 0 {
		gapThreshold = 30 * time.Minute
	}
	const compactThreshold = 1 * time.Minute

	// Group por project+branch
	groups := map[string][]*model.Session{}
	for _, s := range sessions {
		if s == nil {
			continue
		}
		key := s.ProjectDir + "|" + s.GitBranch
		groups[key] = append(groups[key], s)
	}

	var threads []*Thread
	for key, list := range groups {
		// Sort por start_time
		sort.Slice(list, func(i, j int) bool {
			return list[i].StartTime.Before(list[j].StartTime)
		})

		idx := strings.Index(key, "|")
		projectDir, branch := key[:idx], key[idx+1:]

		var current *Thread
		for _, s := range list {
			if current == nil {
				current = &Thread{
					ProjectDir: projectDir,
					Branch:     branch,
					StartTime:  s.StartTime,
				}
				current.Sessions = append(current.Sessions, &ThreadSession{
					Session: s,
					Kind:    "first",
				})
				continue
			}
			lastSess := current.Sessions[len(current.Sessions)-1]
			gap := s.StartTime.Sub(lastSess.EndTime)
			if gap < 0 {
				gap = 0
			}

			if gap > gapThreshold {
				// Fecha thread atual, abre nova
				current.EndTime = lastSess.EndTime
				threads = append(threads, current)
				current = &Thread{
					ProjectDir: projectDir,
					Branch:     branch,
					StartTime:  s.StartTime,
				}
				current.Sessions = append(current.Sessions, &ThreadSession{
					Session: s,
					Kind:    "first",
				})
				continue
			}

			kind := "resumed"
			if gap < compactThreshold {
				kind = "compact"
			}
			current.Sessions = append(current.Sessions, &ThreadSession{
				Session:     s,
				GapFromPrev: gap,
				Kind:        kind,
			})
		}
		if current != nil {
			current.EndTime = current.Sessions[len(current.Sessions)-1].EndTime
			threads = append(threads, current)
		}
	}

	// Sort threads por start time desc (mais recentes primeiro)
	sort.Slice(threads, func(i, j int) bool {
		return threads[i].StartTime.After(threads[j].StartTime)
	})
	return threads
}

// CalcTotals enche TotalCost e TotalDur de cada thread baseado em pricing
// passado externalmente (não usar ai.* aqui pra evitar import cycle).
type costFn func(*model.Session) (float64, bool)

// CalcTotals popula TotalCost (USD) e TotalDur somando sessions da thread.
func (t *Thread) CalcTotals(cost costFn) {
	t.TotalCost = 0
	t.TotalDur = 0
	for _, s := range t.Sessions {
		t.TotalDur += s.Duration()
		if cost != nil {
			if c, ok := cost(s.Session); ok {
				t.TotalCost += c
			}
		}
	}
}

// GroupByProject agrupa threads por project_dir, mantendo ordem original
// dentro de cada grupo.
func GroupByProject(threads []*Thread) map[string][]*Thread {
	out := map[string][]*Thread{}
	for _, t := range threads {
		out[t.ProjectDir] = append(out[t.ProjectDir], t)
	}
	return out
}

// SortedProjectDirs devolve keys de GroupByProject ordenadas por número total
// de sessions desc (projeto mais ativo primeiro).
func SortedProjectDirs(grouped map[string][]*Thread) []string {
	dirs := make([]string, 0, len(grouped))
	for d := range grouped {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		ci, cj := 0, 0
		for _, t := range grouped[dirs[i]] {
			ci += len(t.Sessions)
		}
		for _, t := range grouped[dirs[j]] {
			cj += len(t.Sessions)
		}
		if ci != cj {
			return ci > cj
		}
		return dirs[i] < dirs[j]
	})
	return dirs
}

// SparklineFromThread devolve um sparkline ▁▂▃...█ representando a duração
// relativa de cada session dentro da thread. Útil pra mostrar "shape" da
// thread numa linha só.
func SparklineFromThread(t *Thread) string {
	if len(t.Sessions) == 0 {
		return ""
	}
	var maxDur time.Duration
	for _, s := range t.Sessions {
		if d := s.Duration(); d > maxDur {
			maxDur = d
		}
	}
	if maxDur == 0 {
		return strings.Repeat("▁", len(t.Sessions))
	}
	chars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	var b strings.Builder
	for _, s := range t.Sessions {
		ratio := float64(s.Duration()) / float64(maxDur)
		idx := int(ratio * float64(len(chars)-1))
		if idx < 0 {
			idx = 0
		} else if idx >= len(chars) {
			idx = len(chars) - 1
		}
		b.WriteRune(chars[idx])
	}
	return b.String()
}

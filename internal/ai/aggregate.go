package ai

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/felipeness/claude-history/internal/index"
)

// KnowledgeAggregate é a visão cross-session — agrega tudo em session_knowledge
// e expõe 5 visões úteis pro "segundo cérebro": top patterns, timeline de
// decisões, problemas recorrentes, tech consolidada, open questions com idade.
type KnowledgeAggregate struct {
	SessionsAnalyzed  int                   `json:"sessions_analyzed"`
	TopPatterns       []PatternFrequency    `json:"top_patterns"`
	DecisionHistory   []DecisionEntry       `json:"decision_history"`
	RecurringProblems []ProblemCluster      `json:"recurring_problems"`
	TechFrequency     []TechFrequency       `json:"tech_frequency"`
	OpenQuestions     []OpenQuestionEntry   `json:"open_questions"`
}

// PatternFrequency é um code_pattern que aparece em N sessions.
type PatternFrequency struct {
	Pattern  string   `json:"pattern"`
	Count    int      `json:"count"`
	Sessions []string `json:"sessions"` // top 5 session_ids
}

// DecisionEntry é uma decisão tomada com link pra origem.
type DecisionEntry struct {
	Decision    string `json:"decision"`
	Rationale   string `json:"rationale"`
	SessionID   string `json:"session_id"`
	GeneratedAt int64  `json:"generated_at"`
}

// ProblemCluster agrupa problems com keywords similares — "você teve 4
// sessions com problema parecido com X".
type ProblemCluster struct {
	Representative string   `json:"representative"` // problem text mais longo do cluster
	Sessions       []string `json:"sessions"`
	Count          int      `json:"count"`
	Keywords       []string `json:"keywords"` // top palavras compartilhadas
}

// TechFrequency consolida tech_used cross-session.
type TechFrequency struct {
	Name     string   `json:"name"`
	Count    int      `json:"count"`
	Sessions []string `json:"sessions"` // top 5
}

// OpenQuestionEntry é uma pergunta/coisa em aberto, com idade em dias.
type OpenQuestionEntry struct {
	Question    string `json:"question"`
	SessionID   string `json:"session_id"`
	GeneratedAt int64  `json:"generated_at"`
	AgeDays     int    `json:"age_days"`
}

// AggregateKnowledge percorre todas entradas de session_knowledge e produz
// as 5 visões. Computado on-demand (sem cache) — com 1000 sessions ainda
// é instantâneo. Pode adicionar cache 60s depois se virar gargalo.
func AggregateKnowledge(db *index.DB) (*KnowledgeAggregate, error) {
	list, err := db.KnowledgeList()
	if err != nil {
		return nil, err
	}
	out := &KnowledgeAggregate{SessionsAnalyzed: len(list)}
	if len(list) == 0 {
		return out, nil
	}

	out.TopPatterns = aggregatePatterns(list)
	out.DecisionHistory = aggregateDecisions(list)
	out.RecurringProblems = aggregateProblems(list)
	out.TechFrequency = aggregateTech(list)
	out.OpenQuestions = aggregateOpenQuestions(list)
	return out, nil
}

// aggregatePatterns conta code_patterns por frequência. Normaliza pra
// lowercase trimmed pra deduplicar variações de capitalização.
func aggregatePatterns(list []*index.Knowledge) []PatternFrequency {
	type entry struct {
		display  string
		sessions []string
	}
	freq := map[string]*entry{}
	for _, k := range list {
		var patterns []string
		if err := json.Unmarshal([]byte(k.CodePatterns), &patterns); err != nil {
			continue
		}
		for _, p := range patterns {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			norm := strings.ToLower(p)
			if e, ok := freq[norm]; ok {
				e.sessions = append(e.sessions, k.SessionID)
			} else {
				freq[norm] = &entry{display: p, sessions: []string{k.SessionID}}
			}
		}
	}
	out := make([]PatternFrequency, 0, len(freq))
	for _, e := range freq {
		out = append(out, PatternFrequency{
			Pattern:  e.display,
			Count:    len(e.sessions),
			Sessions: limitSlice(e.sessions, 5),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Pattern < out[j].Pattern
	})
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// aggregateDecisions devolve TODAS decisões cross-session ordenadas por
// data desc (mais recentes primeiro). Cap em 50.
func aggregateDecisions(list []*index.Knowledge) []DecisionEntry {
	type rawDec struct {
		Decision  string `json:"decision"`
		Rationale string `json:"rationale"`
	}
	var out []DecisionEntry
	for _, k := range list {
		var ds []rawDec
		if err := json.Unmarshal([]byte(k.Decisions), &ds); err != nil {
			continue
		}
		for _, d := range ds {
			if d.Decision == "" {
				continue
			}
			out = append(out, DecisionEntry{
				Decision:    d.Decision,
				Rationale:   d.Rationale,
				SessionID:   k.SessionID,
				GeneratedAt: k.GeneratedAt,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].GeneratedAt > out[j].GeneratedAt
	})
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}

// aggregateProblems clustera problems por keywords compartilhadas. Heurística
// simples Phase B v1: extrai palavras-chave (>3 chars, não-stop), agrupa por
// jaccard ≥ 0.4 entre conjuntos. Pra v2 podemos usar embeddings.
func aggregateProblems(list []*index.Knowledge) []ProblemCluster {
	type prob struct {
		text     string
		keywords map[string]bool
		session  string
		ts       int64
	}
	var probs []prob
	for _, k := range list {
		if k.Problem == "" {
			continue
		}
		probs = append(probs, prob{
			text:     k.Problem,
			keywords: extractKeywords(k.Problem),
			session:  k.SessionID,
			ts:       k.GeneratedAt,
		})
	}

	// Cluster: cada problem entra no primeiro cluster com jaccard ≥ 0.4
	// senão cria novo cluster.
	type cluster struct {
		problems []prob
		shared   map[string]int // count de cada keyword no cluster
	}
	var clusters []*cluster
	for _, p := range probs {
		assigned := false
		for _, c := range clusters {
			rep := c.problems[0].keywords
			if jaccard(p.keywords, rep) >= 0.4 {
				c.problems = append(c.problems, p)
				for k := range p.keywords {
					c.shared[k]++
				}
				assigned = true
				break
			}
		}
		if !assigned {
			cl := &cluster{
				problems: []prob{p},
				shared:   map[string]int{},
			}
			for k := range p.keywords {
				cl.shared[k]++
			}
			clusters = append(clusters, cl)
		}
	}

	out := make([]ProblemCluster, 0, len(clusters))
	for _, c := range clusters {
		// Só interessa se tem 2+ sessions (recorrente)
		if len(c.problems) < 2 {
			continue
		}
		// Representante: o problem mais longo (mais detalhado)
		rep := c.problems[0]
		for _, p := range c.problems {
			if len(p.text) > len(rep.text) {
				rep = p
			}
		}
		// Top keywords compartilhadas
		type kv struct {
			k string
			n int
		}
		var pairs []kv
		for k, n := range c.shared {
			if n >= 2 {
				pairs = append(pairs, kv{k, n})
			}
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].n > pairs[j].n })
		var keywords []string
		for i, p := range pairs {
			if i >= 5 {
				break
			}
			keywords = append(keywords, p.k)
		}
		var sessions []string
		for _, p := range c.problems {
			sessions = append(sessions, p.session)
		}
		out = append(out, ProblemCluster{
			Representative: rep.text,
			Sessions:       limitSlice(sessions, 10),
			Count:          len(c.problems),
			Keywords:       keywords,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

// aggregateTech consolida tech_used cross-session. Diferente de DetectTech
// (que escaneia mensagens), aqui usa o que o LLM extraiu — mais semântico.
func aggregateTech(list []*index.Knowledge) []TechFrequency {
	type entry struct {
		display  string
		sessions []string
	}
	freq := map[string]*entry{}
	for _, k := range list {
		var techs []string
		if err := json.Unmarshal([]byte(k.TechUsed), &techs); err != nil {
			continue
		}
		for _, t := range techs {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			norm := strings.ToLower(t)
			if e, ok := freq[norm]; ok {
				e.sessions = append(e.sessions, k.SessionID)
			} else {
				freq[norm] = &entry{display: t, sessions: []string{k.SessionID}}
			}
		}
	}
	out := make([]TechFrequency, 0, len(freq))
	for _, e := range freq {
		out = append(out, TechFrequency{
			Name:     e.display,
			Count:    len(e.sessions),
			Sessions: limitSlice(e.sessions, 5),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// aggregateOpenQuestions lista todas open_questions com idade em dias.
// Mais antigas primeiro — pressão visual pra fechar pendências esquecidas.
func aggregateOpenQuestions(list []*index.Knowledge) []OpenQuestionEntry {
	now := time.Now().Unix()
	var out []OpenQuestionEntry
	for _, k := range list {
		var qs []string
		if err := json.Unmarshal([]byte(k.OpenQuestions), &qs); err != nil {
			continue
		}
		for _, q := range qs {
			q = strings.TrimSpace(q)
			if q == "" {
				continue
			}
			age := int((now - k.GeneratedAt) / 86400)
			if age < 0 {
				age = 0
			}
			out = append(out, OpenQuestionEntry{
				Question:    q,
				SessionID:   k.SessionID,
				GeneratedAt: k.GeneratedAt,
				AgeDays:     age,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AgeDays > out[j].AgeDays
	})
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}

// extractKeywords pega palavras com >3 chars (sem stopwords pt/en) pro
// cluster de recurring problems. Heurística simples — sem stemming.
func extractKeywords(s string) map[string]bool {
	out := map[string]bool{}
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_')
	})
	for _, w := range words {
		if len(w) < 4 {
			continue
		}
		if problemStopWords[w] {
			continue
		}
		out[w] = true
	}
	return out
}

// problemStopWords — palavras comuns que não distinguem problems.
var problemStopWords = map[string]bool{
	"para": true, "como": true, "fazer": true, "quero": true, "esta": true,
	"tudo": true, "isso": true, "esse": true, "essa": true, "agora": true,
	"depois": true, "antes": true, "apenas": true, "seguir": true,
	"with": true, "from": true, "that": true, "this": true, "have": true,
	"need": true, "want": true, "make": true, "into": true, "user": true,
	"using": true, "trying": true, "wants": true, "tried": true,
	"então": true, "tambem": true, "também": true,
}

// jaccard mede similaridade entre dois conjuntos de keywords. 1 = idêntico,
// 0 = disjuntos.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersect := 0
	for k := range a {
		if b[k] {
			intersect++
		}
	}
	union := len(a) + len(b) - intersect
	if union == 0 {
		return 0
	}
	return float64(intersect) / float64(union)
}

// limitSlice corta slice em N.
func limitSlice(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return xs[:n]
}

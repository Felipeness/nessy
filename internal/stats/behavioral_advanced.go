package stats

import (
	"math"
	"sort"
	"time"

	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/parser"
	"github.com/felipeness/nessy/internal/pricing"
)

// --- N-grams ---

type Bigram struct {
	A, B  string
	Count int
}

type Trigram struct {
	A, B, C string
	Count   int
}

// TopBigrams retorna os bigrams mais frequentes em user msgs.
func TopBigrams(sessions []*model.Session, n int) []Bigram {
	freq := map[[2]string]int{}
	walkUserTokens(sessions, func(toks []string) {
		for i := 0; i < len(toks)-1; i++ {
			freq[[2]string{toks[i], toks[i+1]}]++
		}
	})
	out := make([]Bigram, 0, len(freq))
	for k, c := range freq {
		out = append(out, Bigram{A: k[0], B: k[1], Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].A != out[j].A {
			return out[i].A < out[j].A
		}
		return out[i].B < out[j].B
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// TopTrigrams retorna os trigrams mais frequentes em user msgs.
func TopTrigrams(sessions []*model.Session, n int) []Trigram {
	freq := map[[3]string]int{}
	walkUserTokens(sessions, func(toks []string) {
		for i := 0; i < len(toks)-2; i++ {
			freq[[3]string{toks[i], toks[i+1], toks[i+2]}]++
		}
	})
	out := make([]Trigram, 0, len(freq))
	for k, c := range freq {
		out = append(out, Trigram{A: k[0], B: k[1], C: k[2], Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].A != out[j].A {
			return out[i].A < out[j].A
		}
		if out[i].B != out[j].B {
			return out[i].B < out[j].B
		}
		return out[i].C < out[j].C
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// --- Co-occurrence + PMI ---

type CoOccur struct {
	A, B  string
	Count int
	PMI   float64
}

// CoOccurrences calcula pares de palavras que co-ocorrem em mesma user msg
// (não adjacentes — bigrams cobrem isso). Score = pointwise mutual information.
func CoOccurrences(sessions []*model.Session, minCount, n int) []CoOccur {
	pairFreq := map[[2]string]int{}
	wordFreq := map[string]int{}
	totalDocs := 0

	walkUserTokens(sessions, func(toks []string) {
		totalDocs++
		// unique tokens nessa msg pra contar word freq por documento
		seen := map[string]bool{}
		for _, t := range toks {
			seen[t] = true
		}
		uniq := make([]string, 0, len(seen))
		for t := range seen {
			uniq = append(uniq, t)
			wordFreq[t]++
		}
		sort.Strings(uniq)
		// gera pares ordenados (a < b) — evita duplicar
		for i := 0; i < len(uniq); i++ {
			for j := i + 1; j < len(uniq); j++ {
				pairFreq[[2]string{uniq[i], uniq[j]}]++
			}
		}
	})

	if totalDocs == 0 {
		return nil
	}
	totalF := float64(totalDocs)

	out := make([]CoOccur, 0)
	for k, c := range pairFreq {
		if c < minCount {
			continue
		}
		pa := float64(wordFreq[k[0]]) / totalF
		pb := float64(wordFreq[k[1]]) / totalF
		pab := float64(c) / totalF
		if pa <= 0 || pb <= 0 || pab <= 0 {
			continue
		}
		pmi := math.Log2(pab / (pa * pb))
		out = append(out, CoOccur{A: k[0], B: k[1], Count: c, PMI: pmi})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PMI != out[j].PMI {
			return out[i].PMI > out[j].PMI
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].A != out[j].A {
			return out[i].A < out[j].A
		}
		return out[i].B < out[j].B
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// --- Conversation flow ---

type FlowHist struct {
	Bucket string
	Count  int
}

type FlowSummary struct {
	Hist []FlowHist
	P50  int
	P90  int
	P99  int
}

func FlowDistribution(sessions []*model.Session) FlowSummary {
	if len(sessions) == 0 {
		return FlowSummary{}
	}
	buckets := []struct {
		label string
		max   int
	}{
		{"<10", 10},
		{"10-30", 30},
		{"30-100", 100},
		{"100-300", 300},
		{"300-1000", 1000},
		{">1000", 1<<31 - 1},
	}
	hist := make([]FlowHist, len(buckets))
	for i, b := range buckets {
		hist[i] = FlowHist{Bucket: b.label}
	}
	counts := make([]int, len(sessions))
	for i, s := range sessions {
		counts[i] = s.MessageCount
		for j, b := range buckets {
			if s.MessageCount < b.max {
				hist[j].Count++
				break
			}
		}
	}
	sort.Ints(counts)
	pIdx := func(p float64) int {
		idx := int(float64(len(counts)-1) * p)
		if idx < 0 {
			idx = 0
		}
		if idx >= len(counts) {
			idx = len(counts) - 1
		}
		return counts[idx]
	}
	return FlowSummary{Hist: hist, P50: pIdx(0.50), P90: pIdx(0.90), P99: pIdx(0.99)}
}

// --- Style comparison ---

type StyleStats struct {
	AvgWordsUser           float64
	AvgWordsAssistant      float64
	UniqueWordsUser        int
	UniqueWordsAssistant   int
	TopWordsUser           []WordCount
	TopWordsAssistant      []WordCount
	AvgSentencesUser       float64
	AvgSentencesAssistant  float64
}

func StyleComparison(sessions []*model.Session) StyleStats {
	uTotalWords, uMsgs := 0, 0
	aTotalWords, aMsgs := 0, 0
	uTotalSent, aTotalSent := 0, 0
	uVocab := map[string]int{}
	aVocab := map[string]int{}

	for _, s := range sessions {
		if s.JSONLPath == "" {
			continue
		}
		msgs, err := parser.ParseMessages(s.JSONLPath)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			toks := Tokenize(m.Content)
			sentences := countSentences(m.Content)
			if m.Role == "user" {
				uMsgs++
				uTotalWords += len(toks)
				uTotalSent += sentences
				for _, t := range toks {
					uVocab[t]++
				}
			} else if m.Role == "assistant" {
				aMsgs++
				aTotalWords += len(toks)
				aTotalSent += sentences
				for _, t := range toks {
					aVocab[t]++
				}
			}
		}
	}
	avgU := 0.0
	avgA := 0.0
	avgUS := 0.0
	avgAS := 0.0
	if uMsgs > 0 {
		avgU = float64(uTotalWords) / float64(uMsgs)
		avgUS = float64(uTotalSent) / float64(uMsgs)
	}
	if aMsgs > 0 {
		avgA = float64(aTotalWords) / float64(aMsgs)
		avgAS = float64(aTotalSent) / float64(aMsgs)
	}
	return StyleStats{
		AvgWordsUser:          avgU,
		AvgWordsAssistant:     avgA,
		UniqueWordsUser:       len(uVocab),
		UniqueWordsAssistant:  len(aVocab),
		TopWordsUser:          topFromMap(uVocab, 10),
		TopWordsAssistant:     topFromMap(aVocab, 10),
		AvgSentencesUser:      avgUS,
		AvgSentencesAssistant: avgAS,
	}
}

func countSentences(text string) int {
	count := 0
	for _, r := range text {
		if r == '.' || r == '!' || r == '?' {
			count++
		}
	}
	if count == 0 && len(text) > 0 {
		return 1
	}
	return count
}

func topFromMap(m map[string]int, n int) []WordCount {
	out := make([]WordCount, 0, len(m))
	for k, v := range m {
		out = append(out, WordCount{Word: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Word < out[j].Word
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// --- High-error sessions ---

type ErrorSession struct {
	Session   *model.Session `json:"session"`
	ErrorRate float64        `json:"error_rate"`
	Hits      int            `json:"hits"`
	Total     int            `json:"total"`
}

func HighErrorSessions(sessions []*model.Session, threshold float64) []ErrorSession {
	out := make([]ErrorSession, 0)
	for _, s := range sessions {
		rate, hits, total := perSessionErrorRate(s)
		if total == 0 || rate < threshold {
			continue
		}
		out = append(out, ErrorSession{Session: s, ErrorRate: rate, Hits: hits, Total: total})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ErrorRate != out[j].ErrorRate {
			return out[i].ErrorRate > out[j].ErrorRate
		}
		return out[i].Session.SessionID < out[j].Session.SessionID
	})
	return out
}

func perSessionErrorRate(s *model.Session) (rate float64, hits, total int) {
	if s.JSONLPath == "" {
		return 0, 0, 0
	}
	msgs, err := parser.ParseMessages(s.JSONLPath)
	if err != nil {
		return 0, 0, 0
	}
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		total++
		low := lowerASCII(m.Content)
		for _, p := range errorPatterns {
			if containsLow(low, p) {
				hits++
				break
			}
		}
	}
	if total > 0 {
		rate = float64(hits) / float64(total)
	}
	return
}

func lowerASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func containsLow(haystack, needle string) bool {
	return len(haystack) >= len(needle) && index(haystack, needle) >= 0
}

func index(haystack, needle string) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// --- Time × cost scatter ---

type TimeCostPoint struct {
	Hour    int     `json:"hour"`
	CostUSD float64 `json:"cost_usd"`
	Model   string  `json:"model"`
	Project string  `json:"project_dir"`
}

func TimeCostPoints(sessions []*model.Session, p *pricing.Pricing) []TimeCostPoint {
	if p == nil {
		return nil
	}
	out := make([]TimeCostPoint, 0, len(sessions))
	for _, s := range sessions {
		c, ok := p.Cost(s)
		if !ok {
			continue
		}
		out = append(out, TimeCostPoint{
			Hour:    s.StartTime.Hour(),
			CostUSD: c.USD,
			Model:   s.Model,
			Project: s.ProjectDir,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hour != out[j].Hour {
			return out[i].Hour < out[j].Hour
		}
		return out[i].CostUSD < out[j].CostUSD
	})
	return out
}

// --- helpers ---

// walkUserTokens itera tokenizando user messages e chamando fn pra cada lista de tokens.
// Cache simples por sessionID seria possível mas mantemos simples — re-parse JSONL.
func walkUserTokens(sessions []*model.Session, fn func([]string)) {
	for _, s := range sessions {
		if s.JSONLPath == "" {
			continue
		}
		msgs, err := parser.ParseMessages(s.JSONLPath)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if m.Role != "user" {
				continue
			}
			toks := Tokenize(m.Content)
			if len(toks) > 0 {
				fn(toks)
			}
		}
	}
}

// (avoid unused import warnings if pricing/time not used in some build paths)
var _ = time.Time{}

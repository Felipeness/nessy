package stats

import (
	"regexp"
	"sort"
	"strings"

	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/parser"
)

var wordRe = regexp.MustCompile(`(?i)\b[\p{L}]+\b`)

// errorPatterns são heurísticas determinísticas pra detectar retrabalho.
var errorPatterns = []string{
	"errado", "errei", "errou", "errado",
	"nao funciona", "não funciona", "não rodou", "nao rodou",
	"rollback", "desfaz", "desfaça", "ignora",
	"esqueci", "mudei de ideia",
	"cancela", "para", "stop", "fix",
	"bug", "broken", "fail", "failed",
}

// Tokenize quebra texto em palavras lowercased, filtrando stopwords.
func Tokenize(text string) []string {
	matches := wordRe.FindAllString(text, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		w := strings.ToLower(m)
		if IsStopword(w) || len(w) < 2 {
			continue
		}
		out = append(out, w)
	}
	return out
}

// WordCount é uma palavra com sua contagem.
type WordCount struct {
	Word  string
	Count int
}

// TopWords reúne user msgs de todas as sessions e retorna top N palavras.
func TopWords(sessions []*model.Session, n int) []WordCount {
	freq := map[string]int{}
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
			for _, w := range Tokenize(m.Content) {
				freq[w]++
			}
		}
	}
	pairs := make([]WordCount, 0, len(freq))
	for w, c := range freq {
		pairs = append(pairs, WordCount{w, c})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Count != pairs[j].Count {
			return pairs[i].Count > pairs[j].Count
		}
		return pairs[i].Word < pairs[j].Word
	})
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	return pairs
}

// ErrorRate retorna proporção de user msgs com sinais de retrabalho.
func ErrorRate(sessions []*model.Session) (rate float64, hits, total int) {
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
			total++
			low := strings.ToLower(m.Content)
			for _, p := range errorPatterns {
				if strings.Contains(low, p) {
					hits++
					break
				}
			}
		}
	}
	if total > 0 {
		rate = float64(hits) / float64(total)
	}
	return
}

// TopPrefixes retorna top N primeiras-palavras de user msgs.
func TopPrefixes(sessions []*model.Session, n int) []WordCount {
	freq := map[string]int{}
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
			words := Tokenize(m.Content)
			if len(words) > 0 {
				freq[words[0]]++
			}
		}
	}
	pairs := make([]WordCount, 0, len(freq))
	for w, c := range freq {
		pairs = append(pairs, WordCount{w, c})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Count != pairs[j].Count {
			return pairs[i].Count > pairs[j].Count
		}
		return pairs[i].Word < pairs[j].Word
	})
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	return pairs
}

// PeakHour retorna count de user msgs por hora do dia (24 bins).
func PeakHour(sessions []*model.Session) [24]int {
	var bins [24]int
	for _, s := range sessions {
		if s.JSONLPath == "" {
			continue
		}
		// Heurística rápida: usa StartTime da session (custo: subestima distribuição
		// dentro de uma session longa, mas é determinístico e barato).
		bins[s.StartTime.Hour()] += s.UserMessages
	}
	return bins
}

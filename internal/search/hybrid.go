// Package search implementa busca híbrida via Reciprocal Rank Fusion (RRF).
//
// Por que RRF?
//
//	Combinar BM25 (lexical, pega identifiers tipo "UserAuth") com dense
//	(semantic, pega "how to authenticate users") sem precisar normalizar
//	scores entre sistemas (BM25 score depende de IDF do corpus, cosine
//	é 0..1). RRF ignora score absoluto, usa só o RANK em cada fonte:
//	  score(d) = Σ_source weight_source / (k + rank_source(d))
//	Constante k=60 (default da literatura) suaviza diferenças no top.
//
// Trade-offs:
//
//	+ Sem hyperparameter tuning (BM25 vs dense weight)
//	+ Robusto a outliers de score
//	- Perde info de "score gap" (top-1 vs top-2) — 2 itens próximos
//	  no rank ficam ~iguais no RRF
package search

import (
	"regexp"
	"sort"
	"strings"
)

const (
	// rrfK é a constante de smoothing. 60 é o default na literatura
	// (Cormack et al, SIGIR 2009). Maior = mais peso pros top hits;
	// menor = ranks distantes pesam mais.
	rrfK = 60
	// finalLimit limita o output combinado. 50 é folgado o bastante pra
	// reranker downstream filtrar os top-N que importam.
	finalLimit = 50
)

// QueryType classifica o tipo de query pra ajustar pesos RRF.
// Detecta heuristicamente identifier-like (camelCase, snake_case, paths)
// vs prose (frases longas em natural language).
type QueryType int

const (
	// QueryHybrid: peso balanceado BM25/dense. Default pra queries curtas
	// neutras tipo "auth bug".
	QueryHybrid QueryType = iota
	// QueryBM25Heavy: identifiers/símbolos onde lexical match >> semantic.
	// Ex: "UserAuth", "getUser()", "auth.go", "CC-1234".
	QueryBM25Heavy
	// QueryDenseHeavy: prose longa onde paráfrase importa.
	// Ex: "como autenticar usuários no NestJS via JWT".
	QueryDenseHeavy
)

// identRe casa identifiers comuns: camelCase (FooBar), snake_case (foo_bar),
// scope (Foo::bar), method calls (.foo(), ::bar()), file paths (foo.go),
// ticket IDs (CC-1234).
var identRe = regexp.MustCompile(
	`[A-Z][a-z]+[A-Z]|` + // camelCase
		`_[a-z]|` + // snake_case
		`::|` + // C++/Rust scope
		`\.[a-z]+\(|` + // method call
		`\.[a-z]+\b|` + // file ext
		`[A-Z]+-\d+`, // ticket ID
)

// DetectQueryType aplica heurísticas pra classificar.
//
//	≥6 palavras            → DenseHeavy (prose dominates, mesmo com identifiers)
//	identifier sem prose   → BM25Heavy
//	caso geral             → Hybrid (50/50)
//
// Ordem importa: prose check primeiro porque uma query longa COM identifiers
// (ex: "fix bug in UserAuth using NestJS guards") se beneficia mais do
// componente semântico que do lexical.
func DetectQueryType(q string) QueryType {
	q = strings.TrimSpace(q)
	if q == "" {
		return QueryHybrid
	}
	if len(strings.Fields(q)) >= 6 {
		return QueryDenseHeavy
	}
	if identRe.MatchString(q) {
		return QueryBM25Heavy
	}
	return QueryHybrid
}

// WeightsFor devolve pesos por source pra cada QueryType.
// Pesos somam ~2 pra simplificar comparação visual; o que importa é a
// proporção entre eles.
func WeightsFor(qt QueryType) map[string]float64 {
	switch qt {
	case QueryBM25Heavy:
		return map[string]float64{"bm25": 1.5, "dense": 0.5}
	case QueryDenseHeavy:
		return map[string]float64{"bm25": 0.5, "dense": 1.5}
	default:
		return map[string]float64{"bm25": 1.0, "dense": 1.0}
	}
}

// Hit é uma sessão num resultado RRF mergeado.
type Hit struct {
	SessionID string
	// Score acumulado de RRF — soma das contribuições de cada source.
	Score float64
	// Sources lista as fontes que contribuíram (ordem da contribuição).
	// Útil pra UI mostrar "matched: bm25, dense".
	Sources []string
	// Ranks: source → posição (1-based) naquela fonte. Pra debug/UI.
	Ranks map[string]int
}

// IsUUID devolve true se a query parece um session UUID (8-4-4-4-12 hex).
// raine/claude-history detecta isso mas não wira ao path; nessy roteia
// pra direct jump quando user cola UUID no search.
func IsUUID(q string) bool {
	q = strings.TrimSpace(q)
	parts := strings.Split(q, "-")
	if len(parts) != 5 {
		return false
	}
	expectedLens := []int{8, 4, 4, 4, 12}
	for i, p := range parts {
		if len(p) != expectedLens[i] {
			return false
		}
		for _, c := range p {
			if !isHex(byte(c)) {
				return false
			}
		}
	}
	return true
}

// IsUUIDPrefix devolve true se q é prefix de UUID (≥6 hex chars contínuos
// ou parte com dashes nas posições corretas). Pra search por "abc12345".
func IsUUIDPrefix(q string) bool {
	q = strings.TrimSpace(q)
	if len(q) < 6 || len(q) > 36 {
		return false
	}
	for i, c := range q {
		// Posições 8, 13, 18, 23 só aceitam '-' (estrutura UUID)
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !isHex(byte(c)) {
			return false
		}
	}
	return true
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// MergeRRF combina múltiplos rankings de session_ids via RRF.
//
//	rankings: map source → []sessionID ordenado por relevância (rank 1 = melhor)
//	weights:  source → peso multiplicativo (nil/missing = 1.0)
//
// Devolve hits ordenados por score desc, capped em finalLimit (50).
// Tiebreak deterministic por SessionID asc.
func MergeRRF(rankings map[string][]string, weights map[string]float64) []Hit {
	if weights == nil {
		weights = map[string]float64{}
	}
	scores := map[string]*Hit{}
	for source, ids := range rankings {
		w := weights[source]
		if w == 0 {
			w = 1
		}
		for i, id := range ids {
			rank := i + 1
			h, ok := scores[id]
			if !ok {
				h = &Hit{SessionID: id, Ranks: map[string]int{}}
				scores[id] = h
			}
			h.Score += w / float64(rrfK+rank)
			h.Sources = append(h.Sources, source)
			h.Ranks[source] = rank
		}
	}
	out := make([]Hit, 0, len(scores))
	for _, h := range scores {
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].SessionID < out[j].SessionID
	})
	if len(out) > finalLimit {
		out = out[:finalLimit]
	}
	return out
}

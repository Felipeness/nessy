package ai

import (
	"math"
	"sort"

	"github.com/felipeness/nessy/internal/index"
)

// Cosine retorna similaridade cosseno entre 2 vetores. Retorna 0 se algum for vazio.
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// SimilarResult representa uma session ranqueada por similaridade.
type SimilarResult struct {
	SessionID  string  `json:"session_id"`
	Similarity float64 `json:"similarity"`
}

// FindSimilar carrega todos os embeddings e retorna top N mais próximos.
func FindSimilar(db *index.DB, sessionID string, n int) ([]SimilarResult, error) {
	target, err := db.AICacheGet(sessionID)
	if err != nil {
		return nil, err
	}
	if len(target.Embedding) == 0 {
		return nil, nil
	}
	tEmb := DecodeEmbedding(target.Embedding)

	all, err := db.AICacheList()
	if err != nil {
		return nil, err
	}
	out := make([]SimilarResult, 0, len(all)-1)
	for _, c := range all {
		if c.SessionID == sessionID || len(c.Embedding) == 0 {
			continue
		}
		sim := Cosine(tEmb, DecodeEmbedding(c.Embedding))
		out = append(out, SimilarResult{SessionID: c.SessionID, Similarity: sim})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Similarity != out[j].Similarity {
			return out[i].Similarity > out[j].Similarity
		}
		return out[i].SessionID < out[j].SessionID
	})
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

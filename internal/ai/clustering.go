package ai

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/felipeness/nessy/internal/index"
)

const defaultK = 10
const maxIters = 50

// KMeans roda k-means++ em embeddings, retorna cluster IDs e centroids.
func KMeans(embeddings [][]float32, k int, seed int64) ([]int, [][]float32) {
	n := len(embeddings)
	if n == 0 || k <= 0 {
		return nil, nil
	}
	if k > n {
		k = n
	}
	dim := len(embeddings[0])
	rng := rand.New(rand.NewSource(seed))

	// k-means++ init
	centroids := make([][]float32, k)
	first := rng.Intn(n)
	centroids[0] = copyVec(embeddings[first])
	for c := 1; c < k; c++ {
		dists := make([]float64, n)
		var sum float64
		for i := 0; i < n; i++ {
			minD := 1e18
			for j := 0; j < c; j++ {
				d := euclidSq(embeddings[i], centroids[j])
				if d < minD {
					minD = d
				}
			}
			dists[i] = minD
			sum += minD
		}
		if sum == 0 {
			centroids[c] = copyVec(embeddings[rng.Intn(n)])
			continue
		}
		r := rng.Float64() * sum
		acc := 0.0
		picked := 0
		for i, d := range dists {
			acc += d
			if acc >= r {
				picked = i
				break
			}
		}
		centroids[c] = copyVec(embeddings[picked])
	}

	assigns := make([]int, n)
	for iter := 0; iter < maxIters; iter++ {
		changed := false
		for i := 0; i < n; i++ {
			best := 0
			bestD := euclidSq(embeddings[i], centroids[0])
			for j := 1; j < k; j++ {
				d := euclidSq(embeddings[i], centroids[j])
				if d < bestD {
					best, bestD = j, d
				}
			}
			if assigns[i] != best {
				assigns[i] = best
				changed = true
			}
		}
		// recompute centroids
		newCent := make([][]float32, k)
		counts := make([]int, k)
		for j := 0; j < k; j++ {
			newCent[j] = make([]float32, dim)
		}
		for i := 0; i < n; i++ {
			c := assigns[i]
			counts[c]++
			for d := 0; d < dim; d++ {
				newCent[c][d] += embeddings[i][d]
			}
		}
		for j := 0; j < k; j++ {
			if counts[j] == 0 {
				newCent[j] = copyVec(embeddings[rng.Intn(n)])
				continue
			}
			for d := 0; d < dim; d++ {
				newCent[j][d] /= float32(counts[j])
			}
		}
		centroids = newCent
		if !changed {
			break
		}
	}
	return assigns, centroids
}

func euclidSq(a, b []float32) float64 {
	var s float64
	for i := range a {
		d := float64(a[i] - b[i])
		s += d * d
	}
	return s
}

func copyVec(v []float32) []float32 {
	out := make([]float32, len(v))
	copy(out, v)
	return out
}

// RecomputeClusters carrega cache, roda k-means, persiste cluster IDs e gera labels via LLM.
func RecomputeClusters(ctx context.Context, db *index.DB, client *Client, genModel string) ([]ClusterInfo, error) {
	caches, err := db.AICacheList()
	if err != nil {
		return nil, err
	}
	type item struct {
		cache *index.AICache
		emb   []float32
	}
	var items []item
	for _, c := range caches {
		emb := DecodeEmbedding(c.Embedding)
		if len(emb) == 0 {
			continue
		}
		items = append(items, item{c, emb})
	}
	if len(items) < 2 {
		return nil, nil
	}
	embs := make([][]float32, len(items))
	for i, it := range items {
		embs[i] = it.emb
	}
	k := defaultK
	if len(items) < k {
		k = len(items)
	}
	assigns, centroids := KMeans(embs, k, 42)

	// pra cada cluster, pega top 5 mais próximos do centroid pra gerar label
	type clusterAggr struct {
		members []int
		samples []string
	}
	cs := make([]clusterAggr, k)
	dists := make([]struct {
		idx   int
		dist  float64
	}, len(items))
	for j := 0; j < k; j++ {
		dists = dists[:0]
		for i, a := range assigns {
			if a != j {
				continue
			}
			dists = append(dists, struct {
				idx  int
				dist float64
			}{i, euclidSq(embs[i], centroids[j])})
			cs[j].members = append(cs[j].members, i)
		}
		sort.Slice(dists, func(a, b int) bool {
			if dists[a].dist != dists[b].dist {
				return dists[a].dist < dists[b].dist
			}
			return items[dists[a].idx].cache.SessionID < items[dists[b].idx].cache.SessionID
		})
		for i := 0; i < 5 && i < len(dists); i++ {
			s := items[dists[i].idx].cache.Summary
			if s != "" {
				cs[j].samples = append(cs[j].samples, s)
			}
		}
	}

	// gera labels via LLM
	labels := make([]string, k)
	for j := 0; j < k; j++ {
		if len(cs[j].samples) == 0 {
			labels[j] = fmt.Sprintf("cluster-%d", j)
			continue
		}
		label, err := generateLabel(ctx, client, genModel, cs[j].samples)
		if err != nil {
			labels[j] = fmt.Sprintf("cluster-%d", j)
		} else {
			labels[j] = label
		}
	}

	// persist clusters
	out := make([]ClusterInfo, 0, k)
	for j := 0; j < k; j++ {
		ci := ClusterInfo{ClusterID: j, Label: labels[j]}
		for _, idx := range cs[j].members {
			sid := items[idx].cache.SessionID
			_ = db.AICacheUpdateCluster(sid, j, labels[j])
			ci.SessionIDs = append(ci.SessionIDs, sid)
		}
		out = append(out, ci)
	}
	return out, nil
}

const labelPromptPT = `Dê um label de NO MÁXIMO 4 palavras (em português brasileiro, lowercase, sem pontuação) que descreva esse grupo de conversas. Responda SÓ com o label, sem explicação.

Exemplos do grupo:
%s

Label (máx 4 palavras, lowercase):`

func generateLabel(ctx context.Context, client *Client, model string, samples []string) (string, error) {
	body := strings.Join(samples, "\n- ")
	prompt := fmt.Sprintf(labelPromptPT, "- "+body)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := client.Generate(cctx, model, prompt)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "\"'`.,")
		line = strings.ToLower(line)
		if line == "" {
			continue
		}
		// limit 4 words
		words := strings.Fields(line)
		if len(words) > 4 {
			words = words[:4]
		}
		return strings.Join(words, " "), nil
	}
	return "", fmt.Errorf("empty label")
}

// ClusterInfo é o resumo de um cluster pra API.
type ClusterInfo struct {
	ClusterID  int      `json:"cluster_id"`
	Label      string   `json:"label"`
	SessionIDs []string `json:"session_ids"`
}

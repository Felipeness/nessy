// Package ai cliente Ollama + geração de resumos, embeddings e clustering.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client é um cliente HTTP minimal pra Ollama local.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient cria um cliente apontando pra `baseURL` (ex: http://localhost:11434).
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

// Health checa se Ollama responde em /api/tags em até 2s.
func (c *Client) Health(ctx context.Context) bool {
	hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(hctx, http.MethodGet, c.BaseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// Models lista modelos instalados.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/tags", nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		out = append(out, m.Name)
	}
	return out, nil
}

// Generate chama /api/generate (non-streaming) com 2048 tokens de output.
func (c *Client) Generate(ctx context.Context, model, prompt string) (string, error) {
	return c.generate(ctx, model, prompt, 2048)
}

// GenerateLong é como Generate mas com 4096 tokens — pra outputs longos
// (insights JSON, profile detalhado).
func (c *Client) GenerateLong(ctx context.Context, model, prompt string) (string, error) {
	return c.generate(ctx, model, prompt, 4096)
}

func (c *Client) generate(ctx context.Context, model, prompt string, numPredict int) (string, error) {
	body := map[string]any{
		"model":  model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]any{
			"temperature": 0.3,
			"num_predict": numPredict,
		},
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/generate", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama generate %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Response), nil
}

// Embedding chama /api/embeddings, retorna vetor float32.
func (c *Client) Embedding(ctx context.Context, model, text string) ([]float32, error) {
	body := map[string]any{
		"model":  model,
		"prompt": text,
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/embeddings", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embedding %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	emb := make([]float32, len(out.Embedding))
	for i, v := range out.Embedding {
		emb[i] = float32(v)
	}
	return emb, nil
}

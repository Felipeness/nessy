package ai

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/felipeness/nessy/internal/index"
)

// EventBroadcaster é uma interface mínima de hub SSE pra desacoplar do server.
type EventBroadcaster interface {
	Broadcast(name string, data any)
}

// Worker processa queue de geração de summary + embedding em background.
type Worker struct {
	DB         *index.DB
	Client     *Client
	GenModel   string
	EmbedModel string
	Hub        EventBroadcaster

	queue   chan string
	mu      sync.Mutex
	pending map[string]bool
}

// NewWorker constrói um Worker com queue tamanho 256.
func NewWorker(db *index.DB, client *Client, genModel, embedModel string, hub EventBroadcaster) *Worker {
	return &Worker{
		DB:         db,
		Client:     client,
		GenModel:   genModel,
		EmbedModel: embedModel,
		Hub:        hub,
		queue:      make(chan string, 256),
		pending:    map[string]bool{},
	}
}

// Enqueue adiciona session_id à fila se não estiver pendente.
func (w *Worker) Enqueue(sessionID string) {
	w.mu.Lock()
	if w.pending[sessionID] {
		w.mu.Unlock()
		return
	}
	w.pending[sessionID] = true
	w.mu.Unlock()
	select {
	case w.queue <- sessionID:
	default:
		// queue cheia — drop, próxima chamada de generate-all preenche
	}
}

// QueuedCount aproxima quantos itens estão pendentes.
func (w *Worker) QueuedCount() int {
	return len(w.queue)
}

// Run consome a queue até ctx cancelado.
func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-w.queue:
			w.process(ctx, id)
			w.mu.Lock()
			delete(w.pending, id)
			w.mu.Unlock()
		}
	}
}

func (w *Worker) process(ctx context.Context, sessionID string) {
	sess, err := w.DB.GetByID(sessionID)
	if err != nil {
		return
	}
	transcript := BuildTranscript(sess)
	if transcript == "" {
		return
	}

	summary, err := GenerateSummary(ctx, w.Client, w.GenModel, transcript)
	if err != nil {
		log.Printf("ai: summary fail %s: %v", sessionID[:8], err)
		return
	}

	embText := EmbedTextFromSession(sess)
	emb, err := w.Client.Embedding(ctx, w.EmbedModel, embText)
	if err != nil {
		log.Printf("ai: embedding fail %s: %v", sessionID[:8], err)
	}

	cache := &index.AICache{
		SessionID:    sessionID,
		JSONLMtime:   sess.JSONLMtime.UnixNano(),
		Summary:      summary,
		Embedding:    EncodeEmbedding(emb),
		TopicCluster: -1,
		GeneratedAt:  time.Now().Unix(),
	}
	if err := w.DB.AICacheUpsert(cache); err != nil {
		log.Printf("ai: cache upsert fail %s: %v", sessionID[:8], err)
		return
	}

	if w.Hub != nil {
		w.Hub.Broadcast("summary_done", map[string]any{
			"session_id": sessionID,
			"summary":    summary,
		})
	}
}

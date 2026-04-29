// Package server expõe a API REST + SSE pra Web UI da Fase 3.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Hub mantém conexões SSE ativas e faz broadcast.
type Hub struct {
	mu      sync.Mutex
	clients map[chan sseEvent]bool
}

type sseEvent struct {
	Name string
	Data any
}

// NewHub cria um Hub vazio.
func NewHub() *Hub {
	return &Hub{clients: map[chan sseEvent]bool{}}
}

// Broadcast envia evento pra todos os clients conectados.
func (h *Hub) Broadcast(name string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- sseEvent{Name: name, Data: data}:
		default:
			// client lento — drop pra não bloquear o broadcast
		}
	}
}

// Handler é o http.Handler que mantém a conexão aberta e escreve eventos.
func (h *Hub) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming não suportado", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ch := make(chan sseEvent, 16)
		h.mu.Lock()
		h.clients[ch] = true
		h.mu.Unlock()

		defer func() {
			h.mu.Lock()
			delete(h.clients, ch)
			h.mu.Unlock()
			close(ch)
		}()

		// hello inicial pra cliente saber que está conectado
		fmt.Fprintf(w, "event: hello\ndata: %q\n\n", "connected")
		flusher.Flush()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-ch:
				payload, err := json.Marshal(ev.Data)
				if err != nil {
					payload = []byte("null")
				}
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, payload)
				flusher.Flush()
			}
		}
	}
}

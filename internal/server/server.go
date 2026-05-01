package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/felipeness/nessy/internal/ai"
	"github.com/felipeness/nessy/internal/index"
	"github.com/felipeness/nessy/internal/config"
	"github.com/felipeness/nessy/internal/pricing"
)

// Server agrega backend state pra os handlers.
type Server struct {
	DB      *index.DB
	Pricing *pricing.Pricing
	Hub     *Hub
	Static  http.Handler // serve frontend (go:embed); pode ser nil em dev

	AIEnabled bool
	AIClient  *ai.Client
	AIWorker  *ai.Worker
	GenModel  string

	Config     *config.Config
	ConfigPath string
}

// Run inicializa o HTTP server, registra rotas e bloqueia até erro/sigterm.
// Se openBrowser, dispara `open` na URL após bind.
func Run(s *Server, listen string, openBrowser bool) error {
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})

	// API
	registerAPI(mux, s)

	// SSE
	mux.HandleFunc("/api/events", s.Hub.Handler())

	// Static (se houver) — wildcard fallback pra SPA routing
	if s.Static != nil {
		mux.Handle("/", s.Static)
	}

	srv := &http.Server{
		Addr:              listen,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if openBrowser {
		go func() {
			time.Sleep(200 * time.Millisecond)
			openInBrowser("http://" + listen)
		}()
	}

	log.Printf("listening on http://%s", listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// localhost only — libera tudo
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// openInBrowser tenta abrir a URL no browser default. Erro silencioso —
// o usuário sempre pode abrir manualmente.
func openInBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}

// Shutdown gracefully (não usado por enquanto, mas reservado).
func Shutdown(ctx context.Context, srv *http.Server) error {
	return srv.Shutdown(ctx)
}

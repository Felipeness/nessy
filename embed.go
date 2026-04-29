package main

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
)

// webDist é o build estático do Vite. Em dev (sem `bun run build`), a pasta
// fica vazia ou inexistente — webStatic será nil e o backend serve só /api.
//
//go:embed all:web/dist
var webDist embed.FS

// webStatic é o http.Handler que serve a SPA. Suporta SPA fallback: paths
// não-encontrados servem index.html (pra hash routing/deep links).
var webStatic http.Handler = func() http.Handler {
	sub, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		return nil
	}
	// se o dist está vazio, serve uma mensagem simples
	if entries, err := fs.ReadDir(sub, "."); err != nil || len(entries) == 0 {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "frontend não buildado — rode `cd web && bun run build` antes de `go build`, ou use `bun dev` em :5173 com proxy", http.StatusServiceUnavailable)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// se path tem extensão, serve direto (asset)
		if hasExt(r.URL.Path) {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback: serve index.html
		f, err := sub.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.Copy(w, f)
	})
}()

func hasExt(path string) bool {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return true
		}
		if path[i] == '/' {
			return false
		}
	}
	return false
}

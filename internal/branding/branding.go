// Package branding centraliza nome, tagline e paths do app.
//
// O projeto era chamado "claude-history" durante a fase 1-12.S; em maio/2026
// renomeou pra "Nessy" pra evitar colisão com raine/claude-history (Rust)
// e alinhar com o chatbot interno "Ness IA". O Go module path permanece
// github.com/felipeness/nessy até GitHub repo ser renomeado.
package branding

import (
	"os"
	"path/filepath"
)

const (
	// Name é o nome de marca. UI/help/banners usam este.
	Name = "Nessy"

	// Tagline expande o acrônimo.
	Tagline = "Nano Episodic Session Studio (Your)"

	// Binary é o nome do executável instalado em PATH.
	Binary = "nessy"

	// CacheDirName é o subdir no $HOME pra cache local (sem ponto).
	cacheDirNew = ".nessy"
	cacheDirOld = ".claude-history" // legacy — migrado on-demand
)

// CacheDir devolve o caminho absoluto do cache dir, fazendo migração
// transparente se a versão antiga (~/.claude-history/) existir e a nova
// (~/.nessy/) ainda não. Op é silenciosa em caso de erro.
func CacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	newDir := filepath.Join(home, cacheDirNew)
	oldDir := filepath.Join(home, cacheDirOld)
	// Migração one-shot: rename atômico se possível
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		if _, err := os.Stat(oldDir); err == nil {
			_ = os.Rename(oldDir, newDir)
		}
	}
	return newDir
}

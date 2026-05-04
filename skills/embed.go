// Package skills embeds the Nessy spec-pipeline skills into the binary.
//
// Skills são markdown files (SKILL.md) que ensinam um AI agent (Claude Code,
// Codex, Cursor, etc) a executar o pipeline de spec generation. O comando
// `nessy install` copia esses arquivos pro engine detectado.
package skills

import (
	"embed"
	"io/fs"
)

//go:embed all:nessy all:nessy-mapper all:nessy-decoder all:nessy-blueprint all:nessy-scribe
var fsRaw embed.FS

// FS devolve o filesystem embedded com todos os skills.
// Cada subdir top-level é um skill (ex: "nessy", "nessy-mapper") e
// contém um SKILL.md.
func FS() fs.FS {
	return fsRaw
}

// Names devolve os nomes de todos os skills bundled (sem path).
func Names() []string {
	return []string{
		"nessy",
		"nessy-mapper",
		"nessy-decoder",
		"nessy-blueprint",
		"nessy-scribe",
	}
}

// Comando `nessy install` — instala os skills bundled em engines de AI
// detectados (Claude Code, Codex, Cursor, Gemini CLI, etc.). Skills sao prompt
// files (SKILL.md) copiados pra um diretorio que o engine escaneia
// automaticamente, mais um entry file na raiz do projeto pra documentar
// que o skill esta disponivel.
//
// Modo padrao: instala no projeto atual (.claude/skills/, .agents/skills/).
// `--global` instala em ~/.claude/skills/ pra ficar disponivel em todos
// projetos do usuario (Claude Code only — outros engines nao tem skills
// globais).

package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/felipeness/nessy/skills"
)

// engine descreve um AI engine alvo. SkillsPath e relativo ao projeto (ou
// ~/, se IsGlobalCapable e useGlobal).
type engine struct {
	Name         string
	SkillsPath   string // ex: ".claude/skills" ou ".agents/skills"
	EntryFile    string // ex: "CLAUDE.md", "AGENTS.md"
	GlobalPath   string // ex: ".claude/skills" sob ~/, vazio se nao suporta global
	DetectMarker string // arquivo/dir cuja existencia (sob ~/) sugere o engine instalado
}

var engines = []engine{
	{
		Name:         "Claude Code",
		SkillsPath:   ".claude/skills",
		EntryFile:    "CLAUDE.md",
		GlobalPath:   ".claude/skills",
		DetectMarker: ".claude",
	},
	{
		Name:         "Codex",
		SkillsPath:   ".agents/skills",
		EntryFile:    "AGENTS.md",
		GlobalPath:   "",
		DetectMarker: ".codex",
	},
	{
		Name:         "Cursor",
		SkillsPath:   ".agents/skills",
		EntryFile:    ".cursorrules",
		GlobalPath:   "",
		DetectMarker: ".cursor",
	},
	{
		Name:         "Gemini CLI",
		SkillsPath:   ".agents/skills",
		EntryFile:    "GEMINI.md",
		GlobalPath:   "",
		DetectMarker: ".gemini",
	},
}

func cmdInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	global := fs.Bool("global", false, "install in user home (~) instead of current project")
	all := fs.Bool("all", false, "install for all known engines without prompting")
	yes := fs.Bool("yes", false, "skip confirmation prompts (alias of --all)")
	_ = fs.Parse(args)

	autoConfirm := *all || *yes

	home, err := os.UserHomeDir()
	if err != nil {
		fatal(err)
	}

	projectRoot, err := os.Getwd()
	if err != nil {
		fatal(err)
	}

	// Detecta engines provaveis
	detected := []engine{}
	for _, e := range engines {
		if e.DetectMarker == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(home, e.DetectMarker)); err == nil {
			detected = append(detected, e)
		}
	}

	// Decide alvos
	var targets []engine
	switch {
	case autoConfirm && len(detected) > 0:
		targets = detected
	case autoConfirm:
		targets = engines // --all sem detectar instala em todos
	default:
		targets = pickInteractive(detected)
	}

	if len(targets) == 0 {
		fmt.Println("Nenhum engine selecionado. Saindo.")
		return
	}

	// Instala
	for _, e := range targets {
		base := projectRoot
		if *global {
			if e.GlobalPath == "" {
				fmt.Fprintf(os.Stderr, "%s: nao suporta install global, pulando\n", e.Name)
				continue
			}
			base = home
		}
		if err := installEngine(e, base, *global); err != nil {
			fmt.Fprintf(os.Stderr, "%s: erro — %v\n", e.Name, err)
			continue
		}
		scope := "projeto"
		if *global {
			scope = "global (~)"
		}
		fmt.Printf("✓ %s — skills instalados em %s (%s)\n",
			e.Name, filepath.Join(base, e.SkillsPath), scope)
	}

	fmt.Println()
	fmt.Println("Pronto. Pra usar:")
	fmt.Println("  - Claude Code/Cursor/Gemini: digite /nessy")
	fmt.Println("  - Codex/Aider: digite 'nessy' (sem barra)")
	fmt.Println()
	fmt.Println("O agent vai analisar o codebase e gerar specs em _nessy_atlas/.")
}

// installEngine copia todos os skills bundled pra <base>/<engine.SkillsPath>/
// e cria o entry file (so se nao existir, pra nao sobrescrever CLAUDE.md
// customizado do user — append/merge fica pra V2).
func installEngine(e engine, base string, isGlobal bool) error {
	skillsDir := filepath.Join(base, e.SkillsPath)
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return err
	}

	// Copia skills
	embeddedFS := skills.FS()
	for _, name := range skills.Names() {
		dest := filepath.Join(skillsDir, name)
		if err := os.MkdirAll(dest, 0755); err != nil {
			return err
		}
		if err := copyEmbeddedSkill(embeddedFS, name, dest); err != nil {
			return fmt.Errorf("copiar skill %s: %w", name, err)
		}
	}

	// Cria entry file (so se nao existe E nao for global)
	if !isGlobal && e.EntryFile != "" {
		entryPath := filepath.Join(base, e.EntryFile)
		if _, err := os.Stat(entryPath); os.IsNotExist(err) {
			if err := writeEntryFile(entryPath, e); err != nil {
				return fmt.Errorf("entry file: %w", err)
			}
		}
		// Se existe, nao mexe — user pode ter conteudo customizado
	}

	return nil
}

// copyEmbeddedSkill copia recursivamente um skill do embedded FS pro destino.
func copyEmbeddedSkill(efs fs.FS, skillName, dest string) error {
	return fs.WalkDir(efs, skillName, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(skillName, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		src, err := efs.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, src)
		return err
	})
}

// writeEntryFile cria o arquivo de entry esperado pelo engine (CLAUDE.md,
// AGENTS.md, etc) com instrucao minima apontando pra Nessy. Nao sobrescreve
// existing files — caller checa antes.
func writeEntryFile(path string, e engine) error {
	content := fmt.Sprintf(`# %s

## Nessy — Spec generation skill

Este projeto tem o skill **nessy** instalado. Pra reverse-engineer o codebase
e gerar specs operacionais em `+"`"+`_nessy_sdd/`+"`"+`:

`+"```"+`
/nessy
`+"```"+`

(ou apenas `+"`"+`nessy`+"`"+` em engines sem slash command)

O skill coordena 4 sub-skills (archaeologist → detective → architect → writer)
e marca toda afirmacao com confidence (🟢 confirmado / 🟡 inferido / 🔴 gap).

Skills instalados em `+"`"+`%s/`+"`"+`. Saiba mais: https://github.com/Felipeness/nessy
`, e.Name, e.SkillsPath)
	return os.WriteFile(path, []byte(content), 0644)
}

// pickInteractive mostra os engines detectados e pede confirmacao do user.
// Sem TTY (pipe/script), faz tudo (mesmo comportamento de --all).
func pickInteractive(detected []engine) []engine {
	if len(detected) == 0 {
		fmt.Println("Nenhum engine de AI detectado em ~/.")
		fmt.Println("Use --all pra instalar em todos os engines suportados:")
		for _, e := range engines {
			fmt.Printf("  - %s\n", e.Name)
		}
		return nil
	}
	fmt.Println("Engines detectados:")
	for i, e := range detected {
		fmt.Printf("  [%d] %s\n", i+1, e.Name)
	}
	fmt.Print("Instalar em todos? [Y/n] ")
	var resp string
	if _, err := fmt.Scanln(&resp); err != nil && err.Error() != "unexpected newline" {
		// Sem TTY — assume yes
		return detected
	}
	if strings.ToLower(strings.TrimSpace(resp)) == "n" {
		return nil
	}
	return detected
}

// cmdUninstall remove os skills do projeto atual (ou global com --global).
// Nao toca em entry files — user pode ter customizado.
func cmdUninstall(args []string) {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	global := fs.Bool("global", false, "remove from user home (~) instead of current project")
	_ = fs.Parse(args)

	home, _ := os.UserHomeDir()
	projectRoot, _ := os.Getwd()

	for _, e := range engines {
		base := projectRoot
		if *global {
			if e.GlobalPath == "" {
				continue
			}
			base = home
		}
		skillsDir := filepath.Join(base, e.SkillsPath)
		removed := 0
		for _, name := range skills.Names() {
			path := filepath.Join(skillsDir, name)
			if err := os.RemoveAll(path); err == nil {
				removed++
			}
		}
		if removed > 0 {
			fmt.Printf("✓ %s — %d skills removidos de %s\n", e.Name, removed, skillsDir)
		}
	}
	fmt.Println()
	fmt.Println("Entry files (CLAUDE.md/AGENTS.md/etc) nao removidos — voce pode ter")
	fmt.Println("conteudo customizado neles. Edite manualmente se quiser.")
}

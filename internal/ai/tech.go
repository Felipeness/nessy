package ai

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/felipeness/nessy/internal/model"
	"github.com/felipeness/nessy/internal/parser"
)

// TechMention é uma tecnologia detectada com count de menções.
type TechMention struct {
	Name  string
	Count int
}

// techPatterns lista keywords case-insensitive a procurar no conteúdo das msgs.
// Ordem importa pra labels — primeira que match ganha.
var techPatterns = []struct {
	label string
	re    *regexp.Regexp
}{
	{"Go", regexp.MustCompile(`\b(?i:golang|go\s+(build|run|test|mod|install))\b`)},
	{"TypeScript", regexp.MustCompile(`\b(?i:typescript|tsx?|ts-node)\b`)},
	{"JavaScript", regexp.MustCompile(`\b(?i:javascript|node\.?js|nodejs|npm|yarn)\b`)},
	{"React", regexp.MustCompile(`\b(?i:react|jsx|tsx|next\.?js|vite)\b`)},
	{"Python", regexp.MustCompile(`\b(?i:python|pip|pipx|pyenv|poetry|pyproject|venv)\b`)},
	{"Bun", regexp.MustCompile(`\bbun(?:\s+(?:install|run|add|dev|build|x))?\b`)},
	{"NestJS", regexp.MustCompile(`\bnest(?:js|js)?\b`)},
	{"Tailwind", regexp.MustCompile(`\btailwind(?:css)?\b`)},
	{"Postgres", regexp.MustCompile(`\b(?i:postgres|postgresql|psql|pgcli)\b`)},
	{"Redis", regexp.MustCompile(`\bredis(?:-cli)?\b`)},
	{"MongoDB", regexp.MustCompile(`\b(?i:mongodb|mongosh|mongo)\b`)},
	{"Docker", regexp.MustCompile(`\b(?i:docker|colima|orbstack|dockerfile)\b`)},
	{"Kubernetes", regexp.MustCompile(`\b(?i:kubectl|kubernetes|k8s|helm|stern|kubectx|k9s)\b`)},
	{"AWS", regexp.MustCompile(`\b(?i:aws|ec2|lambda|s3|eks|cloudformation|cloudwatch)\b`)},
	{"Terraform", regexp.MustCompile(`\b(?i:terraform|terragrunt|atmos|opentofu)\b`)},
	{"Bubble Tea", regexp.MustCompile(`\b(?i:bubble.?tea|lipgloss|charm)\b`)},
	{"SQLite", regexp.MustCompile(`\b(?i:sqlite|fts5|modernc)\b`)},
	{"Vite", regexp.MustCompile(`\bvite\b`)},
	{"Recharts", regexp.MustCompile(`\brecharts\b`)},
	{"Ollama", regexp.MustCompile(`\bollama\b`)},
	{"Git", regexp.MustCompile(`\b(?i:git|github|gh\s+(?:repo|pr|issue))\b`)},
	{"Obsidian", regexp.MustCompile(`\bobsidian\b`)},
	{"Ghostty", regexp.MustCompile(`\bghostty\b`)},
	{"Hammerspoon", regexp.MustCompile(`\bhammerspoon\b`)},
	{"Claude Code", regexp.MustCompile(`\bclaude.?code\b`)},
}

// DetectTech escaneia user+assistant msgs e retorna tecnologias mencionadas
// ranqueadas por contagem. Tiebreaker alfabético.
func DetectTech(sessions []*model.Session) []TechMention {
	counts := map[string]int{}
	for _, s := range sessions {
		if s.JSONLPath == "" {
			continue
		}
		msgs, err := parser.ParseMessages(s.JSONLPath)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			content := m.Content
			for _, tp := range techPatterns {
				matches := tp.re.FindAllString(content, -1)
				if len(matches) > 0 {
					counts[tp.label] += len(matches)
				}
			}
		}
	}
	out := make([]TechMention, 0, len(counts))
	for k, v := range counts {
		out = append(out, TechMention{Name: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// loadAboutOverride lê ~/.claude-history/about.txt se existir.
func loadAboutOverride() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".claude-history", "about.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

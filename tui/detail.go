package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/pricing"
)

func renderDetail(s *model.Session, p *pricing.Pricing) string {
	if s == nil {
		return "(nenhuma session selecionada)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Session: %s\n", s.SessionID)
	fmt.Fprintf(&b, "Pasta: %s\n", s.ProjectDir)
	fmt.Fprintf(&b, "Branch: %s\n", orDash(s.GitBranch))
	fmt.Fprintf(&b, "Início: %s\n", s.StartTime.Local().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "Duração: %s\n", s.Duration().Round(1e9))
	fmt.Fprintf(&b, "Modelo: %s\n", orDash(s.Model))
	fmt.Fprintf(&b, "\nTokens\n──────\n")
	fmt.Fprintf(&b, "Input:    %s\n", fmtInt(s.InputTokens))
	fmt.Fprintf(&b, "Output:   %s\n", fmtInt(s.OutputTokens))
	fmt.Fprintf(&b, "Cache cr: %s\n", fmtInt(s.CacheCreationTokens))
	fmt.Fprintf(&b, "Cache rd: %s\n", fmtInt(s.CacheReadTokens))
	if p != nil {
		if cost, ok := p.Cost(s); ok {
			if p.BRLRate > 0 {
				fmt.Fprintf(&b, "Custo: $%.2f USD (~R$ %.2f)\n", cost.USD, cost.BRL)
			} else {
				fmt.Fprintf(&b, "Custo: $%.2f USD\n", cost.USD)
			}
		} else {
			fmt.Fprintf(&b, "Custo: ? (modelo %q sem entry no pricing.toml)\n", s.Model)
		}
	}
	if len(s.ToolCalls) > 0 {
		fmt.Fprintf(&b, "\nTools\n─────\n")
		type kv struct {
			k string
			v int
		}
		var pairs []kv
		for k, v := range s.ToolCalls {
			pairs = append(pairs, kv{k, v})
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
		for _, p := range pairs {
			fmt.Fprintf(&b, "  %-15s %d\n", p.k, p.v)
		}
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func fmtInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

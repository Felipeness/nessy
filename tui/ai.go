package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/ai"
	"github.com/felipeness/claude-history/internal/index"
	"github.com/felipeness/claude-history/internal/model"
)

func jsonUnmarshalDirect(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

type aiView struct {
	enabled    bool
	client     *ai.Client
	worker     *ai.Worker
	genModel   string
	embedModel string
	db         *index.DB
	sessions   []*model.Session
	caches     map[string]*index.AICache
	insights   []*index.Insight
	profile    string
	knowledge  map[string]*index.Knowledge
	aggregated *ai.KnowledgeAggregate
}

func newAIView(enabled bool, client *ai.Client, worker *ai.Worker, genModel, embedModel string, db *index.DB, sessions []*model.Session) aiView {
	v := aiView{
		enabled:    enabled,
		client:     client,
		worker:     worker,
		genModel:   genModel,
		embedModel: embedModel,
		db:         db,
		sessions:   sessions,
		caches:     map[string]*index.AICache{},
	}
	if db != nil {
		all, _ := db.AICacheList()
		for _, c := range all {
			v.caches[c.SessionID] = c
		}
		v.insights, _ = db.InsightsList()
		v.profile, _, _ = db.ProfileGet()
		v.knowledge = map[string]*index.Knowledge{}
		if ks, err := db.KnowledgeList(); err == nil {
			for _, k := range ks {
				v.knowledge[k.SessionID] = k
			}
		}
		v.aggregated, _ = ai.AggregateKnowledge(db)
	}
	return v
}

func (v aiView) View(width int, selected *model.Session) string {
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	var b strings.Builder

	// Header status
	if !v.enabled {
		fmt.Fprintln(&b, header.Render("🤖 AI desabilitada"))
		fmt.Fprintln(&b, muted.Render("Edite ~/.claude-history/config.toml [ai] enabled = true,"))
		fmt.Fprintln(&b, muted.Render("ou rode sem --no-ai pra ativar."))
		return lipgloss.NewStyle().Width(width).Padding(1, 2).Render(b.String())
	}

	reachable := false
	if v.client != nil {
		reachable = v.client.Health(context.Background())
	}
	cached := 0
	for _, c := range v.caches {
		if c.Summary != "" {
			cached++
		}
	}
	statusStr := "❌"
	if reachable {
		statusStr = "✓"
	}
	queued := 0
	if v.worker != nil {
		queued = v.worker.QueuedCount()
	}
	fmt.Fprintln(&b, header.Render(fmt.Sprintf("🤖 Ollama %s · %s · %d/%d cached · queue: %d",
		statusStr, v.genModel, cached, len(v.sessions), queued)))
	if !reachable {
		fmt.Fprintln(&b, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(
			"Ollama não responde. Rode `ollama serve` e baixe os modelos:"))
		fmt.Fprintln(&b, muted.Render(fmt.Sprintf("  ollama pull %s", v.genModel)))
		fmt.Fprintln(&b, muted.Render(fmt.Sprintf("  ollama pull %s", v.embedModel)))
		return lipgloss.NewStyle().Width(width).Padding(1, 2).Render(b.String())
	}
	b.WriteByte('\n')

	// Clusters
	fmt.Fprintln(&b, header.Render("🗂  Clusters temáticos"))
	clusterMap := map[int]*ai.ClusterInfo{}
	for _, c := range v.caches {
		if c.TopicCluster < 0 {
			continue
		}
		ci, ok := clusterMap[c.TopicCluster]
		if !ok {
			ci = &ai.ClusterInfo{ClusterID: c.TopicCluster, Label: c.TopicLabel}
			clusterMap[c.TopicCluster] = ci
		}
		ci.SessionIDs = append(ci.SessionIDs, c.SessionID)
	}
	if len(clusterMap) == 0 {
		fmt.Fprintln(&b, muted.Render("  (clusters não computados — POST /api/ai/clusters/recompute pra rodar)"))
	} else {
		clusters := make([]ai.ClusterInfo, 0, len(clusterMap))
		for _, ci := range clusterMap {
			clusters = append(clusters, *ci)
		}
		sort.Slice(clusters, func(i, j int) bool {
			if len(clusters[i].SessionIDs) != len(clusters[j].SessionIDs) {
				return len(clusters[i].SessionIDs) > len(clusters[j].SessionIDs)
			}
			return clusters[i].ClusterID < clusters[j].ClusterID
		})
		for _, ci := range clusters {
			fmt.Fprintf(&b, "  Cluster %d  [%s]  %d sessions\n", ci.ClusterID, ci.Label, len(ci.SessionIDs))
			for i, sid := range ci.SessionIDs {
				if i >= 4 {
					fmt.Fprintf(&b, "    +%d more…\n", len(ci.SessionIDs)-i)
					break
				}
				summary := "?"
				if c, ok := v.caches[sid]; ok {
					summary = c.Summary
				}
				fmt.Fprintf(&b, "    %s  %s\n", sid[:8], summary)
			}
		}
	}
	b.WriteByte('\n')

	// Similar to selected
	if selected != nil {
		fmt.Fprintln(&b, header.Render(fmt.Sprintf("🔗 Sessions similares a %s", selected.SessionID[:8])))
		results, err := ai.FindSimilar(v.db, selected.SessionID, 8)
		if err != nil || len(results) == 0 {
			fmt.Fprintln(&b, muted.Render("  (nenhuma similar — embeddings não geradas?)"))
		} else {
			for _, r := range results {
				summary := "?"
				if c, ok := v.caches[r.SessionID]; ok && c.Summary != "" {
					summary = c.Summary
				}
				fmt.Fprintf(&b, "  %.2f  %s  %s\n", r.Similarity, r.SessionID[:8], summary)
			}
		}
		b.WriteByte('\n')
	}

	// Insights
	fmt.Fprintln(&b, header.Render("💡 Insights & advisor"))
	if len(v.insights) == 0 {
		fmt.Fprintln(&b, muted.Render("  (sem insights — POST /api/ai/insights/generate pra rodar)"))
	} else {
		for _, ins := range v.insights {
			icon := insightIconTUI(ins.Type)
			fmt.Fprintf(&b, "  %s [%s] %s\n", icon, ins.Type, ins.Title)
			if ins.Description != "" {
				fmt.Fprintf(&b, "      %s\n", ins.Description)
			}
			if ins.SuggestedAction != "" {
				fmt.Fprintf(&b, "      → %s\n", ins.SuggestedAction)
			}
		}
	}
	b.WriteByte('\n')

	// Knowledge da session selecionada (Phase A)
	if selected != nil {
		if k := v.knowledge[selected.SessionID]; k != nil {
			fmt.Fprintln(&b, header.Render(fmt.Sprintf("📚 Knowledge da session %s", selected.SessionID[:8])))
			renderKnowledgeTUI(&b, k, muted)
			b.WriteByte('\n')
		}
	}

	// Knowledge agregado cross-session (Phase B)
	if v.aggregated != nil && v.aggregated.SessionsAnalyzed > 0 {
		fmt.Fprintln(&b, header.Render(fmt.Sprintf("🧬 Knowledge agregado · %d sessions",
			v.aggregated.SessionsAnalyzed)))
		renderAggregateTUI(&b, v.aggregated, muted)
		b.WriteByte('\n')
	}

	// Profile
	fmt.Fprintln(&b, header.Render("🧠 Personal profile"))
	if v.profile == "" {
		fmt.Fprintln(&b, muted.Render("  (sem profile — POST /api/ai/profile/generate pra gerar)"))
	} else {
		// imprime line-wrapped
		lines := strings.Split(v.profile, "\n")
		for _, l := range lines {
			fmt.Fprintf(&b, "  %s\n", l)
		}
	}
	b.WriteByte('\n')

	// Recent summaries
	fmt.Fprintln(&b, header.Render("📋 Recent summaries (top 10)"))
	type entry struct {
		sid       string
		summary   string
		generated int64
	}
	entries := make([]entry, 0, len(v.caches))
	for _, c := range v.caches {
		if c.Summary == "" {
			continue
		}
		entries = append(entries, entry{c.SessionID, c.Summary, c.GeneratedAt})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].generated > entries[j].generated })
	if len(entries) > 10 {
		entries = entries[:10]
	}
	for _, e := range entries {
		ts := time.Unix(e.generated, 0).Format("15:04")
		fmt.Fprintf(&b, "  %s  %s  %s\n", ts, e.sid[:8], e.summary)
	}

	return lipgloss.NewStyle().Width(width).Padding(1, 2).Render(b.String())
}

// renderKnowledgeTUI imprime o card de Knowledge de uma session: problem,
// solution, decisões, learnings, code_patterns, tech_used, open_questions.
// Cada bloco só aparece se tiver conteúdo.
func renderKnowledgeTUI(b *strings.Builder, k *index.Knowledge, muted lipgloss.Style) {
	label := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	if k.Problem != "" {
		fmt.Fprintf(b, "  %s %s\n", label.Render("🎯 problem:"), k.Problem)
	}
	if k.Solution != "" {
		fmt.Fprintf(b, "  %s %s\n", label.Render("✅ solution:"), k.Solution)
	}
	type rawDec struct {
		Decision  string `json:"decision"`
		Rationale string `json:"rationale"`
	}
	var decisions []rawDec
	_ = unmarshalJSONIgnore(k.Decisions, &decisions)
	if len(decisions) > 0 {
		fmt.Fprintf(b, "  %s\n", label.Render("⚖️  decisions:"))
		for _, d := range decisions {
			fmt.Fprintf(b, "    · %s", d.Decision)
			if d.Rationale != "" {
				fmt.Fprintf(b, "\n      %s", muted.Render(d.Rationale))
			}
			b.WriteByte('\n')
		}
	}
	var learnings, patterns, tech, open []string
	_ = unmarshalJSONIgnore(k.Learnings, &learnings)
	_ = unmarshalJSONIgnore(k.CodePatterns, &patterns)
	_ = unmarshalJSONIgnore(k.TechUsed, &tech)
	_ = unmarshalJSONIgnore(k.OpenQuestions, &open)
	if len(learnings) > 0 {
		fmt.Fprintf(b, "  %s\n", label.Render("💡 learnings:"))
		for _, l := range learnings {
			fmt.Fprintf(b, "    · %s\n", l)
		}
	}
	if len(patterns) > 0 {
		fmt.Fprintf(b, "  %s\n", label.Render("⚙️  patterns:"))
		for _, p := range patterns {
			fmt.Fprintf(b, "    · %s\n", p)
		}
	}
	if len(tech) > 0 {
		fmt.Fprintf(b, "  %s %s\n", label.Render("🔧 tech:"), strings.Join(tech, ", "))
	}
	if len(open) > 0 {
		fmt.Fprintf(b, "  %s\n", label.Render("❓ em aberto:"))
		for _, q := range open {
			fmt.Fprintf(b, "    · %s\n", q)
		}
	}
}

// renderAggregateTUI imprime as 5 visões cross-session compactamente.
func renderAggregateTUI(b *strings.Builder, agg *ai.KnowledgeAggregate, muted lipgloss.Style) {
	label := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	if len(agg.TechFrequency) > 0 {
		fmt.Fprintf(b, "  %s ", label.Render("🔧 tech:"))
		var parts []string
		for i, t := range agg.TechFrequency {
			if i >= 10 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s·%d", t.Name, t.Count))
		}
		fmt.Fprintln(b, strings.Join(parts, "  "))
	}

	if len(agg.TopPatterns) > 0 {
		fmt.Fprintf(b, "  %s\n", label.Render("⚙️  top patterns:"))
		for i, p := range agg.TopPatterns {
			if i >= 8 {
				fmt.Fprintf(b, "    %s\n", muted.Render(fmt.Sprintf("+%d more…", len(agg.TopPatterns)-i)))
				break
			}
			fmt.Fprintf(b, "    ×%-2d %s\n", p.Count, p.Pattern)
		}
	}

	if len(agg.RecurringProblems) > 0 {
		fmt.Fprintf(b, "  %s\n", label.Render("🔁 problemas recorrentes:"))
		for i, c := range agg.RecurringProblems {
			if i >= 5 {
				break
			}
			rep := c.Representative
			if len(rep) > 80 {
				rep = rep[:80] + "…"
			}
			fmt.Fprintf(b, "    ×%-2d %s\n", c.Count, rep)
			if len(c.Keywords) > 0 {
				fmt.Fprintf(b, "        %s\n", muted.Render("keywords: "+strings.Join(c.Keywords, ", ")))
			}
		}
	}

	if len(agg.DecisionHistory) > 0 {
		fmt.Fprintf(b, "  %s\n", label.Render("⚖️  últimas decisões:"))
		for i, d := range agg.DecisionHistory {
			if i >= 8 {
				fmt.Fprintf(b, "    %s\n", muted.Render(fmt.Sprintf("+%d more…", len(agg.DecisionHistory)-i)))
				break
			}
			when := time.Unix(d.GeneratedAt, 0).Format("Jan 02")
			dec := d.Decision
			if len(dec) > 70 {
				dec = dec[:70] + "…"
			}
			fmt.Fprintf(b, "    %s %s  %s\n", muted.Render(when), dec, muted.Render(d.SessionID[:8]))
		}
	}

	if len(agg.OpenQuestions) > 0 {
		fmt.Fprintf(b, "  %s\n", label.Render("❓ em aberto:"))
		for i, q := range agg.OpenQuestions {
			if i >= 8 {
				fmt.Fprintf(b, "    %s\n", muted.Render(fmt.Sprintf("+%d more…", len(agg.OpenQuestions)-i)))
				break
			}
			ageStr := "hoje"
			if q.AgeDays > 0 {
				ageStr = fmt.Sprintf("%dd", q.AgeDays)
			}
			ageStyle := muted
			if q.AgeDays > 14 {
				ageStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
			} else if q.AgeDays > 7 {
				ageStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
			}
			question := q.Question
			if len(question) > 70 {
				question = question[:70] + "…"
			}
			fmt.Fprintf(b, "    %s  %s\n", ageStyle.Render(fmt.Sprintf("%4s", ageStr)), question)
		}
	}
}

// unmarshalJSONIgnore ignora erros — JSON malformado vira slice vazia.
func unmarshalJSONIgnore(s string, dst any) error {
	if s == "" || s == "null" {
		return nil
	}
	return jsonUnmarshalSafe([]byte(s), dst)
}

func jsonUnmarshalSafe(data []byte, v any) error {
	return jsonUnmarshalImpl(data, v)
}

// Indireção pra evitar import "encoding/json" no top do arquivo (já está,
// mas mantém o helper isolado pra clareza).
func jsonUnmarshalImpl(data []byte, v any) error {
	return jsonUnmarshalDirect(data, v)
}

func insightIconTUI(t string) string {
	switch t {
	case "repeated_task":
		return "🔁"
	case "chronic_problem":
		return "⚠️"
	case "script_opportunity":
		return "🚀"
	case "token_waste":
		return "💸"
	case "performance_hint":
		return "⚡"
	case "anti_pattern":
		return "🚫"
	case "personal_pattern":
		return "🎯"
	}
	return "💡"
}

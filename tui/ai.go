package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/ai"
	"github.com/felipeness/claude-history/internal/index"
	"github.com/felipeness/claude-history/internal/model"
)

type aiView struct {
	enabled    bool
	client     *ai.Client
	worker     *ai.Worker
	genModel   string
	embedModel string
	db         *index.DB
	sessions   []*model.Session
	caches     map[string]*index.AICache
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

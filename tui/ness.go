package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/felipeness/claude-history/internal/ai"
	"github.com/felipeness/claude-history/internal/index"
)

// nessView é o tab "Ness IA" — chat com RAG sobre as sessions. Mantém
// histórico em memória (por execução do TUI) — ao sair, conversa some.
type nessView struct {
	enabled    bool
	client     *ai.Client
	db         *index.DB
	genModel   string
	embedModel string
	input      textinput.Model
	history    []nessTurn
	loading    bool
	errMsg     string
}

type nessTurn struct {
	role    string // "user" | "assistant"
	content string
	sources []ai.ChatSource
}

func newNessView(enabled bool, client *ai.Client, db *index.DB, genModel, embedModel string) nessView {
	ti := textinput.New()
	ti.Placeholder = "pergunta pro Ness — ex: 'como resolvi auth bug?'"
	ti.Focus()
	ti.CharLimit = 500
	return nessView{
		enabled:    enabled,
		client:     client,
		db:         db,
		genModel:   genModel,
		embedModel: embedModel,
		input:      ti,
	}
}

// SubmitCmd é uma tea.Cmd que dispara o chat com a query atual da input.
// Limpa input + marca loading. Quando termina, envia nessChatDoneMsg.
func (v *nessView) SubmitCmd() func() any {
	q := strings.TrimSpace(v.input.Value())
	if q == "" || v.loading || !v.enabled || v.client == nil {
		return nil
	}
	v.history = append(v.history, nessTurn{role: "user", content: q})
	v.input.SetValue("")
	v.loading = true
	v.errMsg = ""

	// Snapshot da conversa pra mandar pro backend (sem alterar v dentro do goroutine)
	msgs := make([]ai.ChatMsg, 0, len(v.history))
	for _, t := range v.history {
		msgs = append(msgs, ai.ChatMsg{Role: t.role, Content: t.content})
	}

	client := v.client
	db := v.db
	gen := v.genModel
	emb := v.embedModel
	return func() any {
		resp, err := ai.ChatWithContext(context.Background(), db, client, gen, emb, msgs)
		return nessChatDoneMsg{resp: resp, err: err}
	}
}

// nessChatDoneMsg dispara quando ai.ChatWithContext termina.
type nessChatDoneMsg struct {
	resp *ai.ChatResponse
	err  error
}

// HandleDone aplica o resultado da chamada async — adiciona resposta no
// history (ou erro), limpa loading.
func (v *nessView) HandleDone(msg nessChatDoneMsg) {
	v.loading = false
	if msg.err != nil {
		v.errMsg = msg.err.Error()
		return
	}
	if msg.resp == nil {
		return
	}
	v.history = append(v.history, nessTurn{
		role:    "assistant",
		content: msg.resp.Response,
		sources: msg.resp.Sources,
	})
}

// Clear reseta a conversa.
func (v *nessView) Clear() {
	v.history = nil
	v.errMsg = ""
}

func (v nessView) View(width, height int) string {
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	var b strings.Builder

	if !v.enabled {
		fmt.Fprintln(&b, header.Render("🧠 Ness IA — desabilitada"))
		fmt.Fprintln(&b, muted.Render("AI desativada. Edite ~/.claude-history/config.toml [ai] enabled = true"))
		return lipgloss.NewStyle().Width(width).Padding(1, 2).Render(b.String())
	}

	fmt.Fprintln(&b, header.Render("🧠 Ness IA"))
	fmt.Fprintln(&b, muted.Render("chat com seu segundo cérebro · RAG sobre suas sessions com knowledge"))
	b.WriteByte('\n')

	if len(v.history) == 0 && !v.loading {
		fmt.Fprintln(&b, muted.Render("Pergunta qualquer coisa sobre seu histórico."))
		fmt.Fprintln(&b, muted.Render("Exemplos:"))
		fmt.Fprintln(&b, muted.Render("  · 'como resolvi auth bug 3 meses atrás?'"))
		fmt.Fprintln(&b, muted.Render("  · 'qual padrao eu uso pra error handling em Go?'"))
		fmt.Fprintln(&b, muted.Render("  · 'tem algo em aberto que eu deveria fechar?'"))
		fmt.Fprintln(&b, muted.Render(""))
		fmt.Fprintln(&b, muted.Render("⚠ Pra Ness funcionar bem, gere knowledge das sessions: tab AI → ctrl+k"))
		b.WriteByte('\n')
	}

	// Renderiza turnos
	you := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	ness := lipgloss.NewStyle().Foreground(lipgloss.Color("#a5b4fc")).Bold(true)
	for _, t := range v.history {
		if t.role == "user" {
			fmt.Fprintln(&b, you.Render("você:"))
		} else {
			fmt.Fprintln(&b, ness.Render("ness:"))
		}
		// Indenta linhas com 2 espaços
		for _, line := range strings.Split(t.content, "\n") {
			fmt.Fprintln(&b, "  "+line)
		}
		if len(t.sources) > 0 {
			var pills []string
			for _, s := range t.sources {
				pill := fmt.Sprintf("[%s] %d%%", s.SessionID[:8], int(s.Similarity*100))
				pills = append(pills, pill)
			}
			fmt.Fprintln(&b, muted.Render("  fontes: "+strings.Join(pills, "  ")))
		}
		b.WriteByte('\n')
	}

	if v.loading {
		fmt.Fprintln(&b, muted.Render("⏳ Ness pensando…"))
		b.WriteByte('\n')
	}
	if v.errMsg != "" {
		fmt.Fprintln(&b, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("erro: "+v.errMsg))
		b.WriteByte('\n')
	}

	// Footer com input + atalhos
	fmt.Fprintln(&b, muted.Render(strings.Repeat("─", 60)))
	fmt.Fprintln(&b, v.input.View())
	fmt.Fprintln(&b, muted.Render("[enter] enviar  ·  [ctrl+l] limpar conversa"))

	return lipgloss.NewStyle().Width(width).Padding(1, 2).Render(b.String())
}

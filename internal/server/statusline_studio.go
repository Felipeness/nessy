package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/felipeness/claude-history/internal/statusline"
)

// Studio endpoints — alimentam o tab "Statusline Studio" do Web UI:
//   GET  /api/statusline/components — catálogo de components
//   GET  /api/statusline/themes — themes disponíveis (com cores)
//   GET  /api/statusline/config — config atual (TOML → JSON)
//   POST /api/statusline/config — salva config nova
//   POST /api/statusline/render — renderiza config + mock_input → ANSI string
//
// Justificativa de POST /render: engine de render mora em Go (single source
// of truth, mesmo código que o Claude Code chama). Studio porta a UI mas
// não duplica o renderer em JS.

func (s *Server) handleStatuslineComponents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, statusline.Metas())
}

func (s *Server) handleStatuslineThemes(w http.ResponseWriter, r *http.Request) {
	type colorOut struct {
		R uint8 `json:"r"`
		G uint8 `json:"g"`
		B uint8 `json:"b"`
	}
	type segOut struct {
		BG colorOut `json:"bg"`
		FG colorOut `json:"fg"`
	}
	type themeOut struct {
		Name    string            `json:"name"`
		Default segOut            `json:"default"`
		Segs    map[string]segOut `json:"segs"`
		Status  struct {
			OK   colorOut `json:"ok"`
			Warn colorOut `json:"warn"`
			Crit colorOut `json:"crit"`
		} `json:"status"`
		Muted colorOut `json:"muted"`
	}
	out := make([]themeOut, 0, len(statusline.ThemeNames))
	for _, name := range statusline.ThemeNames {
		t := statusline.Themes[name]
		if t == nil {
			continue
		}
		o := themeOut{
			Name: t.Name,
			Default: segOut{
				BG: colorOut{t.Default.BG.R, t.Default.BG.G, t.Default.BG.B},
				FG: colorOut{t.Default.FG.R, t.Default.FG.G, t.Default.FG.B},
			},
			Muted: colorOut{t.Muted.R, t.Muted.G, t.Muted.B},
			Segs:  map[string]segOut{},
		}
		o.Status.OK = colorOut{t.Status.OK.R, t.Status.OK.G, t.Status.OK.B}
		o.Status.Warn = colorOut{t.Status.Warn.R, t.Status.Warn.G, t.Status.Warn.B}
		o.Status.Crit = colorOut{t.Status.Crit.R, t.Status.Crit.G, t.Status.Crit.B}
		for k, v := range t.Segs {
			o.Segs[k] = segOut{
				BG: colorOut{v.BG.R, v.BG.G, v.BG.B},
				FG: colorOut{v.FG.R, v.FG.G, v.FG.B},
			}
		}
		out = append(out, o)
	}
	writeJSON(w, 200, map[string]any{
		"themes": out,
		"styles": statusline.StyleNames,
	})
}

func statuslineConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude-history", "statusline.toml")
}

func (s *Server) handleStatuslineConfig(w http.ResponseWriter, r *http.Request) {
	path := statuslineConfigPath()
	switch r.Method {
	case http.MethodGet:
		cfg, err := statusline.LoadConfig(path)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, cfg)
	case http.MethodPost:
		var cfg statusline.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeErr(w, 400, "invalid json: "+err.Error())
			return
		}
		// defaults pra campos vazios
		if cfg.Theme == "" {
			cfg.Theme = "graphite"
		}
		if cfg.Style == "" {
			cfg.Style = "plain"
		}
		if cfg.History.Endpoint == "" {
			cfg.History.Endpoint = "http://localhost:5555"
		}
		if cfg.History.Timeout == "" {
			cfg.History.Timeout = "80ms"
		}
		if err := statusline.SaveConfig(path, &cfg); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"status": "saved", "path": path})
	default:
		writeErr(w, 405, "method not allowed")
	}
}

// renderRequest body: { config: Config, mock_input: Input }. Devolve a string
// ANSI gerada — frontend converte com ansi-up pra HTML.
type renderRequest struct {
	Config    *statusline.Config `json:"config"`
	MockInput *statusline.Input  `json:"mock_input"`
}

func (s *Server) handleStatuslineRender(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST required")
		return
	}
	var req renderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid json: "+err.Error())
		return
	}
	if req.Config == nil {
		writeErr(w, 400, "config required")
		return
	}
	if req.MockInput == nil {
		req.MockInput = defaultMockInput()
	}
	// Disable history fetch durante preview — engine usa só mock data.
	// (frontend pode passar history mockado depois se quiser badges p90).
	cfg := *req.Config
	cfg.History.Endpoint = ""
	out := statusline.Render(req.MockInput, &cfg)
	writeJSON(w, 200, map[string]string{
		"ansi": out,
		"html": statusline.AnsiToHTML(out),
	})
}

func defaultMockInput() *statusline.Input {
	return &statusline.Input{
		CWD:       "/Users/dev/projects/my-app",
		SessionID: "preview-mock",
		Model: statusline.ModelInfo{
			DisplayName: "Opus 4.7",
			ID:          "claude-opus-4-7",
		},
		Workspace: statusline.Workspace{
			CurrentDir: "/Users/dev/projects/my-app",
			ProjectDir: "/Users/dev/projects/my-app",
		},
		Context: statusline.ContextWindow{
			UsedPercentage:    42,
			TotalInputTokens:  18432,
			TotalOutputTokens: 4521,
		},
		Cost: statusline.CostInfo{
			TotalCostUSD:      0.32,
			TotalLinesAdded:   45,
			TotalLinesRemoved: 12,
		},
		RateLimits: &statusline.RateLimits{
			FiveHour: &statusline.RateLimitWindow{UsedPercentage: 73},
			SevenDay: &statusline.RateLimitWindow{UsedPercentage: 18},
		},
		Worktree: &statusline.WorktreeInfo{Branch: "feat/CC-1234-statusline"},
	}
}

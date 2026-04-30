// Package statusline implementa o statusline custom do Claude Code:
// engine de render, components, themes e fetch de dados históricos
// do daemon claude-history.
package statusline

// Input espelha o JSON que Claude Code pipa via stdin pro script de
// statusline (campos opcionais via ponteiro). Schema documentado em
// https://code.claude.com/docs/en/statusline.
type Input struct {
	CWD            string `json:"cwd"`
	SessionID      string `json:"session_id"`
	SessionName    string `json:"session_name,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	Version        string `json:"version,omitempty"`
	Exceeds200K    bool   `json:"exceeds_200k_tokens,omitempty"`

	Model     ModelInfo     `json:"model"`
	Workspace Workspace     `json:"workspace"`
	Context   ContextWindow `json:"context_window"`
	Cost      CostInfo      `json:"cost"`

	RateLimits  *RateLimits   `json:"rate_limits,omitempty"`
	Vim         *VimMode      `json:"vim,omitempty"`
	OutputStyle *OutputStyle  `json:"output_style,omitempty"`
	Agent       *AgentInfo    `json:"agent,omitempty"`
	Worktree    *WorktreeInfo `json:"worktree,omitempty"`
}

type ModelInfo struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type Workspace struct {
	CurrentDir  string   `json:"current_dir,omitempty"`
	ProjectDir  string   `json:"project_dir,omitempty"`
	AddedDirs   []string `json:"added_dirs,omitempty"`
	GitWorktree string   `json:"git_worktree,omitempty"`
}

type ContextWindow struct {
	TotalInputTokens    int     `json:"total_input_tokens,omitempty"`
	TotalOutputTokens   int     `json:"total_output_tokens,omitempty"`
	ContextWindowSize   int     `json:"context_window_size,omitempty"`
	UsedPercentage      float64 `json:"used_percentage,omitempty"`
	RemainingPercentage float64 `json:"remaining_percentage,omitempty"`

	Current struct {
		InputTokens              int `json:"input_tokens,omitempty"`
		OutputTokens             int `json:"output_tokens,omitempty"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	} `json:"current_usage,omitempty"`
}

type CostInfo struct {
	TotalCostUSD       float64 `json:"total_cost_usd,omitempty"`
	TotalDurationMS    int64   `json:"total_duration_ms,omitempty"`
	TotalAPIDurationMS int64   `json:"total_api_duration_ms,omitempty"`
	TotalLinesAdded    int     `json:"total_lines_added,omitempty"`
	TotalLinesRemoved  int     `json:"total_lines_removed,omitempty"`
}

type RateLimitWindow struct {
	UsedPercentage float64 `json:"used_percentage,omitempty"`
	ResetsAt       int64   `json:"resets_at,omitempty"` // unix epoch seconds
}

type RateLimits struct {
	FiveHour *RateLimitWindow `json:"five_hour,omitempty"`
	SevenDay *RateLimitWindow `json:"seven_day,omitempty"`
}

type VimMode struct {
	Mode string `json:"mode,omitempty"` // NORMAL | INSERT
}

type OutputStyle struct {
	Name string `json:"name,omitempty"`
}

type AgentInfo struct {
	Name string `json:"name,omitempty"`
}

type WorktreeInfo struct {
	Name           string `json:"name,omitempty"`
	Path           string `json:"path,omitempty"`
	Branch         string `json:"branch,omitempty"`
	OriginalCWD    string `json:"original_cwd,omitempty"`
	OriginalBranch string `json:"original_branch,omitempty"`
}

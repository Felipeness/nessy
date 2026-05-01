// Package model contains domain types shared across parser, index, pricing, and tui.
package model

import "time"

// Session is the indexed view of one Claude Code conversation.
//
// Sidechain* fields trackeiam subagent spawns dentro da session — Claude Code
// emite turnos com isSidechain=true e um agentId quando lança um subagent
// (Task tool, Skill spawns, etc). Isso permite ver no TUI quais sessions
// tiveram heavy subagent usage e separar "trabalho da main thread" de
// "trabalho que rodou em paralelo".
type Session struct {
	SessionID           string         `json:"session_id"`
	ProjectDir          string         `json:"project_dir"`
	JSONLPath           string         `json:"jsonl_path"`
	JSONLMtime          time.Time      `json:"jsonl_mtime"`
	StartTime           time.Time      `json:"start_time"`
	EndTime             time.Time      `json:"end_time"`
	MessageCount        int            `json:"message_count"`
	UserMessages        int            `json:"user_messages"`
	AssistantMessages   int            `json:"assistant_messages"`
	FirstUserMsg        string         `json:"first_user_msg"`
	LastUserMsg         string         `json:"last_user_msg"`
	GitBranch           string         `json:"git_branch"`
	ClaudeVersion       string         `json:"claude_version"`
	Model               string         `json:"model"`
	InputTokens         int64          `json:"input_tokens"`
	OutputTokens        int64          `json:"output_tokens"`
	CacheCreationTokens int64          `json:"cache_creation_tokens"`
	CacheReadTokens     int64          `json:"cache_read_tokens"`
	ToolCalls           map[string]int `json:"tool_calls"`

	// Sidechain (subagent) tracking — agregado em parse-time.
	SidechainTurns  int `json:"sidechain_turns"`  // total de turns com isSidechain=true
	SidechainAgents int `json:"sidechain_agents"` // distinct agentIds
}

// Duration returns the session wall-clock duration.
func (s Session) Duration() time.Duration {
	return s.EndTime.Sub(s.StartTime)
}

// TotalTokens returns input+output+cache_creation+cache_read.
func (s Session) TotalTokens() int64 {
	return s.InputTokens + s.OutputTokens + s.CacheCreationTokens + s.CacheReadTokens
}

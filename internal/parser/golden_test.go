package parser

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseSession_golden(t *testing.T) {
	s, err := ParseSession(filepath.Join("testdata", "sample-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionID != "golden-1" {
		t.Errorf("SessionID = %q", s.SessionID)
	}
	if s.UserMessages != 2 {
		t.Errorf("UserMessages = %d, want 2", s.UserMessages)
	}
	if s.AssistantMessages != 1 {
		t.Errorf("AssistantMessages = %d, want 1", s.AssistantMessages)
	}
	if s.GitBranch != "main" {
		t.Errorf("GitBranch = %q", s.GitBranch)
	}
	if s.ToolCalls["Bash"] != 1 {
		t.Errorf("Bash count = %d", s.ToolCalls["Bash"])
	}
	if s.InputTokens != 100 {
		t.Errorf("InputTokens = %d", s.InputTokens)
	}
	if s.FirstUserMsg != "oi" {
		t.Errorf("FirstUserMsg = %q", s.FirstUserMsg)
	}
	if s.LastUserMsg != "obrigado" {
		t.Errorf("LastUserMsg = %q", s.LastUserMsg)
	}
	if !s.StartTime.Equal(time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("StartTime = %v", s.StartTime)
	}
}

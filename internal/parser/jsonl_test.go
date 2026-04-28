package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSession_extractsTokensAndModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	const fixture = `{"type":"user","sessionId":"abc","cwd":"/tmp","timestamp":"2026-04-28T10:00:00Z","message":{"role":"user","content":"oi"}}
{"type":"assistant","sessionId":"abc","cwd":"/tmp","timestamp":"2026-04-28T10:00:01Z","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"oi de volta"}],"usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":200,"cache_read_input_tokens":1000}}}
`
	if err := os.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}
	if s.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want claude-sonnet-4-6", s.Model)
	}
	if s.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", s.InputTokens)
	}
	if s.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", s.OutputTokens)
	}
	if s.CacheCreationTokens != 200 {
		t.Errorf("CacheCreationTokens = %d, want 200", s.CacheCreationTokens)
	}
	if s.CacheReadTokens != 1000 {
		t.Errorf("CacheReadTokens = %d, want 1000", s.CacheReadTokens)
	}
}

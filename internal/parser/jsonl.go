// Package parser reads Claude Code JSONL session files and extracts structured metadata.
//
// Claude Code stores each session as a JSONL file under
// ~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl, where each line is one
// event (user message, assistant response, tool call, etc.). We do a single
// streaming pass per file, pulling out the fields we care about for indexing.
package parser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Session is the indexed view of one Claude Code conversation.
type Session struct {
	SessionID    string         `json:"session_id"`
	ProjectDir   string         `json:"project_dir"` // decoded original cwd
	JSONLPath    string         `json:"jsonl_path"`
	StartTime    time.Time      `json:"start_time"`
	EndTime      time.Time      `json:"end_time"`
	MessageCount int            `json:"message_count"`
	UserMessages int             `json:"user_messages"`
	AssistantMsgs int           `json:"assistant_msgs"`
	FirstUserMsg string         `json:"first_user_msg"`
	LastUserMsg  string         `json:"last_user_msg"`
	GitBranch    string         `json:"git_branch"`
	ClaudeVersion string        `json:"claude_version"`
	ToolCalls    map[string]int `json:"tool_calls"`
}

// rawEvent captures only the fields we need from any line of the JSONL.
type rawEvent struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	SessionID string          `json:"sessionId"`
	CWD       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Version   string          `json:"version"`
	Message   *rawMessage     `json:"message,omitempty"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// DecodeProjectDir undoes Claude's path encoding (`/` → `-`) on the project
// folder name. Claude flattens absolute paths so `/Users/felipe.coelho/foo`
// becomes `-Users-felipe-coelho-foo`. We only restore the leading slash and
// path separators; we cannot recover hyphens that were already in the name,
// but the `cwd` field on each event holds the truth.
func DecodeProjectDir(name string) string {
	if !strings.HasPrefix(name, "-") {
		return name
	}
	return "/" + strings.ReplaceAll(name[1:], "-", "/")
}

// ParseSession reads one JSONL file and builds a Session record.
func ParseSession(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &Session{
		JSONLPath: path,
		ToolCalls: map[string]int{},
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev rawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // tolerate malformed lines
		}
		if ev.SessionID != "" && s.SessionID == "" {
			s.SessionID = ev.SessionID
		}
		if ev.CWD != "" && s.ProjectDir == "" {
			s.ProjectDir = ev.CWD
		}
		if ev.GitBranch != "" && s.GitBranch == "" {
			s.GitBranch = ev.GitBranch
		}
		if ev.Version != "" && s.ClaudeVersion == "" {
			s.ClaudeVersion = ev.Version
		}
		if t, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil {
			if s.StartTime.IsZero() || t.Before(s.StartTime) {
				s.StartTime = t
			}
			if t.After(s.EndTime) {
				s.EndTime = t
			}
		}
		switch ev.Type {
		case "user":
			s.UserMessages++
			s.MessageCount++
			if ev.Message != nil {
				text := extractText(ev.Message.Content)
				if s.FirstUserMsg == "" && text != "" {
					s.FirstUserMsg = truncate(text, 200)
				}
				if text != "" {
					s.LastUserMsg = truncate(text, 200)
				}
			}
		case "assistant":
			s.AssistantMsgs++
			s.MessageCount++
			if ev.Message != nil {
				countToolUses(ev.Message.Content, s.ToolCalls)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if s.SessionID == "" {
		// derive from filename
		s.SessionID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	if s.ProjectDir == "" {
		// derive from parent dir
		s.ProjectDir = DecodeProjectDir(filepath.Base(filepath.Dir(path)))
	}
	return s, nil
}

// extractText pulls plain text from a `message.content` field, which can be
// either a JSON string or an array of {type:"text",text:"..."} blocks.
func extractText(raw json.RawMessage) string {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	}
	return ""
}

// countToolUses walks an assistant content array and increments a counter for
// each `tool_use` block by tool name.
func countToolUses(raw json.RawMessage, into map[string]int) {
	var blocks []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return
	}
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name != "" {
			into[b.Name]++
		}
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ListSessions walks ~/.claude/projects and returns one Session per JSONL file.
// Sessions whose JSONL is unreadable or empty are skipped silently.
func ListSessions() ([]*Session, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, ".claude", "projects")
	var out []*Session
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		// skip sub-agent files: those live under <session-id>/subagents/ and
		// share the parent's sessionId, so they would just duplicate rows
		if strings.Contains(path, string(os.PathSeparator)+"subagents"+string(os.PathSeparator)) {
			return nil
		}
		s, err := ParseSession(path)
		if err != nil {
			return nil
		}
		if s.MessageCount == 0 {
			return nil
		}
		out = append(out, s)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return out, nil
}

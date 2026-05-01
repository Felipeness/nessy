// Package parser reads Claude Code JSONL session files and extracts structured metadata.
//
// Claude Code stores each session as a JSONL file under
// ~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl, where each line is one
// event (user message, assistant response, tool call, etc.). We do a single
// streaming pass per file, pulling out the fields we care about for indexing.
package parser

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/felipeness/nessy/internal/model"
)

// Session is an alias for model.Session, kept for backwards-compat with callers.
type Session = model.Session

// rawEvent captures only the fields we need from any line of the JSONL.
type rawEvent struct {
	Type        string      `json:"type"`
	Timestamp   string      `json:"timestamp"`
	SessionID   string      `json:"sessionId"`
	CWD         string      `json:"cwd"`
	GitBranch   string      `json:"gitBranch"`
	Version     string      `json:"version"`
	IsSidechain bool        `json:"isSidechain,omitempty"`
	AgentID     string      `json:"agentId,omitempty"`
	Message     *rawMessage `json:"message,omitempty"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model,omitempty"`
	Usage   *rawUsage       `json:"usage,omitempty"`
}

// Message é uma user/assistant message individual extraída do JSONL.
type Message struct {
	SessionID string
	Role      string
	Content   string
}

// LastUserMessages re-parseia o JSONL e retorna as últimas N user messages
// (ordem cronológica crescente). Útil pra preview no detail panel.
func LastUserMessages(path string, n int) ([]Message, error) {
	all, err := ParseMessages(path)
	if err != nil {
		return nil, err
	}
	var users []Message
	for _, m := range all {
		if m.Role == "user" {
			users = append(users, m)
		}
	}
	if len(users) > n {
		users = users[len(users)-n:]
	}
	return users, nil
}

// ParseMessages reads the JSONL and returns flat user/assistant messages
// for FTS indexing. Tool result blocks are excluded.
func ParseMessages(path string) ([]Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Message
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var ev rawEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != "user" && ev.Type != "assistant" {
			continue
		}
		if ev.Message == nil {
			continue
		}
		text := extractText(ev.Message.Content)
		if text == "" {
			continue
		}
		out = append(out, Message{
			SessionID: ev.SessionID,
			Role:      ev.Type,
			Content:   text,
		})
	}
	return out, scanner.Err()
}

// rawUsage matches the shape of message.usage emitted by Claude Code.
type rawUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
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
	agentSet := map[string]struct{}{} // distinct agentIds vistos
	turnIdx := 0                      // 1-based turn counter pra resolved_at_turn
	lastResolvedTurn := 0
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
		// Sidechain tracking — só conta turnos com message (skipa permission-mode etc)
		if ev.IsSidechain && ev.Message != nil {
			s.SidechainTurns++
			if ev.AgentID != "" {
				agentSet[ev.AgentID] = struct{}{}
			}
		}
		switch ev.Type {
		case "user":
			s.UserMessages++
			s.MessageCount++
			turnIdx++
			if ev.Message != nil {
				text := extractText(ev.Message.Content)
				if s.FirstUserMsg == "" && text != "" {
					s.FirstUserMsg = truncate(text, 200)
				}
				if text != "" {
					s.LastUserMsg = truncate(text, 200)
				}
				// Resolved-at-turn: última msg user com signal positivo ganha
				if !ev.IsSidechain && hasResolvedSignal(text) {
					lastResolvedTurn = turnIdx
				}
			}
		case "assistant":
			s.AssistantMessages++
			s.MessageCount++
			turnIdx++
			if ev.Message != nil {
				if ev.Message.Model != "" && s.Model == "" {
					s.Model = ev.Message.Model
				}
				if ev.Message.Usage != nil {
					s.InputTokens += ev.Message.Usage.InputTokens
					s.OutputTokens += ev.Message.Usage.OutputTokens
					s.CacheCreationTokens += ev.Message.Usage.CacheCreationInputTokens
					s.CacheReadTokens += ev.Message.Usage.CacheReadInputTokens
				}
				countToolUses(ev.Message.Content, s.ToolCalls)
			}
		}
	}
	s.ResolvedAtTurn = lastResolvedTurn
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
	s.SidechainAgents = len(agentSet)

	// Sidechains vivem em arquivos separados em <session-id>/subagents/agent-*.jsonl.
	// Scan o diretório se existir e agrega contadores.
	if extra := scanSidechainDir(path, s.SessionID); extra != nil {
		s.SidechainTurns += extra.turns
		s.SidechainAgents += extra.agents
	}
	return s, nil
}

// sidechainStats agrega métricas de subagent JSONLs separados.
type sidechainStats struct {
	turns  int
	agents int
}

// scanSidechainDir varre <session-jsonl-dir>/<session-id>/subagents/agent-*.jsonl.
// Cada arquivo é um subagent inteiro — conta as msgs e o agentId é único por arquivo.
func scanSidechainDir(sessionPath, sessionID string) *sidechainStats {
	if sessionID == "" {
		return nil
	}
	subDir := filepath.Join(filepath.Dir(sessionPath), sessionID, "subagents")
	entries, err := os.ReadDir(subDir)
	if err != nil {
		return nil
	}
	out := &sidechainStats{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(subDir, e.Name())
		out.agents++
		out.turns += countMessages(path)
	}
	return out
}

// countMessages devolve nº de events do tipo user/assistant com message != nil.
func countMessages(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	count := 0
	for scanner.Scan() {
		var ev rawEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if (ev.Type == "user" || ev.Type == "assistant") && ev.Message != nil {
			count++
		}
	}
	return count
}

// extractText pulls searchable text de um `message.content` field. Aceita:
//   - string direta: "..."
//   - array de blocks com types: "text", "tool_use", "tool_result"
//
// Pra tool_use, inclui name + input JSON serializado. Pra tool_result, inclui
// o content (que pode ser string ou array de blocks aninhado). Sem isso, ~70%
// do conteúdo das sessions ficam fora do FTS.
func extractText(raw json.RawMessage) string {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Name    string          `json:"name"`              // pra tool_use
		Input   json.RawMessage `json:"input,omitempty"`   // pra tool_use
		Content json.RawMessage `json:"content,omitempty"` // pra tool_result
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "tool_use":
			if b.Name != "" {
				parts = append(parts, "tool:"+b.Name)
			}
			if input := flattenJSON(b.Input); input != "" {
				parts = append(parts, input)
			}
		case "tool_result":
			if t := extractText(b.Content); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// flattenJSON serializa um JSON ressaltando só strings — ignora structure.
// Pra tool_use input com 50 keys aninhadas, isso vira o conteúdo de string
// concatenado. Search-friendly, sem ruído de chaves técnicas.
func flattenJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var anyVal any
	if err := json.Unmarshal(raw, &anyVal); err != nil {
		return string(raw)
	}
	var b strings.Builder
	walkStrings(anyVal, &b)
	return strings.TrimSpace(b.String())
}

func walkStrings(v any, b *strings.Builder) {
	switch x := v.(type) {
	case string:
		b.WriteString(x)
		b.WriteByte(' ')
	case []any:
		for _, e := range x {
			walkStrings(e, b)
		}
	case map[string]any:
		for _, e := range x {
			walkStrings(e, b)
		}
	}
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

// ToolEvent é um único tool_use individual, com timestamp e hash do input.
// Usado pra loop detection retroativa.
type ToolEvent struct {
	SessionID    string
	Timestamp    time.Time
	ToolName     string
	InputHash    string // SHA-256 do input JSON canonicalizado
	InputPreview string // primeiros ~100 chars do input flatten — pra UI
}

// FileOp representa um toque em arquivo (Edit/Write/Read/etc) numa session.
type FileOp struct {
	SessionID string
	FilePath  string
	OpCount   int       // somado quando o mesmo path aparece N vezes
	FirstOpAt time.Time // primeiro toque cronológico
}

// resolvedPatterns são frases positivas do user que sinalizam conclusão.
// Conservador — só palavras inequívocas. Falsos positivos seriam piores que
// falsos negativos (pra esta métrica perder uma session = OK; classificar
// uma session frustrada como resolvida = veneno na análise).
var resolvedPatterns = []string{
	"funcionou", "funciona", "funcionando",
	"perfeito", "perfeita",
	"resolvido", "resolveu",
	"obrigado", "obrigada", "valeu",
	"ficou bom", "ficou ótimo", "ficou otimo",
	"ta bom assim", "show",
	"merge ", "vou commitar", "commitei", "commitar",
	"deploy", "deployed",
}

// ParseToolEvents lê o JSONL e devolve um event por tool_use encontrado.
// Filtra subagents/ pra não duplicar.
func ParseToolEvents(path string) ([]ToolEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []ToolEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var ev rawEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != "assistant" || ev.Message == nil {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, ev.Timestamp)
		if err != nil {
			continue
		}
		var blocks []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input,omitempty"`
		}
		if err := json.Unmarshal(ev.Message.Content, &blocks); err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type != "tool_use" || b.Name == "" {
				continue
			}
			out = append(out, ToolEvent{
				SessionID:    ev.SessionID,
				Timestamp:    t,
				ToolName:     b.Name,
				InputHash:    hashToolInput(b.Input),
				InputPreview: previewInput(b.Input),
			})
		}
	}
	return out, scanner.Err()
}

// hasResolvedSignal devolve true se a msg do user tem palavras-chave positivas.
// Conservador — false negatives são preferíveis a false positives.
func hasResolvedSignal(text string) bool {
	if text == "" {
		return false
	}
	low := strings.ToLower(text)
	for _, p := range resolvedPatterns {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

// ParseFileOps varre o JSONL e devolve operações em arquivos por path.
// Extrai file_path de tool_use de Edit/Write/Read/NotebookEdit/Glob/Grep.
// Agrega: mesmo path em N tool_uses dentro de 1 session = 1 row com op_count=N.
func ParseFileOps(path string) ([]FileOp, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)

	type acc struct {
		count   int
		firstAt time.Time
	}
	byPath := map[string]*acc{}
	sessionID := ""

	for scanner.Scan() {
		var ev rawEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.SessionID != "" && sessionID == "" {
			sessionID = ev.SessionID
		}
		if ev.Type != "assistant" || ev.Message == nil {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, ev.Timestamp)
		if err != nil {
			continue
		}
		var blocks []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input,omitempty"`
		}
		if err := json.Unmarshal(ev.Message.Content, &blocks); err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type != "tool_use" {
				continue
			}
			fp := extractFilePath(b.Name, b.Input)
			if fp == "" {
				continue
			}
			a, ok := byPath[fp]
			if !ok {
				a = &acc{firstAt: t}
				byPath[fp] = a
			}
			a.count++
			if t.Before(a.firstAt) {
				a.firstAt = t
			}
		}
	}

	out := make([]FileOp, 0, len(byPath))
	for fp, a := range byPath {
		out = append(out, FileOp{
			SessionID: sessionID,
			FilePath:  fp,
			OpCount:   a.count,
			FirstOpAt: a.firstAt,
		})
	}
	return out, scanner.Err()
}

// extractFilePath descodifica o input JSON de um tool_use e devolve o path
// se o tool é file-touching (Edit/Write/Read/etc). Vazio caso contrário.
func extractFilePath(toolName string, input json.RawMessage) string {
	switch toolName {
	case "Edit", "Write", "Read", "MultiEdit":
		var inp struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal(input, &inp); err == nil {
			return inp.FilePath
		}
	case "NotebookEdit":
		var inp struct {
			NotebookPath string `json:"notebook_path"`
		}
		if err := json.Unmarshal(input, &inp); err == nil {
			return inp.NotebookPath
		}
	}
	return ""
}

// previewInput devolve uma versão truncada do input pra UI debug.
// Usa flattenJSON pra extrair só strings (sem chaves técnicas) e trunca
// em 100 chars. Single-line.
func previewInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	flat := flattenJSON(raw)
	flat = strings.ReplaceAll(flat, "\n", " ")
	flat = strings.Join(strings.Fields(flat), " ")
	if len(flat) > 100 {
		return flat[:99] + "…"
	}
	return flat
}

// hashToolInput devolve SHA-256 do input JSON canonicalizado (chaves sorted
// recursivamente). Garante que dois inputs equivalentes tenham mesmo hash
// independente da ordem de keys.
func hashToolInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "0"
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		// fallback: hash bytes brutos
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:8])
	}
	canon, _ := canonicalJSON(v)
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:8])
}

// canonicalJSON serializa v com chaves de map ordenadas alfabeticamente.
func canonicalJSON(v any) ([]byte, error) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			vb, err := canonicalJSON(x[k])
			if err != nil {
				return nil, err
			}
			b.Write(vb)
		}
		b.WriteByte('}')
		return []byte(b.String()), nil
	case []any:
		var b strings.Builder
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			eb, err := canonicalJSON(e)
			if err != nil {
				return nil, err
			}
			b.Write(eb)
		}
		b.WriteByte(']')
		return []byte(b.String()), nil
	default:
		return json.Marshal(v)
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

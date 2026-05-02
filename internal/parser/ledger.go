package parser

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// LedgerEntry é uma entrada estruturada pra renderização "ledger format"
// estilo raine — uma linha por turno (user msg, assistant text, tool_use,
// tool_result, thinking).
//
// Diferente do Message simples, mantém timestamp + role + tipo do bloco
// pra permitir rendering com truncate/cycle de tool blocks.
type LedgerEntry struct {
	Timestamp   time.Time
	Role        string // "user", "assistant", "tool_use", "tool_result", "thinking"
	Text        string // texto principal
	ToolName    string // só pra tool_use
	ToolInput   string // JSON do input pra tool_use (raw, pra truncate/full toggle)
	ParentUUID  string // pra detectar sidechains
	IsSidechain bool
	UUID        string
	AgentID     string // se isSidechain
	// Index é a ordem cronológica original. Útil pra navegação por mensagem.
	Index int
}

// ParseLedger lê o JSONL e devolve entries pronto pra render ledger.
// Diferente de ParseMessages, separa text/tool_use/tool_result/thinking
// em entries distintas pra cycling/toggle.
func ParseLedger(path string) ([]LedgerEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var out []LedgerEntry
	idx := 0

	for scanner.Scan() {
		var ev rawLedgerEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Message == nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339Nano, ev.Timestamp)

		switch ev.Type {
		case "user":
			text := extractText(ev.Message.Content)
			if text == "" {
				continue
			}
			out = append(out, LedgerEntry{
				Timestamp:   t,
				Role:        "user",
				Text:        text,
				UUID:        ev.UUID,
				ParentUUID:  ev.ParentUUID,
				IsSidechain: ev.IsSidechain,
				AgentID:     ev.AgentID,
				Index:       idx,
			})
			idx++
		case "assistant":
			// Quebra content em blocks: text → "assistant", tool_use → "tool_use",
			// thinking → "thinking", tool_result → "tool_result"
			var blocks []struct {
				Type    string          `json:"type"`
				Text    string          `json:"text"`
				Name    string          `json:"name"`
				Input   json.RawMessage `json:"input,omitempty"`
				Content json.RawMessage `json:"content,omitempty"`
				// thinking field
				Thinking string `json:"thinking,omitempty"`
			}
			if err := json.Unmarshal(ev.Message.Content, &blocks); err != nil {
				// Pode ser string direta — trata como text
				var asString string
				if err := json.Unmarshal(ev.Message.Content, &asString); err == nil && asString != "" {
					out = append(out, LedgerEntry{
						Timestamp:   t,
						Role:        "assistant",
						Text:        strings.TrimSpace(asString),
						UUID:        ev.UUID,
						ParentUUID:  ev.ParentUUID,
						IsSidechain: ev.IsSidechain,
						AgentID:     ev.AgentID,
						Index:       idx,
					})
					idx++
				}
				continue
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					if strings.TrimSpace(b.Text) == "" {
						continue
					}
					out = append(out, LedgerEntry{
						Timestamp:   t,
						Role:        "assistant",
						Text:        b.Text,
						UUID:        ev.UUID,
						ParentUUID:  ev.ParentUUID,
						IsSidechain: ev.IsSidechain,
						AgentID:     ev.AgentID,
						Index:       idx,
					})
					idx++
				case "thinking":
					if strings.TrimSpace(b.Thinking) == "" {
						continue
					}
					out = append(out, LedgerEntry{
						Timestamp:   t,
						Role:        "thinking",
						Text:        b.Thinking,
						UUID:        ev.UUID,
						IsSidechain: ev.IsSidechain,
						Index:       idx,
					})
					idx++
				case "tool_use":
					out = append(out, LedgerEntry{
						Timestamp:   t,
						Role:        "tool_use",
						ToolName:    b.Name,
						ToolInput:   string(b.Input),
						UUID:        ev.UUID,
						IsSidechain: ev.IsSidechain,
						Index:       idx,
					})
					idx++
				case "tool_result":
					text := extractText(b.Content)
					if text == "" {
						continue
					}
					out = append(out, LedgerEntry{
						Timestamp:   t,
						Role:        "tool_result",
						Text:        text,
						UUID:        ev.UUID,
						IsSidechain: ev.IsSidechain,
						Index:       idx,
					})
					idx++
				}
			}
		}
	}
	return out, scanner.Err()
}

// rawLedgerEvent inclui campos extras (uuid, parentUuid, isSidechain, agentId)
// que rawEvent comum não tem.
type rawLedgerEvent struct {
	Type        string      `json:"type"`
	Timestamp   string      `json:"timestamp"`
	UUID        string      `json:"uuid,omitempty"`
	ParentUUID  string      `json:"parentUuid,omitempty"`
	IsSidechain bool        `json:"isSidechain,omitempty"`
	AgentID     string      `json:"agentId,omitempty"`
	Message     *rawMessage `json:"message,omitempty"`
}

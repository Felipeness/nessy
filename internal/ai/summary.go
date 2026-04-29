package ai

import (
	"context"
	"encoding/binary"
	"strings"

	"github.com/felipeness/claude-history/internal/model"
	"github.com/felipeness/claude-history/internal/parser"
)

const summaryPromptPT = `Resuma a conversa abaixo entre um dev e Claude Code em UMA frase de no máximo 20 palavras, em português brasileiro, focando no que foi feito (não no que foi discutido). Não use markdown nem bullet points, só texto puro numa única linha.

CONVERSA:
%s

RESUMO (1 frase em pt-BR, máx 20 palavras):`

const transcriptCharCap = 8000

// BuildTranscript concat user/assistant msgs até atingir o cap.
func BuildTranscript(s *model.Session) string {
	if s == nil || s.JSONLPath == "" {
		return ""
	}
	msgs, err := parser.ParseMessages(s.JSONLPath)
	if err != nil || len(msgs) == 0 {
		return s.FirstUserMsg
	}
	var b strings.Builder
	for _, m := range msgs {
		if b.Len() > transcriptCharCap {
			break
		}
		b.WriteString(m.Role)
		b.WriteString(": ")
		text := m.Content
		if len(text) > 800 {
			text = text[:800] + "…"
		}
		b.WriteString(text)
		b.WriteString("\n---\n")
	}
	out := b.String()
	if len(out) > transcriptCharCap {
		out = out[:transcriptCharCap] + "\n…(truncated)"
	}
	return out
}

// GenerateSummary chama o LLM e retorna 1 frase só.
func GenerateSummary(ctx context.Context, c *Client, model string, transcript string) (string, error) {
	prompt := joinPrompt(summaryPromptPT, transcript)
	out, err := c.Generate(ctx, model, prompt)
	if err != nil {
		return "", err
	}
	// Pega só a primeira linha não-vazia, sem markdown
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.Trim(line, "\"'`")
		if line == "" {
			continue
		}
		return line, nil
	}
	return strings.TrimSpace(out), nil
}

func joinPrompt(template, body string) string {
	return strings.Replace(template, "%s", body, 1)
}

// EmbedTextFromSession constrói texto pra embedding (user msgs concat até 4000).
func EmbedTextFromSession(s *model.Session) string {
	if s == nil || s.JSONLPath == "" {
		return s.FirstUserMsg
	}
	msgs, err := parser.ParseMessages(s.JSONLPath)
	if err != nil {
		return s.FirstUserMsg
	}
	var b strings.Builder
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		if b.Len() > 4000 {
			break
		}
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	out := b.String()
	if len(out) > 4000 {
		out = out[:4000]
	}
	return out
}

// EncodeEmbedding serializa []float32 em bytes (little-endian).
func EncodeEmbedding(emb []float32) []byte {
	buf := make([]byte, 4*len(emb))
	for i, v := range emb {
		binary.LittleEndian.PutUint32(buf[i*4:], floatBits(v))
	}
	return buf
}

// DecodeEmbedding faz o inverso.
func DecodeEmbedding(blob []byte) []float32 {
	if len(blob)%4 != 0 {
		return nil
	}
	out := make([]float32, len(blob)/4)
	for i := range out {
		out[i] = bitsToFloat(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return out
}

// helpers porque Go não tem cast direto de uint32 ⇄ float32 sem unsafe ou math
func floatBits(f float32) uint32 {
	bits := f
	_ = bits
	return floatToBitsViaConv(f)
}
func bitsToFloat(b uint32) float32 {
	return bitsToFloatViaConv(b)
}

// implementadas em arquivo separado pra usar math.Float32 (evita unsafe).

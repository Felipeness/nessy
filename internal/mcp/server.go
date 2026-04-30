package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Handler é a função que processa uma tool. Recebe o JSON dos args e devolve
// texto humano-legível (que o LLM lê) ou erro.
type Handler func(ctx context.Context, args json.RawMessage) (string, error)

// Server agrega o registry de tools + estado do handshake.
type Server struct {
	name        string
	version     string
	tools       []Tool
	handlers    map[string]Handler
	initialized bool
}

// NewServer cria um server com nome+versão (aparecem no handshake).
func NewServer(name, version string) *Server {
	return &Server{
		name:     name,
		version:  version,
		handlers: map[string]Handler{},
	}
}

// Register adiciona uma tool com seu handler.
func (s *Server) Register(t Tool, h Handler) {
	s.tools = append(s.tools, t)
	s.handlers[t.Name] = h
}

// Run consome stdin linha-por-linha (cada linha é um JSON-RPC request),
// processa e escreve respostas em stdout. Bloqueia até EOF/erro fatal.
//
// Spec MCP: stdio transport usa newline-delimited JSON (NDJSON). Logs e
// debug VÃO PRA STDERR — stdout é só pra protocol.
func (s *Server) Run(ctx context.Context) error {
	in := bufio.NewReader(os.Stdin)
	out := os.Stdout
	encoder := json.NewEncoder(out)

	for {
		line, err := in.ReadBytes('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		if len(line) == 0 || line[0] == '\n' {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = encoder.Encode(errorResp(nil, -32700, "parse error: "+err.Error()))
			continue
		}

		// Notification (id ausente) → não responde
		isNotification := len(req.ID) == 0 || string(req.ID) == "null"

		resp := s.dispatch(ctx, &req)
		if isNotification {
			continue
		}
		if err := encoder.Encode(resp); err != nil {
			fmt.Fprintln(os.Stderr, "encode response:", err)
		}
	}
}

// dispatch roteia o método pro handler apropriado.
func (s *Server) dispatch(ctx context.Context, req *Request) Response {
	switch req.Method {
	case "initialize":
		s.initialized = true
		return okResp(req.ID, InitializeResult{
			ProtocolVersion: protocolVersion,
			Capabilities: map[string]any{
				"tools": map[string]any{},
			},
			ServerInfo: ServerInfo{Name: s.name, Version: s.version},
		})

	case "notifications/initialized":
		// Notification sem resposta — só consume.
		return Response{}

	case "ping":
		return okResp(req.ID, map[string]any{})

	case "tools/list":
		return okResp(req.ID, ToolsListResult{Tools: s.tools})

	case "tools/call":
		return s.handleToolCall(ctx, req)

	default:
		return errorResp(req.ID, -32601, "method not found: "+req.Method)
	}
}

// handleToolCall processa "tools/call": parsea args, chama handler, formata
// como ToolCallResult com text content.
func (s *Server) handleToolCall(ctx context.Context, req *Request) Response {
	var p ToolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResp(req.ID, -32602, "invalid params: "+err.Error())
	}
	h, ok := s.handlers[p.Name]
	if !ok {
		return errorResp(req.ID, -32601, "tool not found: "+p.Name)
	}
	text, err := h(ctx, p.Arguments)
	if err != nil {
		return okResp(req.ID, ToolCallResult{
			Content: []ToolContent{{Type: "text", Text: "Error: " + err.Error()}},
			IsError: true,
		})
	}
	return okResp(req.ID, ToolCallResult{
		Content: []ToolContent{{Type: "text", Text: text}},
	})
}

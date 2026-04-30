// Package mcp implementa o Model Context Protocol — JSON-RPC 2.0 sobre stdio
// — minimalista, sem deps externas. Cobre só os métodos que o Claude Code
// precisa pra descobrir e invocar tools: initialize, tools/list, tools/call,
// ping. Spec: https://spec.modelcontextprotocol.io/
package mcp

import "encoding/json"

// Protocol version que o MCP spec exige no handshake. Atualizar quando o
// Claude Code começar a exigir mais novo (eles aceitam negociação reversa
// retornando o mais antigo suportado).
const protocolVersion = "2024-11-05"

// Request é a JSON-RPC 2.0 request/notification. ID nil = notification.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response é a JSON-RPC 2.0 response. Result OU Error, nunca ambos.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError segue JSON-RPC 2.0. Códigos:
//   -32700 parse · -32600 invalid request · -32601 method not found
//   -32602 invalid params · -32603 internal error · -32000 server error
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ServerInfo é o que retornamos no handshake.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult é a resposta de "initialize".
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
}

// Tool é a metadata pública de cada tool. Description é o que o LLM lê pra
// decidir quando invocar — afiar essa parte é o segredo de adoção.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolsListResult é a resposta de "tools/list".
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

// ToolCallParams é o body de "tools/call".
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolContent é um bloco de output da tool. Type=text é o caso comum;
// MCP suporta image/resource também mas não usamos por enquanto.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolCallResult é a resposta de "tools/call".
type ToolCallResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// errorResp constrói uma Response de erro. ID pode ser nil pra notification
// (mas notifications não recebem resposta — quem chama não envia se for null).
func errorResp(id json.RawMessage, code int, message string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}

func okResp(id json.RawMessage, result any) Response {
	return Response{JSONRPC: "2.0", ID: id, Result: result}
}

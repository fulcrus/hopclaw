package mcp

import "encoding/json"

// ---------------------------------------------------------------------------
// Protocol constants
// ---------------------------------------------------------------------------

const (
	// ProtocolVersion is the MCP specification version implemented.
	ProtocolVersion = "2024-11-05"

	// ClientName identifies HopClaw when performing the MCP handshake.
	ClientName = "hopclaw"

	// ClientVersion is the version reported during initialization.
	ClientVersion = "0.1.0"

	// JSONRPC is the JSON-RPC version string included in every message.
	JSONRPC = "2.0"
)

// ---------------------------------------------------------------------------
// JSON-RPC error codes
// ---------------------------------------------------------------------------

const (
	// ErrCodeParse indicates an invalid JSON payload.
	ErrCodeParse = -32700

	// ErrCodeInvalidRequest indicates the request object is not valid.
	ErrCodeInvalidRequest = -32600

	// ErrCodeMethodNotFound indicates the requested method does not exist.
	ErrCodeMethodNotFound = -32601

	// ErrCodeInvalidParams indicates invalid method parameters.
	ErrCodeInvalidParams = -32602

	// ErrCodeInternal indicates a server-side error.
	ErrCodeInternal = -32603
)

// ---------------------------------------------------------------------------
// MCP method names
// ---------------------------------------------------------------------------

const (
	MethodInitialize  = "initialize"
	MethodInitialized = "notifications/initialized"
	MethodToolsList   = "tools/list"
	MethodToolsCall   = "tools/call"
)

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 types
// ---------------------------------------------------------------------------

// JSONRPCMessage represents a JSON-RPC 2.0 request, response, or notification.
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// IsResponse returns true when the message is a response (has ID and no Method).
func (m *JSONRPCMessage) IsResponse() bool {
	return m.ID != nil && m.Method == ""
}

// IsNotification returns true when the message is a notification (has Method but no ID).
func (m *JSONRPCMessage) IsNotification() bool {
	return m.Method != "" && m.ID == nil
}

// ---------------------------------------------------------------------------
// Server capabilities
// ---------------------------------------------------------------------------

// ServerCapabilities describes what the MCP server supports.
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// ToolsCapability indicates tool-related capabilities.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability indicates resource-related capabilities.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability indicates prompt-related capabilities.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ---------------------------------------------------------------------------
// Client capabilities
// ---------------------------------------------------------------------------

// ClientCapabilities describes what the MCP client supports.
type ClientCapabilities struct {
	Roots    *RootsCapability    `json:"roots,omitempty"`
	Sampling *SamplingCapability `json:"sampling,omitempty"`
}

// RootsCapability indicates the client can provide workspace roots.
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability indicates the client supports model sampling requests.
type SamplingCapability struct{}

// ---------------------------------------------------------------------------
// Initialize handshake
// ---------------------------------------------------------------------------

// Implementation identifies an MCP client or server.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeParams is sent by the client to start the handshake.
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation     `json:"clientInfo"`
}

// InitializeResult is returned by the server to complete the handshake.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
}

// ---------------------------------------------------------------------------
// Tool types
// ---------------------------------------------------------------------------

// Tool describes a single tool exposed by an MCP server.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolListResult is the response payload for tools/list.
type ToolListResult struct {
	Tools []Tool `json:"tools"`
}

// CallToolParams is the request payload for tools/call.
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CallToolResult is the response payload for tools/call.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock represents a single piece of content in a tool result.
type ContentBlock struct {
	Type     string `json:"type"`               // "text", "image", "resource"
	Text     string `json:"text,omitempty"`     // for type "text"
	MIMEType string `json:"mimeType,omitempty"` // for type "image"
	Data     string `json:"data,omitempty"`     // base64 for images
	URI      string `json:"uri,omitempty"`      // for type "resource"
}

// ---------------------------------------------------------------------------
// Server configuration
// ---------------------------------------------------------------------------

// ServerConfig describes how to spawn and connect to an external MCP server.
type ServerConfig struct {
	Name    string            `json:"name" yaml:"name"`
	Command string            `json:"command" yaml:"command"`
	Args    []string          `json:"args,omitempty" yaml:"args"`
	Env     map[string]string `json:"env,omitempty" yaml:"env"`
	WorkDir string            `json:"work_dir,omitempty" yaml:"work_dir"`
}

// ---------------------------------------------------------------------------
// Server status
// ---------------------------------------------------------------------------

// ServerStatus reports the health of a connected MCP server.
type ServerStatus struct {
	Connected bool   `json:"connected"`
	Tools     int    `json:"tools"`
	Error     string `json:"error,omitempty"`
}

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ToolProvider is implemented by the agent's tool runtime to expose tools
// via the MCP server protocol.
type ToolProvider interface {
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, args map[string]any) (*CallToolResult, error)
}

// Server implements the MCP server protocol, reading JSON-RPC requests from
// a reader and writing responses to a writer (typically stdin/stdout).
type Server struct {
	provider ToolProvider
	info     Implementation
	done     chan struct{}
}

// NewServer creates an MCP server backed by the given ToolProvider.
func NewServer(provider ToolProvider, info Implementation) *Server {
	return &Server{
		provider: provider,
		info:     info,
		done:     make(chan struct{}),
	}
}

// Serve runs the server loop, reading requests from r and writing responses
// to w until the context is cancelled or the reader reaches EOF.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	transport := NewTransport(r, w)
	defer transport.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := transport.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("receive: %w", err)
		}

		// Notifications have no ID and require no response.
		if msg.IsNotification() {
			continue
		}

		resp := s.dispatch(ctx, msg)
		if resp == nil {
			continue
		}
		if err := transport.Send(resp); err != nil {
			return fmt.Errorf("send response: %w", err)
		}
	}
}

// dispatch routes a request to the appropriate handler.
func (s *Server) dispatch(ctx context.Context, msg *JSONRPCMessage) *JSONRPCMessage {
	switch msg.Method {
	case MethodInitialize:
		return s.handleInitialize(msg.ID, msg.Params)
	case MethodToolsList:
		return s.handleToolsList(ctx, msg.ID)
	case MethodToolsCall:
		return s.handleToolsCall(ctx, msg.ID, msg.Params)
	default:
		return errorResponse(msg.ID, ErrCodeMethodNotFound, fmt.Sprintf("method %q not found", msg.Method))
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleInitialize(id any, params json.RawMessage) *JSONRPCMessage {
	var p InitializeParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			return errorResponse(id, ErrCodeInvalidParams, fmt.Sprintf("invalid initialize params: %v", err))
		}
	}

	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{},
		},
		ServerInfo: s.info,
	}
	return successResponse(id, result)
}

func (s *Server) handleToolsList(ctx context.Context, id any) *JSONRPCMessage {
	tools, err := s.provider.ListTools(ctx)
	if err != nil {
		return errorResponse(id, ErrCodeInternal, fmt.Sprintf("list tools: %v", err))
	}
	return successResponse(id, ToolListResult{Tools: tools})
}

func (s *Server) handleToolsCall(ctx context.Context, id any, params json.RawMessage) *JSONRPCMessage {
	var p CallToolParams
	if params == nil {
		return errorResponse(id, ErrCodeInvalidParams, "missing params for tools/call")
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return errorResponse(id, ErrCodeInvalidParams, fmt.Sprintf("invalid call params: %v", err))
	}
	if p.Name == "" {
		return errorResponse(id, ErrCodeInvalidParams, "tool name is required")
	}

	result, err := s.provider.CallTool(ctx, p.Name, p.Arguments)
	if err != nil {
		return errorResponse(id, ErrCodeInternal, fmt.Sprintf("call tool %q: %v", p.Name, err))
	}
	return successResponse(id, result)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func successResponse(id any, result any) *JSONRPCMessage {
	data, err := json.Marshal(result)
	if err != nil {
		return errorResponse(id, ErrCodeInternal, fmt.Sprintf("marshal result: %v", err))
	}
	return &JSONRPCMessage{
		JSONRPC: JSONRPC,
		ID:      id,
		Result:  data,
	}
}

func errorResponse(id any, code int, message string) *JSONRPCMessage {
	return &JSONRPCMessage{
		JSONRPC: JSONRPC,
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
		},
	}
}

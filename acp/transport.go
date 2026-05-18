package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

const (
	// scannerInitBuf is the initial buffer size for the NDJSON scanner.
	scannerInitBuf = 64 * 1024
)

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 framing types
// ---------------------------------------------------------------------------

// jsonrpcVersion is the JSON-RPC version string used in every message.
const jsonrpcVersion = "2.0"

// JSONRPCMessage represents a single JSON-RPC 2.0 request, response, or
// notification. The ACP package defines its own copy to avoid circular
// dependencies with other protocol packages.
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	HasID   bool            `json:"-"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError describes an error in a JSON-RPC 2.0 response.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (m JSONRPCMessage) MarshalJSON() ([]byte, error) {
	payload := map[string]any{
		"jsonrpc": m.JSONRPC,
	}
	if m.HasID {
		payload["id"] = m.ID
	}
	if m.Method != "" {
		payload["method"] = m.Method
	}
	if len(bytes.TrimSpace(m.Params)) > 0 {
		payload["params"] = json.RawMessage(m.Params)
	}
	if len(bytes.TrimSpace(m.Result)) > 0 {
		payload["result"] = json.RawMessage(m.Result)
	}
	if m.Error != nil {
		payload["error"] = m.Error
	}
	return json.Marshal(payload)
}

// Standard JSON-RPC error codes.
const (
	errCodeParse          = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternal       = -32603
)

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

// Transport provides NDJSON send/receive over a reader/writer pair.
type Transport struct {
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex // guards writer
	closed atomic.Bool
}

// NewTransport creates a Transport that reads from r and writes to w.
func NewTransport(r io.Reader, w io.Writer) *Transport {
	scanner := bufio.NewReaderSize(r, scannerInitBuf)
	return &Transport{
		reader: scanner,
		writer: w,
	}
}

// Send marshals msg as a single JSON line and writes it to the underlying
// writer. It is safe for concurrent use.
func (t *Transport) Send(msg *JSONRPCMessage) error {
	if t.closed.Load() {
		return fmt.Errorf("acp: transport is closed")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("acp: failed to marshal message: %w", err)
	}
	data = append(data, '\n')

	t.mu.Lock()
	defer t.mu.Unlock()

	_, err = t.writer.Write(data)
	if err != nil {
		return fmt.Errorf("acp: failed to write message: %w", err)
	}
	return nil
}

// Receive reads and unmarshals the next JSON-RPC message from the transport.
// It blocks until a complete line is available or the reader returns an error.
func (t *Transport) Receive() (*JSONRPCMessage, error) {
	if t.closed.Load() {
		return nil, fmt.Errorf("acp: transport is closed")
	}

	line, err := t.receiveLine()
	if err != nil {
		return nil, err
	}

	msg, perr := decodeJSONRPCMessage(line)
	if perr != nil {
		return nil, fmt.Errorf("acp: failed to decode message: %w", perr)
	}
	return msg, nil
}

// Close marks the transport as closed. Subsequent Send and Receive calls will
// return an error. Close does not close the underlying reader or writer.
func (t *Transport) Close() error {
	t.closed.Store(true)
	return nil
}

func (t *Transport) receiveLine() ([]byte, error) {
	if t.closed.Load() {
		return nil, fmt.Errorf("acp: transport is closed")
	}
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("acp: failed to read message: %w", err)
	}
	return line, nil
}

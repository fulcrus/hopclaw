package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// ---------------------------------------------------------------------------
// Buffer sizes
// ---------------------------------------------------------------------------

const (
	// scannerInitBuf is the initial buffer size for the NDJSON scanner.
	scannerInitBuf = 64 * 1024 // 64 KB

	// scannerMaxBuf is the maximum buffer size for the NDJSON scanner.
	scannerMaxBuf = 4 * 1024 * 1024 // 4 MB
)

// Transport handles NDJSON (newline-delimited JSON) communication over
// stdio-style reader/writer pairs.
type Transport struct {
	scanner *bufio.Scanner
	writer  io.Writer
	mu      sync.Mutex // guards writer
	closed  atomic.Bool
}

// NewTransport creates a Transport that reads NDJSON from r and writes to w.
func NewTransport(r io.Reader, w io.Writer) *Transport {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)
	return &Transport{
		scanner: scanner,
		writer:  w,
	}
}

// Send marshals msg as JSON and writes it followed by a newline.
// It is safe for concurrent use.
func (t *Transport) Send(msg *JSONRPCMessage) error {
	if t.closed.Load() {
		return fmt.Errorf("transport is closed")
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, err := t.writer.Write(data); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

// Receive reads the next NDJSON line and decodes it into a JSONRPCMessage.
// It blocks until a line is available or the reader is closed.
func (t *Transport) Receive() (*JSONRPCMessage, error) {
	if t.closed.Load() {
		return nil, fmt.Errorf("transport is closed")
	}
	if !t.scanner.Scan() {
		if err := t.scanner.Err(); err != nil {
			return nil, fmt.Errorf("read message: %w", err)
		}
		return nil, io.EOF
	}
	line := t.scanner.Bytes()
	var msg JSONRPCMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal message: %w", err)
	}
	return &msg, nil
}

// Close marks the transport as closed. Subsequent Send calls will fail.
// The underlying reader/writer are not closed; the caller owns their lifecycle.
func (t *Transport) Close() error {
	t.closed.Store(true)
	return nil
}

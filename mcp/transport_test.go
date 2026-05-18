package mcp

import (
	"encoding/json"
	"io"
	"testing"
)

// ---------------------------------------------------------------------------
// NewTransport
// ---------------------------------------------------------------------------

func TestNewTransport(t *testing.T) {
	t.Parallel()

	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()

	tr := NewTransport(r, w)
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
}

// ---------------------------------------------------------------------------
// Send and Receive round-trip
// ---------------------------------------------------------------------------

func TestTransportSendReceive(t *testing.T) {
	t.Parallel()

	readerForReceive, writerForSend := io.Pipe()
	defer readerForReceive.Close()
	defer writerForSend.Close()

	sender := NewTransport(nil, writerForSend)
	receiver := NewTransport(readerForReceive, nil)

	msg := &JSONRPCMessage{
		JSONRPC: JSONRPC,
		ID:      float64(1),
		Method:  "test/method",
	}

	go func() {
		if err := sender.Send(msg); err != nil {
			t.Errorf("send: %v", err)
		}
	}()

	got, err := receiver.Receive()
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if got.Method != "test/method" {
		t.Errorf("Method = %q, want test/method", got.Method)
	}
	if got.JSONRPC != JSONRPC {
		t.Errorf("JSONRPC = %q, want %q", got.JSONRPC, JSONRPC)
	}
}

// ---------------------------------------------------------------------------
// Send with params
// ---------------------------------------------------------------------------

func TestTransportSendReceive_WithParams(t *testing.T) {
	t.Parallel()

	readerForReceive, writerForSend := io.Pipe()
	defer readerForReceive.Close()
	defer writerForSend.Close()

	sender := NewTransport(nil, writerForSend)
	receiver := NewTransport(readerForReceive, nil)

	params, _ := json.Marshal(map[string]any{"key": "value"})
	msg := &JSONRPCMessage{
		JSONRPC: JSONRPC,
		ID:      float64(42),
		Method:  "tools/call",
		Params:  params,
	}

	go func() {
		if err := sender.Send(msg); err != nil {
			t.Errorf("send: %v", err)
		}
	}()

	got, err := receiver.Receive()
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if got.ID != float64(42) {
		t.Errorf("ID = %v, want 42", got.ID)
	}

	var p map[string]any
	if err := json.Unmarshal(got.Params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if p["key"] != "value" {
		t.Errorf("params[key] = %v, want value", p["key"])
	}
}

// ---------------------------------------------------------------------------
// Multiple messages
// ---------------------------------------------------------------------------

func TestTransportMultipleMessages(t *testing.T) {
	t.Parallel()

	readerForReceive, writerForSend := io.Pipe()
	defer readerForReceive.Close()
	defer writerForSend.Close()

	sender := NewTransport(nil, writerForSend)
	receiver := NewTransport(readerForReceive, nil)

	const count = 5
	go func() {
		for i := 0; i < count; i++ {
			msg := &JSONRPCMessage{
				JSONRPC: JSONRPC,
				ID:      float64(i),
				Method:  "test/sequence",
			}
			if err := sender.Send(msg); err != nil {
				t.Errorf("send %d: %v", i, err)
				return
			}
		}
	}()

	for i := 0; i < count; i++ {
		got, err := receiver.Receive()
		if err != nil {
			t.Fatalf("receive %d: %v", i, err)
		}
		if got.ID != float64(i) {
			t.Errorf("message %d: ID = %v, want %v", i, got.ID, float64(i))
		}
	}
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

func TestTransportClose_SendAfterClose(t *testing.T) {
	t.Parallel()

	_, w := io.Pipe()
	defer w.Close()

	tr := NewTransport(nil, w)
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Send after close should fail.
	err := tr.Send(&JSONRPCMessage{JSONRPC: JSONRPC, Method: "test"})
	if err == nil {
		t.Fatal("expected error on Send after Close")
	}
}

func TestTransportClose_ReceiveAfterClose(t *testing.T) {
	t.Parallel()

	r, _ := io.Pipe()
	defer r.Close()

	tr := NewTransport(r, nil)
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Receive after close should fail.
	_, err := tr.Receive()
	if err == nil {
		t.Fatal("expected error on Receive after Close")
	}
}

// ---------------------------------------------------------------------------
// Receive returns EOF on closed pipe
// ---------------------------------------------------------------------------

func TestTransportReceive_EOF(t *testing.T) {
	t.Parallel()

	r, w := io.Pipe()
	tr := NewTransport(r, nil)

	// Close the writer side so the reader gets EOF.
	w.Close()

	_, err := tr.Receive()
	if err == nil {
		t.Fatal("expected error when pipe is closed")
	}
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Receive handles invalid JSON
// ---------------------------------------------------------------------------

func TestTransportReceive_InvalidJSON(t *testing.T) {
	t.Parallel()

	r, w := io.Pipe()
	defer r.Close()

	tr := NewTransport(r, nil)

	go func() {
		_, _ = w.Write([]byte("not-valid-json\n"))
		w.Close()
	}()

	_, err := tr.Receive()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

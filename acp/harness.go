package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// harnessLineMaxSize is the maximum line size for NDJSON scanning from
	// external processes.
	harnessLineMaxSize = 1024 * 1024
	// harnessInitBuf is the initial buffer size for the NDJSON scanner.
	harnessInitBuf = 64 * 1024
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// HarnessConfig describes how to launch and communicate with an external AI
// client process (e.g. Claude Code, Codex, Gemini CLI).
type HarnessConfig struct {
	Name       string   `json:"name" yaml:"name"`       // "claude-code", "codex", "gemini-cli"
	Command    string   `json:"command" yaml:"command"` // executable path
	Args       []string `json:"args" yaml:"args"`
	Env        []string `json:"env" yaml:"env"`
	WorkDir    string   `json:"work_dir" yaml:"work_dir"`
	Persistent bool     `json:"persistent" yaml:"persistent"` // persistent vs one-shot mode
}

// ---------------------------------------------------------------------------
// ExternalHarness
// ---------------------------------------------------------------------------

// ExternalHarness manages an external AI client process as a sub-agent,
// communicating over stdin/stdout using NDJSON-framed JSON-RPC 2.0 messages.
type ExternalHarness struct {
	cfg     HarnessConfig
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex // guards stdin writes
	nextID  atomic.Int64
	done    chan struct{}
}

// StartHarness launches the external process described by cfg and sets up
// stdin/stdout NDJSON pipes for communication.
func StartHarness(ctx context.Context, cfg HarnessConfig) (*ExternalHarness, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("acp: harness command is required")
	}

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = cfg.Env
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: failed to start process %s: %w", cfg.Command, err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, harnessInitBuf), harnessLineMaxSize)

	h := &ExternalHarness{
		cfg:     cfg,
		cmd:     cmd,
		stdin:   stdin,
		scanner: scanner,
		done:    make(chan struct{}),
	}

	return h, nil
}

// Send writes a JSON-RPC request with the given method and params, then reads
// and returns the response. It is safe for concurrent use.
func (h *ExternalHarness) Send(ctx context.Context, method string, params any) (*JSONRPCMessage, error) {
	id := h.nextID.Add(1)

	var rawParams json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("acp: failed to marshal params: %w", err)
		}
		rawParams = data
	}

	msg := &JSONRPCMessage{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		HasID:   true,
		Method:  method,
		Params:  rawParams,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("acp: failed to marshal request: %w", err)
	}
	data = append(data, '\n')

	h.mu.Lock()
	_, writeErr := h.stdin.Write(data)
	h.mu.Unlock()

	if writeErr != nil {
		return nil, fmt.Errorf("acp: failed to write to process stdin: %w", writeErr)
	}

	// Read the next response line.
	return h.readResponse(ctx, id)
}

// readResponse scans lines from stdout until a response matching the given
// request ID is found, or the context is cancelled.
func (h *ExternalHarness) readResponse(ctx context.Context, requestID int64) (*JSONRPCMessage, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if !h.scanner.Scan() {
			if err := h.scanner.Err(); err != nil {
				return nil, fmt.Errorf("acp: scanner error: %w", err)
			}
			return nil, fmt.Errorf("acp: process stdout closed unexpectedly")
		}

		line := h.scanner.Bytes()
		var resp JSONRPCMessage
		if err := json.Unmarshal(line, &resp); err != nil {
			// Skip malformed lines (could be process debug output).
			log.Warn("acp: harness skipping malformed line", "error", err)
			continue
		}

		// Match response by ID.
		if resp.ID != nil {
			respID, ok := toInt64(resp.ID)
			if ok && respID == requestID {
				return &resp, nil
			}
		}
	}
}

// Prompt is a convenience method that sends an "acp/prompt" request with the
// given message and extracts the response text from the result.
func (h *ExternalHarness) Prompt(ctx context.Context, message string) (string, error) {
	params := PromptParams{
		SessionID: "harness",
		Message:   message,
	}

	resp, err := h.Send(ctx, "acp/prompt", params)
	if err != nil {
		return "", fmt.Errorf("acp: prompt failed: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("acp: prompt error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	// Extract text from the result payload.
	var result struct {
		Text string `json:"text"`
	}
	if resp.Result != nil {
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return "", fmt.Errorf("acp: failed to unmarshal prompt result: %w", err)
		}
	}
	return result.Text, nil
}

// Stop closes stdin to signal the external process to exit, then waits for
// it to terminate.
func (h *ExternalHarness) Stop() error {
	select {
	case <-h.done:
		return nil
	default:
	}

	if err := h.stdin.Close(); err != nil {
		log.Warn("acp: failed to close harness stdin", "error", err)
	}

	err := h.cmd.Wait()
	close(h.done)
	return err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// toInt64 attempts to convert a JSON-unmarshalled ID (float64 or int) to int64.
func toInt64(v any) (int64, bool) {
	switch id := v.(type) {
	case float64:
		return int64(id), true
	case int64:
		return id, true
	case int:
		return int64(id), true
	case json.Number:
		n, err := id.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

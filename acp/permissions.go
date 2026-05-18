package acp

import (
	"context"
	"fmt"
	"sync"

	"crypto/rand"
	"encoding/hex"
)

const (
	// permissionRequestIDLen is the byte length of generated request IDs
	// (hex-encoded to 2x characters).
	permissionRequestIDLen = 16
)

// safeTools is the set of tool names that never require client approval.
var safeTools = map[string]bool{
	"fs.read":      true,
	"fs.list":      true,
	"fs.tree":      true,
	"fs.find":      true,
	"fs.grep":      true,
	"fs.stat":      true,
	"fs.hash":      true,
	"env.probe":    true,
	"env.info":     true,
	"env.get":      true,
	"text.count":   true,
	"text.extract": true,
	"skill.list":   true,
	"net.dns":      true,
}

// PermissionHandler manages the request/response lifecycle for tool execution
// approvals.
type PermissionHandler struct {
	transport *Transport
	mu        sync.Mutex           // guards pending
	pending   map[string]chan bool // requestID -> response channel
}

// NewPermissionHandler creates a PermissionHandler that sends requests over
// the provided transport.
func NewPermissionHandler(transport *Transport) *PermissionHandler {
	return &PermissionHandler{
		transport: transport,
		pending:   make(map[string]chan bool),
	}
}

// RequestPermission sends a permission request to the client and blocks until
// the client responds or the context expires.
func (h *PermissionHandler) RequestPermission(ctx context.Context, sessionID, toolName, description, input string) (bool, error) {
	reqID, err := generateRequestID()
	if err != nil {
		return false, fmt.Errorf("acp: failed to generate request id: %w", err)
	}

	ch := make(chan bool, 1)
	h.mu.Lock()
	h.pending[reqID] = ch
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pending, reqID)
		h.mu.Unlock()
	}()

	req := PermissionRequest{
		RequestID:   reqID,
		SessionID:   sessionID,
		ToolName:    toolName,
		Description: description,
		Input:       input,
	}
	if err := sendNotification(h.transport, "acp/permissionRequest", req); err != nil {
		return false, fmt.Errorf("acp: failed to send permission request: %w", err)
	}

	select {
	case <-ctx.Done():
		return false, fmt.Errorf("acp: permission request timed out: %w", ctx.Err())
	case approved := <-ch:
		return approved, nil
	}
}

// HandleResponse routes an incoming permission response to the waiting
// RequestPermission call.
func (h *PermissionHandler) HandleResponse(resp PermissionResponse) {
	h.mu.Lock()
	ch, ok := h.pending[resp.RequestID]
	h.mu.Unlock()

	if ok {
		select {
		case ch <- resp.Approved:
		default:
		}
	}
}

// NeedsPermission reports whether the given tool requires client approval.
// Safe tools return false; dangerous and unknown tools return true.
func NeedsPermission(toolName string) bool {
	return !safeTools[toolName]
}

// generateRequestID returns a cryptographically random hex string.
func generateRequestID() (string, error) {
	b := make([]byte, permissionRequestIDLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("acp: failed to read random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

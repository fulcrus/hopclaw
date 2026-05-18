package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	clientTimeout  = 10 * time.Second
	authHeaderName = "X-HopClaw-Token"

	// Run status values (mirror agent.RunStatus for API responses)
	runStatusFailed    = "failed"
	runStatusCancelled = "cancelled"
	runStatusCompleted = "completed"
)

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// Client wraps HTTP calls to the HopClaw gateway for the TUI.
type Client struct {
	baseURL   string
	authToken string
	http      *http.Client
}

// NewClient returns a Client configured for the given gateway address and auth token.
func NewClient(baseURL, authToken string) *Client {
	return &Client{
		baseURL:   baseURL,
		authToken: authToken,
		http:      &http.Client{Timeout: clientTimeout},
	}
}

// ---------------------------------------------------------------------------
// API response types
// ---------------------------------------------------------------------------

// StatusResponse is the shape of GET /operator/status.
type StatusResponse struct {
	OK              bool   `json:"ok"`
	Version         string `json:"version"`
	Uptime          string `json:"uptime"`
	CapabilityCount int    `json:"capability_count"`
}

// CapabilitiesResponse is the shape of GET /operator/capabilities.
type CapabilitiesResponse struct {
	Items []CapabilityItem `json:"items"`
	Count int              `json:"count"`
}

// CapabilityItem represents one capability in the list.
type CapabilityItem struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Model   string `json:"model,omitempty"`
	Channel string `json:"channel,omitempty"`
}

// SessionsResponse is the shape of GET /runtime/sessions.
type SessionsResponse struct {
	Items []SessionItem `json:"items"`
	Count int           `json:"count"`
}

// SessionItem represents one session summary.
type SessionItem struct {
	ID           string    `json:"id"`
	Key          string    `json:"key"`
	Model        string    `json:"model"`
	MessageCount int       `json:"message_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// EventsResponse is the shape of GET /runtime/events.
type EventsResponse struct {
	Items []EventItem `json:"items"`
}

// EventItem represents a single event.
type EventItem struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	RunID     string         `json:"run_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Time      time.Time      `json:"time"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

// SubmitRequest is the body for POST /runtime/runs.
type SubmitRequest struct {
	SessionKey string `json:"session_key"`
	Content    string `json:"content"`
}

// RunResponse is the shape of a run returned by the API.
type RunResponse struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Phase     string `json:"phase"`
	Error     string `json:"error,omitempty"`
}

// SessionDetailResponse is the shape of GET /runtime/sessions/{id}.
type SessionDetailResponse struct {
	ID       string           `json:"id"`
	Key      string           `json:"key"`
	Messages []MessageContent `json:"messages,omitempty"`
}

// MessageContent is a single message in a session.
type MessageContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ApprovalTicket represents an approval ticket from the gateway.
type ApprovalTicket struct {
	ID         string            `json:"id"`
	SessionID  string            `json:"session_id"`
	RunID      string            `json:"run_id"`
	Status     string            `json:"status"`
	ToolCalls  []ToolCallSummary `json:"tool_calls"`
	CreatedAt  time.Time         `json:"created_at"`
	ResolvedAt *time.Time        `json:"resolved_at,omitempty"`
	ResolvedBy string            `json:"resolved_by,omitempty"`
	Note       string            `json:"note,omitempty"`
}

// ToolCallSummary is a brief description of a tool call in an approval ticket.
type ToolCallSummary struct {
	Name  string `json:"name"`
	Input string `json:"input,omitempty"`
}

// ApprovalsResponse is the shape of GET /operator/approvals.
type ApprovalsResponse struct {
	Items []ApprovalTicket `json:"items"`
	Count int              `json:"count"`
}

// resolveApprovalRequest is the body for POST /operator/approvals/{id}/resolve.
type resolveApprovalRequest struct {
	Status string `json:"status"`
	Scope  string `json:"scope,omitempty"`
	Note   string `json:"note,omitempty"`
	By     string `json:"by,omitempty"`
}

// RunItem represents a run in the run list.
type RunItem struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Status    string    `json:"status"`
	Phase     string    `json:"phase,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// RunsResponse is the shape of GET /runtime/runs.
type RunsResponse struct {
	Items []RunItem `json:"items"`
	Count int       `json:"count"`
}

// CreateSessionRequest is the body for POST /runtime/runs (session creation
// is implicit via submitting with a new session key).
type CreateSessionRequest struct {
	SessionKey string `json:"session_key"`
	Content    string `json:"content"`
	Model      string `json:"model,omitempty"`
}

// ---------------------------------------------------------------------------
// API methods
// ---------------------------------------------------------------------------

// GetStatus fetches GET /operator/status.
func (c *Client) GetStatus(ctx context.Context) (StatusResponse, error) {
	var resp StatusResponse
	err := c.get(ctx, "/operator/status", &resp)
	return resp, err
}

// GetCapabilities fetches GET /operator/capabilities.
func (c *Client) GetCapabilities(ctx context.Context) (CapabilitiesResponse, error) {
	var resp CapabilitiesResponse
	err := c.get(ctx, "/operator/capabilities", &resp)
	return resp, err
}

// GetSessions fetches GET /runtime/sessions.
func (c *Client) GetSessions(ctx context.Context) (SessionsResponse, error) {
	var resp SessionsResponse
	err := c.get(ctx, "/runtime/sessions", &resp)
	return resp, err
}

// GetEvents fetches GET /runtime/events.
func (c *Client) GetEvents(ctx context.Context) (EventsResponse, error) {
	var resp EventsResponse
	err := c.get(ctx, "/runtime/events", &resp)
	return resp, err
}

// SubmitMessage posts a message via POST /runtime/runs.
func (c *Client) SubmitMessage(ctx context.Context, sessionKey, content string) (RunResponse, error) {
	var resp RunResponse
	body := SubmitRequest{
		SessionKey: sessionKey,
		Content:    content,
	}
	err := c.post(ctx, "/runtime/runs", body, &resp)
	return resp, err
}

// GetRun fetches GET /runtime/runs/{id}.
func (c *Client) GetRun(ctx context.Context, id string) (RunResponse, error) {
	var resp RunResponse
	err := c.get(ctx, "/runtime/runs/"+id, &resp)
	return resp, err
}

// GetSession fetches GET /runtime/sessions/{id}.
func (c *Client) GetSession(ctx context.Context, id string) (SessionDetailResponse, error) {
	var resp SessionDetailResponse
	err := c.get(ctx, "/runtime/sessions/"+id, &resp)
	return resp, err
}

// GetApprovals fetches GET /operator/approvals?status={status}.
func (c *Client) GetApprovals(ctx context.Context, status string) (ApprovalsResponse, error) {
	var resp ApprovalsResponse
	path := "/operator/approvals"
	if status != "" {
		path += "?status=" + status
	}
	err := c.get(ctx, path, &resp)
	return resp, err
}

// ResolveApproval resolves an approval ticket via POST /operator/approvals/{id}/resolve.
func (c *Client) ResolveApproval(ctx context.Context, id, status, scope, note string) error {
	body := resolveApprovalRequest{
		Status: status,
		Scope:  scope,
		Note:   note,
		By:     "tui-operator",
	}
	return c.post(ctx, "/operator/approvals/"+id+"/resolve", body, nil)
}

// CancelApproval cancels an approval ticket via POST /operator/approvals/{id}/cancel.
func (c *Client) CancelApproval(ctx context.Context, id string) error {
	return c.post(ctx, "/operator/approvals/"+id+"/cancel", nil, nil)
}

// GetRuns fetches GET /runtime/runs.
func (c *Client) GetRuns(ctx context.Context) (RunsResponse, error) {
	var resp RunsResponse
	err := c.get(ctx, "/runtime/runs", &resp)
	return resp, err
}

// CreateSession creates a new session by submitting a message with the given
// session key. The server will create the session if it does not exist.
func (c *Client) CreateSession(ctx context.Context, sessionKey, model string) (RunResponse, error) {
	var resp RunResponse
	body := CreateSessionRequest{
		SessionKey: sessionKey,
		Content:    "(session created from TUI)",
		Model:      model,
	}
	err := c.post(ctx, "/runtime/runs", body, &resp)
	return resp, err
}

// DeleteSession deletes a runtime session through the canonical runtime API.
func (c *Client) DeleteSession(ctx context.Context, id string) error {
	return c.delete(ctx, "/runtime/sessions/"+id)
}

// ---------------------------------------------------------------------------
// Internal HTTP helpers
// ---------------------------------------------------------------------------

func (c *Client) get(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	return c.doJSON(req, target)
}

func (c *Client) post(ctx context.Context, path string, body any, target any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.doJSON(req, target)
}

func (c *Client) delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.url(path), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	return c.doJSON(req, nil)
}

func (c *Client) url(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.baseURL + path
}

func (c *Client) doJSON(req *http.Request, target any) error {
	if c.authToken != "" {
		req.Header.Set(authHeaderName, c.authToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("gateway error (HTTP %d): %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("gateway error (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if target != nil {
		if err := json.Unmarshal(body, target); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

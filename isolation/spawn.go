package isolation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("isolation")

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// childStatusRunning indicates the child session is still executing.
	childStatusRunning = "running"

	// childStatusCompleted indicates the child session finished successfully.
	childStatusCompleted = "completed"

	// childStatusFailed indicates the child session finished with an error.
	childStatusFailed = "failed"

	// waitPollInterval is the polling interval for WaitAll.
	waitPollInterval = 250 * time.Millisecond
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ChildSession represents a sub-agent session spawned from a parent session.
type ChildSession struct {
	ID          string    `json:"id"`
	ParentID    string    `json:"parent_id"`
	AgentName   string    `json:"agent_name"`
	SessionKey  string    `json:"session_key"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Result      string    `json:"result,omitempty"`
	WorkspaceID string    `json:"workspace_id,omitempty"`

	pendingMessages int
}

// IsTerminal returns true if the child session has finished (completed or failed).
func (c *ChildSession) IsTerminal() bool {
	return c.Status == childStatusCompleted || c.Status == childStatusFailed
}

// SpawnRequest contains the parameters needed to spawn a child agent session.
type SpawnRequest struct {
	ParentSessionID string
	AgentName       string
	Message         string
	Workspace       *Workspace // optional isolated workspace
}

// SubmitFunc submits a message to the runtime for processing.
// It returns the resulting session key and an error.
type SubmitFunc func(ctx context.Context, sessionKey, message string) (string, error)

// ---------------------------------------------------------------------------
// Spawner
// ---------------------------------------------------------------------------

// Spawner manages child agent sessions spawned from a parent session.
type Spawner struct {
	mu       sync.RWMutex
	children map[string][]ChildSession // parent session ID -> children
	submit   SubmitFunc
}

// NewSpawner creates a Spawner that uses the given submit function to dispatch
// messages to child agent sessions.
func NewSpawner(submit SubmitFunc) *Spawner {
	return &Spawner{
		children: make(map[string][]ChildSession),
		submit:   submit,
	}
}

// Spawn creates a new child agent session for the given parent and dispatches
// the initial message asynchronously.
func (s *Spawner) Spawn(ctx context.Context, req SpawnRequest) (*ChildSession, error) {
	if req.ParentSessionID == "" {
		return nil, fmt.Errorf("parent session id is required")
	}
	if req.AgentName == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	if req.Message == "" {
		return nil, fmt.Errorf("message is required")
	}

	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate child session id: %w", err)
	}

	sessionKey := fmt.Sprintf("agent:%s:%s", req.AgentName, id)

	child := ChildSession{
		ID:         id,
		ParentID:   req.ParentSessionID,
		AgentName:  req.AgentName,
		SessionKey: sessionKey,
		Status:     childStatusRunning,
		CreatedAt:  time.Now().UTC(),
	}
	if req.Workspace != nil {
		child.WorkspaceID = req.Workspace.ID
	}

	s.mu.Lock()
	s.children[req.ParentSessionID] = append(s.children[req.ParentSessionID], child)
	s.mu.Unlock()

	go func() {
		result, submitErr := s.submit(ctx, sessionKey, req.Message)
		s.completeChild(req.ParentSessionID, id, result, submitErr)
	}()

	return &child, nil
}

// Yield returns all completed children for the given parent and removes them
// from the tracked list.
func (s *Spawner) Yield(parentID string) ([]ChildSession, error) {
	if parentID == "" {
		return nil, fmt.Errorf("parent session id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	all := s.children[parentID]
	var completed []ChildSession
	var remaining []ChildSession

	for _, child := range all {
		if child.IsTerminal() {
			completed = append(completed, child)
		} else {
			remaining = append(remaining, child)
		}
	}

	if len(remaining) == 0 {
		delete(s.children, parentID)
	} else {
		s.children[parentID] = remaining
	}

	return completed, nil
}

// ListChildren returns all children (running and completed) for the given parent.
func (s *Spawner) ListChildren(parentID string) []ChildSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := s.children[parentID]
	out := make([]ChildSession, len(all))
	copy(out, all)
	return out
}

// WaitAll blocks until all children for the given parent have completed or
// the context is cancelled. It returns the final state of all children.
func (s *Spawner) WaitAll(ctx context.Context, parentID string) ([]ChildSession, error) {
	if parentID == "" {
		return nil, fmt.Errorf("parent session id is required")
	}

	for {
		s.mu.RLock()
		all := s.children[parentID]
		allDone := true
		for _, child := range all {
			if !child.IsTerminal() {
				allDone = false
				break
			}
		}
		out := make([]ChildSession, len(all))
		copy(out, all)
		s.mu.RUnlock()

		if allDone && len(out) > 0 {
			return out, nil
		}
		if len(out) == 0 {
			return nil, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(waitPollInterval):
			// continue polling
		}
	}
}

// SendMessage dispatches a follow-up message to a running child session by
// submitting a new run on the child's session key.
func (s *Spawner) SendMessage(ctx context.Context, parentID, childID, message string) error {
	if parentID == "" {
		return fmt.Errorf("parent session id is required")
	}
	if childID == "" {
		return fmt.Errorf("child session id is required")
	}
	if message == "" {
		return fmt.Errorf("message is required")
	}

	s.mu.Lock()
	children := s.children[parentID]
	var sessionKey string
	for i := range children {
		if children[i].ID != childID {
			continue
		}
		if children[i].IsTerminal() {
			status := children[i].Status
			s.mu.Unlock()
			return fmt.Errorf("child session %s is %s", childID, status)
		}
		children[i].pendingMessages++
		sessionKey = children[i].SessionKey
		break
	}
	s.mu.Unlock()

	if sessionKey == "" {
		return fmt.Errorf("child session %s not found under parent %s", childID, parentID)
	}

	go func() {
		defer s.finishPendingMessage(parentID, childID)
		if _, err := s.submit(ctx, sessionKey, message); err != nil {
			log.Warn("submit isolation session failed", "error", err)
		}
	}()
	return nil
}

// ---------------------------------------------------------------------------
// internal
// ---------------------------------------------------------------------------

func (s *Spawner) completeChild(parentID, childID, result string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	children := s.children[parentID]
	for i := range children {
		if children[i].ID == childID {
			children[i].CompletedAt = time.Now().UTC()
			if err != nil {
				children[i].Status = childStatusFailed
				children[i].Result = err.Error()
			} else {
				children[i].Status = childStatusCompleted
				children[i].Result = result
			}
			break
		}
	}
}

func (s *Spawner) finishPendingMessage(parentID, childID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	children := s.children[parentID]
	for i := range children {
		if children[i].ID == childID {
			if children[i].pendingMessages > 0 {
				children[i].pendingMessages--
			}
			break
		}
	}
}

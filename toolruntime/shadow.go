package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

// EditShadow captures file contents before modification so they can be
// diffed against or reverted. One shadow store per session; it lives in
// memory and is released when the session ends.
type EditShadow struct {
	mu        sync.RWMutex
	originals map[string]shadowEntry // abs path → entry
}

type shadowEntry struct {
	content   []byte // nil means file was created (did not exist before)
	captureAt time.Time
}

// NewEditShadow creates an empty shadow store.
func NewEditShadow() *EditShadow {
	return &EditShadow{originals: make(map[string]shadowEntry)}
}

// Capture records the current content of path. Only the first capture per
// path is stored (like git: diff is always against HEAD, not last edit).
func (s *EditShadow) Capture(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.originals[path]; exists {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist yet → mark as created.
		s.originals[path] = shadowEntry{content: nil, captureAt: time.Now()}
		return
	}
	s.originals[path] = shadowEntry{content: data, captureAt: time.Now()}
}

// Diff returns a unified diff for a single file (original vs current).
// Returns empty string if file is unmodified or not tracked.
func (s *EditShadow) Diff(path, displayPath string) string {
	s.mu.RLock()
	entry, ok := s.originals[path]
	s.mu.RUnlock()
	if !ok {
		return ""
	}
	current, err := os.ReadFile(path)
	if err != nil {
		if entry.content == nil {
			return "" // created then deleted → no diff
		}
		// File was deleted after modification.
		return computeDiff("a/"+displayPath, "/dev/null", string(entry.content), "")
	}
	oldText := ""
	oldName := "/dev/null"
	if entry.content != nil {
		oldText = string(entry.content)
		oldName = "a/" + displayPath
	}
	return computeDiff(oldName, "b/"+displayPath, oldText, string(current))
}

// Revert restores a file to its original content. Returns the diff that
// was reverted (for display purposes). If the file was created by the
// agent, it is deleted.
func (s *EditShadow) Revert(path string) (string, error) {
	s.mu.Lock()
	entry, ok := s.originals[path]
	if !ok {
		s.mu.Unlock()
		return "", fmt.Errorf("no shadow entry for %s", path)
	}
	delete(s.originals, path)
	s.mu.Unlock()

	if entry.content == nil {
		// File was created by agent → delete it.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return "file deleted (was created by agent)", nil
	}
	if err := atomicWriteFile(path, entry.content, 0o644); err != nil {
		return "", err
	}
	return "file reverted to original", nil
}

// ChangeEntry describes a tracked file modification.
type ChangeEntry struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "modified", "created", "deleted"
}

func (s *EditShadow) Changes() []ChangeEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]ChangeEntry, 0, len(s.originals))
	for path, entry := range s.originals {
		status := "modified"
		if entry.content == nil {
			status = "created"
		} else if _, err := os.Stat(path); os.IsNotExist(err) {
			status = "deleted"
		}
		entries = append(entries, ChangeEntry{Path: path, Status: status})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries
}

// HasChanges returns true if any files have been modified.
func (s *EditShadow) HasChanges() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.originals) > 0
}

// -------------------------------------------------------------------
// ShadowMiddleware: intercepts write tools to capture pre-edit state
// -------------------------------------------------------------------

// writingTools are tool names whose execution modifies files.
var writingTools = map[string]bool{
	"fs.write":  true,
	"fs.edit":   true,
	"fs.patch":  true,
	"fs.delete": true,
	"fs.move":   true,
	"fs.append": true,
}

// ShadowExecutor wraps a ToolExecutor, capturing file state before writes.
type ShadowExecutor struct {
	inner    agent.ToolExecutor
	shadows  sync.Map // session ID → *EditShadow
}

type pathResolver interface {
	resolvePath(path string) (string, error)
	displayPath(absPath string) string
}

func WithEditShadow() ToolMiddleware {
	return func(next agent.ToolExecutor) agent.ToolExecutor {
		return &ShadowExecutor{
			inner: next,
		}
	}
}

func (e *ShadowExecutor) shadowFor(session *agent.Session) *EditShadow {
	id := ""
	if session != nil {
		id = session.ID
	}
	val, _ := e.shadows.LoadOrStore(id, NewEditShadow())
	return val.(*EditShadow)
}

func (e *ShadowExecutor) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	shadow := e.shadowFor(session)

	// Capture originals before any writes execute.
	for _, call := range calls {
		if writingTools[call.Name] {
			e.captureForCall(shadow, call)
		}
	}

	results := make([]contextengine.ToolResult, len(calls))

	delegateCalls := make([]agent.ToolCall, 0, len(calls))
	delegateIndexes := make([]int, 0, len(calls))
	for i, call := range calls {
		if isShadowToolName(call.Name) {
			continue
		}
		delegateCalls = append(delegateCalls, call)
		delegateIndexes = append(delegateIndexes, i)
	}

	if len(delegateCalls) > 0 {
		delegateResults, err := e.inner.ExecuteBatch(ctx, run, session, delegateCalls)
		for i, result := range delegateResults {
			if i >= len(delegateIndexes) {
				break
			}
			results[delegateIndexes[i]] = result
		}
		if err != nil {
			return results, err
		}
	}

	for i, call := range calls {
		if !isShadowToolName(call.Name) {
			continue
		}
		var (
			result contextengine.ToolResult
			err    error
		)
		switch call.Name {
		case "fs.diff":
			result, err = e.handleDiff(call, shadow)
		case "fs.changes":
			result, err = e.handleChanges(call, shadow)
		case "fs.revert":
			result, err = e.handleRevert(call, shadow)
		}
		if err != nil {
			return nil, err
		}
		results[i] = result
	}
	return results, nil
}

func (e *ShadowExecutor) ToolDefinitions(session *agent.Session) []agent.ToolDefinition {
	var out []agent.ToolDefinition
	seen := make(map[string]struct{})
	if provider, ok := e.inner.(agent.ToolDefinitionProvider); ok {
		for _, definition := range provider.ToolDefinitions(session) {
			key := strings.TrimSpace(definition.Name)
			if key == "" {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, copyToolDefinition(definition))
		}
	}
	for _, definition := range shadowToolDefinitions() {
		key := strings.TrimSpace(definition.Name)
		if _, ok := seen[key]; ok {
			continue
		}
		out = append(out, copyToolDefinition(definition))
	}
	return out
}

func (e *ShadowExecutor) ResolveTool(session *agent.Session, name string) (*agent.ResolvedTool, bool) {
	if definition, ok := shadowToolDefinition(name); ok {
		return resolvedToolFromBinding(nil, definition, "shadow"), true
	}
	resolver, ok := e.inner.(agent.ToolResolver)
	if !ok {
		return nil, false
	}
	return resolver.ResolveTool(session, name)
}

func isShadowToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case "fs.diff", "fs.changes", "fs.revert":
		return true
	default:
		return false
	}
}

func shadowToolDefinitions() []agent.ToolDefinition {
	return []agent.ToolDefinition{
		{
			Name:            "fs.diff",
			Description:     "Generate a unified diff between two files within the workspace root.",
			InputSchema:     builtinFSDiffSchema(),
			OutputSchema:    builtinFSDiffOutputSchema(),
			SideEffectClass: "read",
			Idempotent:      true,
			ExecutionKey:    "fs:diff",
			Source:          "builtin",
			SourceRef:       "builtin:shadow",
			Trust:           "bundled",
			Eligible:        true,
			Availability: agent.ToolAvailability{
				Status: agent.AvailabilityReady,
			},
		},
		{
			Name:            "fs.changes",
			Description:     "List all files modified in this session, optionally showing diffs.",
			InputSchema:     builtinFSChangesSchema(),
			OutputSchema:    builtinFSChangesOutputSchema(),
			SideEffectClass: "read",
			Idempotent:      true,
			ExecutionKey:    "fs:changes",
			Source:          "builtin",
			SourceRef:       "builtin:shadow",
			Trust:           "bundled",
			Eligible:        true,
			Availability: agent.ToolAvailability{
				Status: agent.AvailabilityReady,
			},
		},
		{
			Name:             "fs.revert",
			Description:      "Revert a file to its state before session modifications.",
			InputSchema:      builtinFSRevertSchema(),
			OutputSchema:     builtinFSRevertOutputSchema(),
			SideEffectClass:  "local_write",
			Idempotent:       false,
			RequiresApproval: true,
			ExecutionKey:     "fs:{path}",
			Source:           "builtin",
			SourceRef:        "builtin:shadow",
			Trust:            "bundled",
			Eligible:         true,
			Availability: agent.ToolAvailability{
				Status: agent.AvailabilityReady,
			},
		},
	}
}

func shadowToolDefinition(name string) (agent.ToolDefinition, bool) {
	trimmed := strings.TrimSpace(name)
	for _, definition := range shadowToolDefinitions() {
		if strings.EqualFold(definition.Name, trimmed) {
			return copyToolDefinition(definition), true
		}
	}
	return agent.ToolDefinition{}, false
}

// captureForCall extracts the path from a write tool call and captures it.
func (e *ShadowExecutor) captureForCall(shadow *EditShadow, call agent.ToolCall) {
	var pathStr string
	switch call.Name {
	case "fs.write", "fs.edit", "fs.delete", "fs.append":
		pathStr, _ = stringFrom(call.Input["path"])
	case "fs.move":
		pathStr, _ = stringFrom(call.Input["source"])
	case "fs.patch":
		// Patch can touch multiple files; capture happens inside the
		// inner executor's patch logic. We'll need a hook there.
		return
	}
	if pathStr == "" {
		return
	}
	if r := e.resolverFromInner(); r != nil {
		if abs, err := r.resolvePath(pathStr); err == nil {
			shadow.Capture(abs)
		}
	}
}

func (e *ShadowExecutor) resolverFromInner() pathResolver {
	if r, ok := e.inner.(pathResolver); ok {
		return r
	}
	return nil
}

// --- Shadow query tool handlers ---

func (e *ShadowExecutor) handleDiff(call agent.ToolCall, shadow *EditShadow) (contextengine.ToolResult, error) {
	pathA, _ := stringFrom(call.Input["path_a"])
	pathB, _ := stringFrom(call.Input["path_b"])

	if pathA == "" && pathB == "" {
		return contextengine.ToolResult{}, fmt.Errorf("fs.diff requires path_a and path_b, or use fs.changes to see session diffs")
	}

	// Read both files.
	contentA, err := readFileContent(pathA, e.resolverFromInner())
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.diff: cannot read %s: %w", pathA, err)
	}
	contentB, err := readFileContent(pathB, e.resolverFromInner())
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.diff: cannot read %s: %w", pathB, err)
	}

	diff := computeDiff("a/"+pathA, "b/"+pathB, contentA, contentB)
	if diff == "" {
		diff = "(no differences)"
	}

	body, err := json.MarshalIndent(map[string]any{
		"path_a": pathA,
		"path_b": pathB,
		"diff":   diff,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (e *ShadowExecutor) handleChanges(call agent.ToolCall, shadow *EditShadow) (contextengine.ToolResult, error) {
	pathFilter, _ := stringFrom(call.Input["path"])
	showDiff, _ := boolFrom(call.Input["detail"])

	changes := shadow.Changes()

	r := e.resolverFromInner()
	type changeOutput struct {
		Path   string `json:"path"`
		Status string `json:"status"`
		Diff   string `json:"diff,omitempty"`
	}

	var filtered []changeOutput
	for _, ch := range changes {
		displayP := ch.Path
		if r != nil {
			displayP = r.displayPath(ch.Path)
		}

		if pathFilter != "" && !strings.Contains(displayP, pathFilter) && !strings.Contains(ch.Path, pathFilter) {
			continue
		}

		out := changeOutput{Path: displayP, Status: ch.Status}
		if showDiff {
			out.Diff = shadow.Diff(ch.Path, displayP)
		}
		filtered = append(filtered, out)
	}

	body, err := json.MarshalIndent(map[string]any{
		"changes": filtered,
		"count":   len(filtered),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (e *ShadowExecutor) handleRevert(call agent.ToolCall, shadow *EditShadow) (contextengine.ToolResult, error) {
	pathStr, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	absPath := pathStr
	r := e.resolverFromInner()
	if r != nil {
		if resolved, err := r.resolvePath(pathStr); err == nil {
			absPath = resolved
		}
	}

	dryRun, _ := boolFrom(call.Input["dry_run"])
	if dryRun {
		displayP := pathStr
		if r != nil {
			displayP = r.displayPath(absPath)
		}
		diff := shadow.Diff(absPath, displayP)
		if diff == "" {
			diff = "(no changes to revert)"
		}
		body, _ := json.MarshalIndent(map[string]any{
			"path":    displayP,
			"dry_run": true,
			"diff":    diff,
		}, "", "  ")
		return contextengine.ToolResult{
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Content:    string(body),
		}, nil
	}

	msg, err := shadow.Revert(absPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fs.revert failed: %w", err)
	}

	displayP := pathStr
	if r != nil {
		displayP = r.displayPath(absPath)
	}
	body, _ := json.MarshalIndent(map[string]any{
		"path":    displayP,
		"message": msg,
	}, "", "  ")
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func readFileContent(path string, r pathResolver) (string, error) {
	absPath := path
	if r != nil {
		if resolved, err := r.resolvePath(path); err == nil {
			absPath = resolved
		}
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

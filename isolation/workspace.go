package isolation

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// workspaceIDBytes is the number of random bytes used to generate a
	// workspace ID (yielding a 32-character hex string).
	workspaceIDBytes = 16

	// workSubdir is the subdirectory inside a workspace for working files.
	workSubdir = "work"

	// artifactsSubdir is the subdirectory inside a workspace for output artifacts.
	artifactsSubdir = "artifacts"

	// logsSubdir is the subdirectory inside a workspace for workspace-specific logs.
	logsSubdir = "logs"

	// dirPerm is the permission mode used when creating workspace directories.
	dirPerm = 0o755

	// filePerm is the permission mode used when writing workspace files.
	filePerm = 0o644
)

// ---------------------------------------------------------------------------
// Workspace
// ---------------------------------------------------------------------------

// Workspace represents an isolated working directory for an agent.
// Each agent gets its own workspace to prevent cross-contamination.
type Workspace struct {
	ID        string            `json:"id"`
	AgentName string            `json:"agent_name"`
	BaseDir   string            `json:"base_dir"`
	WorkDir   string            `json:"work_dir"`
	CreatedAt time.Time         `json:"created_at"`
	Env       map[string]string `json:"env,omitempty"`
}

// EnsureDirs creates the work/, artifacts/, and logs/ subdirectories inside
// the workspace directory.
func (w *Workspace) EnsureDirs() error {
	for _, sub := range []string{workSubdir, artifactsSubdir, logsSubdir} {
		dir := filepath.Join(w.WorkDir, sub)
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("workspace %s: failed to create %s: %w", w.ID, sub, err)
		}
	}
	return nil
}

// WriteFile writes data to a file relative to the workspace work/ directory.
// Parent directories are created automatically.
func (w *Workspace) WriteFile(name string, data []byte) error {
	path, err := w.resolveWorkPath(name)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("workspace %s: failed to create parent dir for %s: %w", w.ID, name, err)
	}
	if err := os.WriteFile(path, data, filePerm); err != nil {
		return fmt.Errorf("workspace %s: failed to write %s: %w", w.ID, name, err)
	}
	return nil
}

// ReadFile reads a file relative to the workspace work/ directory.
func (w *Workspace) ReadFile(name string) ([]byte, error) {
	path, err := w.resolveWorkPath(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workspace %s: failed to read %s: %w", w.ID, name, err)
	}
	return data, nil
}

func (w *Workspace) resolveWorkPath(name string) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace is required")
	}
	rawName := strings.TrimSpace(name)
	if rawName == "" {
		return "", fmt.Errorf("workspace %s: file name is required", w.ID)
	}
	if filepath.IsAbs(rawName) {
		return "", fmt.Errorf("workspace %s: rejected path %q: absolute paths are not allowed", w.ID, name)
	}
	cleaned := filepath.Clean(rawName)
	if cleaned == "." || cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("workspace %s: rejected path %q: file name is required", w.ID, name)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace %s: rejected path %q: parent traversal is not allowed", w.ID, name)
	}
	root := filepath.Clean(filepath.Join(w.WorkDir, workSubdir))
	resolved := filepath.Clean(filepath.Join(root, cleaned))
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", fmt.Errorf("workspace %s: rejected path %q: %w", w.ID, name, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace %s: rejected path %q: escapes workspace root", w.ID, name)
	}
	return resolved, nil
}

// ListFiles returns the relative paths of all files in the workspace work/
// directory, sorted alphabetically.
func (w *Workspace) ListFiles() ([]string, error) {
	root := filepath.Join(w.WorkDir, workSubdir)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}

	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("workspace %s: failed to list files: %w", w.ID, err)
	}
	sort.Strings(files)
	return files, nil
}

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

// Manager manages isolated workspace directories for agents.
type Manager struct {
	mu         sync.Mutex
	baseDir    string
	workspaces map[string]*Workspace
}

// NewManager creates a Manager that stores workspaces under baseDir.
// The base directory is created if it does not exist.
func NewManager(baseDir string) (*Manager, error) {
	if err := os.MkdirAll(baseDir, dirPerm); err != nil {
		return nil, fmt.Errorf("failed to create workspace base dir: %w", err)
	}
	return &Manager{
		baseDir:    baseDir,
		workspaces: make(map[string]*Workspace),
	}, nil
}

// Create creates a new isolated workspace for the given agent.
// The workspace directory structure is:
//
//	baseDir/agentName/workspaceID/
//	  work/
//	  artifacts/
//	  logs/
func (m *Manager) Create(agentName string) (*Workspace, error) {
	if agentName == "" {
		return nil, fmt.Errorf("agent name is required")
	}

	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate workspace id: %w", err)
	}

	workDir := filepath.Join(m.baseDir, agentName, id)
	ws := &Workspace{
		ID:        id,
		AgentName: agentName,
		BaseDir:   m.baseDir,
		WorkDir:   workDir,
		CreatedAt: time.Now().UTC(),
		Env:       make(map[string]string),
	}

	if err := ws.EnsureDirs(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.workspaces[id] = ws
	m.mu.Unlock()

	return ws, nil
}

// Get returns the workspace with the given ID.
func (m *Manager) Get(id string) (*Workspace, bool) {
	m.mu.Lock()
	ws, ok := m.workspaces[id]
	m.mu.Unlock()
	return ws, ok
}

// List returns all managed workspaces sorted by creation time (oldest first).
func (m *Manager) List() []*Workspace {
	m.mu.Lock()
	out := make([]*Workspace, 0, len(m.workspaces))
	for _, ws := range m.workspaces {
		out = append(out, ws)
	}
	m.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// ListByAgent returns workspaces belonging to the given agent, sorted by
// creation time (oldest first).
func (m *Manager) ListByAgent(agentName string) []*Workspace {
	m.mu.Lock()
	var out []*Workspace
	for _, ws := range m.workspaces {
		if ws.AgentName == agentName {
			out = append(out, ws)
		}
	}
	m.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Cleanup removes the workspace directory and deregisters it from the manager.
func (m *Manager) Cleanup(id string) error {
	m.mu.Lock()
	ws, ok := m.workspaces[id]
	if ok {
		delete(m.workspaces, id)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("workspace %s not found", id)
	}
	if err := os.RemoveAll(ws.WorkDir); err != nil {
		return fmt.Errorf("workspace %s: failed to remove directory: %w", id, err)
	}
	return nil
}

// CleanupByAgent removes all workspaces belonging to the given agent.
func (m *Manager) CleanupByAgent(agentName string) error {
	m.mu.Lock()
	var toRemove []*Workspace
	for _, ws := range m.workspaces {
		if ws.AgentName == agentName {
			toRemove = append(toRemove, ws)
		}
	}
	for _, ws := range toRemove {
		delete(m.workspaces, ws.ID)
	}
	m.mu.Unlock()

	var firstErr error
	for _, ws := range toRemove {
		if err := os.RemoveAll(ws.WorkDir); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("workspace %s: failed to remove directory: %w", ws.ID, err)
		}
	}
	return firstErr
}

// CleanupStale removes workspaces older than maxAge and returns the count of
// removed workspaces.
func (m *Manager) CleanupStale(maxAge time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-maxAge)

	m.mu.Lock()
	var stale []*Workspace
	for _, ws := range m.workspaces {
		if ws.CreatedAt.Before(cutoff) {
			stale = append(stale, ws)
		}
	}
	for _, ws := range stale {
		delete(m.workspaces, ws.ID)
	}
	m.mu.Unlock()

	var firstErr error
	for _, ws := range stale {
		if err := os.RemoveAll(ws.WorkDir); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("workspace %s: failed to remove directory: %w", ws.ID, err)
		}
	}
	return len(stale), firstErr
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func generateID() (string, error) {
	var buf [workspaceIDBytes]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

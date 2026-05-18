package registry

import (
	"context"
	"fmt"
	"sort"
	"sync"

	captypes "github.com/fulcrus/hopclaw/capability/types"
)

// Capability is the interface that all capability implementations must satisfy.
type Capability interface {
	Manifest() captypes.Manifest
	Health(ctx context.Context) captypes.Health
	Invoke(ctx context.Context, req captypes.InvokeRequest) (*captypes.InvokeResult, error)
}

// SessionCapability extends Capability with session lifecycle.
type SessionCapability interface {
	Capability
	OpenSession(ctx context.Context, params map[string]any) (*captypes.SessionHandle, error)
	CloseSession(ctx context.Context, sessionID string) error
}

// SessionLister is optionally implemented by session capabilities that
// track their active sessions in-process.
type SessionLister interface {
	ListSessions() []*captypes.SessionHandle
}

// JobCapability extends Capability with job lifecycle.
type JobCapability interface {
	Capability
	StartJob(ctx context.Context, params map[string]any) (*captypes.JobHandle, error)
	CancelJob(ctx context.Context, jobID string) error
	JobStatus(ctx context.Context, jobID string) (*captypes.JobHandle, error)
}

// Registry holds all registered capabilities and provides lookup.
type Registry struct {
	mu           sync.RWMutex
	capabilities map[string]Capability
}

// New creates an empty capability registry.
func New() *Registry {
	return &Registry{
		capabilities: make(map[string]Capability),
	}
}

// Register adds a capability. Returns error if name is already taken.
func (r *Registry) Register(cap Capability) error {
	m := cap.Manifest()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.capabilities[m.Name]; exists {
		return fmt.Errorf("capability %q already registered", m.Name)
	}
	r.capabilities[m.Name] = cap
	return nil
}

// Get returns a capability by name.
func (r *Registry) Get(name string) (Capability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.capabilities[name]
	return c, ok
}

// List returns all registered manifests.
func (r *Registry) List() []captypes.Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]captypes.Manifest, 0, len(r.capabilities))
	for _, c := range r.capabilities {
		out = append(out, c.Manifest())
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// HealthAll checks health of all capabilities.
func (r *Registry) HealthAll(ctx context.Context) map[string]captypes.Health {
	r.mu.RLock()
	caps := make(map[string]Capability, len(r.capabilities))
	for k, v := range r.capabilities {
		caps[k] = v
	}
	r.mu.RUnlock()

	results := make(map[string]captypes.Health, len(caps))
	for name, cap := range caps {
		results[name] = cap.Health(ctx)
	}
	return results
}

// Names returns the list of registered capability names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.capabilities))
	for name := range r.capabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ListCapabilitySessions returns tracked sessions for the named capability,
// or nil if it does not exist or does not implement SessionLister.
func (r *Registry) ListCapabilitySessions(capName string) []*captypes.SessionHandle {
	cap, ok := r.Get(capName)
	if !ok {
		return nil
	}
	lister, ok := cap.(SessionLister)
	if !ok {
		return nil
	}
	return lister.ListSessions()
}

// CloseCapabilitySession closes a session on the named capability.
func (r *Registry) CloseCapabilitySession(ctx context.Context, capName, sessionID string) error {
	cap, ok := r.Get(capName)
	if !ok {
		return fmt.Errorf("capability %q not found", capName)
	}
	sc, ok := cap.(SessionCapability)
	if !ok {
		return fmt.Errorf("capability %q does not support sessions", capName)
	}
	return sc.CloseSession(ctx, sessionID)
}

// Reports returns operator-facing capability manifests with current health.
func (r *Registry) Reports(ctx context.Context) []captypes.Report {
	r.mu.RLock()
	caps := make(map[string]Capability, len(r.capabilities))
	for name, cap := range r.capabilities {
		caps[name] = cap
	}
	r.mu.RUnlock()

	names := make([]string, 0, len(caps))
	for name := range caps {
		names = append(names, name)
	}
	sort.Strings(names)

	reports := make([]captypes.Report, 0, len(names))
	for _, name := range names {
		cap := caps[name]
		reports = append(reports, captypes.Report{
			Manifest: cap.Manifest(),
			Health:   cap.Health(ctx),
		})
	}
	return reports
}

package runtime

import (
	"sort"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Agent session key prefix
// ---------------------------------------------------------------------------

const agentSessionKeyPrefix = "agent:"

// minAgentKeyParts is the minimum number of colon-separated segments in an
// agent-routed session key ("agent", name, innerKey).
const minAgentKeyParts = 3

// ---------------------------------------------------------------------------
// AgentProfile
// ---------------------------------------------------------------------------

// AgentProfile defines a named agent configuration that can be selected
// via session key prefix "agent:name:rest".
type AgentProfile struct {
	Name         string   `json:"name" yaml:"name"`
	Description  string   `json:"description,omitempty" yaml:"description,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty" yaml:"system_prompt,omitempty"`
	Model        string   `json:"model,omitempty" yaml:"model,omitempty"`
	Tools        []string `json:"tools,omitempty" yaml:"tools,omitempty"`   // allowed tool names
	Skills       []string `json:"skills,omitempty" yaml:"skills,omitempty"` // allowed skill names
	MaxTokens    int      `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
}

// ---------------------------------------------------------------------------
// AgentRouter
// ---------------------------------------------------------------------------

// AgentRouter manages named agent profiles and routes session keys.
type AgentRouter struct {
	mu       sync.RWMutex             // guards profiles
	profiles map[string]*AgentProfile // keyed by profile name
}

// NewAgentRouter creates an AgentRouter populated with the supplied profiles.
// Profiles whose Name is empty are silently skipped.
func NewAgentRouter(profiles []AgentProfile) *AgentRouter {
	index := make(map[string]*AgentProfile, len(profiles))
	for i := range profiles {
		name := strings.TrimSpace(profiles[i].Name)
		if name == "" {
			continue
		}
		p := profiles[i]
		p.Name = name
		index[name] = &p
	}
	return &AgentRouter{profiles: index}
}

// Resolve parses a session key with the format "agent:name:rest" and returns
// the matched profile. If the key does not carry the agent prefix, or the
// named profile is unknown, agentName is empty and profile is nil.
func (r *AgentRouter) Resolve(sessionKey string) (agentName string, innerKey string, profile *AgentProfile) {
	if !strings.HasPrefix(sessionKey, agentSessionKeyPrefix) {
		return "", sessionKey, nil
	}

	parts := strings.SplitN(sessionKey, ":", minAgentKeyParts)
	if len(parts) < minAgentKeyParts {
		return "", sessionKey, nil
	}

	name := parts[1]
	inner := parts[2]

	r.mu.RLock()
	p := r.profiles[name]
	r.mu.RUnlock()

	if p == nil {
		return "", sessionKey, nil
	}
	return name, inner, p
}

// List returns all registered profiles sorted by name.
func (r *AgentRouter) List() []AgentProfile {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]AgentProfile, 0, len(r.profiles))
	for _, p := range r.profiles {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// Get returns the profile with the given name.
func (r *AgentRouter) Get(name string) (*AgentProfile, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.profiles[name]
	if !ok {
		return nil, false
	}
	cp := *p
	return &cp, true
}

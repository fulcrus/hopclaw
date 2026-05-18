package modelrouter

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// FailureReason classifies model call failures for cooldown/disable decisions.
type FailureReason string

const (
	FailureAuth          FailureReason = "auth"
	FailureAuthPermanent FailureReason = "auth_permanent"
	FailureFormat        FailureReason = "format"
	FailureRateLimit     FailureReason = "rate_limit"
	FailureOverloaded    FailureReason = "overloaded"
	FailureBilling       FailureReason = "billing"
	FailureTimeout       FailureReason = "timeout"
	FailureModelNotFound FailureReason = "model_not_found"
	FailureUnknown       FailureReason = "unknown"
)

// FailureClassToReason maps old FailureClass values to the new FailureReason type.
func FailureClassToReason(class FailureClass) FailureReason {
	switch class {
	case FailureRateLimited:
		return FailureRateLimit
	case FailureQuota:
		return FailureBilling
	case FailureUnavailable:
		return FailureOverloaded
	case FailureServer:
		return FailureTimeout
	case FailureClient:
		return FailureFormat
	default:
		return FailureUnknown
	}
}

type Capability string

const (
	CapabilityChat      Capability = "chat"
	CapabilityTools     Capability = "tools"
	CapabilityVision    Capability = "vision"
	CapabilityThinking  Capability = "thinking"
	CapabilityJSONMode  Capability = "json_mode"
	CapabilityStreaming Capability = "streaming"
)

type FailureClass string

const (
	FailureRateLimited FailureClass = "rate_limited"
	FailureQuota       FailureClass = "quota"
	FailureUnavailable FailureClass = "unavailable"
	FailureServer      FailureClass = "server_error"
	FailureClient      FailureClass = "client_error"
)

type ModelProfile struct {
	ID              string
	Provider        string
	Priority        int
	ContextWindow   int
	MaxOutputTokens int
	Enabled         bool
	Supports        map[Capability]bool
	CooldownUntil   time.Time // Deprecated: use InMemoryRouter's internal failure stats instead.
}

// ProfileView is the serialized/public representation of a model router profile.
// It omits transient runtime state so operator/CLI surfaces can share a stable contract.
type ProfileView struct {
	ID              string              `json:"id"`
	Provider        string              `json:"provider"`
	Priority        int                 `json:"priority"`
	ContextWindow   int                 `json:"context_window"`
	MaxOutputTokens int                 `json:"max_output_tokens"`
	Enabled         bool                `json:"enabled"`
	Supports        map[Capability]bool `json:"supports,omitempty"`
}

// ProfileViewFromProfile projects an in-memory router profile onto the shared
// public contract used by operator/CLI surfaces.
func ProfileViewFromProfile(profile ModelProfile) ProfileView {
	return ProfileView{
		ID:              profile.ID,
		Provider:        profile.Provider,
		Priority:        profile.Priority,
		ContextWindow:   profile.ContextWindow,
		MaxOutputTokens: profile.MaxOutputTokens,
		Enabled:         profile.Enabled,
		Supports:        cloneCapabilities(profile.Supports),
	}
}

// ProfileViewsFromProfiles converts router profiles into their serialized view contract.
func ProfileViewsFromProfiles(profiles []ModelProfile) []ProfileView {
	if len(profiles) == 0 {
		return nil
	}
	out := make([]ProfileView, len(profiles))
	for i, profile := range profiles {
		out[i] = ProfileViewFromProfile(profile)
	}
	return out
}

// ModelProfile converts the serialized/public contract back into an in-memory profile.
func (view ProfileView) ModelProfile() ModelProfile {
	return ModelProfile{
		ID:              view.ID,
		Provider:        view.Provider,
		Priority:        view.Priority,
		ContextWindow:   view.ContextWindow,
		MaxOutputTokens: view.MaxOutputTokens,
		Enabled:         view.Enabled,
		Supports:        cloneCapabilities(view.Supports),
	}
}

// ProfileFailureStats tracks failure state for a single model profile.
type ProfileFailureStats struct {
	CooldownUntil  time.Time // Transient failures (rate_limit, overloaded, timeout)
	DisabledUntil  time.Time // Permanent failures (auth, billing)
	DisabledReason FailureReason
	ErrorCount     int
	FailureCounts  map[FailureReason]int
	LastFailureAt  time.Time
	LastSuccessAt  time.Time
}

type RouteRequest struct {
	RequestedModel   string
	Required         []Capability
	MinContextWindow int
	MinOutputTokens  int
}

type RouteDecision struct {
	Model        ModelProfile
	FailoverFrom string
	Reason       string
}

type Router interface {
	Select(ctx context.Context, req RouteRequest) (RouteDecision, error)
	ReportFailure(ctx context.Context, modelID string, class FailureClass) error
	ReportSuccess(ctx context.Context, modelID string) error
}

type InMemoryRouter struct {
	mu              sync.RWMutex
	profiles        map[string]ModelProfile
	failureStats    map[string]*ProfileFailureStats
	defaultCooldown time.Duration
}

func NewInMemoryRouter(profiles []ModelProfile, defaultCooldown time.Duration) *InMemoryRouter {
	index := make(map[string]ModelProfile, len(profiles))
	stats := make(map[string]*ProfileFailureStats, len(profiles))
	for _, profile := range profiles {
		if profile.ID == "" {
			continue
		}
		normalized := normalizeProfile(profile)
		index[profile.ID] = normalized
		fs := &ProfileFailureStats{
			FailureCounts: make(map[FailureReason]int),
		}
		// Migrate legacy CooldownUntil from ModelProfile into failure stats.
		if !profile.CooldownUntil.IsZero() {
			fs.CooldownUntil = profile.CooldownUntil
		}
		stats[profile.ID] = fs
	}
	if defaultCooldown <= 0 {
		defaultCooldown = 30 * time.Second
	}
	return &InMemoryRouter{
		profiles:        index,
		failureStats:    stats,
		defaultCooldown: defaultCooldown,
	}
}

func (r *InMemoryRouter) Select(_ context.Context, req RouteRequest) (RouteDecision, error) {
	r.mu.Lock()
	r.clearExpiredCooldowns()
	r.mu.Unlock()

	r.mu.RLock()
	defer r.mu.RUnlock()

	candidates := r.compatibleProfiles(req)
	if len(candidates) == 0 {
		return RouteDecision{}, fmt.Errorf("no compatible model for request")
	}
	chosen := candidates[0]
	decision := RouteDecision{
		Model: chosen,
	}
	if req.RequestedModel != "" && chosen.ID != req.RequestedModel {
		decision.FailoverFrom = req.RequestedModel
		decision.Reason = "requested model unavailable or incompatible"
	}
	return decision, nil
}

func (r *InMemoryRouter) ReportFailure(_ context.Context, modelID string, class FailureClass) error {
	reason := FailureClassToReason(class)
	return r.reportFailureWithReason(modelID, reason)
}

// ReportFailureWithReason reports a model failure using the expanded FailureReason type.
func (r *InMemoryRouter) ReportFailureWithReason(_ context.Context, modelID string, reason FailureReason) error {
	return r.reportFailureWithReason(modelID, reason)
}

func (r *InMemoryRouter) reportFailureWithReason(modelID string, reason FailureReason) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.profiles[modelID]; !ok {
		return fmt.Errorf("model %s not found", modelID)
	}
	stats := r.failureStats[modelID]
	if stats == nil {
		stats = &ProfileFailureStats{
			FailureCounts: make(map[FailureReason]int),
		}
		r.failureStats[modelID] = stats
	}

	now := time.Now().UTC()
	stats.ErrorCount++
	stats.FailureCounts[reason]++
	stats.LastFailureAt = now

	if IsTransient(reason) {
		// Active window preservation: don't extend an existing active cooldown.
		if stats.CooldownUntil.IsZero() || !stats.CooldownUntil.After(now) {
			stats.CooldownUntil = now.Add(TransientCooldown(stats.FailureCounts[reason]))
		}
	} else {
		// Permanent failure: apply disable duration.
		// Active window preservation for disabled window too.
		if stats.DisabledUntil.IsZero() || !stats.DisabledUntil.After(now) {
			stats.DisabledUntil = now.Add(PermanentDisableDuration(stats.FailureCounts[reason]))
			stats.DisabledReason = reason
		}
	}

	return nil
}

func (r *InMemoryRouter) ReportSuccess(_ context.Context, modelID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.profiles[modelID]; !ok {
		return fmt.Errorf("model %s not found", modelID)
	}
	stats := r.failureStats[modelID]
	if stats == nil {
		return nil
	}

	now := time.Now().UTC()
	stats.LastSuccessAt = now
	// Clear transient cooldown on success.
	stats.CooldownUntil = time.Time{}
	// Reset transient error counts; keep permanent counts until they expire.
	for reason := range stats.FailureCounts {
		if IsTransient(reason) {
			delete(stats.FailureCounts, reason)
		}
	}
	// Recalculate ErrorCount from remaining permanent failure counts.
	total := 0
	for _, count := range stats.FailureCounts {
		total += count
	}
	stats.ErrorCount = total
	return nil
}

func (r *InMemoryRouter) compatibleProfiles(req RouteRequest) []ModelProfile {
	var requested *ModelProfile
	var requestedProvider string
	if req.RequestedModel != "" {
		if profile, ok := r.profiles[req.RequestedModel]; ok {
			requestedProvider = profile.Provider
			if r.isCompatible(profile, req) {
				copied := profile
				requested = &copied
			}
		}
	}

	var sameProvider []ModelProfile
	var crossProvider []ModelProfile
	for _, profile := range r.profiles {
		if requested != nil && profile.ID == requested.ID {
			continue
		}
		if !r.isCompatible(profile, req) {
			continue
		}
		if requestedProvider != "" && profile.Provider == requestedProvider {
			sameProvider = append(sameProvider, profile)
		} else {
			crossProvider = append(crossProvider, profile)
		}
	}
	sortProfiles(sameProvider)
	sortProfiles(crossProvider)

	out := make([]ModelProfile, 0, 1+len(sameProvider)+len(crossProvider))
	if requested != nil {
		out = append(out, *requested)
	}
	out = append(out, sameProvider...)
	out = append(out, crossProvider...)
	return out
}

func (r *InMemoryRouter) isCompatible(profile ModelProfile, req RouteRequest) bool {
	if !profile.Enabled {
		return false
	}
	// Check internal failure stats for cooldown/disable windows.
	now := time.Now().UTC()
	if stats, ok := r.failureStats[profile.ID]; ok {
		if !stats.CooldownUntil.IsZero() && stats.CooldownUntil.After(now) {
			return false
		}
		if !stats.DisabledUntil.IsZero() && stats.DisabledUntil.After(now) {
			return false
		}
	}
	// Legacy: still honor CooldownUntil on the profile struct.
	if !profile.CooldownUntil.IsZero() && profile.CooldownUntil.After(now) {
		return false
	}
	if req.MinContextWindow > 0 && profile.ContextWindow < req.MinContextWindow {
		return false
	}
	if req.MinOutputTokens > 0 && profile.MaxOutputTokens > 0 && profile.MaxOutputTokens < req.MinOutputTokens {
		return false
	}
	for _, cap := range req.Required {
		if !profile.Supports[cap] {
			return false
		}
	}
	return true
}

// clearExpiredCooldowns resets expired cooldown and disable windows.
// Must be called with r.mu held for writing.
func (r *InMemoryRouter) clearExpiredCooldowns() {
	now := time.Now().UTC()
	for _, stats := range r.failureStats {
		if !stats.CooldownUntil.IsZero() && !stats.CooldownUntil.After(now) {
			stats.CooldownUntil = time.Time{}
			// Reset transient error counts when cooldown expires.
			for reason := range stats.FailureCounts {
				if IsTransient(reason) {
					delete(stats.FailureCounts, reason)
				}
			}
		}
		if !stats.DisabledUntil.IsZero() && !stats.DisabledUntil.After(now) {
			stats.DisabledUntil = time.Time{}
			stats.DisabledReason = ""
			// Reset permanent error counts when disable expires.
			for reason := range stats.FailureCounts {
				if !IsTransient(reason) {
					delete(stats.FailureCounts, reason)
				}
			}
			// Recalculate ErrorCount.
			total := 0
			for _, count := range stats.FailureCounts {
				total += count
			}
			stats.ErrorCount = total
		}
	}
}

// GetFailureStats returns a copy of the failure stats for the given model.
// Returns nil if the model has no stats.
func (r *InMemoryRouter) GetFailureStats(modelID string) *ProfileFailureStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stats, ok := r.failureStats[modelID]
	if !ok || stats == nil {
		return nil
	}
	// Return a copy.
	cp := *stats
	cp.FailureCounts = make(map[FailureReason]int, len(stats.FailureCounts))
	for k, v := range stats.FailureCounts {
		cp.FailureCounts[k] = v
	}
	return &cp
}

func normalizeProfile(profile ModelProfile) ModelProfile {
	if profile.Supports == nil {
		profile.Supports = map[Capability]bool{}
	}
	profile.Supports[CapabilityChat] = true
	return profile
}

func cloneCapabilities(in map[Capability]bool) map[Capability]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[Capability]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortProfiles(profiles []ModelProfile) {
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].Priority != profiles[j].Priority {
			return profiles[i].Priority > profiles[j].Priority
		}
		if !strings.EqualFold(profiles[i].Provider, profiles[j].Provider) {
			return profiles[i].Provider < profiles[j].Provider
		}
		return profiles[i].ID < profiles[j].ID
	})
}

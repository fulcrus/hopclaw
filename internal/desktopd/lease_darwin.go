//go:build darwin

package desktopd

import (
	"context"
	"time"
)

type desktopFocusLease struct {
	AppName       string    `json:"app,omitempty"`
	BundleID      string    `json:"bundle_id,omitempty"`
	TitleContains string    `json:"title_contains,omitempty"`
	WindowIndex   int       `json:"window_index,omitempty"`
	AcquiredAt    time.Time `json:"acquired_at,omitempty"`
}

func (s *darwinSession) SessionMetadata(_ context.Context) map[string]any {
	data := map[string]any{
		"workspace":    s.workspace,
		"host_profile": cloneMapAny(s.hostProfile),
	}
	if lease := s.focusLease; lease != nil {
		data["focus_lease"] = lease.toMap()
	}
	if lease := s.visibilityLease; lease != nil {
		data["visibility_lease"] = lease.toMap()
	}
	return data
}

func (s *darwinSession) setFocusLease(appName, bundleID, titleContains string, windowIndex int) {
	s.focusLease = &desktopFocusLease{
		AppName:       appName,
		BundleID:      bundleID,
		TitleContains: titleContains,
		WindowIndex:   windowIndex,
		AcquiredAt:    time.Now().UTC(),
	}
}

func (s *darwinSession) setVisibilityLease(appName, bundleID, titleContains string, windowIndex int) {
	s.visibilityLease = &desktopFocusLease{
		AppName:       appName,
		BundleID:      bundleID,
		TitleContains: titleContains,
		WindowIndex:   windowIndex,
		AcquiredAt:    time.Now().UTC(),
	}
}

func (s *darwinSession) ensureFocusLease(ctx context.Context, params map[string]any) (map[string]any, error) {
	target := leaseTargetFromParams(params)
	if target == nil {
		target = s.focusLease
	}
	if target == nil {
		return nil, nil
	}
	if target.WindowIndex > 0 || target.TitleContains != "" {
		state, _, err := focusWindowReady(ctx, target.AppName, target.BundleID, target.TitleContains, target.WindowIndex, desktopWaitInteractive, desktopReadyDefaultWait)
		if err != nil {
			return nil, err
		}
		resolvedTitle := firstNonEmpty(state.MatchedWindow.Title, target.TitleContains)
		resolvedIndex := state.MatchedWindow.Index
		s.setFocusLease(state.App.Name, state.App.BundleID, resolvedTitle, resolvedIndex)
		s.setVisibilityLease(state.App.Name, state.App.BundleID, resolvedTitle, resolvedIndex)
		return s.focusLease.toMap(), nil
	}
	state, _, err := waitForAppReady(ctx, target.AppName, target.BundleID, desktopWaitFocused, desktopReadyDefaultWait)
	if err != nil {
		return nil, err
	}
	s.setFocusLease(state.App.Name, state.App.BundleID, "", 0)
	s.setVisibilityLease(state.App.Name, state.App.BundleID, "", 0)
	return s.focusLease.toMap(), nil
}

func leaseTargetFromParams(params map[string]any) *desktopFocusLease {
	appName := stringParam(params, "app")
	bundleID := stringParam(params, "bundle_id")
	titleContains := stringParam(params, "title_contains")
	windowIndex := intParam(params, "window_index")
	if appName == "" && bundleID == "" && titleContains == "" && windowIndex <= 0 {
		return nil
	}
	return &desktopFocusLease{
		AppName:       appName,
		BundleID:      bundleID,
		TitleContains: titleContains,
		WindowIndex:   windowIndex,
	}
}

func (l *desktopFocusLease) toMap() map[string]any {
	if l == nil {
		return nil
	}
	data := map[string]any{
		"app":       l.AppName,
		"bundle_id": l.BundleID,
	}
	if !l.AcquiredAt.IsZero() {
		data["acquired_at"] = l.AcquiredAt.Format(time.RFC3339)
	}
	if l.TitleContains != "" {
		data["title_contains"] = l.TitleContains
	}
	if l.WindowIndex > 0 {
		data["window_index"] = l.WindowIndex
	}
	return data
}

func cloneMapAny(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMapAny(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneValue(item))
		}
		return out
	default:
		return typed
	}
}

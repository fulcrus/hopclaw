package config

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Config Reload Plan — Analyze changes for minimum-scope reload
// ---------------------------------------------------------------------------

// ReloadAction describes what kind of reload is needed.
type ReloadAction string

const (
	ReloadActionNone    ReloadAction = "none"
	ReloadActionHot     ReloadAction = "hot"
	ReloadActionRestart ReloadAction = "restart"
)

// ReloadPlan describes the reload strategy for a config change.
type ReloadPlan struct {
	Action      ReloadAction `json:"action"`
	SideEffects []string     `json:"side_effects,omitempty"`
	Reason      string       `json:"reason,omitempty"`
}

// restartPrefixes are config paths that require a full restart.
var restartPrefixes = []string{
	"server.address",
	"server.auth_token",
	"auth.",
	"store.",
}

// hotReloadMap maps config path prefixes to their required side effects.
var hotReloadMap = map[string]string{
	"channels.":  "restart-channels",
	"cron.":      "restart-cron",
	"heartbeat.": "restart-heartbeat",
	"hooks.":     "reload-hooks",
	"agent.":     "reload-agent",
	"models.":    "reload-models",
	"skills.":    "reload-skills",
	"tools.":     "reload-tools",
}

// AnalyzeReloadPlan determines the minimum reload scope for config changes.
func AnalyzeReloadPlan(changedPaths []string) ReloadPlan {
	if len(changedPaths) == 0 {
		return ReloadPlan{Action: ReloadActionNone}
	}

	plan := ReloadPlan{Action: ReloadActionNone}

	for _, path := range changedPaths {
		// Check restart-required paths.
		for _, prefix := range restartPrefixes {
			if strings.HasPrefix(path, prefix) {
				return ReloadPlan{
					Action: ReloadActionRestart,
					Reason: fmt.Sprintf("change to %q requires restart", path),
				}
			}
		}

		// Check hot-reloadable paths.
		for prefix, sideEffect := range hotReloadMap {
			if strings.HasPrefix(path, prefix) {
				plan.Action = ReloadActionHot
				plan.SideEffects = appendUnique(plan.SideEffects, sideEffect)
			}
		}
	}

	if plan.Action == ReloadActionNone && len(changedPaths) > 0 {
		plan.Action = ReloadActionHot
	}

	return plan
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}

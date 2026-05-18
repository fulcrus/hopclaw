package bootstrap

import (
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/config"
)

type ReloadStage string

const (
	ReloadStageConfig      ReloadStage = "config"
	ReloadStageModels      ReloadStage = "models"
	ReloadStageModules     ReloadStage = "modules"
	ReloadStageHosts       ReloadStage = "hosts"
	ReloadStageTools       ReloadStage = "tools"
	ReloadStageChannels    ReloadStage = "channels"
	ReloadStagePolicy      ReloadStage = "policy"
	ReloadStageApprovals   ReloadStage = "approvals"
	ReloadStageGovernance  ReloadStage = "governance"
	ReloadStageProjections ReloadStage = "projections"
)

var canonicalReloadStageOrder = []ReloadStage{
	ReloadStageConfig,
	ReloadStageModels,
	ReloadStageModules,
	ReloadStageHosts,
	ReloadStageTools,
	ReloadStageChannels,
	ReloadStagePolicy,
	ReloadStageApprovals,
	ReloadStageGovernance,
	ReloadStageProjections,
}

type ReloadGraph struct {
	Stages []ReloadStage `json:"stages"`
}

type ReloadPostCommitAction string

const (
	ReloadPostCommitRebuildChannelHealth ReloadPostCommitAction = "rebuild_channel_health"
	ReloadPostCommitRefreshPlugins       ReloadPostCommitAction = "refresh_plugins"
	ReloadPostCommitRestartPluginWatcher ReloadPostCommitAction = "restart_plugin_watcher"
)

func newReloadGraph(stages ...ReloadStage) (ReloadGraph, error) {
	requested := make(map[ReloadStage]struct{}, len(stages))
	for _, stage := range stages {
		if _, ok := reloadStageOrderIndex(stage); !ok {
			return ReloadGraph{}, fmt.Errorf("unknown reload stage %q", stage)
		}
		requested[stage] = struct{}{}
	}
	ordered := make([]ReloadStage, 0, len(requested))
	for _, stage := range canonicalReloadStageOrder {
		if _, ok := requested[stage]; ok {
			ordered = append(ordered, stage)
		}
	}
	graph := ReloadGraph{Stages: ordered}
	if err := validateReloadGraph(graph); err != nil {
		return ReloadGraph{}, err
	}
	return graph, nil
}

func validateReloadGraph(graph ReloadGraph) error {
	seen := make(map[ReloadStage]struct{}, len(graph.Stages))
	lastIndex := -1
	for _, stage := range graph.Stages {
		index, ok := reloadStageOrderIndex(stage)
		if !ok {
			return fmt.Errorf("unknown reload stage %q", stage)
		}
		if _, duplicate := seen[stage]; duplicate {
			return fmt.Errorf("duplicate reload stage %q", stage)
		}
		if index < lastIndex {
			return fmt.Errorf("reload stage %q appears out of order", stage)
		}
		seen[stage] = struct{}{}
		lastIndex = index
	}
	return nil
}

func reloadStageOrderIndex(stage ReloadStage) (int, bool) {
	for idx, candidate := range canonicalReloadStageOrder {
		if candidate == stage {
			return idx, true
		}
	}
	return 0, false
}

func (g ReloadGraph) HasStage(stage ReloadStage) bool {
	for _, candidate := range g.Stages {
		if candidate == stage {
			return true
		}
	}
	return false
}

func (g ReloadGraph) RuntimeStages() []ReloadStage {
	if len(g.Stages) == 0 {
		return nil
	}
	stages := make([]ReloadStage, 0, len(g.Stages))
	for _, stage := range g.Stages {
		if stage == ReloadStageConfig {
			continue
		}
		stages = append(stages, stage)
	}
	return stages
}

func (g ReloadGraph) String() string {
	if len(g.Stages) == 0 {
		return ""
	}
	parts := make([]string, 0, len(g.Stages))
	for _, stage := range g.Stages {
		if trimmed := strings.TrimSpace(string(stage)); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " -> ")
}

func buildRefreshPostCommitActions(plan RefreshPlan, oldCfg, newCfg config.Config) []ReloadPostCommitAction {
	actions := make([]ReloadPostCommitAction, 0, 2)
	if plan.RebuildChannelHealth {
		actions = append(actions, ReloadPostCommitRebuildChannelHealth)
	}
	if !pluginWatcherConfigChanged(oldCfg, newCfg) {
		return actions
	}
	if pluginsRuntimeConfigChanged(oldCfg, newCfg) {
		actions = append(actions, ReloadPostCommitRefreshPlugins)
		return actions
	}
	actions = append(actions, ReloadPostCommitRestartPluginWatcher)
	return actions
}

func pluginWatcherConfigChanged(oldCfg, newCfg config.Config) bool {
	if pluginsRuntimeConfigChanged(oldCfg, newCfg) {
		return true
	}
	return oldCfg.Skills.RefreshInterval != newCfg.Skills.RefreshInterval
}

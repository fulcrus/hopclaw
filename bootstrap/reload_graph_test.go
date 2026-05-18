package bootstrap

import (
	"reflect"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
)

func TestReloadGraphOrdersStagesCanonically(t *testing.T) {
	t.Parallel()

	graph, err := newReloadGraph(
		ReloadStageTools,
		ReloadStageConfig,
		ReloadStageApprovals,
		ReloadStageModules,
	)
	if err != nil {
		t.Fatalf("newReloadGraph() error = %v", err)
	}

	want := []ReloadStage{
		ReloadStageConfig,
		ReloadStageModules,
		ReloadStageTools,
		ReloadStageApprovals,
	}
	if !reflect.DeepEqual(graph.Stages, want) {
		t.Fatalf("graph.Stages = %#v, want %#v", graph.Stages, want)
	}
	if got := graph.String(); got != "config -> modules -> tools -> approvals" {
		t.Fatalf("graph.String() = %q", got)
	}
}

func TestValidateReloadGraphRejectsOutOfOrderStages(t *testing.T) {
	t.Parallel()

	err := validateReloadGraph(ReloadGraph{
		Stages: []ReloadStage{
			ReloadStageConfig,
			ReloadStageTools,
			ReloadStageHosts,
		},
	})
	if err == nil {
		t.Fatal("validateReloadGraph() error = nil, want failure")
	}
}

func TestAppModuleReloaderPlanBuildsStructuredReloadGraph(t *testing.T) {
	t.Parallel()

	plan := appModuleReloader{}.Plan(config.ChangeSet{
		Changes: []config.Change{
			{Section: "models", Kind: config.ChangeUpdated},
			{Section: "skills", Kind: config.ChangeUpdated},
			{Section: "hosts", Kind: config.ChangeUpdated},
			{Section: "channels", Kind: config.ChangeUpdated},
			{Section: "runtime", Kind: config.ChangeUpdated},
			{Section: "exec_approval", Kind: config.ChangeUpdated},
		},
	}, nil, nil)

	want := []ReloadStage{
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
	if !reflect.DeepEqual(plan.Graph.Stages, want) {
		t.Fatalf("plan.Graph.Stages = %#v, want %#v", plan.Graph.Stages, want)
	}
	if got := plan.RuntimeStages(); !reflect.DeepEqual(got, want[1:]) {
		t.Fatalf("plan.RuntimeStages() = %#v, want %#v", got, want[1:])
	}
}

func TestBuildRefreshPostCommitActions(t *testing.T) {
	t.Parallel()

	oldCfg := config.Config{
		Skills:  config.SkillsConfig{RefreshInterval: time.Second},
		Plugins: config.PluginsConfig{Enabled: boolPtr(true), Dirs: []string{"./plugins"}},
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{Root: "."},
		},
	}
	newCfg := oldCfg

	plan := RefreshPlan{RebuildChannelHealth: true}
	got := buildRefreshPostCommitActions(plan, oldCfg, newCfg)
	want := []ReloadPostCommitAction{
		ReloadPostCommitRebuildChannelHealth,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRefreshPostCommitActions() = %#v, want %#v", got, want)
	}

	changedWatcherInterval := newCfg
	changedWatcherInterval.Skills.RefreshInterval = 2 * time.Second
	got = buildRefreshPostCommitActions(plan, oldCfg, changedWatcherInterval)
	want = []ReloadPostCommitAction{
		ReloadPostCommitRebuildChannelHealth,
		ReloadPostCommitRestartPluginWatcher,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRefreshPostCommitActions(watcher interval) = %#v, want %#v", got, want)
	}

	changedPlugins := newCfg
	changedPlugins.Tools.Builtins.Root = "./other"
	got = buildRefreshPostCommitActions(plan, oldCfg, changedPlugins)
	want = []ReloadPostCommitAction{
		ReloadPostCommitRebuildChannelHealth,
		ReloadPostCommitRefreshPlugins,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRefreshPostCommitActions(plugin change) = %#v, want %#v", got, want)
	}
}

func TestRefreshApplyTxnDomainsFollowReloadGraphOrder(t *testing.T) {
	t.Parallel()

	txn := &refreshApplyTxn{
		app:          &App{},
		plan:         RefreshPlan{Graph: ReloadGraph{Stages: canonicalReloadStageOrder}, RebuildModels: true, RebuildPolicy: true, RebuildApproval: true, RebuildGovernance: true},
		nextState:    &controlPlaneRuntimeState{},
		nextSkills:   &preparedSkillService{},
		nextHosts:    &preparedHostRuntime{},
		nextTools:    &preparedToolRuntime{},
		nextChannels: &preparedChannels{},
	}

	got := runtimeTransactionDomainNames(txn.domains())
	want := []string{
		"models",
		"modules",
		"hosts",
		"tools",
		"channels",
		"policy",
		"approvals",
		"governance",
		"projections:bindings",
		"projections:gateway",
		"projections",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runtimeTransactionDomainNames(txn.domains()) = %#v, want %#v", got, want)
	}
}

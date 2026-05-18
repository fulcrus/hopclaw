package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/bootstrap"
	"github.com/fulcrus/hopclaw/controlplane"
	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type interactiveSetupStatusResponse struct {
	Configured bool     `json:"configured"`
	Providers  []string `json:"providers"`
}

type interactiveHelperState struct {
	Status       string `json:"status"`
	SessionCount int    `json:"session_count"`
}

type interactiveHelpersStatusResponse struct {
	Browser interactiveHelperState `json:"browser"`
	Desktop interactiveHelperState `json:"desktop"`
}

type interactiveSkillsListResponse struct {
	Count int `json:"count"`
}

type interactivePluginsListResponse struct {
	Count int `json:"count"`
}

func (g *externalInteractiveGateway) ReadinessSnapshot(ctx context.Context) (*replpkg.ReadinessSnapshot, error) {
	return buildGatewayReadinessSnapshot(ctx, g.client, g.target)
}

func (g *embeddedInteractiveGateway) ReadinessSnapshot(ctx context.Context) (*replpkg.ReadinessSnapshot, error) {
	if g == nil || g.app == nil {
		return nil, nil
	}
	return buildEmbeddedReadinessSnapshot(ctx, g.app, g.target)
}

func buildGatewayReadinessSnapshot(ctx context.Context, client *GatewayClient, target interactiveTarget) (*replpkg.ReadinessSnapshot, error) {
	if client == nil {
		return nil, nil
	}
	categories := defaultReadinessCategories(target)
	startupDiagnostics := ""
	setCategory := func(id, status, summary string) {
		interactiveApplyReadinessCategoryUpdate(categories, id, status, summary)
	}

	if body, statusCode, err := fetchGatewayHealth(ctx, client); err == nil {
		if statusCode >= 400 {
			setCategory("remote_target", "ready", readinessGatewayReachableSummary(target))
			setCategory("gateway", "blocked", gatewayHTTPError(statusCode, body).Error())
		} else if health, decodeErr := decodeGatewayHealth(body); decodeErr == nil {
			setCategory("remote_target", "ready", readinessGatewayReachableSummary(target))
			setCategory("gateway", interactiveHealthStatus(gatewayHealthLabel(health)), gatewayHealthSummary(health))
		}
	} else if gatewayErrorStatus(err) != http.StatusNotFound {
		return nil, err
	}

	if body, statusCode, err := fetchOperatorStatus(ctx, client); err == nil {
		switch {
		case statusCode >= 400:
			setCategory("gateway", "blocked", gatewayHTTPError(statusCode, body).Error())
		default:
			if status, decodeErr := decodeOperatorStatus(body); decodeErr == nil {
				if diagnostics := strings.TrimSpace(status.UserSurface.StartupDiagnostics); diagnostics != "" {
					startupDiagnostics = diagnostics
				}
				setCategory("gateway", interactiveHealthStatus(operatorStatusLabel(status)), operatorStatusSummary(status))
				if len(status.Channels) > 0 {
					channelStatus := "ready"
					summary := fmt.Sprintf("%d channel(s) connected", len(status.Channels))
					for _, item := range status.Channels {
						if !strings.EqualFold(strings.TrimSpace(item.Status), "connected") {
							channelStatus = "degraded"
							summary = strings.TrimSpace(item.Name) + " " + strings.TrimSpace(item.Status)
							break
						}
					}
					setCategory("channel_delivery", channelStatus, summary)
				}
			}
		}
	} else if gatewayErrorStatus(err) != http.StatusNotFound {
		return nil, err
	}

	var setup interactiveSetupStatusResponse
	if err := client.Get(ctx, "/operator/setup/status", &setup); err == nil {
		status := "blocked"
		summary := "no provider configured"
		if setup.Configured || len(setup.Providers) > 0 {
			status = "ready"
			summary = strings.Join(setup.Providers, ", ")
		}
		setCategory("model_provider", status, summary)
	} else if gatewayErrorStatus(err) != http.StatusNotFound {
		return nil, err
	}

	var health runtimesvc.GovernanceDeliveryHealth
	if err := client.Get(ctx, "/operator/governance/health", &health); err == nil {
		status := firstNonEmpty(strings.TrimSpace(health.Status), "unknown")
		setCategory("channel_delivery", interactiveHealthStatus(status), firstNonEmpty(strings.TrimSpace(health.Summary), status))
	} else if gatewayErrorStatus(err) != http.StatusNotFound {
		return nil, err
	}

	var helpers interactiveHelpersStatusResponse
	if err := client.Get(ctx, "/operator/helpers/status", &helpers); err == nil {
		status := "ready"
		summary := "helpers running"
		if !strings.EqualFold(strings.TrimSpace(helpers.Browser.Status), "running") && !strings.EqualFold(strings.TrimSpace(helpers.Desktop.Status), "running") {
			status = "unknown"
			summary = "managed helpers unavailable"
		}
		setCategory("devices_helpers", status, summary)
	} else if gatewayErrorStatus(err) != http.StatusNotFound {
		return nil, err
	}

	var memory memoryStatusResponse
	if err := client.Get(ctx, "/runtime/memory/status", &memory); err == nil {
		status := "degraded"
		summary := fmt.Sprintf("%s store", firstNonEmpty(strings.TrimSpace(memory.StoreType), "memory"))
		if memory.IndexReady {
			status = "ready"
			summary = fmt.Sprintf("%s index ready (%d entries)", firstNonEmpty(strings.TrimSpace(memory.StoreType), "memory"), memory.EntryCount)
		}
		setCategory("memory_index", status, summary)
	} else if gatewayErrorStatus(err) != http.StatusNotFound {
		return nil, err
	}

	var automation automationCLIItemsResponse
	if err := client.Get(ctx, automationPath, &automation); err == nil {
		status := "ready"
		summary := fmt.Sprintf("%d automation item(s)", automation.Count)
		runningServices := 0
		for _, service := range automation.Services {
			if service.Running {
				runningServices++
			}
		}
		if runningServices == 0 && len(automation.Services) > 0 {
			status = "degraded"
			summary = "automation services configured but idle"
		}
		setCategory("automation_runtime", status, summary)
	} else if gatewayErrorStatus(err) != http.StatusNotFound {
		return nil, err
	}

	var skills interactiveSkillsListResponse
	var plugins interactivePluginsListResponse
	skillsErr := client.Get(ctx, "/operator/skills", &skills)
	pluginsErr := client.Get(ctx, "/operator/plugins", &plugins)
	switch {
	case skillsErr == nil || pluginsErr == nil:
		setCategory("skills_plugins", "ready", fmt.Sprintf("%d skills · %d plugins", skills.Count, plugins.Count))
	case gatewayErrorStatus(skillsErr) != http.StatusNotFound:
		return nil, skillsErr
	case gatewayErrorStatus(pluginsErr) != http.StatusNotFound:
		return nil, pluginsErr
	}

	if report, err := loadReleaseReadiness(ctx, client); err == nil {
		status := "blocked"
		if report.Ready {
			status = "ready"
		}
		summary := fmt.Sprintf("%d blocker(s)", len(report.Blockers))
		if report.Ready {
			summary = fmt.Sprintf("%d checks passing", len(report.Checks))
		}
		setCategory("quality_release", status, summary)
	} else if gatewayErrorStatus(err) != http.StatusNotFound {
		return nil, err
	}

	candidates, err := (&externalInteractiveGateway{client: client, target: target}).RecoveryCandidates(ctx)
	if err != nil {
		return nil, err
	}
	return finalizeReadinessSnapshot(categories, candidates, startupDiagnostics), nil
}

func buildEmbeddedReadinessSnapshot(ctx context.Context, app *bootstrap.App, target interactiveTarget) (*replpkg.ReadinessSnapshot, error) {
	if app == nil {
		return nil, nil
	}
	categories := defaultReadinessCategories(target)
	startupDiagnostics := embeddedStartupDiagnostics(app)
	setCategory := func(id, status, summary string) {
		interactiveApplyReadinessCategoryUpdate(categories, id, status, summary)
	}

	providerCount := len(app.Config.Models.Providers)
	if strings.TrimSpace(app.Config.Models.OpenAICompat.APIKey) != "" {
		providerCount++
	}
	if providerCount > 0 {
		setCategory("model_provider", "ready", fmt.Sprintf("%d provider(s) configured", providerCount))
	} else {
		setCategory("model_provider", "blocked", "no model provider configured")
	}

	setCategory("gateway", "ready", "local runtime ready")
	setCategory("remote_target", "ready", localRuntimeReadinessSummary(target))

	channelStatus := "ready"
	channelSummary := "inline terminal delivery"
	if len(app.Webhooks) > 0 {
		channelSummary = fmt.Sprintf("%d webhook adapter(s)", len(app.Webhooks))
	}
	setCategory("channel_delivery", channelStatus, channelSummary)

	if app.ManagedHelpers != nil {
		setCategory("devices_helpers", "ready", "managed helpers configured")
	} else {
		setCategory("devices_helpers", "unknown", "managed helpers unavailable")
	}

	skillDirs := len(app.Config.Skills.Dirs)
	pluginDirs := len(app.Config.Plugins.Dirs)
	if skillDirs > 0 || pluginDirs > 0 || app.Plugins != nil {
		setCategory("skills_plugins", "ready", fmt.Sprintf("%d skill dir(s) · %d plugin dir(s)", skillDirs, pluginDirs))
	}

	if app.Runtime != nil {
		if entries, err := app.Runtime.ListMemory(ctx); err == nil {
			setCategory("memory_index", "ready", fmt.Sprintf("memory store available (%d entries)", len(entries)))
		} else {
			setCategory("memory_index", "unknown", "memory status unavailable")
		}
	}

	automationServices := 0
	runningServices := 0
	if app.CronService != nil {
		automationServices++
		if app.CronService.IsRunning() {
			runningServices++
		}
	}
	if app.WatchService != nil {
		automationServices++
		if app.WatchService.IsRunning() {
			runningServices++
		}
	}
	if app.WakeupService != nil {
		automationServices++
		if app.WakeupService.IsRunning() {
			runningServices++
		}
	}
	if automationServices > 0 {
		status := "degraded"
		summary := fmt.Sprintf("%d/%d automation service(s) running", runningServices, automationServices)
		if runningServices == automationServices {
			status = "ready"
		}
		setCategory("automation_runtime", status, summary)
	}

	if app.Runtime != nil {
		report, err := app.Runtime.GetReleaseReadiness(ctx, runtimesvc.ReleaseReadinessRequest{})
		if err != nil {
			return nil, err
		}
		status := "blocked"
		summary := fmt.Sprintf("%d blocker(s)", len(report.Blockers))
		if report.Ready {
			status = "ready"
			summary = fmt.Sprintf("%d checks passing", len(report.Checks))
		}
		setCategory("quality_release", status, summary)
	}

	warnings := app.OperationalWarnings()
	if len(warnings) > 0 {
		summary := controlplane.OperationalWarningHeadline(warnings)
		if summary == "" {
			summary = "runtime started in degraded mode"
		}
		setCategory("gateway", "degraded", summary)
		for _, warning := range warnings {
			if strings.HasPrefix(strings.TrimSpace(warning.Component), "channel/") {
				setCategory("channel_delivery", "degraded", summary)
				break
			}
		}
	}

	candidates, err := (&embeddedInteractiveGateway{app: app}).RecoveryCandidates(ctx)
	if err != nil {
		return nil, err
	}
	return finalizeReadinessSnapshot(categories, candidates, startupDiagnostics), nil
}

func defaultReadinessCategories(target interactiveTarget) []replpkg.ReadinessCategory {
	return []replpkg.ReadinessCategory{
		{ID: "model_provider", Label: "AI Setup", Kind: "setup", Status: "unknown", Summary: "provider status unavailable"},
		{ID: "gateway", Label: "System", Kind: "runtime", Status: "unknown", Summary: "system status unavailable"},
		{ID: "remote_target", Label: readinessCategoryLabel(target), Kind: "runtime", Status: "unknown", Summary: readinessCategoryUnavailableSummary(target)},
		{ID: "channel_delivery", Label: "Replies", Kind: "runtime", Status: "unknown", Summary: "reply delivery status unavailable"},
		{ID: "devices_helpers", Label: "Local Helpers", Kind: "runtime", Status: "unknown", Summary: "helper status unavailable"},
		{ID: "skills_plugins", Label: "Extensions", Kind: "runtime", Status: "unknown", Summary: "extensions unavailable"},
		{ID: "memory_index", Label: "Memory", Kind: "runtime", Status: "unknown", Summary: "memory status unavailable"},
		{ID: "automation_runtime", Label: "Automations", Kind: "runtime", Status: "unknown", Summary: "automation status unavailable"},
		{ID: "quality_release", Label: "Release Checks", Kind: "quality", Status: "unknown", Summary: "release readiness unavailable"},
	}
}

func readinessCategoryLabel(target interactiveTarget) string {
	name := readinessCategoryName(target)
	switch readinessCategoryKind(target) {
	case interactiveTargetLocal:
		if name == "" || strings.EqualFold(name, localTargetName) {
			return "Local runtime"
		}
		return "Local runtime " + name
	case interactiveTargetRemote:
		if name == "" {
			return "Remote"
		}
		return "Remote " + name
	default:
		return "Runtime"
	}
}

func readinessCategoryUnavailableSummary(target interactiveTarget) string {
	switch readinessCategoryKind(target) {
	case interactiveTargetLocal:
		return "local runtime status unavailable"
	case interactiveTargetRemote:
		return "remote runtime status unavailable"
	default:
		return "runtime status unavailable"
	}
}

func readinessGatewayReachableSummary(target interactiveTarget) string {
	switch readinessCategoryKind(target) {
	case interactiveTargetLocal:
		return "gateway local runtime reachable"
	case interactiveTargetRemote:
		return "gateway remote reachable"
	default:
		return "gateway runtime reachable"
	}
}

func localRuntimeReadinessSummary(target interactiveTarget) string {
	name := readinessCategoryName(target)
	if name == "" || strings.EqualFold(name, localTargetName) {
		return "local runtime ready"
	}
	return "local runtime " + name + " ready"
}

func readinessCategoryKind(target interactiveTarget) interactiveTargetKind {
	switch target.Kind {
	case interactiveTargetLocal, interactiveTargetRemote:
		return target.Kind
	}
	if isBuiltinLocalTargetName(target.Name) {
		return interactiveTargetLocal
	}
	if strings.TrimSpace(target.BaseURL) != "" {
		return targetKindForBaseURL(target.BaseURL)
	}
	return ""
}

func readinessCategoryName(target interactiveTarget) string {
	switch readinessCategoryKind(target) {
	case interactiveTargetLocal:
		label := target.label()
		if strings.TrimSpace(label) == "" {
			return localTargetName
		}
		return label
	case interactiveTargetRemote:
		if name := strings.TrimSpace(target.Name); name != "" {
			return name
		}
		if strings.TrimSpace(target.BaseURL) != "" {
			return deriveConfiguredTargetName(target.BaseURL)
		}
	}
	return strings.TrimSpace(target.Name)
}

func finalizeReadinessSnapshot(categories []replpkg.ReadinessCategory, candidates []replpkg.RecoveryCandidate, startupDiagnostics string) *replpkg.ReadinessSnapshot {
	overall := "ready"
	for _, item := range categories {
		overall = interactiveMergeOverallReadinessStatus(overall, item.Status)
	}
	return &replpkg.ReadinessSnapshot{
		OverallStatus:      overall,
		StartupDiagnostics: strings.TrimSpace(startupDiagnostics),
		Categories:         categories,
		RecoveryCandidates: candidates,
	}
}

func interactiveMergeReadinessStatus(current, incoming string) string {
	current = firstNonEmpty(strings.TrimSpace(current), "ready")
	incoming = firstNonEmpty(strings.TrimSpace(incoming), "ready")
	switch {
	case strings.EqualFold(current, "unknown") && !strings.EqualFold(incoming, "unknown"):
		return incoming
	case interactiveReadinessPriority(incoming) > interactiveReadinessPriority(current):
		return incoming
	default:
		return current
	}
}

func interactiveMergeOverallReadinessStatus(current, incoming string) string {
	current = firstNonEmpty(strings.TrimSpace(current), "ready")
	incoming = firstNonEmpty(strings.TrimSpace(incoming), "ready")
	if interactiveOverallReadinessPriority(incoming) > interactiveOverallReadinessPriority(current) {
		return incoming
	}
	return current
}

func interactiveHealthStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "blocked", "fail", "failed", "error":
		return "blocked"
	case "warn", "warning", "degraded":
		return "degraded"
	case "", "ok", "ready":
		return "ready"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func interactiveApplyReadinessCategoryUpdate(categories []replpkg.ReadinessCategory, id, status, summary string) {
	for i := range categories {
		if categories[i].ID != id {
			continue
		}
		currentStatus := firstNonEmpty(strings.TrimSpace(categories[i].Status), "unknown")
		incomingStatus := firstNonEmpty(strings.TrimSpace(status), currentStatus)
		categories[i].Status = interactiveMergeReadinessStatus(currentStatus, incomingStatus)
		summary = strings.TrimSpace(summary)
		if summary == "" {
			return
		}
		switch {
		case strings.TrimSpace(categories[i].Summary) == "":
			categories[i].Summary = summary
		case interactiveReadinessPriority(incomingStatus) > interactiveReadinessPriority(currentStatus):
			categories[i].Summary = summary
		case interactiveReadinessPriority(incomingStatus) == interactiveReadinessPriority(currentStatus):
			categories[i].Summary = summary
		case strings.EqualFold(currentStatus, "unknown"):
			categories[i].Summary = summary
		}
		return
	}
}

func interactiveReadinessPriority(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "blocked":
		return 3
	case "degraded":
		return 2
	case "ready":
		return 1
	default:
		return 0
	}
}

func interactiveOverallReadinessPriority(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "blocked":
		return 3
	case "degraded":
		return 2
	case "unknown":
		return 1
	default:
		return 0
	}
}

func embeddedStartupDiagnostics(app *bootstrap.App) string {
	if app == nil {
		return ""
	}
	var snapshot *controlplane.EffectiveConfigSnapshot
	if app.Runtime != nil {
		snapshot = app.Runtime.EffectiveConfigSnapshot()
	}
	return controlplane.BuildUserSurfaceSummary(
		snapshot,
		embeddedAuthConfigured(app),
		len(app.Config.ExecApproval.Providers),
		len(app.Config.Runtime.Governance.Adapters),
		len(app.Config.Runtime.Audit.Sinks),
	).StartupDiagnostics
}

func embeddedAuthConfigured(app *bootstrap.App) bool {
	if app == nil {
		return false
	}
	if strings.TrimSpace(app.Config.Server.AuthToken) != "" || strings.TrimSpace(app.Config.Auth.BearerToken) != "" {
		return true
	}
	return len(app.Config.Auth.APIKeys) > 0 || app.Config.Auth.JWT != nil || app.Config.Auth.OAuth2 != nil || app.Config.Auth.Session != nil
}

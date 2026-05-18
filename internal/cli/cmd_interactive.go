package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/bootstrap"
	"github.com/fulcrus/hopclaw/config"
	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/fulcrus/hopclaw/internal/version"
	"github.com/fulcrus/hopclaw/model"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	interactiveHealthTimeout = 1200 * time.Millisecond
	interactiveSSEBuffer     = 1024 * 1024
)

func runInteractive(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	stdin := cmd.InOrStdin()
	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()
	stdinFile, stdinTTY := readerFile(stdin)
	stdoutFile, stdoutTTY := writerFile(stdout)

	if flagLocal && strings.TrimSpace(flagRemote) != "" {
		return fmt.Errorf("--local and --remote cannot be used together")
	}

	initial := strings.TrimSpace(strings.Join(args, " "))
	oneShot := false
	if initial != "" {
		oneShot = true
	}

	if !stdinTTY {
		body, err := io.ReadAll(stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		bodyText := strings.TrimSpace(string(body))
		if bodyText != "" {
			if initial == "" {
				initial = bodyText
			} else {
				initial += "\n" + bodyText
			}
		}
		oneShot = true
	}
	if !stdoutTTY {
		if initial == "" && stdinTTY {
			return fmt.Errorf("interactive REPL requires a TTY stdout or a single message input")
		}
		oneShot = true
	}
	if oneShot && initial == "" {
		return fmt.Errorf("no input provided")
	}

	target, err := resolveInitialInteractiveTarget(ctx, stdin, stdout, stdinTTY && stdoutTTY && !oneShot)
	if err != nil {
		return err
	}
	sessionKey := strings.TrimSpace(flagInteractiveSession)
	if sessionKey == "" {
		sessionKey = generateInteractiveSessionKey()
	}

	connection, err := openInteractiveTarget(ctx, target, sessionKey)
	if err != nil {
		return err
	}
	defer closeInteractiveConnection(connection)

	targetManager := newInteractiveTargetManager(target)

	if err := daemon.EnsureStateDir(); err != nil {
		return fmt.Errorf("prepare state dir: %w", err)
	}
	history := replpkg.NewHistory(filepath.Join(daemon.StateDir(), "repl_history"), 500)
	promptIn := os.Stdin
	if stdinFile != nil {
		promptIn = stdinFile
	}
	promptOut := os.Stdout
	if stdoutFile != nil {
		promptOut = stdoutFile
	}
	renderer := replpkg.NewRenderer(stdout, stdoutTTY)
	if oneShot {
		renderer = replpkg.NewSplitRenderer(stderr, stdout, false)
	}

	cfg := replpkg.Config{
		Client:         connection.client,
		Service:        connection.backend,
		Target:         target.label(),
		TargetManager:  targetManager,
		Prompter:       replpkg.NewTerminalPrompter(promptIn, promptOut, history),
		Renderer:       renderer,
		History:        history,
		Version:        version.Version,
		SessionKey:     sessionKey,
		InitialMessage: initial,
		OneShot:        oneShot,
		Model:          flagInteractiveModel,
		Thinking:       flagInteractiveThink,
	}

	repl, err := replpkg.New(cfg)
	if err != nil {
		return err
	}
	return repl.Run(ctx)
}

func applyInteractiveLoggingOverrides(cfg *config.Config, target interactiveTarget) {
	if cfg == nil || flagVerbose || !isPrivateLocalInteractiveTarget(target) {
		return
	}
	cfg.Logging.ConsoleCapture = false
	cfg.Logging.SubsystemLevels = nil
	switch strings.ToLower(strings.TrimSpace(cfg.Logging.Level)) {
	case "", "debug", "info":
		cfg.Logging.Level = "warn"
	}
}

func applyInteractiveRuntimeProfileDefaults(cfg *config.Config, target interactiveTarget) {
	if cfg == nil || !isPrivateLocalInteractiveTarget(target) {
		return
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Runtime.Profile)) {
	case "", config.RuntimeProfileDesktop:
		cfg.Runtime.Profile = config.RuntimeProfileTrustedDesktop
	}
}

func newExternalInteractiveBackend(client *GatewayClient, target interactiveTarget) *interactiveBackend {
	gateway := &externalInteractiveGateway{client: client, target: target}
	return &interactiveBackend{
		gateway: gateway,
		closeFn: func(context.Context) error { return nil },
		commandsFn: func(ctx context.Context) ([]acp.Command, error) {
			return fetchSkillCommands(ctx, client)
		},
		modelsFn: func(ctx context.Context) ([]replpkg.ModelInfo, error) {
			return fetchModelInfo(ctx, client)
		},
		listSessionsFn: func(ctx context.Context) ([]replpkg.SessionSummary, error) {
			return fetchSessionSummaries(ctx, client)
		},
		getSessionFn: func(ctx context.Context, id string) (*replpkg.SessionDetail, error) {
			return fetchSessionDetail(ctx, client, id)
		},
		listApprovalsFn: func(ctx context.Context, status string, limit int) ([]replpkg.ApprovalSummary, error) {
			items, err := loadApprovals(ctx, client, status, limit)
			if err != nil {
				return nil, err
			}
			return mapApprovalSummaries(items), nil
		},
		resolveApprovalFn: func(ctx context.Context, id string, approved bool) (*replpkg.ApprovalSummary, error) {
			status := approval.StatusDenied
			if approved {
				status = approval.StatusApproved
			}
			view, err := resolveApprovalView(ctx, client, id, status, "")
			if err != nil {
				return nil, err
			}
			return mapApprovalSummary(view), nil
		},
		qualityFn: func(ctx context.Context) (*replpkg.QualitySnapshot, error) {
			summary, err := loadQualitySummary(ctx, client)
			if err != nil {
				return nil, err
			}
			report, err := loadReleaseReadiness(ctx, client)
			if err != nil {
				return nil, err
			}
			return mapQualitySnapshot(summary, report), nil
		},
		listEvalSuitesFn: func(ctx context.Context) ([]replpkg.EvalSuiteSummary, error) {
			items, err := loadEvalSuites(ctx, client)
			if err != nil {
				return nil, err
			}
			return mapEvalSuites(items), nil
		},
		runEvalSuiteFn: func(ctx context.Context, suiteID string) (*replpkg.EvalRunSummary, error) {
			report, err := runEvalSuiteReport(ctx, client, suiteID)
			if err != nil {
				return nil, err
			}
			return mapEvalRunSummary(report), nil
		},
		listRunsFn: func(ctx context.Context, sessionID string, limit int) ([]replpkg.RunSummary, error) {
			items, err := loadRunViews(ctx, client, sessionID, limit)
			if err != nil {
				return nil, err
			}
			return mapRunSummaries(items), nil
		},
		getRunDetailFn: func(ctx context.Context, id string) (*replpkg.RunDetail, error) {
			view, err := loadRunView(ctx, client, id)
			if items, listErr := loadRunViewsFiltered(ctx, client, agent.RunListFilter{
				Limit: interactiveSupervisorLimit,
			}, runtimesvc.RunListViewOptions{
				IncludeVerification:   true,
				IncludeExecutionGraph: true,
			}); listErr == nil {
				for _, candidate := range items {
					if candidate != nil && strings.TrimSpace(candidate.ID) == strings.TrimSpace(id) {
						view = candidate
						break
					}
				}
			} else if err == nil {
				err = nil
			}
			if err != nil {
				return nil, err
			}
			result, err := loadRunResult(ctx, client, id)
			if err != nil {
				result = nil
			}
			output, err := loadRunCompletionText(ctx, client, id)
			if err != nil {
				if result != nil {
					output = strings.TrimSpace(result.Output)
				}
			}
			return mapRunDetail(view, result, output), nil
		},
		doctorChecksFn: func(context.Context) ([]replpkg.DoctorCheck, error) {
			return mapDoctorChecks(collectDoctorChecks()), nil
		},
		listToolsFn: func(ctx context.Context, sessionKey string) ([]replpkg.ToolSummary, error) {
			return fetchToolSummaries(ctx, client, sessionKey)
		},
		listSkillsFn: func(ctx context.Context) ([]replpkg.SkillSummary, error) {
			return fetchInstalledSkillSummaries(ctx, client)
		},
		searchSkillCatalogFn: func(ctx context.Context, query string) ([]replpkg.SkillCatalogSummary, error) {
			return searchSkillCatalog(ctx, client, query)
		},
		getSkillFn: func(ctx context.Context, name string) (*replpkg.SkillDetail, error) {
			return fetchSkillDetail(ctx, client, name)
		},
		installSkillFn: func(ctx context.Context, source, version string) (*replpkg.SkillInstallResult, error) {
			return installSkillViaClient(ctx, client, source, version)
		},
		removeSkillFn: func(ctx context.Context, name string) error {
			return removeSkillViaClient(ctx, client, name)
		},
		supervisorFn: func(ctx context.Context) (*replpkg.SupervisorSnapshot, error) {
			return gateway.SupervisorSnapshot(ctx)
		},
		runDeliveryFn: func(ctx context.Context, runID string) (*replpkg.RunDeliveryDetail, error) {
			return gateway.GetRunDelivery(ctx, runID)
		},
		readinessFn: func(ctx context.Context) (*replpkg.ReadinessSnapshot, error) {
			return gateway.ReadinessSnapshot(ctx)
		},
		recoveryFn: func(ctx context.Context) ([]replpkg.RecoveryCandidate, error) {
			return gateway.RecoveryCandidates(ctx)
		},
		listAutomationsFn: func(ctx context.Context, limit int) ([]replpkg.AutomationItem, error) {
			return gateway.ListAutomations(ctx, limit)
		},
		createAutomationFn: func(ctx context.Context, req replpkg.AutomationCreateRequest) (*replpkg.AutomationItem, error) {
			return gateway.CreateAutomation(ctx, req)
		},
		pauseAutomationFn: func(ctx context.Context, kind, id string) error {
			return gateway.PauseAutomation(ctx, kind, id)
		},
		resumeAutomationFn: func(ctx context.Context, kind, id string) error {
			return gateway.ResumeAutomation(ctx, kind, id)
		},
		runAutomationNowFn: func(ctx context.Context, kind, id string) error {
			return gateway.RunAutomationNow(ctx, kind, id)
		},
		listMemoryFn: func(ctx context.Context, query string, limit int) ([]agent.MemoryEntry, error) {
			return listMemoryEntries(ctx, client, query, limit)
		},
		getMemoryFn: func(ctx context.Context, key string) (*agent.MemoryEntry, error) {
			return getMemoryEntry(ctx, client, key)
		},
		saveMemoryFn: func(ctx context.Context, key, value, label, sessionKey, projectID string) (*agent.MemoryEntry, error) {
			return upsertMemoryEntry(ctx, client, key, value, label, sessionKey, projectID)
		},
		deleteMemoryFn: func(ctx context.Context, key string) error {
			return deleteMemoryEntry(ctx, client, key)
		},
		recallMemoriesFn: func(ctx context.Context, sessionKey, projectID string) ([]agent.MemoryEntry, error) {
			var resp struct {
				Items []agent.MemoryEntry `json:"items"`
			}
			if err := client.Get(ctx, "/runtime/memory", &resp); err != nil {
				return nil, err
			}
			return agent.RecallForContext(resp.Items, sessionKey, projectID).Memories, nil
		},
		memoryUsageFn: func(ctx context.Context, sessionID string) ([]replpkg.MemoryUsageItem, error) {
			return gateway.MemoryUsedInContext(ctx, sessionID)
		},
		contextPressureFn: func(ctx context.Context, sessionID string) (*replpkg.ContextPressureInfo, error) {
			return gateway.ContextPressure(ctx, sessionID)
		},
		findProjectFn: func(_ context.Context, directory string) (*agent.Project, error) {
			return interactiveProjectFromDirectory(directory)
		},
		startEpisodeFn: func(ctx context.Context, sessionID string) error {
			return client.Post(ctx, "/runtime/sessions/"+sessionID+"/episode", map[string]any{}, nil)
		},
		resetSessionFn: func(ctx context.Context, sessionKey string) error {
			return resetSessionByKey(ctx, client, sessionKey)
		},
		compactSessionFn: func(ctx context.Context, sessionID string) error {
			return client.Post(ctx, "/runtime/sessions/"+sessionID+"/compact", map[string]any{}, nil)
		},
		resolvePermissionFn: func(ctx context.Context, req acp.PermissionRequest, decision replpkg.PermissionDecision) error {
			status := approval.StatusDenied
			if decision.Approved {
				status = approval.StatusApproved
			}
			return client.Post(ctx, "/runtime/approvals/"+req.RequestID+"/resolve", approval.Resolution{
				Status: status,
				Scope:  approval.Scope(strings.TrimSpace(decision.Scope)),
			}, nil)
		},
	}
}

func newEmbeddedInteractiveBackend(app *bootstrap.App, target interactiveTarget) *interactiveBackend {
	gateway := &embeddedInteractiveGateway{app: app, target: target}
	return &interactiveBackend{
		gateway: gateway,
		closeFn: func(ctx context.Context) error {
			return app.Close(ctx)
		},
		commandsFn: func(context.Context) ([]acp.Command, error) {
			return embeddedSkillCommands(app), nil
		},
		modelsFn: func(context.Context) ([]replpkg.ModelInfo, error) {
			return embeddedModelInfo(app), nil
		},
		listSessionsFn: func(ctx context.Context) ([]replpkg.SessionSummary, error) {
			items, err := app.Runtime.ListSessions(ctx)
			if err != nil {
				return nil, err
			}
			return mapSessionSummaries(items), nil
		},
		getSessionFn: func(ctx context.Context, id string) (*replpkg.SessionDetail, error) {
			session, err := app.Runtime.GetSession(ctx, id)
			if err != nil {
				return nil, err
			}
			return mapSessionDetail(session), nil
		},
		listApprovalsFn: func(ctx context.Context, status string, limit int) ([]replpkg.ApprovalSummary, error) {
			items, err := app.Runtime.ListApprovalViewsFiltered(ctx, approval.ListFilter{
				Status: approval.Status(strings.TrimSpace(status)),
				Limit:  limit,
			}, agent.ScopeFilter{})
			if err != nil {
				return nil, err
			}
			return mapApprovalSummaries(items), nil
		},
		resolveApprovalFn: func(ctx context.Context, id string, approved bool) (*replpkg.ApprovalSummary, error) {
			status := approval.StatusDenied
			if approved {
				status = approval.StatusApproved
			}
			view, err := app.Runtime.ResolveApprovalView(ctx, id, approval.Resolution{
				Status:     status,
				ResolvedBy: "cli",
			})
			if err != nil {
				return nil, err
			}
			return mapApprovalSummary(view), nil
		},
		qualityFn: func(ctx context.Context) (*replpkg.QualitySnapshot, error) {
			summary, err := app.Runtime.GetQualitySummary(ctx, runtimesvc.QualitySummaryRequest{})
			if err != nil {
				return nil, err
			}
			report, err := app.Runtime.GetReleaseReadiness(ctx, runtimesvc.ReleaseReadinessRequest{})
			if err != nil {
				return nil, err
			}
			return mapQualitySnapshot(summary, report), nil
		},
		listEvalSuitesFn: func(context.Context) ([]replpkg.EvalSuiteSummary, error) {
			return mapEvalSuites(app.Runtime.ListEvalSuites()), nil
		},
		runEvalSuiteFn: func(ctx context.Context, suiteID string) (*replpkg.EvalRunSummary, error) {
			report, err := app.Runtime.RunEvalSuite(ctx, runtimesvc.EvalRunRequest{SuiteID: strings.TrimSpace(suiteID)})
			if err != nil {
				return nil, err
			}
			return mapEvalRunSummary(report), nil
		},
		listRunsFn: func(ctx context.Context, sessionID string, limit int) ([]replpkg.RunSummary, error) {
			items, err := app.Runtime.ListRunViews(ctx, agent.RunListFilter{
				SessionID: strings.TrimSpace(sessionID),
				Limit:     limit,
			}, runtimesvc.RunListViewOptions{})
			if err != nil {
				return nil, err
			}
			return mapRunSummaries(items), nil
		},
		getRunDetailFn: func(ctx context.Context, id string) (*replpkg.RunDetail, error) {
			run, err := app.Runtime.GetRun(ctx, id)
			if err != nil {
				return nil, err
			}
			views := app.Runtime.BuildRunViews(ctx, []*agent.Run{run}, runtimesvc.RunListViewOptions{
				IncludeExecutionGraph: true,
			})
			if len(views) == 0 || views[0] == nil {
				return nil, fmt.Errorf("run %s view unavailable", id)
			}
			result, err := app.Runtime.GetRunResult(ctx, id)
			if err != nil {
				result = nil
			}
			output := ""
			if completion, err := app.Runtime.GetRunCompletion(ctx, id); err == nil && completion != nil {
				if completion.Bundle != nil {
					output = strings.TrimSpace(completion.Bundle.FinalText)
				}
				if output == "" && completion.Result != nil {
					output = strings.TrimSpace(completion.Result.Output)
				}
			}
			if output == "" && result != nil {
				output = strings.TrimSpace(result.Output)
			}
			return mapRunDetail(views[0], result, output), nil
		},
		doctorChecksFn: func(context.Context) ([]replpkg.DoctorCheck, error) {
			return mapDoctorChecks(collectDoctorChecks()), nil
		},
		listToolsFn: func(ctx context.Context, sessionKey string) ([]replpkg.ToolSummary, error) {
			return embeddedToolSummaries(ctx, app, sessionKey)
		},
		listSkillsFn: func(context.Context) ([]replpkg.SkillSummary, error) {
			return embeddedInstalledSkillSummaries(app)
		},
		searchSkillCatalogFn: func(ctx context.Context, query string) ([]replpkg.SkillCatalogSummary, error) {
			return embeddedSkillCatalogSummaries(ctx, app, query)
		},
		getSkillFn: func(ctx context.Context, name string) (*replpkg.SkillDetail, error) {
			return embeddedSkillDetail(ctx, app, name)
		},
		installSkillFn: func(ctx context.Context, source, version string) (*replpkg.SkillInstallResult, error) {
			return embeddedInstallSkill(ctx, app, source, version)
		},
		removeSkillFn: func(ctx context.Context, name string) error {
			return embeddedRemoveSkill(ctx, app, name)
		},
		supervisorFn: func(ctx context.Context) (*replpkg.SupervisorSnapshot, error) {
			return gateway.SupervisorSnapshot(ctx)
		},
		runDeliveryFn: func(ctx context.Context, runID string) (*replpkg.RunDeliveryDetail, error) {
			return gateway.GetRunDelivery(ctx, runID)
		},
		readinessFn: func(ctx context.Context) (*replpkg.ReadinessSnapshot, error) {
			return gateway.ReadinessSnapshot(ctx)
		},
		recoveryFn: func(ctx context.Context) ([]replpkg.RecoveryCandidate, error) {
			return gateway.RecoveryCandidates(ctx)
		},
		listAutomationsFn: func(ctx context.Context, limit int) ([]replpkg.AutomationItem, error) {
			return gateway.ListAutomations(ctx, limit)
		},
		createAutomationFn: func(ctx context.Context, req replpkg.AutomationCreateRequest) (*replpkg.AutomationItem, error) {
			return gateway.CreateAutomation(ctx, req)
		},
		pauseAutomationFn: func(ctx context.Context, kind, id string) error {
			return gateway.PauseAutomation(ctx, kind, id)
		},
		resumeAutomationFn: func(ctx context.Context, kind, id string) error {
			return gateway.ResumeAutomation(ctx, kind, id)
		},
		runAutomationNowFn: func(ctx context.Context, kind, id string) error {
			return gateway.RunAutomationNow(ctx, kind, id)
		},
		listMemoryFn: func(ctx context.Context, query string, limit int) ([]agent.MemoryEntry, error) {
			items, err := app.Runtime.ListMemoryFiltered(ctx, agent.MemoryFilter{
				Query: strings.TrimSpace(query),
			})
			if err != nil {
				return nil, err
			}
			if limit > 0 && len(items) > limit {
				items = items[:limit]
			}
			return items, nil
		},
		getMemoryFn: func(ctx context.Context, key string) (*agent.MemoryEntry, error) {
			return app.Runtime.GetMemory(ctx, strings.TrimSpace(key))
		},
		saveMemoryFn: func(ctx context.Context, key, value, label, sessionKey, projectID string) (*agent.MemoryEntry, error) {
			return app.Runtime.UpsertMemoryRecord(ctx, agent.MemoryRecord{
				Key:        strings.TrimSpace(key),
				Label:      strings.TrimSpace(label),
				Value:      strings.TrimSpace(value),
				Namespace:  "project",
				Source:     "user",
				SessionKey: strings.TrimSpace(sessionKey),
				ProjectID:  strings.TrimSpace(projectID),
			})
		},
		deleteMemoryFn: func(ctx context.Context, key string) error {
			return app.Runtime.DeleteMemory(ctx, strings.TrimSpace(key))
		},
		recallMemoriesFn: func(ctx context.Context, sessionKey, projectID string) ([]agent.MemoryEntry, error) {
			entries, err := app.Runtime.ListMemory(ctx)
			if err != nil {
				return nil, err
			}
			return agent.RecallForContext(entries, sessionKey, projectID).Memories, nil
		},
		memoryUsageFn: func(ctx context.Context, sessionID string) ([]replpkg.MemoryUsageItem, error) {
			return gateway.MemoryUsedInContext(ctx, sessionID)
		},
		contextPressureFn: func(ctx context.Context, sessionID string) (*replpkg.ContextPressureInfo, error) {
			return gateway.ContextPressure(ctx, sessionID)
		},
		findProjectFn: func(_ context.Context, directory string) (*agent.Project, error) {
			return interactiveProjectFromDirectory(directory)
		},
		startEpisodeFn: func(ctx context.Context, sessionID string) error {
			_, err := app.Runtime.StartNewEpisode(ctx, sessionID)
			return err
		},
		resetSessionFn: func(ctx context.Context, sessionKey string) error {
			session, err := agent.LoadSessionMetadataByKey(ctx, app.Sessions, sessionKey, agent.ScopeFilter{})
			if err != nil {
				return err
			}
			return app.Runtime.DeleteSession(ctx, session.ID)
		},
		compactSessionFn: func(ctx context.Context, sessionID string) error {
			_, err := app.Runtime.CompactSession(ctx, sessionID)
			return err
		},
		resolvePermissionFn: func(ctx context.Context, req acp.PermissionRequest, decision replpkg.PermissionDecision) error {
			status := approval.StatusDenied
			if decision.Approved {
				status = approval.StatusApproved
			}
			_, err := app.Runtime.ResolveApprovalView(ctx, req.RequestID, approval.Resolution{
				Status: status,
				Scope:  approval.Scope(strings.TrimSpace(decision.Scope)),
			})
			return err
		},
	}
}

func interactiveProjectFromDirectory(directory string) (*agent.Project, error) {
	dir := strings.TrimSpace(directory)
	if dir == "" {
		return nil, nil
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	absDir = filepath.Clean(absDir)
	name := filepath.Base(absDir)
	if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
		name = absDir
	}
	now := time.Now().UTC()
	return &agent.Project{
		ID:        agent.ProjectID(absDir),
		Name:      name,
		Directory: absDir,
		CreatedAt: now,
		LastUsed:  now,
	}, nil
}

func gatewayHealthy(ctx context.Context, client *GatewayClient) bool {
	checkCtx, cancel := context.WithTimeout(ctx, interactiveHealthTimeout)
	defer cancel()

	oldTimeout := client.HTTP.Timeout
	client.HTTP.Timeout = interactiveHealthTimeout
	defer func() { client.HTTP.Timeout = oldTimeout }()

	_, status, err := client.GetRawWithStatus(checkCtx, "/healthz")
	return err == nil && status < 400
}

func loadInteractiveConfig() (config.Config, error) {
	configPath := resolveConfigPath()
	if configPath == "" {
		if !config.HasAPIKey() {
			return config.Config{}, handleNoConfig()
		}
		if err := daemon.EnsureStateDir(); err != nil {
			return config.Config{}, fmt.Errorf("create state dir: %w", err)
		}
		configPath = daemon.ConfigFilePath()
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			content := config.GenerateDefaultConfig()
			if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
				return config.Config{}, fmt.Errorf("create config dir: %w", err)
			}
			if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
				return config.Config{}, fmt.Errorf("write config: %w", err)
			}
		}
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return config.Config{}, fmt.Errorf("load config %s: %w", configPath, err)
	}
	cfg.Server.Version = version.Version
	return cfg, nil
}

func fetchSkillCommands(ctx context.Context, client *GatewayClient) ([]acp.Command, error) {
	var response struct {
		Items []struct {
			Name          string `json:"name"`
			Summary       string `json:"summary"`
			UserInvocable *bool  `json:"user_invocable,omitempty"`
		} `json:"items"`
	}
	if err := client.Get(ctx, "/operator/skills", &response); err != nil {
		return nil, err
	}
	out := make([]acp.Command, 0, len(response.Items))
	for _, item := range response.Items {
		if item.UserInvocable != nil && !*item.UserInvocable {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		description := strings.TrimSpace(item.Summary)
		if description == "" {
			description = "Run installed skill " + name
		}
		out = append(out, acp.Command{Name: name, Description: description, Shortcut: "/" + name})
	}
	return out, nil
}

func embeddedSkillCommands(app *bootstrap.App) []acp.Command {
	if app == nil || app.SkillService == nil {
		return nil
	}
	snapshot := app.SkillService.Snapshot()
	out := make([]acp.Command, 0, len(snapshot.Ordered))
	for _, item := range snapshot.Ordered {
		if item == nil || !item.Prompt.UserInvocable {
			continue
		}
		description := strings.TrimSpace(item.Prompt.Description)
		if description == "" {
			description = "Run installed skill " + item.Name()
		}
		out = append(out, acp.Command{
			Name:        item.Name(),
			Description: description,
			Shortcut:    "/" + item.Name(),
		})
	}
	return out
}

func fetchModelInfo(ctx context.Context, client *GatewayClient) ([]replpkg.ModelInfo, error) {
	var response struct {
		Providers []providerInfoView `json:"providers"`
	}
	if err := client.Get(ctx, "/operator/models", &response); err != nil {
		return nil, err
	}
	seen := make(map[string]replpkg.ModelInfo)
	for _, provider := range response.Providers {
		collectProviderModelInfo(seen, provider)
	}
	return sortedModelInfo(seen), nil
}

func embeddedModelInfo(app *bootstrap.App) []replpkg.ModelInfo {
	if app == nil {
		return nil
	}
	providers, _, err := buildCLIModelProviders(app.Config.Models)
	if err != nil {
		return nil
	}
	seen := make(map[string]replpkg.ModelInfo)
	for name, entry := range providers {
		for _, meta := range model.ModelsForProvider(name) {
			addModelInfo(seen, replpkg.ModelInfo{
				ID:               meta.Model,
				ContextWindow:    meta.ContextWindow,
				SupportsThinking: model.HasCapability(meta, model.CapReasoning),
			})
		}
		if strings.TrimSpace(entry.DefaultModel) == "" {
			continue
		}
		matrix := model.CapabilityMatrixForProvider(name, entry)
		addModelInfo(seen, replpkg.ModelInfo{
			ID:               matrix.Model,
			ContextWindow:    matrix.ContextWindow,
			SupportsThinking: matrix.SupportsReasoning,
		})
	}
	return sortedModelInfo(seen)
}

type providerModelItem struct {
	Model         string                  `json:"model"`
	ContextWindow int                     `json:"context_window"`
	Capabilities  []model.ModelCapability `json:"capabilities"`
}

type capabilityMatrixItem struct {
	Model             string `json:"model"`
	ContextWindow     int    `json:"context_window"`
	SupportsReasoning bool   `json:"supports_reasoning"`
}

type providerInfoView struct {
	DefaultModel     string               `json:"default_model"`
	Models           []providerModelItem  `json:"models"`
	CapabilityMatrix capabilityMatrixItem `json:"capability_matrix"`
}

func collectProviderModelInfo(seen map[string]replpkg.ModelInfo, provider providerInfoView) {
	for _, item := range provider.Models {
		addModelInfo(seen, replpkg.ModelInfo{
			ID:               item.Model,
			ContextWindow:    item.ContextWindow,
			SupportsThinking: slicesContainsCapability(item.Capabilities, model.CapReasoning),
		})
	}
	matrix := provider.CapabilityMatrix
	if strings.TrimSpace(matrix.Model) != "" {
		addModelInfo(seen, replpkg.ModelInfo{
			ID:               matrix.Model,
			ContextWindow:    matrix.ContextWindow,
			SupportsThinking: matrix.SupportsReasoning,
		})
		return
	}
	if defaultModel := strings.TrimSpace(provider.DefaultModel); defaultModel != "" {
		addModelInfo(seen, replpkg.ModelInfo{ID: defaultModel})
	}
}

func addModelInfo(seen map[string]replpkg.ModelInfo, item replpkg.ModelInfo) {
	id := strings.TrimSpace(item.ID)
	if id == "" {
		return
	}
	current := seen[id]
	current.ID = id
	if item.ContextWindow > current.ContextWindow {
		current.ContextWindow = item.ContextWindow
	}
	current.SupportsThinking = current.SupportsThinking || item.SupportsThinking
	seen[id] = current
}

func sortedModelInfo(seen map[string]replpkg.ModelInfo) []replpkg.ModelInfo {
	out := make([]replpkg.ModelInfo, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	slices.SortFunc(out, func(a, b replpkg.ModelInfo) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func slicesContainsCapability(items []model.ModelCapability, want model.ModelCapability) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func readerFile(reader io.Reader) (*os.File, bool) {
	file, ok := reader.(*os.File)
	return file, ok && term.IsTerminal(int(file.Fd()))
}

func writerFile(writer io.Writer) (*os.File, bool) {
	file, ok := writer.(*os.File)
	return file, ok && term.IsTerminal(int(file.Fd()))
}

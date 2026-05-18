package repl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

type scriptedPrompter struct {
	lines     []string
	approvals []rune
	secrets   []string
}

type panelAwarePrompter struct {
	scriptedPrompter
	overlay richedit.OverlayController
}

func (p *panelAwarePrompter) SetOverlayController(controller richedit.OverlayController) {
	p.overlay = controller
}

func (p *scriptedPrompter) ReadLine(_ string, _ *CommandRegistry) (string, error) {
	if len(p.lines) == 0 {
		return "", io.EOF
	}
	line := p.lines[0]
	p.lines = p.lines[1:]
	if line == "<quit>" {
		return "", ErrPromptQuit
	}
	if line == "<interrupt>" {
		return "", ErrPromptInterrupted
	}
	return line, nil
}

func (p *scriptedPrompter) ReadRichLine(prompt string, registry *CommandRegistry) (RichReadResult, error) {
	line, err := p.ReadLine(prompt, registry)
	return RichReadResult{Text: line}, err
}

func (p *scriptedPrompter) ReadApproval(_ string) (rune, error) {
	if len(p.approvals) == 0 {
		return 0, errors.New("no approval input")
	}
	value := p.approvals[0]
	p.approvals = p.approvals[1:]
	return value, nil
}

func (p *scriptedPrompter) ReadSecret(_ string) (string, error) {
	if len(p.secrets) == 0 {
		return "", errors.New("no secret input")
	}
	value := p.secrets[0]
	p.secrets = p.secrets[1:]
	return value, nil
}

type fakeService struct {
	commands           []acp.Command
	models             []ModelInfo
	sessions           []SessionSummary
	detail             *SessionDetail
	sessionByID        map[string]*SessionDetail
	approvals          []ApprovalSummary
	quality            *QualitySnapshot
	evals              []EvalSuiteSummary
	evalRun            *EvalRunSummary
	runs               []RunSummary
	runsBySession      map[string][]RunSummary
	runDetails         map[string]*RunDetail
	doctorChecks       []DoctorCheck
	tools              []ToolSummary
	skills             []SkillSummary
	skillCatalog       []SkillCatalogSummary
	skillDetail        *SkillDetail
	installedSkills    []SkillInstallResult
	removedSkills      []string
	supervisor         *SupervisorSnapshot
	runDelivery        map[string]*RunDeliveryDetail
	governanceItems    []DeliveryListItem
	redriveResult      *RedriveResult
	readiness          *ReadinessSnapshot
	recovery           []RecoveryCandidate
	automations        []AutomationItem
	automationDetail   map[string]*AutomationItem
	createdAutomations []AutomationCreateRequest
	pausedAutomations  []string
	resumedAutomations []string
	ranAutomations     []string
	memoryUsage        []MemoryUsageItem
	contextPressure    *ContextPressureInfo
	memoryItems        []agent.MemoryEntry
	memories           []agent.MemoryEntry
	memoryConflicts    []agent.MemoryEntry
	pendingMemory      []agent.MemoryEntry
	project            *agent.Project
	projectByDirectory map[string]*agent.Project
	startEpisodeCalled bool
	startEpisodeID     string
	compactCalled      bool
	compactSessionID   string
	resetCalled        bool
	resolvedApprovals  []PermissionDecision
	closed             bool
	listRunsRequests   []string
	getSessionRequests []string
}

func (s *fakeService) Commands(context.Context) ([]acp.Command, error) { return s.commands, nil }
func (s *fakeService) Models(context.Context) ([]ModelInfo, error)     { return s.models, nil }
func (s *fakeService) ListSessions(context.Context) ([]SessionSummary, error) {
	return s.sessions, nil
}
func (s *fakeService) GetSession(_ context.Context, id string) (*SessionDetail, error) {
	s.getSessionRequests = append(s.getSessionRequests, id)
	if s.sessionByID != nil {
		detail, ok := s.sessionByID[id]
		if !ok {
			return nil, fmt.Errorf("session %s not found", id)
		}
		return detail, nil
	}
	return s.detail, nil
}
func (s *fakeService) ListApprovals(_ context.Context, status string, limit int) ([]ApprovalSummary, error) {
	items := make([]ApprovalSummary, 0, len(s.approvals))
	for _, item := range s.approvals {
		if strings.TrimSpace(status) != "" && !strings.EqualFold(strings.TrimSpace(item.Status), strings.TrimSpace(status)) {
			continue
		}
		items = append(items, item)
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
func (s *fakeService) ResolveApproval(_ context.Context, id string, approved bool) (*ApprovalSummary, error) {
	status := "denied"
	if approved {
		status = "approved"
	}
	for i := range s.approvals {
		if s.approvals[i].ID == id {
			s.approvals[i].Status = status
			item := s.approvals[i]
			return &item, nil
		}
	}
	return &ApprovalSummary{ID: id, Status: status}, nil
}
func (s *fakeService) QualitySnapshot(context.Context) (*QualitySnapshot, error) {
	return s.quality, nil
}
func (s *fakeService) ListEvalSuites(context.Context) ([]EvalSuiteSummary, error) {
	return append([]EvalSuiteSummary(nil), s.evals...), nil
}
func (s *fakeService) RunEvalSuite(context.Context, string) (*EvalRunSummary, error) {
	return s.evalRun, nil
}
func (s *fakeService) ListRuns(_ context.Context, sessionID string, limit int) ([]RunSummary, error) {
	s.listRunsRequests = append(s.listRunsRequests, sessionID)
	if s.runsBySession != nil {
		items := append([]RunSummary(nil), s.runsBySession[sessionID]...)
		if limit > 0 && len(items) > limit {
			items = items[:limit]
		}
		return items, nil
	}
	return append([]RunSummary(nil), s.runs...), nil
}
func (s *fakeService) GetRunDetail(_ context.Context, id string) (*RunDetail, error) {
	if s.runDetails == nil {
		return nil, nil
	}
	return s.runDetails[id], nil
}
func (s *fakeService) DoctorChecks(context.Context) ([]DoctorCheck, error) {
	return append([]DoctorCheck(nil), s.doctorChecks...), nil
}
func (s *fakeService) ListTools(_ context.Context, _ string) ([]ToolSummary, error) {
	return append([]ToolSummary(nil), s.tools...), nil
}
func (s *fakeService) ListSkills(context.Context) ([]SkillSummary, error) {
	return append([]SkillSummary(nil), s.skills...), nil
}
func (s *fakeService) SearchSkillCatalog(_ context.Context, query string) ([]SkillCatalogSummary, error) {
	if strings.TrimSpace(query) == "" {
		return append([]SkillCatalogSummary(nil), s.skillCatalog...), nil
	}
	items := make([]SkillCatalogSummary, 0, len(s.skillCatalog))
	for _, item := range s.skillCatalog {
		haystack := strings.ToLower(strings.Join([]string{item.ID, item.Name, item.Summary, item.Description}, " "))
		if strings.Contains(haystack, strings.ToLower(strings.TrimSpace(query))) {
			items = append(items, item)
		}
	}
	return items, nil
}
func (s *fakeService) GetSkill(_ context.Context, name string) (*SkillDetail, error) {
	if s.skillDetail != nil {
		return s.skillDetail, nil
	}
	detail := &SkillDetail{}
	for _, item := range s.skills {
		if strings.EqualFold(strings.TrimSpace(item.ID), strings.TrimSpace(name)) || strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(name)) {
			copy := item
			detail.Installed = &copy
			break
		}
	}
	for _, item := range s.skillCatalog {
		if strings.EqualFold(strings.TrimSpace(item.ID), strings.TrimSpace(name)) || strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(name)) {
			copy := item
			detail.Catalog = &copy
			break
		}
	}
	if detail.Installed == nil && detail.Catalog == nil {
		return nil, nil
	}
	return detail, nil
}
func (s *fakeService) InstallSkill(_ context.Context, source, version string) (*SkillInstallResult, error) {
	result := SkillInstallResult{
		SkillID:            strings.TrimSpace(source),
		Version:            strings.TrimSpace(version),
		InstallDir:         "/tmp/" + strings.TrimSpace(source),
		LockFile:           "/tmp/skills.lock.json",
		InstallerStepCount: 1,
	}
	s.installedSkills = append(s.installedSkills, result)
	return &result, nil
}
func (s *fakeService) RemoveSkill(_ context.Context, name string) error {
	s.removedSkills = append(s.removedSkills, strings.TrimSpace(name))
	return nil
}
func (s *fakeService) SupervisorSnapshot(context.Context) (*SupervisorSnapshot, error) {
	return s.supervisor, nil
}
func (s *fakeService) GetRunDelivery(_ context.Context, runID string) (*RunDeliveryDetail, error) {
	if s.runDelivery == nil {
		return nil, nil
	}
	return s.runDelivery[runID], nil
}
func (s *fakeService) ListGovernanceDeliveries(_ context.Context, query string, limit int) ([]DeliveryListItem, error) {
	items := make([]DeliveryListItem, 0, len(s.governanceItems))
	query = strings.ToLower(strings.TrimSpace(query))
	for _, item := range s.governanceItems {
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{item.ID, item.RunID, item.AdapterName, item.Status, item.Summary}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		items = append(items, item)
		if limit > 0 && len(items) >= limit {
			break
		}
	}
	return items, nil
}
func (s *fakeService) RedriveDelivery(context.Context, string) (*RedriveResult, error) {
	if s.redriveResult == nil {
		return &RedriveResult{}, nil
	}
	result := *s.redriveResult
	return &result, nil
}
func (s *fakeService) GetAutomationDetail(_ context.Context, kind, id string) (*AutomationItem, error) {
	if s.automationDetail == nil {
		return nil, nil
	}
	if item := s.automationDetail[strings.TrimSpace(kind)+":"+strings.TrimSpace(id)]; item != nil {
		copyItem := *item
		return &copyItem, nil
	}
	if item := s.automationDetail[strings.TrimSpace(id)]; item != nil {
		copyItem := *item
		return &copyItem, nil
	}
	return nil, nil
}
func (s *fakeService) ListMemoryConflicts(context.Context) ([]agent.MemoryEntry, error) {
	return append([]agent.MemoryEntry(nil), s.memoryConflicts...), nil
}
func (s *fakeService) ListPendingMemoryWrites(context.Context) ([]agent.MemoryEntry, error) {
	return append([]agent.MemoryEntry(nil), s.pendingMemory...), nil
}
func (s *fakeService) ResolveMemoryConflict(context.Context, string, string) error {
	return nil
}
func (s *fakeService) ReadinessSnapshot(context.Context) (*ReadinessSnapshot, error) {
	return s.readiness, nil
}
func (s *fakeService) RecoveryCandidates(context.Context) ([]RecoveryCandidate, error) {
	if len(s.recovery) > 0 {
		return append([]RecoveryCandidate(nil), s.recovery...), nil
	}
	items := make([]RecoveryCandidate, 0, len(s.runs))
	for _, item := range s.runs {
		if !item.Resumable && normalizedExecutionState(firstNonEmpty(item.Status, item.Phase)) != "paused" {
			continue
		}
		items = append(items, RecoveryCandidate{
			Type:    "paused",
			ID:      item.ID,
			Summary: firstNonEmpty(item.Error, item.Status, item.Phase, "paused work"),
			Action:  "continue",
		})
	}
	return items, nil
}
func (s *fakeService) ListAutomations(context.Context, int) ([]AutomationItem, error) {
	return append([]AutomationItem(nil), s.automations...), nil
}
func (s *fakeService) CreateAutomation(_ context.Context, req AutomationCreateRequest) (*AutomationItem, error) {
	s.createdAutomations = append(s.createdAutomations, req)
	item := AutomationItem{
		ID:       fmt.Sprintf("cron-%d", len(s.createdAutomations)),
		Name:     req.Name,
		Kind:     firstNonEmpty(req.Kind, "cron"),
		Status:   "ready",
		Schedule: firstNonEmpty(req.Expression, req.Every, req.At, req.ScheduleKind),
		Delivery: req.Delivery,
		NextRun:  "scheduled",
		Health:   "healthy",
	}
	s.automations = append(s.automations, item)
	return &item, nil
}
func (s *fakeService) PauseAutomation(_ context.Context, kind, id string) error {
	s.pausedAutomations = append(s.pausedAutomations, kind+":"+id)
	return nil
}
func (s *fakeService) ResumeAutomation(_ context.Context, kind, id string) error {
	s.resumedAutomations = append(s.resumedAutomations, kind+":"+id)
	return nil
}
func (s *fakeService) RunAutomationNow(_ context.Context, kind, id string) error {
	s.ranAutomations = append(s.ranAutomations, kind+":"+id)
	return nil
}
func (s *fakeService) ListMemory(_ context.Context, query string, limit int) ([]agent.MemoryEntry, error) {
	items := make([]agent.MemoryEntry, 0, len(s.memoryItems))
	query = strings.ToLower(strings.TrimSpace(query))
	for _, item := range s.memoryItems {
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{item.Key, item.Label, item.Value, item.Source}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		items = append(items, item)
		if limit > 0 && len(items) >= limit {
			break
		}
	}
	return items, nil
}
func (s *fakeService) GetMemory(_ context.Context, key string) (*agent.MemoryEntry, error) {
	for _, item := range s.memoryItems {
		if item.Key == key {
			copyItem := item
			return &copyItem, nil
		}
	}
	return nil, nil
}
func (s *fakeService) SaveMemory(_ context.Context, key, value, label, sessionKey, projectID string) (*agent.MemoryEntry, error) {
	if strings.TrimSpace(key) == "" {
		key = fmt.Sprintf("memory-%d", len(s.memoryItems)+1)
	}
	entry := agent.MemoryEntry{
		Key:        key,
		Value:      value,
		Label:      label,
		Source:     "user",
		SessionKey: sessionKey,
		ProjectID:  projectID,
		UpdatedAt:  time.Now().UTC(),
	}
	s.memoryItems = append(s.memoryItems, entry)
	return &entry, nil
}
func (s *fakeService) DeleteMemory(_ context.Context, key string) error {
	filtered := s.memoryItems[:0]
	for _, item := range s.memoryItems {
		if item.Key == key {
			continue
		}
		filtered = append(filtered, item)
	}
	s.memoryItems = filtered
	return nil
}
func (s *fakeService) findMemory(key string) (int, *agent.MemoryEntry) {
	for index, item := range s.memoryItems {
		if item.Key == key {
			copyItem := item
			return index, &copyItem
		}
	}
	return -1, nil
}
func (s *fakeService) RecallMemories(_ context.Context, _, _ string) ([]agent.MemoryEntry, error) {
	return append([]agent.MemoryEntry(nil), s.memories...), nil
}
func (s *fakeService) MemoryUsedInContext(context.Context, string) ([]MemoryUsageItem, error) {
	return append([]MemoryUsageItem(nil), s.memoryUsage...), nil
}
func (s *fakeService) ContextPressure(context.Context, string) (*ContextPressureInfo, error) {
	return s.contextPressure, nil
}
func (s *fakeService) FindOrCreateProject(_ context.Context, directory string) (*agent.Project, error) {
	if s.projectByDirectory != nil {
		if project := s.projectByDirectory[directory]; project != nil {
			return project, nil
		}
		if resolved, err := filepath.EvalSymlinks(directory); err == nil {
			if project := s.projectByDirectory[resolved]; project != nil {
				return project, nil
			}
		}
		if cleaned := filepath.Clean(directory); cleaned != directory {
			if project := s.projectByDirectory[cleaned]; project != nil {
				return project, nil
			}
		}
		for key, project := range s.projectByDirectory {
			resolvedKey, err := filepath.EvalSymlinks(key)
			if err == nil && resolvedKey == directory {
				return project, nil
			}
			if filepath.Clean(key) == filepath.Clean(directory) {
				return project, nil
			}
		}
	}
	return s.project, nil
}

func resolvedTestDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err == nil {
		return resolved
	}
	absDir, err := filepath.Abs(dir)
	if err == nil {
		return absDir
	}
	return filepath.Clean(dir)
}

func TestRenderBriefingShowsProjectAndMemories(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q) error = %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	actualDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	project := &agent.Project{ID: "proj-hopclaw", Name: "hopclaw", Directory: actualDir}
	service := &fakeService{
		projectByDirectory: map[string]*agent.Project{resolvedTestDir(t, actualDir): project},
		memories: []agent.MemoryEntry{
			{Value: "Uses Go", Source: agent.MemorySourceUser},
			{Value: "Run tests before build", Label: "workflow", Source: agent.MemorySourceAgent},
		},
	}
	var output strings.Builder
	repl := &REPL{
		service:    service,
		sessionKey: "session-alpha",
		renderer:   NewRenderer(&output, false),
	}

	repl.renderBriefing(context.Background())

	rendered := output.String()
	if strings.TrimSpace(rendered) != "" {
		t.Fatalf("quiet briefing should not print idle startup noise: %q", rendered)
	}
	if repl.currentProject != project {
		t.Fatalf("currentProject not updated: %#v", repl.currentProject)
	}
}

func TestRenderBriefingShowsRecoveryHintForPausedRunAndDegradedHealth(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q) error = %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	actualDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	project := &agent.Project{ID: "proj-ops", Name: "ops", Directory: actualDir}
	service := &fakeService{
		projectByDirectory: map[string]*agent.Project{resolvedTestDir(t, actualDir): project},
		doctorChecks: []DoctorCheck{
			{Name: "Remote prod-eu", Status: "warn", Detail: "heartbeat stale"},
		},
		runs: []RunSummary{{
			ID:         "run-128",
			SessionKey: "ops-incident",
			Status:     "paused",
			Phase:      "paused",
			Resumable:  true,
		}},
	}
	var output strings.Builder
	repl := &REPL{
		service:    service,
		sessionKey: "ops-incident",
		targetName: "prod-eu",
		renderer:   NewRenderer(&output, false),
	}

	repl.renderBriefing(context.Background())

	rendered := output.String()
	for _, want := range []string{
		"[card] Welcome back",
		"Conversation  ops-incident",
		"degraded (remote prod-eu: heartbeat stale)",
		"paused run-128",
		"continue  /continue run-128  /remote  /doctor",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("briefing missing %q: %q", want, rendered)
		}
	}
	for _, unwanted := range []string{
		"Project: ",
		"Path: ",
		"Loaded project memories:",
		"Try /model, /session, /remote, /badge, or just ask in natural language.",
	} {
		if strings.Contains(rendered, unwanted) {
			t.Fatalf("briefing should stay compact, found %q in %q", unwanted, rendered)
		}
	}
}

func TestRenderBriefingUsesSnapshotRecoveryCandidates(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q) error = %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	actualDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	project := &agent.Project{ID: "proj-ops", Name: "ops", Directory: actualDir}
	service := &fakeService{
		projectByDirectory: map[string]*agent.Project{resolvedTestDir(t, actualDir): project},
		readiness: &ReadinessSnapshot{
			OverallStatus: "degraded",
			Categories: []ReadinessCategory{{
				ID:      "remote_target",
				Label:   "Remote prod-eu",
				Status:  "degraded",
				Summary: "heartbeat stale",
			}},
			RecoveryCandidates: []RecoveryCandidate{
				{Type: "paused_run", ID: "run-128", Action: "continue"},
				{Type: "draft", ID: "ops-incident", Action: "restore"},
			},
		},
	}
	var output strings.Builder
	repl := &REPL{
		service:    service,
		sessionKey: "ops-incident",
		targetName: "prod-eu",
		renderer:   NewRenderer(&output, false),
	}

	repl.renderBriefing(context.Background())

	rendered := output.String()
	for _, want := range []string{
		"[card] Welcome back",
		"degraded (remote prod-eu: heartbeat stale)",
		"paused run-128 (+1 more)",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("briefing missing %q: %q", want, rendered)
		}
	}
}

func TestRenderBriefingSkipsQualityReleaseOnlyStartupCard(t *testing.T) {
	service := &fakeService{
		readiness: &ReadinessSnapshot{
			OverallStatus: "blocked",
			Categories: []ReadinessCategory{{
				ID:      "quality_release",
				Label:   "Quality Release",
				Status:  "blocked",
				Summary: "1 blocker(s)",
			}},
		},
	}
	var output strings.Builder
	repl := &REPL{
		service:    service,
		sessionKey: "default",
		targetName: "local",
		targetKind: "local",
		renderer:   NewRenderer(&output, false),
	}

	repl.renderBriefing(context.Background())

	if strings.Contains(output.String(), "[card] Welcome back") {
		t.Fatalf("quality-release-only startup should stay quiet: %q", output.String())
	}
}

func TestRenderBriefingSkipsFailedRunOnlyStartupCard(t *testing.T) {
	service := &fakeService{
		runs: []RunSummary{{
			ID:     "run-404",
			Status: "failed",
			Error:  "gateway unreachable",
		}},
	}
	var output strings.Builder
	repl := &REPL{
		service:    service,
		sessionKey: "default",
		targetName: "local",
		targetKind: "local",
		renderer:   NewRenderer(&output, false),
	}

	repl.renderBriefing(context.Background())

	if strings.Contains(output.String(), "[card] Welcome back") {
		t.Fatalf("failed-run-only startup should stay quiet: %q", output.String())
	}
}

func TestStartupHealthSummarySkipsDoctorFallbackForPrivateLocalSession(t *testing.T) {
	repl := &REPL{
		service: &fakeService{
			doctorChecks: []DoctorCheck{{
				Name:   "Local runtime",
				Status: "warn",
				Detail: "not running at 127.0.0.1:16280",
			}},
		},
		targetName: "local",
		targetKind: "local",
	}

	if got := repl.startupHealthSummary(context.Background()); got != "" {
		t.Fatalf("startupHealthSummary() = %q, want empty for private local session", got)
	}
}

func TestStartupHealthSummaryHonorsQuietWhenHealthySnapshotPolicy(t *testing.T) {
	repl := &REPL{
		service: &fakeService{
			doctorChecks: []DoctorCheck{{
				Name:   "Remote prod-eu",
				Status: "warn",
				Detail: "heartbeat stale",
			}},
		},
		readinessSnapshot: &ReadinessSnapshot{
			OverallStatus:      "ready",
			StartupDiagnostics: "quiet_when_healthy",
			Categories: []ReadinessCategory{{
				ID:      "gateway",
				Label:   "System",
				Status:  "ready",
				Summary: "ready",
			}},
		},
		targetName: "prod-eu",
		targetKind: "remote",
	}

	if got := repl.startupHealthSummary(context.Background()); got != "" {
		t.Fatalf("startupHealthSummary() = %q, want empty when quiet_when_healthy policy is active", got)
	}
}

func TestSubmitPreparedGreetingReachesModel(t *testing.T) {
	client := newTestACPClient(t)
	t.Cleanup(func() { client.Close() })

	service := &fakeService{
		detail: &SessionDetail{Summary: SessionSummary{Key: "default", Model: "gpt-4o"}},
		recovery: []RecoveryCandidate{{
			Type:   "paused_run",
			ID:     "run-128",
			Action: "continue",
		}},
	}
	var output strings.Builder
	repl := &REPL{
		client:     client,
		service:    service,
		renderer:   NewRenderer(&output, false),
		prompt:     &DynamicPrompt{},
		streamer:   NewStreamer(client.Notifications()),
		sessionKey: "default",
		targetName: "local",
		layoutMode: LayoutAuto,
	}
	if err := repl.switchSession(context.Background(), "default", false); err != nil {
		t.Fatalf("switchSession() error = %v", err)
	}

	if err := repl.submitPrepared(context.Background(), "hi", nil, nil, "gpt-4o"); err != nil {
		t.Fatalf("submitPrepared() error = %v", err)
	}

	text := output.String()
	if strings.Contains(text, "[card] Welcome back") {
		t.Fatalf("greeting should not replay startup recovery card: %q", text)
	}
	if !strings.Contains(text, "hello world") {
		t.Fatalf("output = %q, want model response", text)
	}
}

func TestSubmitPreparedNaturalLanguageDoesNotResumeLocally(t *testing.T) {
	client := newTestACPClient(t)
	t.Cleanup(func() { client.Close() })

	service := &fakeService{
		detail: &SessionDetail{Summary: SessionSummary{Key: "default", Model: "gpt-4o"}},
		recovery: []RecoveryCandidate{{
			Type:   "paused_run",
			ID:     "run-128",
			Action: "continue",
		}},
		runDetails: map[string]*RunDetail{
			"run-128": {
				Run: RunSummary{
					ID:         "run-128",
					SessionKey: "ops-incident",
					Status:     "paused",
					Phase:      "paused",
				},
				Output: "latest verified output",
			},
		},
	}
	var output strings.Builder
	repl := &REPL{
		client:     client,
		service:    service,
		renderer:   NewRenderer(&output, false),
		prompt:     &DynamicPrompt{},
		streamer:   NewStreamer(client.Notifications()),
		sessionKey: "default",
		targetName: "local",
		layoutMode: LayoutAuto,
	}
	if err := repl.switchSession(context.Background(), "default", false); err != nil {
		t.Fatalf("switchSession() error = %v", err)
	}

	if err := repl.submitPrepared(context.Background(), "continue", nil, nil, "gpt-4o"); err != nil {
		t.Fatalf("submitPrepared() error = %v", err)
	}

	if repl.sessionKey != "default" {
		t.Fatalf("sessionKey = %q, want %q", repl.sessionKey, "default")
	}
	if strings.Contains(output.String(), "Resumed work from task run-128 in conversation ops-incident.") {
		t.Fatalf("output = %q, want no local resume interception", output.String())
	}
	if !strings.Contains(output.String(), "hello world") {
		t.Fatalf("output = %q, want model response", output.String())
	}
}

func TestCDCommandChangesDirectoryAndLoadsProjectContext(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	startDir := t.TempDir()
	nextDir := t.TempDir()
	if err := os.Chdir(startDir); err != nil {
		t.Fatalf("Chdir(%q) error = %v", startDir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	wantDir := resolvedTestDir(t, nextDir)
	project := &agent.Project{ID: "proj-next", Name: "next", Directory: wantDir}
	service := &fakeService{
		projectByDirectory: map[string]*agent.Project{wantDir: project},
		memories:           []agent.MemoryEntry{{Value: "Remember this", Source: agent.MemorySourceAgent}},
	}
	var output strings.Builder
	repl := &REPL{
		service:    service,
		sessionKey: "session-alpha",
		renderer:   NewRenderer(&output, false),
	}
	registry := NewCommandRegistry()

	if _, err := registry.Execute(context.Background(), repl, "/cd "+nextDir); err != nil {
		t.Fatalf("Execute(/cd) error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if cwd != wantDir {
		t.Fatalf("cwd = %q, want %q", cwd, wantDir)
	}
	if repl.currentProject != project {
		t.Fatalf("currentProject = %#v, want %#v", repl.currentProject, project)
	}
	rendered := output.String()
	if !strings.Contains(rendered, "[panel] Directory Changed") {
		t.Fatalf("output missing directory panel: %q", rendered)
	}
	if !strings.Contains(rendered, "Project: next") {
		t.Fatalf("output missing switch line: %q", rendered)
	}
	if !strings.Contains(rendered, "Path: ") {
		t.Fatalf("output missing path line: %q", rendered)
	}
	if !strings.Contains(rendered, "Loaded project memories: 1") {
		t.Fatalf("output missing memory count: %q", rendered)
	}
	if !strings.Contains(rendered, "Suggested actions: /history  /context  /runs recent") {
		t.Fatalf("output missing suggested actions: %q", rendered)
	}
}
func (s *fakeService) StartNewEpisode(_ context.Context, sessionID string) error {
	s.startEpisodeCalled = true
	s.startEpisodeID = sessionID
	return nil
}
func (s *fakeService) ResetSession(context.Context, string) error {
	s.resetCalled = true
	return nil
}
func (s *fakeService) CompactSession(_ context.Context, sessionID string) error {
	s.compactCalled = true
	s.compactSessionID = sessionID
	return nil
}
func (s *fakeService) ResolvePermission(_ context.Context, _ acp.PermissionRequest, decision PermissionDecision) error {
	s.resolvedApprovals = append(s.resolvedApprovals, decision)
	return nil
}
func (s *fakeService) Close(context.Context) error {
	s.closed = true
	return nil
}

type fakeTargetManager struct {
	current    TargetInfo
	targets    []TargetInfo
	binding    *TargetBinding
	listErr    error
	switchErr  error
	switchName string
	loginErr   error
	logoutErr  error
	loginName  string
	loginToken string
	logoutName string
}

func (m *fakeTargetManager) CurrentTarget() TargetInfo {
	return m.current
}

func (m *fakeTargetManager) ListTargets(context.Context) ([]TargetInfo, error) {
	return append([]TargetInfo(nil), m.targets...), m.listErr
}

func (m *fakeTargetManager) SwitchTarget(_ context.Context, name string) (*TargetBinding, error) {
	m.switchName = name
	if m.switchErr != nil {
		return nil, m.switchErr
	}
	return m.binding, nil
}

func (m *fakeTargetManager) LoginTarget(_ context.Context, name, token string) error {
	m.loginName = name
	m.loginToken = token
	return m.loginErr
}

func (m *fakeTargetManager) LogoutTarget(_ context.Context, name string) error {
	m.logoutName = name
	return m.logoutErr
}

type fakeGateway struct{}

func (fakeGateway) SubmitRunWithOptions(_ context.Context, _ string, _ string, _ []string, _ acp.PromptOptions) (string, <-chan acp.RunEvent, error) {
	events := make(chan acp.RunEvent, 2)
	events <- acp.RunEvent{Type: "text_delta", Text: "hello world\n"}
	events <- acp.RunEvent{
		Type:       "complete",
		StopReason: acp.StopEndTurn,
		Usage: &acp.UsageInfo{
			PromptTokens:     5,
			CompletionTokens: 7,
			TotalTokens:      12,
		},
	}
	close(events)
	return "run-1", events, nil
}

func (fakeGateway) SubmitRun(_ context.Context, _ string, _ string, _ []string) (string, <-chan acp.RunEvent, error) {
	return "", nil, errors.New("unexpected call")
}
func (fakeGateway) CancelRun(context.Context, string, string) error { return nil }
func (fakeGateway) ListSessions(context.Context, int, int) ([]acp.SessionInfo, error) {
	return nil, nil
}
func (fakeGateway) ResolveSession(_ context.Context, key string) (*acp.SessionInfo, error) {
	return nil, errors.New("not found: " + key)
}
func (fakeGateway) ResetSession(context.Context, string) error { return nil }

type chatReplyGateway struct{}

func (chatReplyGateway) SubmitRunWithOptions(_ context.Context, _ string, _ string, _ []string, _ acp.PromptOptions) (string, <-chan acp.RunEvent, error) {
	events := make(chan acp.RunEvent, 2)
	events <- acp.RunEvent{Type: "text_delta", Text: "I am here.\n", Runless: true}
	events <- acp.RunEvent{Type: "complete", StopReason: acp.StopEndTurn, Runless: true}
	close(events)
	return "", events, nil
}

func (chatReplyGateway) SubmitRun(_ context.Context, _ string, _ string, _ []string) (string, <-chan acp.RunEvent, error) {
	return "", nil, errors.New("unexpected call")
}
func (chatReplyGateway) CancelRun(context.Context, string, string) error { return nil }
func (chatReplyGateway) ListSessions(context.Context, int, int) ([]acp.SessionInfo, error) {
	return nil, nil
}
func (chatReplyGateway) ResolveSession(_ context.Context, key string) (*acp.SessionInfo, error) {
	return nil, errors.New("not found: " + key)
}
func (chatReplyGateway) ResetSession(context.Context, string) error { return nil }

func newTestACPClient(t *testing.T) *acp.InProcessClient {
	t.Helper()
	server := acp.NewServer(fakeGateway{}, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	return client
}

type approvalGateway struct{}

func (approvalGateway) SubmitRunWithOptions(_ context.Context, _ string, _ string, _ []string, _ acp.PromptOptions) (string, <-chan acp.RunEvent, error) {
	events := make(chan acp.RunEvent, 3)
	events <- acp.RunEvent{
		Type: "permission_request",
		Permission: &acp.PermissionRequest{
			RequestID:                  "approval-1",
			ToolName:                   "exec.shell",
			Description:                "Delete temporary logs",
			Input:                      `{"cmd":"rm -rf /tmp/logs"}`,
			MaxGrantScope:              "session",
			RequiresExternalSideEffect: true,
		},
	}
	events <- acp.RunEvent{Type: "text_delta", Text: "done\n"}
	events <- acp.RunEvent{Type: "complete", StopReason: acp.StopEndTurn}
	close(events)
	return "run-approval", events, nil
}

func (approvalGateway) SubmitRun(_ context.Context, _ string, _ string, _ []string) (string, <-chan acp.RunEvent, error) {
	return "", nil, errors.New("unexpected call")
}
func (approvalGateway) CancelRun(context.Context, string, string) error { return nil }
func (approvalGateway) ListSessions(context.Context, int, int) ([]acp.SessionInfo, error) {
	return nil, nil
}
func (approvalGateway) ResolveSession(_ context.Context, key string) (*acp.SessionInfo, error) {
	return nil, errors.New("not found: " + key)
}
func (approvalGateway) ResetSession(context.Context, string) error { return nil }
func (approvalGateway) ResolveApproval(context.Context, string, approval.Resolution) error {
	return nil
}

type cancellableGateway struct {
	cancelCh    chan struct{}
	startedCh   chan struct{}
	startedOnce sync.Once
	runCount    int
	cancelled   bool
}

func (g *cancellableGateway) SubmitRunWithOptions(_ context.Context, _ string, _ string, _ []string, _ acp.PromptOptions) (string, <-chan acp.RunEvent, error) {
	g.runCount++
	g.startedOnce.Do(func() {
		if g.startedCh != nil {
			close(g.startedCh)
		}
	})
	events := make(chan acp.RunEvent, 4)
	runID := "run-pause"
	if g.runCount == 1 {
		go func() {
			defer close(events)
			events <- acp.RunEvent{Type: "tool_delta", ToolName: "audit.deliveries", ToolOutput: "checking"}
			<-g.cancelCh
			events <- acp.RunEvent{Type: "complete", StopReason: acp.StopCancelled}
		}()
		return runID, events, nil
	}
	go func() {
		defer close(events)
		events <- acp.RunEvent{Type: "text_delta", Text: "resumed\n"}
		events <- acp.RunEvent{Type: "complete", StopReason: acp.StopEndTurn, Usage: &acp.UsageInfo{PromptTokens: 8, CompletionTokens: 3, TotalTokens: 11}}
	}()
	return "run-resumed", events, nil
}

func (g *cancellableGateway) SubmitRun(_ context.Context, _ string, _ string, _ []string) (string, <-chan acp.RunEvent, error) {
	return "", nil, errors.New("unexpected call")
}

func (g *cancellableGateway) CancelRun(context.Context, string, string) error {
	if !g.cancelled {
		g.cancelled = true
		close(g.cancelCh)
	}
	return nil
}

func (*cancellableGateway) ListSessions(context.Context, int, int) ([]acp.SessionInfo, error) {
	return nil, errors.New("persisted sessions unavailable")
}
func (*cancellableGateway) ResolveSession(_ context.Context, key string) (*acp.SessionInfo, error) {
	return nil, errors.New("not found: " + key)
}
func (*cancellableGateway) ResetSession(context.Context, string) error { return nil }

func newCancellableRunREPL(t *testing.T, tty bool) (*REPL, *cancellableGateway, *strings.Builder) {
	t.Helper()

	gateway := &cancellableGateway{
		cancelCh:  make(chan struct{}),
		startedCh: make(chan struct{}),
	}
	server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	t.Cleanup(func() { client.Close() })

	service := &fakeService{
		detail: &SessionDetail{Summary: SessionSummary{ID: "default", Key: "default", Model: "gpt-4o"}},
	}
	var output strings.Builder
	repl, err := New(Config{
		Client:     client,
		Service:    service,
		Prompter:   &scriptedPrompter{},
		Renderer:   NewRenderer(&output, tty),
		History:    NewHistory("", 10),
		SessionKey: "default",
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := client.Initialize(context.Background(), acp.InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      acp.Implementation{Name: "test-repl", Version: "test"},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := repl.switchSession(context.Background(), "default", false); err != nil {
		t.Fatalf("switchSession() error = %v", err)
	}
	return repl, gateway, &output
}

func waitForCancellableRunStart(t *testing.T, repl *REPL, gateway *cancellableGateway) {
	t.Helper()
	select {
	case <-gateway.startedCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("run did not start within timeout: runCount=%d running=%t", gateway.runCount, repl.running)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if gateway.runCount > 0 && repl.running && repl.client != nil {
			sessions, err := repl.client.ListSessions(context.Background(), acp.ListSessionsParams{Limit: 10})
			if err == nil {
				for _, session := range sessions {
					if session.SessionID == repl.sessionID && session.Status == acp.SessionStreaming {
						return
					}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run did not enter streaming state: runCount=%d running=%t session=%q", gateway.runCount, repl.running, repl.sessionID)
}

func awaitSubmitResult(t *testing.T, done <-chan error, label string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not finish within timeout", label)
		return nil
	}
}

func TestCommandRegistryDynamicCommandReturnsExplicitError(t *testing.T) {
	registry := NewCommandRegistry()
	registry.SetDynamic([]acp.Command{{Name: "review-pr", Description: "Review a PR"}})
	result, err := registry.Execute(context.Background(), nil, "/review-pr 123")
	if err == nil {
		t.Fatal("Execute() error = nil, want explicit unsupported-command error")
	}
	if result.Submit != "" {
		t.Fatalf("Submit = %q, want empty submit for unsupported dynamic command", result.Submit)
	}
	if !strings.Contains(err.Error(), `"/review-pr" is not a REPL slash command`) {
		t.Fatalf("error = %q, want advertised-command guidance", err)
	}
}

func TestCommandRegistrySuggestionsAndCompletion(t *testing.T) {
	registry := NewCommandRegistry()
	registry.SetDynamic([]acp.Command{{Name: "review-pr", Description: "Review a PR"}})

	suggestions := registry.Suggestions("/re")
	if len(suggestions) > 6 {
		t.Fatalf("Suggestions() returned %d items, want at most 6", len(suggestions))
	}
	names := make([]string, 0, len(suggestions))
	for _, item := range suggestions {
		names = append(names, item.Name)
	}
	for _, want := range []string{"remote", "reset", "retry"} {
		if !slices.Contains(names, want) {
			t.Fatalf("Suggestions missing %q: %#v", want, names)
		}
	}
	if slices.Contains(names, "resume") {
		t.Fatalf("Suggestions unexpectedly included deprecated command: %#v", names)
	}
	if slices.Contains(names, "review-pr") {
		t.Fatalf("Suggestions unexpectedly included dynamic inventory command: %#v", names)
	}

	if got := registry.Complete("/mod"); got != "/model " {
		t.Fatalf("Complete(/mod) = %q, want %q", got, "/model ")
	}
	if got := registry.Complete("/att"); got != "/attach " {
		t.Fatalf("Complete(/att) = %q, want %q", got, "/attach ")
	}
	if got := registry.Complete("/review"); got != "/review" {
		t.Fatalf("Complete(/review) = %q, want %q", got, "/review")
	}
	if _, _, ok := registry.Describe("review-pr"); ok {
		t.Fatal("Describe(review-pr) unexpectedly treated runtime inventory as executable help")
	}
	if command, ok := registry.DynamicCommand("review-pr"); !ok || command.Name != "review-pr" {
		t.Fatalf("DynamicCommand(review-pr) = (%#v, %t), want review-pr", command, ok)
	}
}

func TestCommandRegistryRuntimeInventoryFiltersACPDefaultsAndBuiltIns(t *testing.T) {
	registry := NewCommandRegistry()
	registry.SetDynamicFromRuntimeInventory([]acp.Command{
		{Name: "review-pr", Description: "Review a PR"},
		{Name: "config", Description: "Show config"},
		{Name: "status", Description: "Show current conversation status"},
	})

	commands := registry.DynamicCommands()
	if len(commands) != 1 || commands[0].Name != "review-pr" {
		t.Fatalf("DynamicCommands() = %#v, want review-pr only", commands)
	}
	if _, ok := registry.DynamicCommand("config"); ok {
		t.Fatal("DynamicCommand(config) unexpectedly kept ACP default inventory entry")
	}
	if _, ok := registry.DynamicCommand("status"); ok {
		t.Fatal("DynamicCommand(status) unexpectedly kept built-in command shadow")
	}
}

func TestCommandRegistryUnknownCommandSuggestsMatch(t *testing.T) {
	registry := NewCommandRegistry()

	_, err := registry.Execute(context.Background(), nil, "/remtoe")
	if err == nil || !strings.Contains(err.Error(), "Did you mean") || !strings.Contains(err.Error(), "/remote") {
		t.Fatalf("Execute(/remtoe) error = %v, want typo suggestion", err)
	}
}

func TestSessionCommandRequiresKeyForNew(t *testing.T) {
	registry := NewCommandRegistry()
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  &fakeService{},
	}

	_, err := registry.Execute(context.Background(), repl, "/session new")
	if err == nil || !strings.Contains(err.Error(), "usage: /session new [key]") {
		t.Fatalf("Execute(/session new) error = %v, want usage error", err)
	}
}

func TestCommandRegistryStopSuggestsPause(t *testing.T) {
	registry := NewCommandRegistry()
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  &fakeService{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/stop"); err == nil || !strings.Contains(err.Error(), `unknown command "/stop". Use /pause instead`) {
		t.Fatalf("Execute(/stop) error = %v, want replacement guidance", err)
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want no alias execution side effects", output.String())
	}
}

func TestHelpShowsGroupedCommands(t *testing.T) {
	registry := NewCommandRegistry()
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  &fakeService{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/help"); err != nil {
		t.Fatalf("Execute(/help) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{
		"Quick Start",
		"Type normally and press Enter to send.",
		"Keys: Enter send · Ctrl+J newline · Ctrl+V clipboard · @ attach · Ctrl+C quit",
		"Core commands",
		"/help <command>  /status  /last  /runs  /model  /remote  /clear  /quit",
		"Task control",
		"/pause  /continue  /cancel  /retry",
		"Attachments",
		"/attach <image|file|dir|video> <path>",
		"State: idle",
		"Use /help <command> for details.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("grouped help missing %q: %q", want, text)
		}
	}
}

func TestHelpCommandShowsPurposeFlowsAndExamples(t *testing.T) {
	registry := NewCommandRegistry()
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  &fakeService{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/help model"); err != nil {
		t.Fatalf("Execute(/help model) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{
		"Help: /model",
		"/model — Show or change model",
		"Group: Runtime",
		"Purpose: inspect or change the active model",
		"Common flows",
		"Examples",
		"/model gpt-5.4",
		"/think on",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("command help missing %q: %q", want, text)
		}
	}
}

func TestHelpAttachShowsComposerCommandGuidance(t *testing.T) {
	registry := NewCommandRegistry()
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  &fakeService{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/help attach"); err != nil {
		t.Fatalf("Execute(/help attach) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{
		"Help: /attach",
		"/attach inserts an attachment token into the composer instead of sending a slash command.",
		"Usage: /attach <image|file|dir|video> <path>",
		"/attach image ./tmp/screenshot.png",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("attach help missing %q: %q", want, text)
		}
	}
}

func TestToolsCommandRendersPanelAndDetails(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		tools: []ToolSummary{
			{
				Name:             "fs.read",
				Description:      "Read a file from disk",
				SideEffectClass:  "read",
				Source:           "builtin",
				Eligible:         true,
				RequiresApproval: false,
				InputSchema:      map[string]any{"properties": map[string]any{"path": map[string]any{}}},
			},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  service,
	}

	if _, err := registry.Execute(context.Background(), repl, "/tools"); err != nil {
		t.Fatalf("Execute(/tools) error = %v", err)
	}
	if _, err := registry.Execute(context.Background(), repl, "/tools info fs.read"); err != nil {
		t.Fatalf("Execute(/tools info) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{
		"Tools",
		"fs.read",
		"Tool Detail",
		"Source: builtin",
		"Input schema: path",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("tools output missing %q: %q", want, text)
		}
	}
}

func TestSkillsCommandsRenderCatalogAndInstall(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		skills: []SkillSummary{
			{
				ID:         "review-skill",
				Name:       "review-skill",
				Version:    "1.2.3",
				Status:     "ready",
				Trust:      "community",
				InstallDir: "/tmp/review-skill",
				Installed:  true,
			},
		},
		skillCatalog: []SkillCatalogSummary{
			{
				ID:          "review-skill",
				Name:        "review-skill",
				Version:     "1.2.3",
				Summary:     "Review pull requests",
				Description: "Review pull requests",
			},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  service,
	}

	for _, input := range []string{
		"/skills",
		"/skills search review",
		"/skills info review-skill",
		"/skills install review-skill",
		"/skills remove " + internalConfirmedArg + " review-skill",
	} {
		if _, err := registry.Execute(context.Background(), repl, input); err != nil {
			t.Fatalf("Execute(%s) error = %v", input, err)
		}
	}

	if len(service.installedSkills) != 1 || service.installedSkills[0].SkillID != "review-skill" {
		t.Fatalf("installedSkills = %#v", service.installedSkills)
	}
	if len(service.removedSkills) != 1 || service.removedSkills[0] != "review-skill" {
		t.Fatalf("removedSkills = %#v", service.removedSkills)
	}

	text := output.String()
	for _, want := range []string{
		"Skills",
		"Skill Catalog",
		"Skill Detail",
		"Skill Installed",
		"Removed skill review-skill.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("skills output missing %q: %q", want, text)
		}
	}
}

func TestHelpStopShowsReplacementGuidanceWithoutDeprecatedTopic(t *testing.T) {
	registry := NewCommandRegistry()
	var output strings.Builder
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  &fakeService{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/help stop"); err != nil {
		t.Fatalf("Execute(/help stop) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{
		"Help Search",
		"/stop is not a current REPL command.",
		"Use /pause instead.",
		"Task control split:",
		"- /pause keeps a resumable paused handle",
		"- /cancel ends the task without keeping paused work",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("stop help missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "Deprecated: /stop") {
		t.Fatalf("stop help should not expose a deprecated topic: %q", text)
	}
}

func TestHelpPrioritizesPausedAndApprovalStateHints(t *testing.T) {
	registry := NewCommandRegistry()

	t.Run("paused", func(t *testing.T) {
		var output strings.Builder
		repl := &REPL{
			renderer:  NewRenderer(&output, false),
			service:   &fakeService{},
			phase:     PhasePaused,
			pausedRun: &pausedRunState{Message: "resume me"},
		}

		if _, err := registry.Execute(context.Background(), repl, "/help"); err != nil {
			t.Fatalf("Execute(/help) error = %v", err)
		}

		text := output.String()
		for _, want := range []string{
			"State: paused",
			"Enter continue · x discard · /retry · /quit",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("paused help missing %q: %q", want, text)
			}
		}
	})

	t.Run("approval", func(t *testing.T) {
		var output strings.Builder
		repl := &REPL{
			renderer:        NewRenderer(&output, false),
			service:         &fakeService{},
			phase:           PhaseWaitingApproval,
			pendingApproval: true,
		}

		if _, err := registry.Execute(context.Background(), repl, "/help"); err != nil {
			t.Fatalf("Execute(/help) error = %v", err)
		}

		text := output.String()
		for _, want := range []string{
			"State: approval",
			"y approve once · a always · n deny · v details",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("approval help missing %q: %q", want, text)
			}
		}
	})
}

func TestPhaseOneCommandsRenderOperatorViews(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		approvals: []ApprovalSummary{{
			ID:            "approval-1",
			Status:        "pending",
			ToolName:      "shell.exec",
			PolicySummary: "needs approval",
			CreatedAt:     "2026-04-05T12:00:00Z",
		}},
		quality: &QualitySnapshot{
			RunCount:            12,
			TerminalRunCount:    10,
			TaskSuccess:         "80.0% (8/10)",
			FalseSuccess:        "10.0% (1/10)",
			VerificationFailure: "20.0% (2/10)",
			TraceCount:          7,
			Ready:               false,
			CheckCount:          1,
			BlockerCount:        1,
			Blockers:            []string{"sample_size: need more terminal runs"},
		},
		evals: []EvalSuiteSummary{{
			ID:        "browser.smoke",
			Name:      "Browser Smoke",
			Surface:   "browser",
			CaseCount: 1,
		}},
		runs: []RunSummary{{
			ID:        "run-1",
			SessionID: "sess-1",
			Status:    "completed",
			Phase:     "completed",
		}},
		runDetails: map[string]*RunDetail{
			"run-1": {
				Run: RunSummary{
					ID:     "run-1",
					Status: "completed",
					Phase:  "completed",
				},
				Output: "final answer",
			},
		},
		doctorChecks: []DoctorCheck{
			{Name: "Gateway", Status: "ok", Detail: "reachable"},
			{Name: "Config", Status: "warn", Detail: "missing optional key"},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer:  NewRenderer(&output, false),
		service:   service,
		sessionID: "sess-1",
	}

	for _, command := range []string{"/approvals", "/quality", "/evals", "/runs recent", "/last", "/doctor"} {
		if _, err := registry.Execute(context.Background(), repl, command); err != nil {
			t.Fatalf("Execute(%s) error = %v", command, err)
		}
	}

	text := output.String()
	for _, want := range []string{
		"approval-1",
		"Release readiness: no",
		"sample_size: need more terminal runs",
		"Browser Smoke      browser    1 cases",
		"run-1      FG completed",
		"Output\nfinal answer",
		"System Readiness",
		"System          degraded",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("operator command output missing %q: %q", want, text)
		}
	}
}

func TestPauseCommandRequestsPauseForRunningTask(t *testing.T) {
	repl, gateway, output := newCancellableRunREPL(t, false)
	registry := NewCommandRegistry()
	repl.commands = registry
	done := make(chan error, 1)
	go func() {
		done <- repl.submit(context.Background(), "pause me")
	}()
	waitForCancellableRunStart(t, repl, gateway)

	if _, err := registry.Execute(context.Background(), repl, "/pause"); err != nil {
		t.Fatalf("Execute(/pause) error = %v", err)
	}
	if err := awaitSubmitResult(t, done, "pause submit"); err != nil {
		t.Fatalf("submit() error = %v", err)
	}
	if !gateway.cancelled {
		t.Fatal("gateway.cancelled = false, want true after /pause")
	}
	if !strings.Contains(output.String(), "Pausing current task") {
		t.Fatalf("output = %q, want pause confirmation", output.String())
	}
}

func TestStopCommandSuggestsPauseWithoutCancellingRun(t *testing.T) {
	repl, gateway, output := newCancellableRunREPL(t, false)
	registry := NewCommandRegistry()
	repl.commands = registry
	done := make(chan error, 1)
	go func() {
		done <- repl.submit(context.Background(), "pause me")
	}()
	waitForCancellableRunStart(t, repl, gateway)

	if _, err := registry.Execute(context.Background(), repl, "/stop"); err == nil || !strings.Contains(err.Error(), `unknown command "/stop". Use /pause instead`) {
		t.Fatalf("Execute(/stop) error = %v, want replacement guidance", err)
	}
	if !gateway.cancelled {
		// expected: /stop no longer aliases /pause
	} else {
		t.Fatal("gateway.cancelled = true, want running task to remain active after /stop guidance")
	}

	if _, err := registry.Execute(context.Background(), repl, "/pause"); err != nil {
		t.Fatalf("Execute(/pause) error = %v", err)
	}
	if err := awaitSubmitResult(t, done, "pause submit"); err != nil {
		t.Fatalf("submit() error = %v", err)
	}
	if !gateway.cancelled {
		t.Fatal("gateway.cancelled = false, want true after /pause")
	}
	if !strings.Contains(output.String(), "Pausing current task") {
		t.Fatalf("output = %q, want explicit /pause confirmation", output.String())
	}
}

func TestEpisodeCommandStartsNewEpisode(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{}
	var output strings.Builder
	repl := &REPL{
		sessionID: "sess-episode",
		renderer:  NewRenderer(&output, false),
		service:   service,
	}

	if _, err := registry.Execute(context.Background(), repl, "/episode"); err != nil {
		t.Fatalf("Execute(/episode) error = %v", err)
	}
	if !service.startEpisodeCalled {
		t.Fatal("expected StartNewEpisode to be called")
	}
	if !strings.Contains(output.String(), "Started a new conversation checkpoint.") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestEpisodeCommandResolvesBackendSessionByKey(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		sessions: []SessionSummary{{ID: "sess-real", Key: "cli-1", Model: "gpt-4o"}},
		sessionByID: map[string]*SessionDetail{
			"sess-real": {
				Summary: SessionSummary{ID: "sess-real", Key: "cli-1", Model: "gpt-4o"},
			},
		},
	}
	var output strings.Builder
	repl := &REPL{
		sessionID:  "acp-1",
		sessionKey: "cli-1",
		renderer:   NewRenderer(&output, false),
		service:    service,
	}

	if _, err := registry.Execute(context.Background(), repl, "/episode"); err != nil {
		t.Fatalf("Execute(/episode) error = %v", err)
	}
	if !service.startEpisodeCalled {
		t.Fatal("expected StartNewEpisode to be called")
	}
	if service.startEpisodeID != "sess-real" {
		t.Fatalf("startEpisodeID = %q, want %q", service.startEpisodeID, "sess-real")
	}
	if !strings.Contains(output.String(), "Started a new conversation checkpoint.") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestCompactCommandResolvesBackendSessionByKey(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		sessions: []SessionSummary{{ID: "sess-real", Key: "cli-1", Model: "gpt-4o"}},
		sessionByID: map[string]*SessionDetail{
			"sess-real": {
				Summary: SessionSummary{ID: "sess-real", Key: "cli-1", Model: "gpt-4o"},
				Messages: []SessionMessage{
					{Role: "user", Content: "hello"},
					{Role: "assistant", Content: "world"},
				},
			},
		},
	}
	var output strings.Builder
	repl := &REPL{
		sessionID:  "acp-1",
		sessionKey: "cli-1",
		renderer:   NewRenderer(&output, false),
		service:    service,
	}

	if _, err := registry.Execute(context.Background(), repl, "/compact "+internalConfirmedArg); err != nil {
		t.Fatalf("Execute(/compact) error = %v", err)
	}
	if !service.compactCalled {
		t.Fatal("expected CompactSession to be called")
	}
	if service.compactSessionID != "sess-real" {
		t.Fatalf("compactSessionID = %q, want %q", service.compactSessionID, "sess-real")
	}
	if !strings.Contains(output.String(), "Conversation compacted.") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestHistoryRoundTrip(t *testing.T) {
	path := t.TempDir() + "/history.txt"
	history := NewHistory(path, 10)
	if err := history.Add("first"); err != nil {
		t.Fatalf("Add(first) error = %v", err)
	}
	if err := history.Add("second"); err != nil {
		t.Fatalf("Add(second) error = %v", err)
	}

	loaded := NewHistory(path, 10)
	if got := loaded.Previous(""); got != "second" {
		t.Fatalf("Previous() = %q", got)
	}
	if got := loaded.Previous(""); got != "first" {
		t.Fatalf("Previous() second step = %q", got)
	}
}

func TestREPLRunInitialMessageAndExit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := acp.NewServer(fakeGateway{}, acp.ServerConfig{
		DefaultSessionKey: "default",
		CommandProvider: func(context.Context) ([]acp.Command, error) {
			return []acp.Command{{Name: "help", Description: "Show help"}}, nil
		},
	})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	service := &fakeService{
		models: []ModelInfo{{ID: "gpt-4o", ContextWindow: 128000}},
		detail: &SessionDetail{Summary: SessionSummary{ID: "default", Key: "default", Model: "gpt-4o"}},
	}
	var output strings.Builder
	repl, err := New(Config{
		Client:         client,
		Service:        service,
		Prompter:       &scriptedPrompter{lines: []string{"/exit"}},
		Renderer:       NewRenderer(&output, false),
		History:        NewHistory("", 10),
		SessionKey:     "default",
		InitialMessage: "hello",
		OneShot:        true,
		Version:        "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := output.String()
	for _, want := range []string{
		"hello world",
		"Task Completed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "conversation default · /help") || strings.Contains(got, "HopClaw") {
		t.Fatalf("one-shot output should not include banner text: %q", got)
	}
}

func TestREPLRunOneShotSlashCommandsExecuteWithoutSubmit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	service := &fakeService{
		models: []ModelInfo{{ID: "gpt-4o", ContextWindow: 128000}},
		detail: &SessionDetail{Summary: SessionSummary{ID: "default", Key: "default", Model: "gpt-4o"}},
	}

	tests := []struct {
		name              string
		initial           string
		wantStatus        []string
		wantAssistant     []string
		unwantedStatus    []string
		unwantedAssistant []string
	}{
		{
			name:              "help",
			initial:           "/help",
			wantStatus:        []string{"Quick Start", "/help <command>", "/status  /last  /runs  /model  /remote  /clear  /quit"},
			unwantedStatus:    []string{"Task Completed"},
			unwantedAssistant: []string{"hello world"},
		},
		{
			name:              "status",
			initial:           "/status",
			wantStatus:        []string{"local · conversation default · model gpt-4o · status ready · phase idle"},
			unwantedStatus:    []string{"Task Completed"},
			unwantedAssistant: []string{"hello world"},
		},
		{
			name:              "message",
			initial:           "hello",
			wantStatus:        []string{"Task Completed"},
			wantAssistant:     []string{"hello world"},
			unwantedStatus:    []string{"Quick Start"},
			unwantedAssistant: []string{"[system]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := acp.NewServer(fakeGateway{}, acp.ServerConfig{
				DefaultSessionKey: "default",
			})
			client, err := acp.NewInProcessClient(context.Background(), server)
			if err != nil {
				t.Fatalf("NewInProcessClient() error = %v", err)
			}
			t.Cleanup(func() { client.Close() })

			var status strings.Builder
			var assistant strings.Builder
			repl, err := New(Config{
				Client:         client,
				Service:        service,
				Prompter:       &scriptedPrompter{lines: []string{"/exit"}},
				Renderer:       NewSplitRenderer(&status, &assistant, false),
				History:        NewHistory("", 10),
				SessionKey:     "default",
				InitialMessage: tt.initial,
				OneShot:        true,
				Version:        "test",
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if err := repl.Run(context.Background()); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			statusText := status.String()
			assistantText := assistant.String()
			for _, want := range tt.wantStatus {
				if !strings.Contains(statusText, want) {
					t.Fatalf("status output missing %q: %q", want, statusText)
				}
			}
			for _, want := range tt.wantAssistant {
				if !strings.Contains(assistantText, want) {
					t.Fatalf("assistant output missing %q: %q", want, assistantText)
				}
			}
			for _, unwanted := range tt.unwantedStatus {
				if strings.Contains(statusText, unwanted) {
					t.Fatalf("status output unexpectedly contained %q: %q", unwanted, statusText)
				}
			}
			for _, unwanted := range tt.unwantedAssistant {
				if strings.Contains(assistantText, unwanted) {
					t.Fatalf("assistant output unexpectedly contained %q: %q", unwanted, assistantText)
				}
			}
		})
	}
}

func TestREPLRunOneShotChatReplyDoesNotRenderTaskArtifacts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := acp.NewServer(chatReplyGateway{}, acp.ServerConfig{
		DefaultSessionKey: "default",
	})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	t.Cleanup(func() { client.Close() })

	service := &fakeService{
		detail: &SessionDetail{Summary: SessionSummary{ID: "default", Key: "default", Model: "gpt-4o"}},
	}
	var status strings.Builder
	var assistant strings.Builder
	repl, err := New(Config{
		Client:         client,
		Service:        service,
		Prompter:       &scriptedPrompter{},
		Renderer:       NewSplitRenderer(&status, &assistant, false),
		History:        NewHistory("", 10),
		SessionKey:     "default",
		InitialMessage: "hi",
		OneShot:        true,
		Version:        "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := assistant.String(); !strings.Contains(got, "I am here.") {
		t.Fatalf("assistant output = %q, want chat reply", got)
	}
	statusText := status.String()
	for _, unwanted := range []string{"Task Completed", "任务完成", "[task]", "/last  /runs recent", "* Completed"} {
		if strings.Contains(statusText, unwanted) {
			t.Fatalf("status output unexpectedly contained %q: %q", unwanted, statusText)
		}
	}
}

func TestREPLUsesACPCommandsUpdateBeforeFirstPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := acp.NewServer(fakeGateway{}, acp.ServerConfig{
		DefaultSessionKey: "default",
		CommandProvider: func(context.Context) ([]acp.Command, error) {
			return []acp.Command{{Name: "review-pr", Description: "Review a PR"}}, nil
		},
	})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	service := &fakeService{
		detail: &SessionDetail{Summary: SessionSummary{ID: "default", Key: "default", Model: "gpt-4o"}},
	}
	var output strings.Builder
	repl, err := New(Config{
		Client:     client,
		Service:    service,
		Prompter:   &scriptedPrompter{lines: []string{"/help", "/exit"}},
		Renderer:   NewRenderer(&output, false),
		History:    NewHistory("", 10),
		SessionKey: "default",
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "Runtime inventory:") || !strings.Contains(got, "(reference only)") {
		t.Fatalf("output = %q, want lightweight runtime inventory hint", got)
	}
	if strings.Contains(got, "Runtime Inventory (reference only)") || strings.Contains(got, "not REPL slash commands") {
		t.Fatalf("output = %q, want first-screen help to stay lightweight", got)
	}
}

func TestREPLHelpTopicShowsDynamicInventoryAsAdvisory(t *testing.T) {
	registry := NewCommandRegistry()
	registry.SetDynamic([]acp.Command{{Name: "review-pr", Description: "Review a PR"}})
	repl := &REPL{commands: registry}

	title, lines, _ := repl.helpTopic([]string{"review-pr"})
	if title != "Runtime Inventory: review-pr" {
		t.Fatalf("title = %q, want %q", title, "Runtime Inventory: review-pr")
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{
		"Inventory entry: review-pr",
		"Source: connected runtime inventory",
		"Status: reference only",
		"does not execute runtime inventory entries as slash commands",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("help lines missing %q: %q", want, text)
		}
	}
}

func TestREPLRunOneShotDynamicSlashCommandDoesNotSubmitToModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := acp.NewServer(fakeGateway{}, acp.ServerConfig{
		DefaultSessionKey: "default",
		CommandProvider: func(context.Context) ([]acp.Command, error) {
			return []acp.Command{{Name: "review-pr", Description: "Review a PR"}}, nil
		},
	})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	t.Cleanup(func() { client.Close() })

	service := &fakeService{
		detail: &SessionDetail{Summary: SessionSummary{ID: "default", Key: "default", Model: "gpt-4o"}},
	}
	var status strings.Builder
	var assistant strings.Builder
	repl, err := New(Config{
		Client:         client,
		Service:        service,
		Prompter:       &scriptedPrompter{},
		Renderer:       NewSplitRenderer(&status, &assistant, false),
		History:        NewHistory("", 10),
		SessionKey:     "default",
		InitialMessage: "/review-pr 123",
		OneShot:        true,
		Version:        "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	statusText := status.String()
	if !strings.Contains(statusText, `"/review-pr" is not a REPL slash command`) {
		t.Fatalf("status output = %q, want explicit unsupported-command error", statusText)
	}
	for _, unwanted := range []string{"Task Completed", "* Completed"} {
		if strings.Contains(statusText, unwanted) {
			t.Fatalf("status output unexpectedly contained %q: %q", unwanted, statusText)
		}
	}
	if assistant.Len() != 0 {
		t.Fatalf("assistant output = %q, want no model submission", assistant.String())
	}
}

func TestREPLHandlesPermissionApprovalInteraction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := acp.NewServer(approvalGateway{}, acp.ServerConfig{
		DefaultSessionKey: "default",
	})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	service := &fakeService{
		detail: &SessionDetail{Summary: SessionSummary{ID: "default", Key: "default", Model: "gpt-4o"}},
	}
	var output strings.Builder
	repl, err := New(Config{
		Client:         client,
		Service:        service,
		Prompter:       &scriptedPrompter{approvals: []rune{'v', 'a'}},
		Renderer:       NewRenderer(&output, true),
		History:        NewHistory("", 10),
		SessionKey:     "default",
		InitialMessage: "clean logs",
		OneShot:        true,
		Version:        "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(service.resolvedApprovals) != 1 {
		t.Fatalf("resolved approvals = %#v, want one decision", service.resolvedApprovals)
	}
	if !service.resolvedApprovals[0].Approved || service.resolvedApprovals[0].Scope != "session" {
		t.Fatalf("approval decision = %#v, want approved session", service.resolvedApprovals[0])
	}
	for _, want := range []string{"Approval Required", "approval-1", "Approved."} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestApprovalsCommandUsesInteractiveQueueAndQuickResolve(t *testing.T) {
	registry := NewCommandRegistry()
	prompter := &panelAwarePrompter{}
	service := &fakeService{
		approvals: []ApprovalSummary{{
			ID:            "approval-1",
			Status:        "pending",
			ToolName:      "exec.shell",
			PolicySummary: "remote write requires approval",
			CreatedAt:     "2026-04-05T18:00:00Z",
		}},
	}
	repl := &REPL{
		renderer: NewRenderer(io.Discard, true),
		service:  service,
		commands: registry,
		prompter: prompter,
	}

	if _, err := registry.Execute(context.Background(), repl, "/approvals"); err != nil {
		t.Fatalf("Execute(/approvals) error = %v", err)
	}
	panel, ok := repl.panelController.(*selectionPanel)
	if !ok || panel == nil {
		t.Fatalf("panelController = %#v, want *selectionPanel", repl.panelController)
	}
	if !strings.Contains(panel.actions, "a approve") || !strings.Contains(panel.actions, "n deny") {
		t.Fatalf("panel.actions = %q, want quick resolve actions", panel.actions)
	}

	result, err := panel.HandleOverlayKey(richedit.KeyEvent{Action: richedit.ActionSubmit})
	if err != nil {
		t.Fatalf("HandleOverlayKey(submit) error = %v", err)
	}
	if !result.Handled {
		t.Fatal("expected Enter to be handled")
	}
	if !strings.Contains(panel.status, "approval-1") || !strings.Contains(panel.status, "remote write requires approval") {
		t.Fatalf("panel.status = %q, want detail summary", panel.status)
	}

	if _, err := panel.hotkeys['a'](panel, firstPanelItem(panel.items)); err != nil {
		t.Fatalf("approval hotkey error = %v", err)
	}
	if len(panel.items) != 0 {
		t.Fatalf("panel.items = %#v, want empty after resolving pending approval", panel.items)
	}
	if !strings.Contains(panel.status, "Approval approval-1 approved.") {
		t.Fatalf("panel.status = %q, want resolved message", panel.status)
	}
}

func TestTargetCommandListsAvailableTargets(t *testing.T) {
	registry := NewCommandRegistry()
	manager := &fakeTargetManager{
		current: TargetInfo{Name: "local"},
		targets: []TargetInfo{
			{Name: "local-dev", Description: "http://127.0.0.1:16280"},
			{Name: "local", Description: "Private local conversation"},
		},
	}
	var output strings.Builder
	repl := &REPL{
		targetName:    "local",
		targetManager: manager,
		renderer:      NewRenderer(&output, false),
		commands:      registry,
		prompt:        &DynamicPrompt{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/remote"); err != nil {
		t.Fatalf("Execute(/remote) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{
		"[panel] Remotes",
		"local-dev        http://127.0.0.1:16280",
		"> local            Private local conversation  current",
		"Actions: Enter switch  l login  i inspect  o logout  Esc back",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q: %q", want, text)
		}
	}
}

func TestTargetCommandSwitchesTargetAndResetsSessionBinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry := NewCommandRegistry()
	oldClient := newTestACPClient(t)
	defer oldClient.Close()
	newClient := newTestACPClient(t)
	oldService := &fakeService{}
	newService := &fakeService{}
	manager := &fakeTargetManager{
		current: TargetInfo{Name: "local", Kind: "local"},
		binding: &TargetBinding{
			Client:       newClient,
			Service:      newService,
			Target:       TargetInfo{Name: "local-dev", Kind: "local", Description: "http://127.0.0.1:16280"},
			SessionID:    "sess-2",
			SessionKey:   "cli-2",
			SessionModel: "claude-3-7-sonnet",
			Models:       []ModelInfo{{ID: "claude-3-7-sonnet", ContextWindow: 200000}},
			Commands:     []acp.Command{{Name: "review-pr", Description: "Review a PR"}},
		},
	}
	var output strings.Builder
	repl, err := New(Config{
		Client:        oldClient,
		Service:       oldService,
		Target:        "local",
		TargetManager: manager,
		Prompter:      &scriptedPrompter{lines: []string{"y"}},
		Renderer:      NewRenderer(&output, false),
		History:       NewHistory("", 10),
		SessionKey:    "cli-1",
		Model:         "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	repl.sessionID = "sess-1"
	repl.sessionModel = "gpt-4o"

	if _, err := registry.Execute(context.Background(), repl, "/remote local-dev"); err != nil {
		t.Fatalf("Execute(/remote local-dev) error = %v", err)
	}

	if manager.switchName != "local-dev" {
		t.Fatalf("switchName = %q, want %q", manager.switchName, "local-dev")
	}
	if repl.targetName != "local-dev" {
		t.Fatalf("repl.targetName = %q, want %q", repl.targetName, "local-dev")
	}
	if repl.targetKind != "local" {
		t.Fatalf("repl.targetKind = %q, want %q", repl.targetKind, "local")
	}
	if repl.sessionID != "sess-2" || repl.sessionKey != "cli-2" {
		t.Fatalf("session = (%q, %q), want (%q, %q)", repl.sessionID, repl.sessionKey, "sess-2", "cli-2")
	}
	if repl.sessionModel != "claude-3-7-sonnet" {
		t.Fatalf("sessionModel = %q, want %q", repl.sessionModel, "claude-3-7-sonnet")
	}
	if repl.selectedModel != "" {
		t.Fatalf("selectedModel = %q, want cleared on remote switch", repl.selectedModel)
	}
	if got := repl.prompt.Input(); got != "> " {
		t.Fatalf("prompt.Input() = %q, want quiet workbench prompt", got)
	}
	if !oldService.closed {
		t.Fatal("expected previous service to be closed")
	}
	commands := repl.commands.DynamicCommands()
	if len(commands) != 1 || commands[0].Name != "review-pr" {
		t.Fatalf("dynamic commands = %#v, want review-pr", commands)
	}
	if !strings.Contains(output.String(), "Switched to local runtime local-dev.") || !strings.Contains(output.String(), "Conversation binding moved to the selected runtime target.") {
		t.Fatalf("output = %q, want switch confirmation", output.String())
	}
}

func TestTargetCommandLoginPromptsAndStoresCredentials(t *testing.T) {
	registry := NewCommandRegistry()
	manager := &fakeTargetManager{}
	var output strings.Builder
	repl := &REPL{
		targetName:    "local",
		targetManager: manager,
		prompter:      &scriptedPrompter{secrets: []string{"secret-token"}},
		renderer:      NewRenderer(&output, false),
		commands:      registry,
		prompt:        &DynamicPrompt{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/remote login prod"); err != nil {
		t.Fatalf("Execute(/remote login prod) error = %v", err)
	}
	if manager.loginName != "prod" || manager.loginToken != "secret-token" {
		t.Fatalf("login call = (%q, %q)", manager.loginName, manager.loginToken)
	}
	if !strings.Contains(output.String(), "Saved credentials for remote prod") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestTargetCommandLogoutClearsCredentials(t *testing.T) {
	registry := NewCommandRegistry()
	manager := &fakeTargetManager{}
	var output strings.Builder
	repl := &REPL{
		targetName:    "local",
		targetManager: manager,
		prompter:      &scriptedPrompter{},
		renderer:      NewRenderer(&output, false),
		commands:      registry,
		prompt:        &DynamicPrompt{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/remote logout prod"); err != nil {
		t.Fatalf("Execute(/remote logout prod) error = %v", err)
	}
	if manager.logoutName != "prod" {
		t.Fatalf("logoutName = %q, want %q", manager.logoutName, "prod")
	}
	if !strings.Contains(output.String(), "Cleared credentials for remote prod") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestMemoryCommandRendersInspectsPinsAndDeletes(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		memoryItems: []agent.MemoryEntry{{
			Key:             "deploy_server",
			Value:           "198.51.100.42",
			Label:           "server",
			Namespace:       "project",
			Source:          "user",
			SessionKey:      "default",
			EvidenceCount:   2,
			CorrectionCount: 1,
			PreviousValues:  []string{"198.51.100.43"},
			UpdatedAt:       time.Now().UTC(),
		}},
	}
	var output strings.Builder
	repl := &REPL{
		renderer:   NewRenderer(&output, false),
		service:    service,
		sessionKey: "default",
		prompter:   &scriptedPrompter{lines: []string{"y"}},
		commands:   registry,
	}

	for _, command := range []string{
		"/memory",
		"/memory inspect deploy_server",
		"/memory pin Remember the staging host",
		"/memory delete deploy_server",
	} {
		if _, err := registry.Execute(context.Background(), repl, command); err != nil {
			t.Fatalf("Execute(%s) error = %v", command, err)
		}
	}

	text := output.String()
	for _, want := range []string{
		"[panel] Memory",
		"deploy_server",
		"you",
		"conversation · current",
		"[panel] Memory Detail",
		"Kind: conversation memory",
		"Saved by: you",
		"Applies to: current conversation",
		"State: active",
		"Updates: 1",
		"Supporting items: 2",
		"Previous values",
		"198.51.100.43",
		"[panel] Memory Pinned",
		"Deleted memory deploy_server.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("memory workflow output missing %q: %q", want, text)
		}
	}
	if _, item := service.findMemory("deploy_server"); item != nil {
		t.Fatalf("deploy_server should have been deleted, memoryItems=%#v", service.memoryItems)
	}
	if _, item := service.findMemory("memory-2"); item == nil {
		t.Fatalf("pinned memory was not stored, memoryItems=%#v", service.memoryItems)
	}
}

func TestMemoryCommandInteractiveDeleteUsesConfirmPanel(t *testing.T) {
	registry := NewCommandRegistry()
	prompter := &panelAwarePrompter{}
	service := &fakeService{
		memoryItems: []agent.MemoryEntry{{
			Key:        "deploy_server",
			Value:      "198.51.100.42",
			Label:      "server",
			Source:     "user",
			SessionKey: "default",
			UpdatedAt:  time.Now().UTC(),
		}},
	}
	repl := &REPL{
		renderer:   NewRenderer(io.Discard, true),
		service:    service,
		sessionKey: "default",
		commands:   registry,
		prompter:   prompter,
	}

	if _, err := registry.Execute(context.Background(), repl, "/memory"); err != nil {
		t.Fatalf("Execute(/memory) error = %v", err)
	}
	panel, ok := repl.panelController.(*selectionPanel)
	if !ok || panel == nil {
		t.Fatalf("panelController = %#v, want *selectionPanel", repl.panelController)
	}

	if _, err := panel.hotkeys['d'](panel, firstPanelItem(panel.items)); err != nil {
		t.Fatalf("delete hotkey error = %v", err)
	}
	confirm, ok := repl.panelController.(*confirmPanel)
	if !ok || confirm == nil {
		t.Fatalf("panelController = %#v, want *confirmPanel", repl.panelController)
	}
	if confirm.title != "Delete Memory" {
		t.Fatalf("confirm.title = %q, want %q", confirm.title, "Delete Memory")
	}

	result, err := confirm.HandleOverlayKey(richedit.KeyEvent{Action: richedit.ActionInsertRune, Rune: 'y'})
	if err != nil {
		t.Fatalf("HandleOverlayKey(confirm) error = %v", err)
	}
	if strings.TrimSpace(result.Submit) == "" {
		t.Fatalf("confirm result = %#v, want submit command", result)
	}
	if _, err := registry.Execute(context.Background(), repl, result.Submit); err != nil {
		t.Fatalf("Execute(%s) error = %v", result.Submit, err)
	}
	if _, item := service.findMemory("deploy_server"); item != nil {
		t.Fatalf("deploy_server should have been deleted, memoryItems=%#v", service.memoryItems)
	}
}

func TestMemoryCommandDeleteTreatsPromptInterruptAsCancel(t *testing.T) {
	registry := NewCommandRegistry()
	var output bytes.Buffer
	service := &fakeService{
		memoryItems: []agent.MemoryEntry{{
			Key:        "deploy_server",
			Value:      "198.51.100.42",
			Label:      "server",
			Source:     "user",
			SessionKey: "default",
			UpdatedAt:  time.Now().UTC(),
		}},
	}
	repl := &REPL{
		renderer:   NewRenderer(&output, false),
		service:    service,
		sessionKey: "default",
		commands:   registry,
		prompter:   &scriptedPrompter{lines: []string{"<interrupt>"}},
	}

	if _, err := registry.Execute(context.Background(), repl, "/memory delete deploy_server"); err != nil {
		t.Fatalf("Execute(/memory delete deploy_server) error = %v", err)
	}
	if _, item := service.findMemory("deploy_server"); item == nil {
		t.Fatalf("deploy_server should still exist after interrupted confirm, memoryItems=%#v", service.memoryItems)
	}
	if !strings.Contains(output.String(), "Memory deletion cancelled.") {
		t.Fatalf("output = %q, want cancellation feedback", output.String())
	}
}

func TestRefreshViewStatePrefersForegroundAttention(t *testing.T) {
	repl := &REPL{
		renderer: NewRenderer(&strings.Builder{}, false),
		supervisorSnapshot: &SupervisorSnapshot{
			ForegroundRunID: "run-2",
			AttentionCount:  2,
			Items: []RunSummary{
				{ID: "run-1", Attention: "approval"},
				{ID: "run-2", Attention: "error"},
			},
		},
	}

	repl.refreshViewState()

	if repl.viewState.AttentionPrimary != "error" {
		t.Fatalf("AttentionPrimary = %q, want %q", repl.viewState.AttentionPrimary, "error")
	}
}

func TestHandleUpdateDedupesConsecutiveToolStatusLines(t *testing.T) {
	var output strings.Builder
	repl := &REPL{renderer: NewRenderer(&output, false)}

	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status:     acp.SessionToolUse,
		ToolName:   "fs.read",
		ToolOutput: "read docs/HopClaw 实施版总方案.md",
	}); err != nil {
		t.Fatalf("handleUpdate(tool 1) error = %v", err)
	}
	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status:     acp.SessionToolUse,
		ToolName:   "fs.read",
		ToolOutput: "read docs/HopClaw 实施版总方案.md",
	}); err != nil {
		t.Fatalf("handleUpdate(tool 2) error = %v", err)
	}
	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status:    acp.SessionStreaming,
		TextDelta: "done\n",
	}); err != nil {
		t.Fatalf("handleUpdate(stream) error = %v", err)
	}

	got := output.String()
	if strings.Count(got, "[tool] fs.read — read docs/HopClaw 实施版总方案.md") != 1 {
		t.Fatalf("tool status was not deduped: %q", got)
	}
	if !strings.Contains(got, "done\n") {
		t.Fatalf("streaming output missing final delta: %q", got)
	}
	if !strings.Contains(got, "status=running") {
		t.Fatalf("dock summary missing from updated output: %q", got)
	}
}

func TestHandleUpdateCachesTimelineForLastRun(t *testing.T) {
	var output strings.Builder
	service := &fakeService{
		runs: []RunSummary{{
			ID:        "run-1",
			SessionID: "sess-1",
			Status:    "completed",
			Phase:     "completed",
		}},
	}
	repl := &REPL{
		renderer:     NewRenderer(&output, false),
		service:      service,
		sessionID:    "sess-1",
		runStartedAt: time.Now().Add(-2 * time.Second),
	}

	repl.startOrUpdateToolTimeline("fs.read", "read plan")
	if repl.activeTimeline == nil {
		t.Fatal("activeTimeline should be created")
	}
	repl.activeTimeline.StartedAt = time.Now().Add(-1500 * time.Millisecond)

	if err := repl.handleUpdate(acp.SessionUpdateNotification{Status: acp.SessionCompleted}); err != nil {
		t.Fatalf("handleUpdate() error = %v", err)
	}

	timeline := repl.timelineForRun("run-1")
	if len(timeline) != 1 {
		t.Fatalf("timelineForRun() = %#v, want 1 item", timeline)
	}
	if timeline[0].Name != "fs.read" || timeline[0].Status != "ok" {
		t.Fatalf("timeline[0] = %#v", timeline[0])
	}
	if timeline[0].Duration < time.Second {
		t.Fatalf("timeline duration = %s, want >= 1s", timeline[0].Duration)
	}
}

func TestHandleUpdateRendersTaskSnapshots(t *testing.T) {
	var output strings.Builder
	service := &fakeService{
		runs: []RunSummary{{
			ID:         "run-1",
			SessionID:  "sess-1",
			SessionKey: "ops",
			Status:     "running",
			Phase:      "executing_tools",
		}},
	}
	repl := &REPL{
		renderer:     NewRenderer(&output, false),
		service:      service,
		sessionID:    "sess-1",
		sessionKey:   "ops",
		targetName:   "prod-eu",
		runStartedAt: time.Now().Add(-20 * time.Second),
		running:      true,
	}

	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status:     acp.SessionToolUse,
		ToolName:   "audit.deliveries",
		ToolOutput: "retry queue",
	}); err != nil {
		t.Fatalf("handleUpdate(tool_use) error = %v", err)
	}
	repl.lastUsage = &acp.UsageInfo{PromptTokens: 16300, CompletionTokens: 1800}
	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status: acp.SessionCompleted,
		Usage:  repl.lastUsage,
	}); err != nil {
		t.Fatalf("handleUpdate(completed) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{
		"[task] Current Task · run-1",
		"Task Completed",
		"Duration",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("snapshot output missing %q: %q", want, text)
		}
	}
}

func TestHandleUpdateRendersProgressSnapshotAfterLongRunToolChange(t *testing.T) {
	var output strings.Builder
	repl := &REPL{
		renderer:     NewRenderer(&output, false),
		service:      &fakeService{},
		sessionID:    "sess-1",
		sessionKey:   "ops",
		currentRunID: "run-1",
		targetName:   "prod-eu",
		runStartedAt: time.Now().Add(-20 * time.Second),
		running:      true,
		snapshotTracker: snapshotTracker{
			planShown:        true,
			lastByKey:        map[string]time.Time{},
			lastProgressTool: "fs.read",
		},
	}

	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status:     acp.SessionToolUse,
		ToolName:   "exec.shell",
		ToolOutput: "go test ./...",
	}); err != nil {
		t.Fatalf("handleUpdate(tool_use) error = %v", err)
	}

	if got := output.String(); !strings.Contains(got, "[task] Current Task · run-1") {
		t.Fatalf("progress snapshot missing from output: %q", got)
	}
}

func TestREPLRefreshViewStateCarriesLastFailureSummary(t *testing.T) {
	repl := &REPL{
		targetName:  "prod-eu",
		renderer:    NewRenderer(&strings.Builder{}, false),
		phase:       PhaseError,
		lastFailure: "gateway unreachable",
		lastTimeline: []ToolTimelineEntry{
			{Name: "audit.deliveries", Status: "error", Summary: "dead letter queue"},
		},
	}

	repl.refreshViewState()

	if repl.viewState.Profile != ProfileOps {
		t.Fatalf("viewState.Profile = %q, want %q", repl.viewState.Profile, ProfileOps)
	}
	if repl.viewState.LastFailure != "gateway unreachable" {
		t.Fatalf("viewState.LastFailure = %q, want %q", repl.viewState.LastFailure, "gateway unreachable")
	}
}

func TestREPLRefreshViewStateUsesCatalogDefaultModel(t *testing.T) {
	repl := &REPL{
		targetName: "local",
		renderer:   NewRenderer(&strings.Builder{}, false),
		modelCache: []ModelInfo{
			{ID: "gpt-5.4", ContextWindow: 200000},
			{ID: "gpt-5.4-mini", ContextWindow: 128000},
		},
	}

	repl.refreshViewState()

	if repl.viewState.Model != "gpt-5.4" {
		t.Fatalf("viewState.Model = %q, want %q", repl.viewState.Model, "gpt-5.4")
	}
}

func TestREPLRefreshViewStateKeepsIdleHeuristicsZeroValued(t *testing.T) {
	repl := &REPL{
		targetName: "local",
		renderer:   NewRenderer(&strings.Builder{}, false),
		running:    true,
	}

	repl.refreshViewState()

	if repl.viewState.ExecutionState != "running" {
		t.Fatalf("ExecutionState = %q, want %q", repl.viewState.ExecutionState, "running")
	}
	if repl.viewState.QueueDepth != 0 {
		t.Fatalf("QueueDepth = %d, want 0 without supervisor projection", repl.viewState.QueueDepth)
	}
	if repl.viewState.Channel != "" {
		t.Fatalf("Channel = %q, want empty", repl.viewState.Channel)
	}
	if repl.viewState.Sandbox != "" {
		t.Fatalf("Sandbox = %q, want empty", repl.viewState.Sandbox)
	}
	if repl.viewState.Quality != "" {
		t.Fatalf("Quality = %q, want empty", repl.viewState.Quality)
	}
}

func TestREPLRefreshViewStateKeepsNamedLocalRuntimeLocal(t *testing.T) {
	repl := &REPL{
		targetName: "local-dev",
		targetKind: "local",
		renderer:   NewRenderer(&strings.Builder{}, false),
	}

	repl.refreshViewState()

	if repl.viewState.Runtime != "local" {
		t.Fatalf("Runtime = %q, want %q", repl.viewState.Runtime, "local")
	}
	if repl.viewState.TargetKind != "local" {
		t.Fatalf("TargetKind = %q, want %q", repl.viewState.TargetKind, "local")
	}
	if repl.viewState.Profile != ProfileCoding {
		t.Fatalf("Profile = %q, want %q", repl.viewState.Profile, ProfileCoding)
	}
}

func TestHandleUpdateTransitionsPhasesOncePerChange(t *testing.T) {
	var output strings.Builder
	repl := &REPL{renderer: NewRenderer(&output, false), phase: PhaseIdle}

	repl.transitionPhase(PhaseThinking, "")
	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status:     acp.SessionToolUse,
		ToolName:   "fs.read",
		ToolOutput: "read plan",
	}); err != nil {
		t.Fatalf("handleUpdate(tool 1) error = %v", err)
	}
	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status:     acp.SessionToolUse,
		ToolName:   "fs.read",
		ToolOutput: "read plan",
	}); err != nil {
		t.Fatalf("handleUpdate(tool 2) error = %v", err)
	}
	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status:    acp.SessionStreaming,
		TextDelta: "part one",
	}); err != nil {
		t.Fatalf("handleUpdate(stream 1) error = %v", err)
	}
	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status:    acp.SessionStreaming,
		TextDelta: "part two",
	}); err != nil {
		t.Fatalf("handleUpdate(stream 2) error = %v", err)
	}

	got := output.String()
	if strings.Count(got, "* Running tools: fs.read") != 1 {
		t.Fatalf("tool phase should render once per phase change: %q", got)
	}
	for _, want := range []string{"* Thinking", "* Processing results", "* Delivering response"} {
		if !strings.Contains(got, want) {
			t.Fatalf("phase output missing %q: %q", want, got)
		}
	}
	if repl.phase != PhaseDelivering {
		t.Fatalf("repl.phase = %q, want %q", repl.phase, PhaseDelivering)
	}
}

func TestHandlePermissionDeniesOnNonTTY(t *testing.T) {
	var output strings.Builder
	service := &fakeService{}
	repl := &REPL{
		renderer: NewRenderer(&output, false),
		service:  service,
		prompt:   &DynamicPrompt{},
	}

	err := repl.handlePermission(context.Background(), acp.PermissionRequest{
		RequestID:   "approval-2",
		ToolName:    "exec.shell",
		Description: "Delete temporary logs",
		Input:       `{"cmd":"rm -rf /tmp/logs"}`,
	})
	if err != nil {
		t.Fatalf("handlePermission() error = %v", err)
	}
	if len(service.resolvedApprovals) != 1 || service.resolvedApprovals[0].Approved {
		t.Fatalf("resolved approvals = %#v, want one denied decision", service.resolvedApprovals)
	}
	if !strings.Contains(output.String(), "Non-interactive mode: denied.") {
		t.Fatalf("output = %q, want non-interactive denial message", output.String())
	}
}

func TestHandlePermissionCtrlLClearsAndRerendersApprovalCard(t *testing.T) {
	var output strings.Builder
	service := &fakeService{}
	repl := &REPL{
		renderer: NewRenderer(&output, true),
		service:  service,
		prompt:   &DynamicPrompt{},
		prompter: &scriptedPrompter{approvals: []rune{12, 'y'}},
	}

	err := repl.handlePermission(context.Background(), acp.PermissionRequest{
		RequestID:   "approval-3",
		ToolName:    "exec.shell",
		Description: "Delete temporary logs",
		Input:       `{"cmd":"rm -rf /tmp/logs"}`,
	})
	if err != nil {
		t.Fatalf("handlePermission() error = %v", err)
	}
	if len(service.resolvedApprovals) != 1 || !service.resolvedApprovals[0].Approved {
		t.Fatalf("resolved approvals = %#v, want one approved decision", service.resolvedApprovals)
	}
	got := output.String()
	if !strings.Contains(got, "\033[2J\033[H") {
		t.Fatalf("output = %q, want clear-screen sequence", got)
	}
	if strings.Count(got, "Approval Required") < 2 {
		t.Fatalf("output = %q, want approval card to render again after ctrl+l", got)
	}
	if !strings.Contains(got, "Approved.") {
		t.Fatalf("output = %q, want approval resolution message", got)
	}
}

func TestHandlePermissionCtrlCShowsQuitConfirmationAndBack(t *testing.T) {
	var output strings.Builder
	service := &fakeService{}
	repl := &REPL{
		renderer: NewRenderer(&output, true),
		service:  service,
		prompt:   &DynamicPrompt{},
		prompter: &scriptedPrompter{approvals: []rune{3, 'b', 'y'}},
		running:  true,
		escFactory: func() (<-chan rune, func(), error) {
			return nil, func() {}, nil
		},
	}

	err := repl.handlePermission(context.Background(), acp.PermissionRequest{
		RequestID:   "approval-4",
		ToolName:    "exec.shell",
		Description: "Delete temporary logs",
		Input:       `{"cmd":"rm -rf /tmp/logs"}`,
	})
	if err != nil {
		t.Fatalf("handlePermission() error = %v", err)
	}
	if len(service.resolvedApprovals) != 1 || !service.resolvedApprovals[0].Approved {
		t.Fatalf("resolved approvals = %#v, want one approved decision after backing out of quit confirmation", service.resolvedApprovals)
	}
	if repl.quitConfirmPending {
		t.Fatal("quitConfirmPending = true, want false after backing out")
	}
	got := output.String()
	if !strings.Contains(got, "Quit HopClaw?") {
		t.Fatalf("output = %q, want quit confirmation card", got)
	}
	if strings.Count(got, "Approval Required") < 2 {
		t.Fatalf("output = %q, want approval card to render again after backing out", got)
	}
	if !strings.Contains(got, "Approved.") {
		t.Fatalf("output = %q, want approval resolution message", got)
	}
}

func TestHandlePermissionCtrlCQuitExitsWithoutResolvingApproval(t *testing.T) {
	var output strings.Builder
	service := &fakeService{}
	repl := &REPL{
		renderer: NewRenderer(&output, true),
		service:  service,
		prompt:   &DynamicPrompt{},
		prompter: &scriptedPrompter{approvals: []rune{3, 'q'}},
		running:  true,
		escFactory: func() (<-chan rune, func(), error) {
			return nil, func() {}, nil
		},
	}
	exitCode := -1
	repl.exitFn = func(code int) {
		exitCode = code
	}

	err := repl.handlePermission(context.Background(), acp.PermissionRequest{
		RequestID:   "approval-5",
		ToolName:    "exec.shell",
		Description: "Delete temporary logs",
		Input:       `{"cmd":"rm -rf /tmp/logs"}`,
	})
	if !errors.Is(err, errREPLExitRequested) {
		t.Fatalf("handlePermission() error = %v, want %v", err, errREPLExitRequested)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	if len(service.resolvedApprovals) != 0 {
		t.Fatalf("resolved approvals = %#v, want no approval decision after quit", service.resolvedApprovals)
	}
	if repl.pendingApproval {
		t.Fatal("pendingApproval = true, want false after quit")
	}
	if !strings.Contains(output.String(), "Quit HopClaw?") {
		t.Fatalf("output = %q, want quit confirmation card", output.String())
	}
}

func TestHandleUpdateCompletionRefreshesDeliveryRetryRail(t *testing.T) {
	originalGetSize := termGetSize
	termGetSize = func(int) (int, int, error) { return 132, 24, nil }
	t.Cleanup(func() { termGetSize = originalGetSize })

	var output strings.Builder
	service := &fakeService{
		runs: []RunSummary{{
			ID:        "run-1",
			SessionID: "sess-1",
			Status:    "completed",
			Phase:     "completed",
		}},
		runDetails: map[string]*RunDetail{
			"run-1": {
				Run: RunSummary{
					ID:        "run-1",
					SessionID: "sess-1",
					Status:    "completed",
					Phase:     "completed",
				},
				Delivery: &RunDelivery{
					Status:      "retrying",
					Attempt:     "2/5",
					NextAttempt: "12:46",
					Summary:     "webhook retry",
				},
			},
		},
	}
	repl := &REPL{
		renderer:       NewRenderer(&output, true),
		service:        service,
		sessionID:      "sess-1",
		sessionKey:     "ops-incident",
		targetName:     "local",
		sessionModel:   "gpt-5.4",
		layoutMode:     LayoutFull,
		runStartedAt:   time.Now().Add(-41 * time.Second),
		lastRunID:      "run-1",
		currentRunID:   "run-1",
		running:        true,
		phase:          PhaseDelivering,
		lastTimeline:   []ToolTimelineEntry{{Name: "audit.deliveries", Status: "ok", Duration: 2 * time.Second}},
		currentProject: &agent.Project{Name: "hopclaw"},
	}

	if err := repl.handleUpdate(acp.SessionUpdateNotification{
		Status: acp.SessionCompleted,
		Usage:  &acp.UsageInfo{PromptTokens: 16300, CompletionTokens: 1800, TotalTokens: 18100},
	}); err != nil {
		t.Fatalf("handleUpdate(completed) error = %v", err)
	}

	if repl.deliveryState.State != "retrying" {
		t.Fatalf("deliveryState.State = %q, want %q", repl.deliveryState.State, "retrying")
	}
	got := output.String()
	if !strings.Contains(got, "COMPLETED") && !strings.Contains(got, "Delivery") {
		t.Fatalf("output missing delivery/completion state: %q", got)
	}
}

func TestREPLPauseResumeAndDiscardStateMachine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	gateway := &cancellableGateway{
		cancelCh:  make(chan struct{}),
		startedCh: make(chan struct{}),
	}
	server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	service := &fakeService{
		detail: &SessionDetail{Summary: SessionSummary{ID: "default", Key: "default", Model: "gpt-4o"}},
	}
	var output strings.Builder
	repl, err := New(Config{
		Client:     client,
		Service:    service,
		Prompter:   &scriptedPrompter{},
		Renderer:   NewRenderer(&output, false),
		History:    NewHistory("", 10),
		SessionKey: "default",
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := client.Initialize(context.Background(), acp.InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      acp.Implementation{Name: "test-repl", Version: "test"},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := repl.switchSession(context.Background(), "default", false); err != nil {
		t.Fatalf("switchSession() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- repl.submit(context.Background(), "pause me")
	}()

	waitForCancellableRunStart(t, repl, gateway)
	if err := repl.requestPause(context.Background()); err != nil {
		t.Fatalf("requestPause() error = %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("submit() error = %v", err)
	}
	if repl.phase != PhasePaused || repl.pausedRun == nil {
		t.Fatalf("repl paused state = (%q, %#v), want paused with snapshot", repl.phase, repl.pausedRun)
	}
	if got := repl.prompt.Input(); got != "paused> " {
		t.Fatalf("paused prompt = %q, want %q", got, "paused> ")
	}
	if !strings.Contains(output.String(), "Task Paused") {
		t.Fatalf("output = %q, want paused card", output.String())
	}

	if err := repl.resumePaused(context.Background(), false); err != nil {
		t.Fatalf("resumePaused() error = %v", err)
	}
	if repl.pausedRun != nil {
		t.Fatalf("pausedRun = %#v, want cleared after resume", repl.pausedRun)
	}
	if got := repl.prompt.Input(); strings.HasPrefix(got, "paused>") {
		t.Fatalf("prompt after resume = %q, want non-paused prompt", got)
	}
	if !strings.Contains(output.String(), "Task Completed") {
		t.Fatalf("output = %q, want completion card after resume", output.String())
	}

	repl.pausedRun = &pausedRunState{Message: "discard me"}
	repl.prompt.SetPaused(true)
	repl.phase = PhasePaused
	repl.discardPaused()
	if repl.pausedRun != nil {
		t.Fatalf("pausedRun = %#v, want nil after discard", repl.pausedRun)
	}
	if got := repl.prompt.Input(); got == "paused> " {
		t.Fatalf("prompt after discard = %q, want non-paused prompt", got)
	}
}

func TestREPLCancelStateMachineDoesNotCreatePausedHandle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repl, gateway, output := newCancellableRunREPL(t, false)
	registry := NewCommandRegistry()
	repl.commands = registry

	errCh := make(chan error, 1)
	go func() {
		errCh <- repl.submit(context.Background(), "cancel me")
	}()

	waitForCancellableRunStart(t, repl, gateway)
	if _, err := registry.Execute(context.Background(), repl, "/cancel"); err != nil {
		t.Fatalf("Execute(/cancel) error = %v", err)
	}
	if err := awaitSubmitResult(t, errCh, "cancel submit"); err != nil {
		t.Fatalf("submit() error = %v", err)
	}
	if !gateway.cancelled {
		t.Fatal("gateway.cancelled = false, want true after /cancel")
	}
	if repl.phase != PhaseCancelled {
		t.Fatalf("phase = %q, want %q", repl.phase, PhaseCancelled)
	}
	if repl.pausedRun != nil {
		t.Fatalf("pausedRun = %#v, want nil after /cancel", repl.pausedRun)
	}
	if got := repl.prompt.Input(); got == "paused> " {
		t.Fatalf("prompt after cancel = %q, want non-paused prompt", got)
	}
	text := output.String()
	for _, want := range []string{"Cancelling current task", "Task Cancelled"} {
		if !strings.Contains(text, want) {
			t.Fatalf("cancel output missing %q: %q", want, text)
		}
	}
}

func TestRecoveryHintAvoidsRetryWhenNoPausedTaskExists(t *testing.T) {
	safe, next := recoveryHint("gateway unreachable", "prod-eu", "remote")
	if safe == "" {
		t.Fatal("recoveryHint() safe fallback = empty, want recovery guidance")
	}
	if strings.Contains(next, "/retry") {
		t.Fatalf("recoveryHint() next = %q, should not route to /retry without a paused handle", next)
	}
	if !strings.Contains(next, "/last") {
		t.Fatalf("recoveryHint() next = %q, want /last inspection guidance", next)
	}
}

func TestRecoveryHintForNamedLocalRuntimeDoesNotSuggestSwitching(t *testing.T) {
	safe, next := recoveryHint("gateway unreachable", "local-dev", "local")
	if safe == "" {
		t.Fatal("recoveryHint() safe fallback = empty, want local recovery guidance")
	}
	if strings.Contains(strings.ToLower(safe), "switch remote") {
		t.Fatalf("recoveryHint() safe = %q, want no remote switch guidance for local runtime", safe)
	}
	if strings.Contains(next, "/remote list") {
		t.Fatalf("recoveryHint() next = %q, want no remote switch guidance for local runtime", next)
	}
}

func TestREPLRunEmptyPausedPromptResumesTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repl, gateway, output := newCancellableRunREPL(t, false)
	gateway.runCount = 1
	repl.prompter = &scriptedPrompter{lines: []string{""}}
	repl.pausedRun = &pausedRunState{Message: "resume me", LastStep: "audit.deliveries"}
	repl.prompt.SetPaused(true)
	repl.phase = PhasePaused

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	text := output.String()
	for _, want := range []string{"Continuing paused task from the last stable step", "resumed", "Task Completed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("paused enter-resume output missing %q: %q", want, text)
		}
	}
}

func TestREPLRunPromptQuitExitsWithoutSubmitting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repl, gateway, _ := newCancellableRunREPL(t, false)
	repl.prompter = &scriptedPrompter{lines: []string{"<quit>"}}

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gateway.runCount != 0 {
		t.Fatalf("gateway.runCount = %d, want 0 after prompt quit", gateway.runCount)
	}
	if repl.running {
		t.Fatal("repl.running = true, want false after prompt quit")
	}
}

func TestREPLSubmitEscPausesRunningTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repl, gateway, output := newCancellableRunREPL(t, false)
	repl.escFactory = func() (<-chan rune, func(), error) {
		ch := make(chan rune, 1)
		ch <- escKey
		close(ch)
		return ch, func() {}, nil
	}

	if err := repl.submit(context.Background(), "pause me"); err != nil {
		t.Fatalf("submit() error = %v", err)
	}
	if !gateway.cancelled {
		t.Fatal("expected Esc listener to cancel the active run")
	}
	if repl.phase != PhasePaused || repl.pausedRun == nil {
		t.Fatalf("repl paused state = (%q, %#v), want paused with snapshot", repl.phase, repl.pausedRun)
	}
	if !strings.Contains(output.String(), "Task Paused") {
		t.Fatalf("output = %q, want paused card", output.String())
	}
}

func TestREPLRunInterruptShowsQuitConfirmationAndBack(t *testing.T) {
	repl, gateway, output := newCancellableRunREPL(t, true)
	repl.running = true
	repl.escFactory = func() (<-chan rune, func(), error) {
		ch := make(chan rune)
		close(ch)
		return ch, func() {}, nil
	}

	if err := repl.handleRunInterrupt(context.Background()); err != nil {
		t.Fatalf("handleRunInterrupt() error = %v", err)
	}
	if !repl.quitConfirmPending {
		t.Fatal("quitConfirmPending = false, want true after first Ctrl+C")
	}
	if gateway.cancelled {
		t.Fatal("expected first Ctrl+C to keep the run active")
	}
	if !strings.Contains(output.String(), "Quit HopClaw?") || !strings.Contains(output.String(), "Current task is still running.") {
		t.Fatalf("output = %q, want quit confirmation card", output.String())
	}

	if err := repl.handleRunKey(context.Background(), 'b'); err != nil {
		t.Fatalf("handleRunKey(back) error = %v", err)
	}
	if repl.quitConfirmPending {
		t.Fatal("quitConfirmPending = true, want false after backing out")
	}
}

func TestREPLRunInterruptQuitCancelsAndExits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repl, gateway, _ := newCancellableRunREPL(t, true)
	repl.escFactory = func() (<-chan rune, func(), error) {
		ch := make(chan rune, 2)
		ch <- 3
		ch <- 'q'
		close(ch)
		return ch, func() {}, nil
	}
	exitCode := -1
	repl.exitFn = func(code int) {
		exitCode = code
	}

	err := repl.submit(context.Background(), "quit me")
	if !errors.Is(err, errREPLExitRequested) {
		t.Fatalf("submit() error = %v, want %v", err, errREPLExitRequested)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	if !gateway.cancelled {
		t.Fatal("expected quit confirmation to cancel the active run before exit")
	}
}

func TestREPLRunCtrlLRedrawsQuitConfirmation(t *testing.T) {
	repl, _, output := newCancellableRunREPL(t, true)
	repl.running = true
	repl.quitConfirmPending = true

	if err := repl.handleRunKey(context.Background(), 12); err != nil {
		t.Fatalf("handleRunKey(ctrl+l) error = %v", err)
	}
	repl.renderer.StopSpinner()

	text := output.String()
	if !strings.Contains(text, "\033[2J\033[H") {
		t.Fatalf("output = %q, want clear-screen sequence", text)
	}
	if !strings.Contains(text, "Quit HopClaw?") || !strings.Contains(text, "Current task is still running.") {
		t.Fatalf("output = %q, want quit confirmation to be redrawn", text)
	}
}

func TestREPLRunCtrlLClearsAndRedrawsWorkbench(t *testing.T) {
	repl, _, output := newCancellableRunREPL(t, true)
	repl.running = true
	repl.phase = PhaseExecutingTools
	repl.targetName = "prod-eu"
	repl.targetKind = "remote"
	repl.sessionModel = "gpt-5.4"
	repl.lastToolStatus = "audit.deliveries"

	if err := repl.handleRunKey(context.Background(), 12); err != nil {
		t.Fatalf("handleRunKey(ctrl+l) error = %v", err)
	}
	repl.renderer.StopSpinner()

	text := output.String()
	if !strings.Contains(text, "\033[2J\033[H") {
		t.Fatalf("output = %q, want clear-screen sequence", text)
	}
	for _, want := range []string{"REMOTE prod-eu", "MODEL gpt-5.4", "RUNNING"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q: %q", want, text)
		}
	}
}

func TestREPLRunCtrlLRedrawsPassivePromptWorkbench(t *testing.T) {
	var output strings.Builder
	repl := &REPL{
		targetName:   "local",
		sessionKey:   "default",
		sessionModel: "gpt-5.4",
		running:      true,
		phase:        PhaseThinking,
		renderer:     NewRenderer(&output, true),
		prompter:     &TerminalPrompter{tty: true},
	}

	if err := repl.handleRunKey(context.Background(), 12); err != nil {
		t.Fatalf("handleRunKey(ctrl+l) error = %v", err)
	}
	repl.renderer.StopSpinner()

	text := output.String()
	if !strings.Contains(text, "\033[2J\033[H") {
		t.Fatalf("output = %q, want clear-screen sequence", text)
	}
	for _, want := range []string{"LOCAL", "MODEL gpt-5.4", "RUNNING"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q: %q", want, text)
		}
	}
}

func TestREPLRunDispatchesSlashCommandsWithoutSubmittingToModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	gateway := &recordingGateway{}
	server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	repl, err := New(Config{
		Client:   client,
		Service:  &fakeService{detail: &SessionDetail{Summary: SessionSummary{ID: "sess-1", Key: "default", Model: "gpt-4o"}}},
		Prompter: &scriptedPrompter{lines: []string{"/help"}},
		Renderer: NewRenderer(io.Discard, false),
		History:  NewHistory("", 10),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gateway.message != "" {
		t.Fatalf("gateway.message = %q, want slash command to stay local", gateway.message)
	}
	if len(gateway.images) != 0 {
		t.Fatalf("gateway.images = %#v, want no model submission for /help", gateway.images)
	}
}

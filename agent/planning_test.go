package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/meta"
	planpkg "github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/skill"
)

type capturingPlanner struct {
	requests []PlanningRequest
	plan     *planpkg.Plan
}

func (p *capturingPlanner) Plan(_ context.Context, req PlanningRequest) (*planpkg.Plan, error) {
	p.requests = append(p.requests, req)
	if p.plan != nil {
		return clonePlan(p.plan), nil
	}
	return planpkg.TrivialPlan(req.LatestMessage), nil
}

func TestEnsurePlanUsesCurrentRunMessagesOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	planner := &capturingPlanner{}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(planner)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey: "chat-plan-scope",
		Content:    "打开页面，等搜索结果加载出来，再提取前 5 条",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	session, err := sessions.GetOrCreate(ctx, "chat-plan-scope", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	session.Messages = append([]contextengine.Message{
		{
			Role:      contextengine.RoleUser,
			Content:   "旧任务：填写表单并提交",
			CreatedAt: time.Now().UTC().Add(-2 * time.Minute),
			Metadata:  map[string]any{meta.KeyRunID: "run-old"},
		},
	}, session.Messages...)
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   buildPlanRecoveryDirective("failed tasks: 旧任务卡在 browser.click"),
		CreatedAt: time.Now().UTC(),
		Metadata: map[string]any{
			meta.KeyRunID:   "run-old",
			"synthetic_msg": true,
			"auto_recovery": true,
		},
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := component.ensurePlan(ctx, run, session, skill.RuntimeContext{}, false); err != nil {
		t.Fatalf("ensurePlan() error = %v", err)
	}
	if len(planner.requests) != 1 {
		t.Fatalf("len(planner.requests) = %d, want 1", len(planner.requests))
	}
	if got := planner.requests[0].LatestMessage; got != "打开页面，等搜索结果加载出来，再提取前 5 条" {
		t.Fatalf("LatestMessage = %q", got)
	}
	if len(planner.requests[0].RecentMessages) != 1 || planner.requests[0].RecentMessages[0].Content != "打开页面，等搜索结果加载出来，再提取前 5 条" {
		t.Fatalf("RecentMessages = %#v", planner.requests[0].RecentMessages)
	}
}

func TestEnsurePlanCarriesBrowserReferenceSummaryAcrossRunFiltering(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	planner := &capturingPlanner{}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(planner)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey: "chat-plan-browser-summary",
		Content:    "抓取页面信息，写到 docs/tmp/example-brief.md",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	session, err := sessions.GetOrCreate(ctx, "chat-plan-browser-summary", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	session.Messages = append([]contextengine.Message{
		{
			Role:      contextengine.RoleUser,
			Content:   "旧任务：打开表单页",
			CreatedAt: time.Now().UTC().Add(-2 * time.Minute),
			Metadata:  map[string]any{meta.KeyRunID: "run-old"},
		},
		{
			Role:      contextengine.RoleTool,
			Name:      "browser.snapshot",
			Content:   `{"url":"https://httpbin.org/forms/post","title":"HTTPBin Form"}`,
			CreatedAt: time.Now().UTC().Add(-90 * time.Second),
			Metadata:  map[string]any{meta.KeyRunID: "run-old"},
		},
	}, session.Messages...)
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := component.ensurePlan(ctx, run, session, skill.RuntimeContext{}, false); err != nil {
		t.Fatalf("ensurePlan() error = %v", err)
	}
	if len(planner.requests) != 1 {
		t.Fatalf("len(planner.requests) = %d, want 1", len(planner.requests))
	}
	if !strings.Contains(planner.requests[0].SessionSummary, "https://httpbin.org/forms/post") {
		t.Fatalf("SessionSummary = %q, want browser reference context", planner.requests[0].SessionSummary)
	}
	if len(planner.requests[0].RecentMessages) != 1 || planner.requests[0].RecentMessages[0].Content != "抓取页面信息，写到 docs/tmp/example-brief.md" {
		t.Fatalf("RecentMessages = %#v", planner.requests[0].RecentMessages)
	}
}

func TestEnsurePlanCarriesDelegationContract(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	planner := &capturingPlanner{}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(planner)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey: "chat-plan-delegation",
		Content:    "并行分析仓库并修复失败测试",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	run.ExecutionMode = ExecutionModePlanned
	run.Delegation = &DelegationContract{
		Goal:                "Analyze and fix failures",
		AllowedDomains:      []string{string(DomainFS), string(DomainExec), string(DomainText)},
		SideEffectClass:     "local_write",
		MaxTurns:            4,
		MaxBudgetTokens:     4000,
		VerificationPlanRef: "task_contract:visible_result",
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session, err := sessions.GetOrCreate(ctx, "chat-plan-delegation", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if err := component.ensurePlan(ctx, run, session, skill.RuntimeContext{}, false); err != nil {
		t.Fatalf("ensurePlan() error = %v", err)
	}
	if len(planner.requests) != 1 {
		t.Fatalf("len(planner.requests) = %d, want 1", len(planner.requests))
	}
	if planner.requests[0].Delegation == nil {
		t.Fatal("PlanningRequest.Delegation = nil, want delegation contract")
	}
	if planner.requests[0].Delegation.Goal != "Analyze and fix failures" {
		t.Fatalf("Delegation.Goal = %q", planner.requests[0].Delegation.Goal)
	}
	if planner.requests[0].Delegation.SideEffectClass != "local_write" {
		t.Fatalf("Delegation.SideEffectClass = %q", planner.requests[0].Delegation.SideEffectClass)
	}
}

func TestEnsurePlanCarriesPinnedFactsIntoPlannerRequest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	planner := &capturingPlanner{}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(planner)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey: "chat-plan-pinned-facts",
		Content:    "继续当前任务并保留已有约束",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	session, err := sessions.GetOrCreate(ctx, "chat-plan-pinned-facts", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	session.PinnedFacts = []contextengine.PinnedFact{
		{Key: "deploy_env", Content: "Deploy only to staging."},
		{Key: "_memory_guide", Content: "system-only helper should be dropped"},
	}
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := component.ensurePlan(ctx, run, session, skill.RuntimeContext{}, false); err != nil {
		t.Fatalf("ensurePlan() error = %v", err)
	}
	if len(planner.requests) != 1 {
		t.Fatalf("len(planner.requests) = %d, want 1", len(planner.requests))
	}
	if got := planner.requests[0].PinnedFacts; len(got) != 1 || got[0] != "[deploy_env] Deploy only to staging." {
		t.Fatalf("PinnedFacts = %v, want only user-facing planning facts", got)
	}
}

func TestTaskContractPlanningContextCarriesSemanticAndEvidenceFields(t *testing.T) {
	t.Parallel()

	ctx := taskContractPlanningContext(&TaskContract{
		Goal:             "Inspect the target page and send a summary",
		JobType:          taskContractJobDelivery,
		TargetSummary:    "https://example.com",
		SuggestedDomains: []string{"browser", "email"},
		CapabilityHints:  []string{"email.send"},
		ExpectedDeliverables: []TaskContractDeliverable{
			{Kind: taskDeliverableSummary, Summary: "Provide a concise result", Required: true},
			{Kind: taskDeliverableBrowserEvidence, Summary: "Capture browser evidence", Required: false},
			{Kind: taskDeliverableMessageDelivery, Summary: "Leave delivery evidence", Required: true},
		},
		AcceptanceCriteria: []TaskContractAcceptance{{
			Summary:       "The final message is sent to the recipient.",
			EvidenceHints: []string{"delivery_receipt"},
		}},
		MissingInfo: []TaskContractMissingInfo{{
			ID:       taskMissingInfoDeliveryTarget,
			Summary:  "The task needs an explicit recipient.",
			Required: true,
		}},
		RequiresExternalEffect: true,
		RequiresApproval:       true,
	})

	if ctx == nil {
		t.Fatal("expected planning contract context")
	}
	if got := strings.Join(ctx.SuggestedDomains, ","); got != "browser,email" {
		t.Fatalf("SuggestedDomains = %v, want [browser email]", ctx.SuggestedDomains)
	}
	if got := strings.Join(ctx.CapabilityHints, ","); got != "email.send" {
		t.Fatalf("CapabilityHints = %v, want [email.send]", ctx.CapabilityHints)
	}
	if len(ctx.ExpectedDeliverables) != 3 {
		t.Fatalf("ExpectedDeliverables = %v, want all deliverables including evidence", ctx.ExpectedDeliverables)
	}
	if !containsPlanningString(ctx.EvidenceRequirements, "expected browser_evidence: Capture browser evidence") {
		t.Fatalf("EvidenceRequirements = %v, want browser evidence", ctx.EvidenceRequirements)
	}
	if !containsPlanningString(ctx.EvidenceRequirements, "required message_delivery: Leave delivery evidence") {
		t.Fatalf("EvidenceRequirements = %v, want message delivery evidence", ctx.EvidenceRequirements)
	}
	if !containsPlanningString(ctx.EvidenceRequirements, "delivery_receipt") {
		t.Fatalf("EvidenceRequirements = %v, want acceptance evidence hint", ctx.EvidenceRequirements)
	}
	if len(ctx.MissingInfo) != 1 || ctx.MissingInfo[0] != "The task needs an explicit recipient." {
		t.Fatalf("MissingInfo = %v", ctx.MissingInfo)
	}
	if !ctx.RequiresExternalEffect || !ctx.RequiresApproval {
		t.Fatalf("requires flags = %#v, want both true", ctx)
	}
}

func TestBuildPlanningPayloadIncludesContextHints(t *testing.T) {
	t.Parallel()

	payload, err := buildPlanningPayload(PlanningRequest{
		LatestMessage:   "Continue the task",
		SessionSummary:  "Current page context: https://example.com",
		PinnedFacts:     []string{"[deploy_env] staging", "Potential memory conflict: service URL differs"},
		SessionState:    "<session_state>\n- delivery_target: ceo@example.com\n</session_state>",
		RecalledContext: "<recalled_context source=\"segment seg-1\">\nSummary: Earlier we agreed to send a markdown summary.\n</recalled_context>",
		TaskContract: &TaskContract{
			Goal:             "Continue the task",
			SuggestedDomains: []string{"browser", "email"},
			CapabilityHints:  []string{"email.send"},
		},
	})
	if err != nil {
		t.Fatalf("buildPlanningPayload() error = %v", err)
	}

	var decoded planpkg.Context
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(decoded.PinnedFacts) != 2 {
		t.Fatalf("PinnedFacts = %v, want 2 facts", decoded.PinnedFacts)
	}
	if !strings.Contains(decoded.SessionState, "delivery_target") {
		t.Fatalf("SessionState = %q", decoded.SessionState)
	}
	if !strings.Contains(decoded.RecalledContext, "Earlier we agreed") {
		t.Fatalf("RecalledContext = %q", decoded.RecalledContext)
	}
	if decoded.TaskContract == nil || len(decoded.TaskContract.SuggestedDomains) != 2 {
		t.Fatalf("TaskContract = %#v, want suggested domains carried into payload", decoded.TaskContract)
	}
}

func TestEnsurePlanPreservesExistingWorkflowPlan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	planner := &capturingPlanner{
		plan: &planpkg.Plan{
			Goal: "replacement plan",
			Tasks: []planpkg.Task{
				{ID: "replacement", Goal: "should not be used"},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(planner)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey: "chat-plan-existing-workflow",
		Content:    "continue the existing workflow",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	run.ExecutionMode = ExecutionModeWorkflow
	run.Plan = &planpkg.Plan{
		Goal: "existing workflow plan",
		Tasks: []planpkg.Task{
			{ID: "t1", Goal: "preserve me", Status: planpkg.TaskCompleted},
			{ID: "t2", Goal: "continue here", Status: planpkg.TaskQueued, DependsOn: []string{"t1"}},
		},
		FinalTask: "t2",
	}

	session, err := sessions.GetOrCreate(ctx, "chat-plan-existing-workflow", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if err := component.ensurePlan(ctx, run, session, skill.RuntimeContext{}, false); err != nil {
		t.Fatalf("ensurePlan() error = %v", err)
	}
	if len(planner.requests) != 0 {
		t.Fatalf("planner was invoked %d times, want 0", len(planner.requests))
	}
	if run.Plan.Goal != "existing workflow plan" {
		t.Fatalf("run.Plan.Goal = %q, want existing plan preserved", run.Plan.Goal)
	}
	if len(run.Plan.Tasks) != 2 || run.Plan.Tasks[1].ID != "t2" {
		t.Fatalf("run.Plan = %#v", run.Plan)
	}
}

func containsPlanningString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestValidatePlanCoverageWarnsWhenRequiredDeliverableMissing(t *testing.T) {
	t.Parallel()

	plan := &planpkg.Plan{
		Goal: "Investigate and summarize",
		Tasks: []planpkg.Task{
			{ID: "t1", Kind: planpkg.TaskResearch, Goal: "Inspect logs and gather facts"},
			{ID: "t2", Kind: planpkg.TaskDeliver, Goal: "Share final summary", DependsOn: []string{"t1"}},
		},
		FinalTask: "t2",
	}
	contract := &TaskContract{
		ExpectedDeliverables: []TaskContractDeliverable{
			{Kind: taskDeliverableSummary, Required: true},
			{Kind: taskDeliverableSpreadsheet, Required: true, Summary: "Produce a spreadsheet export."},
		},
	}

	warnings := validatePlanCoverage(plan, contract)
	if len(warnings) != 1 {
		t.Fatalf("len(warnings) = %d, want 1 (%#v)", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], taskDeliverableSpreadsheet) {
		t.Fatalf("warnings = %#v, want spreadsheet coverage warning", warnings)
	}
}

func TestEnsurePlanStoresCoverageWarningsOnRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	planner := &capturingPlanner{
		plan: &planpkg.Plan{
			Goal: "Review repo and reply",
			Tasks: []planpkg.Task{
				{ID: "t1", Kind: planpkg.TaskResearch, Goal: "Inspect the repository"},
				{ID: "t2", Kind: planpkg.TaskDeliver, Goal: "Reply with a summary", DependsOn: []string{"t1"}},
			},
			FinalTask: "t2",
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(planner)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey: "chat-plan-coverage",
		Content:    "Read the repo and send me both a summary and a spreadsheet export.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	run.ExecutionMode = ExecutionModePlanned
	run.TaskContract = &TaskContract{
		ExpectedDeliverables: []TaskContractDeliverable{
			{Kind: taskDeliverableSummary, Required: true},
			{Kind: taskDeliverableSpreadsheet, Required: true, Summary: "Produce a spreadsheet export."},
		},
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session, err := sessions.GetOrCreate(ctx, "chat-plan-coverage", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if err := component.ensurePlan(ctx, run, session, skill.RuntimeContext{}, true); err != nil {
		t.Fatalf("ensurePlan() error = %v", err)
	}
	if len(run.Plan.CoverageWarnings) != 1 {
		t.Fatalf("run.Plan.CoverageWarnings = %#v, want 1 warning", run.Plan.CoverageWarnings)
	}
	if !strings.Contains(run.Plan.CoverageWarnings[0], taskDeliverableSpreadsheet) {
		t.Fatalf("CoverageWarnings = %#v", run.Plan.CoverageWarnings)
	}
}

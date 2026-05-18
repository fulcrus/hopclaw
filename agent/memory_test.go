package agent

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/planner"
)

func TestCoordinatorRejectMode(t *testing.T) {
	t.Parallel()

	coord := NewInMemoryCoordinator()
	ctx := context.Background()
	if err := coord.EnqueueSessionRun(ctx, "sess-1", "run-1", QueueEnqueue); err != nil {
		t.Fatalf("EnqueueSessionRun(run-1) error = %v", err)
	}
	if err := coord.StartRun(ctx, "sess-1", "run-1"); err != nil {
		t.Fatalf("StartRun(run-1) error = %v", err)
	}
	if err := coord.EnqueueSessionRun(ctx, "sess-1", "run-2", QueueReject); err == nil {
		t.Fatal("expected reject mode to fail when session already busy")
	}
}

func TestCoordinatorInterruptMode(t *testing.T) {
	t.Parallel()

	coord := NewInMemoryCoordinator()
	ctx := context.Background()
	if err := coord.EnqueueSessionRun(ctx, "sess-1", "run-1", QueueEnqueue); err != nil {
		t.Fatalf("EnqueueSessionRun(run-1) error = %v", err)
	}
	if err := coord.StartRun(ctx, "sess-1", "run-1"); err != nil {
		t.Fatalf("StartRun(run-1) error = %v", err)
	}
	if err := coord.EnqueueSessionRun(ctx, "sess-1", "run-2", QueueInterrupt); err != nil {
		t.Fatalf("EnqueueSessionRun(run-2) error = %v", err)
	}
	if err := coord.StartRun(ctx, "sess-1", "run-2"); err == nil {
		t.Fatal("StartRun(run-2) should fail while run-1 is still active")
	}
	if err := coord.FinishRun(ctx, "sess-1", "run-1"); err != nil {
		t.Fatalf("FinishRun(run-1) error = %v", err)
	}
	if err := coord.StartRun(ctx, "sess-1", "run-2"); err != nil {
		t.Fatalf("StartRun(run-2) after finish error = %v", err)
	}
}

func TestCoordinatorCoalesceMode(t *testing.T) {
	t.Parallel()

	coord := NewInMemoryCoordinator()
	ctx := context.Background()
	if err := coord.EnqueueSessionRun(ctx, "sess-1", "run-1", QueueEnqueue); err != nil {
		t.Fatalf("EnqueueSessionRun(run-1) error = %v", err)
	}
	if err := coord.EnqueueSessionRun(ctx, "sess-1", "run-2", QueueEnqueue); err != nil {
		t.Fatalf("EnqueueSessionRun(run-2) error = %v", err)
	}
	if err := coord.EnqueueSessionRun(ctx, "sess-1", "run-3", QueueCoalesce); err != nil {
		t.Fatalf("EnqueueSessionRun(run-3) error = %v", err)
	}
	if err := coord.StartRun(ctx, "sess-1", "run-1"); err != nil {
		t.Fatalf("StartRun(run-1) error = %v", err)
	}
	if err := coord.FinishRun(ctx, "sess-1", "run-1"); err != nil {
		t.Fatalf("FinishRun(run-1) error = %v", err)
	}
	if err := coord.StartRun(ctx, "sess-1", "run-3"); err != nil {
		t.Fatalf("expected coalesced run-3 at queue head, got error %v", err)
	}
}

func TestCoordinatorNextQueuedRunFollowsInterruptAndCoalesceSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		enqueue []struct {
			runID string
			mode  QueueMode
		}
		wantHead string
	}{
		{
			name: "interrupt replaces prior queued work",
			enqueue: []struct {
				runID string
				mode  QueueMode
			}{
				{runID: "run-2", mode: QueueEnqueue},
				{runID: "run-3", mode: QueueInterrupt},
			},
			wantHead: "run-3",
		},
		{
			name: "coalesce rewrites tail queued run",
			enqueue: []struct {
				runID string
				mode  QueueMode
			}{
				{runID: "run-2", mode: QueueEnqueue},
				{runID: "run-3", mode: QueueCoalesce},
			},
			wantHead: "run-3",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			coord := NewInMemoryCoordinator()
			ctx := context.Background()
			if err := coord.EnqueueSessionRun(ctx, "sess-1", "run-1", QueueEnqueue); err != nil {
				t.Fatalf("EnqueueSessionRun(run-1) error = %v", err)
			}
			if err := coord.StartRun(ctx, "sess-1", "run-1"); err != nil {
				t.Fatalf("StartRun(run-1) error = %v", err)
			}
			for _, item := range tt.enqueue {
				if err := coord.EnqueueSessionRun(ctx, "sess-1", item.runID, item.mode); err != nil {
					t.Fatalf("EnqueueSessionRun(%s) error = %v", item.runID, err)
				}
			}

			got, ok, err := coord.NextQueuedRun(ctx, "sess-1")
			if err != nil {
				t.Fatalf("NextQueuedRun() error = %v", err)
			}
			if !ok {
				t.Fatal("NextQueuedRun() = false, want true")
			}
			if got != tt.wantHead {
				t.Fatalf("NextQueuedRun() = %q, want %q", got, tt.wantHead)
			}
		})
	}
}

func TestCoordinatorFinishRunRemovesQueuedRunID(t *testing.T) {
	t.Parallel()

	coord := NewInMemoryCoordinator()
	ctx := context.Background()
	if err := coord.EnqueueSessionRun(ctx, "sess-1", "run-1", QueueEnqueue); err != nil {
		t.Fatalf("EnqueueSessionRun(run-1) error = %v", err)
	}
	if err := coord.EnqueueSessionRun(ctx, "sess-1", "run-2", QueueEnqueue); err != nil {
		t.Fatalf("EnqueueSessionRun(run-2) error = %v", err)
	}

	if err := coord.FinishRun(ctx, "sess-1", "run-2"); err != nil {
		t.Fatalf("FinishRun(run-2) error = %v", err)
	}

	q := coord.queue("sess-1")
	if len(q.queued) != 1 || q.queued[0] != "run-1" {
		t.Fatalf("queued = %#v", q.queued)
	}
}

func TestInMemoryRunStoreClaimQueuedRun(t *testing.T) {
	t.Parallel()

	store := NewInMemoryRunStore()
	ctx := context.Background()

	run, err := store.Create(ctx, "sess-1", IncomingMessage{
		ExternalEventID: "evt-claim",
		Content:         "start me",
	}, AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	claimed, ok, err := store.ClaimQueuedRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ClaimQueuedRun(first) error = %v", err)
	}
	if !ok {
		t.Fatal("ClaimQueuedRun(first) = false, want true")
	}
	if claimed.Status != RunRunning {
		t.Fatalf("claimed.Status = %q, want %q", claimed.Status, RunRunning)
	}
	if claimed.StartedAt.IsZero() {
		t.Fatal("claimed.StartedAt should be set")
	}

	second, ok, err := store.ClaimQueuedRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ClaimQueuedRun(second) error = %v", err)
	}
	if ok {
		t.Fatal("ClaimQueuedRun(second) = true, want false")
	}
	if second.Status != RunRunning {
		t.Fatalf("second.Status = %q, want %q", second.Status, RunRunning)
	}
}

func TestAppendUserMessageBuildsImageContentBlocks(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "chat-images", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	images := []string{
		"data:image/png;base64,ZmFrZS1wbmc=",
		"cmF3LWpwZWc=",
	}
	if err := store.AppendUserMessage(context.Background(), session.ID, IncomingMessage{
		Content: "describe this image",
		Images:  images,
	}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}

	session, err = store.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(session.Messages))
	}
	got := session.Messages[0]
	if got.Role != contextengine.RoleUser {
		t.Fatalf("Role = %q, want %q", got.Role, contextengine.RoleUser)
	}
	if got.Content != "describe this image" {
		t.Fatalf("Content = %q, want %q", got.Content, "describe this image")
	}
	wantBlocks := []contextengine.ContentBlock{
		{Type: contextengine.ContentBlockText, Text: "describe this image"},
		{Type: contextengine.ContentBlockImage, MediaType: "image/png", Data: "ZmFrZS1wbmc="},
		{Type: contextengine.ContentBlockImage, MediaType: "image/jpeg", Data: "cmF3LWpwZWc="},
	}
	if !reflect.DeepEqual(got.ContentBlocks, wantBlocks) {
		t.Fatalf("ContentBlocks = %#v, want %#v", got.ContentBlocks, wantBlocks)
	}
}

func TestAppendUserMessageStoresImageContentBlocksAsMediaRefs(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionStore()
	media := &recordingMediaStore{
		uris: []string{"artifact://local/img-1", "artifact://local/img-2"},
	}
	store.SetMediaStore(media)
	session, err := store.GetOrCreate(context.Background(), "chat-images-media-ref", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	if err := store.AppendUserMessage(context.Background(), session.ID, IncomingMessage{
		Content: "describe this image",
		Images: []string{
			"data:image/png;base64,ZmFrZS1wbmc=",
			"cmF3LWpwZWc=",
		},
	}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}

	session, err = store.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got := session.Messages[0]
	wantBlocks := []contextengine.ContentBlock{
		{Type: contextengine.ContentBlockText, Text: "describe this image"},
		{Type: contextengine.ContentBlockImage, MediaType: "image/png", MediaRef: "artifact://local/img-1"},
		{Type: contextengine.ContentBlockImage, MediaType: "image/jpeg", MediaRef: "artifact://local/img-2"},
	}
	if !reflect.DeepEqual(got.ContentBlocks, wantBlocks) {
		t.Fatalf("ContentBlocks = %#v, want %#v", got.ContentBlocks, wantBlocks)
	}
	if len(media.calls) != 2 {
		t.Fatalf("len(media.calls) = %d, want 2", len(media.calls))
	}
	if media.calls[0].kind != "user_image" || media.calls[0].contentType != "image/png" || string(media.calls[0].body) != "fake-png" {
		t.Fatalf("media call[0] = %#v", media.calls[0])
	}
	if media.calls[1].kind != "user_image" || media.calls[1].contentType != "image/jpeg" || string(media.calls[1].body) != "raw-jpeg" {
		t.Fatalf("media call[1] = %#v", media.calls[1])
	}
}

func TestAppendUserMessagePreservesCallerSuppliedContentBlocks(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "chat-structured-blocks", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	wantBlocks := []contextengine.ContentBlock{
		{Type: contextengine.ContentBlockText, Text: "inspect this"},
		{Type: contextengine.ContentBlockFile, Label: "spec.md", Path: "/tmp/spec.md"},
		{Type: contextengine.ContentBlockLink, Label: "reference", SourceURL: "https://example.com/spec"},
	}
	if err := store.AppendUserMessage(context.Background(), session.ID, IncomingMessage{
		Content:       "inspect this",
		ContentBlocks: append([]contextengine.ContentBlock(nil), wantBlocks...),
	}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}

	session, err = store.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(session.Messages))
	}
	if got := session.Messages[0].ContentBlocks; !reflect.DeepEqual(got, wantBlocks) {
		t.Fatalf("ContentBlocks = %#v, want %#v", got, wantBlocks)
	}
}

type recordingMediaStore struct {
	uris  []string
	calls []recordingMediaCall
}

type recordingMediaCall struct {
	kind        string
	contentType string
	body        []byte
}

func (s *recordingMediaStore) Put(_ context.Context, kind, contentType string, body []byte) (string, error) {
	s.calls = append(s.calls, recordingMediaCall{
		kind:        kind,
		contentType: contentType,
		body:        append([]byte(nil), body...),
	})
	if len(s.uris) == 0 {
		return "", nil
	}
	uri := s.uris[0]
	s.uris = s.uris[1:]
	return uri, nil
}

func TestCloneRunDeepCopiesStructuredState(t *testing.T) {
	t.Parallel()

	original := &Run{
		ID: "run-clone",
		Plan: &planner.Plan{
			Goal:          "ship report",
			Summary:       "summarize and deliver",
			Strategy:      planner.StrategyMixed,
			FailurePolicy: planner.FailFast,
			Tasks: []planner.Task{
				{
					ID:                   "task-1",
					Title:                "Collect data",
					Kind:                 planner.TaskResearch,
					Goal:                 "collect source rows",
					DependsOn:            []string{"bootstrap"},
					Outputs:              []string{"rows.csv"},
					RequiredCapabilities: []string{"browser"},
					VerificationHints:    []string{"row count matches"},
					Status:               planner.TaskQueued,
				},
			},
			RunningTasks: []string{"task-1"},
		},
		PendingTools: []ToolCall{
			{
				ID:   "tool-1",
				Name: "browser.open",
				Input: map[string]any{
					"url": "https://example.com",
				},
			},
		},
		Preflight: &RunPreflightReport{
			State:            RunPreflightNeedsConfirmation,
			Summary:          "Need one more detail",
			ReplyHints:       []string{"reply with the destination"},
			SuggestedDomains: []string{"browser", "office"},
			Checks: []RunPreflightCheck{
				{ID: "delivery_target", Title: "Pick a delivery target", State: RunPreflightNeedsConfirmation, Blocking: true},
			},
			ClarificationSlots: []RunClarificationSlot{
				{ID: "schedule", Label: "Schedule", Hints: []string{"Every weekday at 9am"}},
			},
			GeneratedAt: time.Now().UTC(),
		},
		Triage: &RunTriageTrace{
			Source:           "model",
			Reason:           "needs browser + office",
			Confidence:       0.92,
			SuggestedDomains: []string{"browser", "office"},
			GeneratedAt:      time.Now().UTC(),
		},
		TaskContract: &TaskContract{
			Goal:             "Send a spreadsheet report",
			JobType:          "report",
			TargetSummary:    "Q2 finance package",
			SuggestedDomains: []string{"spreadsheet", "email"},
			ExpectedDeliverables: []TaskContractDeliverable{
				{Kind: "spreadsheet", Summary: "Q2 workbook", Required: true},
			},
			AcceptanceCriteria: []TaskContractAcceptance{
				{
					ID:               "deliverable_ready",
					Summary:          "Workbook is attached",
					Required:         true,
					DeliverableKinds: []string{"spreadsheet"},
					EvidenceHints:    []string{"preview attached"},
				},
			},
			MissingInfo: []TaskContractMissingInfo{
				{
					ID:       "delivery_target",
					Label:    "Recipient",
					Summary:  "Need the delivery destination",
					Required: true,
					Hints:    []string{"Slack #finance"},
				},
			},
			ResolvedInfo: []TaskContractResolvedInfo{
				{ID: "source_target", Label: "Source", Value: "Q2 ledger", Source: "user", InputMode: "text"},
			},
			RequiresExternalEffect: true,
			RequiresApproval:       true,
			Confidence:             0.88,
			Source:                 "heuristic",
			GeneratedAt:            time.Now().UTC(),
		},
		Delegation: &DelegationContract{
			Goal:                "Split delivery preparation",
			AllowedDomains:      []string{"spreadsheet", "email"},
			AllowedTools:        []string{"spreadsheet.read", "spreadsheet.write", "email.send"},
			SideEffectClass:     "external_write",
			MaxTurns:            4,
			MaxBudgetTokens:     4000,
			RequiresApproval:    true,
			VerificationPlanRef: "task_contract:deliverables_ready",
			Source:              "heuristic",
			GeneratedAt:         time.Now().UTC(),
		},
		WorkflowState: &WorkflowState{
			OriginalRunID:     "run-root",
			ContinuationIndex: 2,
			MaxContinuations:  DefaultMaxContinuations,
			TotalRoundsUsed:   7,
			MaxTotalRounds:    DefaultMaxTotalRounds,
			PriorRunSummaries: []string{"Run run-0001: completed 1/3 tasks"},
			CompletedTaskIDs:  []string{"task-0"},
			Yielded:           true,
			YieldReason:       YieldReasonRoundBudget,
		},
	}

	cloned := cloneRun(original)
	if cloned == nil {
		t.Fatal("cloneRun() returned nil")
	}

	cloned.Plan.Goal = "changed goal"
	cloned.Plan.Tasks[0].DependsOn[0] = "different"
	cloned.Plan.RunningTasks[0] = "task-2"
	cloned.PendingTools[0].Input["url"] = "https://changed.example.com"
	cloned.Preflight.ReplyHints[0] = "different reply"
	cloned.Preflight.SuggestedDomains[0] = "desktop"
	cloned.Preflight.Checks[0].Title = "Different title"
	cloned.Preflight.ClarificationSlots[0].Hints[0] = "Manual trigger"
	cloned.Triage.SuggestedDomains[0] = "desktop"
	cloned.TaskContract.SuggestedDomains[0] = "desktop"
	cloned.TaskContract.ExpectedDeliverables[0].Summary = "Changed workbook"
	cloned.TaskContract.AcceptanceCriteria[0].EvidenceHints[0] = "different evidence"
	cloned.TaskContract.MissingInfo[0].Hints[0] = "Email ops@example.com"
	cloned.TaskContract.ResolvedInfo[0].Value = "Changed source"
	cloned.Delegation.AllowedDomains[0] = "desktop"
	cloned.Delegation.AllowedTools[0] = "desktop.open_app"
	cloned.WorkflowState.PriorRunSummaries[0] = "mutated summary"
	cloned.WorkflowState.CompletedTaskIDs[0] = "task-x"

	if original.Plan.Goal != "ship report" {
		t.Fatalf("original.Plan.Goal = %q", original.Plan.Goal)
	}
	if got := original.Plan.Tasks[0].DependsOn[0]; got != "bootstrap" {
		t.Fatalf("original.Plan.Tasks[0].DependsOn[0] = %q", got)
	}
	if got := original.Plan.RunningTasks[0]; got != "task-1" {
		t.Fatalf("original.Plan.RunningTasks[0] = %q", got)
	}
	if got := original.PendingTools[0].Input["url"]; got != "https://example.com" {
		t.Fatalf("original.PendingTools[0].Input[url] = %#v", got)
	}
	if got := original.Preflight.ReplyHints[0]; got != "reply with the destination" {
		t.Fatalf("original.Preflight.ReplyHints[0] = %q", got)
	}
	if got := original.Preflight.SuggestedDomains[0]; got != "browser" {
		t.Fatalf("original.Preflight.SuggestedDomains[0] = %q", got)
	}
	if got := original.Preflight.Checks[0].Title; got != "Pick a delivery target" {
		t.Fatalf("original.Preflight.Checks[0].Title = %q", got)
	}
	if got := original.Preflight.ClarificationSlots[0].Hints[0]; got != "Every weekday at 9am" {
		t.Fatalf("original.Preflight.ClarificationSlots[0].Hints[0] = %q", got)
	}
	if got := original.Triage.SuggestedDomains[0]; got != "browser" {
		t.Fatalf("original.Triage.SuggestedDomains[0] = %q", got)
	}
	if got := original.TaskContract.SuggestedDomains[0]; got != "spreadsheet" {
		t.Fatalf("original.TaskContract.SuggestedDomains[0] = %q", got)
	}
	if got := original.TaskContract.ExpectedDeliverables[0].Summary; got != "Q2 workbook" {
		t.Fatalf("original.TaskContract.ExpectedDeliverables[0].Summary = %q", got)
	}
	if got := original.TaskContract.AcceptanceCriteria[0].EvidenceHints[0]; got != "preview attached" {
		t.Fatalf("original.TaskContract.AcceptanceCriteria[0].EvidenceHints[0] = %q", got)
	}
	if got := original.TaskContract.MissingInfo[0].Hints[0]; got != "Slack #finance" {
		t.Fatalf("original.TaskContract.MissingInfo[0].Hints[0] = %q", got)
	}
	if got := original.TaskContract.ResolvedInfo[0].Value; got != "Q2 ledger" {
		t.Fatalf("original.TaskContract.ResolvedInfo[0].Value = %q", got)
	}
	if got := original.Delegation.AllowedDomains[0]; got != "spreadsheet" {
		t.Fatalf("original.Delegation.AllowedDomains[0] = %q", got)
	}
	if got := original.Delegation.AllowedTools[0]; got != "spreadsheet.read" {
		t.Fatalf("original.Delegation.AllowedTools[0] = %q", got)
	}
	if got := original.WorkflowState.PriorRunSummaries[0]; got != "Run run-0001: completed 1/3 tasks" {
		t.Fatalf("original.WorkflowState.PriorRunSummaries[0] = %q", got)
	}
	if got := original.WorkflowState.CompletedTaskIDs[0]; got != "task-0" {
		t.Fatalf("original.WorkflowState.CompletedTaskIDs[0] = %q", got)
	}
}

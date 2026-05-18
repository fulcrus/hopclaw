package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/resultmodel"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

func TestGetRunResultBuildsDeliveryEnvelopeAndGovernanceSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "telegram:chat-1", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey:      "telegram:chat-1",
		ExternalEventID: "msg-1",
		Content:         "summarize the document",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.Scope = domainscope.Ref{
		AutomationID: "auto-delivery",
	}.Normalize()
	run.Governance = &domaingov.Evaluation{
		Decision: domaingov.Decision{
			Action:       domaingov.DecisionRequireApproval,
			PolicySource: "policy.test/delivery",
			Summary:      "approval required before document access",
			Reasons:      []string{"document access must be approved"},
		},
		ToolNames:                 []string{"document.read"},
		EffectiveConfigSnapshotID: "ecs-delivery-1",
		UpdatedAt:                 now,
	}
	run.StartedAt = now.Add(-2 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	ticket, err := approvals.Create(ctx, approval.Ticket{
		RunID:     run.ID,
		SessionID: session.ID,
		Kind:      approval.KindToolCalls,
		ToolCalls: []approval.ToolCall{{ID: "call-1", Name: "document.read"}},
		Metadata: map[string]any{
			"scope":                        run.Scope,
			"effective_config_snapshot_id": "ecs-delivery-1",
			"policy_action":                "require_approval",
			"policy_source":                "policy.test/delivery",
			"policy_summary":               "approval required before document access",
		},
	})
	if err != nil {
		t.Fatalf("approvals.Create() error = %v", err)
	}
	if _, err := approvals.Resolve(ctx, ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("approvals.Resolve() error = %v", err)
	}
	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "summarize the document",
			CreatedAt: now.Add(-1500 * time.Millisecond),
			Metadata: map[string]any{
				meta.KeyChannel:    "telegram",
				meta.KeyMessageID:  "msg-1",
				meta.KeyReplyToID:  "msg-parent",
				meta.KeyThreadID:   "topic-7",
				meta.KeySenderID:   "user-1",
				meta.KeySenderName: "Alice",
			},
		},
		contextengine.Message{
			Role:       contextengine.RoleTool,
			Name:       "document.read",
			ToolCallID: "call-1",
			Content:    `{"status":"error","tool_execution_error":true,"tool_name":"document.read","error":"permission denied"}`,
			CreatedAt:  now.Add(-time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "I prepared a partial summary and noted the read failure.",
			CreatedAt: now,
			Metadata:  map[string]any{meta.KeyRunID: run.ID},
		},
	)
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := bus.Publish(ctx, eventbus.Event{
		Type:      eventbus.EventToolExecuted,
		RunID:     run.ID,
		SessionID: session.ID,
		Attrs: map[string]any{
			"tool_names":    []string{"document.read"},
			"artifact_uris": []string{"artifact://doc-1"},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, approvals, bus, nil)
	result, err := svc.GetRunResult(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunResult() error = %v", err)
	}
	if result.Delivery == nil {
		t.Fatal("expected delivery envelope")
	}
	if result.Governance == nil || result.Governance.Policy == nil {
		t.Fatalf("result.Governance = %#v", result.Governance)
	}
	if len(result.Delivery.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(result.Delivery.Attachments))
	}
	if result.Delivery.Governance == nil || result.Delivery.Governance.Approval == nil {
		t.Fatalf("delivery.Governance = %#v", result.Delivery.Governance)
	}
	if result.Delivery.Conversation == nil || result.Delivery.Conversation.ThreadID != "topic-7" {
		t.Fatalf("conversation = %#v", result.Delivery.Conversation)
	}
	if result.Delivery.Conversation.ParticipantName != "Alice" {
		t.Fatalf("participant name = %#v", result.Delivery.Conversation)
	}
	if result.Outcome != RunOutcomePartial {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, RunOutcomePartial)
	}
	if result.VerificationStatus != string(verifyrt.StatusWarning) {
		t.Fatalf("VerificationStatus = %q, want %q", result.VerificationStatus, verifyrt.StatusWarning)
	}
	if result.Bundle == nil {
		t.Fatal("expected canonical result bundle")
	}
	if result.Bundle.Outcome != RunOutcomePartial {
		t.Fatalf("bundle.Outcome = %q, want %q", result.Bundle.Outcome, RunOutcomePartial)
	}
	if result.Governance.Scope.AutomationID != "auto-delivery" {
		t.Fatalf("result.Governance.Scope = %#v", result.Governance.Scope)
	}
	if result.Governance.Approval == nil || result.Governance.Approval.Status != approval.StatusApproved {
		t.Fatalf("result.Governance.Approval = %#v", result.Governance.Approval)
	}
	if result.Delivery.Governance == nil || result.Delivery.Governance.Policy == nil {
		t.Fatalf("delivery.Governance = %#v", result.Delivery.Governance)
	}
	if result.Bundle.Governance == nil || result.Bundle.Governance.EffectiveConfigSnapshotID != "ecs-delivery-1" {
		t.Fatalf("bundle.Governance = %#v", result.Bundle.Governance)
	}
	if len(result.Bundle.SuggestedActions) == 0 {
		t.Fatal("expected suggested actions")
	}

	snapshot, err := svc.GetGovernanceSnapshot(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetGovernanceSnapshot() error = %v", err)
	}
	if snapshot.RequiredIssues != 1 {
		t.Fatalf("RequiredIssues = %d, want 1", snapshot.RequiredIssues)
	}
	if snapshot.Policy == nil || snapshot.Policy.PolicySource != "policy.test/delivery" {
		t.Fatalf("snapshot.Policy = %#v", snapshot.Policy)
	}
	if snapshot.Approval == nil || snapshot.Approval.Status != approval.StatusApproved {
		t.Fatalf("snapshot.Approval = %#v", snapshot.Approval)
	}
	if snapshot.AdvisoryIssues != 1 {
		t.Fatalf("AdvisoryIssues = %d, want 1", snapshot.AdvisoryIssues)
	}
	if !snapshot.ThreadContextPresent {
		t.Fatal("expected thread context to be present")
	}
	if snapshot.DeliveryAttachmentCount != 1 {
		t.Fatalf("DeliveryAttachmentCount = %d, want 1", snapshot.DeliveryAttachmentCount)
	}
	if snapshot.RecoveredToolFailures != 1 {
		t.Fatalf("RecoveredToolFailures = %d, want 1", snapshot.RecoveredToolFailures)
	}
}

func TestBuildDeliveryEnvelopeBlocksConfiguredVerifierFailures(t *testing.T) {
	t.Parallel()

	result := &RunResult{
		Summary: "Email sent successfully.",
		Output:  "Email sent successfully.",
		Deliverables: []DeliverableRef{{
			Kind: "artifact",
			URI:  "artifact://email-proof",
		}},
	}
	verification := &verifyrt.RunVerification{
		Status:           verifyrt.StatusFailed,
		Summary:          "verification blocked delivery: 1 blocking check did not pass",
		BlockingFailures: 1,
		RequiredFailures: 1,
		Checks: []verifyrt.Check{{
			Name:     "email.result",
			Status:   verifyrt.StatusFailed,
			Severity: verifyrt.SeverityBlocking,
			Summary:  "email delivery did not pass verification",
		}},
	}

	delivery := buildDeliveryEnvelope(result, nil, nil, verification)
	if delivery == nil {
		t.Fatal("expected delivery envelope")
	}
	if len(delivery.Attachments) != 0 {
		t.Fatalf("attachments = %d, want 0 when delivery is blocked", len(delivery.Attachments))
	}
	if len(delivery.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(delivery.Blocks))
	}
	if !strings.Contains(delivery.Summary, "Task completed, but verification did not pass") {
		t.Fatalf("delivery.Summary = %q", delivery.Summary)
	}
	if delivery.Verification == nil || delivery.Verification.BlockingIssues != 1 {
		t.Fatalf("delivery.Verification = %#v", delivery.Verification)
	}
}

func TestGetRunResultDoesNotLeakToolResultsAcrossRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()

	session, err := sessions.GetOrCreate(ctx, "chat:shared", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	runOne, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey:      "chat:shared",
		ExternalEventID: "evt-1",
		Content:         "first task",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create(runOne) error = %v", err)
	}
	runTwo, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey:      "chat:shared",
		ExternalEventID: "evt-2",
		Content:         "second task",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create(runTwo) error = %v", err)
	}
	runTwo.Status = agent.RunCompleted
	runTwo.FinishedAt = time.Now().UTC()
	if err := runs.Update(ctx, runTwo); err != nil {
		t.Fatalf("Update(runTwo) error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role: contextengine.RoleAssistant,
			ToolCalls: []contextengine.ToolCallRef{{
				ID:   "call-old",
				Name: "browser.snapshot",
			}},
			Metadata:  map[string]any{meta.KeyRunID: runOne.ID},
			CreatedAt: time.Now().UTC().Add(-2 * time.Minute),
		},
		contextengine.Message{
			Role:       contextengine.RoleTool,
			Name:       "browser.snapshot",
			ToolCallID: "call-old",
			Content:    "old artifact",
			Metadata: map[string]any{
				meta.KeyRunID: runOne.ID,
				resultmodel.MetadataKeyToolResult: map[string]any{
					"tool_name":       "browser.snapshot",
					"tool_call_id":    "call-old",
					"transcript_text": "old artifact",
					"artifacts": []map[string]any{{
						"kind": "artifact",
						"uri":  "artifact://local/old-artifact",
					}},
				},
			},
			CreatedAt: time.Now().UTC().Add(-2 * time.Minute),
		},
		contextengine.Message{
			Role: contextengine.RoleAssistant,
			ToolCalls: []contextengine.ToolCallRef{{
				ID:   "call-new",
				Name: "fs.write",
			}},
			Metadata:  map[string]any{meta.KeyRunID: runTwo.ID},
			CreatedAt: time.Now().UTC().Add(-time.Minute),
		},
		contextengine.Message{
			Role:       contextengine.RoleTool,
			Name:       "fs.write",
			ToolCallID: "call-new",
			Content:    "wrote report",
			Metadata: map[string]any{
				meta.KeyRunID: runTwo.ID,
				resultmodel.MetadataKeyToolResult: map[string]any{
					"tool_name":       "fs.write",
					"tool_call_id":    "call-new",
					"transcript_text": "wrote report",
					"artifacts": []map[string]any{{
						"kind": "artifact",
						"uri":  "artifact://local/new-artifact",
					}},
					"actions": []map[string]any{{
						"kind":   "open_artifact",
						"label":  "Open report",
						"target": "artifact://local/new-artifact",
					}},
				},
			},
			CreatedAt: time.Now().UTC().Add(-time.Minute),
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "second task completed",
			Metadata:  map[string]any{meta.KeyRunID: runTwo.ID},
			CreatedAt: time.Now().UTC(),
		},
	)
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, nil, nil)
	result, err := svc.GetRunResult(ctx, runTwo.ID)
	if err != nil {
		t.Fatalf("GetRunResult() error = %v", err)
	}
	if len(result.Deliverables) != 1 {
		t.Fatalf("deliverables = %#v", result.Deliverables)
	}
	if result.Deliverables[0].URI != "artifact://local/new-artifact" {
		t.Fatalf("deliverable URI = %q", result.Deliverables[0].URI)
	}
	if len(result.NextActions) != 1 || result.NextActions[0].Target != "artifact://local/new-artifact" {
		t.Fatalf("next actions = %#v", result.NextActions)
	}
	if strings.Contains(result.Summary, "old artifact") {
		t.Fatalf("summary leaked prior run result: %q", result.Summary)
	}
}

func TestGetRunResultEnrichesArtifactDeliverablesInBundle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	artifacts := artifact.NewInMemoryStore()

	session, err := sessions.GetOrCreate(ctx, "desktop:report-1", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "desktop:report-1",
		Content:    "生成报告并输出文件",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-2 * time.Second)
	run.UpdatedAt = now
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleAssistant,
		Content:   "报告已生成。",
		CreatedAt: now,
		Metadata:  map[string]any{meta.KeyRunID: run.ID},
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	blob, err := artifacts.Put(ctx, artifact.PutRequest{
		Kind:        "tool_output",
		ContentType: "application/json",
		Body:        []byte(`{"report":"ok","rows":12}`),
		Metadata: map[string]any{
			meta.KeyRunID:     run.ID,
			meta.KeySessionID: session.ID,
			meta.KeyToolName:  "spreadsheet.export",
			"name":            "weekly-report.json",
		},
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, eventbus.NewInMemoryBus(), artifacts)
	result, err := svc.GetRunResult(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunResult() error = %v", err)
	}
	if result.Bundle == nil {
		t.Fatal("expected bundle")
	}
	if len(result.Bundle.Deliverables) != 1 {
		t.Fatalf("len(bundle.Deliverables) = %d, want 1", len(result.Bundle.Deliverables))
	}
	item := result.Bundle.Deliverables[0]
	if item.Name != "weekly-report.json" {
		t.Fatalf("deliverable name = %q", item.Name)
	}
	if item.SizeBytes != blob.Size {
		t.Fatalf("deliverable size = %d, want %d", item.SizeBytes, blob.Size)
	}
	if !strings.Contains(item.PreviewText, `"report":"ok"`) {
		t.Fatalf("preview_text = %q", item.PreviewText)
	}
	if len(result.Bundle.SuggestedActions) == 0 || result.Bundle.SuggestedActions[0].Kind != "open_deliverables" {
		t.Fatalf("suggested_actions = %#v", result.Bundle.SuggestedActions)
	}
}

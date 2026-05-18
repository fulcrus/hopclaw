package channels

import (
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

func TestRunEventProjectorProjectsToolProgress(t *testing.T) {
	t.Parallel()

	projector := NewRunEventProjector()
	projected, ok := projector.ProjectLive(eventbus.Event{
		Type:  eventbus.EventToolExecuted,
		RunID: "run-1",
		Attrs: map[string]any{
			"tool_round": 2,
			"tool_names": []string{"search.web", "web.fetch"},
		},
	}, RunProgressSnapshot{})
	if !ok {
		t.Fatal("expected projection")
	}
	if projected.Kind != ProjectedRunEventToolProgress {
		t.Fatalf("projected.Kind = %q", projected.Kind)
	}
	if projected.ToolRounds != 2 {
		t.Fatalf("projected.ToolRounds = %d", projected.ToolRounds)
	}
}

func TestRunEventProjectorProjectsPhaseProgress(t *testing.T) {
	t.Parallel()

	projector := NewRunEventProjector()
	projected, ok := projector.ProjectLive(eventbus.Event{
		Type:  eventbus.EventRunPhaseChanged,
		RunID: "run-1",
		Attrs: map[string]any{
			"phase":      "executing_tools",
			"tool_names": []string{"search.web", "web.fetch"},
		},
	}, RunProgressSnapshot{})
	if !ok {
		t.Fatal("expected projection")
	}
	if projected.Kind != ProjectedRunEventPhase {
		t.Fatalf("projected.Kind = %q", projected.Kind)
	}
	if projected.Phase != "executing_tools" {
		t.Fatalf("projected.Phase = %q", projected.Phase)
	}
	if len(projected.ToolNames) != 2 || projected.ToolNames[0] != "search.web" {
		t.Fatalf("projected.ToolNames = %v", projected.ToolNames)
	}
}

func TestRunEventProjectorProjectsPlanProgress(t *testing.T) {
	t.Parallel()

	projector := NewRunEventProjector()
	projected, ok := projector.ProjectLive(eventbus.Event{
		Type:  eventbus.EventPlanTaskStarted,
		RunID: "run-1",
		Attrs: map[string]any{
			"task_title":      "Fetch sources",
			"completed_count": 1,
			"total_tasks":     3,
		},
	}, RunProgressSnapshot{})
	if !ok {
		t.Fatal("expected projection")
	}
	if projected.Kind != ProjectedRunEventPlanProgress {
		t.Fatalf("projected.Kind = %q", projected.Kind)
	}
	if projected.ActiveTask != "Fetch sources" {
		t.Fatalf("projected.ActiveTask = %q", projected.ActiveTask)
	}
	if projected.Completed != 1 || projected.Total != 3 {
		t.Fatalf("progress = %d/%d", projected.Completed, projected.Total)
	}
}

func TestRunEventProjectorProjectsTaskProgressEvent(t *testing.T) {
	t.Parallel()

	projector := NewRunEventProjector()
	projected, ok := projector.ProjectLive(eventbus.Event{
		Type:  eventbus.EventTaskProgress,
		RunID: "run-1",
		Attrs: map[string]any{
			"current_task":    "Fetch sources",
			"completed_count": 2,
			"total_tasks":     5,
		},
	}, RunProgressSnapshot{})
	if !ok {
		t.Fatal("expected projection")
	}
	if projected.Kind != ProjectedRunEventPlanProgress {
		t.Fatalf("projected.Kind = %q", projected.Kind)
	}
	if projected.ActiveTask != "Fetch sources" {
		t.Fatalf("projected.ActiveTask = %q", projected.ActiveTask)
	}
	if projected.Completed != 2 || projected.Total != 5 {
		t.Fatalf("progress = %d/%d", projected.Completed, projected.Total)
	}
}

func TestRunEventProjectorProjectsTerminalFailure(t *testing.T) {
	t.Parallel()

	projector := NewRunEventProjector()
	session := &agent.Session{
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:    contextengine.RoleUser,
				Content: "帮我查资料",
				Metadata: map[string]any{
					"message_id": "evt-1",
				},
			}},
		},
	}
	run := &agent.Run{ID: "run-1", InputEventID: "evt-1"}
	projected, ok := projector.ProjectTerminal(session, run, nil, nil, eventbus.Event{
		Type:  eventbus.EventRunFailed,
		RunID: "run-1",
		Attrs: map[string]any{"error": "boom"},
	})
	if !ok {
		t.Fatal("expected terminal projection")
	}
	if projected.StatusKind != "failed" {
		t.Fatalf("projected.StatusKind = %q", projected.StatusKind)
	}
	if projected.Content == "" {
		t.Fatal("expected failure content")
	}
}

func TestRunEventProjectorUsesRunResultOutputForCompleted(t *testing.T) {
	t.Parallel()

	projector := NewRunEventProjector()
	session := &agent.Session{
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:    contextengine.RoleUser,
				Content: "hello",
				Metadata: map[string]any{
					"message_id": "evt-2",
				},
			}},
		},
	}
	run := &agent.Run{ID: "run-2", InputEventID: "evt-2"}
	projected, ok := projector.ProjectTerminal(session, run, &runtimesvc.RunResult{
		RunID:   "run-2",
		Output:  "final answer",
		Summary: "done",
	}, nil, eventbus.Event{
		Type:  eventbus.EventRunCompleted,
		RunID: "run-2",
	})
	if !ok {
		t.Fatal("expected terminal projection")
	}
	if projected.StatusKind != "completed" {
		t.Fatalf("projected.StatusKind = %q", projected.StatusKind)
	}
	if projected.Content != "final answer" {
		t.Fatalf("projected.Content = %q", projected.Content)
	}
}

func TestRunEventProjectorMarksPartialOnCompletedWarning(t *testing.T) {
	t.Parallel()

	projector := NewRunEventProjector()
	session := &agent.Session{
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:    contextengine.RoleUser,
				Content: "hello",
				Metadata: map[string]any{
					"message_id": "evt-warning",
				},
			}},
		},
	}
	run := &agent.Run{ID: "run-warning", InputEventID: "evt-warning"}
	projected, ok := projector.ProjectTerminal(session, run, &runtimesvc.RunResult{
		RunID:   "run-warning",
		Output:  "final answer",
		Outcome: runtimesvc.RunOutcomePartial,
	}, &verifyrt.RunVerification{
		RunID:   "run-warning",
		Status:  verifyrt.StatusWarning,
		Summary: "verification finished with 1 required warning",
	}, eventbus.Event{
		Type:  eventbus.EventRunCompleted,
		RunID: "run-warning",
	})
	if !ok {
		t.Fatal("expected terminal projection")
	}
	if projected.StatusKind != "partial" {
		t.Fatalf("projected.StatusKind = %q", projected.StatusKind)
	}
	if !strings.Contains(projected.Content, "part") && !strings.Contains(projected.Content, "部分") {
		t.Fatalf("projected.Content = %q", projected.Content)
	}
}

func TestRunEventProjectorUsesVerificationFailureMessageOnCompleted(t *testing.T) {
	t.Parallel()

	projector := NewRunEventProjector()
	session := &agent.Session{
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:    contextengine.RoleUser,
				Content: "帮我查资料",
				Metadata: map[string]any{
					"message_id": "evt-fail",
				},
			}},
		},
	}
	run := &agent.Run{ID: "run-fail", InputEventID: "evt-fail"}
	projected, ok := projector.ProjectTerminal(session, run, &runtimesvc.RunResult{
		RunID:  "run-fail",
		Output: "final answer",
	}, &verifyrt.RunVerification{
		RunID:            "run-fail",
		Status:           verifyrt.StatusFailed,
		Summary:          "verification blocked delivery: 1 blocking check did not pass",
		BlockingFailures: 1,
	}, eventbus.Event{
		Type:  eventbus.EventRunCompleted,
		RunID: "run-fail",
	})
	if !ok {
		t.Fatal("expected terminal projection")
	}
	if projected.StatusKind != "verification_failed" {
		t.Fatalf("projected.StatusKind = %q", projected.StatusKind)
	}
	if !strings.Contains(projected.Content, "没有验证通过") {
		t.Fatalf("projected.Content = %q", projected.Content)
	}
}

func TestRunEventProjectorSkipsSilentClarificationCancellation(t *testing.T) {
	t.Parallel()

	projector := NewRunEventProjector()
	_, ok := projector.ProjectLive(eventbus.Event{
		Type:  eventbus.EventRunCancelled,
		RunID: "run-clarification-old",
		Attrs: map[string]any{"reason": "preflight_clarification_superseded"},
	}, RunProgressSnapshot{})
	if ok {
		t.Fatal("expected silent clarification cancellation to be skipped")
	}
}

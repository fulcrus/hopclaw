package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/contextengine"
	planpkg "github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/resultmodel"
)

func TestTaskRunnerSharesStagedPipelineAcrossModes(t *testing.T) {
	t.Parallel()

	makeFixture := func(t *testing.T, sessionKey string) (*AgentComponent, *Run, *recordingModelClient) {
		t.Helper()

		sessions := NewInMemorySessionStore()
		runs := NewInMemoryRunStore()
		model := &recordingModelClient{
			responses: []*ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "Summarized the readme into a compact answer.",
				},
			}},
		}
		component := NewComponent(AgentConfig{
			DefaultModel:  "test-model",
			MaxToolRounds: 2,
			QueueMode:     QueueEnqueue,
		}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil)

		run, err := component.Submit(context.Background(), IncomingMessage{
			SessionKey:      sessionKey,
			ExternalEventID: sessionKey + "-evt",
			Content:         "read the readme and summarize it",
		})
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
		run.Plan = &planpkg.Plan{
			Goal:     "Read the readme and summarize it.",
			Strategy: planpkg.StrategySerial,
			Tasks: []planpkg.Task{{
				ID:    "inspect",
				Kind:  planpkg.TaskResearch,
				Title: "Inspect file",
				Goal:  "Read the readme and summarize it.",
			}},
			FinalTask: "inspect",
		}
		if err := runs.Update(context.Background(), run); err != nil {
			t.Fatalf("runs.Update() error = %v", err)
		}
		return component, run, model
	}

	componentDetached, runDetached, modelDetached := makeFixture(t, "task-runner-detached")
	sessionDetached, err := componentDetached.sessions.GetOrCreate(context.Background(), "task-runner-detached", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate(detached) error = %v", err)
	}
	taskDetached := &runDetached.Plan.Tasks[0]
	detachedResult := componentDetached.runSingleTask(context.Background(), runDetached, cloneSession(sessionDetached), taskDetached, nil, nil)

	componentSerial, runSerial, modelSerial := makeFixture(t, "task-runner-serial")
	sessionSerial, unlock, err := componentSerial.sessions.LoadForExecution(context.Background(), runSerial.SessionID)
	if err != nil {
		t.Fatalf("LoadForExecution(serial) error = %v", err)
	}
	lease := &sessionLease{
		session: sessionSerial,
		unlock:  unlock,
	}
	defer lease.close()
	taskSerial := &runSerial.Plan.Tasks[0]
	serialResult := componentSerial.runSingleTaskSerial(context.Background(), runSerial, lease, taskSerial, nil, nil)

	if detachedResult.Status != planpkg.TaskCompleted {
		t.Fatalf("detached status = %q, want completed", detachedResult.Status)
	}
	if serialResult.Status != planpkg.TaskCompleted {
		t.Fatalf("serial status = %q, want completed", serialResult.Status)
	}
	if detachedResult.Output != serialResult.Output {
		t.Fatalf("task outputs differ: detached=%q serial=%q", detachedResult.Output, serialResult.Output)
	}
	if detachedResult.Summary != serialResult.Summary {
		t.Fatalf("task summaries differ: detached=%q serial=%q", detachedResult.Summary, serialResult.Summary)
	}

	if len(modelDetached.requests) != 1 {
		t.Fatalf("detached request count = %d, want 1", len(modelDetached.requests))
	}
	if len(modelSerial.requests) != 1 {
		t.Fatalf("serial request count = %d, want 1", len(modelSerial.requests))
	}
	detachedPrompt := modelDetached.requests[0].SystemPrompt
	serialPrompt := modelSerial.requests[0].SystemPrompt
	if detachedPrompt != serialPrompt {
		t.Fatalf("staged prompts differ:\n--- detached ---\n%s\n--- serial ---\n%s", detachedPrompt, serialPrompt)
	}
	for _, marker := range []string{
		"Current task: Inspect file",
		"Overall goal: Read the readme and summarize it.",
		"This is the final task. Produce the user-facing answer.",
	} {
		if !strings.Contains(detachedPrompt, marker) {
			t.Fatalf("shared staged prompt missing %q:\n%s", marker, detachedPrompt)
		}
	}
}

func TestDependencyOutcomePromptPayloadUsesStructuredArtifacts(t *testing.T) {
	t.Parallel()

	payload := dependencyOutcomePromptPayload([]TaskExecutionResult{{
		TaskID: "research",
		Status: planpkg.TaskCompleted,
		Outcome: &TaskOutcome{
			TaskID:         "research",
			Status:         planpkg.TaskCompleted,
			Attempt:        1,
			Summary:        "captured browser evidence",
			IdempotencyKey: "task:research",
			Artifacts: []resultmodel.ResultArtifact{{
				Kind: "artifact",
				URI:  "artifact://local/screenshot",
			}},
			ToolResults: []resultmodel.ToolResult{{
				ToolName:   "browser.snapshot",
				ToolCallID: "call-1",
				Status:     resultmodel.ToolResultOK,
				Summary:    "snapshot captured",
				Structured: map[string]any{"url": "https://example.com"},
				Artifacts: []resultmodel.ResultArtifact{{
					Kind: "artifact",
					URI:  "artifact://local/screenshot",
				}},
			}},
		},
	}})

	if !strings.Contains(payload, "\"artifact://local/screenshot\"") {
		t.Fatalf("payload missing artifact uri: %s", payload)
	}
	if !strings.Contains(payload, "\"idempotency_key\": \"task:research\"") {
		t.Fatalf("payload missing idempotency_key: %s", payload)
	}
	if !strings.Contains(payload, "\"tool_results\"") {
		t.Fatalf("payload missing structured tool results: %s", payload)
	}
	if strings.Contains(payload, "Completed dependency tasks:") {
		t.Fatalf("payload leaked legacy free-text dependency summary: %s", payload)
	}
}

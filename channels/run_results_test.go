package channels

import (
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func TestBridgeCompletedResultContentIncludesModelFailoverNotice(t *testing.T) {
	t.Parallel()

	session := &agent.Session{
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:    contextengine.RoleUser,
				Content: "帮我处理这个请求",
				Metadata: map[string]any{
					"message_id": "evt-failover",
				},
			}},
		},
	}

	result := &runtimesvc.RunResult{
		RunID:  "run-failover",
		Output: "处理完成。",
		EventLedger: &runtimesvc.EventLedger{
			RunID: "run-failover",
			Events: []runtimesvc.LedgerEvent{{
				Type:  eventbus.EventModelFailover,
				RunID: "run-failover",
				Attrs: map[string]any{
					"from_model": "gpt-5.4",
					"to_model":   "deepseek/deepseek-chat",
					"reason":     "retry failover after timeout",
				},
			}},
		},
	}

	content := BridgeCompletedResultContent(session, "run-failover", "evt-failover", result, nil, eventbus.Event{
		Type:  eventbus.EventRunCompleted,
		RunID: "run-failover",
	})

	if !strings.Contains(content, "主模型 gpt-5.4 请求超时") {
		t.Fatalf("content = %q, want timeout failover notice", content)
	}
	if !strings.Contains(content, "deepseek/deepseek-chat") {
		t.Fatalf("content = %q, want fallback model", content)
	}
	if !strings.Contains(content, "处理完成。") {
		t.Fatalf("content = %q, want original output", content)
	}
}

func TestBridgeCompletedResultContentIncludesThinkingDegradedNotice(t *testing.T) {
	t.Parallel()

	session := &agent.Session{
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:    contextengine.RoleUser,
				Content: "please solve this",
				Metadata: map[string]any{
					"message_id": "evt-thinking",
				},
			}},
		},
	}

	result := &runtimesvc.RunResult{
		RunID:  "run-thinking",
		Output: "Done.",
		EventLedger: &runtimesvc.EventLedger{
			RunID: "run-thinking",
			Events: []runtimesvc.LedgerEvent{{
				Type:  eventbus.EventThinkingDegraded,
				RunID: "run-thinking",
				Attrs: map[string]any{
					"model":  "gpt-5.4",
					"from":   "extended",
					"to":     "regular",
					"reason": "timeout",
				},
			}},
		},
	}

	content := BridgeCompletedResultContent(session, "run-thinking", "evt-thinking", result, nil, eventbus.Event{
		Type:  eventbus.EventRunCompleted,
		RunID: "run-thinking",
	})

	if !strings.Contains(content, "extended thinking mode timed out") {
		t.Fatalf("content = %q, want thinking degradation notice", content)
	}
	if !strings.Contains(content, "Done.") {
		t.Fatalf("content = %q, want original output", content)
	}
}

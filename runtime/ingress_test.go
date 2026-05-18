package runtime

import (
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestParseStructuredClarificationSlotsAcceptsCanonicalReplyTemplateLines(t *testing.T) {
	t.Parallel()

	run := &agent.Run{
		Preflight: &agent.RunPreflightReport{
			ClarificationSlots: []agent.RunClarificationSlot{{
				ID:    "source_target",
				Label: "目标对象",
			}},
		},
	}

	got := parseStructuredClarificationSlots(run, "source_target (目标对象): docs/tmp/example-brief.md")
	if got["source_target"] != "docs/tmp/example-brief.md" {
		t.Fatalf("parseStructuredClarificationSlots() = %#v", got)
	}
}

func TestParseStructuredClarificationSlotsAcceptsRunScopedDynamicLabels(t *testing.T) {
	t.Parallel()

	run := &agent.Run{
		Preflight: &agent.RunPreflightReport{
			ClarificationSlots: []agent.RunClarificationSlot{{
				ID:    "delivery_target",
				Label: "发送位置",
			}},
		},
	}

	got := parseStructuredClarificationSlots(run, "发送位置: Slack #ops")
	if got["delivery_target"] != "Slack #ops" {
		t.Fatalf("parseStructuredClarificationSlots() = %#v", got)
	}
}

func TestClarificationReplyContainsOnlyResolvedInputsUsesResidualThreshold(t *testing.T) {
	t.Parallel()

	run := &agent.Run{
		TaskContract: &agent.TaskContract{
			MissingInfo: []agent.TaskContractMissingInfo{{
				ID:    "source_target",
				Label: "目标对象",
			}},
		},
	}

	if !clarificationReplyContainsOnlyResolvedInputs(run, "/tmp/demo.txt，继续") {
		t.Fatal("expected short residual clarification to merge")
	}
	if clarificationReplyContainsOnlyResolvedInputs(run, "抓取页面信息，写到 docs/tmp/example-brief.md") {
		t.Fatal("expected standalone task wording to stay separate")
	}
}

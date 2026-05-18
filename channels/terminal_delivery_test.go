package channels

import (
	"testing"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/resultmodel"
)

func TestParseToolDeliverablesReadsStructuredMetadata(t *testing.T) {
	t.Parallel()

	msg := contextengine.Message{
		Role:       contextengine.RoleTool,
		Name:       "browser.snapshot",
		ToolCallID: "call-1",
		Content:    "snapshot captured",
		Metadata: map[string]any{
			resultmodel.MetadataKeyToolResult: map[string]any{
				"tool_name":       "browser.snapshot",
				"tool_call_id":    "call-1",
				"transcript_text": "snapshot captured",
				"artifacts": []map[string]any{{
					"kind": "artifact",
					"uri":  "artifact://local/browser-snapshot",
				}},
			},
		},
	}

	refs := parseToolDeliverables(msg)
	if len(refs) != 1 {
		t.Fatalf("refs = %#v", refs)
	}
	if refs[0].ArtifactURI != "artifact://local/browser-snapshot" {
		t.Fatalf("ArtifactURI = %q", refs[0].ArtifactURI)
	}
}

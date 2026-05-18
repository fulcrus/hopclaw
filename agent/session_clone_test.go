package agent

import (
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

func TestCloneSessionDeepCopiesNestedState(t *testing.T) {
	t.Parallel()

	original := &Session{
		ID:       "sess-1",
		Key:      "chat-1",
		Revision: 3,
		Metadata: map[string]any{
			"nested": map[string]any{
				"items": []any{"one", "two"},
			},
		},
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:    contextengine.RoleUser,
				Content: "hello",
				Metadata: map[string]any{
					"payload": map[string]any{
						"tags": []any{"a"},
					},
				},
				CreatedAt: time.Now().UTC(),
			}},
			PinnedFacts: []contextengine.PinnedFact{{
				Key:     "profile.name",
				Content: "Alice",
				Metadata: map[string]any{
					"source": "user",
				},
			}},
			SkillSnapshot: skill.SessionSkillSnapshot{
				Skills: map[string]skill.BoundSkill{
					"writer": {Package: &skill.SkillPackage{Prompt: skill.PromptSkill{Name: "writer"}}},
				},
			},
		},
	}

	cloned := cloneSession(original)
	if cloned == nil {
		t.Fatal("cloneSession() = nil")
	}

	cloned.Metadata["nested"].(map[string]any)["items"].([]any)[0] = "changed"
	cloned.Messages[0].Metadata["payload"].(map[string]any)["tags"].([]any)[0] = "changed"
	cloned.PinnedFacts[0].Metadata["source"] = "agent"
	bound := cloned.SkillSnapshot.Skills["writer"]
	bound.Package.Prompt.Name = "reviewer"
	cloned.SkillSnapshot.Skills["writer"] = bound

	if got := original.Metadata["nested"].(map[string]any)["items"].([]any)[0]; got != "one" {
		t.Fatalf("original session metadata mutated: %v", got)
	}
	if got := original.Messages[0].Metadata["payload"].(map[string]any)["tags"].([]any)[0]; got != "a" {
		t.Fatalf("original message metadata mutated: %v", got)
	}
	if got := original.PinnedFacts[0].Metadata["source"]; got != "user" {
		t.Fatalf("original pinned fact metadata mutated: %v", got)
	}
	if got := original.SkillSnapshot.Skills["writer"].Package.Name(); got != "writer" {
		t.Fatalf("original skill snapshot mutated: %q", got)
	}
}

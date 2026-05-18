package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// memoryExtractionPrompt guides the agent on when and how to persist memories.
const memoryExtractionPrompt = `You have persistent memory across conversations. Use the available memory tools in this turn to save useful information when they are present.

SAVE (via memory tools when available):
- Server addresses, ports, connection methods shared by the user
- Deployment procedures you successfully executed
- User preferences ("I prefer...", "always use...", "never...")
- Project architecture decisions
- Recurring workflows or procedures

DO NOT SAVE:
- Code snippets (already in repository)
- One-time debugging information
- Content already in config files

RULES:
- Save at most 3 memories per conversation
- Before saving, check for duplicates with the available memory lookup tools
- Use descriptive English keys (deploy_server, deploy_steps, db_port)
- When user indicates something is wrong, inspect stored memory with the available lookup tools and update stale entries
- After successfully completing a multi-step operation, save the procedure
- If memory tools are not available in this turn, do not claim that you saved or searched memory

AUTHORITY:
- Memories marked with source=user CANNOT be overwritten by you
- If a memory write returns blocked, ask the user if they want to update it`

const memoryConflictPrompt = `Potential memory conflicts were detected in recalled context. Treat the conflicting entries as uncertain until you resolve the discrepancy from stronger evidence, fresher evidence, or an explicit user confirmation.`

// InjectMemoryFacts converts recalled memories into PinnedFacts for prompt injection.
func InjectMemoryFacts(memories []MemoryEntry) []contextengine.PinnedFact {
	if len(memories) == 0 {
		return nil
	}
	facts := make([]contextengine.PinnedFact, 0, len(memories)+1)
	facts = append(facts, contextengine.PinnedFact{
		Key:     "_memory_guide",
		Content: memoryExtractionPrompt,
		Source:  "system",
	})
	for _, memory := range memories {
		value := strings.TrimSpace(memory.Value)
		if value == "" {
			continue
		}
		if label := strings.TrimSpace(memory.Label); label != "" {
			value = "[" + label + "] " + value
		}
		source := "agent-learned"
		if strings.EqualFold(strings.TrimSpace(memory.Source), MemorySourceUser) {
			source = "user-defined (DO NOT overwrite)"
		}
		key := strings.TrimSpace(memory.Key)
		if key == "" {
			key = fmt.Sprintf("memory_%d", len(facts))
		}
		facts = append(facts, contextengine.PinnedFact{
			Key:     "memory:" + key,
			Content: fmt.Sprintf("%s (source: %s)", value, source),
			Source:  "memory",
		})
	}
	if len(facts) == 1 {
		return nil
	}
	return facts
}

// InjectMemoryRecallFacts converts recalled memories and memory conflicts into
// pinned facts for prompt injection.
func InjectMemoryRecallFacts(recalled RecallResult) []contextengine.PinnedFact {
	facts := InjectMemoryFacts(recalled.Memories)
	conflicts := InjectMemoryConflictFacts(recalled.Conflicts)
	if len(facts) == 0 {
		return conflicts
	}
	if len(conflicts) == 0 {
		return facts
	}
	return append(facts, conflicts...)
}

// InjectMemoryConflictFacts converts detected conflicts into pinned facts so
// the model can treat conflicting memories as uncertain instead of silently
// trusting both.
func InjectMemoryConflictFacts(conflicts []MemoryConflict) []contextengine.PinnedFact {
	if len(conflicts) == 0 {
		return nil
	}
	facts := []contextengine.PinnedFact{{
		Key:     "_memory_conflicts",
		Content: memoryConflictPrompt,
		Source:  "memory",
	}}
	for i, conflict := range conflicts {
		summary := summarizeMemoryConflict(conflict)
		if summary == "" {
			continue
		}
		facts = append(facts, contextengine.PinnedFact{
			Key:     fmt.Sprintf("memory_conflict:%d", i),
			Content: summary,
			Source:  "memory",
		})
	}
	if len(facts) == 1 {
		return nil
	}
	return facts
}

func (a *AgentComponent) injectPromptMemoryFacts(ctx context.Context, session *Session, runtimeCtx skill.RuntimeContext) {
	if a == nil || session == nil || a.memory == nil {
		return
	}
	memories, err := a.memory.List(ctx)
	if err != nil {
		log.Warn("load memory prompt facts failed", "session_id", session.ID, "error", err)
		return
	}
	recalled := RecallForContext(memories, session.Key, projectIDFromRuntimeContext(runtimeCtx))
	injected := InjectMemoryRecallFacts(recalled)
	if len(injected) == 0 {
		return
	}
	session.PinnedFacts = mergePromptPinnedFacts(session.PinnedFacts, injected)
}

func projectIDFromRuntimeContext(runtimeCtx skill.RuntimeContext) string {
	for _, root := range []string{runtimeCtx.Git.Root, runtimeCtx.Workspace.Root} {
		root = strings.TrimSpace(root)
		if root != "" {
			return ProjectID(root)
		}
	}
	return ""
}

func mergePromptPinnedFacts(existing, injected []contextengine.PinnedFact) []contextengine.PinnedFact {
	if len(existing) == 0 {
		return clonePinnedFacts(injected)
	}
	if len(injected) == 0 {
		return clonePinnedFacts(existing)
	}
	out := clonePinnedFacts(existing)
	index := make(map[string]int, len(out))
	for i, fact := range out {
		index[pinnedFactIdentity(fact)] = i
	}
	for _, fact := range injected {
		identity := pinnedFactIdentity(fact)
		if idx, ok := index[identity]; ok {
			out[idx] = fact
			continue
		}
		index[identity] = len(out)
		out = append(out, fact)
	}
	return out
}

func pinnedFactIdentity(fact contextengine.PinnedFact) string {
	if key := strings.TrimSpace(fact.Key); key != "" {
		return "key:" + key
	}
	return "content:" + strings.TrimSpace(fact.Content)
}

func summarizeMemoryConflict(conflict MemoryConflict) string {
	base := strings.TrimSpace(conflict.Message)
	switch conflict.Kind {
	case ConflictMemoryVsMemory:
		if base == "" {
			base = fmt.Sprintf("Memory conflict: %s=%s vs %s=%s",
				preferMemoryConflictField(conflict.EntryA),
				strings.TrimSpace(conflict.EntryA.Value),
				preferMemoryConflictField(conflict.EntryBValue()),
				strings.TrimSpace(conflict.EntryBValue().Value),
			)
		}
	case ConflictMemoryVsEnv:
		if base == "" {
			base = fmt.Sprintf("Memory vs environment conflict: %s=%s vs env=%s",
				preferMemoryConflictField(conflict.EntryA),
				strings.TrimSpace(conflict.EntryA.Value),
				strings.TrimSpace(conflict.EnvValue),
			)
		}
	case ConflictGlobalVsProject:
		if base == "" {
			base = fmt.Sprintf("Global vs project memory conflict: %s=%s",
				preferMemoryConflictField(conflict.EntryA),
				strings.TrimSpace(conflict.EntryA.Value),
			)
		}
	}
	if base == "" {
		return ""
	}
	return "Potential memory conflict: " + base
}

func preferMemoryConflictField(entry MemoryEntry) string {
	for _, value := range []string{entry.Field, entry.Key, entry.Label, entry.Namespace} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "memory"
}

func (c MemoryConflict) EntryBValue() MemoryEntry {
	if c.EntryB != nil {
		return *c.EntryB
	}
	return MemoryEntry{}
}

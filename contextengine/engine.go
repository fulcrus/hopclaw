package contextengine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

// toolResultKeepLinesDefault is the number of lines preserved at the head and
// tail of a large tool result when soft-trimming. The middle is replaced with
// a single "N lines omitted" marker so the model can see both the beginning
// (context/structure) and the end (final output/errors) of the result.
const toolResultKeepLinesDefault = 20

const (
	defaultSkillPromptMaxChars = 12000
	skillPromptOmittedNotice   = "Additional skills are omitted from this summary. Only use skill tools when they are available in the current turn."
	defaultPinnedFactsMaxChars = 4000
	compactionTimeout          = 2 * time.Minute
)

type Config struct {
	BaseSystemPrompt     string
	IncludeSkillCatalog  bool
	SkillPromptMaxChars  int
	KeepFirstN           int
	KeepLastN            int
	MaxInputRatio        float64
	DefaultContextWindow int
	DefaultOutputTokens  int
	ToolResultMaxChars   int
	// ToolResultKeepLines is the number of lines preserved at the head and tail
	// of a large tool result when soft-trimming. 0 disables line-preserving
	// trimming and falls back to a hard character-limit cut.
	ToolResultKeepLines int
	CompactKeepLastN    int
	CompactSummaryChars int
	PinnedFactsMaxChars int
	Estimator           TokenEstimator
	Summarizer          Summarizer // Optional model-based summarizer; falls back to naive concatenation.

	// AutoCompactThreshold triggers NeedsCompaction when the estimated input
	// token usage exceeds this fraction of MaxInputTokens (0 to 1).
	// 0 disables auto-compact signaling. Default: 0 (disabled; caller decides).
	AutoCompactThreshold float64

	// CompactKeepRecentTokens is the minimum number of recent tokens to preserve
	// after compaction. Whichever constraint (this or CompactKeepLastN) keeps
	// MORE messages wins. Default: 0 (message count only).
	CompactKeepRecentTokens int

	// PreCompactHook is called before summarization to flush important
	// context from discarded messages to durable storage. Non-fatal on error.
	PreCompactHook PreCompactHook

	// MemoryReader loads durable pinned facts from the memory store during
	// Prepare(). Optional; when nil, only session-local pinned facts are used.
	MemoryReader MemoryReader

	// MemoryWriter persists pinned facts to the memory store during Compact().
	// Optional; when nil, pinned facts are not synced to durable storage.
	MemoryWriter MemoryWriter

	// SegmentWriter persists immutable summary segments during Compact().
	// Optional; when nil, compaction falls back to session.Summary only.
	SegmentWriter SegmentWriter

	// SegmentReader loads recent immutable summary segments during Prepare().
	// Optional; when nil, Prepare falls back to session.Summary only.
	SegmentReader SegmentReader

	// SegmentSearcher performs semantic recall against previously compacted
	// segments during Prepare(). Optional; when nil, recalled_context is omitted.
	SegmentSearcher SegmentSearcher

	// EmbeddingClient generates segment embeddings during Compact() and query
	// embeddings during Prepare(). Optional; failures are non-fatal.
	EmbeddingClient EmbeddingClient

	// StateWriter persists extracted session state during Compact().
	// Optional; when nil, compaction only updates summary/segments.
	StateWriter StateWriter

	// StateReader loads active durable session state during Prepare().
	// Optional; when nil, Prepare omits the session_state prompt block.
	StateReader StateReader

	// OnCompact is called after compaction completes with observability data.
	OnCompact func(CompactEvent)

	// PostCompactHook runs after compaction computes the next session state.
	// Returning an error aborts the compaction before the new state is committed.
	PostCompactHook PostCompactHook
}

type SlidingWindowEngine struct {
	config Config
	skills SkillBinder
}

func NewSlidingWindowEngine(cfg Config, skills SkillBinder) *SlidingWindowEngine {
	if cfg.KeepFirstN < 0 {
		cfg.KeepFirstN = 0
	}
	if cfg.KeepLastN <= 0 {
		cfg.KeepLastN = 20
	}
	if cfg.MaxInputRatio <= 0 || cfg.MaxInputRatio > 1 {
		cfg.MaxInputRatio = 0.75
	}
	if cfg.DefaultContextWindow <= 0 {
		cfg.DefaultContextWindow = 128000
	}
	if cfg.DefaultOutputTokens <= 0 {
		cfg.DefaultOutputTokens = 4000
	}
	if cfg.SkillPromptMaxChars <= 0 {
		cfg.SkillPromptMaxChars = defaultSkillPromptMaxChars
	}
	if cfg.ToolResultMaxChars <= 0 {
		cfg.ToolResultMaxChars = 4000
	}
	if cfg.ToolResultKeepLines <= 0 {
		cfg.ToolResultKeepLines = toolResultKeepLinesDefault
	}
	if cfg.CompactKeepLastN <= 0 {
		cfg.CompactKeepLastN = 20
	}
	if cfg.CompactSummaryChars <= 0 {
		cfg.CompactSummaryChars = 4000
	}
	if cfg.PinnedFactsMaxChars <= 0 {
		cfg.PinnedFactsMaxChars = defaultPinnedFactsMaxChars
	}
	if cfg.Estimator == nil {
		cfg.Estimator = CharRatioEstimator{
			CharsPerToken:        4.0,
			ToolCharsPerToken:    2.0,
			EmptyMessageOverhead: 4,
		}
	}
	return &SlidingWindowEngine{
		config: cfg,
		skills: skills,
	}
}

func filterSkillSnapshot(snapshot skill.SessionSkillSnapshot, allowed []string) skill.SessionSkillSnapshot {
	if len(allowed) == 0 {
		return snapshot
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		allowedSet[strings.ToLower(trimmed)] = struct{}{}
	}
	if len(allowedSet) == 0 {
		return snapshot
	}

	filtered := snapshot
	filtered.Skills = make(map[string]skill.BoundSkill, len(snapshot.Skills))
	filtered.Ordered = make([]skill.BoundSkill, 0, len(snapshot.Ordered))
	filtered.PromptCatalog = make([]skill.PromptCatalogEntry, 0, len(snapshot.PromptCatalog))

	for name, bound := range snapshot.Skills {
		if _, ok := allowedSet[strings.ToLower(strings.TrimSpace(name))]; !ok {
			continue
		}
		filtered.Skills[name] = bound
	}
	for _, bound := range snapshot.Ordered {
		if bound.Package == nil {
			continue
		}
		if _, ok := allowedSet[strings.ToLower(strings.TrimSpace(bound.Package.Name()))]; !ok {
			continue
		}
		filtered.Ordered = append(filtered.Ordered, bound)
	}
	for _, entry := range snapshot.PromptCatalog {
		if _, ok := allowedSet[strings.ToLower(strings.TrimSpace(entry.Name))]; !ok {
			continue
		}
		filtered.PromptCatalog = append(filtered.PromptCatalog, entry)
	}
	filtered.PromptBlock = strings.TrimSpace(skill.FormatPromptCatalog(filtered.PromptCatalog))
	return filtered
}

func (e *SlidingWindowEngine) AppendToolResults(_ context.Context, session *Session, results []ToolResult) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}
	for _, result := range results {
		normalized := result.Normalized()
		content := strings.TrimSpace(normalized.TranscriptText)
		if e.config.ToolResultMaxChars > 0 && len(content) > e.config.ToolResultMaxChars {
			content = softTrimContent(content, e.config.ToolResultMaxChars, e.config.ToolResultKeepLines)
		}
		metadata := cloneAnyMap(normalized.Metadata)
		if metadata == nil {
			metadata = make(map[string]any, 1)
		}
		metadata[resultmodel.MetadataKeyToolResult] = normalized.MarshalMetadata()
		session.Messages = append(session.Messages, Message{
			Role:       RoleTool,
			Name:       normalized.ToolName,
			ToolCallID: normalized.ToolCallID,
			Content:    content,
			CreatedAt:  time.Now().UTC(),
			Metadata:   metadata,
		})
	}
	return nil
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func collectPinnedFacts(messages []Message) []PinnedFact {
	if len(messages) == 0 {
		return nil
	}
	var out []PinnedFact
	for _, msg := range messages {
		if len(msg.Metadata) == 0 {
			continue
		}
		out = append(out, decodePinnedFactsMetadata(msg.Metadata[MetadataKeyPinnedFact], msg)...)
		out = append(out, decodePinnedFactsMetadata(msg.Metadata[MetadataKeyPinnedFacts], msg)...)
	}
	return mergePinnedFacts(out)
}

func decodePinnedFactsMetadata(raw any, msg Message) []PinnedFact {
	switch typed := raw.(type) {
	case nil:
		return nil
	case string:
		fact := normalizePinnedFact(PinnedFact{
			Content:   typed,
			Source:    string(msg.Role),
			UpdatedAt: msg.CreatedAt,
		})
		if fact.Content == "" {
			return nil
		}
		return []PinnedFact{fact}
	case map[string]any:
		fact := normalizePinnedFact(decodePinnedFactMap(typed, msg))
		if fact.Content == "" {
			return nil
		}
		return []PinnedFact{fact}
	case []any:
		out := make([]PinnedFact, 0, len(typed))
		for _, item := range typed {
			out = append(out, decodePinnedFactsMetadata(item, msg)...)
		}
		return out
	default:
		return nil
	}
}

func decodePinnedFactMap(raw map[string]any, msg Message) PinnedFact {
	fact := PinnedFact{
		Source:    string(msg.Role),
		UpdatedAt: msg.CreatedAt,
	}
	if raw == nil {
		return fact
	}
	if value, ok := raw["key"]; ok {
		fact.Key = strings.TrimSpace(fmt.Sprint(value))
	}
	for _, field := range []string{"content", "text", "summary"} {
		if value, ok := raw[field]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			fact.Content = strings.TrimSpace(fmt.Sprint(value))
			break
		}
	}
	if value, ok := raw["source"]; ok {
		fact.Source = strings.TrimSpace(fmt.Sprint(value))
	}
	if value, ok := raw["updated_at"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
			fact.UpdatedAt = parsed
		}
	}
	if metadata, ok := raw["metadata"].(map[string]any); ok && len(metadata) != 0 {
		fact.Metadata = cloneAnyMap(metadata)
	}
	return fact
}

func normalizePinnedFact(fact PinnedFact) PinnedFact {
	fact.Key = strings.TrimSpace(fact.Key)
	fact.Content = strings.TrimSpace(fact.Content)
	fact.Source = strings.TrimSpace(fact.Source)
	fact.Metadata = cloneAnyMap(fact.Metadata)
	return fact
}

func clonePinnedFacts(in []PinnedFact) []PinnedFact {
	if len(in) == 0 {
		return nil
	}
	out := make([]PinnedFact, len(in))
	copy(out, in)
	for i := range out {
		out[i].Metadata = cloneAnyMap(out[i].Metadata)
	}
	return out
}

func cloneMessages(in []Message) []Message {
	if in == nil {
		return nil
	}
	out := make([]Message, len(in))
	copy(out, in)
	for i := range out {
		out[i].ContentBlocks = cloneContentBlocks(out[i].ContentBlocks)
		out[i].ToolCalls = cloneToolCallRefs(out[i].ToolCalls)
		out[i].Metadata = cloneAnyMap(out[i].Metadata)
	}
	return out
}

func cloneContentBlocks(in []ContentBlock) []ContentBlock {
	if in == nil {
		return nil
	}
	out := make([]ContentBlock, len(in))
	copy(out, in)
	return out
}

func cloneToolCallRefs(in []ToolCallRef) []ToolCallRef {
	if in == nil {
		return nil
	}
	out := make([]ToolCallRef, len(in))
	copy(out, in)
	return out
}

func cloneSession(session *Session) *Session {
	if session == nil {
		return nil
	}
	cloned := *session
	cloned.Messages = cloneMessages(session.Messages)
	cloned.PinnedFacts = clonePinnedFacts(session.PinnedFacts)
	cloned.LoadedMessageSeqs = append([]int64(nil), session.LoadedMessageSeqs...)
	return &cloned
}

func mergePinnedFacts(groups ...[]PinnedFact) []PinnedFact {
	var out []PinnedFact
	indexByID := make(map[string]int)
	for _, group := range groups {
		for _, raw := range group {
			fact := normalizePinnedFact(raw)
			if fact.Content == "" {
				continue
			}
			identity := pinnedFactIdentity(fact)
			if idx, ok := indexByID[identity]; ok {
				out[idx] = fact
				continue
			}
			indexByID[identity] = len(out)
			out = append(out, fact)
		}
	}
	return clonePinnedFacts(out)
}

func pinnedFactIdentity(fact PinnedFact) string {
	if key := strings.ToLower(strings.TrimSpace(fact.Key)); key != "" {
		return "key:" + key
	}
	return "content:" + strings.ToLower(strings.TrimSpace(fact.Content))
}

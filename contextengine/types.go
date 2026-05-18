package contextengine

import (
	"context"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"

	MetadataKeyPinnedFact         = "pinned_fact"
	MetadataKeyPinnedFacts        = "pinned_facts"
	MetadataKeySignalDecisions    = "signal_decisions"
	MetadataKeySignalTODOs        = "signal_todos"
	MetadataKeySignalConstraints  = "signal_constraints"
	MetadataKeyMessageImportance  = "message_importance"
)

// ---------------------------------------------------------------------------
// Content block types for multimodal messages (vision / image understanding)
// ---------------------------------------------------------------------------

// ContentBlockType identifies the kind of content in a ContentBlock.
type ContentBlockType string

const (
	ContentBlockText      ContentBlockType = "text"
	ContentBlockImage     ContentBlockType = "image"
	ContentBlockFile      ContentBlockType = "file"
	ContentBlockDirectory ContentBlockType = "directory"
	ContentBlockVideo     ContentBlockType = "video"
	ContentBlockSnippet   ContentBlockType = "snippet"
	ContentBlockLink      ContentBlockType = "link"
)

// ContentBlock represents a single piece of content within a multimodal message.
// When a Message has ContentBlocks, they take precedence over the plain Content field.
type ContentBlock struct {
	Type      ContentBlockType `json:"type"`
	Text      string           `json:"text,omitempty"`       // for text blocks
	Label     string           `json:"label,omitempty"`      // display label for referenced content
	Path      string           `json:"path,omitempty"`       // local path for file / dir / video references
	MediaType string           `json:"media_type,omitempty"` // e.g. "image/jpeg", "image/png"
	Data      string           `json:"data,omitempty"`       // base64-encoded image data
	MediaRef  string           `json:"media_ref,omitempty"`  // artifact URI reference, e.g. "artifact://local/{id}"
	SourceURL string           `json:"source_url,omitempty"` // URL reference for the image
}

// MediaResolver reads media blobs by URI reference.
type MediaResolver interface {
	Read(uri string) (body []byte, contentType string, err error)
}

type Message struct {
	Role          MessageRole    `json:"role"`
	Content       string         `json:"content"`
	ContentBlocks []ContentBlock `json:"content_blocks,omitempty"`
	Name          string         `json:"name,omitempty"`
	ToolCallID    string         `json:"tool_call_id,omitempty"`
	ToolCalls     []ToolCallRef  `json:"tool_calls,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type PinnedFact struct {
	Key       string         `json:"key,omitempty"`
	Content   string         `json:"content"`
	Source    string         `json:"source,omitempty"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// HasImageContent reports whether the message contains at least one image content block.
func (m Message) HasImageContent() bool {
	for _, block := range m.ContentBlocks {
		if block.Type == ContentBlockImage {
			return true
		}
	}
	return false
}

// TextContent returns the textual content of the message. When ContentBlocks
// is non-empty, it concatenates all text blocks; otherwise it returns Content.
func (m Message) TextContent() string {
	if len(m.ContentBlocks) == 0 {
		return m.Content
	}
	var parts []string
	for _, block := range m.ContentBlocks {
		if block.Type == ContentBlockText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "")
}

// NewTextMessage creates a simple text-only message.
func NewTextMessage(role MessageRole, text string) Message {
	return Message{
		Role:      role,
		Content:   text,
		CreatedAt: time.Now().UTC(),
	}
}

// NewImageMessage creates a message containing a text block and an image block.
func NewImageMessage(role MessageRole, text, mediaType, base64Data string) Message {
	blocks := make([]ContentBlock, 0, 2)
	if text != "" {
		blocks = append(blocks, ContentBlock{
			Type: ContentBlockText,
			Text: text,
		})
	}
	blocks = append(blocks, ContentBlock{
		Type:      ContentBlockImage,
		MediaType: mediaType,
		Data:      base64Data,
	})
	return Message{
		Role:          role,
		Content:       text, // fallback for consumers that don't support ContentBlocks
		ContentBlocks: blocks,
		CreatedAt:     time.Now().UTC(),
	}
}

// ToolCallRef stores a reference to a tool call made by the assistant,
// so the conversation can be replayed correctly for model APIs that
// require assistant tool_call messages before tool result messages.
type ToolCallRef struct {
	ID        string
	Name      string
	Arguments string
}

type Session struct {
	// ID is assigned by the context engine for internal tracking. When this
	// session is embedded in agent.Session, the outer Session.ID takes
	// precedence and should be treated as authoritative.
	ID                 string       `json:"id,omitempty"`
	Messages           []Message    `json:"messages,omitempty"`
	Summary            string       `json:"summary,omitempty"`
	SummaryAt          time.Time    `json:"summary_at,omitempty"`
	PinnedFacts        []PinnedFact `json:"pinned_facts,omitempty"`
	ExecutionWatermark int64        `json:"execution_watermark,omitempty"`
	// LoadedMessageSeqs tracks the durable seq values for the currently loaded
	// hot messages. SQLite-backed execution snapshots use it to advance the
	// execution watermark without reloading full history.
	LoadedMessageSeqs []int64 `json:"-"`
	// SkillSnapshot caches the last skill binding used for this session.
	// Prepare may refresh and replace it when the skill registry fingerprint
	// or runtime-context fingerprint changes.
	SkillSnapshot skill.SessionSkillSnapshot `json:"skill_snapshot,omitempty"`
}

type Run struct {
	ID               string
	SystemPrompt     string
	Goal             string
	TargetSummary    string
	Model            string
	JobType          string
	DetectedDomains  []string
	AllowedSkills    []string
	MaxContextTokens int
	MaxOutputTokens  int
}

type ToolResult = resultmodel.ToolResult

type CompactReason string

const (
	CompactManual        CompactReason = "manual"
	CompactEmergency     CompactReason = "emergency"
	CompactPeriodic      CompactReason = "periodic"
	CompactAutoThreshold CompactReason = "auto_threshold"
)

type SegmentKind string

const (
	SegmentBaseSystem   SegmentKind = "base_system"
	SegmentRunSystem    SegmentKind = "run_system"
	SegmentSkillPrompt  SegmentKind = "skill_prompt"
	SegmentPinnedFacts  SegmentKind = "pinned_facts"
	SegmentSessionState SegmentKind = "session_state"
	SegmentRecalled     SegmentKind = "recalled_segments"
	SegmentSummary      SegmentKind = "summary"
	SegmentMessages     SegmentKind = "messages"
)

type ContextSegment struct {
	Kind         SegmentKind
	Tokens       int
	MessageCount int
	Note         string
}

type Budget struct {
	ContextWindow        int
	ReservedOutput       int
	MaxInputTokens       int
	EstimatedInputTokens int
	RemainingInputTokens int
}

type PreparedContext struct {
	SystemPrompt string
	Messages     []Message
	Skills       skill.SessionSkillSnapshot
	Segments     []ContextSegment
	Budget       Budget
	// SessionStatePrompt and RecalledContextPrompt expose the compact context
	// projections that were prepared for the model turn so downstream planners
	// can reuse the same semantic context instead of planning from a thinner view.
	SessionStatePrompt    string
	RecalledContextPrompt string
	// RetrievalReceipt explains which recalled hits were considered and which
	// ones were ultimately injected into the prepared prompt.
	RetrievalReceipt *RetrievalReceipt

	// NeedsCompaction is set to true when the context budget is exhausted
	// and the caller should compact before the next model call.
	NeedsCompaction bool
}

type ReceiptHit struct {
	Kind       string
	ID         string
	Score      float64
	Reason     string
	Tokens     int
	Injected   bool
	TrimReason string
}

type RetrievalReceipt struct {
	Queries     []string
	Hits        []ReceiptHit
	Injected    []ReceiptHit
	Trimmed     []ReceiptHit
	TotalTokens int
	GeneratedAt time.Time
}

type ContextReport struct {
	GeneratedAt        time.Time
	SystemPrompt       string
	Segments           []ContextSegment
	Budget             Budget
	SkillFingerprint   string
	EligibleSkillCount int
	BlockedSkillCount  int
	RetrievalReceipt   *RetrievalReceipt
}

type TokenEstimator interface {
	Estimate(text string) int
	EstimateMessages(msgs []Message) int
}

// Summarizer produces a concise summary of conversation messages.
// The model-based implementation calls an LLM; the fallback concatenates content.
type Summarizer interface {
	Summarize(ctx context.Context, messages []Message, maxChars int) (string, error)
}

// EmbeddingClient generates vector embeddings for semantic segment recall.
// It mirrors the contract used by the agent package to avoid an import cycle.
type EmbeddingClient interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type EpisodeWriter interface {
	CreateEpisode(ctx context.Context, sessionID string, reason string) (episodeID string, err error)
	SealEpisode(ctx context.Context, episodeID string, seqEnd int64) error
}

type EpisodeReader interface {
	ActiveEpisode(ctx context.Context, sessionID string) (episodeID string, err error)
	ListEpisodes(ctx context.Context, sessionID string) ([]EpisodeSummary, error)
}

type EpisodeSummary struct {
	ID           string
	SessionID    string
	SeqNum       int
	Status       string
	StartedAt    time.Time
	SealedAt     time.Time
	MessageCount int
}

type SegmentWriter interface {
	InsertSegment(ctx context.Context, seg SummarySegment) error
	UpdateParentSegmentID(ctx context.Context, segmentID string, parentSegmentID string) error
}

type SegmentReader interface {
	RecentSegments(ctx context.Context, sessionID string, level int, limit int) ([]SummarySegment, error)
	SegmentsByEpisode(ctx context.Context, episodeID string) ([]SummarySegment, error)
	UnparentedL1Segments(ctx context.Context, episodeID string, limit int) ([]SummarySegment, error)
}

type SegmentSearcher interface {
	// SearchSegments returns segments most relevant to the query. When
	// queryEmbedding is present it uses cosine similarity. When it is absent it
	// falls back to matching against the system-generated keywords column using
	// queryText.
	SearchSegments(ctx context.Context, sessionID string, queryText string, queryEmbedding []float32, limit int) ([]SummarySegment, error)
}

type SummarySegment struct {
	ID              string
	SessionID       string
	EpisodeID       string
	Level           int
	SeqStart        int64
	SeqEnd          int64
	TSStart         time.Time
	TSEnd           time.Time
	SummaryText     string
	Decisions       []string
	TODOs           []string
	Constraints     []string
	Entities        []string
	ArtifactRefs    []string
	Embedding       []float32
	Keywords        string
	QualityScore    float64
	ParentSegmentID string
	CreatedAt       time.Time
}

type StateWriter interface {
	UpsertState(ctx context.Context, sessionID string, entries []StateEntry) error
}

type StateReader interface {
	ActiveStates(ctx context.Context, sessionID string) ([]StateEntry, error)
}

type StateEntry struct {
	Key           string
	Category      string
	Value         string
	Status        string
	SourceEpisode string
	SourceSegment string
	SupersededBy  string
	Confidence    float64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ExpiresAt     time.Time
}

type ContextEngine interface {
	// Prepare assembles the prompt and message window for the next model call.
	// It may refresh session.SkillSnapshot to cache the current skill binding
	// for the supplied runtime context.
	Prepare(ctx context.Context, session *Session, run *Run, runtimeCtx skill.RuntimeContext) (*PreparedContext, Budget, error)
	AppendToolResults(ctx context.Context, session *Session, results []ToolResult) error
	Compact(ctx context.Context, session *Session, reason CompactReason) error
	Inspect(ctx context.Context, session *Session, run *Run, runtimeCtx skill.RuntimeContext) (*ContextReport, error)
}

type SkillBinder interface {
	Snapshot() skill.RegistrySnapshot
	BindSession(runtimeCtx skill.RuntimeContext) skill.SessionSkillSnapshot
}

// ---------------------------------------------------------------------------
// Pre-compaction memory flush
// ---------------------------------------------------------------------------

// MemoryWriter is a minimal interface for writing memory entries during
// pre-compaction flush. It is intentionally smaller than agent.MemoryStore
// to avoid import cycles between contextengine and agent.
type MemoryWriter interface {
	Set(ctx context.Context, key, value string) error
}

// PreCompactHook is called before compaction to flush important context
// from messages about to be discarded into durable storage.
// Returning an error is non-fatal: compaction proceeds regardless.
type PreCompactHook func(ctx context.Context, discarding []Message, session *Session) error

// PostCompactHook runs after compaction computes the next session state.
// It may inspect or mutate event.Session before the compaction result is
// committed. Returning an error aborts the compaction.
type PostCompactHook func(ctx context.Context, event CompactEvent) error

// CompactEvent records observability data about a compaction operation.
type CompactEvent struct {
	Session          *Session
	Reason           CompactReason
	DiscardedCount   int
	PreTokens        int
	PostTokens       int
	SummaryChars     int
	PinnedFactCount  int
	MemoryFlushCount int
	Duration         time.Duration
}

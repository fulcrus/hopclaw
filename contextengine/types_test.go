package contextengine

import (
	"testing"
)

func TestMessageTextContentPlainContent(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role:    RoleUser,
		Content: "hello world",
	}
	got := msg.TextContent()
	if got != "hello world" {
		t.Fatalf("TextContent() = %q, want \"hello world\"", got)
	}
}

func TestMessageTextContentFromBlocks(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role:    RoleUser,
		Content: "fallback",
		ContentBlocks: []ContentBlock{
			{Type: ContentBlockText, Text: "first"},
			{Type: ContentBlockImage, Data: "base64data"},
			{Type: ContentBlockText, Text: " second"},
		},
	}
	got := msg.TextContent()
	if got != "first second" {
		t.Fatalf("TextContent() = %q, want \"first second\"", got)
	}
}

func TestMessageTextContentEmptyBlocks(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role:          RoleUser,
		Content:       "fallback",
		ContentBlocks: []ContentBlock{},
	}
	// Empty ContentBlocks slice -> falls back to Content.
	got := msg.TextContent()
	if got != "fallback" {
		t.Fatalf("TextContent() = %q, want \"fallback\"", got)
	}
}

func TestMessageTextContentImageOnly(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role: RoleUser,
		ContentBlocks: []ContentBlock{
			{Type: ContentBlockImage, MediaType: "image/png", Data: "base64"},
		},
	}
	got := msg.TextContent()
	if got != "" {
		t.Fatalf("TextContent() = %q, want empty for image-only message", got)
	}
}

func TestMessageHasImageContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		blocks []ContentBlock
		want   bool
	}{
		{"no blocks", nil, false},
		{"text only", []ContentBlock{{Type: ContentBlockText, Text: "hi"}}, false},
		{"image present", []ContentBlock{
			{Type: ContentBlockText, Text: "look"},
			{Type: ContentBlockImage, Data: "base64"},
		}, true},
		{"image only", []ContentBlock{
			{Type: ContentBlockImage, Data: "base64"},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := Message{ContentBlocks: tt.blocks}
			got := msg.HasImageContent()
			if got != tt.want {
				t.Fatalf("HasImageContent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTextMessage(t *testing.T) {
	t.Parallel()

	msg := NewTextMessage(RoleUser, "hello")
	if msg.Role != RoleUser {
		t.Fatalf("Role = %q", msg.Role)
	}
	if msg.Content != "hello" {
		t.Fatalf("Content = %q", msg.Content)
	}
	if msg.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be set")
	}
	if len(msg.ContentBlocks) != 0 {
		t.Fatal("ContentBlocks should be empty for text message")
	}
}

func TestNewImageMessage(t *testing.T) {
	t.Parallel()

	msg := NewImageMessage(RoleUser, "describe this", "image/png", "base64data")
	if msg.Role != RoleUser {
		t.Fatalf("Role = %q", msg.Role)
	}
	if msg.Content != "describe this" {
		t.Fatalf("Content = %q", msg.Content)
	}
	if len(msg.ContentBlocks) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(msg.ContentBlocks))
	}
	if msg.ContentBlocks[0].Type != ContentBlockText {
		t.Fatalf("block[0].Type = %q", msg.ContentBlocks[0].Type)
	}
	if msg.ContentBlocks[0].Text != "describe this" {
		t.Fatalf("block[0].Text = %q", msg.ContentBlocks[0].Text)
	}
	if msg.ContentBlocks[1].Type != ContentBlockImage {
		t.Fatalf("block[1].Type = %q", msg.ContentBlocks[1].Type)
	}
	if msg.ContentBlocks[1].MediaType != "image/png" {
		t.Fatalf("block[1].MediaType = %q", msg.ContentBlocks[1].MediaType)
	}
	if msg.ContentBlocks[1].Data != "base64data" {
		t.Fatalf("block[1].Data = %q", msg.ContentBlocks[1].Data)
	}
	if msg.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be set")
	}
}

func TestNewImageMessageNoText(t *testing.T) {
	t.Parallel()

	msg := NewImageMessage(RoleUser, "", "image/jpeg", "imgdata")
	if len(msg.ContentBlocks) != 1 {
		t.Fatalf("expected 1 content block for no-text image, got %d", len(msg.ContentBlocks))
	}
	if msg.ContentBlocks[0].Type != ContentBlockImage {
		t.Fatalf("block[0].Type = %q", msg.ContentBlocks[0].Type)
	}
}

func TestToolResultFields(t *testing.T) {
	t.Parallel()

	result := ToolResult{
		ToolName:    "bash",
		ToolCallID:  "call-123",
		Content:     "output here",
		ArtifactURI: "artifact://results/1",
	}
	if result.ToolName != "bash" {
		t.Fatalf("ToolName = %q", result.ToolName)
	}
	if result.ToolCallID != "call-123" {
		t.Fatalf("ToolCallID = %q", result.ToolCallID)
	}
	if result.Content != "output here" {
		t.Fatalf("Content = %q", result.Content)
	}
	if result.ArtifactURI != "artifact://results/1" {
		t.Fatalf("ArtifactURI = %q", result.ArtifactURI)
	}
}

func TestToolCallRefFields(t *testing.T) {
	t.Parallel()

	ref := ToolCallRef{
		ID:        "tc-456",
		Name:      "read_file",
		Arguments: `{"path": "/tmp/test.txt"}`,
	}
	if ref.ID != "tc-456" {
		t.Fatalf("ID = %q", ref.ID)
	}
	if ref.Name != "read_file" {
		t.Fatalf("Name = %q", ref.Name)
	}
	if ref.Arguments != `{"path": "/tmp/test.txt"}` {
		t.Fatalf("Arguments = %q", ref.Arguments)
	}
}

func TestMessageRoleConstants(t *testing.T) {
	t.Parallel()

	if RoleSystem != "system" {
		t.Fatalf("RoleSystem = %q", RoleSystem)
	}
	if RoleUser != "user" {
		t.Fatalf("RoleUser = %q", RoleUser)
	}
	if RoleAssistant != "assistant" {
		t.Fatalf("RoleAssistant = %q", RoleAssistant)
	}
	if RoleTool != "tool" {
		t.Fatalf("RoleTool = %q", RoleTool)
	}
}

func TestSegmentKindConstants(t *testing.T) {
	t.Parallel()

	if SegmentBaseSystem != "base_system" {
		t.Fatalf("SegmentBaseSystem = %q", SegmentBaseSystem)
	}
	if SegmentRunSystem != "run_system" {
		t.Fatalf("SegmentRunSystem = %q", SegmentRunSystem)
	}
	if SegmentSkillPrompt != "skill_prompt" {
		t.Fatalf("SegmentSkillPrompt = %q", SegmentSkillPrompt)
	}
	if SegmentSummary != "summary" {
		t.Fatalf("SegmentSummary = %q", SegmentSummary)
	}
	if SegmentMessages != "messages" {
		t.Fatalf("SegmentMessages = %q", SegmentMessages)
	}
}

func TestCompactReasonConstants(t *testing.T) {
	t.Parallel()

	if CompactManual != "manual" {
		t.Fatalf("CompactManual = %q", CompactManual)
	}
	if CompactEmergency != "emergency" {
		t.Fatalf("CompactEmergency = %q", CompactEmergency)
	}
	if CompactPeriodic != "periodic" {
		t.Fatalf("CompactPeriodic = %q", CompactPeriodic)
	}
}

func TestBudgetFields(t *testing.T) {
	t.Parallel()

	budget := Budget{
		ContextWindow:        32000,
		ReservedOutput:       4000,
		MaxInputTokens:       24000,
		EstimatedInputTokens: 10000,
		RemainingInputTokens: 14000,
	}
	if budget.ContextWindow != 32000 {
		t.Fatalf("ContextWindow = %d", budget.ContextWindow)
	}
	if budget.RemainingInputTokens != budget.MaxInputTokens-budget.EstimatedInputTokens {
		t.Fatal("RemainingInputTokens should be MaxInput - Estimated")
	}
}

func TestContextSegmentFields(t *testing.T) {
	t.Parallel()

	seg := ContextSegment{
		Kind:         SegmentMessages,
		Tokens:       1500,
		MessageCount: 10,
		Note:         "main conversation",
	}
	if seg.Kind != SegmentMessages {
		t.Fatalf("Kind = %q", seg.Kind)
	}
	if seg.Tokens != 1500 {
		t.Fatalf("Tokens = %d", seg.Tokens)
	}
	if seg.MessageCount != 10 {
		t.Fatalf("MessageCount = %d", seg.MessageCount)
	}
}

func TestContentBlockTypes(t *testing.T) {
	t.Parallel()

	if ContentBlockText != "text" {
		t.Fatalf("ContentBlockText = %q", ContentBlockText)
	}
	if ContentBlockImage != "image" {
		t.Fatalf("ContentBlockImage = %q", ContentBlockImage)
	}
}

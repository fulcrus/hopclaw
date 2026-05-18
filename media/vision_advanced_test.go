package media

import (
	"context"
	"strings"
	"testing"
)

type promptCapturingImageProvider struct {
	mockImageProvider
	lastPrompt string
}

func (p *promptCapturingImageProvider) DescribeImage(ctx context.Context, req ImageRequest) (*ImageResult, error) {
	p.lastPrompt = req.Prompt
	return p.mockImageProvider.DescribeImage(ctx, req)
}

func TestVisionAnalyzerDefaultsStructuredModesToJSON(t *testing.T) {
	t.Parallel()

	provider := &promptCapturingImageProvider{
		mockImageProvider: mockImageProvider{
			id:    "test-image",
			text:  `{"layout":"dashboard","elements":[],"navigation":"sidebar","visual_hierarchy":"header first"}`,
			model: "vision-v1",
		},
	}
	registry := NewRegistry()
	registry.Register(provider)

	analyzer := NewVisionAnalyzer(registry)
	result, err := analyzer.AnalyzeImage(context.Background(), ImageAnalysisRequest{
		Data: []byte{0x89, 0x50, 0x4E, 0x47},
		Mode: ModeAnalyzeUI,
	})
	if err != nil {
		t.Fatalf("AnalyzeImage() error = %v", err)
	}
	if !strings.Contains(provider.lastPrompt, "valid JSON") {
		t.Fatalf("expected structured mode prompt to default to JSON, got %q", provider.lastPrompt)
	}
	if result.Structured == nil {
		t.Fatalf("expected structured JSON to be parsed, got %#v", result)
	}
}

func TestVisionAnalyzerHonorsMarkdownFormatForStructuredMode(t *testing.T) {
	t.Parallel()

	provider := &promptCapturingImageProvider{
		mockImageProvider: mockImageProvider{
			id:    "test-image",
			text:  "## Layout\n- Header\n\n## Interactive elements\n- Save button",
			model: "vision-v1",
		},
	}
	registry := NewRegistry()
	registry.Register(provider)

	analyzer := NewVisionAnalyzer(registry)
	result, err := analyzer.AnalyzeImage(context.Background(), ImageAnalysisRequest{
		Data:   []byte{0x89, 0x50, 0x4E, 0x47},
		Mode:   ModeAnalyzeUI,
		Format: FormatMarkdown,
	})
	if err != nil {
		t.Fatalf("AnalyzeImage() error = %v", err)
	}
	if strings.Contains(provider.lastPrompt, "valid JSON") {
		t.Fatalf("expected markdown prompt without JSON-only instruction, got %q", provider.lastPrompt)
	}
	if strings.Contains(provider.lastPrompt, "Format your response as JSON") {
		t.Fatalf("expected markdown prompt without hardcoded JSON instruction, got %q", provider.lastPrompt)
	}
	if !strings.Contains(provider.lastPrompt, "Markdown") {
		t.Fatalf("expected markdown instruction in prompt, got %q", provider.lastPrompt)
	}
	if result.Structured != nil {
		t.Fatalf("expected no structured JSON parse for markdown output, got %#v", result.Structured)
	}
}

func TestBuildPromptStructuredModesHonorRequestedNonJSONFormats(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		mode         AnalysisMode
		format       OutputFormat
		wantContains string
	}{
		{name: "identify objects markdown", mode: ModeIdentifyObjects, format: FormatMarkdown, wantContains: "Markdown list"},
		{name: "identify objects plain", mode: ModeIdentifyObjects, format: FormatPlainText, wantContains: "plain-text list"},
		{name: "analyze ui markdown", mode: ModeAnalyzeUI, format: FormatMarkdown, wantContains: "Format the response in Markdown"},
		{name: "analyze ui plain", mode: ModeAnalyzeUI, format: FormatPlainText, wantContains: "Return plain text"},
		{name: "extract data markdown", mode: ModeExtractData, format: FormatMarkdown, wantContains: "Format the response in Markdown"},
		{name: "extract data plain", mode: ModeExtractData, format: FormatPlainText, wantContains: "Return plain text"},
		{name: "analyze document markdown", mode: ModeAnalyzeDocument, format: FormatMarkdown, wantContains: "Format the response in Markdown"},
		{name: "analyze document plain", mode: ModeAnalyzeDocument, format: FormatPlainText, wantContains: "Return plain text"},
		{name: "describe diagram markdown", mode: ModeDescribeDiagram, format: FormatMarkdown, wantContains: "Format the response in Markdown"},
		{name: "describe diagram plain", mode: ModeDescribeDiagram, format: FormatPlainText, wantContains: "Return plain text"},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prompt := BuildPrompt(tt.mode, DetailMedium, tt.format, "")
			for _, disallowed := range []string{
				"Format your response as JSON",
				"Format as JSON",
				"Return the data as JSON",
				"valid JSON only",
			} {
				if strings.Contains(prompt, disallowed) {
					t.Fatalf("BuildPrompt(%q, %q) unexpectedly contained %q:\n%s", tt.mode, tt.format, disallowed, prompt)
				}
			}
			if !strings.Contains(prompt, tt.wantContains) {
				t.Fatalf("BuildPrompt(%q, %q) missing %q:\n%s", tt.mode, tt.format, tt.wantContains, prompt)
			}
		})
	}
}

func TestVisionAnalyzerParsesRequestedJSONForDescribeMode(t *testing.T) {
	t.Parallel()

	provider := &promptCapturingImageProvider{
		mockImageProvider: mockImageProvider{
			id:    "test-image",
			text:  `{"summary":"A cat sleeping on a chair."}`,
			model: "vision-v1",
		},
	}
	registry := NewRegistry()
	registry.Register(provider)

	analyzer := NewVisionAnalyzer(registry)
	result, err := analyzer.AnalyzeImage(context.Background(), ImageAnalysisRequest{
		Data:   []byte{0x89, 0x50, 0x4E, 0x47},
		Mode:   ModeDescribe,
		Format: FormatJSON,
	})
	if err != nil {
		t.Fatalf("AnalyzeImage() error = %v", err)
	}
	if !strings.Contains(provider.lastPrompt, `{"summary": "..."}`) {
		t.Fatalf("expected describe-mode JSON instruction, got %q", provider.lastPrompt)
	}
	if result.Structured == nil {
		t.Fatalf("expected requested JSON output to be parsed, got %#v", result)
	}
}

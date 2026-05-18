package media

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Advanced vision analysis constants
// ---------------------------------------------------------------------------

const (
	// defaultAnalysisDetail is the default detail level for analysis requests.
	defaultAnalysisDetail = DetailMedium

	// defaultAnalysisFormat is the default output format for analysis results.
	defaultAnalysisFormat = FormatPlainText
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	// ErrInvalidAnalysisMode is returned when an unsupported analysis mode is requested.
	ErrInvalidAnalysisMode = fmt.Errorf("invalid analysis mode")

	// ErrNoImageData is returned when an analysis request contains no image data.
	ErrNoImageData = fmt.Errorf("no image data provided")
)

// ---------------------------------------------------------------------------
// Request and result types
// ---------------------------------------------------------------------------

// ImageAnalysisRequest describes what analysis to perform on an image.
type ImageAnalysisRequest struct {
	// Data holds the raw image bytes.
	Data []byte `json:"-"`
	// MIMEType is the MIME type of the image (e.g., "image/png").
	MIMEType string `json:"mime_type,omitempty"`
	// Mode selects the analysis type (describe, extract_text, analyze_ui, etc.).
	Mode AnalysisMode `json:"mode"`
	// Detail controls the verbosity of the output.
	Detail DetailLevel `json:"detail,omitempty"`
	// Format controls the output format (plain, markdown, json).
	Format OutputFormat `json:"format,omitempty"`
	// CustomPrompt overrides the built-in prompt for the selected mode.
	CustomPrompt string `json:"custom_prompt,omitempty"`
	// DocumentType provides a hint for ModeAnalyzeDocument.
	DocumentType DocumentType `json:"document_type,omitempty"`
	// LanguageHint provides a language hint for OCR / text extraction.
	LanguageHint string `json:"language_hint,omitempty"`
	// PreferredProvider selects a specific provider by ID (empty = first available).
	PreferredProvider string `json:"preferred_provider,omitempty"`
}

// ImageAnalysisResult holds the output from advanced image analysis.
type ImageAnalysisResult struct {
	// Mode is the analysis mode that was used.
	Mode AnalysisMode `json:"mode"`
	// Text is the raw text response from the model.
	Text string `json:"text"`
	// Structured holds parsed JSON output when JSON output was requested and
	// parsing succeeds. nil when parsing is not applicable or fails.
	Structured any `json:"structured,omitempty"`
	// Provider is the ID of the provider that performed the analysis.
	Provider string `json:"provider"`
	// Model is the model identifier used by the provider.
	Model string `json:"model,omitempty"`
	// DurationMs is the wall-clock duration in milliseconds.
	DurationMs int64 `json:"duration_ms"`
}

// ---------------------------------------------------------------------------
// VisionAnalyzer
// ---------------------------------------------------------------------------

// VisionAnalyzer provides advanced image understanding using registered
// ImageProviders. It builds mode-specific prompts and parses structured
// output from model responses.
type VisionAnalyzer struct {
	registry *Registry
}

// NewVisionAnalyzer creates a VisionAnalyzer backed by the given registry.
func NewVisionAnalyzer(registry *Registry) *VisionAnalyzer {
	return &VisionAnalyzer{registry: registry}
}

// ---------------------------------------------------------------------------
// AnalyzeImage
// ---------------------------------------------------------------------------

// AnalyzeImage performs advanced image analysis using the configured mode.
// It selects the appropriate prompt, sends the image to the first available
// (or preferred) ImageProvider, and optionally parses structured output.
func (v *VisionAnalyzer) AnalyzeImage(ctx context.Context, req ImageAnalysisRequest) (*ImageAnalysisResult, error) {
	if len(req.Data) == 0 {
		return nil, fmt.Errorf("media/vision: %w", ErrNoImageData)
	}
	if req.Mode == "" {
		req.Mode = ModeDescribe
	}
	if !IsValidMode(req.Mode) {
		return nil, fmt.Errorf("media/vision: %w: %q", ErrInvalidAnalysisMode, req.Mode)
	}
	if req.Detail == "" {
		req.Detail = defaultAnalysisDetail
	}
	req.Format = normalizeAnalysisFormat(req.Mode, req.Format)

	// Find a suitable provider.
	provider, err := v.registry.FindImageProvider(req.PreferredProvider)
	if err != nil {
		return nil, fmt.Errorf("media/vision: %w", err)
	}

	// Build the prompt.
	prompt := v.buildPrompt(req, provider.ID())

	// Detect MIME type if not provided.
	mimeType := req.MIMEType
	if mimeType == "" {
		mimeType = DetectMIMETypeFromBytes(req.Data)
	}

	start := time.Now()

	imgResult, err := provider.DescribeImage(ctx, ImageRequest{
		Data:     req.Data,
		MIMEType: mimeType,
		Prompt:   prompt,
	})
	if err != nil {
		return nil, fmt.Errorf("media/vision: analysis failed: %w", err)
	}

	result := &ImageAnalysisResult{
		Mode:       req.Mode,
		Text:       imgResult.Text,
		Provider:   provider.ID(),
		Model:      imgResult.Model,
		DurationMs: time.Since(start).Milliseconds(),
	}

	// Attempt to parse structured output for modes that produce JSON.
	if req.Format == FormatJSON {
		result.Structured = parseStructuredOutput(imgResult.Text)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Prompt building
// ---------------------------------------------------------------------------

// buildPrompt constructs the analysis prompt from the request parameters.
func (v *VisionAnalyzer) buildPrompt(req ImageAnalysisRequest, providerID string) string {
	// Use custom prompt if provided.
	if req.CustomPrompt != "" {
		return BuildCustomPrompt(req.CustomPrompt, req.Format)
	}

	// Build mode-specific prompt.
	switch req.Mode {
	case ModeAnalyzeDocument:
		return BuildDocumentPrompt(req.DocumentType, req.Detail, req.Format, providerID)
	default:
		prompt := BuildPrompt(req.Mode, req.Detail, req.Format, providerID)
		if req.LanguageHint != "" && req.Mode == ModeExtractText {
			prompt = fmt.Sprintf("The text is expected to be in %s. %s", req.LanguageHint, prompt)
		}
		return prompt
	}
}

func normalizeAnalysisFormat(mode AnalysisMode, format OutputFormat) OutputFormat {
	if format != "" {
		return format
	}
	if modeDefaultProducesStructuredOutput(mode) {
		return FormatJSON
	}
	return defaultAnalysisFormat
}

func modeDefaultProducesStructuredOutput(mode AnalysisMode) bool {
	switch mode {
	case ModeIdentifyObjects, ModeAnalyzeUI, ModeExtractData, ModeAnalyzeDocument, ModeDescribeDiagram:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Structured output parsing
// ---------------------------------------------------------------------------

// parseStructuredOutput attempts to extract a JSON value from the model's
// text response. It handles responses that wrap JSON in markdown code fences.
// Returns nil if no valid JSON is found.
func parseStructuredOutput(text string) any {
	cleaned := extractJSONBlock(text)

	// Try parsing as a JSON object.
	var obj map[string]any
	if err := json.Unmarshal([]byte(cleaned), &obj); err == nil {
		return obj
	}

	// Try parsing as a JSON array.
	var arr []any
	if err := json.Unmarshal([]byte(cleaned), &arr); err == nil {
		return arr
	}

	return nil
}

// extractJSONBlock extracts JSON from a response that may be wrapped in
// markdown code fences (```json ... ```).
func extractJSONBlock(text string) string {
	// Check for ```json ... ``` fences.
	if idx := strings.Index(text, "```json"); idx >= 0 {
		start := idx + len("```json")
		if end := strings.Index(text[start:], "```"); end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	// Check for plain ``` ... ``` fences.
	if idx := strings.Index(text, "```"); idx >= 0 {
		start := idx + len("```")
		// Skip any language identifier on the same line.
		if nl := strings.IndexByte(text[start:], '\n'); nl >= 0 {
			start += nl + 1
		}
		if end := strings.Index(text[start:], "```"); end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	// No fences found; return the raw text trimmed.
	return strings.TrimSpace(text)
}

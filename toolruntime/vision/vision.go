// Package vision implements image and vision analysis tool handlers
// (vision.describe, vision.extract_text, vision.analyze_ui, vision.compare,
// vision.extract_table, vision.read_document, vision.describe_diagram) for
// the toolruntime registry.
package vision

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/media"
	"github.com/fulcrus/hopclaw/skill"
)

// Runtime is the narrow interface that vision handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	// VisionAnalyzer returns a VisionAnalyzer, or an error if not configured.
	VisionAnalyzer() (*media.VisionAnalyzer, error)
	// ImageComparer returns an ImageComparer, or an error if not configured.
	ImageComparer() (*media.ImageComparer, error)
}

// Handler is the tool handler signature for vision tools.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with a vision handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	visionHTTPTimeout        = 30 * time.Second
	visionMaxDownloadBytes   = 20 * 1024 * 1024
	visionDefaultDetailLevel = "medium"
	visionDefaultDocType     = "general"
)

// ToolDefs returns all vision domain tool definitions.
func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "vision.describe",
				Description:     "Describe an image from a file path or URL.",
				InputSchema:     visionDescribeInputSchema(),
				OutputSchema:    visionGenericOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "vision:describe",
			},
			Handler: handleVisionDescribe,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "vision.extract_text",
				Description:     "Extract text (OCR) from an image file or URL.",
				InputSchema:     visionExtractTextInputSchema(),
				OutputSchema:    visionGenericOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "vision:extract_text",
			},
			Handler: handleVisionExtractText,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "vision.analyze_ui",
				Description:     "Analyze a UI screenshot to identify interactive elements and layout.",
				InputSchema:     visionAnalyzeUIInputSchema(),
				OutputSchema:    visionGenericOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "vision:analyze_ui",
			},
			Handler: handleVisionAnalyzeUI,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "vision.compare",
				Description:     "Compare two images and describe similarities and differences.",
				InputSchema:     visionCompareInputSchema(),
				OutputSchema:    visionCompareOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "vision:compare",
			},
			Handler: handleVisionCompare,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "vision.extract_table",
				Description:     "Extract table data from an image as structured JSON.",
				InputSchema:     visionExtractTableInputSchema(),
				OutputSchema:    visionGenericOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "vision:extract_table",
			},
			Handler: handleVisionExtractTable,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "vision.read_document",
				Description:     "Understand a document image and extract structured fields.",
				InputSchema:     visionReadDocumentInputSchema(),
				OutputSchema:    visionGenericOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "vision:read_document",
			},
			Handler: handleVisionReadDocument,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "vision.describe_diagram",
				Description:     "Interpret a technical diagram or flowchart.",
				InputSchema:     visionDescribeDiagramInputSchema(),
				OutputSchema:    visionGenericOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "vision:describe_diagram",
			},
			Handler: handleVisionDescribeDiagram,
		},
	}
}

// ---------------------------------------------------------------------------
// Param helpers
// ---------------------------------------------------------------------------

func stringFrom(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	default:
		return "", fmt.Errorf("expected string, got %T", value)
	}
}

func stringFromDefault(value any, fallback string) string {
	s, err := stringFrom(value)
	if err != nil || strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleVisionDescribe(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	analyzer, err := rt.VisionAnalyzer()
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	imgData, mimeType, err := loadImageFromCall(call)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	detail := stringFromDefault(call.Input["detail_level"], visionDefaultDetailLevel)
	result, err := analyzer.AnalyzeImage(ctx, media.ImageAnalysisRequest{
		Data:     imgData,
		MIMEType: mimeType,
		Mode:     media.ModeDescribe,
		Detail:   media.DetailLevel(detail),
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return visionResultToToolResult(call, result)
}

func handleVisionExtractText(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	analyzer, err := rt.VisionAnalyzer()
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	imgData, mimeType, err := loadImageFromCall(call)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	langHint, _ := stringFrom(call.Input["language_hint"])
	result, err := analyzer.AnalyzeImage(ctx, media.ImageAnalysisRequest{
		Data:         imgData,
		MIMEType:     mimeType,
		Mode:         media.ModeExtractText,
		Detail:       media.DetailHigh,
		LanguageHint: langHint,
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return visionResultToToolResult(call, result)
}

func handleVisionAnalyzeUI(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	analyzer, err := rt.VisionAnalyzer()
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	imgData, mimeType, err := loadImageFromCall(call)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	result, err := analyzer.AnalyzeImage(ctx, media.ImageAnalysisRequest{
		Data:     imgData,
		MIMEType: mimeType,
		Mode:     media.ModeAnalyzeUI,
		Detail:   media.DetailHigh,
		Format:   media.FormatJSON,
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return visionResultToToolResult(call, result)
}

func handleVisionCompare(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	comparer, err := rt.ImageComparer()
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	img1Data, img1MIME, err := loadImageFromArg(call.Input["image1_path"], call.Input["image1_url"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("image 1: %w", err)
	}
	img2Data, img2MIME, err := loadImageFromArg(call.Input["image2_path"], call.Input["image2_url"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("image 2: %w", err)
	}
	images := []media.ImageInput{
		{Data: img1Data, MIMEType: img1MIME, Label: "Image 1"},
		{Data: img2Data, MIMEType: img2MIME, Label: "Image 2"},
	}
	result, err := comparer.CompareImages(ctx, images, media.CompareVisualDiff, media.DetailMedium)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return rt.JSONResult(call, map[string]any{
		"similarities": result.Similarities,
		"differences":  result.Differences,
		"summary":      result.Summary,
		"provider":     result.Provider,
		"model":        result.Model,
		"duration_ms":  result.DurationMs,
	})
}

func handleVisionExtractTable(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	analyzer, err := rt.VisionAnalyzer()
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	imgData, mimeType, err := loadImageFromCall(call)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	result, err := analyzer.AnalyzeImage(ctx, media.ImageAnalysisRequest{
		Data:     imgData,
		MIMEType: mimeType,
		Mode:     media.ModeExtractData,
		Detail:   media.DetailHigh,
		Format:   media.FormatJSON,
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return visionResultToToolResult(call, result)
}

func handleVisionReadDocument(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	analyzer, err := rt.VisionAnalyzer()
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	imgData, mimeType, err := loadImageFromCall(call)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	docType := stringFromDefault(call.Input["document_type"], visionDefaultDocType)
	result, err := analyzer.AnalyzeImage(ctx, media.ImageAnalysisRequest{
		Data:         imgData,
		MIMEType:     mimeType,
		Mode:         media.ModeAnalyzeDocument,
		Detail:       media.DetailHigh,
		Format:       media.FormatJSON,
		DocumentType: media.DocumentType(docType),
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return visionResultToToolResult(call, result)
}

func handleVisionDescribeDiagram(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	analyzer, err := rt.VisionAnalyzer()
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	imgData, mimeType, err := loadImageFromCall(call)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	result, err := analyzer.AnalyzeImage(ctx, media.ImageAnalysisRequest{
		Data:     imgData,
		MIMEType: mimeType,
		Mode:     media.ModeDescribeDiagram,
		Detail:   media.DetailHigh,
		Format:   media.FormatJSON,
	})
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return visionResultToToolResult(call, result)
}

// ---------------------------------------------------------------------------
// Image loading helpers
// ---------------------------------------------------------------------------

func loadImageFromCall(call agent.ToolCall) ([]byte, string, error) {
	return loadImageFromArg(call.Input["path"], call.Input["url"])
}

func loadImageFromArg(pathArg, urlArg any) ([]byte, string, error) {
	path, _ := stringFrom(pathArg)
	url, _ := stringFrom(urlArg)
	if path == "" && url == "" {
		return nil, "", fmt.Errorf("either path or url is required")
	}
	if path != "" {
		return loadImageFromPath(path)
	}
	return loadImageFromURL(url)
}

func loadImageFromPath(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("reading image file: %w", err)
	}
	mimeType := media.DetectMIMEType(path)
	if mimeType == "application/octet-stream" && len(data) > 0 {
		mimeType = media.DetectMIMETypeFromBytes(data)
	}
	return data, mimeType, nil
}

func loadImageFromURL(rawURL string) ([]byte, string, error) {
	client := &http.Client{Timeout: visionHTTPTimeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("downloading image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("downloading image: unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(visionMaxDownloadBytes)+1))
	if err != nil {
		return nil, "", fmt.Errorf("reading image response: %w", err)
	}
	if len(data) > visionMaxDownloadBytes {
		return nil, "", fmt.Errorf("image exceeds maximum download size of %d bytes", visionMaxDownloadBytes)
	}
	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = media.DetectMIMETypeFromBytes(data)
	}
	return data, mimeType, nil
}

// ---------------------------------------------------------------------------
// Result helpers
// ---------------------------------------------------------------------------

func visionResultToToolResult(call agent.ToolCall, result *media.ImageAnalysisResult) (contextengine.ToolResult, error) {
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    result.Text,
	}, nil
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func visionDescribeInputSchema() skill.JSONSchema {
	return skill.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"path":         map[string]any{"type": "string", "description": "Local file path to the image."},
			"url":          map[string]any{"type": "string", "description": "URL of the image to analyze."},
			"detail_level": map[string]any{"type": "string", "enum": []any{"low", "medium", "high"}, "description": "Level of detail in the description.", "default": "medium"},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"path"}},
			map[string]any{"required": []any{"url"}},
		},
	}
}

func visionExtractTextInputSchema() skill.JSONSchema {
	return skill.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"path":          map[string]any{"type": "string", "description": "Local file path to the image."},
			"url":           map[string]any{"type": "string", "description": "URL of the image to analyze."},
			"language_hint": map[string]any{"type": "string", "description": "Expected language of the text (e.g., 'en', 'zh', 'ja')."},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"path"}},
			map[string]any{"required": []any{"url"}},
		},
	}
}

func visionAnalyzeUIInputSchema() skill.JSONSchema {
	return skill.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Local file path to the UI screenshot."},
			"url":  map[string]any{"type": "string", "description": "URL of the UI screenshot."},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"path"}},
			map[string]any{"required": []any{"url"}},
		},
	}
}

func visionCompareInputSchema() skill.JSONSchema {
	return skill.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"image1_path": map[string]any{"type": "string", "description": "Local file path to the first image."},
			"image1_url":  map[string]any{"type": "string", "description": "URL of the first image."},
			"image2_path": map[string]any{"type": "string", "description": "Local file path to the second image."},
			"image2_url":  map[string]any{"type": "string", "description": "URL of the second image."},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"image1_path", "image2_path"}},
			map[string]any{"required": []any{"image1_url", "image2_url"}},
			map[string]any{"required": []any{"image1_path", "image2_url"}},
			map[string]any{"required": []any{"image1_url", "image2_path"}},
		},
	}
}

func visionExtractTableInputSchema() skill.JSONSchema {
	return skill.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Local file path to the image containing a table."},
			"url":  map[string]any{"type": "string", "description": "URL of the image containing a table."},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"path"}},
			map[string]any{"required": []any{"url"}},
		},
	}
}

func visionReadDocumentInputSchema() skill.JSONSchema {
	return skill.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"path":          map[string]any{"type": "string", "description": "Local file path to the document image."},
			"url":           map[string]any{"type": "string", "description": "URL of the document image."},
			"document_type": map[string]any{"type": "string", "enum": []any{"invoice", "receipt", "letter", "form", "general"}, "description": "Type of document for targeted extraction.", "default": "general"},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"path"}},
			map[string]any{"required": []any{"url"}},
		},
	}
}

func visionDescribeDiagramInputSchema() skill.JSONSchema {
	return skill.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Local file path to the diagram image."},
			"url":  map[string]any{"type": "string", "description": "URL of the diagram image."},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"path"}},
			map[string]any{"required": []any{"url"}},
		},
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func visionGenericOutputSchema() skill.JSONSchema {
	return skill.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string", "description": "The analysis result text."},
		},
	}
}

func visionCompareOutputSchema() skill.JSONSchema {
	return skill.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"similarities": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "What the images have in common."},
			"differences":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "What differs between the images."},
			"summary":      map[string]any{"type": "string", "description": "Brief overall comparison summary."},
			"provider":     map[string]any{"type": "string", "description": "Provider that performed the analysis."},
			"model":        map[string]any{"type": "string", "description": "Model used for the analysis."},
			"duration_ms":  map[string]any{"type": "integer", "description": "Analysis duration in milliseconds."},
		},
	}
}

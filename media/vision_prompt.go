package media

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Analysis mode enumeration
// ---------------------------------------------------------------------------

// AnalysisMode defines a specific type of image analysis.
type AnalysisMode string

const (
	// ModeDescribe performs general image description.
	ModeDescribe AnalysisMode = "describe"
	// ModeExtractText performs OCR-focused text extraction.
	ModeExtractText AnalysisMode = "extract_text"
	// ModeIdentifyObjects lists objects with approximate locations.
	ModeIdentifyObjects AnalysisMode = "identify_objects"
	// ModeAnalyzeUI analyzes UI screenshots for interactive elements.
	ModeAnalyzeUI AnalysisMode = "analyze_ui"
	// ModeCompare compares two images and describes differences.
	ModeCompare AnalysisMode = "compare"
	// ModeExtractData extracts structured data (tables, forms, charts).
	ModeExtractData AnalysisMode = "extract_data"
	// ModeAnalyzeDocument understands document images (invoices, receipts, letters).
	ModeAnalyzeDocument AnalysisMode = "analyze_document"
	// ModeDescribeDiagram interprets technical diagrams and flowcharts.
	ModeDescribeDiagram AnalysisMode = "describe_diagram"
)

// validModes is the set of supported analysis modes.
var validModes = map[AnalysisMode]bool{
	ModeDescribe:        true,
	ModeExtractText:     true,
	ModeIdentifyObjects: true,
	ModeAnalyzeUI:       true,
	ModeCompare:         true,
	ModeExtractData:     true,
	ModeAnalyzeDocument: true,
	ModeDescribeDiagram: true,
}

// IsValidMode returns true if the mode is a supported analysis mode.
func IsValidMode(mode AnalysisMode) bool {
	return validModes[mode]
}

// ---------------------------------------------------------------------------
// Detail level enumeration
// ---------------------------------------------------------------------------

// DetailLevel controls the verbosity of analysis output.
type DetailLevel string

const (
	// DetailLow produces a brief, high-level summary.
	DetailLow DetailLevel = "low"
	// DetailMedium produces a balanced description with key details.
	DetailMedium DetailLevel = "medium"
	// DetailHigh produces a thorough, exhaustive description.
	DetailHigh DetailLevel = "high"
)

// ---------------------------------------------------------------------------
// Output format enumeration
// ---------------------------------------------------------------------------

// OutputFormat controls the format of analysis results.
type OutputFormat string

const (
	// FormatPlainText returns plain text output.
	FormatPlainText OutputFormat = "plain"
	// FormatMarkdown returns markdown-formatted output.
	FormatMarkdown OutputFormat = "markdown"
	// FormatJSON returns JSON-structured output.
	FormatJSON OutputFormat = "json"
)

// ---------------------------------------------------------------------------
// Document type enumeration
// ---------------------------------------------------------------------------

// DocumentType classifies a document for targeted extraction.
type DocumentType string

const (
	DocTypeInvoice DocumentType = "invoice"
	DocTypeReceipt DocumentType = "receipt"
	DocTypeLetter  DocumentType = "letter"
	DocTypeForm    DocumentType = "form"
	DocTypeGeneral DocumentType = "general"
)

// ---------------------------------------------------------------------------
// Comparison type enumeration
// ---------------------------------------------------------------------------

// ComparisonType classifies the kind of multi-image comparison.
type ComparisonType string

const (
	CompareVisualDiff  ComparisonType = "visual_diff"
	CompareContentDiff ComparisonType = "content_diff"
	CompareLayoutDiff  ComparisonType = "layout_diff"
)

// ---------------------------------------------------------------------------
// Prompt template constants
// ---------------------------------------------------------------------------

const (
	promptDetailLow    = "Be concise — 1-2 sentences."
	promptDetailMedium = "Include key details in 3-5 sentences."
	promptDetailHigh   = "Provide a thorough, exhaustive description covering every visible detail."
)

// ---------------------------------------------------------------------------
// Prompt templates per analysis mode
// ---------------------------------------------------------------------------

var modePromptTemplates = map[AnalysisMode]string{
	ModeDescribe: `Describe this image in detail. %s`,

	ModeExtractText: `Extract all visible text from this image. Preserve the original layout and formatting as much as possible. If text appears in columns, tables, or structured layouts, maintain that structure. Include any text that appears on buttons, labels, signs, or other UI elements.%s

Return ONLY the extracted text, no commentary.`,

	ModeIdentifyObjects: `Identify all distinct objects visible in this image. For each object, provide:
- Object name/type
- Approximate location (e.g., top-left, center, bottom-right)
- Size relative to the image (small, medium, large)
- Any notable attributes (color, state, orientation)

	%s`,

	ModeAnalyzeUI: `Analyze this UI screenshot in detail. Identify:
1. **Layout structure**: overall page/screen layout, sections, navigation
2. **Interactive elements**: buttons, links, text fields, dropdowns, checkboxes, toggles
3. **Text content**: headings, labels, body text, placeholder text
4. **Visual hierarchy**: what draws attention first, grouping of elements
5. **State indicators**: active/inactive states, selected items, error states, loading states

	%s`,

	ModeCompare: `Compare these images carefully and describe:
1. **Similarities**: What elements, content, or structure do they share?
2. **Differences**: What has changed, been added, or removed?
3. **Summary**: A brief overall assessment of the comparison.

%s`,

	ModeExtractData: `Extract all structured data from this image. Look for:
- Tables (preserve row/column structure)
- Forms (field labels and values)
- Charts/graphs (data points and labels)
- Lists (items and any associated values)

	%s`,

	ModeAnalyzeDocument: `Analyze this document image. Extract:
1. **Document type**: What kind of document is this?
2. **Key fields**: All important fields and their values
3. **Dates**: Any dates mentioned
4. **Amounts**: Any monetary amounts or quantities
5. **Parties**: Names, addresses, organizations mentioned
6. **Summary**: Brief summary of the document's purpose

	%s`,

	ModeDescribeDiagram: `Interpret this technical diagram or flowchart. Describe:
1. **Diagram type**: flowchart, architecture diagram, sequence diagram, ER diagram, network diagram, etc.
2. **Entities/nodes**: All boxes, shapes, or labeled elements
3. **Relationships**: Connections, arrows, data flows between entities
4. **Flow direction**: The overall direction or sequence of the diagram
5. **Legend/annotations**: Any legends, keys, or annotations present

	%s`,
}

// ---------------------------------------------------------------------------
// Provider-specific prompt adjustments
// ---------------------------------------------------------------------------

const (
	providerHintClaude = "\nNote: Be precise with spatial descriptions and structured output."
	providerHintGPT    = "\nNote: Use your vision capabilities to examine every detail carefully."
	providerHintGemini = "\nNote: Leverage your multimodal understanding for accurate analysis."
)

// providerHint returns a provider-specific prompt suffix, or empty string.
func providerHint(providerID string) string {
	switch providerID {
	case "anthropic":
		return providerHintClaude
	case "openai":
		return providerHintGPT
	case "google":
		return providerHintGemini
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Prompt construction
// ---------------------------------------------------------------------------

// BuildPrompt constructs an optimized prompt for the given analysis mode,
// detail level, and optional provider hint.
func BuildPrompt(mode AnalysisMode, detail DetailLevel, format OutputFormat, providerID string) string {
	template, ok := modePromptTemplates[mode]
	if !ok {
		template = modePromptTemplates[ModeDescribe]
	}

	detailInstruction := detailPrompt(detail)
	hint := providerHint(providerID)
	outputInstruction := modeOutputInstruction(mode, format)

	prompt := fmt.Sprintf(template, detailInstruction)
	if outputInstruction != "" {
		prompt += "\n\n" + outputInstruction
	}
	if hint != "" {
		prompt += hint
	}
	return prompt
}

// BuildDocumentPrompt constructs a prompt for document analysis with a
// specific document type hint.
func BuildDocumentPrompt(docType DocumentType, detail DetailLevel, format OutputFormat, providerID string) string {
	base := BuildPrompt(ModeAnalyzeDocument, detail, format, providerID)
	if docType != "" && docType != DocTypeGeneral {
		return fmt.Sprintf("This document appears to be a %s. %s", string(docType), base)
	}
	return base
}

// BuildComparisonPrompt constructs a prompt for comparing multiple images.
func BuildComparisonPrompt(labels []string, compType ComparisonType, detail DetailLevel, format OutputFormat, providerID string) string {
	base := BuildPrompt(ModeCompare, detail, format, providerID)

	var prefix string
	if len(labels) > 0 {
		prefix = fmt.Sprintf("The images are labeled: %s. ", strings.Join(labels, ", "))
	}

	var focus string
	switch compType {
	case CompareVisualDiff:
		focus = "Focus on visual differences: colors, shapes, positions, sizes."
	case CompareContentDiff:
		focus = "Focus on content differences: text changes, added/removed elements, data changes."
	case CompareLayoutDiff:
		focus = "Focus on layout differences: positioning, alignment, spacing, structural changes."
	default:
		focus = "Analyze both visual and content differences."
	}

	return prefix + base + "\n" + focus
}

// detailPrompt returns the detail-level instruction string.
func detailPrompt(detail DetailLevel) string {
	switch detail {
	case DetailLow:
		return promptDetailLow
	case DetailHigh:
		return promptDetailHigh
	default:
		return promptDetailMedium
	}
}

func modeOutputInstruction(mode AnalysisMode, format OutputFormat) string {
	switch mode {
	case ModeExtractText:
		switch format {
		case FormatJSON:
			return `Return valid JSON only in the form {"text": "..."} with the extracted text preserved as faithfully as possible.`
		case FormatMarkdown:
			return "Return ONLY the extracted text using Markdown. Preserve headings, lists, tables, and visible line breaks when possible."
		default:
			return "Return ONLY the extracted text, no commentary."
		}
	case ModeIdentifyObjects:
		switch format {
		case FormatMarkdown:
			return "Format the response as a Markdown list. For each object include name/type, approximate location, relative size, and notable attributes."
		case FormatPlainText:
			return "Return a plain-text list. For each object include name/type, approximate location, relative size, and notable attributes."
		default:
			return `Format your response as valid JSON only:
[{"name": "object name", "location": "position description", "size": "relative size", "attributes": "notable details"}]`
		}
	case ModeAnalyzeUI:
		switch format {
		case FormatMarkdown:
			return "Format the response in Markdown with sections for layout, interactive elements, navigation, and visual hierarchy."
		case FormatPlainText:
			return "Return plain text with labeled sections for layout, interactive elements, navigation, and visual hierarchy."
		default:
			return `Format your response as valid JSON only:
{"layout": "description", "elements": [{"type": "element type", "text": "visible text", "location": "position", "state": "active/inactive/disabled"}], "navigation": "nav description", "visual_hierarchy": "description"}`
		}
	case ModeExtractData:
		switch format {
		case FormatMarkdown:
			return "Format the response in Markdown. Preserve tables, forms, charts, and lists with clear headings and labels."
		case FormatPlainText:
			return "Return plain text with clear sections for tables, forms, charts, and lists. Preserve row/column relationships when possible."
		default:
			return `Return the data as valid JSON only. For tables, use: {"headers": [...], "rows": [[...], ...]}
For forms, use: {"fields": [{"label": "...", "value": "..."}]}
For charts, use: {"type": "chart type", "title": "...", "data_points": [...]}`
		}
	case ModeAnalyzeDocument:
		switch format {
		case FormatMarkdown:
			return "Format the response in Markdown with sections for document type, key fields, dates, amounts, parties, and summary."
		case FormatPlainText:
			return "Return plain text with labeled sections for document type, key fields, dates, amounts, parties, and summary."
		default:
			return `Format as valid JSON only:
{"document_type": "...", "fields": [{"label": "...", "value": "..."}], "dates": [...], "amounts": [...], "parties": [...], "summary": "..."}`
		}
	case ModeDescribeDiagram:
		switch format {
		case FormatMarkdown:
			return "Format the response in Markdown with sections for diagram type, entities, relationships, flow direction, and annotations."
		case FormatPlainText:
			return "Return plain text with labeled sections for diagram type, entities, relationships, flow direction, and annotations."
		default:
			return `Format as valid JSON only:
{"diagram_type": "...", "entities": [{"name": "...", "type": "...", "description": "..."}], "relationships": [{"from": "...", "to": "...", "label": "...", "type": "..."}], "flow_direction": "...", "annotations": [...]}`
		}
	default:
		switch format {
		case FormatJSON:
			return `Return valid JSON only in the form {"summary": "..."}`
		case FormatMarkdown:
			return "Format your response using Markdown."
		default:
			return ""
		}
	}
}

// BuildCustomPrompt wraps a user-provided prompt with output format
// instructions, if a specific format is requested.
func BuildCustomPrompt(userPrompt string, format OutputFormat) string {
	if userPrompt == "" {
		return ""
	}
	switch format {
	case FormatJSON:
		return userPrompt + "\n\nReturn your response as valid JSON."
	case FormatMarkdown:
		return userPrompt + "\n\nFormat your response using Markdown."
	default:
		return userPrompt
	}
}

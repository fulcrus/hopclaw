// Package document implements document tool handlers (document.read, document.info,
// document.create, document.search) for the toolruntime registry.
package document

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// Runtime is the narrow interface that document handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
	DisplayPath(absPath string) string
}

// Handler is the tool handler signature, parameterized on our narrow Runtime interface.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with a document handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// ToolDefs returns all document domain tool definitions.
func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "document.read",
				Description:     "Extract text content, paragraphs, and tables from a DOCX file.",
				InputSchema:     documentReadInputSchema(),
				OutputSchema:    documentReadOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "document:{path}",
			},
			Handler: handleDocumentRead,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "document.info",
				Description:     "Get metadata and statistics from a DOCX file.",
				InputSchema:     documentInfoInputSchema(),
				OutputSchema:    documentInfoOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "document:{path}",
			},
			Handler: handleDocumentInfo,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "document.create",
				Description:      "Create a new DOCX file from structured content with paragraphs and headings.",
				InputSchema:      documentCreateInputSchema(),
				OutputSchema:     documentCreateOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "document:{path}",
			},
			Handler: handleDocumentCreate,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "document.search",
				Description:     "Search for text within paragraphs of a DOCX file.",
				InputSchema:     documentSearchInputSchema(),
				OutputSchema:    documentSearchOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "document:{path}",
			},
			Handler: handleDocumentSearch,
		},
	}
}

// ---------------------------------------------------------------------------
// OOXML namespace and XML structs for DOCX parsing
// ---------------------------------------------------------------------------

const wordprocessingMLNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// docxContent holds the extracted content from a DOCX file.
type docxContent struct {
	Paragraphs []docxParagraph
	Tables     []docxTableData
}

type docxParagraph struct {
	Text  string
	Style string
}

type docxTableData struct {
	Rows [][]string
}

// docxCoreProps holds metadata from docProps/core.xml.
type docxCoreProps struct {
	Title   string
	Creator string
}

// ---------------------------------------------------------------------------
// DOCX reading helpers
// ---------------------------------------------------------------------------

// readDOCXContent reads and parses a DOCX file, extracting paragraphs and tables.
func readDOCXContent(path string) (*docxContent, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open word/document.xml: %w", err)
			}
			defer rc.Close()
			return parseDocumentXML(rc)
		}
	}
	return nil, fmt.Errorf("word/document.xml not found in archive")
}

// parseDocumentXML uses an xml.Decoder to walk tokens and extract paragraphs/tables.
func parseDocumentXML(r io.Reader) (*docxContent, error) {
	dec := xml.NewDecoder(r)
	content := &docxContent{}

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xml decode: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Space == wordprocessingMLNS && se.Name.Local == "p" {
			p, err := parseParagraph(dec)
			if err != nil {
				return nil, err
			}
			content.Paragraphs = append(content.Paragraphs, p)
		}
		if se.Name.Space == wordprocessingMLNS && se.Name.Local == "tbl" {
			tbl, err := parseTable(dec)
			if err != nil {
				return nil, err
			}
			content.Tables = append(content.Tables, tbl)
		}
	}
	return content, nil
}

// parseParagraph consumes tokens until the matching </w:p> and extracts text runs and style.
func parseParagraph(dec *xml.Decoder) (docxParagraph, error) {
	var p docxParagraph
	var textBuf strings.Builder
	depth := 1
	inRun := false
	inText := false
	inPPr := false

	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return p, fmt.Errorf("xml decode paragraph: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Space == wordprocessingMLNS {
				switch t.Name.Local {
				case "r":
					inRun = true
				case "t":
					if inRun {
						inText = true
					}
				case "pPr":
					inPPr = true
				case "pStyle":
					if inPPr {
						for _, attr := range t.Attr {
							if attr.Name.Local == "val" {
								p.Style = attr.Value
							}
						}
					}
				}
			}
		case xml.EndElement:
			depth--
			if t.Name.Space == wordprocessingMLNS {
				switch t.Name.Local {
				case "r":
					inRun = false
				case "t":
					inText = false
				case "pPr":
					inPPr = false
				}
			}
		case xml.CharData:
			if inText {
				textBuf.Write(t)
			}
		}
	}
	p.Text = textBuf.String()
	return p, nil
}

// parseTable consumes tokens until the matching </w:tbl> and extracts rows/cells.
func parseTable(dec *xml.Decoder) (docxTableData, error) {
	var tbl docxTableData
	depth := 1
	inRow := false
	inCell := false
	inText := false
	inRun := false
	var cellBuf strings.Builder
	var currentRow []string

	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return tbl, fmt.Errorf("xml decode table: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Space == wordprocessingMLNS {
				switch t.Name.Local {
				case "tr":
					inRow = true
					currentRow = nil
				case "tc":
					if inRow {
						inCell = true
						cellBuf.Reset()
					}
				case "r":
					if inCell {
						inRun = true
					}
				case "t":
					if inRun && inCell {
						inText = true
					}
				}
			}
		case xml.EndElement:
			depth--
			if t.Name.Space == wordprocessingMLNS {
				switch t.Name.Local {
				case "tr":
					if inRow {
						tbl.Rows = append(tbl.Rows, currentRow)
						inRow = false
					}
				case "tc":
					if inCell {
						currentRow = append(currentRow, cellBuf.String())
						inCell = false
					}
				case "r":
					inRun = false
				case "t":
					inText = false
				}
			}
		case xml.CharData:
			if inText && inCell {
				cellBuf.Write(t)
			}
		}
	}
	return tbl, nil
}

// readDOCXCoreProps reads docProps/core.xml for title and creator.
func readDOCXCoreProps(path string) (docxCoreProps, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return docxCoreProps{}, err
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "docProps/core.xml" {
			rc, err := f.Open()
			if err != nil {
				return docxCoreProps{}, fmt.Errorf("failed to open docProps/core.xml: %w", err)
			}
			defer rc.Close()
			return parseCoreProps(rc)
		}
	}
	return docxCoreProps{}, nil // no core.xml is acceptable
}

// parseCoreProps extracts dc:title and dc:creator from core.xml.
func parseCoreProps(r io.Reader) (docxCoreProps, error) {
	var props docxCoreProps
	dec := xml.NewDecoder(r)
	inTitle := false
	inCreator := false

	const dcNS = "http://purl.org/dc/elements/1.1/"

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return props, fmt.Errorf("xml decode core props: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == dcNS && t.Name.Local == "title" {
				inTitle = true
			}
			if t.Name.Space == dcNS && t.Name.Local == "creator" {
				inCreator = true
			}
		case xml.EndElement:
			if t.Name.Space == dcNS && t.Name.Local == "title" {
				inTitle = false
			}
			if t.Name.Space == dcNS && t.Name.Local == "creator" {
				inCreator = false
			}
		case xml.CharData:
			if inTitle {
				props.Title = string(t)
			}
			if inCreator {
				props.Creator = string(t)
			}
		}
	}
	return props, nil
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func documentReadInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path": stringSchema("Path to the DOCX file."),
	}, "path")
}

func documentInfoInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path": stringSchema("Path to the DOCX file."),
	}, "path")
}

func documentCreateInputSchema() map[string]any {
	paragraphSchema := objectSchema(map[string]any{
		"text":  stringSchema("The paragraph text content."),
		"style": stringSchema("Paragraph style: heading1, heading2, heading3, or normal. Defaults to normal."),
	}, "text")
	return objectSchema(map[string]any{
		"path":    stringSchema("Output path for the DOCX file."),
		"title":   stringSchema("Optional document title metadata."),
		"author":  stringSchema("Optional document author metadata."),
		"content": arraySchema(paragraphSchema, "Array of paragraph objects to include in the document."),
	}, "path", "content")
}

func documentSearchInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":           stringSchema("Path to the DOCX file."),
		"query":          stringSchema("Text to search for."),
		"case_sensitive": booleanSchema("Whether the search is case-sensitive. Defaults to false."),
	}, "path", "query")
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func documentReadOutputSchema() map[string]any {
	tableSchema := objectSchema(map[string]any{
		"rows": arraySchema(arraySchema(stringSchema("Cell value."), "Row cells."), "Table rows."),
	}, "rows")
	return objectSchema(map[string]any{
		"path":            stringSchema("Document path."),
		"paragraphs":      arraySchema(stringSchema("Paragraph text."), "Extracted paragraphs."),
		"tables":          arraySchema(tableSchema, "Extracted tables."),
		"text":            stringSchema("Concatenated text of all paragraphs."),
		"paragraph_count": integerSchema("Number of paragraphs."),
		"word_count":      integerSchema("Total word count."),
	}, "path", "paragraphs", "tables", "text", "paragraph_count", "word_count")
}

func documentInfoOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":            stringSchema("Document path."),
		"title":           stringSchema("Document title from metadata."),
		"author":          stringSchema("Document author from metadata."),
		"word_count":      integerSchema("Total word count."),
		"paragraph_count": integerSchema("Number of paragraphs."),
		"file_size":       integerSchema("File size in bytes."),
	}, "path", "title", "author", "word_count", "paragraph_count", "file_size")
}

func documentCreateOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":            stringSchema("Created document path."),
		"paragraph_count": integerSchema("Number of paragraphs written."),
		"bytes":           integerSchema("File size in bytes."),
	}, "path", "paragraph_count", "bytes")
}

func documentSearchOutputSchema() map[string]any {
	matchSchema := objectSchema(map[string]any{
		"paragraph_index": integerSchema("0-based index of the matching paragraph."),
		"text":            stringSchema("Full text of the matching paragraph."),
		"context":         stringSchema("Surrounding context snippet."),
	}, "paragraph_index", "text", "context")
	return objectSchema(map[string]any{
		"path":          stringSchema("Document path."),
		"query":         stringSchema("Search query."),
		"matches":       arraySchema(matchSchema, "Matching paragraphs."),
		"total_matches": integerSchema("Total number of matches."),
	}, "path", "query", "matches", "total_matches")
}

// ---------------------------------------------------------------------------
// Schema helpers — duplicated locally to avoid importing toolruntime.
// ---------------------------------------------------------------------------

func stringSchema(description string) map[string]any {
	schema := map[string]any{"type": "string"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func integerSchema(description string) map[string]any {
	schema := map[string]any{"type": "integer"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func booleanSchema(description string) map[string]any {
	schema := map[string]any{"type": "boolean"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func arraySchema(items map[string]any, description string) map[string]any {
	schema := map[string]any{
		"type":  "array",
		"items": items,
	}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

// ---------------------------------------------------------------------------
// Param helpers — duplicated locally to avoid importing toolruntime.
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

func requiredString(input map[string]any, key string) (string, error) {
	value, err := stringFrom(input[key])
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func optionalString(input map[string]any, key string) string {
	value, _ := stringFrom(input[key])
	return strings.TrimSpace(value)
}

func boolFromDefault(value any, fallback bool) (bool, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case bool:
		return typed, nil
	default:
		return false, fmt.Errorf("expected boolean, got %T", value)
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleDocumentRead(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.read: %w", err)
	}
	content, err := readDOCXContent(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.read: %w", err)
	}

	paragraphs := make([]string, len(content.Paragraphs))
	for i, p := range content.Paragraphs {
		paragraphs[i] = p.Text
	}

	tables := make([]map[string]any, len(content.Tables))
	for i, t := range content.Tables {
		tables[i] = map[string]any{"rows": t.Rows}
	}

	fullText := strings.Join(paragraphs, "\n")
	wordCount := countWords(fullText)

	return rt.JSONResult(call, map[string]any{
		"path":            rt.DisplayPath(resolved),
		"paragraphs":      paragraphs,
		"tables":          tables,
		"text":            fullText,
		"paragraph_count": len(paragraphs),
		"word_count":      wordCount,
	})
}

func handleDocumentInfo(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.info: %w", err)
	}

	fi, err := os.Stat(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.info: %w", err)
	}

	content, err := readDOCXContent(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.info: %w", err)
	}

	props, _ := readDOCXCoreProps(resolved) // ignore error, metadata is optional

	paragraphs := make([]string, len(content.Paragraphs))
	for i, p := range content.Paragraphs {
		paragraphs[i] = p.Text
	}
	fullText := strings.Join(paragraphs, "\n")
	wordCount := countWords(fullText)

	return rt.JSONResult(call, map[string]any{
		"path":            rt.DisplayPath(resolved),
		"title":           props.Title,
		"author":          props.Creator,
		"word_count":      wordCount,
		"paragraph_count": len(content.Paragraphs),
		"file_size":       fi.Size(),
	})
}

func handleDocumentCreate(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.create: %w", err)
	}

	title := optionalString(call.Input, "title")
	author := optionalString(call.Input, "author")

	contentArr, ok := call.Input["content"].([]any)
	if !ok {
		return contextengine.ToolResult{}, fmt.Errorf("document.create: content must be an array")
	}

	var paragraphs []docxCreateParagraph
	for i, item := range contentArr {
		m, ok := item.(map[string]any)
		if !ok {
			return contextengine.ToolResult{}, fmt.Errorf("document.create: content[%d] must be an object", i)
		}
		text, _ := m["text"].(string)
		if text == "" {
			return contextengine.ToolResult{}, fmt.Errorf("document.create: content[%d].text is required", i)
		}
		style, _ := m["style"].(string)
		if style == "" {
			style = "normal"
		}
		paragraphs = append(paragraphs, docxCreateParagraph{Text: text, Style: style})
	}

	data, err := buildMinimalDOCX(title, author, paragraphs)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.create: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.create: %w", err)
	}
	if err := os.WriteFile(resolved, data, 0o644); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.create: %w", err)
	}

	return rt.JSONResult(call, map[string]any{
		"path":            rt.DisplayPath(resolved),
		"paragraph_count": len(paragraphs),
		"bytes":           len(data),
	})
}

func handleDocumentSearch(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	query, err := requiredString(call.Input, "query")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.search: %w", err)
	}

	caseSensitive, err := boolFromDefault(call.Input["case_sensitive"], false)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.search: case_sensitive: %w", err)
	}

	content, err := readDOCXContent(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("document.search: %w", err)
	}

	var matches []map[string]any
	searchQuery := query
	if !caseSensitive {
		searchQuery = strings.ToLower(query)
	}

	for i, p := range content.Paragraphs {
		text := p.Text
		compareText := text
		if !caseSensitive {
			compareText = strings.ToLower(text)
		}
		if strings.Contains(compareText, searchQuery) {
			contextSnippet := documentSearchContext(text, query, caseSensitive)
			matches = append(matches, map[string]any{
				"paragraph_index": i,
				"text":            text,
				"context":         contextSnippet,
			})
		}
	}

	return rt.JSONResult(call, map[string]any{
		"path":          rt.DisplayPath(resolved),
		"query":         query,
		"matches":       matches,
		"total_matches": len(matches),
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// documentSearchContextWindow is the number of characters shown around a match.
const documentSearchContextWindow = 40

// countWords counts words in text by splitting on whitespace.
func countWords(text string) int {
	fields := strings.Fields(text)
	return len(fields)
}

// documentSearchContext extracts a snippet of text around the first occurrence of query.
func documentSearchContext(text, query string, caseSensitive bool) string {
	searchText := text
	searchQuery := query
	if !caseSensitive {
		searchText = strings.ToLower(text)
		searchQuery = strings.ToLower(query)
	}
	idx := strings.Index(searchText, searchQuery)
	if idx < 0 {
		return text
	}
	start := idx - documentSearchContextWindow
	if start < 0 {
		start = 0
	}
	end := idx + len(query) + documentSearchContextWindow
	if end > len(text) {
		end = len(text)
	}
	snippet := text[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(text) {
		snippet = snippet + "..."
	}
	return snippet
}

// ---------------------------------------------------------------------------
// DOCX creation types
// ---------------------------------------------------------------------------

// docxCreateParagraph represents a paragraph to include when creating a DOCX.
type docxCreateParagraph struct {
	Text  string
	Style string // "heading1", "heading2", "heading3", or "normal"
}

// ---------------------------------------------------------------------------
// DOCX template constants
// ---------------------------------------------------------------------------

const docxContentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
</Types>`

const docxRootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`

const docxDocumentRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

const docxStylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:style w:type="paragraph" w:styleId="Normal" w:default="1">
    <w:name w:val="Normal"/>
    <w:rPr>
      <w:sz w:val="24"/>
    </w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading1">
    <w:name w:val="heading 1"/>
    <w:pPr>
      <w:outlineLvl w:val="0"/>
    </w:pPr>
    <w:rPr>
      <w:b/>
      <w:sz w:val="48"/>
    </w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading2">
    <w:name w:val="heading 2"/>
    <w:pPr>
      <w:outlineLvl w:val="1"/>
    </w:pPr>
    <w:rPr>
      <w:b/>
      <w:sz w:val="36"/>
    </w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading3">
    <w:name w:val="heading 3"/>
    <w:pPr>
      <w:outlineLvl w:val="2"/>
    </w:pPr>
    <w:rPr>
      <w:b/>
      <w:sz w:val="28"/>
    </w:rPr>
  </w:style>
</w:styles>`

// ---------------------------------------------------------------------------
// DOCX builder
// ---------------------------------------------------------------------------

// buildMinimalDOCX creates a valid DOCX file as a byte slice.
func buildMinimalDOCX(title, author string, paragraphs []docxCreateParagraph) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	files := map[string]string{
		"[Content_Types].xml":          docxContentTypesXML,
		"_rels/.rels":                  docxRootRelsXML,
		"word/_rels/document.xml.rels": docxDocumentRelsXML,
		"word/styles.xml":              docxStylesXML,
		"word/document.xml":            buildDocumentXML(paragraphs),
		"docProps/core.xml":            buildCorePropsXML(title, author),
	}

	// Write files in a deterministic order.
	order := []string{
		"[Content_Types].xml",
		"_rels/.rels",
		"word/_rels/document.xml.rels",
		"word/styles.xml",
		"word/document.xml",
		"docProps/core.xml",
	}
	for _, name := range order {
		w, err := zw.Create(name)
		if err != nil {
			return nil, fmt.Errorf("zip create %s: %w", name, err)
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			return nil, fmt.Errorf("zip write %s: %w", name, err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("zip close: %w", err)
	}
	return buf.Bytes(), nil
}

// styleIDForName maps user-facing style names to OOXML style IDs.
func styleIDForName(style string) string {
	switch strings.ToLower(style) {
	case "heading1":
		return "Heading1"
	case "heading2":
		return "Heading2"
	case "heading3":
		return "Heading3"
	default:
		return ""
	}
}

// buildDocumentXML generates the word/document.xml content.
func buildDocumentXML(paragraphs []docxCreateParagraph) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	sb.WriteString(`<w:body>`)

	for _, p := range paragraphs {
		sb.WriteString(`<w:p>`)
		styleID := styleIDForName(p.Style)
		if styleID != "" {
			sb.WriteString(`<w:pPr><w:pStyle w:val="`)
			sb.WriteString(styleID)
			sb.WriteString(`"/></w:pPr>`)
		}
		sb.WriteString(`<w:r><w:t>`)
		sb.WriteString(xmlEscapeString(p.Text))
		sb.WriteString(`</w:t></w:r>`)
		sb.WriteString(`</w:p>`)
	}

	sb.WriteString(`</w:body>`)
	sb.WriteString(`</w:document>`)
	return sb.String()
}

// buildCorePropsXML generates the docProps/core.xml content.
func buildCorePropsXML(title, author string) string {
	created := time.Now().UTC().Format(time.RFC3339)
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"`)
	sb.WriteString(` xmlns:dc="http://purl.org/dc/elements/1.1/"`)
	sb.WriteString(` xmlns:dcterms="http://purl.org/dc/terms/"`)
	sb.WriteString(` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`)
	sb.WriteString(`<dc:title>`)
	sb.WriteString(xmlEscapeString(title))
	sb.WriteString(`</dc:title>`)
	sb.WriteString(`<dc:creator>`)
	sb.WriteString(xmlEscapeString(author))
	sb.WriteString(`</dc:creator>`)
	sb.WriteString(`<dcterms:created xsi:type="dcterms:W3CDTF">`)
	sb.WriteString(created)
	sb.WriteString(`</dcterms:created>`)
	sb.WriteString(`</cp:coreProperties>`)
	return sb.String()
}

// xmlEscapeString escapes a string for safe inclusion in XML text content.
func xmlEscapeString(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return s
	}
	return buf.String()
}

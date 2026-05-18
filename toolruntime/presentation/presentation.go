// Package presentation implements presentation tool handlers (presentation.read,
// presentation.info, presentation.create) for the toolruntime registry.
package presentation

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// Runtime is the narrow interface that presentation handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
	DisplayPath(absPath string) string
}

// Handler is the tool handler signature, parameterized on our narrow Runtime interface.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with a presentation handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// ToolDefs returns all presentation domain tool definitions.
func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "presentation.read",
				Description:     "Extract slide content from a PPTX presentation file.",
				InputSchema:     presentationReadInputSchema(),
				OutputSchema:    presentationReadOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "presentation:{path}",
			},
			Handler: handlePresentationRead,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "presentation.info",
				Description:     "Get metadata and slide count from a PPTX presentation file.",
				InputSchema:     presentationInfoInputSchema(),
				OutputSchema:    presentationInfoOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "presentation:{path}",
			},
			Handler: handlePresentationInfo,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "presentation.create",
				Description:      "Create a PPTX presentation from structured slide content.",
				InputSchema:      presentationCreateInputSchema(),
				OutputSchema:     presentationCreateOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "presentation:{path}",
			},
			Handler: handlePresentationCreate,
		},
	}
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// pptxSlidePattern matches slide XML filenames inside a PPTX archive.
var pptxSlidePattern = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

// drawingMLNamespace is the namespace URI for DrawingML text elements.
const drawingMLNamespace = "http://schemas.openxmlformats.org/drawingml/2006/main"

// presentationMLNamespace is the namespace URI for PresentationML elements.
const presentationMLNamespace = "http://schemas.openxmlformats.org/presentationml/2006/main"

// dublinCoreNamespace is the namespace URI for Dublin Core metadata elements.
const dublinCoreNamespace = "http://purl.org/dc/elements/1.1/"

// cpNamespace is the namespace URI for core properties.
const cpNamespace = "http://schemas.openxmlformats.org/package/2006/metadata/core-properties"

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func presentationReadInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path": stringSchema("Path to the PPTX file."),
	}, "path")
}

func presentationInfoInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path": stringSchema("Path to the PPTX file."),
	}, "path")
}

func presentationCreateInputSchema() map[string]any {
	slideSchema := objectSchema(map[string]any{
		"title": stringSchema("Slide title text."),
		"content": map[string]any{
			"description": "Slide body content. A single string or an array of strings (one per paragraph).",
		},
		"notes": stringSchema("Optional speaker notes for this slide."),
	}, "title")
	return objectSchema(map[string]any{
		"path":   stringSchema("Output path for the PPTX file."),
		"title":  stringSchema("Optional presentation title (metadata)."),
		"author": stringSchema("Optional presentation author (metadata)."),
		"slides": arraySchema(slideSchema, "Array of slide objects to create."),
	}, "path", "slides")
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func presentationReadOutputSchema() map[string]any {
	slideSchema := objectSchema(map[string]any{
		"index":   integerSchema("1-based slide index."),
		"title":   stringSchema("Slide title text."),
		"content": stringSchema("Slide body content."),
		"notes":   stringSchema("Speaker notes for this slide."),
	}, "index")
	return objectSchema(map[string]any{
		"path":        stringSchema("Presentation file path."),
		"slides":      arraySchema(slideSchema, "Extracted slides."),
		"slide_count": integerSchema("Total number of slides."),
	}, "path", "slides", "slide_count")
}

func presentationInfoOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":        stringSchema("Presentation file path."),
		"slide_count": integerSchema("Total number of slides."),
		"title":       stringSchema("Presentation title from metadata."),
		"author":      stringSchema("Presentation author from metadata."),
		"file_size":   integerSchema("File size in bytes."),
	}, "path", "slide_count")
}

func presentationCreateOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":        stringSchema("Created file path."),
		"slide_count": integerSchema("Number of slides created."),
		"bytes":       integerSchema("File size in bytes."),
	}, "path", "slide_count", "bytes")
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

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handlePresentationRead(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.read: %w", err)
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.read: %w", err)
	}
	content, err := readPPTXContent(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.read: %w", err)
	}
	slides := make([]map[string]any, len(content.slides))
	for i, s := range content.slides {
		slides[i] = map[string]any{
			"index":   s.index,
			"title":   s.title,
			"content": s.content,
			"notes":   s.notes,
		}
	}
	return rt.JSONResult(call, map[string]any{
		"path":        rt.DisplayPath(resolved),
		"slides":      slides,
		"slide_count": len(content.slides),
	})
}

func handlePresentationInfo(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.info: %w", err)
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.info: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.info: %w", err)
	}
	content, err := readPPTXContent(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.info: %w", err)
	}
	return rt.JSONResult(call, map[string]any{
		"path":        rt.DisplayPath(resolved),
		"slide_count": len(content.slides),
		"title":       content.title,
		"author":      content.author,
		"file_size":   info.Size(),
	})
}

func handlePresentationCreate(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.create: %w", err)
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.create: %w", err)
	}
	title := optionalString(call.Input, "title")
	author := optionalString(call.Input, "author")

	rawSlides, ok := call.Input["slides"].([]any)
	if !ok || len(rawSlides) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.create: slides is required and must be a non-empty array")
	}
	slides := make([]pptxCreateSlide, 0, len(rawSlides))
	for i, raw := range rawSlides {
		obj, objOK := raw.(map[string]any)
		if !objOK {
			return contextengine.ToolResult{}, fmt.Errorf("presentation.create: slides[%d] must be an object", i)
		}
		slideTitle, _ := stringFrom(obj["title"])
		slideNotes, _ := stringFrom(obj["notes"])
		contentLines := pptxParseContent(obj["content"])
		slides = append(slides, pptxCreateSlide{
			Title:   slideTitle,
			Content: contentLines,
			Notes:   slideNotes,
		})
	}

	data, err := buildMinimalPPTX(title, author, slides)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.create: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.create: %w", err)
	}
	if err := os.WriteFile(resolved, data, 0o644); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("presentation.create: %w", err)
	}
	return rt.JSONResult(call, map[string]any{
		"path":        rt.DisplayPath(resolved),
		"slide_count": len(slides),
		"bytes":       len(data),
	})
}

// ---------------------------------------------------------------------------
// PPTX reading helpers
// ---------------------------------------------------------------------------

// pptxContent holds the extracted content from a PPTX file.
type pptxContent struct {
	slides []pptxSlide
	title  string
	author string
}

// pptxSlide holds the extracted content from a single slide.
type pptxSlide struct {
	index   int
	title   string
	content string
	notes   string
}

// readPPTXContent extracts slide content, notes, and metadata from a PPTX file.
func readPPTXContent(path string) (*pptxContent, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	// Index files by name for quick lookup.
	fileMap := make(map[string]*zip.File, len(r.File))
	for _, f := range r.File {
		fileMap[f.Name] = f
	}

	// Collect slide numbers and sort them.
	type slideEntry struct {
		num  int
		file *zip.File
	}
	var slideEntries []slideEntry
	for name, f := range fileMap {
		if m := pptxSlidePattern.FindStringSubmatch(name); m != nil {
			num, _ := strconv.Atoi(m[1])
			slideEntries = append(slideEntries, slideEntry{num: num, file: f})
		}
	}
	sort.Slice(slideEntries, func(i, j int) bool {
		return slideEntries[i].num < slideEntries[j].num
	})

	result := &pptxContent{}

	// Read slides.
	for _, entry := range slideEntries {
		slideXML, readErr := readZipFile(entry.file)
		if readErr != nil {
			return nil, fmt.Errorf("reading slide %d: %w", entry.num, readErr)
		}
		title, content := pptxParseSlideXML(slideXML)

		// Try to read corresponding notes.
		notesText := ""
		notesName := fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", entry.num)
		if notesFile, ok := fileMap[notesName]; ok {
			notesXML, notesErr := readZipFile(notesFile)
			if notesErr == nil {
				notesText = pptxParseNotesXML(notesXML)
			}
		}

		result.slides = append(result.slides, pptxSlide{
			index:   entry.num,
			title:   title,
			content: content,
			notes:   notesText,
		})
	}

	// Read metadata from docProps/core.xml.
	if coreFile, ok := fileMap["docProps/core.xml"]; ok {
		coreXML, coreErr := readZipFile(coreFile)
		if coreErr == nil {
			result.title, result.author = pptxParseCoreXML(coreXML)
		}
	}

	return result, nil
}

// readZipFile reads the full content of a zip.File entry.
func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// pptxParseSlideXML extracts the title and body content from a slide XML.
// Title is detected by a <p:ph type="title"/> or <p:ph type="ctrTitle"/> placeholder.
// All other text shapes are concatenated as content.
func pptxParseSlideXML(data []byte) (title string, content string) {
	shapes := pptxExtractShapes(data)
	titleFound := false
	var contentParts []string
	for _, shape := range shapes {
		if !titleFound && shape.isTitle {
			title = shape.text
			titleFound = true
		} else if shape.text != "" {
			contentParts = append(contentParts, shape.text)
		}
	}
	// If no explicit title placeholder, use the first shape as title.
	if !titleFound && len(shapes) > 0 {
		title = shapes[0].text
		contentParts = nil
		for _, shape := range shapes[1:] {
			if shape.text != "" {
				contentParts = append(contentParts, shape.text)
			}
		}
	}
	content = strings.Join(contentParts, "\n")
	return title, content
}

// pptxShape holds text extracted from a single shape element.
type pptxShape struct {
	text    string
	isTitle bool
}

// pptxExtractShapes parses the slide XML and extracts text shapes.
func pptxExtractShapes(data []byte) []pptxShape {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	var shapes []pptxShape
	var currentText strings.Builder
	inShape := false
	isTitle := false
	depth := 0

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == presentationMLNamespace && t.Name.Local == "sp" {
				inShape = true
				isTitle = false
				depth = 1
				currentText.Reset()
			} else if inShape {
				depth++
				if t.Name.Space == presentationMLNamespace && t.Name.Local == "ph" {
					for _, attr := range t.Attr {
						if attr.Name.Local == "type" && (attr.Value == "title" || attr.Value == "ctrTitle") {
							isTitle = true
						}
					}
				}
			}
		case xml.EndElement:
			if inShape {
				depth--
				if depth == 0 {
					text := strings.TrimSpace(currentText.String())
					shapes = append(shapes, pptxShape{text: text, isTitle: isTitle})
					inShape = false
				}
			}
		case xml.CharData:
			if inShape {
				currentText.Write(t)
			}
		}
	}
	return shapes
}

// pptxParseNotesXML extracts speaker notes text from a notes slide XML.
func pptxParseNotesXML(data []byte) string {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	var text strings.Builder
	inText := false
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == drawingMLNamespace && t.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			if t.Name.Space == drawingMLNamespace && t.Name.Local == "t" {
				inText = false
			}
		case xml.CharData:
			if inText {
				text.Write(t)
			}
		}
	}
	return strings.TrimSpace(text.String())
}

// pptxParseCoreXML extracts title and author from docProps/core.xml.
func pptxParseCoreXML(data []byte) (title string, author string) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	var current string
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == dublinCoreNamespace && t.Name.Local == "title" {
				current = "title"
			} else if t.Name.Space == dublinCoreNamespace && t.Name.Local == "creator" {
				current = "author"
			} else if t.Name.Space == cpNamespace && t.Name.Local == "lastModifiedBy" {
				// fallback author
				if author == "" {
					current = "author"
				}
			} else {
				current = ""
			}
		case xml.CharData:
			switch current {
			case "title":
				title = strings.TrimSpace(string(t))
			case "author":
				author = strings.TrimSpace(string(t))
			}
		case xml.EndElement:
			current = ""
		}
	}
	return title, author
}

// pptxParseContent converts the "content" field from a create input into a slice of strings.
// It accepts either a single string or an array of strings.
func pptxParseContent(value any) []string {
	if value == nil {
		return nil
	}
	if s, ok := value.(string); ok {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	if arr, ok := value.([]any); ok {
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, sOK := item.(string); sOK {
				result = append(result, s)
			} else {
				result = append(result, fmt.Sprint(item))
			}
		}
		return result
	}
	return []string{fmt.Sprint(value)}
}

// ---------------------------------------------------------------------------
// PPTX creation types
// ---------------------------------------------------------------------------

// pptxCreateSlide holds input data for a single slide to be created.
type pptxCreateSlide struct {
	Title   string
	Content []string // one string per paragraph
	Notes   string
}

// ---------------------------------------------------------------------------
// PPTX builder
// ---------------------------------------------------------------------------

// buildMinimalPPTX creates a minimal valid PPTX file from the given metadata and slides.
func buildMinimalPPTX(title, author string, slides []pptxCreateSlide) ([]byte, error) {
	if len(slides) == 0 {
		return nil, fmt.Errorf("at least one slide is required")
	}

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	// [Content_Types].xml
	if err := pptxWriteZipEntry(w, "[Content_Types].xml", pptxContentTypes(len(slides))); err != nil {
		return nil, err
	}

	// _rels/.rels
	if err := pptxWriteZipEntry(w, "_rels/.rels", pptxRootRels()); err != nil {
		return nil, err
	}

	// docProps/core.xml
	if err := pptxWriteZipEntry(w, "docProps/core.xml", pptxCoreXML(title, author)); err != nil {
		return nil, err
	}

	// ppt/presentation.xml
	if err := pptxWriteZipEntry(w, "ppt/presentation.xml", pptxPresentationXML(len(slides))); err != nil {
		return nil, err
	}

	// ppt/_rels/presentation.xml.rels
	if err := pptxWriteZipEntry(w, "ppt/_rels/presentation.xml.rels", pptxPresentationRels(len(slides))); err != nil {
		return nil, err
	}

	// ppt/slideMasters/slideMaster1.xml
	if err := pptxWriteZipEntry(w, "ppt/slideMasters/slideMaster1.xml", pptxSlideMasterXML()); err != nil {
		return nil, err
	}

	// ppt/slideMasters/_rels/slideMaster1.xml.rels
	if err := pptxWriteZipEntry(w, "ppt/slideMasters/_rels/slideMaster1.xml.rels", pptxSlideMasterRels()); err != nil {
		return nil, err
	}

	// ppt/slideLayouts/slideLayout1.xml
	if err := pptxWriteZipEntry(w, "ppt/slideLayouts/slideLayout1.xml", pptxSlideLayoutXML()); err != nil {
		return nil, err
	}

	// ppt/slideLayouts/_rels/slideLayout1.xml.rels
	if err := pptxWriteZipEntry(w, "ppt/slideLayouts/_rels/slideLayout1.xml.rels", pptxSlideLayoutRels()); err != nil {
		return nil, err
	}

	// Slides
	for i, slide := range slides {
		num := i + 1
		slideXML := pptxSlideXML(slide.Title, slide.Content)
		if err := pptxWriteZipEntry(w, fmt.Sprintf("ppt/slides/slide%d.xml", num), slideXML); err != nil {
			return nil, err
		}
		slideRels := pptxSlideRels()
		if err := pptxWriteZipEntry(w, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", num), slideRels); err != nil {
			return nil, err
		}
	}

	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// ZIP helper
// ---------------------------------------------------------------------------

func pptxWriteZipEntry(w *zip.Writer, name string, content string) error {
	fw, err := w.Create(name)
	if err != nil {
		return err
	}
	_, err = fw.Write([]byte(content))
	return err
}

// ---------------------------------------------------------------------------
// XML templates
// ---------------------------------------------------------------------------

func pptxContentTypes(slideCount int) string {
	var slideParts strings.Builder
	for i := 1; i <= slideCount; i++ {
		slideParts.WriteString(fmt.Sprintf(
			`<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`,
			i))
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>` +
		slideParts.String() +
		`<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>` +
		`<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>` +
		`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
		`</Types>`
}

func pptxRootRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>` +
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
		`</Relationships>`
}

func pptxCoreXML(title, author string) string {
	now := time.Now().UTC().Format(time.RFC3339)
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"` +
		` xmlns:dc="http://purl.org/dc/elements/1.1/"` +
		` xmlns:dcterms="http://purl.org/dc/terms/"` +
		` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:title>` + xmlEscape(title) + `</dc:title>` +
		`<dc:creator>` + xmlEscape(author) + `</dc:creator>` +
		`<dcterms:created xsi:type="dcterms:W3CDTF">` + now + `</dcterms:created>` +
		`<dcterms:modified xsi:type="dcterms:W3CDTF">` + now + `</dcterms:modified>` +
		`</cp:coreProperties>`
}

func pptxPresentationXML(slideCount int) string {
	var slideList strings.Builder
	for i := 1; i <= slideCount; i++ {
		slideList.WriteString(fmt.Sprintf(`<p:sldId id="%d" r:id="rId%d"/>`, 255+i, i))
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:presentation xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"` +
		` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rIdMaster"/></p:sldMasterIdLst>` +
		`<p:sldIdLst>` + slideList.String() + `</p:sldIdLst>` +
		`<p:sldSz cx="9144000" cy="6858000" type="screen4x3"/>` +
		`<p:notesSz cx="6858000" cy="9144000"/>` +
		`</p:presentation>`
}

func pptxPresentationRels(slideCount int) string {
	var rels strings.Builder
	for i := 1; i <= slideCount; i++ {
		rels.WriteString(fmt.Sprintf(
			`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`,
			i, i))
	}
	rels.WriteString(`<Relationship Id="rIdMaster" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>`)
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		rels.String() +
		`</Relationships>`
}

func pptxSlideMasterXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sldMaster xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"` +
		` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<p:cSld><p:spTree>` +
		`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
		`<p:grpSpPr/>` +
		`</p:spTree></p:cSld>` +
		`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst>` +
		`</p:sldMaster>`
}

func pptxSlideMasterRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>` +
		`</Relationships>`
}

func pptxSlideLayoutXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sldLayout xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"` +
		` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
		` type="blank">` +
		`<p:cSld><p:spTree>` +
		`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
		`<p:grpSpPr/>` +
		`</p:spTree></p:cSld>` +
		`</p:sldLayout>`
}

func pptxSlideLayoutRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/>` +
		`</Relationships>`
}

func pptxSlideXML(title string, contentLines []string) string {
	var contentParas strings.Builder
	for _, line := range contentLines {
		contentParas.WriteString(`<a:p><a:r><a:rPr lang="en-US" dirty="0"/><a:t>`)
		contentParas.WriteString(xmlEscape(line))
		contentParas.WriteString(`</a:t></a:r></a:p>`)
	}
	if len(contentLines) == 0 {
		contentParas.WriteString(`<a:p><a:endParaRPr lang="en-US"/></a:p>`)
	}

	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"` +
		` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<p:cSld><p:spTree>` +
		`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
		`<p:grpSpPr/>` +
		// Title shape
		`<p:sp>` +
		`<p:nvSpPr><p:cNvPr id="2" name="Title"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>` +
		`<p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="457200" y="274638"/><a:ext cx="8229600" cy="1143000"/></a:xfrm></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/>` +
		`<a:p><a:r><a:rPr lang="en-US" dirty="0"/><a:t>` + xmlEscape(title) + `</a:t></a:r></a:p>` +
		`</p:txBody></p:sp>` +
		// Content shape
		`<p:sp>` +
		`<p:nvSpPr><p:cNvPr id="3" name="Content"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>` +
		`<p:nvPr><p:ph idx="1"/></p:nvPr></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="457200" y="1600200"/><a:ext cx="8229600" cy="4525963"/></a:xfrm></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/>` +
		contentParas.String() +
		`</p:txBody></p:sp>` +
		`</p:spTree></p:cSld>` +
		`</p:sld>`
}

func pptxSlideRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>` +
		`</Relationships>`
}

// xmlEscape escapes special XML characters in a string.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

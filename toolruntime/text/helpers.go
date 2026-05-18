package text

import (
	"context"
	"crypto/rand"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

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

func intFrom(value any, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback, nil
		}
		var v int64
		_, err := fmt.Sscanf(typed, "%d", &v)
		if err != nil {
			return 0, err
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

func boolFrom(value any) (bool, error) {
	switch typed := value.(type) {
	case nil:
		return false, nil
	case bool:
		return typed, nil
	default:
		return false, fmt.Errorf("expected boolean, got %T", value)
	}
}

func boolFromDefault(value any, fallback bool) (bool, error) {
	if value == nil {
		return fallback, nil
	}
	return boolFrom(value)
}

func stringSliceFrom(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []string:
		return typed, nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, v := range typed {
			s, err := stringFrom(v)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected string array, got %T", value)
	}
}

func mapFrom(value any) (map[string]any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return typed, nil
	default:
		return nil, fmt.Errorf("expected object, got %T", value)
	}
}

// ---------------------------------------------------------------------------
// textContent resolves content from either the "file" or "input" parameter.
// ---------------------------------------------------------------------------

func textContent(rt Runtime, input map[string]any) (string, error) {
	fileValue, err := stringFrom(input["file"])
	if err != nil {
		return "", fmt.Errorf("invalid file parameter: %w", err)
	}
	inputValue, err := stringFrom(input["input"])
	if err != nil {
		return "", fmt.Errorf("invalid input parameter: %w", err)
	}
	if strings.TrimSpace(fileValue) != "" {
		// Handle artifact URIs (e.g. "artifact://local/<id>").
		if strings.HasPrefix(fileValue, "artifact://") {
			data, _, err := rt.ReadArtifact(context.Background(), fileValue)
			if err != nil {
				return "", err
			}
			if data == nil {
				return "", fmt.Errorf("artifact store is not configured")
			}
			maxBytes := rt.MaxReadBytes()
			if maxBytes > 0 && len(data) > maxBytes {
				data = data[:maxBytes]
			}
			return string(data), nil
		}
		resolved, err := rt.ResolvePath(fileValue)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return "", err
		}
		maxBytes := rt.MaxReadBytes()
		if maxBytes > 0 && len(data) > maxBytes {
			data = data[:maxBytes]
		}
		return string(data), nil
	}
	if strings.TrimSpace(inputValue) != "" {
		return inputValue, nil
	}
	return "", fmt.Errorf("either file or input is required")
}

// ---------------------------------------------------------------------------
// JSON query helpers
// ---------------------------------------------------------------------------

func queryJSON(data any, path string) any {
	if path == "" || path == "." {
		return data
	}
	raw := strings.TrimPrefix(path, ".")
	parts := splitJSONPath(raw)
	current := data
	for _, part := range parts {
		if current == nil {
			return nil
		}
		if idx := strings.Index(part, "["); idx >= 0 {
			key := part[:idx]
			indexStr := strings.TrimSuffix(part[idx+1:], "]")
			if key != "" {
				m, ok := current.(map[string]any)
				if !ok {
					return nil
				}
				current = m[key]
				if current == nil {
					return nil
				}
			}
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return nil
			}
			arr, ok := current.([]any)
			if !ok || index < 0 || index >= len(arr) {
				return nil
			}
			current = arr[index]
			continue
		}
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[part]
	}
	return current
}

func splitJSONPath(path string) []string {
	var parts []string
	var buf strings.Builder
	for i := 0; i < len(path); i++ {
		ch := path[i]
		if ch == '.' && buf.Len() > 0 {
			parts = append(parts, buf.String())
			buf.Reset()
		} else if ch == '.' {
			// leading dot or consecutive dots, skip
		} else {
			buf.WriteByte(ch)
		}
	}
	if buf.Len() > 0 {
		parts = append(parts, buf.String())
	}
	return parts
}

// ---------------------------------------------------------------------------
// YAML normalization
// ---------------------------------------------------------------------------

func normalizeYAML(v any) any {
	switch val := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			out[fmt.Sprintf("%v", k)] = normalizeYAML(vv)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			out[k] = normalizeYAML(vv)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = normalizeYAML(vv)
		}
		return out
	default:
		return v
	}
}

// ---------------------------------------------------------------------------
// XML parsing
// ---------------------------------------------------------------------------

type xmlNode struct {
	name     string
	attrs    map[string]string
	text     string
	children []*xmlNode
}

func parseXMLToMap(input string, tagFilter string) (any, error) {
	decoder := xml.NewDecoder(strings.NewReader(input))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose

	var stack []*xmlNode
	root := &xmlNode{name: "_root"}
	stack = append(stack, root)

	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			node := &xmlNode{
				name:  t.Name.Local,
				attrs: make(map[string]string, len(t.Attr)),
			}
			for _, a := range t.Attr {
				node.attrs[a.Name.Local] = a.Value
			}
			stack[len(stack)-1].children = append(stack[len(stack)-1].children, node)
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text != "" && len(stack) > 0 {
				stack[len(stack)-1].text += text
			}
		}
	}

	converted := xmlNodeToMap(root)
	if tagFilter != "" {
		return extractXMLTag(converted, tagFilter), nil
	}
	if m, ok := converted.(map[string]any); ok {
		if len(m) == 1 {
			for _, v := range m {
				return v, nil
			}
		}
	}
	return converted, nil
}

func xmlNodeToMap(n *xmlNode) any {
	if len(n.children) == 0 && len(n.attrs) == 0 {
		return n.text
	}
	result := make(map[string]any)
	for k, v := range n.attrs {
		result["@"+k] = v
	}
	if n.text != "" {
		result["#text"] = n.text
	}
	childMap := make(map[string][]any)
	for _, child := range n.children {
		childMap[child.name] = append(childMap[child.name], xmlNodeToMap(child))
	}
	for name, vals := range childMap {
		if len(vals) == 1 {
			result[name] = vals[0]
		} else {
			result[name] = vals
		}
	}
	return result
}

func extractXMLTag(data any, tag string) any {
	switch v := data.(type) {
	case map[string]any:
		if val, ok := v[tag]; ok {
			return val
		}
		for _, val := range v {
			if found := extractXMLTag(val, tag); found != nil {
				return found
			}
		}
	case []any:
		for _, item := range v {
			if found := extractXMLTag(item, tag); found != nil {
				return found
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// TOML parsing
// ---------------------------------------------------------------------------

func parseBasicTOML(input string) (map[string]any, error) {
	result := make(map[string]any)
	current := result
	lines := strings.Split(input, "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || line[0] == '#' {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			section := strings.TrimSpace(line[2 : len(line)-2])
			parts := strings.Split(section, ".")
			target := result
			for _, p := range parts[:len(parts)-1] {
				sub, ok := target[p]
				if !ok {
					sub = make(map[string]any)
					target[p] = sub
				}
				target, ok = sub.(map[string]any)
				if !ok {
					target = result
				}
			}
			lastKey := parts[len(parts)-1]
			var arr []any
			if existing, ok := target[lastKey]; ok {
				arr, _ = existing.([]any)
			}
			newTable := make(map[string]any)
			arr = append(arr, newTable)
			target[lastKey] = arr
			current = newTable
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(line[1 : len(line)-1])
			parts := strings.Split(section, ".")
			current = result
			for _, p := range parts {
				sub, ok := current[p]
				if !ok {
					sub = make(map[string]any)
					current[p] = sub
				}
				if m, ok := sub.(map[string]any); ok {
					current = m
				} else {
					newMap := make(map[string]any)
					current[p] = newMap
					current = newMap
				}
			}
			continue
		}
		eqIdx := strings.Index(line, "=")
		if eqIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eqIdx])
		val := strings.TrimSpace(line[eqIdx+1:])
		current[key] = parseTOMLValue(val)
	}
	return result, nil
}

func parseTOMLValue(val string) any {
	if (strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`)) ||
		(strings.HasPrefix(val, `'`) && strings.HasSuffix(val, `'`)) {
		return val[1 : len(val)-1]
	}
	if val == "true" {
		return true
	}
	if val == "false" {
		return false
	}
	if i, err := strconv.ParseInt(val, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(val, 64); err == nil {
		return f
	}
	if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
		inner := strings.TrimSpace(val[1 : len(val)-1])
		if inner == "" {
			return []any{}
		}
		parts := splitTOMLArray(inner)
		arr := make([]any, 0, len(parts))
		for _, p := range parts {
			arr = append(arr, parseTOMLValue(strings.TrimSpace(p)))
		}
		return arr
	}
	return val
}

func splitTOMLArray(s string) []string {
	var parts []string
	depth := 0
	inQuote := byte(0)
	start := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuote != 0 {
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			inQuote = ch
		case '[':
			depth++
		case ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

// ---------------------------------------------------------------------------
// INI parsing
// ---------------------------------------------------------------------------

func parseINI(input string) map[string]any {
	result := make(map[string]any)
	currentSection := ""
	lines := strings.Split(input, "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.TrimSpace(line[1 : len(line)-1])
			if _, ok := result[currentSection]; !ok {
				result[currentSection] = make(map[string]any)
			}
			continue
		}
		sep := strings.IndexAny(line, "=:")
		if sep < 0 {
			continue
		}
		key := strings.TrimSpace(line[:sep])
		val := strings.TrimSpace(line[sep+1:])
		if len(val) >= 2 &&
			((val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		if currentSection == "" {
			result[key] = val
		} else {
			sectionMap, ok := result[currentSection].(map[string]any)
			if !ok {
				sectionMap = make(map[string]any)
				result[currentSection] = sectionMap
			}
			sectionMap[key] = val
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Dotenv parsing
// ---------------------------------------------------------------------------

func parseDotenv(input string) map[string]string {
	vars := make(map[string]string)
	lines := strings.Split(input, "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || line[0] == '#' {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		line = strings.TrimPrefix(line, "export\t")
		eqIdx := strings.Index(line, "=")
		if eqIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eqIdx])
		val := strings.TrimSpace(line[eqIdx+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		vars[key] = val
	}
	return vars
}

// ---------------------------------------------------------------------------
// HTML helpers
// ---------------------------------------------------------------------------

func stripHTMLTags(input string) string {
	reScript := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	out := reScript.ReplaceAllString(input, "")
	reStyle := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	out = reStyle.ReplaceAllString(out, "")
	reTag := regexp.MustCompile(`<[^>]+>`)
	out = reTag.ReplaceAllString(out, " ")
	out = html.UnescapeString(out)
	reWS := regexp.MustCompile(`[ \t]+`)
	out = reWS.ReplaceAllString(out, " ")
	reNL := regexp.MustCompile(`\n{3,}`)
	out = reNL.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out)
}

func extractHTMLLinks(input string) []string {
	re := regexp.MustCompile(`(?i)<a\s[^>]*href\s*=\s*["']([^"']+)["'][^>]*>`)
	matches := re.FindAllStringSubmatch(input, -1)
	links := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			links = append(links, m[1])
		}
	}
	return links
}

func extractHTMLTagContent(input string, tag string) []string {
	pattern := fmt.Sprintf(`(?is)<%s[^>]*>(.*?)</%s>`, regexp.QuoteMeta(tag), regexp.QuoteMeta(tag))
	re := regexp.MustCompile(pattern)
	matches := re.FindAllStringSubmatch(input, -1)
	contents := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			contents = append(contents, strings.TrimSpace(m[1]))
		}
	}
	return contents
}

// ---------------------------------------------------------------------------
// Markdown helpers
// ---------------------------------------------------------------------------

func extractMarkdownFrontmatter(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	var buf strings.Builder
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return strings.TrimSpace(buf.String())
}

func buildMarkdownTOC(content string) []string {
	var toc []string
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i == 0 && trimmed == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			toc = append(toc, trimmed)
		}
	}
	return toc
}

func extractMarkdownSection(content string, section string) string {
	lines := strings.Split(content, "\n")
	sectionLower := strings.ToLower(strings.TrimSpace(section))
	inFrontmatter := false
	capturing := false
	captureLevel := 0
	var buf strings.Builder
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i == 0 && trimmed == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for _, ch := range trimmed {
				if ch == '#' {
					level++
				} else {
					break
				}
			}
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if capturing {
				if level <= captureLevel {
					break
				}
			}
			if strings.EqualFold(heading, sectionLower) {
				capturing = true
				captureLevel = level
				continue
			}
		}
		if capturing {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	return strings.TrimSpace(buf.String())
}

// ---------------------------------------------------------------------------
// UUID generation
// ---------------------------------------------------------------------------

func generateUUIDv4() (string, error) {
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return "", err
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // variant 1
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}

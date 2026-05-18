package toolruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/support/ints"
)

func (b *Builtins) listFiles(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := stringFrom(call.Input["path"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.list path: %w", err)
	}
	resolvedPath, err := b.resolvePathWithOptions(pathValue, true)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	recursive, err := boolFrom(call.Input["recursive"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.list recursive flag: %w", err)
	}
	limit, err := intFrom(call.Input["limit"], 200)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.list limit: %w", err)
	}
	if limit <= 0 {
		limit = 200
	}

	type entry struct {
		Path  string `json:"path"`
		Name  string `json:"name"`
		Type  string `json:"type"`
		Size  int64  `json:"size,omitempty"`
		IsDir bool   `json:"is_dir"`
	}

	entries := make([]entry, 0)
	appendEntry := func(absPath string, info os.FileInfo) {
		if b.shouldHideFSPath(absPath, info) {
			return
		}
		if len(entries) >= limit {
			return
		}
		entries = append(entries, entry{
			Path:  b.displayPath(absPath),
			Name:  info.Name(),
			Type:  fileKind(info),
			Size:  info.Size(),
			IsDir: info.IsDir(),
		})
	}

	if recursive {
		err = filepath.Walk(resolvedPath, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == resolvedPath {
				return nil
			}
			if b.shouldHideFSPath(path, info) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			appendEntry(path, info)
			if len(entries) >= limit {
				return io.EOF
			}
			return nil
		})
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if err != nil {
			return contextengine.ToolResult{}, err
		}
	} else {
		items, err := os.ReadDir(resolvedPath)
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name() < items[j].Name() })
		for _, item := range items {
			if len(entries) >= limit {
				break
			}
			info, err := item.Info()
			if err != nil {
				return contextengine.ToolResult{}, err
			}
			appendEntry(filepath.Join(resolvedPath, item.Name()), info)
		}
	}

	body, err := json.MarshalIndent(map[string]any{
		"path":      b.displayPath(resolvedPath),
		"recursive": recursive,
		"count":     len(entries),
		"entries":   entries,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) treeFiles(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := stringFrom(call.Input["path"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.tree path: %w", err)
	}
	resolvedPath, err := b.resolvePathWithOptions(pathValue, true)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	maxDepth, err := intFrom(call.Input["max_depth"], 4)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.tree max_depth: %w", err)
	}
	if maxDepth < 0 {
		maxDepth = 0
	}
	limit, err := intFrom(call.Input["limit"], 500)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.tree limit: %w", err)
	}
	if limit <= 0 {
		limit = 500
	}

	type entry struct {
		Path  string `json:"path"`
		Name  string `json:"name"`
		Type  string `json:"type"`
		Depth int    `json:"depth"`
	}

	entries := make([]entry, 0)
	var walk func(string, int) error
	walk = func(current string, depth int) error {
		items, err := os.ReadDir(current)
		if err != nil {
			return err
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name() < items[j].Name() })
		for _, item := range items {
			if len(entries) >= limit {
				return io.EOF
			}
			info, err := item.Info()
			if err != nil {
				return err
			}
			absPath := filepath.Join(current, item.Name())
			if b.shouldHideFSPath(absPath, info) {
				continue
			}
			entries = append(entries, entry{
				Path:  b.displayPath(absPath),
				Name:  item.Name(),
				Type:  fileKind(info),
				Depth: depth,
			})
			if info.IsDir() && depth < maxDepth {
				if err := walk(absPath, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if info.IsDir() {
		if err := walk(resolvedPath, 0); err != nil && err != io.EOF {
			return contextengine.ToolResult{}, err
		}
	} else {
		entries = append(entries, entry{
			Path:  b.displayPath(resolvedPath),
			Name:  info.Name(),
			Type:  fileKind(info),
			Depth: 0,
		})
	}

	body, err := json.MarshalIndent(map[string]any{
		"path":      b.displayPath(resolvedPath),
		"max_depth": maxDepth,
		"count":     len(entries),
		"entries":   entries,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) findFiles(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := stringFrom(call.Input["path"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.find path: %w", err)
	}
	resolvedPath, err := b.resolvePathWithOptions(pathValue, true)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	pattern, err := requiredString(call.Input, "pattern")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	recursive, err := boolFrom(call.Input["recursive"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.find recursive flag: %w", err)
	}
	useGlob, err := boolFrom(call.Input["glob"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.find glob flag: %w", err)
	}
	kind, err := stringFrom(call.Input["kind"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.find kind: %w", err)
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "any"
	}
	limit, err := intFrom(call.Input["limit"], 200)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.find limit: %w", err)
	}
	if limit <= 0 {
		limit = 200
	}

	type match struct {
		Path string `json:"path"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	matches := make([]match, 0)

	record := func(path string, info os.FileInfo) {
		if b.shouldHideFSPath(path, info) {
			return
		}
		if len(matches) >= limit {
			return
		}
		fileType := fileKind(info)
		if kind == "file" && info.IsDir() {
			return
		}
		if kind == "directory" && !info.IsDir() {
			return
		}
		name := info.Name()
		if !matchesPattern(name, pattern, useGlob) && !matchesPattern(b.displayPath(path), pattern, useGlob) {
			return
		}
		matches = append(matches, match{
			Path: b.displayPath(path),
			Name: name,
			Type: fileType,
		})
	}

	if recursive {
		err = filepath.Walk(resolvedPath, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == resolvedPath {
				return nil
			}
			if b.shouldHideFSPath(path, info) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			record(path, info)
			if len(matches) >= limit {
				return io.EOF
			}
			return nil
		})
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if err != nil {
			return contextengine.ToolResult{}, err
		}
	} else {
		entries, err := os.ReadDir(resolvedPath)
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				return contextengine.ToolResult{}, err
			}
			record(filepath.Join(resolvedPath, entry.Name()), info)
			if len(matches) >= limit {
				break
			}
		}
	}

	body, err := json.MarshalIndent(map[string]any{
		"path":      b.displayPath(resolvedPath),
		"pattern":   pattern,
		"glob":      useGlob,
		"recursive": recursive,
		"count":     len(matches),
		"matches":   matches,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) grepFiles(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := stringFrom(call.Input["path"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.grep path: %w", err)
	}
	resolvedPath, err := b.resolvePathWithOptions(pathValue, true)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	pattern, err := requiredString(call.Input, "pattern")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	recursive, err := boolFrom(call.Input["recursive"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.grep recursive flag: %w", err)
	}
	ignoreCase, err := boolFrom(call.Input["ignore_case"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.grep ignore_case flag: %w", err)
	}
	useRegexp, err := boolFrom(call.Input["regexp"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.grep regexp flag: %w", err)
	}
	limit, err := intFrom(call.Input["limit"], 200)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.grep limit: %w", err)
	}
	if limit <= 0 {
		limit = 200
	}

	type match struct {
		Path    string `json:"path"`
		Line    int    `json:"line"`
		Content string `json:"content"`
	}
	matches := make([]match, 0)
	var matcher lineMatcher
	if useRegexp {
		expr := pattern
		if ignoreCase {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("invalid fs.grep regexp: %w", err)
		}
		matcher = regexpMatcher{re: re}
	} else {
		needle := pattern
		if ignoreCase {
			needle = strings.ToLower(needle)
		}
		matcher = containsMatcher{
			needle:     needle,
			ignoreCase: ignoreCase,
		}
	}

	searchFile := func(path string) error {
		if err := b.authorizeAbsolutePath(path, true); err != nil {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if b.shouldHideFSPath(path, info) {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		limited := io.LimitReader(file, int64(b.config.MaxReadBytes))
		scanner := bufio.NewScanner(limited)
		scanner.Buffer(make([]byte, 0, 64*1024), ints.Max(1024*1024, b.config.MaxReadBytes))
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := scanner.Text()
			if matcher.Match(line) {
				matches = append(matches, match{
					Path:    b.displayPath(path),
					Line:    lineNumber,
					Content: line,
				})
				if len(matches) >= limit {
					return io.EOF
				}
			}
		}
		return scanner.Err()
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if !info.IsDir() {
		if err := searchFile(resolvedPath); err != nil && err != io.EOF {
			return contextengine.ToolResult{}, err
		}
	} else if recursive {
		err = filepath.Walk(resolvedPath, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				if path != resolvedPath && b.shouldHideFSPath(path, info) {
					return filepath.SkipDir
				}
				return nil
			}
			return searchFile(path)
		})
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if err != nil {
			return contextengine.ToolResult{}, err
		}
	} else {
		entries, err := os.ReadDir(resolvedPath)
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			err := searchFile(filepath.Join(resolvedPath, entry.Name()))
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return contextengine.ToolResult{}, err
			}
		}
	}

	body, err := json.MarshalIndent(map[string]any{
		"path":        b.displayPath(resolvedPath),
		"pattern":     pattern,
		"regexp":      useRegexp,
		"ignore_case": ignoreCase,
		"recursive":   recursive,
		"count":       len(matches),
		"matches":     matches,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) shouldHideFSPath(absPath string, info os.FileInfo) bool {
	if info != nil && info.IsDir() && b.isSkippedDir(info.Name()) {
		return true
	}
	if err := b.authorizeAbsolutePath(absPath, true); err != nil {
		return true
	}
	return b.isDeniedPath(absPath)
}

func (b *Builtins) isSkippedDir(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, skip := range b.config.FSConstraints.SkipDirs {
		if strings.EqualFold(strings.TrimSpace(skip), name) {
			return true
		}
	}
	return false
}

func (b *Builtins) hashFile(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolvedPath, err := b.resolvePath(pathValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if info.IsDir() {
		return contextengine.ToolResult{}, fmt.Errorf("fs.hash requires a file path")
	}
	algorithm, err := stringFrom(call.Input["algorithm"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.hash algorithm: %w", err)
	}
	hasher, normalized, err := newHasher(algorithm)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	file, err := os.Open(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	defer file.Close()
	if _, err := io.Copy(hasher, file); err != nil {
		return contextengine.ToolResult{}, err
	}
	body, err := json.MarshalIndent(map[string]any{
		"path":      b.displayPath(resolvedPath),
		"algorithm": normalized,
		"hash":      fmt.Sprintf("%x", hasher.Sum(nil)),
		"size":      info.Size(),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) readFile(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	if strings.HasPrefix(strings.TrimSpace(pathValue), "artifact://") {
		if b.artifactStore == nil {
			return contextengine.ToolResult{}, fmt.Errorf("artifact store is not configured")
		}
		data, _, err := b.artifactStore.Read(context.Background(), pathValue)
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		if b.config.MaxReadBytes > 0 && len(data) > b.config.MaxReadBytes {
			data = data[:b.config.MaxReadBytes]
		}
		body, err := json.MarshalIndent(map[string]any{
			"path":    pathValue,
			"content": string(data),
		}, "", "  ")
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		return contextengine.ToolResult{
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Content:    string(body),
		}, nil
	}

	resolvedPath, err := b.resolvePath(pathValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	file, err := os.Open(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	defer file.Close()

	offset, err := int64From(call.Input["offset"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.read offset: %w", err)
	}
	if offset < 0 {
		return contextengine.ToolResult{}, fmt.Errorf("fs.read offset must be >= 0")
	}
	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return contextengine.ToolResult{}, err
		}
	}

	limit, err := int64From(call.Input["limit"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.read limit: %w", err)
	}
	if limit <= 0 || limit > int64(b.config.MaxReadBytes) {
		limit = int64(b.config.MaxReadBytes)
	}
	content, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	displayPath := b.displayPath(resolvedPath)
	body, err := json.MarshalIndent(map[string]any{
		"path":    displayPath,
		"content": string(content),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) statFile(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolvedPath, err := b.resolvePath(pathValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	body, err := json.MarshalIndent(map[string]any{
		"path":      b.displayPath(resolvedPath),
		"name":      info.Name(),
		"type":      fileKind(info),
		"is_dir":    info.IsDir(),
		"size":      info.Size(),
		"mode":      info.Mode().String(),
		"mod_time":  info.ModTime().UTC().Format(time.RFC3339Nano),
		"workspace": filepath.ToSlash(b.rootAbs),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) writeFile(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	contentValue, err := requiredString(call.Input, "content")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolvedPath, err := b.resolvePath(pathValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o755); err != nil {
		return contextengine.ToolResult{}, err
	}

	appendMode, err := boolFrom(call.Input["append"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.write append flag: %w", err)
	}
	if appendMode {
		file, err := os.OpenFile(resolvedPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return contextengine.ToolResult{}, err
		}
		defer file.Close()
		if _, err := io.Copy(file, strings.NewReader(contentValue)); err != nil {
			return contextengine.ToolResult{}, err
		}
	} else {
		if err := atomicWriteFile(resolvedPath, []byte(contentValue), 0o644); err != nil {
			return contextengine.ToolResult{}, err
		}
	}

	displayPath := b.displayPath(resolvedPath)
	body, err := json.MarshalIndent(map[string]any{
		"path":          displayPath,
		"workspace":     filepath.ToSlash(b.rootAbs),
		"bytes_written": len(contentValue),
		"append":        appendMode,
		"message":       fmt.Sprintf("wrote %d bytes to %s", len(contentValue), displayPath),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (b *Builtins) editFile(call agent.ToolCall) (contextengine.ToolResult, error) {
	pathValue, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	oldText, err := requiredString(call.Input, "old_text")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	newText, err := stringFrom(call.Input["new_text"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.edit new_text: %w", err)
	}
	replaceAll, err := boolFrom(call.Input["replace_all"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid fs.edit replace_all flag: %w", err)
	}
	resolvedPath, err := b.resolvePath(pathValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	text := string(content)

	matchMode := "exact"

	if !strings.Contains(text, oldText) {
		lineStart, _ := intFrom(call.Input["line_start"], 0)
		lineEnd, _ := intFrom(call.Input["line_end"], 0)
		if lineStart > 0 {
			result, lineErr := editByLineRange(text, oldText, newText, lineStart, lineEnd)
			if lineErr == nil {
				text = result
				matchMode = "line_range"
				goto applyEdit
			}
		}

		normalizedIdx := findNormalizedMatch(text, oldText)
		if normalizedIdx >= 0 {
			actualOld := extractActualMatch(text, oldText, normalizedIdx)
			if replaceAll {
				text = strings.ReplaceAll(text, actualOld, newText)
			} else {
				text = strings.Replace(text, actualOld, newText, 1)
			}
			matchMode = "normalized"
			goto applyEdit
		}

		return contextengine.ToolResult{}, editDiagnosticError(
			b.displayPath(resolvedPath), text, oldText,
		)
	}

	if replaceAll {
		text = strings.ReplaceAll(text, oldText, newText)
	} else {
		text = strings.Replace(text, oldText, newText, 1)
	}

applyEdit:
	if err := atomicWriteFile(resolvedPath, []byte(text), 0o644); err != nil {
		return contextengine.ToolResult{}, err
	}
	body, err := json.MarshalIndent(map[string]any{
		"path":       b.displayPath(resolvedPath),
		"workspace":  filepath.ToSlash(b.rootAbs),
		"match_mode": matchMode,
		"message":    fmt.Sprintf("updated %s (match: %s)", b.displayPath(resolvedPath), matchMode),
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func normalizeWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		lines[i] = strings.Join(fields, " ")
	}
	return strings.Join(lines, "\n")
}

func findNormalizedMatch(text, target string) int {
	normTarget := normalizeWhitespace(target)
	normText := normalizeWhitespace(text)
	return strings.Index(normText, normTarget)
}

func extractActualMatch(text, target string, normIdx int) string {
	targetLines := strings.Split(target, "\n")
	textLines := strings.Split(text, "\n")

	normTargetFirst := strings.TrimSpace(targetLines[0])
	startLine := -1
	for i, line := range textLines {
		if strings.Contains(strings.TrimSpace(line), normTargetFirst) {
			startLine = i
			break
		}
	}
	if startLine < 0 {
		endIdx := normIdx + len(target)
		if endIdx > len(text) {
			endIdx = len(text)
		}
		return text[normIdx:endIdx]
	}

	endLine := startLine + len(targetLines)
	if endLine > len(textLines) {
		endLine = len(textLines)
	}

	return strings.Join(textLines[startLine:endLine], "\n")
}

func editByLineRange(text, oldText, newText string, lineStart, lineEnd int) (string, error) {
	lines := strings.Split(text, "\n")
	if lineStart < 1 || lineStart > len(lines) {
		return "", fmt.Errorf("line_start %d out of range (file has %d lines)", lineStart, len(lines))
	}
	if lineEnd <= 0 {
		oldLines := strings.Split(oldText, "\n")
		lineEnd = lineStart + len(oldLines) - 1
	}
	if lineEnd > len(lines) {
		lineEnd = len(lines)
	}
	if lineEnd < lineStart {
		return "", fmt.Errorf("line_end %d < line_start %d", lineEnd, lineStart)
	}

	before := lines[:lineStart-1]
	after := lines[lineEnd:]
	newLines := strings.Split(newText, "\n")

	result := make([]string, 0, len(before)+len(newLines)+len(after))
	result = append(result, before...)
	result = append(result, newLines...)
	result = append(result, after...)

	return strings.Join(result, "\n"), nil
}

func editDiagnosticError(displayPath, fileText, oldText string) error {
	oldLines := strings.Split(oldText, "\n")
	fileLines := strings.Split(fileText, "\n")
	firstOldLine := strings.TrimSpace(oldLines[0])

	bestLine := 0
	bestScore := 0
	for i, line := range fileLines {
		trimmed := strings.TrimSpace(line)
		score := longestCommonSubstring(trimmed, firstOldLine)
		if score > bestScore {
			bestScore = score
			bestLine = i + 1
		}
	}

	hint := ""
	if bestScore > len(firstOldLine)/2 {
		start := bestLine - 1
		end := bestLine + len(oldLines)
		if start < 0 {
			start = 0
		}
		if end > len(fileLines) {
			end = len(fileLines)
		}
		actual := strings.Join(fileLines[start:end], "\n")
		hint = fmt.Sprintf(
			"\n\nClosest match found near line %d:\n```\n%s\n```\n\n"+
				"Tip: use fs.read to get the exact current content, or use line_start=%d with the correct text.",
			bestLine, actual, bestLine,
		)
	} else {
		hint = "\n\nTip: the file content may have changed. Use fs.read to get the current content before editing."
	}

	return fmt.Errorf("fs.edit could not find target text in %s%s", displayPath, hint)
}

func longestCommonSubstring(a, b string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	const maxLen = 500
	if len(a) > maxLen {
		a = a[:maxLen]
	}
	if len(b) > maxLen {
		b = b[:maxLen]
	}
	maxSoFar := 0
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for i := range a {
		for j := range b {
			if a[i] == b[j] {
				curr[j+1] = prev[j] + 1
				if curr[j+1] > maxSoFar {
					maxSoFar = curr[j+1]
				}
			} else {
				curr[j+1] = 0
			}
		}
		prev, curr = curr, prev
		for k := range curr {
			curr[k] = 0
		}
	}
	return maxSoFar
}

package toolruntime

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var hunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type unifiedPatch struct {
	Files []patchFile
}

type patchFile struct {
	OldPath string
	NewPath string
	Hunks   []patchHunk
}

type patchHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []patchLine
}

type patchLine struct {
	Kind       byte
	Text       string
	HasNewline bool
}

type textLine struct {
	Text       string
	HasNewline bool
}

func parseUnifiedPatch(input string) (*unifiedPatch, error) {
	lines, err := scanLines(input)
	if err != nil {
		return nil, err
	}
	patch := &unifiedPatch{}
	for i := 0; i < len(lines); {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "diff --git "):
			i++
		case strings.HasPrefix(line, "--- "):
			file, next, err := parsePatchFile(lines, i)
			if err != nil {
				return nil, err
			}
			patch.Files = append(patch.Files, file)
			i = next
		default:
			i++
		}
	}
	if len(patch.Files) == 0 {
		return nil, fmt.Errorf("fs.patch requires at least one unified diff file")
	}
	return patch, nil
}

func parsePatchFile(lines []string, start int) (patchFile, int, error) {
	if start+1 >= len(lines) {
		return patchFile{}, 0, fmt.Errorf("invalid unified diff: missing file headers")
	}
	oldPath, err := parsePatchPath(lines[start], "--- ")
	if err != nil {
		return patchFile{}, 0, err
	}
	newPath, err := parsePatchPath(lines[start+1], "+++ ")
	if err != nil {
		return patchFile{}, 0, err
	}
	file := patchFile{OldPath: oldPath, NewPath: newPath}
	i := start + 2
	for i < len(lines) {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "@@ "):
			hunk, next, err := parsePatchHunk(lines, i)
			if err != nil {
				return patchFile{}, 0, err
			}
			file.Hunks = append(file.Hunks, hunk)
			i = next
		case strings.HasPrefix(line, "diff --git "), strings.HasPrefix(line, "--- "):
			return file, i, nil
		default:
			i++
		}
	}
	return file, i, nil
}

func parsePatchPath(line, prefix string) (string, error) {
	if !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf("invalid unified diff header %q", line)
	}
	path := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if idx := strings.IndexByte(path, '\t'); idx >= 0 {
		path = path[:idx]
	}
	if path == "/dev/null" {
		return "", nil
	}
	return path, nil
}

func parsePatchHunk(lines []string, start int) (patchHunk, int, error) {
	header := hunkHeaderPattern.FindStringSubmatch(lines[start])
	if header == nil {
		return patchHunk{}, 0, fmt.Errorf("invalid unified diff hunk header %q", lines[start])
	}
	oldStart, err := parsePatchInt(header[1], 0)
	if err != nil {
		return patchHunk{}, 0, err
	}
	oldCount, err := parsePatchInt(header[2], 1)
	if err != nil {
		return patchHunk{}, 0, err
	}
	newStart, err := parsePatchInt(header[3], 0)
	if err != nil {
		return patchHunk{}, 0, err
	}
	newCount, err := parsePatchInt(header[4], 1)
	if err != nil {
		return patchHunk{}, 0, err
	}
	hunk := patchHunk{
		OldStart: oldStart,
		OldCount: oldCount,
		NewStart: newStart,
		NewCount: newCount,
	}
	i := start + 1
	for i < len(lines) {
		line := lines[i]
		if strings.HasPrefix(line, "@@ ") || strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "--- ") {
			break
		}
		if line == `\ No newline at end of file` {
			if len(hunk.Lines) == 0 {
				return patchHunk{}, 0, fmt.Errorf(`invalid unified diff: stray "\ No newline at end of file"`)
			}
			hunk.Lines[len(hunk.Lines)-1].HasNewline = false
			i++
			continue
		}
		if len(line) == 0 {
			return patchHunk{}, 0, fmt.Errorf("invalid unified diff: empty hunk line")
		}
		switch line[0] {
		case ' ', '+', '-':
			hunk.Lines = append(hunk.Lines, patchLine{
				Kind:       line[0],
				Text:       line[1:],
				HasNewline: true,
			})
		default:
			return patchHunk{}, 0, fmt.Errorf("invalid unified diff hunk line %q", line)
		}
		i++
	}
	return hunk, i, nil
}

func parsePatchInt(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	return intFrom(raw, fallback)
}

func scanLines(input string) ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(input))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	lines := make([]string, 0)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func (b *Builtins) applyUnifiedPatch(ctx context.Context, patchText string, strip int, reverse bool) ([]string, error) {
	patch, err := parseUnifiedPatch(patchText)
	if err != nil {
		return nil, err
	}
	applied := make([]string, 0, len(patch.Files))
	for _, filePatch := range patch.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := b.applyPatchFile(ctx, filePatch, strip, reverse)
		if err != nil {
			return nil, err
		}
		if path != "" && !containsString(applied, path) {
			applied = append(applied, path)
		}
	}
	return applied, nil
}

func (b *Builtins) applyPatchFile(ctx context.Context, filePatch patchFile, strip int, reverse bool) (string, error) {
	sourcePath := filePatch.OldPath
	targetPath := filePatch.NewPath
	if reverse {
		sourcePath, targetPath = targetPath, sourcePath
	}
	sourcePath, err := stripPatchPath(sourcePath, strip)
	if err != nil {
		return "", err
	}
	targetPath, err = stripPatchPath(targetPath, strip)
	if err != nil {
		return "", err
	}
	if sourcePath == "" && targetPath == "" {
		return "", fmt.Errorf("invalid unified diff: empty source and target paths")
	}

	var sourceAbs string
	var original []textLine
	if sourcePath != "" {
		sourceAbs, err = b.resolvePath(sourcePath)
		if err != nil {
			return "", err
		}
		original, err = readTextLines(sourceAbs)
		if err != nil {
			return "", err
		}
	}

	result, err := applyPatchHunks(ctx, original, filePatch.Hunks, reverse, displayPatchPath(sourcePath, targetPath))
	if err != nil {
		return "", err
	}

	targetAbs := sourceAbs
	if targetPath != "" {
		targetAbs, err = b.resolvePath(targetPath)
		if err != nil {
			return "", err
		}
	}

	if targetPath == "" {
		if sourceAbs == "" {
			return "", fmt.Errorf("invalid delete patch without source path")
		}
		if err := os.Remove(sourceAbs); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return sourcePath, nil
	}

	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		return "", err
	}
	if err := atomicWriteFile(targetAbs, joinTextLines(result), 0o644); err != nil {
		return "", err
	}
	if sourceAbs != "" && targetAbs != sourceAbs {
		if err := os.Remove(sourceAbs); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return targetPath, nil
}

func stripPatchPath(path string, strip int) (string, error) {
	if path == "" {
		return "", nil
	}
	if strip < 0 {
		return "", fmt.Errorf("strip must be >= 0")
	}
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	parts := strings.Split(path, "/")
	if strip > len(parts) {
		return "", fmt.Errorf("patch path %q cannot be stripped by %d", path, strip)
	}
	if strip == len(parts) && len(parts) == 1 {
		return filepath.Clean(parts[0]), nil
	}
	path = strings.Join(parts[strip:], "/")
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("patch path %q becomes empty after strip=%d", strings.Join(parts, "/"), strip)
	}
	return filepath.Clean(path), nil
}

func displayPatchPath(sourcePath, targetPath string) string {
	if targetPath != "" {
		return targetPath
	}
	return sourcePath
}

func readTextLines(path string) ([]textLine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return splitTextLines(string(data)), nil
}

func splitTextLines(content string) []textLine {
	if content == "" {
		return nil
	}
	parts := strings.SplitAfter(content, "\n")
	lines := make([]textLine, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		hasNewline := strings.HasSuffix(part, "\n")
		if hasNewline {
			part = strings.TrimSuffix(part, "\n")
		}
		lines = append(lines, textLine{
			Text:       part,
			HasNewline: hasNewline,
		})
	}
	return lines
}

func joinTextLines(lines []textLine) []byte {
	var builder strings.Builder
	for _, line := range lines {
		builder.WriteString(line.Text)
		if line.HasNewline {
			builder.WriteByte('\n')
		}
	}
	return []byte(builder.String())
}

func applyPatchHunks(ctx context.Context, original []textLine, hunks []patchHunk, reverse bool, path string) ([]textLine, error) {
	cursor := 0
	out := make([]textLine, 0, len(original))
	for _, hunk := range hunks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sourceStart := hunk.OldStart
		if reverse {
			sourceStart = hunk.NewStart
		}
		sourceIndex := sourceStart - 1
		if sourceStart == 0 {
			sourceIndex = 0
		}
		if sourceIndex < cursor || sourceIndex > len(original) {
			return nil, fmt.Errorf("fs.patch hunk for %s is out of range", path)
		}
		out = append(out, cloneTextLines(original[cursor:sourceIndex])...)
		cursor = sourceIndex
		for _, line := range hunk.Lines {
			kind := line.Kind
			if reverse {
				switch kind {
				case '+':
					kind = '-'
				case '-':
					kind = '+'
				}
			}
			switch kind {
			case ' ':
				if cursor >= len(original) || !sameTextLine(original[cursor], line) {
					return nil, fmt.Errorf("fs.patch context mismatch for %s", path)
				}
				out = append(out, cloneTextLine(original[cursor]))
				cursor++
			case '-':
				if cursor >= len(original) || !sameTextLine(original[cursor], line) {
					return nil, fmt.Errorf("fs.patch remove mismatch for %s", path)
				}
				cursor++
			case '+':
				out = append(out, textLine{
					Text:       line.Text,
					HasNewline: line.HasNewline,
				})
			default:
				return nil, fmt.Errorf("fs.patch encountered unsupported line kind %q", string(kind))
			}
		}
	}
	out = append(out, cloneTextLines(original[cursor:])...)
	return out, nil
}

func sameTextLine(fileLine textLine, patchLine patchLine) bool {
	return fileLine.Text == patchLine.Text && fileLine.HasNewline == patchLine.HasNewline
}

func cloneTextLines(lines []textLine) []textLine {
	if len(lines) == 0 {
		return nil
	}
	out := make([]textLine, len(lines))
	copy(out, lines)
	return out
}

func cloneTextLine(line textLine) textLine {
	return textLine{
		Text:       line.Text,
		HasNewline: line.HasNewline,
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

package repl

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

type composerCompleter struct {
	registry      *CommandRegistry
	cwd           string
	root          string
	modelNamesFn  func() []string
	remoteNamesFn func() []string
}

func newComposerCompleter(registry *CommandRegistry) *composerCompleter {
	cwd, _ := os.Getwd()
	root := gitRootOrCWD(cwd)
	return &composerCompleter{
		registry: registry,
		cwd:      cwd,
		root:     root,
	}
}

func (c *composerCompleter) AttachmentCandidates(query string) []richedit.CompletionItem {
	root := defaultString(c.root, c.cwd)
	if root == "" {
		return nil
	}
	query = strings.ToLower(strings.TrimSpace(query))
	type candidate struct {
		item     richedit.CompletionItem
		isDir    bool
		inCWD    bool
		recent   time.Time
		basename string
	}
	items := make([]candidate, 0, 64)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == root {
			return nil
		}
		name := d.Name()
		if d.IsDir() && name == ".git" {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		lowerRel := strings.ToLower(rel)
		lowerName := strings.ToLower(name)
		if query != "" && !strings.Contains(lowerRel, query) && !strings.Contains(lowerName, query) {
			if d.IsDir() {
				return nil
			}
			return nil
		}
		item := richedit.CompletionItem{
			Kind:   classifyCompletionKind(path, d.IsDir()),
			Label:  completionLabel(path, rel, d.IsDir()),
			Detail: rel,
			Path:   path,
		}
		info, statErr := d.Info()
		modTime := time.Time{}
		if statErr == nil {
			modTime = info.ModTime()
		}
		items = append(items, candidate{
			item:     item,
			isDir:    d.IsDir(),
			inCWD:    isPathWithin(c.cwd, path),
			recent:   modTime,
			basename: lowerName,
		})
		if len(items) >= 256 {
			return fs.SkipAll
		}
		return nil
	})

	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.inCWD != b.inCWD {
			return a.inCWD
		}
		if a.isDir != b.isDir {
			return !a.isDir
		}
		if query != "" {
			aPrefix := strings.HasPrefix(a.basename, query)
			bPrefix := strings.HasPrefix(b.basename, query)
			if aPrefix != bPrefix {
				return aPrefix
			}
		}
		if !a.recent.Equal(b.recent) {
			return a.recent.After(b.recent)
		}
		return strings.Compare(a.item.Detail, b.item.Detail) < 0
	})

	out := make([]richedit.CompletionItem, 0, min(len(items), 8))
	for _, item := range items {
		out = append(out, item.item)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func (c *composerCompleter) CompletePath(prefix string) (string, bool) {
	original := strings.TrimSpace(prefix)
	resolved, displayBase := resolveCompletionPrefix(prefix)
	if displayBase == "" && !filepath.IsAbs(resolved) {
		resolved = filepath.Join(c.cwd, resolved)
	}
	dir := resolved
	namePrefix := ""
	if !strings.HasSuffix(resolved, string(os.PathSeparator)) {
		dir = filepath.Dir(resolved)
		namePrefix = filepath.Base(resolved)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	matches := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if namePrefix == "" || strings.HasPrefix(strings.ToLower(name), strings.ToLower(namePrefix)) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return "", false
	}
	sort.Strings(matches)
	resolvedMatch := filepath.Join(dir, commonPrefixStrings(matches))
	if len(matches) == 1 {
		resolvedMatch = filepath.Join(dir, matches[0])
	}
	if info, err := os.Stat(resolvedMatch); err == nil && info.IsDir() && !strings.HasSuffix(resolvedMatch, string(os.PathSeparator)) {
		resolvedMatch += string(os.PathSeparator)
	}
	if displayBase == "" && !filepath.IsAbs(original) {
		rel, err := filepath.Rel(c.cwd, resolvedMatch)
		if err == nil {
			trailingSlash := strings.HasSuffix(resolvedMatch, string(os.PathSeparator))
			rel = filepath.ToSlash(rel)
			if trailingSlash && !strings.HasSuffix(rel, "/") {
				rel += "/"
			}
			switch {
			case strings.HasPrefix(original, "./") && !strings.HasPrefix(rel, "./"):
				return "./" + rel, true
			case strings.HasPrefix(original, "../") && !strings.HasPrefix(rel, "../"):
				return "../" + rel, true
			default:
				return rel, true
			}
		}
	}
	return restoreCompletionPrefix(resolvedMatch, displayBase), true
}

func (c *composerCompleter) CompleteSlash(linePrefix, currentArg string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(linePrefix))
	if len(fields) == 0 {
		return "", false
	}
	switch fields[0] {
	case "/remote":
		return completeFromSet(currentArg, append([]string{"local", "list", "login", "logout"}, c.remoteChoiceNames()...))
	case "/badge":
		if len(fields) >= 2 && fields[1] == "set" {
			return completeFromSet(currentArg, badgeChoices())
		}
	case "/tools":
		if len(fields) == 1 {
			return completeFromSet(currentArg, []string{"list", "search", "info", "check"})
		}
	case "/skills":
		if len(fields) == 1 {
			return completeFromSet(currentArg, []string{"list", "search", "info", "install", "remove"})
		}
		if len(fields) >= 2 && strings.EqualFold(strings.TrimSpace(fields[1]), "install") {
			return c.CompletePath(currentArg)
		}
	case "/cd":
		return c.CompletePath(currentArg)
	case "/attach":
		if len(fields) == 1 {
			return completeFromSet(currentArg, []string{"image", "file", "dir", "video"})
		}
		switch strings.ToLower(strings.TrimSpace(fields[1])) {
		case "image", "file", "dir", "video":
			return c.CompletePath(currentArg)
		default:
			return completeFromSet(currentArg, []string{"image", "file", "dir", "video"})
		}
	case "/model":
		return completeFromSet(currentArg, c.modelChoiceNames())
	}
	return "", false
}

func gitRootOrCWD(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	dir := cwd
	for {
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info != nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

func classifyCompletionKind(path string, isDir bool) richedit.TokenKind {
	if isDir {
		return richedit.TokenDir
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return richedit.TokenImage
	case ".mp4", ".mov", ".mkv", ".avi", ".webm":
		return richedit.TokenVideo
	default:
		return richedit.TokenFile
	}
}

func completionLabel(path, rel string, isDir bool) string {
	if isDir {
		return rel
	}
	return filepath.Base(path)
}

func isPathWithin(base, path string) bool {
	base = strings.TrimSpace(base)
	path = strings.TrimSpace(path)
	if base == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
}

func resolveCompletionPrefix(prefix string) (resolved string, displayBase string) {
	displayBase = prefix
	prefix = strings.TrimSpace(prefix)
	if strings.HasPrefix(prefix, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, prefix[2:]), "~/"
		}
	}
	return prefix, ""
}

func restoreCompletionPrefix(path, displayBase string) string {
	if displayBase == "~/" {
		home, err := os.UserHomeDir()
		if err == nil && strings.HasPrefix(path, home) {
			trimmed := strings.TrimPrefix(path, home)
			trimmed = strings.TrimPrefix(trimmed, string(os.PathSeparator))
			return "~/" + filepath.ToSlash(trimmed)
		}
	}
	return path
}

func commonPrefixStrings(items []string) string {
	if len(items) == 0 {
		return ""
	}
	prefix := items[0]
	for _, item := range items[1:] {
		prefix = commonPrefix(prefix, item)
		if prefix == "" {
			return ""
		}
	}
	return prefix
}

func completeFromSet(current string, choices []string) (string, bool) {
	current = strings.TrimSpace(current)
	if current == "" {
		if len(choices) == 1 {
			return choices[0], true
		}
		return "", false
	}
	matches := make([]string, 0, len(choices))
	for _, choice := range choices {
		if strings.HasPrefix(strings.ToLower(choice), strings.ToLower(current)) {
			matches = append(matches, choice)
		}
	}
	if len(matches) == 0 {
		return "", false
	}
	sort.Strings(matches)
	if len(matches) == 1 {
		return matches[0], true
	}
	prefix := commonPrefixStrings(matches)
	if prefix == "" || prefix == current {
		return "", false
	}
	return prefix, true
}

func badgeChoices() []string {
	choices := make([]string, 0, 50)
	for ch := 'A'; ch <= 'Z'; ch++ {
		choices = append(choices, string(ch))
	}
	for i := 0; i < 24; i++ {
		choices = append(choices, "custom-"+strconv.Itoa(i))
	}
	return choices
}

func (c *composerCompleter) modelChoiceNames() []string {
	if c == nil || c.modelNamesFn == nil {
		return nil
	}
	return dedupeChoices(c.modelNamesFn())
}

func (c *composerCompleter) remoteChoiceNames() []string {
	if c == nil || c.remoteNamesFn == nil {
		return nil
	}
	return dedupeChoices(c.remoteNamesFn())
}

func dedupeChoices(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

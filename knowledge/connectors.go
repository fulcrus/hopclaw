package knowledge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxSourceReadBytes   = 256 * 1024
	chunkRuneLimit       = 1200
	chunkOverlapRuneSize = 120
)

type Connector interface {
	Sync(ctx context.Context, source Source) ([]DocumentSnapshot, error)
}

type LocalDirConnector struct{}

func (c *LocalDirConnector) Sync(ctx context.Context, source Source) ([]DocumentSnapshot, error) {
	root := strings.TrimSpace(source.Path)
	if root == "" {
		return nil, fmt.Errorf("path is required")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path must be a directory")
	}
	return syncDirectory(ctx, source, root, false)
}

type GitRepoConnector struct{}

func (c *GitRepoConnector) Sync(ctx context.Context, source Source) ([]DocumentSnapshot, error) {
	root := strings.TrimSpace(source.Path)
	if root == "" {
		return nil, fmt.Errorf("path is required")
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return nil, fmt.Errorf("git repository not found at %s", root)
	}
	return syncDirectory(ctx, source, root, true)
}

type WebURLConnector struct {
	Client *http.Client
}

func (c *WebURLConnector) Sync(ctx context.Context, source Source) ([]DocumentSnapshot, error) {
	urls := uniqueStrings(source.URLs)
	if len(urls) == 0 {
		return nil, fmt.Errorf("urls are required")
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	snapshots := make([]DocumentSnapshot, 0, len(urls))
	for _, rawURL := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request for %s: %w", rawURL, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxSourceReadBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", rawURL, readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("fetch %s returned status %d", rawURL, resp.StatusCode)
		}
		content := normalizeChunkContent(string(body))
		snapshots = append(snapshots, buildDocumentSnapshot(Document{
			ID:              rawURL,
			SourceID:        source.ID,
			Kind:            DocumentKindWebPage,
			Title:           rawURL,
			Path:            rawURL,
			URI:             rawURL,
			Locale:          source.Locale,
			Bytes:           int64(len(body)),
			SourceUpdatedAt: time.Now().UTC(),
			Metadata: DocumentMetadata{
				MIMEType: strings.TrimSpace(resp.Header.Get("Content-Type")),
				ETag:     strings.TrimSpace(resp.Header.Get("ETag")),
			},
		}, content))
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Document.ID < snapshots[j].Document.ID
	})
	return snapshots, nil
}

func syncDirectory(ctx context.Context, source Source, root string, skipGit bool) ([]DocumentSnapshot, error) {
	snapshots := make([]DocumentSnapshot, 0, 64)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == ".hopclaw" || name == "node_modules" || name == ".idea" || name == ".vscode" {
				return filepath.SkipDir
			}
			if skipGit && name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !supportedKnowledgeFile(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if shouldSkipByGlobs(rel, source.ExcludeGlobs) {
			return nil
		}
		if len(source.IncludeGlobs) > 0 && !matchesAnyGlob(rel, source.IncludeGlobs) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(body) > maxSourceReadBytes {
			body = body[:maxSourceReadBytes]
		}
		content := normalizeChunkContent(string(body))
		if strings.TrimSpace(content) == "" {
			return nil
		}
		snapshots = append(snapshots, buildDocumentSnapshot(Document{
			ID:              rel,
			SourceID:        source.ID,
			Kind:            DocumentKindFile,
			Title:           filepath.Base(path),
			Path:            rel,
			URI:             path,
			Locale:          source.Locale,
			Bytes:           int64(len(body)),
			SourceUpdatedAt: info.ModTime().UTC(),
			Metadata: DocumentMetadata{
				Extension: strings.ToLower(filepath.Ext(path)),
			},
		}, content))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].Document.Path != snapshots[j].Document.Path {
			return snapshots[i].Document.Path < snapshots[j].Document.Path
		}
		return snapshots[i].Document.ID < snapshots[j].Document.ID
	})
	return snapshots, nil
}

func supportedKnowledgeFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".txt", ".json", ".yaml", ".yml", ".csv", ".html", ".htm", ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".css", ".sql", ".xml", ".toml", ".sh":
		return true
	default:
		return false
	}
}

func normalizeChunkContent(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func buildPreview(content string) string {
	content = strings.TrimSpace(strings.ReplaceAll(content, "\n", " "))
	runes := []rune(content)
	if len(runes) <= 220 {
		return content
	}
	return strings.TrimSpace(string(runes[:219])) + "…"
}

func shouldSkipByGlobs(path string, globs []string) bool {
	return matchesAnyGlob(path, globs)
}

func matchesAnyGlob(path string, globs []string) bool {
	for _, pattern := range globs {
		matched, err := filepath.Match(pattern, path)
		if err == nil && matched {
			return true
		}
	}
	return false
}

package skill

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/logging"
)

// FileClawHubClient implements ClawHubClient using a local index directory.
// Skills are discovered from JSON manifest files in the index directory.
// This serves as the default implementation when no remote hub is configured,
// and also as the local cache for a remote hub.
type FileClawHubClient struct {
	Layout     ClawHubLayout
	HTTPClient *http.Client // optional, for remote hub
	BaseURL    string       // optional, e.g. "https://hub.openclaw.dev/v1"
	AuthToken  string       // optional bearer token for remote hub
	Sources    []string     // optional local directories or remote JSON catalogs
}

// NewFileClawHubClient creates a ClawHub client backed by the local filesystem.
func NewFileClawHubClient(root string) *FileClawHubClient {
	return &FileClawHubClient{
		Layout: ClawHubLayout{Root: root},
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// catalogEntry is the on-disk format for a skill in the index.
type catalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	BundleURL   string `json:"bundle_url,omitempty"` // remote download URL
	BundleDir   string `json:"bundle_dir,omitempty"` // local bundle path
}

// Search returns skills matching the query from the local index + remote hub.
func (c *FileClawHubClient) Search(ctx context.Context, query string) ([]RegistrySkill, error) {
	var results []RegistrySkill

	// 1. Search local index directory.
	indexDir := c.Layout.IndexDir()
	entries, err := c.loadLocalIndex(indexDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load local index: %w", err)
	}

	query = strings.ToLower(strings.TrimSpace(query))
	for _, e := range entries {
		if query == "" || matchesQuery(e, query) {
			results = append(results, RegistrySkill{
				ID:        e.ID,
				Name:      e.Name,
				Version:   e.Version,
				Summary:   e.Summary,
				BundleURL: e.BundleURL,
				BundleDir: e.BundleDir,
			})
		}
	}

	// 2. Search configured directory/JSON sources.
	if fromSources, err := c.searchConfiguredSources(ctx, query); err == nil {
		seen := make(map[string]bool, len(results))
		for _, r := range results {
			seen[strings.ToLower(strings.TrimSpace(r.ID))] = true
		}
		for _, item := range fromSources {
			key := strings.ToLower(strings.TrimSpace(item.ID))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, item)
		}
	}

	// 3. Try remote hub if configured.
	if c.BaseURL != "" {
		remote, err := c.searchRemote(ctx, query)
		if err == nil {
			// Merge, preferring local entries for same ID.
			seen := make(map[string]bool, len(results))
			for _, r := range results {
				seen[r.ID] = true
			}
			for _, r := range remote {
				if !seen[r.ID] {
					results = append(results, r)
				}
			}
		}
		// Ignore remote errors — local index is sufficient.
	}

	return results, nil
}

func (c *FileClawHubClient) searchConfiguredSources(ctx context.Context, query string) ([]RegistrySkill, error) {
	if len(c.Sources) == 0 {
		return nil, nil
	}
	var merged []RegistrySkill
	seen := make(map[string]struct{})
	var firstErr error
	for _, source := range c.Sources {
		results, err := c.loadSource(ctx, source, query)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, item := range results {
			key := strings.ToLower(strings.TrimSpace(item.ID))
			if key == "" {
				key = strings.ToLower(strings.TrimSpace(item.Name))
			}
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, item)
		}
	}
	if len(merged) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

// Install downloads and installs a skill by ID.
func (c *FileClawHubClient) Install(ctx context.Context, req InstallRequest) (*InstallResult, error) {
	if req.SkillID == "" {
		return nil, fmt.Errorf("skill ID is required")
	}

	// Find the skill in catalog.
	entry, err := c.findSkill(ctx, req.SkillID)
	if err != nil {
		return nil, err
	}

	version := req.Version
	if version == "" {
		version = entry.Version
	}
	if version == "" {
		version = "latest"
	}

	// Resolve bundle directory.
	bundleDir := entry.BundleDir
	if bundleDir == "" && entry.BundleURL != "" {
		// Download from remote.
		bundleDir, err = c.downloadBundle(ctx, entry.BundleURL, req.SkillID, version)
		if err != nil {
			return nil, fmt.Errorf("download bundle: %w", err)
		}
	}
	if bundleDir == "" {
		return nil, fmt.Errorf("skill %q has no bundle source (no bundle_dir or bundle_url)", req.SkillID)
	}

	// Verify bundle directory exists and has a recognized manifest.
	if _, err := os.Stat(bundleDir); err != nil {
		return nil, fmt.Errorf("bundle directory %q does not exist", bundleDir)
	}
	if !hasSkillMarkdown(bundleDir) && !hasBundleManifest(bundleDir) {
		return nil, fmt.Errorf("bundle directory %q must contain SKILL.md or BUNDLE.yaml", bundleDir)
	}

	// Install via LocalInstaller.
	installer := LocalInstaller{Layout: c.Layout}
	result, err := installer.InstallFromBundle(ctx, InstallRequest{
		SkillID: req.SkillID,
		Version: version,
		Root:    c.Layout.Root,
	}, bundleDir)
	if err != nil {
		return nil, fmt.Errorf("install from bundle: %w", err)
	}

	return result, nil
}

// Update re-installs the latest version of a skill.
func (c *FileClawHubClient) Update(ctx context.Context, skillID string) (*InstallResult, error) {
	return c.Install(ctx, InstallRequest{SkillID: skillID})
}

// Pin marks a skill version as pinned.
func (c *FileClawHubClient) Pin(ctx context.Context, skillID, version string) error {
	installer := LocalInstaller{Layout: c.Layout}
	return installer.PinVersion(skillID, version)
}

// Sync refreshes the local index from the remote hub.
func (c *FileClawHubClient) Sync(ctx context.Context) error {
	if c.BaseURL == "" && len(c.Sources) == 0 {
		return nil // No remote hub configured.
	}

	var combined []RegistrySkill
	seen := make(map[string]struct{})
	appendUnique := func(items []RegistrySkill) {
		for _, item := range items {
			key := strings.ToLower(strings.TrimSpace(item.ID))
			if key == "" {
				key = strings.ToLower(strings.TrimSpace(item.Name))
			}
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			combined = append(combined, item)
		}
	}

	if len(c.Sources) > 0 {
		localSources, err := c.searchConfiguredSources(ctx, "")
		if err != nil {
			return fmt.Errorf("sync source catalogs: %w", err)
		}
		appendUnique(localSources)
	}

	if c.BaseURL != "" {
		remote, err := c.searchRemote(ctx, "")
		if err != nil {
			return fmt.Errorf("sync remote index: %w", err)
		}
		appendUnique(remote)
	}

	indexDir := c.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return err
	}

	// Write each entry as a JSON file.
	for _, r := range combined {
		data, err := json.MarshalIndent(catalogEntry{
			ID:        r.ID,
			Name:      r.Name,
			Version:   r.Version,
			Summary:   r.Summary,
			BundleURL: r.BundleURL,
			BundleDir: r.BundleDir,
		}, "", "  ")
		if err != nil {
			continue
		}
		path := filepath.Join(indexDir, r.ID+".json")
		logging.LogIfErr(context.Background(), os.WriteFile(path, append(data, '\n'), 0o644), "write clawhub cache failed")
	}

	return nil
}

// Remove removes an installed skill and updates the lock file.
func (c *FileClawHubClient) Remove(skillID string) error {
	installer := LocalInstaller{Layout: c.Layout}
	lock, err := installer.LoadLock()
	if err != nil {
		return fmt.Errorf("load lock: %w", err)
	}

	found := false
	var remaining []InstalledSkillLock
	for _, entry := range lock.Skills {
		if entry.SkillID == skillID {
			found = true
			// Remove the install directory.
			if entry.InstallDir != "" {
				logging.DebugIfErr(os.RemoveAll(entry.InstallDir), "remove clawhub install dir failed")
			}
			continue
		}
		remaining = append(remaining, entry)
	}

	if !found {
		return fmt.Errorf("skill %q is not installed", skillID)
	}

	lock.Skills = remaining
	lock.GeneratedAt = time.Now().UTC()
	return installer.SaveLock(lock)
}

// Installed returns the list of currently installed skills.
func (c *FileClawHubClient) Installed() ([]InstalledSkillLock, error) {
	installer := LocalInstaller{Layout: c.Layout}
	lock, err := installer.LoadLock()
	if err != nil {
		return nil, err
	}
	return lock.Skills, nil
}

// Publish publishes a skill to the remote hub.
// Publish uploads a skill directory to the remote ClawHub registry.
func (c *FileClawHubClient) Publish(ctx context.Context, req PublishRequest) (*PublishResult, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("publish requires a configured hub URL")
	}
	if c.AuthToken == "" {
		return nil, fmt.Errorf("publish requires authentication (set hub token)")
	}
	if req.SkillDir == "" {
		return nil, fmt.Errorf("publish requires skill directory")
	}
	if req.Slug == "" {
		return nil, fmt.Errorf("publish requires a slug")
	}
	if req.Version == "" {
		return nil, fmt.Errorf("publish requires a version")
	}

	// 1. Verify a supported bundle manifest exists.
	if !hasSkillMarkdown(req.SkillDir) && !hasBundleManifest(req.SkillDir) {
		return nil, fmt.Errorf("publish: SKILL.md or BUNDLE.yaml not found in %s", req.SkillDir)
	}

	// 2. Create tar.gz bundle.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	err := filepath.Walk(req.SkillDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(req.SkillDir, path)
		if err != nil {
			return err
		}
		// Skip hidden files and common junk.
		base := filepath.Base(rel)
		if strings.HasPrefix(base, ".") && rel != "." {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() && (base == "node_modules" || base == "__pycache__" || base == "vendor") {
			return filepath.SkipDir
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(tw, f)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("publish: create bundle: %w", err)
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return nil, fmt.Errorf("publish: finalize tar bundle: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("publish: finalize gzip bundle: %w", err)
	}

	// 3. Upload to hub.
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/skills/publish"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/gzip")
	httpReq.Header.Set("Authorization", "Bearer "+c.AuthToken)
	httpReq.Header.Set("X-Skill-Slug", req.Slug)
	httpReq.Header.Set("X-Skill-Version", req.Version)
	if req.Changelog != "" {
		httpReq.Header.Set("X-Skill-Changelog", req.Changelog)
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("publish: upload: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("publish: hub returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result PublishResult
	if err := json.Unmarshal(body, &result); err != nil {
		// Non-JSON response — return basic result.
		return &PublishResult{Slug: req.Slug, Version: req.Version}, nil
	}
	return &result, nil
}

// --- internal helpers ---

func (c *FileClawHubClient) loadLocalIndex(dir string) ([]catalogEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var catalog []catalogEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var entry catalogEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		if entry.ID == "" {
			entry.ID = strings.TrimSuffix(e.Name(), ".json")
		}
		catalog = append(catalog, entry)
	}
	return catalog, nil
}

func (c *FileClawHubClient) loadSource(ctx context.Context, source, query string) ([]RegistrySkill, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return c.loadHTTPSource(ctx, trimmed, query)
	}
	info, err := os.Stat(trimmed)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return c.loadFileSource(trimmed, query)
	}
	return c.loadDirectorySource(ctx, trimmed, query)
}

func (c *FileClawHubClient) loadHTTPSource(ctx context.Context, source, query string) ([]RegistrySkill, error) {
	if c.HTTPClient == nil {
		return nil, fmt.Errorf("no HTTP client for source %q", source)
	}
	u, err := url.Parse(source)
	if err != nil {
		return nil, err
	}
	if query != "" {
		q := u.Query()
		q.Set("q", query)
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source %q returned %d", source, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	return decodeRegistrySkills(body)
}

func (c *FileClawHubClient) loadFileSource(path, query string) ([]RegistrySkill, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	results, err := decodeRegistrySkills(body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		return results, nil
	}
	filtered := make([]RegistrySkill, 0, len(results))
	for _, item := range results {
		if matchesRegistrySkill(item, query) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (c *FileClawHubClient) loadDirectorySource(ctx context.Context, path, query string) ([]RegistrySkill, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	loader := FilesystemLoader{}
	sources, err := loader.Discover(ctx, []DiscoveryRoot{{
		Kind: SourceClawHub,
		Path: absPath,
	}})
	if err != nil {
		return nil, err
	}
	results := make([]RegistrySkill, 0, len(sources))
	for _, src := range sources {
		spec, err := loader.Load(context.Background(), src)
		if err != nil {
			continue
		}
		entry := registrySkillFromSpec(src, spec)
		if strings.TrimSpace(entry.ID) == "" {
			continue
		}
		if query != "" && !matchesRegistrySkill(entry, query) {
			continue
		}
		results = append(results, entry)
	}
	return results, nil
}

func decodeRegistrySkills(body []byte) ([]RegistrySkill, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, nil
	}

	var direct []RegistrySkill
	if err := json.Unmarshal(body, &direct); err == nil {
		return direct, nil
	}

	var directCatalog []catalogEntry
	if err := json.Unmarshal(body, &directCatalog); err == nil {
		return catalogEntriesToRegistrySkills(directCatalog), nil
	}

	var wrapped struct {
		Skills  []RegistrySkill `json:"skills"`
		Results []RegistrySkill `json:"results"`
		Entries []catalogEntry  `json:"entries"`
		Items   []catalogEntry  `json:"items"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, err
	}
	switch {
	case len(wrapped.Skills) > 0:
		return wrapped.Skills, nil
	case len(wrapped.Results) > 0:
		return wrapped.Results, nil
	case len(wrapped.Entries) > 0:
		return catalogEntriesToRegistrySkills(wrapped.Entries), nil
	case len(wrapped.Items) > 0:
		return catalogEntriesToRegistrySkills(wrapped.Items), nil
	default:
		return nil, nil
	}
}

func catalogEntriesToRegistrySkills(entries []catalogEntry) []RegistrySkill {
	out := make([]RegistrySkill, 0, len(entries))
	for _, entry := range entries {
		out = append(out, RegistrySkill{
			ID:        entry.ID,
			Name:      entry.Name,
			Version:   entry.Version,
			Summary:   entry.Summary,
			BundleURL: entry.BundleURL,
			BundleDir: entry.BundleDir,
		})
	}
	return out
}

func registrySkillFromSpec(src SkillSource, spec *ExternalSkillSpec) RegistrySkill {
	id := strings.TrimSpace(src.NameHint)
	version := "local"
	if spec != nil {
		if spec.Bundle != nil {
			if strings.TrimSpace(spec.Bundle.ID) != "" {
				id = strings.TrimSpace(spec.Bundle.ID)
			}
			if strings.TrimSpace(spec.Bundle.Version) != "" {
				version = strings.TrimSpace(spec.Bundle.Version)
			}
		} else if spec.Companion != nil && strings.TrimSpace(spec.Companion.Version) != "" {
			version = strings.TrimSpace(spec.Companion.Version)
		}
		if strings.TrimSpace(spec.Name) == "" {
			spec.Name = id
		}
	}
	name := id
	summary := ""
	if spec != nil {
		if strings.TrimSpace(spec.Name) != "" {
			name = strings.TrimSpace(spec.Name)
		}
		summary = strings.TrimSpace(spec.Description)
	}
	return RegistrySkill{
		ID:        id,
		Name:      name,
		Version:   version,
		Summary:   summary,
		BundleDir: src.Dir,
	}
}

func matchesRegistrySkill(item RegistrySkill, query string) bool {
	return matchesQuery(catalogEntry{
		ID:      item.ID,
		Name:    item.Name,
		Summary: item.Summary,
	}, strings.ToLower(strings.TrimSpace(query)))
}

func matchesQuery(e catalogEntry, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	return strings.Contains(strings.ToLower(e.ID), query) ||
		strings.Contains(strings.ToLower(e.Name), query) ||
		strings.Contains(strings.ToLower(e.Summary), query)
}

func (c *FileClawHubClient) findSkill(ctx context.Context, skillID string) (*catalogEntry, error) {
	// Check local index first.
	indexDir := c.Layout.IndexDir()
	specific := filepath.Join(indexDir, skillID+".json")
	if data, err := os.ReadFile(specific); err == nil {
		var entry catalogEntry
		if err := json.Unmarshal(data, &entry); err == nil {
			return &entry, nil
		}
	}

	// Search all entries.
	entries, err := c.loadLocalIndex(indexDir)
	if err == nil {
		for i := range entries {
			if strings.EqualFold(entries[i].ID, skillID) || strings.EqualFold(entries[i].Name, skillID) {
				return &entries[i], nil
			}
		}
	}

	// Try configured sources.
	if len(c.Sources) > 0 {
		sourceResults, err := c.searchConfiguredSources(ctx, skillID)
		if err == nil {
			for _, r := range sourceResults {
				if strings.EqualFold(r.ID, skillID) || strings.EqualFold(r.Name, skillID) {
					return &catalogEntry{
						ID:        r.ID,
						Name:      r.Name,
						Version:   r.Version,
						Summary:   r.Summary,
						BundleURL: r.BundleURL,
						BundleDir: r.BundleDir,
					}, nil
				}
			}
		}
	}

	// Try remote.
	if c.BaseURL != "" {
		remote, err := c.searchRemote(ctx, skillID)
		if err == nil {
			for _, r := range remote {
				if strings.EqualFold(r.ID, skillID) || strings.EqualFold(r.Name, skillID) {
					return &catalogEntry{
						ID:        r.ID,
						Name:      r.Name,
						Version:   r.Version,
						Summary:   r.Summary,
						BundleURL: r.BundleURL,
						BundleDir: r.BundleDir,
					}, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("skill %q not found in catalog", skillID)
}

func (c *FileClawHubClient) searchRemote(ctx context.Context, query string) ([]RegistrySkill, error) {
	if c.HTTPClient == nil || c.BaseURL == "" {
		return nil, fmt.Errorf("no remote hub configured")
	}

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/skills/search"
	q := u.Query()
	if query != "" {
		q.Set("q", query)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote hub returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}

	var results []RegistrySkill
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return results, nil
}

func (c *FileClawHubClient) InstallFromSource(ctx context.Context, req InstallRequest, source string) (*InstallResult, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("source path is required")
	}
	absSource, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("resolve source path: %w", err)
	}
	info, err := os.Stat(absSource)
	if err != nil {
		return nil, fmt.Errorf("stat source: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source %q is not a directory", absSource)
	}

	spec, err := ParseDir(absSource)
	if err != nil {
		return nil, fmt.Errorf("parse skill source: %w", err)
	}

	if req.SkillID == "" {
		req.SkillID = filepath.Base(absSource)
	}
	if req.Version == "" {
		if spec.Bundle != nil && strings.TrimSpace(spec.Bundle.Version) != "" {
			req.Version = strings.TrimSpace(spec.Bundle.Version)
		} else if spec.Companion != nil && strings.TrimSpace(spec.Companion.Version) != "" {
			req.Version = strings.TrimSpace(spec.Companion.Version)
		} else {
			req.Version = "local"
		}
	}
	if req.Root == "" {
		req.Root = c.Layout.Root
	}

	installer := LocalInstaller{Layout: c.Layout}
	return installer.InstallFromBundle(ctx, req, absSource)
}

func (c *FileClawHubClient) downloadBundle(ctx context.Context, bundleURL, skillID, version string) (string, error) {
	if c.HTTPClient == nil {
		return "", fmt.Errorf("no HTTP client for download")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bundleURL, nil)
	if err != nil {
		return "", err
	}
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	bundleDir := c.Layout.BundleDir(skillID, version)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return "", err
	}

	// Detect format and extract.
	limitedBody := io.LimitReader(resp.Body, bundleDownloadMaxBytes)

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if isZipBundle(bundleURL, contentType) {
		if err := extractZip(limitedBody, bundleDir, 0); err != nil {
			return "", fmt.Errorf("extract zip: %w", err)
		}
	} else {
		// Default: tar.gz
		if err := extractTarGz(limitedBody, bundleDir, 0); err != nil {
			return "", fmt.Errorf("extract tar.gz: %w", err)
		}
	}

	return bundleDir, nil
}

func isZipBundle(bundleURL, contentType string) bool {
	lowerURL := strings.ToLower(strings.TrimSpace(bundleURL))
	switch {
	case strings.HasSuffix(lowerURL, ".zip"):
		return true
	case strings.Contains(contentType, "application/zip"):
		return true
	case strings.Contains(contentType, "application/x-zip"):
		return true
	default:
		return false
	}
}

const bundleDownloadMaxBytes = 100 * 1024 * 1024 // 100 MB

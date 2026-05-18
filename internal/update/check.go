// Package update implements version checking and self-update for HopClaw.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/fulcrus/hopclaw/internal/version"
	"github.com/fulcrus/hopclaw/logging"
)

const (
	defaultCheckInterval = 24 * time.Hour
	checkTimeout         = 10 * time.Second
	stateFileName        = "update-check.json"
)

// Policy controls update-channel selection and background check behavior.
type Policy struct {
	Enabled       bool
	CheckOnStart  bool
	CheckInterval time.Duration
	Channel       string
	ManifestURL   string
	SkipVersion   string
	DisableManifest bool
}

// DefaultPolicy returns the product defaults for release checks.
func DefaultPolicy() Policy {
	return Policy{
		Enabled:       true,
		CheckOnStart:  true,
		CheckInterval: defaultCheckInterval,
		Channel:       "stable",
		ManifestURL:   version.DefaultManifestURL,
	}
}

func normalizePolicy(policy Policy) Policy {
	base := DefaultPolicy()
	if policy.CheckInterval > 0 {
		base.CheckInterval = policy.CheckInterval
	}
	if trimmed := normalizeChannel(policy.Channel); trimmed != "" {
		base.Channel = trimmed
	}
	if trimmed := strings.TrimSpace(policy.ManifestURL); trimmed != "" {
		base.ManifestURL = trimmed
	}
	if trimmed := strings.TrimSpace(policy.SkipVersion); trimmed != "" {
		base.SkipVersion = trimmed
	}
	base.DisableManifest = policy.DisableManifest
	if policy.Enabled {
		base.Enabled = true
	}
	if !policy.Enabled && policy != (Policy{}) {
		base.Enabled = false
	}
	if policy.CheckOnStart {
		base.CheckOnStart = true
	}
	if !policy.CheckOnStart && policy != (Policy{}) {
		base.CheckOnStart = false
	}
	if policy.DisableManifest {
		base.ManifestURL = ""
	}
	return base
}

func normalizeChannel(channel string) string {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "", "stable":
		return "stable"
	case "beta":
		return "beta"
	case "nightly", "dev":
		return "nightly"
	default:
		return strings.ToLower(strings.TrimSpace(channel))
	}
}

// CheckResult holds the outcome of a version check.
type CheckResult struct {
	CurrentVersion string    `json:"current_version"`
	CurrentChannel string    `json:"current_channel,omitempty"`
	LatestVersion  string    `json:"latest_version,omitempty"`
	LatestChannel  string    `json:"latest_channel,omitempty"`
	UpToDate       bool      `json:"up_to_date"`
	UpdateURL      string    `json:"update_url,omitempty"`
	Notes          string    `json:"notes,omitempty"`
	PublishedAt    time.Time `json:"published_at,omitempty"`
	ManifestURL    string    `json:"manifest_url,omitempty"`
	Source         string    `json:"source,omitempty"` // manifest or github
	CheckedAt      time.Time `json:"checked_at,omitempty"`
	Error          string    `json:"error,omitempty"`
}

// Check queries the default release source for the latest version and compares
// it with the current build.
func Check(ctx context.Context) (*CheckResult, error) {
	return CheckWithPolicy(ctx, DefaultPolicy())
}

// CheckWithPolicy queries the configured release source and compares it with
// the current build. It prefers the release manifest and falls back to GitHub
// releases when needed.
func CheckWithPolicy(ctx context.Context, policy Policy) (*CheckResult, error) {
	policy = normalizePolicy(policy)
	result := &CheckResult{
		CurrentVersion: version.Version,
		CurrentChannel: policy.Channel,
		UpToDate:       true,
		ManifestURL:    policy.ManifestURL,
		CheckedAt:      time.Now().UTC(),
	}

	if !policy.Enabled {
		result.Error = "update checks disabled"
		return result, nil
	}

	// Skip check for dev builds.
	if version.Version == "dev" || version.Version == "" {
		result.Error = "dev build, skipping update check"
		return result, nil
	}

	release, source, err := resolveLatestRelease(ctx, policy, "")
	if err != nil {
		result.Error = err.Error()
		_ = saveCheckState(checkStateFromResult(result, policy))
		return result, err
	}

	result.Source = source
	result.LatestVersion = release.Version
	result.LatestChannel = release.Channel
	result.UpdateURL = release.URL
	result.Notes = strings.TrimSpace(release.Notes)
	result.PublishedAt = release.PublishedAt
	result.UpToDate = !isNewer(version.Version, release.Version)
	if policy.SkipVersion != "" && sameVersion(policy.SkipVersion, release.Version) {
		result.UpToDate = true
	}

	logging.DebugIfErr(saveCheckState(checkStateFromResult(result, policy)), "save update check state failed")
	return result, nil
}

// BackgroundCheck starts an asynchronous update check if enough time has
// passed since the last check. Results are written to the state file.
func BackgroundCheck(policy Policy) {
	policy = normalizePolicy(policy)
	if !policy.Enabled || !policy.CheckOnStart {
		return
	}
	state, err := loadCheckState()
	if err == nil && normalizeChannel(state.Channel) == policy.Channel && time.Since(state.LastCheck) < policy.CheckInterval {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		defer cancel()
		if _, err := CheckWithPolicy(ctx, policy); err != nil {
			log.Debug("background update check failed", "error", err)
		}
	}()
}

// LastCheckResult reads the most recent check result from disk.
func LastCheckResult() *CheckResult {
	state, err := loadCheckState()
	if err != nil {
		return nil
	}
	result := &CheckResult{
		CurrentVersion: version.Version,
		CurrentChannel: normalizeChannel(defaultString(state.Channel, version.Channel)),
		LatestVersion:  state.LatestVersion,
		LatestChannel:  state.LatestChannel,
		UpdateURL:      state.UpdateURL,
		Notes:          state.Notes,
		ManifestURL:    state.ManifestURL,
		Source:         state.Source,
		CheckedAt:      state.LastCheck,
		Error:          state.Error,
		PublishedAt:    state.PublishedAt,
	}
	result.UpToDate = !isNewer(version.Version, state.LatestVersion)
	if state.SkipVersion != "" && sameVersion(state.SkipVersion, state.LatestVersion) {
		result.UpToDate = true
	}
	return result
}

type releaseInfo struct {
	Version     string
	Channel     string
	URL         string
	Notes       string
	PublishedAt time.Time
	Assets      []assetInfo
}

type assetInfo struct {
	Name   string
	OS     string
	Arch   string
	URL    string
	SHA256 string
}

func resolveLatestRelease(ctx context.Context, policy Policy, requestedVersion string) (releaseInfo, string, error) {
	if !policy.DisableManifest {
		if manifestURL := strings.TrimSpace(policy.ManifestURL); manifestURL != "" {
			if release, err := fetchReleaseFromManifest(ctx, manifestURL, policy.Channel, requestedVersion); err == nil {
				return release, "manifest", nil
			}
		}
	}
	release, err := fetchReleaseFromGitHub(ctx, policy.Channel, requestedVersion)
	if err != nil {
		return releaseInfo{}, "", err
	}
	return release, "github", nil
}

// ---------------------------------------------------------------------------
// Manifest support
// ---------------------------------------------------------------------------

type releaseManifest struct {
	Version     string          `json:"version"`
	Channel     string          `json:"channel"`
	URL         string          `json:"url"`
	Notes       string          `json:"notes"`
	PublishedAt string          `json:"published_at"`
	Assets      []assetManifest `json:"assets"`
}

type assetManifest struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type manifestChannel struct {
	Latest   string            `json:"latest"`
	Releases []releaseManifest `json:"releases"`
}

type releaseManifestFile struct {
	Product  string                     `json:"product"`
	Channels map[string]manifestChannel `json:"channels"`
	Releases []releaseManifest          `json:"releases"`
}

func fetchReleaseFromManifest(ctx context.Context, manifestURL, channel, requestedVersion string) (releaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return releaseInfo{}, fmt.Errorf("create manifest request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.ProductName+"/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return releaseInfo{}, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, fmt.Errorf("manifest returned HTTP %d", resp.StatusCode)
	}

	var manifest releaseManifestFile
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return releaseInfo{}, fmt.Errorf("decode manifest: %w", err)
	}
	release, err := selectManifestRelease(manifest, channel, requestedVersion)
	if err != nil {
		return releaseInfo{}, err
	}
	return manifestReleaseToInfo(release), nil
}

func selectManifestRelease(manifest releaseManifestFile, channel, requestedVersion string) (releaseManifest, error) {
	var candidates []releaseManifest
	if len(manifest.Releases) > 0 {
		candidates = append(candidates, manifest.Releases...)
	}
	if entry, ok := manifest.Channels[normalizeChannel(channel)]; ok {
		if requestedVersion == "" && strings.TrimSpace(entry.Latest) != "" {
			for _, rel := range entry.Releases {
				if sameVersion(rel.Version, entry.Latest) {
					return rel, nil
				}
			}
			for _, rel := range manifest.Releases {
				if sameVersion(rel.Version, entry.Latest) {
					return rel, nil
				}
			}
		}
		candidates = append(candidates, entry.Releases...)
	}
	if len(candidates) == 0 {
		return releaseManifest{}, fmt.Errorf("no manifest releases available for channel %s", normalizeChannel(channel))
	}

	filtered := make([]releaseManifest, 0, len(candidates))
	for _, rel := range dedupeManifestReleases(candidates) {
		rel.Channel = normalizeChannel(defaultString(rel.Channel, channel))
		if requestedVersion != "" && !sameVersion(rel.Version, requestedVersion) {
			continue
		}
		if requestedVersion == "" && rel.Channel != normalizeChannel(channel) {
			continue
		}
		filtered = append(filtered, rel)
	}
	if len(filtered) == 0 {
		if requestedVersion != "" {
			return releaseManifest{}, fmt.Errorf("version %s not found in manifest", requestedVersion)
		}
		return releaseManifest{}, fmt.Errorf("channel %s not found in manifest", normalizeChannel(channel))
	}
	sort.Slice(filtered, func(i, j int) bool {
		return compareReleaseOrder(filtered[i].Version, filtered[i].PublishedAt, filtered[j].Version, filtered[j].PublishedAt) > 0
	})
	return filtered[0], nil
}

func dedupeManifestReleases(items []releaseManifest) []releaseManifest {
	seen := make(map[string]struct{}, len(items))
	out := make([]releaseManifest, 0, len(items))
	for _, item := range items {
		key := normalizeChannel(item.Channel) + ":" + strings.TrimSpace(item.Version)
		if key == ":" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func manifestReleaseToInfo(rel releaseManifest) releaseInfo {
	assets := make([]assetInfo, 0, len(rel.Assets))
	for _, asset := range rel.Assets {
		assets = append(assets, assetInfo{
			Name:   strings.TrimSpace(asset.Name),
			OS:     strings.ToLower(strings.TrimSpace(asset.OS)),
			Arch:   strings.ToLower(strings.TrimSpace(asset.Arch)),
			URL:    strings.TrimSpace(asset.URL),
			SHA256: strings.ToLower(strings.TrimSpace(asset.SHA256)),
		})
	}
	return releaseInfo{
		Version:     strings.TrimSpace(rel.Version),
		Channel:     normalizeChannel(rel.Channel),
		URL:         strings.TrimSpace(rel.URL),
		Notes:       strings.TrimSpace(rel.Notes),
		PublishedAt: parseReleaseTime(rel.PublishedAt),
		Assets:      assets,
	}
}

// ---------------------------------------------------------------------------
// GitHub fallback
// ---------------------------------------------------------------------------

type githubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchReleaseFromGitHub(ctx context.Context, channel, requestedVersion string) (releaseInfo, error) {
	apiURL := githubReleasesAPIURL()
	reqURL := apiURL
	if requestedVersion == "" {
		if strings.Contains(reqURL, "?") {
			reqURL += "&per_page=20"
		} else {
			reqURL += "?per_page=20"
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return releaseInfo{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", version.ProductName+"/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return releaseInfo{}, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, fmt.Errorf("github api returned %d", resp.StatusCode)
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return releaseInfo{}, fmt.Errorf("decode release list: %w", err)
	}
	if len(releases) == 0 {
		return releaseInfo{}, fmt.Errorf("no GitHub releases found")
	}
	selected, err := selectGitHubRelease(releases, channel, requestedVersion)
	if err != nil {
		return releaseInfo{}, err
	}
	return githubReleaseToInfo(selected), nil
}

func githubReleasesAPIURL() string {
	if raw := strings.TrimSpace(os.Getenv("HOPCLAW_UPDATE_API_URL")); raw != "" {
		return raw
	}
	url := strings.TrimSuffix(version.DefaultRepository, "/") + "/releases"
	return strings.Replace(url, "https://github.com/", "https://api.github.com/repos/", 1)
}

func selectGitHubRelease(items []githubRelease, channel, requestedVersion string) (githubRelease, error) {
	channel = normalizeChannel(channel)
	filtered := make([]githubRelease, 0, len(items))
	for _, item := range items {
		if requestedVersion != "" && !sameVersion(item.TagName, requestedVersion) {
			continue
		}
		switch channel {
		case "stable":
			if item.Prerelease {
				continue
			}
		case "beta":
			// beta accepts prerelease and stable; highest wins.
		case "nightly":
			// GitHub fallback does not encode nightly separately. Keep prereleases.
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == 0 {
		if requestedVersion != "" {
			return githubRelease{}, fmt.Errorf("version %s not found in GitHub releases", requestedVersion)
		}
		return githubRelease{}, fmt.Errorf("no GitHub releases available for channel %s", channel)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return compareReleaseOrder(filtered[i].TagName, filtered[i].PublishedAt, filtered[j].TagName, filtered[j].PublishedAt) > 0
	})
	return filtered[0], nil
}

func githubReleaseToInfo(rel githubRelease) releaseInfo {
	assets := make([]assetInfo, 0, len(rel.Assets))
	for _, asset := range rel.Assets {
		assets = append(assets, assetInfo{
			Name: strings.TrimSpace(asset.Name),
			URL:  strings.TrimSpace(asset.BrowserDownloadURL),
		})
	}
	channel := "stable"
	if rel.Prerelease {
		channel = "beta"
	}
	return releaseInfo{
		Version:     strings.TrimSpace(rel.TagName),
		Channel:     channel,
		URL:         strings.TrimSpace(rel.HTMLURL),
		Notes:       strings.TrimSpace(rel.Body),
		PublishedAt: parseReleaseTime(rel.PublishedAt),
		Assets:      assets,
	}
}

// ---------------------------------------------------------------------------
// Version comparison
// ---------------------------------------------------------------------------

// isNewer returns true if latest is newer than current.
func isNewer(current, latest string) bool {
	return compareVersions(latest, current) > 0
}

func sameVersion(a, b string) bool {
	return compareVersions(a, b) == 0
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 3 {
		return ""
	}
	var sb strings.Builder
	for i, p := range parts {
		if i == 2 {
			if idx := strings.IndexAny(p, "-+"); idx >= 0 {
				p = p[:idx]
			}
		}
		if p == "" {
			return ""
		}
		for len(p) < 4 {
			p = "0" + p
		}
		if i > 0 {
			sb.WriteByte('.')
		}
		sb.WriteString(p)
	}
	return sb.String()
}

func parseReleaseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return ts
}

func compareReleaseOrder(versionA, publishedA, versionB, publishedB string) int {
	if cmp := compareVersions(versionA, versionB); cmp != 0 {
		return cmp
	}
	at, bt := parseReleaseTime(publishedA), parseReleaseTime(publishedB)
	switch {
	case at.After(bt):
		return 1
	case at.Before(bt):
		return -1
	default:
		return 0
	}
}

type comparableVersion struct {
	base       [3]int
	stageRank  int
	stageLabel string
	parts      []prereleasePart
}

type prereleasePart struct {
	number *int
	text   string
}

func compareVersions(a, b string) int {
	parsedA, okA := parseComparableVersion(a)
	parsedB, okB := parseComparableVersion(b)
	switch {
	case !okA || !okB:
		return 0
	}
	for i := range parsedA.base {
		switch {
		case parsedA.base[i] > parsedB.base[i]:
			return 1
		case parsedA.base[i] < parsedB.base[i]:
			return -1
		}
	}
	switch {
	case parsedA.stageRank > parsedB.stageRank:
		return 1
	case parsedA.stageRank < parsedB.stageRank:
		return -1
	}
	if parsedA.stageLabel != parsedB.stageLabel {
		if parsedA.stageLabel > parsedB.stageLabel {
			return 1
		}
		return -1
	}
	maxParts := len(parsedA.parts)
	if len(parsedB.parts) > maxParts {
		maxParts = len(parsedB.parts)
	}
	for i := 0; i < maxParts; i++ {
		if i >= len(parsedA.parts) {
			return -1
		}
		if i >= len(parsedB.parts) {
			return 1
		}
		if cmp := comparePrereleasePart(parsedA.parts[i], parsedB.parts[i]); cmp != 0 {
			return cmp
		}
	}
	return 0
}

func parseComparableVersion(v string) (comparableVersion, bool) {
	raw := strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if raw == "" || strings.EqualFold(raw, "dev") {
		return comparableVersion{}, false
	}
	raw = strings.SplitN(raw, "+", 2)[0]

	baseRaw := raw
	prereleaseRaw := ""
	if idx := strings.Index(baseRaw, "-"); idx >= 0 {
		prereleaseRaw = baseRaw[idx+1:]
		baseRaw = baseRaw[:idx]
	}

	baseParts := strings.Split(baseRaw, ".")
	if len(baseParts) != 3 {
		return comparableVersion{}, false
	}

	out := comparableVersion{
		stageRank:  stageRank("stable"),
		stageLabel: "stable",
	}
	for i, part := range baseParts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return comparableVersion{}, false
		}
		out.base[i] = n
	}

	if strings.TrimSpace(prereleaseRaw) == "" {
		return out, true
	}

	tokens := strings.Split(prereleaseRaw, ".")
	if len(tokens) == 0 {
		return comparableVersion{}, false
	}
	stageLabel, trailingNumber := splitStageToken(tokens[0])
	out.stageLabel = stageLabel
	out.stageRank = stageRank(stageLabel)
	if trailingNumber != nil {
		out.parts = append(out.parts, prereleasePart{number: trailingNumber})
	}
	for _, token := range tokens[1:] {
		token = strings.TrimSpace(strings.ToLower(token))
		if token == "" {
			continue
		}
		if n, err := strconv.Atoi(token); err == nil {
			num := n
			out.parts = append(out.parts, prereleasePart{number: &num})
			continue
		}
		out.parts = append(out.parts, prereleasePart{text: token})
	}
	return out, true
}

func splitStageToken(token string) (string, *int) {
	token = strings.TrimSpace(strings.ToLower(token))
	if token == "" {
		return "prerelease", nil
	}
	idx := len(token)
	for i, r := range token {
		if r >= '0' && r <= '9' {
			idx = i
			break
		}
	}
	if idx == len(token) {
		return token, nil
	}
	label := token[:idx]
	if label == "" {
		label = "prerelease"
	}
	n, err := strconv.Atoi(token[idx:])
	if err != nil {
		return label, nil
	}
	return label, &n
}

func stageRank(label string) int {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "stable", "":
		return 5
	case "rc":
		return 4
	case "beta":
		return 3
	case "nightly", "dev":
		return 2
	case "alpha":
		return 1
	default:
		return 0
	}
}

func comparePrereleasePart(a, b prereleasePart) int {
	switch {
	case a.number != nil && b.number != nil:
		switch {
		case *a.number > *b.number:
			return 1
		case *a.number < *b.number:
			return -1
		default:
			return 0
		}
	case a.number != nil && b.number == nil:
		return -1
	case a.number == nil && b.number != nil:
		return 1
	default:
		switch {
		case a.text > b.text:
			return 1
		case a.text < b.text:
			return -1
		default:
			return 0
		}
	}
}

// ---------------------------------------------------------------------------
// Check state persistence
// ---------------------------------------------------------------------------

type checkState struct {
	LastCheck      time.Time `json:"last_check"`
	CurrentVersion string    `json:"current_version,omitempty"`
	Channel        string    `json:"channel,omitempty"`
	LatestVersion  string    `json:"latest_version"`
	LatestChannel  string    `json:"latest_channel,omitempty"`
	UpdateURL      string    `json:"update_url"`
	Notes          string    `json:"notes,omitempty"`
	ManifestURL    string    `json:"manifest_url,omitempty"`
	Source         string    `json:"source,omitempty"`
	PublishedAt    time.Time `json:"published_at,omitempty"`
	SkipVersion    string    `json:"skip_version,omitempty"`
	Error          string    `json:"error,omitempty"`
}

func checkStateFromResult(result *CheckResult, policy Policy) checkState {
	if result == nil {
		return checkState{}
	}
	return checkState{
		LastCheck:      result.CheckedAt,
		CurrentVersion: result.CurrentVersion,
		Channel:        normalizeChannel(policy.Channel),
		LatestVersion:  result.LatestVersion,
		LatestChannel:  normalizeChannel(result.LatestChannel),
		UpdateURL:      result.UpdateURL,
		Notes:          result.Notes,
		ManifestURL:    result.ManifestURL,
		Source:         result.Source,
		PublishedAt:    result.PublishedAt,
		SkipVersion:    strings.TrimSpace(policy.SkipVersion),
		Error:          result.Error,
	}
}

func stateFilePath() string {
	return filepath.Join(daemon.StateDir(), stateFileName)
}

func loadCheckState() (checkState, error) {
	data, err := os.ReadFile(stateFilePath())
	if err != nil {
		return checkState{}, err
	}
	var state checkState
	if err := json.Unmarshal(data, &state); err != nil {
		return checkState{}, err
	}
	return state, nil
}

func saveCheckState(state checkState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	logging.DebugIfErr(os.MkdirAll(filepath.Dir(stateFilePath()), 0o755), "create update state dir failed")
	return os.WriteFile(stateFilePath(), data, 0o644)
}

func defaultString(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

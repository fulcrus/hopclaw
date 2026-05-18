package toolruntime

import (
	"context"
	"hash"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/media"
	"github.com/fulcrus/hopclaw/mediagen"
	netpkg "github.com/fulcrus/hopclaw/toolruntime/net"
)

// Exported helper functions for use by domain sub-packages.
// These wrap the existing package-internal helpers to avoid renaming all
// call sites in the root package at once.

// StringFrom extracts a string from an interface value.
func StringFrom(value any) (string, error) { return stringFrom(value) }

// RequiredString extracts a non-empty string from a map by key, or returns an error.
func RequiredString(input map[string]any, key string) (string, error) {
	return requiredString(input, key)
}

// IntFrom extracts an integer from an interface value, with a fallback default.
func IntFrom(value any, fallback int) (int, error) { return intFrom(value, fallback) }

// BoolFrom extracts a boolean from an interface value.
func BoolFrom(value any) (bool, error) { return boolFrom(value) }

// BoolFromDefault extracts a boolean with a fallback when value is nil.
func BoolFromDefault(value any, fallback bool) (bool, error) { return boolFromDefault(value, fallback) }

// StringFromDefault extracts a string with a fallback when value is empty or nil.
func StringFromDefault(value any, fallback string) string { return stringFromDefault(value, fallback) }

// StringSliceFrom extracts a string slice from an interface value.
func StringSliceFrom(value any) ([]string, error) { return stringSliceFrom(value) }

// FloatFrom extracts a float64 from an interface value, with a fallback default.
func FloatFrom(value any, fallback float64) (float64, error) { return floatFrom(value, fallback) }

// Int64From extracts an int64 from an interface value.
func Int64From(value any) (int64, error) { return int64From(value) }

// MapFrom extracts a map[string]any from an interface value.
func MapFrom(value any) (map[string]any, error) { return mapFrom(value) }

// ObjectSchema builds a JSON Schema object type.
func ObjectSchema(properties map[string]any, required ...string) map[string]any {
	return objectSchema(properties, required...)
}

// StringSchema builds a JSON Schema string type.
func StringSchema(description string) map[string]any { return stringSchema(description) }

// IntegerSchema builds a JSON Schema integer type.
func IntegerSchema(description string) map[string]any { return integerSchema(description) }

// BooleanSchema builds a JSON Schema boolean type.
func BooleanSchema(description string) map[string]any { return booleanSchema(description) }

// NumberSchema builds a JSON Schema number type.
func NumberSchema(description string) map[string]any { return numberSchema(description) }

// StringArraySchema builds a JSON Schema array-of-strings type.
func StringArraySchema(description string) map[string]any { return stringArraySchema(description) }

// ArraySchema builds a JSON Schema array type.
func ArraySchema(items map[string]any, description string) map[string]any {
	return arraySchema(items, description)
}

// JSONResult builds a ToolResult from a JSON-serializable payload.
// Exported for use by domain sub-packages.
func (b *Builtins) JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error) {
	return b.jsonResult(call, payload)
}

// ResolvePath resolves a path relative to the workspace root and validates it.
// Exported for use by domain sub-packages.
func (b *Builtins) ResolvePath(input string) (string, error) {
	return b.resolvePath(input)
}

// DisplayPath returns a human-readable path relative to the workspace root.
// Exported for use by domain sub-packages.
func (b *Builtins) DisplayPath(absPath string) string {
	return b.displayPath(absPath)
}

// VisionAnalyzer returns a VisionAnalyzer, or an error if no media registry
// is configured. Exported for use by domain sub-packages.
func (b *Builtins) VisionAnalyzer() (*media.VisionAnalyzer, error) {
	return b.visionAnalyzer()
}

// ImageComparer returns an ImageComparer, or an error if no media registry
// is configured. Exported for use by domain sub-packages.
func (b *Builtins) ImageComparer() (*media.ImageComparer, error) {
	return b.imageComparer()
}

// MediaGenerationRegistry returns the configured media-generation registry.
func (b *Builtins) MediaGenerationRegistry() *mediagen.Registry {
	if b == nil {
		return nil
	}
	return b.mediaGenRegistry
}

// RootAbs returns the absolute path of the workspace root.
// Exported for use by domain sub-packages.
func (b *Builtins) RootAbs() string { return b.rootAbs }

// Config returns the builtins configuration.
// Exported for use by domain sub-packages.
func (b *Builtins) Config() BuiltinsConfig { return b.config }

// EffectiveExecTimeout applies exec constraint caps to a requested timeout.
// Exported for use by domain sub-packages.
func (b *Builtins) EffectiveExecTimeout(requested time.Duration) time.Duration {
	return b.effectiveExecTimeout(requested)
}

// ValidateExecInvocation checks exec constraints for a direct command invocation.
// Exported for use by domain sub-packages.
func (b *Builtins) ValidateExecInvocation(command string, args []string) error {
	return b.validateExecInvocation(command, args)
}

// ValidateShellInvocation checks exec constraints for a shell command.
// Exported for use by domain sub-packages.
func (b *Builtins) ValidateShellInvocation(command string) error {
	return b.validateShellInvocation(command)
}

// ExecOutputLimit returns the configured exec output capture limit.
// Exported for use by domain sub-packages.
func (b *Builtins) ExecOutputLimit() int { return b.execOutputLimit() }

// DefaultExecTimeout returns the configured default exec timeout.
// Exported for use by domain sub-packages.
func (b *Builtins) DefaultExecTimeout() time.Duration { return b.config.DefaultExecTimeout }

// NewExecCapture creates capped stdout/stderr capture buffers.
// Exported as a package-level function for use by domain sub-packages.
func NewExecCapture(limit int) (stdout, stderr interface{ String() string }, stdoutW, stderrW io.Writer) {
	so, se, sw, ew := newExecCapture(limit)
	return so, se, sw, ew
}

// NewExecCaptureMethod creates capped stdout/stderr capture buffers (method version).
// Exported for use by domain sub-packages that need this on the Runtime interface.
func (b *Builtins) NewExecCapture(limit int) (stdout, stderr interface{ String() string }, stdoutW, stderrW io.Writer) {
	so, se, sw, ew := newExecCapture(limit)
	return so, se, sw, ew
}

// TimeoutFrom parses a timeout value with a fallback default.
// Exported for use by domain sub-packages.
func TimeoutFrom(value any, fallback time.Duration) (time.Duration, error) {
	return timeoutFrom(value, fallback)
}

// ResolvePathWithOptions resolves a path with root-allowance control.
// Exported for use by domain sub-packages.
func (b *Builtins) ResolvePathWithOptions(input string, allowRoot bool) (string, error) {
	return b.resolvePathWithOptions(input, allowRoot)
}

// ResolvePathNoFollow resolves a path without following symlinks.
// Exported for use by domain sub-packages.
func (b *Builtins) ResolvePathNoFollow(input string) (string, error) {
	return b.resolvePathNoFollow(input)
}

// ShouldHideFSPath returns true when a path should be hidden from filesystem results.
// Exported for use by domain sub-packages.
func (b *Builtins) ShouldHideFSPath(absPath string, info os.FileInfo) bool {
	return b.shouldHideFSPath(absPath, info)
}

// AuthorizeAbsolutePath checks whether an absolute path is allowed.
// Exported for use by domain sub-packages.
func (b *Builtins) AuthorizeAbsolutePath(absPath string, followLeaf bool) error {
	return b.authorizeAbsolutePath(absPath, followLeaf)
}

// ApplyUnifiedPatch applies a unified diff patch.
// Exported for use by domain sub-packages.
func (b *Builtins) ApplyUnifiedPatch(ctx context.Context, patchText string, strip int, reverse bool) ([]string, error) {
	return b.applyUnifiedPatch(ctx, patchText, strip, reverse)
}

// ArtifactStore returns the configured artifact store (may be nil).
// Exported for use by domain sub-packages.
func (b *Builtins) ArtifactStore() interface {
	Read(ctx context.Context, uri string) ([]byte, string, error)
} {
	return b.artifactStore
}

// PutArtifact stores an artifact and attaches run/session/tool metadata from
// the current builtin execution context.
func (b *Builtins) PutArtifact(ctx context.Context, call agent.ToolCall, req artifact.PutRequest) (*artifact.Blob, error) {
	if b == nil || b.artifactStore == nil {
		return nil, agent.ErrArtifactStoreNil
	}
	metadata := make(map[string]any, len(req.Metadata)+4)
	for key, value := range req.Metadata {
		metadata[key] = value
	}
	if run := builtinRunFromContext(ctx); run != nil && strings.TrimSpace(run.ID) != "" {
		metadata[meta.KeyRunID] = run.ID
	}
	if session := builtinSessionFromContext(ctx); session != nil && strings.TrimSpace(session.ID) != "" {
		metadata[meta.KeySessionID] = session.ID
	}
	if strings.TrimSpace(call.Name) != "" {
		metadata[meta.KeyToolName] = call.Name
	}
	if strings.TrimSpace(call.ID) != "" {
		metadata[meta.KeyToolCallID] = call.ID
	}
	req.Metadata = metadata
	return b.artifactStore.Put(ctx, req)
}

// MaxReadBytes returns the configured maximum file read size (0 = unlimited).
// Exported for use by domain sub-packages.
func (b *Builtins) MaxReadBytes() int { return b.config.MaxReadBytes }

// ReadArtifact reads an artifact by URI, returning data, content type, and error.
// Returns nil, "", nil when the artifact store is not configured.
// Exported for use by domain sub-packages.
func (b *Builtins) ReadArtifact(ctx context.Context, uri string) ([]byte, string, error) {
	if b.artifactStore == nil {
		return nil, "", nil
	}
	return b.artifactStore.Read(ctx, uri)
}

// AtomicWriteFile writes data to a file atomically.
// Exported for use by domain sub-packages.
func AtomicWriteFile(path string, data []byte, mode os.FileMode) error {
	return atomicWriteFile(path, data, mode)
}

// NewHasher creates a hash function by algorithm name.
// Exported for use by domain sub-packages.
func NewHasher(algorithm string) (hash.Hash, string, error) {
	return newHasher(algorithm)
}

// MatchesPattern checks whether candidate matches a pattern (substring or glob).
// Exported for use by domain sub-packages.
func MatchesPattern(candidate, pattern string, useGlob bool) bool {
	return matchesPattern(candidate, pattern, useGlob)
}

// FileKind returns the type string for a file info.
// Exported for use by domain sub-packages.
func FileKind(info os.FileInfo) string {
	return fileKind(info)
}

// DiskUsage returns disk space stats for a path.
// Exported for use by domain sub-packages.
func DiskUsage(path string) (total, free, available uint64, err error) {
	return diskUsage(path)
}

// NetConstraints re-exports config.NetConstraints for sub-packages.
type NetConstraints = config.NetConstraints

// CheckURLSSRF validates a URL against SSRF constraints.
// Exported for use by domain sub-packages.
func CheckURLSSRF(rawURL string, constraints config.NetConstraints) error {
	return checkURLSSRF(rawURL, constraints)
}

// CheckHostSSRF validates a hostname against SSRF constraints.
// Exported for use by domain sub-packages.
func CheckHostSSRF(host string, constraints config.NetConstraints) error {
	return checkHostSSRF(host, constraints)
}

// HostMatchesList checks whether a host matches any entry in a list.
// Exported for use by domain sub-packages.
func HostMatchesList(host string, list []string) bool {
	return hostMatchesList(host, list)
}

// NewSSRFProtectedHTTPClientExported returns an *http.Client with SSRF protections.
// Exported for use by domain sub-packages.
func NewSSRFProtectedHTTPClientExported(constraints config.NetConstraints) *http.Client {
	return newSSRFProtectedHTTPClient(constraints)
}

// FetchReadableURL fetches a URL and extracts readable content.
// Exported for use by domain sub-packages.
func FetchReadableURL(ctx context.Context, rawURL string, timeout time.Duration, maxBytes, maxChars int, constraints config.NetConstraints) (FetchedWebContent, error) {
	result, err := fetchReadableURL(ctx, rawURL, timeout, maxBytes, maxChars, constraints)
	if err != nil {
		return FetchedWebContent{}, err
	}
	return FetchedWebContent(result), nil
}

// FetchedWebContent holds the result of FetchReadableURL.
type FetchedWebContent struct {
	URL         string
	FinalURL    string
	Domain      string
	Title       string
	ContentType string
	Content     string
	StatusCode  int
	Truncated   bool
	Bytes       int
}

// DefaultWebFetchMaxChars is the default max chars for web fetch.
const DefaultWebFetchMaxChars = 8000

// StripHTML removes HTML tags and returns cleaned text.
// Exported for use by domain sub-packages.
func StripHTML(html string) string {
	return stripHTML(html)
}

// IntSliceFrom extracts an []int from an interface value.
// Exported for use by domain sub-packages.
func IntSliceFrom(value any) ([]int, error) {
	return intSliceFrom(value)
}

// OptionalString extracts a trimmed string from a map by key, returning "" if absent.
// Exported for use by domain sub-packages.
func OptionalString(input map[string]any, key string) string {
	return optionalString(input, key)
}

// optionalString extracts a trimmed string from a map by key, returning "" if absent.
// Defined here so it remains available to root-package files after sub-package extraction.
func optionalString(input map[string]any, key string) string {
	value, _ := stringFrom(input[key])
	return strings.TrimSpace(value)
}

// CheckURLSSRF validates a URL against the configured SSRF constraints.
// Method version for use on the Runtime interface by domain sub-packages.
func (b *Builtins) CheckURLSSRF(rawURL string) error {
	return checkURLSSRF(rawURL, b.config.NetConstraints)
}

// CheckHostSSRFMethod validates a hostname against the configured SSRF constraints.
// Method version for use on the Runtime interface by domain sub-packages.
func (b *Builtins) CheckHostSSRF(host string) error {
	return checkHostSSRF(host, b.config.NetConstraints)
}

// HostMatchesListMethod checks whether a host matches any entry in a list.
// Method version for use on the Runtime interface by domain sub-packages.
func (b *Builtins) HostMatchesList(host string, list []string) bool {
	return hostMatchesList(host, list)
}

// NewSSRFProtectedHTTPClient returns an *http.Client with the configured SSRF protections.
// Method version for use on the Runtime interface by domain sub-packages.
func (b *Builtins) NewSSRFProtectedHTTPClient() *http.Client {
	return newSSRFProtectedHTTPClient(b.config.NetConstraints)
}

// AllowHosts returns the configured allow-hosts list.
// Exported for use by domain sub-packages.
func (b *Builtins) AllowHosts() []string { return b.config.NetConstraints.AllowHosts }

// DenyHosts returns the configured deny-hosts list.
// Exported for use by domain sub-packages.
func (b *Builtins) DenyHosts() []string { return b.config.NetConstraints.DenyHosts }

// AllowLocal returns the configured allow-local flag.
// Exported for use by domain sub-packages.
func (b *Builtins) AllowLocal() *bool { return b.config.NetConstraints.AllowLocal }

// MaxDownload returns the configured max download byte limit.
// Exported for use by domain sub-packages.
func (b *Builtins) MaxDownload() int64 { return b.config.NetConstraints.MaxDownload }

// FetchReadableURL adapts the root readable-fetch helper to the net sub-package runtime contract.
func (b *Builtins) FetchReadableURL(ctx context.Context, rawURL string, timeout time.Duration, maxBytes, maxChars int) (netpkg.FetchedWebContent, error) {
	result, err := b.FetchReadableURLRaw(ctx, rawURL, timeout, maxBytes, maxChars)
	if err != nil {
		return netpkg.FetchedWebContent{}, err
	}
	return netpkg.FetchedWebContent(result), nil
}

// FetchReadableURLRaw fetches a URL and extracts readable content using configured constraints.
// Returns a FetchedWebContent from the root package. Domain sub-packages should adapt as needed.
func (b *Builtins) FetchReadableURLRaw(ctx context.Context, rawURL string, timeout time.Duration, maxBytes, maxChars int) (FetchedWebContent, error) {
	result, err := fetchReadableURL(ctx, rawURL, timeout, maxBytes, maxChars, b.config.NetConstraints)
	if err != nil {
		return FetchedWebContent{}, err
	}
	return FetchedWebContent(result), nil
}

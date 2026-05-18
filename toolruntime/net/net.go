// Package net implements net.http, net.fetch, web.fetch, net.download, net.upload,
// net.dns, net.ping, net.ip, net.port_check, net.cert, net.serve, and net.whois
// tool handlers for the toolruntime registry.
package net

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// Runtime is the narrow interface that net handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
	DisplayPath(absPath string) string
	RootAbs() string
	MaxReadBytes() int
	// SSRF and network constraint methods.
	CheckURLSSRF(rawURL string) error
	CheckHostSSRF(host string) error
	HostMatchesList(host string, list []string) bool
	NewSSRFProtectedHTTPClient() *http.Client
	FetchReadableURL(ctx context.Context, rawURL string, timeout time.Duration, maxBytes, maxChars int) (FetchedWebContent, error)
	AllowHosts() []string
	DenyHosts() []string
	AllowLocal() *bool
	MaxDownload() int64
}

// FetchedWebContent holds the result of a readable URL fetch.
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

// Handler is the tool handler signature.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with a net handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// DefaultWebFetchMaxChars is the default max characters for web fetch.
const DefaultWebFetchMaxChars = 8000

// ToolDefs returns all net domain tool definitions.
func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:             "net.http",
				Description:      "Full HTTP client: send a request and return the response.",
				InputSchema:      netHTTPInputSchema(),
				OutputSchema:     netHTTPOutputSchema(),
				SideEffectClass:  "external_write",
				RequiresApproval: true,
				ExecutionKey:     "net:http:{url}",
			},
			Handler: handleNetHTTP,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "net.fetch",
				Description:     "Fetch a URL and return its content as readable text (HTML tags stripped).",
				InputSchema:     netFetchInputSchema(),
				OutputSchema:    netFetchOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "net:fetch:{url}",
			},
			Handler: handleNetFetch,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "web.fetch",
				Description:     "Fetch a web page and extract readable content with title and final URL metadata.",
				InputSchema:     webFetchInputSchema(),
				OutputSchema:    webFetchOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "web:fetch:{url}",
			},
			Handler: handleWebFetch,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "net.download",
				Description:      "Download a file from a URL to a local path.",
				InputSchema:      netDownloadInputSchema(),
				OutputSchema:     netDownloadOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "net:download:{url}",
			},
			Handler: handleNetDownload,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "net.upload",
				Description:      "Upload a file via HTTP multipart form POST.",
				InputSchema:      netUploadInputSchema(),
				OutputSchema:     netUploadOutputSchema(),
				SideEffectClass:  "external_write",
				RequiresApproval: true,
				ExecutionKey:     "net:upload:{url}",
			},
			Handler: handleNetUpload,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "net.dns",
				Description:     "DNS lookup for a hostname (A, AAAA, MX, TXT, CNAME, NS).",
				InputSchema:     netDNSInputSchema(),
				OutputSchema:    netDNSOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "net:dns:{host}",
			},
			Handler: handleNetDNS,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "net.ping",
				Description:     "TCP connectivity check to a host and port.",
				InputSchema:     netPingInputSchema(),
				OutputSchema:    netPingOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "net:ping:{host}:{port}",
			},
			Handler: handleNetPing,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "net.ip",
				Description:     "List local network interfaces and their IP addresses.",
				InputSchema:     netIPInputSchema(),
				OutputSchema:    netIPOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "net:ip",
			},
			Handler: handleNetIP,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "net.port_check",
				Description:     "Batch TCP port connectivity check across multiple ports.",
				InputSchema:     netPortCheckInputSchema(),
				OutputSchema:    netPortCheckOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "net:port_check:{host}",
			},
			Handler: handleNetPortCheck,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "net.cert",
				Description:     "Inspect TLS certificate information for a host.",
				InputSchema:     netCertInputSchema(),
				OutputSchema:    netCertOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "net:cert:{host}",
			},
			Handler: handleNetCert,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "net.serve",
				Description:      "Start a background HTTP file server for the current directory or a specified path.",
				InputSchema:      netServeInputSchema(),
				OutputSchema:     netServeOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "net:serve",
			},
			Handler: handleNetServe,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "net.whois",
				Description:     "WHOIS lookup for a domain name.",
				InputSchema:     netWhoisInputSchema(),
				OutputSchema:    netWhoisOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "net:whois:{domain}",
			},
			Handler: handleNetWhois,
		},
	}
}

// HandleUploadForwarder exposes the net.upload handler for legacy root-package
// call sites that still need a narrow Runtime-based entry point.
func HandleUploadForwarder(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleNetUpload(ctx, rt, call)
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

func int64From(value any) (int64, error) {
	switch typed := value.(type) {
	case nil:
		return 0, nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		return int64(typed), nil
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

func intSliceFrom(value any) ([]int, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case []int:
		return append([]int(nil), typed...), nil
	case []any:
		out := make([]int, 0, len(typed))
		for _, item := range typed {
			n, err := int64From(item)
			if err != nil {
				return nil, err
			}
			out = append(out, int(n))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected array of integers, got %T", value)
	}
}

func timeoutFrom(value any, fallback time.Duration) (time.Duration, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case float64:
		if typed <= 0 {
			return fallback, nil
		}
		return time.Duration(typed * float64(time.Second)), nil
	case int:
		if typed <= 0 {
			return fallback, nil
		}
		return time.Duration(typed) * time.Second, nil
	case int64:
		if typed <= 0 {
			return fallback, nil
		}
		return time.Duration(typed) * time.Second, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback, nil
		}
		d, err := time.ParseDuration(typed)
		if err != nil {
			return 0, fmt.Errorf("invalid timeout: %w", err)
		}
		return d, nil
	default:
		return 0, fmt.Errorf("expected number or duration string for timeout, got %T", value)
	}
}

// ---------------------------------------------------------------------------
// Schema helpers — duplicated locally.
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

func stringArraySchema(description string) map[string]any {
	schema := map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string"},
	}
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
// HTML stripping
// ---------------------------------------------------------------------------

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var whitespaceCollapseRe = regexp.MustCompile(`[ \t]+`)
var blankLineCollapseRe = regexp.MustCompile(`\n{3,}`)

func stripHTML(html string) string {
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	text := scriptRe.ReplaceAllString(html, "")
	text = styleRe.ReplaceAllString(text, "")
	text = htmlTagRe.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", `"`)
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&apos;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = whitespaceCollapseRe.ReplaceAllString(text, " ")
	text = blankLineCollapseRe.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	return text
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleNetHTTP(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	rawURL, err := requiredString(call.Input, "url")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := rt.CheckURLSSRF(rawURL); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.http: %w", err)
	}
	method, _ := stringFrom(call.Input["method"])
	if method == "" {
		method = "GET"
	}
	method = strings.ToUpper(method)

	bodyStr, _ := stringFrom(call.Input["body"])

	timeout, err := timeoutFrom(call.Input["timeout_seconds"], 30*time.Second)
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var bodyReader io.Reader
	if bodyStr != "" {
		bodyReader = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.http: %w", err)
	}
	req.Header.Set("User-Agent", "HopClaw/1.0")

	if hdrs, ok := call.Input["headers"]; ok && hdrs != nil {
		if hdrMap, ok := hdrs.(map[string]any); ok {
			for k, v := range hdrMap {
				if s, ok := v.(string); ok {
					req.Header.Set(k, s)
				}
			}
		}
	}

	client := rt.NewSSRFProtectedHTTPClient()
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.http: %w", err)
	}
	defer resp.Body.Close()

	maxBytes := rt.MaxReadBytes()
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	limitedBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.http: reading body: %w", err)
	}

	respHeaders := make(map[string]string, len(resp.Header))
	for k := range resp.Header {
		respHeaders[k] = resp.Header.Get(k)
	}

	return rt.JSONResult(call, map[string]any{
		"status":      resp.Status,
		"status_code": resp.StatusCode,
		"headers":     respHeaders,
		"body":        string(limitedBody),
		"elapsed_ms":  elapsed.Milliseconds(),
	})
}

func handleNetFetch(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	rawURL, err := requiredString(call.Input, "url")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := rt.CheckURLSSRF(rawURL); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.fetch: %w", err)
	}

	timeout, err := timeoutFrom(call.Input["timeout_seconds"], 30*time.Second)
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.fetch: %w", err)
	}
	req.Header.Set("User-Agent", "HopClaw/1.0")

	resp, err := rt.NewSSRFProtectedHTTPClient().Do(req)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.fetch: %w", err)
	}
	defer resp.Body.Close()

	maxBytes := rt.MaxReadBytes()
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.fetch: reading body: %w", err)
	}

	truncated := len(raw) > maxBytes
	if truncated {
		raw = raw[:maxBytes]
	}

	contentType := resp.Header.Get("Content-Type")
	content := string(raw)
	if strings.Contains(contentType, "text/html") {
		content = stripHTML(content)
	}

	return rt.JSONResult(call, map[string]any{
		"url":          rawURL,
		"content_type": contentType,
		"content":      content,
		"truncated":    truncated,
		"bytes":        len(raw),
	})
}

func handleWebFetch(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	rawURL, err := requiredString(call.Input, "url")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := rt.CheckURLSSRF(rawURL); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("web.fetch: %w", err)
	}

	timeout, err := timeoutFrom(call.Input["timeout_seconds"], 30*time.Second)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	maxChars, err := intFrom(call.Input["max_chars"], DefaultWebFetchMaxChars)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("web.fetch: max_chars: %w", err)
	}
	if maxChars <= 0 {
		maxChars = DefaultWebFetchMaxChars
	}

	result, err := rt.FetchReadableURL(ctx, rawURL, timeout, rt.MaxReadBytes(), maxChars)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("web.fetch: %w", err)
	}
	return rt.JSONResult(call, map[string]any{
		"url":          result.URL,
		"final_url":    result.FinalURL,
		"domain":       result.Domain,
		"title":        result.Title,
		"content_type": result.ContentType,
		"content":      result.Content,
		"status_code":  result.StatusCode,
		"truncated":    result.Truncated,
		"bytes":        result.Bytes,
	})
}

func handleNetDownload(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	rawURL, err := requiredString(call.Input, "url")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := rt.CheckURLSSRF(rawURL); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.download: %w", err)
	}
	destPath, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	absPath, err := rt.ResolvePath(destPath)
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	timeout, err := timeoutFrom(call.Input["timeout_seconds"], 120*time.Second)
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.download: %w", err)
	}
	req.Header.Set("User-Agent", "HopClaw/1.0")

	resp, err := rt.NewSSRFProtectedHTTPClient().Do(req)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return contextengine.ToolResult{}, fmt.Errorf("net.download: server returned %s", resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.download: mkdir: %w", err)
	}

	f, err := os.Create(absPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.download: create: %w", err)
	}
	defer f.Close()

	maxDownload := rt.MaxDownload()
	var reader io.Reader = resp.Body
	if maxDownload > 0 {
		reader = io.LimitReader(resp.Body, maxDownload+1)
	}
	written, err := io.Copy(f, reader)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.download: write: %w", err)
	}
	if maxDownload > 0 && written > maxDownload {
		_ = f.Close()
		_ = os.Remove(absPath)
		return contextengine.ToolResult{}, fmt.Errorf("net.download: response exceeds max_download limit of %d bytes", maxDownload)
	}

	return rt.JSONResult(call, map[string]any{
		"url":           rawURL,
		"path":          rt.DisplayPath(absPath),
		"workspace":     filepath.ToSlash(rt.RootAbs()),
		"bytes_written": written,
		"content_type":  resp.Header.Get("Content-Type"),
	})
}

func handleNetUpload(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	rawURL, err := requiredString(call.Input, "url")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := rt.CheckURLSSRF(rawURL); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.upload: %w", err)
	}
	srcPath, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	absPath, err := rt.ResolvePath(srcPath)
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	fieldName, _ := stringFrom(call.Input["field"])
	if fieldName == "" {
		fieldName = "file"
	}

	method, _ := stringFrom(call.Input["method"])
	if method == "" {
		method = "POST"
	}
	method = strings.ToUpper(method)

	timeout, err := timeoutFrom(call.Input["timeout_seconds"], 120*time.Second)
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	f, err := os.Open(absPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.upload: open: %w", err)
	}
	defer f.Close()

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		part, err := writer.CreateFormFile(fieldName, filepath.Base(absPath))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.CloseWithError(writer.Close())
	}()

	req, err := http.NewRequestWithContext(ctx, method, rawURL, pr)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.upload: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "HopClaw/1.0")

	resp, err := rt.NewSSRFProtectedHTTPClient().Do(req)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.upload: %w", err)
	}
	defer resp.Body.Close()

	maxBytes := rt.MaxReadBytes()
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.upload: reading response: %w", err)
	}

	return rt.JSONResult(call, map[string]any{
		"url":         rawURL,
		"path":        rt.DisplayPath(absPath),
		"status_code": resp.StatusCode,
		"body":        string(respBody),
	})
}

func handleNetDNS(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	host, err := requiredString(call.Input, "host")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	allowHosts := rt.AllowHosts()
	denyHosts := rt.DenyHosts()
	if len(allowHosts) > 0 && !rt.HostMatchesList(host, allowHosts) {
		return contextengine.ToolResult{}, fmt.Errorf("net.dns: ssrf: host %q is not in the allow list", host)
	}
	if rt.HostMatchesList(host, denyHosts) {
		return contextengine.ToolResult{}, fmt.Errorf("net.dns: ssrf: host %q is blocked by deny list", host)
	}
	queryType, _ := stringFrom(call.Input["type"])
	if queryType == "" {
		queryType = "A"
	}
	queryType = strings.ToUpper(queryType)

	var records []string

	switch queryType {
	case "A", "AAAA":
		addrs, lookupErr := net.LookupHost(host)
		if lookupErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("net.dns: %w", lookupErr)
		}
		if queryType == "AAAA" {
			for _, addr := range addrs {
				if strings.Contains(addr, ":") {
					records = append(records, addr)
				}
			}
		} else {
			for _, addr := range addrs {
				if !strings.Contains(addr, ":") {
					records = append(records, addr)
				}
			}
		}
	case "MX":
		mxRecords, lookupErr := net.LookupMX(host)
		if lookupErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("net.dns: %w", lookupErr)
		}
		for _, mx := range mxRecords {
			records = append(records, fmt.Sprintf("%s (pref %d)", mx.Host, mx.Pref))
		}
	case "TXT":
		txtRecords, lookupErr := net.LookupTXT(host)
		if lookupErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("net.dns: %w", lookupErr)
		}
		records = txtRecords
	case "CNAME":
		cname, lookupErr := net.LookupCNAME(host)
		if lookupErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("net.dns: %w", lookupErr)
		}
		records = []string{cname}
	case "NS":
		nsRecords, lookupErr := net.LookupNS(host)
		if lookupErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("net.dns: %w", lookupErr)
		}
		for _, ns := range nsRecords {
			records = append(records, ns.Host)
		}
	default:
		return contextengine.ToolResult{}, fmt.Errorf("net.dns: unsupported query type %q", queryType)
	}

	if records == nil {
		records = []string{}
	}

	return rt.JSONResult(call, map[string]any{
		"host":    host,
		"type":    queryType,
		"records": records,
	})
}

func handleNetPing(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	host, err := requiredString(call.Input, "host")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := rt.CheckHostSSRF(host); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.ping: %w", err)
	}
	port, err := intFrom(call.Input["port"], 0)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if port <= 0 {
		return contextengine.ToolResult{}, fmt.Errorf("net.ping: port is required")
	}

	timeout, err := timeoutFrom(call.Input["timeout_seconds"], 5*time.Second)
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	start := time.Now()
	conn, dialErr := net.DialTimeout("tcp", addr, timeout)
	elapsed := time.Since(start)

	result := map[string]any{
		"host":       host,
		"port":       port,
		"reachable":  dialErr == nil,
		"elapsed_ms": elapsed.Milliseconds(),
	}

	if dialErr != nil {
		result["error"] = dialErr.Error()
	} else {
		conn.Close()
		result["error"] = ""
	}

	return rt.JSONResult(call, result)
}

func handleNetIP(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.ip: %w", err)
	}

	entries := make([]map[string]any, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, addrErr := iface.Addrs()
		if addrErr != nil {
			continue
		}
		addrStrings := make([]string, 0, len(addrs))
		for _, a := range addrs {
			addrStrings = append(addrStrings, a.String())
		}
		entries = append(entries, map[string]any{
			"name":          iface.Name,
			"hardware_addr": iface.HardwareAddr.String(),
			"flags":         iface.Flags.String(),
			"addrs":         addrStrings,
		})
	}

	return rt.JSONResult(call, map[string]any{
		"interfaces": entries,
	})
}

func handleNetPortCheck(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	host, err := requiredString(call.Input, "host")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := rt.CheckHostSSRF(host); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.port_check: %w", err)
	}
	ports, err := intSliceFrom(call.Input["ports"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.port_check: %w", err)
	}
	if len(ports) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("net.port_check: ports is required")
	}

	timeout, err := timeoutFrom(call.Input["timeout_seconds"], 3*time.Second)
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	type indexedResult struct {
		idx  int
		port int
		open bool
	}
	ch := make(chan indexedResult, len(ports))

	for i, p := range ports {
		go func(idx, port int) {
			addr := net.JoinHostPort(host, strconv.Itoa(port))
			conn, dialErr := net.DialTimeout("tcp", addr, timeout)
			open := dialErr == nil
			if open {
				conn.Close()
			}
			ch <- indexedResult{idx: idx, port: port, open: open}
		}(i, p)
	}

	results := make([]map[string]any, len(ports))
	for range ports {
		r := <-ch
		results[r.idx] = map[string]any{
			"port": r.port,
			"open": r.open,
		}
	}

	return rt.JSONResult(call, map[string]any{
		"host":    host,
		"results": results,
	})
}

func handleNetCert(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	host, err := requiredString(call.Input, "host")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := rt.CheckHostSSRF(host); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.cert: %w", err)
	}
	port, err := intFrom(call.Input["port"], 443)
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 10 * time.Second},
		"tcp",
		addr,
		&tls.Config{
			InsecureSkipVerify: false,
			ServerName:         host,
		},
	)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.cert: %w", err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("net.cert: no certificates returned")
	}

	leaf := certs[0]
	now := time.Now()

	dnsNames := leaf.DNSNames
	if dnsNames == nil {
		dnsNames = []string{}
	}

	return rt.JSONResult(call, map[string]any{
		"host":       host,
		"port":       port,
		"subject":    leaf.Subject.String(),
		"issuer":     leaf.Issuer.String(),
		"not_before": leaf.NotBefore.Format(time.RFC3339),
		"not_after":  leaf.NotAfter.Format(time.RFC3339),
		"dns_names":  dnsNames,
		"is_expired": now.After(leaf.NotAfter),
	})
}

func handleNetServe(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	dir, _ := stringFrom(call.Input["directory"])
	if dir == "" {
		dir = "."
	}
	portNum, _ := intFrom(call.Input["port"], 0)

	allowLocal := rt.AllowLocal()
	if allowLocal != nil && !*allowLocal {
		return contextengine.ToolResult{}, fmt.Errorf("net.serve: local listening is disabled by net constraints (allow_local=false)")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(portNum))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.serve: %w", err)
	}
	assignedPort := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", assignedPort)

	server := &http.Server{Handler: http.FileServer(http.Dir(dir))}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		server.Shutdown(shutCtx)
	}()
	go server.Serve(listener)

	return rt.JSONResult(call, map[string]any{
		"url":       url,
		"port":      assignedPort,
		"directory": dir,
		"status":    "serving",
	})
}

func handleNetWhois(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	domain, err := requiredString(call.Input, "domain")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	allowHosts := rt.AllowHosts()
	denyHosts := rt.DenyHosts()
	if len(allowHosts) > 0 && !rt.HostMatchesList(domain, allowHosts) {
		return contextengine.ToolResult{}, fmt.Errorf("net.whois: ssrf: domain %q is not in the allow list", domain)
	}
	if rt.HostMatchesList(domain, denyHosts) {
		return contextengine.ToolResult{}, fmt.Errorf("net.whois: ssrf: domain %q is blocked by deny list", domain)
	}

	whoisServer := "whois.iana.org:43"

	conn, err := net.DialTimeout("tcp", whoisServer, 10*time.Second)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.whois: connect: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(15 * time.Second))

	_, err = fmt.Fprintf(conn, "%s\r\n", domain)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.whois: write: %w", err)
	}

	maxBytes := rt.MaxReadBytes()
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	raw, err := io.ReadAll(io.LimitReader(conn, int64(maxBytes)))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.whois: read: %w", err)
	}

	return rt.JSONResult(call, map[string]any{
		"domain": domain,
		"raw":    string(raw),
		"server": "whois.iana.org",
	})
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func netHTTPInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":    stringSchema("The URL to send the request to."),
		"method": stringSchema("HTTP method (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS). Default: GET."),
		"headers": map[string]any{
			"type":        "object",
			"description": "Optional request headers as key-value pairs.",
			"additionalProperties": map[string]any{
				"type": "string",
			},
		},
		"body":            stringSchema("Optional request body string."),
		"timeout_seconds": map[string]any{"type": "number", "description": "Request timeout in seconds. Default: 30."},
	}, "url")
}

func netFetchInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":             stringSchema("The URL to fetch."),
		"timeout_seconds": map[string]any{"type": "number", "description": "Request timeout in seconds. Default: 30."},
	}, "url")
}

func webFetchInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":             stringSchema("The HTTP or HTTPS URL to fetch."),
		"max_chars":       integerSchema("Maximum characters to return after extraction (default: 8000)."),
		"timeout_seconds": map[string]any{"type": "number", "description": "Request timeout in seconds. Default: 30."},
	}, "url")
}

func netDownloadInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":             stringSchema("The URL to download from."),
		"path":            stringSchema("Destination file path relative to the workspace root."),
		"timeout_seconds": map[string]any{"type": "number", "description": "Download timeout in seconds. Default: 120."},
	}, "url", "path")
}

func netUploadInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":             stringSchema("The URL to upload to."),
		"path":            stringSchema("Source file path relative to the workspace root."),
		"field":           stringSchema("Form field name for the file. Default: file."),
		"method":          stringSchema("HTTP method to use (POST or PUT). Default: POST."),
		"timeout_seconds": map[string]any{"type": "number", "description": "Upload timeout in seconds. Default: 120."},
	}, "url", "path")
}

func netDNSInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"host": stringSchema("Hostname to look up."),
		"type": stringSchema("DNS record type: A, AAAA, MX, TXT, CNAME, NS. Default: A."),
	}, "host")
}

func netPingInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"host":            stringSchema("Hostname or IP address to check."),
		"port":            integerSchema("TCP port number to connect to."),
		"timeout_seconds": map[string]any{"type": "number", "description": "Connection timeout in seconds. Default: 5."},
	}, "host", "port")
}

func netIPInputSchema() map[string]any {
	return objectSchema(map[string]any{})
}

func netPortCheckInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"host": stringSchema("Hostname or IP address to check."),
		"ports": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "integer"},
			"description": "List of TCP port numbers to check.",
		},
		"timeout_seconds": map[string]any{"type": "number", "description": "Per-port connection timeout in seconds. Default: 3."},
	}, "host", "ports")
}

func netCertInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"host": stringSchema("Hostname to inspect the TLS certificate for."),
		"port": integerSchema("Port number. Default: 443."),
	}, "host")
}

func netServeInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"dir":  stringSchema("Directory to serve. Defaults to workspace root."),
		"port": integerSchema("Port number. Default: 0 (auto-assign)."),
	})
}

func netWhoisInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"domain": stringSchema("Domain name to look up."),
	}, "domain")
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func netHTTPOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"status":      stringSchema("HTTP status line (e.g. '200 OK')."),
		"status_code": integerSchema("HTTP status code."),
		"headers": map[string]any{
			"type":        "object",
			"description": "Response headers.",
			"additionalProperties": map[string]any{
				"type": "string",
			},
		},
		"body":       stringSchema("Response body (truncated to MaxReadBytes)."),
		"elapsed_ms": integerSchema("Request duration in milliseconds."),
	}, "status", "status_code", "headers", "body", "elapsed_ms")
}

func netFetchOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":          stringSchema("The fetched URL."),
		"content_type": stringSchema("Content-Type header from the response."),
		"content":      stringSchema("Readable text content (HTML stripped if applicable)."),
		"truncated":    booleanSchema("Whether the content was truncated."),
		"bytes":        integerSchema("Number of raw bytes read."),
	}, "url", "content_type", "content", "truncated", "bytes")
}

func webFetchOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":          stringSchema("Original fetched URL."),
		"final_url":    stringSchema("Final URL after redirects."),
		"domain":       stringSchema("Normalized source domain."),
		"title":        stringSchema("Best-effort page title."),
		"content_type": stringSchema("Content-Type header from the response."),
		"content":      stringSchema("Readable extracted content."),
		"status_code":  integerSchema("HTTP status code."),
		"truncated":    booleanSchema("Whether the content was truncated."),
		"bytes":        integerSchema("Number of raw bytes read."),
	}, "url", "final_url", "domain", "title", "content_type", "content", "status_code", "truncated", "bytes")
}

func netDownloadOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":           stringSchema("The URL downloaded from."),
		"path":          stringSchema("Destination path relative to workspace root."),
		"workspace":     stringSchema("Workspace root."),
		"bytes_written": integerSchema("Number of bytes written to disk."),
		"content_type":  stringSchema("Content-Type header from the response."),
	}, "url", "path", "workspace", "bytes_written", "content_type")
}

func netUploadOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":         stringSchema("The URL uploaded to."),
		"path":        stringSchema("Source file path."),
		"status_code": integerSchema("HTTP status code from the server."),
		"body":        stringSchema("Response body from the server."),
	}, "url", "path", "status_code", "body")
}

func netDNSOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"host":    stringSchema("Queried hostname."),
		"type":    stringSchema("DNS record type queried."),
		"records": stringArraySchema("Resolved DNS records."),
	}, "host", "type", "records")
}

func netPingOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"host":       stringSchema("Checked hostname."),
		"port":       integerSchema("Checked port number."),
		"reachable":  booleanSchema("Whether the TCP connection succeeded."),
		"elapsed_ms": integerSchema("Connection time in milliseconds."),
		"error":      stringSchema("Error message if connection failed."),
	}, "host", "port", "reachable", "elapsed_ms")
}

func netIPOutputSchema() map[string]any {
	ifaceEntry := objectSchema(map[string]any{
		"name":          stringSchema("Interface name."),
		"hardware_addr": stringSchema("MAC address."),
		"flags":         stringSchema("Interface flags."),
		"addrs":         stringArraySchema("IP addresses with subnet masks."),
	}, "name", "hardware_addr", "flags", "addrs")
	return objectSchema(map[string]any{
		"interfaces": arraySchema(ifaceEntry, "Network interfaces."),
	}, "interfaces")
}

func netPortCheckOutputSchema() map[string]any {
	portEntry := objectSchema(map[string]any{
		"port": integerSchema("Port number."),
		"open": booleanSchema("Whether the port is open."),
	}, "port", "open")
	return objectSchema(map[string]any{
		"host":    stringSchema("Checked hostname."),
		"results": arraySchema(portEntry, "Per-port check results."),
	}, "host", "results")
}

func netCertOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"host":       stringSchema("Inspected hostname."),
		"port":       integerSchema("Port number used."),
		"subject":    stringSchema("Certificate subject."),
		"issuer":     stringSchema("Certificate issuer."),
		"not_before": stringSchema("Certificate validity start (RFC3339)."),
		"not_after":  stringSchema("Certificate validity end (RFC3339)."),
		"dns_names":  stringArraySchema("Subject Alternative Names."),
		"is_expired": booleanSchema("Whether the certificate has expired."),
	}, "host", "port", "subject", "issuer", "not_before", "not_after", "dns_names", "is_expired")
}

func netServeOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"message": stringSchema("Status message."),
	}, "message")
}

func netWhoisOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"domain": stringSchema("Queried domain."),
		"raw":    stringSchema("Raw WHOIS response text."),
		"server": stringSchema("WHOIS server used."),
	}, "domain", "raw", "server")
}

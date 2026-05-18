package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

const (
	defaultFeishuBaseURL          = "https://open.feishu.cn/open-apis"
	defaultNotionBaseURL          = "https://api.notion.com"
	defaultNotionVersion          = "2022-06-28"
	defaultGoogleDriveBaseURL     = "https://www.googleapis.com"
	defaultGoogleDriveExportMIME  = "text/plain"
	defaultYuqueBaseURL           = "https://www.yuque.com/api/v2"
	defaultTencentDocsBaseURL     = "https://docs.qq.com"
	defaultConnectorTimeout       = 20 * time.Second
	maxConnectorDepth             = 8
	tencentDocsExportPollAttempts = 8
	tencentDocsExportPollInterval = 300 * time.Millisecond
)

var (
	reFeishuDocToken      = regexp.MustCompile(`/(?:docx|docs)/([A-Za-z0-9]+)`)
	reNotionPageID        = regexp.MustCompile(`([0-9a-fA-F]{32}|[0-9a-fA-F]{8}-[0-9a-fA-F-]{27})`)
	reConfluencePageIDURL = regexp.MustCompile(`/pages/([0-9]+)`)
	reGoogleDriveFileID   = regexp.MustCompile(`/(?:d|folders)/([A-Za-z0-9_-]+)`)
	reYuqueRepoURL        = regexp.MustCompile(`https?://[^/]+/([^/]+/[^/?#]+)`)
	reYuqueDocURL         = regexp.MustCompile(`https?://[^/]+/([^/]+/[^/]+)/([^/?#]+)`)
	reTencentDocsFileID   = regexp.MustCompile(`/(?:doc|sheet|slide|mindmap|pdf)/([A-Za-z0-9_-]+)`)
)

func connectorHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: defaultConnectorTimeout}
}

func toObjectSlice(values []any) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, raw := range values {
		item, ok := raw.(map[string]any)
		if ok {
			out = append(out, item)
		}
	}
	return out
}

func doJSON(ctx context.Context, client *http.Client, method, endpoint string, headers map[string]string, requestBody any, out any) error {
	bodyBytes, err := doRequest(ctx, client, method, endpoint, headers, requestBody)
	if err != nil {
		return err
	}
	if out == nil || len(bodyBytes) == 0 {
		return nil
	}
	if err := json.Unmarshal(bodyBytes, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func doText(ctx context.Context, client *http.Client, method, endpoint string, headers map[string]string, requestBody any) (string, error) {
	bodyBytes, err := doRequest(ctx, client, method, endpoint, headers, requestBody)
	if err != nil {
		return "", err
	}
	return normalizeChunkContent(string(bodyBytes)), nil
}

func doRequest(ctx context.Context, client *http.Client, method, endpoint string, headers map[string]string, requestBody any) ([]byte, error) {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceReadBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	return bodyBytes, nil
}

func nestedString(root map[string]any, path ...string) string {
	current := any(root)
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = obj[key]
	}
	switch typed := current.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func nestedSlice(root map[string]any, path ...string) []any {
	current := any(root)
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = obj[key]
	}
	items, _ := current.([]any)
	return items
}

func parseConnectorTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC(), false
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts.UTC(), true
	}
	if ts, err := time.Parse("2006-01-02T15:04:05.000Z", raw); err == nil {
		return ts.UTC(), true
	}
	return time.Now().UTC(), false
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func trimConnectorPath(value string) string {
	return strings.Trim(strings.TrimSpace(value), "/")
}

func htmlToText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	tokenizer := xhtml.NewTokenizer(strings.NewReader(raw))
	var builder strings.Builder
	for {
		tt := tokenizer.Next()
		switch tt {
		case xhtml.ErrorToken:
			return normalizeChunkContent(htmlpkg.UnescapeString(builder.String()))
		case xhtml.TextToken:
			text := strings.TrimSpace(tokenizer.Token().Data)
			if text == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			builder.WriteString(text)
		case xhtml.StartTagToken, xhtml.EndTagToken, xhtml.SelfClosingTagToken:
			name := strings.ToLower(tokenizer.Token().Data)
			switch name {
			case "p", "div", "br", "li", "ul", "ol", "h1", "h2", "h3", "h4", "h5", "h6", "tr":
				builder.WriteByte('\n')
			}
		}
	}
}

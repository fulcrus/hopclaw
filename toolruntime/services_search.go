package toolruntime

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
)

const (
	defaultSearchCount       = 8
	maxSearchCount           = 10
	defaultNewsFetchTopN     = 3
	maxNewsFetchTopN         = 5
	defaultWebFetchMaxChars  = 8000
	defaultNewsFetchMaxChars = 4000
)

type searchQueryParams struct {
	Query          string
	Count          int
	Freshness      string
	DateAfter      string
	DateBefore     string
	Language       string
	Region         string
	Domains        []string
	ExcludeDomains []string
}

type searchResultItem struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet,omitempty"`
	Source      string `json:"source,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Domain      string `json:"domain,omitempty"`
}

type searchToolPayload struct {
	Provider      string             `json:"provider"`
	Kind          string             `json:"kind"`
	Query         string             `json:"query"`
	ExecutedQuery string             `json:"executed_query"`
	StatusCode    int                `json:"status_code"`
	ResultCount   int                `json:"result_count"`
	Results       []searchResultItem `json:"results"`
}

type fetchedWebContent struct {
	URL         string `json:"url"`
	FinalURL    string `json:"final_url,omitempty"`
	Domain      string `json:"domain,omitempty"`
	Title       string `json:"title,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Content     string `json:"content"`
	StatusCode  int    `json:"status_code"`
	Truncated   bool   `json:"truncated"`
	Bytes       int    `json:"bytes"`
}

type newsDigestItem struct {
	Rank        int    `json:"rank"`
	Title       string `json:"title"`
	Source      string `json:"source,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	URL         string `json:"url"`
	Domain      string `json:"domain,omitempty"`
	Snippet     string `json:"snippet,omitempty"`
	Summary     string `json:"summary,omitempty"`
}

var htmlTitleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
var metaDescriptionRe = regexp.MustCompile(`(?is)<meta[^>]+(?:name|property)=["'](?:description|og:description)["'][^>]+content=["']([^"']+)["'][^>]*>`)

func parseSearchQueryParams(input map[string]any) (searchQueryParams, error) {
	query, err := requiredString(input, "query")
	if err != nil {
		return searchQueryParams{}, err
	}
	count, err := intFrom(input["count"], defaultSearchCount)
	if err != nil {
		return searchQueryParams{}, fmt.Errorf("count: %w", err)
	}
	if count <= 0 {
		count = defaultSearchCount
	}
	if count > maxSearchCount {
		count = maxSearchCount
	}
	freshness, err := stringFrom(input["freshness"])
	if err != nil {
		return searchQueryParams{}, fmt.Errorf("freshness: %w", err)
	}
	freshness = strings.ToLower(strings.TrimSpace(freshness))
	if freshness != "" {
		switch freshness {
		case "day", "week", "month", "year":
		default:
			return searchQueryParams{}, fmt.Errorf("freshness must be one of day, week, month, or year")
		}
	}
	dateAfter, err := stringFrom(input["date_after"])
	if err != nil {
		return searchQueryParams{}, fmt.Errorf("date_after: %w", err)
	}
	dateBefore, err := stringFrom(input["date_before"])
	if err != nil {
		return searchQueryParams{}, fmt.Errorf("date_before: %w", err)
	}
	language, err := stringFrom(input["language"])
	if err != nil {
		return searchQueryParams{}, fmt.Errorf("language: %w", err)
	}
	region, err := stringFrom(input["region"])
	if err != nil {
		return searchQueryParams{}, fmt.Errorf("region: %w", err)
	}
	domains, err := flexibleStringSliceFrom(input["domains"])
	if err != nil {
		return searchQueryParams{}, fmt.Errorf("domains: %w", err)
	}
	excludeDomains, err := flexibleStringSliceFrom(input["exclude_domains"])
	if err != nil {
		return searchQueryParams{}, fmt.Errorf("exclude_domains: %w", err)
	}
	return searchQueryParams{
		Query:          strings.TrimSpace(query),
		Count:          count,
		Freshness:      freshness,
		DateAfter:      strings.TrimSpace(dateAfter),
		DateBefore:     strings.TrimSpace(dateBefore),
		Language:       strings.TrimSpace(language),
		Region:         strings.TrimSpace(region),
		Domains:        trimStrings(domains),
		ExcludeDomains: trimStrings(excludeDomains),
	}, nil
}

func executeSearchTool(ctx context.Context, svc SearchServiceConfig, toolName string, params searchQueryParams) (searchToolPayload, error) {
	kind := "web"
	if toolName == "search.news" || toolName == "news.digest" {
		kind = "news"
	}

	raw, statusCode, executedQuery, err := performSearchRequest(ctx, svc, kind, params)
	if err != nil {
		return searchToolPayload{}, err
	}
	results := normalizeSearchResults(strings.ToLower(strings.TrimSpace(svc.Provider)), kind, raw)
	return searchToolPayload{
		Provider:      strings.ToLower(strings.TrimSpace(svc.Provider)),
		Kind:          kind,
		Query:         params.Query,
		ExecutedQuery: executedQuery,
		StatusCode:    statusCode,
		ResultCount:   len(results),
		Results:       results,
	}, nil
}

func performSearchRequest(ctx context.Context, svc SearchServiceConfig, kind string, params searchQueryParams) (any, int, string, error) {
	provider := strings.ToLower(strings.TrimSpace(svc.Provider))
	if provider == "" {
		provider = "generic"
	}
	executedQuery := buildExecutedSearchQuery(params)

	switch provider {
	case "serpapi":
		endpoint := svc.BaseURL
		if endpoint == "" {
			endpoint = "https://serpapi.com/search.json"
		}
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, 0, executedQuery, err
		}
		q := req.URL.Query()
		q.Set("q", executedQuery)
		q.Set("engine", "google")
		q.Set("output", "json")
		if svc.APIKey != "" {
			q.Set("api_key", svc.APIKey)
		}
		q.Set("num", fmt.Sprintf("%d", params.Count))
		if kind == "news" {
			q.Set("tbm", "nws")
		}
		if tbs := serpAPITimeFilter(params); tbs != "" {
			q.Set("tbs", tbs)
		}
		if params.Region != "" {
			q.Set("gl", strings.ToLower(params.Region))
		}
		if params.Language != "" {
			q.Set("hl", strings.ToLower(params.Language))
		}
		req.URL.RawQuery = q.Encode()
		raw, statusCode, err := executeJSONRequest(req)
		return raw, statusCode, executedQuery, err
	case "tavily":
		endpoint := svc.BaseURL
		if endpoint == "" {
			endpoint = "https://api.tavily.com/search"
		}
		body := map[string]any{
			"query":        executedQuery,
			"api_key":      svc.APIKey,
			"max_results":  params.Count,
			"search_depth": "basic",
		}
		if kind == "news" {
			body["topic"] = "news"
		} else {
			body["topic"] = "general"
		}
		if len(params.Domains) > 0 {
			body["include_domains"] = params.Domains
		}
		if len(params.ExcludeDomains) > 0 {
			body["exclude_domains"] = params.ExcludeDomains
		}
		if params.Freshness != "" {
			body["time_range"] = params.Freshness
		}
		if params.Region != "" {
			body["region"] = params.Region
		}
		if params.Language != "" {
			body["language"] = params.Language
		}
		req, err := jsonRequest(ctx, endpoint, body, svc.APIKey)
		if err != nil {
			return nil, 0, executedQuery, err
		}
		raw, statusCode, err := executeJSONRequest(req)
		return raw, statusCode, executedQuery, err
	case "bing":
		endpoint := svc.BaseURL
		if endpoint == "" {
			if kind == "news" {
				endpoint = "https://api.bing.microsoft.com/v7.0/news/search"
			} else {
				endpoint = "https://api.bing.microsoft.com/v7.0/search"
			}
		}
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, 0, executedQuery, err
		}
		q := req.URL.Query()
		q.Set("q", executedQuery)
		q.Set("count", fmt.Sprintf("%d", params.Count))
		if params.Freshness != "" {
			q.Set("freshness", params.Freshness)
		}
		if params.Region != "" {
			q.Set("mkt", params.Region)
		}
		if params.Language != "" {
			q.Set("setLang", params.Language)
		}
		req.URL.RawQuery = q.Encode()
		if svc.APIKey != "" {
			req.Header.Set("Ocp-Apim-Subscription-Key", svc.APIKey)
		}
		raw, statusCode, err := executeJSONRequest(req)
		return raw, statusCode, executedQuery, err
	case "google":
		endpoint := svc.BaseURL
		if endpoint == "" {
			endpoint = "https://www.googleapis.com/customsearch/v1"
		}
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, 0, executedQuery, err
		}
		q := req.URL.Query()
		q.Set("q", executedQuery)
		q.Set("num", fmt.Sprintf("%d", params.Count))
		if svc.APIKey != "" {
			q.Set("key", svc.APIKey)
		}
		if kind == "news" {
			q.Set("tbm", "nws")
		}
		if params.Region != "" {
			q.Set("gl", strings.ToLower(params.Region))
		}
		if params.Language != "" {
			q.Set("lr", "lang_"+strings.ToLower(params.Language))
		}
		req.URL.RawQuery = q.Encode()
		raw, statusCode, err := executeJSONRequest(req)
		return raw, statusCode, executedQuery, err
	default:
		if svc.BaseURL == "" {
			return nil, 0, executedQuery, fmt.Errorf("search service base_url is required for provider %q", provider)
		}
		body := map[string]any{
			"query":           executedQuery,
			"kind":            kind,
			"count":           params.Count,
			"freshness":       params.Freshness,
			"date_after":      params.DateAfter,
			"date_before":     params.DateBefore,
			"language":        params.Language,
			"region":          params.Region,
			"domains":         params.Domains,
			"exclude_domains": params.ExcludeDomains,
		}
		req, err := jsonRequest(ctx, svc.BaseURL, body, svc.APIKey)
		if err != nil {
			return nil, 0, executedQuery, err
		}
		raw, statusCode, err := executeJSONRequest(req)
		return raw, statusCode, executedQuery, err
	}
}

func jsonRequest(ctx context.Context, endpoint string, body map[string]any, apiKey string) (*http.Request, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	return req, nil
}

func executeJSONRequest(req *http.Request) (any, int, error) {
	resp, err := serviceHTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("read body: %w", err)
	}

	var data any
	if err := json.Unmarshal(respBody, &data); err != nil {
		data = map[string]any{"body": string(respBody)}
	}
	return data, resp.StatusCode, nil
}

func buildExecutedSearchQuery(params searchQueryParams) string {
	query := strings.TrimSpace(params.Query)
	for _, domain := range params.Domains {
		if domain != "" {
			query += " site:" + domain
		}
	}
	for _, domain := range params.ExcludeDomains {
		if domain != "" {
			query += " -site:" + domain
		}
	}
	return strings.TrimSpace(query)
}

func serpAPITimeFilter(params searchQueryParams) string {
	if params.DateAfter != "" || params.DateBefore != "" {
		after := params.DateAfter
		if after == "" {
			after = "1970-01-01"
		}
		before := params.DateBefore
		if before == "" {
			before = time.Now().Format("2006-01-02")
		}
		return "cdr:1,cd_min:" + after + ",cd_max:" + before
	}
	switch params.Freshness {
	case "day":
		return "qdr:d"
	case "week":
		return "qdr:w"
	case "month":
		return "qdr:m"
	case "year":
		return "qdr:y"
	default:
		return ""
	}
}

func normalizeSearchResults(provider, kind string, raw any) []searchResultItem {
	switch provider {
	case "serpapi":
		if data, ok := raw.(map[string]any); ok {
			key := "organic_results"
			if kind == "news" {
				key = "news_results"
			}
			return dedupeSearchResults(normalizeSearchEntryArray(data[key], map[string][]string{
				"title":       {"title"},
				"url":         {"link", "url"},
				"snippet":     {"snippet", "summary", "snippet_highlighted_words"},
				"source":      {"source"},
				"publishedAt": {"date"},
			}))
		}
	case "tavily":
		if data, ok := raw.(map[string]any); ok {
			return dedupeSearchResults(normalizeSearchEntryArray(data["results"], map[string][]string{
				"title":       {"title"},
				"url":         {"url"},
				"snippet":     {"content", "snippet", "description"},
				"source":      {"source"},
				"publishedAt": {"published_date"},
			}))
		}
	case "bing":
		if data, ok := raw.(map[string]any); ok {
			value := data["value"]
			if kind != "news" {
				if webPages, ok := data["webPages"].(map[string]any); ok {
					value = webPages["value"]
				}
			}
			return dedupeSearchResults(normalizeSearchEntryArray(value, map[string][]string{
				"title":       {"name", "title"},
				"url":         {"url"},
				"snippet":     {"description", "snippet"},
				"source":      {"provider", "siteName"},
				"publishedAt": {"datePublished"},
			}))
		}
	case "google":
		if data, ok := raw.(map[string]any); ok {
			return dedupeSearchResults(normalizeSearchEntryArray(data["items"], map[string][]string{
				"title":       {"title"},
				"url":         {"link"},
				"snippet":     {"snippet"},
				"source":      {"displayLink"},
				"publishedAt": {"published_at"},
			}))
		}
	}

	if data, ok := raw.(map[string]any); ok {
		for _, candidate := range []any{
			data["results"],
			data["items"],
			data["articles"],
			data["news_results"],
			data["organic_results"],
			data["value"],
		} {
			items := dedupeSearchResults(normalizeSearchEntryArray(candidate, map[string][]string{
				"title":       {"title", "name", "headline"},
				"url":         {"url", "link", "href"},
				"snippet":     {"snippet", "description", "summary", "content", "body", "text"},
				"source":      {"source", "siteName", "displayLink", "publisher"},
				"publishedAt": {"published_at", "published", "published_date", "date", "datePublished"},
			}))
			if len(items) > 0 {
				return items
			}
		}
	}
	return nil
}

func normalizeSearchEntryArray(value any, fieldMap map[string][]string) []searchResultItem {
	entries, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]searchResultItem, 0, len(entries))
	for _, entry := range entries {
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		item := searchResultItem{
			Title:       pickString(obj, fieldMap["title"]...),
			URL:         pickString(obj, fieldMap["url"]...),
			Snippet:     flattenStringValue(obj, fieldMap["snippet"]...),
			Source:      pickSource(obj, fieldMap["source"]...),
			PublishedAt: pickString(obj, fieldMap["publishedAt"]...),
		}
		if item.URL == "" && item.Title == "" {
			continue
		}
		if item.Source == "" {
			item.Source = hostFromURL(item.URL)
		}
		item.Domain = hostFromURL(item.URL)
		item.Snippet = collapseWhitespace(item.Snippet)
		items = append(items, item)
	}
	return items
}

func dedupeSearchResults(items []searchResultItem) []searchResultItem {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]searchResultItem, 0, len(items))
	for _, item := range items {
		key := item.URL
		if key == "" {
			key = item.Title + "|" + item.Source
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func pickString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(searchStringValue(obj[key]))
		if value != "" {
			return value
		}
	}
	return ""
}

func flattenStringValue(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := collapseWhitespace(searchStringValue(obj[key])); value != "" {
			return value
		}
	}
	return ""
}

func pickSource(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := obj[key].(type) {
		case []any:
			for _, item := range value {
				if sourceObj, ok := item.(map[string]any); ok {
					if name := pickString(sourceObj, "name", "title"); name != "" {
						return name
					}
				}
			}
		default:
			if picked := strings.TrimSpace(searchStringValue(value)); picked != "" {
				return picked
			}
		}
	}
	return ""
}

func searchStringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		return fmt.Sprintf("%v", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case []string:
		return strings.Join(typed, " ")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(searchStringValue(item))
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	case map[string]any:
		if content := pickString(typed, "text", "content", "value", "name"); content != "" {
			return content
		}
		return ""
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func trimStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func flexibleStringSliceFrom(value any) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		parts := strings.Split(typed, ",")
		return trimStrings(parts), nil
	default:
		return stringSliceFrom(value)
	}
}

func hostFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := neturl.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(host), "www.")
}

func collapseWhitespace(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = whitespaceCollapseRe.ReplaceAllString(text, " ")
	text = blankLineCollapseRe.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func fetchReadableURL(ctx context.Context, rawURL string, timeout time.Duration, maxBytes, maxChars int, constraints config.NetConstraints) (fetchedWebContent, error) {
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	if maxChars <= 0 {
		maxChars = defaultWebFetchMaxChars
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return fetchedWebContent{}, err
	}
	req.Header.Set("User-Agent", "HopClaw/1.0")

	resp, err := newSSRFProtectedHTTPClient(constraints).Do(req)
	if err != nil {
		return fetchedWebContent{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return fetchedWebContent{}, fmt.Errorf("reading body: %w", err)
	}
	truncated := len(raw) > maxBytes
	if truncated {
		raw = raw[:maxBytes]
	}

	body := string(raw)
	contentType := resp.Header.Get("Content-Type")
	title := ""
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		title = extractHTMLTitle(body)
		body = stripHTML(body)
		if body == "" {
			body = extractMetaDescription(string(raw))
		}
	}
	body, charTrimmed := truncateRunes(collapseWhitespace(body), maxChars)
	truncated = truncated || charTrimmed

	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return fetchedWebContent{
		URL:         rawURL,
		FinalURL:    finalURL,
		Domain:      hostFromURL(finalURL),
		Title:       title,
		ContentType: contentType,
		Content:     body,
		StatusCode:  resp.StatusCode,
		Truncated:   truncated,
		Bytes:       len(raw),
	}, nil
}

func extractHTMLTitle(html string) string {
	matches := htmlTitleRe.FindStringSubmatch(html)
	if len(matches) < 2 {
		return ""
	}
	return collapseWhitespace(stripHTML(matches[1]))
}

func extractMetaDescription(html string) string {
	matches := metaDescriptionRe.FindStringSubmatch(html)
	if len(matches) < 2 {
		return ""
	}
	return collapseWhitespace(matches[1])
}

func truncateRunes(text string, max int) (string, bool) {
	if max <= 0 {
		return text, false
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text, false
	}
	return strings.TrimSpace(string(runes[:max])) + "...", true
}

func newsDigestExec(ctx context.Context, _ *ws, config BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	svc := config.Services.Search
	if !svc.IsConfigured() {
		return notConfiguredResult(call, "tools.services.search")
	}

	params, err := parseSearchQueryParams(call.Input)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	fetchTopN, err := intFrom(call.Input["fetch_top_n"], defaultNewsFetchTopN)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("fetch_top_n: %w", err)
	}
	if fetchTopN < 0 {
		fetchTopN = 0
	}
	if fetchTopN > maxNewsFetchTopN {
		fetchTopN = maxNewsFetchTopN
	}

	searchPayload, err := executeSearchTool(ctx, svc, call.Name, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s: %w", call.Name, err)
	}

	items := make([]newsDigestItem, 0, len(searchPayload.Results))
	fetchedCount := 0
	for i, result := range searchPayload.Results {
		item := newsDigestItem{
			Rank:        i + 1,
			Title:       result.Title,
			Source:      result.Source,
			PublishedAt: result.PublishedAt,
			URL:         result.URL,
			Domain:      result.Domain,
			Snippet:     result.Snippet,
			Summary:     summarizeNewsContent(result.Snippet, ""),
		}
		if i < fetchTopN && result.URL != "" {
			fetched, fetchErr := fetchReadableURL(ctx, result.URL, 15*time.Second, 256*1024, defaultNewsFetchMaxChars, config.NetConstraints)
			if fetchErr == nil {
				fetchedCount++
				if item.Title == "" {
					item.Title = fetched.Title
				}
				if item.Domain == "" {
					item.Domain = fetched.Domain
				}
				if item.Source == "" {
					item.Source = fetched.Domain
				}
				item.Summary = summarizeNewsContent(result.Snippet, fetched.Content)
			}
		}
		items = append(items, item)
	}

	markdownTable := renderNewsMarkdownTable(items)
	csvTable, err := renderNewsCSV(items)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("news.digest: render csv: %w", err)
	}

	body, err := json.MarshalIndent(map[string]any{
		"provider":       searchPayload.Provider,
		"query":          searchPayload.Query,
		"executed_query": searchPayload.ExecutedQuery,
		"status_code":    searchPayload.StatusCode,
		"result_count":   len(items),
		"fetched_count":  fetchedCount,
		"items":          items,
		"markdown_table": markdownTable,
		"csv":            csvTable,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}, nil
}

func summarizeNewsContent(snippet, content string) string {
	base := collapseWhitespace(content)
	if base == "" {
		base = collapseWhitespace(snippet)
	}
	if base == "" {
		return ""
	}
	base, _ = truncateRunes(base, 220)
	return base
}

func renderNewsMarkdownTable(items []newsDigestItem) string {
	var b strings.Builder
	b.WriteString("| # | Title | Source | Published | Summary |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, item := range items {
		title := escapeMarkdownTableText(item.Title)
		if item.URL != "" {
			title = fmt.Sprintf("[%s](%s)", title, item.URL)
		}
		source := escapeMarkdownTableText(item.Source)
		if source == "" {
			source = escapeMarkdownTableText(item.Domain)
		}
		published := escapeMarkdownTableText(item.PublishedAt)
		summary := escapeMarkdownTableText(item.Summary)
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s |\n", item.Rank, title, source, published, summary)
	}
	return b.String()
}

func renderNewsCSV(items []newsDigestItem) (string, error) {
	var b bytes.Buffer
	writer := csv.NewWriter(&b)
	if err := writer.Write([]string{"rank", "title", "source", "published_at", "url", "summary"}); err != nil {
		return "", err
	}
	for _, item := range items {
		if err := writer.Write([]string{
			fmt.Sprintf("%d", item.Rank),
			item.Title,
			item.Source,
			item.PublishedAt,
			item.URL,
			item.Summary,
		}); err != nil {
			return "", err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", err
	}
	return b.String(), nil
}

func escapeMarkdownTableText(text string) string {
	text = strings.ReplaceAll(text, "|", `\|`)
	text = strings.ReplaceAll(text, "\n", " ")
	return strings.TrimSpace(text)
}

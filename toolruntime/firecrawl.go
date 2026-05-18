package toolruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// ---------------------------------------------------------------------------
// Firecrawl — intelligent web scraping via the Firecrawl API
// ---------------------------------------------------------------------------

const (
	firecrawlDefaultBaseURL   = "https://api.firecrawl.dev/v1"
	firecrawlEnvKey           = "FIRECRAWL_API_KEY"
	firecrawlDefaultTimeout   = 30 * time.Second
	firecrawlMaxResponseBytes = 512 * 1024
)

// FirecrawlClient provides intelligent web scraping via the Firecrawl API.
type FirecrawlClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewFirecrawlClient creates a new Firecrawl client. The API key is resolved
// from the provided value or the FIRECRAWL_API_KEY environment variable.
func NewFirecrawlClient(apiKey, baseURL string) *FirecrawlClient {
	if apiKey == "" {
		apiKey = os.Getenv(firecrawlEnvKey)
	}
	if baseURL == "" {
		baseURL = firecrawlDefaultBaseURL
	}
	return &FirecrawlClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: firecrawlDefaultTimeout},
	}
}

// FirecrawlResult holds the scraped content for a single URL.
type FirecrawlResult struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
	HTML     string `json:"html,omitempty"`
}

// CrawlOptions configures a multi-page crawl job.
type CrawlOptions struct {
	MaxPages int      `json:"max_pages,omitempty"`
	Includes []string `json:"includes,omitempty"` // URL patterns to include
	Excludes []string `json:"excludes,omitempty"` // URL patterns to exclude
}

// CrawlJob holds the response from starting a crawl job.
type CrawlJob struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
}

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

type firecrawlScrapeRequest struct {
	URL     string   `json:"url"`
	Formats []string `json:"formats"`
}

type firecrawlScrapeResponse struct {
	Success bool                `json:"success"`
	Data    firecrawlScrapeData `json:"data"`
	Error   string              `json:"error,omitempty"`
}

type firecrawlScrapeData struct {
	Markdown string                  `json:"markdown"`
	HTML     string                  `json:"html,omitempty"`
	Metadata firecrawlScrapeMetadata `json:"metadata"`
}

type firecrawlScrapeMetadata struct {
	Title string `json:"title"`
}

type firecrawlCrawlRequest struct {
	URL          string   `json:"url"`
	Limit        int      `json:"limit,omitempty"`
	IncludePaths []string `json:"includePaths,omitempty"`
	ExcludePaths []string `json:"excludePaths,omitempty"`
}

type firecrawlCrawlResponse struct {
	Success bool   `json:"success"`
	ID      string `json:"id"`
	Error   string `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// API methods
// ---------------------------------------------------------------------------

// Scrape fetches and cleans a single URL, returning markdown content.
func (c *FirecrawlClient) Scrape(ctx context.Context, url string) (*FirecrawlResult, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("firecrawl: API key is required (set %s)", firecrawlEnvKey)
	}

	reqBody := firecrawlScrapeRequest{
		URL:     url,
		Formats: []string{"markdown"},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("firecrawl: marshal request: %w", err)
	}

	endpoint := c.baseURL + "/scrape"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("firecrawl: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("firecrawl: send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(firecrawlMaxResponseBytes)))
	if err != nil {
		return nil, fmt.Errorf("firecrawl: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("firecrawl: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var scrapeResp firecrawlScrapeResponse
	if err := json.Unmarshal(respBody, &scrapeResp); err != nil {
		return nil, fmt.Errorf("firecrawl: unmarshal response: %w", err)
	}

	if !scrapeResp.Success {
		errMsg := scrapeResp.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, fmt.Errorf("firecrawl: scrape failed: %s", errMsg)
	}

	return &FirecrawlResult{
		URL:      url,
		Title:    scrapeResp.Data.Metadata.Title,
		Markdown: scrapeResp.Data.Markdown,
		HTML:     scrapeResp.Data.HTML,
	}, nil
}

// Crawl starts a crawl job for a URL and its linked pages.
func (c *FirecrawlClient) Crawl(ctx context.Context, url string, opts CrawlOptions) (*CrawlJob, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("firecrawl: API key is required (set %s)", firecrawlEnvKey)
	}

	reqBody := firecrawlCrawlRequest{
		URL:          url,
		Limit:        opts.MaxPages,
		IncludePaths: opts.Includes,
		ExcludePaths: opts.Excludes,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("firecrawl: marshal request: %w", err)
	}

	endpoint := c.baseURL + "/crawl"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("firecrawl: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("firecrawl: send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(firecrawlMaxResponseBytes)))
	if err != nil {
		return nil, fmt.Errorf("firecrawl: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("firecrawl: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var crawlResp firecrawlCrawlResponse
	if err := json.Unmarshal(respBody, &crawlResp); err != nil {
		return nil, fmt.Errorf("firecrawl: unmarshal response: %w", err)
	}

	if !crawlResp.Success {
		errMsg := crawlResp.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, fmt.Errorf("firecrawl: crawl failed: %s", errMsg)
	}

	return &CrawlJob{
		ID:      crawlResp.ID,
		Success: crawlResp.Success,
	}, nil
}

// ---------------------------------------------------------------------------
// Tool definitions — registered as net.firecrawl_scrape and net.firecrawl_crawl
// ---------------------------------------------------------------------------

func firecrawlToolDefs(_ BuiltinsConfig) []builtinToolDef {
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "net.firecrawl_scrape",
				Description:     "Scrape a URL using the Firecrawl API and return clean markdown content.",
				InputSchema:     firecrawlScrapeInputSchema(),
				OutputSchema:    firecrawlScrapeOutputSchema(),
				SideEffectClass: "external_write",
				ExecutionKey:    "net:firecrawl_scrape:{url}",
			},
			Handler: handleFirecrawlScrape,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "net.firecrawl_crawl",
				Description:      "Start a multi-page crawl job using the Firecrawl API.",
				InputSchema:      firecrawlCrawlInputSchema(),
				OutputSchema:     firecrawlCrawlOutputSchema(),
				SideEffectClass:  "external_write",
				RequiresApproval: true,
				ExecutionKey:     "net:firecrawl_crawl:{url}",
			},
			Handler: handleFirecrawlCrawl,
		},
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleFirecrawlScrape(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	rawURL, err := requiredString(call.Input, "url")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	apiKey, _ := stringFrom(call.Input["api_key"])
	baseURL, _ := stringFrom(call.Input["base_url"])

	client := NewFirecrawlClient(apiKey, baseURL)
	result, err := client.Scrape(ctx, rawURL)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.firecrawl_scrape: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"url":      result.URL,
		"title":    result.Title,
		"markdown": result.Markdown,
	})
}

func handleFirecrawlCrawl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	rawURL, err := requiredString(call.Input, "url")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	apiKey, _ := stringFrom(call.Input["api_key"])
	baseURL, _ := stringFrom(call.Input["base_url"])
	maxPages, _ := intFrom(call.Input["max_pages"], 0)

	var includes, excludes []string
	if inc, ok := call.Input["includes"]; ok {
		includes, _ = stringSliceFrom(inc)
	}
	if exc, ok := call.Input["excludes"]; ok {
		excludes, _ = stringSliceFrom(exc)
	}

	client := NewFirecrawlClient(apiKey, baseURL)
	job, err := client.Crawl(ctx, rawURL, CrawlOptions{
		MaxPages: maxPages,
		Includes: includes,
		Excludes: excludes,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("net.firecrawl_crawl: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"crawl_id": job.ID,
		"success":  job.Success,
	})
}

// stringSliceFrom is defined in builtin.go — shared across the package.

// ---------------------------------------------------------------------------
// Input/Output schemas
// ---------------------------------------------------------------------------

func firecrawlScrapeInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":      stringSchema("The URL to scrape."),
		"api_key":  stringSchema("Firecrawl API key. If omitted, uses FIRECRAWL_API_KEY env var."),
		"base_url": stringSchema("Firecrawl API base URL. Default: https://api.firecrawl.dev/v1."),
	}, "url")
}

func firecrawlScrapeOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":      stringSchema("The scraped URL."),
		"title":    stringSchema("Page title extracted from metadata."),
		"markdown": stringSchema("Page content converted to clean markdown."),
	}, "url", "title", "markdown")
}

func firecrawlCrawlInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":       stringSchema("The starting URL to crawl."),
		"max_pages": integerSchema("Maximum number of pages to crawl."),
		"includes":  stringArraySchema("URL path patterns to include."),
		"excludes":  stringArraySchema("URL path patterns to exclude."),
		"api_key":   stringSchema("Firecrawl API key. If omitted, uses FIRECRAWL_API_KEY env var."),
		"base_url":  stringSchema("Firecrawl API base URL. Default: https://api.firecrawl.dev/v1."),
	}, "url")
}

func firecrawlCrawlOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"crawl_id": stringSchema("The crawl job ID for status polling."),
		"success":  booleanSchema("Whether the crawl job was accepted."),
	}, "crawl_id", "success")
}

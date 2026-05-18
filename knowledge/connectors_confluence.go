package knowledge

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ConfluenceConnector struct {
	Client *http.Client
}

func (c *ConfluenceConnector) Sync(ctx context.Context, source Source) ([]DocumentSnapshot, error) {
	client := connectorHTTPClient(c.Client)
	baseURL := trimBaseURL(configString(source.Config, "base_url"))
	if baseURL == "" {
		return nil, fmt.Errorf("confluence source requires base_url")
	}
	pageIDs := cleanConfigStrings(append(configStrings(source.Config, "page_ids"), extractConfluencePageIDs(configStrings(source.Config, "page_urls"))...))
	if len(pageIDs) == 0 {
		return nil, fmt.Errorf("confluence source requires page_ids or page_urls")
	}
	headers, err := confluenceHeaders(source.Config)
	if err != nil {
		return nil, err
	}
	includeDescendants := true
	if value := source.Config["include_descendants"]; value != nil {
		includeDescendants = configBool(source.Config, "include_descendants")
	}

	seen := map[string]struct{}{}
	snapshots := make([]DocumentSnapshot, 0, len(pageIDs))
	for _, pageID := range pageIDs {
		pages, err := confluenceFetchPageTree(ctx, client, baseURL, headers, pageID, includeDescendants)
		if err != nil {
			return nil, err
		}
		for _, page := range pages {
			id := configString(page, "id")
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			title := configString(page, "title")
			body := confluenceBodyText(page)
			if strings.TrimSpace(body) == "" {
				body = title
			}
			uri := confluencePageURL(baseURL, page)
			updatedAt := confluenceUpdatedAt(page)
			snapshots = append(snapshots, buildDocumentSnapshot(Document{
				ID:              id,
				SourceID:        source.ID,
				Kind:            DocumentKindRemoteDoc,
				Title:           title,
				Path:            id,
				URI:             uri,
				Locale:          source.Locale,
				Bytes:           int64(len(body)),
				SourceUpdatedAt: updatedAt,
				Metadata: DocumentMetadata{
					MIMEType: "text/html",
				},
			}, body))
		}
	}
	return snapshots, nil
}

func confluenceHeaders(config map[string]any) (map[string]string, error) {
	token, err := resolveSecretConfigString(config, "token")
	if err != nil {
		return nil, err
	}
	if token != "" {
		return map[string]string{"Authorization": "Bearer " + token}, nil
	}
	email := configString(config, "email")
	apiToken, err := resolveSecretConfigString(config, "api_token")
	if err != nil {
		return nil, err
	}
	password, err := resolveSecretConfigString(config, "password")
	if err != nil {
		return nil, err
	}
	if email != "" && apiToken != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(email + ":" + apiToken))
		return map[string]string{"Authorization": "Basic " + auth}, nil
	}
	if email != "" && password != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(email + ":" + password))
		return map[string]string{"Authorization": "Basic " + auth}, nil
	}
	return nil, fmt.Errorf("confluence source requires token or email with api_token/password")
}

func confluenceFetchPageTree(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, pageID string, includeDescendants bool) ([]map[string]any, error) {
	page, err := confluenceFetchPage(ctx, client, baseURL, headers, pageID)
	if err != nil {
		return nil, err
	}
	pages := []map[string]any{page}
	if !includeDescendants {
		return pages, nil
	}
	endpoint := absoluteURL(baseURL, "/rest/api/content/"+url.PathEscape(pageID)+"/descendant/page?expand=body.storage,version")
	var response map[string]any
	if err := doJSON(ctx, client, http.MethodGet, endpoint, headers, nil, &response); err != nil {
		return nil, fmt.Errorf("fetch confluence descendants for %s: %w", pageID, err)
	}
	pages = append(pages, extractConfluenceResults(response)...)
	return pages, nil
}

func confluenceFetchPage(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, pageID string) (map[string]any, error) {
	endpoint := absoluteURL(baseURL, "/rest/api/content/"+url.PathEscape(pageID)+"?expand=body.storage,version")
	var response map[string]any
	if err := doJSON(ctx, client, http.MethodGet, endpoint, headers, nil, &response); err != nil {
		return nil, fmt.Errorf("fetch confluence page %s: %w", pageID, err)
	}
	return response, nil
}

func confluenceBodyText(page map[string]any) string {
	value := nestedString(page, "body", "storage", "value")
	return normalizeChunkContent(htmlToText(value))
}

func confluenceUpdatedAt(page map[string]any) time.Time {
	when := nestedString(page, "version", "when")
	updatedAt, _ := parseConnectorTime(when)
	return updatedAt
}

func confluencePageURL(baseURL string, page map[string]any) string {
	if links, ok := page["_links"].(map[string]any); ok {
		if webUI := configString(links, "webui"); webUI != "" {
			return absoluteURL(baseURL, webUI)
		}
	}
	if id := configString(page, "id"); id != "" {
		return absoluteURL(baseURL, "/pages/"+id)
	}
	return baseURL
}

func extractConfluenceResults(response map[string]any) []map[string]any {
	if results, ok := response["results"].([]any); ok {
		return toObjectSlice(results)
	}
	if pageMap, ok := response["page"].(map[string]any); ok {
		if results, ok := pageMap["results"].([]any); ok {
			return toObjectSlice(results)
		}
	}
	out := make([]map[string]any, 0)
	for _, raw := range response {
		if entry, ok := raw.(map[string]any); ok {
			if results, ok := entry["results"].([]any); ok {
				out = append(out, toObjectSlice(results)...)
			}
		}
	}
	return out
}

func extractConfluencePageIDs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if matches := reConfluencePageIDURL.FindStringSubmatch(value); len(matches) == 2 {
			out = append(out, matches[1])
			continue
		}
		out = append(out, value)
	}
	return cleanConfigStrings(out)
}

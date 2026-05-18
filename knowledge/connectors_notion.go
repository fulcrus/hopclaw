package knowledge

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type NotionConnector struct {
	Client *http.Client
}

func (c *NotionConnector) Sync(ctx context.Context, source Source) ([]DocumentSnapshot, error) {
	client := connectorHTTPClient(c.Client)
	baseURL := trimBaseURL(configString(source.Config, "base_url"))
	if baseURL == "" {
		baseURL = defaultNotionBaseURL
	}
	token, err := resolveSecretConfigString(source.Config, "token")
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, fmt.Errorf("notion source requires token")
	}
	pageIDs := cleanConfigStrings(append(configStrings(source.Config, "page_ids"), extractNotionPageIDs(configStrings(source.Config, "page_urls"))...))
	if len(pageIDs) == 0 {
		return nil, fmt.Errorf("notion source requires page_ids or page_urls")
	}
	version := configString(source.Config, "notion_version")
	if version == "" {
		version = defaultNotionVersion
	}
	headers := map[string]string{
		"Authorization":  "Bearer " + token,
		"Notion-Version": version,
	}
	snapshots := make([]DocumentSnapshot, 0, len(pageIDs))
	for _, pageID := range pageIDs {
		pageID = normalizeNotionID(pageID)
		page, err := notionGetPage(ctx, client, baseURL, headers, pageID)
		if err != nil {
			return nil, err
		}
		title := notionPageTitle(page, pageID)
		content, err := notionCollectPageContent(ctx, client, baseURL, headers, pageID, 0)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(content) == "" {
			content = title
		}
		uri := configString(page, "url")
		updatedAt, _ := parseConnectorTime(configString(page, "last_edited_time"))
		snapshots = append(snapshots, buildDocumentSnapshot(Document{
			ID:              pageID,
			SourceID:        source.ID,
			Kind:            DocumentKindRemoteDoc,
			Title:           title,
			Path:            pageID,
			URI:             uri,
			Locale:          source.Locale,
			Bytes:           int64(len(content)),
			SourceUpdatedAt: updatedAt,
			Metadata: DocumentMetadata{
				MIMEType: "text/plain",
			},
		}, content))
	}
	return snapshots, nil
}

func notionGetPage(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, pageID string) (map[string]any, error) {
	var page map[string]any
	if err := doJSON(ctx, client, http.MethodGet, absoluteURL(baseURL, "/v1/pages/"+url.PathEscape(pageID)), headers, nil, &page); err != nil {
		return nil, fmt.Errorf("fetch notion page %s: %w", pageID, err)
	}
	return page, nil
}

func notionPageTitle(page map[string]any, fallback string) string {
	if props, ok := page["properties"].(map[string]any); ok {
		if title := notionTitle(props); title != "" {
			return title
		}
	}
	if title := configString(page, "title"); title != "" {
		return title
	}
	return fallback
}

func notionCollectPageContent(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, blockID string, depth int) (string, error) {
	if depth >= maxConnectorDepth {
		return "", nil
	}
	var lines []string
	cursor := ""
	for {
		endpoint := absoluteURL(baseURL, "/v1/blocks/"+url.PathEscape(blockID)+"/children?page_size=100")
		if cursor != "" {
			endpoint += "&start_cursor=" + url.QueryEscape(cursor)
		}
		var response map[string]any
		if err := doJSON(ctx, client, http.MethodGet, endpoint, headers, nil, &response); err != nil {
			return "", fmt.Errorf("fetch notion blocks %s: %w", blockID, err)
		}
		results, _ := response["results"].([]any)
		for _, raw := range results {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if line := notionBlockText(block); line != "" {
				lines = append(lines, line)
			}
			if childID := configString(block, "id"); childID != "" && configBool(block, "has_children") {
				childContent, err := notionCollectPageContent(ctx, client, baseURL, headers, childID, depth+1)
				if err != nil {
					return "", err
				}
				if strings.TrimSpace(childContent) != "" {
					lines = append(lines, childContent)
				}
			}
		}
		if !configBool(response, "has_more") {
			break
		}
		cursor = configString(response, "next_cursor")
		if cursor == "" {
			break
		}
	}
	return normalizeChunkContent(strings.Join(lines, "\n")), nil
}

func notionBlockText(block map[string]any) string {
	blockType := configString(block, "type")
	if blockType == "" {
		return ""
	}
	contentObj, _ := block[blockType].(map[string]any)
	if len(contentObj) == 0 {
		return ""
	}
	if rich, ok := contentObj["rich_text"].([]any); ok {
		if text := joinRichText(rich); text != "" {
			return text
		}
	}
	if rich, ok := contentObj["title"].([]any); ok {
		if text := joinRichText(rich); text != "" {
			return text
		}
	}
	if rich, ok := contentObj["caption"].([]any); ok {
		if text := joinRichText(rich); text != "" {
			return text
		}
	}
	switch blockType {
	case "child_page":
		return "Page: " + configString(contentObj, "title")
	case "bookmark", "embed", "link_preview":
		return configString(contentObj, "url")
	case "equation":
		return configString(contentObj, "expression")
	case "table_of_contents":
		return "Table of contents"
	default:
		if urlValue := configString(contentObj, "url"); urlValue != "" {
			return urlValue
		}
	}
	return ""
}

func extractNotionPageIDs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if matches := reNotionPageID.FindStringSubmatch(value); len(matches) >= 2 {
			out = append(out, normalizeNotionID(matches[1]))
			continue
		}
		out = append(out, normalizeNotionID(value))
	}
	return cleanConfigStrings(out)
}

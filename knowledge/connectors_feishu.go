package knowledge

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type FeishuDocsConnector struct {
	Client *http.Client
}

func (c *FeishuDocsConnector) Sync(ctx context.Context, source Source) ([]DocumentSnapshot, error) {
	client := connectorHTTPClient(c.Client)
	baseURL := trimBaseURL(configString(source.Config, "base_url"))
	if baseURL == "" {
		baseURL = defaultFeishuBaseURL
	}
	token, err := feishuAccessToken(ctx, client, baseURL, source.Config)
	if err != nil {
		return nil, err
	}

	documentIDs := cleanConfigStrings(append(configStrings(source.Config, "document_ids"), extractFeishuDocIDs(configStrings(source.Config, "document_urls"))...))
	wikiTokens := cleanConfigStrings(configStrings(source.Config, "wiki_node_tokens"))
	if len(documentIDs) == 0 && len(wikiTokens) == 0 {
		return nil, fmt.Errorf("feishu source requires document_ids, document_urls, or wiki_node_tokens")
	}

	snapshots := make([]DocumentSnapshot, 0, len(documentIDs)+len(wikiTokens))
	for _, documentID := range documentIDs {
		title, rawContent, uri, updatedAt, err := fetchFeishuDocument(ctx, client, baseURL, token, documentID)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, buildDocumentSnapshot(Document{
			ID:              documentID,
			SourceID:        source.ID,
			Kind:            DocumentKindRemoteDoc,
			Title:           title,
			Path:            documentID,
			URI:             uri,
			Locale:          source.Locale,
			Bytes:           int64(len(rawContent)),
			SourceUpdatedAt: updatedAt,
			Metadata: DocumentMetadata{
				MIMEType: "text/plain",
			},
		}, rawContent))
	}
	for _, wikiToken := range wikiTokens {
		documentID, title, err := resolveFeishuWikiNode(ctx, client, baseURL, token, wikiToken)
		if err != nil {
			return nil, err
		}
		resolvedTitle, rawContent, uri, updatedAt, err := fetchFeishuDocument(ctx, client, baseURL, token, documentID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(title) == "" {
			title = resolvedTitle
		}
		snapshots = append(snapshots, buildDocumentSnapshot(Document{
			ID:              documentID,
			SourceID:        source.ID,
			Kind:            DocumentKindRemoteDoc,
			Title:           title,
			Path:            documentID,
			URI:             uri,
			Locale:          source.Locale,
			Bytes:           int64(len(rawContent)),
			SourceUpdatedAt: updatedAt,
			Metadata: DocumentMetadata{
				MIMEType: "text/plain",
			},
		}, rawContent))
	}
	return snapshots, nil
}

func feishuAccessToken(ctx context.Context, client *http.Client, baseURL string, config map[string]any) (string, error) {
	token, err := resolveSecretConfigString(config, "tenant_access_token")
	if err != nil {
		return "", err
	}
	if token != "" {
		return token, nil
	}
	appID := configString(config, "app_id")
	appSecret, err := resolveSecretConfigString(config, "app_secret")
	if err != nil {
		return "", err
	}
	if appID == "" || appSecret == "" {
		return "", fmt.Errorf("feishu source requires tenant_access_token or app_id/app_secret")
	}
	var response map[string]any
	if err := doJSON(ctx, client, http.MethodPost, absoluteURL(baseURL, "/auth/v3/tenant_access_token/internal"), nil, map[string]any{
		"app_id":     appID,
		"app_secret": appSecret,
	}, &response); err != nil {
		return "", err
	}
	token = configString(response, "tenant_access_token")
	if token == "" {
		if data, ok := response["data"].(map[string]any); ok {
			token = configString(data, "tenant_access_token")
		}
	}
	if token == "" {
		return "", fmt.Errorf("feishu auth response did not include tenant_access_token")
	}
	return token, nil
}

func fetchFeishuDocument(ctx context.Context, client *http.Client, baseURL, token, documentID string) (title string, rawContent string, uri string, updatedAt time.Time, err error) {
	headers := map[string]string{"Authorization": "Bearer " + token}
	var meta map[string]any
	if err = doJSON(ctx, client, http.MethodGet, absoluteURL(baseURL, "/docx/v1/documents/"+url.PathEscape(documentID)), headers, nil, &meta); err != nil {
		return "", "", "", time.Time{}, fmt.Errorf("fetch feishu document %s: %w", documentID, err)
	}
	title = nestedString(meta, "data", "document", "title")
	if title == "" {
		title = nestedString(meta, "data", "title")
	}
	uri = nestedString(meta, "data", "document", "url")
	if uri == "" {
		uri = absoluteURL(baseURL, "/docx/"+documentID)
	}
	updatedAt, _ = parseConnectorTime(nestedString(meta, "data", "document", "updated_at"))

	var raw map[string]any
	if err = doJSON(ctx, client, http.MethodGet, absoluteURL(baseURL, "/docx/v1/documents/"+url.PathEscape(documentID)+"/raw_content"), headers, nil, &raw); err != nil {
		return "", "", "", time.Time{}, fmt.Errorf("fetch feishu raw content %s: %w", documentID, err)
	}
	rawContent = nestedString(raw, "data", "content")
	if rawContent == "" {
		rawContent = nestedString(raw, "data", "raw_content")
	}
	if rawContent == "" {
		return "", "", "", time.Time{}, fmt.Errorf("feishu document %s returned empty raw content", documentID)
	}
	return strings.TrimSpace(title), normalizeChunkContent(rawContent), uri, updatedAt, nil
}

func resolveFeishuWikiNode(ctx context.Context, client *http.Client, baseURL, token, wikiToken string) (documentID string, title string, err error) {
	headers := map[string]string{"Authorization": "Bearer " + token}
	endpoint := absoluteURL(baseURL, "/wiki/v2/spaces/get_node?token="+url.QueryEscape(wikiToken))
	var response map[string]any
	if err = doJSON(ctx, client, http.MethodGet, endpoint, headers, nil, &response); err != nil {
		return "", "", fmt.Errorf("resolve feishu wiki node %s: %w", wikiToken, err)
	}
	documentID = nestedString(response, "data", "node", "obj_token")
	if documentID == "" {
		documentID = nestedString(response, "data", "obj_token")
	}
	title = nestedString(response, "data", "node", "title")
	if documentID == "" {
		return "", "", fmt.Errorf("feishu wiki node %s did not resolve to an obj_token", wikiToken)
	}
	return documentID, title, nil
}

func extractFeishuDocIDs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if matches := reFeishuDocToken.FindStringSubmatch(value); len(matches) == 2 {
			out = append(out, matches[1])
			continue
		}
		out = append(out, value)
	}
	return cleanConfigStrings(out)
}

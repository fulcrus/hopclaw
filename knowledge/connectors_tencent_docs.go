package knowledge

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type TencentDocsConnector struct {
	Client *http.Client
}

func (c *TencentDocsConnector) Sync(ctx context.Context, source Source) ([]DocumentSnapshot, error) {
	client := connectorHTTPClient(c.Client)
	baseURL := trimBaseURL(configString(source.Config, "base_url"))
	if baseURL == "" {
		baseURL = defaultTencentDocsBaseURL
	}
	token, err := resolveSecretConfigString(source.Config, "token")
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, fmt.Errorf("tencent docs source requires token")
	}
	fileIDs := cleanConfigStrings(append(configStrings(source.Config, "file_ids"), extractTencentDocsFileIDs(configStrings(source.Config, "file_urls"))...))
	if len(fileIDs) == 0 {
		return nil, fmt.Errorf("tencent docs source requires file_ids or file_urls")
	}
	exportType := configString(source.Config, "export_type")
	if exportType == "" {
		exportType = "txt"
	}
	headers := map[string]string{"Authorization": "Bearer " + token}

	snapshots := make([]DocumentSnapshot, 0, len(fileIDs))
	for _, fileID := range fileIDs {
		meta, err := tencentDocsFileMetadata(ctx, client, baseURL, headers, fileID)
		if err != nil {
			return nil, err
		}
		downloadURL, err := tencentDocsExportFile(ctx, client, baseURL, headers, fileID, exportType)
		if err != nil {
			return nil, err
		}
		content, err := doText(ctx, client, http.MethodGet, downloadURL, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("download tencent docs export %s: %w", fileID, err)
		}
		title := nestedString(meta, "data", "title")
		if title == "" {
			title = nestedString(meta, "data", "name")
		}
		if title == "" {
			title = fileID
		}
		uri := nestedString(meta, "data", "url")
		updatedAt, _ := parseConnectorTime(nestedString(meta, "data", "updated_at"))
		snapshots = append(snapshots, buildDocumentSnapshot(Document{
			ID:              fileID,
			SourceID:        source.ID,
			Kind:            DocumentKindRemoteDoc,
			Title:           title,
			Path:            fileID,
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

func tencentDocsFileMetadata(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, fileID string) (map[string]any, error) {
	var response map[string]any
	endpoint := absoluteURL(baseURL, "/openapi/drive/v2/files/"+url.PathEscape(fileID)+"/metadata")
	if err := doJSON(ctx, client, http.MethodGet, endpoint, headers, nil, &response); err != nil {
		return nil, fmt.Errorf("fetch tencent docs file %s: %w", fileID, err)
	}
	return response, nil
}

func tencentDocsExportFile(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, fileID string, exportType string) (string, error) {
	var createResp map[string]any
	endpoint := absoluteURL(baseURL, "/openapi/export/v2/files/"+url.PathEscape(fileID))
	if err := doJSON(ctx, client, http.MethodPost, endpoint, headers, map[string]any{"type": exportType}, &createResp); err != nil {
		return "", fmt.Errorf("request tencent docs export %s: %w", fileID, err)
	}
	if downloadURL := nestedString(createResp, "data", "download_url"); downloadURL != "" {
		return downloadURL, nil
	}
	taskID := nestedString(createResp, "data", "task_id")
	if taskID == "" {
		taskID = nestedString(createResp, "data", "operation_id")
	}
	if taskID == "" {
		return "", fmt.Errorf("tencent docs export %s did not return task_id", fileID)
	}
	statusEndpoint := absoluteURL(baseURL, "/openapi/export/v2/files/"+url.PathEscape(fileID)+"/tasks/"+url.PathEscape(taskID))
	for attempt := 0; attempt < tencentDocsExportPollAttempts; attempt++ {
		var statusResp map[string]any
		if err := doJSON(ctx, client, http.MethodGet, statusEndpoint, headers, nil, &statusResp); err != nil {
			return "", fmt.Errorf("poll tencent docs export %s: %w", fileID, err)
		}
		if downloadURL := nestedString(statusResp, "data", "download_url"); downloadURL != "" {
			return downloadURL, nil
		}
		status := strings.ToLower(nestedString(statusResp, "data", "status"))
		if status == "failed" || status == "error" {
			message := nestedString(statusResp, "data", "error_message")
			if message == "" {
				message = nestedString(statusResp, "message")
			}
			return "", fmt.Errorf("tencent docs export %s failed: %s", fileID, strings.TrimSpace(message))
		}
		if err := sleepContext(ctx, tencentDocsExportPollInterval); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("tencent docs export %s timed out", fileID)
}

func extractTencentDocsFileIDs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if matches := reTencentDocsFileID.FindStringSubmatch(value); len(matches) == 2 {
			out = append(out, matches[1])
			continue
		}
		out = append(out, value)
	}
	return cleanConfigStrings(out)
}

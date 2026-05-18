package knowledge

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type GoogleDriveConnector struct {
	Client *http.Client
}

func (c *GoogleDriveConnector) Sync(ctx context.Context, source Source) ([]DocumentSnapshot, error) {
	client := connectorHTTPClient(c.Client)
	baseURL := trimBaseURL(configString(source.Config, "base_url"))
	if baseURL == "" {
		baseURL = defaultGoogleDriveBaseURL
	}
	token, err := resolveSecretConfigString(source.Config, "token")
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, fmt.Errorf("google drive source requires token")
	}
	fileIDs := cleanConfigStrings(append(configStrings(source.Config, "file_ids"), extractGoogleDriveFileIDs(configStrings(source.Config, "file_urls"))...))
	if len(fileIDs) == 0 {
		return nil, fmt.Errorf("google drive source requires file_ids or file_urls")
	}
	exportMIMEType := configString(source.Config, "export_mime_type")
	if exportMIMEType == "" {
		exportMIMEType = defaultGoogleDriveExportMIME
	}
	headers := map[string]string{"Authorization": "Bearer " + token}

	snapshots := make([]DocumentSnapshot, 0, len(fileIDs))
	for _, fileID := range fileIDs {
		meta, err := googleDriveFileMetadata(ctx, client, baseURL, headers, fileID)
		if err != nil {
			return nil, err
		}
		content, uri, err := googleDriveFileContent(ctx, client, baseURL, headers, meta, exportMIMEType)
		if err != nil {
			return nil, err
		}
		title := configString(meta, "name")
		if title == "" {
			title = fileID
		}
		updatedAt, _ := parseConnectorTime(configString(meta, "modifiedTime"))
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
				MIMEType: configString(meta, "mimeType"),
			},
		}, content))
	}
	return snapshots, nil
}

func googleDriveFileMetadata(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, fileID string) (map[string]any, error) {
	endpoint := absoluteURL(baseURL, "/drive/v3/files/"+url.PathEscape(fileID)+"?fields=id,name,mimeType,modifiedTime,webViewLink")
	var meta map[string]any
	if err := doJSON(ctx, client, http.MethodGet, endpoint, headers, nil, &meta); err != nil {
		return nil, fmt.Errorf("fetch google drive file %s: %w", fileID, err)
	}
	return meta, nil
}

func googleDriveFileContent(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, meta map[string]any, exportMIMEType string) (string, string, error) {
	fileID := configString(meta, "id")
	mimeType := configString(meta, "mimeType")
	uri := configString(meta, "webViewLink")
	if uri == "" {
		uri = absoluteURL(baseURL, "/drive/file/d/"+fileID)
	}
	if strings.HasPrefix(mimeType, "application/vnd.google-apps") {
		endpoint := absoluteURL(baseURL, "/drive/v3/files/"+url.PathEscape(fileID)+"/export?mimeType="+url.QueryEscape(exportMIMEType))
		content, err := doText(ctx, client, http.MethodGet, endpoint, headers, nil)
		if err != nil {
			return "", "", fmt.Errorf("export google workspace file %s: %w", fileID, err)
		}
		return content, uri, nil
	}
	endpoint := absoluteURL(baseURL, "/drive/v3/files/"+url.PathEscape(fileID)+"?alt=media")
	content, err := doText(ctx, client, http.MethodGet, endpoint, headers, nil)
	if err != nil {
		return "", "", fmt.Errorf("download google drive file %s: %w", fileID, err)
	}
	return content, uri, nil
}

func extractGoogleDriveFileIDs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if matches := reGoogleDriveFileID.FindStringSubmatch(value); len(matches) == 2 {
			out = append(out, matches[1])
			continue
		}
		if parsed, err := url.Parse(value); err == nil {
			if id := strings.TrimSpace(parsed.Query().Get("id")); id != "" {
				out = append(out, id)
				continue
			}
		}
		out = append(out, value)
	}
	return cleanConfigStrings(out)
}

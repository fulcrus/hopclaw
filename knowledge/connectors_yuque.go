package knowledge

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type YuqueConnector struct {
	Client *http.Client
}

type yuqueDocRef struct {
	Repo string
	Doc  string
}

func (c *YuqueConnector) Sync(ctx context.Context, source Source) ([]DocumentSnapshot, error) {
	client := connectorHTTPClient(c.Client)
	baseURL := trimBaseURL(configString(source.Config, "base_url"))
	if baseURL == "" {
		baseURL = defaultYuqueBaseURL
	}
	token, err := resolveSecretConfigString(source.Config, "token")
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, fmt.Errorf("yuque source requires token")
	}
	headers := map[string]string{"X-Auth-Token": token}

	repoPaths := cleanConfigStrings(append(
		append(configStrings(source.Config, "repo_ids"), configStrings(source.Config, "repo_paths")...),
		extractYuqueRepoPaths(configStrings(source.Config, "repo_urls"))...,
	))
	docRefs := extractYuqueDocRefs(configStrings(source.Config, "doc_urls"))
	if len(repoPaths) == 0 {
		for _, ref := range docRefs {
			repoPaths = append(repoPaths, ref.Repo)
		}
		repoPaths = cleanConfigStrings(repoPaths)
	}
	if len(repoPaths) == 0 {
		return nil, fmt.Errorf("yuque source requires repo_paths, repo_ids, repo_urls, or doc_urls")
	}

	docFilter := map[string]map[string]struct{}{}
	for _, slug := range cleanConfigStrings(configStrings(source.Config, "doc_slugs")) {
		for _, repoPath := range repoPaths {
			if docFilter[repoPath] == nil {
				docFilter[repoPath] = map[string]struct{}{}
			}
			docFilter[repoPath][slug] = struct{}{}
		}
	}
	for _, ref := range docRefs {
		if docFilter[ref.Repo] == nil {
			docFilter[ref.Repo] = map[string]struct{}{}
		}
		docFilter[ref.Repo][ref.Doc] = struct{}{}
	}

	snapshots := make([]DocumentSnapshot, 0, len(repoPaths))
	for _, repoPath := range repoPaths {
		docs, err := yuqueRepoDocuments(ctx, client, baseURL, headers, repoPath, docFilter[repoPath])
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			docID := configString(doc, "id")
			if docID == "" {
				docID = configString(doc, "slug")
			}
			title := configString(doc, "title")
			uri := configString(doc, "url")
			body := configString(doc, "body")
			if body == "" {
				body = htmlToText(configString(doc, "body_html"))
			}
			if strings.TrimSpace(body) == "" {
				body = title
			}
			updatedAt, _ := parseConnectorTime(configString(doc, "updated_at"))
			snapshots = append(snapshots, buildDocumentSnapshot(Document{
				ID:              docID,
				SourceID:        source.ID,
				Kind:            DocumentKindRemoteDoc,
				Title:           title,
				Path:            repoPath + "/" + docID,
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

func yuqueRepoDocuments(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, repoPath string, docFilter map[string]struct{}) ([]map[string]any, error) {
	slugs := make([]string, 0, len(docFilter))
	for slug := range docFilter {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	if len(slugs) == 0 {
		var response map[string]any
		if err := doJSON(ctx, client, http.MethodGet, absoluteURL(baseURL, "/repos/"+trimConnectorPath(repoPath)+"/docs"), headers, nil, &response); err != nil {
			return nil, fmt.Errorf("list yuque docs for %s: %w", repoPath, err)
		}
		for _, item := range toObjectSlice(nestedSlice(response, "data")) {
			slug := configString(item, "slug")
			if slug == "" {
				slug = configString(item, "id")
			}
			if slug != "" {
				slugs = append(slugs, slug)
			}
		}
	}
	seen := map[string]struct{}{}
	out := make([]map[string]any, 0, len(slugs))
	for _, slug := range slugs {
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		doc, err := yuqueFetchDoc(ctx, client, baseURL, headers, repoPath, slug)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, nil
}

func yuqueFetchDoc(ctx context.Context, client *http.Client, baseURL string, headers map[string]string, repoPath string, slug string) (map[string]any, error) {
	var response map[string]any
	endpoint := absoluteURL(baseURL, "/repos/"+trimConnectorPath(repoPath)+"/docs/"+url.PathEscape(slug))
	if err := doJSON(ctx, client, http.MethodGet, endpoint, headers, nil, &response); err != nil {
		return nil, fmt.Errorf("fetch yuque doc %s/%s: %w", repoPath, slug, err)
	}
	if data, ok := response["data"].(map[string]any); ok {
		return data, nil
	}
	return response, nil
}

func extractYuqueRepoPaths(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if matches := reYuqueRepoURL.FindStringSubmatch(value); len(matches) == 2 {
			out = append(out, matches[1])
			continue
		}
		out = append(out, value)
	}
	return cleanConfigStrings(out)
}

func extractYuqueDocRefs(values []string) []yuqueDocRef {
	out := make([]yuqueDocRef, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if matches := reYuqueDocURL.FindStringSubmatch(value); len(matches) == 3 {
			ref := yuqueDocRef{Repo: matches[1], Doc: matches[2]}
			key := ref.Repo + "|" + ref.Doc
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ref)
		}
	}
	return out
}

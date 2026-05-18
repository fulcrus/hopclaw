package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/bootstrap"
	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	"github.com/fulcrus/hopclaw/skill"
)

func fetchToolSummaries(ctx context.Context, client *GatewayClient, sessionKey string) ([]replpkg.ToolSummary, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	path := "/runtime/tools"
	if trimmed := strings.TrimSpace(sessionKey); trimmed != "" {
		path += "?session_key=" + url.QueryEscape(trimmed)
	}
	var resp toolDefinitionsResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return mapToolSummaries(resp.Items), nil
}

func embeddedToolSummaries(ctx context.Context, app *bootstrap.App, sessionKey string) ([]replpkg.ToolSummary, error) {
	if app == nil || app.Runtime == nil {
		return nil, nil
	}
	items, err := app.Runtime.ListTools(ctx, strings.TrimSpace(sessionKey))
	if err != nil {
		return nil, err
	}
	return mapEmbeddedToolSummaries(items), nil
}

func mapToolSummaries(items []toolDefinitionRow) []replpkg.ToolSummary {
	out := make([]replpkg.ToolSummary, 0, len(items))
	for _, item := range items {
		out = append(out, replpkg.ToolSummary{
			Name:             strings.TrimSpace(item.Name),
			Description:      strings.TrimSpace(item.Description),
			InputSchema:      cloneJSONSchema(item.InputSchema),
			OutputSchema:     cloneJSONSchema(item.OutputSchema),
			SideEffectClass:  strings.TrimSpace(item.SideEffectClass),
			RequiresApproval: item.RequiresApproval,
			Source:           strings.TrimSpace(item.Source),
			Eligible:         item.Eligible,
		})
	}
	return out
}

func mapEmbeddedToolSummaries(items []agent.ToolDefinition) []replpkg.ToolSummary {
	out := make([]replpkg.ToolSummary, 0, len(items))
	for _, item := range items {
		out = append(out, replpkg.ToolSummary{
			Name:             strings.TrimSpace(item.Name),
			Description:      strings.TrimSpace(item.Description),
			InputSchema:      cloneJSONSchema(item.InputSchema),
			OutputSchema:     cloneJSONSchema(item.OutputSchema),
			SideEffectClass:  strings.TrimSpace(item.SideEffectClass),
			RequiresApproval: item.RequiresApproval,
			Eligible:         true,
		})
	}
	return out
}

func fetchInstalledSkillSummaries(ctx context.Context, client *GatewayClient) ([]replpkg.SkillSummary, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	var resp installedSkillsResponse
	if err := client.Get(ctx, "/operator/skills", &resp); err != nil {
		return nil, err
	}
	return mapInstalledSkillSummaries(resp.Items), nil
}

func searchSkillCatalog(ctx context.Context, client *GatewayClient, query string) ([]replpkg.SkillCatalogSummary, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	var resp catalogSkillsResponse
	path := "/operator/skills/catalog?q=" + url.QueryEscape(strings.TrimSpace(query))
	if err := client.Get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return mapCatalogSkillSummaries(resp.Items), nil
}

func fetchSkillDetail(ctx context.Context, client *GatewayClient, name string) (*replpkg.SkillDetail, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	var installed installedSkillsResponse
	if err := client.Get(ctx, "/operator/skills", &installed); err != nil {
		return nil, err
	}
	var catalog catalogSkillsResponse
	if err := client.Get(ctx, "/operator/skills/catalog?q="+url.QueryEscape(strings.TrimSpace(name)), &catalog); err != nil {
		return nil, err
	}
	return buildSkillDetail(installed.Items, catalog.Items, name), nil
}

func installSkillViaClient(ctx context.Context, client *GatewayClient, raw string, version string) (*replpkg.SkillInstallResult, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	source, name := resolveSkillInstallTarget(strings.TrimSpace(raw))
	req := map[string]any{
		"name":   name,
		"source": valueOrFallback(source, raw),
	}
	if trimmed := strings.TrimSpace(version); trimmed != "" {
		req["version"] = trimmed
	}
	var resp skillInstallResponse
	if err := client.Post(ctx, "/operator/skills/install", req, &resp); err != nil {
		return nil, err
	}
	return &replpkg.SkillInstallResult{
		SkillID:            strings.TrimSpace(valueOrFallback(resp.SkillID, name)),
		Version:            strings.TrimSpace(resp.Version),
		InstallDir:         strings.TrimSpace(resp.InstallDir),
		LockFile:           strings.TrimSpace(resp.LockFile),
		InstallerStepCount: len(resp.InstallSteps),
	}, nil
}

func removeSkillViaClient(ctx context.Context, client *GatewayClient, name string) error {
	if client == nil {
		return fmt.Errorf("gateway client is required")
	}
	var resp map[string]any
	return client.Delete(ctx, "/operator/skills/"+url.PathEscape(strings.TrimSpace(name)), &resp)
}

func embeddedInstalledSkillSummaries(app *bootstrap.App) ([]replpkg.SkillSummary, error) {
	if app == nil {
		return nil, nil
	}
	locks, err := app.InstalledSkillLocks()
	if err != nil {
		return nil, err
	}
	items := make([]replpkg.SkillSummary, 0, len(locks))
	index := make(map[string]int, len(locks))
	for _, entry := range locks {
		ref := firstNonEmptyString(strings.TrimSpace(entry.SkillID), strings.TrimSpace(entry.InstallDir))
		index[ref] = len(items)
		items = append(items, replpkg.SkillSummary{
			ID:              strings.TrimSpace(entry.SkillID),
			Name:            strings.TrimSpace(entry.SkillID),
			Version:         strings.TrimSpace(entry.Version),
			InstallDir:      strings.TrimSpace(entry.InstallDir),
			BundleDir:       strings.TrimSpace(entry.BundleDir),
			Pinned:          entry.Pinned,
			Installed:       true,
			InstalledAt:     entry.InstalledAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			DetailAvailable: true,
		})
	}
	if app.SkillService == nil {
		return items, nil
	}
	snapshot := app.SkillService.Snapshot()
	for _, pkg := range snapshot.Ordered {
		ref := strings.TrimSpace(pkg.Name())
		summary := replpkg.SkillSummary{
			ID:              ref,
			Name:            strings.TrimSpace(pkg.Name()),
			Kind:            string(pkg.Kind),
			Status:          string(pkg.Status),
			Trust:           string(pkg.Trust),
			SourceKind:      string(pkg.Source.Kind),
			Summary:         strings.TrimSpace(pkg.Prompt.Description),
			Description:     strings.TrimSpace(pkg.Prompt.Description),
			Installed:       true,
			Ready:           pkg.Status != skill.StatusBlocked,
			Eligible:        pkg.Status != skill.StatusBlocked,
			DetailAvailable: true,
		}
		if idx, ok := index[ref]; ok {
			items[idx] = mergeInstalledSkillSummary(items[idx], summary)
			continue
		}
		items = append(items, summary)
	}
	return items, nil
}

func embeddedSkillCatalogSummaries(ctx context.Context, app *bootstrap.App, query string) ([]replpkg.SkillCatalogSummary, error) {
	if app == nil {
		return nil, nil
	}
	results, err := app.SearchSkillCatalog(ctx, strings.TrimSpace(query))
	if err != nil {
		return nil, err
	}
	installedSet := map[string]bool{}
	if locks, err := app.InstalledSkillLocks(); err == nil {
		for _, entry := range locks {
			installedSet[strings.TrimSpace(entry.SkillID)] = true
		}
	}
	out := make([]replpkg.SkillCatalogSummary, 0, len(results))
	for _, entry := range results {
		ref := strings.TrimSpace(entry.ID)
		out = append(out, replpkg.SkillCatalogSummary{
			ID:              ref,
			Name:            strings.TrimSpace(entry.Name),
			Version:         strings.TrimSpace(entry.Version),
			Summary:         strings.TrimSpace(entry.Summary),
			Description:     strings.TrimSpace(entry.Summary),
			Installed:       installedSet[ref],
			SourceKind:      string(skill.SourceClawHub),
			DetailAvailable: strings.TrimSpace(entry.BundleDir) != "",
		})
	}
	return out, nil
}

func embeddedSkillDetail(ctx context.Context, app *bootstrap.App, name string) (*replpkg.SkillDetail, error) {
	installed, err := embeddedInstalledSkillSummaries(app)
	if err != nil {
		return nil, err
	}
	catalog, err := embeddedSkillCatalogSummaries(ctx, app, name)
	if err != nil {
		return nil, err
	}
	return buildSkillDetailFromSummaries(installed, catalog, name), nil
}

func embeddedInstallSkill(ctx context.Context, app *bootstrap.App, raw string, version string) (*replpkg.SkillInstallResult, error) {
	if app == nil {
		return nil, nil
	}
	result, err := app.InstallSkill(ctx, strings.TrimSpace(raw), "", strings.TrimSpace(version))
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return &replpkg.SkillInstallResult{
		SkillID:            strings.TrimSpace(result.SkillID),
		Version:            strings.TrimSpace(result.Version),
		InstallDir:         strings.TrimSpace(result.InstallDir),
		LockFile:           strings.TrimSpace(result.LockFilePath),
		InstallerStepCount: len(result.InstallerSteps),
	}, nil
}

func embeddedRemoveSkill(ctx context.Context, app *bootstrap.App, name string) error {
	if app == nil {
		return nil
	}
	return app.RemoveInstalledSkill(ctx, strings.TrimSpace(name))
}

func mapInstalledSkillSummaries(items []installedSkillRow) []replpkg.SkillSummary {
	out := make([]replpkg.SkillSummary, 0, len(items))
	for _, item := range items {
		out = append(out, replpkg.SkillSummary{
			ID:              strings.TrimSpace(item.ID),
			Name:            strings.TrimSpace(item.Name),
			Kind:            strings.TrimSpace(item.Kind),
			Status:          strings.TrimSpace(item.Status),
			Trust:           strings.TrimSpace(item.Trust),
			Version:         strings.TrimSpace(item.Version),
			InstallDir:      strings.TrimSpace(item.InstallDir),
			BundleDir:       strings.TrimSpace(item.BundleDir),
			Pinned:          item.Pinned,
			Installed:       true,
			InstalledAt:     strings.TrimSpace(item.InstalledAt),
			DetailAvailable: true,
		})
	}
	return out
}

func mapCatalogSkillSummaries(items []catalogSkillRow) []replpkg.SkillCatalogSummary {
	out := make([]replpkg.SkillCatalogSummary, 0, len(items))
	for _, item := range items {
		out = append(out, replpkg.SkillCatalogSummary{
			ID:          strings.TrimSpace(item.ID),
			Name:        strings.TrimSpace(item.Name),
			Version:     strings.TrimSpace(item.Version),
			Summary:     strings.TrimSpace(item.Summary),
			Description: strings.TrimSpace(item.Summary),
			Installed:   item.Installed,
		})
	}
	return out
}

func buildSkillDetail(installed []installedSkillRow, catalog []catalogSkillRow, name string) *replpkg.SkillDetail {
	return buildSkillDetailFromSummaries(mapInstalledSkillSummaries(installed), mapCatalogSkillSummaries(catalog), name)
}

func buildSkillDetailFromSummaries(installed []replpkg.SkillSummary, catalog []replpkg.SkillCatalogSummary, name string) *replpkg.SkillDetail {
	detail := &replpkg.SkillDetail{}
	needle := strings.TrimSpace(name)
	for i := range installed {
		if matchesSkillName(installed[i].ID, installed[i].Name, needle) {
			item := installed[i]
			detail.Installed = &item
			break
		}
	}
	for i := range catalog {
		if matchesSkillName(catalog[i].ID, catalog[i].Name, needle) {
			item := catalog[i]
			detail.Catalog = &item
			break
		}
	}
	if detail.Installed == nil && len(installed) > 0 {
		item := installed[0]
		if matchesSkillName(item.ID, item.Name, needle) || strings.TrimSpace(needle) == "" {
			detail.Installed = &item
		}
	}
	if detail.Catalog == nil && len(catalog) > 0 {
		item := catalog[0]
		if matchesSkillName(item.ID, item.Name, needle) || strings.TrimSpace(needle) == "" {
			detail.Catalog = &item
		}
	}
	if detail.Installed == nil && detail.Catalog == nil {
		return nil
	}
	return detail
}

func mergeInstalledSkillSummary(base, incoming replpkg.SkillSummary) replpkg.SkillSummary {
	if incoming.Name != "" {
		base.Name = incoming.Name
	}
	if incoming.Kind != "" {
		base.Kind = incoming.Kind
	}
	if incoming.Status != "" {
		base.Status = incoming.Status
	}
	if incoming.Trust != "" {
		base.Trust = incoming.Trust
	}
	if incoming.SourceKind != "" {
		base.SourceKind = incoming.SourceKind
	}
	if incoming.Summary != "" {
		base.Summary = incoming.Summary
	}
	if incoming.Description != "" {
		base.Description = incoming.Description
	}
	if incoming.Ready {
		base.Ready = true
	}
	if incoming.Eligible {
		base.Eligible = true
	}
	if incoming.DetailAvailable {
		base.DetailAvailable = true
	}
	return base
}

func matchesSkillName(id, name, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(id), needle) || strings.EqualFold(strings.TrimSpace(name), needle)
}

func cloneJSONSchema(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

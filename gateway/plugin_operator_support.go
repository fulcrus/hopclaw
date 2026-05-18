package gateway

import (
	"context"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/internal/telemetry"
	"github.com/fulcrus/hopclaw/plugin"
)

func requirePluginInstaller(w http.ResponseWriter, inst *plugin.Installer) bool {
	if inst == nil {
		gwError(w, http.StatusServiceUnavailable, "plugin installer not available")
		return false
	}
	return true
}

func requiredPluginNameFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		gwError(w, http.StatusBadRequest, "missing plugin name")
		return "", false
	}
	return name, true
}

type pluginOperatorDeps struct {
	pluginInstaller *plugin.Installer
	pluginRuntime   PluginRuntimeController
	moduleCatalog   *modules.Store
	diagnostics     config.DiagnosticsConfig
}

func pluginOperatorDepsFromGateway(g *Gateway) pluginOperatorDeps {
	if g == nil {
		return pluginOperatorDeps{}
	}
	return pluginOperatorDeps{
		pluginInstaller: g.pluginInstaller,
		pluginRuntime:   g.pluginRuntime,
		moduleCatalog:   g.moduleCatalog,
		diagnostics:     g.config.Diagnostics,
	}
}

func (d pluginOperatorDeps) installPlugin(ctx context.Context, req pluginInstallRequest) (*plugin.InstallResult, int, error) {
	source := strings.TrimSpace(req.Source)
	if source == "" {
		return nil, http.StatusBadRequest, errSourceRequired("plugin")
	}

	result, err := d.pluginInstaller.Install(ctx, source)
	if err != nil {
		return nil, pluginInstallErrorStatus(source, err), err
	}
	if err := d.refreshPluginRuntime(ctx); err != nil {
		if rollbackErr := d.pluginInstaller.Uninstall(result.Name); rollbackErr != nil {
			log.Warn("plugin install rollback failed", "name", result.Name, "error", rollbackErr)
		}
		return nil, http.StatusInternalServerError, err
	}
	go func(diagCfg config.DiagnosticsConfig, name, version, source string) {
		emitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := telemetry.RecordPluginInstalled(emitCtx, diagCfg, name, version, source); err != nil {
			telemetry.DebugLog(diagCfg, "plugin install telemetry failed", "name", name, "error", err)
		}
	}(d.diagnostics, result.Name, result.Version, result.Source)
	return result, 0, nil
}

func (d pluginOperatorDeps) uninstallPlugin(ctx context.Context, name string) (int, error) {
	if err := d.pluginInstaller.Uninstall(name); err != nil {
		return http.StatusInternalServerError, err
	}
	if err := d.refreshPluginRuntime(ctx); err != nil {
		return http.StatusInternalServerError, err
	}
	return 0, nil
}

func (d pluginOperatorDeps) setPluginEnabled(ctx context.Context, name string, enabled bool) (int, error) {
	var (
		applyErr error
		rollback func() error
	)
	if enabled {
		applyErr = d.pluginInstaller.Enable(name)
		rollback = func() error { return d.pluginInstaller.Disable(name) }
	} else {
		applyErr = d.pluginInstaller.Disable(name)
		rollback = func() error { return d.pluginInstaller.Enable(name) }
	}
	if applyErr != nil {
		return http.StatusBadRequest, applyErr
	}
	if err := d.refreshPluginRuntime(ctx); err != nil {
		if undoErr := rollback(); undoErr != nil {
			log.Warn("plugin state rollback failed", "name", name, "enabled", enabled, "error", undoErr)
		}
		return http.StatusInternalServerError, err
	}
	return 0, nil
}

func pluginInstallErrorStatus(source string, err error) int {
	source = strings.TrimSpace(source)
	switch {
	case source == "":
		return http.StatusBadRequest
	case strings.HasPrefix(source, "/"), strings.HasPrefix(source, "./"), strings.HasPrefix(source, "../"):
		return http.StatusBadRequest
	case strings.HasPrefix(source, "https://github.com/"), strings.HasPrefix(source, "git@"):
		return http.StatusBadGateway
	default:
		return gatewayHTTPStatusForError(err, http.StatusBadGateway)
	}
}

func (d pluginOperatorDeps) refreshPluginRuntime(ctx context.Context) error {
	if d.pluginRuntime == nil {
		return nil
	}
	return d.pluginRuntime.RefreshPlugins(ctx)
}

func (d pluginOperatorDeps) listPluginSummaries() []pluginSummary {
	entries := d.listPluginEntries()
	items := make([]pluginSummary, 0, len(entries))
	for _, entry := range entries {
		items = append(items, d.pluginSummaryFromEntry(entry))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Enabled != items[j].Enabled {
			return items[i].Enabled
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func (d pluginOperatorDeps) lookupPlugin(name string) (pluginInventoryEntry, bool) {
	name = strings.TrimSpace(name)
	for _, entry := range d.listPluginEntries() {
		if pluginInventoryEntryMatches(entry, name) {
			return entry, true
		}
	}
	return pluginInventoryEntry{}, false
}

func pluginDetailResponse(loaded plugin.LoadedPlugin, enabled bool) pluginGetResponse {
	return pluginDetailResponseWithRuntime(loaded, enabled, nil)
}

func pluginDetailResponseWithRuntime(loaded plugin.LoadedPlugin, enabled bool, runtimeModule *pluginRuntimeModuleSummary) pluginGetResponse {
	return pluginGetResponse{
		Plugin:          pluginSummaryFromLoaded(loaded, enabled, runtimeModule),
		Channels:        pluginChannelInfo(loaded),
		Tools:           len(loaded.Manifest.Tools),
		Components:      loaded.Components(),
		ComponentCounts: loaded.ComponentCounts(),
	}
}

func pluginChannelInfo(loaded plugin.LoadedPlugin) map[string]any {
	channelInfo := make(map[string]any, len(loaded.Manifest.Channels))
	for name, ch := range loaded.Manifest.Channels {
		channelInfo[name] = map[string]any{
			"type":    ch.Type,
			"command": ch.Command,
		}
	}
	return channelInfo
}

func pluginSummaryFromLoaded(loaded plugin.LoadedPlugin, enabled bool, runtimeModule *pluginRuntimeModuleSummary) pluginSummary {
	return pluginSummary{
		Name:            loaded.Manifest.Name,
		Version:         loaded.Manifest.Version,
		Description:     loaded.Manifest.Description,
		Author:          loaded.Manifest.Author,
		Enabled:         enabled,
		Dir:             loaded.Dir,
		Source:          pluginSourceKind(loaded.Dir),
		ComponentCounts: loaded.ComponentCounts(),
		RuntimeModule:   runtimeModule,
	}
}

func (d pluginOperatorDeps) pluginSummaryFromLoaded(loaded plugin.LoadedPlugin, enabled bool) pluginSummary {
	return pluginSummaryFromLoaded(loaded, enabled, d.runtimeModuleSummary(loaded))
}

func (d pluginOperatorDeps) pluginDetailResponse(loaded plugin.LoadedPlugin, enabled bool) pluginGetResponse {
	return pluginDetailResponseWithRuntime(loaded, enabled, d.runtimeModuleSummary(loaded))
}

type pluginInventoryEntry struct {
	loaded  *plugin.LoadedPlugin
	module  *modules.StaticModule
	enabled bool
}

func (d pluginOperatorDeps) listPluginEntries() []pluginInventoryEntry {
	if d.pluginInstaller == nil {
		return d.listModulePluginEntries()
	}
	enabled, disabled := d.pluginInstaller.ListInstalled()
	entries := make([]pluginInventoryEntry, 0, len(enabled)+len(disabled))
	for _, loaded := range enabled {
		entries = append(entries, d.pluginEntryFromLoaded(loaded, true))
	}
	for _, loaded := range disabled {
		entries = append(entries, d.pluginEntryFromLoaded(loaded, false))
	}
	return entries
}

func (d pluginOperatorDeps) listModulePluginEntries() []pluginInventoryEntry {
	modulesList := d.catalogPluginModules()
	entries := make([]pluginInventoryEntry, 0, len(modulesList))
	for _, module := range modulesList {
		moduleCopy := module
		entries = append(entries, pluginInventoryEntry{
			module:  &moduleCopy,
			enabled: true,
		})
	}
	return entries
}

func (d pluginOperatorDeps) pluginEntryFromLoaded(loaded plugin.LoadedPlugin, enabled bool) pluginInventoryEntry {
	loadedCopy := loaded
	entry := pluginInventoryEntry{
		loaded:  &loadedCopy,
		enabled: enabled,
	}
	if module, ok := d.catalogPluginModule(strings.TrimSpace(loaded.Manifest.Name)); ok {
		moduleCopy := module
		entry.module = &moduleCopy
	}
	return entry
}

func (d pluginOperatorDeps) pluginSummaryFromEntry(entry pluginInventoryEntry) pluginSummary {
	runtimeModule := d.runtimeModuleSummaryForEntry(entry)
	if entry.loaded != nil {
		return pluginSummaryFromLoaded(*entry.loaded, entry.enabled, runtimeModule)
	}
	if entry.module == nil {
		return pluginSummary{}
	}
	return pluginSummaryFromModule(*entry.module, entry.enabled, runtimeModule)
}

func (d pluginOperatorDeps) pluginDetailResponseFromEntry(entry pluginInventoryEntry) pluginGetResponse {
	runtimeModule := d.runtimeModuleSummaryForEntry(entry)
	if entry.loaded != nil {
		return pluginDetailResponseWithRuntime(*entry.loaded, entry.enabled, runtimeModule)
	}
	if entry.module == nil {
		return pluginGetResponse{}
	}
	return pluginDetailResponseFromModule(*entry.module, entry.enabled, runtimeModule)
}

func (d pluginOperatorDeps) runtimeModuleSummaryForEntry(entry pluginInventoryEntry) *pluginRuntimeModuleSummary {
	if entry.module != nil {
		return d.runtimeModuleSummaryFromModule(*entry.module)
	}
	if entry.loaded == nil {
		return nil
	}
	if module, ok := d.catalogPluginModule(strings.TrimSpace(entry.loaded.Manifest.Name)); ok {
		return d.runtimeModuleSummaryFromModule(module)
	}
	return nil
}

func (d pluginOperatorDeps) runtimeModuleSummaryFromModule(module modules.StaticModule) *pluginRuntimeModuleSummary {
	if d.moduleCatalog == nil {
		return nil
	}
	manifest := module.Manifest()
	health := module.Health(context.Background())
	return &pluginRuntimeModuleSummary{
		ID:                manifest.ID,
		Kind:              manifest.Kind,
		Source:            manifest.Source,
		Delivery:          manifest.Delivery,
		Level:             manifest.Level,
		Health:            &health,
		ContributionCount: module.Contributions().Count(),
		ProjectionVersion: d.moduleCatalog.ProjectionVersion(),
	}
}

func (d pluginOperatorDeps) catalogPluginModules() []modules.StaticModule {
	if d.moduleCatalog == nil {
		return nil
	}
	all := d.moduleCatalog.Modules()
	out := make([]modules.StaticModule, 0, len(all))
	for _, module := range all {
		if module.Manifest().Source != modules.SourcePlugin {
			continue
		}
		out = append(out, module)
	}
	return out
}

func (d pluginOperatorDeps) catalogPluginModule(name string) (modules.StaticModule, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return modules.StaticModule{}, false
	}
	for _, module := range d.catalogPluginModules() {
		if pluginModuleMatchesName(module, name) {
			return module, true
		}
	}
	return modules.StaticModule{}, false
}

func (d pluginOperatorDeps) runtimeModuleSummary(loaded plugin.LoadedPlugin) *pluginRuntimeModuleSummary {
	if module, ok := d.catalogPluginModule(strings.TrimSpace(loaded.Manifest.Name)); ok {
		return d.runtimeModuleSummaryFromModule(module)
	}
	return nil
}

func pluginSourceKind(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	base := filepath.Base(filepath.Dir(dir))
	if strings.EqualFold(base, ".disabled") {
		return "disabled"
	}
	if strings.Contains(dir, string(filepath.Separator)+".hopclaw"+string(filepath.Separator)) {
		return "installed"
	}
	return "local"
}

func pluginNameMatches(loaded plugin.LoadedPlugin, name string) bool {
	if strings.EqualFold(strings.TrimSpace(loaded.Manifest.Name), name) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(filepath.Base(loaded.Dir)), name)
}

func pluginInventoryEntryMatches(entry pluginInventoryEntry, name string) bool {
	name = strings.TrimSpace(name)
	switch {
	case entry.loaded != nil:
		return pluginNameMatches(*entry.loaded, name)
	case entry.module != nil:
		return pluginModuleMatchesName(*entry.module, name)
	default:
		return false
	}
}

func pluginSummaryFromModule(module modules.StaticModule, enabled bool, runtimeModule *pluginRuntimeModuleSummary) pluginSummary {
	manifest := module.Manifest()
	dir := moduleManifestMetadataString(manifest.Metadata, "dir")
	return pluginSummary{
		Name:            strings.TrimSpace(manifest.Name),
		Version:         strings.TrimSpace(manifest.Version),
		Description:     strings.TrimSpace(manifest.Description),
		Author:          moduleManifestMetadataString(manifest.Metadata, "author"),
		Enabled:         enabled,
		Dir:             dir,
		Source:          pluginSourceKind(dir),
		ComponentCounts: pluginComponentCountsFromModule(module),
		RuntimeModule:   runtimeModule,
	}
}

func pluginDetailResponseFromModule(module modules.StaticModule, enabled bool, runtimeModule *pluginRuntimeModuleSummary) pluginGetResponse {
	components := pluginComponentDescriptorsFromModule(module)
	return pluginGetResponse{
		Plugin:          pluginSummaryFromModule(module, enabled, runtimeModule),
		Channels:        pluginChannelInfoFromModule(module),
		Tools:           len(module.Contributions().Tools),
		Components:      components,
		ComponentCounts: componentCountsFromDescriptors(components),
	}
}

func pluginChannelInfoFromModule(module modules.StaticModule) map[string]any {
	contrib := module.Contributions()
	if len(contrib.Channels) == 0 {
		return nil
	}
	channelInfo := make(map[string]any, len(contrib.Channels))
	for _, ch := range contrib.Channels {
		channelInfo[ch.Name] = map[string]any{
			"type":    moduleManifestMetadataString(ch.Metadata, "type"),
			"command": moduleManifestMetadataString(ch.Metadata, "command"),
		}
	}
	return channelInfo
}

func pluginComponentDescriptorsFromModule(module modules.StaticModule) []plugin.ComponentDescriptor {
	components := module.Contributions().Components()
	if len(components) == 0 {
		return nil
	}
	out := make([]plugin.ComponentDescriptor, 0, len(components))
	for _, component := range components {
		out = append(out, plugin.ComponentDescriptor{
			Kind:        plugin.ComponentKind(component.Kind),
			Name:        strings.TrimSpace(component.Name),
			Description: strings.TrimSpace(component.Description),
			Path:        strings.TrimSpace(component.Path),
			Metadata:    cloneKnowledgeConfig(component.Metadata),
		})
	}
	return out
}

func pluginComponentCountsFromModule(module modules.StaticModule) map[string]int {
	return componentCountsFromDescriptors(pluginComponentDescriptorsFromModule(module))
}

func componentCountsFromDescriptors(items []plugin.ComponentDescriptor) map[string]int {
	if len(items) == 0 {
		return nil
	}
	counts := make(map[string]int, len(items))
	for _, item := range items {
		counts[string(item.Kind)]++
	}
	return counts
}

func pluginModuleMatchesName(module modules.StaticModule, name string) bool {
	manifest := module.Manifest()
	if strings.EqualFold(strings.TrimSpace(manifest.Name), name) {
		return true
	}
	dir := moduleManifestMetadataString(manifest.Metadata, "dir")
	if dir == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(filepath.Base(dir)), name)
}

func moduleManifestMetadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func errSourceRequired(kind string) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "resource"
	}
	return &requestValidationError{message: kind + " source is required"}
}

type requestValidationError struct {
	message string
}

func (e *requestValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

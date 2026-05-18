package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/plugin"
	"gopkg.in/yaml.v3"
)

const pluginHookSource = "plugin"

type hookBundle struct {
	Hooks []Hook `json:"hooks" yaml:"hooks"`
	Items []Hook `json:"items" yaml:"items"`
}

// CollectPluginHooks loads hook declarations from plugins and returns the
// desired hook set keyed by "plugin::hook-name".
func CollectPluginHooks(plugins *plugin.Manager) (map[string]Hook, error) {
	desired := make(map[string]Hook)
	if plugins == nil {
		return desired, nil
	}
	names := plugins.Names()
	sort.Strings(names)
	for _, name := range names {
		loaded, ok := plugins.Get(name)
		if !ok {
			continue
		}
		items, err := loadPluginHooks(loaded)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			key := pluginHookKey(item.SourceRef, item.Name)
			if _, exists := desired[key]; exists {
				return nil, fmt.Errorf("plugin hook %q declared more than once", key)
			}
			desired[key] = item
		}
	}
	return desired, nil
}

// CollectModuleHooks loads hook declarations from module hook directory
// projections and returns the desired hook set keyed by "plugin::hook-name".
func CollectModuleHooks(projections []modules.DirectoryProjection) (map[string]Hook, error) {
	desired := make(map[string]Hook)
	if len(projections) == 0 {
		return desired, nil
	}
	items := append([]modules.DirectoryProjection(nil), projections...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].ModuleID != items[j].ModuleID {
			return items[i].ModuleID < items[j].ModuleID
		}
		return items[i].Path < items[j].Path
	})
	for _, projection := range items {
		if projection.Kind != modules.ComponentKindHooksDir || projection.Source != modules.SourcePlugin {
			continue
		}
		loaded, err := loadProjectedHooks(projection)
		if err != nil {
			return nil, err
		}
		for _, item := range loaded {
			key := pluginHookKey(item.SourceRef, item.Name)
			if _, exists := desired[key]; exists {
				return nil, fmt.Errorf("plugin hook %q declared more than once", key)
			}
			desired[key] = item
		}
	}
	return desired, nil
}

// SyncPluginHooks loads hook declarations from plugin hooks directories and
// reconciles them into the runtime hook store.
func SyncPluginHooks(ctx context.Context, store Store, plugins *plugin.Manager) error {
	if store == nil {
		return nil
	}
	desired, err := CollectPluginHooks(plugins)
	if err != nil {
		return err
	}

	existing, err := store.List(ctx)
	if err != nil {
		return err
	}
	existingByKey := make(map[string]*Hook)
	for _, item := range existing {
		if item == nil || item.Source != pluginHookSource {
			continue
		}
		existingByKey[pluginHookKey(item.SourceRef, item.Name)] = item
	}

	for key, item := range existingByKey {
		if _, ok := desired[key]; ok {
			continue
		}
		if err := store.Remove(ctx, item.ID); err != nil {
			return fmt.Errorf("remove stale plugin hook %q: %w", key, err)
		}
	}

	for key, desiredHook := range desired {
		if existingHook, ok := existingByKey[key]; ok {
			desiredHook.ID = existingHook.ID
			desiredHook.CreatedAt = existingHook.CreatedAt
			if _, err := store.Update(ctx, desiredHook); err != nil {
				return fmt.Errorf("update plugin hook %q: %w", key, err)
			}
			continue
		}
		if _, err := store.Add(ctx, desiredHook); err != nil {
			return fmt.Errorf("add plugin hook %q: %w", key, err)
		}
	}

	return nil
}

// SyncModuleHooks loads hook declarations from module hook directory
// projections and reconciles them into the runtime hook store.
func SyncModuleHooks(ctx context.Context, store Store, projections []modules.DirectoryProjection) error {
	if store == nil {
		return nil
	}
	desired, err := CollectModuleHooks(projections)
	if err != nil {
		return err
	}

	existing, err := store.List(ctx)
	if err != nil {
		return err
	}
	existingByKey := make(map[string]*Hook)
	for _, item := range existing {
		if item == nil || item.Source != pluginHookSource {
			continue
		}
		existingByKey[pluginHookKey(item.SourceRef, item.Name)] = item
	}

	for key, item := range existingByKey {
		if _, ok := desired[key]; ok {
			continue
		}
		if err := store.Remove(ctx, item.ID); err != nil {
			return fmt.Errorf("remove stale plugin hook %q: %w", key, err)
		}
	}

	for key, desiredHook := range desired {
		if existingHook, ok := existingByKey[key]; ok {
			desiredHook.ID = existingHook.ID
			desiredHook.CreatedAt = existingHook.CreatedAt
			if _, err := store.Update(ctx, desiredHook); err != nil {
				return fmt.Errorf("update plugin hook %q: %w", key, err)
			}
			continue
		}
		if _, err := store.Add(ctx, desiredHook); err != nil {
			return fmt.Errorf("add plugin hook %q: %w", key, err)
		}
	}

	return nil
}

func loadPluginHooks(loaded plugin.LoadedPlugin) ([]Hook, error) {
	return loadHookDir(strings.TrimSpace(loaded.Manifest.Name), strings.TrimSpace(loaded.Manifest.Name), strings.TrimSpace(loaded.ResolvedHooksDir()))
}

func loadProjectedHooks(projection modules.DirectoryProjection) ([]Hook, error) {
	sourceRef := strings.TrimSpace(projection.ModuleName)
	if sourceRef == "" {
		sourceRef = strings.TrimSpace(strings.TrimPrefix(projection.ModuleID, "plugin:"))
	}
	label := sourceRef
	if label == "" {
		label = strings.TrimSpace(projection.ModuleID)
	}
	return loadHookDir(sourceRef, label, strings.TrimSpace(projection.Path))
}

func loadHookDir(sourceRef, label, dir string) ([]Hook, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}
	sourceRef = strings.TrimSpace(sourceRef)
	label = strings.TrimSpace(label)
	if label == "" {
		label = sourceRef
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read hooks dir for plugin %q: %w", label, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var out []Hook
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			continue
		}
		items, err := decodeHookFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("decode plugin hook file %q: %w", filepath.Join(dir, entry.Name()), err)
		}
		for _, item := range items {
			item.Name = strings.TrimSpace(item.Name)
			if item.Name == "" {
				return nil, fmt.Errorf("plugin hook file %q contains a hook without name", entry.Name())
			}
			item.Source = pluginHookSource
			item.SourceRef = sourceRef
			item.ID = ""
			if !item.CreatedAt.IsZero() {
				item.CreatedAt = item.CreatedAt.UTC()
			}
			if err := ValidateHookDefinition(item); err != nil {
				return nil, fmt.Errorf("%s/%s: %w", label, item.Name, err)
			}
			out = append(out, item)
		}
	}
	return out, nil
}

func decodeHookFile(path string) ([]Hook, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return decodeHookFileJSON(body)
	default:
		return decodeHookFileYAML(body)
	}
}

func decodeHookFileJSON(body []byte) ([]Hook, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var bundle hookBundle
	if err := decodeStrictJSON(trimmed, &bundle); err == nil {
		if len(bundle.Hooks) > 0 {
			return bundle.Hooks, nil
		}
		if len(bundle.Items) > 0 {
			return bundle.Items, nil
		}
	}
	var items []Hook
	if err := decodeStrictJSON(trimmed, &items); err == nil {
		return items, nil
	}
	var single Hook
	if err := decodeStrictJSON(trimmed, &single); err != nil {
		return nil, err
	}
	return []Hook{single}, nil
}

func decodeHookFileYAML(body []byte) ([]Hook, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var bundle hookBundle
	if err := decodeStrictYAML(trimmed, &bundle); err == nil {
		if len(bundle.Hooks) > 0 {
			return bundle.Hooks, nil
		}
		if len(bundle.Items) > 0 {
			return bundle.Items, nil
		}
	}
	var items []Hook
	if err := decodeStrictYAML(trimmed, &items); err == nil {
		return items, nil
	}
	var single Hook
	if err := decodeStrictYAML(trimmed, &single); err != nil {
		return nil, err
	}
	return []Hook{single}, nil
}

func decodeStrictYAML(body []byte, dst any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	decoder.KnownFields(true)
	return decoder.Decode(dst)
}

func decodeStrictJSON(body []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func pluginHookKey(sourceRef, name string) string {
	return strings.TrimSpace(sourceRef) + "::" + strings.TrimSpace(name)
}

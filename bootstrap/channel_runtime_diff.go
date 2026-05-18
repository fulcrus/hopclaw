package bootstrap

import (
	"reflect"
	"sort"
	"strings"

	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/config"
)

type channelRuntimeDiff struct {
	Added   []string
	Updated []string
	Removed []string
}

func (d channelRuntimeDiff) HasChanges() bool {
	return len(d.Added) > 0 || len(d.Updated) > 0 || len(d.Removed) > 0
}

func (d channelRuntimeDiff) ChangedNames() []string {
	names := make([]string, 0, len(d.Added)+len(d.Updated)+len(d.Removed))
	names = append(names, d.Removed...)
	names = append(names, d.Updated...)
	names = append(names, d.Added...)
	return names
}

func (d channelRuntimeDiff) Contains(name string) bool {
	for _, item := range d.ChangedNames() {
		if item == name {
			return true
		}
	}
	return false
}

func diffBuiltinRuntimeChannels(oldCfg, newCfg config.Config) channelRuntimeDiff {
	return diffRuntimeChannelConfigs(
		channelregistry.BuiltinRuntimeChannelConfigs(oldCfg),
		channelregistry.BuiltinRuntimeChannelConfigs(newCfg),
	)
}

func diffRuntimeChannelConfigs(oldRuntime, newRuntime map[string]any) channelRuntimeDiff {
	diff := channelRuntimeDiff{
		Added:   make([]string, 0),
		Updated: make([]string, 0),
		Removed: make([]string, 0),
	}

	for name, oldValue := range oldRuntime {
		newValue, ok := newRuntime[name]
		if !ok {
			diff.Removed = append(diff.Removed, name)
			continue
		}
		if !reflect.DeepEqual(oldValue, newValue) {
			diff.Updated = append(diff.Updated, name)
		}
	}
	for name := range newRuntime {
		if _, ok := oldRuntime[name]; !ok {
			diff.Added = append(diff.Added, name)
		}
	}

	sort.Strings(diff.Added)
	sort.Strings(diff.Updated)
	sort.Strings(diff.Removed)
	return diff
}

func webhookKeyFromChannelName(name string) (string, bool) {
	if !strings.HasPrefix(name, "webhook:") {
		return "", false
	}
	key := strings.TrimSpace(strings.TrimPrefix(name, "webhook:"))
	if key == "" || strings.Contains(key, "/") {
		return "", false
	}
	return key, true
}

func pluginWebhookKeyFromChannelName(name string) (string, bool) {
	if !strings.HasPrefix(name, "webhook:") {
		return "", false
	}
	key := strings.TrimSpace(strings.TrimPrefix(name, "webhook:"))
	if !isPluginWebhookKey(key) {
		return "", false
	}
	return key, true
}

func bridgeByName(bridges []namedChannelBridge) map[string]namedChannelBridge {
	if len(bridges) == 0 {
		return nil
	}
	out := make(map[string]namedChannelBridge, len(bridges))
	for _, bridge := range bridges {
		out[bridge.name] = bridge
	}
	return out
}

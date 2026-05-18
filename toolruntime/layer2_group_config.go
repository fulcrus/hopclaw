package toolruntime

import (
	"reflect"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/config"
)

type layer2GroupToggleBinding struct {
	group  string
	toggle string
}

var (
	layer2GroupToggleMu       sync.RWMutex
	layer2GroupToggleBindings []layer2GroupToggleBinding
)

func RegisterLayer2GroupToggle(group, toggle string) {
	group = strings.TrimSpace(group)
	toggle = strings.TrimSpace(toggle)
	if group == "" || toggle == "" {
		return
	}
	layer2GroupToggleMu.Lock()
	layer2GroupToggleBindings = append(layer2GroupToggleBindings, layer2GroupToggleBinding{
		group:  group,
		toggle: toggle,
	})
	layer2GroupToggleMu.Unlock()
}

func DisabledGroupsFromConfig(cfg config.Layer2Config) map[string]bool {
	toggles := layer2ToggleValues(cfg)
	if len(toggles) == 0 {
		return nil
	}

	layer2GroupToggleMu.RLock()
	bindings := append([]layer2GroupToggleBinding(nil), layer2GroupToggleBindings...)
	layer2GroupToggleMu.RUnlock()

	disabled := make(map[string]bool)
	for _, binding := range bindings {
		enabled, ok := toggles[binding.toggle]
		if ok && enabled != nil && !*enabled {
			disabled[binding.group] = true
		}
	}
	if len(disabled) == 0 {
		return nil
	}
	return disabled
}

func layer2ToggleValues(cfg config.Layer2Config) map[string]*bool {
	value := reflect.ValueOf(cfg)
	typ := value.Type()
	out := make(map[string]*bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type != reflect.TypeOf((*bool)(nil)) {
			continue
		}
		name := strings.TrimSpace(field.Tag.Get("yaml"))
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		ptr, _ := value.Field(i).Interface().(*bool)
		out[name] = ptr
	}
	return out
}

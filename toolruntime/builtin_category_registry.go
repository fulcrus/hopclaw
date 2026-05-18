package toolruntime

import (
	"fmt"
	"slices"
	"strings"
	"sync"
)

const (
	builtinCategoryOrderCore = iota
	builtinCategoryOrderDefault
)

type builtinCategoryDescriptor struct {
	Name        string
	Description string
	Order       int
	Load        func(BuiltinsConfig) []builtinToolDef
}

var (
	builtinCategoryRegistryMu sync.RWMutex
	builtinCategoryRegistry   = make(map[string]builtinCategoryDescriptor)
	builtinCategoryInitOnce   sync.Once
	builtinCategoryInitErr    error
)

func registerBuiltinCategory(spec builtinCategoryDescriptor) error {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return fmt.Errorf("builtin category name is required")
	}
	if spec.Load == nil {
		return fmt.Errorf("builtin category %q loader is required", name)
	}
	spec.Name = name

	builtinCategoryRegistryMu.Lock()
	defer builtinCategoryRegistryMu.Unlock()
	if _, exists := builtinCategoryRegistry[name]; exists {
		return fmt.Errorf("builtin category %q already registered", name)
	}
	builtinCategoryRegistry[name] = spec
	return nil
}

func ensureBuiltinCategoriesRegistered() error {
	builtinCategoryInitOnce.Do(func() {
		builtinCategoryInitErr = registerBuiltinCategories()
	})
	return builtinCategoryInitErr
}

func builtinCategoryCatalog() []builtinCategoryDescriptor {
	if err := ensureBuiltinCategoriesRegistered(); err != nil {
		return nil
	}

	builtinCategoryRegistryMu.RLock()
	defer builtinCategoryRegistryMu.RUnlock()

	out := make([]builtinCategoryDescriptor, 0, len(builtinCategoryRegistry))
	for _, spec := range builtinCategoryRegistry {
		out = append(out, spec)
	}
	slices.SortFunc(out, func(a, b builtinCategoryDescriptor) int {
		if a.Order != b.Order {
			if a.Order < b.Order {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

package bootstrap

import (
	"sort"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/controlplane"
)

type startupWarningCollector struct {
	mu       sync.RWMutex
	warnings map[string]controlplane.OperationalWarning
}

func newStartupWarningCollector() *startupWarningCollector {
	return &startupWarningCollector{warnings: make(map[string]controlplane.OperationalWarning)}
}

func (c *startupWarningCollector) Add(component string, err error) {
	if c == nil || err == nil {
		return
	}
	c.AddDetailed(component, componentWarningSummary(component), err.Error(), "")
}

func (c *startupWarningCollector) AddDetailed(component, summary, detail, fix string) {
	if c == nil {
		return
	}
	component = strings.TrimSpace(component)
	if component == "" {
		return
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = componentWarningSummary(component)
	}
	c.mu.Lock()
	c.warnings[component] = controlplane.OperationalWarning{
		Component: component,
		Summary:   summary,
		Detail:    strings.TrimSpace(detail),
		Fix:       strings.TrimSpace(fix),
	}
	c.mu.Unlock()
}

func (c *startupWarningCollector) Clear(component string) {
	if c == nil {
		return
	}
	component = strings.TrimSpace(component)
	if component == "" {
		return
	}
	c.mu.Lock()
	delete(c.warnings, component)
	c.mu.Unlock()
}

func (c *startupWarningCollector) Components() []string {
	warnings := c.OperationalWarnings()
	if len(warnings) == 0 {
		return nil
	}
	out := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, warning.Component)
	}
	return out
}

func (c *startupWarningCollector) OperationalWarnings() []controlplane.OperationalWarning {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.warnings) == 0 {
		return nil
	}
	out := make([]controlplane.OperationalWarning, 0, len(c.warnings))
	for _, warning := range c.warnings {
		out = append(out, warning)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Component < out[j].Component
	})
	return out
}

func (c *startupWarningCollector) LogSummary() {
	components := c.Components()
	if len(components) == 0 {
		return
	}
	log.Warn("bootstrap started with degraded optional components", "count", len(components), "components", components)
}

func componentWarningSummary(component string) string {
	component = strings.TrimSpace(component)
	if component == "" {
		return "runtime degraded"
	}
	component = strings.ReplaceAll(component, "_", " ")
	component = strings.ReplaceAll(component, "/", " ")
	component = strings.ReplaceAll(component, ".", " ")
	return component + " degraded"
}

package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"

	"gopkg.in/yaml.v3"
)

func (g *Gateway) currentOperatorConfig() (config.Config, bool) {
	if g == nil || g.effectiveCfg == nil {
		return config.Config{}, false
	}
	return g.effectiveCfg.Current(), true
}

func (g *Gateway) putConfigSection(ctx context.Context, section string, sectionValue any) (configUpdateResponse, int, error) {
	currentCfg, ok := g.currentOperatorConfig()
	if !ok {
		return configUpdateResponse{}, http.StatusServiceUnavailable, fmt.Errorf("effective config not available")
	}
	if _, err := extractConfigSection(currentCfg, section); err != nil {
		return configUpdateResponse{}, http.StatusNotFound, err
	}
	nextCfg, err := applyConfigSection(currentCfg, section, sectionValue)
	if err != nil {
		return configUpdateResponse{}, http.StatusBadRequest, err
	}
	normalizedSection, err := extractConfigSection(nextCfg, section)
	if err != nil {
		return configUpdateResponse{}, http.StatusBadRequest, err
	}

	if g.fileBackedConfig() {
		if err := g.writeConfigSection(section, normalizedSection); err != nil {
			return configUpdateResponse{}, http.StatusInternalServerError, err
		}
		if err := g.triggerConfigReload(); err != nil {
			return configUpdateResponse{}, http.StatusInternalServerError, err
		}
	} else {
		if g.configMutator == nil {
			return configUpdateResponse{}, http.StatusServiceUnavailable, controlplane.ErrMutationUnavailable
		}
		if err := g.configMutator.PutSection(ctx, section, normalizedSection); err != nil {
			return configUpdateResponse{}, httpStatusForConfigMutation(err), err
		}
	}

	return configUpdateResponse{
		OK:         true,
		ReloadPlan: config.AnalyzeReloadPlan([]string{section + "."}),
	}, 0, nil
}

func validateConfigPayload(raw json.RawMessage, currentCfg config.Config, hasCurrent bool) []string {
	var intermediate map[string]any
	if err := json.Unmarshal(raw, &intermediate); err != nil {
		return []string{fmt.Errorf("failed to parse json: %w", err).Error()}
	}

	candidate := intermediate
	if hasCurrent {
		rootBytes, err := yaml.Marshal(currentCfg)
		if err != nil {
			return []string{fmt.Errorf("failed to encode current config: %w", err).Error()}
		}
		root := make(map[string]any)
		if err := yaml.Unmarshal(rootBytes, &root); err != nil {
			return []string{fmt.Errorf("failed to decode current config: %w", err).Error()}
		}
		candidate = mergeConfigOverlay(root, intermediate)
	}

	yamlBytes, err := yaml.Marshal(candidate)
	if err != nil {
		return []string{fmt.Errorf("failed to convert to yaml: %w", err).Error()}
	}
	if _, err := config.Parse(yamlBytes); err != nil {
		return []string{err.Error()}
	}
	return make([]string, 0)
}

func mergeConfigOverlay(base, overlay map[string]any) map[string]any {
	if base == nil {
		base = make(map[string]any)
	}
	for key, value := range overlay {
		existing, exists := base[key]
		valueMap, valueIsMap := value.(map[string]any)
		existingMap, existingIsMap := existing.(map[string]any)
		if exists && valueIsMap && existingIsMap {
			base[key] = mergeConfigOverlay(existingMap, valueMap)
			continue
		}
		base[key] = value
	}
	return base
}

// extractConfigSection returns a specific section of the config by name.
func extractConfigSection(cfg config.Config, section string) (any, error) {
	return config.ExtractSection(cfg, section)
}

func applyConfigSection(cfg config.Config, section string, sectionValue any) (config.Config, error) {
	return config.ApplySection(cfg, section, sectionValue)
}

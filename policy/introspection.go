package policy

import (
	"reflect"
	"strings"
)

type ApprovalDefaultsSummary struct {
	DefaultScope string `json:"default_scope,omitempty"`
	MaxScope     string `json:"max_scope,omitempty"`
}

type EngineSummary struct {
	Name                           string                  `json:"name,omitempty"`
	Type                           string                  `json:"type,omitempty"`
	LayerOrder                     int                     `json:"layer_order,omitempty"`
	GrantStoreWired                bool                    `json:"grant_store_wired"`
	SecurityAuditWired             bool                    `json:"security_audit_wired"`
	AllowUnknownTools              bool                    `json:"allow_unknown_tools"`
	RequireApprovalForWrite        bool                    `json:"require_approval_for_write"`
	AllowLocalWriteWithoutApproval bool                    `json:"allow_local_write_without_approval"`
	RequireApprovalCommunity       bool                    `json:"require_approval_community"`
	SkipManifestApproval           bool                    `json:"skip_manifest_approval"`
	DenyDestructive                bool                    `json:"deny_destructive"`
	SkillInstallPolicy             string                  `json:"skill_install_policy,omitempty"`
	SafePatternCount               int                     `json:"safe_pattern_count,omitempty"`
	BlockedCommandCount            int                     `json:"blocked_command_count,omitempty"`
	DangerousToolCount             int                     `json:"dangerous_tool_count,omitempty"`
	ApprovalDefaults               ApprovalDefaultsSummary `json:"approval_defaults,omitempty"`
	SkillInstallApprovalDefault    ApprovalDefaultsSummary `json:"skill_install_approval_defaults,omitempty"`
}

type RuntimeSummary struct {
	Kind       string          `json:"kind"`
	Layered    bool            `json:"layered"`
	LayerCount int             `json:"layer_count"`
	Layers     []EngineSummary `json:"layers,omitempty"`
}

func DescribeEngine(engine Engine) RuntimeSummary {
	if engine == nil {
		return RuntimeSummary{Kind: "none"}
	}
	if chain, ok := engine.(*ChainEngine); ok {
		layers := make([]EngineSummary, 0, len(chain.layers))
		for i, layer := range chain.layers {
			layers = append(layers, describeLayer(layer.Name, layer.Engine, i+1))
		}
		return RuntimeSummary{
			Kind:       "chain",
			Layered:    true,
			LayerCount: len(layers),
			Layers:     emptyIfZeroEngineSummaries(layers),
		}
	}
	layer := describeLayer("", engine, 1)
	return RuntimeSummary{
		Kind:       "single",
		LayerCount: 1,
		Layers:     []EngineSummary{layer},
	}
}

func describeLayer(name string, engine Engine, order int) EngineSummary {
	switch typed := engine.(type) {
	case *DefaultEngine:
		cfg := typed.config.Normalized()
		return EngineSummary{
			Name:                           strings.TrimSpace(name),
			Type:                           "default",
			LayerOrder:                     order,
			GrantStoreWired:                typed.grants != nil,
			SecurityAuditWired:             typed.auditor != nil,
			AllowUnknownTools:              cfg.AllowUnknownTools,
			RequireApprovalForWrite:        cfg.RequireApprovalForWrite,
			AllowLocalWriteWithoutApproval: cfg.AllowLocalWriteWithoutApproval,
			RequireApprovalCommunity:       cfg.RequireApprovalCommunity,
			SkipManifestApproval:           cfg.SkipManifestApproval,
			DenyDestructive:                cfg.DenyDestructive,
			SkillInstallPolicy:             strings.TrimSpace(cfg.SkillInstallPolicy),
			SafePatternCount:               len(cfg.SafePatterns),
			BlockedCommandCount:            len(cfg.BlockedCommands),
			DangerousToolCount:             len(cfg.DangerousTools),
			ApprovalDefaults: ApprovalDefaultsSummary{
				DefaultScope: strings.TrimSpace(string(cfg.ApprovalDefaults().DefaultScope)),
				MaxScope:     strings.TrimSpace(string(cfg.ApprovalDefaults().MaxScope)),
			},
			SkillInstallApprovalDefault: ApprovalDefaultsSummary{
				DefaultScope: strings.TrimSpace(string(cfg.SkillInstallApprovalDefaults().DefaultScope)),
				MaxScope:     strings.TrimSpace(string(cfg.SkillInstallApprovalDefaults().MaxScope)),
			},
		}
	default:
		return EngineSummary{
			Name:       strings.TrimSpace(name),
			Type:       engineTypeName(typed),
			LayerOrder: order,
		}
	}
}

func engineTypeName(engine any) string {
	if engine == nil {
		return ""
	}
	typ := reflect.TypeOf(engine)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.PkgPath() == "" {
		return typ.Name()
	}
	if typ.Name() == "" {
		return typ.String()
	}
	return typ.PkgPath() + "." + typ.Name()
}

func emptyIfZeroEngineSummaries(items []EngineSummary) []EngineSummary {
	if len(items) == 0 {
		return nil
	}
	return items
}

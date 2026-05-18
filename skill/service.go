package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ServiceConfig struct {
	Roots         []DiscoveryRoot
	Loader        Loader
	Compiler      Compiler
	Evaluator     Evaluator
	Limits        Limits
	WatchInterval time.Duration
	OnRefresh     func(RegistrySnapshot)
}

type Service struct {
	registry      *Registry
	roots         []DiscoveryRoot
	evaluator     Evaluator
	limits        Limits
	watchInterval time.Duration
	onRefresh     func(RegistrySnapshot)
}

const promptCatalogTrimNotice = "Additional eligible skill prompts omitted due to size."

func NewService(cfg ServiceConfig) *Service {
	limits := cfg.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	// If no custom loader provided, create one with the configured limits.
	loader := cfg.Loader
	if loader == nil {
		loader = FilesystemLoader{Limits: limits}
	}
	return &Service{
		registry:      NewRegistryWithLimits(loader, cfg.Compiler, limits),
		roots:         append([]DiscoveryRoot(nil), cfg.Roots...),
		evaluator:     cfg.Evaluator,
		limits:        limits,
		watchInterval: cfg.WatchInterval,
		onRefresh:     cfg.OnRefresh,
	}
}

func (s *Service) Refresh(ctx context.Context) (*RegistrySnapshot, error) {
	snapshot, err := s.registry.Refresh(ctx, s.roots)
	if err != nil {
		return nil, err
	}
	if s.onRefresh != nil && snapshot != nil {
		s.onRefresh(snapshot.clone())
	}
	return snapshot, nil
}

func (s *Service) Snapshot() RegistrySnapshot {
	return s.registry.Snapshot()
}

func (s *Service) RefreshAndBind(ctx context.Context, runtimeCtx RuntimeContext) (SessionSkillSnapshot, error) {
	snapshot, err := s.Refresh(ctx)
	if err != nil {
		return SessionSkillSnapshot{}, err
	}
	runtimeCtx = s.evaluator.EnrichRuntimeContext(runtimeCtx, snapshot.Ordered)
	out := snapshot.Bind(runtimeCtx, s.evaluator)
	trimPromptCatalog(&out, s.limits.MaxPromptChars)
	return out, nil
}

func (s *Service) BindSession(runtimeCtx RuntimeContext) SessionSkillSnapshot {
	snapshot := s.registry.Snapshot()
	runtimeCtx = s.evaluator.EnrichRuntimeContext(runtimeCtx, snapshot.Ordered)
	out := snapshot.Bind(runtimeCtx, s.evaluator)
	trimPromptCatalog(&out, s.limits.MaxPromptChars)
	return out
}

func (s *Service) Inspect(ref string, runtimeCtx RuntimeContext) (SkillRuntimeReport, bool) {
	snapshot := s.registry.Snapshot()
	if pkg, ok := FindPackage(snapshot, ref); ok {
		runtimeCtx = s.evaluator.EnrichRuntimeContext(runtimeCtx, []*SkillPackage{pkg})
		return BuildRuntimeReport(pkg, runtimeCtx, s.evaluator), true
	}
	if blocked, ok := FindBlockedSkill(snapshot, ref); ok {
		return BuildBlockedRuntimeReport(blocked), true
	}
	return SkillRuntimeReport{}, false
}

func (s *Service) InspectSource(ctx context.Context, dir string, sourceKind SourceKind, runtimeCtx RuntimeContext) (SkillRuntimeReport, error) {
	root := filepath.Dir(dir)
	src := SkillSource{
		Kind:     sourceKind,
		Root:     root,
		Dir:      dir,
		NameHint: filepath.Base(dir),
		Priority: DiscoveryRoot{Kind: sourceKind}.effectivePriority(),
	}
	spec, err := ParseDir(src.Dir)
	if err != nil {
		return SkillRuntimeReport{}, err
	}
	compiler := s.registry.compiler
	if compiler == nil {
		compiler = DefaultCompiler{}
	}
	pkg, err := compiler.Compile(ctx, src, spec)
	if err != nil {
		return SkillRuntimeReport{}, err
	}
	runtimeCtx = s.evaluator.EnrichRuntimeContext(runtimeCtx, []*SkillPackage{pkg})
	report := BuildRuntimeReport(pkg, runtimeCtx, s.evaluator)
	report.Loaded = false
	report.SourceKind = src.Kind
	report.SourceRoot = src.Root
	report.SourceDir = src.Dir
	report.SourceNameHint = src.NameHint
	report.SourcePriority = src.Priority
	return report, nil
}

func (s *Service) Watcher(onRefresh func(RegistrySnapshot)) Watcher {
	combined := onRefresh
	if s.onRefresh != nil {
		combined = func(snapshot RegistrySnapshot) {
			s.onRefresh(snapshot.clone())
			if onRefresh != nil {
				onRefresh(snapshot)
			}
		}
	}
	return Watcher{
		Registry:  s.registry,
		Roots:     append([]DiscoveryRoot(nil), s.roots...),
		Interval:  s.watchInterval,
		OnRefresh: combined,
	}
}

func (s RegistrySnapshot) Bind(runtimeCtx RuntimeContext, evaluator Evaluator) SessionSkillSnapshot {
	out := SessionSkillSnapshot{
		GeneratedAt:        s.GeneratedAt,
		Fingerprint:        s.Fingerprint,
		ContextFingerprint: FingerprintRuntimeContext(runtimeCtx),
		Skills:             make(map[string]BoundSkill, len(s.Ordered)),
		Ordered:            make([]BoundSkill, 0, len(s.Ordered)),
		Blocked:            append([]BlockedSkill(nil), s.Blocked...),
	}
	for _, pkg := range s.Ordered {
		bound := BoundSkill{
			Package:     pkg,
			Eligibility: evaluator.Evaluate(pkg, runtimeCtx),
		}
		out.Skills[pkg.Name()] = bound
		out.Ordered = append(out.Ordered, bound)
		if bound.Eligibility.Eligible {
			out.PromptCatalog = append(out.PromptCatalog, pkg.PromptEntry())
		}
	}
	out.PromptBlock = FormatPromptCatalog(out.PromptCatalog)
	return out
}

func (s SessionSkillSnapshot) Resolve(name string) (BoundSkill, bool) {
	bound, ok := s.Skills[name]
	return bound, ok
}

func (s SessionSkillSnapshot) ResolveTool(name string) (BoundTool, bool) {
	for _, bound := range s.Ordered {
		for _, manifest := range bound.Package.ToolManifests {
			if toolNameMatches(manifest, name) {
				return BoundTool{
					Package:     bound.Package,
					Manifest:    manifest,
					Eligibility: bound.Eligibility,
				}, true
			}
		}
	}
	return BoundTool{}, false
}

func toolNameMatches(manifest ToolManifest, name string) bool {
	if strings.EqualFold(strings.TrimSpace(manifest.Name), strings.TrimSpace(name)) {
		return true
	}
	for _, alias := range manifest.Aliases {
		if strings.EqualFold(strings.TrimSpace(alias), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// trimPromptCatalog enforces the MaxPromptChars limit on PromptBlock while
// preserving the full PromptCatalog for later relevance-based ordering.
func trimPromptCatalog(snap *SessionSkillSnapshot, maxChars int) {
	if snap == nil {
		return
	}
	if maxChars <= 0 || len(snap.PromptBlock) <= maxChars {
		return
	}
	entries := append([]PromptCatalogEntry(nil), snap.PromptCatalog...)
	for len(entries) > 0 {
		omitted := len(snap.PromptCatalog) - len(entries)
		block := FormatPromptCatalogWithNotice(entries, omitted, promptCatalogTrimNotice)
		if len(block) <= maxChars {
			snap.PromptBlock = block
			return
		}
		entries = entries[:len(entries)-1]
	}
	snap.PromptBlock = trimPromptBlockToChars(snap.PromptBlock, maxChars)
}

func trimPromptBlockToChars(block string, maxChars int) string {
	if maxChars <= 0 || len(block) <= maxChars {
		return block
	}
	runes := []rune(block)
	if len(runes) <= maxChars {
		return block
	}
	if maxChars <= 1 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-1]) + "…"
}

func FingerprintRuntimeContext(runtimeCtx RuntimeContext) string {
	payload, err := json.Marshal(canonicalRuntimeContext(runtimeCtx))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:8])
}

func canonicalRuntimeContext(runtimeCtx RuntimeContext) RuntimeContext {
	out := runtimeCtx
	out.Git.Remotes = canonicalStringSlice(runtimeCtx.Git.Remotes)
	out.Workspace.Markers = canonicalStringSlice(runtimeCtx.Workspace.Markers)
	out.ModuleCapabilities = canonicalStringSlice(runtimeCtx.ModuleCapabilities)
	return out
}

func canonicalStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	if len(cleaned) == 0 {
		return nil
	}
	sort.Strings(cleaned)
	out := cleaned[:0]
	for _, value := range cleaned {
		if len(out) > 0 && out[len(out)-1] == value {
			continue
		}
		out = append(out, value)
	}
	return append([]string(nil), out...)
}

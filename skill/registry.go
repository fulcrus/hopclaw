package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Registry struct {
	loader   Loader
	compiler Compiler
	limits   Limits

	mu       sync.RWMutex
	snapshot RegistrySnapshot
}

func NewRegistry(loader Loader, compiler Compiler) *Registry {
	return NewRegistryWithLimits(loader, compiler, DefaultLimits())
}

func NewRegistryWithLimits(loader Loader, compiler Compiler, limits Limits) *Registry {
	if loader == nil {
		loader = FilesystemLoader{Limits: limits}
	}
	if compiler == nil {
		compiler = DefaultCompiler{}
	}
	return &Registry{
		loader:   loader,
		compiler: compiler,
		limits:   limits,
		snapshot: RegistrySnapshot{Skills: map[string]*SkillPackage{}},
	}
}

func (r *Registry) Refresh(ctx context.Context, roots []DiscoveryRoot) (*RegistrySnapshot, error) {
	sources, err := r.loader.Discover(ctx, roots)
	if err != nil {
		return nil, err
	}

	next := RegistrySnapshot{
		GeneratedAt: time.Now().UTC(),
		Skills:      make(map[string]*SkillPackage),
	}
	for _, src := range sources {
		spec, err := r.loader.Load(ctx, src)
		if err != nil {
			next.Blocked = append(next.Blocked, BlockedSkill{
				Source:   src,
				NameHint: src.NameHint,
				Issues: []SkillIssue{{
					Severity: SeverityError,
					Code:     "load_failed",
					Message:  fmt.Sprintf("load %s: %v", src.Dir, err),
				}},
			})
			continue
		}
		pkg, err := r.compiler.Compile(ctx, src, spec)
		if err != nil {
			next.Blocked = append(next.Blocked, BlockedSkill{
				Source:   src,
				NameHint: src.NameHint,
				Issues: []SkillIssue{{
					Severity: SeverityError,
					Code:     "compile_failed",
					Message:  fmt.Sprintf("compile %s: %v", src.Dir, err),
				}},
			})
			continue
		}
		if pkg.Status == StatusBlocked {
			next.Blocked = append(next.Blocked, BlockedSkill{
				Source:   src,
				NameHint: pkg.Name(),
				Issues:   append([]SkillIssue(nil), pkg.Issues...),
			})
			continue
		}

		existing, ok := next.Skills[pkg.Name()]
		if ok && existing.Source.Priority >= pkg.Source.Priority {
			continue
		}
		next.Skills[pkg.Name()] = pkg
	}

	next.Ordered = orderedSkills(next.Skills)

	// Enforce MaxTotalSkills: keep highest priority skills.
	if r.limits.MaxTotalSkills > 0 && len(next.Ordered) > r.limits.MaxTotalSkills {
		// Sort by priority descending so truncation keeps the most important skills.
		sort.Slice(next.Ordered, func(i, j int) bool {
			return next.Ordered[i].Source.Priority > next.Ordered[j].Source.Priority
		})
		next.Ordered = next.Ordered[:r.limits.MaxTotalSkills]
		pruned := make(map[string]*SkillPackage, len(next.Ordered))
		for _, pkg := range next.Ordered {
			pruned[pkg.Name()] = pkg
		}
		next.Skills = pruned
		// Re-sort into canonical order for deterministic output.
		next.Ordered = orderedSkills(pruned)
	}

	next.Fingerprint = fingerprintSnapshot(next.Ordered, next.Blocked)

	r.mu.Lock()
	r.snapshot = next
	r.mu.Unlock()

	snap := next.clone()
	return &snap, nil
}

func (r *Registry) Snapshot() RegistrySnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot.clone()
}

func (r *Registry) Resolve(name string) (*SkillPackage, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pkg, ok := r.snapshot.Skills[name]
	return pkg, ok
}

func (r *Registry) PromptCatalog(ctx RuntimeContext, evaluator Evaluator) []PromptCatalogEntry {
	snapshot := r.Snapshot()
	entries := make([]PromptCatalogEntry, 0, len(snapshot.Ordered))
	for _, pkg := range snapshot.Ordered {
		if !evaluator.Evaluate(pkg, ctx).Eligible {
			continue
		}
		entries = append(entries, pkg.PromptEntry())
	}
	return entries
}

func (p *SkillPackage) PromptEntry() PromptCatalogEntry {
	return PromptCatalogEntry{
		Name:        p.Prompt.Name,
		Description: p.Prompt.Description,
		Location:    p.Prompt.Location,
		ToolDomains: promptEntryToolDomains(p.ToolManifests),
	}
}

func promptEntryToolDomains(manifests []ToolManifest) []string {
	if len(manifests) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(manifests))
	out := make([]string, 0, len(manifests))
	for _, manifest := range manifests {
		name := strings.TrimSpace(manifest.Name)
		if name == "" {
			continue
		}
		domain := name
		if idx := strings.Index(domain, "."); idx > 0 {
			domain = domain[:idx]
		}
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func orderedSkills(skills map[string]*SkillPackage) []*SkillPackage {
	out := make([]*SkillPackage, 0, len(skills))
	for _, pkg := range skills {
		out = append(out, pkg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Prompt.Name != out[j].Prompt.Name {
			return out[i].Prompt.Name < out[j].Prompt.Name
		}
		return out[i].Source.Priority > out[j].Source.Priority
	})
	return out
}

func (s RegistrySnapshot) clone() RegistrySnapshot {
	out := RegistrySnapshot{
		GeneratedAt: s.GeneratedAt,
		Fingerprint: s.Fingerprint,
		Skills:      make(map[string]*SkillPackage, len(s.Skills)),
		Ordered:     make([]*SkillPackage, len(s.Ordered)),
		Blocked:     make([]BlockedSkill, len(s.Blocked)),
	}
	for k, v := range s.Skills {
		out.Skills[k] = v
	}
	copy(out.Ordered, s.Ordered)
	copy(out.Blocked, s.Blocked)
	return out
}

func fingerprintSnapshot(pkgs []*SkillPackage, blocked []BlockedSkill) string {
	payload := struct {
		Skills  []map[string]any `json:"skills"`
		Blocked []BlockedSkill   `json:"blocked"`
	}{
		Skills:  make([]map[string]any, 0, len(pkgs)),
		Blocked: blocked,
	}
	for _, pkg := range pkgs {
		payload.Skills = append(payload.Skills, map[string]any{
			"id":             pkg.ID,
			"name":           pkg.Name(),
			"source_kind":    pkg.Source.Kind,
			"source_dir":     pkg.Source.Dir,
			"source_root":    pkg.Source.Root,
			"priority":       pkg.Source.Priority,
			"kind":           pkg.Kind,
			"status":         pkg.Status,
			"trust":          pkg.Trust,
			"prompt":         pkg.Prompt,
			"tool_manifests": pkg.ToolManifests,
			"openclaw":       pkg.OpenClaw,
			"issues":         pkg.Issues,
		})
	}
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(fmt.Sprintf("%v", payload))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// stubLoader returns preconfigured sources and specs for testing.
type stubLoader struct {
	mu      sync.Mutex
	sources []SkillSource
	specs   map[string]*ExternalSkillSpec
	loadErr map[string]error
}

func (s *stubLoader) Discover(_ context.Context, _ []DiscoveryRoot) ([]SkillSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SkillSource(nil), s.sources...), nil
}

func (s *stubLoader) Load(_ context.Context, src SkillSource) (*ExternalSkillSpec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := s.loadErr[src.Dir]; ok {
		return nil, err
	}
	if spec, ok := s.specs[src.Dir]; ok {
		return spec, nil
	}
	return nil, fmt.Errorf("no spec for %s", src.Dir)
}

func TestRegistryNewDefaults(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(nil, nil)
	snap := reg.Snapshot()
	if snap.Skills == nil {
		t.Fatal("Skills map should not be nil")
	}
	if len(snap.Skills) != 0 {
		t.Fatalf("expected empty skills, got %d", len(snap.Skills))
	}
}

func TestRegistryRefreshAddsSkills(t *testing.T) {
	t.Parallel()

	loader := &stubLoader{
		sources: []SkillSource{
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/alpha", NameHint: "alpha", Priority: 500},
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/beta", NameHint: "beta", Priority: 500},
		},
		specs: map[string]*ExternalSkillSpec{
			"/ws/alpha": {Name: "alpha", Description: "Alpha skill", Body: "Use alpha."},
			"/ws/beta":  {Name: "beta", Description: "Beta skill", Body: "Use beta."},
		},
	}

	reg := NewRegistry(loader, DefaultCompiler{})
	snap, err := reg.Refresh(context.Background(), []DiscoveryRoot{{Kind: SourceWorkspace, Path: "/ws"}})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(snap.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(snap.Skills))
	}
	if _, ok := snap.Skills["alpha"]; !ok {
		t.Fatal("alpha skill not found")
	}
	if _, ok := snap.Skills["beta"]; !ok {
		t.Fatal("beta skill not found")
	}
	if len(snap.Ordered) != 2 {
		t.Fatalf("expected 2 ordered skills, got %d", len(snap.Ordered))
	}
	if snap.Fingerprint == "" {
		t.Fatal("fingerprint should not be empty")
	}
}

func TestRegistryRefreshHigherPriorityWins(t *testing.T) {
	t.Parallel()

	loader := &stubLoader{
		sources: []SkillSource{
			{Kind: SourceBundled, Root: "/bundled", Dir: "/bundled/tool", NameHint: "tool", Priority: 200},
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/tool", NameHint: "tool", Priority: 500},
		},
		specs: map[string]*ExternalSkillSpec{
			"/bundled/tool": {Name: "tool", Description: "Bundled tool", Body: "Bundled."},
			"/ws/tool":      {Name: "tool", Description: "Workspace tool", Body: "Workspace."},
		},
	}

	reg := NewRegistry(loader, DefaultCompiler{})
	snap, err := reg.Refresh(context.Background(), nil)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(snap.Skills) != 1 {
		t.Fatalf("expected 1 skill (deduped by name), got %d", len(snap.Skills))
	}
	pkg := snap.Skills["tool"]
	if pkg.Prompt.Description != "Workspace tool" {
		t.Fatalf("expected workspace priority to win, got %q", pkg.Prompt.Description)
	}
}

func TestRegistryRefreshSamePriorityFirstWins(t *testing.T) {
	t.Parallel()

	loader := &stubLoader{
		sources: []SkillSource{
			{Kind: SourceWorkspace, Root: "/ws1", Dir: "/ws1/tool", NameHint: "tool", Priority: 500},
			{Kind: SourceWorkspace, Root: "/ws2", Dir: "/ws2/tool", NameHint: "tool", Priority: 500},
		},
		specs: map[string]*ExternalSkillSpec{
			"/ws1/tool": {Name: "tool", Description: "First tool", Body: "First."},
			"/ws2/tool": {Name: "tool", Description: "Second tool", Body: "Second."},
		},
	}

	reg := NewRegistry(loader, DefaultCompiler{})
	snap, err := reg.Refresh(context.Background(), nil)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	pkg := snap.Skills["tool"]
	if pkg.Prompt.Description != "First tool" {
		t.Fatalf("expected first-seen to win at same priority, got %q", pkg.Prompt.Description)
	}
}

func TestRegistryRefreshBlockedSkillsTracked(t *testing.T) {
	t.Parallel()

	loader := &stubLoader{
		sources: []SkillSource{
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/good", NameHint: "good", Priority: 500},
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/bad", NameHint: "bad", Priority: 500},
		},
		specs: map[string]*ExternalSkillSpec{
			"/ws/good": {Name: "good", Description: "Good skill", Body: "Good."},
		},
		loadErr: map[string]error{
			"/ws/bad": fmt.Errorf("parse error"),
		},
	}

	reg := NewRegistry(loader, DefaultCompiler{})
	snap, err := reg.Refresh(context.Background(), nil)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(snap.Skills) != 1 {
		t.Fatalf("expected 1 good skill, got %d", len(snap.Skills))
	}
	if len(snap.Blocked) != 1 {
		t.Fatalf("expected 1 blocked skill, got %d", len(snap.Blocked))
	}
	if snap.Blocked[0].NameHint != "bad" {
		t.Fatalf("blocked name = %q", snap.Blocked[0].NameHint)
	}
}

func TestRegistryResolve(t *testing.T) {
	t.Parallel()

	loader := &stubLoader{
		sources: []SkillSource{
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/alpha", Priority: 500},
		},
		specs: map[string]*ExternalSkillSpec{
			"/ws/alpha": {Name: "alpha", Description: "Alpha", Body: "A."},
		},
	}

	reg := NewRegistry(loader, DefaultCompiler{})
	if _, err := reg.Refresh(context.Background(), nil); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	pkg, ok := reg.Resolve("alpha")
	if !ok {
		t.Fatal("Resolve(alpha) not found")
	}
	if pkg.Name() != "alpha" {
		t.Fatalf("Name() = %q", pkg.Name())
	}

	_, ok = reg.Resolve("nonexistent")
	if ok {
		t.Fatal("Resolve(nonexistent) should return false")
	}
}

func TestRegistrySnapshotIsClone(t *testing.T) {
	t.Parallel()

	loader := &stubLoader{
		sources: []SkillSource{
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/s", Priority: 500},
		},
		specs: map[string]*ExternalSkillSpec{
			"/ws/s": {Name: "s", Description: "d", Body: "b"},
		},
	}

	reg := NewRegistry(loader, DefaultCompiler{})
	if _, err := reg.Refresh(context.Background(), nil); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	snap1 := reg.Snapshot()
	snap2 := reg.Snapshot()

	// Modifying snap1 should not affect snap2.
	delete(snap1.Skills, "s")
	if _, ok := snap2.Skills["s"]; !ok {
		t.Fatal("modifying snapshot clone should not affect other clones")
	}
}

func TestRegistryMaxTotalSkillsEnforced(t *testing.T) {
	t.Parallel()

	sources := make([]SkillSource, 5)
	specs := make(map[string]*ExternalSkillSpec, 5)
	for i := 0; i < 5; i++ {
		dir := fmt.Sprintf("/ws/skill%d", i)
		name := fmt.Sprintf("skill%d", i)
		sources[i] = SkillSource{Kind: SourceWorkspace, Root: "/ws", Dir: dir, Priority: 500 - i}
		specs[dir] = &ExternalSkillSpec{Name: name, Description: name, Body: name}
	}

	loader := &stubLoader{sources: sources, specs: specs}
	limits := DefaultLimits()
	limits.MaxTotalSkills = 3

	reg := NewRegistryWithLimits(loader, DefaultCompiler{}, limits)
	snap, err := reg.Refresh(context.Background(), nil)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(snap.Skills) != 3 {
		t.Fatalf("expected 3 skills (max enforced), got %d", len(snap.Skills))
	}
	if len(snap.Ordered) != 3 {
		t.Fatalf("expected 3 ordered skills, got %d", len(snap.Ordered))
	}
}

func TestRegistryOrderedIsSortedAlphabetically(t *testing.T) {
	t.Parallel()

	loader := &stubLoader{
		sources: []SkillSource{
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/charlie", Priority: 500},
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/alpha", Priority: 500},
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/bravo", Priority: 500},
		},
		specs: map[string]*ExternalSkillSpec{
			"/ws/charlie": {Name: "charlie", Description: "c", Body: "c"},
			"/ws/alpha":   {Name: "alpha", Description: "a", Body: "a"},
			"/ws/bravo":   {Name: "bravo", Description: "b", Body: "b"},
		},
	}

	reg := NewRegistry(loader, DefaultCompiler{})
	snap, err := reg.Refresh(context.Background(), nil)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	if len(snap.Ordered) != 3 {
		t.Fatalf("expected 3, got %d", len(snap.Ordered))
	}
	expected := []string{"alpha", "bravo", "charlie"}
	for i, name := range expected {
		if snap.Ordered[i].Name() != name {
			t.Fatalf("Ordered[%d] = %q, want %q", i, snap.Ordered[i].Name(), name)
		}
	}
}

func TestRegistryFingerprintChanges(t *testing.T) {
	t.Parallel()

	loader := &stubLoader{
		sources: []SkillSource{
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/s1", Priority: 500},
		},
		specs: map[string]*ExternalSkillSpec{
			"/ws/s1": {Name: "s1", Description: "original", Body: "b"},
		},
	}

	reg := NewRegistry(loader, DefaultCompiler{})
	snap1, err := reg.Refresh(context.Background(), nil)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	// Change the spec.
	loader.mu.Lock()
	loader.specs["/ws/s1"] = &ExternalSkillSpec{Name: "s1", Description: "updated", Body: "b"}
	loader.mu.Unlock()

	snap2, err := reg.Refresh(context.Background(), nil)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	if snap1.Fingerprint == snap2.Fingerprint {
		t.Fatalf("fingerprint should change when skill content changes: %q", snap1.Fingerprint)
	}
}

func TestRegistryPromptCatalogFiltersIneligible(t *testing.T) {
	t.Parallel()

	loader := &stubLoader{
		sources: []SkillSource{
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/eligible", Priority: 500},
			{Kind: SourceWorkspace, Root: "/ws", Dir: "/ws/restricted", Priority: 500},
		},
		specs: map[string]*ExternalSkillSpec{
			"/ws/eligible": {Name: "eligible", Description: "open skill", Body: "b"},
			"/ws/restricted": {
				Name:        "restricted",
				Description: "needs token",
				Body:        "b",
				OpenClaw:    OpenClawMetadata{Requires: RequiresSpec{Env: []string{"SECRET_TOKEN"}}},
			},
		},
	}

	reg := NewRegistry(loader, DefaultCompiler{})
	if _, err := reg.Refresh(context.Background(), nil); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	eval := Evaluator{}
	entries := reg.PromptCatalog(RuntimeContext{GOOS: "linux"}, eval)
	if len(entries) != 1 {
		t.Fatalf("expected 1 eligible entry, got %d", len(entries))
	}
	if entries[0].Name != "eligible" {
		t.Fatalf("entry name = %q", entries[0].Name)
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "skills")
	mustWriteSkill(t, filepath.Join(root, "concurrent"), "concurrent", "concurrent skill")

	reg := NewRegistry(FilesystemLoader{}, DefaultCompiler{})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = reg.Refresh(context.Background(), []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}})
		}()
		go func() {
			defer wg.Done()
			_ = reg.Snapshot()
			_, _ = reg.Resolve("concurrent")
		}()
	}
	wg.Wait()

	snap := reg.Snapshot()
	if _, ok := snap.Skills["concurrent"]; !ok {
		t.Fatal("concurrent skill should exist after concurrent access")
	}
}

func TestRegistryRefreshReplacesOldSnapshot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "skills")
	mustWriteSkill(t, filepath.Join(root, "first"), "first", "first skill")

	reg := NewRegistry(FilesystemLoader{}, DefaultCompiler{})
	_, err := reg.Refresh(context.Background(), []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}})
	if err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}

	snap := reg.Snapshot()
	if _, ok := snap.Skills["first"]; !ok {
		t.Fatal("first skill should exist")
	}

	// Remove the skill and add a new one.
	if err := os.RemoveAll(filepath.Join(root, "first")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	mustWriteSkill(t, filepath.Join(root, "second"), "second", "second skill")

	_, err = reg.Refresh(context.Background(), []DiscoveryRoot{{Kind: SourceWorkspace, Path: root}})
	if err != nil {
		t.Fatalf("second Refresh() error = %v", err)
	}

	snap = reg.Snapshot()
	if _, ok := snap.Skills["first"]; ok {
		t.Fatal("first skill should be gone after refresh")
	}
	if _, ok := snap.Skills["second"]; !ok {
		t.Fatal("second skill should exist")
	}
}

func TestSkillPackageConfigKey(t *testing.T) {
	t.Parallel()

	// Uses SkillKey when set.
	pkg := &SkillPackage{
		Prompt:   PromptSkill{Name: "display-name"},
		OpenClaw: OpenClawMetadata{SkillKey: "actual.key"},
	}
	if pkg.ConfigKey() != "actual.key" {
		t.Fatalf("ConfigKey() = %q, want 'actual.key'", pkg.ConfigKey())
	}

	// Falls back to Name when SkillKey empty.
	pkg2 := &SkillPackage{
		Prompt: PromptSkill{Name: "my-skill"},
	}
	if pkg2.ConfigKey() != "my-skill" {
		t.Fatalf("ConfigKey() = %q, want 'my-skill'", pkg2.ConfigKey())
	}
}

func TestDiscoveryRootEffectivePriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind     SourceKind
		explicit int
		want     int
	}{
		{SourceWorkspace, 0, 500},
		{SourceUser, 0, 400},
		{SourceClawHub, 0, 300},
		{SourceBundled, 0, 200},
		{SourcePlugin, 0, 100},
		{"unknown", 0, 10},
		{SourceWorkspace, 999, 999},
	}
	for _, tt := range tests {
		root := DiscoveryRoot{Kind: tt.kind, Priority: tt.explicit}
		got := root.effectivePriority()
		if got != tt.want {
			t.Errorf("effectivePriority(kind=%q, explicit=%d) = %d, want %d", tt.kind, tt.explicit, got, tt.want)
		}
	}
}

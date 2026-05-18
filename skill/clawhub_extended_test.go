package skill

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Round 1: Discover all 50+ bundled skills — both official and community
// ---------------------------------------------------------------------------

func TestClawHubRound1_DiscoverAllBundledSkills(t *testing.T) {
	t.Parallel()

	dir := communitySkillsDir()
	loader := FilesystemLoader{}
	sources, err := loader.Discover(context.Background(), []DiscoveryRoot{
		{Kind: SourceBundled, Path: dir},
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	// We expect at least 50 skills (the skills/ directory has 55+ entries).
	if len(sources) < 50 {
		t.Fatalf("expected at least 50 skills, got %d", len(sources))
	}

	// Verify 10 specific skills spanning official + community categories:
	//   Official: github, github-issues, github-pr (dev tools)
	//   Community: weather, calculator, translate, spotify, docker, rss, slack
	expected := map[string]bool{
		"github":        false, // official - dev
		"github-issues": false, // official - dev
		"github-pr":     false, // official - dev
		"weather":       false, // community - util
		"calculator":    false, // community - util
		"translate":     false, // community - util
		"spotify":       false, // community - media
		"docker":        false, // community - infra
		"rss":           false, // community - util
		"slack":         false, // community - comm
		"email":         false, // community - comm
		"redis":         false, // community - infra
	}

	for _, src := range sources {
		if _, ok := expected[src.NameHint]; ok {
			expected[src.NameHint] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected skill %q not discovered", name)
		}
	}

	t.Logf("Round 1 PASS: Discovered %d skills, all 12 target skills found", len(sources))
}

// ---------------------------------------------------------------------------
// Round 2: Compile and verify metadata for 10+ skills across categories
// ---------------------------------------------------------------------------

func TestClawHubRound2_CompileAndVerifyMetadata(t *testing.T) {
	t.Parallel()

	dir := communitySkillsDir()
	loader := FilesystemLoader{}
	compiler := DefaultCompiler{}

	type skillCheck struct {
		Name      string
		SkillKey  string
		HasBins   bool // requires.bins non-empty
		HasEnv    bool // requires.env non-empty
		HasAnyBin bool // requires.anyBins non-empty
		Always    bool
	}

	checks := []skillCheck{
		{Name: "github", SkillKey: "dev.github", HasBins: true, HasEnv: true},
		{Name: "github-issues", SkillKey: "dev.github-issues", HasBins: true, HasEnv: true},
		{Name: "weather", SkillKey: "util.weather"},
		{Name: "calculator", SkillKey: "util.calculator"},
		{Name: "translate", SkillKey: "util.translate"},
		{Name: "spotify", SkillKey: "media.spotify", HasEnv: true},
		{Name: "docker", SkillKey: "infra.docker", HasBins: true},
		{Name: "rss", SkillKey: "util.rss"},
		{Name: "slack", SkillKey: "comm.slack"},
		{Name: "summarize", SkillKey: "util.summarize", Always: true},
	}

	sources, err := loader.Discover(context.Background(), []DiscoveryRoot{
		{Kind: SourceBundled, Path: dir},
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	sourceMap := make(map[string]SkillSource)
	for _, src := range sources {
		sourceMap[src.NameHint] = src
	}

	for _, check := range checks {
		t.Run(check.Name, func(t *testing.T) {
			t.Parallel()

			src, ok := sourceMap[check.Name]
			if !ok {
				t.Fatalf("skill %q not discovered", check.Name)
			}

			spec, err := loader.Load(context.Background(), src)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			pkg, err := compiler.Compile(context.Background(), src, spec)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}

			if pkg.Status == StatusBlocked {
				t.Fatalf("skill blocked: %v", pkg.Issues)
			}
			if pkg.Trust != TrustBundled {
				t.Errorf("trust = %q, expected bundled", pkg.Trust)
			}

			// Verify skillKey.
			if spec.OpenClaw.SkillKey != check.SkillKey {
				t.Errorf("skillKey = %q, expected %q", spec.OpenClaw.SkillKey, check.SkillKey)
			}

			// Verify requirement presence.
			if check.HasBins && len(spec.OpenClaw.Requires.Bins) == 0 {
				t.Error("expected requires.bins to be non-empty")
			}
			if check.HasEnv && len(spec.OpenClaw.Requires.Env) == 0 {
				t.Error("expected requires.env to be non-empty")
			}
			if check.HasAnyBin && len(spec.OpenClaw.Requires.AnyBins) == 0 {
				t.Error("expected requires.anyBins to be non-empty")
			}
			if check.Always != spec.OpenClaw.Always {
				t.Errorf("always = %v, expected %v", spec.OpenClaw.Always, check.Always)
			}

			// Verify prompt compilation produced instructions.
			if pkg.Prompt.Instructions == "" {
				t.Error("compiled prompt instructions should not be empty")
			}
			if pkg.Prompt.Name != spec.Name {
				t.Errorf("prompt name = %q, spec name = %q", pkg.Prompt.Name, spec.Name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Round 3: Eligibility evaluation for 10+ skills with diverse requirements
// ---------------------------------------------------------------------------

func TestClawHubRound3_EligibilityDiverseSkills(t *testing.T) {
	t.Parallel()

	dir := communitySkillsDir()
	reg := NewRegistry(FilesystemLoader{}, DefaultCompiler{})
	snapshot, err := reg.Refresh(context.Background(), []DiscoveryRoot{
		{Kind: SourceBundled, Path: dir},
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	eval := Evaluator{
		LookPath: func(file string) (string, error) {
			available := map[string]string{
				"gh":      "/usr/local/bin/gh",
				"curl":    "/usr/bin/curl",
				"bc":      "/usr/bin/bc",
				"python3": "/usr/bin/python3",
				"docker":  "/usr/local/bin/docker",
			}
			if p, ok := available[file]; ok {
				return p, nil
			}
			return "", &os.PathError{Op: "lookpath", Path: file, Err: os.ErrNotExist}
		},
	}

	ctx := RuntimeContext{
		GOOS: runtime.GOOS,
		SecretPresence: map[string]SecretStatus{
			"GITHUB_TOKEN": {Resolved: true, Source: "runtime_env"},
		},
		Managed: map[string]ManagedEntry{
			"comm.slack": {InjectedEnv: map[string]SecretStatus{
				"SLACK_TOKEN": {Resolved: true, Source: "managed"},
			}},
		},
	}

	type eligTest struct {
		Name     string
		Eligible bool
		Reason   string // brief description for logging
	}

	tests := []eligTest{
		{"github", true, "gh + GITHUB_TOKEN present"},
		{"github-issues", true, "gh + GITHUB_TOKEN present"},
		{"weather", true, "no extra shell deps required"},
		{"calculator", true, "capability-first guidance with no extra shell deps"},
		{"translate", true, "no extra shell deps required"},
		{"docker", true, "docker binary available"},
		{"rss", true, "no extra shell deps required"},
		{"summarize", true, "always=true, no deps"},
		{"slack", true, "managed apikey injection"},
		{"spotify", false, "spotify credentials missing"},
		{"feishu-doc", false, "FEISHU_APP_ID/SECRET missing"},
	}

	for _, tt := range tests {
		t.Run(tt.Name+"_"+tt.Reason, func(t *testing.T) {
			pkg, ok := snapshot.Skills[tt.Name]
			if !ok {
				t.Fatalf("skill %q not in snapshot", tt.Name)
			}
			result := eval.Evaluate(pkg, ctx)
			if result.Eligible != tt.Eligible {
				t.Errorf("eligible = %v, expected %v, reasons: %v",
					result.Eligible, tt.Eligible, result.Reasons)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Round 4: ClawHub install flow for 5 simulated community skills
// ---------------------------------------------------------------------------

func TestClawHubRound4_MultiSkillInstallFlow(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := NewFileClawHubClient(root)
	ctx := context.Background()

	// Populate local index with 5 skills pointing to bundled skill dirs.
	skillsDir := communitySkillsDir()
	installSkills := []struct {
		ID      string
		Name    string
		Version string
		Summary string
	}{
		{"weather", "Weather", "2.0.0", "Get weather forecasts"},
		{"calculator", "Calculator", "1.5.0", "Math calculations"},
		{"translate", "Translate", "1.2.0", "Language translation"},
		{"rss", "RSS Reader", "1.0.0", "RSS feed reader"},
		{"docker", "Docker", "3.0.0", "Container management"},
	}

	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, s := range installSkills {
		writeJSON(t, filepath.Join(indexDir, s.ID+".json"), catalogEntry{
			ID:        s.ID,
			Name:      s.Name,
			Version:   s.Version,
			Summary:   s.Summary,
			BundleDir: filepath.Join(skillsDir, s.ID),
		})
	}

	// Install all 5.
	for _, s := range installSkills {
		t.Run("install_"+s.ID, func(t *testing.T) {
			result, err := client.Install(ctx, InstallRequest{SkillID: s.ID})
			if err != nil {
				t.Fatalf("Install(%s) error = %v", s.ID, err)
			}
			if result.Version != s.Version {
				t.Errorf("version = %q, expected %q", result.Version, s.Version)
			}
			if _, err := os.Stat(filepath.Join(result.InstallDir, "SKILL.md")); err != nil {
				t.Errorf("SKILL.md not found in install dir: %v", err)
			}
			t.Logf("Installed %s v%s → %s", s.ID, result.Version, result.InstallDir)
		})
	}

	// Verify all 5 appear in Installed().
	installed, err := client.Installed()
	if err != nil {
		t.Fatalf("Installed() error = %v", err)
	}
	if len(installed) != 5 {
		t.Fatalf("expected 5 installed skills, got %d", len(installed))
	}

	// Verify they're loadable by the registry as community trust.
	reg := NewRegistry(FilesystemLoader{}, DefaultCompiler{})
	snapshot, err := reg.Refresh(ctx, []DiscoveryRoot{
		{Kind: SourceClawHub, Path: client.Layout.InstallsDir()},
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	for _, s := range installSkills {
		pkg, ok := snapshot.Skills[s.ID]
		if !ok {
			t.Errorf("installed skill %q not found in registry", s.ID)
			continue
		}
		if pkg.Trust != TrustCommunity {
			t.Errorf("skill %q trust = %q, expected community", s.ID, pkg.Trust)
		}
	}

	t.Logf("Round 4 PASS: Installed and verified 5 community skills")
}

// ---------------------------------------------------------------------------
// Round 5: Full lifecycle — install → search → pin → remove → verify gone
// ---------------------------------------------------------------------------

func TestClawHubRound5_FullLifecycleManagement(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := NewFileClawHubClient(root)
	ctx := context.Background()
	skillsDir := communitySkillsDir()

	// Seed index with 3 skills.
	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	seeds := []struct {
		ID, Name, Version string
	}{
		{"github", "GitHub", "1.0.0"},
		{"slack", "Slack", "2.0.0"},
		{"spotify", "Spotify", "1.1.0"},
	}
	for _, s := range seeds {
		writeJSON(t, filepath.Join(indexDir, s.ID+".json"), catalogEntry{
			ID:        s.ID,
			Name:      s.Name,
			Version:   s.Version,
			Summary:   s.Name + " integration",
			BundleDir: filepath.Join(skillsDir, s.ID),
		})
	}

	// Step 1: Search catalog (empty query = list all).
	results, err := client.Search(ctx, "")
	if err != nil {
		t.Fatalf("Search('') error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Search('') returned %d results, expected 3", len(results))
	}
	t.Logf("Step 1: Search returned %d skills", len(results))

	// Step 2: Search with specific query.
	results, err = client.Search(ctx, "github")
	if err != nil {
		t.Fatalf("Search('github') error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "github" {
		t.Fatalf("Search('github') expected 1 result 'github', got %v", results)
	}
	t.Logf("Step 2: Search('github') returned '%s'", results[0].ID)

	// Step 3: Install github + slack.
	for _, id := range []string{"github", "slack"} {
		res, err := client.Install(ctx, InstallRequest{SkillID: id})
		if err != nil {
			t.Fatalf("Install(%s) error = %v", id, err)
		}
		t.Logf("Step 3: Installed %s v%s", id, res.Version)
	}

	// Step 4: Pin github.
	if err := client.Pin(ctx, "github", "1.0.0"); err != nil {
		t.Fatalf("Pin(github) error = %v", err)
	}
	installed, _ := client.Installed()
	for _, s := range installed {
		if s.SkillID == "github" && !s.Pinned {
			t.Error("github should be pinned after Pin()")
		}
	}
	t.Logf("Step 4: Pinned github v1.0.0")

	// Step 5: Remove slack.
	if err := client.Remove("slack"); err != nil {
		t.Fatalf("Remove(slack) error = %v", err)
	}
	installed, _ = client.Installed()
	for _, s := range installed {
		if s.SkillID == "slack" {
			t.Error("slack should be removed from lock after Remove()")
		}
	}
	if len(installed) != 1 {
		t.Fatalf("expected 1 installed skill after remove, got %d", len(installed))
	}
	t.Logf("Step 5: Removed slack, %d skill remaining", len(installed))

	// Step 6: Verify removed skill's install dir is cleaned up.
	slackDir := client.Layout.InstallDir("slack", "2.0.0")
	if _, err := os.Stat(slackDir); !os.IsNotExist(err) {
		t.Errorf("slack install dir should be removed, but exists: %s", slackDir)
	}
	t.Logf("Step 6: Confirmed slack install dir cleaned up")

	// Step 7: Update github (re-install).
	res, err := client.Update(ctx, "github")
	if err != nil {
		t.Fatalf("Update(github) error = %v", err)
	}
	if res.SkillID != "github" {
		t.Errorf("Update result skill = %q", res.SkillID)
	}
	t.Logf("Step 7: Updated github → v%s", res.Version)

	// Step 8: Verify registry sees only the 1 remaining installed skill.
	reg := NewRegistry(FilesystemLoader{}, DefaultCompiler{})
	snapshot, err := reg.Refresh(ctx, []DiscoveryRoot{
		{Kind: SourceClawHub, Path: client.Layout.InstallsDir()},
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if _, ok := snapshot.Skills["github"]; !ok {
		t.Error("github should be in registry")
	}
	if _, ok := snapshot.Skills["slack"]; ok {
		t.Error("slack should NOT be in registry after removal")
	}

	t.Logf("Round 5 PASS: Full lifecycle (search→install→pin→remove→update→verify)")
}

// ---------------------------------------------------------------------------
// Round 5b: Session binding with 10+ skills and prompt catalog verification
// ---------------------------------------------------------------------------

func TestClawHubRound5b_SessionBindWith10Skills(t *testing.T) {
	t.Parallel()

	dir := communitySkillsDir()
	svc := NewService(ServiceConfig{
		Roots: []DiscoveryRoot{
			{Kind: SourceBundled, Path: dir},
		},
		Evaluator: Evaluator{
			LookPath: func(file string) (string, error) {
				return "/usr/local/bin/" + file, nil
			},
		},
	})

	runtimeCtx := RuntimeContext{
		GOOS: runtime.GOOS,
		SecretPresence: map[string]SecretStatus{
			"GITHUB_TOKEN":          {Resolved: true, Source: "runtime_env"},
			"FEISHU_APP_ID":         {Resolved: true, Source: "runtime_env"},
			"FEISHU_APP_SECRET":     {Resolved: true, Source: "runtime_env"},
			"SLACK_TOKEN":           {Resolved: true, Source: "runtime_env"},
			"SPOTIFY_CLIENT_ID":     {Resolved: true, Source: "runtime_env"},
			"SPOTIFY_CLIENT_SECRET": {Resolved: true, Source: "runtime_env"},
			"DEEPL_API_KEY":         {Resolved: true, Source: "runtime_env"},
		},
	}

	session, err := svc.RefreshAndBind(context.Background(), runtimeCtx)
	if err != nil {
		t.Fatalf("RefreshAndBind() error = %v", err)
	}

	// Verify 10+ skills are bound and eligible.
	targetSkills := []string{
		"github", "github-issues", "github-pr",
		"weather", "calculator", "translate",
		"spotify", "docker", "rss", "slack",
		"summarize", "feishu-doc",
	}

	eligibleCount := 0
	for _, name := range targetSkills {
		bound, ok := session.Resolve(name)
		if !ok {
			t.Errorf("skill %q not found in session", name)
			continue
		}
		if bound.Eligibility.Eligible {
			eligibleCount++
		} else {
			t.Errorf("skill %q should be eligible, reasons: %v", name, bound.Eligibility.Reasons)
		}
	}

	if eligibleCount < 10 {
		t.Fatalf("expected at least 10 eligible skills, got %d", eligibleCount)
	}

	// Verify prompt block contains all target skills.
	for _, name := range targetSkills {
		if !strings.Contains(session.PromptBlock, `name="`+name+`"`) {
			t.Errorf("PromptBlock missing skill %q", name)
		}
	}

	// Verify prompt catalog count.
	if len(session.PromptCatalog) < 10 {
		t.Errorf("expected 10+ prompt catalog entries, got %d", len(session.PromptCatalog))
	}

	t.Logf("Round 5b PASS: Session bound with %d eligible skills, %d total in catalog",
		eligibleCount, len(session.PromptCatalog))
}

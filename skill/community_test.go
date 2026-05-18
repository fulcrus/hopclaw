package skill

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// communitySkillsDir returns the path to the bundled community skills directory.
func communitySkillsDir() string {
	// Navigate from skill/ package to project root/skills/.
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "skills")
}

func TestCommunitySkillsDiscovery(t *testing.T) {
	t.Parallel()

	dir := communitySkillsDir()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("community skills directory not found: %v", err)
	}

	loader := FilesystemLoader{}
	sources, err := loader.Discover(context.Background(), []DiscoveryRoot{
		{Kind: SourceBundled, Path: dir},
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	expected := map[string]bool{
		"github":      false,
		"weather":     false,
		"feishu-doc":  false,
		"feishu-wiki": false,
		"summarize":   false,
		"slack":       false,
	}
	for _, src := range sources {
		expected[src.NameHint] = true
	}
	for name, found := range expected {
		if !found {
			t.Errorf("community skill %q not discovered", name)
		}
	}
}

func TestCommunitySkillsParse(t *testing.T) {
	t.Parallel()

	dir := communitySkillsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, entry.Name())
		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()
			spec, err := ParseDir(skillDir)
			if err != nil {
				t.Fatalf("ParseDir(%s) error = %v", skillDir, err)
			}
			if spec.Name == "" {
				t.Error("skill name is empty")
			}
			if spec.Description == "" {
				t.Error("skill description is empty")
			}
			if spec.Body == "" {
				t.Error("skill body is empty")
			}
			if !spec.UserInvocable {
				t.Error("expected user-invocable = true")
			}
		})
	}
}

func TestCommunitySkillsCompile(t *testing.T) {
	t.Parallel()

	dir := communitySkillsDir()
	loader := FilesystemLoader{}
	compiler := DefaultCompiler{}

	sources, err := loader.Discover(context.Background(), []DiscoveryRoot{
		{Kind: SourceBundled, Path: dir},
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	for _, src := range sources {
		t.Run(src.NameHint, func(t *testing.T) {
			t.Parallel()
			spec, err := loader.Load(context.Background(), src)
			if err != nil {
				t.Fatalf("Load(%s) error = %v", src.Dir, err)
			}
			pkg, err := compiler.Compile(context.Background(), src, spec)
			if err != nil {
				t.Fatalf("Compile(%s) error = %v", src.Dir, err)
			}
			if pkg.Status == StatusBlocked {
				t.Fatalf("skill %q is blocked: %v", pkg.Name(), pkg.Issues)
			}
			if pkg.Trust != TrustBundled {
				t.Errorf("expected trust=bundled for source kind=%s, got %q", src.Kind, pkg.Trust)
			}
			if pkg.Prompt.Name != spec.Name {
				t.Errorf("prompt name %q != spec name %q", pkg.Prompt.Name, spec.Name)
			}
			if pkg.Prompt.Instructions == "" {
				t.Error("prompt instructions should not be empty")
			}
		})
	}
}

func TestCommunitySkillsRegistryRefresh(t *testing.T) {
	t.Parallel()

	dir := communitySkillsDir()
	reg := NewRegistry(FilesystemLoader{}, DefaultCompiler{})
	snapshot, err := reg.Refresh(context.Background(), []DiscoveryRoot{
		{Kind: SourceBundled, Path: dir},
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	if len(snapshot.Skills) < 6 {
		t.Fatalf("expected at least 6 skills, got %d", len(snapshot.Skills))
	}

	for _, name := range []string{"github", "weather", "feishu-doc", "feishu-wiki", "summarize", "slack"} {
		if _, ok := snapshot.Skills[name]; !ok {
			t.Errorf("skill %q missing from registry snapshot", name)
		}
	}

	if len(snapshot.Blocked) > 0 {
		for _, b := range snapshot.Blocked {
			t.Errorf("unexpected blocked skill %q: %v", b.NameHint, b.Issues)
		}
	}

	if snapshot.Fingerprint == "" {
		t.Error("snapshot fingerprint should not be empty")
	}
}

func TestCommunitySkillGitHubMetadata(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(communitySkillsDir(), "github")
	spec, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if spec.Name != "github" {
		t.Errorf("name = %q", spec.Name)
	}
	if spec.OpenClaw.SkillKey != "dev.github" {
		t.Errorf("skillKey = %q", spec.OpenClaw.SkillKey)
	}
	if spec.OpenClaw.PrimaryEnv != "GITHUB_TOKEN" {
		t.Errorf("primaryEnv = %q", spec.OpenClaw.PrimaryEnv)
	}
	if len(spec.OpenClaw.Requires.Bins) != 1 || spec.OpenClaw.Requires.Bins[0] != "gh" {
		t.Errorf("requires.bins = %v", spec.OpenClaw.Requires.Bins)
	}
	if len(spec.OpenClaw.Requires.Env) != 1 || spec.OpenClaw.Requires.Env[0] != "GITHUB_TOKEN" {
		t.Errorf("requires.env = %v", spec.OpenClaw.Requires.Env)
	}
	if spec.CommandTool != "github.run" {
		t.Errorf("commandTool = %q", spec.CommandTool)
	}
}

func TestCommunitySkillWeatherAnyBins(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(communitySkillsDir(), "weather")
	spec, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if spec.Name != "weather" {
		t.Errorf("name = %q", spec.Name)
	}
	if spec.OpenClaw.SkillKey != "util.weather" {
		t.Errorf("skillKey = %q", spec.OpenClaw.SkillKey)
	}
	if len(spec.OpenClaw.Requires.AnyBins) != 0 {
		t.Errorf("expected weather skill to avoid legacy anyBins shell deps, got %v", spec.OpenClaw.Requires.AnyBins)
	}
}

func TestCommunitySkillsPrimaryEnvMustBeRequiredWhenPresent(t *testing.T) {
	t.Parallel()

	dir := communitySkillsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, entry.Name())
		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()

			spec, err := ParseDir(skillDir)
			if err != nil {
				t.Fatalf("ParseDir(%s) error = %v", skillDir, err)
			}
			primary := strings.TrimSpace(spec.OpenClaw.PrimaryEnv)
			if primary == "" {
				return
			}
			for _, env := range spec.OpenClaw.Requires.Env {
				if strings.TrimSpace(env) == primary {
					return
				}
			}
			t.Fatalf("%s primaryEnv %q must also appear in requires.env (%v)", spec.Name, primary, spec.OpenClaw.Requires.Env)
		})
	}
}

func TestCommunityCapabilityFirstSkillsAvoidLegacyShellContracts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		required  []string
		forbidden []string
	}{
		{
			name:      "oracle",
			required:  []string{"search.web", "net.fetch", "skill.ensure"},
			forbidden: []string{`curl -s "https://example.com"`, `python3 -c "`},
		},
		{
			name:      "healthcheck",
			required:  []string{"net.fetch", "skill.ensure"},
			forbidden: []string{`curl -s -o /dev/null -w`, "openssl s_client -servername"},
		},
		{
			name:      "calculator",
			required:  []string{"calculator.eval", "skill.ensure"},
			forbidden: []string{`echo "42 * 17 + 3" | bc -l`, `python3 -c "import math;`},
		},
		{
			name:      "weather",
			required:  []string{"search.web", "net.fetch", "skill.ensure"},
			forbidden: []string{"wttr.in", `curl https://wttr.in/`},
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec, err := ParseDir(filepath.Join(communitySkillsDir(), tt.name))
			if err != nil {
				t.Fatalf("ParseDir(%s) error = %v", tt.name, err)
			}
			if len(spec.OpenClaw.Requires.AnyBins) != 0 {
				t.Fatalf("%s anyBins = %v, want none", tt.name, spec.OpenClaw.Requires.AnyBins)
			}
			if spec.CommandDispatch != "" || spec.CommandTool != "" {
				t.Fatalf("%s command dispatch/tool = %q/%q, want empty guidance-only metadata", tt.name, spec.CommandDispatch, spec.CommandTool)
			}
			for _, want := range tt.required {
				if !strings.Contains(spec.Body, want) {
					t.Fatalf("%s body missing %q:\n%s", tt.name, want, spec.Body)
				}
			}
			for _, forbidden := range tt.forbidden {
				if strings.Contains(spec.Body, forbidden) {
					t.Fatalf("%s body unexpectedly contains %q:\n%s", tt.name, forbidden, spec.Body)
				}
			}
		})
	}
}

func TestCommunityAPIWrapperSkillsAvoidLegacyShellTutorials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		toolRef string
	}{
		{"voice-call", "voice-call.run"},
		{"blogwatcher", "blogwatcher.run"},
		{"gifgrep", "gifgrep.run"},
		{"openai-image-gen", "image.generate"},
		{"openhue", "openhue.run"},
		{"goplaces", "goplaces.run"},
		{"bluebubbles", "bluebubbles.run"},
		{"slack", "slack.send"},
		{"feishu-doc", "feishu.doc"},
		{"feishu-wiki", "feishu.wiki"},
		{"github", "github.run"},
		{"github-issues", "github-issues.run"},
		{"github-pr", "github-pr.run"},
		{"notion", "notion.run"},
		{"trello", "trello.run"},
		{"jira", "jira.run"},
		{"eightctl", "eightctl.run"},
		{"spotify", "spotify.run"},
		{"xurl", "xurl.run"},
		{"openai-whisper-api", "openai-whisper-api.run"},
		{"songsee", "songsee.run"},
		{"nano-pdf", "nano-pdf.run"},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec, err := ParseDir(filepath.Join(communitySkillsDir(), tt.name))
			if err != nil {
				t.Fatalf("ParseDir(%s) error = %v", tt.name, err)
			}

			for _, bin := range append(append([]string(nil), spec.OpenClaw.Requires.Bins...), spec.OpenClaw.Requires.AnyBins...) {
				switch strings.TrimSpace(bin) {
				case "curl", "wget", "python3", "jq", "pdftotext":
					t.Fatalf("%s still depends on legacy shell bin %q", tt.name, bin)
				}
			}
			if !strings.Contains(spec.Body, tt.toolRef) {
				t.Fatalf("%s body missing dedicated tool reference %q:\n%s", tt.name, tt.toolRef, spec.Body)
			}
			for _, want := range []string{"skill.ensure", "existing runtime capabilities"} {
				if !strings.Contains(spec.Body, want) {
					t.Fatalf("%s body missing %q:\n%s", tt.name, want, spec.Body)
				}
			}
			for _, forbidden := range []string{"```bash", "curl -s", "wget ", "python3 -c", "jq ", "pdftotext input.pdf"} {
				if strings.Contains(spec.Body, forbidden) {
					t.Fatalf("%s body unexpectedly contains %q:\n%s", tt.name, forbidden, spec.Body)
				}
			}
		})
	}
}

func TestCommunityAPIWrapperSkillEnvContracts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		primaryEnv string
		required   []string
	}{
		{"voice-call", "TWILIO_ACCOUNT_SID", []string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_PHONE_NUMBER"}},
		{"openhue", "HUE_APPLICATION_KEY", []string{"HUE_APPLICATION_KEY", "HUE_BRIDGE_IP"}},
		{"goplaces", "GOOGLE_PLACES_API_KEY", []string{"GOOGLE_PLACES_API_KEY"}},
		{"bluebubbles", "BLUEBUBBLES_PASSWORD", []string{"BLUEBUBBLES_PASSWORD"}},
		{"notion", "NOTION_API_KEY", []string{"NOTION_API_KEY"}},
		{"trello", "TRELLO_API_KEY", []string{"TRELLO_API_KEY", "TRELLO_TOKEN"}},
		{"jira", "JIRA_URL", []string{"JIRA_URL", "JIRA_EMAIL", "JIRA_API_TOKEN"}},
		{"eightctl", "EIGHT_SLEEP_EMAIL", []string{"EIGHT_SLEEP_EMAIL", "EIGHT_SLEEP_PASSWORD"}},
		{"spotify", "SPOTIFY_CLIENT_ID", []string{"SPOTIFY_CLIENT_ID", "SPOTIFY_CLIENT_SECRET"}},
		{"xurl", "X_BEARER_TOKEN", []string{"X_BEARER_TOKEN"}},
		{"openai-whisper-api", "OPENAI_API_KEY", []string{"OPENAI_API_KEY"}},
		{"songsee", "AUDD_API_KEY", []string{"AUDD_API_KEY"}},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec, err := ParseDir(filepath.Join(communitySkillsDir(), tt.name))
			if err != nil {
				t.Fatalf("ParseDir(%s) error = %v", tt.name, err)
			}
			if spec.OpenClaw.PrimaryEnv != tt.primaryEnv {
				t.Fatalf("%s primaryEnv = %q, want %q", tt.name, spec.OpenClaw.PrimaryEnv, tt.primaryEnv)
			}
			if len(spec.OpenClaw.Requires.Env) != len(tt.required) {
				t.Fatalf("%s requires.env = %v, want %v", tt.name, spec.OpenClaw.Requires.Env, tt.required)
			}
			for i, want := range tt.required {
				if spec.OpenClaw.Requires.Env[i] != want {
					t.Fatalf("%s requires.env[%d] = %q, want %q", tt.name, i, spec.OpenClaw.Requires.Env[i], want)
				}
			}
		})
	}
}

func TestCommunitySkillEmailRequiresConfiguredEnv(t *testing.T) {
	t.Parallel()

	spec, err := ParseDir(filepath.Join(communitySkillsDir(), "email"))
	if err != nil {
		t.Fatalf("ParseDir(email) error = %v", err)
	}
	if spec.OpenClaw.PrimaryEnv != "EMAIL_ADDRESS" {
		t.Fatalf("primaryEnv = %q, want %q", spec.OpenClaw.PrimaryEnv, "EMAIL_ADDRESS")
	}
	want := []string{"EMAIL_ADDRESS", "EMAIL_PASSWORD", "EMAIL_SMTP_HOST", "EMAIL_IMAP_HOST"}
	if len(spec.OpenClaw.Requires.Env) != len(want) {
		t.Fatalf("requires.env = %v, want %v", spec.OpenClaw.Requires.Env, want)
	}
	for i, env := range want {
		if spec.OpenClaw.Requires.Env[i] != env {
			t.Fatalf("requires.env[%d] = %q, want %q", i, spec.OpenClaw.Requires.Env[i], env)
		}
	}
}

func TestCommunitySkillRedisLeavesPrimaryEnvUnsetForOptionalConnectionEnv(t *testing.T) {
	t.Parallel()

	spec, err := ParseDir(filepath.Join(communitySkillsDir(), "redis"))
	if err != nil {
		t.Fatalf("ParseDir(redis) error = %v", err)
	}
	if spec.OpenClaw.PrimaryEnv != "" {
		t.Fatalf("primaryEnv = %q, want empty when redis connection env is optional", spec.OpenClaw.PrimaryEnv)
	}
}

func TestCommunitySkillSummarizeAlwaysOn(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(communitySkillsDir(), "summarize")
	spec, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if spec.Name != "summarize" {
		t.Errorf("name = %q", spec.Name)
	}
	if !spec.OpenClaw.Always {
		t.Error("summarize should have always=true")
	}
	if spec.OpenClaw.SkillKey != "util.summarize" {
		t.Errorf("skillKey = %q", spec.OpenClaw.SkillKey)
	}
}

func TestCommunitySkillFeishuDocRequiresEnv(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(communitySkillsDir(), "feishu-doc")
	spec, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if spec.Name != "feishu-doc" {
		t.Errorf("name = %q", spec.Name)
	}
	if spec.OpenClaw.PrimaryEnv != "FEISHU_APP_ID" {
		t.Errorf("primaryEnv = %q", spec.OpenClaw.PrimaryEnv)
	}
	requiredEnv := spec.OpenClaw.Requires.Env
	if len(requiredEnv) != 2 {
		t.Fatalf("expected 2 required env vars, got %v", requiredEnv)
	}
	hasAppID, hasAppSecret := false, false
	for _, e := range requiredEnv {
		if e == "FEISHU_APP_ID" {
			hasAppID = true
		}
		if e == "FEISHU_APP_SECRET" {
			hasAppSecret = true
		}
	}
	if !hasAppID || !hasAppSecret {
		t.Errorf("expected FEISHU_APP_ID and FEISHU_APP_SECRET, got %v", requiredEnv)
	}
}

func TestCommunitySkillSlackRequiresToken(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(communitySkillsDir(), "slack")
	spec, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	if spec.Name != "slack" {
		t.Errorf("name = %q", spec.Name)
	}
	if spec.OpenClaw.PrimaryEnv != "SLACK_TOKEN" {
		t.Errorf("primaryEnv = %q", spec.OpenClaw.PrimaryEnv)
	}
	if spec.OpenClaw.SkillKey != "comm.slack" {
		t.Errorf("skillKey = %q", spec.OpenClaw.SkillKey)
	}
}

func TestCommunitySkillsEligibility(t *testing.T) {
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
			// Simulate: gh and curl available, wget not available.
			switch file {
			case "gh":
				return "/usr/local/bin/gh", nil
			case "curl":
				return "/usr/bin/curl", nil
			default:
				return "", &os.PathError{Op: "lookpath", Path: file, Err: os.ErrNotExist}
			}
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

	t.Run("github_eligible_with_gh_and_token", func(t *testing.T) {
		pkg := snapshot.Skills["github"]
		result := eval.Evaluate(pkg, ctx)
		if !result.Eligible {
			t.Errorf("github should be eligible, reasons: %v", result.Reasons)
		}
	})

	t.Run("weather_eligible_with_curl", func(t *testing.T) {
		pkg := snapshot.Skills["weather"]
		result := eval.Evaluate(pkg, ctx)
		if !result.Eligible {
			t.Errorf("weather should be eligible (has curl), reasons: %v", result.Reasons)
		}
	})

	t.Run("summarize_always_eligible", func(t *testing.T) {
		pkg := snapshot.Skills["summarize"]
		result := eval.Evaluate(pkg, ctx)
		if !result.Eligible {
			t.Errorf("summarize should always be eligible, reasons: %v", result.Reasons)
		}
		if !result.Always {
			t.Error("summarize should have Always=true")
		}
	})

	t.Run("feishu-doc_ineligible_without_env", func(t *testing.T) {
		pkg := snapshot.Skills["feishu-doc"]
		result := eval.Evaluate(pkg, ctx)
		if result.Eligible {
			t.Error("feishu-doc should be ineligible without FEISHU_APP_ID/SECRET")
		}
	})

	t.Run("feishu-doc_eligible_with_env", func(t *testing.T) {
		pkg := snapshot.Skills["feishu-doc"]
		feishuCtx := RuntimeContext{
			GOOS: runtime.GOOS,
			SecretPresence: map[string]SecretStatus{
				"FEISHU_APP_ID":     {Resolved: true, Source: "runtime_env"},
				"FEISHU_APP_SECRET": {Resolved: true, Source: "runtime_env"},
			},
		}
		result := eval.Evaluate(pkg, feishuCtx)
		if !result.Eligible {
			t.Errorf("feishu-doc should be eligible with env vars, reasons: %v", result.Reasons)
		}
	})

	t.Run("slack_eligible_with_managed_injection", func(t *testing.T) {
		pkg := snapshot.Skills["slack"]
		result := eval.Evaluate(pkg, ctx)
		if !result.Eligible {
			t.Errorf("slack should be eligible via managed injection, reasons: %v", result.Reasons)
		}
		if len(result.InjectedEnv) != 1 || result.InjectedEnv[0] != "SLACK_TOKEN" {
			t.Errorf("expected SLACK_TOKEN injection, got %v", result.InjectedEnv)
		}
	})

	t.Run("slack_ineligible_without_token", func(t *testing.T) {
		pkg := snapshot.Skills["slack"]
		noTokenCtx := RuntimeContext{GOOS: runtime.GOOS}
		result := eval.Evaluate(pkg, noTokenCtx)
		if result.Eligible {
			t.Error("slack should be ineligible without SLACK_TOKEN")
		}
	})
}

func TestCommunitySkillsServiceBindSession(t *testing.T) {
	t.Parallel()

	dir := communitySkillsDir()
	svc := NewService(ServiceConfig{
		Roots: []DiscoveryRoot{
			{Kind: SourceBundled, Path: dir},
		},
		Evaluator: Evaluator{
			LookPath: func(file string) (string, error) {
				// All binaries available.
				return "/usr/local/bin/" + file, nil
			},
		},
	})

	runtimeCtx := RuntimeContext{
		GOOS: runtime.GOOS,
		SecretPresence: map[string]SecretStatus{
			"GITHUB_TOKEN":      {Resolved: true, Source: "runtime_env"},
			"FEISHU_APP_ID":     {Resolved: true, Source: "runtime_env"},
			"FEISHU_APP_SECRET": {Resolved: true, Source: "runtime_env"},
			"SLACK_TOKEN":       {Resolved: true, Source: "runtime_env"},
		},
	}

	session, err := svc.RefreshAndBind(context.Background(), runtimeCtx)
	if err != nil {
		t.Fatalf("RefreshAndBind() error = %v", err)
	}

	// All 6 skills should be in the session.
	if len(session.Skills) < 6 {
		t.Fatalf("expected at least 6 skills in session, got %d", len(session.Skills))
	}

	// All should be eligible when all deps are satisfied.
	for _, name := range []string{"github", "weather", "feishu-doc", "feishu-wiki", "summarize", "slack"} {
		bound, ok := session.Resolve(name)
		if !ok {
			t.Errorf("skill %q not found in session", name)
			continue
		}
		if !bound.Eligibility.Eligible {
			t.Errorf("skill %q should be eligible, reasons: %v", name, bound.Eligibility.Reasons)
		}
	}

	// Prompt catalog should contain all eligible skills.
	if len(session.PromptCatalog) < 6 {
		t.Errorf("expected at least 6 prompt catalog entries, got %d", len(session.PromptCatalog))
	}

	// PromptBlock should be formatted XML.
	if !strings.Contains(session.PromptBlock, `<skills>`) {
		t.Error("PromptBlock should contain <skills> tag")
	}
	for _, name := range []string{"github", "weather", "feishu-doc", "feishu-wiki", "summarize", "slack"} {
		if !strings.Contains(session.PromptBlock, `name="`+name+`"`) {
			t.Errorf("PromptBlock should contain skill %q", name)
		}
	}
}

func TestCommunitySkillsWorkspacePriorityOverBundled(t *testing.T) {
	t.Parallel()

	bundledDir := communitySkillsDir()

	// Create a workspace skill that overrides bundled weather.
	tmp := t.TempDir()
	wsDir := filepath.Join(tmp, "weather")
	mustWriteSkill(t, wsDir, "weather", "workspace weather override")

	reg := NewRegistry(FilesystemLoader{}, DefaultCompiler{})
	snapshot, err := reg.Refresh(context.Background(), []DiscoveryRoot{
		{Kind: SourceBundled, Path: bundledDir},
		{Kind: SourceWorkspace, Path: tmp},
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	pkg, ok := snapshot.Skills["weather"]
	if !ok {
		t.Fatal("weather skill not found")
	}
	// Workspace (priority 500) should override bundled (priority 200).
	if pkg.Prompt.Description != "workspace weather override" {
		t.Errorf("expected workspace override, got description=%q source=%s",
			pkg.Prompt.Description, pkg.Source.Kind)
	}
}

func TestCommunitySkillsClawHubInstallFlow(t *testing.T) {
	t.Parallel()

	// Simulate installing a community skill via ClawHub.
	root := t.TempDir()
	client := NewFileClawHubClient(root)

	// Create a bundle from the github skill.
	githubSkillDir := filepath.Join(communitySkillsDir(), "github")

	indexDir := client.Layout.IndexDir()
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(indexDir, "github.json"), catalogEntry{
		ID:        "github",
		Name:      "GitHub",
		Version:   "1.0.0",
		Summary:   "GitHub integration via gh CLI",
		BundleDir: githubSkillDir,
	})

	ctx := context.Background()
	result, err := client.Install(ctx, InstallRequest{SkillID: "github"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	// Verify the installed skill can be loaded by the registry.
	reg := NewRegistry(FilesystemLoader{}, DefaultCompiler{})
	snapshot, err := reg.Refresh(ctx, []DiscoveryRoot{
		{Kind: SourceClawHub, Path: filepath.Dir(result.InstallDir)},
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	pkg, ok := snapshot.Skills["github"]
	if !ok {
		t.Fatal("installed github skill not found in registry")
	}
	if pkg.Trust != TrustCommunity {
		t.Errorf("installed skill trust = %q, expected community", pkg.Trust)
	}
	if pkg.OpenClaw.SkillKey != "dev.github" {
		t.Errorf("skillKey = %q", pkg.OpenClaw.SkillKey)
	}
}

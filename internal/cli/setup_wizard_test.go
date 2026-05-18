package cli

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/i18n"
)

func TestNormalizeInstallLang(t *testing.T) {

	cases := []struct {
		input string
		want  installLang
	}{
		{input: "zh", want: installLangChinese},
		{input: "zh-CN", want: installLangChinese},
		{input: "en_US.UTF-8", want: installLangEnglish},
		{input: "english", want: installLangEnglish},
		{input: "", want: ""},
		{input: "fr", want: ""},
	}

	for _, tc := range cases {
		if got := normalizeInstallLang(tc.input); got != tc.want {
			t.Fatalf("normalizeInstallLang(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestOnboardSkipOptionLabel(t *testing.T) {
	t.Setenv(installLangEnv, "zh")
	want := i18n.TCtx(i18n.WithLocale(context.Background(), i18n.ZhCN), "cli.setup_cli.skip_dashboard")
	if got := onboardSkipOptionLabel(); got != want {
		t.Fatalf("onboardSkipOptionLabel() = %q, want %q", got, want)
	}
}

func TestInstallCatalogKeyStable(t *testing.T) {

	tests := []struct {
		input string
		want  string
	}{
		{
			input: "HopClaw non-interactive onboarding",
			want:  "cli.install_text.hopclaw_non_interactive_onboarding",
		},
		{
			input: "Step 1/%d: Auth choice ... skipped (using defaults)\n",
			want:  "cli.install_text.step_1_d_auth_choice_skipped_using_defaults",
		},
		{
			input: "  Detected %s API key\n",
			want:  "cli.install_text.detected_s_api_key",
		},
	}

	for _, tt := range tests {
		if got := installCatalogKey(tt.input); got != tt.want {
			t.Fatalf("installCatalogKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestITextUsesCatalogWhenInstallTextKeyExists(t *testing.T) {
	t.Setenv(installLangEnv, "zh-CN")

	want := i18n.TCtx(i18n.WithLocale(context.Background(), i18n.ZhCN), "cli.install_text.hopclaw_non_interactive_onboarding")
	if got := itext("HopClaw non-interactive onboarding", "HopClaw 非交互式安装向导"); got != want {
		t.Fatalf("itext() = %q, want %q", got, want)
	}
}

func TestNonInteractiveOnboardingCatalogKeysExistForAllSupportedLocales(t *testing.T) {

	englishPhrases := []string{
		"HopClaw non-interactive onboarding",
		"Step 1/%d: Auth choice ... skipped (using defaults)\n",
		"Step 2/%d: Model provider\n",
		"  Detected %s API key\n",
		"Step 3/%d: Channel setup ... skipped\n",
		"Step 4/%d: Gateway setup ... using defaults\n",
		"Step 5/%d: Daemon install ... skipped\n",
		"Step 6/%d: Verify connectivity\n",
		"Step 7/%d: Skill install ... skipped\n",
		"Non-interactive onboarding complete!",
	}

	for _, localeName := range i18n.SupportedLocaleStrings() {
		locale := i18n.Locale(localeName)
		i18n.Global().EnsureLoaded(locale)
		messages := i18n.Global().Messages(locale)
		for _, english := range englishPhrases {
			key := installCatalogKey(english)
			if key == "" {
				t.Fatalf("expected catalog key for %q", english)
			}
			if got := messages[key]; got == "" {
				t.Fatalf("locale %q missing catalog key %q", locale, key)
			}
		}
	}
}

func TestDefaultOnboardAuthModePrefersRecommended(t *testing.T) {

	if got := defaultOnboardAuthMode(localCLISetupCatalog()); got != onboardSkipOptionValue {
		t.Fatalf("defaultOnboardAuthMode() = %q, want skip", got)
	}
}

func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, profile := range config.SetupProviderProfiles() {
		for _, envVar := range profile.EnvVars {
			t.Setenv(envVar, "")
		}
	}
}

func TestResolveProviderSetupOptionsWithCatalog_Skip(t *testing.T) {
	opts, err := resolveProviderSetupOptionsWithCatalog(localCLISetupCatalog(), "", true)
	if err != nil {
		t.Fatalf("resolveProviderSetupOptionsWithCatalog() error: %v", err)
	}
	if opts.Provider != "" {
		t.Fatalf("Provider = %q, want empty", opts.Provider)
	}
	if opts.ProviderAPI != "" {
		t.Fatalf("ProviderAPI = %q, want empty", opts.ProviderAPI)
	}
	if len(opts.ProviderValues) != 0 {
		t.Fatalf("ProviderValues = %#v, want empty", opts.ProviderValues)
	}
}

func TestDefaultOnboardProviderSelection_PrefersDetectedProvider(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-detected")

	got := defaultOnboardProviderSelection(localCLISetupCatalog())
	if got != "" {
		t.Fatalf("defaultOnboardProviderSelection() = %q, want empty skip selection", got)
	}
}

func TestDefaultOnboardProviderSelection_FallsBackToFirstCatalogProvider(t *testing.T) {
	clearProviderEnv(t)
	catalog := localCLISetupCatalog()
	profiles := catalog.ProviderProfiles()
	if len(profiles) == 0 {
		t.Fatal("expected provider profiles")
	}

	got := defaultOnboardProviderSelection(catalog)
	if got != "" {
		t.Fatalf("defaultOnboardProviderSelection() = %q, want empty skip selection", got)
	}
}

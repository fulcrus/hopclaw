package agent

import "testing"

func TestDetectLanguageProfileClassifiesPrimaryLanguageAndScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input      string
		wantFamily string
		wantScript string
	}{
		{
			input:      "What's the latest Go version today?",
			wantFamily: "und",
			wantScript: "Latn",
		},
		{
			input:      "读取这个 RSS feed 并总结最近三条：https://example.com/feed.xml",
			wantFamily: "zh",
			wantScript: "Han",
		},
		{
			input:      "¿Puedes abrir https://example.com y guardar un resumen en docs/tmp/resumen.md?",
			wantFamily: "es",
			wantScript: "Latn",
		},
		{
			input:      "Abre https://example.com y guarda un resumen en docs/tmp/resumen.md",
			wantFamily: "es",
			wantScript: "Latn",
		},
		{
			input:      "現在開いているアプリを一覧し、前面ウィンドウのスクリーンショットを見せてください。",
			wantFamily: "ja",
			wantScript: "Jpan",
		},
	}

	for _, tt := range tests {
		profile := detectLanguageProfile(tt.input)
		if profile.Family != tt.wantFamily {
			t.Errorf("detectLanguageProfile(%q).Family = %q, want %q", tt.input, profile.Family, tt.wantFamily)
		}
		if profile.Script != tt.wantScript {
			t.Errorf("detectLanguageProfile(%q).Script = %q, want %q", tt.input, profile.Script, tt.wantScript)
		}
		if profile.SupportsKeywordFallback {
			t.Errorf("detectLanguageProfile(%q).SupportsKeywordFallback = true, want false", tt.input)
		}
		if !profile.MainSemanticPath {
			t.Errorf("detectLanguageProfile(%q).MainSemanticPath = false, want true", tt.input)
		}
		if profile.Confidence <= 0 {
			t.Errorf("detectLanguageProfile(%q).Confidence = %v, want positive", tt.input, profile.Confidence)
		}
	}
}

func TestDetectLanguageProfileReturnsZeroForBlankInput(t *testing.T) {
	t.Parallel()

	if got := detectLanguageProfile("  "); got != (LanguageProfile{}) {
		t.Fatalf("detectLanguageProfile(blank) = %#v, want zero profile", got)
	}
}

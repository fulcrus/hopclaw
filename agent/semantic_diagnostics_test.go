package agent

import "testing"

func TestSupportedSemanticLanguageFamiliesStable(t *testing.T) {
	t.Parallel()

	got := SupportedSemanticLanguageFamilies()
	want := []string{"ar", "de", "el", "es", "fr", "he", "hi", "ja", "ko", "pt", "ru", "th", "und", "zh"}
	if len(got) != len(want) {
		t.Fatalf("len(SupportedSemanticLanguageFamilies()) = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SupportedSemanticLanguageFamilies()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAgentComponentSemanticPipelineSummaryReportsConfiguredStages(t *testing.T) {
	t.Parallel()

	component := (&AgentComponent{}).
		WithRunTriage(&staticRunTriage{}).
		WithPreflightAnalyzer(&staticPreflightAnalyzer{}).
		WithTaskContractAnalyzer(&countingTaskContractAnalyzer{})

	got := component.SemanticPipelineSummary()
	if !got.SharedSignalEnabled || !got.LanguageProfileEnabled {
		t.Fatalf("summary = %+v, want semantic signal and language profile enabled", got)
	}
	if !got.RunTriageConfigured || !got.PreflightAnalyzerConfigured || !got.TaskContractConfigured {
		t.Fatalf("summary = %+v, want all analyzers configured", got)
	}
	if len(got.MainPathLanguageFamilies) == 0 || got.MainPathLanguageFamilies[0] != "ar" {
		t.Fatalf("summary.MainPathLanguageFamilies = %#v, want stable supported families", got.MainPathLanguageFamilies)
	}
}

func TestNilAgentComponentSemanticPipelineSummaryIsZero(t *testing.T) {
	t.Parallel()

	var component *AgentComponent
	got := component.SemanticPipelineSummary()
	if got.SharedSignalEnabled || got.LanguageProfileEnabled || got.RunTriageConfigured || got.PreflightAnalyzerConfigured || got.TaskContractConfigured {
		t.Fatalf("summary = %+v, want zero summary", got)
	}
	if len(got.MainPathLanguageFamilies) != 0 {
		t.Fatalf("summary.MainPathLanguageFamilies = %#v, want empty", got.MainPathLanguageFamilies)
	}
}

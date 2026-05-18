package semanticschema

import (
	"strings"
	"testing"
)

func TestBuildRunTriagePromptUsesCanonicalSuggestedDomains(t *testing.T) {
	t.Parallel()

	prompt := BuildRunTriagePrompt()
	for _, domain := range []string{"search", "web", "news", "browser", "agent", "gateway"} {
		if !strings.Contains(prompt, `"`+domain+`"`) {
			t.Fatalf("prompt missing suggested domain %q: %s", domain, prompt)
		}
	}
	if !strings.Contains(prompt, `"requires_current_info"`) {
		t.Fatalf("prompt missing requires_current_info field: %s", prompt)
	}
	if !strings.Contains(prompt, "requires_current_info=true") {
		t.Fatalf("prompt missing requires_current_info guidance: %s", prompt)
	}
	if !strings.Contains(prompt, "semantic_signal") {
		t.Fatalf("prompt missing semantic_signal guidance: %s", prompt)
	}
}

func TestBuildPreflightAnalyzerPromptListsTier3DetectedDomains(t *testing.T) {
	t.Parallel()

	prompt := BuildPreflightAnalyzerPrompt()
	for _, domain := range []string{"browser", "desktop", "channel", "cron", "agent", "proc"} {
		if !strings.Contains(prompt, `"`+domain+`"`) {
			t.Fatalf("prompt missing detected domain %q: %s", domain, prompt)
		}
	}
	if !strings.Contains(prompt, "Do not include them in detected_domains.") {
		t.Fatalf("prompt missing detected_domains guidance: %s", prompt)
	}
}

func TestTaskContractSchemaValidatorsMatchPromptContracts(t *testing.T) {
	t.Parallel()

	if !IsTaskContractJobType("monitor") {
		t.Fatal("expected monitor to be a valid task contract job type")
	}
	if !IsTaskContractDeliverableKind("browser_evidence") {
		t.Fatal("expected browser_evidence to be a valid task contract deliverable kind")
	}
	if !IsTaskContractMissingInfoID("schedule") {
		t.Fatal("expected schedule to be a valid task contract missing info id")
	}
	if IsTaskContractJobType("unknown_job") {
		t.Fatal("unexpected acceptance of invalid task contract job type")
	}
	if IsTaskContractDeliverableKind("artifact_bundle") {
		t.Fatal("unexpected acceptance of invalid task contract deliverable kind")
	}
	if IsTaskContractMissingInfoID("timezone") {
		t.Fatal("unexpected acceptance of invalid task contract missing info id")
	}
}

func TestTaskContractCapabilityHintsUseCurrentToolFamilies(t *testing.T) {
	t.Parallel()

	hints := TaskContractCapabilityHints()
	for _, want := range []string{"search.news", "news.digest", "search.web", "email.search", "email.send"} {
		found := false
		for _, hint := range hints {
			if hint == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("TaskContractCapabilityHints() missing %q: %#v", want, hints)
		}
	}
	for _, forbidden := range []string{"rss.fetch", "weather.fetch"} {
		for _, hint := range hints {
			if hint == forbidden {
				t.Fatalf("TaskContractCapabilityHints() unexpectedly contains %q: %#v", forbidden, hints)
			}
		}
	}
}

func TestBuildIngressRoutingPromptSharesSingleClassifierText(t *testing.T) {
	t.Parallel()

	prompt := BuildIngressRoutingPrompt("ingress triage engine")
	if !strings.Contains(prompt, "ingress triage engine") {
		t.Fatalf("prompt missing requested role label: %s", prompt)
	}
	if !strings.Contains(prompt, "Allowed intent values: reply_status, cancel_run, steer_current_run, enqueue_task, smalltalk, safe_queue.") {
		t.Fatalf("prompt missing canonical ingress intents: %s", prompt)
	}
}

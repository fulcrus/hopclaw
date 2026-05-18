package agent

import (
	"strings"
	"testing"

	planpkg "github.com/fulcrus/hopclaw/planner"
)

func TestEnrichPlanVerificationHintsInfersTaskAndDeliverChecks(t *testing.T) {
	t.Parallel()

	plan := &planpkg.Plan{
		Goal: "Create a workbook and email it to finance.",
		Tasks: []planpkg.Task{
			{
				ID:                   "sheet",
				Kind:                 planpkg.TaskTransform,
				Goal:                 "Write the quarterly workbook.",
				Outputs:              []string{"reports/q1.xlsx"},
				RequiredCapabilities: []string{"spreadsheet.write_range"},
			},
			{
				ID:                   "send",
				Kind:                 planpkg.TaskDeliver,
				Goal:                 "Email the finished workbook to finance.",
				DependsOn:            []string{"sheet"},
				RequiredCapabilities: []string{"email.send"},
			},
		},
		FinalTask: "send",
	}

	enrichPlanVerificationHints(plan, "整理成 Excel 然后发邮件给财务", "")

	if got := plan.Tasks[0].VerificationHints; len(got) != 1 || got[0] != "spreadsheet" {
		t.Fatalf("sheet verification hints = %v, want [spreadsheet]", got)
	}
	got := strings.Join(plan.Tasks[1].VerificationHints, ",")
	if got != "spreadsheet,email" {
		t.Fatalf("send verification hints = %v, want [spreadsheet email]", plan.Tasks[1].VerificationHints)
	}
}

func TestEnrichPlanVerificationHintsInfersPresentationAndWatch(t *testing.T) {
	t.Parallel()

	plan := &planpkg.Plan{
		Goal: "Monitor pricing changes and prepare a slide deck summary.",
		Tasks: []planpkg.Task{
			{
				ID:                   "monitor",
				Kind:                 planpkg.TaskExecute,
				Goal:                 "Watch the stock page and alert when price changes.",
				RequiredCapabilities: []string{"watch.create"},
			},
			{
				ID:      "deck",
				Kind:    planpkg.TaskDeliver,
				Goal:    "Create the presentation for leadership.",
				Outputs: []string{"deliverables/briefing.pptx"},
			},
		},
		FinalTask: "deck",
	}

	enrichPlanVerificationHints(plan, "监控股价变化并生成 PPT 汇报", "")

	if got := plan.Tasks[0].VerificationHints; len(got) != 1 || got[0] != "watch" {
		t.Fatalf("monitor verification hints = %v, want [watch]", got)
	}
	if got := strings.Join(plan.Tasks[1].VerificationHints, ","); got != "presentation" {
		t.Fatalf("deck verification hints = %v, want [presentation]", plan.Tasks[1].VerificationHints)
	}
}

func TestEnrichPlanVerificationHintsInfersBrowserAndDesktopEvidence(t *testing.T) {
	t.Parallel()

	plan := &planpkg.Plan{
		Goal: "Capture browser and desktop evidence for the issue report.",
		Tasks: []planpkg.Task{
			{
				ID:                   "browser",
				Kind:                 planpkg.TaskExecute,
				Goal:                 "Open the target page and capture a screenshot.",
				RequiredCapabilities: []string{"browser.screenshot"},
			},
			{
				ID:                   "desktop",
				Kind:                 planpkg.TaskDeliver,
				Goal:                 "Capture the desktop UI tree and package the findings.",
				DependsOn:            []string{"browser"},
				RequiredCapabilities: []string{"desktop.snapshot"},
			},
		},
		FinalTask: "desktop",
	}

	enrichPlanVerificationHints(plan, "打开网页后抓浏览器和桌面证据", "")

	if got := strings.Join(plan.Tasks[0].VerificationHints, ","); got != "browser" {
		t.Fatalf("browser verification hints = %v, want [browser]", plan.Tasks[0].VerificationHints)
	}
	if got := strings.Join(plan.Tasks[1].VerificationHints, ","); got != "browser,desktop" {
		t.Fatalf("desktop verification hints = %v, want [browser desktop]", plan.Tasks[1].VerificationHints)
	}
}

func TestComposeTaskSystemPromptIncludesVerificationHintsAndOutputs(t *testing.T) {
	t.Parallel()

	run := &Run{
		Plan: &planpkg.Plan{
			Goal:      "Create and send the report.",
			FinalTask: "deliver",
		},
	}
	task := &planpkg.Task{
		ID:                "deliver",
		Kind:              planpkg.TaskDeliver,
		Title:             "Send report",
		Goal:              "Send the generated spreadsheet by email.",
		Outputs:           []string{"reports/q1.xlsx"},
		VerificationHints: []string{"spreadsheet", "email"},
	}

	prompt := composeTaskSystemPrompt(run, "base prompt", task, nil)
	for _, expected := range []string{
		"Expected outputs: reports/q1.xlsx",
		"Expected verification domains: spreadsheet, email",
		"Do not claim task completion unless there is concrete output, tool evidence, or artifacts",
		"If two similar tool attempts do not change the evidence",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q\n%s", expected, prompt)
		}
	}
}

func TestComposeTaskSystemPromptAddsSearchResultsStopCondition(t *testing.T) {
	t.Parallel()

	run := &Run{
		Plan: &planpkg.Plan{
			Goal:      "Open https://www.bing.com/search?q=openai and extract the first 5 results.",
			FinalTask: "search",
		},
	}
	task := &planpkg.Task{
		ID:    "search",
		Kind:  planpkg.TaskExecute,
		Title: "Extract top search results",
		Goal:  "Wait for https://www.bing.com/search?q=openai to load and extract the first 5 items.",
	}

	prompt := composeTaskSystemPrompt(run, "base prompt", task, nil)
	for _, expected := range []string{
		"once the results are visible and the first requested items are captured",
		"If current page context already includes a loaded search-results page, use that page as the target instead of asking the user to restate the query",
		"Prefer browser.wait plus one evidence pass such as browser.snapshot or browser.screenshot",
		"If titles or links are still unclear, use browser.snapshot_aria once for a targeted structure check, not as a repeated fallback",
		"Avoid browser.element_text or browser.element_attr on broad search-result containers",
		"Avoid browser.screenshot_labeled on large search-result pages unless labeled UI evidence is explicitly requested",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q\n%s", expected, prompt)
		}
	}
}

func TestValidatePlanCoverageUsesStructuredSignals(t *testing.T) {
	t.Parallel()

	plan := &planpkg.Plan{
		Goal: "Complete the requested work.",
		Tasks: []planpkg.Task{
			{
				ID:      "sheet",
				Kind:    planpkg.TaskTransform,
				Goal:    "Task A.",
				Outputs: []string{"deliverables/q1.xlsx"},
			},
			{
				ID:                   "send",
				Kind:                 planpkg.TaskDeliver,
				Goal:                 "Task B.",
				DependsOn:            []string{"sheet"},
				RequiredCapabilities: []string{"email.send"},
			},
			{
				ID:                   "watch",
				Kind:                 planpkg.TaskExecute,
				Goal:                 "Task C.",
				RequiredCapabilities: []string{"watch.add"},
			},
			{
				ID:                   "deploy",
				Kind:                 planpkg.TaskExecute,
				Goal:                 "Task D.",
				RequiredCapabilities: []string{"deploy.apply"},
				Outputs:              []string{"https://deploy.example.com/releases/2026-04-10"},
			},
		},
		FinalTask: "send",
	}
	contract := &TaskContract{
		ExpectedDeliverables: []TaskContractDeliverable{
			{Kind: taskDeliverableSpreadsheet, Required: true},
			{Kind: taskDeliverableMessageDelivery, Required: true},
			{Kind: taskDeliverableWatchAlert, Required: true},
			{Kind: taskDeliverableDeployment, Required: true},
		},
	}

	if warnings := validatePlanCoverage(plan, contract); len(warnings) != 0 {
		t.Fatalf("validatePlanCoverage() warnings = %#v, want none", warnings)
	}
}

func TestValidatePlanCoverageDoesNotTrustNaturalLanguageKeywords(t *testing.T) {
	t.Parallel()

	plan := &planpkg.Plan{
		Goal: "Complete the requested work.",
		Tasks: []planpkg.Task{
			{
				ID:   "sheet",
				Kind: planpkg.TaskExecute,
				Goal: "Create a spreadsheet export and send a deployment update.",
			},
		},
		FinalTask: "sheet",
	}
	contract := &TaskContract{
		ExpectedDeliverables: []TaskContractDeliverable{
			{Kind: taskDeliverableSpreadsheet, Required: true},
			{Kind: taskDeliverableDeployment, Required: true},
		},
	}

	warnings := validatePlanCoverage(plan, contract)
	if len(warnings) != 2 {
		t.Fatalf("len(warnings) = %d, want 2 (%#v)", len(warnings), warnings)
	}
	for _, expected := range []string{taskDeliverableSpreadsheet, taskDeliverableDeployment} {
		if !containsPlanCoverageWarning(warnings, expected) {
			t.Fatalf("warnings = %#v, want warning for %q", warnings, expected)
		}
	}
}

func containsPlanCoverageWarning(warnings []string, want string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, want) {
			return true
		}
	}
	return false
}

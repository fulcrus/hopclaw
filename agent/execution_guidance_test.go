package agent

import (
	"strings"
	"testing"
)

func TestBuildExecutionEvidencePromptForBrowserTask(t *testing.T) {
	prompt := buildExecutionEvidencePrompt(&Run{}, "打开 https://httpbin.org/forms/post，填写表单并提交", []ToolDefinition{
		{Name: "browser.open"},
		{Name: "browser.click"},
		{Name: "browser.wait"},
		{Name: "browser.screenshot"},
		{Name: "exec.run"},
	})
	if !strings.Contains(prompt, "Use `browser.*` tools") {
		t.Fatalf("prompt = %q, want browser guidance", prompt)
	}
	if !strings.Contains(prompt, "post-submit URL/title") {
		t.Fatalf("prompt = %q, want browser evidence guidance", prompt)
	}
	if !strings.Contains(prompt, "Do not guess local browser cache") {
		t.Fatalf("prompt = %q, want browser temp-file guidance", prompt)
	}
	if !strings.Contains(prompt, "Use `browser.type` only for text inputs or textareas") {
		t.Fatalf("prompt = %q, want browser field-type guidance", prompt)
	}
	if !strings.Contains(prompt, "use `browser.type_aria` for text fields and `browser.click_aria` for buttons") {
		t.Fatalf("prompt = %q, want aria follow-up guidance", prompt)
	}
	if !strings.Contains(prompt, "avoid repeated ARIA snapshots unless the page changed") {
		t.Fatalf("prompt = %q, want aria loop-avoidance guidance", prompt)
	}
	if !strings.Contains(prompt, "accessible name matches the requested action") {
		t.Fatalf("prompt = %q, want aria button-selection guidance", prompt)
	}
	if !strings.Contains(prompt, "Skip optional specialized widgets such as time pickers or spinbuttons") {
		t.Fatalf("prompt = %q, want specialized-widget guidance", prompt)
	}
	if !strings.Contains(prompt, "batch the corresponding `browser.type_aria` calls and the final submit click") {
		t.Fatalf("prompt = %q, want browser batching guidance", prompt)
	}
	if !strings.Contains(prompt, "do not stop after typing") {
		t.Fatalf("prompt = %q, want submit-after-typing guidance", prompt)
	}
	if !strings.Contains(prompt, "If you have not called a tool yet") {
		t.Fatalf("prompt = %q, want no-fabrication guidance", prompt)
	}
}

func TestBuildExecutionEvidencePromptForFileTask(t *testing.T) {
	prompt := buildExecutionEvidencePrompt(&Run{
		TaskContract: &TaskContract{
			SuggestedDomains: []string{"fs", "document"},
		},
	}, "比较 README.md 和 README.zh-CN.md 的 browser/desktop 能力说明差异", []ToolDefinition{
		{Name: "fs.read"},
		{Name: "fs.find"},
	})
	if !strings.Contains(prompt, "inspect the real workspace first") {
		t.Fatalf("prompt = %q, want file evidence guidance", prompt)
	}
	if !strings.Contains(prompt, "Do not paste raw JSON") {
		t.Fatalf("prompt = %q, want raw output guidance", prompt)
	}
}

func TestBuildExecutionEvidencePromptForBrowserToFileTask(t *testing.T) {
	prompt := buildExecutionEvidencePrompt(&Run{}, "抓取 https://example.com 页面信息，写到 docs/tmp/example-brief.md", []ToolDefinition{
		{Name: "browser.open"},
		{Name: "browser.snapshot"},
		{Name: "fs.write"},
	})
	if !strings.Contains(prompt, "gather page evidence with `browser.*` tools before reading unrelated workspace files") {
		t.Fatalf("prompt = %q, want browser-to-file ordering guidance", prompt)
	}
	if !strings.Contains(prompt, "explicit destination path") {
		t.Fatalf("prompt = %q, want destination-path guidance", prompt)
	}
}

func TestBuildExecutionEvidencePromptForSearchResultsTask(t *testing.T) {
	prompt := buildExecutionEvidencePrompt(&Run{
		Preflight: &RunPreflightReport{
			SuggestedDomains: []string{"browser", "search", "web"},
		},
	}, "打开页面，等搜索结果加载出来，再提取前 5 条", []ToolDefinition{
		{Name: "browser.wait"},
		{Name: "browser.snapshot"},
		{Name: "browser.screenshot"},
	})
	for _, expected := range []string{
		"wait until the results list is visibly loaded once",
		"If the current session already has a loaded search-results page, use that current page as the target",
		"Prefer `browser.wait` with one page evidence pass such as `browser.snapshot` or `browser.screenshot`",
		"If the titles or links are not obvious from the DOM snapshot, use `browser.snapshot_aria` once",
		"Avoid `browser.element_text` or `browser.element_attr`",
		"Avoid `browser.screenshot_labeled` on large search-result pages",
		"stop taking more snapshots and return the final answer",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q\n%s", expected, prompt)
		}
	}
}

func TestBuildExecutionEvidencePromptDoesNotInferFileContextFromKeywordsAlone(t *testing.T) {
	t.Parallel()

	prompt := buildExecutionEvidencePrompt(&Run{}, "请分析一下日志是否异常", []ToolDefinition{
		{Name: "fs.read"},
		{Name: "fs.find"},
	})
	if strings.Contains(prompt, "inspect the real workspace first") {
		t.Fatalf("prompt = %q, want no file-evidence guidance from raw natural-language keywords", prompt)
	}
}

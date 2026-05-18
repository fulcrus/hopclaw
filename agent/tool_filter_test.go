package agent

import (
	"strings"
	"testing"

	planpkg "github.com/fulcrus/hopclaw/planner"
)

func TestExtractToolDomain(t *testing.T) {
	tests := []struct {
		name string
		want ToolDomain
	}{
		{"fs.read", DomainFS},
		{"fs.write", DomainFS},
		{"browser.click", DomainBrowser},
		{"desktop.list_apps", DomainDesktop},
		{"text.json", DomainText},
		{"spreadsheet.read_range", DomainSheet},
		{"exec.run", DomainExec},
		{"channel.send", DomainChannel},
		{"db.query", DomainDB},
		{"skill.list", DomainSkill},
		{"watch.list", DomainWatch},
		{"custom-tool", ""},   // no dot prefix
		{"my-skill-tool", ""}, // no dot prefix
		{"", ""},              // empty
		{"nodot", ""},         // no dot
		{".leading", ""},      // dot at position 0
	}
	for _, tt := range tests {
		got := extractToolDomain(tt.name)
		if got != tt.want {
			t.Errorf("extractToolDomain(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestBuildAllowedDomains_CoreAlwaysIncluded(t *testing.T) {
	for _, kind := range []planpkg.TaskKind{
		planpkg.TaskResearch,
		planpkg.TaskTranslate,
		planpkg.TaskTransform,
		planpkg.TaskWrite,
		planpkg.TaskExecute,
		planpkg.TaskReview,
		planpkg.TaskDeliver,
	} {
		allowed := buildAllowedDomains(kind, nil)
		if !allowed[DomainFS] {
			t.Errorf("kind=%q: DomainFS not in core domains", kind)
		}
		if !allowed[DomainExec] {
			t.Errorf("kind=%q: DomainExec not in core domains", kind)
		}
	}
}

func TestBuildAllowedDomains_TaskKindActivation(t *testing.T) {
	tests := []struct {
		kind     planpkg.TaskKind
		expected []ToolDomain
		excluded []ToolDomain
	}{
		{
			planpkg.TaskResearch,
			[]ToolDomain{DomainNet, DomainText, DomainBrowser, DomainDesktop, DomainPDF, DomainDB, DomainCanvas},
			[]ToolDomain{DomainChannel, DomainCron, DomainArchive, DomainCrypto, DomainAgent},
		},
		{
			planpkg.TaskTranslate,
			[]ToolDomain{DomainText},
			[]ToolDomain{DomainBrowser, DomainChannel, DomainDB, DomainNet, DomainSheet},
		},
		{
			planpkg.TaskExecute,
			[]ToolDomain{DomainEnv, DomainNet, DomainProc, DomainDB, DomainCron, DomainWatch, DomainDesktop, DomainBrowser},
			[]ToolDomain{DomainChannel, DomainPDF, DomainCanvas},
		},
		{
			planpkg.TaskDeliver,
			[]ToolDomain{DomainChannel, DomainNet, DomainArchive, DomainPDF},
			[]ToolDomain{DomainBrowser, DomainDB, DomainCron, DomainCanvas},
		},
	}
	for _, tt := range tests {
		allowed := buildAllowedDomains(tt.kind, nil)
		for _, d := range tt.expected {
			if !allowed[d] {
				t.Errorf("kind=%q: expected domain %q to be allowed", tt.kind, d)
			}
		}
		for _, d := range tt.excluded {
			if allowed[d] {
				t.Errorf("kind=%q: expected domain %q to be excluded", tt.kind, d)
			}
		}
	}
}

func TestBuildAllowedDomains_RequiredCapabilitiesOverride(t *testing.T) {
	// TaskWrite normally doesn't include browser, but RequiredCapabilities
	// can activate it.
	allowed := buildAllowedDomains(planpkg.TaskWrite, []string{"browser"})
	if !allowed[DomainBrowser] {
		t.Error("RequiredCapabilities='browser' should activate DomainBrowser")
	}
	if !allowed[DomainText] {
		t.Error("TaskWrite should still include DomainText")
	}
	if !allowed[DomainFS] {
		t.Error("core DomainFS should still be included")
	}
}

func TestBuildAllowedDomains_CapabilityAliases(t *testing.T) {
	tests := []struct {
		capability string
		expected   ToolDomain
	}{
		{"database", DomainDB},
		{"db", DomainDB},
		{"network", DomainNet},
		{"net", DomainNet},
		{"http", DomainNet},
		{"filesystem", DomainFS},
		{"encryption", DomainCrypto},
		{"spreadsheet", DomainSheet},
		{"messaging", DomainChannel},
		{"scheduling", DomainCron},
		{"document", DomainDocument},
		{"docx", DomainDocument},
		{"presentation", DomainPresentation},
		{"pptx", DomainPresentation},
		{"calendar", DomainCalendar},
		{"caldav", DomainCalendar},
		{"desktop", DomainDesktop},
		{"window", DomainDesktop},
		{"clipboard", DomainDesktop},
	}
	for _, tt := range tests {
		allowed := buildAllowedDomains(planpkg.TaskWrite, []string{tt.capability})
		if !allowed[tt.expected] {
			t.Errorf("capability=%q should activate domain %q", tt.capability, tt.expected)
		}
	}
}

func TestBuildAllowedDomains_DirectDomainName(t *testing.T) {
	// Raw domain prefix accepted as RequiredCapability.
	allowed := buildAllowedDomains(planpkg.TaskWrite, []string{"canvas"})
	if !allowed[DomainCanvas] {
		t.Error("direct domain name 'canvas' should be accepted")
	}
}

func TestShouldSuppressExecForBrowserTasks(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "exec.run"},
		{Name: "browser.open"},
		{Name: "browser.click"},
		{Name: "browser.wait"},
	}
	if !shouldSuppressExec("打开 https://httpbin.org/forms/post，填写表单并提交", tools) {
		t.Fatal("expected browser task to suppress exec tools")
	}
}

func TestShouldSuppressExecForDesktopTasksWithActivatedDomain(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "exec.run"},
		{Name: "desktop.list_windows"},
		{Name: "desktop.screenshot"},
	}
	if !shouldSuppressExecWithDomains("列出当前打开的应用和窗口，告诉我哪个最像浏览器", tools, map[ToolDomain]bool{DomainDesktop: true}) {
		t.Fatal("expected activated desktop domain to suppress exec tools")
	}
}

func TestShouldNotSuppressExecWhenUserExplicitlyRequestsShell(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "exec.run"},
		{Name: "browser.open"},
	}
	if shouldSuppressExec("Use `curl` to open https://example.com", tools) {
		t.Fatal("expected explicit curl request to keep exec tools")
	}
}

func TestShouldNotSuppressExecWhenUserNamesShellCommandInAnotherLanguage(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "exec.run"},
		{Name: "browser.open"},
	}
	if shouldSuppressExec("Usa este comando:\ncurl https://example.com", tools) {
		t.Fatal("expected explicit shell command token to keep exec tools")
	}
}

func TestShouldSuppressExecWhenCommandMentionIsNaturalLanguageOnly(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "exec.run"},
		{Name: "browser.open"},
	}
	if !shouldSuppressExec("请用 curl 打开 https://example.com", tools) {
		t.Fatal("expected natural-language-only curl mention not to reserve exec tools")
	}
}

func TestPreferInteractiveBrowserToolsRemovesSideDomains(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "browser.open"},
		{Name: "browser.click"},
		{Name: "browser.wait"},
		{Name: "fs.write"},
		{Name: "text.html"},
		{Name: "skill.ensure"},
		{Name: "net.fetch"},
		{Name: "env.probe"},
		{Name: "db.kv.get"},
		{Name: "exec.run"},
	}
	filtered := preferInteractiveBrowserTools("打开 https://httpbin.org/forms/post，填写表单并提交", tools)
	names := toolNames(filtered)
	mustContain(t, names, "browser.open", "browser.click", "browser.wait")
	mustNotContain(t, names, "fs.write", "text.html", "skill.ensure", "net.fetch", "env.probe", "db.kv.get", "exec.run")
}

func TestPreferInteractiveBrowserToolsKeepsFSForBrowserToFileTask(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "browser.open"},
		{Name: "browser.snapshot"},
		{Name: "fs.write"},
		{Name: "fs.stat"},
		{Name: "fs.list"},
		{Name: "fs.find"},
		{Name: "fs.read"},
		{Name: "text.html"},
	}
	filtered := preferInteractiveBrowserTools("抓取 https://example.com 页面信息，写到 docs/tmp/example-brief.md", tools)
	names := toolNames(filtered)
	mustContain(t, names, "browser.open", "browser.snapshot", "fs.write", "fs.stat")
	mustNotContain(t, names, "fs.list", "fs.find", "fs.read", "text.html")
}

func TestPreferInteractiveBrowserToolsHidesBrowserEvalByDefault(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "browser.open"},
		{Name: "browser.snapshot"},
		{Name: "browser.eval"},
		{Name: "browser.click"},
	}
	filtered := preferInteractiveBrowserTools("打开 https://httpbin.org/forms/post，填写表单并提交", tools)
	names := toolNames(filtered)
	mustContain(t, names, "browser.open", "browser.snapshot", "browser.click")
	mustNotContain(t, names, "browser.eval")
}

func TestSelectToolsForRequestHidesBrowserEvalWithoutExplicitJSRequest(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "browser.click"},
		{Name: "browser.snapshot"},
		{Name: "browser.eval"},
		{Name: "exec.run"},
	}
	filtered := selectToolsForRequest(tools, "如果按钮点不到，就自己换 selector 再试，不要直接失败")
	names := toolNames(filtered)
	mustContain(t, names, "browser.click", "browser.snapshot")
	mustNotContain(t, names, "browser.eval")
}

func TestSelectToolsForRequestKeepsBrowserEvalForExplicitJSRequest(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "browser.snapshot"},
		{Name: "browser.eval"},
	}
	filtered := selectToolsForRequest(tools, "在当前页面执行 JavaScript，读取 localStorage 里的 token")
	names := toolNames(filtered)
	mustContain(t, names, "browser.snapshot", "browser.eval")
}

func TestBrowserEvalToolAllowedRequiresTechnicalMarker(t *testing.T) {
	if browserEvalToolAllowed("在当前页面执行脚本，然后把结果告诉我") {
		t.Fatal("natural-language-only browser script request should not enable browser.eval")
	}
	if !browserEvalToolAllowed("Run JavaScript in the current page and read localStorage.") {
		t.Fatal("technical JavaScript markers should enable browser.eval")
	}
}

func TestSelectToolsForRequestAppliesBrowserFilteringBelowBudget(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "browser.open"},
		{Name: "browser.click"},
		{Name: "browser.wait"},
		{Name: "browser.snapshot"},
		{Name: "fs.write"},
		{Name: "text.html"},
		{Name: "skill.ensure"},
		{Name: "net.fetch"},
		{Name: "env.probe"},
		{Name: "db.kv.get"},
		{Name: "exec.run"},
	}
	filtered := selectToolsForRequest(tools, "打开 https://httpbin.org/forms/post，填写表单并提交")
	names := toolNames(filtered)
	mustContain(t, names, "browser.open", "browser.click", "browser.wait", "browser.snapshot")
	mustNotContain(t, names, "fs.write", "text.html", "skill.ensure", "net.fetch", "env.probe", "db.kv.get", "exec.run")
}

func TestSelectToolsForRequestPrioritizesInteractiveBrowserToolsBeforeBudgetTrim(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "fs.read"},
		{Name: "fs.write"},
		{Name: "fs.list"},
		{Name: "fs.find"},
		{Name: "exec.run"},
		{Name: "exec.shell"},
		{Name: "text.json"},
		{Name: "text.yaml"},
		{Name: "text.csv"},
		{Name: "text.regex"},
		{Name: "net.fetch"},
		{Name: "net.download"},
		{Name: "net.dns"},
		{Name: "git.status"},
		{Name: "git.diff"},
		{Name: "crypto.hash"},
		{Name: "env.probe"},
		{Name: "db.query"},
		{Name: "search.query"},
		{Name: "web.fetch"},
		{Name: "news.digest"},
		{Name: "memory.search"},
		{Name: "skill.ensure"},
		{Name: "skill.list"},
		{Name: "browser.open"},
		{Name: "browser.close"},
		{Name: "browser.navigate"},
		{Name: "browser.click"},
		{Name: "browser.type"},
		{Name: "browser.screenshot"},
		{Name: "browser.screenshot_labeled"},
		{Name: "browser.snapshot_aria"},
		{Name: "browser.click_aria"},
		{Name: "browser.type_aria"},
		{Name: "browser.snapshot"},
		{Name: "browser.eval"},
		{Name: "browser.wait"},
		{Name: "browser.select"},
		{Name: "browser.fill"},
	}
	for i := 0; i < 40; i++ {
		tools = append(tools, ToolDefinition{Name: "text.extra_" + strings.TrimSpace(strings.ToLower(string(rune('a'+(i%26)))))})
	}

	filtered := selectToolsForRequest(tools, "打开 https://httpbin.org/forms/post，填写表单并提交")
	names := toolNames(filtered)
	mustContain(t, names, "browser.open", "browser.click", "browser.wait", "browser.snapshot", "browser.select", "browser.fill")
	mustNotContain(t, names, "skill.ensure", "fs.read", "fs.list", "net.fetch", "text.json", "exec.run", "browser.eval")
}

func TestBrowserRequestNeedsWorkspaceTools(t *testing.T) {
	if browserRequestNeedsWorkspaceTools("打开 https://httpbin.org/forms/post，填写表单并提交") {
		t.Fatal("pure browser form task should not expose workspace tools")
	}
	if !browserRequestNeedsWorkspaceTools("抓取页面信息，写到 docs/tmp/example-brief.md") {
		t.Fatal("browser-to-file task should expose workspace tools")
	}
	if browserRequestNeedsWorkspaceTools("Open https://example.com and explain the File menu on the page.") {
		t.Fatal("generic UI 'File menu' wording should not imply workspace file access")
	}
}

func TestBrowserRequestMayNeedExistingWorkspaceContent(t *testing.T) {
	if !browserRequestMayNeedExistingWorkspaceContent("Open https://example.com and merge docs/source.md into docs/summary.md.") {
		t.Fatal("multiple explicit workspace paths should allow existing workspace reads")
	}
	if browserRequestMayNeedExistingWorkspaceContent("Open https://example.com and update the existing report with the findings.") {
		t.Fatal("natural-language-only wording should not unlock workspace reads")
	}
	if browserRequestMayNeedExistingWorkspaceContent("Open https://example.com and write to docs/summary.md.") {
		t.Fatal("single destination path should not unlock existing workspace reads")
	}
}

func TestIsInteractiveBrowserRequest(t *testing.T) {
	if !isInteractiveBrowserRequest("打开 https://httpbin.org/forms/post，填写表单并提交") {
		t.Fatal("expected browser form task to be interactive")
	}
	if isInteractiveBrowserRequest("比较 README.md 和 README.zh-CN.md 的 browser 能力差异") {
		t.Fatal("expected file diff task not to be treated as interactive browser work")
	}
}

func TestIsInteractiveBrowserRequestRejectsArtifactURL(t *testing.T) {
	if isInteractiveBrowserRequest("Inspect https://example.com/feed.xml and summarize the latest items.") {
		t.Fatal("expected feed artifact URL not to be treated as interactive browser work")
	}
}

func TestLooksLikeSearchResultsExtractionRequestUsesStructuredSearchURL(t *testing.T) {
	if !looksLikeSearchResultsExtractionRequest("Review https://www.bing.com/search?q=openai and pull the first results.") {
		t.Fatal("expected explicit search URL to activate search-results handling")
	}
	if looksLikeSearchResultsExtractionRequest("Review https://example.com/articles/openai and summarize it.") {
		t.Fatal("expected ordinary article URL not to activate search-results handling")
	}
}

func TestMessageHasBrowserReferenceRequiresNavigablePageOrCanonicalContext(t *testing.T) {
	if !messageHasBrowserReference("Current page context | https://example.com/dashboard | title=Dashboard | session=sess-1") {
		t.Fatal("expected canonical current-page context to count as browser reference")
	}
	if !messageHasBrowserReference("Open www.example.com/products and inspect the page.") {
		t.Fatal("expected explicit page-like www URL to count as browser reference")
	}
	if !messageHasBrowserReference("打开页面后截图给我，再告诉我页面标题") {
		t.Fatal("expected natural-language page reference to count as browser reference")
	}
	if messageHasBrowserReference("Inspect https://example.com/feed.xml and summarize it.") {
		t.Fatal("expected non-page artifact URL not to count as browser reference")
	}
}

func TestFilterToolsForTask_NilTask(t *testing.T) {
	tools := sampleTools()
	filtered := filterToolsForTask(tools, nil)
	if len(filtered) != len(tools) {
		t.Errorf("nil task should return all tools: got %d, want %d", len(filtered), len(tools))
	}
}

func TestFilterToolsForTask_EmptyKind(t *testing.T) {
	tools := sampleTools()
	task := &planpkg.Task{Kind: ""}
	filtered := filterToolsForTask(tools, task)
	if len(filtered) != len(tools) {
		t.Errorf("empty Kind should return all tools: got %d, want %d", len(filtered), len(tools))
	}
}

func TestFilterToolsForTask_ResearchTask(t *testing.T) {
	tools := sampleTools()
	task := &planpkg.Task{Kind: planpkg.TaskResearch}
	filtered := filterToolsForTask(tools, task)

	names := toolNames(filtered)
	// Should include: core (fs, exec) + research domains (net, text, browser, pdf, db, canvas)
	mustContain(t, names, "fs.read", "fs.write", "exec.run", "browser.click",
		"browser.navigate", "text.json", "db.query", "net.http", "pdf.parse",
		"canvas.draw", "desktop.list_apps", "desktop.screenshot", "custom-skill")
	// Should NOT include: channel, cron, archive, crypto, agent, gateway, nodes, skill
	mustNotContain(t, names, "channel.send", "cron.schedule", "archive.zip",
		"crypto.hash", "agent.status", "gateway.health", "nodes.list", "skill.list")
}

func TestFilterToolsForTask_TranslateTask(t *testing.T) {
	tools := sampleTools()
	task := &planpkg.Task{Kind: planpkg.TaskTranslate}
	filtered := filterToolsForTask(tools, task)

	names := toolNames(filtered)
	// Core + text only
	mustContain(t, names, "fs.read", "exec.run", "text.json", "custom-skill")
	mustNotContain(t, names, "browser.click", "db.query", "channel.send", "net.http")
}

func TestFilterToolsForTask_ExecuteWithBrowserCapability(t *testing.T) {
	tools := sampleTools()
	task := &planpkg.Task{
		Kind:                 planpkg.TaskExecute,
		RequiredCapabilities: []string{"browser"},
	}
	filtered := filterToolsForTask(tools, task)

	names := toolNames(filtered)
	// Execute domains + browser from RequiredCapabilities
	mustContain(t, names, "fs.read", "exec.run", "net.http", "db.query",
		"browser.click", "browser.navigate", "desktop.list_apps", "desktop.screenshot")
	// text.json is now included for execute tasks; channel is still excluded
	mustContain(t, names, "text.json")
	mustNotContain(t, names, "channel.send")
}

func TestFilterToolsForTask_ExternalSkillsAlwaysIncluded(t *testing.T) {
	tools := sampleTools()
	// Even the most restrictive task should include tools without dot prefix.
	task := &planpkg.Task{Kind: planpkg.TaskTranslate}
	filtered := filterToolsForTask(tools, task)

	names := toolNames(filtered)
	mustContain(t, names, "custom-skill", "another-external")
}

func TestFilterToolsForTask_DeliverTask(t *testing.T) {
	tools := sampleTools()
	task := &planpkg.Task{Kind: planpkg.TaskDeliver}
	filtered := filterToolsForTask(tools, task)

	names := toolNames(filtered)
	mustContain(t, names, "fs.read", "exec.run", "channel.send", "net.http",
		"archive.zip", "pdf.parse")
	mustNotContain(t, names, "browser.click", "db.query", "cron.schedule", "canvas.draw")
}

func TestFilterToolsForTask_ReducesToolCount(t *testing.T) {
	tools := sampleTools()
	task := &planpkg.Task{Kind: planpkg.TaskTranslate}
	filtered := filterToolsForTask(tools, task)

	if len(filtered) >= len(tools) {
		t.Errorf("translate task should reduce tools: got %d, have %d total", len(filtered), len(tools))
	}
	// Translate: core (fs, exec) + text + external = much less than all 20+ tools
	if len(filtered) > 10 {
		t.Errorf("translate task should have at most ~10 tools, got %d", len(filtered))
	}
}

func TestNormalizeToolDefinitionAddsDomainAndCategory(t *testing.T) {
	def := normalizeToolDefinition(ToolDefinition{Name: "spreadsheet.read_range", Description: "Read sheet cells"})
	if def.Domain != string(DomainSheet) {
		t.Fatalf("Domain = %q, want %q", def.Domain, DomainSheet)
	}
	if def.Category != "spreadsheet" {
		t.Fatalf("Category = %q, want spreadsheet", def.Category)
	}
}

func TestDescribeToolsForPlanningIncludesCategory(t *testing.T) {
	got := describeToolsForPlanning([]ToolDefinition{
		{Name: "spreadsheet.read_range"},
		{Name: "document.create"},
	}, 10)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0] != "spreadsheet.read_range [spreadsheet]" {
		t.Fatalf("got[0] = %q", got[0])
	}
	if got[1] != "document.create [document]" {
		t.Fatalf("got[1] = %q", got[1])
	}
}

func TestBuildToolCatalogPromptGroupsTools(t *testing.T) {
	prompt := buildToolCatalogPrompt([]ToolDefinition{
		{Name: "spreadsheet.read_range"},
		{Name: "spreadsheet.write_range"},
		{Name: "document.create"},
	})
	if !strings.Contains(prompt, "spreadsheet") {
		t.Fatalf("prompt = %q, want spreadsheet group", prompt)
	}
	if !strings.Contains(prompt, "document") {
		t.Fatalf("prompt = %q, want document group", prompt)
	}
	if !strings.Contains(prompt, "Choose tools from the matching group first") {
		t.Fatalf("prompt = %q, missing tool selection guidance", prompt)
	}
}

func TestFallbackHeuristicDomainsFromConcreteEvidence(t *testing.T) {
	domains := fallbackHeuristicDomains("Please check https://example.com/report.xlsx and email summary to ops@example.com")
	if !domains[DomainBrowser] || !domains[DomainNet] {
		t.Fatalf("domains = %#v, want browser and net", domains)
	}
	if !domains[DomainSheet] {
		t.Fatalf("domains = %#v, want spreadsheet", domains)
	}
	if !domains[DomainEmail] {
		t.Fatalf("domains = %#v, want email", domains)
	}
}

func TestDetectStructuredEvidenceFromMachineFormatOnly(t *testing.T) {
	domains := detectStructuredEvidence("Please check https://example.com/report.xlsx and email ops@example.com", false)
	if !domains[DomainBrowser] || !domains[DomainNet] {
		t.Fatalf("domains = %#v, want browser and net", domains)
	}
	if !domains[DomainSheet] {
		t.Fatalf("domains = %#v, want sheet", domains)
	}
	if !domains[DomainEmail] {
		t.Fatalf("domains = %#v, want email", domains)
	}
}

func TestFallbackHeuristicDomainsDoesNotInferEmailFromMailboxIntentOnly(t *testing.T) {
	domains := fallbackHeuristicDomains("搜索我邮箱里最近关于 invoice 的邮件")
	if domains[DomainEmail] {
		t.Fatalf("domains = %#v, mailbox intent alone should not activate email", domains)
	}
}

func TestFallbackHeuristicDomainsActivatesNewsFromRSSEvidence(t *testing.T) {
	domains := fallbackHeuristicDomains("读取这个 RSS feed 并总结最近三条")
	if !domains[DomainNews] {
		t.Fatalf("domains = %#v, want news", domains)
	}
}

func TestFallbackHeuristicDomainsDoesNotTreatURLAsWorkspacePath(t *testing.T) {
	domains := fallbackHeuristicDomains("打开 https://httpbin.org/forms/post，填写表单并提交")
	if domains[DomainFS] {
		t.Fatalf("domains = %#v, url-only browser task should not activate fs", domains)
	}
}

func TestDetectStructuredEvidenceBrowserContextDoesNotTreatPagePathAsWorkspacePath(t *testing.T) {
	domains := detectStructuredEvidence("forms/post", true)
	if domains[DomainFS] {
		t.Fatalf("domains = %#v, browser page path should not activate fs", domains)
	}
}

func TestDetectStructuredEvidenceBrowserContextKeepsKnownWorkspaceRootPath(t *testing.T) {
	domains := detectStructuredEvidence("docs/output", true)
	if !domains[DomainFS] {
		t.Fatalf("domains = %#v, known workspace root path should still activate fs", domains)
	}
}

func TestFallbackHeuristicDomainsActivatesDesktopForObviousDesktopRequest(t *testing.T) {
	domains := fallbackHeuristicDomains("List the currently running desktop applications and take a screenshot of the frontmost window.")
	if !domains[DomainDesktop] {
		t.Fatalf("domains = %#v, want desktop for an explicit desktop request", domains)
	}
}

func TestFallbackHeuristicDomainsTreatsPageScreenshotAsBrowserTask(t *testing.T) {
	domains := fallbackHeuristicDomains("打开页面后截图给我，再告诉我页面标题")
	if !domains[DomainBrowser] {
		t.Fatalf("domains = %#v, want browser for page screenshot request", domains)
	}
	if domains[DomainDesktop] {
		t.Fatalf("domains = %#v, page screenshot should not activate desktop domain", domains)
	}
}

func TestFallbackHeuristicDomainsActivatesDesktopForClipboardRequest(t *testing.T) {
	domains := fallbackHeuristicDomains("读一下当前剪贴板，然后帮我整理成 3 条")
	if !domains[DomainDesktop] {
		t.Fatalf("domains = %#v, want desktop for clipboard request", domains)
	}
}

func TestFallbackHeuristicDomainsKeepsBrowserAndFileEvidenceForPageWriteTask(t *testing.T) {
	domains := fallbackHeuristicDomains("抓取页面信息，写到 docs/tmp/example-brief.md")
	if !domains[DomainBrowser] {
		t.Fatalf("domains = %#v, want browser for natural-language page request", domains)
	}
	if !domains[DomainFS] {
		t.Fatalf("domains = %#v, want fs from file path evidence", domains)
	}
}

func TestFallbackHeuristicDomainsDoesNotTreatFormEmailFieldAsDeliveryIntent(t *testing.T) {
	domains := fallbackHeuristicDomains("Using browser tools, open https://httpbin.org/forms/post, fill input[name=email] with qa@example.com, submit the form.")
	if domains[DomainEmail] {
		t.Fatalf("domains = %#v, should not activate email for a browser form email field", domains)
	}
	if domains[DomainChannel] {
		t.Fatalf("domains = %#v, should not activate channel for a browser form email field", domains)
	}
}

func TestFallbackHeuristicDomainsTreatsBrowserRouteFragmentAsBrowserContext(t *testing.T) {
	domains := fallbackHeuristicDomains("页面 URL 里有 /post，但这只是一个网页表单")
	if !domains[DomainBrowser] {
		t.Fatalf("domains = %#v, want browser for natural-language webpage reference", domains)
	}
	if domains[DomainFS] {
		t.Fatalf("domains = %#v, route fragment /post should not activate fs", domains)
	}
}

func TestPrepareToolsForModelWithSuggestedDomains(t *testing.T) {
	tools := sampleTools()
	got := prepareToolsForModelWithDomains(tools, "do the thing", map[ToolDomain]bool{
		DomainBrowser: true,
		DomainSheet:   true,
	})
	names := toolNames(got)
	mustContain(t, names, "browser.click", "browser.navigate", "spreadsheet.read_range")
}

func TestPrepareRunToolsUsesTaskContractDomainsForSpanishBrowserWriteTask(t *testing.T) {
	component := &AgentComponent{}
	tools := []ToolDefinition{
		{Name: "exec.run"},
		{Name: "browser.open"},
		{Name: "browser.snapshot"},
		{Name: "fs.write"},
		{Name: "document.create"},
		{Name: "text.json"},
		{Name: "net.fetch"},
	}
	run := &Run{
		TaskContract: &TaskContract{
			JobType:          taskContractJobReport,
			SuggestedDomains: []string{"browser", "fs", "document"},
			ExpectedDeliverables: []TaskContractDeliverable{
				{Kind: taskDeliverableBrowserEvidence},
				{Kind: taskDeliverableDocument},
			},
		},
	}

	got := component.prepareRunTools(run, tools, "Abre https://example.com y guarda un resumen en docs/tmp/resumen.md")
	names := toolNames(got)
	mustContain(t, names, "browser.open", "browser.snapshot", "fs.write", "document.create")
	mustNotContain(t, names, "exec.run", "text.json", "net.fetch")
}

func TestPrepareRunToolsSuppressesExecForJapaneseDesktopRequestUsingSemanticDomains(t *testing.T) {
	component := &AgentComponent{}
	tools := []ToolDefinition{
		{Name: "exec.run"},
		{Name: "desktop.list_apps"},
		{Name: "desktop.screenshot"},
	}
	run := &Run{
		TaskContract: &TaskContract{
			SuggestedDomains: []string{"desktop"},
		},
	}

	got := component.prepareRunTools(run, tools, "現在開いているアプリを一覧し、前面ウィンドウのスクリーンショットを見せてください。")
	names := toolNames(got)
	mustContain(t, names, "desktop.list_apps", "desktop.screenshot")
	mustNotContain(t, names, "exec.run")
}

func TestPrepareRunToolsUsesSessionBrowserContextForSelectorFollowUp(t *testing.T) {
	component := &AgentComponent{}
	tools := []ToolDefinition{
		{Name: "exec.run"},
		{Name: "net.fetch"},
		{Name: "browser.click"},
		{Name: "browser.wait"},
		{Name: "browser.snapshot"},
		{Name: "browser.eval"},
	}

	got := component.prepareRunToolsWithSessionContext(
		&Run{
			Preflight: &RunPreflightReport{
				DetectedDomains: []string{"browser"},
			},
		},
		tools,
		"如果按钮点不到，就自己换 selector 再试，不要直接失败",
		"Current page context | https://httpbin.org/forms/post | title=httpbin.org | session=sess-browser",
	)
	names := toolNames(got)
	mustContain(t, names, "browser.click", "browser.wait", "browser.snapshot")
	mustNotContain(t, names, "exec.run", "net.fetch", "browser.eval")
}

func TestPrepareRunToolsUsesPreflightDetectedDomainsForModelActivation(t *testing.T) {
	component := &AgentComponent{}
	tools := append([]ToolDefinition{
		{Name: "exec.run"},
		{Name: "fs.read"},
		{Name: "skill.ensure"},
		{Name: "desktop.list_apps"},
		{Name: "email.send"},
		{Name: "calendar.list_events"},
	}, manyTextTools(70)...)
	run := &Run{
		Preflight: &RunPreflightReport{
			DetectedDomains: []string{"desktop", "email"},
		},
	}

	got := component.prepareRunTools(run, tools, "Please continue with the prepared task.")
	names := toolNames(got)
	mustContain(t, names, "desktop.list_apps", "email.send")
	mustNotContain(t, names, "calendar.list_events")
}

func TestPrepareRunToolsUsesPreflightDetectedDomainsForJapaneseEmailRequest(t *testing.T) {
	component := &AgentComponent{}
	tools := append([]ToolDefinition{
		{Name: "exec.run"},
		{Name: "fs.read"},
		{Name: "skill.ensure"},
		{Name: "email.send"},
		{Name: "calendar.list_events"},
	}, manyTextTools(70)...)
	run := &Run{
		Preflight: &RunPreflightReport{
			DetectedDomains: []string{"email"},
		},
	}

	got := component.prepareRunTools(run, tools, "メールを送って")
	names := toolNames(got)
	mustContain(t, names, "email.send")
	mustNotContain(t, names, "calendar.list_events")
}

func TestPreferInteractiveBrowserToolsHidesHeavyElementToolsForSearchResults(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "exec.run"},
		{Name: "browser.wait"},
		{Name: "browser.snapshot"},
		{Name: "browser.screenshot"},
		{Name: "browser.screenshot_labeled"},
		{Name: "browser.snapshot_aria"},
		{Name: "browser.element_text"},
		{Name: "browser.element_attr"},
		{Name: "browser.eval"},
	}

	filtered := preferInteractiveBrowserTools("打开 https://www.bing.com/search?q=openai，等搜索结果加载出来，再提取前 5 条", tools)
	names := toolNames(filtered)
	mustContain(t, names, "browser.wait", "browser.snapshot", "browser.screenshot", "browser.snapshot_aria")
	mustNotContain(t, names, "exec.run", "browser.screenshot_labeled", "browser.element_text", "browser.element_attr", "browser.eval")
}

func TestPreferInteractiveBrowserToolsDoesNotFocusArtifactURL(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "exec.run"},
		{Name: "browser.open"},
		{Name: "browser.snapshot"},
		{Name: "net.fetch"},
	}

	filtered := preferInteractiveBrowserTools("Inspect https://example.com/feed.xml and summarize the latest items.", tools)
	if len(filtered) != 0 {
		t.Fatalf("preferInteractiveBrowserTools() = %#v, want no browser focus for artifact URL", filtered)
	}
}

func TestPrepareRunToolsUsesLightweightBrowserToolsForSearchResultsFollowUp(t *testing.T) {
	component := &AgentComponent{}
	tools := []ToolDefinition{
		{Name: "exec.run"},
		{Name: "browser.wait"},
		{Name: "browser.snapshot"},
		{Name: "browser.screenshot"},
		{Name: "browser.screenshot_labeled"},
		{Name: "browser.snapshot_aria"},
		{Name: "browser.element_text"},
		{Name: "browser.element_attr"},
	}

	got := component.prepareRunToolsWithSessionContext(
		&Run{
			Preflight: &RunPreflightReport{
				DetectedDomains: []string{"browser"},
			},
		},
		tools,
		"打开页面，等搜索结果加载出来，再提取前 5 条",
		"Current page context | https://www.bing.com/search?q=openai | title=openai - Search | session=sess-search",
	)
	names := toolNames(got)
	mustContain(t, names, "browser.wait", "browser.snapshot", "browser.screenshot", "browser.snapshot_aria")
	mustNotContain(t, names, "exec.run", "browser.screenshot_labeled", "browser.element_text", "browser.element_attr")
}

func TestDiscoverDomainsByToolCatalogActivatesTier3DomainFromTechnicalMarker(t *testing.T) {
	domains := DiscoverDomainsByToolCatalog("Need PPTX output with speaker notes.", []ToolDefinition{
		{Name: "presentation.create", Description: "Create PPTX slide output with speaker notes."},
		{Name: "calendar.list_events", Description: "List ICS calendar events."},
	})
	if !domains[DomainPresentation] {
		t.Fatalf("domains = %#v, want presentation", domains)
	}
	if domains[DomainCalendar] {
		t.Fatalf("domains = %#v, did not want calendar", domains)
	}
}

func TestDiscoverDomainsByToolCatalogDoesNotUseNaturalLanguageDescriptionMatch(t *testing.T) {
	domains := DiscoverDomainsByToolCatalog("Need a leadership deck", []ToolDefinition{
		{Name: "presentation.create", Description: "Create a leadership slide deck with speaker notes."},
	})
	if domains[DomainPresentation] {
		t.Fatalf("domains = %#v, natural-language description match should not activate presentation", domains)
	}
}

func TestResolveActivatedDomainsDoesNotBackfillKeywordFallbackWhenCatalogMisses(t *testing.T) {
	domains := resolveActivatedDomainsForToolSelection([]ToolDefinition{
		{Name: "desktop.screenshot", Description: "Take a screenshot of the desktop."},
	}, "给当前桌面截个图", toolSelectionSignals{})
	if domains[DomainDesktop] {
		t.Fatalf("domains = %#v, did not want keyword-only desktop activation", domains)
	}
}

func TestResolveActivatedDomainsKeepsStructuredEvidenceActivation(t *testing.T) {
	domains := resolveActivatedDomainsForToolSelection([]ToolDefinition{
		{Name: "pdf.parse", Description: "Parse PDF documents."},
	}, "Review /tmp/report.pdf in Terminal and summarize it.", toolSelectionSignals{})
	if !domains[DomainPDF] {
		t.Fatalf("domains = %#v, want pdf activation from structured evidence", domains)
	}
	if domains[DomainDesktop] {
		t.Fatalf("domains = %#v, did not want unrelated desktop activation", domains)
	}
}

func TestResolveActivatedDomainsKeepsCatalogActivationWhenStructuredEvidenceMissing(t *testing.T) {
	domains := resolveActivatedDomainsForToolSelection([]ToolDefinition{
		{Name: "presentation.create", Description: "Create PPTX slide output with speaker notes."},
	}, "Need PPTX output in Terminal.", toolSelectionSignals{})
	if !domains[DomainPresentation] {
		t.Fatalf("domains = %#v, want presentation activation from catalog search", domains)
	}
	if domains[DomainDesktop] {
		t.Fatalf("domains = %#v, did not want unrelated desktop activation", domains)
	}
}

func TestSelectToolsForRequestReturnsDeterministicOrder(t *testing.T) {
	toolsA := []ToolDefinition{
		{Name: "browser.click"},
		{Name: "exec.run"},
		{Name: "browser.open"},
		{Name: "browser.wait"},
		{Name: "fs.write"},
		{Name: "fs.read"},
	}
	toolsB := []ToolDefinition{
		{Name: "fs.read"},
		{Name: "browser.wait"},
		{Name: "browser.open"},
		{Name: "fs.write"},
		{Name: "exec.run"},
		{Name: "browser.click"},
	}

	gotA := orderedToolNames(selectToolsForRequest(toolsA, "打开 https://example.com，点击按钮，再把结果写到 docs/out.md"))
	gotB := orderedToolNames(selectToolsForRequest(toolsB, "打开 https://example.com，点击按钮，再把结果写到 docs/out.md"))

	if strings.Join(gotA, ",") != strings.Join(gotB, ",") {
		t.Fatalf("deterministic ordering mismatch:\na=%v\nb=%v", gotA, gotB)
	}
}

func TestPreferInteractiveBrowserToolsReturnsDeterministicOrder(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "browser.wait"},
		{Name: "browser.open"},
		{Name: "fs.write"},
		{Name: "browser.click"},
		{Name: "fs.stat"},
	}

	got := orderedToolNames(preferInteractiveBrowserTools("打开 https://example.com，点击按钮并保存结果到 docs/out.md", tools))
	want := []string{"browser.click", "browser.open", "browser.wait", "fs.stat", "fs.write"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("preferInteractiveBrowserTools() = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sampleTools() []ToolDefinition {
	return []ToolDefinition{
		{Name: "fs.read"},
		{Name: "fs.write"},
		{Name: "fs.find"},
		{Name: "exec.run"},
		{Name: "exec.shell"},
		{Name: "browser.click"},
		{Name: "browser.navigate"},
		{Name: "desktop.list_apps"},
		{Name: "desktop.screenshot"},
		{Name: "text.json"},
		{Name: "text.yaml"},
		{Name: "spreadsheet.read_range"},
		{Name: "db.query"},
		{Name: "db.execute"},
		{Name: "net.http"},
		{Name: "channel.send"},
		{Name: "channel.list"},
		{Name: "cron.schedule"},
		{Name: "watch.list"},
		{Name: "archive.zip"},
		{Name: "crypto.hash"},
		{Name: "pdf.parse"},
		{Name: "canvas.draw"},
		{Name: "agent.status"},
		{Name: "gateway.health"},
		{Name: "nodes.list"},
		{Name: "skill.list"},
		{Name: "env.probe"},
		{Name: "proc.list"},
		{Name: "custom-skill"},
		{Name: "another-external"},
	}
}

func manyTextTools(count int) []ToolDefinition {
	out := make([]ToolDefinition, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, ToolDefinition{Name: "text.extra_" + strings.TrimSpace(strings.ToLower(string(rune('a'+(i%26))))) + "_" + strings.TrimSpace(strings.ToLower(string(rune('a'+((i/26)%26)))))})
	}
	return out
}

func toolNames(tools []ToolDefinition) map[string]bool {
	m := make(map[string]bool, len(tools))
	for _, t := range tools {
		m[t.Name] = true
	}
	return m
}

func orderedToolNames(tools []ToolDefinition) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func mustContain(t *testing.T, names map[string]bool, expected ...string) {
	t.Helper()
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected tool %q to be present, but it was filtered out", name)
		}
	}
}

func mustNotContain(t *testing.T, names map[string]bool, excluded ...string) {
	t.Helper()
	for _, name := range excluded {
		if names[name] {
			t.Errorf("expected tool %q to be filtered out, but it was present", name)
		}
	}
}

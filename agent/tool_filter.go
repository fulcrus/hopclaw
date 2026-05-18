package agent

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	planpkg "github.com/fulcrus/hopclaw/planner"
)

func truncateLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ---------------------------------------------------------------------------
// Tool Domain Classification
// ---------------------------------------------------------------------------

// ToolDomain represents a tool category derived from the dot-prefix of a tool
// name. For example, "fs.read" belongs to DomainFS, "browser.click" belongs
// to DomainBrowser.
type ToolDomain string

const (
	DomainFS           ToolDomain = "fs"
	DomainExec         ToolDomain = "exec"
	DomainEnv          ToolDomain = "env"
	DomainNet          ToolDomain = "net"
	DomainText         ToolDomain = "text"
	DomainArchive      ToolDomain = "archive"
	DomainCrypto       ToolDomain = "crypto"
	DomainDB           ToolDomain = "db"
	DomainSheet        ToolDomain = "spreadsheet"
	DomainProc         ToolDomain = "proc"
	DomainChannel      ToolDomain = "channel"
	DomainCron         ToolDomain = "cron"
	DomainWatch        ToolDomain = "watch"
	DomainAgent        ToolDomain = "agent"
	DomainBrowser      ToolDomain = "browser"
	DomainDesktop      ToolDomain = "desktop"
	DomainPDF          ToolDomain = "pdf"
	DomainCanvas       ToolDomain = "canvas"
	DomainGateway      ToolDomain = "gateway"
	DomainNodes        ToolDomain = "nodes"
	DomainSkill        ToolDomain = "skill"
	DomainGit          ToolDomain = "git"
	DomainSearch       ToolDomain = "search"
	DomainSpeech       ToolDomain = "speech"
	DomainMedia        ToolDomain = "media"
	DomainVision       ToolDomain = "vision"
	DomainMemory       ToolDomain = "memory"
	DomainSession      ToolDomain = "session"
	DomainEmail        ToolDomain = "email"
	DomainWeb          ToolDomain = "web"
	DomainNews         ToolDomain = "news"
	DomainDocument     ToolDomain = "document"
	DomainPresentation ToolDomain = "presentation"
	DomainCalendar     ToolDomain = "calendar"
)

// ---------------------------------------------------------------------------
// Core tools — always included in every request (essential subset per domain)
// ---------------------------------------------------------------------------

// essentialTools lists the tool names that are always included regardless of
// context. These are the minimal set every agent invocation needs.
var essentialTools = map[string]bool{
	// Filesystem (most common operations)
	"fs.read":  true,
	"fs.write": true,
	"fs.list":  true,
	"fs.find":  true,
	"fs.grep":  true,
	"fs.edit":  true,
	"fs.stat":  true,
	"fs.tree":  true,

	// Execution
	"exec.run":   true,
	"exec.shell": true,

	// Skill management (fallback mechanism)
	"skill.ensure": true,
	"skill.list":   true,
}

// ---------------------------------------------------------------------------
// Domain budget — max tools included per domain when the domain is active
// ---------------------------------------------------------------------------

// domainBudget caps how many tools from each domain can be sent. Domains
// with many tools (browser: 56) are trimmed to their most useful subset.
var domainBudget = map[ToolDomain]int{
	DomainFS:           12, // comprehensive
	DomainExec:         4,
	DomainText:         10,
	DomainNet:          6,
	DomainGit:          8,
	DomainCrypto:       4,
	DomainArchive:      5,
	DomainDB:           4,
	DomainSheet:        8, // expanded for XLSX tools
	DomainEnv:          3,
	DomainProc:         2,
	DomainChannel:      4,
	DomainCron:         3,
	DomainWatch:        3,
	DomainAgent:        3,
	DomainBrowser:      12, // trimmed from 56
	DomainDesktop:      12,
	DomainPDF:          6, // expanded for merge/watermark/create
	DomainCanvas:       4,
	DomainGateway:      2,
	DomainNodes:        4,
	DomainSkill:        4,
	DomainSearch:       2,
	DomainSpeech:       2,
	DomainMedia:        6,
	DomainVision:       4,
	DomainMemory:       4,
	DomainSession:      2,
	DomainEmail:        3, // expanded for attachment tools
	DomainWeb:          1,
	DomainNews:         1,
	DomainDocument:     4,
	DomainPresentation: 3,
	DomainCalendar:     4,
}

// defaultDomainBudget is the budget for domains not in the map.
const defaultDomainBudget = 3

// ---------------------------------------------------------------------------
// Domain tiers — which domains to include by default vs on-demand
// ---------------------------------------------------------------------------

// Tier 1: always included (core operations)
// Tier 2: included by default (commonly needed)
// Tier 3: only included when user message or task hints at them

var domainTier = map[ToolDomain]int{
	// Tier 1 — always
	DomainFS:   1,
	DomainExec: 1,

	// Tier 2 — default on
	DomainText:   2,
	DomainNet:    2,
	DomainGit:    2,
	DomainCrypto: 2,
	DomainEnv:    2,
	DomainDB:     2,
	DomainSkill:  2,
	DomainSearch: 2,
	DomainWeb:    2,
	DomainNews:   2,
	DomainMemory: 2,

	// Archive moved to Tier 3 (activated by keywords like "zip", "compress")
	DomainArchive: 3,
	DomainSheet:   3,

	// Tier 2 — channel always available when configured
	DomainChannel: 2,

	// Tier 3 — on demand
	DomainBrowser:      3,
	DomainDesktop:      3,
	DomainPDF:          3,
	DomainCanvas:       3,
	DomainGateway:      3,
	DomainNodes:        3,
	DomainAgent:        3,
	DomainCron:         3,
	DomainWatch:        3,
	DomainProc:         3,
	DomainSpeech:       3,
	DomainMedia:        3,
	DomainVision:       3,
	DomainSession:      3,
	DomainEmail:        3,
	DomainDocument:     3,
	DomainPresentation: 3,
	DomainCalendar:     3,
}

// ---------------------------------------------------------------------------
// Main entry: selectToolsForRequest
// ---------------------------------------------------------------------------

// maxToolsPerRequest is the hard ceiling on tool definitions per model call.
const maxToolsPerRequest = 64

type toolSelectionSignals struct {
	activatedDomains            map[ToolDomain]bool
	browserFocusDomains         map[ToolDomain]bool
	contract                    *TaskContract
	reuseSessionBrowserContext  bool
	browserSearchResultsContext bool
}

var browserFocusCompanionDomains = map[ToolDomain]bool{
	DomainBrowser:      true,
	DomainFS:           true,
	DomainPDF:          true,
	DomainDocument:     true,
	DomainPresentation: true,
	DomainSheet:        true,
	DomainMedia:        true,
	DomainVision:       true,
}

var browserFocusCompatibleDomains = map[ToolDomain]bool{
	DomainBrowser:      true,
	DomainFS:           true,
	DomainText:         true,
	DomainNet:          true,
	DomainSearch:       true,
	DomainWeb:          true,
	DomainPDF:          true,
	DomainDocument:     true,
	DomainPresentation: true,
	DomainSheet:        true,
	DomainMedia:        true,
	DomainVision:       true,
}

// selectToolsForRequest picks the most relevant tools based on user message
// content. The algorithm:
//
//  1. Always include essential tools (fs core, exec, skill.ensure).
//  2. Include all Tier 1-2 domains (up to their budget).
//  3. Activate Tier 3 domains in priority order:
//     structured evidence -> upstream detected domains -> tool catalog search.
//  4. If the browser is clearly the primary interaction surface, focus the
//     tool pool around browser + closely related companion domains.
//  5. If still over budget, trim lowest-tier domains first.
//  6. Tools not selected are still reachable via skill.ensure fallback.
func selectToolsForRequest(tools []ToolDefinition, userMessage string) []ToolDefinition {
	return selectToolsForRequestWithDomains(tools, userMessage, nil)
}

func selectToolsForRequestWithDomains(tools []ToolDefinition, userMessage string, activatedDomains map[ToolDomain]bool) []ToolDefinition {
	signals := inferToolSelectionSignals(userMessage, activatedDomains, nil)
	return selectToolsForRequestWithSignals(tools, userMessage, signals)
}

func selectToolsForRequestWithSignals(tools []ToolDefinition, userMessage string, signals toolSelectionSignals) []ToolDefinition {
	tools = canonicalToolDefinitions(tools)
	signals.activatedDomains = resolveActivatedDomainsForToolSelection(tools, userMessage, signals)
	if len(signals.browserFocusDomains) == 0 {
		signals.browserFocusDomains = inferBrowserFocusDomains(userMessage, signals.activatedDomains, signals.contract)
	}

	if focused := focusInteractiveBrowserToolPoolWithSignals(tools, userMessage, signals); len(focused) > 0 {
		return finalizeSelectedTools(focused, userMessage, len(tools), signals)
	}

	if len(tools) <= maxToolsPerRequest {
		result := append([]ToolDefinition(nil), tools...)
		return finalizeSelectedTools(result, userMessage, len(tools), signals)
	}

	activatedDomains := signals.activatedDomains

	// Group tools by domain.
	type domainGroup struct {
		domain ToolDomain
		tier   int
		tools  []ToolDefinition
	}
	groups := make(map[ToolDomain]*domainGroup)
	var essentials []ToolDefinition

	for _, tool := range tools {
		if essentialTools[tool.Name] {
			essentials = append(essentials, tool)
			continue
		}
		domain := extractToolDomain(tool.Name)
		g, ok := groups[domain]
		if !ok {
			tier := domainTier[domain]
			if tier == 0 {
				tier = 3 // unknown domain → Tier 3
			}
			g = &domainGroup{domain: domain, tier: tier}
			groups[domain] = g
		}
		g.tools = append(g.tools, tool)
	}

	// Build result: essentials first.
	result := make([]ToolDefinition, 0, maxToolsPerRequest)
	result = append(result, essentials...)
	remaining := maxToolsPerRequest - len(result)

	orderedGroups := make([]*domainGroup, 0, len(groups))
	for _, g := range groups {
		orderedGroups = append(orderedGroups, g)
	}
	sort.Slice(orderedGroups, func(i, j int) bool {
		if orderedGroups[i].tier != orderedGroups[j].tier {
			return orderedGroups[i].tier < orderedGroups[j].tier
		}
		return orderedGroups[i].domain < orderedGroups[j].domain
	})

	// Tier 1 + 2: always included.
	for _, g := range orderedGroups {
		if g.tier > 2 {
			continue
		}
		budget := domainBudgetFor(g.domain)
		added := appendUpTo(&result, g.tools, budget, remaining)
		remaining -= added
		if remaining <= 0 {
			break
		}
	}

	// Tier 3: only if activated by keywords.
	if remaining > 0 {
		for _, g := range orderedGroups {
			if g.tier != 3 {
				continue
			}
			if !activatedDomains[g.domain] {
				continue
			}
			budget := domainBudgetFor(g.domain)
			added := appendUpTo(&result, g.tools, budget, remaining)
			remaining -= added
			if remaining <= 0 {
				break
			}
		}
	}

	return finalizeSelectedTools(result, userMessage, len(tools), signals)
}

func finalizeSelectedTools(result []ToolDefinition, userMessage string, totalIn int, signals toolSelectionSignals) []ToolDefinition {
	// Apply enhanced descriptions to make built-in tools more discoverable.
	applyEnhancedDescriptions(result)
	if focused := preferInteractiveBrowserToolsWithDomains(userMessage, result, signals); len(focused) > 0 {
		result = focused
	}
	if searchResultsExtractionContext(userMessage, signals) {
		result = removeTools(result, "browser.element_text", "browser.element_attr", "browser.screenshot_labeled")
	}
	if !browserEvalToolAllowed(userMessage) {
		result = removeTools(result, "browser.eval")
	}

	// When the request clearly targets a built-in tool or an interactive
	// browser/desktop workflow, remove exec.* so the model stays on the
	// structured tool path instead of shelling out.
	suppress := shouldSuppressExecWithDomains(userMessage, result, signals.activatedDomains)
	if suppress {
		result = removeTools(result, "exec.run", "exec.shell", "exec.script")
	}
	log.Info("selectToolsForRequest",
		"user_message", truncateLog(userMessage, 80),
		"total_in", totalIn,
		"total_out", len(result),
		"exec_suppressed", suppress,
	)

	return result
}

func inferToolSelectionSignals(message string, activated map[ToolDomain]bool, contract *TaskContract) toolSelectionSignals {
	return toolSelectionSignals{
		activatedDomains: cloneToolDomainSet(activated),
		contract:         contract,
	}
}

func resolveActivatedDomainsForToolSelection(tools []ToolDefinition, message string, signals toolSelectionSignals) map[ToolDomain]bool {
	activated := cloneToolDomainSet(detectStructuredEvidence(message))
	if activated == nil {
		activated = make(map[ToolDomain]bool)
	}

	upstream := cloneToolDomainSet(signals.activatedDomains)
	if len(upstream) > 0 {
		for domain, enabled := range upstream {
			if enabled {
				activated[domain] = true
			}
		}
	} else {
		catalogMatches := DiscoverDomainsByToolCatalog(message, tools)
		for domain, enabled := range catalogMatches {
			if enabled {
				activated[domain] = true
			}
		}
	}
	return sanitizeDetectedDomains(message, activated)
}

func inferBrowserFocusDomains(message string, activated map[ToolDomain]bool, contract *TaskContract) map[ToolDomain]bool {
	if !hasActiveDomain(activated, DomainBrowser) {
		return nil
	}
	if contract != nil {
		if !taskContractPrefersBrowserFocus(contract, message, activated) {
			return nil
		}
		return buildBrowserFocusDomains(message, activated, true)
	}
	if !heuristicBrowserFocus(message, activated) {
		return nil
	}
	return buildBrowserFocusDomains(message, activated, false)
}

func taskContractPrefersBrowserFocus(contract *TaskContract, message string, activated map[ToolDomain]bool) bool {
	if contract == nil {
		return false
	}
	if hasActivatedDomainOutside(activated, browserFocusCompatibleDomains) {
		return false
	}
	if contract.JobType == taskContractJobDelivery && hasActiveDomain(activated, DomainEmail, DomainChannel, DomainCalendar) {
		return false
	}
	if contract.RequiresExternalEffect {
		return true
	}
	if browserRequestNeedsWorkspaceTools(message) {
		return true
	}
	return taskContractDeliverablesContain(contract.ExpectedDeliverables,
		taskDeliverableBrowserEvidence,
		taskDeliverableDocument,
		taskDeliverableSpreadsheet,
		taskDeliverablePresentation,
	)
}

func heuristicBrowserFocus(message string, activated map[ToolDomain]bool) bool {
	if hasActivatedDomainOutside(activated, browserFocusCompatibleDomains) {
		return false
	}
	if isInteractiveBrowserRequest(message) || looksLikeSearchResultsExtractionRequest(message) || browserRequestNeedsWorkspaceTools(message) {
		return true
	}
	return messageHasBrowserReference(message)
}

func buildBrowserFocusDomains(message string, activated map[ToolDomain]bool, includeCompanions bool) map[ToolDomain]bool {
	out := map[ToolDomain]bool{
		DomainBrowser: true,
	}
	if includeCompanions {
		for domain := range activated {
			if browserFocusCompanionDomains[domain] {
				out[domain] = true
			}
		}
	}
	if browserRequestNeedsWorkspaceTools(message) {
		out[DomainFS] = true
	}
	return out
}

func cloneToolDomainSet(in map[ToolDomain]bool) map[ToolDomain]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[ToolDomain]bool, len(in))
	for domain, enabled := range in {
		if !enabled {
			continue
		}
		out[domain] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func checkEmptyActivatedDomains(activated map[ToolDomain]bool, tools []ToolDefinition) []ToolDomain {
	if len(activated) == 0 {
		return nil
	}
	available := make(map[ToolDomain]bool)
	for _, tool := range canonicalToolDefinitions(tools) {
		domain := extractToolDomain(tool.Name)
		if domain == "" {
			continue
		}
		available[domain] = true
	}
	empty := make([]ToolDomain, 0, len(activated))
	for domain, enabled := range activated {
		if !enabled || domain == "" {
			continue
		}
		if domain == DomainAgent {
			continue
		}
		if tier := domainTier[domain]; tier > 0 && tier <= 2 {
			continue
		}
		if available[domain] {
			continue
		}
		empty = append(empty, domain)
	}
	if len(empty) == 0 {
		return nil
	}
	sort.Slice(empty, func(i, j int) bool {
		return empty[i] < empty[j]
	})
	return empty
}

func hasActiveDomain(domains map[ToolDomain]bool, candidates ...ToolDomain) bool {
	for _, domain := range candidates {
		if domains[domain] {
			return true
		}
	}
	return false
}

func hasActivatedDomainOutside(domains map[ToolDomain]bool, allowed map[ToolDomain]bool) bool {
	for domain, enabled := range domains {
		if !enabled {
			continue
		}
		if allowed[domain] {
			continue
		}
		return true
	}
	return false
}

// execSuppressKeywords only includes technical identifiers that map cleanly to
// built-in tools. Natural-language verbs are excluded so exec suppression does
// not depend on the user's language.
var execSuppressKeywords = []struct {
	keyword      string
	requiresTool string // at least this tool must be in the list
}{
	{"sha256", "crypto.hash"},
	{"sha1", "crypto.hash"},
	{"md5", "crypto.hash"},
	{"base64", "text.base64"},
	{"json", "text.json"},
	{"yaml", "text.yaml"},
	{"csv", "text.csv"},
	{"xml", "text.xml"},
	{"regex", "text.regex"},
	{"dns", "net.dns"},
	{"hmac", "crypto.hmac"},
	{"uuid", "text.uuid"},
}

// shouldSuppressExec returns true when the user message targets a capability
// covered by a built-in tool that is already in the result list.
func shouldSuppressExec(message string, tools []ToolDefinition) bool {
	return shouldSuppressExecWithDomains(message, tools, nil)
}

func shouldSuppressExecWithDomains(message string, tools []ToolDefinition, activatedDomains map[ToolDomain]bool) bool {
	if message == "" {
		return false
	}
	lower := strings.ToLower(message)

	if explicitlyRequestsExecPath(message) {
		return false
	}

	toolSet := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		toolSet[t.Name] = struct{}{}
	}

	for _, entry := range execSuppressKeywords {
		if strings.Contains(lower, entry.keyword) {
			if _, ok := toolSet[entry.requiresTool]; ok {
				return true
			}
		}
	}
	domains := cloneToolDomainSet(activatedDomains)
	if len(domains) == 0 {
		domains = resolveActivatedDomainsForToolSelection(tools, message, toolSelectionSignals{})
	}
	if domains[DomainBrowser] && hasAnyTool(toolSet,
		"browser.open", "browser.navigate", "browser.click", "browser.fill", "browser.type",
		"browser.snapshot", "browser.screenshot", "browser.wait",
	) {
		return true
	}
	if domains[DomainDesktop] && hasAnyTool(toolSet,
		"desktop.list_apps", "desktop.list_windows", "desktop.screenshot",
		"desktop.get_clipboard", "desktop.set_clipboard", "desktop.focus_window", "desktop.type_text",
	) {
		return true
	}
	return false
}

func mentionsExplicitShellCommand(lower string) bool {
	for _, token := range []string{
		"curl",
		"wget",
		"openssl",
		"bash",
		"zsh",
		"sh",
		"pwsh",
		"powershell",
		"cmd.exe",
	} {
		if hasExplicitCommandStructure(lower, token) {
			return true
		}
	}
	return false
}

func explicitlyRequestsExecPath(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	if containsAny(lower, "exec.run", "exec.shell", "exec.script") {
		return true
	}
	return mentionsExplicitShellCommand(lower)
}

func removeTools(tools []ToolDefinition, names ...string) []ToolDefinition {
	remove := make(map[string]bool, len(names))
	for _, n := range names {
		remove[n] = true
	}
	filtered := make([]ToolDefinition, 0, len(tools))
	for _, t := range tools {
		if !remove[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func isInteractiveBrowserRequest(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	if !hasStructuredURLContext(lower) && !messageHasBrowserReference(message) {
		return false
	}
	if looksLikeSearchResultsExtractionRequest(message) {
		return true
	}
	return containsAny(lower,
		"open", "打开", "form", "submit", "click", "fill", "type", "input", "button",
		"表单", "提交", "点击", "填写", "输入", "按钮",
		"screenshot", "snapshot", "extract", "grab", "wait", "loaded", "load",
		"截图", "抓取", "提取", "等待", "加载",
	)
}

func looksLikeSearchResultsExtractionRequest(message string) bool {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return false
	}
	if ctx, ok := browserReferenceContextFromSummary(trimmed); ok {
		return browserReferenceContextLooksLikeSearchResults(ctx)
	}
	for _, rawURL := range explicitBrowserReferenceURLs(trimmed) {
		if browserURLLooksLikeSearchResults(rawURL) {
			return true
		}
	}
	lower := strings.ToLower(trimmed)
	hasSearchContext := containsAny(lower,
		"search result", "search results", "search page", "serp",
		"搜索结果", "搜索页", "检索结果",
	)
	if !hasSearchContext {
		hasSearchContext = containsAny(lower, "bing", "google", "duckduckgo", "baidu") &&
			containsAny(lower, "search", "搜索")
	}
	if !hasSearchContext {
		return false
	}
	return containsAny(lower,
		"extract", "grab", "list", "summarize", "summarise",
		"wait", "loaded", "load", "top 5", "first 5",
		"提取", "抓取", "列出", "总结", "等待", "加载", "显示出来",
		"前 5", "前5", "前五", "前几条", "前几项",
	)
}

func preferInteractiveBrowserTools(message string, tools []ToolDefinition) []ToolDefinition {
	signals := inferToolSelectionSignals(message, nil, nil)
	signals.activatedDomains = resolveActivatedDomainsForToolSelection(tools, message, signals)
	signals.browserFocusDomains = inferBrowserFocusDomains(message, signals.activatedDomains, nil)
	return preferInteractiveBrowserToolsWithDomains(message, tools, signals)
}

func preferInteractiveBrowserToolsWithDomains(message string, tools []ToolDefinition, signals toolSelectionSignals) []ToolDefinition {
	allowedDomains := signals.browserFocusDomains
	if len(tools) == 0 {
		return nil
	}
	if len(allowedDomains) == 0 {
		return nil
	}
	filtered := make([]ToolDefinition, 0, len(tools))
	for _, tool := range canonicalToolDefinitions(tools) {
		domain := extractToolDomain(tool.Name)
		if !allowedDomains[domain] {
			continue
		}
		if domain == DomainBrowser && !browserToolVisibleForRequest(tool.Name, message, signals) {
			continue
		}
		if domain == DomainFS && !browserWorkspaceToolAllowed(tool.Name, message) {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func focusInteractiveBrowserToolPoolWithSignals(tools []ToolDefinition, message string, signals toolSelectionSignals) []ToolDefinition {
	if len(tools) == 0 || len(signals.browserFocusDomains) == 0 || !shouldSuppressExecWithDomains(message, tools, signals.activatedDomains) {
		return nil
	}

	filtered := preferInteractiveBrowserToolsWithDomains(message, tools, signals)
	if len(filtered) == 0 {
		return nil
	}

	hasBrowserTool := false
	for _, tool := range filtered {
		if extractToolDomain(tool.Name) == DomainBrowser {
			hasBrowserTool = true
			break
		}
	}
	if !hasBrowserTool {
		return nil
	}
	return filtered
}

func browserRequestNeedsWorkspaceTools(message string) bool {
	return messageHasLocalPathReference(message)
}

func messageHasLocalPathReference(message string) bool {
	return countLocalPathReferences(message) > 0
}

func countLocalPathReferences(message string) int {
	fields := strings.Fields(message)
	seen := make(map[string]struct{}, len(fields))
	count := 0
	for _, field := range fields {
		token := strings.Trim(field, `"'()[]{}<>，。；：,.!?`)
		lower := strings.ToLower(token)
		if lower == "" {
			continue
		}
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "artifact://") {
			continue
		}
		if strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") || strings.HasPrefix(token, "/") {
			if _, ok := seen[lower]; !ok {
				seen[lower] = struct{}{}
				count++
			}
			continue
		}
		if strings.Contains(token, "/") {
			if _, ok := seen[lower]; !ok {
				seen[lower] = struct{}{}
				count++
			}
			continue
		}
		if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".log") {
			if _, ok := seen[lower]; !ok {
				seen[lower] = struct{}{}
				count++
			}
		}
	}
	return count
}

func browserWorkspaceToolAllowed(name, message string) bool {
	switch strings.TrimSpace(name) {
	case "fs.write", "fs.stat":
		return true
	case "fs.read", "fs.edit":
		return browserRequestMayNeedExistingWorkspaceContent(message)
	default:
		return false
	}
}

var browserEvalKeywordPattern = regexp.MustCompile(`(?i)\b(browser\.eval|javascript|java script|devtools?|console|console\.log|localstorage|local storage|sessionstorage|session storage|document\.cookie)\b`)
var browserReferenceURLPattern = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s<>"'(){}\[\]]+`)
var browserEmailFieldPattern = regexp.MustCompile(`(?i)(type=email|autocomplete=email|input\[[^\]]*email[^\]]*\]|name=email|id=email|selector=[^\s]*email|field=email)`)

var browserNavigablePageExtensions = map[string]bool{
	".asp":   true,
	".aspx":  true,
	".cfm":   true,
	".cgi":   true,
	".htm":   true,
	".html":  true,
	".jsp":   true,
	".php":   true,
	".shtml": true,
	".xhtml": true,
}

var browserNonPageArtifactExtensions = map[string]bool{
	".atom": true,
	".csv":  true,
	".doc":  true,
	".docx": true,
	".gif":  true,
	".gz":   true,
	".ics":  true,
	".jpeg": true,
	".jpg":  true,
	".json": true,
	".md":   true,
	".mov":  true,
	".mp3":  true,
	".mp4":  true,
	".ods":  true,
	".pdf":  true,
	".png":  true,
	".ppt":  true,
	".pptx": true,
	".rss":  true,
	".svg":  true,
	".tar":  true,
	".tgz":  true,
	".tsv":  true,
	".txt":  true,
	".wav":  true,
	".webp": true,
	".xls":  true,
	".xlsx": true,
	".xml":  true,
	".yaml": true,
	".yml":  true,
	".zip":  true,
}

func browserToolVisibleForRequest(name, message string, signals toolSelectionSignals) bool {
	switch strings.TrimSpace(name) {
	case "browser.eval":
		return browserEvalToolAllowed(message)
	case "browser.element_text", "browser.element_attr", "browser.screenshot_labeled":
		return !searchResultsExtractionContext(message, signals)
	default:
		return true
	}
}

func searchResultsExtractionContext(message string, signals toolSelectionSignals) bool {
	if signals.reuseSessionBrowserContext && signals.browserSearchResultsContext {
		return true
	}
	return looksLikeSearchResultsExtractionRequest(message)
}

func browserEvalToolAllowed(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	return browserEvalKeywordPattern.MatchString(lower)
}

func browserRequestMayNeedExistingWorkspaceContent(message string) bool {
	return countLocalPathReferences(message) > 1
}

// applyEnhancedDescriptions overwrites tool descriptions with richer versions
// that explicitly state preference over shell commands. This helps the model
// discover and choose built-in tools instead of shelling out.
func applyEnhancedDescriptions(tools []ToolDefinition) {
	for i := range tools {
		if desc, ok := enhancedDescriptions[tools[i].Name]; ok {
			tools[i].Description = desc
		}
	}
}

// enhancedDescriptions maps tool names to improved descriptions that hint
// at when to use each tool and why it's preferred over shell alternatives.
var enhancedDescriptions = map[string]string{
	"crypto.random": "Generate cryptographically secure random bytes (hex, base64, or raw). " +
		"Use this instead of 'openssl rand' — cross-platform, no external dependency. " +
		"Parameters: bytes (int), format (hex|base64|raw).",

	"crypto.hash": "Compute hash of a string or file. Supports MD5, SHA1, SHA256, SHA512. " +
		"Use this instead of 'shasum', 'md5sum', 'openssl dgst' — consistent cross-platform output. " +
		"Parameters: input (string), algorithm (md5|sha1|sha256|sha512), file (optional path).",

	"crypto.hmac": "Compute HMAC signature. Supports SHA256, SHA512. " +
		"Use this instead of 'openssl dgst -hmac' — simpler, cross-platform.",

	"text.base64": "Encode or decode Base64 strings. " +
		"Use this instead of the 'base64' shell command — handles binary safely, no escaping issues. " +
		"Parameters: input (string), mode (encode|decode).",

	"text.json": "Parse JSON and optionally query with dot-path (like jq). " +
		"Use this instead of 'jq' or 'python3 -c json' — no external dependency. " +
		"Parameters: input (string or file), query (optional dot-path like '.name').",

	"text.yaml": "Parse YAML to JSON. Use this instead of 'python3 -c yaml' — no dependency.",

	"text.csv": "Parse CSV/TSV with optional column selection. No external dependency.",

	"text.regex": "Match, extract, replace, or split text with regex. " +
		"Use this instead of 'grep', 'sed', 'awk' for complex text processing.",

	"net.fetch": "Fetch URL content via HTTP/HTTPS with automatic text extraction. " +
		"Use this instead of 'curl' for reading web pages — returns clean text, handles redirects. " +
		"Parameters: url (string), method (GET|POST), headers (optional).",

	"net.dns": "DNS lookup for A, AAAA, MX, TXT, CNAME, NS records. " +
		"Use this instead of 'dig' or 'nslookup' — structured JSON output.",

	"exec.run": "Execute a program with arguments (no shell interpretation). " +
		"NOTE: Prefer specialized built-in tools (crypto.*, text.*, net.*) when available. " +
		"Only use exec.run when no built-in tool covers the task.",

	"exec.shell": "Execute a shell command string via bash. " +
		"NOTE: Prefer specialized built-in tools (crypto.*, text.*, net.*) when available. " +
		"Only use exec.shell as a last resort when no built-in tool covers the task.",
}

// Structured evidence detection only looks at machine-readable signals such as
// URLs, file extensions, email addresses, artifact URIs, and explicit technical
// identifiers. It intentionally avoids natural-language keyword activation.
var evidenceTokenPattern = regexp.MustCompile(`[a-z0-9._:+/-]+`)
var emailEvidencePattern = regexp.MustCompile(`[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)

var structuredEvidenceTokenToDomain = buildStructuredEvidenceTokenToDomain()
var evidenceTokenToDomain = buildEvidenceTokenToDomain()

func buildStructuredEvidenceTokenToDomain() map[string]ToolDomain {
	return map[string]ToolDomain{
		"rss":    DomainNews,
		"atom":   DomainNews,
		"hnrss":  DomainNews,
		"ics":    DomainCalendar,
		"caldav": DomainCalendar,
		"csv":    DomainSheet,
		"tsv":    DomainSheet,
		"xlsx":   DomainSheet,
		"xls":    DomainSheet,
		"ods":    DomainSheet,
		"doc":    DomainDocument,
		"docx":   DomainDocument,
		"odt":    DomainDocument,
		"rtf":    DomainDocument,
		"ppt":    DomainPresentation,
		"pptx":   DomainPresentation,
		"key":    DomainPresentation,
		"pdf":    DomainPDF,
		"zip":    DomainArchive,
		"tar":    DomainArchive,
		"gz":     DomainArchive,
		"tgz":    DomainArchive,
		"sha":    DomainCrypto,
		"sha1":   DomainCrypto,
		"sha256": DomainCrypto,
		"sha512": DomainCrypto,
		"md5":    DomainCrypto,
		"hmac":   DomainCrypto,
		"base64": DomainText,
		"json":   DomainText,
		"yaml":   DomainText,
		"xml":    DomainText,
		"regex":  DomainText,
		"uuid":   DomainText,
	}
}

func buildEvidenceTokenToDomain() map[string]ToolDomain {
	out := make(map[string]ToolDomain)
	for key, domain := range capabilityToDomain {
		key = strings.TrimSpace(strings.ToLower(key))
		if key == "" {
			continue
		}
		out[key] = domain
	}
	for domain, category := range toolDomainCategories {
		if domain == "" {
			continue
		}
		out[strings.ToLower(string(domain))] = domain
		out[strings.ToLower(strings.TrimSpace(category))] = domain
	}
	for domain := range domainTier {
		if domain == "" {
			continue
		}
		out[strings.ToLower(string(domain))] = domain
	}
	return out
}

func sanitizeSuggestedDomainsForMessage(message string, domains []string) []string {
	normalized := normalizeSemanticDomains(domains)
	if len(normalized) == 0 {
		return nil
	}
	if !hasSemanticDomain(normalized, DomainBrowser) {
		return normalized
	}

	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return normalized
	}
	suppressDelivery := shouldSuppressDeliveryDomainsForBrowserContext(message, lower, normalized)
	suppressWatch := shouldSuppressWatchDomainsForBrowserContext(message, normalized)
	if !suppressDelivery && !suppressWatch {
		return normalized
	}

	out := make([]string, 0, len(normalized))
	for _, domain := range normalized {
		switch ToolDomain(domain) {
		case DomainEmail, DomainChannel:
			if suppressDelivery {
				continue
			}
		case DomainWatch, DomainCron:
			if suppressWatch {
				continue
			}
		}
		out = append(out, domain)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func removeSemanticDomains(domains []string, blocked ...ToolDomain) []string {
	normalized := normalizeSemanticDomains(domains)
	if len(normalized) == 0 || len(blocked) == 0 {
		return normalized
	}
	blockedSet := make(map[ToolDomain]struct{}, len(blocked))
	for _, domain := range blocked {
		if domain == "" {
			continue
		}
		blockedSet[domain] = struct{}{}
	}
	out := make([]string, 0, len(normalized))
	for _, domain := range normalized {
		if _, ok := blockedSet[ToolDomain(domain)]; ok {
			continue
		}
		out = append(out, domain)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func shouldSuppressDeliveryDomainsForBrowserContext(message, lower string, domains []string) bool {
	if !hasSemanticDomain(domains, DomainBrowser) {
		return false
	}
	if taskExplicitlyRequestsMessageDelivery(message) {
		return false
	}
	_ = lower
	return looksLikeBrowserEmailFieldReference(message)
}

func looksLikeBrowserEmailFieldReference(message string) bool {
	return browserEmailFieldPattern.MatchString(message)
}

func looksLikeLocalPathToken(token, _ string, browserContext bool) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if strings.HasPrefix(token, "http://") || strings.HasPrefix(token, "https://") || strings.HasPrefix(token, "www.") {
		return false
	}
	if strings.Contains(token, `\`) {
		return true
	}
	if strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") || strings.HasPrefix(token, "~/") {
		return true
	}
	if strings.HasPrefix(token, "/") {
		absolute := strings.TrimPrefix(token, "/")
		if absolute == "" {
			return false
		}
		if idx := strings.Index(absolute, "/"); idx >= 0 {
			return true
		}
		switch strings.ToLower(absolute) {
		case ".github", "cmd", "dev", "docs", "etc", "home", "internal", "mnt",
			"opt", "pkg", "private", "scripts", "src", "testdata", "tmp", "users",
			"usr", "var", "volumes":
			return true
		default:
			return false
		}
	}
	if !strings.Contains(token, "/") {
		return false
	}
	if pathExt(token) != "" {
		return true
	}

	firstSegment := strings.TrimPrefix(token, "/")
	if idx := strings.Index(firstSegment, "/"); idx >= 0 {
		firstSegment = firstSegment[:idx]
	}
	switch strings.ToLower(firstSegment) {
	case ".github", "cmd", "dev", "docs", "etc", "home", "internal", "mnt",
		"opt", "pkg", "private", "scripts", "src", "testdata", "tmp", "users",
		"usr", "var", "volumes":
		return true
	}

	if browserContext &&
		!strings.HasPrefix(token, "/") &&
		strings.Count(token, "/") == 1 &&
		pathExt(token) == "" &&
		!strings.Contains(firstSegment, ".") {
		return false
	}

	return true
}

func sanitizeDetectedDomains(message string, activated map[ToolDomain]bool) map[ToolDomain]bool {
	if len(activated) == 0 {
		return nil
	}
	raw := make([]string, 0, len(activated))
	for domain := range activated {
		raw = append(raw, string(domain))
	}
	sanitized := sanitizeSuggestedDomainsForMessage(message, raw)
	if len(sanitized) == 0 {
		return nil
	}
	out := make(map[ToolDomain]bool, len(sanitized))
	for _, domain := range sanitized {
		out[ToolDomain(domain)] = true
	}
	return out
}

func fallbackHeuristicDomains(message string) map[ToolDomain]bool {
	// Start from structured evidence, then add a small natural-language fallback
	// for browser/watch/report/desktop intents so degraded analyzer paths still
	// cover ordinary user requests.
	activated := cloneToolDomainSet(detectStructuredEvidence(message))
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower != "" {
		if activated == nil {
			activated = make(map[ToolDomain]bool)
		}
		if isInteractiveBrowserRequest(message) ||
			messageHasBrowserReference(message) ||
			fallbackPreflightMentionsBrowserContext(lower) ||
			looksLikeSearchResultsExtractionRequest(message) {
			activated[DomainBrowser] = true
		}
		if fallbackTaskContractMentionsMonitorIntent(lower) || fallbackTaskContractMentionsBrowserWatchIntent(lower) {
			activated[DomainWatch] = true
		}
		if fallbackTaskContractMentionsAutomationIntent(lower) ||
			fallbackTaskContractMentionsScheduledExecution(lower) ||
			fallbackTaskContractMentionsScheduleReference(lower) {
			activated[DomainCron] = true
		}
		if fallbackTaskContractMentionsSpreadsheetDeliverable(lower) {
			activated[DomainSheet] = true
		}
		if fallbackTaskContractMentionsDocumentDeliverable(lower) || fallbackTaskContractMentionsStructuredWriteup(lower) {
			activated[DomainDocument] = true
		}
		if fallbackTaskContractMentionsPresentationDeliverable(lower) {
			activated[DomainPresentation] = true
		}
		if fallbackDesktopIntent(message, lower) {
			activated[DomainDesktop] = true
		}
	}
	return sanitizeDetectedDomains(message, activated)
}

func fallbackDesktopIntent(message, lower string) bool {
	if lower == "" {
		return false
	}
	if containsAny(lower,
		"desktop.", "desktop app", "desktop apps", "desktop application", "desktop applications",
		"frontmost window", "foreground window", "window list", "window title",
		"current clipboard", "system clipboard",
		"当前桌面", "桌面应用", "桌面程序", "运行中的应用", "应用列表", "前台窗口", "窗口标题", "当前剪贴板", "系统剪贴板", "剪贴板",
	) {
		return true
	}
	if messageHasBrowserReference(message) || fallbackPreflightMentionsBrowserContext(lower) {
		return false
	}
	if containsAny(lower, "desktop screenshot", "screenshot of the desktop", "screenshot the desktop") {
		return true
	}
	if strings.Contains(lower, "截图") && strings.Contains(lower, "桌面") {
		return true
	}
	return false
}

func detectStructuredEvidence(message string, browserContextHint ...bool) map[ToolDomain]bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return nil
	}
	activated := make(map[ToolDomain]bool)
	urlContext := hasStructuredURLContext(lower)
	if len(browserContextHint) > 0 && browserContextHint[0] {
		urlContext = true
	}
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		activated[DomainNet] = true
		activated[DomainBrowser] = true
	}
	if strings.Contains(lower, "artifact://") {
		activated[DomainFS] = true
	}
	if emailEvidencePattern.MatchString(lower) {
		activated[DomainEmail] = true
	}
	for _, token := range evidenceTokenPattern.FindAllString(lower, -1) {
		token = strings.Trim(token, ".,;:!?()[]{}<>\"'")
		if token == "" {
			continue
		}
		normalized := strings.TrimPrefix(token, ".")
		if domain, ok := structuredEvidenceTokenToDomain[normalized]; ok {
			activated[domain] = true
		}
		switch {
		case strings.HasSuffix(token, ".csv"), strings.HasSuffix(token, ".tsv"), strings.HasSuffix(token, ".xlsx"), strings.HasSuffix(token, ".xls"), strings.HasSuffix(token, ".ods"):
			activated[DomainSheet] = true
		case strings.HasSuffix(token, ".doc"), strings.HasSuffix(token, ".docx"), strings.HasSuffix(token, ".odt"), strings.HasSuffix(token, ".rtf"):
			activated[DomainDocument] = true
		case strings.HasSuffix(token, ".ppt"), strings.HasSuffix(token, ".pptx"), strings.HasSuffix(token, ".key"):
			activated[DomainPresentation] = true
		case strings.HasSuffix(token, ".ics"):
			activated[DomainCalendar] = true
		case strings.HasSuffix(token, ".pdf"):
			activated[DomainPDF] = true
		case strings.HasSuffix(token, ".zip"), strings.HasSuffix(token, ".tar"), strings.HasSuffix(token, ".gz"), strings.HasSuffix(token, ".tgz"):
			activated[DomainArchive] = true
		case strings.HasSuffix(token, ".png"), strings.HasSuffix(token, ".jpg"), strings.HasSuffix(token, ".jpeg"), strings.HasSuffix(token, ".gif"), strings.HasSuffix(token, ".webp"), strings.HasSuffix(token, ".svg"):
			activated[DomainMedia] = true
			activated[DomainVision] = true
		case strings.HasSuffix(token, ".mp3"), strings.HasSuffix(token, ".wav"), strings.HasSuffix(token, ".m4a"), strings.HasSuffix(token, ".flac"):
			activated[DomainMedia] = true
			activated[DomainSpeech] = true
		case strings.HasSuffix(token, ".mp4"), strings.HasSuffix(token, ".mov"), strings.HasSuffix(token, ".avi"), strings.HasSuffix(token, ".mkv"):
			activated[DomainMedia] = true
		case (strings.Contains(token, "/") || strings.Contains(token, `\`)) &&
			!strings.HasPrefix(token, "http://") &&
			!strings.HasPrefix(token, "https://") &&
			!strings.HasPrefix(token, "www.") &&
			looksLikeLocalPathToken(token, lower, urlContext):
			activated[DomainFS] = true
		case strings.HasPrefix(normalized, "sha"), normalized == "md5", normalized == "hmac":
			activated[DomainCrypto] = true
		case normalized == "base64", normalized == "json", normalized == "yaml", normalized == "xml", normalized == "regex", normalized == "uuid":
			activated[DomainText] = true
		}
	}
	return activated
}

func hasStructuredURLContext(lower string) bool {
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "www.") {
		return true
	}
	return containsAny(lower, " url ", " uri ", "url:", "uri:", "path=", "query=")
}

func messageHasBrowserReference(message string) bool {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return false
	}
	if ctx, ok := browserReferenceContextFromSummary(trimmed); ok {
		return browserReferenceContextLooksLikeNavigablePage(ctx)
	}
	for _, rawURL := range explicitBrowserReferenceURLs(trimmed) {
		if browserURLLooksLikeNavigablePage(rawURL) {
			return true
		}
	}
	lower := strings.ToLower(trimmed)
	return fallbackPreflightMentionsBrowserContext(lower) ||
		containsAny(lower, fallbackTaskContractBrowserReferencePhrases...)
}

func explicitBrowserReferenceURLs(message string) []string {
	matches := browserReferenceURLPattern.FindAllString(message, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		normalized := normalizeStructuredBrowserURLToken(match)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeStructuredBrowserURLToken(token string) string {
	token = strings.TrimSpace(strings.Trim(token, `"'()[]{}<>，。；：,.!?`))
	if token == "" {
		return ""
	}
	lower := strings.ToLower(token)
	if strings.HasPrefix(lower, "www.") {
		return "https://" + token
	}
	return token
}

func parseStructuredBrowserURL(token string) (*url.URL, bool) {
	normalized := normalizeStructuredBrowserURLToken(token)
	if normalized == "" {
		return nil, false
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed == nil {
		return nil, false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, false
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return nil, false
	}
	return parsed, true
}

func browserURLLooksLikeNavigablePage(raw string) bool {
	parsed, ok := parseStructuredBrowserURL(raw)
	if !ok {
		return false
	}
	ext := strings.ToLower(pathExt(parsed.Path))
	if ext == "" {
		return true
	}
	if browserNavigablePageExtensions[ext] {
		return true
	}
	if browserNonPageArtifactExtensions[ext] {
		return false
	}
	return false
}

func browserURLLooksLikeSearchResults(raw string) bool {
	parsed, ok := parseStructuredBrowserURL(raw)
	if !ok || !browserURLLooksLikeNavigablePage(raw) {
		return false
	}
	pathValue := strings.ToLower(strings.TrimSpace(parsed.Path))
	if pathValue == "/search" || strings.HasSuffix(pathValue, "/search") || strings.HasSuffix(pathValue, "/search/") {
		return true
	}
	query := parsed.Query()
	for _, key := range []string{"q", "query", "wd", "text", "keyword", "search_query"} {
		if strings.TrimSpace(query.Get(key)) != "" {
			return true
		}
	}
	return false
}

func browserReferenceContextLooksLikeNavigablePage(ctx browserReferenceContext) bool {
	return browserURLLooksLikeNavigablePage(ctx.URL)
}

func domainBudgetFor(domain ToolDomain) int {
	if b, ok := domainBudget[domain]; ok {
		return b
	}
	return defaultDomainBudget
}

func appendUpTo(result *[]ToolDefinition, tools []ToolDefinition, budget, remaining int) int {
	added := 0
	for _, tool := range tools {
		if added >= budget || added >= remaining {
			break
		}
		// Skip if already in result (essential tools).
		alreadyAdded := false
		for _, existing := range *result {
			if existing.Name == tool.Name {
				alreadyAdded = true
				break
			}
		}
		if alreadyAdded {
			continue
		}
		*result = append(*result, tool)
		added++
	}
	return added
}

// ---------------------------------------------------------------------------
// extractToolDomain
// ---------------------------------------------------------------------------

// extractToolDomain derives the ToolDomain from a dot-prefixed tool name.
// "fs.read" → DomainFS, "browser.click" → DomainBrowser.
func extractToolDomain(toolName string) ToolDomain {
	if idx := strings.IndexByte(toolName, '.'); idx > 0 {
		return ToolDomain(toolName[:idx])
	}
	return ""
}

// ---------------------------------------------------------------------------
// Plan-aware filtering (unchanged, used by task runner)
// ---------------------------------------------------------------------------

// coreDomains are always included regardless of task kind.
var coreDomains = map[ToolDomain]bool{
	DomainFS:   true,
	DomainExec: true,
}

// taskKindDomains maps each TaskKind to additional tool domains.
var taskKindDomains = map[planpkg.TaskKind][]ToolDomain{
	planpkg.TaskResearch:  {DomainNet, DomainText, DomainBrowser, DomainDesktop, DomainPDF, DomainDB, DomainCanvas, DomainSearch, DomainWeb},
	planpkg.TaskTranslate: {DomainText},
	planpkg.TaskTransform: {DomainText, DomainArchive, DomainCrypto, DomainSheet, DomainDocument, DomainPresentation},
	planpkg.TaskWrite:     {DomainText, DomainArchive, DomainSheet, DomainDocument, DomainPresentation},
	planpkg.TaskExecute:   {DomainEnv, DomainNet, DomainProc, DomainDB, DomainCron, DomainWatch, DomainCrypto, DomainText, DomainArchive, DomainGit, DomainSheet, DomainDesktop, DomainBrowser},
	planpkg.TaskReview:    {DomainText, DomainGit},
	planpkg.TaskDeliver:   {DomainChannel, DomainNet, DomainArchive, DomainPDF, DomainEmail, DomainDocument, DomainCalendar},
}

var capabilityToDomain = map[string]ToolDomain{
	"browser": DomainBrowser, "canvas": DomainCanvas,
	"desktop": DomainDesktop, "ui": DomainDesktop, "window": DomainDesktop, "clipboard": DomainDesktop, "screen": DomainDesktop,
	"database": DomainDB, "db": DomainDB,
	"crypto": DomainCrypto, "encryption": DomainCrypto,
	"network": DomainNet, "net": DomainNet, "http": DomainNet,
	"web": DomainWeb, "website": DomainWeb, "site": DomainWeb, "article": DomainWeb,
	"search": DomainSearch, "research": DomainSearch,
	"spreadsheet": DomainSheet, "sheet": DomainSheet,
	"channel": DomainChannel, "messaging": DomainChannel,
	"slack": DomainChannel, "discord": DomainChannel, "telegram": DomainChannel, "webhook": DomainChannel, "feishu": DomainChannel, "wechat": DomainChannel,
	"cron": DomainCron, "scheduling": DomainCron,
	"watch": DomainWatch, "monitor": DomainWatch, "monitoring": DomainWatch,
	"pdf": DomainPDF, "archive": DomainArchive,
	"document": DomainDocument, "docx": DomainDocument,
	"presentation": DomainPresentation, "pptx": DomainPresentation,
	"calendar": DomainCalendar, "caldav": DomainCalendar,
	"email": DomainEmail, "e-mail": DomainEmail, "mailbox": DomainEmail, "imap": DomainEmail, "gmail": DomainEmail, "inbox": DomainEmail, "outbox": DomainEmail,
	"news": DomainNews, "rss": DomainNews, "feed": DomainNews, "feeds": DomainNews, "atom": DomainNews, "newsletter": DomainNews, "hnrss": DomainNews,
	"process": DomainProc, "proc": DomainProc,
	"env": DomainEnv, "environment": DomainEnv,
	"gateway": DomainGateway, "nodes": DomainNodes,
	"agent": DomainAgent, "text": DomainText,
	"fs": DomainFS, "filesystem": DomainFS,
	"exec": DomainExec, "skill": DomainSkill,
}

// filterToolsForTask returns tools relevant to a plan task.
func filterToolsForTask(tools []ToolDefinition, task *planpkg.Task) []ToolDefinition {
	if task == nil || task.Kind == "" {
		return tools
	}
	allowed := buildAllowedDomains(task.Kind, task.RequiredCapabilities)
	filtered := make([]ToolDefinition, 0, len(tools)/2)
	for _, tool := range tools {
		domain := extractToolDomain(tool.Name)
		if domain == "" || allowed[domain] {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func buildAllowedDomains(kind planpkg.TaskKind, capabilities []string) map[ToolDomain]bool {
	allowed := make(map[ToolDomain]bool, len(coreDomains)+8)
	for d := range coreDomains {
		allowed[d] = true
	}
	if domains, ok := taskKindDomains[kind]; ok {
		for _, d := range domains {
			allowed[d] = true
		}
	}
	for _, cap := range capabilities {
		normalized := strings.ToLower(strings.TrimSpace(cap))
		if normalized == "" {
			continue
		}
		if d, ok := capabilityToDomain[normalized]; ok {
			allowed[d] = true
		}
		allowed[ToolDomain(normalized)] = true
	}
	return allowed
}

package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/skill"
)

var toolDomainCategories = map[ToolDomain]string{
	DomainFS:           "files",
	DomainExec:         "shell",
	DomainEnv:          "environment",
	DomainNet:          "network",
	DomainText:         "text",
	DomainArchive:      "archive",
	DomainCrypto:       "crypto",
	DomainDB:           "database",
	DomainSheet:        "spreadsheet",
	DomainProc:         "process",
	DomainChannel:      "messaging",
	DomainCron:         "scheduling",
	DomainWatch:        "monitoring",
	DomainAgent:        "agent",
	DomainBrowser:      "browser",
	DomainDesktop:      "desktop",
	DomainPDF:          "pdf",
	DomainCanvas:       "canvas",
	DomainGateway:      "gateway",
	DomainNodes:        "desktop-device",
	DomainSkill:        "skills",
	DomainGit:          "git",
	DomainSearch:       "search",
	DomainSpeech:       "speech",
	DomainMedia:        "media",
	DomainVision:       "vision",
	DomainMemory:       "memory",
	DomainSession:      "session",
	DomainEmail:        "email",
	DomainWeb:          "web-fetch",
	DomainNews:         "news",
	DomainDocument:     "document",
	DomainPresentation: "presentation",
	DomainCalendar:     "calendar",
}

var toolDomainSummaries = map[ToolDomain]string{
	DomainFS:               "workspace files, search, and edits",
	DomainExec:             "shell commands and process execution",
	DomainEnv:              "environment inspection and skill/session utilities",
	DomainNet:              "HTTP, DNS, download, and network probing",
	DomainText:             "structured text parsing and transformation",
	DomainArchive:          "zip/tar packaging and extraction",
	DomainCrypto:           "hashing, encryption, and secure random",
	DomainDB:               "database queries and execution",
	DomainSheet:            "CSV, TSV, and XLSX table operations",
	DomainProc:             "background process control",
	DomainChannel:          "chat/channel delivery operations",
	DomainCron:             "scheduled jobs",
	DomainWatch:            "monitoring and watch triggers",
	DomainAgent:            "sub-agent and coordination tools",
	DomainBrowser:          "browser automation and page interaction",
	DomainDesktop:          "desktop automation, windows, clipboard, apps, and screenshots",
	DomainPDF:              "PDF parsing and generation",
	DomainCanvas:           "UI canvas rendering and interaction",
	DomainGateway:          "runtime and gateway introspection",
	DomainNodes:            "desktop, device, and node control",
	DomainSkill:            "skill discovery and installation",
	DomainGit:              "git repository inspection and write operations",
	DomainSearch:           "search and retrieval",
	DomainSpeech:           "speech synthesis and transcription",
	DomainMedia:            "image, audio, and video processing",
	DomainVision:           "OCR and visual extraction",
	DomainMemory:           "memory retrieval and updates",
	DomainSession:          "session history and lookup",
	DomainEmail:            "mailbox and email delivery",
	DomainWeb:              "web content fetch",
	DomainNews:             "news digest retrieval",
	DomainDocument:         "DOCX document reading and creation",
	DomainPresentation:     "PPTX slide reading and creation",
	DomainCalendar:         "calendar and ICS operations",
	"":                     "external or uncategorized tools",
	ToolDomain("external"): "external or uncategorized tools",
}

func normalizeToolDefinition(def ToolDefinition) ToolDefinition {
	def.SideEffectClass = skill.NormalizeSideEffectClass(def.SideEffectClass)
	domain := extractToolDomain(def.Name)
	if strings.TrimSpace(def.Domain) == "" {
		def.Domain = string(domain)
	}
	if strings.TrimSpace(def.Category) == "" {
		def.Category = toolCategoryForDomain(domain)
	}
	return def
}

func toolCategoryForDomain(domain ToolDomain) string {
	if category, ok := toolDomainCategories[domain]; ok {
		return category
	}
	if domain == "" {
		return "external"
	}
	return string(domain)
}

func toolSummaryForDomain(domain ToolDomain) string {
	if summary, ok := toolDomainSummaries[domain]; ok {
		return summary
	}
	if domain == "" {
		return toolDomainSummaries[ToolDomain("external")]
	}
	return string(domain) + " operations"
}

func buildToolCatalogPrompt(tools []ToolDefinition) string {
	if len(tools) == 0 {
		return ""
	}
	groups := make(map[string][]string)
	summaries := make(map[string]string)
	order := make([]string, 0, 8)
	for _, tool := range tools {
		normalized := normalizeToolDefinition(tool)
		category := normalized.Category
		if category == "" {
			category = "external"
		}
		if _, ok := groups[category]; !ok {
			order = append(order, category)
			summaries[category] = toolSummaryForDomain(ToolDomain(normalized.Domain))
		}
		groups[category] = append(groups[category], normalized.Name)
	}
	sort.Strings(order)
	var lines []string
	lines = append(lines, "Tool groups for this turn:")
	for _, category := range order {
		names := groups[category]
		sort.Strings(names)
		if len(names) > 6 {
			names = append(names[:6], fmt.Sprintf("+%d more", len(groups[category])-6))
		}
		lines = append(lines, fmt.Sprintf("- %s: %s. Tools: %s.", category, summaries[category], strings.Join(names, ", ")))
	}
	lines = append(lines, "Choose tools from the matching group first. Avoid unrelated groups when a suitable group already exists.")
	return strings.Join(lines, "\n")
}

func decorateToolDescription(tool ToolDefinition) string {
	normalized := normalizeToolDefinition(tool)
	category := normalized.Category
	if category == "" {
		category = "external"
	}
	summary := toolSummaryForDomain(ToolDomain(normalized.Domain))
	prefix := fmt.Sprintf("[category:%s domain:%s] %s.", category, defaultString(normalized.Domain, "external"), summary)
	description := strings.TrimSpace(normalized.Description)
	if description == "" {
		return prefix
	}
	if strings.HasPrefix(description, "[category:") {
		return description
	}
	return prefix + " " + description
}

func prepareToolsForModel(tools []ToolDefinition, userMessage string) []ToolDefinition {
	return prepareToolsForModelWithDomains(filterModelVisibleTools(tools), userMessage, nil)
}

func prepareToolsForModelWithDomains(tools []ToolDefinition, userMessage string, activated map[ToolDomain]bool) []ToolDefinition {
	signals := inferToolSelectionSignals(userMessage, activated, nil)
	return prepareToolsForModelWithSignals(tools, userMessage, signals)
}

func prepareToolsForModelWithSignals(tools []ToolDefinition, userMessage string, signals toolSelectionSignals) []ToolDefinition {
	selected := selectToolsForRequestWithSignals(tools, userMessage, signals)
	out := make([]ToolDefinition, 0, len(selected))
	for _, tool := range selected {
		normalized := normalizeToolDefinition(tool)
		normalized.Description = decorateToolDescription(normalized)
		out = append(out, normalized)
	}
	return out
}

func describeToolsForPlanning(defs []ToolDefinition, limit int) []string {
	if len(defs) == 0 || limit == 0 {
		return nil
	}
	out := make([]string, 0, min(limit, len(defs)))
	seen := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		normalized := normalizeToolDefinition(def)
		name := strings.TrimSpace(normalized.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		category := normalized.Category
		if category == "" {
			category = "external"
		}
		out = append(out, fmt.Sprintf("%s [%s]", name, category))
		if len(out) == limit {
			break
		}
	}
	return out
}

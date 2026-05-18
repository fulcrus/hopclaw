package agent

import (
	"strings"
	"time"
)

func buildRunPreflightWithAnalysis(message string, analysis PreflightAnalysis) *RunPreflightReport {
	return buildRunPreflightWithContract(message, analysis, nil)
}

func buildRunPreflightWithContract(message string, analysis PreflightAnalysis, contract *TaskContract) *RunPreflightReport {
	message = strings.TrimSpace(message)
	checks := make([]RunPreflightCheck, 0, 5)
	analysis.SuggestedDomains = normalizeSemanticDomains(analysis.SuggestedDomains)
	analysis.DetectedDomains = normalizeSemanticDomains(analysis.DetectedDomains)

	if detail := preflightReferenceGapDetail(message, analysis); detail != "" {
		checks = append(checks, RunPreflightCheck{
			ID:       "reference_gap",
			Title:    "Need A Concrete Target",
			State:    RunPreflightNeedsConfirmation,
			Detail:   detail,
			Blocking: true,
		})
	}

	if detail := detectPreflightAutoPreparation(analysis.SuggestedDomains); detail != "" {
		checks = append(checks, RunPreflightCheck{
			ID:     "auto_prepare",
			Title:  "Automatic Preparation",
			State:  RunPreflightAutoPreparing,
			Detail: detail,
		})
	}

	if detail := detectPreflightExpectedConfirmation(analysis); detail != "" {
		checks = append(checks, RunPreflightCheck{
			ID:     "expected_confirmation",
			Title:  "Expected Confirmation",
			State:  RunPreflightNeedsConfirmation,
			Detail: detail,
		})
	}

	checks = append(checks, buildContractMissingInfoChecks(analysis.SuggestedDomains, checks, contract)...)
	checks = append(checks, buildDomainPreparedChecks(analysis.SuggestedDomains)...)

	if len(checks) == 0 {
		return &RunPreflightReport{
			State:            RunPreflightReady,
			Summary:          "Ready to execute.",
			SuggestedDomains: analysis.SuggestedDomains,
			DetectedDomains:  analysis.DetectedDomains,
			GeneratedAt:      time.Now().UTC(),
		}
	}

	report := &RunPreflightReport{
		State:            RunPreflightReady,
		Checks:           checks,
		SuggestedDomains: analysis.SuggestedDomains,
		DetectedDomains:  analysis.DetectedDomains,
		GeneratedAt:      time.Now().UTC(),
	}
	for _, check := range checks {
		if check.Blocking {
			report.Blocking = true
		}
		switch check.State {
		case RunPreflightNeedsConfirmation:
			report.State = RunPreflightNeedsConfirmation
		case RunPreflightAutoPreparing:
			if report.State == RunPreflightReady {
				report.State = RunPreflightAutoPreparing
			}
		}
	}
	report.Summary = summarizePreflightReport(report)
	report.ClarificationSlots = buildClarificationSlots(report, contract)
	report.Prompt = buildPreflightPrompt(report)
	report.Question = buildPreflightQuestion(report)
	report.ReplyHints = buildPreflightReplyHints(report)
	report.ReplyTemplate = buildPreflightReplyTemplate(report)
	report.ContinueHint = buildPreflightContinueHint(report)
	return report
}

func summarizePreflightReport(report *RunPreflightReport) string {
	if report == nil {
		return ""
	}
	switch report.State {
	case RunPreflightNeedsConfirmation:
		if report.Blocking {
			return "More information or confirmation is needed before the task is well-grounded."
		}
		return "The task will likely need confirmation during execution."
	case RunPreflightAutoPreparing:
		return "The system is preparing the required capabilities automatically."
	default:
		return "Ready to execute."
	}
}

func buildPreflightPrompt(report *RunPreflightReport) string {
	if report == nil {
		return ""
	}
	if report.State != RunPreflightNeedsConfirmation {
		return ""
	}
	for _, check := range report.Checks {
		switch check.ID {
		case "reference_gap":
			return "Please reply with the exact file path, URL, screenshot, repository, or other concrete target so I can continue."
		case "expected_confirmation":
			return "Please reply with confirmation or narrow the scope before I continue."
		case taskMissingInfoSourceTarget:
			return "Please reply with the exact file, URL, screenshot, repository, or other concrete target so I can continue."
		case taskMissingInfoDeliveryTarget:
			return "Please reply with the exact recipient, channel, webhook, or destination so I can continue."
		case taskMissingInfoSchedule:
			return "Please reply with when this should run and how often it should repeat."
		case taskMissingInfoDeploymentScope:
			return "Please reply with the exact environment, service, URL, or server I should deploy to."
		}
	}
	if len(report.ClarificationSlots) > 1 {
		return "Please reply with the missing fields below so I can continue the current task without restarting it."
	}
	if len(report.ClarificationSlots) == 1 {
		slot := report.ClarificationSlots[0]
		if question := strings.TrimSpace(slot.Question); question != "" {
			return "Please reply with: " + question
		}
		if label := strings.TrimSpace(slot.Label); label != "" {
			return "Please reply with the missing " + label + " so I can continue."
		}
	}
	return "Please reply with the missing information or confirmation so I can continue."
}

func buildPreflightQuestion(report *RunPreflightReport) string {
	if report == nil {
		return ""
	}
	switch report.State {
	case RunPreflightNeedsConfirmation:
		if len(report.ClarificationSlots) > 1 {
			return "Which missing fields can you fill now so I can continue?"
		}
		if len(report.ClarificationSlots) == 1 {
			slot := report.ClarificationSlots[0]
			if question := strings.TrimSpace(slot.Question); question != "" {
				return question
			}
		}
		for _, check := range report.Checks {
			switch check.ID {
			case "reference_gap":
				return preflightReferenceQuestion(report.SuggestedDomains)
			case "expected_confirmation":
				return "Should I continue with this action, or do you want to narrow the scope first?"
			case taskMissingInfoSourceTarget:
				return preflightReferenceQuestion(report.SuggestedDomains)
			case taskMissingInfoDeliveryTarget:
				return "Where should I send or post the result?"
			case taskMissingInfoSchedule:
				return "When should this run, and how often should it repeat?"
			case taskMissingInfoDeploymentScope:
				return "Which environment, service, URL, or server should I deploy to?"
			}
		}
		return "What should I use or confirm before I continue?"
	case RunPreflightAutoPreparing:
		return "I am preparing the required runtime capabilities."
	default:
		return ""
	}
}

func buildPreflightReplyHints(report *RunPreflightReport) []string {
	if report == nil {
		return nil
	}
	hints := make([]string, 0, 4)
	for _, slot := range report.ClarificationSlots {
		hints = append(hints, slot.Hints...)
	}
	for _, check := range report.Checks {
		switch check.ID {
		case "reference_gap":
			hints = append(hints, preflightReferenceHints(report.SuggestedDomains)...)
		case "expected_confirmation":
			hints = append(hints,
				"Confirm and continue",
				"Continue, but only change README.md",
			)
		case taskMissingInfoSourceTarget:
			hints = append(hints, preflightReferenceHints(report.SuggestedDomains)...)
		case taskMissingInfoDeliveryTarget:
			hints = append(hints,
				"发给 ceo@example.com",
				"发送到 Slack #ops",
				"回到当前会话即可",
			)
		case taskMissingInfoSchedule:
			hints = append(hints,
				"每天上午 9 点",
				"每周一 08:30",
				"从下周开始，每小时一次",
			)
		case taskMissingInfoDeploymentScope:
			hints = append(hints,
				"部署到 staging",
				"发布到 https://app.example.com",
				"推到生产集群 web-api",
			)
		}
	}
	if len(hints) == 0 {
		return nil
	}
	return dedupePreflightHints(hints)
}

func buildPreflightReplyTemplate(report *RunPreflightReport) string {
	if report == nil || len(report.ClarificationSlots) == 0 {
		return ""
	}
	lines := make([]string, 0, len(report.ClarificationSlots))
	for _, slot := range report.ClarificationSlots {
		id := strings.TrimSpace(slot.ID)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(slot.Label)
		if label == "" {
			label = preflightMissingInfoTitle(id)
		}
		placeholder := strings.TrimSpace(slot.Placeholder)
		if placeholder == "" {
			placeholder = "<请补充>"
		}
		if label != "" && label != id {
			lines = append(lines, id+" ("+label+"): "+placeholder)
			continue
		}
		lines = append(lines, id+": "+placeholder)
	}
	return strings.Join(lines, "\n")
}

func buildPreflightContinueHint(report *RunPreflightReport) string {
	if report == nil {
		return ""
	}
	switch report.State {
	case RunPreflightNeedsConfirmation:
		return "After you reply, I will continue the current task instead of starting over, prepare the needed capabilities, and verify the result before sending it."
	case RunPreflightAutoPreparing:
		return "The system is preparing runtime support now, will continue automatically, and will verify the expected output before reporting success."
	default:
		return ""
	}
}

func buildClarificationSlots(report *RunPreflightReport, contract *TaskContract) []RunClarificationSlot {
	if report == nil || report.State != RunPreflightNeedsConfirmation {
		return nil
	}
	slots := make([]RunClarificationSlot, 0, 4)
	appendSlot := func(slot RunClarificationSlot) {
		slot.ID = strings.TrimSpace(slot.ID)
		if slot.ID == "" {
			return
		}
		for _, existing := range slots {
			if existing.ID == slot.ID {
				return
			}
		}
		if strings.TrimSpace(slot.Label) == "" {
			slot.Label = taskMissingInfoLabel(slot.ID)
		}
		if strings.TrimSpace(slot.InputMode) == "" {
			slot.InputMode = taskMissingInfoInputMode(slot.ID)
		}
		if strings.TrimSpace(slot.Placeholder) == "" {
			slot.Placeholder = taskMissingInfoPlaceholder(slot.ID)
		}
		slot.Hints = dedupePreflightHints(slot.Hints)
		slots = append(slots, slot)
	}

	if contract != nil {
		for _, item := range contract.MissingInfo {
			appendSlot(RunClarificationSlot{
				ID:          item.ID,
				Label:       item.Label,
				Question:    item.Question,
				InputMode:   item.InputMode,
				Placeholder: item.Placeholder,
				Required:    item.Required,
				Hints:       append([]string(nil), item.Hints...),
			})
		}
	}

	if len(slots) == 0 {
		for _, check := range report.Checks {
			switch check.ID {
			case "reference_gap", taskMissingInfoSourceTarget:
				appendSlot(RunClarificationSlot{
					ID:          taskMissingInfoSourceTarget,
					Label:       taskMissingInfoLabel(taskMissingInfoSourceTarget),
					Question:    preflightReferenceQuestion(report.SuggestedDomains),
					InputMode:   taskMissingInfoInputMode(taskMissingInfoSourceTarget),
					Placeholder: taskMissingInfoPlaceholder(taskMissingInfoSourceTarget),
					Required:    check.Blocking,
					Hints:       preflightReferenceHints(report.SuggestedDomains),
				})
			case taskMissingInfoDeliveryTarget:
				appendSlot(RunClarificationSlot{
					ID:          taskMissingInfoDeliveryTarget,
					Label:       taskMissingInfoLabel(taskMissingInfoDeliveryTarget),
					Question:    "Where should I send or post the result?",
					InputMode:   taskMissingInfoInputMode(taskMissingInfoDeliveryTarget),
					Placeholder: taskMissingInfoPlaceholder(taskMissingInfoDeliveryTarget),
					Required:    check.Blocking,
					Hints:       []string{"发给 ceo@example.com", "发送到 Slack #ops", "回到当前会话即可"},
				})
			case taskMissingInfoSchedule:
				appendSlot(RunClarificationSlot{
					ID:          taskMissingInfoSchedule,
					Label:       taskMissingInfoLabel(taskMissingInfoSchedule),
					Question:    "When should this run, and how often should it repeat?",
					InputMode:   taskMissingInfoInputMode(taskMissingInfoSchedule),
					Placeholder: taskMissingInfoPlaceholder(taskMissingInfoSchedule),
					Required:    check.Blocking,
					Hints:       []string{"每天上午 9 点", "每周一 08:30", "从下周开始，每小时一次"},
				})
			case taskMissingInfoDeploymentScope:
				appendSlot(RunClarificationSlot{
					ID:          taskMissingInfoDeploymentScope,
					Label:       taskMissingInfoLabel(taskMissingInfoDeploymentScope),
					Question:    "Which environment, service, URL, or server should I deploy to?",
					InputMode:   taskMissingInfoInputMode(taskMissingInfoDeploymentScope),
					Placeholder: taskMissingInfoPlaceholder(taskMissingInfoDeploymentScope),
					Required:    check.Blocking,
					Hints:       []string{"部署到 staging", "发布到 https://app.example.com", "推到生产集群 web-api"},
				})
			}
		}
	}
	if len(slots) == 0 {
		return nil
	}
	return slots
}

func buildContractMissingInfoChecks(domains []string, existing []RunPreflightCheck, contract *TaskContract) []RunPreflightCheck {
	if contract == nil || len(contract.MissingInfo) == 0 {
		return nil
	}
	checks := make([]RunPreflightCheck, 0, len(contract.MissingInfo))
	for _, item := range contract.MissingInfo {
		switch item.ID {
		case taskMissingInfoSourceTarget:
			if preflightChecksContain(existing, "reference_gap") {
				continue
			}
		case taskMissingInfoDeliveryTarget, taskMissingInfoSchedule, taskMissingInfoDeploymentScope:
		default:
			continue
		}
		detail := strings.TrimSpace(item.Summary)
		if detail == "" && item.ID == taskMissingInfoSourceTarget {
			detail = preflightReferenceGapDetail("", PreflightAnalysis{
				NeedsReference:   true,
				SuggestedDomains: domains,
			})
		}
		if detail == "" {
			continue
		}
		checks = append(checks, RunPreflightCheck{
			ID:       item.ID,
			Title:    preflightMissingInfoTitle(item.ID),
			State:    RunPreflightNeedsConfirmation,
			Detail:   detail,
			Blocking: item.Required && preflightMissingInfoBlocksExecution(item.ID),
		})
	}
	return checks
}

func preflightMissingInfoBlocksExecution(id string) bool {
	switch strings.TrimSpace(id) {
	case taskMissingInfoSourceTarget, taskMissingInfoSchedule:
		return false
	case taskMissingInfoDeliveryTarget, taskMissingInfoDeploymentScope:
		return true
	default:
		return true
	}
}

func preflightChecksContain(checks []RunPreflightCheck, id string) bool {
	id = strings.TrimSpace(id)
	for _, check := range checks {
		if strings.TrimSpace(check.ID) == id {
			return true
		}
	}
	return false
}

func preflightMissingInfoTitle(id string) string {
	switch strings.TrimSpace(id) {
	case taskMissingInfoSourceTarget:
		return "Need A Concrete Target"
	case taskMissingInfoDeliveryTarget:
		return "Need A Delivery Destination"
	case taskMissingInfoSchedule:
		return "Need A Schedule"
	case taskMissingInfoDeploymentScope:
		return "Need A Deployment Target"
	default:
		return "Need More Information"
	}
}

func dedupePreflightHints(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func preflightReferenceGapDetail(message string, analysis PreflightAnalysis) string {
	if !analysis.NeedsReference {
		return ""
	}
	if hasConcreteSourceReference(message, analysis.SuggestedDomains) {
		return ""
	}
	return "The request depends on an existing file, URL, screenshot, repository, or other concrete target, but no concrete reference was included."
}

func containsConcreteReference(message string) bool {
	if strings.Contains(message, "http://") || strings.Contains(message, "https://") || strings.Contains(message, "artifact://") {
		return true
	}
	markers := []string{"/", "\\", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".html", ".csv", ".md", ".txt", ".json", ".yaml", ".yml", ".go", ".js", ".ts", ".py", ".java", ".tsx", ".docx", ".pptx", ".xlsx", "@", ".pdf"}
	for _, marker := range markers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func detectPreflightAutoPreparation(domains []string) string {
	if len(domains) == 0 {
		return ""
	}
	autoDomains := []ToolDomain{DomainBrowser, DomainDesktop, DomainCanvas, DomainPDF, DomainMedia, DomainVision, DomainNodes, DomainSpeech}
	labels := make([]string, 0, len(autoDomains))
	activated := make(map[ToolDomain]bool, len(domains))
	for _, item := range domains {
		activated[ToolDomain(strings.TrimSpace(item))] = true
	}
	for _, domain := range autoDomains {
		if !activated[domain] {
			continue
		}
		switch domain {
		case DomainBrowser:
			labels = append(labels, "browser")
		case DomainDesktop:
			labels = append(labels, "desktop")
		case DomainCanvas:
			labels = append(labels, "canvas")
		case DomainPDF:
			labels = append(labels, "pdf")
		case DomainMedia:
			labels = append(labels, "media")
		case DomainVision:
			labels = append(labels, "vision")
		case DomainNodes:
			labels = append(labels, "desktop")
		case DomainSpeech:
			labels = append(labels, "speech")
		}
	}
	if len(labels) == 0 {
		return ""
	}
	return "Preparing runtime support for " + strings.Join(labels, ", ") + " and the verification steps needed for a reliable result."
}

func detectPreflightExpectedConfirmation(analysis PreflightAnalysis) string {
	if !analysis.NeedsConfirmation {
		return ""
	}
	return "This task is likely to request confirmation before changing files, apps, or external systems."
}

func buildDomainPreparedChecks(domains []string) []RunPreflightCheck {
	if len(domains) == 0 {
		return nil
	}
	checks := make([]RunPreflightCheck, 0, 2)
	if detail := buildExpectedOutputDetail(domains); detail != "" {
		checks = append(checks, RunPreflightCheck{
			ID:     "expected_output",
			Title:  "Expected Output",
			State:  RunPreflightReady,
			Detail: detail,
		})
	}
	if detail := buildVerificationPlanDetail(domains); detail != "" {
		checks = append(checks, RunPreflightCheck{
			ID:     "verification_plan",
			Title:  "Verification Plan",
			State:  RunPreflightReady,
			Detail: detail,
		})
	}
	if len(checks) == 0 {
		return nil
	}
	return checks
}

func preflightReferenceQuestion(domains []string) string {
	for _, domain := range normalizeSemanticDomains(domains) {
		switch ToolDomain(domain) {
		case DomainBrowser:
			return "Which exact URL or webpage should I use?"
		case DomainDesktop:
			return "Which app, window, or screenshot should I operate on?"
		case DomainSheet:
			return "Which spreadsheet file, sheet, or export target should I use?"
		case DomainDocument:
			return "Which document file or source should I use?"
		case DomainPresentation:
			return "Which presentation file or slide source should I use?"
		case DomainEmail:
			return "Which mailbox, recipient, or email thread should I use?"
		case DomainWatch:
			return "Which page, feed, mailbox, or target should I monitor?"
		}
	}
	return "What exact file, URL, screenshot, repository, or target should I use?"
}

func preflightReferenceHints(domains []string) []string {
	hints := make([]string, 0, 6)
	for _, domain := range normalizeSemanticDomains(domains) {
		switch ToolDomain(domain) {
		case DomainBrowser:
			hints = append(hints,
				"https://example.com/page",
				"https://docs.example.com/report",
			)
		case DomainDesktop:
			hints = append(hints,
				"Safari 登录窗口",
				"artifact://local/screenshot-1",
			)
		case DomainSheet:
			hints = append(hints,
				"/tmp/report.xlsx",
				"/tmp/data.csv",
			)
		case DomainDocument:
			hints = append(hints,
				"/tmp/brief.docx",
				"/tmp/notes.md",
			)
		case DomainPresentation:
			hints = append(hints,
				"/tmp/pitch.pptx",
				"/tmp/slides-outline.md",
			)
		case DomainEmail:
			hints = append(hints,
				"发给 ceo@example.com",
				"INBOX 里主题含 invoice 的邮件",
			)
		case DomainWatch:
			hints = append(hints,
				"https://example.com/pricing",
				"每小时检查一次这个页面",
			)
		}
	}
	hints = append(hints,
		"/tmp/demo.txt",
		"artifact://local/blob-1",
		"github.com/owner/repo",
	)
	return dedupePreflightHints(hints)
}

func buildExpectedOutputDetail(domains []string) string {
	parts := make([]string, 0, 3)
	add := func(text string) {
		if text != "" {
			parts = append(parts, text)
		}
	}
	seen := make(map[ToolDomain]struct{}, len(domains))
	for _, item := range normalizeSemanticDomains(domains) {
		domain := ToolDomain(item)
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		switch domain {
		case DomainBrowser:
			add("Expect a browsed page result such as a screenshot, DOM snapshot, or extracted page data.")
		case DomainDesktop:
			add("Expect desktop evidence such as a screenshot, UI tree snapshot, or app/window state.")
		case DomainSheet:
			add("Expect a sheet update, export file, or structured table output.")
		case DomainDocument:
			add("Expect a document file, extracted metadata, or search/read result.")
		case DomainPresentation:
			add("Expect slide metadata, a generated deck, or presentation file evidence.")
		case DomainEmail:
			add("Expect send/read/search receipts or attachment output.")
		case DomainWatch:
			add("Expect a notification payload, report, or changed-state summary.")
		}
	}
	return strings.Join(parts, " ")
}

func buildVerificationPlanDetail(domains []string) string {
	parts := make([]string, 0, 3)
	add := func(text string) {
		if text != "" {
			parts = append(parts, text)
		}
	}
	seen := make(map[ToolDomain]struct{}, len(domains))
	for _, item := range normalizeSemanticDomains(domains) {
		domain := ToolDomain(item)
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		switch domain {
		case DomainBrowser:
			add("I will check that browser actions produced navigation, page state, or artifact evidence before reporting success.")
		case DomainDesktop:
			add("I will check that desktop actions produced UI state, screenshot, or accessibility evidence before reporting success.")
		case DomainSheet:
			add("I will check for sheet ranges, sheet metadata, or spreadsheet deliverables before reporting success.")
		case DomainDocument:
			add("I will check for document metadata, search/read evidence, or document deliverables before reporting success.")
		case DomainPresentation:
			add("I will check for slide metadata or presentation deliverables before reporting success.")
		case DomainEmail:
			add("I will check for delivery, inbox, or attachment evidence before reporting success.")
		case DomainWatch:
			add("I will check that the run produced a final notification payload before reporting success.")
		}
	}
	return strings.Join(parts, " ")
}

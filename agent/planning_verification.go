package agent

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	planpkg "github.com/fulcrus/hopclaw/planner"
)

var verificationHintPriority = map[string]int{
	"browser":      5,
	"desktop":      7,
	"spreadsheet":  10,
	"document":     20,
	"presentation": 30,
	"email":        40,
	"watch":        50,
}

func enrichPlanVerificationHints(plan *planpkg.Plan, latestMessage, sessionSummary string) {
	if plan == nil || len(plan.Tasks) == 0 {
		return
	}

	planContext := strings.Join(nonEmptyPlanStrings(plan.Goal, latestMessage, sessionSummary), "\n")
	hintsByTask := make(map[string][]string, len(plan.Tasks))

	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		context := ""
		if task.Kind == planpkg.TaskDeliver || strings.TrimSpace(task.ID) == strings.TrimSpace(plan.FinalTask) {
			context = planContext
		}
		hintsByTask[task.ID] = inferTaskVerificationHints(*task, context)
	}

	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		if task.Kind != planpkg.TaskDeliver && strings.TrimSpace(task.ID) != strings.TrimSpace(plan.FinalTask) {
			continue
		}
		hints := append([]string(nil), hintsByTask[task.ID]...)
		for _, dep := range task.DependsOn {
			hints = appendUniqueVerificationHints(hints, hintsByTask[dep]...)
		}
		hintsByTask[task.ID] = normalizeVerificationHints(hints)
	}

	for i := range plan.Tasks {
		plan.Tasks[i].VerificationHints = normalizeVerificationHints(hintsByTask[plan.Tasks[i].ID])
	}
}

func inferTaskVerificationHints(task planpkg.Task, extraContext string) []string {
	hints := normalizeVerificationHints(task.VerificationHints)

	for _, output := range task.Outputs {
		switch outputVerificationHint(output) {
		case "":
		default:
			hints = appendUniqueVerificationHints(hints, outputVerificationHint(output))
		}
	}

	for _, capability := range task.RequiredCapabilities {
		switch capabilityVerificationHint(capability) {
		case "":
		default:
			hints = appendUniqueVerificationHints(hints, capabilityVerificationHint(capability))
		}
	}

	text := strings.ToLower(strings.Join(nonEmptyPlanStrings(task.Title, task.Goal, extraContext), "\n"))
	if text != "" {
		domains := detectStructuredEvidence(text)
		if domains[DomainSheet] {
			hints = appendUniqueVerificationHints(hints, "spreadsheet")
		}
		if domains[DomainBrowser] {
			hints = appendUniqueVerificationHints(hints, "browser")
		}
		if domains[DomainDesktop] {
			hints = appendUniqueVerificationHints(hints, "desktop")
		}
		if domains[DomainDocument] {
			hints = appendUniqueVerificationHints(hints, "document")
		}
		if domains[DomainPresentation] {
			hints = appendUniqueVerificationHints(hints, "presentation")
		}
		if domains[DomainEmail] {
			hints = appendUniqueVerificationHints(hints, "email")
		}
		if domains[DomainWatch] || domains[DomainCron] {
			hints = appendUniqueVerificationHints(hints, "watch")
		}
	}

	return normalizeVerificationHints(hints)
}

func outputVerificationHint(output string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(output)))
	switch ext {
	case ".csv", ".tsv", ".xlsx", ".xls", ".ods":
		return "spreadsheet"
	case ".docx", ".doc", ".rtf", ".odt":
		return "document"
	case ".pptx", ".ppt", ".key":
		return "presentation"
	default:
		return ""
	}
}

func capabilityVerificationHint(capability string) string {
	capability = strings.ToLower(strings.TrimSpace(capability))
	switch {
	case strings.HasPrefix(capability, "browser.") || strings.Contains(capability, "browser"):
		return "browser"
	case strings.HasPrefix(capability, "desktop.") || strings.Contains(capability, "desktop"):
		return "desktop"
	case strings.HasPrefix(capability, "spreadsheet.") || strings.Contains(capability, "spreadsheet"):
		return "spreadsheet"
	case strings.HasPrefix(capability, "document.") || strings.Contains(capability, "document"):
		return "document"
	case strings.HasPrefix(capability, "presentation.") || strings.Contains(capability, "presentation"):
		return "presentation"
	case strings.HasPrefix(capability, "email.") || strings.Contains(capability, "email"):
		return "email"
	case strings.HasPrefix(capability, "watch.") || strings.HasPrefix(capability, "cron.") || strings.Contains(capability, "watch"):
		return "watch"
	default:
		return ""
	}
}

func normalizeVerificationHints(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
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
	sort.Slice(out, func(i, j int) bool {
		pi, okI := verificationHintPriority[out[i]]
		pj, okJ := verificationHintPriority[out[j]]
		switch {
		case okI && okJ && pi != pj:
			return pi < pj
		case okI != okJ:
			return okI
		default:
			return out[i] < out[j]
		}
	})
	return out
}

func appendUniqueVerificationHints(dst []string, items ...string) []string {
	return normalizeVerificationHints(append(dst, items...))
}

func nonEmptyPlanStrings(items ...string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func applyPlanCoverageWarnings(plan *planpkg.Plan, contract *TaskContract) []string {
	if plan == nil {
		return nil
	}
	plan.CoverageWarnings = validatePlanCoverage(plan, contract)
	return cloneStrings(plan.CoverageWarnings)
}

func validatePlanCoverage(plan *planpkg.Plan, contract *TaskContract) []string {
	if plan == nil || contract == nil || len(contract.ExpectedDeliverables) == 0 {
		return nil
	}
	warnings := make([]string, 0, len(contract.ExpectedDeliverables))
	for _, deliverable := range contract.ExpectedDeliverables {
		if !deliverable.Required {
			continue
		}
		if planCoversDeliverable(plan, deliverable) {
			continue
		}
		description := strings.TrimSpace(deliverable.Kind)
		if summary := strings.TrimSpace(deliverable.Summary); summary != "" {
			description = strings.TrimSpace(description + ": " + summary)
		}
		if description == "" {
			description = "required deliverable"
		}
		warnings = append(warnings, fmt.Sprintf("deliverable %q is not clearly covered by any plan task", description))
	}
	return dedupePlanCoverageWarnings(warnings)
}

func planCoversDeliverable(plan *planpkg.Plan, deliverable TaskContractDeliverable) bool {
	if plan == nil {
		return false
	}
	finalTaskID := strings.TrimSpace(plan.FinalTask)
	if finalTaskID == "" && len(plan.Tasks) > 0 {
		finalTaskID = strings.TrimSpace(plan.Tasks[len(plan.Tasks)-1].ID)
	}
	for _, task := range plan.Tasks {
		if taskCoversDeliverable(task, deliverable, finalTaskID) {
			return true
		}
	}
	return false
}

func taskCoversDeliverable(task planpkg.Task, deliverable TaskContractDeliverable, finalTaskID string) bool {
	coverage := buildTaskDeliverableCoverage(task, finalTaskID)
	kind := strings.ToLower(strings.TrimSpace(deliverable.Kind))
	switch kind {
	case "", taskDeliverableSummary:
		return coverage.isFinal || coverage.isDeliverTask
	case taskDeliverableDocument:
		return coverage.hasInferredHint("document")
	case taskDeliverableSpreadsheet:
		return coverage.hasInferredHint("spreadsheet")
	case taskDeliverablePresentation:
		return coverage.hasInferredHint("presentation")
	case taskDeliverableBrowserEvidence:
		return coverage.hasInferredHint("browser")
	case taskDeliverableDesktopEvidence:
		return coverage.hasInferredHint("desktop")
	case taskDeliverableMessageDelivery:
		return coverage.isDeliverTask ||
			coverage.hasExplicitHint("email") ||
			coverage.hasExplicitHint("delivery") ||
			coverage.hasStructuredDeliveryTarget ||
			coverage.hasCapability(taskCoverageCapabilityMatch{
				exact: []string{
					"email.send",
					"channel.send",
					"channel.reply",
					"channel.post",
					"calendar.create_event",
					"calendar.create_ics",
				},
			})
	case taskDeliverableWatchAlert:
		return coverage.hasInferredHint("watch") ||
			coverage.hasStructuredSchedule ||
			coverage.hasCapability(taskCoverageCapabilityMatch{
				exact: []string{
					"watch",
					"cron",
				},
				prefixes: []string{
					"watch.",
					"cron.",
				},
				segments: []string{
					"watch",
					"cron",
				},
			})
	case taskDeliverableDeployment:
		return coverage.hasExplicitHint("deployment") ||
			coverage.hasStructuredDeploymentTarget ||
			coverage.hasCapability(taskCoverageCapabilityMatch{
				exact: []string{
					"deploy",
					"deployment",
				},
				prefixes: []string{
					"deploy.",
					"deployment.",
				},
				segments: []string{
					"deploy",
					"deployment",
				},
			})
	default:
		return false
	}
}

type taskDeliverableCoverage struct {
	isFinal                       bool
	isDeliverTask                 bool
	hasStructuredDeliveryTarget   bool
	hasStructuredSchedule         bool
	hasStructuredDeploymentTarget bool
	explicitHints                 map[string]struct{}
	inferredHints                 map[string]struct{}
	requiredCapabilities          []string
}

type taskCoverageCapabilityMatch struct {
	exact    []string
	prefixes []string
	segments []string
}

func buildTaskDeliverableCoverage(task planpkg.Task, finalTaskID string) taskDeliverableCoverage {
	coverage := taskDeliverableCoverage{
		isFinal:              strings.TrimSpace(task.ID) != "" && strings.TrimSpace(task.ID) == strings.TrimSpace(finalTaskID),
		isDeliverTask:        task.Kind == planpkg.TaskDeliver,
		explicitHints:        taskCoverageHintSet(normalizeVerificationHints(task.VerificationHints)),
		inferredHints:        taskCoverageHintSet(inferTaskVerificationHints(task, "")),
		requiredCapabilities: normalizeTaskCoverageCapabilities(task.RequiredCapabilities),
	}

	for _, output := range task.Outputs {
		output = strings.TrimSpace(output)
		if output == "" {
			continue
		}
		if taskCoverageHasStructuredDeliveryTarget(output) {
			coverage.hasStructuredDeliveryTarget = true
		}
		if taskCoverageHasStructuredSchedule(output) {
			coverage.hasStructuredSchedule = true
		}
		if taskCoverageHasStructuredDeploymentTarget(output) {
			coverage.hasStructuredDeploymentTarget = true
		}
	}

	return coverage
}

func taskCoverageHintSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		out[item] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeTaskCoverageCapabilities(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
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

func (c taskDeliverableCoverage) hasExplicitHint(value string) bool {
	return taskCoverageHintPresent(c.explicitHints, value)
}

func (c taskDeliverableCoverage) hasInferredHint(value string) bool {
	return taskCoverageHintPresent(c.inferredHints, value)
}

func taskCoverageHintPresent(set map[string]struct{}, value string) bool {
	if len(set) == 0 {
		return false
	}
	_, ok := set[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

func (c taskDeliverableCoverage) hasCapability(match taskCoverageCapabilityMatch) bool {
	return taskCoverageHasCapability(c.requiredCapabilities, match)
}

func taskCoverageHasCapability(capabilities []string, match taskCoverageCapabilityMatch) bool {
	if len(capabilities) == 0 {
		return false
	}
	for _, capability := range capabilities {
		if taskCoverageCapabilityMatches(capability, match) {
			return true
		}
	}
	return false
}

func taskCoverageCapabilityMatches(capability string, match taskCoverageCapabilityMatch) bool {
	capability = strings.ToLower(strings.TrimSpace(capability))
	if capability == "" {
		return false
	}
	for _, exact := range match.exact {
		if capability == strings.ToLower(strings.TrimSpace(exact)) {
			return true
		}
	}
	for _, prefix := range match.prefixes {
		prefix = strings.ToLower(strings.TrimSpace(prefix))
		if prefix == "" {
			continue
		}
		if strings.HasPrefix(capability, prefix) {
			return true
		}
	}
	if len(match.segments) == 0 {
		return false
	}
	for _, token := range strings.FieldsFunc(capability, func(r rune) bool {
		switch r {
		case '.', '_', '-', ':', '/', '\\':
			return true
		default:
			return false
		}
	}) {
		for _, segment := range match.segments {
			if token == strings.ToLower(strings.TrimSpace(segment)) {
				return true
			}
		}
	}
	return false
}

func taskCoverageHasStructuredDeliveryTarget(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	return taskContractEmailPattern.MatchString(trimmed) ||
		taskContractChannelPattern.MatchString(trimmed) ||
		taskContractMentionPattern.MatchString(trimmed) ||
		strings.Contains(trimmed, "http://") ||
		strings.Contains(trimmed, "https://")
}

func taskCoverageHasStructuredSchedule(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	return taskContractCronPattern.MatchString(trimmed) ||
		taskContractTimePattern.MatchString(trimmed) ||
		taskContractISODatePattern.MatchString(trimmed) ||
		taskContractSlashDatePattern.MatchString(trimmed)
}

func taskCoverageHasStructuredDeploymentTarget(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if taskContractURLPattern.MatchString(trimmed) ||
		taskContractArtifactPattern.MatchString(trimmed) ||
		taskContractIPPattern.MatchString(trimmed) {
		return true
	}
	for _, token := range evidenceTokenPattern.FindAllString(strings.ToLower(trimmed), -1) {
		token = strings.Trim(token, ".,;:!?()[]{}<>\"'")
		if token == "" || strings.Contains(token, "/") || strings.Contains(token, `\`) {
			continue
		}
		if looksLikeLocalFileToken(token) {
			continue
		}
		if taskContractHostPattern.MatchString(token) {
			return true
		}
	}
	return false
}

func dedupePlanCoverageWarnings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
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
	sort.Strings(out)
	return out
}

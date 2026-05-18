package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
)

const (
	memoryProfileScopeUser     = "user"
	memoryMemorySourceSubmit   = "runtime.submit"
	memoryMemorySourceContract = "runtime.task_contract"
	memoryEvidenceSummaryLimit = 160
)

var (
	memoryPathPattern                 = regexp.MustCompile(`(?:/[\w.\-~/]+|[A-Za-z]:\\[\w.\-\\]+)`)
	memoryStructuredAssignmentPattern = regexp.MustCompile(`(?mi)(?:^|[\n;,])\s*([A-Za-z_][A-Za-z0-9_-]{0,31})\s*[:=]\s*("[^"\n]*"|'[^'\n]*'|[^\n;,]+)`)
	memoryLocalePattern               = regexp.MustCompile(`(?i)^[a-z]{2,3}(?:-[a-z0-9]{2,8})*$`)
	memoryProfileNamePattern          = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} .'\-_]{0,47}$`)
)

func (s *Service) captureSubmissionMemory(ctx context.Context, sessionKey, message string, run *agent.Run) error {
	if s.memory == nil || run == nil {
		return nil
	}
	managed, ok := s.memory.(agent.ManagedMemoryStore)
	if !ok {
		return nil
	}
	records := append(extractProfileMemory(message), extractWorkspaceAndProjectMemory(sessionKey, message)...)
	records = append(records, extractTaskMemory(run)...)
	if len(records) == 0 {
		return nil
	}
	for _, record := range records {
		if _, err := managed.UpsertRecord(ctx, record); err != nil {
			return fmt.Errorf("upsert memory record %q: %w", record.Key, err)
		}
	}
	return nil
}

func extractProfileMemory(message string) []agent.MemoryRecord {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return nil
	}
	records := make([]agent.MemoryRecord, 0, 4)
	appendRecord := func(field, label, value string, score float64, tags ...string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		records = append(records, agent.MemoryRecord{
			Namespace: "profile",
			ScopeKey:  memoryProfileScopeUser,
			Field:     field,
			Label:     label,
			Value:     value,
			Source:    memoryMemorySourceSubmit,
			Score:     score,
			Tags:      tags,
			Evidence: []agent.MemoryRecordEvidence{{
				Source:     memoryMemorySourceSubmit,
				Summary:    truncateForEvidence(trimmed),
				Value:      value,
				ObservedAt: time.Now().UTC(),
			}},
		})
	}

	values := extractStructuredProfileValues(trimmed)
	if value := normalizeProfileName(values["name"]); value != "" {
		appendRecord("name", "User Name", value, 0.93, "identity")
	}
	if value := normalizeProfileReplyLanguage(values["reply_language"]); value != "" {
		appendRecord("reply_language", "Reply Language", value, 0.88, "preference", "language")
	}
	if value := normalizeProfileResponseStyle(values["response_style"]); value != "" {
		appendRecord("response_style", "Response Style", value, 0.82, "preference", "style")
	}
	return records
}

func extractWorkspaceAndProjectMemory(sessionKey, message string) []agent.MemoryRecord {
	scopeKey := strings.TrimSpace(sessionKey)
	if scopeKey == "" {
		scopeKey = "default"
	}
	paths := memoryPathPattern.FindAllString(message, -1)
	records := make([]agent.MemoryRecord, 0, 4)
	seen := make(map[string]struct{}, 4)
	appendRecord := func(namespace, field, label, value string, score float64, source string, tags ...string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		dedupeKey := namespace + "|" + field + "|" + value
		if _, ok := seen[dedupeKey]; ok {
			return
		}
		seen[dedupeKey] = struct{}{}
		records = append(records, agent.MemoryRecord{
			Namespace: namespace,
			ScopeKey:  scopeKey,
			Field:     field,
			Label:     label,
			Value:     value,
			Source:    source,
			Score:     score,
			Tags:      tags,
			Evidence: []agent.MemoryRecordEvidence{{
				Source:     source,
				Summary:    truncateForEvidence(message),
				Value:      value,
				ObservedAt: time.Now().UTC(),
			}},
		})
	}
	appendRecord("workspace", "session_key", "Workspace Session", scopeKey, 0.95, memoryMemorySourceSubmit, "workspace")
	if len(paths) > 0 {
		path := strings.TrimSpace(paths[0])
		appendRecord("workspace", "primary_path", "Primary Path", path, 0.90, memoryMemorySourceSubmit, "workspace", "path")
		base := filepath.Base(path)
		if base != "." && base != string(filepath.Separator) {
			appendRecord("project", "name", "Project Name", base, 0.76, memoryMemorySourceSubmit, "project")
			appendRecord("project", "root_hint", "Project Root Hint", path, 0.74, memoryMemorySourceSubmit, "project", "path")
		}
	}
	return records
}

func extractStructuredProfileValues(message string) map[string]string {
	values := make(map[string]string, 4)
	if parsed := extractStructuredProfileJSON(message); len(parsed) > 0 {
		for key, value := range parsed {
			values[key] = value
		}
	}
	for _, match := range memoryStructuredAssignmentPattern.FindAllStringSubmatch(message, -1) {
		if len(match) != 3 {
			continue
		}
		key := normalizeStructuredProfileKey(match[1])
		if key == "" {
			continue
		}
		values[key] = stripQuotedMemoryValue(match[2])
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func extractStructuredProfileJSON(message string) map[string]string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil
	}
	values := make(map[string]string, len(payload))
	for rawKey, rawValue := range payload {
		key := normalizeStructuredProfileKey(rawKey)
		if key == "" {
			continue
		}
		value, ok := rawValue.(string)
		if !ok {
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func normalizeStructuredProfileKey(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "name", "user_name", "display_name":
		return "name"
	case "reply_language", "language", "locale":
		return "reply_language"
	case "response_style", "style":
		return "response_style"
	default:
		return ""
	}
}

func stripQuotedMemoryValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return strings.TrimSpace(value)
}

func normalizeProfileName(value string) string {
	value = stripQuotedMemoryValue(value)
	if !memoryProfileNamePattern.MatchString(value) {
		return ""
	}
	return value
}

func normalizeProfileReplyLanguage(value string) string {
	value = stripQuotedMemoryValue(value)
	if !memoryLocalePattern.MatchString(value) {
		return ""
	}
	parts := strings.Split(strings.ReplaceAll(value, "_", "-"), "-")
	if len(parts) == 0 {
		return ""
	}
	parts[0] = strings.ToLower(parts[0])
	for i := 1; i < len(parts); i++ {
		switch len(parts[i]) {
		case 2:
			parts[i] = strings.ToUpper(parts[i])
		case 4:
			parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
		default:
			parts[i] = strings.ToLower(parts[i])
		}
	}
	return strings.Join(parts, "-")
}

func normalizeProfileResponseStyle(value string) string {
	switch strings.ToLower(stripQuotedMemoryValue(value)) {
	case "concise", "detailed":
		return strings.ToLower(stripQuotedMemoryValue(value))
	default:
		return ""
	}
}

func extractTaskMemory(run *agent.Run) []agent.MemoryRecord {
	if run == nil || run.TaskContract == nil {
		return nil
	}
	contract := run.TaskContract
	scopeKey := strings.TrimSpace(run.ID)
	if scopeKey == "" {
		scopeKey = "task"
	}
	records := make([]agent.MemoryRecord, 0, 6)
	appendRecord := func(field, label, value string, score float64, tags ...string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		records = append(records, agent.MemoryRecord{
			Namespace: "task",
			ScopeKey:  scopeKey,
			Field:     field,
			Label:     label,
			Value:     value,
			Source:    memoryMemorySourceContract,
			Score:     score,
			Tags:      tags,
			Evidence: []agent.MemoryRecordEvidence{{
				Source:     memoryMemorySourceContract,
				Ref:        run.ID,
				Summary:    truncateForEvidence(contract.Goal),
				Value:      value,
				ObservedAt: time.Now().UTC(),
			}},
		})
	}
	appendRecord("goal", "Task Goal", contract.Goal, contract.Confidence, "task", "goal")
	appendRecord("target_summary", "Target Summary", contract.TargetSummary, contract.Confidence, "task", "target")
	appendRecord("job_type", "Job Type", contract.JobType, contract.Confidence, "task")
	if summary := joinContractDeliverables(contract.ExpectedDeliverables); summary != "" {
		appendRecord("deliverables", "Expected Deliverables", summary, contract.Confidence, "task", "deliverables")
	}
	if summary := joinContractAcceptance(contract.AcceptanceCriteria); summary != "" {
		appendRecord("acceptance", "Acceptance", summary, contract.Confidence, "task", "acceptance")
	}
	if summary := joinContractMissing(contract.MissingInfo); summary != "" {
		appendRecord("missing_info", "Missing Info", summary, contract.Confidence, "task", "clarification")
	}
	return records
}

func joinContractDeliverables(items []agent.TaskContractDeliverable) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Summary) == "" {
			continue
		}
		parts = append(parts, item.Kind+": "+strings.TrimSpace(item.Summary))
	}
	return strings.Join(parts, "\n")
}

func joinContractAcceptance(items []agent.TaskContractAcceptance) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Summary) == "" {
			continue
		}
		parts = append(parts, item.ID+": "+strings.TrimSpace(item.Summary))
	}
	return strings.Join(parts, "\n")
}

func joinContractMissing(items []agent.TaskContractMissingInfo) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Label) == "" && strings.TrimSpace(item.Summary) == "" {
			continue
		}
		line := strings.TrimSpace(item.Label)
		if line == "" {
			line = item.ID
		}
		if strings.TrimSpace(item.Summary) != "" {
			line += ": " + strings.TrimSpace(item.Summary)
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n")
}

func truncateForEvidence(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= memoryEvidenceSummaryLimit {
		return value
	}
	return strings.TrimSpace(value[:memoryEvidenceSummaryLimit-1]) + "…"
}

func filterMemoryEntries(entries []agent.MemoryEntry, filter agent.MemoryFilter) []agent.MemoryEntry {
	namespace := strings.TrimSpace(filter.Namespace)
	scopeKey := strings.TrimSpace(filter.ScopeKey)
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	if namespace == "" && scopeKey == "" && query == "" && !filter.ManagedOnly {
		return entries
	}
	filtered := make([]agent.MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		if filter.ManagedOnly && !entry.Managed {
			continue
		}
		if namespace != "" && entry.Namespace != namespace {
			continue
		}
		if scopeKey != "" && entry.ScopeKey != scopeKey {
			continue
		}
		if query != "" && !memoryEntryMatchesQuery(entry, query) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func memoryEntryMatchesQuery(entry agent.MemoryEntry, query string) bool {
	for _, candidate := range []string{
		entry.Key,
		entry.Value,
		entry.Namespace,
		entry.ScopeKey,
		entry.Field,
		entry.Label,
		entry.Source,
		strings.Join(entry.Tags, " "),
		strings.Join(entry.PreviousValues, " "),
	} {
		if strings.Contains(strings.ToLower(candidate), query) {
			return true
		}
	}
	return false
}

package agent

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

const defaultHalfLifeDays = 30.0

func recencyDecay(age time.Duration) float64 {
	days := age.Hours() / 24.0
	if days <= 0 {
		return 1.0
	}
	return math.Exp(-math.Ln2 * days / defaultHalfLifeDays)
}

// ComputeScore calculates the composite confidence score for a memory entry.
// User-sourced memories (Source="user") always return 0 (absolute authority).
func ComputeScore(entry MemoryEntry) float64 {
	if entry.Source == MemorySourceUser {
		return 0
	}

	// base: by evidence count
	base := 0.3
	if entry.EvidenceCount >= 3 {
		base = 0.5
	}
	if entry.EvidenceCount >= 5 {
		base = 0.6
	}

	// recency: by last used time
	recency := 1.0
	if !entry.LastUsedAt.IsZero() {
		recency = recencyDecay(time.Since(entry.LastUsedAt))
	}

	// reliability: by usage and corrections
	reliability := 1.0
	if entry.CorrectionCount == 1 {
		reliability = 0.7
	} else if entry.CorrectionCount >= 2 {
		reliability = 0.4
	}
	if entry.UsedCount >= 10 && entry.CorrectionCount == 0 {
		reliability = math.Min(reliability*1.2, 1.0)
	}
	reliability = math.Min(reliability*verificationFactor(entry), 1.0)

	score := base * recency * reliability
	return math.Min(score, 0.95)
}

func verificationFactor(entry MemoryEntry) float64 {
	if entry.VerificationFailCount > 0 {
		return 0.5
	}
	switch {
	case entry.VerificationPassCount >= 3:
		return 1.15
	case entry.VerificationPassCount >= 1:
		return 1.05
	default:
		return 1.0
	}
}

// RecallResult contains recalled memories and any detected conflicts.
type RecallResult struct {
	Memories  []MemoryEntry
	Conflicts []MemoryConflict
}

// MemoryQuery describes a query-aware memory recall request.
type MemoryQuery struct {
	Text          string
	TargetSummary string
	JobType       string
	Domains       []string
	SessionKey    string
	ProjectID     string
	MaxResults    int
}

// MemoryHit is a memory recall result with an explanation and relevance score.
type MemoryHit struct {
	Entry          MemoryEntry
	RelevanceScore float64
	Reason         string
}

// ConflictKind identifies the type of memory conflict.
type ConflictKind string

const (
	ConflictMemoryVsMemory  ConflictKind = "memory_vs_memory"
	ConflictMemoryVsEnv     ConflictKind = "memory_vs_env"
	ConflictGlobalVsProject ConflictKind = "global_vs_project"
)

// MemoryConflict describes a detected conflict between memories or memory vs environment.
type MemoryConflict struct {
	Kind      ConflictKind
	EntryA    MemoryEntry
	EntryB    *MemoryEntry
	EnvSource string
	EnvValue  string
	Message   string
}

// RecallForContext retrieves active memories matching the current session and project.
func RecallForContext(entries []MemoryEntry, sessionKey, projectID string) RecallResult {
	var active []MemoryEntry
	for _, entry := range entries {
		if entry.State == MemorySuperseded {
			continue
		}
		if entry.SessionKey != "" && entry.SessionKey != sessionKey {
			continue
		}
		if entry.ProjectID != "" && entry.ProjectID != projectID {
			continue
		}
		active = append(active, entry)
	}

	// Sort: user source first, then by Score desc, then by LastUsedAt desc
	sort.Slice(active, func(i, j int) bool {
		if (active[i].Source == MemorySourceUser) != (active[j].Source == MemorySourceUser) {
			return active[i].Source == MemorySourceUser
		}
		if active[i].Score != active[j].Score {
			return active[i].Score > active[j].Score
		}
		return active[i].LastUsedAt.After(active[j].LastUsedAt)
	})

	if len(active) > 20 {
		active = active[:20]
	}

	conflicts := detectMemoryConflicts(active)
	return RecallResult{Memories: active, Conflicts: conflicts}
}

// RecallWithQuery performs query-aware memory recall using authority, lexical
// match, recency, evidence quality, and correction history.
func RecallWithQuery(entries []MemoryEntry, query MemoryQuery) []MemoryHit {
	if recallQueryIsEmpty(query) {
		return recallWithStaticFallback(entries, query.SessionKey, query.ProjectID)
	}

	maxResults := query.MaxResults
	if maxResults <= 0 {
		maxResults = 20
	}

	queryTokens := tokenizeRecallText(recallQueryText(query))
	normalizedDomains := normalizeRecallValues(query.Domains)
	hits := make([]MemoryHit, 0, len(entries))
	for _, entry := range entries {
		if !recallEntryMatchesScope(entry, query.SessionKey, query.ProjectID) {
			continue
		}

		authority := recallAuthorityScore(entry)
		lexicalMatch, lexicalField := recallLexicalMatch(queryTokens, entry)
		recency := recallRecencyScore(entry)
		evidenceQuality := recallEvidenceQuality(entry)
		correctionPenalty := recallCorrectionPenalty(entry)

		relevance := 0.25*authority +
			0.30*lexicalMatch +
			0.20*recency +
			0.15*evidenceQuality -
			0.10*correctionPenalty

		matchedDomain := recallMatchingDomain(entry.Tags, normalizedDomains)
		if matchedDomain != "" {
			relevance += 0.1
		}

		jobTypeMatched := recallNamespaceMatchesJobType(entry.Namespace, query.JobType)
		if jobTypeMatched {
			relevance += 0.05
		}

		hits = append(hits, MemoryHit{
			Entry:          entry,
			RelevanceScore: clampRecallScore(relevance),
			Reason:         buildMemoryHitReason(entry, lexicalField, matchedDomain, jobTypeMatched, query.JobType),
		})
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].RelevanceScore != hits[j].RelevanceScore {
			return hits[i].RelevanceScore > hits[j].RelevanceScore
		}
		authorityI := recallAuthorityScore(hits[i].Entry)
		authorityJ := recallAuthorityScore(hits[j].Entry)
		if authorityI != authorityJ {
			return authorityI > authorityJ
		}
		if !hits[i].Entry.LastUsedAt.Equal(hits[j].Entry.LastUsedAt) {
			return hits[i].Entry.LastUsedAt.After(hits[j].Entry.LastUsedAt)
		}
		return hits[i].Entry.Key < hits[j].Entry.Key
	})

	if len(hits) > maxResults {
		hits = hits[:maxResults]
	}
	return hits
}

// detectMemoryConflicts finds potential contradictions among active memories.
func detectMemoryConflicts(memories []MemoryEntry) []MemoryConflict {
	var conflicts []MemoryConflict
	for i := 0; i < len(memories); i++ {
		for j := i + 1; j < len(memories); j++ {
			a, b := memories[i], memories[j]
			if a.Namespace != b.Namespace {
				continue
			}
			// Same namespace, similar field, different value -> potential conflict
			if fieldsSimilar(a.Field, b.Field) && a.Value != b.Value {
				bCopy := b
				conflicts = append(conflicts, MemoryConflict{
					Kind:   ConflictMemoryVsMemory,
					EntryA: a,
					EntryB: &bCopy,
					Message: "conflicting values for similar fields: " +
						a.Field + "=" + a.Value + " vs " + b.Field + "=" + b.Value,
				})
			}
		}
	}
	return conflicts
}

// fieldsSimilar checks if two field names are similar enough to potentially conflict.
func fieldsSimilar(a, b string) bool {
	if a == b {
		return true
	}
	a = strings.ToLower(a)
	b = strings.ToLower(b)
	if a == b {
		return true
	}
	// One contains the other
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}
	return false
}

// TouchMemory updates usage statistics after a memory is referenced.
func TouchMemory(entry *MemoryEntry) {
	touchMemoryUsage(entry)
	if entry.Source != MemorySourceUser {
		entry.Score = ComputeScore(*entry)
	}
}

// TouchMemoryVerification updates usage and verification feedback after a
// memory contributed to a completed run.
func TouchMemoryVerification(entry *MemoryEntry, passed bool) {
	touchMemoryUsage(entry)
	if passed {
		entry.VerificationPassCount++
	} else {
		entry.VerificationFailCount++
	}
	if entry.Source != MemorySourceUser {
		entry.Score = ComputeScore(*entry)
	}
}

func touchMemoryUsage(entry *MemoryEntry) {
	entry.UsedCount++
	entry.LastUsedAt = time.Now().UTC()
}

func recallQueryIsEmpty(query MemoryQuery) bool {
	if strings.TrimSpace(query.Text) != "" {
		return false
	}
	if strings.TrimSpace(query.TargetSummary) != "" {
		return false
	}
	if strings.TrimSpace(query.JobType) != "" {
		return false
	}
	for _, domain := range query.Domains {
		if strings.TrimSpace(domain) != "" {
			return false
		}
	}
	return true
}

func recallWithStaticFallback(entries []MemoryEntry, sessionKey, projectID string) []MemoryHit {
	result := RecallForContext(entries, sessionKey, projectID)
	hits := make([]MemoryHit, 0, len(result.Memories))
	for _, entry := range result.Memories {
		hits = append(hits, MemoryHit{
			Entry:          entry,
			RelevanceScore: entry.Score,
			Reason:         "fallback:static",
		})
	}
	return hits
}

func recallEntryMatchesScope(entry MemoryEntry, sessionKey, projectID string) bool {
	if entry.State == MemorySuperseded {
		return false
	}
	if entry.SessionKey != "" && entry.SessionKey != sessionKey {
		return false
	}
	if entry.ProjectID != "" && entry.ProjectID != projectID {
		return false
	}
	return true
}

func recallQueryText(query MemoryQuery) string {
	parts := make([]string, 0, 2)
	if text := strings.TrimSpace(query.Text); text != "" {
		parts = append(parts, text)
	}
	if summary := strings.TrimSpace(query.TargetSummary); summary != "" && !strings.EqualFold(summary, strings.TrimSpace(query.Text)) {
		parts = append(parts, summary)
	}
	return strings.Join(parts, " ")
}

func recallAuthorityScore(entry MemoryEntry) float64 {
	switch {
	case entry.Source == MemorySourceUser:
		return 1.0
	case entry.Managed:
		return 0.8
	case entry.EvidenceCount > 0:
		return 0.6
	default:
		return 0.3
	}
}

func recallLexicalMatch(queryTokens map[string]struct{}, entry MemoryEntry) (float64, string) {
	if len(queryTokens) == 0 {
		return 0, ""
	}

	candidates := []struct {
		field string
		value string
	}{
		{field: "field", value: entry.Field},
		{field: "label", value: entry.Label},
		{field: "value", value: entry.Value},
		{field: "tags", value: strings.Join(entry.Tags, " ")},
	}

	bestScore := 0.0
	bestField := ""
	for _, candidate := range candidates {
		score := recallJaccardSimilarity(queryTokens, tokenizeRecallText(candidate.value))
		if score > bestScore {
			bestScore = score
			bestField = candidate.field
		}
	}
	return bestScore, bestField
}

func recallRecencyScore(entry MemoryEntry) float64 {
	recency := 1.0
	if !entry.LastUsedAt.IsZero() {
		recency = recencyDecay(time.Since(entry.LastUsedAt))
	}
	return recency
}

func recallEvidenceQuality(entry MemoryEntry) float64 {
	return math.Min(float64(entry.EvidenceCount)/5.0, 1.0)
}

func recallCorrectionPenalty(entry MemoryEntry) float64 {
	return math.Min(float64(entry.CorrectionCount)/3.0, 1.0)
}

func recallMatchingDomain(tags, domains []string) string {
	if len(tags) == 0 || len(domains) == 0 {
		return ""
	}
	normalizedTags := normalizeRecallValues(tags)
	for _, domain := range domains {
		for _, tag := range normalizedTags {
			if tag == domain {
				return domain
			}
		}
	}
	return ""
}

func recallNamespaceMatchesJobType(namespace, jobType string) bool {
	namespace = strings.ToLower(strings.TrimSpace(namespace))
	jobType = strings.ToLower(strings.TrimSpace(jobType))
	if namespace == "" || jobType == "" {
		return false
	}
	if strings.Contains(namespace, jobType) || strings.Contains(jobType, namespace) {
		return true
	}
	switch namespace {
	case "task":
		return jobType == taskContractJobDevelopment ||
			jobType == taskContractJobAutomation ||
			jobType == taskContractJobMonitor ||
			strings.Contains(jobType, "debug")
	case "project":
		return jobType == taskContractJobDevelopment ||
			jobType == taskContractJobDeployment ||
			jobType == taskContractJobReport ||
			jobType == taskContractJobResearch
	case "workspace":
		return jobType == taskContractJobDevelopment ||
			jobType == taskContractJobAutomation ||
			jobType == taskContractJobResearch
	case "profile":
		return jobType == taskContractJobGeneral ||
			jobType == taskContractJobDelivery
	case "general":
		return jobType == taskContractJobGeneral
	default:
		return false
	}
}

func buildMemoryHitReason(entry MemoryEntry, lexicalField, matchedDomain string, jobTypeMatched bool, jobType string) string {
	parts := make([]string, 0, 3)
	if lexicalField != "" {
		parts = append(parts, "lexical:"+lexicalField)
	} else {
		parts = append(parts, recallAuthorityReason(entry))
	}
	if matchedDomain != "" {
		parts = append(parts, "domain:"+matchedDomain)
	}
	if jobTypeMatched {
		jobType = strings.ToLower(strings.TrimSpace(jobType))
		if jobType == "" {
			jobType = "namespace"
		}
		parts = append(parts, "job:"+jobType)
	}
	return strings.Join(parts, ", ")
}

func recallAuthorityReason(entry MemoryEntry) string {
	switch {
	case entry.Source == MemorySourceUser:
		return "authority:user"
	case entry.Managed:
		return "authority:managed"
	case entry.EvidenceCount > 0:
		return "authority:agent_with_evidence"
	default:
		return "authority:agent_no_evidence"
	}
}

func normalizeRecallValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func tokenizeRecallText(text string) map[string]struct{} {
	tokens := make(map[string]struct{})
	for _, token := range strings.FieldsFunc(strings.ToLower(strings.TrimSpace(text)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if token == "" {
			continue
		}
		tokens[token] = struct{}{}
	}
	return tokens
}

func recallJaccardSimilarity(left, right map[string]struct{}) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	intersection := 0
	union := len(left)
	for token := range right {
		if _, ok := left[token]; ok {
			intersection++
			continue
		}
		union++
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func clampRecallScore(score float64) float64 {
	return math.Max(0, math.Min(score, 1))
}

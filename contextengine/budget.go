package contextengine

import (
	"sort"
	"strings"
)

// BudgetPlan defines per-section token budgets for Prepare().
type BudgetPlan struct {
	PinnedFacts     int
	TaskState       int
	RecalledContext int
	RecentMessages  int
	Knowledge       int
}

type budgetProfile struct {
	pinnedFacts     int
	taskState       int
	recalledContext int
	recentMessages  int
	knowledge       int
}

func PlanBudget(totalTokens int, jobType string, domains []string) BudgetPlan {
	if totalTokens <= 0 {
		return BudgetPlan{}
	}

	profile := budgetProfileFor(jobType, domains)
	shares := []struct {
		percent int
	}{
		{percent: profile.pinnedFacts},
		{percent: profile.taskState},
		{percent: profile.recalledContext},
		{percent: profile.recentMessages},
		{percent: profile.knowledge},
	}

	type allocation struct {
		index     int
		remainder float64
		percent   int
	}

	plan := BudgetPlan{}
	assignments := []*int{
		&plan.PinnedFacts,
		&plan.TaskState,
		&plan.RecalledContext,
		&plan.RecentMessages,
		&plan.Knowledge,
	}

	sum := 0
	remainders := make([]allocation, 0, len(shares))
	for index, share := range shares {
		raw := float64(totalTokens*share.percent) / 100.0
		base := int(raw)
		*assignments[index] = base
		sum += base
		remainders = append(remainders, allocation{
			index:     index,
			remainder: raw - float64(base),
			percent:   share.percent,
		})
	}

	sort.SliceStable(remainders, func(i, j int) bool {
		if remainders[i].remainder == remainders[j].remainder {
			if remainders[i].percent == remainders[j].percent {
				return remainders[i].index < remainders[j].index
			}
			return remainders[i].percent > remainders[j].percent
		}
		return remainders[i].remainder > remainders[j].remainder
	})

	for index := 0; index < totalTokens-sum; index++ {
		*assignments[remainders[index].index]++
	}

	return plan
}

func budgetProfileFor(jobType string, domains []string) budgetProfile {
	switch normalizeBudgetClass(jobType, domains) {
	case "debug":
		return budgetProfile{
			pinnedFacts:     8,
			taskState:       10,
			recalledContext: 15,
			recentMessages:  60,
			knowledge:       7,
		}
	case "writing":
		return budgetProfile{
			pinnedFacts:     12,
			taskState:       5,
			recalledContext: 20,
			recentMessages:  50,
			knowledge:       13,
		}
	case "research":
		return budgetProfile{
			pinnedFacts:     8,
			taskState:       5,
			recalledContext: 25,
			recentMessages:  45,
			knowledge:       17,
		}
	default:
		return budgetProfile{
			pinnedFacts:     10,
			taskState:       8,
			recalledContext: 18,
			recentMessages:  55,
			knowledge:       9,
		}
	}
}

func normalizeBudgetClass(jobType string, domains []string) string {
	switch strings.ToLower(strings.TrimSpace(jobType)) {
	case "debug", "development", "deployment", "automation", "monitor", "ops", "incident":
		return "debug"
	case "writing", "write", "report", "delivery", "document", "presentation":
		return "writing"
	case "research", "analysis", "browse", "browser", "search":
		return "research"
	case "", "default", "general":
		// Fall through to domain-based inference below.
	default:
		return "default"
	}

	domainSet := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		normalized := strings.ToLower(strings.TrimSpace(domain))
		if normalized == "" {
			continue
		}
		domainSet[normalized] = struct{}{}
	}

	if hasAnyBudgetDomain(domainSet, "search", "news", "browser", "web", "docs", "research", "knowledge") {
		return "research"
	}
	if hasAnyBudgetDomain(domainSet, "document", "presentation", "spreadsheet", "canvas", "email", "text") {
		return "writing"
	}
	if hasAnyBudgetDomain(domainSet, "git", "fs", "exec", "desktop", "terminal", "runtime") {
		return "debug"
	}
	return "default"
}

func hasAnyBudgetDomain(domains map[string]struct{}, candidates ...string) bool {
	for _, candidate := range candidates {
		if _, ok := domains[candidate]; ok {
			return true
		}
	}
	return false
}

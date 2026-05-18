package agent

import "strings"

func normalizeSemanticDomains(items []string) []string {
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
		if domain, ok := evidenceTokenToDomain[item]; ok {
			item = string(domain)
		}
		if _, ok := domainTier[ToolDomain(item)]; !ok {
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

func domainsToStrings(domains map[ToolDomain]bool) []string {
	if len(domains) == 0 {
		return nil
	}
	out := make([]string, 0, len(domains))
	for domain, enabled := range domains {
		if !enabled || domain == "" {
			continue
		}
		out = append(out, string(domain))
	}
	return normalizeSemanticDomains(out)
}

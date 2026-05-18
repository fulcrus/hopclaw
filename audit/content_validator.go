package audit

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	riskTypeSuspiciousURL    = "suspicious_url"
	riskTypeSensitiveData    = "sensitive_data"
	riskTypeOversizedContent = "oversized_content"

	// defaultMaxContentSize is the maximum byte length of content the
	// validator will inspect. Larger content is flagged.
	defaultMaxContentSize int64 = 10 * 1024 * 1024 // 10 MiB
)

// ---------------------------------------------------------------------------
// ContentRisk
// ---------------------------------------------------------------------------

// ContentRisk describes a content-level security finding.
type ContentRisk struct {
	Type     string `json:"type"` // e.g. "suspicious_url"
	Detail   string `json:"detail"`
	Severity string `json:"severity"` // "high", "medium", "low"
}

// ---------------------------------------------------------------------------
// ContentValidator
// ---------------------------------------------------------------------------

// ContentValidator inspects URLs and content for security-sensitive patterns
// including private IPs, unsafe protocols, API key leaks, and PII.
type ContentValidator struct {
	maxContentSize int64
	blockedDomains []string
	apiKeyPatterns []contentPattern
	piiPatterns    []contentPattern
}

type contentPattern struct {
	re       *regexp.Regexp
	label    string
	severity string
}

// NewContentValidator creates a validator with default thresholds and pattern
// sets.
func NewContentValidator() *ContentValidator {
	return &ContentValidator{
		maxContentSize: defaultMaxContentSize,
		blockedDomains: nil,
		apiKeyPatterns: defaultAPIKeyPatterns(),
		piiPatterns:    defaultPIIPatterns(),
	}
}

// WithMaxContentSize overrides the default maximum content size.
func (v *ContentValidator) WithMaxContentSize(size int64) *ContentValidator {
	v.maxContentSize = size
	return v
}

// WithBlockedDomains sets the list of domains that should be rejected.
func (v *ContentValidator) WithBlockedDomains(domains []string) *ContentValidator {
	v.blockedDomains = domains
	return v
}

// ---------------------------------------------------------------------------
// URL validation
// ---------------------------------------------------------------------------

// ValidateURL checks if a URL is safe to fetch.
func (v *ContentValidator) ValidateURL(rawURL string) []ContentRisk {
	var risks []ContentRisk

	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		risks = append(risks, ContentRisk{
			Type:     riskTypeSuspiciousURL,
			Detail:   fmt.Sprintf("malformed url: %s", err.Error()),
			Severity: severityMedium,
		})
		return risks
	}

	// Block file:// protocol.
	if strings.EqualFold(parsed.Scheme, "file") {
		risks = append(risks, ContentRisk{
			Type:     riskTypeSuspiciousURL,
			Detail:   "file:// protocol is blocked",
			Severity: severityHigh,
		})
	}

	// Warn on non-HTTPS.
	if parsed.Scheme != "" && !strings.EqualFold(parsed.Scheme, "https") && !strings.EqualFold(parsed.Scheme, "file") {
		risks = append(risks, ContentRisk{
			Type:     riskTypeSuspiciousURL,
			Detail:   fmt.Sprintf("non-https scheme %q", parsed.Scheme),
			Severity: severityLow,
		})
	}

	// Check for private/internal IPs.
	host := parsed.Hostname()
	if risk := checkPrivateIP(host); risk != nil {
		risks = append(risks, *risk)
	}

	// Check blocked domains.
	lowerHost := strings.ToLower(host)
	for _, blocked := range v.blockedDomains {
		if lowerHost == strings.ToLower(blocked) || strings.HasSuffix(lowerHost, "."+strings.ToLower(blocked)) {
			risks = append(risks, ContentRisk{
				Type:     riskTypeSuspiciousURL,
				Detail:   fmt.Sprintf("blocked domain %q", blocked),
				Severity: severityHigh,
			})
			break
		}
	}

	return risks
}

// privateIPNets contains the CIDR ranges considered private/internal.
var privateIPNets = func() []*net.IPNet {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, n, err := net.ParseCIDR(cidr)
		if err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

func checkPrivateIP(host string) *ContentRisk {
	ip := net.ParseIP(host)
	if ip == nil {
		// Try resolving in case it is a hostname that looks like an IP.
		return nil
	}
	for _, n := range privateIPNets {
		if n.Contains(ip) {
			return &ContentRisk{
				Type:     riskTypeSuspiciousURL,
				Detail:   fmt.Sprintf("url targets private/internal ip %s", host),
				Severity: severityHigh,
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Content validation
// ---------------------------------------------------------------------------

// ValidateContent checks content for sensitive data patterns such as API keys,
// passwords, and PII.
func (v *ContentValidator) ValidateContent(content string) []ContentRisk {
	var risks []ContentRisk

	if v.maxContentSize > 0 && int64(len(content)) > v.maxContentSize {
		risks = append(risks, ContentRisk{
			Type:     riskTypeOversizedContent,
			Detail:   fmt.Sprintf("content size %d exceeds max %d bytes", len(content), v.maxContentSize),
			Severity: severityLow,
		})
	}

	for _, p := range v.apiKeyPatterns {
		if p.re.MatchString(content) {
			risks = append(risks, ContentRisk{
				Type:     riskTypeSensitiveData,
				Detail:   fmt.Sprintf("potential api key detected: %s", p.label),
				Severity: p.severity,
			})
		}
	}

	for _, p := range v.piiPatterns {
		if p.re.MatchString(content) {
			risks = append(risks, ContentRisk{
				Type:     riskTypeSensitiveData,
				Detail:   fmt.Sprintf("potential pii detected: %s", p.label),
				Severity: p.severity,
			})
		}
	}

	return risks
}

// ---------------------------------------------------------------------------
// Default patterns
// ---------------------------------------------------------------------------

func defaultAPIKeyPatterns() []contentPattern {
	return []contentPattern{
		{re: regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`), label: "openai_api_key", severity: severityHigh},
		{re: regexp.MustCompile(`AKIA[0-9A-Z]{16}`), label: "aws_access_key", severity: severityHigh},
		{re: regexp.MustCompile(`ghp_[A-Za-z0-9]{36,}`), label: "github_pat", severity: severityHigh},
		{re: regexp.MustCompile(`gho_[A-Za-z0-9]{36,}`), label: "github_oauth", severity: severityHigh},
		{re: regexp.MustCompile(`glpat-[A-Za-z0-9\-]{20,}`), label: "gitlab_pat", severity: severityHigh},
		{re: regexp.MustCompile(`xox[baprs]-[A-Za-z0-9\-]{10,}`), label: "slack_token", severity: severityHigh},
	}
}

func defaultPIIPatterns() []contentPattern {
	return []contentPattern{
		// US Social Security Number: XXX-XX-XXXX
		{re: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), label: "ssn", severity: severityHigh},
		// Credit card (Visa, Mastercard, Amex basic formats)
		{re: regexp.MustCompile(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13})\b`), label: "credit_card", severity: severityHigh},
	}
}

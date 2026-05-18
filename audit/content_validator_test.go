package audit

import (
	"strings"
	"testing"
)

func TestContentValidator_ValidateURL_PrivateIP(t *testing.T) {
	t.Parallel()

	v := NewContentValidator()
	tests := []struct {
		name    string
		url     string
		wantLen int
	}{
		{"loopback", "http://127.0.0.1/admin", 2},    // private IP + non-https
		{"private 10", "http://10.0.0.1/api", 2},     // private IP + non-https
		{"private 192", "http://192.168.1.1/", 2},    // private IP + non-https
		{"link-local", "http://169.254.169.254/", 2}, // private IP + non-https
		{"ipv6 loopback", "http://[::1]/", 2},        // private IP + non-https
		{"public https", "https://example.com/api", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			risks := v.ValidateURL(tt.url)
			if len(risks) != tt.wantLen {
				t.Errorf("ValidateURL(%q) risks = %d, want %d; risks: %+v", tt.url, len(risks), tt.wantLen, risks)
			}
		})
	}
}

func TestContentValidator_ValidateURL_FileProtocol(t *testing.T) {
	t.Parallel()

	v := NewContentValidator()
	risks := v.ValidateURL("file:///etc/passwd")
	found := false
	for _, r := range risks {
		if r.Type == riskTypeSuspiciousURL && strings.Contains(r.Detail, "file://") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected file:// protocol risk")
	}
}

func TestContentValidator_ValidateURL_NonHTTPS(t *testing.T) {
	t.Parallel()

	v := NewContentValidator()
	risks := v.ValidateURL("http://example.com/api")
	found := false
	for _, r := range risks {
		if strings.Contains(r.Detail, "non-https") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected non-https warning")
	}
}

func TestContentValidator_ValidateURL_BlockedDomains(t *testing.T) {
	t.Parallel()

	v := NewContentValidator().WithBlockedDomains([]string{"evil.com", "malware.org"})
	risks := v.ValidateURL("https://evil.com/payload")
	found := false
	for _, r := range risks {
		if strings.Contains(r.Detail, "blocked domain") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected blocked domain risk")
	}

	// Subdomain should also match.
	risks = v.ValidateURL("https://sub.evil.com/payload")
	found = false
	for _, r := range risks {
		if strings.Contains(r.Detail, "blocked domain") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected blocked domain risk for subdomain")
	}
}

func TestContentValidator_ValidateContent_APIKeys(t *testing.T) {
	t.Parallel()

	v := NewContentValidator()
	tests := []struct {
		name    string
		content string
		wantKey string
	}{
		{"openai key", "Authorization: Bearer sk-abcdefghij1234567890abcdef", "openai_api_key"},
		{"aws key", "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE", "aws_access_key"},
		{"github pat", "token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij12", "github_pat"},
		{"slack token", "SLACK_TOKEN=xoxb-1234567890-abcdef", "slack_token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			risks := v.ValidateContent(tt.content)
			found := false
			for _, r := range risks {
				if r.Type == riskTypeSensitiveData && strings.Contains(r.Detail, tt.wantKey) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %q detection in %q, got risks: %+v", tt.wantKey, tt.content, risks)
			}
		})
	}
}

func TestContentValidator_ValidateContent_PII(t *testing.T) {
	t.Parallel()

	v := NewContentValidator()
	tests := []struct {
		name    string
		content string
		wantPII string
	}{
		{"ssn", "SSN: 123-45-6789", "ssn"},
		{"visa card", "card: 4111111111111111", "credit_card"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			risks := v.ValidateContent(tt.content)
			found := false
			for _, r := range risks {
				if strings.Contains(r.Detail, tt.wantPII) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %q detection, got risks: %+v", tt.wantPII, risks)
			}
		})
	}
}

func TestContentValidator_ValidateContent_OversizedContent(t *testing.T) {
	t.Parallel()

	v := NewContentValidator().WithMaxContentSize(100)
	content := strings.Repeat("x", 200)
	risks := v.ValidateContent(content)
	found := false
	for _, r := range risks {
		if r.Type == riskTypeOversizedContent {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected oversized content risk")
	}
}

func TestContentValidator_ValidateContent_SafeContent(t *testing.T) {
	t.Parallel()

	v := NewContentValidator()
	risks := v.ValidateContent("Hello, world! This is a normal message.")
	if len(risks) != 0 {
		t.Errorf("expected no risks for safe content, got %d: %+v", len(risks), risks)
	}
}

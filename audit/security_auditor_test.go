package audit

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/eventbus"
)

func TestSecurityAuditor_AuditToolCall_InjectionDetected(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	auditor := NewSecurityAuditor(WithEventPublisher(bus))

	// Use a non-exec tool to verify injection detection works.
	// exec.shell/exec.run are exempt since pipes and chains are normal shell syntax.
	risks := auditor.AuditToolCall(context.Background(), "net.fetch", map[string]any{
		"url": "http://example.com; rm -rf /",
	})
	if len(risks) == 0 {
		t.Fatal("expected injection risks")
	}

	hasInjection := false
	for _, r := range risks {
		if r.Category == "injection" {
			hasInjection = true
			break
		}
	}
	if !hasInjection {
		t.Error("expected injection category risk")
	}

	// Verify events were published.
	events := bus.Snapshot()
	if len(events) == 0 {
		t.Fatal("expected security events to be published")
	}
	foundRiskDetected := false
	foundInjectionAttempt := false
	for _, e := range events {
		if e.Type == eventbus.EventSecurityRiskDetected {
			foundRiskDetected = true
		}
		if e.Type == eventbus.EventSecurityInjectionAttempt {
			foundInjectionAttempt = true
		}
	}
	if !foundRiskDetected {
		t.Error("expected security.risk_detected event")
	}
	if !foundInjectionAttempt {
		t.Error("expected security.injection_attempt event")
	}
}

func TestSecurityAuditor_AuditToolCall_PathViolation(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	auditor := NewSecurityAuditor(WithEventPublisher(bus))

	risks := auditor.AuditToolCall(context.Background(), "file.read", map[string]any{
		"path": "/home/user/.ssh/id_rsa",
	})
	if len(risks) == 0 {
		t.Fatal("expected path safety risks")
	}

	hasPathSafety := false
	for _, r := range risks {
		if r.Category == "path_safety" {
			hasPathSafety = true
			break
		}
	}
	if !hasPathSafety {
		t.Error("expected path_safety category risk")
	}

	// Verify path violation event.
	events := bus.Snapshot()
	foundPathViolation := false
	for _, e := range events {
		if e.Type == eventbus.EventSecurityPathViolation {
			foundPathViolation = true
			break
		}
	}
	if !foundPathViolation {
		t.Error("expected security.path_violation event")
	}
}

func TestSecurityAuditor_AuditToolCall_ContentRisk(t *testing.T) {
	t.Parallel()

	auditor := NewSecurityAuditor()
	risks := auditor.AuditToolCall(context.Background(), "web.fetch", map[string]any{
		"url": "http://192.168.1.1/admin",
	})
	if len(risks) == 0 {
		t.Fatal("expected content risks for private IP URL")
	}

	hasContent := false
	for _, r := range risks {
		if r.Category == "content" {
			hasContent = true
			break
		}
	}
	if !hasContent {
		t.Error("expected content category risk")
	}
}

func TestSecurityAuditor_AuditToolCall_SafeInput(t *testing.T) {
	t.Parallel()

	auditor := NewSecurityAuditor()
	risks := auditor.AuditToolCall(context.Background(), "file.read", map[string]any{
		"path": "src/main.go",
	})
	if len(risks) != 0 {
		t.Errorf("expected no risks for safe input, got %d: %+v", len(risks), risks)
	}
}

func TestSecurityAuditor_AuditToolCall_NoBus(t *testing.T) {
	t.Parallel()

	// Should not panic when no bus is configured.
	// Use a non-exec tool to verify injection detection (exec tools are exempt).
	auditor := NewSecurityAuditor()
	risks := auditor.AuditToolCall(context.Background(), "net.fetch", map[string]any{
		"url": "http://example.com; rm -rf /",
	})
	if len(risks) == 0 {
		t.Fatal("expected injection risks even without bus")
	}
}

func TestHasHighSeverity(t *testing.T) {
	t.Parallel()

	if HasHighSeverity(nil) {
		t.Error("nil should not have high severity")
	}
	if HasHighSeverity([]SecurityRisk{{Severity: "low"}, {Severity: "medium"}}) {
		t.Error("low+medium should not have high severity")
	}
	if !HasHighSeverity([]SecurityRisk{{Severity: "low"}, {Severity: "high"}}) {
		t.Error("low+high should have high severity")
	}
}

func TestSecurityAuditor_AuditToolCall_APIKeyInContent(t *testing.T) {
	t.Parallel()

	auditor := NewSecurityAuditor()
	risks := auditor.AuditToolCall(context.Background(), "file.write", map[string]any{
		"content": "API_KEY=sk-abcdefghij1234567890abcdef",
	})

	hasAPIKey := false
	for _, r := range risks {
		if r.Category == "content" && r.Type == riskTypeSensitiveData {
			hasAPIKey = true
			break
		}
	}
	if !hasAPIKey {
		t.Errorf("expected sensitive_data risk for API key, got: %+v", risks)
	}
}

func TestSecurityAuditor_DangerousToolDetection(t *testing.T) {
	t.Parallel()

	auditor := NewSecurityAuditor(
		WithDangerousTools([]string{"deploy.prod", "db.drop"}),
	)

	// Dangerous tool should produce a high-severity risk.
	risks := auditor.AuditToolCall(context.Background(), "deploy.prod", map[string]any{
		"target": "production",
	})
	found := false
	for _, r := range risks {
		if r.Category == "dangerous_tool" && r.Severity == severityHigh {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dangerous_tool risk for deploy.prod, got: %+v", risks)
	}

	// Case-insensitive match.
	risks = auditor.AuditToolCall(context.Background(), "DB.Drop", map[string]any{})
	found = false
	for _, r := range risks {
		if r.Category == "dangerous_tool" && r.Severity == severityHigh {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dangerous_tool risk for DB.Drop (case-insensitive), got: %+v", risks)
	}

	// Non-dangerous tool should not trigger.
	risks = auditor.AuditToolCall(context.Background(), "file.read", map[string]any{
		"path": "src/main.go",
	})
	for _, r := range risks {
		if r.Category == "dangerous_tool" {
			t.Errorf("unexpected dangerous_tool risk for file.read: %+v", r)
		}
	}
}

func TestSecurityAuditor_CustomPatternMatching(t *testing.T) {
	t.Parallel()

	auditor := NewSecurityAuditor(
		WithCustomPatterns([]CustomPatternConfig{
			{
				Name:     "internal_endpoint",
				Pattern:  `internal\.corp\.example\.com`,
				Severity: "high",
				Category: "content",
			},
			{
				Name:     "debug_flag",
				Pattern:  `--debug|--unsafe`,
				Severity: "medium",
				Category: "injection",
			},
		}),
	)

	// Matching custom pattern.
	risks := auditor.AuditToolCall(context.Background(), "web.fetch", map[string]any{
		"url": "https://internal.corp.example.com/secrets",
	})
	found := false
	for _, r := range risks {
		if r.Type == "internal_endpoint" && r.Severity == severityHigh {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected internal_endpoint custom pattern risk, got: %+v", risks)
	}

	// Matching another custom pattern.
	risks = auditor.AuditToolCall(context.Background(), "exec.run", map[string]any{
		"command": "myapp --debug --verbose",
	})
	found = false
	for _, r := range risks {
		if r.Type == "debug_flag" && r.Severity == severityMedium {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected debug_flag custom pattern risk, got: %+v", risks)
	}

	// Non-matching input should not trigger custom patterns.
	risks = auditor.AuditToolCall(context.Background(), "file.read", map[string]any{
		"path": "src/main.go",
	})
	for _, r := range risks {
		if r.Type == "internal_endpoint" || r.Type == "debug_flag" {
			t.Errorf("unexpected custom pattern risk for safe input: %+v", r)
		}
	}
}

func TestSecurityAuditor_CustomPattern_InvalidRegex(t *testing.T) {
	t.Parallel()

	// Invalid regex should be silently skipped.
	auditor := NewSecurityAuditor(
		WithCustomPatterns([]CustomPatternConfig{
			{
				Name:     "invalid",
				Pattern:  `[invalid`,
				Severity: "high",
				Category: "content",
			},
			{
				Name:     "valid",
				Pattern:  `secret_token`,
				Severity: "medium",
				Category: "content",
			},
		}),
	)

	risks := auditor.AuditToolCall(context.Background(), "file.write", map[string]any{
		"content": "my_secret_token_here",
	})
	found := false
	for _, r := range risks {
		if r.Type == "valid" {
			found = true
		}
		if r.Type == "invalid" {
			t.Errorf("invalid regex pattern should have been skipped")
		}
	}
	if !found {
		t.Errorf("expected valid custom pattern risk, got: %+v", risks)
	}
}

func TestSecurityAuditor_ConfigurableBlockedDomains(t *testing.T) {
	t.Parallel()

	cv := NewContentValidator().WithBlockedDomains([]string{"malicious.io", "phishing.net"})
	auditor := NewSecurityAuditor(WithContentValidator(cv))

	risks := auditor.AuditToolCall(context.Background(), "web.fetch", map[string]any{
		"url": "https://malicious.io/payload",
	})
	found := false
	for _, r := range risks {
		if r.Category == "content" && r.Type == riskTypeSuspiciousURL {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected blocked domain risk for malicious.io, got: %+v", risks)
	}

	// Subdomain should also match.
	risks = auditor.AuditToolCall(context.Background(), "web.fetch", map[string]any{
		"url": "https://sub.phishing.net/login",
	})
	found = false
	for _, r := range risks {
		if r.Category == "content" && r.Type == riskTypeSuspiciousURL {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected blocked domain risk for sub.phishing.net, got: %+v", risks)
	}

	// Non-blocked domain should not trigger.
	risks = auditor.AuditToolCall(context.Background(), "web.fetch", map[string]any{
		"url": "https://example.com/safe",
	})
	for _, r := range risks {
		if r.Category == "content" && r.Type == riskTypeSuspiciousURL {
			t.Errorf("unexpected suspicious URL risk for safe domain: %+v", r)
		}
	}
}

func TestSecurityAuditor_IsDangerousTool(t *testing.T) {
	t.Parallel()

	auditor := NewSecurityAuditor(
		WithDangerousTools([]string{"deploy.prod", "DB.Drop"}),
	)

	if !auditor.IsDangerousTool("deploy.prod") {
		t.Error("deploy.prod should be dangerous")
	}
	if !auditor.IsDangerousTool("Deploy.Prod") {
		t.Error("Deploy.Prod should be dangerous (case-insensitive)")
	}
	if !auditor.IsDangerousTool("db.drop") {
		t.Error("db.drop should be dangerous")
	}
	if auditor.IsDangerousTool("file.read") {
		t.Error("file.read should not be dangerous")
	}

	// No dangerous tools configured.
	auditor2 := NewSecurityAuditor()
	if auditor2.IsDangerousTool("deploy.prod") {
		t.Error("deploy.prod should not be dangerous when no tools configured")
	}
}

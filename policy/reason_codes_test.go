package policy

import (
	"reflect"
	"strings"
	"testing"
)

func TestAllReasonCodesStable(t *testing.T) {
	t.Parallel()

	want := []string{
		"unknown_tool_allowed",
		"unknown_tool",
		"tool_unavailable",
		"tool_ineligible",
		"session_denied_grant",
		"approval_grant",
		"blocked_command",
		"safe_exec_allowlist",
		"destructive_tool_blocked",
		"skill_install_auto_allowed",
		"skill_install_denied",
		"skill_install_requires_approval",
		"high_security_risk",
	}
	got := AllReasonCodes()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllReasonCodes() = %#v, want %#v", got, want)
	}

	seen := make(map[string]struct{}, len(got))
	for _, code := range got {
		if _, ok := seen[code]; ok {
			t.Fatalf("duplicate reason code %q in %#v", code, got)
		}
		seen[code] = struct{}{}
		if code != strings.ToLower(code) || strings.Contains(code, "-") {
			t.Fatalf("reason code %q is not canonical snake_case", code)
		}
	}
}

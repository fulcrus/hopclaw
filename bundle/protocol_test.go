package bundle

import (
	"testing"
)

// ---------------------------------------------------------------------------
// ExecuteResponse.Normalized
// ---------------------------------------------------------------------------

func TestNormalizedDefaultsProtocolVersion(t *testing.T) {
	t.Parallel()

	resp := ExecuteResponse{}.Normalized()
	if resp.ProtocolVersion != ProtocolVersionV1 {
		t.Fatalf("ProtocolVersion = %q, want %q", resp.ProtocolVersion, ProtocolVersionV1)
	}
}

func TestNormalizedPreservesExplicitProtocolVersion(t *testing.T) {
	t.Parallel()

	resp := ExecuteResponse{ProtocolVersion: "hopclaw.tool/v2"}.Normalized()
	if resp.ProtocolVersion != "hopclaw.tool/v2" {
		t.Fatalf("ProtocolVersion = %q", resp.ProtocolVersion)
	}
}

func TestNormalizedInfersSuccessFromContent(t *testing.T) {
	t.Parallel()

	resp := ExecuteResponse{Summary: "done"}.Normalized()
	if resp.Status != ResultSuccess {
		t.Fatalf("Status = %q, want %q", resp.Status, ResultSuccess)
	}
	if !resp.OK {
		t.Fatal("OK = false, want true")
	}
}

func TestNormalizedInfersErrorWhenEmpty(t *testing.T) {
	t.Parallel()

	resp := ExecuteResponse{}.Normalized()
	if resp.Status != ResultError {
		t.Fatalf("Status = %q, want %q", resp.Status, ResultError)
	}
	if resp.OK {
		t.Fatal("OK = true, want false")
	}
	if resp.Error == nil {
		t.Fatal("expected Error to be populated")
	}
}

func TestNormalizedInfersRetryableError(t *testing.T) {
	t.Parallel()

	resp := ExecuteResponse{
		Error: &ExecuteError{Message: "timeout", Retryable: true},
	}.Normalized()

	if resp.Status != ResultRetryableError {
		t.Fatalf("Status = %q, want %q", resp.Status, ResultRetryableError)
	}
}

func TestNormalizedInfersErrorFromError(t *testing.T) {
	t.Parallel()

	resp := ExecuteResponse{
		Error: &ExecuteError{Message: "oops"},
	}.Normalized()

	if resp.Status != ResultError {
		t.Fatalf("Status = %q, want %q", resp.Status, ResultError)
	}
}

func TestNormalizedInfersErrorFromFailedVerification(t *testing.T) {
	t.Parallel()

	resp := ExecuteResponse{
		Summary: "something",
		Verification: &Verification{
			Attempted: true,
			Status:    VerificationFailed,
		},
	}.Normalized()

	if resp.Status != ResultError {
		t.Fatalf("Status = %q, want %q", resp.Status, ResultError)
	}
}

func TestNormalizedPreservesExplicitStatus(t *testing.T) {
	t.Parallel()

	statuses := []ResultStatus{
		ResultSuccess, ResultAccepted, ResultPartial,
		ResultBlocked, ResultRetryableError, ResultError,
	}
	for _, s := range statuses {
		resp := ExecuteResponse{Status: s}.Normalized()
		if resp.Status != s {
			t.Fatalf("Status = %q, want %q", resp.Status, s)
		}
	}
}

func TestNormalizedSetsOKForSuccessStatuses(t *testing.T) {
	t.Parallel()

	okStatuses := []ResultStatus{ResultSuccess, ResultAccepted, ResultPartial}
	for _, s := range okStatuses {
		resp := ExecuteResponse{Status: s, Summary: "x"}.Normalized()
		if !resp.OK {
			t.Fatalf("OK = false for status %q", s)
		}
	}

	notOK := []ResultStatus{ResultBlocked, ResultRetryableError, ResultError}
	for _, s := range notOK {
		resp := ExecuteResponse{Status: s}.Normalized()
		if resp.OK {
			t.Fatalf("OK = true for status %q", s)
		}
	}
}

func TestNormalizedVerificationStatusNormalization(t *testing.T) {
	t.Parallel()

	// normalizeVerificationStatus is case-sensitive after trimming; unknown values
	// map to VerificationUnknown.
	resp := ExecuteResponse{
		Status: ResultSuccess,
		Verification: &Verification{
			Status: VerificationPassed,
		},
	}.Normalized()

	if resp.Verification.Status != VerificationPassed {
		t.Fatalf("Verification.Status = %q, want %q", resp.Verification.Status, VerificationPassed)
	}

	// Upper-case variant is treated as unknown.
	resp2 := ExecuteResponse{
		Status: ResultSuccess,
		Verification: &Verification{
			Status: "  PASSED  ",
		},
	}.Normalized()

	if resp2.Verification.Status != VerificationUnknown {
		t.Fatalf("Verification.Status = %q, want %q for case mismatch", resp2.Verification.Status, VerificationUnknown)
	}
}

func TestNormalizedErrorCategoryNormalization(t *testing.T) {
	t.Parallel()

	// normalizeErrorCategory is case-sensitive after trimming.
	resp := ExecuteResponse{
		Status: ResultError,
		Error:  &ExecuteError{Category: ErrorAuth, Message: "bad"},
	}.Normalized()

	if resp.Error.Category != ErrorAuth {
		t.Fatalf("Error.Category = %q, want %q", resp.Error.Category, ErrorAuth)
	}

	// Upper-case variant falls through to ErrorInternal.
	resp2 := ExecuteResponse{
		Status: ResultError,
		Error:  &ExecuteError{Category: "  AUTH  ", Message: "bad"},
	}.Normalized()

	if resp2.Error.Category != ErrorInternal {
		t.Fatalf("Error.Category = %q, want %q for case mismatch", resp2.Error.Category, ErrorInternal)
	}
}

// ---------------------------------------------------------------------------
// ExecuteResponse.Successful
// ---------------------------------------------------------------------------

func TestSuccessful(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		resp ExecuteResponse
		want bool
	}{
		{"success", ExecuteResponse{Status: ResultSuccess}, true},
		{"accepted", ExecuteResponse{Status: ResultAccepted}, true},
		{"partial", ExecuteResponse{Status: ResultPartial}, true},
		{"error", ExecuteResponse{Status: ResultError}, false},
		{"blocked", ExecuteResponse{Status: ResultBlocked}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.resp.Successful(); got != tt.want {
				t.Fatalf("Successful() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeResultStatus
// ---------------------------------------------------------------------------

func TestNormalizeResultStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input ResultStatus
		want  ResultStatus
	}{
		{ResultSuccess, ResultSuccess},
		{ResultAccepted, ResultAccepted},
		{ResultPartial, ResultPartial},
		{ResultBlocked, ResultBlocked},
		{ResultRetryableError, ResultRetryableError},
		{ResultError, ResultError},
		{"unknown", ""},
		{"", ""},
		{"  success  ", ResultSuccess},
	}
	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			t.Parallel()
			if got := normalizeResultStatus(tt.input); got != tt.want {
				t.Fatalf("normalizeResultStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeVerificationStatus
// ---------------------------------------------------------------------------

func TestNormalizeVerificationStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input VerificationStatus
		want  VerificationStatus
	}{
		{"", VerificationNone},
		{VerificationNone, VerificationNone},
		{VerificationPassed, VerificationPassed},
		{VerificationFailed, VerificationFailed},
		{VerificationUnknown, VerificationUnknown},
		{"invalid", VerificationUnknown},
	}
	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			t.Parallel()
			if got := normalizeVerificationStatus(tt.input); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeErrorCategory
// ---------------------------------------------------------------------------

func TestNormalizeErrorCategory(t *testing.T) {
	t.Parallel()

	categories := []ErrorCategory{
		ErrorAuth, ErrorPermission, ErrorValidation, ErrorNotFound,
		ErrorConflict, ErrorRateLimit, ErrorNetwork, ErrorTimeout,
		ErrorUpstream, ErrorInternal,
	}
	for _, cat := range categories {
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()
			if got := normalizeErrorCategory(cat); got != cat {
				t.Fatalf("got %q, want %q", got, cat)
			}
		})
	}

	// Unknown category defaults to internal.
	if got := normalizeErrorCategory("unknown"); got != ErrorInternal {
		t.Fatalf("got %q, want %q", got, ErrorInternal)
	}
	// Empty defaults to internal.
	if got := normalizeErrorCategory(""); got != ErrorInternal {
		t.Fatalf("got %q, want %q", got, ErrorInternal)
	}
}

package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestRunQualitySummaryPrintsMetrics(t *testing.T) {
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/runtime/quality/summary" {
			t.Fatalf("path = %s, want /runtime/quality/summary", req.URL.Path)
		}
		return jsonHTTPResponse(http.StatusOK, `{"run_count":12,"terminal_run_count":10,"task_success":{"count":8,"total":10,"rate":0.8},"false_success":{"count":1,"total":10,"rate":0.1},"verification_failure":{"count":2,"total":10,"rate":0.2},"trace_count":7}`), nil
	})

	restore := captureStdout(t)
	if err := runQualitySummary(context.Background()); err != nil {
		t.Fatalf("runQualitySummary() error = %v", err)
	}
	output := restore()
	for _, want := range []string{
		"Run Count:            12",
		"Terminal Runs:        10",
		"Task Success:         80.0% (8/10)",
		"False Success:        10.0% (1/10)",
		"Verification Failure: 20.0% (2/10)",
		"Trace Count:          7",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

func TestRunQualityReadinessPrintsBlockers(t *testing.T) {
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/runtime/release-readiness" {
			t.Fatalf("path = %s, want /runtime/release-readiness", req.URL.Path)
		}
		return jsonHTTPResponse(http.StatusOK, `{"ready":false,"checks":[{"id":"sample_size","status":"blocked"}],"blockers":[{"id":"sample_size","status":"blocked","summary":"need more terminal runs"}]}`), nil
	})

	restore := captureStdout(t)
	if err := runQualityReadiness(context.Background()); err != nil {
		t.Fatalf("runQualityReadiness() error = %v", err)
	}
	output := restore()
	for _, want := range []string{
		"Ready:    no",
		"Checks:   1",
		"Blockers: 1",
		"sample_size: need more terminal runs",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

package qualityhttp

import (
	"net/url"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

func TestParseQualitySummaryRequestAppliesDefaultsAndCapsLimit(t *testing.T) {
	t.Parallel()

	query := url.Values{
		"limit": []string{"99999"},
		"since": []string{"2026-04-10T12:00:00Z"},
	}
	req, err := ParseQualitySummaryRequest(query, agent.ScopeFilter{})
	if err != nil {
		t.Fatalf("ParseQualitySummaryRequest() error = %v", err)
	}
	if req.Limit != MaxQualitySummaryLimit {
		t.Fatalf("Limit = %d, want %d", req.Limit, MaxQualitySummaryLimit)
	}
	if got := req.Since.UTC().Format("2006-01-02T15:04:05Z"); got != "2026-04-10T12:00:00Z" {
		t.Fatalf("Since = %q", got)
	}
}

func TestParseReleaseReadinessRequestRejectsInvalidThreshold(t *testing.T) {
	t.Parallel()

	_, err := ParseReleaseReadinessRequest(url.Values{
		"max_fallback_rate": []string{"1.5"},
	}, agent.ScopeFilter{})
	if err == nil || err.Error() != "invalid max_fallback_rate" {
		t.Fatalf("err = %v, want invalid max_fallback_rate", err)
	}
}

func TestDecodeEvalRunRequestRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	var req runtimepkg.EvalRunRequest
	err := DecodeEvalRunRequest(strings.NewReader(`{"suite_id":"browser.smoke"} {"extra":true}`), &req)
	if err == nil || err.Error() != "unexpected trailing data" {
		t.Fatalf("err = %v, want unexpected trailing data", err)
	}
}

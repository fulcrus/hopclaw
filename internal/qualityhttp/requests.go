package qualityhttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

const (
	DefaultQualitySummaryLimit = 200
	MaxQualitySummaryLimit     = 2000
)

func ParseQualitySummaryRequest(query url.Values, scope agent.ScopeFilter) (runtimepkg.QualitySummaryRequest, error) {
	req := runtimepkg.QualitySummaryRequest{
		Scope: scope.Normalize(),
		Limit: DefaultQualitySummaryLimit,
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return runtimepkg.QualitySummaryRequest{}, fmt.Errorf("invalid limit")
		}
		if limit > MaxQualitySummaryLimit {
			limit = MaxQualitySummaryLimit
		}
		req.Limit = limit
	}
	if raw := strings.TrimSpace(query.Get("since")); raw != "" {
		since, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return runtimepkg.QualitySummaryRequest{}, fmt.Errorf("invalid since")
		}
		req.Since = since.UTC()
	}
	return req, nil
}

func ParseReleaseReadinessRequest(query url.Values, scope agent.ScopeFilter) (runtimepkg.ReleaseReadinessRequest, error) {
	summaryReq, err := ParseQualitySummaryRequest(query, scope)
	if err != nil {
		return runtimepkg.ReleaseReadinessRequest{}, err
	}

	thresholds := runtimepkg.DefaultReleaseReadinessThresholds()
	if raw := strings.TrimSpace(query.Get("min_terminal_runs")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return runtimepkg.ReleaseReadinessRequest{}, fmt.Errorf("invalid min_terminal_runs")
		}
		thresholds.MinTerminalRuns = value
	}
	if raw := strings.TrimSpace(query.Get("min_task_success_rate")); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value < 0 || value > 1 {
			return runtimepkg.ReleaseReadinessRequest{}, fmt.Errorf("invalid min_task_success_rate")
		}
		thresholds.MinTaskSuccessRate = value
	}
	if raw := strings.TrimSpace(query.Get("max_false_success_rate")); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value < 0 || value > 1 {
			return runtimepkg.ReleaseReadinessRequest{}, fmt.Errorf("invalid max_false_success_rate")
		}
		thresholds.MaxFalseSuccessRate = value
	}
	if raw := strings.TrimSpace(query.Get("max_verification_failure_rate")); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value < 0 || value > 1 {
			return runtimepkg.ReleaseReadinessRequest{}, fmt.Errorf("invalid max_verification_failure_rate")
		}
		thresholds.MaxVerificationFailureRate = value
	}
	if raw := strings.TrimSpace(query.Get("max_fallback_rate")); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value < 0 || value > 1 {
			return runtimepkg.ReleaseReadinessRequest{}, fmt.Errorf("invalid max_fallback_rate")
		}
		thresholds.MaxFallbackRate = value
	}
	if raw := strings.TrimSpace(query.Get("max_visual_fallback_rate")); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value < 0 || value > 1 {
			return runtimepkg.ReleaseReadinessRequest{}, fmt.Errorf("invalid max_visual_fallback_rate")
		}
		thresholds.MaxVisualFallbackRate = value
	}
	if raw := strings.TrimSpace(query.Get("min_profile_hit_rate")); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value < 0 || value > 1 {
			return runtimepkg.ReleaseReadinessRequest{}, fmt.Errorf("invalid min_profile_hit_rate")
		}
		thresholds.MinProfileHitRate = value
	}

	return runtimepkg.ReleaseReadinessRequest{
		SummaryRequest: summaryReq,
		Thresholds:     thresholds,
	}, nil
}

func DecodeEvalRunRequest(reader io.Reader, dst *runtimepkg.EvalRunRequest) error {
	if reader == nil {
		return fmt.Errorf("request body is required")
	}
	if dst == nil {
		return fmt.Errorf("eval run request destination is required")
	}
	dec := json.NewDecoder(reader)
	if err := dec.Decode(dst); err != nil {
		return err
	}

	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing data")
		}
		return err
	}
	return nil
}

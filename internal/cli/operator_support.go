package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/approval"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

func loadApprovals(ctx context.Context, client *GatewayClient, status string, limit int) ([]*runtimepkg.ApprovalView, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	values := url.Values{}
	if status = strings.TrimSpace(status); status != "" {
		values.Set("status", status)
	}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	path := "/runtime/approvals"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var resp struct {
		Items []*runtimepkg.ApprovalView `json:"items"`
		Count int                        `json:"count"`
	}
	if err := client.Get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func resolveApprovalView(ctx context.Context, client *GatewayClient, id string, status approval.Status, note string) (*runtimepkg.ApprovalView, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	resolution := approval.Resolution{
		Status:     status,
		ResolvedBy: "cli",
		Note:       strings.TrimSpace(note),
	}
	var view runtimepkg.ApprovalView
	if err := client.Post(ctx, "/runtime/approvals/"+strings.TrimSpace(id)+"/resolve", resolution, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

func loadApprovalView(ctx context.Context, client *GatewayClient, id string) (*runtimepkg.ApprovalView, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	var view runtimepkg.ApprovalView
	if err := client.Get(ctx, "/runtime/approvals/"+strings.TrimSpace(id), &view); err != nil {
		return nil, err
	}
	return &view, nil
}

func loadQualitySummary(ctx context.Context, client *GatewayClient) (*runtimepkg.QualitySummary, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	var summary runtimepkg.QualitySummary
	if err := client.Get(ctx, "/runtime/quality/summary", &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

func loadReleaseReadiness(ctx context.Context, client *GatewayClient) (*runtimepkg.ReleaseReadinessReport, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	var report runtimepkg.ReleaseReadinessReport
	if err := client.Get(ctx, "/runtime/release-readiness", &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func loadEvalSuites(ctx context.Context, client *GatewayClient) ([]runtimepkg.EvalSuite, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	var resp struct {
		Items []runtimepkg.EvalSuite `json:"items"`
		Count int                    `json:"count"`
	}
	if err := client.Get(ctx, "/runtime/evals/suites", &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func runEvalSuiteReport(ctx context.Context, client *GatewayClient, suiteID string) (*runtimepkg.EvalSuiteRunReport, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	req := runtimepkg.EvalRunRequest{SuiteID: strings.TrimSpace(suiteID)}
	var report runtimepkg.EvalSuiteRunReport
	if err := client.Post(ctx, "/runtime/evals/run", req, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func loadRunViews(ctx context.Context, client *GatewayClient, sessionID string, limit int) ([]*runtimepkg.RunListView, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	if limit <= 0 {
		limit = messageDefaultLimit
	}
	values := url.Values{}
	values.Set("limit", strconv.Itoa(limit))
	values.Set("include", "verification")
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" {
		values.Set("session_id", sessionID)
	}
	var resp struct {
		Items []*runtimepkg.RunListView `json:"items"`
		Count int                       `json:"count"`
	}
	if err := client.Get(ctx, "/runtime/runs?"+values.Encode(), &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func loadRunView(ctx context.Context, client *GatewayClient, runID string) (*runtimepkg.RunListView, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	var view runtimepkg.RunListView
	if err := client.Get(ctx, "/runtime/runs/"+strings.TrimSpace(runID), &view); err != nil {
		return nil, err
	}
	return &view, nil
}

func loadRunCompletionText(ctx context.Context, client *GatewayClient, runID string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("gateway client is required")
	}
	var completion messageCompletionResponse
	if err := client.Get(ctx, "/runtime/runs/"+strings.TrimSpace(runID)+"/completion", &completion); err != nil {
		return "", err
	}
	return messageCompletionText(completion), nil
}

func loadRunResult(ctx context.Context, client *GatewayClient, runID string) (*runtimepkg.RunResult, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	var result runtimepkg.RunResult
	if err := client.Get(ctx, "/runtime/runs/"+strings.TrimSpace(runID)+"/result", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

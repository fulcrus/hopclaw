package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

func TestRunEvalsListPrintsSuites(t *testing.T) {
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/runtime/evals/suites" {
			t.Fatalf("path = %s, want /runtime/evals/suites", req.URL.Path)
		}
		return jsonHTTPResponse(http.StatusOK, `{"items":[{"id":"browser.smoke","name":"Browser Smoke","surface":"browser","cases":[{"id":"read_example_domain","name":"Read Example Domain","prompt":"Read example.com"}]}],"count":1}`), nil
	})

	restore := captureStdout(t)
	if err := runEvalsList(context.Background()); err != nil {
		t.Fatalf("runEvalsList() error = %v", err)
	}
	output := restore()
	if !strings.Contains(output, "Browser Smoke | browser | 1 cases") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestRunEvalsRunPostsSuiteID(t *testing.T) {
	var gotReq runtimepkg.EvalRunRequest
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", req.Method)
		}
		if req.URL.Path != "/runtime/evals/run" {
			t.Fatalf("path = %s, want /runtime/evals/run", req.URL.Path)
		}
		if err := json.NewDecoder(req.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, `{"suite":{"id":"browser.smoke","name":"Browser Smoke"},"status":"passed","case_count":1,"passed":1,"failed":0,"errored":0}`), nil
	})

	restore := captureStdout(t)
	if err := runEvalsRun(context.Background(), "browser.smoke"); err != nil {
		t.Fatalf("runEvalsRun() error = %v", err)
	}
	if gotReq.SuiteID != "browser.smoke" {
		t.Fatalf("SuiteID = %q, want browser.smoke", gotReq.SuiteID)
	}
	output := restore()
	for _, want := range []string{
		"Suite:   browser.smoke",
		"Status:  passed",
		"Cases:   1",
		"Passed:  1",
		"Failed:  0",
		"Errored: 0",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

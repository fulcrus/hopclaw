package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRunProjectListPrintsProjects(t *testing.T) {
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/runtime/projects" {
			t.Fatalf("path = %s, want /runtime/projects", req.URL.Path)
		}
		return jsonHTTPResponse(http.StatusOK, `[{"name":"hopclaw","directory":"/repo/hopclaw"}]`), nil
	})

	restore := captureStdout(t)
	if err := runProjectList(context.Background()); err != nil {
		t.Fatalf("runProjectList() error = %v", err)
	}
	output := restore()
	if !strings.Contains(output, "hopclaw | /repo/hopclaw") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestRunProjectShowPrintsDetails(t *testing.T) {
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/runtime/projects/hopclaw" {
			t.Fatalf("path = %s, want /runtime/projects/hopclaw", req.URL.Path)
		}
		return jsonHTTPResponse(http.StatusOK, `{"name":"hopclaw","directory":"/repo/hopclaw","git_repo":"github.com/fulcrus/hopclaw"}`), nil
	})

	restore := captureStdout(t)
	if err := runProjectShow(context.Background(), "hopclaw"); err != nil {
		t.Fatalf("runProjectShow() error = %v", err)
	}
	output := restore()
	for _, want := range []string{"Project: hopclaw", "Directory: /repo/hopclaw", "Git Repo: github.com/fulcrus/hopclaw"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

func TestRunProjectDeletePrintsConfirmation(t *testing.T) {
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodDelete {
			t.Fatalf("method = %s, want DELETE", req.Method)
		}
		if req.URL.Path != "/runtime/projects/hopclaw" {
			t.Fatalf("path = %s, want /runtime/projects/hopclaw", req.URL.Path)
		}
		return jsonHTTPResponse(http.StatusOK, `{"ok":true}`), nil
	})

	restore := captureStdout(t)
	if err := runProjectDelete(context.Background(), "hopclaw"); err != nil {
		t.Fatalf("runProjectDelete() error = %v", err)
	}
	output := restore()
	if !strings.Contains(output, `✓ Project "hopclaw" deleted.`) {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestRunProjectRenamePrintsConfirmation(t *testing.T) {
	var gotBody map[string]string
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPatch {
			t.Fatalf("method = %s, want PATCH", req.Method)
		}
		if req.URL.Path != "/runtime/projects/hopclaw" {
			t.Fatalf("path = %s, want /runtime/projects/hopclaw", req.URL.Path)
		}
		if err := json.NewDecoder(req.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, `{"ok":true}`), nil
	})

	restore := captureStdout(t)
	if err := runProjectRename(context.Background(), "hopclaw", "hopclaw-next"); err != nil {
		t.Fatalf("runProjectRename() error = %v", err)
	}
	if gotBody["name"] != "hopclaw-next" {
		t.Fatalf("body name = %q, want hopclaw-next", gotBody["name"])
	}
	output := restore()
	if !strings.Contains(output, `✓ Project "hopclaw" renamed to "hopclaw-next".`) {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestProjectDeleteCmdCancelsWithoutConfirmation(t *testing.T) {
	var deleteCalled bool
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		deleteCalled = true
		return jsonHTTPResponse(http.StatusOK, `{"ok":true}`), nil
	})

	cmd := newProjectDeleteCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"hopclaw"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if deleteCalled {
		t.Fatal("expected delete request to be skipped")
	}
	text := out.String()
	if !strings.Contains(text, `Delete project "hopclaw" and all its memories? [y/N] `) {
		t.Fatalf("prompt missing from output: %q", text)
	}
	if !strings.Contains(text, "Cancelled.") {
		t.Fatalf("cancel message missing from output: %q", text)
	}
}

func TestProjectDeleteCmdYesSkipsPrompt(t *testing.T) {
	var deleteCalled bool
	withGatewayClientStub(t, func(req *http.Request) (*http.Response, error) {
		deleteCalled = true
		if req.Method != http.MethodDelete {
			t.Fatalf("method = %s, want DELETE", req.Method)
		}
		if req.URL.Path != "/runtime/projects/hopclaw" {
			t.Fatalf("path = %s, want /runtime/projects/hopclaw", req.URL.Path)
		}
		return jsonHTTPResponse(http.StatusOK, `{"ok":true}`), nil
	})

	restore := captureStdout(t)
	cmd := newProjectDeleteCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--yes", "hopclaw"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !deleteCalled {
		t.Fatal("expected delete request")
	}
	if strings.Contains(out.String(), "Delete project") {
		t.Fatalf("unexpected prompt output: %q", out.String())
	}
	if !strings.Contains(restore(), `✓ Project "hopclaw" deleted.`) {
		t.Fatal("expected delete confirmation on stdout")
	}
}

func TestNewProjectCmdRegistersSubcommands(t *testing.T) {
	cmd := newProjectCmd()
	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	if !strings.Contains(strings.Join(names, ","), "list") || !strings.Contains(strings.Join(names, ","), "show") || !strings.Contains(strings.Join(names, ","), "rename") || !strings.Contains(strings.Join(names, ","), "delete") {
		t.Fatalf("unexpected subcommands: %v", names)
	}

	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
}

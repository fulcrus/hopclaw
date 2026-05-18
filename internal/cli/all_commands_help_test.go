package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestAllCLICommandsRenderHelp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	paths := collectCommandPaths(newRootCmd(), nil)
	if len(paths) == 0 {
		t.Fatal("no CLI command paths discovered")
	}

	for _, path := range paths {
		path := path
		t.Run(strings.Join(path, "_"), func(t *testing.T) {
			cmd := newRootCmd()
			var stdout strings.Builder
			var stderr strings.Builder
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append(path, "--help"))

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute(%q --help) error = %v\nstdout=%q\nstderr=%q", strings.Join(path, " "), err, stdout.String(), stderr.String())
			}
			if stdout.Len() == 0 && stderr.Len() == 0 {
				t.Fatalf("Execute(%q --help) produced no output", strings.Join(path, " "))
			}
		})
	}
}

func collectCommandPaths(cmd interface{ Commands() []*cobra.Command }, prefix []string) [][]string {
	var out [][]string
	for _, child := range cmd.Commands() {
		if child.Hidden {
			continue
		}
		name := strings.TrimSpace(child.Name())
		if name == "" || name == "help" || strings.HasPrefix(name, "__") {
			continue
		}
		path := append(append([]string(nil), prefix...), name)
		out = append(out, path)
		out = append(out, collectCommandPaths(child, path)...)
	}
	return out
}

func TestAllCLICommandsRenderHelpFromBuiltBinary(t *testing.T) {
	bin := buildCLIIntegrationBinary(t)
	paths := collectCommandPaths(newRootCmd(), nil)
	if len(paths) == 0 {
		t.Fatal("no CLI command paths discovered")
	}

	for _, path := range paths {
		path := path
		t.Run(strings.Join(path, "_"), func(t *testing.T) {
			cmd := exec.Command(bin, append(path, "--help")...)
			cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("binary help failed for %q: %v\noutput=%q", strings.Join(path, " "), err, string(output))
			}
			if len(output) == 0 {
				t.Fatalf("binary help for %q produced no output", strings.Join(path, " "))
			}
		})
	}
}

func TestSelectedCLICommandsExecuteFromBuiltBinary(t *testing.T) {
	bin := buildCLIIntegrationBinary(t)
	configPath := filepath.Join(t.TempDir(), "hopclaw.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  address: http://127.0.0.1:16280\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", configPath, err)
	}

	tests := []struct {
		name string
		args []string
		env  []string
		want string
	}{
		{name: "version", args: []string{"version"}, want: "hopclaw "},
		{name: "completion_bash", args: []string{"completion", "bash"}, want: "hopclaw"},
		{name: "completion_zsh", args: []string{"completion", "zsh"}, want: "hopclaw"},
		{name: "completion_fish", args: []string{"completion", "fish"}, want: "hopclaw"},
		{name: "completion_powershell", args: []string{"completion", "powershell"}, want: "hopclaw"},
		{name: "config_path", args: []string{"config", "path"}, env: []string{"HOPCLAW_CONFIG=" + configPath}, want: configPath},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bin, tt.args...)
			env := append(os.Environ(), "HOME="+t.TempDir())
			env = append(env, tt.env...)
			cmd.Env = env
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("binary command failed for %q: %v\noutput=%q", strings.Join(tt.args, " "), err, string(output))
			}
			if !strings.Contains(string(output), tt.want) {
				t.Fatalf("binary command output = %q, want substring %q", string(output), tt.want)
			}
		})
	}
}

func buildCLIIntegrationBinary(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	bin := filepath.Join(t.TempDir(), "hopclaw-test")

	ldflags := strings.Join([]string{
		"-X github.com/fulcrus/hopclaw/internal/version.Version=2026.04.06",
		"-X github.com/fulcrus/hopclaw/internal/version.Channel=stable",
		"-X github.com/fulcrus/hopclaw/internal/version.GitCommit=testcommit",
		"-X github.com/fulcrus/hopclaw/internal/version.BuildDate=2026-04-06T12:00:00Z",
	}, " ")
	buildEnvRoot := t.TempDir()
	goCache := filepath.Join(buildEnvRoot, "gocache")
	goTmp := filepath.Join(buildEnvRoot, "gotmp")
	for _, dir := range []string{goCache, goTmp} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, "./cmd/hopclaw")
	cmd.Dir = repoRoot
	cmd.Env = append(filteredEnv(os.Environ(), "GOCACHE", "GOTMPDIR"), "GOCACHE="+goCache, "GOTMPDIR="+goTmp)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/hopclaw error = %v\noutput=%q", err, string(output))
	}
	return bin
}

func filteredEnv(env []string, dropKeys ...string) []string {
	if len(dropKeys) == 0 {
		return append([]string(nil), env...)
	}
	drop := make(map[string]struct{}, len(dropKeys))
	for _, key := range dropKeys {
		drop[strings.TrimSpace(key)] = struct{}{}
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			out = append(out, entry)
			continue
		}
		if _, exists := drop[key]; exists {
			continue
		}
		out = append(out, entry)
	}
	return out
}

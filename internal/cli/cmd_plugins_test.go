package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginsValidateCmd(t *testing.T) {

	dir := t.TempDir()
	manifest := `name: demo
version: "1.0.0"
providers:
  demo:
    api: openai-completions
`
	writeTestFile(t, filepath.Join(dir, "hopclaw.plugin.yaml"), manifest)

	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"plugins", "validate", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(plugins validate) error: %v", err)
	}

	if got := buf.String(); !strings.Contains(got, "Plugin demo is valid") {
		t.Fatalf("output = %q", got)
	}
}

func TestPluginsValidateCmdAcceptsManifestFilePath(t *testing.T) {

	dir := t.TempDir()
	path := filepath.Join(dir, "hopclaw.plugin.yaml")
	manifest := `name: demo
version: "1.0.0"
commands:
  - name: inspect
    exec: ./inspect
`
	writeTestFile(t, path, manifest)

	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"plugins", "validate", path})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(plugins validate manifest path) error: %v", err)
	}

	if got := buf.String(); !strings.Contains(got, "Plugin demo is valid") {
		t.Fatalf("output = %q", got)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

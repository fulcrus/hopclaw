package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	pluginpkg "github.com/fulcrus/hopclaw/plugin"
	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

func TestPluginsInitCmdScaffoldsToolPlugin(t *testing.T) {

	dir := t.TempDir()
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"plugins", "init", "demo-tool", "--dir", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(plugins init tool) error = %v", err)
	}

	target := filepath.Join(dir, "demo-tool")
	for _, rel := range []string{"go.mod", "hopclaw.plugin.yaml", "plugin.go", "plugin_test.go"} {
		if _, err := os.Stat(filepath.Join(target, rel)); err != nil {
			t.Fatalf("missing scaffold file %q: %v", rel, err)
		}
	}

	loaded, err := pluginpkg.Load(target)
	if err != nil {
		t.Fatalf("plugin.Load() error = %v", err)
	}
	if errs := sdkplugin.ValidateManifest(loaded.Manifest); len(errs) != 0 {
		t.Fatalf("ValidateManifest() errors = %#v", errs)
	}
	if got := buf.String(); !strings.Contains(got, "Initialized tool plugin") {
		t.Fatalf("output = %q", got)
	}

	appendGoReplaceDirective(t, filepath.Join(target, "go.mod"))
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = target
	cmd.Env = pluginScaffoldTestEnv(t)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test ./... error = %v\n%s", err, output)
	}

	goModData, err := os.ReadFile(filepath.Join(target, "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile(go.mod) error = %v", err)
	}
	if !strings.Contains(string(goModData), "require github.com/fulcrus/hopclaw") {
		t.Fatalf("go.mod = %q, want HopClaw module requirement", string(goModData))
	}
	if !strings.Contains(string(goModData), "replace github.com/fulcrus/hopclaw => ") {
		t.Fatalf("go.mod = %q, want local replace directive during in-repo scaffolding", string(goModData))
	}
}

func TestPluginsInitCmdScaffoldsSkillPlugin(t *testing.T) {

	dir := t.TempDir()
	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"plugins", "init", "demo-skill", "--kind", "skill", "--dir", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(plugins init skill) error = %v", err)
	}

	target := filepath.Join(dir, "demo-skill")
	skillPath := filepath.Join(target, "skills", "demo-skill", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", skillPath, err)
	}
	if !strings.Contains(string(data), "## TL;DR") {
		t.Fatalf("SKILL.md = %q", string(data))
	}

	loaded, err := pluginpkg.Load(target)
	if err != nil {
		t.Fatalf("plugin.Load() error = %v", err)
	}
	if loaded.Manifest.SkillsDir != "skills" {
		t.Fatalf("SkillsDir = %q, want skills", loaded.Manifest.SkillsDir)
	}
}

func TestPluginsInitCmdRejectsUnknownKind(t *testing.T) {

	root := newRootCmd()
	root.SetArgs([]string{"plugins", "init", "demo", "--kind", "unknown"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported plugin kind") {
		t.Fatalf("Execute() error = %v, want unsupported plugin kind", err)
	}
}

func TestPluginsCmdHelpIncludesInit(t *testing.T) {

	root := newRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"plugins", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(plugins --help) error = %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "init") {
		t.Fatalf("output = %q, want init subcommand", got)
	}
}

func TestPluginAliasInitCmdScaffoldsToolPlugin(t *testing.T) {

	dir := t.TempDir()
	root := newRootCmd()
	root.SetArgs([]string{"plugin", "init", "demo-alias", "--dir", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(plugin init) error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "demo-alias", "go.mod")); err != nil {
		t.Fatalf("missing go.mod from plugin alias scaffold: %v", err)
	}
}

func pluginScaffoldTestEnv(t *testing.T) []string {
	t.Helper()

	gocache := filepath.Join(t.TempDir(), "gocache")
	if err := os.MkdirAll(gocache, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", gocache, err)
	}
	return append(os.Environ(), "GOWORK=off", "GOCACHE="+gocache)
}

func appendGoReplaceDirective(t *testing.T, goModPath string) {
	t.Helper()

	repoRoot, ok := locatePluginScaffoldModuleRoot()
	if !ok {
		repoRoot, ok = locatePluginScaffoldModuleRootFromTestSource()
	}
	if !ok {
		t.Fatal("failed to locate repo module root for plugin scaffold test")
	}

	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", goModPath, err)
	}
	if strings.Contains(string(data), "replace "+pluginScaffoldModulePath+" => ") {
		return
	}

	file, err := os.OpenFile(goModPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile(%q) error = %v", goModPath, err)
	}
	defer file.Close()

	if len(data) > 0 && data[len(data)-1] != '\n' {
		if _, err := file.WriteString("\n"); err != nil {
			t.Fatalf("WriteString(newline) error = %v", err)
		}
	}
	if _, err := fmt.Fprintf(file, "replace %s => %s\n", pluginScaffoldModulePath, repoRoot); err != nil {
		t.Fatalf("Write replace directive error = %v", err)
	}
}

func locatePluginScaffoldModuleRootFromTestSource() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}

	dir := filepath.Dir(file)
	for {
		goModPath := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(goModPath)
		if err == nil && strings.Contains(string(data), "module "+pluginScaffoldModulePath) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

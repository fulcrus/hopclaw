package skill

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func projectRootFromCaller(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Join(filepath.Dir(file), "..")
}

func TestParseDirLoadsBundleManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bundleDir := filepath.Join(root, "bundle")
	runtimeDir := filepath.Join(bundleDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(runtime): %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "run.py"), []byte("#!/usr/bin/env python3\nprint('ok')\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(run.py): %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "BUNDLE.yaml"), []byte(`
id: sample-bundle
version: 1.0.0
name: Sample Bundle
description: Sample executable bundle
runtime:
  type: executable
  executable:
    entry: runtime/run.py
tools:
  - name: sample.echo
    description: Echo text
    side_effect_class: read
`), 0o644); err != nil {
		t.Fatalf("WriteFile(BUNDLE.yaml): %v", err)
	}

	spec, err := ParseDir(bundleDir)
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}
	if spec.Bundle == nil {
		t.Fatal("expected bundle manifest")
	}
	if spec.Bundle.ID != "sample-bundle" {
		t.Fatalf("bundle id = %q", spec.Bundle.ID)
	}
	if spec.Name != "Sample Bundle" {
		t.Fatalf("spec.Name = %q", spec.Name)
	}
	if spec.Body == "" {
		t.Fatal("expected default bundle instructions")
	}
}

func TestCompileFeishuSuiteBundle(t *testing.T) {
	t.Parallel()

	root := projectRootFromCaller(t)
	src := SkillSource{
		Kind: SourceWorkspace,
		Root: filepath.Join(root, "bundles"),
		Dir:  filepath.Join(root, "bundles", "feishu-suite"),
	}
	spec, err := ParseDir(src.Dir)
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}

	pkg, err := DefaultCompiler{}.Compile(context.Background(), src, spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if pkg.Kind != SkillKindExecutable {
		t.Fatalf("pkg.Kind = %q", pkg.Kind)
	}
	if pkg.Status == StatusBlocked {
		t.Fatalf("pkg.Status = %q, issues = %#v", pkg.Status, pkg.Issues)
	}
	if pkg.OpenClaw.SkillKey != "enterprise.feishu-suite" {
		t.Fatalf("skill key = %q", pkg.OpenClaw.SkillKey)
	}
	if len(pkg.ToolManifests) < 10 {
		t.Fatalf("expected many tool manifests, got %d", len(pkg.ToolManifests))
	}
	found := false
	for _, tool := range pkg.ToolManifests {
		if tool.Name == "feishu.doc.write" {
			found = true
			if tool.Runtime.Entry != "runtime/feishu_suite.py" {
				t.Fatalf("runtime entry = %q", tool.Runtime.Entry)
			}
			if tool.SideEffectClass != "external_write" {
				t.Fatalf("side effect = %q", tool.SideEffectClass)
			}
		}
	}
	if !found {
		t.Fatal("feishu.doc.write not found in compiled tool manifests")
	}
}

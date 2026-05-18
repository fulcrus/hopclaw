package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallerInstallFromRegistryVerifiesChecksum(t *testing.T) {
	t.Parallel()

	manifest := []byte(`name: demo
version: "1.2.3"
description: "Demo plugin"
channels:
  demo:
    type: stdio
    command: "./demo"
`)
	sum := sha256.Sum256(manifest)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugins/demo":
			_ = json.NewEncoder(w).Encode(registryPluginInfo{
				Name:        "demo",
				Version:     "1.2.3",
				DownloadURL: server.URL + "/artifacts/" + manifestFile,
				Checksum:    "sha256:" + hex.EncodeToString(sum[:]),
			})
		case "/artifacts/" + manifestFile:
			w.Write(manifest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	inst := &Installer{
		PluginDir:                 t.TempDir(),
		RegistryURL:               server.URL,
		Manager:                   NewManager(),
		AllowPrivateDownloadHosts: true,
	}

	result, err := inst.Install(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Name != "demo" || result.Version != "1.2.3" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(result.Dir, manifestFile)); err != nil {
		t.Fatalf("Stat(manifest) error = %v", err)
	}
}

func TestInstallerInstallFromRegistryRejectsChecksumMismatch(t *testing.T) {
	t.Parallel()

	manifest := []byte(`name: demo
version: "1.2.3"
description: "Demo plugin"
channels:
  demo:
    type: stdio
    command: "./demo"
`)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugins/demo":
			_ = json.NewEncoder(w).Encode(registryPluginInfo{
				Name:        "demo",
				Version:     "1.2.3",
				DownloadURL: server.URL + "/artifacts/" + manifestFile,
				Checksum:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			})
		case "/artifacts/" + manifestFile:
			w.Write(manifest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	inst := &Installer{
		PluginDir:   root,
		RegistryURL: server.URL,
	}

	if _, err := inst.Install(context.Background(), "demo"); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	if _, err := os.Stat(filepath.Join(root, "demo")); !os.IsNotExist(err) {
		t.Fatalf("expected failed install dir cleanup, stat err = %v", err)
	}
}

func TestInstallerInstallFromGitHubDoesNotRunMakefile(t *testing.T) {
	root := t.TempDir()
	sourceRepo := filepath.Join(root, "source-repo")
	if err := os.MkdirAll(sourceRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceRepo) error = %v", err)
	}
	manifest := []byte(`name: repo-demo
version: "0.1.0"
description: "Repo plugin"
channels:
  repo-demo:
    type: stdio
    command: "./repo-demo"
`)
	if err := os.WriteFile(filepath.Join(sourceRepo, manifestFile), manifest, 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRepo, "Makefile"), []byte("all:\n\t@touch made.txt\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(Makefile) error = %v", err)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(binDir) error = %v", err)
	}
	gitScript := "#!/bin/sh\nset -eu\nif [ \"$1\" = \"clone\" ]; then\n  mkdir -p \"$4\"\n  cp -R \"$FAKE_REPO_SRC\"/. \"$4\"\n  exit 0\nfi\nif [ \"$1\" = \"-C\" ]; then\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(gitScript), 0o755); err != nil {
		t.Fatalf("WriteFile(git) error = %v", err)
	}
	makeScript := "#!/bin/sh\nset -eu\n: > \"$FAKE_MAKE_MARKER\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "make"), []byte(makeScript), 0o755); err != nil {
		t.Fatalf("WriteFile(make) error = %v", err)
	}

	marker := filepath.Join(root, "make-ran")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_REPO_SRC", sourceRepo)
	t.Setenv("FAKE_MAKE_MARKER", marker)

	inst := &Installer{
		PluginDir: filepath.Join(root, "plugins"),
		Manager:   NewManager(),
	}

	result, err := inst.Install(context.Background(), "https://github.com/example/hopclaw-plugin-repo-demo")
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Name != "repo-demo" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("expected make to remain unused, stat err = %v", err)
	}
}

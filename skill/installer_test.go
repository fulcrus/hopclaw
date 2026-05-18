package skill

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAutoInstallerDownloadInstallsExecutable(t *testing.T) {
	prev := allowPrivateSkillDownloads
	allowPrivateSkillDownloads = true
	defer func() { allowPrivateSkillDownloads = prev }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		_, _ = w.Write([]byte("#!/bin/sh\necho helper\n"))
	}))
	defer server.Close()

	installDir := t.TempDir()
	installer := AutoInstaller{
		InstallDir: installDir,
		HTTPClient: server.Client(),
		LookPath: func(string) (string, error) {
			return "", exec.ErrNotFound
		},
	}

	path, err := installer.Install(context.Background(), SkillDependency{
		Binary:  "helper",
		Install: InstallDownload,
		Package: server.URL + "/helper",
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	wantPath := filepath.Join(installDir, "helper")
	if runtime.GOOS == "windows" {
		wantPath += ".exe"
	}
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "#!/bin/sh\necho helper\n" {
		t.Fatalf("downloaded content = %q", string(data))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		t.Fatalf("mode = %v, want executable bit set", info.Mode())
	}
}

func TestAutoInstallerReturnsExistingPathWhenBinaryPresent(t *testing.T) {
	t.Parallel()

	installer := AutoInstaller{
		LookPath: func(name string) (string, error) {
			if name == "helper" {
				return "/tmp/helper", nil
			}
			return "", errors.New("not found")
		},
	}

	path, err := installer.Install(context.Background(), SkillDependency{
		Binary:  "helper",
		Install: InstallDownload,
		Package: "https://example.com/helper",
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if path != "/tmp/helper" {
		t.Fatalf("path = %q, want %q", path, "/tmp/helper")
	}
}

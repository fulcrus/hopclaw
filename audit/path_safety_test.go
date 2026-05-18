package audit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathChecker_NullBytes(t *testing.T) {
	t.Parallel()

	pc := NewPathChecker(nil)
	err := pc.CheckPath("/tmp/foo\x00bar")
	if err == nil {
		t.Fatal("expected error for null byte in path")
	}
}

func TestPathChecker_Traversal(t *testing.T) {
	t.Parallel()

	pc := NewPathChecker(nil)
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"clean path", "/tmp/foo/bar.txt", false},
		{"relative clean", "foo/bar.txt", false},
		{"traversal", "/tmp/../etc/passwd", true},
		{"double traversal", "../../etc/shadow", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := pc.checkTraversal(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkTraversal(%q) err = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestPathChecker_SensitivePatterns(t *testing.T) {
	t.Parallel()

	pc := NewPathChecker(nil)
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"normal file", "/home/user/project/main.go", false},
		{"ssh key", "/home/user/.ssh/id_rsa", true},
		{"env file", "/app/.env", true},
		{"aws creds", "/home/user/.aws/credentials", true},
		{"kube config", "/home/user/.kube/config", true},
		{"docker config", "/home/user/.docker/config.json", true},
		{"gnupg", "/home/user/.gnupg/secring.gpg", true},
		{"git config", "/repo/.git/config", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := pc.checkSensitivePatterns(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkSensitivePatterns(%q) err = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestPathChecker_AbsoluteOutsideRoots(t *testing.T) {
	t.Parallel()

	// Use real temp directories so symlink resolution works on macOS
	// where /tmp -> /private/tmp.
	rootA := t.TempDir()
	rootB := t.TempDir()

	// Create a subdirectory inside rootA for testing.
	insideDir := filepath.Join(rootA, "src")
	if err := os.MkdirAll(insideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	insideFile := filepath.Join(insideDir, "main.go")
	if err := os.WriteFile(insideFile, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	pc := NewPathChecker([]string{rootA, rootB})
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"inside rootA", insideFile, false},
		{"inside rootB", filepath.Join(rootB, "build/output"), false},
		{"exact root", rootA, false},
		{"outside roots", "/etc/passwd", true},
		{"relative path", "src/main.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := pc.checkAbsoluteInRelative(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkAbsoluteInRelative(%q) err = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestPathChecker_SymlinkOutsideRoots(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	allowedDir := filepath.Join(dir, "allowed")
	outsideDir := filepath.Join(dir, "outside")
	if err := os.MkdirAll(allowedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a real file outside allowed root.
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create symlink inside allowed root pointing outside.
	symlinkPath := filepath.Join(allowedDir, "link.txt")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatal(err)
	}

	pc := NewPathChecker([]string{allowedDir})
	err := pc.checkSymlinks(symlinkPath)
	if err == nil {
		t.Error("expected error for symlink resolving outside allowed roots")
	}

	// A real file inside should pass.
	insideFile := filepath.Join(allowedDir, "normal.txt")
	if err := os.WriteFile(insideFile, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = pc.checkSymlinks(insideFile)
	if err != nil {
		t.Errorf("unexpected error for file inside allowed root: %v", err)
	}
}

func TestPathChecker_CheckPathCombined(t *testing.T) {
	t.Parallel()

	pc := NewPathChecker(nil)

	// A safe path should pass.
	if err := pc.CheckPath("/home/user/project/main.go"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}

	// Null byte should fail.
	if err := pc.CheckPath("foo\x00bar"); err == nil {
		t.Error("expected error for null byte")
	}

	// Traversal should fail.
	if err := pc.CheckPath("../../etc/passwd"); err == nil {
		t.Error("expected error for traversal")
	}

	// Sensitive path should fail.
	if err := pc.CheckPath("/home/user/.ssh/id_rsa"); err == nil {
		t.Error("expected error for sensitive path")
	}
}

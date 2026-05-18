package agent

import (
	"context"
	"strings"
	"testing"
)

func TestProjectID(t *testing.T) {
	tests := []struct {
		dir    string
		prefix string // expected prefix
	}{
		{"/home/user/projects/hopclaw", "proj_hopclaw_"},
		{"/home/user/projects/webapp", "proj_webapp_"},
		{"/home/user/my-project", "proj_my_project_"},
		{"/root", "proj_root_"},
		{"/tmp/test.dir", "proj_test_dir_"},
	}
	for _, tt := range tests {
		id := ProjectID(tt.dir)
		if !strings.HasPrefix(id, tt.prefix) {
			t.Errorf("ProjectID(%q) = %q, want prefix %q", tt.dir, id, tt.prefix)
		}
		// ID should be deterministic
		if id2 := ProjectID(tt.dir); id != id2 {
			t.Errorf("ProjectID not deterministic: %q vs %q", id, id2)
		}
		// Should have exactly 8 hex chars at the end
		parts := strings.Split(id, "_")
		hash := parts[len(parts)-1]
		if len(hash) != 8 {
			t.Errorf("expected 8 char hash suffix, got %q", hash)
		}
	}
}

func TestProjectIDUniqueness(t *testing.T) {
	// Same directory name but different paths should produce different IDs
	id1 := ProjectID("/home/alice/project")
	id2 := ProjectID("/home/bob/project")
	if id1 == id2 {
		t.Errorf("different paths should produce different IDs: %q == %q", id1, id2)
	}
}

func TestSanitizeProjectName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"HopClaw", "hopclaw"},
		{"my-project", "my_project"},
		{"test.dir", "test_dir"},
		{"UPPER_case", "upper_case"},
		{"", "unnamed"},
		{"---", "unnamed"},
		{"a-very-long-project-name-that-exceeds-twenty-chars", "a_very_long_project_"},
	}
	for _, tt := range tests {
		got := sanitizeProjectName(tt.input, 20)
		if got != tt.expected {
			t.Errorf("sanitizeProjectName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestInMemoryProjectStore(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryProjectStore()

	// Upsert
	p := Project{
		ID:        "proj_test_abc123",
		Name:      "Test",
		Directory: "/home/user/test",
		GitRepo:   "github.com/user/test",
	}
	if err := store.Upsert(ctx, p); err != nil {
		t.Fatal(err)
	}

	// FindByID
	found, err := store.FindByID(ctx, "proj_test_abc123")
	if err != nil {
		t.Fatal(err)
	}
	if found == nil || found.Name != "Test" {
		t.Fatalf("FindByID: expected Test, got %v", found)
	}

	// FindByDirectory
	found, err = store.FindByDirectory(ctx, "/home/user/test")
	if err != nil {
		t.Fatal(err)
	}
	if found == nil || found.ID != "proj_test_abc123" {
		t.Fatalf("FindByDirectory: expected proj_test_abc123, got %v", found)
	}

	// FindByName (case insensitive)
	found, err = store.FindByName(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if found == nil || found.ID != "proj_test_abc123" {
		t.Fatalf("FindByName: expected proj_test_abc123, got %v", found)
	}

	// List
	list, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 project, got %d", len(list))
	}

	// Delete
	if err := store.Delete(ctx, "proj_test_abc123"); err != nil {
		t.Fatal(err)
	}
	found, _ = store.FindByID(ctx, "proj_test_abc123")
	if found != nil {
		t.Fatal("expected nil after delete")
	}

	// FindByDirectory not found
	found, _ = store.FindByDirectory(ctx, "/nonexistent")
	if found != nil {
		t.Fatal("expected nil for nonexistent directory")
	}
}

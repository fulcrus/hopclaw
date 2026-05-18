package toolruntime

import (
	"strings"
	"testing"
)

func TestBuiltinCategoryCatalogHasNoRegistrationErrors(t *testing.T) {
	t.Parallel()

	if err := ensureBuiltinCategoriesRegistered(); err != nil {
		t.Fatalf("ensureBuiltinCategoriesRegistered() error = %v", err)
	}
}

func TestBuiltinCategoryCatalogIncludesSelfRegisteredDomains(t *testing.T) {
	t.Parallel()

	catalog := builtinCategoryCatalog()
	if len(catalog) == 0 {
		t.Fatal("expected builtin category catalog to be populated")
	}
	if len(catalog) != 33 {
		t.Fatalf("expected 33 builtin categories, got %d", len(catalog))
	}

	seen := make(map[string]struct{}, len(catalog))
	for _, category := range catalog {
		seen[strings.TrimSpace(category.Name)] = struct{}{}
	}

	for _, name := range []string{"core", "channel", "firecrawl", "watch", "media_generation"} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("builtin category %q not registered", name)
		}
	}
	if _, ok := seen["spreadsheet_xlsx"]; !ok {
		t.Fatal("builtin category \"spreadsheet_xlsx\" not registered")
	}
	if _, ok := seen["gateway"]; ok {
		t.Fatal("gateway category should no longer be registered as a builtin category")
	}
}

func TestNewBuiltinsBuildsCatalogFromRegisteredCategories(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	definitions := builtins.ToolDefinitions(nil)
	if len(definitions) == 0 {
		t.Fatal("expected builtin definitions")
	}

	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		seen[strings.TrimSpace(definition.Name)] = struct{}{}
	}
	for _, name := range []string{"fs.list", "channel.list", "watch.list", "document.create", "image.generate", "video.generate", "music.generate"} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("expected builtin definition %q to be present", name)
		}
	}
	if _, ok := seen["spreadsheet.read_range"]; !ok {
		t.Fatal("expected builtin definition \"spreadsheet.read_range\" to be present")
	}
	if _, ok := seen["browser.open"]; !ok {
		t.Fatal("expected builtin definition \"browser.open\" to be present")
	}
}

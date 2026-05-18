package maps

import "testing"

func TestCloneNilMapReturnsNil(t *testing.T) {
	t.Parallel()

	if got := Clone(nil); got != nil {
		t.Fatalf("Clone(nil) = %#v, want nil", got)
	}
}

func TestClonePreservesEmptyMaps(t *testing.T) {
	t.Parallel()

	in := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"items": []any{
			map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}

	got := Clone(in)
	if got == nil {
		t.Fatal("Clone(in) = nil")
	}

	properties, ok := got["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", got["properties"])
	}
	if properties == nil {
		t.Fatal("properties = nil, want empty map")
	}
	if len(properties) != 0 {
		t.Fatalf("len(properties) = %d, want 0", len(properties))
	}

	items, ok := got["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want single-element []any", got["items"])
	}
	itemSchema, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("items[0] type = %T, want map[string]any", items[0])
	}
	nestedProperties, ok := itemSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("nested properties type = %T, want map[string]any", itemSchema["properties"])
	}
	if nestedProperties == nil {
		t.Fatal("nested properties = nil, want empty map")
	}
}

package toolruntime

import (
	"testing"
)

func TestStringSchema(t *testing.T) {
	t.Parallel()

	schema := stringSchema("a description")
	if schema["type"] != "string" {
		t.Fatalf("type = %q, want string", schema["type"])
	}
	if schema["description"] != "a description" {
		t.Fatalf("description = %q", schema["description"])
	}
}

func TestStringSchemaNoDescription(t *testing.T) {
	t.Parallel()

	schema := stringSchema("")
	if schema["type"] != "string" {
		t.Fatalf("type = %q", schema["type"])
	}
	if _, ok := schema["description"]; ok {
		t.Fatal("should not include description when empty")
	}
}

func TestIntegerSchema(t *testing.T) {
	t.Parallel()

	schema := integerSchema("count of items")
	if schema["type"] != "integer" {
		t.Fatalf("type = %q", schema["type"])
	}
	if schema["description"] != "count of items" {
		t.Fatalf("description = %q", schema["description"])
	}
}

func TestBooleanSchema(t *testing.T) {
	t.Parallel()

	schema := booleanSchema("is active")
	if schema["type"] != "boolean" {
		t.Fatalf("type = %q", schema["type"])
	}
}

func TestNumberSchema(t *testing.T) {
	t.Parallel()

	schema := numberSchema("score")
	if schema["type"] != "number" {
		t.Fatalf("type = %q", schema["type"])
	}
}

func TestStringArraySchema(t *testing.T) {
	t.Parallel()

	schema := stringArraySchema("list of names")
	if schema["type"] != "array" {
		t.Fatalf("type = %q", schema["type"])
	}
	items, ok := schema["items"].(map[string]any)
	if !ok {
		t.Fatal("items should be a map")
	}
	if items["type"] != "string" {
		t.Fatalf("items.type = %q", items["type"])
	}
}

func TestObjectSchema(t *testing.T) {
	t.Parallel()

	props := map[string]any{
		"name": stringSchema("the name"),
		"age":  integerSchema("user age"),
	}
	schema := objectSchema(props, "name")
	if schema["type"] != "object" {
		t.Fatalf("type = %q", schema["type"])
	}
	if schema["additionalProperties"] != false {
		t.Fatal("additionalProperties should be false")
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("required should be []string")
	}
	if len(required) != 1 || required[0] != "name" {
		t.Fatalf("required = %v", required)
	}
}

func TestObjectSchemaNoRequired(t *testing.T) {
	t.Parallel()

	schema := objectSchema(map[string]any{"x": stringSchema("")})
	if _, ok := schema["required"]; ok {
		t.Fatal("should not have required field when none provided")
	}
}

func TestArraySchema(t *testing.T) {
	t.Parallel()

	items := stringSchema("entry")
	schema := arraySchema(items, "list of entries")
	if schema["type"] != "array" {
		t.Fatalf("type = %q", schema["type"])
	}
	if schema["description"] != "list of entries" {
		t.Fatalf("description = %q", schema["description"])
	}
}

func TestBuiltinTextResultSchema(t *testing.T) {
	t.Parallel()

	schema := builtinTextResultSchema("result message")
	if schema["type"] != "object" {
		t.Fatalf("type = %q", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	if _, ok := props["message"]; !ok {
		t.Fatal("expected 'message' in properties")
	}
}

func TestBuiltinFSReadOutputSchema(t *testing.T) {
	t.Parallel()

	schema := builtinFSReadOutputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	if _, ok := props["path"]; !ok {
		t.Fatal("expected 'path' in properties")
	}
	if _, ok := props["content"]; !ok {
		t.Fatal("expected 'content' in properties")
	}
}

func TestBuiltinExecRunOutputSchema(t *testing.T) {
	t.Parallel()

	schema := builtinExecRunOutputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	for _, key := range []string{"command", "dir", "stdout", "stderr", "content"} {
		if _, ok := props[key]; !ok {
			t.Fatalf("expected %q in properties", key)
		}
	}
}

package skill

import (
	"reflect"
	"strings"
	"testing"
)

func TestToolManifestSpecFieldContractsStable(t *testing.T) {
	t.Parallel()

	want := []string{
		"name",
		"description",
		"aliases",
		"input_schema",
		"output_schema",
		"side_effect_class",
		"idempotent",
		"execution_key",
		"timeout",
		"requires_approval",
	}

	if got := tagFieldNames(reflect.TypeOf(ToolManifestSpec{}), "json"); !reflect.DeepEqual(got, want) {
		t.Fatalf("ToolManifestSpec json fields = %#v, want %#v", got, want)
	}
	if got := tagFieldNames(reflect.TypeOf(ToolManifestSpec{}), "yaml"); !reflect.DeepEqual(got, want) {
		t.Fatalf("ToolManifestSpec yaml fields = %#v, want %#v", got, want)
	}
}

func tagFieldNames(typ reflect.Type, tagName string) []string {
	out := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get(tagName)
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

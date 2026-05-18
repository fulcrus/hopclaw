package modules

import (
	"reflect"
	"strings"
	"testing"
)

func TestModuleManifestJSONFieldsStable(t *testing.T) {
	t.Parallel()

	got := jsonFieldNames(reflect.TypeOf(Manifest{}))
	want := []string{"id", "name", "version", "description", "kind", "source", "delivery", "level", "metadata", "default_enabled", "dependencies"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Manifest json fields = %#v, want %#v", got, want)
	}
}

func jsonFieldNames(typ reflect.Type) []string {
	out := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
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

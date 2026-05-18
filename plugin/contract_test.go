package plugin

import (
	"reflect"
	"strings"
	"testing"
)

func TestPluginManifestFieldContractsStable(t *testing.T) {
	t.Parallel()

	want := []string{
		"name",
		"version",
		"description",
		"author",
		"config_schema",
		"ui_hints",
		"provider_auth_env_vars",
		"providers",
		"channels",
		"tools",
		"skills_dir",
		"skills_dirs",
		"hooks_dir",
		"mcp_servers",
		"agents",
		"commands",
	}

	if got := tagFieldNames(reflect.TypeOf(Manifest{}), "json"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Manifest json fields = %#v, want %#v", got, want)
	}
	if got := tagFieldNames(reflect.TypeOf(Manifest{}), "yaml"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Manifest yaml fields = %#v, want %#v", got, want)
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

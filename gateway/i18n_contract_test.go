package gateway

import (
	"reflect"
	"strings"
	"testing"
)

func TestWebChatCatalogContractFieldsStable(t *testing.T) {
	t.Parallel()

	got := gatewayJSONFieldNames(reflect.TypeOf(webChatCatalog{}))
	want := []string{"lang", "locale", "messages"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("webChatCatalog json fields = %#v, want %#v", got, want)
	}
}

func TestControlPlaneI18NSummaryFieldsStable(t *testing.T) {
	t.Parallel()

	got := gatewayJSONFieldNames(reflect.TypeOf(controlPlaneI18NSummary{}))
	want := []string{
		"configured_locale",
		"effective_locale",
		"fallback_locale",
		"supported_locales",
		"console_config_path",
		"console_catalog_path",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("controlPlaneI18NSummary json fields = %#v, want %#v", got, want)
	}
}

func gatewayJSONFieldNames(typ reflect.Type) []string {
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

package apiresponse

import (
	"reflect"
	"strings"
	"testing"
)

func TestErrorResponseContractFieldsStable(t *testing.T) {
	t.Parallel()

	got := jsonFieldNames(reflect.TypeOf(Error{}))
	want := []string{"code", "error"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Error json fields = %#v, want %#v", got, want)
	}
}

func TestAllErrorCodesStable(t *testing.T) {
	t.Parallel()

	want := []string{
		"request_failed",
		"invalid_argument",
		"invalid_json",
		"request_body_too_large",
		"unauthenticated",
		"authorization_denied",
		"not_found",
		"conflict",
		"rate_limited",
		"service_unavailable",
		"internal_error",
	}
	got := AllErrorCodes()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllErrorCodes() = %#v, want %#v", got, want)
	}

	seen := make(map[string]struct{}, len(got))
	for _, code := range got {
		if _, ok := seen[code]; ok {
			t.Fatalf("duplicate error code %q in %#v", code, got)
		}
		seen[code] = struct{}{}
		if code != strings.ToLower(code) || strings.Contains(code, "-") {
			t.Fatalf("error code %q is not canonical snake_case", code)
		}
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

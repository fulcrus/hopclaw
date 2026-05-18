package authz

import (
	"reflect"
	"strings"
	"testing"
)

func TestAuthorizationRequestFieldsStable(t *testing.T) {
	t.Parallel()

	got := jsonFieldNames(reflect.TypeOf(AuthorizationRequest{}))
	want := []string{"resource", "action", "method", "path", "principal", "metadata"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AuthorizationRequest json fields = %#v, want %#v", got, want)
	}
}

func TestAuthorizationDecisionFieldsStable(t *testing.T) {
	t.Parallel()

	got := jsonFieldNames(reflect.TypeOf(AuthorizationDecision{}))
	want := []string{"allowed", "reason", "source", "metadata"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AuthorizationDecision json fields = %#v, want %#v", got, want)
	}
}

func TestAuthorizationCatalogsStable(t *testing.T) {
	t.Parallel()

	if got, want := ResourceNames(AllResources()), []string{
		"*",
		"approvals",
		"audit",
		"channels",
		"config",
		"cron",
		"discovery",
		"governance",
		"hooks",
		"knowledge",
		"operator",
		"plugins",
		"runs",
		"sandbox",
		"sessions",
		"skills",
		"tools",
		"usage",
		"wakeup",
		"watch",
		"wire",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AllResources() = %#v, want %#v", got, want)
	}

	if got, want := ActionNames(AllActions()), []string{"admin", "approve", "execute", "read", "write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AllActions() = %#v, want %#v", got, want)
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

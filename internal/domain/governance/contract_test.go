package governance

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecisionFieldsStable(t *testing.T) {
	t.Parallel()

	got := jsonFieldNames(reflect.TypeOf(Decision{}))
	want := []string{
		"action",
		"reasons",
		"reason_codes",
		"policy_source",
		"summary",
		"policy_layers",
		"audit_labels",
		"approval_policy",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Decision json fields = %#v, want %#v", got, want)
	}
}

func TestApprovalPolicyFieldsStable(t *testing.T) {
	t.Parallel()

	got := jsonFieldNames(reflect.TypeOf(ApprovalPolicy{}))
	want := []string{"default_scope", "max_scope"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ApprovalPolicy json fields = %#v, want %#v", got, want)
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

package automationintent

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Plan.Normalized
// ---------------------------------------------------------------------------

func TestPlanNormalizedDefaultAction(t *testing.T) {
	t.Parallel()

	p := Plan{}.Normalized()
	if p.Action != ActionNone {
		t.Fatalf("Action = %q, want %q", p.Action, ActionNone)
	}
}

func TestPlanNormalizedTrimsFields(t *testing.T) {
	t.Parallel()

	p := Plan{
		Kind:   "  CRON  ",
		Reason: "  because  ",
	}.Normalized()

	if p.Kind != "cron" {
		t.Fatalf("Kind = %q, want %q", p.Kind, "cron")
	}
	if p.Reason != "because" {
		t.Fatalf("Reason = %q, want %q", p.Reason, "because")
	}
}

func TestPlanNormalizedQueryDefaults(t *testing.T) {
	t.Parallel()

	p := Plan{
		Action: ActionQuery,
		Query:  Query{Limit: 0, Metric: ""},
	}.Normalized()

	if p.Query.Limit != 10 {
		t.Fatalf("Query.Limit = %d, want 10", p.Query.Limit)
	}
	if p.Query.Metric != QueryMetricSummary {
		t.Fatalf("Query.Metric = %q, want %q", p.Query.Metric, QueryMetricSummary)
	}
}

func TestPlanNormalizedPreservesExplicitQueryMetric(t *testing.T) {
	t.Parallel()

	p := Plan{
		Action: ActionQuery,
		Query:  Query{Limit: 5, Metric: QueryMetricList},
	}.Normalized()

	if p.Query.Limit != 5 {
		t.Fatalf("Query.Limit = %d, want 5", p.Query.Limit)
	}
	if p.Query.Metric != QueryMetricList {
		t.Fatalf("Query.Metric = %q, want %q", p.Query.Metric, QueryMetricList)
	}
}

// ---------------------------------------------------------------------------
// Plan.Actionable
// ---------------------------------------------------------------------------

func TestPlanActionable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action Action
		want   bool
	}{
		{"none", ActionNone, false},
		{"empty", "", false},
		{"create", ActionCreate, true},
		{"update", ActionUpdate, true},
		{"disable", ActionDisable, true},
		{"delete", ActionDelete, true},
		{"query", ActionQuery, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := Plan{Action: tt.action}
			if got := p.Actionable(); got != tt.want {
				t.Fatalf("Actionable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Selector.Normalized
// ---------------------------------------------------------------------------

func TestSelectorNormalized(t *testing.T) {
	t.Parallel()

	s := Selector{
		Query:           "  some query  ",
		Kind:            "  CRON  ",
		IDs:             []string{"  a  ", "", "  b  "},
		Names:           []string{"  name1  "},
		Cities:          []string{""},
		DeliveryChannel: "  feishu  ",
		DeliveryTarget:  "  target  ",
	}.Normalized()

	if s.Query != "some query" {
		t.Fatalf("Query = %q", s.Query)
	}
	if s.Kind != "cron" {
		t.Fatalf("Kind = %q", s.Kind)
	}
	if len(s.IDs) != 2 || s.IDs[0] != "a" || s.IDs[1] != "b" {
		t.Fatalf("IDs = %v", s.IDs)
	}
	if len(s.Names) != 1 || s.Names[0] != "name1" {
		t.Fatalf("Names = %v", s.Names)
	}
	if s.Cities != nil {
		t.Fatalf("Cities = %v, want nil (all empty)", s.Cities)
	}
	if s.DeliveryChannel != "feishu" {
		t.Fatalf("DeliveryChannel = %q", s.DeliveryChannel)
	}
}

func TestSelectorNormalizedEmptySlices(t *testing.T) {
	t.Parallel()

	s := Selector{IDs: nil, Names: []string{}}.Normalized()
	if s.IDs != nil {
		t.Fatalf("IDs = %v, want nil", s.IDs)
	}
	if s.Names != nil {
		t.Fatalf("Names = %v, want nil", s.Names)
	}
}

// ---------------------------------------------------------------------------
// Spec.Normalized
// ---------------------------------------------------------------------------

func TestSpecNormalized(t *testing.T) {
	t.Parallel()

	s := Spec{
		Kind:       "  CRON  ",
		Name:       "  test  ",
		Schedule:   "  */5 * * * *  ",
		SourceKind: "  HTTP  ",
		Delivery: &DeliveryTarget{
			Channel: "  feishu  ",
			Target:  "  oc_123  ",
		},
	}.Normalized()

	if s.Kind != "cron" {
		t.Fatalf("Kind = %q", s.Kind)
	}
	if s.Name != "test" {
		t.Fatalf("Name = %q", s.Name)
	}
	if s.Schedule != "*/5 * * * *" {
		t.Fatalf("Schedule = %q", s.Schedule)
	}
	if s.SourceKind != "http" {
		t.Fatalf("SourceKind = %q", s.SourceKind)
	}
	if s.Delivery == nil {
		t.Fatal("Delivery is nil")
	}
	if s.Delivery.Channel != "feishu" {
		t.Fatalf("Delivery.Channel = %q", s.Delivery.Channel)
	}
	if s.Delivery.Kind != "channel" {
		t.Fatalf("Delivery.Kind = %q, want %q", s.Delivery.Kind, "channel")
	}
}

func TestSpecNormalizedNilsEmptyDelivery(t *testing.T) {
	t.Parallel()

	s := Spec{
		Delivery: &DeliveryTarget{},
	}.Normalized()

	if s.Delivery != nil {
		t.Fatal("expected Delivery to be nil when all fields empty")
	}
}

func TestSpecNormalizedDeliveryProviderInference(t *testing.T) {
	t.Parallel()

	s := Spec{
		Delivery: &DeliveryTarget{
			Channel: "telegram",
			Target:  "123456",
		},
	}.Normalized()

	if s.Delivery.Provider != "telegram" {
		t.Fatalf("Delivery.Provider = %q, want %q", s.Delivery.Provider, "telegram")
	}
	if s.Delivery.Kind != "channel" {
		t.Fatalf("Delivery.Kind = %q, want %q", s.Delivery.Kind, "channel")
	}
}

// ---------------------------------------------------------------------------
// trimStringSlice
// ---------------------------------------------------------------------------

func TestTrimStringSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"nil", nil, nil},
		{"empty", []string{}, nil},
		{"all whitespace", []string{"", "  ", "\t"}, nil},
		{"mixed", []string{"  a  ", "", "  b  "}, []string{"a", "b"}},
		{"clean", []string{"x", "y"}, []string{"x", "y"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := trimStringSlice(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

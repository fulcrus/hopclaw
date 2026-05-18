package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMetricsRegistered(t *testing.T) {
	HTTPRequestDuration.WithLabelValues("GET", "GET /operator/status", "200").Observe(0)
	RunsTotal.WithLabelValues("completed").Add(0)
	ModelCallDuration.WithLabelValues("openai", "gpt-4o").Observe(0)
	ModelCallErrors.WithLabelValues("openai", "gpt-4o", "other").Add(0)
	ToolCallDuration.WithLabelValues("fs.read").Observe(0)
	ToolCallErrors.WithLabelValues("fs.read").Add(0)
	EventBusPublished.WithLabelValues("run.started").Add(0)
	EventBusSinkErrors.WithLabelValues("*eventbus.testSink", "run.started").Add(0)
	EventBusSubscriberDropped.WithLabelValues("run.started").Add(0)
	AuditDeliveryQueuedTotal.WithLabelValues("siem").Add(0)
	AuditDeliveryAttemptsTotal.WithLabelValues("siem", "delivered").Add(0)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"hopclaw_http_request_duration_seconds":     false,
		"hopclaw_http_requests_in_flight":           false,
		"hopclaw_runtime_runs_total":                false,
		"hopclaw_runtime_runs_in_flight":            false,
		"hopclaw_runtime_sessions_active":           false,
		"hopclaw_model_call_duration_seconds":       false,
		"hopclaw_model_call_errors_total":           false,
		"hopclaw_tool_call_duration_seconds":        false,
		"hopclaw_tool_call_errors_total":            false,
		"hopclaw_approval_wait_duration_seconds":    false,
		"hopclaw_approval_pending":                  false,
		"hopclaw_eventbus_published_total":          false,
		"hopclaw_eventbus_queue_depth":              false,
		"hopclaw_eventbus_sink_errors_total":        false,
		"hopclaw_eventbus_subscriber_dropped_total": false,
		"hopclaw_audit_delivery_queued_total":       false,
		"hopclaw_audit_delivery_attempts_total":     false,
	}

	for _, family := range families {
		if _, ok := want[family.GetName()]; ok {
			want[family.GetName()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("metric %q not registered", name)
		}
	}
}

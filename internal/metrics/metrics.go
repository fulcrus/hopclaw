package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "hopclaw"

// ---------------------------------------------------------------------------
// HTTP surface
// ---------------------------------------------------------------------------

var HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: namespace,
	Subsystem: "http",
	Name:      "request_duration_seconds",
	Help:      "HTTP request latency by method, path pattern, and status code.",
	Buckets:   prometheus.DefBuckets,
}, []string{"method", "route", "status"})

var HTTPRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: namespace,
	Subsystem: "http",
	Name:      "requests_in_flight",
	Help:      "Number of HTTP requests currently being served.",
})

// ---------------------------------------------------------------------------
// Run / Session lifecycle
// ---------------------------------------------------------------------------

var RunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: namespace,
	Subsystem: "runtime",
	Name:      "runs_total",
	Help:      "Total runs by final status.",
}, []string{"status"})

var RunsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: namespace,
	Subsystem: "runtime",
	Name:      "runs_in_flight",
	Help:      "Number of runs currently executing.",
})

var SessionsActive = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: namespace,
	Subsystem: "runtime",
	Name:      "sessions_active",
	Help:      "Number of sessions with at least one active run.",
})

// ---------------------------------------------------------------------------
// Model calls
// ---------------------------------------------------------------------------

var ModelCallDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: namespace,
	Subsystem: "model",
	Name:      "call_duration_seconds",
	Help:      "Model API call latency by provider and model.",
	Buckets:   []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
}, []string{"provider", "model"})

var ModelCallErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: namespace,
	Subsystem: "model",
	Name:      "call_errors_total",
	Help:      "Model API call failures by provider, model, and error class.",
}, []string{"provider", "model", "error_class"})

// ---------------------------------------------------------------------------
// Tool execution
// ---------------------------------------------------------------------------

var ToolCallDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: namespace,
	Subsystem: "tool",
	Name:      "call_duration_seconds",
	Help:      "Tool execution latency by tool name.",
	Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
}, []string{"tool"})

var ToolCallErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: namespace,
	Subsystem: "tool",
	Name:      "call_errors_total",
	Help:      "Tool execution failures by tool name.",
}, []string{"tool"})

// ---------------------------------------------------------------------------
// Approval flow
// ---------------------------------------------------------------------------

var ApprovalWaitDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Namespace: namespace,
	Subsystem: "approval",
	Name:      "wait_duration_seconds",
	Help:      "Time between approval request and resolution.",
	Buckets:   []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800},
})

var ApprovalsPending = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: namespace,
	Subsystem: "approval",
	Name:      "pending",
	Help:      "Number of approval tickets currently pending.",
})

// ---------------------------------------------------------------------------
// Event bus
// ---------------------------------------------------------------------------

var EventBusPublished = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: namespace,
	Subsystem: "eventbus",
	Name:      "published_total",
	Help:      "Events published by type.",
}, []string{"type"})

var EventBusQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: namespace,
	Subsystem: "eventbus",
	Name:      "queue_depth",
	Help:      "Current number of buffered events in the in-memory bus.",
})

var EventBusSinkErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: namespace,
	Subsystem: "eventbus",
	Name:      "sink_errors_total",
	Help:      "Synchronous sink failures by sink type and event type.",
}, []string{"sink", "type"})

var EventBusSubscriberDropped = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: namespace,
	Subsystem: "eventbus",
	Name:      "subscriber_dropped_total",
	Help:      "Live subscription events dropped because a subscriber buffer was full.",
}, []string{"type"})

// ---------------------------------------------------------------------------
// Audit delivery
// ---------------------------------------------------------------------------

var AuditDeliveryQueuedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: namespace,
	Subsystem: "audit_delivery",
	Name:      "queued_total",
	Help:      "Audit delivery records queued by sink.",
}, []string{"sink"})

var AuditDeliveryAttemptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: namespace,
	Subsystem: "audit_delivery",
	Name:      "attempts_total",
	Help:      "Audit delivery attempts by sink and result.",
}, []string{"sink", "result"})

package observe

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Flint application metrics. Initialized once, used throughout the codebase.
var (
	meter = otel.Meter("flint")

	// Pipeline runs.
	PipelineRunsTotal   metric.Int64Counter
	PipelineRunDuration metric.Float64Histogram

	// Outbox.
	OutboxEventsProcessed metric.Int64Counter
	OutboxEventsFailed    metric.Int64Counter
	OutboxProcessingTime  metric.Float64Histogram

	// Webhook.
	WebhooksReceived metric.Int64Counter
	WebhooksInvalid  metric.Int64Counter

	// Secrets.
	SecretOperations metric.Int64Counter

	// HTTP.
	HTTPRequestsTotal   metric.Int64Counter
	HTTPRequestDuration metric.Float64Histogram
)

func init() {
	var err error

	PipelineRunsTotal, err = meter.Int64Counter("flint.pipeline.runs.total",
		metric.WithDescription("Total pipeline runs by status"))
	must(err)

	PipelineRunDuration, err = meter.Float64Histogram("flint.pipeline.runs.duration_seconds",
		metric.WithDescription("Pipeline run duration in seconds"))
	must(err)

	OutboxEventsProcessed, err = meter.Int64Counter("flint.outbox.events.processed",
		metric.WithDescription("Total outbox events processed"))
	must(err)

	OutboxEventsFailed, err = meter.Int64Counter("flint.outbox.events.failed",
		metric.WithDescription("Total outbox events that exhausted retries"))
	must(err)

	OutboxProcessingTime, err = meter.Float64Histogram("flint.outbox.processing_seconds",
		metric.WithDescription("Outbox event processing time in seconds"))
	must(err)

	WebhooksReceived, err = meter.Int64Counter("flint.webhooks.received",
		metric.WithDescription("Total webhooks received by forge type"))
	must(err)

	WebhooksInvalid, err = meter.Int64Counter("flint.webhooks.invalid",
		metric.WithDescription("Total invalid/rejected webhooks"))
	must(err)

	SecretOperations, err = meter.Int64Counter("flint.secrets.operations",
		metric.WithDescription("Total secret operations by type (get, set, delete)"))
	must(err)

	HTTPRequestsTotal, err = meter.Int64Counter("flint.http.requests.total",
		metric.WithDescription("Total HTTP requests by method and status"))
	must(err)

	HTTPRequestDuration, err = meter.Float64Histogram("flint.http.requests.duration_seconds",
		metric.WithDescription("HTTP request duration in seconds"))
	must(err)
}

func must(err error) {
	if err != nil {
		panic("observe: failed to create metric: " + err.Error())
	}
}

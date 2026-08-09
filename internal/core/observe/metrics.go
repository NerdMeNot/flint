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

	// Engine — step lifecycle.
	StepsDispatched metric.Int64Counter
	StepsCompleted  metric.Int64Counter
	DispatchErrors  metric.Int64Counter
	StepsThrottled  metric.Int64Counter

	// Secrets.
	SecretOperations metric.Int64Counter

	// HTTP.
	HTTPRequestsTotal   metric.Int64Counter
	HTTPRequestDuration metric.Float64Histogram

	// Auth — sign-in outcomes labelled by method (local/oidc/saml) and result
	// (success/failure). Powers SSO dashboards + failure-spike alerting.
	AuthLoginsTotal metric.Int64Counter

	// Fleet — machine lifecycle and the economics decisions behind it. The
	// decision ledger explains any ONE provision after the fact; these are what
	// tell you the fleet is 40 machines deep and climbing while it happens.
	FleetProvisionAttempts   metric.Int64Counter
	FleetMachinesTerminated  metric.Int64Counter
	FleetMachinesLost        metric.Int64Counter
	FleetBootDuration        metric.Float64Histogram
	FleetAssignmentsBound    metric.Int64Counter
	FleetQueueWaitSeconds    metric.Float64Histogram
	FleetProviderCalls       metric.Int64Counter
	FleetProviderCallSeconds metric.Float64Histogram
)

// Meter exposes the shared "flint" meter so packages that own their own state
// (the fleet's DB-backed inventory gauges) can register observable instruments
// against it, instead of this file reaching into their dependencies.
func Meter() metric.Meter { return meter }

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

	StepsDispatched, err = meter.Int64Counter("flint.engine.steps.dispatched",
		metric.WithDescription("Total steps dispatched to machines"))
	must(err)

	StepsCompleted, err = meter.Int64Counter("flint.engine.steps.completed",
		metric.WithDescription("Total steps completed by status (succeeded/failed)"))
	must(err)

	DispatchErrors, err = meter.Int64Counter("flint.engine.dispatch.errors",
		metric.WithDescription("Total step dispatch failures"))
	must(err)

	StepsThrottled, err = meter.Int64Counter("flint.engine.steps.throttled",
		metric.WithDescription("Total steps re-queued due to org concurrency limit"))
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

	AuthLoginsTotal, err = meter.Int64Counter("flint.auth.logins.total",
		metric.WithDescription("Total sign-in attempts by method (local/oidc/saml) and result (success/failure)"))
	must(err)

	FleetProvisionAttempts, err = meter.Int64Counter("flint.fleet.provision.attempts",
		metric.WithDescription("Machine provision attempts by pool, capacity type, objective, and result (booted/no_capacity/create_failed)"))
	must(err)

	FleetMachinesTerminated, err = meter.Int64Counter("flint.fleet.machines.terminated",
		metric.WithDescription("Machines terminated by pool and reason (idle_ttl, drain, ...)"))
	must(err)

	FleetMachinesLost, err = meter.Int64Counter("flint.fleet.machines.lost",
		metric.WithDescription("Machines lost by pool and cause (heartbeat_expired, instance_gone, boot_timeout)"))
	must(err)

	FleetBootDuration, err = meter.Float64Histogram("flint.fleet.boot.duration_seconds",
		metric.WithDescription("Time from provision request to agent registration, by pool and capacity type"))
	must(err)

	FleetAssignmentsBound, err = meter.Int64Counter("flint.fleet.assignments.bound",
		metric.WithDescription("Step assignments bound to a machine by pool"))
	must(err)

	FleetQueueWaitSeconds, err = meter.Float64Histogram("flint.fleet.assignments.queue_wait_seconds",
		metric.WithDescription("Time a step assignment waited pending before being bound to a machine, by pool — the user-visible 'why is my build queued'"))
	must(err)

	FleetProviderCalls, err = meter.Int64Counter("flint.fleet.provider.calls",
		metric.WithDescription("Compute-provider API calls by provider, operation, and result (ok/error/timeout)"))
	must(err)

	FleetProviderCallSeconds, err = meter.Float64Histogram("flint.fleet.provider.call_duration_seconds",
		metric.WithDescription("Compute-provider API call latency by provider and operation — the signal ProviderTimeouts is tuned against"))
	must(err)
}

func must(err error) {
	if err != nil {
		panic("observe: failed to create metric: " + err.Error())
	}
}

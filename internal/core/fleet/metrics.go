package fleet

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
)

// inventoryScrapeTimeout bounds the gauge callback's query. A metrics scrape
// must never be able to hold a connection open indefinitely — the fleet has
// exactly one Postgres pool, and telemetry is the last thing that should be
// allowed to starve scheduling of it.
const inventoryScrapeTimeout = 5 * time.Second

// RegisterInventoryMetrics publishes the fleet's live shape as observable
// gauges: machine count and committed $/hour, by pool, state, and reliability
// class. Returns an unregister function.
//
// These are OBSERVED from Postgres on each collection rather than accumulated
// in-process, because in-process counters are wrong in exactly the situations
// that matter: they reset on restart, and every dispatch replica would hold its
// own partial view of a fleet that is global by definition. The DB is the only
// place the true number lives.
func (f *Fleet) RegisterInventoryMetrics() (func() error, error) {
	meter := observe.Meter()

	machines, err := meter.Int64ObservableGauge("flint.fleet.machines",
		metric.WithDescription("Live machines by pool, status, and capacity type"))
	if err != nil {
		return nil, err
	}
	cost, err := meter.Float64ObservableGauge("flint.fleet.hourly_cost_usd",
		metric.WithDescription("Committed machine spend in USD per hour, by pool, status, and capacity type"))
	if err != nil {
		return nil, err
	}

	reg, err := meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		ctx, cancel := context.WithTimeout(ctx, inventoryScrapeTimeout)
		defer cancel()

		rows, err := db.New(f.pool).FleetInventory(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			attrs := metric.WithAttributes(
				attribute.String("pool", r.PoolName),
				attribute.String("status", r.Status),
				attribute.String("capacity_type", r.CapacityType),
			)
			o.ObserveInt64(machines, r.N, attrs)
			o.ObserveFloat64(cost, r.HourlyCostUsd, attrs)
		}
		return nil
	}, machines, cost)
	if err != nil {
		return nil, err
	}
	return reg.Unregister, nil
}

// recordProvisionAttempt ledgers-in-metrics one provisioning outcome. The
// decision ledger already explains any single boot; this is what shows a pool
// failing to find capacity repeatedly, which no single ledger row reveals.
func recordProvisionAttempt(ctx context.Context, pool, capacityType, objective, result string) {
	observe.FleetProvisionAttempts.Add(ctx, 1, metric.WithAttributes(
		attribute.String("pool", pool),
		attribute.String("capacity_type", capacityType),
		attribute.String("objective", objective),
		attribute.String("result", result),
	))
}

func recordMachineTerminated(ctx context.Context, pool, reason string) {
	observe.FleetMachinesTerminated.Add(ctx, 1, metric.WithAttributes(
		attribute.String("pool", pool),
		attribute.String("reason", reason),
	))
}

func recordMachineLost(ctx context.Context, cause string, n int) {
	if n <= 0 {
		return
	}
	observe.FleetMachinesLost.Add(ctx, int64(n), metric.WithAttributes(
		attribute.String("cause", cause),
	))
}

// recordBootDuration times provision-request → agent-registration: the latency
// the fleet's warm-vs-boot economics is a trade against, so it has to be
// measured rather than assumed from Offer.ExpectedBootSeconds.
func recordBootDuration(ctx context.Context, pool, capacityType string, d time.Duration) {
	observe.FleetBootDuration.Record(ctx, d.Seconds(), metric.WithAttributes(
		attribute.String("pool", pool),
		attribute.String("capacity_type", capacityType),
	))
}

// recordAssignmentBound reports one binding and how long it waited. queuedFor is
// measured from the assignment row's creation, so it captures the whole wait —
// including time spent waiting on capacity that had to be booted first.
func recordAssignmentBound(ctx context.Context, pool string, queuedFor time.Duration) {
	attrs := metric.WithAttributes(attribute.String("pool", pool))
	observe.FleetAssignmentsBound.Add(ctx, 1, attrs)
	observe.FleetQueueWaitSeconds.Record(ctx, queuedFor.Seconds(), attrs)
}

// recordProviderCall reports one provider API call's latency and outcome. This
// is the feedback loop for ProviderTimeouts: a rising p99 on an operation is
// the warning that its ceiling is about to start firing.
func recordProviderCall(ctx context.Context, provider, op string, d time.Duration, err error) {
	attrs := metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("operation", op),
		attribute.String("result", callResult(ctx, err)),
	)
	observe.FleetProviderCalls.Add(ctx, 1, attrs)
	observe.FleetProviderCallSeconds.Record(ctx, d.Seconds(), attrs)
}

// callResult distinguishes a timeout from an ordinary provider error, because
// they call for opposite responses: a timeout means Flint gave up (and may have
// leaked an instance), an error means the provider refused.
func callResult(ctx context.Context, err error) string {
	switch {
	case err == nil:
		return "ok"
	case ctx.Err() != nil:
		return "timeout"
	default:
		return "error"
	}
}

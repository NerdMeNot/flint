// Package observe provides unified observability for Flint: OpenTelemetry tracing,
// Prometheus metrics, and zerolog structured logging with correlation IDs.
//
// Every request/workflow/event gets a correlation context (request_id, org_id, run_id)
// that flows through logs, traces, and metrics for end-to-end debugging.
package observe

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config configures the observability stack.
type Config struct {
	// ServiceName identifies this service in traces and metrics.
	ServiceName string

	// ServiceVersion is the build version.
	ServiceVersion string

	// OTLPEndpoint is the OTLP gRPC collector endpoint (e.g., "otel-collector:4317").
	// If empty, tracing is disabled (noop exporter).
	OTLPEndpoint string

	// LogLevel sets the minimum log level. Default: "info".
	LogLevel string

	// PrettyLogs enables human-readable console output (dev mode).
	// Default: false (JSON output for production).
	PrettyLogs bool
}

// Shutdown is returned by Init and must be called on process exit.
type Shutdown func(ctx context.Context) error

// Init initializes the full observability stack: tracing, metrics, and logging.
// Returns a Shutdown function that must be called on process exit to flush.
func Init(ctx context.Context, cfg Config) (Shutdown, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("observe: failed to create resource: %w", err)
	}

	// Logging (first — so tracing/metrics init can log).
	initLogging(cfg)

	// Tracing.
	tracerShutdown, err := initTracing(ctx, res, cfg)
	if err != nil {
		return nil, err
	}

	// Metrics.
	meterShutdown, err := initMetrics(res)
	if err != nil {
		return nil, err
	}

	shutdown := func(ctx context.Context) error {
		var firstErr error
		if err := tracerShutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := meterShutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	}

	return shutdown, nil
}

func initTracing(ctx context.Context, res *resource.Resource, cfg Config) (Shutdown, error) {
	if cfg.OTLPEndpoint == "" {
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
		return func(ctx context.Context) error { return nil }, nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("observe: failed to create trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(ctx context.Context) error { return tp.Shutdown(ctx) }, nil
}

func initMetrics(res *resource.Resource) (Shutdown, error) {
	promExporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("observe: failed to create prometheus exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(promExporter),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error { return mp.Shutdown(ctx) }, nil
}

func initLogging(cfg Config) {
	// zerolog uses Unix timestamps by default — switch to ISO 8601.
	zerolog.TimeFieldFormat = time.RFC3339Nano

	level := zerolog.InfoLevel
	switch cfg.LogLevel {
	case "debug":
		level = zerolog.DebugLevel
	case "warn":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	case "trace":
		level = zerolog.TraceLevel
	}

	zerolog.SetGlobalLevel(level)

	if cfg.PrettyLogs {
		log.Logger = zerolog.New(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "15:04:05",
		}).With().Timestamp().Caller().Logger()
	} else {
		log.Logger = zerolog.New(os.Stdout).With().
			Timestamp().
			Str("service", cfg.ServiceName).
			Str("version", cfg.ServiceVersion).
			Logger()
	}
}

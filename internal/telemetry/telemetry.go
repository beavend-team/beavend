package telemetry

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// Telemetry manages OpenTelemetry tracing and metrics.
type Telemetry struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	PrometheusExp  *prometheus.Exporter
	Tracer         trace.Tracer
}

// Init initializes OpenTelemetry and Prometheus exporter.
func Init(ctx context.Context, serviceName, version string) (*Telemetry, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create otel resource: %w", err)
	}

	// 1. Prometheus Metrics Exporter & MeterProvider
	promExporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(promExporter),
	)
	otel.SetMeterProvider(meterProvider)

	// 2. TracerProvider
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer := tracerProvider.Tracer(serviceName)

	return &Telemetry{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
		PrometheusExp:  promExporter,
		Tracer:         tracer,
	}, nil
}

// MetricsHandler returns an HTTP handler serving Prometheus metrics.
func (t *Telemetry) MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// Shutdown flushes and shuts down tracing and metrics providers.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var tracerErr, meterErr error
	if t.TracerProvider != nil {
		tracerErr = t.TracerProvider.Shutdown(ctx)
	}
	if t.MeterProvider != nil {
		meterErr = t.MeterProvider.Shutdown(ctx)
	}
	if tracerErr != nil {
		return fmt.Errorf("failed to shut down tracer provider: %w", tracerErr)
	}
	if meterErr != nil {
		return fmt.Errorf("failed to shut down meter provider: %w", meterErr)
	}
	return nil
}

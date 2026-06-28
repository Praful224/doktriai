package core

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// InitTracer sets up a global OpenTelemetry trace provider exporting to an OTLP HTTP receiver.
// Returns a shutdown function that flushes remaining spans on exit.
func InitTracer(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:4318" // default OTLP HTTP receiver port
	}

	// Allow disabling tracing in tests or isolated local environments
	if os.Getenv("DOKTRIAI_OTEL_DISABLED") == "true" {
		tp := sdktrace.NewTracerProvider()
		otel.SetTracerProvider(tp)
		return func(ctx context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		// Fallback to in-memory tracer to avoid runtime crash
		tp := sdktrace.NewTracerProvider()
		otel.SetTracerProvider(tp)
		return func(ctx context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		res = resource.Default()
	}

	bsp := sdktrace.NewBatchSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)

	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

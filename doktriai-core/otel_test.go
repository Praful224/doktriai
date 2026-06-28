package core

import (
	"context"
	"os"
	"testing"
)

func TestInitTracer(t *testing.T) {
	// Disable live OTLP exporter to run isolated unit tests
	os.Setenv("DOKTRIAI_OTEL_DISABLED", "true")
	defer os.Unsetenv("DOKTRIAI_OTEL_DISABLED")

	ctx := context.Background()
	shutdown, err := InitTracer(ctx, "doktriai-test-service")
	if err != nil {
		t.Fatalf("expected no error during tracer initialization, got %v", err)
	}

	if shutdown == nil {
		t.Fatal("expected non-nil shutdown cleanup function")
	}

	if err := shutdown(ctx); err != nil {
		t.Errorf("expected no error during tracer shutdown, got %v", err)
	}
}

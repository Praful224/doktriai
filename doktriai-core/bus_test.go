package core

import (
	"context"
	"testing"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

// ─── EventBus Subscribe/Publish ───────────────────────────────────────────────

func TestEventBus_SubscribeAndPublish(t *testing.T) {
	bus := NewEventBus(20)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	event := packages.Event{Level: "ok", Source: "test", Message: "hello"}
	bus.Publish(event)

	select {
	case received := <-ch:
		if received.Message != "hello" {
			t.Errorf("expected message 'hello', got %q", received.Message)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for event")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewEventBus(20)
	ch1 := bus.Subscribe()
	ch2 := bus.Subscribe()
	defer bus.Unsubscribe(ch1)
	defer bus.Unsubscribe(ch2)

	bus.Publish(packages.Event{Level: "ok", Source: "test", Message: "broadcast"})

	for _, ch := range []chan packages.Event{ch1, ch2} {
		select {
		case received := <-ch:
			if received.Message != "broadcast" {
				t.Errorf("expected 'broadcast', got %q", received.Message)
			}
		case <-time.After(time.Second):
			t.Error("subscriber did not receive event")
		}
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus(20)
	ch := bus.Subscribe()
	bus.Unsubscribe(ch)

	// Channel should be closed after unsubscribe
	_, open := <-ch
	if open {
		t.Error("expected channel to be closed after unsubscribe")
	}
}

func TestEventBus_NonBlockingPublish(t *testing.T) {
	bus := NewEventBus(20)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// Fill the channel buffer (capacity 20)
	for i := 0; i < 25; i++ {
		bus.Publish(packages.Event{Level: "ok", Source: "test", Message: "fill"})
	}

	// Should not block or panic — excess events are dropped
	bus.Publish(packages.Event{Level: "ok", Source: "test", Message: "overflow"})
	// If we reach here, the non-blocking publish works
}

func TestEventBus_PublishWithNoSubscribers(t *testing.T) {
	bus := NewEventBus(20)
	// Should not panic
	bus.Publish(packages.Event{Level: "ok", Source: "test", Message: "nobody listening"})
}

// ─── Engine Circuit Breaker ───────────────────────────────────────────────────

func TestEngine_CircuitBreaker_Closed(t *testing.T) {
	store, _ := tempStore(t)
	bus := NewEventBus(20)
	driver := &mockDriver{}
	engine := NewEngine(store, driver, bus, 30*time.Second)

	if engine.circuitOpen("test-workload") {
		t.Error("expected circuit closed initially")
	}
}

func TestEngine_CircuitBreaker_OpensAfterFailures(t *testing.T) {
	store, _ := tempStore(t)
	bus := NewEventBus(20)
	driver := &mockDriver{}
	engine := NewEngine(store, driver, bus, 30*time.Second)

	for i := 0; i < 3; i++ {
		engine.circuitRecordFailure("fail-workload")
	}

	if !engine.circuitOpen("fail-workload") {
		t.Error("expected circuit open after 3 failures")
	}
}

func TestEngine_CircuitBreaker_Reset(t *testing.T) {
	store, _ := tempStore(t)
	bus := NewEventBus(20)
	driver := &mockDriver{}
	engine := NewEngine(store, driver, bus, 30*time.Second)

	for i := 0; i < 3; i++ {
		engine.circuitRecordFailure("reset-workload")
	}
	engine.circuitReset("reset-workload")

	if engine.circuitOpen("reset-workload") {
		t.Error("expected circuit closed after reset")
	}
}

func TestEngine_CircuitBreaker_BelowThreshold(t *testing.T) {
	store, _ := tempStore(t)
	bus := NewEventBus(20)
	driver := &mockDriver{}
	engine := NewEngine(store, driver, bus, 30*time.Second)

	engine.circuitRecordFailure("partial-fail")
	engine.circuitRecordFailure("partial-fail")
	// Only 2 failures — threshold is 3

	if engine.circuitOpen("partial-fail") {
		t.Error("expected circuit closed with only 2 failures")
	}
}

func TestEngine_ListCircuitBreakers(t *testing.T) {
	store, _ := tempStore(t)
	bus := NewEventBus(20)
	driver := &mockDriver{}
	engine := NewEngine(store, driver, bus, 30*time.Second)

	for i := 0; i < 3; i++ {
		engine.circuitRecordFailure("open-workload")
	}

	breakers := engine.ListCircuitBreakers()
	if len(breakers) == 0 {
		t.Error("expected at least 1 circuit breaker entry")
	}

	entry, ok := breakers["open-workload"]
	if !ok {
		t.Fatal("expected 'open-workload' in circuit breakers")
	}

	m := entry.(map[string]any)
	if !m["open"].(bool) {
		t.Error("expected circuit breaker to be open")
	}
}

// ─── Mock Driver for Engine Tests ─────────────────────────────────────────────

type mockDriver struct {
	applied []packages.WorkloadSpec
}

func (m *mockDriver) Name() string { return "mock" }
func (m *mockDriver) List(_ context.Context) ([]packages.ActualWorkload, error) {
	return nil, nil
}
func (m *mockDriver) Apply(_ context.Context, spec packages.WorkloadSpec, replica int) error {
	m.applied = append(m.applied, spec)
	return nil
}
func (m *mockDriver) Delete(_ context.Context, workload string, replica int) error {
	return nil
}
func (m *mockDriver) DeleteWorkload(_ context.Context, workload string) error {
	return nil
}
func (m *mockDriver) Logs(_ context.Context, workload string, tail int) ([]string, error) {
	return []string{"mock log"}, nil
}

package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

const (
	// circuitOpenThreshold: how many consecutive reconcile failures on the same
	// workload before the circuit breaker opens and pauses that workload (ASI08).
	circuitOpenThreshold = 3
	// circuitResetAfter: how long the circuit stays open before auto-resetting.
	circuitResetAfter = 2 * time.Minute
)

// circuitState tracks failure state per workload.
type circuitState struct {
	failures  int
	openSince time.Time
}

// Engine drives the reconciliation loop and state application.
type Engine struct {
	store    *Store
	runtime  packages.RuntimeDriver
	bus      *EventBus
	interval time.Duration
	mu       sync.Mutex

	// circuit: per-workload failure tracking (Layer 3 — ASI08 cascading failure prevention)
	circuitMu sync.Mutex
	circuit   map[string]*circuitState

	// Reconcile timing for Prometheus observability
	reconcileMu      sync.RWMutex
	lastReconcileAt  time.Time
	lastReconcileDur time.Duration
}

func NewEngine(store *Store, runtime packages.RuntimeDriver, bus *EventBus, interval time.Duration) *Engine {
	return &Engine{
		store:    store,
		runtime:  runtime,
		bus:      bus,
		interval: interval,
		circuit:  make(map[string]*circuitState),
	}
}

func (e *Engine) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(e.interval)
		defer ticker.Stop()
		_ = e.Reconcile(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = e.Reconcile(ctx)
			}
		}
	}()
}

// Apply stores the desired workload state and triggers reconciliation.
// Records StateHashBefore and StateHashAfter in the audit trail (Layer 3).
func (e *Engine) Apply(ctx context.Context, actor string, spec packages.WorkloadSpec) error {
	spec = NormalizeSpec(spec)
	if err := ValidateSpec(spec); err != nil {
		_, _ = e.store.AddAudit(packages.AuditRecord{
			Actor: actor, Action: "apply", Workload: spec.Name, Allowed: false, Reason: err.Error(),
		})
		return err
	}

	hashBefore := e.store.SnapshotHash()
	if err := e.store.PutWorkload(spec, actor); err != nil {
		return err
	}
	hashAfter := e.store.SnapshotHash()

	_, _ = e.store.AddAudit(packages.AuditRecord{
		Actor:           actor,
		Action:          "apply",
		Workload:        spec.Name,
		Allowed:         true,
		StateHashBefore: hashBefore,
		StateHashAfter:  hashAfter,
	})
	e.emit(packages.Event{Level: "ok", Source: "api", Workload: spec.Name, Message: "desired workload accepted"})
	// Reset circuit for this workload on a fresh apply
	e.circuitReset(spec.Name)
	return e.Reconcile(ctx)
}

// Rollback restores a workload to a previous specification version in history.
func (e *Engine) Rollback(ctx context.Context, actor, name string, version int64) error {
	spec, err := e.store.RollbackWorkload(name, version)
	if err != nil {
		return fmt.Errorf("rollback: failed to find version %d: %w", version, err)
	}
	e.emit(packages.Event{
		Level:    "warn",
		Source:   "api",
		Workload: name,
		Message:  fmt.Sprintf("rolling back workload state to version %d (initiated by %s)", version, actor),
	})
	return e.Apply(ctx, actor, spec)
}

// Delete removes the desired workload state and stops running containers.
// Records StateHashBefore and StateHashAfter in the audit trail (Layer 3).
func (e *Engine) Delete(ctx context.Context, actor, name string) error {
	hashBefore := e.store.SnapshotHash()
	if err := e.store.DeleteWorkload(name); err != nil {
		return err
	}
	if err := e.runtime.DeleteWorkload(ctx, name); err != nil {
		e.emit(packages.Event{Level: "error", Source: "runtime", Workload: name, Message: err.Error()})
	}
	hashAfter := e.store.SnapshotHash()

	_, _ = e.store.AddAudit(packages.AuditRecord{
		Actor:           actor,
		Action:          "delete",
		Workload:        name,
		Allowed:         true,
		StateHashBefore: hashBefore,
		StateHashAfter:  hashAfter,
	})
	e.emit(packages.Event{Level: "warn", Source: "api", Workload: name, Message: "desired workload deleted"})
	return nil
}

// Reconcile converges the actual workload state toward the desired state.
// Per-workload circuit breakers prevent cascading failure storms (Layer 3 — ASI08).
func (e *Engine) Reconcile(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := time.Now()
	defer func() {
		e.reconcileMu.Lock()
		e.lastReconcileAt = time.Now()
		e.lastReconcileDur = time.Since(start)
		e.reconcileMu.Unlock()
	}()
	desired := e.store.ListWorkloads()
	actual, err := e.runtime.List(ctx)
	if err != nil {
		e.emit(packages.Event{Level: "error", Source: "runtime", Message: err.Error()})
		return err
	}

	actualByName := map[string]map[int]packages.ActualWorkload{}
	for _, item := range actual {
		if actualByName[item.Name] == nil {
			actualByName[item.Name] = map[int]packages.ActualWorkload{}
		}
		actualByName[item.Name][item.Replica] = item
	}

	for _, spec := range desired {
		// --- Circuit breaker check (ASI08) ---
		if e.circuitOpen(spec.Name) {
			e.emit(packages.Event{
				Level: "warn", Source: "core", Workload: spec.Name,
				Message: fmt.Sprintf("circuit breaker OPEN for workload %q — skipping reconcile until reset", spec.Name),
			})
			continue
		}

		replicas := actualByName[spec.Name]
		if len(replicas) != spec.Replicas {
			NotifyDrift(spec.Name, spec.Replicas, len(replicas))
		}
		
		hadError := false
		if spec.DeployStrategy == "rolling" {
			hadError = e.applyRolling(ctx, spec, replicas)
		} else {
			hadError = e.applyRecreate(ctx, spec, replicas)
		}

		// Cleanup extra replicas
		for replica := range replicas {
			if replica >= spec.Replicas {
				if err := e.runtime.Delete(ctx, spec.Name, replica); err != nil {
					e.emit(packages.Event{Level: "error", Source: "runtime", Workload: spec.Name, Message: err.Error()})
					hadError = true
					continue
				}
				e.emit(packages.Event{Level: "warn", Source: "core", Workload: spec.Name, Message: fmt.Sprintf("removed extra replica %d", replica)})
			}
		}

		if hadError {
			e.circuitRecordFailure(spec.Name)
		} else {
			e.circuitReset(spec.Name)
		}
	}

	desiredNames := map[string]struct{}{}
	for _, spec := range desired {
		desiredNames[spec.Name] = struct{}{}
	}
	for name := range actualByName {
		if _, ok := desiredNames[name]; !ok {
			_ = e.runtime.DeleteWorkload(ctx, name)
			e.emit(packages.Event{Level: "warn", Source: "core", Workload: name, Message: "removed unmanaged Doktriai replica set"})
		}
	}

	e.emit(packages.Event{Level: "ok", Source: "core", Message: "reconcile tick complete"})
	return nil
}

func (e *Engine) Status(ctx context.Context) ([]packages.WorkloadStatus, error) {
	specs := e.store.ListWorkloads()
	actual, err := e.runtime.List(ctx)
	if err != nil {
		return nil, err
	}
	actualByName := map[string][]packages.ActualWorkload{}
	for _, item := range actual {
		actualByName[item.Name] = append(actualByName[item.Name], item)
	}
	out := make([]packages.WorkloadStatus, 0, len(specs))
	for _, spec := range specs {
		items := actualByName[spec.Name]
		status := packages.WorkloadStatus{Spec: spec, Actual: items, Healthy: len(items) == spec.Replicas}
		if !status.Healthy {
			status.Drift = "desired=" + strconv.Itoa(spec.Replicas) + " actual=" + strconv.Itoa(len(items))
		}
		out = append(out, status)
	}
	return out, nil
}

func (e *Engine) Logs(ctx context.Context, workload string, tail int) ([]string, error) {
	if tail <= 0 || tail > 500 {
		tail = 80
	}
	return e.runtime.Logs(ctx, strings.TrimSpace(workload), tail)
}

func (e *Engine) emit(event packages.Event) {
	saved, err := e.store.AddEvent(event)
	if err == nil {
		event.ID = saved.ID
		event.Time = saved.Time
	}
	e.bus.Publish(event)
}

// --- Circuit breaker helpers (ASI08 — Cascading Failure Prevention) ---

func (e *Engine) circuitOpen(workload string) bool {
	e.circuitMu.Lock()
	defer e.circuitMu.Unlock()
	cs, ok := e.circuit[workload]
	if !ok {
		return false
	}
	if cs.failures < circuitOpenThreshold {
		return false
	}
	// Auto-reset after the cooldown window
	if time.Since(cs.openSince) > circuitResetAfter {
		delete(e.circuit, workload)
		return false
	}
	return true
}

func (e *Engine) circuitRecordFailure(workload string) {
	e.circuitMu.Lock()
	defer e.circuitMu.Unlock()
	cs, ok := e.circuit[workload]
	if !ok {
		cs = &circuitState{}
		e.circuit[workload] = cs
	}
	cs.failures++
	if cs.failures == circuitOpenThreshold {
		cs.openSince = time.Now()
		// Publish a circuit_open event so the dashboard can surface it
		e.bus.Publish(packages.Event{
			Level: "error", Source: "circuit-breaker", Workload: workload,
			Message: fmt.Sprintf("circuit OPENED for %q after %d consecutive failures — paused for %s",
				workload, circuitOpenThreshold, circuitResetAfter),
		})
	}
}

func (e *Engine) circuitReset(workload string) {
	e.circuitMu.Lock()
	defer e.circuitMu.Unlock()
	delete(e.circuit, workload)
}

func (e *Engine) Runtime() packages.RuntimeDriver {
	return e.runtime
}

// ListCircuitBreakers returns all active circuit breaker records.
func (e *Engine) ListCircuitBreakers() map[string]any {
	e.circuitMu.Lock()
	defer e.circuitMu.Unlock()
	res := make(map[string]any)
	for wl, cs := range e.circuit {
		isOpen := cs.failures >= circuitOpenThreshold
		var remainingSeconds float64
		if isOpen {
			elapsed := time.Since(cs.openSince)
			if elapsed < circuitResetAfter {
				remainingSeconds = (circuitResetAfter - elapsed).Seconds()
			} else {
				isOpen = false
			}
		}
		res[wl] = map[string]any{
			"failures":         cs.failures,
			"open":             isOpen,
			"remainingSeconds": remainingSeconds,
		}
	}
	return res
}

// LastReconcileAt returns the timestamp of the last completed reconcile tick.
func (e *Engine) LastReconcileAt() time.Time {
	e.reconcileMu.RLock()
	defer e.reconcileMu.RUnlock()
	return e.lastReconcileAt
}

// LastReconcileDur returns the wall-clock duration of the last reconcile tick.
func (e *Engine) LastReconcileDur() time.Duration {
	e.reconcileMu.RLock()
	defer e.reconcileMu.RUnlock()
	return e.lastReconcileDur
}

func (e *Engine) applyRecreate(ctx context.Context, spec packages.WorkloadSpec, replicas map[int]packages.ActualWorkload) bool {
	hadError := false
	// First delete all replicas that don't match the new spec config (e.g. image changed)
	for replica := 0; replica < spec.Replicas; replica++ {
		if act, ok := replicas[replica]; ok {
			if act.Image != "" && act.Image != spec.Image {
				e.emit(packages.Event{Level: "warn", Source: "core", Workload: spec.Name, Message: fmt.Sprintf("recreating replica %d due to image change", replica)})
				if err := e.runtime.Delete(ctx, spec.Name, replica); err != nil {
					e.emit(packages.Event{Level: "error", Source: "runtime", Workload: spec.Name, Message: err.Error()})
					hadError = true
					continue
				}
				delete(replicas, replica)
			}
		}
	}
	
	// Now apply the rest
	for replica := 0; replica < spec.Replicas; replica++ {
		if _, ok := replicas[replica]; !ok {
			if err := e.runtime.Apply(ctx, spec, replica); err != nil {
				e.emit(packages.Event{Level: "error", Source: "runtime", Workload: spec.Name, Message: err.Error()})
				hadError = true
				continue
			}
			e.emit(packages.Event{Level: "ok", Source: "core", Workload: spec.Name, Message: fmt.Sprintf("started replica %d", replica)})
		}
	}
	return hadError
}

func (e *Engine) applyRolling(ctx context.Context, spec packages.WorkloadSpec, replicas map[int]packages.ActualWorkload) bool {
	hadError := false
	// Update one replica at a time
	for replica := 0; replica < spec.Replicas; replica++ {
		act, exists := replicas[replica]
		if exists {
			if act.Image != "" && act.Image != spec.Image {
				e.emit(packages.Event{Level: "info", Source: "core", Workload: spec.Name, Message: fmt.Sprintf("rolling upgrade replica %d from %s to %s", replica, act.Image, spec.Image)})
				// Delete old replica
				if err := e.runtime.Delete(ctx, spec.Name, replica); err != nil {
					e.emit(packages.Event{Level: "error", Source: "runtime", Workload: spec.Name, Message: err.Error()})
					hadError = true
					continue
				}
				// Start new replica
				if err := e.runtime.Apply(ctx, spec, replica); err != nil {
					e.emit(packages.Event{Level: "error", Source: "runtime", Workload: spec.Name, Message: err.Error()})
					hadError = true
					continue
				}
				e.emit(packages.Event{Level: "ok", Source: "core", Workload: spec.Name, Message: fmt.Sprintf("rolled replica %d successfully", replica)})
				// Delay briefly for rollout simulation / checks
				time.Sleep(500 * time.Millisecond)
			}
		} else {
			if err := e.runtime.Apply(ctx, spec, replica); err != nil {
				e.emit(packages.Event{Level: "error", Source: "runtime", Workload: spec.Name, Message: err.Error()})
				hadError = true
				continue
			}
			e.emit(packages.Event{Level: "ok", Source: "core", Workload: spec.Name, Message: fmt.Sprintf("started replica %d", replica)})
		}
	}
	return hadError
}

package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	store    *Store
	runtime  RuntimeDriver
	bus      *EventBus
	interval time.Duration
	mu       sync.Mutex
}

func NewEngine(store *Store, runtime RuntimeDriver, bus *EventBus, interval time.Duration) *Engine {
	return &Engine{store: store, runtime: runtime, bus: bus, interval: interval}
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

func (e *Engine) Apply(ctx context.Context, actor string, spec WorkloadSpec) error {
	spec = NormalizeSpec(spec)
	if err := ValidateSpec(spec); err != nil {
		_, _ = e.store.AddAudit(AuditRecord{Actor: actor, Action: "apply", Workload: spec.Name, Allowed: false, Reason: err.Error()})
		return err
	}
	if err := e.store.PutWorkload(spec); err != nil {
		return err
	}
	_, _ = e.store.AddAudit(AuditRecord{Actor: actor, Action: "apply", Workload: spec.Name, Allowed: true})
	e.emit(Event{Level: "ok", Source: "api", Workload: spec.Name, Message: "desired workload accepted"})
	return e.Reconcile(ctx)
}

func (e *Engine) Delete(ctx context.Context, actor, name string) error {
	if err := e.store.DeleteWorkload(name); err != nil {
		return err
	}
	if err := e.runtime.DeleteWorkload(ctx, name); err != nil {
		e.emit(Event{Level: "error", Source: "runtime", Workload: name, Message: err.Error()})
	}
	_, _ = e.store.AddAudit(AuditRecord{Actor: actor, Action: "delete", Workload: name, Allowed: true})
	e.emit(Event{Level: "warn", Source: "api", Workload: name, Message: "desired workload deleted"})
	return nil
}

func (e *Engine) Reconcile(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	desired := e.store.ListWorkloads()
	actual, err := e.runtime.List(ctx)
	if err != nil {
		e.emit(Event{Level: "error", Source: "runtime", Message: err.Error()})
		return err
	}

	actualByName := map[string]map[int]ActualWorkload{}
	for _, item := range actual {
		if actualByName[item.Name] == nil {
			actualByName[item.Name] = map[int]ActualWorkload{}
		}
		actualByName[item.Name][item.Replica] = item
	}

	for _, spec := range desired {
		replicas := actualByName[spec.Name]
		for replica := 0; replica < spec.Replicas; replica++ {
			if _, ok := replicas[replica]; !ok {
				if err := e.runtime.Apply(ctx, spec, replica); err != nil {
					e.emit(Event{Level: "error", Source: "runtime", Workload: spec.Name, Message: err.Error()})
					continue
				}
				e.emit(Event{Level: "ok", Source: "core", Workload: spec.Name, Message: fmt.Sprintf("started replica %d", replica)})
			}
		}
		for replica := range replicas {
			if replica >= spec.Replicas {
				if err := e.runtime.Delete(ctx, spec.Name, replica); err != nil {
					e.emit(Event{Level: "error", Source: "runtime", Workload: spec.Name, Message: err.Error()})
					continue
				}
				e.emit(Event{Level: "warn", Source: "core", Workload: spec.Name, Message: fmt.Sprintf("removed extra replica %d", replica)})
			}
		}
	}

	desiredNames := map[string]struct{}{}
	for _, spec := range desired {
		desiredNames[spec.Name] = struct{}{}
	}
	for name := range actualByName {
		if _, ok := desiredNames[name]; !ok {
			_ = e.runtime.DeleteWorkload(ctx, name)
			e.emit(Event{Level: "warn", Source: "core", Workload: name, Message: "removed unmanaged Kranix replica set"})
		}
	}

	e.emit(Event{Level: "ok", Source: "core", Message: "reconcile tick complete"})
	return nil
}

func (e *Engine) Status(ctx context.Context) ([]WorkloadStatus, error) {
	specs := e.store.ListWorkloads()
	actual, err := e.runtime.List(ctx)
	if err != nil {
		return nil, err
	}
	actualByName := map[string][]ActualWorkload{}
	for _, item := range actual {
		actualByName[item.Name] = append(actualByName[item.Name], item)
	}
	out := make([]WorkloadStatus, 0, len(specs))
	for _, spec := range specs {
		items := actualByName[spec.Name]
		status := WorkloadStatus{Spec: spec, Actual: items, Healthy: len(items) == spec.Replicas}
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

func (e *Engine) emit(event Event) {
	saved, err := e.store.AddEvent(event)
	if err == nil {
		event.ID = saved.ID
		event.Time = saved.Time
	}
	e.bus.Publish(event)
}

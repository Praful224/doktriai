package core

import (
	"context"
	"testing"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

type canaryMockDriver struct {
	actual []packages.ActualWorkload
}

func (m *canaryMockDriver) Name() string { return "canary-mock" }

func (m *canaryMockDriver) List(_ context.Context) ([]packages.ActualWorkload, error) {
	return m.actual, nil
}

func (m *canaryMockDriver) Apply(_ context.Context, spec packages.WorkloadSpec, replica int) error {
	found := false
	for i, act := range m.actual {
		if act.Name == spec.Name && act.Replica == replica {
			m.actual[i].Image = spec.Image
			found = true
			break
		}
	}
	if !found {
		m.actual = append(m.actual, packages.ActualWorkload{
			Name:    spec.Name,
			Replica: replica,
			Image:   spec.Image,
			Status:  "running",
		})
	}
	return nil
}

func (m *canaryMockDriver) Delete(_ context.Context, workload string, replica int) error {
	for i, act := range m.actual {
		if act.Name == workload && act.Replica == replica {
			m.actual = append(m.actual[:i], m.actual[i+1:]...)
			break
		}
	}
	return nil
}

func (m *canaryMockDriver) DeleteWorkload(_ context.Context, workload string) error {
	var keep []packages.ActualWorkload
	for _, act := range m.actual {
		if act.Name != workload {
			keep = append(keep, act)
		}
	}
	m.actual = keep
	return nil
}

func (m *canaryMockDriver) Logs(_ context.Context, workload string, tail int) ([]string, error) {
	return nil, nil
}

func filterReplicas(actual []packages.ActualWorkload, name string) []packages.ActualWorkload {
	var res []packages.ActualWorkload
	for _, act := range actual {
		if act.Name == name {
			res = append(res, act)
		}
	}
	return res
}

func TestCanaryRolloutWorkflow(t *testing.T) {
	store, _ := tempStore(t)
	bus := NewEventBus(20)
	driver := &canaryMockDriver{}
	engine := NewEngine(store, driver, bus, 30*time.Second)

	ctx := context.Background()

	// 1. Initial stable state: 5 replicas running version v1
	v1Spec := packages.WorkloadSpec{
		Name:           "nginx-service",
		Image:          "nginx:1.20",
		Replicas:       5,
		DeployStrategy: "canary",
	}

	err := engine.Apply(ctx, "operator", v1Spec)
	if err != nil {
		t.Fatalf("failed to apply initial spec: %v", err)
	}

	// Verify all 5 replicas run nginx:1.20
	allAct, _ := driver.List(ctx)
	actual := filterReplicas(allAct, "nginx-service")
	if len(actual) != 5 {
		t.Fatalf("expected 5 replicas initially, got %d", len(actual))
	}
	for _, act := range actual {
		if act.Image != "nginx:1.20" {
			t.Errorf("expected image nginx:1.20, got %s", act.Image)
		}
	}

	// 2. Trigger Canary: Apply version v2 image update
	v2Spec := v1Spec
	v2Spec.Image = "nginx:1.21"

	err = engine.Apply(ctx, "operator", v2Spec)
	if err != nil {
		t.Fatalf("failed to apply updated spec: %v", err)
	}

	// Active canary check
	canary, ok := engine.GetCanary("nginx-service")
	if !ok || !canary.Active {
		t.Fatal("expected active canary rollout to be initialized")
	}
	if canary.Weight != 10 || canary.Step != 0 {
		t.Errorf("expected 10%% weight at step 0, got %d%% at step %d", canary.Weight, canary.Step)
	}

	// Weight = 10% on 5 replicas: should result in 1 canary replica (index 0) and 4 old replicas (indexes 1-4)
	allAct, _ = driver.List(ctx)
	actual = filterReplicas(allAct, "nginx-service")
	if len(actual) != 5 {
		t.Fatalf("expected 5 replicas during canary step 0, got %d", len(actual))
	}
	
	// Map replicas by index for easy assertions
	actualMap := make(map[int]packages.ActualWorkload)
	for _, act := range actual {
		actualMap[act.Replica] = act
	}

	if actualMap[0].Image != "nginx:1.21" {
		t.Errorf("replica 0 should run canary image nginx:1.21, got %s", actualMap[0].Image)
	}
	for r := 1; r < 5; r++ {
		if actualMap[r].Image != "nginx:1.20" {
			t.Errorf("replica %d should run stable image nginx:1.20, got %s", r, actualMap[r].Image)
		}
	}

	// 3. Promote to Step 1 (50% weight)
	canary, err = engine.PromoteCanary(ctx, "nginx-service")
	if err != nil {
		t.Fatalf("failed to promote canary: %v", err)
	}
	if canary.Weight != 50 || canary.Step != 1 {
		t.Errorf("expected 50%% weight at step 1, got %d%% at step %d", canary.Weight, canary.Step)
	}

	// 50% weight on 5 replicas: should result in 2 canary replicas (indexes 0-1) and 3 old replicas (indexes 2-4)
	allAct, _ = driver.List(ctx)
	actual = filterReplicas(allAct, "nginx-service")
	actualMap = make(map[int]packages.ActualWorkload)
	for _, act := range actual {
		actualMap[act.Replica] = act
	}

	for r := 0; r < 2; r++ {
		if actualMap[r].Image != "nginx:1.21" {
			t.Errorf("replica %d should run canary image nginx:1.21, got %s", r, actualMap[r].Image)
		}
	}
	for r := 2; r < 5; r++ {
		if actualMap[r].Image != "nginx:1.20" {
			t.Errorf("replica %d should run stable image nginx:1.20, got %s", r, actualMap[r].Image)
		}
	}

	// 4. Promote to Step 2 (100% weight)
	canary, err = engine.PromoteCanary(ctx, "nginx-service")
	if err != nil {
		t.Fatalf("failed to promote canary: %v", err)
	}
	if canary.Weight != 100 || canary.Step != 2 {
		t.Errorf("expected 100%% weight at step 2, got %d%% at step %d", canary.Weight, canary.Step)
	}

	// 100% weight on 5 replicas: all 5 run new version
	allAct, _ = driver.List(ctx)
	actual = filterReplicas(allAct, "nginx-service")
	for _, act := range actual {
		if act.Image != "nginx:1.21" {
			t.Errorf("replica %d should run canary image nginx:1.21, got %s", act.Replica, act.Image)
		}
	}

	// 5. Finalize canary
	_, err = engine.PromoteCanary(ctx, "nginx-service")
	if err == nil {
		t.Error("expected error attempting to promote completed canary")
	}

	canary, ok = engine.GetCanary("nginx-service")
	if ok && canary.Active {
		t.Error("expected canary to be complete/inactive")
	}
}

func TestCanaryRollback(t *testing.T) {
	store, _ := tempStore(t)
	bus := NewEventBus(20)
	driver := &canaryMockDriver{}
	engine := NewEngine(store, driver, bus, 30*time.Second)

	ctx := context.Background()

	// Initial stable state
	v1Spec := packages.WorkloadSpec{
		Name:           "nginx-service",
		Image:          "nginx:1.20",
		Replicas:       5,
		DeployStrategy: "canary",
	}
	_ = engine.Apply(ctx, "operator", v1Spec)

	// Apply version v2 image update to trigger canary rollout (Step 0, 10%)
	v2Spec := v1Spec
	v2Spec.Image = "nginx:1.21"
	_ = engine.Apply(ctx, "operator", v2Spec)

	// Abort/Rollback Canary
	err := engine.RollbackCanary(ctx, "nginx-service", "operator")
	if err != nil {
		t.Fatalf("failed to rollback canary: %v", err)
	}

	// Canary should be deleted from active tracking map
	_, ok := engine.GetCanary("nginx-service")
	if ok {
		t.Error("expected canary rollout tracking to be deleted")
	}

	// Replicas should be reconciled back to old stable version (nginx:1.20)
	allAct, _ := driver.List(ctx)
	actual := filterReplicas(allAct, "nginx-service")
	if len(actual) != 5 {
		t.Fatalf("expected 5 replicas after rollback, got %d", len(actual))
	}
	for _, act := range actual {
		if act.Image != "nginx:1.20" {
			t.Errorf("replica %d should run rolled back image nginx:1.20, got %s", act.Replica, act.Image)
		}
	}
}

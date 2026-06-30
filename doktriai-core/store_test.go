package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

func tempStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store, path
}

// ─── OpenStore ────────────────────────────────────────────────────────────────

func TestOpenStore_CreatesFile(t *testing.T) {
	store, path := tempStore(t)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected state.json to be created")
	}
}

func TestOpenStore_SeedData(t *testing.T) {
	store, _ := tempStore(t)
	workloads := store.ListWorkloads()
	if len(workloads) == 0 {
		t.Error("expected seed workloads on fresh store")
	}

	// Check seed workload names
	names := make(map[string]bool)
	for _, w := range workloads {
		names[w.Name] = true
	}
	for _, expected := range []string{"secure-ingress", "reconciler-daemon", "agent-gateway"} {
		if !names[expected] {
			t.Errorf("expected seed workload %q", expected)
		}
	}
}

func TestOpenStore_Reopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// First open — creates seed data
	store1, err := OpenStore(path)
	if err != nil {
		t.Fatalf("first open failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store1.Close()
	})

	// Add a custom workload
	store1.PutWorkload(packages.WorkloadSpec{Name: "custom-app", Image: "redis:7", Replicas: 2, Runtime: "docker"}, "admin")

	// Reopen — should restore state
	store2, err := OpenStore(path)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store2.Close()
	})

	spec, ok := store2.GetWorkload("custom-app")
	if !ok {
		t.Error("expected custom-app to persist across reopen")
	}
	if spec.Image != "redis:7" {
		t.Errorf("expected image 'redis:7', got %q", spec.Image)
	}
}

// ─── Workload CRUD ────────────────────────────────────────────────────────────

func TestStore_PutAndGetWorkload(t *testing.T) {
	store, _ := tempStore(t)

	spec := packages.WorkloadSpec{Name: "my-app", Image: "nginx:alpine", Replicas: 3, Runtime: "docker"}
	if err := store.PutWorkload(spec, "admin"); err != nil {
		t.Fatalf("PutWorkload failed: %v", err)
	}

	got, ok := store.GetWorkload("my-app")
	if !ok {
		t.Fatal("expected workload to exist")
	}
	if got.Replicas != 3 {
		t.Errorf("expected 3 replicas, got %d", got.Replicas)
	}
}

func TestStore_GetWorkload_NotFound(t *testing.T) {
	store, _ := tempStore(t)
	_, ok := store.GetWorkload("nonexistent")
	if ok {
		t.Error("expected workload not found")
	}
}

func TestStore_DeleteWorkload(t *testing.T) {
	store, _ := tempStore(t)

	store.PutWorkload(packages.WorkloadSpec{Name: "to-delete", Image: "nginx:alpine", Replicas: 1, Runtime: "docker"}, "admin")

	if err := store.DeleteWorkload("to-delete"); err != nil {
		t.Fatalf("DeleteWorkload failed: %v", err)
	}

	_, ok := store.GetWorkload("to-delete")
	if ok {
		t.Error("expected workload deleted")
	}
}

func TestStore_DeleteWorkload_Nonexistent(t *testing.T) {
	store, _ := tempStore(t)
	// Should not error on deleting non-existent workload
	if err := store.DeleteWorkload("ghost"); err != nil {
		t.Errorf("expected no error deleting non-existent workload, got: %v", err)
	}
}

func TestStore_UpdateWorkload(t *testing.T) {
	store, _ := tempStore(t)

	store.PutWorkload(packages.WorkloadSpec{Name: "update-me", Image: "nginx:alpine", Replicas: 1, Runtime: "docker"}, "admin")
	store.PutWorkload(packages.WorkloadSpec{Name: "update-me", Image: "nginx:latest", Replicas: 5, Runtime: "docker"}, "admin")

	got, _ := store.GetWorkload("update-me")
	if got.Image != "nginx:latest" {
		t.Errorf("expected updated image, got %q", got.Image)
	}
	if got.Replicas != 5 {
		t.Errorf("expected 5 replicas, got %d", got.Replicas)
	}
}

func TestStore_ListWorkloads_Sorted(t *testing.T) {
	store, _ := tempStore(t)
	// Clear seed data and add our own
	store.DeleteWorkload("secure-ingress")
	store.DeleteWorkload("reconciler-daemon")
	store.DeleteWorkload("agent-gateway")

	store.PutWorkload(packages.WorkloadSpec{Name: "zzz-last", Image: "nginx:alpine", Replicas: 1, Runtime: "docker"}, "admin")
	store.PutWorkload(packages.WorkloadSpec{Name: "aaa-first", Image: "nginx:alpine", Replicas: 1, Runtime: "docker"}, "admin")
	store.PutWorkload(packages.WorkloadSpec{Name: "mmm-middle", Image: "nginx:alpine", Replicas: 1, Runtime: "docker"}, "admin")

	list := store.ListWorkloads()
	if len(list) != 3 {
		t.Fatalf("expected 3 workloads, got %d", len(list))
	}
	if list[0].Name != "aaa-first" || list[1].Name != "mmm-middle" || list[2].Name != "zzz-last" {
		t.Errorf("expected sorted order, got: %q, %q, %q", list[0].Name, list[1].Name, list[2].Name)
	}
}

// ─── Audit ────────────────────────────────────────────────────────────────────

func TestStore_AddAudit(t *testing.T) {
	store, _ := tempStore(t)

	record, err := store.AddAudit(packages.AuditRecord{
		Actor: "alice", Action: "apply", Workload: "test-app", Allowed: true,
	})
	if err != nil {
		t.Fatalf("AddAudit failed: %v", err)
	}
	if record.ID == 0 {
		t.Error("expected non-zero audit ID")
	}
	if record.Time.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestStore_AuditOrdering(t *testing.T) {
	store, _ := tempStore(t)

	store.AddAudit(packages.AuditRecord{Actor: "first", Action: "apply", Allowed: true})
	store.AddAudit(packages.AuditRecord{Actor: "second", Action: "apply", Allowed: true})

	audit := store.ListAudit()
	if len(audit) < 2 {
		t.Fatalf("expected at least 2 audit records, got %d", len(audit))
	}
	// Most recent should be first (prepended)
	if audit[0].Actor != "second" {
		t.Errorf("expected most recent first, got %q", audit[0].Actor)
	}
}

func TestStore_AuditCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store, _ := OpenStore(path)
	t.Cleanup(func() {
		_ = store.Close()
	})

	// Add 510 audit records
	for i := 0; i < 510; i++ {
		store.AddAudit(packages.AuditRecord{Actor: "bot", Action: "apply", Allowed: true})
	}

	audit := store.ListAudit()
	if len(audit) > 500 {
		t.Errorf("expected audit capped at 500, got %d", len(audit))
	}
}

func TestStore_GetAuditSince(t *testing.T) {
	store, _ := tempStore(t)

	r1, _ := store.AddAudit(packages.AuditRecord{Actor: "a", Action: "apply", Allowed: true})
	store.AddAudit(packages.AuditRecord{Actor: "b", Action: "apply", Allowed: true})
	store.AddAudit(packages.AuditRecord{Actor: "c", Action: "apply", Allowed: true})

	since := store.GetAuditSince(r1.SeqID)
	if len(since) < 3 {
		t.Errorf("expected at least 3 records since seq %d, got %d", r1.SeqID, len(since))
	}
}

// ─── Events ───────────────────────────────────────────────────────────────────

func TestStore_AddEvent(t *testing.T) {
	store, _ := tempStore(t)

	event, err := store.AddEvent(packages.Event{
		Level: "ok", Source: "test", Message: "hello world",
	})
	if err != nil {
		t.Fatalf("AddEvent failed: %v", err)
	}
	if event.ID == 0 {
		t.Error("expected non-zero event ID")
	}
}

func TestStore_EventsCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store, _ := OpenStore(path)
	t.Cleanup(func() {
		_ = store.Close()
	})

	for i := 0; i < 510; i++ {
		store.AddEvent(packages.Event{Level: "ok", Source: "test", Message: "tick"})
	}

	events := store.ListEvents()
	if len(events) > 500 {
		t.Errorf("expected events capped at 500, got %d", len(events))
	}
}

// ─── Lock ─────────────────────────────────────────────────────────────────────

func TestStore_LockAcquireAndRelease(t *testing.T) {
	store, _ := tempStore(t)

	if store.GetLock().Locked {
		t.Error("expected unlocked by default")
	}

	if err := store.AcquireLock("alice", "maintenance"); err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}

	lock := store.GetLock()
	if !lock.Locked {
		t.Error("expected locked after acquire")
	}
	if lock.AcquiredBy != "alice" {
		t.Errorf("expected lockedBy 'alice', got %q", lock.AcquiredBy)
	}
	if lock.Reason != "maintenance" {
		t.Errorf("expected reason 'maintenance', got %q", lock.Reason)
	}

	if err := store.ReleaseLock(); err != nil {
		t.Fatalf("ReleaseLock failed: %v", err)
	}

	if store.GetLock().Locked {
		t.Error("expected unlocked after release")
	}
}

// ─── SnapshotHash ─────────────────────────────────────────────────────────────

func TestStore_SnapshotHash_ChangesOnMutation(t *testing.T) {
	store, _ := tempStore(t)

	hash1 := store.SnapshotHash()
	store.PutWorkload(packages.WorkloadSpec{Name: "new-app", Image: "redis:7", Replicas: 1, Runtime: "docker"}, "admin")
	hash2 := store.SnapshotHash()

	if hash1 == hash2 {
		t.Error("expected different hashes after workload mutation")
	}
}

func TestStore_SnapshotHash_Deterministic(t *testing.T) {
	store, _ := tempStore(t)
	h1 := store.SnapshotHash()
	h2 := store.SnapshotHash()
	if h1 != h2 {
		t.Error("expected same hash on consecutive calls without mutation")
	}
}

// ─── Atomic Save ──────────────────────────────────────────────────────────────

func TestStore_AtomicSave(t *testing.T) {
	store, path := tempStore(t)

	// Write and verify no .tmp file left behind
	store.PutWorkload(packages.WorkloadSpec{Name: "atomic-test", Image: "nginx:alpine", Replicas: 1, Runtime: "docker"}, "admin")

	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("expected .tmp file to be cleaned up after atomic save")
	}
}

// ─── TTL Lease Expiry ──────────────────────────────────────────────────────────

func TestStore_WorkloadTTL(t *testing.T) {
	store, _ := tempStore(t)

	spec := packages.WorkloadSpec{
		Name:       "ttl-app",
		Image:      "nginx:alpine",
		Replicas:   1,
		Runtime:    "docker",
		TTLSeconds: 1, // Expires in 1 second
	}
	if err := store.PutWorkload(spec, "admin"); err != nil {
		t.Fatalf("failed to put workload: %v", err)
	}

	// Should not be expired immediately
	expired, err := store.IsWorkloadExpired("ttl-app")
	if err != nil {
		t.Fatalf("IsWorkloadExpired failed: %v", err)
	}
	if expired {
		t.Error("expected workload to not be expired immediately")
	}

	// Wait 1.2 seconds for lease to expire
	time.Sleep(1200 * time.Millisecond)

	expired, err = store.IsWorkloadExpired("ttl-app")
	if err != nil {
		t.Fatalf("IsWorkloadExpired failed: %v", err)
	}
	if !expired {
		t.Error("expected workload to be expired after 1.2 seconds")
	}
}

// ─── Topological Sort ─────────────────────────────────────────────────────────

func TestTopologicalSort(t *testing.T) {
	specs := []packages.WorkloadSpec{
		{Name: "app", DependsOn: []string{"backend"}},
		{Name: "backend", DependsOn: []string{"db"}},
		{Name: "db"},
	}

	sorted, err := topologicalSort(specs)
	if err != nil {
		t.Fatalf("topologicalSort failed: %v", err)
	}

	if len(sorted) != 3 {
		t.Fatalf("expected 3 sorted workloads, got %d", len(sorted))
	}

	// db should be first, then backend, then app
	if sorted[0].Name != "db" || sorted[1].Name != "backend" || sorted[2].Name != "app" {
		t.Errorf("incorrect sort order: %s -> %s -> %s", sorted[0].Name, sorted[1].Name, sorted[2].Name)
	}

	// Test cycle detection
	cycleSpecs := []packages.WorkloadSpec{
		{Name: "app", DependsOn: []string{"backend"}},
		{Name: "backend", DependsOn: []string{"app"}},
	}
	_, err = topologicalSort(cycleSpecs)
	if err == nil {
		t.Error("expected error due to dependency cycle, got nil")
	}
}

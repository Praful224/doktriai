package storage

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
	_ "github.com/lib/pq" // Ensure postgres driver is imported (usually in go.mod)
)

func getTestDB(t *testing.T) (*sql.DB, bool) {
	t.Helper()
	connStr := os.Getenv("DOKTRIAI_TEST_DB_URL")
	if connStr == "" {
		// Try fallback to local docker instance
		connStr = "postgres://doktri_user:doktri_secure_password@localhost:5432/doktriai_db?sslmode=disable"
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Logf("Database connection skipped (no PostgreSQL running): %v", err)
		return nil, false
	}
	err = db.Ping()
	if err != nil {
		t.Logf("PostgreSQL is unreachable: %v. Please start container: docker run -d --name doktriai-db -p 5432:5432 -e POSTGRES_DB=doktriai_db -e POSTGRES_USER=doktri_user -e POSTGRES_PASSWORD=doktri_secure_password postgres:15", err)
		return nil, false
	}
	return db, true
}

func TestPostgresStorage_Workflow(t *testing.T) {
	db, ok := getTestDB(t)
	if !ok {
		t.Skip("PostgreSQL database connection unavailable for testing")
	}
	defer db.Close()

	ctx := context.Background()
	store := NewPostgresStorage(db)

	// Clean up previous runs
	_, _ = db.Exec("DROP TABLE IF EXISTS workloads, audit_records, environment_locks, pte_plans CASCADE;")

	// 1. Run migrations
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// 2. Put & Get Workload
	spec := packages.WorkloadSpec{
		Name:          "test-scalable-app",
		Image:         "nginx:alpine",
		Replicas:      3,
		Port:          80,
		ContainerPort: 80,
		Runtime:       "docker",
		Env:           map[string]string{"ENV_VAR": "value"},
	}
	if err := store.PutWorkload(ctx, spec); err != nil {
		t.Fatalf("PutWorkload failed: %v", err)
	}

	got, found, err := store.GetWorkload(ctx, spec.Name)
	if err != nil || !found {
		t.Fatalf("GetWorkload failed: found=%v, err=%v", found, err)
	}
	if got.Replicas != 3 || got.Env["ENV_VAR"] != "value" {
		t.Errorf("Returned spec properties mismatched: %+v", got)
	}

	// 3. Test list
	list, err := store.ListWorkloads(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListWorkloads failed: len=%d, err=%v", len(list), err)
	}

	// 4. Test Lock
	lock, err := store.GetLock(ctx)
	if err != nil || lock.Locked {
		t.Fatalf("Default lock state should be unlocked, got: %+v, err: %v", lock, err)
	}

	if err := store.AcquireLock(ctx, "architect-tester", "production scale test"); err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}

	lock, _ = store.GetLock(ctx)
	if !lock.Locked || lock.AcquiredBy != "architect-tester" {
		t.Errorf("Lock acquisition state wrong: %+v", lock)
	}

	_ = store.ReleaseLock(ctx)

	// 5. Test Audit limit
	for i := 0; i < 505; i++ {
		_, err = store.AddAudit(ctx, packages.AuditRecord{
			Actor:    "scale-bot",
			Action:   "deploy",
			Workload: "test-app",
			Allowed:  true,
		})
		if err != nil {
			t.Fatalf("AddAudit iteration %d failed: %v", i, err)
		}
	}

	audits, err := store.ListAudit(ctx)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audits) > 500 {
		t.Errorf("Audits failed to cap at 500 records: got %d", len(audits))
	}

	// 6. Test PTE Plan Gate
	plan := &packages.PendingPlan{
		ID:             "plan-123",
		RequestedBy:    "agent-claude",
		ApprovalReason: "exceeds replica limits",
		Status:         "pending",
		CreatedAt:      time.Now().UTC(),
		ExpiresAt:      time.Now().Add(10 * time.Minute).UTC(),
		Spec:           spec,
	}
	if err := store.SubmitPlan(ctx, plan); err != nil {
		t.Fatalf("SubmitPlan failed: %v", err)
	}

	planGot, err := store.GetPlan(ctx, plan.ID)
	if err != nil || planGot.Status != "pending" {
		t.Fatalf("GetPlan failed: %+v, err: %v", planGot, err)
	}

	if err := store.UpdatePlanStatus(ctx, plan.ID, "approved", "admin-user", ""); err != nil {
		t.Fatalf("UpdatePlanStatus failed: %v", err)
	}

	planGot, _ = store.GetPlan(ctx, plan.ID)
	if planGot.Status != "approved" || planGot.ApprovedBy != "admin-user" {
		t.Errorf("Approve update failed: %+v", planGot)
	}
}

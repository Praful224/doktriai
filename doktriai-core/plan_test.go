package core

import (
	"strings"
	"testing"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

// ─── PlanStore Submit ─────────────────────────────────────────────────────────

func TestPlanStore_Submit(t *testing.T) {
	ps := NewPlanStore()
	spec := packages.WorkloadSpec{Name: "test-app", Image: "nginx:alpine", Replicas: 10}

	plan, err := ps.Submit("alice", "agent-1", "deploy app", "high replicas", spec)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	if plan.ID == "" {
		t.Error("expected non-empty plan ID")
	}
	if !strings.HasPrefix(plan.ID, "plan-") {
		t.Errorf("expected plan ID prefix 'plan-', got %q", plan.ID)
	}
	if plan.RequestedBy != "alice" {
		t.Errorf("expected requestedBy 'alice', got %q", plan.RequestedBy)
	}
	if plan.Status != packages.PlanStatusPending {
		t.Errorf("expected status 'pending', got %q", plan.Status)
	}
	if plan.ExpiresAt.Before(time.Now()) {
		t.Error("expected future expiry time")
	}
}

func TestPlanStore_SubmitMultiple(t *testing.T) {
	ps := NewPlanStore()
	spec := packages.WorkloadSpec{Name: "app", Image: "nginx:alpine", Replicas: 1}

	p1, _ := ps.Submit("alice", "", "", "reason1", spec)
	p2, _ := ps.Submit("bob", "", "", "reason2", spec)

	if p1.ID == p2.ID {
		t.Error("expected unique plan IDs")
	}

	plans := ps.List()
	if len(plans) != 2 {
		t.Errorf("expected 2 plans, got %d", len(plans))
	}
}

// ─── PlanStore Get ────────────────────────────────────────────────────────────

func TestPlanStore_Get(t *testing.T) {
	ps := NewPlanStore()
	spec := packages.WorkloadSpec{Name: "test", Image: "nginx:alpine", Replicas: 1}
	plan, _ := ps.Submit("alice", "", "", "test", spec)

	got, err := ps.Get(plan.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.ID != plan.ID {
		t.Errorf("expected plan ID %q, got %q", plan.ID, got.ID)
	}
}

func TestPlanStore_GetNotFound(t *testing.T) {
	ps := NewPlanStore()
	_, err := ps.Get("plan-nonexistent")
	if err == nil {
		t.Error("expected error for non-existent plan")
	}
}

// ─── PlanStore Approve ────────────────────────────────────────────────────────

func TestPlanStore_Approve(t *testing.T) {
	ps := NewPlanStore()
	spec := packages.WorkloadSpec{Name: "test", Image: "nginx:alpine", Replicas: 10}
	plan, _ := ps.Submit("alice", "", "", "high replicas", spec)

	approved, err := ps.Approve(plan.ID, "admin-bob")
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if approved.Status != packages.PlanStatusApproved {
		t.Errorf("expected status 'approved', got %q", approved.Status)
	}
	if approved.ApprovedBy != "admin-bob" {
		t.Errorf("expected approvedBy 'admin-bob', got %q", approved.ApprovedBy)
	}
	if approved.ApprovedAt == nil {
		t.Error("expected non-nil approvedAt timestamp")
	}
}

func TestPlanStore_ApproveNonPending(t *testing.T) {
	ps := NewPlanStore()
	spec := packages.WorkloadSpec{Name: "test", Image: "nginx:alpine", Replicas: 1}
	plan, _ := ps.Submit("alice", "", "", "test", spec)

	// Approve first
	ps.Approve(plan.ID, "admin")

	// Try to approve again
	_, err := ps.Approve(plan.ID, "admin")
	if err == nil {
		t.Error("expected error approving already-approved plan")
	}
	if !strings.Contains(err.Error(), "not in pending status") {
		t.Errorf("expected 'not in pending status' error, got: %v", err)
	}
}

func TestPlanStore_ApproveNotFound(t *testing.T) {
	ps := NewPlanStore()
	_, err := ps.Approve("plan-nonexistent", "admin")
	if err == nil {
		t.Error("expected error for non-existent plan")
	}
}

// ─── PlanStore Reject ─────────────────────────────────────────────────────────

func TestPlanStore_Reject(t *testing.T) {
	ps := NewPlanStore()
	spec := packages.WorkloadSpec{Name: "test", Image: "nginx:alpine", Replicas: 1}
	plan, _ := ps.Submit("alice", "", "", "test", spec)

	err := ps.Reject(plan.ID, "admin-bob", "too risky")
	if err != nil {
		t.Fatalf("reject failed: %v", err)
	}

	got, _ := ps.Get(plan.ID)
	if got.Status != packages.PlanStatusRejected {
		t.Errorf("expected status 'rejected', got %q", got.Status)
	}
	if got.RejectedBy != "admin-bob" {
		t.Errorf("expected rejectedBy 'admin-bob', got %q", got.RejectedBy)
	}
	if got.RejectionComment != "too risky" {
		t.Errorf("expected comment 'too risky', got %q", got.RejectionComment)
	}
}

func TestPlanStore_RejectNonPending(t *testing.T) {
	ps := NewPlanStore()
	spec := packages.WorkloadSpec{Name: "test", Image: "nginx:alpine", Replicas: 1}
	plan, _ := ps.Submit("alice", "", "", "test", spec)

	ps.Reject(plan.ID, "admin", "nope")

	err := ps.Reject(plan.ID, "admin", "double reject")
	if err == nil {
		t.Error("expected error rejecting already-rejected plan")
	}
}

func TestPlanStore_RejectThenApprove(t *testing.T) {
	ps := NewPlanStore()
	spec := packages.WorkloadSpec{Name: "test", Image: "nginx:alpine", Replicas: 1}
	plan, _ := ps.Submit("alice", "", "", "test", spec)

	ps.Reject(plan.ID, "admin", "nope")

	_, err := ps.Approve(plan.ID, "admin")
	if err == nil {
		t.Error("expected error approving rejected plan")
	}
}

// ─── PlanStore Expiry ─────────────────────────────────────────────────────────

func TestPlanStore_ExpiredPlanCannotBeApproved(t *testing.T) {
	ps := NewPlanStore()
	spec := packages.WorkloadSpec{Name: "test", Image: "nginx:alpine", Replicas: 1}
	plan, _ := ps.Submit("alice", "", "", "test", spec)

	// Manually expire the plan
	ps.mu.Lock()
	ps.plans[plan.ID].ExpiresAt = time.Now().Add(-1 * time.Minute)
	ps.mu.Unlock()

	_, err := ps.Approve(plan.ID, "admin")
	if err == nil {
		t.Error("expected error approving expired plan")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected 'expired' in error, got: %v", err)
	}
}

func TestPlanStore_PruneExpired(t *testing.T) {
	ps := NewPlanStore()
	spec := packages.WorkloadSpec{Name: "test", Image: "nginx:alpine", Replicas: 1}
	plan, _ := ps.Submit("alice", "", "", "test", spec)

	// Manually expire
	ps.mu.Lock()
	ps.plans[plan.ID].ExpiresAt = time.Now().Add(-1 * time.Minute)
	ps.mu.Unlock()

	// List triggers prune
	plans := ps.List()
	for _, p := range plans {
		if p.ID == plan.ID && p.Status != packages.PlanStatusExpired {
			t.Errorf("expected expired status after prune, got %q", p.Status)
		}
	}
}

// ─── Plan ID Generation ──────────────────────────────────────────────────────

func TestNewPlanID_Format(t *testing.T) {
	id, err := newPlanID()
	if err != nil {
		t.Fatalf("newPlanID failed: %v", err)
	}
	if !strings.HasPrefix(id, "plan-") {
		t.Errorf("expected 'plan-' prefix, got %q", id)
	}
	// plan- (5 chars) + 24 hex chars (12 bytes) = 29 chars
	if len(id) != 29 {
		t.Errorf("expected 29 char plan ID, got %d: %q", len(id), id)
	}
}

func TestNewPlanID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, _ := newPlanID()
		if seen[id] {
			t.Fatalf("duplicate plan ID: %s", id)
		}
		seen[id] = true
	}
}

package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

const (
	// planTTL is how long a pending plan waits for approval before auto-expiring.
	planTTL = 15 * time.Minute
)

// PlanStore is the in-memory store for pending PTE approval plans.
// Thread-safe; all mutations are protected by a mutex.
type PlanStore struct {
	mu    sync.RWMutex
	plans map[string]*packages.PendingPlan
}

// NewPlanStore creates an initialized PlanStore.
func NewPlanStore() *PlanStore {
	return &PlanStore{plans: make(map[string]*packages.PendingPlan)}
}

// Submit creates a new pending plan for the given spec, requested by actor.
// Returns the plan ID that the requester should surface to an operator.
func (ps *PlanStore) Submit(
	requestedBy, agentID, agentGoal, approvalReason string,
	spec packages.WorkloadSpec,
) (*packages.PendingPlan, error) {
	id, err := newPlanID()
	if err != nil {
		return nil, fmt.Errorf("plan id generation failed: %w", err)
	}
	now := time.Now().UTC()
	plan := &packages.PendingPlan{
		ID:             id,
		RequestedBy:    requestedBy,
		AgentID:        agentID,
		AgentGoal:      agentGoal,
		Spec:           spec,
		ApprovalReason: approvalReason,
		Status:         packages.PlanStatusPending,
		CreatedAt:      now,
		ExpiresAt:      now.Add(planTTL),
	}
	ps.mu.Lock()
	ps.plans[id] = plan
	ps.mu.Unlock()

	// Fire notifications
	NotifyPTEWebhook(PlanEvent{
		Event:     "plan_created",
		PlanID:    plan.ID,
		Workload:  plan.Spec.Name,
		Actor:     requestedBy,
		Reason:    approvalReason,
		ExpiresAt: &plan.ExpiresAt,
		Spec:      plan.Spec,
		Timestamp: time.Now().UTC(),
	})
	NotifySlack(plan, os.Getenv("DOKTRIAI_API_BASE_URL"))

	return plan, nil
}

// Get returns the plan by ID, and prunes expired plans.
func (ps *PlanStore) Get(id string) (*packages.PendingPlan, error) {
	ps.pruneExpired()
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.plans[id]
	if !ok {
		return nil, fmt.Errorf("plan %q not found", id)
	}
	return p, nil
}

// List returns all plans (pending + recent approved/rejected, excluding expired).
func (ps *PlanStore) List() []*packages.PendingPlan {
	ps.pruneExpired()
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	out := make([]*packages.PendingPlan, 0, len(ps.plans))
	for _, p := range ps.plans {
		out = append(out, p)
	}
	return out
}

// Approve transitions a pending plan to approved, recording who approved it.
// Returns the plan so the caller can immediately apply it.
func (ps *PlanStore) Approve(id, approver string) (*packages.PendingPlan, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.plans[id]
	if !ok {
		return nil, fmt.Errorf("plan %q not found", id)
	}
	if p.Status != packages.PlanStatusPending {
		return nil, fmt.Errorf("plan %q is not in pending status (current: %s)", id, p.Status)
	}
	if time.Now().UTC().After(p.ExpiresAt) {
		p.Status = packages.PlanStatusExpired
		return nil, fmt.Errorf("plan %q has expired (15-min TTL exceeded)", id)
	}
	now := time.Now().UTC()
	p.Status = packages.PlanStatusApproved
	p.ApprovedBy = approver
	p.ApprovedAt = &now

	NotifyPTEWebhook(PlanEvent{
		Event:     "plan_approved",
		PlanID:    p.ID,
		Workload:  p.Spec.Name,
		Actor:     approver,
		Reason:    p.ApprovalReason,
		Spec:      p.Spec,
		Timestamp: now,
	})

	return p, nil
}

// Reject transitions a pending plan to rejected with a comment.
func (ps *PlanStore) Reject(id, rejector, comment string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.plans[id]
	if !ok {
		return fmt.Errorf("plan %q not found", id)
	}
	if p.Status != packages.PlanStatusPending {
		return fmt.Errorf("plan %q is not in pending status (current: %s)", id, p.Status)
	}
	p.Status = packages.PlanStatusRejected
	p.RejectedBy = rejector
	p.RejectionComment = comment

	NotifyPTEWebhook(PlanEvent{
		Event:     "plan_rejected",
		PlanID:    p.ID,
		Workload:  p.Spec.Name,
		Actor:     rejector,
		Reason:    fmt.Sprintf("Rejected: %s", comment),
		Spec:      p.Spec,
		Timestamp: time.Now().UTC(),
	})

	return nil
}

// pruneExpired marks plans past their TTL as expired (called before reads).
func (ps *PlanStore) pruneExpired() {
	now := time.Now().UTC()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for id, p := range ps.plans {
		if p.Status == packages.PlanStatusPending && now.After(p.ExpiresAt) {
			ps.plans[id].Status = packages.PlanStatusExpired
			NotifyPTEWebhook(PlanEvent{
				Event:     "plan_expired",
				PlanID:    p.ID,
				Workload:  p.Spec.Name,
				Actor:     p.RequestedBy,
				Reason:    "PTE plan expired (15-min TTL exceeded)",
				Spec:      p.Spec,
				Timestamp: now,
			})
		}
	}
}

// newPlanID generates a cryptographically random 12-byte hex plan ID.
func newPlanID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "plan-" + hex.EncodeToString(b), nil
}

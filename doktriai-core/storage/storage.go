package storage

import (
	"context"

	"github.com/praful224/doktriai/doktriai-packages"
)

// Storage defines the contract for persistent control plane state.
// All implementations must be thread-safe and safe for concurrent use.
type Storage interface {
	// Workloads
	ListWorkloads(ctx context.Context) ([]packages.WorkloadSpec, error)
	GetWorkload(ctx context.Context, name string) (packages.WorkloadSpec, bool, error)
	PutWorkload(ctx context.Context, spec packages.WorkloadSpec) error
	DeleteWorkload(ctx context.Context, name string) error

	// Audit Logs
	AddAudit(ctx context.Context, record packages.AuditRecord) (packages.AuditRecord, error)
	ListAudit(ctx context.Context) ([]packages.AuditRecord, error)
	GetAuditSince(ctx context.Context, sinceSeq int64) ([]packages.AuditRecord, error)

	// Environment Lock
	GetLock(ctx context.Context) (packages.LockState, error)
	AcquireLock(ctx context.Context, actor, reason string) error
	ReleaseLock(ctx context.Context) error

	// PTE Plans (Human Approval Gate)
	SubmitPlan(ctx context.Context, plan *packages.PendingPlan) error
	GetPlan(ctx context.Context, id string) (*packages.PendingPlan, error)
	ListPlans(ctx context.Context) ([]*packages.PendingPlan, error)
	UpdatePlanStatus(ctx context.Context, id string, status string, actor string, comment string) error

	// Workload History
	InsertHistory(ctx context.Context, name, specJSON, actor string) error
	GetHistory(ctx context.Context, name string, limit int) ([]packages.WorkloadHistoryEntry, error)
	GetHistoryVersion(ctx context.Context, name string, version int64) (packages.WorkloadSpec, error)
}

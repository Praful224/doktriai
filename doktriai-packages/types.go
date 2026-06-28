package packages

import (
	"context"
	"time"
)

// SecurityMode controls which validations are enforced at runtime.
type SecurityMode string

const (
	SecurityModeDev        SecurityMode = "dev"
	SecurityModeStaging    SecurityMode = "staging"
	SecurityModeProduction SecurityMode = "production"
)

// PlanStatus represents the lifecycle of a pending approval plan.
type PlanStatus string

const (
	PlanStatusPending  PlanStatus = "pending"
	PlanStatusApproved PlanStatus = "approved"
	PlanStatusRejected PlanStatus = "rejected"
	PlanStatusExpired  PlanStatus = "expired"
)

// ResourceLimits controls CPU and memory constraints for a workload container.
type ResourceLimits struct {
	CPUShares int64  `json:"cpuShares,omitempty"` // relative CPU weight (e.g. 512, 1024)
	MemoryMB  int64  `json:"memoryMb,omitempty"`  // memory limit in megabytes
	CPUPeriod int64  `json:"cpuPeriod,omitempty"` // CPU period in microseconds (default 100000)
	CPUQuota  int64  `json:"cpuQuota,omitempty"`  // CPU quota in microseconds
}

// HealthCheck defines a container readiness/liveness probe.
type HealthCheck struct {
	Command     []string `json:"command,omitempty"`     // exec probe command
	HTTPPath    string   `json:"httpPath,omitempty"`    // HTTP GET path for HTTP probe
	HTTPPort    int      `json:"httpPort,omitempty"`    // port for HTTP probe
	IntervalSec int      `json:"intervalSec,omitempty"` // seconds between checks (default 30)
	TimeoutSec  int      `json:"timeoutSec,omitempty"`  // seconds before timeout (default 5)
	Retries     int      `json:"retries,omitempty"`     // failure threshold before unhealthy (default 3)
}

// VolumeMount defines a host→container bind mount.
type VolumeMount struct {
	HostPath      string `json:"hostPath"`
	ContainerPath string `json:"containerPath"`
	ReadOnly      bool   `json:"readOnly,omitempty"`
}

// WorkloadSpec is the desired state declaration for a container workload.
type WorkloadSpec struct {
	Name          string            `json:"name" yaml:"name"`
	Image         string            `json:"image" yaml:"image"`
	Replicas      int               `json:"replicas" yaml:"replicas"`
	Port          int               `json:"port" yaml:"port"`
	ContainerPort int               `json:"containerPort" yaml:"containerPort"`
	Runtime       string            `json:"runtime" yaml:"runtime"`
	Env           map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	SecurityMode  SecurityMode      `json:"securityMode,omitempty" yaml:"securityMode,omitempty"`
	Resources     ResourceLimits    `json:"resources,omitempty" yaml:"resources,omitempty"`
	HealthCheck   *HealthCheck      `json:"healthCheck,omitempty" yaml:"healthCheck,omitempty"`
	Volumes        []VolumeMount     `json:"volumes,omitempty" yaml:"volumes,omitempty"`
	Labels         map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	DeployStrategy string            `json:"deployStrategy,omitempty" yaml:"deployStrategy,omitempty"` // "recreate" (default) or "rolling"
	MaxSurge       int               `json:"maxSurge,omitempty" yaml:"maxSurge,omitempty"`
	MaxUnavailable int               `json:"maxUnavailable,omitempty" yaml:"maxUnavailable,omitempty"`
}

// ActualWorkload is the observed container state from the runtime driver.
type ActualWorkload struct {
	Name      string `json:"name"`
	Replica   int    `json:"replica"`
	Runtime   string `json:"runtime"`
	ID        string `json:"id"`
	Status    string `json:"status"`
	StartedAt string `json:"startedAt,omitempty"`
	CPUUsage  string `json:"cpuUsage,omitempty"`
	MemUsage  string `json:"memUsage,omitempty"`
	Image     string `json:"image,omitempty"`
}

// WorkloadStatus combines desired spec with observed actual state.
type WorkloadStatus struct {
	Spec    WorkloadSpec     `json:"spec"`
	Actual  []ActualWorkload `json:"actual"`
	Healthy bool             `json:"healthy"`
	Drift   string           `json:"drift,omitempty"`
}


// Event is a real-time notification emitted to the SSE event bus.
type Event struct {
	ID       int64     `json:"id"`
	Time     time.Time `json:"time"`
	Level    string    `json:"level"`
	Source   string    `json:"source"`
	Workload string    `json:"workload,omitempty"`
	Message  string    `json:"message"`
}

// AuditRecord is an immutable, append-only log of every control-plane action.
// Includes agentic identity fields (ASI03) and state-diff hashes (ASI06/ASI10).
type AuditRecord struct {
	// Core fields
	ID        int64     `json:"id"`
	SeqID     int64     `json:"seqId"` // monotonic sequence for incremental reads
	Time      time.Time `json:"time"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Workload  string    `json:"workload,omitempty"`
	Allowed   bool      `json:"allowed"`
	Reason    string    `json:"reason,omitempty"`
	RequestID string    `json:"requestId,omitempty"`

	// Agentic identity fields (Layer 0 — ASI03, ASI07)
	AgentID          string `json:"agentId,omitempty"`
	AgentScope       string `json:"agentScope,omitempty"`
	AgentGoal        string `json:"agentGoal,omitempty"`
	SignatureVerified bool   `json:"signatureVerified"`

	// PTE plan fields (Layer 2 — ASI09)
	PlanID       string `json:"planId,omitempty"`
	PlanApproved bool   `json:"planApproved"`
	ApprovedBy   string `json:"approvedBy,omitempty"`

	// State diff hashes (Layer 3 — ASI06, ASI10)
	StateHashBefore string `json:"stateHashBefore,omitempty"`
	StateHashAfter  string `json:"stateHashAfter,omitempty"`
}

// WorkloadHistoryEntry represents a single timestamped version of a workload spec.
type WorkloadHistoryEntry struct {
	ID        int64        `json:"id"`
	Spec      WorkloadSpec `json:"spec"`
	AppliedBy string       `json:"appliedBy"`
	AppliedAt time.Time    `json:"appliedAt"`
}

// PendingPlan is a high-risk workload change waiting for human approval (PTE Gate).
type PendingPlan struct {
	ID               string       `json:"id"`
	RequestedBy      string       `json:"requestedBy"`
	AgentID          string       `json:"agentId,omitempty"`
	AgentGoal        string       `json:"agentGoal,omitempty"`
	Spec             WorkloadSpec `json:"spec"`
	ApprovalReason   string       `json:"approvalReason"` // why human approval is required
	Status           PlanStatus   `json:"status"`
	CreatedAt        time.Time    `json:"createdAt"`
	ExpiresAt        time.Time    `json:"expiresAt"`
	ApprovedBy       string       `json:"approvedBy,omitempty"`
	ApprovedAt       *time.Time   `json:"approvedAt,omitempty"`
	RejectedBy       string       `json:"rejectedBy,omitempty"`
	RejectionComment string       `json:"rejectionComment,omitempty"`
}

// BehaviorMetric tracks per-actor rolling action counts for anomaly detection (Layer 3 — ASI10).
type BehaviorMetric struct {
	Actor         string    `json:"actor"`
	DeployCount   int64     `json:"deployCount"`
	DeleteCount   int64     `json:"deleteCount"`
	RejectCount   int64     `json:"rejectCount"`
	LastSeen      time.Time `json:"lastSeen"`
	AnomalyScore  float64   `json:"anomalyScore"`  // 0.0 = normal, >1.0 = anomalous
	Flagged       bool      `json:"flagged"`
}

// LockState represents an environment lock preventing concurrent AI agent writes.
type LockState struct {
	Locked     bool      `json:"locked"`
	AcquiredBy string    `json:"acquiredBy,omitempty"`
	Time       time.Time `json:"time,omitempty"`
	Reason     string    `json:"reason,omitempty"`
}

// RuntimeDriver is the interface all container runtime backends must implement.
type RuntimeDriver interface {
	Name() string
	List(ctx context.Context) ([]ActualWorkload, error)
	Apply(ctx context.Context, spec WorkloadSpec, replica int) error
	Delete(ctx context.Context, workload string, replica int) error
	DeleteWorkload(ctx context.Context, workload string) error
	Logs(ctx context.Context, workload string, tail int) ([]string, error)
}


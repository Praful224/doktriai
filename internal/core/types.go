package core

import (
	"context"
	"time"
)

type WorkloadSpec struct {
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	Replicas      int               `json:"replicas"`
	Port          int               `json:"port"`
	ContainerPort int               `json:"containerPort"`
	Runtime       string            `json:"runtime"`
	Env           map[string]string `json:"env,omitempty"`
}

type ActualWorkload struct {
	Name    string `json:"name"`
	Replica int    `json:"replica"`
	Runtime string `json:"runtime"`
	ID      string `json:"id"`
	Status  string `json:"status"`
}

type WorkloadStatus struct {
	Spec    WorkloadSpec     `json:"spec"`
	Actual  []ActualWorkload `json:"actual"`
	Healthy bool             `json:"healthy"`
	Drift   string           `json:"drift,omitempty"`
}

type Event struct {
	ID       int64     `json:"id"`
	Time     time.Time `json:"time"`
	Level    string    `json:"level"`
	Source   string    `json:"source"`
	Workload string    `json:"workload,omitempty"`
	Message  string    `json:"message"`
}

type AuditRecord struct {
	ID        int64     `json:"id"`
	Time      time.Time `json:"time"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Workload  string    `json:"workload,omitempty"`
	Allowed   bool      `json:"allowed"`
	Reason    string    `json:"reason,omitempty"`
	RequestID string    `json:"requestId,omitempty"`
}

type RuntimeDriver interface {
	Name() string
	List(ctx context.Context) ([]ActualWorkload, error)
	Apply(ctx context.Context, spec WorkloadSpec, replica int) error
	Delete(ctx context.Context, workload string, replica int) error
	DeleteWorkload(ctx context.Context, workload string) error
	Logs(ctx context.Context, workload string, tail int) ([]string, error)
}

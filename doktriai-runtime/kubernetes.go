package runtime

import (
	"context"
	"fmt"

	"github.com/praful224/doktriai/doktriai-packages"
)

type K8sDriver struct {
	Namespace string
}

func NewK8sDriver(ns string) *K8sDriver {
	return &K8sDriver{Namespace: ns}
}

func (k *K8sDriver) Name() string {
	return "kubernetes"
}

func (k *K8sDriver) List(ctx context.Context) ([]packages.ActualWorkload, error) {
	// Simple in-memory stub/mock listing
	return []packages.ActualWorkload{}, nil
}

func (k *K8sDriver) Apply(ctx context.Context, spec packages.WorkloadSpec, replica int) error {
	fmt.Printf("[K8S-DRIVER] Mock deploy: spec %s (replica %d) in ns %s\n", spec.Name, replica, k.Namespace)
	return nil
}

func (k *K8sDriver) Delete(ctx context.Context, workload string, replica int) error {
	fmt.Printf("[K8S-DRIVER] Mock delete: workload %s (replica %d)\n", workload, replica)
	return nil
}

func (k *K8sDriver) DeleteWorkload(ctx context.Context, workload string) error {
	fmt.Printf("[K8S-DRIVER] Mock delete workload %s\n", workload)
	return nil
}

func (k *K8sDriver) Logs(ctx context.Context, workload string, tail int) ([]string, error) {
	return []string{fmt.Sprintf("[k8s-mock-logs] Awaiting logs for workload %s", workload)}, nil
}

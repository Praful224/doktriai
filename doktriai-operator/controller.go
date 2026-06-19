package operator

import (
	"context"
	"fmt"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

type DoktriaiAppController struct {
	Namespace string
	SyncEvery time.Duration
}

func NewDoktriaiAppController(ns string, sync time.Duration) *DoktriaiAppController {
	return &DoktriaiAppController{
		Namespace: ns,
		SyncEvery: sync,
	}
}

func (c *DoktriaiAppController) ReconcileCRD(ctx context.Context, crdName string, spec packages.WorkloadSpec) error {
	fmt.Printf("[DOKTRIAI-OPERATOR] Reconciling custom resource definition: %s (Target Image: %s)\n", crdName, spec.Image)
	// Simulate K8s reconciliation: check desired state vs actual state
	time.Sleep(100 * time.Millisecond)
	fmt.Printf("[DOKTRIAI-OPERATOR] Sync complete: custom resource %s converged successfully.\n", crdName)
	return nil
}

func (c *DoktriaiAppController) StartWatchLoop(ctx context.Context) {
	ticker := time.NewTicker(c.SyncEvery)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Simulated watch trigger: scan cluster workloads namespaces
				fmt.Printf("[DOKTRIAI-OPERATOR] GitOps watch tick: scanning CRDs in namespace %q\n", c.Namespace)
			}
		}
	}()
}

package operator

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

// DoktriaiAppController watches for DoktriaiApp CRDs and reconciles them
// against the doktriai-api using kubectl. Falls back to simulation if kubectl
// is not available.
type DoktriaiAppController struct {
	Namespace   string
	SyncEvery   time.Duration
	APIURL      string // doktriai-api base URL for notifications
	binary      string
	simulated   bool
}

func NewDoktriaiAppController(ns string, sync time.Duration) *DoktriaiAppController {
	c := &DoktriaiAppController{
		Namespace: ns,
		SyncEvery: sync,
		binary:    "kubectl",
		APIURL:    "http://localhost:18080",
	}
	// Probe kubectl availability
	if err := exec.Command(c.binary, "version", "--client").Run(); err != nil {
		c.simulated = true
	}
	return c
}

// crdManifest template generates a DoktriaiApp custom resource manifest.
var crdManifest = template.Must(template.New("doktriapp").Parse(`apiVersion: doktriai.io/v1alpha1
kind: DoktriaiApp
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    io.doktri.managed: "true"
spec:
  image: {{.Image}}
  replicas: {{.Replicas}}
  {{- if .Port}}
  port: {{.Port}}
  {{- end}}
  {{- if .Runtime}}
  runtime: {{.Runtime}}
  {{- end}}
`))

// ApplyCRD creates or updates a DoktriaiApp custom resource in the cluster.
func (c *DoktriaiAppController) ApplyCRD(ctx context.Context, spec packages.WorkloadSpec) error {
	if c.simulated {
		fmt.Printf("[DOKTRIAI-OPERATOR] Simulated CRD apply: %s (image: %s, replicas: %d)\n",
			spec.Name, spec.Image, spec.Replicas)
		return nil
	}
	type tmpl struct {
		packages.WorkloadSpec
		Namespace string
	}
	var buf bytes.Buffer
	if err := crdManifest.Execute(&buf, tmpl{WorkloadSpec: spec, Namespace: c.Namespace}); err != nil {
		return fmt.Errorf("operator manifest generation failed: %w", err)
	}
	cmd := exec.CommandContext(ctx, c.binary, "apply", "-f", "-")
	cmd.Stdin = &buf
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply CRD failed: %s", strings.TrimSpace(string(out)))
	}
	fmt.Printf("[DOKTRIAI-OPERATOR] CRD applied: %s → %s\n", spec.Name, strings.TrimSpace(string(out)))
	return nil
}

// ReconcileCRD reconciles a specific CRD by name — checks actual vs desired state.
func (c *DoktriaiAppController) ReconcileCRD(ctx context.Context, crdName string, spec packages.WorkloadSpec) error {
	if c.simulated {
		fmt.Printf("[DOKTRIAI-OPERATOR] Simulated reconcile: CRD %s (target image: %s)\n", crdName, spec.Image)
		return nil
	}
	// Check if the CRD resource exists
	out, err := exec.CommandContext(ctx, c.binary,
		"get", "doktriaiapp", crdName,
		"-n", c.Namespace,
		"--ignore-not-found",
		"-o", "jsonpath={.spec.replicas}",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl get CRD failed: %s", string(out))
	}
	currentReplicas := strings.TrimSpace(string(out))
	desiredReplicas := fmt.Sprintf("%d", spec.Replicas)
	if currentReplicas != desiredReplicas {
		fmt.Printf("[DOKTRIAI-OPERATOR] Drift detected on %s: replicas %s→%s — applying\n",
			crdName, currentReplicas, desiredReplicas)
		return c.ApplyCRD(ctx, spec)
	}
	fmt.Printf("[DOKTRIAI-OPERATOR] %s converged (replicas=%s)\n", crdName, desiredReplicas)
	return nil
}

// DeleteCRD removes a DoktriaiApp CRD from the cluster.
func (c *DoktriaiAppController) DeleteCRD(ctx context.Context, name string) error {
	if c.simulated {
		fmt.Printf("[DOKTRIAI-OPERATOR] Simulated CRD delete: %s\n", name)
		return nil
	}
	out, err := exec.CommandContext(ctx, c.binary,
		"delete", "doktriaiapp", name,
		"-n", c.Namespace,
		"--ignore-not-found",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl delete CRD failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// ListCRDs returns all DoktriaiApp CRDs in the namespace.
func (c *DoktriaiAppController) ListCRDs(ctx context.Context) ([]string, error) {
	if c.simulated {
		return []string{"[simulated] no kubectl available"}, nil
	}
	out, err := exec.CommandContext(ctx, c.binary,
		"get", "doktriaiapp",
		"-n", c.Namespace,
		"--no-headers",
		"-o", "custom-columns=NAME:.metadata.name",
	).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl list CRDs failed: %s", strings.TrimSpace(string(out)))
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

// StartWatchLoop starts a periodic GitOps-style reconciliation loop.
// On each tick it lists all DoktriaiApp CRDs and emits drift alerts.
func (c *DoktriaiAppController) StartWatchLoop(ctx context.Context) {
	ticker := time.NewTicker(c.SyncEvery)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if c.simulated {
					fmt.Printf("[DOKTRIAI-OPERATOR] GitOps watch tick (simulated): scanning CRDs in namespace %q\n", c.Namespace)
					continue
				}
				crds, err := c.ListCRDs(ctx)
				if err != nil {
					fmt.Printf("[DOKTRIAI-OPERATOR] watch error: %v\n", err)
					continue
				}
				fmt.Printf("[DOKTRIAI-OPERATOR] GitOps watch tick: found %d CRDs in namespace %q: %v\n",
					len(crds), c.Namespace, crds)
			}
		}
	}()
}

// IsSimulated returns true if kubectl is not available and the operator is running in simulation mode.
func (c *DoktriaiAppController) IsSimulated() bool {
	return c.simulated
}

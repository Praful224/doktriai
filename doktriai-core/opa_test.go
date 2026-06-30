package core

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/praful224/doktriai/doktriai-packages"
)

func TestEvaluateOPAPolicy(t *testing.T) {
	// Write a mock policy file for testing
	regoContent := `
package doktriai.authz

default allow = false
default requires_approval = false
default reason = ""

allow {
	input.role == "admin"
	input.spec.replicas <= 10
}

requires_approval {
	input.spec.replicas > 5
}
reason = "replicas exceed 5" {
	input.spec.replicas > 5
}
`
	tempFile, err := os.CreateTemp("", "policy_test_*.rego")
	if err != nil {
		t.Fatalf("failed to create temp rego file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	if _, err := tempFile.Write([]byte(regoContent)); err != nil {
		t.Fatalf("failed to write temp rego content: %v", err)
	}
	tempFile.Close()

	// Update policy configuration
	policy := GetPolicy()
	originalUse := policy.Security.UseOPA
	originalPath := policy.Security.OPAPolicyPath

	policy.Security.UseOPA = true
	policy.Security.OPAPolicyPath = tempFile.Name()

	defer func() {
		policy.Security.UseOPA = originalUse
		policy.Security.OPAPolicyPath = originalPath
	}()

	ctx := context.Background()

	// Test 1: Admin within limits -> allow = true, requires_approval = false
	spec1 := packages.WorkloadSpec{
		Name:     "app-1",
		Image:    "nginx",
		Replicas: 3,
	}
	allow, approval, _, err := EvaluateOPAPolicy(ctx, "admin-user", "admin", "apply", spec1)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if !allow {
		t.Error("expected allow to be true for admin with replicas 3")
	}
	if approval {
		t.Error("expected requires_approval to be false")
	}

	// Test 2: Admin exceeding pte threshold (6 replicas) -> allow = true, requires_approval = true
	spec2 := packages.WorkloadSpec{
		Name:     "app-2",
		Image:    "nginx",
		Replicas: 6,
	}
	allow, approval, reason, err := EvaluateOPAPolicy(ctx, "admin-user", "admin", "apply", spec2)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if !allow {
		t.Error("expected admin to be allowed")
	}
	if !approval {
		t.Error("expected requires_approval to be true for replicas > 5")
	}
	if !strings.Contains(reason, "replicas exceed 5") {
		t.Errorf("expected reason 'replicas exceed 5', got: %q", reason)
	}

	// Test 3: Operator (not admin) -> allow = false
	allow, _, _, err = EvaluateOPAPolicy(ctx, "op-user", "operator", "apply", spec1)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if allow {
		t.Error("expected operator to be denied based on mock policy rules")
	}
}

func TestProductionPolicy(t *testing.T) {
	// Set config to use local policy.rego
	policy := GetPolicy()
	originalUse := policy.Security.UseOPA
	originalPath := policy.Security.OPAPolicyPath

	policy.Security.UseOPA = true
	policy.Security.OPAPolicyPath = "../policy.rego" // Relative to core package directory

	defer func() {
		policy.Security.UseOPA = originalUse
		policy.Security.OPAPolicyPath = originalPath
	}()

	ctx := context.Background()

	// Test 1: Admin requesting port without runsc -> should be rejected
	spec1 := packages.WorkloadSpec{
		Name:         "unsecured-port",
		Image:        "nginx:alpine",
		Replicas:     2,
		Port:         80,
		RuntimeClass: "",
	}
	allow, _, reason, err := EvaluateOPAPolicy(ctx, "admin-user", "admin", "apply", spec1)
	if err != nil {
		t.Fatalf("failed to evaluate policy: %v", err)
	}
	if allow {
		t.Error("expected workload with port and empty runtimeClass to be rejected")
	}
	if !strings.Contains(reason, "must declare runtimeClass = 'runsc'") {
		t.Errorf("expected gvisor sandbox requirement message, got: %q", reason)
	}

	// Test 2: Admin requesting port WITH runsc -> should be allowed
	spec2 := packages.WorkloadSpec{
		Name:         "secured-port",
		Image:        "nginx:alpine",
		Replicas:     2,
		Port:         80,
		RuntimeClass: "runsc",
	}
	allow, _, _, err = EvaluateOPAPolicy(ctx, "admin-user", "admin", "apply", spec2)
	if err != nil {
		t.Fatalf("failed to evaluate policy: %v", err)
	}
	if !allow {
		t.Error("expected workload with port and runtimeClass='runsc' to be allowed")
	}

	// Test 3: Admin requesting hostPath volume mount -> should be blocked entirely
	spec3 := packages.WorkloadSpec{
		Name:     "escaped-container",
		Image:    "nginx:alpine",
		Replicas: 1,
		Volumes: []packages.VolumeMount{
			{
				HostPath:      "/var/run/docker.sock",
				ContainerPath: "/var/run/docker.sock",
			},
		},
	}
	allow, _, reason, err = EvaluateOPAPolicy(ctx, "admin-user", "admin", "apply", spec3)
	if err != nil {
		t.Fatalf("failed to evaluate policy: %v", err)
	}
	if allow {
		t.Error("expected host path volume mount to be rejected")
	}
	if !strings.Contains(reason, "prohibited for security containment") {
		t.Errorf("expected host path prohibited message, got: %q", reason)
	}
}

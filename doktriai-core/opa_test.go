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

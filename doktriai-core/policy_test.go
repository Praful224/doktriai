package core

import (
	"strings"
	"testing"

	"github.com/praful224/doktriai/doktriai-packages"
)

// ─── NormalizeSpec ────────────────────────────────────────────────────────────

func TestNormalizeSpec_Defaults(t *testing.T) {
	spec := NormalizeSpec(packages.WorkloadSpec{
		Name:  " My-App ",
		Image: " nginx:alpine ",
	})
	if spec.Name != "my-app" {
		t.Errorf("expected lowercase trimmed name, got %q", spec.Name)
	}
	if spec.Image != "nginx:alpine" {
		t.Errorf("expected trimmed image, got %q", spec.Image)
	}
	if spec.Replicas != 1 {
		t.Errorf("expected default replicas 1, got %d", spec.Replicas)
	}
	if spec.Runtime != "docker" {
		t.Errorf("expected default runtime 'docker', got %q", spec.Runtime)
	}
	if spec.SecurityMode != packages.SecurityModeDev {
		t.Errorf("expected default security mode 'dev', got %q", spec.SecurityMode)
	}
	if spec.Env == nil {
		t.Error("expected non-nil Env map")
	}
}

func TestNormalizeSpec_PreservesExplicitValues(t *testing.T) {
	spec := NormalizeSpec(packages.WorkloadSpec{
		Name:         "hello-web",
		Image:        "redis:7",
		Replicas:     3,
		Port:         8080,
		Runtime:      "kubernetes",
		SecurityMode: packages.SecurityModeProduction,
		Env:          map[string]string{"DB_HOST": "localhost"},
	})
	if spec.Replicas != 3 {
		t.Errorf("expected 3 replicas, got %d", spec.Replicas)
	}
	if spec.Runtime != "kubernetes" {
		t.Errorf("expected kubernetes runtime, got %q", spec.Runtime)
	}
	if spec.ContainerPort != 8080 {
		t.Errorf("expected containerPort to default to port, got %d", spec.ContainerPort)
	}
}

func TestNormalizeSpec_ZeroReplicas(t *testing.T) {
	spec := NormalizeSpec(packages.WorkloadSpec{
		Name: "test", Image: "nginx:alpine", Replicas: 0,
	})
	if spec.Replicas != 1 {
		t.Errorf("expected replicas clamped to 1, got %d", spec.Replicas)
	}
}

func TestNormalizeSpec_NegativeReplicas(t *testing.T) {
	spec := NormalizeSpec(packages.WorkloadSpec{
		Name: "test", Image: "nginx:alpine", Replicas: -5,
	})
	if spec.Replicas != 1 {
		t.Errorf("expected negative replicas clamped to 1, got %d", spec.Replicas)
	}
}

// ─── ValidateSpec ─────────────────────────────────────────────────────────────

func TestValidateSpec_ValidSpecs(t *testing.T) {
	validSpecs := []packages.WorkloadSpec{
		{Name: "hello-web", Image: "nginx:alpine", Replicas: 1, Runtime: "docker"},
		{Name: "my-app-123", Image: "redis:7", Replicas: 5, Runtime: "docker"},
		{Name: "a1b2c3d4e5", Image: "postgres:16", Replicas: 50, Runtime: "kubernetes"},
		{Name: "ab-cd-ef-gh", Image: "node:20-alpine", Replicas: 1, Port: 8080, ContainerPort: 80, Runtime: "k8s"},
	}
	for _, spec := range validSpecs {
		if err := ValidateSpec(spec); err != nil {
			t.Errorf("expected valid spec %q, got error: %v", spec.Name, err)
		}
	}
}

func TestValidateSpec_InvalidNames(t *testing.T) {
	invalidNames := []string{
		"",                // empty
		"A",               // too short (min 3 chars required by regex)
		"-start-dash",     // starts with dash
		"end-dash-",       // ends with dash
		"has space",       // has space
		"UPPERCASE",       // uppercase
		"special!chars",   // special chars
		"a",               // too short
		"evil;rm -rf /",   // shell injection
		"my_underscore",   // underscores not allowed
	}
	for _, name := range invalidNames {
		spec := packages.WorkloadSpec{Name: name, Image: "nginx:alpine", Replicas: 1, Runtime: "docker"}
		if err := ValidateSpec(spec); err == nil {
			t.Errorf("expected error for name %q, but got nil", name)
		}
	}
}

func TestValidateSpec_InvalidImages(t *testing.T) {
	invalidImages := []struct {
		image  string
		reason string
	}{
		{"", "empty image"},
		{"nginx;echo hacked", "shell metachar semicolon"},
		{"nginx|cat /etc/passwd", "shell metachar pipe"},
		{"nginx&& rm -rf /", "shell metachar ampersand"},
		{"nginx`whoami`", "shell metachar backtick"},
		{"nginx$(id)", "shell metachar dollar"},
	}
	for _, tc := range invalidImages {
		spec := packages.WorkloadSpec{Name: "test-app", Image: tc.image, Replicas: 1, Runtime: "docker"}
		if err := ValidateSpec(spec); err == nil {
			t.Errorf("expected error for image %q (%s), but got nil", tc.image, tc.reason)
		}
	}
}

func TestValidateSpec_ReplicaBounds(t *testing.T) {
	tests := []struct {
		replicas int
		valid    bool
	}{
		{0, false},
		{1, true},
		{25, true},
		{50, true},
		{51, false},
		{-1, false},
		{100, false},
	}
	for _, tc := range tests {
		spec := packages.WorkloadSpec{Name: "test-app", Image: "nginx:alpine", Replicas: tc.replicas, Runtime: "docker"}
		err := ValidateSpec(spec)
		if tc.valid && err != nil {
			t.Errorf("replicas=%d expected valid, got: %v", tc.replicas, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("replicas=%d expected error, got nil", tc.replicas)
		}
	}
}

func TestValidateSpec_PortBounds(t *testing.T) {
	tests := []struct {
		port  int
		valid bool
	}{
		{0, true},
		{80, true},
		{8080, true},
		{65535, true},
		{-1, false},
		{65536, false},
	}
	for _, tc := range tests {
		spec := packages.WorkloadSpec{Name: "test-app", Image: "nginx:alpine", Replicas: 1, Port: tc.port, Runtime: "docker"}
		err := ValidateSpec(spec)
		if tc.valid && err != nil {
			t.Errorf("port=%d expected valid, got: %v", tc.port, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("port=%d expected error, got nil", tc.port)
		}
	}
}

func TestValidateSpec_InvalidRuntime(t *testing.T) {
	spec := packages.WorkloadSpec{Name: "test-app", Image: "nginx:alpine", Replicas: 1, Runtime: "podman"}
	if err := ValidateSpec(spec); err == nil {
		t.Error("expected error for unsupported runtime 'podman'")
	}
}

func TestValidateSpec_ValidRuntimes(t *testing.T) {
	for _, rt := range []string{"docker", "kubernetes", "k8s"} {
		spec := packages.WorkloadSpec{Name: "test-app", Image: "nginx:alpine", Replicas: 1, Runtime: rt}
		if err := ValidateSpec(spec); err != nil {
			t.Errorf("expected valid runtime %q, got: %v", rt, err)
		}
	}
}

func TestValidateSpec_EnvKeyValidation(t *testing.T) {
	tests := []struct {
		key   string
		valid bool
	}{
		{"DB_HOST", true},
		{"PORT", true},
		{"A", true},
		{"MY_VAR_123", true},
		{"", false},
		{"1_STARTS_NUM", false},
		{"lowercase", false},
		{"has-dash", false},
		{"has space", false},
	}
	for _, tc := range tests {
		spec := packages.WorkloadSpec{
			Name: "test-app", Image: "nginx:alpine", Replicas: 1, Runtime: "docker",
			Env: map[string]string{tc.key: "value"},
		}
		err := ValidateSpec(spec)
		if tc.valid && err != nil {
			t.Errorf("env key %q expected valid, got: %v", tc.key, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("env key %q expected error, got nil", tc.key)
		}
	}
}

func TestValidateSpec_EnvValueMetachars(t *testing.T) {
	badValues := []string{
		"value\x00null",
		"value\rnewline",
		"value\nnewline",
	}
	for _, v := range badValues {
		spec := packages.WorkloadSpec{
			Name: "test-app", Image: "nginx:alpine", Replicas: 1, Runtime: "docker",
			Env: map[string]string{"KEY": v},
		}
		if err := ValidateSpec(spec); err == nil {
			t.Errorf("expected error for env value with control chars, got nil")
		}
	}
}

func TestValidateSpec_ResourceLimitsValidation(t *testing.T) {
	spec := packages.WorkloadSpec{
		Name: "test-app", Image: "nginx:alpine", Replicas: 1, Runtime: "docker",
		Resources: packages.ResourceLimits{MemoryMB: -100},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Error("expected error for negative memoryMb")
	}

	spec.Resources = packages.ResourceLimits{CPUShares: -50}
	if err := ValidateSpec(spec); err == nil {
		t.Error("expected error for negative cpuShares")
	}
}

func TestValidateSpec_VolumeMountValidation(t *testing.T) {
	// Missing paths
	spec := packages.WorkloadSpec{
		Name: "test-app", Image: "nginx:alpine", Replicas: 1, Runtime: "docker",
		Volumes: []packages.VolumeMount{{HostPath: "", ContainerPath: "/data"}},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Error("expected error for empty hostPath")
	}

	// Shell metachar in path
	spec.Volumes = []packages.VolumeMount{{HostPath: "/tmp;rm -rf /", ContainerPath: "/data"}}
	if err := ValidateSpec(spec); err == nil {
		t.Error("expected error for shell metachar in volume path")
	}
}

// ─── CheckAgentIntent ─────────────────────────────────────────────────────────

func TestCheckAgentIntent_ApprovedRegistries(t *testing.T) {
	approved := []string{
		"nginx:alpine", "nginx:latest", "nginx@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		"redis:7", "node:20", "mysql:8", "postgres:16",
		"doktri/my-app:v1", "doktriai/agent:latest",
	}
	for _, img := range approved {
		spec := packages.WorkloadSpec{Name: "test-app", Image: img, Replicas: 1, Runtime: "docker"}
		if err := CheckAgentIntent(spec); err != nil {
			t.Errorf("expected approved image %q, got: %v", img, err)
		}
	}
}

func TestCheckAgentIntent_BlockedRegistries(t *testing.T) {
	blocked := []string{
		"malicious.io/exploit:latest",
		"evil-corp/backdoor:v1",
		"alpine:latest",
		"ubuntu:22.04",
		"busybox:latest",
		"python:3.11",
	}
	for _, img := range blocked {
		spec := packages.WorkloadSpec{Name: "test-app", Image: img, Replicas: 1, Runtime: "docker"}
		if err := CheckAgentIntent(spec); err == nil {
			t.Errorf("expected blocked image %q, got nil", img)
		}
	}
}

func TestCheckAgentIntent_ProductionRequiresDigestPin(t *testing.T) {
	// Without digest — should fail in production
	spec := packages.WorkloadSpec{
		Name: "test-app", Image: "nginx:alpine", Replicas: 1, Runtime: "docker",
		SecurityMode: packages.SecurityModeProduction,
	}
	if err := CheckAgentIntent(spec); err == nil {
		t.Error("expected error for production mode without digest pin")
	}

	// With digest — should pass
	spec.Image = "nginx@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	if err := CheckAgentIntent(spec); err != nil {
		t.Errorf("expected valid production image with digest, got: %v", err)
	}
}

func TestCheckAgentIntent_DevModeNoDigestRequired(t *testing.T) {
	spec := packages.WorkloadSpec{
		Name: "test-app", Image: "nginx:alpine", Replicas: 1, Runtime: "docker",
		SecurityMode: packages.SecurityModeDev,
	}
	if err := CheckAgentIntent(spec); err != nil {
		t.Errorf("expected dev mode to not require digest, got: %v", err)
	}
}

func TestCheckAgentIntent_ShellInjectionInEnv(t *testing.T) {
	attacks := []string{
		"value;echo hacked",
		"value|cat /etc/passwd",
		"value&& rm -rf /",
		"value`id`",
		"$(whoami)",
	}
	for _, v := range attacks {
		spec := packages.WorkloadSpec{
			Name: "test-app", Image: "nginx:alpine", Replicas: 1, Runtime: "docker",
			Env: map[string]string{"MY_VAR": v},
		}
		if err := CheckAgentIntent(spec); err == nil {
			t.Errorf("expected shell injection blocked for env value %q, got nil", v)
		}
	}
}

func TestCheckAgentIntent_Base64EncodedInjection(t *testing.T) {
	// "echo hacked;rm -rf /" base64 encoded = "ZWNobyBoYWNrZWQ7cm0gLXJmIC8="
	spec := packages.WorkloadSpec{
		Name: "test-app", Image: "nginx:alpine", Replicas: 1, Runtime: "docker",
		Env: map[string]string{"PAYLOAD": "ZWNobyBoYWNrZWQ7cm0gLXJmIC8="},
	}
	if err := CheckAgentIntent(spec); err == nil {
		t.Error("expected base64-encoded shell injection detected")
	}
}

func TestCheckAgentIntent_LargeEnvValue(t *testing.T) {
	spec := packages.WorkloadSpec{
		Name: "test-app", Image: "nginx:alpine", Replicas: 1, Runtime: "docker",
		Env: map[string]string{"HUGE": strings.Repeat("a", 5000)},
	}
	if err := CheckAgentIntent(spec); err == nil {
		t.Error("expected error for env value exceeding 4096 chars")
	}
}

// ─── RequiresHumanApproval ────────────────────────────────────────────────────

func TestRequiresHumanApproval_HighReplicas(t *testing.T) {
	spec := packages.WorkloadSpec{Name: "test", Replicas: 10}
	needs, reason := RequiresHumanApproval(spec)
	if !needs {
		t.Error("expected PTE gate for 10 replicas")
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestRequiresHumanApproval_LowReplicas(t *testing.T) {
	spec := packages.WorkloadSpec{Name: "test", Replicas: 3}
	needs, _ := RequiresHumanApproval(spec)
	if needs {
		t.Error("expected no PTE gate for 3 replicas")
	}
}

func TestRequiresHumanApproval_ThresholdBoundary(t *testing.T) {
	// Exactly 5 should NOT require approval
	spec := packages.WorkloadSpec{Name: "test", Replicas: 5}
	needs, _ := RequiresHumanApproval(spec)
	if needs {
		t.Error("expected no PTE gate for exactly 5 replicas")
	}

	// 6 should require
	spec.Replicas = 6
	needs, _ = RequiresHumanApproval(spec)
	if !needs {
		t.Error("expected PTE gate for 6 replicas")
	}
}

func TestRequiresHumanApproval_SensitiveEnvKeys(t *testing.T) {
	sensitiveKeys := []string{
		"DB_SECRET", "API_KEY", "AUTH_TOKEN", "ADMIN_PASSWORD",
		"MY_PASSWD", "AWS_CREDENTIAL", "SSH_PRIVATE_KEY",
	}
	for _, key := range sensitiveKeys {
		spec := packages.WorkloadSpec{
			Name: "test", Replicas: 1,
			Env: map[string]string{key: "some-value"},
		}
		needs, _ := RequiresHumanApproval(spec)
		if !needs {
			t.Errorf("expected PTE gate for sensitive env key %q", key)
		}
	}
}

func TestRequiresHumanApproval_SafeEnvKeys(t *testing.T) {
	safeKeys := []string{"DB_HOST", "PORT", "NODE_ENV", "LOG_LEVEL"}
	for _, key := range safeKeys {
		spec := packages.WorkloadSpec{
			Name: "test", Replicas: 1,
			Env: map[string]string{key: "value"},
		}
		needs, _ := RequiresHumanApproval(spec)
		if needs {
			t.Errorf("expected no PTE gate for safe env key %q", key)
		}
	}
}

func TestRequiresDeleteApproval_AlwaysRequired(t *testing.T) {
	needs, reason := RequiresDeleteApproval("any-workload")
	if !needs {
		t.Error("expected delete approval always required")
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

// ─── RoleCan (RBAC) ──────────────────────────────────────────────────────────

func TestRoleCan_Admin(t *testing.T) {
	actions := []string{"read", "apply", "reconcile", "delete"}
	for _, a := range actions {
		if !RoleCan("admin", a) {
			t.Errorf("admin should be able to %s", a)
		}
	}
}

func TestRoleCan_Operator(t *testing.T) {
	allowed := map[string]bool{
		"read":      true,
		"apply":     true,
		"reconcile": true,
		"delete":    false,
	}
	for action, expected := range allowed {
		if RoleCan("operator", action) != expected {
			t.Errorf("operator %s expected=%v", action, expected)
		}
	}
}

func TestRoleCan_Viewer(t *testing.T) {
	if !RoleCan("viewer", "read") {
		t.Error("viewer should be able to read")
	}
	for _, a := range []string{"apply", "reconcile", "delete"} {
		if RoleCan("viewer", a) {
			t.Errorf("viewer should NOT be able to %s", a)
		}
	}
}

func TestRoleCan_UnknownRoleIsViewer(t *testing.T) {
	if !RoleCan("unknown-role", "read") {
		t.Error("unknown role should be able to read")
	}
	if RoleCan("unknown-role", "apply") {
		t.Error("unknown role should NOT be able to apply")
	}
}

// ─── ValidateAgentScope ───────────────────────────────────────────────────────

func TestValidateAgentScope_EmptyScopeBlocksWrites(t *testing.T) {
	if err := ValidateAgentScope("apply", ""); err == nil {
		t.Error("expected empty scope to block write actions")
	}
	if err := ValidateAgentScope("read", ""); err != nil {
		t.Errorf("expected empty scope to allow reads, got: %v", err)
	}
}

func TestValidateAgentScope_ExplicitScope(t *testing.T) {
	if err := ValidateAgentScope("deploy_workload", "list_workloads,deploy_workload"); err != nil {
		t.Errorf("expected scope match, got: %v", err)
	}
	if err := ValidateAgentScope("delete_workload", "list_workloads,deploy_workload"); err == nil {
		t.Error("expected scope mismatch for delete")
	}
}

// ─── Unicode normalization ───────────────────────────────────────────────────

func TestNormalizeString_StripZeroWidthChars(t *testing.T) {
	// Zero-width space U+200B
	input := "ngi\u200Bnx"
	result := normalizeString(input)
	if result != "nginx" {
		t.Errorf("expected zero-width stripped to 'nginx', got %q", result)
	}
}

func TestNormalizeString_NFKCNormalization(t *testing.T) {
	// Fullwidth N should normalize
	input := "\uFF2Eginx" // Ｎginx
	result := normalizeString(input)
	if result != "Nginx" {
		t.Errorf("expected NFKC normalization to 'Nginx', got %q", result)
	}
}

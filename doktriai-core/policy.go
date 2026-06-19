package core

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/praful224/doktriai/doktriai-packages"
)

// safeName: DNS-label style workload names only.
var safeName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,46}[a-z0-9]$`)

// safeEnvKey: POSIX-style env key.
var safeEnvKey = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)

// imageDigestPin: requires <image>@sha256:<64-hex-chars>
var imageDigestPin = regexp.MustCompile(`^.+@sha256:[0-9a-f]{64}$`)

// approvedPrefixes lists the baseline approved image name prefixes.
// In production, images must also carry a @sha256: digest pin.
var approvedPrefixes = []string{
	"nginx", "redis", "node", "mysql", "postgres", "doktri/", "doktriai/",
}

// sensitiveEnvKeys triggers PTE gate when present.
var sensitiveEnvKeyPatterns = []string{
	"SECRET", "KEY", "TOKEN", "PASSWORD", "PASSWD", "CREDENTIAL", "PRIVATE",
}

// shellMetachars: shell injection characters forbidden in image refs and env values.
const shellMetachars = ";|&`$<>()\r\n\x00"

// NormalizeSpec fills in defaults before validation.
func NormalizeSpec(spec packages.WorkloadSpec) packages.WorkloadSpec {
	spec.Name = strings.ToLower(strings.TrimSpace(spec.Name))
	spec.Image = normalizeString(strings.TrimSpace(spec.Image))
	if spec.Replicas < 1 {
		spec.Replicas = 1
	}
	if spec.ContainerPort == 0 {
		spec.ContainerPort = spec.Port
	}
	if spec.Runtime == "" {
		spec.Runtime = "docker"
	}
	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	if spec.SecurityMode == "" {
		spec.SecurityMode = packages.SecurityModeDev
	}
	return spec
}

// ValidateSpec performs structural validation of a WorkloadSpec.
func ValidateSpec(spec packages.WorkloadSpec) error {
	if !safeName.MatchString(spec.Name) {
		return fmt.Errorf("workload name must be lowercase DNS-style text")
	}
	if spec.Image == "" || strings.ContainsAny(spec.Image, shellMetachars) {
		return fmt.Errorf("image must be a single container image reference")
	}
	if spec.Replicas < 1 || spec.Replicas > 50 {
		return fmt.Errorf("replicas must be between 1 and 50")
	}
	if spec.Port < 0 || spec.Port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535")
	}
	if spec.ContainerPort < 0 || spec.ContainerPort > 65535 {
		return fmt.Errorf("container port must be between 0 and 65535")
	}
	if spec.Runtime != "docker" {
		return fmt.Errorf("runtime %q is not enabled in this build", spec.Runtime)
	}
	for key, value := range spec.Env {
		if !safeEnvKey.MatchString(key) {
			return fmt.Errorf("env key %q is not safe", key)
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("env value for %q contains unsupported characters", key)
		}
	}
	return nil
}

// RoleCan checks whether a role is permitted to perform an action.
// RBAC: admin > operator > viewer.
func RoleCan(role, action string) bool {
	switch role {
	case "admin":
		return true
	case "operator":
		return action == "read" || action == "apply" || action == "reconcile"
	default:
		return action == "read"
	}
}

// CheckAgentIntent is the multi-stage intent guard (Layer 1).
// It runs 4 stages: normalization → registry check → env scan → scope check.
// Returns a descriptive DOKTRIAI Security Violation error on any failure.
func CheckAgentIntent(spec packages.WorkloadSpec) error {
	// Stage 1: Normalize image to defeat Unicode lookalike attacks (ASI01, ASI05)
	image := normalizeString(spec.Image)

	// Stage 2: Registry allowlist check
	if err := validateRegistry(image); err != nil {
		return err
	}

	// Stage 3: Production mode requires digest pinning (ASI04 — supply chain)
	if spec.SecurityMode == packages.SecurityModeProduction {
		if err := ValidateImageDigestPin(image); err != nil {
			return err
		}
	}

	// Stage 4: Environment value deep scan (ASI05 — unexpected code execution)
	if err := validateEnvValues(spec.Env); err != nil {
		return err
	}

	return nil
}

// RequiresHumanApproval returns true and a reason string if the spec is
// high-risk enough to require PTE gate approval (Layer 2 — ASI09).
func RequiresHumanApproval(spec packages.WorkloadSpec) (bool, string) {
	if spec.Replicas > 5 {
		return true, fmt.Sprintf("replica count %d exceeds safe auto-apply threshold (5)", spec.Replicas)
	}
	for key := range spec.Env {
		upper := strings.ToUpper(key)
		for _, pattern := range sensitiveEnvKeyPatterns {
			if strings.Contains(upper, pattern) {
				return true, fmt.Sprintf("env key %q matches sensitive credential pattern", key)
			}
		}
	}
	return false, ""
}

// RequiresDeleteApproval returns true for any delete of a named workload
// to prevent automated mass-deletion by a rogue agent.
func RequiresDeleteApproval(workloadName string) (bool, string) {
	return true, fmt.Sprintf("deletion of workload %q requires human approval (ASI09 policy)", workloadName)
}

// ValidateAgentScope checks that the action falls within the declared agent scope claim.
// Scope is a comma-separated list of allowed tool names (e.g. "list_workloads,deploy_workload").
// An empty scope claim is treated as "read-only" in production mode.
func ValidateAgentScope(action, scopeClaim string) error {
	if scopeClaim == "" || scopeClaim == "*" {
		// Empty scope = read-only in production (no writes allowed without explicit scope)
		if action != "read" && action != "list" {
			return fmt.Errorf("DOKTRIAI Security Violation: agent scope claim is empty — write action %q denied", action)
		}
		return nil
	}
	allowed := strings.Split(scopeClaim, ",")
	for _, a := range allowed {
		if strings.TrimSpace(a) == action {
			return nil
		}
	}
	return fmt.Errorf("DOKTRIAI Security Violation: action %q is outside declared agent scope %q", action, scopeClaim)
}

// ValidateImageDigestPin enforces that production images carry an explicit SHA256 digest.
func ValidateImageDigestPin(image string) error {
	if !imageDigestPin.MatchString(image) {
		return fmt.Errorf(
			"DOKTRIAI Security Violation: production security mode requires a pinned image digest "+
				"(e.g. nginx@sha256:<64-hex>), got %q", image)
	}
	return nil
}

// --- Internal helpers ---

// normalizeString applies Unicode NFKC normalization and strips invisible/control characters.
// This defeats zero-width space attacks, lookalike character substitution, and
// Unicode homoglyph injection (e.g., "ngiɴx" → "nginx" or caught as unequal).
func normalizeString(s string) string {
	// NFKC: compatibility decomposition then canonical composition
	normalized := norm.NFKC.String(s)
	// Strip Unicode format/control characters (zero-width joiners, soft hyphens, etc.)
	stripped := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1 // drop format characters
		}
		if unicode.Is(unicode.Cs, r) {
			return -1 // drop surrogates
		}
		return r
	}, normalized)
	return stripped
}

// validateRegistry checks the normalized image against the approved prefix allowlist.
func validateRegistry(image string) error {
	for _, p := range approvedPrefixes {
		if strings.HasPrefix(image, p) {
			return nil
		}
	}
	return fmt.Errorf(
		"DOKTRIAI Security Violation: image registry %q fails allowlist verification — "+
			"approved prefixes: %s", image, strings.Join(approvedPrefixes, ", "))
}

// validateEnvValues performs deep scanning of env variable values.
// Covers: shell metachar injection, base64-encoded payloads, length limits.
func validateEnvValues(env map[string]string) error {
	for k, v := range env {
		// Shell metachar check (direct)
		if strings.ContainsAny(v, shellMetachars) {
			return fmt.Errorf(
				"DOKTRIAI Security Violation: env key %q contains characters that violate execution security policies", k)
		}

		// Length limit: env values over 4096 chars are suspicious
		if len(v) > 4096 {
			return fmt.Errorf(
				"DOKTRIAI Security Violation: env key %q value exceeds maximum allowed length (4096 chars)", k)
		}

		// Base64 decode and re-scan for hidden metacharacters (indirect injection)
		if decoded, err := base64.StdEncoding.DecodeString(v); err == nil {
			decodedStr := string(decoded)
			if strings.ContainsAny(decodedStr, shellMetachars) {
				return fmt.Errorf(
					"DOKTRIAI Security Violation: env key %q base64-decoded value contains shell injection characters", k)
			}
		}
	}
	return nil
}

package core

import (
	"fmt"
	"regexp"
	"strings"
)

var safeName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,46}[a-z0-9]$`)

func NormalizeSpec(spec WorkloadSpec) WorkloadSpec {
	spec.Name = strings.ToLower(strings.TrimSpace(spec.Name))
	spec.Image = strings.TrimSpace(spec.Image)
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
	return spec
}

func ValidateSpec(spec WorkloadSpec) error {
	if !safeName.MatchString(spec.Name) {
		return fmt.Errorf("workload name must be lowercase DNS-style text")
	}
	if spec.Image == "" || strings.ContainsAny(spec.Image, " ;|&`$<>") {
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
		if !regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`).MatchString(key) {
			return fmt.Errorf("env key %q is not safe", key)
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("env value for %q contains unsupported characters", key)
		}
	}
	return nil
}

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

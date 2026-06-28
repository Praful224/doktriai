package packages

import (
	"fmt"
	"gopkg.in/yaml.v3"
)

// DoktriManifest is the .doktri declarative manifest file schema
type DoktriManifest struct {
	APIVersion string       `yaml:"apiVersion"`
	Kind       string       `yaml:"kind"`
	Metadata   struct {
		Name   string            `yaml:"name"`
		Labels map[string]string `yaml:"labels,omitempty"`
	} `yaml:"metadata"`
	Spec WorkloadSpec `yaml:"spec"`
}

// ParseManifest parses a YAML stream into a WorkloadSpec
func ParseManifest(data []byte) (*WorkloadSpec, error) {
	var m DoktriManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse YAML manifest: %w", err)
	}
	if m.APIVersion != "doktriai/v1" {
		return nil, fmt.Errorf("unsupported apiVersion %q (expected 'doktriai/v1')", m.APIVersion)
	}
	if m.Kind != "Workload" {
		return nil, fmt.Errorf("unsupported resource kind %q (expected 'Workload')", m.Kind)
	}
	if m.Spec.Name == "" {
		m.Spec.Name = m.Metadata.Name
	}
	if m.Spec.Labels == nil {
		m.Spec.Labels = m.Metadata.Labels
	} else {
		for k, v := range m.Metadata.Labels {
			if _, exists := m.Spec.Labels[k]; !exists {
				m.Spec.Labels[k] = v
			}
		}
	}
	return &m.Spec, nil
}

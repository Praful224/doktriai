package core

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// PolicyConfig is the runtime configuration for DoktriAI security policies.
type PolicyConfig struct {
	Security struct {
		PTEReplicaThreshold          int      `yaml:"pteReplicaThreshold"`
		ApprovedImagePrefixes        []string `yaml:"approvedImagePrefixes"`
		SensitiveEnvKeyPatterns      []string `yaml:"sensitiveEnvKeyPatterns"`
		RequireDigestPinInProduction bool     `yaml:"requireDigestPinInProduction"`
		UseOPA                       bool     `yaml:"useOPA"`
		OPAPolicyPath                string   `yaml:"opaPolicyPath"`
	} `yaml:"security"`
	Notifications struct {
		PTEWebhookURL        string `yaml:"pteWebhookURL"`
		PTEWebhookTimeoutSec int    `yaml:"pteWebhookTimeoutSec"`
		DriftWebhookURL      string `yaml:"driftWebhookURL"`
		SlackWebhookURL      string `yaml:"slackWebhookURL"`
	} `yaml:"notifications"`
}

var (
	globalPolicy *PolicyConfig
	policyMu     sync.RWMutex
	// Default policy matches existing hardcoded values
	defaultPolicy = &PolicyConfig{}
)

func init() {
	defaultPolicy.Security.PTEReplicaThreshold = 5
	defaultPolicy.Security.ApprovedImagePrefixes = []string{
		"nginx", "redis", "node", "mysql", "postgres", "doktri/", "doktriai/",
	}
	defaultPolicy.Security.SensitiveEnvKeyPatterns = []string{
		"SECRET", "KEY", "TOKEN", "PASSWORD", "PASSWD", "CREDENTIAL", "PRIVATE",
	}
	defaultPolicy.Security.RequireDigestPinInProduction = true
	defaultPolicy.Security.UseOPA = false
	defaultPolicy.Security.OPAPolicyPath = "./policy.rego"
}

// LoadPolicy parses the policy YAML file. If path doesn't exist, uses defaults.
func LoadPolicy(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			policyMu.Lock()
			globalPolicy = defaultPolicy
			policyMu.Unlock()
			return nil // not an error — defaults are used
		}
		return err
	}
	var cfg PolicyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("policy file invalid: %w", err)
	}
	// Merge: if fields are empty, use defaults
	if len(cfg.Security.ApprovedImagePrefixes) == 0 {
		cfg.Security.ApprovedImagePrefixes = defaultPolicy.Security.ApprovedImagePrefixes
	}
	if cfg.Security.PTEReplicaThreshold == 0 {
		cfg.Security.PTEReplicaThreshold = defaultPolicy.Security.PTEReplicaThreshold
	}
	if cfg.Security.OPAPolicyPath == "" {
		cfg.Security.OPAPolicyPath = defaultPolicy.Security.OPAPolicyPath
	}
	policyMu.Lock()
	globalPolicy = &cfg
	policyMu.Unlock()
	return nil
}

// GetPolicy returns the active policy (thread-safe).
func GetPolicy() *PolicyConfig {
	policyMu.RLock()
	defer policyMu.RUnlock()
	if globalPolicy == nil {
		return defaultPolicy
	}
	return globalPolicy
}

package core

import (
	"context"
	"fmt"
	"os"

	"github.com/open-policy-agent/opa/rego"
	"github.com/praful224/doktriai/doktriai-packages"
)

// EvaluateOPAPolicy evaluates the Rego policy file configured in the workspace settings.
// Returns (allow, requires_approval, reason, error) based on the input context.
func EvaluateOPAPolicy(ctx context.Context, actor, role, action string, spec packages.WorkloadSpec) (bool, bool, string, error) {
	policyPath := GetPolicy().Security.OPAPolicyPath
	if policyPath == "" {
		policyPath = "./policy.rego"
	}

	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		return false, false, "", fmt.Errorf("failed to read Rego policy file %q: %w", policyPath, err)
	}

	r := rego.New(
		rego.Query("data.doktriai.authz"),
		rego.Module(policyPath, string(policyBytes)),
	)

	query, err := r.PrepareForEval(ctx)
	if err != nil {
		return false, false, "", fmt.Errorf("failed to prepare Rego query: %w", err)
	}

	// Wrap inputs into evaluation map
	input := map[string]any{
		"actor":  actor,
		"role":   role,
		"action": action,
		"spec":   spec,
	}

	results, err := query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return false, false, "", fmt.Errorf("failed to evaluate Rego query: %w", err)
	}

	if len(results) == 0 || len(results[0].Expressions) == 0 {
		return false, false, "empty evaluation results", nil
	}

	expr := results[0].Expressions[0].Value
	resultMap, ok := expr.(map[string]any)
	if !ok {
		return false, false, "invalid result format from Rego policy", nil
	}

	allow, _ := resultMap["allow"].(bool)
	requiresApproval, _ := resultMap["requires_approval"].(bool)
	reason, _ := resultMap["reason"].(string)

	return allow, requiresApproval, reason, nil
}

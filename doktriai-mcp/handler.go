package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/praful224/doktriai/doktriai-core"
	"github.com/praful224/doktriai/doktriai-packages"
)

var mcpTracer = otel.Tracer("doktriai-mcp-handler")

// RPCRequest is the standard JSON-RPC 2.0 request envelope.
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ProtocolHandler implements the MCP (Model Context Protocol) JSON-RPC bridge.
// All tool calls are routed through the same security layers as the HTTP API.
type ProtocolHandler struct {
	store   *core.Store
	engine  *core.Engine
	plans   *core.PlanStore
	tracker *core.BehaviorTracker
}

func NewProtocolHandler(store *core.Store, engine *core.Engine, plans *core.PlanStore, tracker *core.BehaviorTracker) *ProtocolHandler {
	return &ProtocolHandler{
		store:   store,
		engine:  engine,
		plans:   plans,
		tracker: tracker,
	}
}

func (h *ProtocolHandler) HandleRPC(ctx context.Context, actor string, payload []byte) (any, error) {
	var req RPCRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	switch req.Method {
	case "initialize":
		return map[string]any{
			"server":       "doktriai-mcp",
			"capabilities": []string{"tools"},
			"version":      "2.0.0",
			"security": map[string]any{
				"intentGuard":     true,
				"pteGate":         true,
				"behaviorTracking": true,
				"authMode":        map[bool]string{true: "dev", false: "production"}[core.IsDevMode()],
			},
		}, nil

	case "tools/list":
		return []map[string]any{
			{"name": "deploy_workload", "description": "Declare and reconcile a container workload (high-risk: may require PTE approval)", "requiresApproval": true},
			{"name": "list_workloads", "description": "List desired and actual workload states", "requiresApproval": false},
			{"name": "get_workload", "description": "Get a single workload by name with live actual state", "requiresApproval": false},
			{"name": "delete_workload", "description": "Delete desired state and stop containers (ALWAYS requires PTE approval)", "requiresApproval": true},
			{"name": "get_logs", "description": "Read container logs for a workload", "requiresApproval": false},
			{"name": "list_pending_plans", "description": "List plans awaiting human approval", "requiresApproval": false},
			{"name": "approve_plan", "description": "Approve a pending PTE plan by ID (executes the workload change)", "requiresApproval": false},
			{"name": "reject_plan", "description": "Reject a pending PTE plan by ID with an optional comment", "requiresApproval": false},
			{"name": "get_behavior_metrics", "description": "Retrieve per-actor behavioral anomaly metrics", "requiresApproval": false},
		}, nil

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			AgentID   string          `json:"agentId,omitempty"`
			Scope     string          `json:"scope,omitempty"`
			Goal      string          `json:"goal,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, err
		}

		// Scope validation (Layer 1 — ValidateAgentScope)
		if params.Scope != "" {
			if err := core.ValidateAgentScope(params.Name, params.Scope); err != nil {
				return nil, err
			}
		}

		return h.callTool(ctx, actor, params.AgentID, params.Scope, params.Goal, params.Name, params.Arguments)

	default:
		return nil, fmt.Errorf("unknown method %q", req.Method)
	}
}

func (h *ProtocolHandler) callTool(
	ctx context.Context,
	actor, agentID, scope, goal, name string,
	args json.RawMessage,
) (any, error) {
	ctx, span := mcpTracer.Start(ctx, fmt.Sprintf("MCP/Tool/%s", name))
	defer span.End()

	span.SetAttributes(
		attribute.String("doktri.actor", actor),
		attribute.String("doktri.agent.id", agentID),
		attribute.String("doktri.agent.scope", scope),
		attribute.String("doktri.agent.goal", goal),
		attribute.String("doktri.tool.name", name),
	)

	res, err := h.executeTool(ctx, actor, agentID, scope, goal, name, args)
	if err != nil {
		span.RecordError(err)
	}
	return res, err
}

func (h *ProtocolHandler) executeTool(
	ctx context.Context,
	actor, agentID, scope, goal, name string,
	args json.RawMessage,
) (any, error) {
	switch name {

	case "list_workloads":
		return h.engine.Status(ctx)

	case "deploy_workload":
		var spec packages.WorkloadSpec
		if err := json.Unmarshal(args, &spec); err != nil {
			return nil, err
		}

		// Environment lock check
		lockState := h.store.GetLock()
		if lockState.Locked && lockState.AcquiredBy != actor {
			return nil, fmt.Errorf("environment locked by %s: %s", lockState.AcquiredBy, lockState.Reason)
		}

		// Extract role from context
		role, _ := ctx.Value("role").(string)
		if role == "" {
			role = "operator" // default fallback
		}

		if core.GetPolicy().Security.UseOPA {
			allow, requiresApproval, reason, err := core.EvaluateOPAPolicy(ctx, actor, role, "apply", spec)
			if err != nil {
				return nil, fmt.Errorf("DOKTRIAI OPA Error: %w", err)
			}
			if !allow && !requiresApproval {
				return nil, fmt.Errorf("DOKTRIAI Security Violation: %s", reason)
			}
			if requiresApproval {
				plan, err := h.plans.Submit(actor, agentID, goal, reason, spec)
				if err != nil {
					return nil, fmt.Errorf("failed to create approval plan: %w", err)
				}
				return map[string]any{
					"status":         "pending_approval",
					"planId":         plan.ID,
					"approvalReason": reason,
					"message":        fmt.Sprintf("This operation requires human approval. Plan %s created, expires in 15 minutes.", plan.ID),
					"expiresAt":      plan.ExpiresAt,
				}, nil
			}
		} else {
			// Fallback to legacy checks
			if err := core.CheckAgentIntent(spec); err != nil {
				return nil, err
			}
			if needsApproval, reason := core.RequiresHumanApproval(spec); needsApproval {
				plan, err := h.plans.Submit(actor, agentID, goal, reason, spec)
				if err != nil {
					return nil, fmt.Errorf("failed to create approval plan: %w", err)
				}
				return map[string]any{
					"status":         "pending_approval",
					"planId":         plan.ID,
					"approvalReason": reason,
					"message":        fmt.Sprintf("This operation requires human approval. Plan %s created, expires in 15 minutes.", plan.ID),
					"expiresAt":      plan.ExpiresAt,
				}, nil
			}
		}

		return map[string]string{"status": "accepted"}, h.engine.Apply(ctx, actor, spec)

	case "delete_workload":
		var payload struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(args, &payload); err != nil {
			return nil, err
		}

		lockState := h.store.GetLock()
		if lockState.Locked && lockState.AcquiredBy != actor {
			return nil, fmt.Errorf("environment locked by %s: %s", lockState.AcquiredBy, lockState.Reason)
		}

		// Extract role from context
		role, _ := ctx.Value("role").(string)
		if role == "" {
			role = "operator"
		}

		spec := packages.WorkloadSpec{Name: payload.Name}
		if core.GetPolicy().Security.UseOPA {
			allow, requiresApproval, reason, err := core.EvaluateOPAPolicy(ctx, actor, role, "delete", spec)
			if err != nil {
				return nil, fmt.Errorf("DOKTRIAI OPA Error: %w", err)
			}
			if !allow && !requiresApproval {
				return nil, fmt.Errorf("DOKTRIAI Security Violation: %s", reason)
			}
			if requiresApproval {
				plan, err := h.plans.Submit(actor, agentID, goal, reason, spec)
				if err != nil {
					return nil, fmt.Errorf("failed to create delete approval plan: %w", err)
				}
				return map[string]any{
					"status":         "pending_approval",
					"planId":         plan.ID,
					"approvalReason": reason,
					"message":        fmt.Sprintf("Deletion of %q requires human approval. Plan %s created.", payload.Name, plan.ID),
				}, nil
			}
		} else {
			// Fallback to legacy checks
			needsApproval, reason := core.RequiresDeleteApproval(payload.Name)
			if needsApproval {
				plan, err := h.plans.Submit(actor, agentID, goal, reason, spec)
				if err != nil {
					return nil, fmt.Errorf("failed to create delete approval plan: %w", err)
				}
				return map[string]any{
					"status":         "pending_approval",
					"planId":         plan.ID,
					"approvalReason": reason,
					"message":        fmt.Sprintf("Deletion of %q requires human approval. Plan %s created.", payload.Name, plan.ID),
				}, nil
			}
		}

		return map[string]string{"status": "deleted"}, h.engine.Delete(ctx, actor, payload.Name)

	case "get_logs":
		var payload struct {
			Name string `json:"name"`
			Tail int    `json:"tail"`
		}
		if err := json.Unmarshal(args, &payload); err != nil {
			return nil, err
		}
		return h.engine.Logs(ctx, payload.Name, payload.Tail)

	case "list_pending_plans":
		return h.plans.List(), nil

	case "approve_plan":
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(args, &payload); err != nil {
			return nil, err
		}
		plan, err := h.plans.Approve(payload.ID, actor)
		if err != nil {
			return nil, err
		}
		if err := h.engine.Apply(ctx, actor, plan.Spec); err != nil {
			return nil, fmt.Errorf("plan approved but apply failed: %w", err)
		}
		return map[string]string{"status": "approved_and_applied", "planId": payload.ID}, nil

	case "reject_plan":
		var payload struct {
			ID      string `json:"id"`
			Comment string `json:"comment"`
		}
		if err := json.Unmarshal(args, &payload); err != nil {
			return nil, err
		}
		if err := h.plans.Reject(payload.ID, actor, payload.Comment); err != nil {
			return nil, err
		}
		return map[string]string{"status": "rejected", "planId": payload.ID}, nil

	case "get_workload":
		var payload struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(args, &payload); err != nil {
			return nil, err
		}
		status, err := h.engine.Status(ctx)
		if err != nil {
			return nil, err
		}
		for _, ws := range status {
			if ws.Spec.Name == payload.Name {
				return ws, nil
			}
		}
		return nil, fmt.Errorf("workload %q not found", payload.Name)

	case "get_behavior_metrics":
		return h.tracker.AllMetrics(), nil

	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

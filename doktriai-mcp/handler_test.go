package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/praful224/doktriai/doktriai-core"
	"github.com/praful224/doktriai/doktriai-packages"
	doktriruntime "github.com/praful224/doktriai/doktriai-runtime"
)

func testHandler(t *testing.T) *ProtocolHandler {
	t.Helper()
	dir := t.TempDir()
	store, err := core.OpenStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	bus := core.NewEventBus(20)
	driver := doktriruntime.NewDockerDriver("docker")
	driver.SetSimulated(true)
	engine := core.NewEngine(store, driver, bus, 30*time.Second)
	plans := core.NewPlanStore()
	return NewProtocolHandler(store, engine, plans)
}

func rpcPayload(t *testing.T, method string, params any) []byte {
	t.Helper()
	p := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		raw, _ := json.Marshal(params)
		p["params"] = json.RawMessage(raw)
	}
	data, _ := json.Marshal(p)
	return data
}

// ─── Initialize ───────────────────────────────────────────────────────────────

func TestMCP_Initialize(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "initialize", nil)

	result, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	m := result.(map[string]any)
	if m["server"] != "doktriai-mcp" {
		t.Errorf("expected server 'doktriai-mcp', got %v", m["server"])
	}
}

// ─── Tools List ───────────────────────────────────────────────────────────────

func TestMCP_ToolsList(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/list", nil)

	result, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err != nil {
		t.Fatalf("tools/list failed: %v", err)
	}

	tools := result.([]map[string]any)
	if len(tools) == 0 {
		t.Error("expected non-empty tools list")
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool["name"].(string)] = true
	}

	expected := []string{"deploy_workload", "list_workloads", "delete_workload", "get_logs", "approve_plan", "reject_plan"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected tool %q in tools list", name)
		}
	}
}

// ─── List Workloads ───────────────────────────────────────────────────────────

func TestMCP_ListWorkloads(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/call", map[string]any{
		"name":      "list_workloads",
		"arguments": map[string]any{},
	})

	result, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err != nil {
		t.Fatalf("list_workloads failed: %v", err)
	}

	statuses := result.([]packages.WorkloadStatus)
	if len(statuses) == 0 {
		t.Error("expected non-empty workload list (seed data)")
	}
}

// ─── Deploy Workload via MCP ──────────────────────────────────────────────────

func TestMCP_DeployWorkload_Valid(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/call", map[string]any{
		"name": "deploy_workload",
		"arguments": map[string]any{
			"name": "mcp-deploy", "image": "nginx:alpine", "replicas": 1,
			"port": 8080, "containerPort": 80, "runtime": "docker",
		},
	})

	result, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err != nil {
		t.Fatalf("deploy_workload failed: %v", err)
	}

	m := result.(map[string]string)
	if m["status"] != "accepted" {
		t.Errorf("expected status 'accepted', got %q", m["status"])
	}
}

func TestMCP_DeployWorkload_BlockedRegistry(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/call", map[string]any{
		"name": "deploy_workload",
		"arguments": map[string]any{
			"name": "evil-app", "image": "malware.io/exploit:latest", "replicas": 1,
			"runtime": "docker",
		},
	})

	_, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err == nil {
		t.Error("expected error for blocked registry")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Errorf("expected allowlist error, got: %v", err)
	}
}

func TestMCP_DeployWorkload_HighReplicas_PTEGate(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/call", map[string]any{
		"name": "deploy_workload",
		"arguments": map[string]any{
			"name": "big-scale", "image": "nginx:alpine", "replicas": 20,
			"runtime": "docker",
		},
	})

	result, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err != nil {
		t.Fatalf("expected PTE gate result, got error: %v", err)
	}

	m := result.(map[string]any)
	if m["status"] != "pending_approval" {
		t.Errorf("expected pending_approval, got %v", m["status"])
	}
	if m["planId"] == nil || m["planId"] == "" {
		t.Error("expected non-empty planId")
	}
}

// ─── Delete Workload via MCP ──────────────────────────────────────────────────

func TestMCP_DeleteWorkload_RequiresApproval(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/call", map[string]any{
		"name":      "delete_workload",
		"arguments": map[string]any{"name": "secure-ingress"},
	})

	result, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err != nil {
		t.Fatalf("delete_workload failed: %v", err)
	}

	m := result.(map[string]any)
	if m["status"] != "pending_approval" {
		t.Errorf("expected pending_approval for delete, got %v", m["status"])
	}
}

// ─── Get Workload ─────────────────────────────────────────────────────────────

func TestMCP_GetWorkload_Exists(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/call", map[string]any{
		"name":      "get_workload",
		"arguments": map[string]any{"name": "secure-ingress"},
	})

	_, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err != nil {
		t.Fatalf("get_workload failed: %v", err)
	}
}

func TestMCP_GetWorkload_NotFound(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/call", map[string]any{
		"name":      "get_workload",
		"arguments": map[string]any{"name": "nonexistent"},
	})

	_, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err == nil {
		t.Error("expected error for non-existent workload")
	}
}

// ─── Get Logs ─────────────────────────────────────────────────────────────────

func TestMCP_GetLogs(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/call", map[string]any{
		"name":      "get_logs",
		"arguments": map[string]any{"name": "secure-ingress", "tail": 10},
	})

	_, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err != nil {
		t.Fatalf("get_logs failed: %v", err)
	}
}

// ─── Plan Approval/Rejection via MCP ──────────────────────────────────────────

func TestMCP_ApprovePlan(t *testing.T) {
	h := testHandler(t)

	// First create a plan via deploy
	deployPayload := rpcPayload(t, "tools/call", map[string]any{
		"name": "deploy_workload",
		"arguments": map[string]any{
			"name": "approve-test", "image": "nginx:alpine", "replicas": 20,
			"runtime": "docker",
		},
	})
	result, _ := h.HandleRPC(context.Background(), "test-agent", deployPayload)
	m := result.(map[string]any)
	planID := m["planId"].(string)

	// Approve it
	approvePayload := rpcPayload(t, "tools/call", map[string]any{
		"name":      "approve_plan",
		"arguments": map[string]any{"id": planID},
	})
	result, err := h.HandleRPC(context.Background(), "admin", approvePayload)
	if err != nil {
		t.Fatalf("approve_plan failed: %v", err)
	}

	approved := result.(map[string]string)
	if approved["status"] != "approved_and_applied" {
		t.Errorf("expected approved_and_applied, got %q", approved["status"])
	}
}

func TestMCP_RejectPlan(t *testing.T) {
	h := testHandler(t)

	// Create a plan
	deployPayload := rpcPayload(t, "tools/call", map[string]any{
		"name": "deploy_workload",
		"arguments": map[string]any{
			"name": "reject-test", "image": "nginx:alpine", "replicas": 20,
			"runtime": "docker",
		},
	})
	result, _ := h.HandleRPC(context.Background(), "test-agent", deployPayload)
	m := result.(map[string]any)
	planID := m["planId"].(string)

	// Reject it
	rejectPayload := rpcPayload(t, "tools/call", map[string]any{
		"name":      "reject_plan",
		"arguments": map[string]any{"id": planID, "comment": "too risky"},
	})
	result, err := h.HandleRPC(context.Background(), "admin", rejectPayload)
	if err != nil {
		t.Fatalf("reject_plan failed: %v", err)
	}

	rejected := result.(map[string]string)
	if rejected["status"] != "rejected" {
		t.Errorf("expected rejected, got %q", rejected["status"])
	}
}

// ─── Unknown Tool ─────────────────────────────────────────────────────────────

func TestMCP_UnknownTool(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/call", map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]any{},
	})

	_, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

// ─── Unknown Method ───────────────────────────────────────────────────────────

func TestMCP_UnknownMethod(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "invalid/method", nil)

	_, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err == nil {
		t.Error("expected error for unknown method")
	}
}

// ─── Scope Validation ─────────────────────────────────────────────────────────

func TestMCP_ScopeValidation_Allowed(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/call", map[string]any{
		"name":      "list_workloads",
		"arguments": map[string]any{},
		"scope":     "list_workloads,deploy_workload",
	})

	_, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err != nil {
		t.Fatalf("expected scope-allowed call, got: %v", err)
	}
}

func TestMCP_ScopeValidation_Denied(t *testing.T) {
	h := testHandler(t)
	payload := rpcPayload(t, "tools/call", map[string]any{
		"name":      "delete_workload",
		"arguments": map[string]any{"name": "test"},
		"scope":     "list_workloads",
	})

	_, err := h.HandleRPC(context.Background(), "test-agent", payload)
	if err == nil {
		t.Error("expected scope violation error")
	}
	if !strings.Contains(err.Error(), "outside declared agent scope") {
		t.Errorf("expected scope violation message, got: %v", err)
	}
}

// ─── Environment Lock Enforcement ─────────────────────────────────────────────

func TestMCP_DeployBlockedByLock(t *testing.T) {
	dir := t.TempDir()
	store, _ := core.OpenStore(filepath.Join(dir, "state.json"))
	bus := core.NewEventBus(20)
	driver := doktriruntime.NewDockerDriver("docker")
	engine := core.NewEngine(store, driver, bus, 30*time.Second)
	plans := core.NewPlanStore()
	h := NewProtocolHandler(store, engine, plans)

	// Acquire lock as a different user
	store.AcquireLock("admin-user", "maintenance window")

	payload := rpcPayload(t, "tools/call", map[string]any{
		"name": "deploy_workload",
		"arguments": map[string]any{
			"name": "locked-test", "image": "nginx:alpine", "replicas": 1, "runtime": "docker",
		},
	})

	_, err := h.HandleRPC(context.Background(), "other-agent", payload)
	if err == nil {
		t.Error("expected deploy blocked by environment lock")
	}
	if !strings.Contains(err.Error(), "locked") {
		t.Errorf("expected 'locked' error, got: %v", err)
	}
}

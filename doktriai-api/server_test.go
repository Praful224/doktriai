package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/praful224/doktriai/doktriai-core"
	"github.com/praful224/doktriai/doktriai-packages"
	doktriruntime "github.com/praful224/doktriai/doktriai-runtime"
)

func testServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	store, err := core.OpenStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	bus := core.NewEventBus(20)
	driver := doktriruntime.NewDockerDriver("docker")
	// Set driver simulated to true explicitly to ensure we don't attempt calling a real docker binary
	// under test settings.
	driver.SetSimulated(true)
	engine := core.NewEngine(store, driver, bus, 30*time.Second)
	server := NewServer(store, engine, bus, dir)
	return server, server.Routes()
}

func makeJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	return data
}

// ─── Health ───────────────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("GET", "/api/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var body map[string]any
	json.Unmarshal(rr.Body.Bytes(), &body)
	if body["ok"] != true {
		t.Error("expected ok=true")
	}
	if body["service"] != "doktriai-api" {
		t.Errorf("expected service 'doktriai-api', got %v", body["service"])
	}
}

// ─── Workloads List ───────────────────────────────────────────────────────────

func TestWorkloadsList(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("GET", "/api/workloads", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// ─── Deploy Workload ──────────────────────────────────────────────────────────

func TestDeployWorkload_Valid(t *testing.T) {
	_, handler := testServer(t)

	body := makeJSON(t, map[string]any{
		"name": "test-deploy", "image": "nginx:alpine", "replicas": 1,
		"port": 8080, "containerPort": 80, "runtime": "docker",
	})

	req := httptest.NewRequest("POST", "/api/workloads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Doktri-Role", "admin")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeployWorkload_BadJSON(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("POST", "/api/workloads", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Doktri-Role", "admin")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestDeployWorkload_InvalidName(t *testing.T) {
	_, handler := testServer(t)

	body := makeJSON(t, map[string]any{
		"name": "INVALID NAME!", "image": "nginx:alpine", "replicas": 1,
	})

	req := httptest.NewRequest("POST", "/api/workloads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Doktri-Role", "admin")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeployWorkload_BlockedRegistry(t *testing.T) {
	_, handler := testServer(t)

	body := makeJSON(t, map[string]any{
		"name": "test-blocked", "image": "malicious.io/exploit:latest", "replicas": 1,
	})

	req := httptest.NewRequest("POST", "/api/workloads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Doktri-Role", "admin")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeployWorkload_HighReplicas_PTEGate(t *testing.T) {
	_, handler := testServer(t)

	body := makeJSON(t, map[string]any{
		"name": "scale-test", "image": "nginx:alpine", "replicas": 10,
		"runtime": "docker",
	})

	req := httptest.NewRequest("POST", "/api/workloads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Doktri-Role", "admin")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["status"] != "pending_approval" {
		t.Errorf("expected pending_approval, got %v", resp["status"])
	}
	if resp["planId"] == nil || resp["planId"] == "" {
		t.Error("expected non-empty planId")
	}
}

// ─── RBAC ─────────────────────────────────────────────────────────────────────

func TestDeployWorkload_ViewerDenied(t *testing.T) {
	_, handler := testServer(t)

	body := makeJSON(t, map[string]any{
		"name": "test-viewer", "image": "nginx:alpine", "replicas": 1,
	})

	req := httptest.NewRequest("POST", "/api/workloads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Doktri-Role", "viewer")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for viewer, got %d", rr.Code)
	}
}

// ─── Get Single Workload ──────────────────────────────────────────────────────

func TestGetWorkload_Exists(t *testing.T) {
	_, handler := testServer(t)

	// Seed data includes "secure-ingress"
	req := httptest.NewRequest("GET", "/api/workloads/secure-ingress", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetWorkload_NotFound(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("GET", "/api/workloads/nonexistent", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// ─── Delete Workload (PTE Gate) ───────────────────────────────────────────────

func TestDeleteWorkload_RequiresApproval(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("DELETE", "/api/workloads/secure-ingress", nil)
	req.Header.Set("X-Doktri-Role", "admin")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202 (pending approval), got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["status"] != "pending_approval" {
		t.Errorf("expected pending_approval, got %v", resp["status"])
	}
}

func TestDeleteWorkload_ViewerDenied(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("DELETE", "/api/workloads/secure-ingress", nil)
	req.Header.Set("X-Doktri-Role", "viewer")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for viewer delete, got %d", rr.Code)
	}
}

// ─── Validate ─────────────────────────────────────────────────────────────────

func TestValidateWorkload_Valid(t *testing.T) {
	_, handler := testServer(t)

	body := makeJSON(t, map[string]any{
		"name": "valid-app", "image": "nginx:alpine", "replicas": 1, "runtime": "docker",
	})

	req := httptest.NewRequest("POST", "/api/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["valid"] != true {
		t.Error("expected valid=true")
	}
}

func TestValidateWorkload_Invalid(t *testing.T) {
	_, handler := testServer(t)

	body := makeJSON(t, map[string]any{
		"name": "bad name!", "image": "nginx:alpine",
	})

	req := httptest.NewRequest("POST", "/api/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (with valid=false), got %d", rr.Code)
	}

	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["valid"] != false {
		t.Error("expected valid=false")
	}
}

// ─── Audit ────────────────────────────────────────────────────────────────────

func TestAudit(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("GET", "/api/audit", nil)
	req.Header.Set("X-Doktri-Role", "admin")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// ─── Plans ────────────────────────────────────────────────────────────────────

func TestListPlans(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("GET", "/api/plan", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// ─── Metrics ──────────────────────────────────────────────────────────────────

func TestMetrics(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("GET", "/api/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !bytes.Contains(rr.Body.Bytes(), []byte("doktriai_workloads_total")) {
		t.Errorf("expected Prometheus metric 'doktriai_workloads_total' in body: %s", body)
	}
}

// ─── Schema ───────────────────────────────────────────────────────────────────

func TestSchema(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("GET", "/api/schema", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var fields []map[string]any
	json.Unmarshal(rr.Body.Bytes(), &fields)
	if len(fields) == 0 {
		t.Error("expected non-empty schema fields")
	}
}

// ─── Lock ─────────────────────────────────────────────────────────────────────

func TestLock_GetDefault(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("GET", "/api/lock", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var lock packages.LockState
	json.Unmarshal(rr.Body.Bytes(), &lock)
	if lock.Locked {
		t.Error("expected unlocked by default")
	}
}

func TestLock_AcquireAndRelease(t *testing.T) {
	_, handler := testServer(t)

	// Acquire
	body := makeJSON(t, map[string]string{"reason": "test maintenance"})
	req := httptest.NewRequest("POST", "/api/lock", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Doktri-Role", "admin")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for lock acquire, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify locked
	req = httptest.NewRequest("GET", "/api/lock", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var lock packages.LockState
	json.Unmarshal(rr.Body.Bytes(), &lock)
	if !lock.Locked {
		t.Error("expected locked after acquire")
	}

	// Release
	req = httptest.NewRequest("DELETE", "/api/lock", nil)
	req.Header.Set("X-Doktri-Role", "admin")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for lock release, got %d", rr.Code)
	}
}

// ─── Security Headers ─────────────────────────────────────────────────────────

func TestSecurityHeaders(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("GET", "/api/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	expectedHeaders := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
		"X-Xss-Protection":      "1; mode=block",
	}
	for header, expected := range expectedHeaders {
		got := rr.Header().Get(header)
		if got != expected {
			t.Errorf("expected %s=%q, got %q", header, expected, got)
		}
	}
}

// ─── CORS ─────────────────────────────────────────────────────────────────────

func TestCORS_AllowedOrigin(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("OPTIONS", "/api/health", nil)
	req.Header.Set("Origin", "http://localhost:18080")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204 for preflight, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://localhost:18080" {
		t.Errorf("expected CORS origin header, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_BlockedOrigin(t *testing.T) {
	_, handler := testServer(t)

	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("Origin", "http://evil-site.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("expected no CORS header for blocked origin")
	}
}

// ─── Rate Limiting ────────────────────────────────────────────────────────────

func TestRateLimit_NotTriggeredUnderLimit(t *testing.T) {
	_, handler := testServer(t)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/api/health", nil)
		req.RemoteAddr = "10.0.0.99:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			t.Errorf("unexpected rate limit on request %d", i)
		}
	}
}

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/praful224/doktriai/doktriai-core"
	"github.com/praful224/doktriai/doktriai-mcp"
	"github.com/praful224/doktriai/doktriai-packages"
	doktriruntime "github.com/praful224/doktriai/doktriai-runtime"
)

// Server is the HTTP API surface for the DOKTRIAI control plane.
type Server struct {
	store    *core.Store
	engine   *core.Engine
	bus      *core.EventBus
	plans    *core.PlanStore
	behavior *core.BehaviorTracker
	mcp      *mcp.ProtocolHandler
	webDir   string
}

func NewServer(store *core.Store, engine *core.Engine, bus *core.EventBus, webDir string) *Server {
	return &Server{
		store:    store,
		engine:   engine,
		bus:      bus,
		plans:    core.NewPlanStore(),
		behavior: core.NewBehaviorTracker(),
		mcp:      mcp.NewProtocolHandler(store, engine),
		webDir:   webDir,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /api/health", s.health)

	// Workload management
	mux.HandleFunc("GET /api/workloads", s.workloads)
	mux.HandleFunc("POST /api/workloads", s.applyWorkload)
	mux.HandleFunc("DELETE /api/workloads/{name}", s.deleteWorkload)
	mux.HandleFunc("POST /api/reconcile", s.reconcile)
	mux.HandleFunc("POST /api/validate", s.validateWorkload)

	// PTE Plan Gate (Layer 2 — ASI09)
	mux.HandleFunc("POST /api/plan", s.submitPlan)
	mux.HandleFunc("GET /api/plan", s.listPlans)
	mux.HandleFunc("POST /api/plan/{id}/approve", s.approvePlan)
	mux.HandleFunc("POST /api/plan/{id}/reject", s.rejectPlan)

	// Real-time & observability
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/audit", s.audit)
	mux.HandleFunc("GET /api/logs/{name}", s.logs)

	// Behavioral metrics (Layer 3 — ASI10)
	mux.HandleFunc("GET /api/behavior", s.behaviorMetrics)

	// MCP bridge
	mux.HandleFunc("POST /api/mcp", s.mcpHandler)

	// Environment lock
	mux.HandleFunc("GET /api/lock", s.getLock)
	mux.HandleFunc("POST /api/lock", s.acquireLock)
	mux.HandleFunc("DELETE /api/lock", s.releaseLock)

	// Extended modules endpoints
	mux.HandleFunc("GET /api/schema", s.schema)
	mux.HandleFunc("GET /api/runtime/status", s.runtimeStatus)
	mux.HandleFunc("POST /api/charts/render", s.renderChart)

	// Static web dashboard
	mux.Handle("/", http.FileServer(http.Dir(filepath.Clean(s.webDir))))

	return withRateLimit(withSecurityHeaders(withCORS(mux)))
}

// --- Health ---

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"service":   "doktriai-api",
		"runtime":   "docker",
		"timestamp": time.Now().UTC(),
		"authMode":  map[bool]string{true: "dev", false: "production"}[core.IsDevMode()],
		"circuits":  s.engine.ListCircuitBreakers(),
	})
}

// --- Workloads ---

func (s *Server) workloads(w http.ResponseWriter, r *http.Request) {
	status, err := s.engine.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) applyWorkload(w http.ResponseWriter, r *http.Request) {
	claims := s.authenticate(w, r)
	if claims == nil {
		return
	}
	if !authorizeRole(w, r, claims.Role, "apply") {
		return
	}

	// Behavioral anomaly check (Layer 3)
	s.behavior.Record(claims.ActorName, "apply")
	if anomalous, score := s.behavior.IsAnomalous(claims.ActorName); anomalous {
		s.bus.Publish(packages.Event{
			Level: "error", Source: "behavior-tracker",
			Message: fmt.Sprintf("ANOMALY: actor %q flagged (score=%.2f) — rate limit exceeded", claims.ActorName, score),
		})
	}

	lockState := s.store.GetLock()
	if lockState.Locked && lockState.AcquiredBy != claims.ActorName {
		writeError(w, http.StatusConflict, fmt.Errorf("environment locked by %s: %s", lockState.AcquiredBy, lockState.Reason))
		return
	}

	var spec packages.WorkloadSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if err := core.CheckAgentIntent(spec); err != nil {
		s.behavior.Record(claims.ActorName, "reject")
		_, _ = s.store.AddAudit(packages.AuditRecord{
			Actor: claims.ActorName, Action: "apply", Workload: spec.Name,
			Allowed: false, Reason: err.Error(),
			AgentID: claims.AgentID, AgentScope: claims.Scope, AgentGoal: claims.Goal,
			SignatureVerified: !claims.Dev,
		})
		s.bus.Publish(packages.Event{Level: "error", Source: "api", Workload: spec.Name, Message: fmt.Sprintf("Intent Guard block: %v", err)})
		writeError(w, http.StatusForbidden, err)
		return
	}

	// PTE Gate: check if human approval is required (Layer 2)
	if needsApproval, reason := core.RequiresHumanApproval(spec); needsApproval {
		plan, err := s.plans.Submit(claims.ActorName, claims.AgentID, claims.Goal, reason, spec)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.bus.Publish(packages.Event{
			Level: "warn", Source: "pte-gate", Workload: spec.Name,
			Message: fmt.Sprintf("PTE Gate: plan %s created — awaiting human approval (%s)", plan.ID, reason),
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":         "pending_approval",
			"planId":         plan.ID,
			"approvalReason": reason,
			"expiresAt":      plan.ExpiresAt,
		})
		return
	}

	if err := s.engine.Apply(r.Context(), claims.ActorName, spec); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *Server) deleteWorkload(w http.ResponseWriter, r *http.Request) {
	claims := s.authenticate(w, r)
	if claims == nil {
		return
	}
	if !authorizeRole(w, r, claims.Role, "delete") {
		return
	}

	s.behavior.Record(claims.ActorName, "delete")

	lockState := s.store.GetLock()
	if lockState.Locked && lockState.AcquiredBy != claims.ActorName {
		writeError(w, http.StatusConflict, fmt.Errorf("environment locked by %s: %s", lockState.AcquiredBy, lockState.Reason))
		return
	}

	name := r.PathValue("name")

	// Deletes ALWAYS require PTE approval (Layer 2)
	if needsApproval, reason := core.RequiresDeleteApproval(name); needsApproval {
		plan, err := s.plans.Submit(claims.ActorName, claims.AgentID, claims.Goal, reason,
			packages.WorkloadSpec{Name: name})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.bus.Publish(packages.Event{
			Level: "warn", Source: "pte-gate", Workload: name,
			Message: fmt.Sprintf("PTE Gate: delete plan %s created — awaiting human approval", plan.ID),
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":         "pending_approval",
			"planId":         plan.ID,
			"approvalReason": reason,
		})
		return
	}

	if err := s.engine.Delete(r.Context(), claims.ActorName, name); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) reconcile(w http.ResponseWriter, r *http.Request) {
	claims := s.authenticate(w, r)
	if claims == nil {
		return
	}
	if !authorizeRole(w, r, claims.Role, "reconcile") {
		return
	}
	if err := s.engine.Reconcile(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reconciled"})
}

func (s *Server) validateWorkload(w http.ResponseWriter, r *http.Request) {
	var spec packages.WorkloadSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	spec = core.NormalizeSpec(spec)
	if err := core.ValidateSpec(spec); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	if err := core.CheckAgentIntent(spec); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	needsApproval, approvalReason := core.RequiresHumanApproval(spec)
	writeJSON(w, http.StatusOK, map[string]any{
		"valid":          true,
		"needsApproval":  needsApproval,
		"approvalReason": approvalReason,
	})
}

// --- PTE Plan Gate (Layer 2 — ASI09) ---

func (s *Server) submitPlan(w http.ResponseWriter, r *http.Request) {
	claims := s.authenticate(w, r)
	if claims == nil {
		return
	}
	var spec packages.WorkloadSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := core.CheckAgentIntent(spec); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	_, reason := core.RequiresHumanApproval(spec)
	if reason == "" {
		reason = "manually submitted plan"
	}
	plan, err := s.plans.Submit(claims.ActorName, claims.AgentID, claims.Goal, reason, spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.plans.List())
}

func (s *Server) approvePlan(w http.ResponseWriter, r *http.Request) {
	claims := s.authenticate(w, r)
	if claims == nil {
		return
	}
	if !authorizeRole(w, r, claims.Role, "reconcile") {
		return
	}
	id := r.PathValue("id")
	plan, err := s.plans.Approve(id, claims.ActorName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// Execute the approved plan
	if err := s.engine.Apply(r.Context(), claims.ActorName, plan.Spec); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.bus.Publish(packages.Event{
		Level: "ok", Source: "pte-gate", Workload: plan.Spec.Name,
		Message: fmt.Sprintf("Plan %s approved by %s and applied", id, claims.ActorName),
	})
	_, _ = s.store.AddAudit(packages.AuditRecord{
		Actor: claims.ActorName, Action: "plan_approve", Workload: plan.Spec.Name,
		Allowed: true, PlanID: id, PlanApproved: true, ApprovedBy: claims.ActorName,
		AgentID: claims.AgentID, SignatureVerified: !claims.Dev,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved_and_applied", "planId": id})
}

func (s *Server) rejectPlan(w http.ResponseWriter, r *http.Request) {
	claims := s.authenticate(w, r)
	if claims == nil {
		return
	}
	if !authorizeRole(w, r, claims.Role, "reconcile") {
		return
	}
	id := r.PathValue("id")
	var body struct {
		Comment string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.plans.Reject(id, claims.ActorName, body.Comment); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.bus.Publish(packages.Event{
		Level: "warn", Source: "pte-gate",
		Message: fmt.Sprintf("Plan %s rejected by %s: %s", id, claims.ActorName, body.Comment),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected", "planId": id})
}

// --- Events & Audit ---

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	for _, event := range reverseEvents(s.store.ListEvents()) {
		writeSSE(w, event)
	}
	flusher.Flush()
	ch := s.bus.Subscribe()
	defer s.bus.Unsubscribe(ch)
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			writeSSE(w, event)
			flusher.Flush()
		}
	}
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	// Audit requires at least operator role (Layer 3 — ASI06: protect audit from poisoning)
	claims := s.authenticate(w, r)
	if claims == nil {
		return
	}
	if !authorizeRole(w, r, claims.Role, "read") {
		return
	}

	// Support incremental reads via ?since=<seqId>
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		seq, err := strconv.ParseInt(sinceStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid since param"))
			return
		}
		writeJSON(w, http.StatusOK, s.store.GetAuditSince(seq))
		return
	}
	writeJSON(w, http.StatusOK, s.store.ListAudit())
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	logs, err := s.engine.Logs(r.Context(), r.PathValue("name"), tail)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

// --- Behavioral Metrics (Layer 3 — ASI10) ---

func (s *Server) behaviorMetrics(w http.ResponseWriter, r *http.Request) {
	claims := s.authenticate(w, r)
	if claims == nil {
		return
	}
	if !authorizeRole(w, r, claims.Role, "read") {
		return
	}
	writeJSON(w, http.StatusOK, s.behavior.AllMetrics())
}

// --- MCP bridge ---

func (s *Server) mcpHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	claims := s.authenticate(w, r)
	if claims == nil {
		return
	}
	result, err := s.mcp.HandleRPC(r.Context(), claims.ActorName, body)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "error": map[string]any{"code": -32000, "message": err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "result": result})
}

// --- Lock ---

func (s *Server) getLock(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.GetLock())
}

func (s *Server) acquireLock(w http.ResponseWriter, r *http.Request) {
	claims := s.authenticate(w, r)
	if claims == nil {
		return
	}
	if !authorizeRole(w, r, claims.Role, "reconcile") {
		return
	}
	var payload struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if err := s.store.AcquireLock(claims.ActorName, payload.Reason); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.bus.Publish(packages.Event{Level: "warn", Source: "api", Message: fmt.Sprintf("environment locked by %s", claims.ActorName)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "locked"})
}

func (s *Server) releaseLock(w http.ResponseWriter, r *http.Request) {
	claims := s.authenticate(w, r)
	if claims == nil {
		return
	}
	if !authorizeRole(w, r, claims.Role, "reconcile") {
		return
	}
	if err := s.store.ReleaseLock(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.bus.Publish(packages.Event{Level: "ok", Source: "api", Message: fmt.Sprintf("environment unlocked by %s", claims.ActorName)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "unlocked"})
}

// --- Auth helpers (Layer 0) ---

// authenticate resolves AgentClaims from the request.
// In dev mode: falls back to X-Doktri-Role / X-Doktri-Actor headers.
// In production mode: requires a valid X-Doktri-Token.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) *core.AgentClaims {
	tokenStr := strings.TrimSpace(r.Header.Get("X-Doktri-Token"))
	if tokenStr != "" {
		claims, err := core.VerifyRequestToken(tokenStr)
		if err != nil {
			writeError(w, http.StatusUnauthorized, fmt.Errorf("authentication failed: %v", err))
			return nil
		}
		return &claims
	}
	// No token: dev-mode fallback
	if !core.IsDevMode() {
		writeError(w, http.StatusUnauthorized, fmt.Errorf("X-Doktri-Token required in production auth mode"))
		return nil
	}
	roleVal := strings.TrimSpace(r.Header.Get("X-Doktri-Role"))
	actorVal := strings.TrimSpace(r.Header.Get("X-Doktri-Actor"))
	if roleVal == "" {
		roleVal = "admin"
	}
	claims := core.DevClaimsFromHeaders(roleVal, actorVal)
	return &claims
}

func authorizeRole(w http.ResponseWriter, r *http.Request, role, action string) bool {
	if core.RoleCan(role, action) {
		return true
	}
	writeError(w, http.StatusForbidden, fmt.Errorf("role %q cannot %s", role, action))
	return false
}

// --- Rate limiter (Layer 5 — per-IP token bucket, 60 req/min) ---

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens   float64
	lastFill time.Time
}

var globalRateLimiter = &rateLimiter{buckets: make(map[string]*bucket)}

const (
	rateLimit      = 60.0  // requests per minute
	bucketCapacity = 120.0 // burst up to 2×
)

func withRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := strings.Split(r.RemoteAddr, ":")[0]
		globalRateLimiter.mu.Lock()
		b, ok := globalRateLimiter.buckets[ip]
		if !ok {
			b = &bucket{tokens: bucketCapacity, lastFill: time.Now()}
			globalRateLimiter.buckets[ip] = b
		}
		now := time.Now()
		elapsed := now.Sub(b.lastFill).Seconds()
		b.tokens += elapsed * (rateLimit / 60.0)
		if b.tokens > bucketCapacity {
			b.tokens = bucketCapacity
		}
		b.lastFill = now
		allowed := b.tokens >= 1.0
		if allowed {
			b.tokens--
		}
		globalRateLimiter.mu.Unlock()
		if !allowed {
			writeError(w, http.StatusTooManyRequests, fmt.Errorf("rate limit exceeded — max %d requests/minute", int(rateLimit)))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Security headers (Layer 5) ---

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		next.ServeHTTP(w, r)
	})
}

// --- CORS (Layer 5 — hardened, no wildcard) ---

func withCORS(next http.Handler) http.Handler {
	// Configurable via env DOKTRIAI_CORS_ORIGINS (comma-separated).
	// Defaults to localhost only for development safety.
	allowedOrigins := map[string]bool{
		"http://localhost:18082": true,
		"http://127.0.0.1:18082": true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Doktri-Role, X-Doktri-Actor, X-Doktri-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Shared helpers ---

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeSSE(w http.ResponseWriter, event packages.Event) {
	body, _ := json.Marshal(event)
	_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Source, body)
}

func reverseEvents(events []packages.Event) []packages.Event {
	out := make([]packages.Event, len(events))
	for i := range events {
		out[i] = events[len(events)-1-i]
	}
	return out
}

func (s *Server) schema(w http.ResponseWriter, r *http.Request) {
	fields := []map[string]any{
		{
			"name":        "name",
			"type":        "string",
			"required":    true,
			"description": "Unique identifier name for the container workload (e.g. frontend-web). Must contain only lowercase alphanumeric characters, dots, or dashes.",
			"example":     "hello-world",
		},
		{
			"name":        "image",
			"type":        "string",
			"required":    true,
			"description": "Container image reference (e.g. nginx:alpine). Production security mode requires a SHA256 digest pin identifier.",
			"example":     "nginx@sha256:455d39...",
		},
		{
			"name":        "replicas",
			"type":        "integer",
			"required":    false,
			"description": "Desired count of container instances. Defaults to 1. Values exceeding 5 require explicit operator PTE Gate approval.",
			"example":     3,
		},
		{
			"name":        "port",
			"type":        "integer",
			"required":    false,
			"description": "Service ingress port exposed on the host machine.",
			"example":     8080,
		},
		{
			"name":        "containerPort",
			"type":        "integer",
			"required":    false,
			"description": "Internal port exposed by the target container process.",
			"example":     80,
		},
		{
			"name":        "runtime",
			"type":        "string",
			"required":    false,
			"description": "Driver selector for virtualization runtime host. Choices: 'docker' or 'kubernetes'. Defaults to 'docker'.",
			"example":     "docker",
		},
		{
			"name":        "env",
			"type":        "object",
			"required":    false,
			"description": "Key-value pair mappings for environment variables injected into the container scope. Sensitive variables must be base64-encoded.",
			"example":     `{"DB_PORT": "5432"}`,
		},
		{
			"name":        "securityMode",
			"type":        "string",
			"required":    false,
			"description": "Compliance checking mode. Choices: 'dev', 'staging', 'production'. In production, registry allowlists and digest pins are strictly verified.",
			"example":     "production",
		},
	}
	writeJSON(w, http.StatusOK, fields)
}

func (s *Server) runtimeStatus(w http.ResponseWriter, r *http.Request) {
	driver := s.engine.Runtime()
	
	dockerSimulated := false
	if d, ok := driver.(*doktriruntime.DockerDriver); ok {
		dockerSimulated = d.IsSimulated()
	}
	
	statusList, err := s.engine.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	
	var containers []any
	for _, ws := range statusList {
		for _, act := range ws.Actual {
			containers = append(containers, map[string]any{
				"id":      act.ID,
				"name":    act.Name,
				"replica": act.Replica,
				"runtime": act.Runtime,
				"status":  act.Status,
				"image":   ws.Spec.Image,
			})
		}
	}
	
	writeJSON(w, http.StatusOK, map[string]any{
		"docker": map[string]any{
			"status":    "active",
			"simulated": dockerSimulated,
			"binary":    "docker",
		},
		"kubernetes": map[string]any{
			"status":    "active",
			"simulated": true,
		},
		"containers": containers,
	})
}

func (s *Server) renderChart(w http.ResponseWriter, r *http.Request) {
	var spec packages.WorkloadSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	
	var yamlBuilder strings.Builder
	yamlBuilder.WriteString("# Generated values.yaml for DOKTRIAI " + spec.Name + "\n")
	yamlBuilder.WriteString("global:\n")
	yamlBuilder.WriteString("  securityMode: " + string(spec.SecurityMode) + "\n")
	yamlBuilder.WriteString("  runtime: " + spec.Runtime + "\n\n")
	yamlBuilder.WriteString("replicaCount: " + strconv.Itoa(spec.Replicas) + "\n\n")
	yamlBuilder.WriteString("image:\n")
	parts := strings.Split(spec.Image, ":")
	repo := parts[0]
	tag := "latest"
	if len(parts) > 1 {
		tag = parts[1]
	}
	yamlBuilder.WriteString("  repository: " + repo + "\n")
	yamlBuilder.WriteString("  tag: " + tag + "\n")
	yamlBuilder.WriteString("  pullPolicy: IfNotPresent\n\n")
	yamlBuilder.WriteString("service:\n")
	yamlBuilder.WriteString("  type: ClusterIP\n")
	yamlBuilder.WriteString("  port: " + strconv.Itoa(spec.Port) + "\n")
	yamlBuilder.WriteString("  targetPort: " + strconv.Itoa(spec.ContainerPort) + "\n\n")
	
	if len(spec.Env) > 0 {
		yamlBuilder.WriteString("env:\n")
		for k, v := range spec.Env {
			yamlBuilder.WriteString("  " + k + ": " + fmt.Sprintf("%q", v) + "\n")
		}
	} else {
		yamlBuilder.WriteString("env: {}\n")
	}

	writeJSON(w, http.StatusOK, map[string]string{"yaml": yamlBuilder.String()})
}


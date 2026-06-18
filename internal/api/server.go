package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/local/kronix-control-plane/internal/core"
)

type Server struct {
	store  *core.Store
	engine *core.Engine
	bus    *core.EventBus
	webDir string
}

func NewServer(store *core.Store, engine *core.Engine, bus *core.EventBus, webDir string) *Server {
	return &Server{store: store, engine: engine, bus: bus, webDir: webDir}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/workloads", s.workloads)
	mux.HandleFunc("POST /api/workloads", s.applyWorkload)
	mux.HandleFunc("DELETE /api/workloads/{name}", s.deleteWorkload)
	mux.HandleFunc("POST /api/reconcile", s.reconcile)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/audit", s.audit)
	mux.HandleFunc("GET /api/logs/{name}", s.logs)
	mux.HandleFunc("POST /api/mcp", s.mcp)
	mux.Handle("/", http.FileServer(http.Dir(filepath.Clean(s.webDir))))
	return withCORS(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"service":   "kranix-api",
		"runtime":   "docker",
		"timestamp": time.Now().UTC(),
	})
}

func (s *Server) workloads(w http.ResponseWriter, r *http.Request) {
	status, err := s.engine.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) applyWorkload(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r, "apply") {
		return
	}
	var spec core.WorkloadSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.engine.Apply(r.Context(), actor(r), spec); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *Server) deleteWorkload(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r, "delete") {
		return
	}
	name := r.PathValue("name")
	if err := s.engine.Delete(r.Context(), actor(r), name); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) reconcile(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r, "reconcile") {
		return
	}
	if err := s.engine.Reconcile(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reconciled"})
}

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

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.handleRPC(r.Context(), r, req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func (s *Server) handleRPC(ctx context.Context, r *http.Request, req rpcRequest) (any, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{"server": "kranix-mcp", "capabilities": []string{"tools"}, "version": "0.2.0"}, nil
	case "tools/list":
		return []map[string]string{
			{"name": "deploy_workload", "description": "Declare and reconcile a Docker workload"},
			{"name": "list_workloads", "description": "List desired and actual workload state"},
			{"name": "delete_workload", "description": "Delete desired state and remove containers"},
			{"name": "get_logs", "description": "Read Docker logs for a workload"},
		}, nil
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, err
		}
		return s.callTool(ctx, r, params.Name, params.Arguments)
	default:
		return nil, fmt.Errorf("unknown method %q", req.Method)
	}
}

func (s *Server) callTool(ctx context.Context, r *http.Request, name string, args json.RawMessage) (any, error) {
	switch name {
	case "list_workloads":
		return s.engine.Status(ctx)
	case "deploy_workload":
		if !core.RoleCan(role(r), "apply") {
			return nil, fmt.Errorf("role %q cannot deploy workloads", role(r))
		}
		var spec core.WorkloadSpec
		if err := json.Unmarshal(args, &spec); err != nil {
			return nil, err
		}
		return map[string]string{"status": "accepted"}, s.engine.Apply(ctx, actor(r), spec)
	case "delete_workload":
		if !core.RoleCan(role(r), "delete") {
			return nil, fmt.Errorf("role %q cannot delete workloads", role(r))
		}
		var payload struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(args, &payload); err != nil {
			return nil, err
		}
		return map[string]string{"status": "deleted"}, s.engine.Delete(ctx, actor(r), payload.Name)
	case "get_logs":
		var payload struct {
			Name string `json:"name"`
			Tail int    `json:"tail"`
		}
		if err := json.Unmarshal(args, &payload); err != nil {
			return nil, err
		}
		return s.engine.Logs(ctx, payload.Name, payload.Tail)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func authorize(w http.ResponseWriter, r *http.Request, action string) bool {
	if core.RoleCan(role(r), action) {
		return true
	}
	writeError(w, http.StatusForbidden, fmt.Errorf("role %q cannot %s", role(r), action))
	return false
}

func role(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Kranix-Role"))
	if value == "" {
		return "admin"
	}
	return value
}

func actor(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Kranix-Actor"))
	if value == "" {
		return role(r)
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeSSE(w http.ResponseWriter, event core.Event) {
	body, _ := json.Marshal(event)
	_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Source, body)
}

func reverseEvents(events []core.Event) []core.Event {
	out := make([]core.Event, len(events))
	for i := range events {
		out[i] = events[len(events)-1-i]
	}
	return out
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Kranix-Role, X-Kranix-Actor")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

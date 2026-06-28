# DoktriAI Control Plane Upgrade Summary (Phase 1 & 2)

This document details the telemetry, security, deployment strategies, and governance features implemented to transform **DoktriAI** from a local tool into a production-ready, SaaS-capable orchestrator.

---

## 🛠️ Phase 1: Observability, Governance & Safety (Complete)

### 1. Prometheus Telemetry Engine (`/api/metrics`)
* **Features:** Rewrote the raw metrics endpoint to expose scrapable Prometheus key-value metrics detailing system health.
* **Exposed Metrics:**
  * `doktriai_workloads_total`: Cumulative gauge of active workloads in the registry.
  * `doktriai_workload_desired_replicas` / `doktriai_workload_actual_replicas`: Multi-label metrics exposing replica count drift.
  * `doktriai_reconcile_duration_seconds` / `doktriai_last_reconcile_timestamp_seconds`: Timing latency gauges of the reconcile loop.
  * `doktriai_circuit_breakers_open`: Indicator of active workload safety lockouts.
  * `doktriai_behavior_flagged_actors`: Anomalous agent execution alerts.
* **Files Created/Modified:** [server.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-api/server.go), [engine.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-core/engine.go)

### 2. Live MCP Behavior Metrics
* **Features:** Wired the Model Context Protocol (MCP) `get_behavior_metrics` JSON-RPC handler directly into the core `BehaviorTracker` to report real-time agent execution heuristics.
* **Files Modified:** [handler.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-mcp/handler.go)

### 3. Declarative Policy Configuration (`doktri-policy.yaml`)
* **Features:** Removed hardcoded variables inside `policy.go` and replaced them with a runtime-configurable YAML policy loader. Allows administrators to change security mode thresholds dynamically on startup.
* **Files Created/Modified:** [doktri-policy.yaml](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktri-policy.yaml), [config.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-core/config.go), [policy.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-core/policy.go)

### 4. Alert Webhooks & Slack Integration
* **Features:** Implemented an HTTP webhook notifier inside `notifier.go` that triggers alerts on:
  * Policy Trigger Event (PTE) creation, approval, rejection, and expiration.
  * Runtime replica drift detections (mismatch between desired and actual count).
* **Files Created/Modified:** [notifier.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-core/notifier.go), [plan.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-core/plan.go)

### 5. Workload DB Version History & Rollback
* **Features:** Upgraded the database layer to track full historical snaps of workload specifications whenever changes are submitted. Added `doktriai-cli history` and `rollback` commands.
* **Files Created/Modified:** [sqlite.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-core/storage/sqlite.go), [client.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-cli/client.go)

---

## 🚀 Phase 2: Operational Power & Identity (Complete)

### 1. Rolling Upgrade deployment Strategies (F2.1)
* **Features:** Added support for sequential container deployment rolling updates (`rolling`) vs wholesale deletion (`recreate`) strategies based on version/image changes.
* **Schema Parity:** Synchronized SQLite and Postgres database engines to write and parse `deploy_strategy`, `max_surge`, and `max_unavailable` attributes.
* **Files Modified:** [postgres.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-core/storage/postgres.go), [docker.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-runtime/docker.go)

### 2. Declarative `.doktri` DSL Manifests (F2.2)
* **Features:** Introduced a YAML-based deployment spec parser. Allows deploying complex workloads directly via:
  ```bash
  doktriai-cli deploy -f manifest.yaml
  ```
* **Files Created:** [manifest.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-packages/manifest.go)

### 3. Per-Agent JWT Identity Validation (F2.5)
* **Features:** Configured a secure stateless agent authentication model using JWT tokens (`golang-jwt/jwt/v5`). Includes `POST /api/agents/issue-token` for minting credentials.
* **Files Modified:** [auth.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-core/auth.go), [server.go](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/doktriai-api/server.go)

---

## 📈 Monitoring Stack Integration (Prometheus & Grafana)

To visualize these metrics, we configured a containerized monitoring stack in the project root:
* [prometheus.yml](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/prometheus.yml) - Configures the scraper to pull from `/api/metrics`.
* [docker-compose.yml](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/docker-compose.yml) - Provisions Prometheus and Grafana on port `:3000`.
* [grafana-dashboard.json](file:///c:/Users/prafu/OneDrive/Documents/Placement/DevOps/DoktriAI-control-plane/grafana-dashboard.json) - Custom, pre-configured dashboard JSON containing panels for desired vs actual state drift, rogue agent indicators, and loop reconcile speed.

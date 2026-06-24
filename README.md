# DOKTRIAI Control Plane

Autonomous control plane for declarative container clusters. `doktriai-api` coordinates multi-tenant workload requests, and `doktriai-core` reconciles desired state against local and remote Docker runtimes.

## Platform Modules

| Module | Role |
|---|---|
| `doktriai-api` | REST & gRPC gateway — auth, audit, SSE streaming |
| `doktriai-core` | Reconciler engine — drift detection, scheduling, events |
| `doktriai-runtime` | Universal driver — Docker, Kubernetes, Podman, bare-metal |
| `doktriai-operator` | GitOps Kubernetes bridge — CRDs, canary, drift alerts |
| `doktriai-mcp` | AI agent gateway — Claude, GPT, multi-agent automation |
| `doktriai-cli` | Terminal UX — deploy, logs, diagnose from shell |
| `doktriai-packages` | Shared types, SDK, domain models |
| `doktriai-charts` | Official Helm charts for cluster deployment |
| `doktriai-examples` | Quickstarts, agent scripts, reference architectures |

## Requirements

- Go 1.22+
- Docker Desktop or Docker Engine (available as `docker` on PATH)

## Run the Control Plane

```powershell
go run ./cmd/doktriai-api -addr :18080
```

Open the web workspace:

```
http://localhost:18080
```

## CLI Usage

```powershell
# List running workloads
go run ./cmd/doktriai-api -- list

# Deploy a container workload
doktriai-cli apply -f app.doktri

# Check system status
doktriai-cli status

# Stream container logs
doktriai-cli logs hello-web --tail=50

# Delete a workload
doktriai-cli delete workload hello-web

# List MCP agent tools
doktriai-cli mcp-tools
```

## REST API

```powershell
# Health check
Invoke-RestMethod http://localhost:18080/api/health

# Deploy a Docker workload
Invoke-RestMethod http://localhost:18080/api/workloads `
  -Method POST `
  -ContentType application/json `
  -Body '{"name":"hello-nginx","image":"nginx:alpine","replicas":1,"port":8080,"containerPort":80,"runtime":"docker"}'

# List all workloads (desired + actual state)
Invoke-RestMethod http://localhost:18080/api/workloads

# Force reconciliation loop
Invoke-RestMethod http://localhost:18080/api/reconcile -Method POST

# Delete workload and containers
Invoke-RestMethod http://localhost:18080/api/workloads/hello-nginx -Method DELETE

# Stream live events (SSE)
curl http://localhost:18080/api/events

# Fetch workload logs
Invoke-RestMethod http://localhost:18080/api/logs/hello-nginx
```

## MCP Agent Integration

```powershell
# List available agent tools
Invoke-RestMethod http://localhost:18080/api/mcp `
  -Method POST `
  -ContentType application/json `
  -Body '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

`tools/call` supports:

- `deploy_workload` — deploy a container via AI agent
- `list_workloads` — fetch current workload state
- `delete_workload` — decommission a workload
- `get_logs` — stream container logs

Generate a signed HMAC auth token in the web UI → **CLI Reference → Secure Token Generator** and attach it as `X-Doktri-Token` header.

## Security Layers

| Layer | Purpose |
|---|---|
| Layer 0 | Token auth — HMAC-SHA256 signed tokens |
| Layer 1 | Intent guard — Unicode normalisation, allowlist, Base64 scan |
| Layer 2 | PTE Gate — high-risk actions held for human approval |
| Layer 3 | Behavioral anomaly detection — rogue agent flagging |
| Layer 4 | Audit trail — SHA256 state diff hashing per mutation |

High-risk deploys (replicas > 5, sensitive env keys, all deletes) are gated at **Layer 2 — Plan-Then-Execute**. Navigate to the web UI → **Security → Pending Approvals** to approve or reject.

## Architecture

```text
Developer / AI Agent
        │
   doktriai-cli  |  doktriai-mcp
        │
   doktriai-api          ← REST/gRPC, auth, audit, SSE
        │
   doktriai-core         ← reconcile, schedule, policy, events
        │
   ┌────┼────────────┐
   ▼    ▼            ▼
runtime  operator   packages
   │
Docker · K8s · Podman · Remote · Compose
```

## Web Workspace

The web workspace at `http://localhost:18080` provides:
- **Overview** — live stats, platform modules, quick-start buttons
- **Workloads** — deploy and manage container workloads via form
- **Projects** — view and connect all platform modules
- **Documentation** — step-by-step guides, CLI & API reference, security docs
- **Security** — PTE approval gate, behavioral anomaly metrics, audit trail
- **Activity** — real-time SSE event stream and security audit trail
- **Gallery** — architecture connection diagram
- **Notes** — persistent workspace notes
- **Settings** — RBAC role simulation

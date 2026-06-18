# KRANIX-IO Control Plane

This is a Kranix-like control plane with a real Go backend and a workspace UI styled after the referenced Kranix portal.

It includes:

- `kranix-api`: Go HTTP API for workloads, audit, SSE, logs, health, and MCP-style JSON-RPC.
- `kranix-core`: desired-vs-actual reconciliation loop, policy validation, audit records, and event bus.
- `kranix-runtime`: Docker CLI driver using Kranix labels on real containers.
- `kranix-web`: static workspace UI served by the Go API.

There is no seeded browser data. Desired state starts empty and is persisted in `data/state.json`.

## Requirements

- Go 1.22+
- Docker Desktop or Docker Engine available as `docker`

Go was not installed in this local environment when the project was generated, so build verification must be run after installing Go.

## Run

```powershell
cd "C:\Users\prafu\OneDrive\Documents\New project\kronix-control-plane"
go run ./cmd/kranix-api -addr :18080
```

Open:

```text
http://localhost:18080
```

## CLI

```powershell
go run ./cmd/kranix-cli -- list
go run ./cmd/kranix-cli -- deploy hello-nginx nginx:alpine 1 8080 80
go run ./cmd/kranix-cli -- logs hello-nginx
go run ./cmd/kranix-cli -- delete hello-nginx
```

## API

```powershell
# Health
Invoke-RestMethod http://localhost:18080/api/health

# Deploy a real Docker workload
Invoke-RestMethod http://localhost:18080/api/workloads `
  -Method POST `
  -ContentType application/json `
  -Body '{"name":"hello-nginx","image":"nginx:alpine","replicas":1,"port":8080,"containerPort":80,"runtime":"docker"}'

# List desired and actual state
Invoke-RestMethod http://localhost:18080/api/workloads

# Force reconciliation
Invoke-RestMethod http://localhost:18080/api/reconcile -Method POST

# Delete workload and containers
Invoke-RestMethod http://localhost:18080/api/workloads/hello-nginx -Method DELETE
```

## MCP-style JSON-RPC

```powershell
Invoke-RestMethod http://localhost:18080/api/mcp `
  -Method POST `
  -ContentType application/json `
  -Body '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

`tools/call` supports:

- `deploy_workload`
- `list_workloads`
- `delete_workload`
- `get_logs`

## Architecture

```text
Developer / AI Agent
        │
   kranix-cli  |  kranix-mcp
        │
   kranix-api          ← REST/gRPC, auth, audit, SSE
        │
   kranix-core         ← reconcile, schedule, policy, events
        │
   ┌────┼────────────┐
   ▼    ▼            ▼
runtime  operator   packages
   │
Docker · K8s · Podman · Remote · Compose
```

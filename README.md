# 🎛️ DoktriAI: The AI-Native GitOps Control Plane

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22%2B-blue?style=for-the-badge&logo=go" alt="Go Version" />
  <img src="https://img.shields.io/badge/Docker-Compatible-blue?style=for-the-badge&logo=docker" alt="Docker Support" />
  <img src="https://img.shields.io/badge/MCP-Supported-purple?style=for-the-badge&logo=modelcontextprotocol" alt="MCP Support" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License" />
</p>

**DoktriAI** is a self-hosted, lightweight, and AI-native GitOps control plane. Think of it as a local, zero-dependency alternative to Heroku, Vercel, or Render, built specifically to let **AI Coding Agents (like Cursor, Windsurf, or Claude)** deploy, monitor, and self-heal container workloads directly on your infrastructure.

---

## ✨ Core Features

* 🤖 **AI-Native MCP Server**: Native Model Context Protocol (MCP) standard support. Hook it directly into **Cursor, Windsurf, or Claude Desktop** so your AI agent can deploy apps, read logs, and diagnose errors autonomously.
* 🛡️ **Zero-Trust Security Gate**: 5-layer security check (including Open Policy Agent (OPA) policy rules, Unicode normalization, intent scan, and human-in-the-loop **Plan-Then-Execute (PTE) approval gates**).
* 🔄 **GitOps & Preview Environments**: Connect GitHub Webhooks to spin up isolated, lease-based preview environments per Pull Request. Expired workloads are automatically reaped.
* ⚡ **Declarative Reconciler Engine**: A lightweight Go engine (`doktriai-core`) that acts as a mini-Kubernetes, checking desired state against Docker or Podman and auto-correcting any container drift.
* 🎛️ **Premium Web Console**: A sleek, dark-mode terminal IDE layout showing real-time SSE stream events, container status, and audit trails.

---

## 🚀 1-Minute Quickstart

### Option A: Run via Docker (Recommended)
Spin up DoktriAI instantly and mount your local Docker daemon to orchestrate containers:

```bash
docker run -d -p 18080:18080 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v doktriai-data:/app/data \
  --name doktriai \
  ghcr.io/praful224/doktriai:latest
```

### Option B: Run via Go
Clone and run the Go API server locally:

```bash
go run ./cmd/doktriai-api -addr :18080
```

Once running, open the visual console at:
👉 **[http://localhost:18080](http://localhost:18080)**

---

## 🤖 Connect to your AI Editor (Cursor / Windsurf)

You can allow your AI assistant to deploy and manage containers on your machine. Add DoktriAI as an MCP server:

### Cursor Setup
1. Open Cursor **Settings** -> **Features** -> **MCP**.
2. Click **+ Add New MCP Server**.
3. Configure:
   * **Name**: `doktriai`
   * **Type**: `command`
   * **Command**: `doktriai-cli mcp-bridge` (or point to `go run ./cmd/doktriai-cli/main.go mcp-bridge`)

---

## 🛠️ Developer CLI Reference

DoktriAI comes with a CLI (`doktriai-cli`) to interact with your control plane from the command line:

```bash
# List active workloads
doktriai-cli list

# Apply/Deploy a container workload manifest
doktriai-cli apply -f my-workload.yaml

# Check system health & reconciler loops
doktriai-cli status

# Stream container logs
doktriai-cli logs my-service --tail=50

# List active agent tools exposed to MCP
doktriai-cli mcp-tools
```

---

## 🛡️ 5-Layer Security Blueprint

DoktriAI secures AI-agent actions using an advanced multi-layered gate:

| Layer | Security Gate | What it prevents |
|---|---|---|
| **Layer 0** | Token Authentication | Unauthorized API requests via signed HMAC-SHA256 tokens |
| **Layer 1** | Intent & Sanitization | Injection attacks, directory traversal, and unicode bypasses |
| **Layer 2** | Plan-Then-Execute (PTE) | Human-in-the-loop validation for high-risk actions (e.g. replica count > 5, deletes) |
| **Layer 3** | Anomaly Detection | Rogue AI agents spamming the deployment control plane |
| **Layer 4** | State Audit Trail | Tamper-proof SHA-256 historical hashing on desired state logs |

---

## 🏗️ Architecture Mapping

```text
       Developer / AI Agent (Cursor, Windsurf, Claude)
                           │
       ┌───────────────────┴───────────────────┐
       ▼                                       ▼
  doktriai-cli                           doktriai-mcp (JSON-RPC)
       │                                       │
       └───────────────────┬───────────────────┘
                           ▼
                     doktriai-api (REST, Auth, Audit, SSE Stream)
                           │
                     doktriai-core (Reconciler, DAG Sorter, Policy)
                           │
             ┌─────────────┼─────────────┐
             ▼             ▼             ▼
          Runtime       Operator      Packages
             │
   Docker · K8s · Podman
```

---

## 📜 License

Distributed under the MIT License. See `LICENSE` for more information.

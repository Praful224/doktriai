const api = {
  role: "admin",
  headers() {
    return { "Content-Type": "application/json", "X-Doktri-Role": this.role, "X-Doktri-Actor": `web:${this.role}` };
  },
  async json(path, options = {}) {
    const res = await fetch(path, { ...options, headers: { ...this.headers(), ...(options.headers || {}) } });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error || res.statusText);
    return body;
  }
};

const state = { workloads: [], events: [], audit: [], plans: [], behaviorMetrics: [] };
const qs = (selector) => document.querySelector(selector);
const qsa = (selector) => [...document.querySelectorAll(selector)];

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function switchView(view, updateHash = true) {
  qsa(".view").forEach((item) => item.classList.remove("active-view"));
  qsa(".tree,.tab").forEach((item) => item.classList.toggle("active", item.dataset.view === view));
  const target = qs(`#${view}`);
  if (target) target.classList.add("active-view");
  if (view === "runtime") {
    drawArchitecture();
    renderRuntimeView();
  }
  if (view === "core") renderCoreView();
  if (view === "packages") renderPackagesView();
  if (view === "examples") renderExamplesView();
  if (updateHash) {
    window.location.hash = "/" + view;
  }
}

function handleRouting() {
  const hash = window.location.hash.replace("#/", "") || "overview";
  const target = qs(`#${hash}`);
  if (target) {
    switchView(hash, false);
  }
}

window.addEventListener("hashchange", handleRouting);

async function refreshAll() {
  try {
    state.workloads = await api.json("/api/workloads");
    state.audit = await api.json("/api/audit");
    
    // PTE pending plans (Layer 2)
    try { state.plans = await api.json("/api/plan") || []; } catch { state.plans = []; }
    // Behavior metrics (Layer 3)
    try { state.behaviorMetrics = await api.json("/api/behavior") || []; } catch { state.behaviorMetrics = []; }
    
    let lockState = { locked: false };
    try {
      lockState = await api.json("/api/lock");
    } catch (e) {
      // ignore
    }
    
    renderWorkloads();
    renderAudit();
    renderPendingPlans();
    renderBehaviorMetrics();
    
    // Live update overview stat cards
    const wlCountEl = qs("#overviewWorkloadsCount");
    if (wlCountEl) wlCountEl.textContent = `${state.workloads.length} running`;
    
    const driftCountEl = qs("#overviewDriftCount");
    if (driftCountEl) {
      const deviations = state.workloads.filter(w => !w.healthy).length;
      driftCountEl.textContent = `${deviations} deviations`;
    }
    
    // If current active view is 'core' or 'runtime', refresh them too
    const activeView = qs(".view.active-view");
    if (activeView) {
      if (activeView.id === "core") renderCoreView();
      if (activeView.id === "runtime") renderRuntimeView();
    }
    
    qs("#statusText").textContent = "reconciler online";
    const dot = qs("#connectionDot");
    if (dot) dot.classList.remove("offline");
    
    // Update pending plan badge
    const pendingCount = (state.plans || []).filter(p => p.status === "pending").length;
    const planBadge = qs("#planBadge");
    if (planBadge) {
      planBadge.textContent = pendingCount;
      planBadge.style.display = pendingCount > 0 ? "inline-flex" : "none";
    }
    
    // Update Lock UI
    const lockDot = qs("#lockDot");
    const lockStatusText = qs("#lockStatusText");
    if (lockDot && lockStatusText) {
      if (lockState.locked) {
        lockDot.style.background = "var(--amber)";
        lockDot.style.boxShadow = "0 0 10px rgba(255, 157, 0, 0.4)";
        lockStatusText.textContent = `locked (${lockState.acquiredBy})`;
      } else {
        lockDot.style.background = "var(--green)";
        lockDot.style.boxShadow = "var(--glow-emerald)";
        lockStatusText.textContent = "system unlocked";
      }
    }
    
    qs("#mcpStatus").textContent = "MCP ready · alpha-v1";
  } catch (error) {
    qs("#statusText").textContent = "api offline";
    const dot = qs("#connectionDot");
    if (dot) dot.classList.add("offline");
    writeTerminal(`error: ${error.message}`);
  }
}

function renderWorkloads() {
  const rows = qs("#workloadRows");
  if (!rows) return;
  rows.innerHTML = "";
  if (!state.workloads.length) {
    rows.innerHTML = `<tr><td colspan="7">No declared workloads. Use the deploy form, terminal, CLI, or MCP.</td></tr>`;
    qs("#issueCount").textContent = "0";
    qs("#issueCountBadge").classList.add("healthy");
    return;
  }
  let issues = 0;
  for (const item of state.workloads) {
    if (!item.healthy) issues++;
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td><strong>${escapeHtml(item.spec.name)}</strong></td>
      <td>${escapeHtml(item.spec.image)}</td>
      <td>${item.spec.replicas}</td>
      <td>${item.actual ? item.actual.length : 0}</td>
      <td>${escapeHtml(item.spec.runtime)}</td>
      <td class="${item.healthy ? "ok" : "danger"}">${escapeHtml(item.drift || "healthy")}</td>
      <td><button class="row-action" data-delete="${escapeHtml(item.spec.name)}">delete</button></td>
    `;
    rows.appendChild(tr);
  }
  qs("#issueCount").textContent = String(issues);
  if (issues === 0) {
    qs("#issueCountBadge").classList.add("healthy");
  } else {
    qs("#issueCountBadge").classList.remove("healthy");
  }
}

function renderAudit() {
  const lists = qsa("#auditList, #securityAuditList");
  if (lists.length === 0) return;
  lists.forEach(list => {
    list.innerHTML = state.audit.length ? "" : `<div class="audit-line">No audit records yet.</div>`;
    for (const item of state.audit.slice(0, 60)) {
      const line = document.createElement("div");
      line.className = "audit-line";
      const secBadge = item.signatureVerified
        ? `<span class="badge-verified">✓ signed</span>`
        : item.agentId ? `<span class="badge-dev">dev</span>` : "";
      const hashBadge = item.stateHashAfter
        ? `<span class="badge-hash" title="Hash: ${escapeHtml(item.stateHashAfter)}">🔒 hashed</span>`
        : "";
      const planBadge = item.planApproved
        ? `<span class="badge-approved">✓ approved</span>`
        : "";
      line.innerHTML = `
        <strong>${escapeHtml(item.action)}</strong> ${escapeHtml(item.workload || "")} · 
        ${escapeHtml(item.actor)} · 
        ${item.allowed ? "<span class='ok'>allowed</span>" : "<span class='danger'>blocked</span>"}
        ${secBadge}${hashBadge}${planBadge}
        <small>${escapeHtml(item.reason || "")}</small>
      `;
      list.appendChild(line);
    }
  });
}

// ── PTE Plan Gate UI (Layer 2 — ASI09) ──────────────────────────────────────

function renderPendingPlans() {
  const container = qs("#pendingPlansContainer");
  if (!container) return;
  const plans = (state.plans || []);
  const pending = plans.filter(p => p.status === "pending");
  if (pending.length === 0) {
    container.innerHTML = `<div class="plan-empty">No pending approvals — all agent actions are within safe auto-apply thresholds.</div>`;
    return;
  }
  container.innerHTML = "";
  for (const plan of pending) {
    const expiresIn = Math.max(0, Math.round((new Date(plan.expiresAt) - Date.now()) / 60000));
    const card = document.createElement("div");
    card.className = "plan-card";
    card.innerHTML = `
      <div class="plan-header">
        <span class="plan-id">${escapeHtml(plan.id)}</span>
        <span class="plan-expiry">⏱ ${expiresIn}m remaining</span>
      </div>
      <div class="plan-meta">
        <strong>Requested by:</strong> ${escapeHtml(plan.requestedBy)}
        ${plan.agentId ? `· <strong>Agent:</strong> ${escapeHtml(plan.agentId)}` : ""}
        ${plan.agentGoal ? `· <strong>Goal:</strong> ${escapeHtml(plan.agentGoal)}` : ""}
      </div>
      <div class="plan-reason">⚠ ${escapeHtml(plan.approvalReason)}</div>
      <div class="plan-spec">
        <span><strong>Workload:</strong> ${escapeHtml(plan.spec.name)}</span>
        <span><strong>Image:</strong> ${escapeHtml(plan.spec.image)}</span>
        <span><strong>Replicas:</strong> ${plan.spec.replicas}</span>
      </div>
      <div class="plan-actions">
        <button class="plan-approve-btn" data-plan-id="${escapeHtml(plan.id)}">✓ Approve &amp; Apply</button>
        <button class="plan-reject-btn" data-plan-id="${escapeHtml(plan.id)}">✗ Reject</button>
      </div>
    `;
    container.appendChild(card);
  }

  // Bind approve/reject buttons
  container.querySelectorAll(".plan-approve-btn").forEach(btn => {
    btn.addEventListener("click", async () => {
      try {
        const res = await api.json(`/api/plan/${encodeURIComponent(btn.dataset.planId)}/approve`, { method: "POST", body: "{}" });
        writeTerminal(`Plan ${btn.dataset.planId} approved and applied: ${res.status}`);
        await refreshAll();
      } catch (err) {
        writeTerminal(`Approve error: ${err.message}`);
      }
    });
  });
  container.querySelectorAll(".plan-reject-btn").forEach(btn => {
    btn.addEventListener("click", async () => {
      const comment = prompt("Rejection reason (optional):") || "";
      try {
        await api.json(`/api/plan/${encodeURIComponent(btn.dataset.planId)}/reject`, { method: "POST", body: JSON.stringify({ comment }) });
        writeTerminal(`Plan ${btn.dataset.planId} rejected.`);
        await refreshAll();
      } catch (err) {
        writeTerminal(`Reject error: ${err.message}`);
      }
    });
  });
}

// ── Behavioral Metrics UI (Layer 3 — ASI10) ─────────────────────────────────

function renderBehaviorMetrics() {
  const container = qs("#behaviorMetricsContainer");
  if (!container) return;
  const metrics = state.behaviorMetrics || [];
  if (metrics.length === 0) {
    container.innerHTML = `<div class="plan-empty">No behavioral data yet — metrics populate as actors make API calls.</div>`;
    return;
  }
  container.innerHTML = "";
  for (const m of metrics) {
    const row = document.createElement("div");
    row.className = `behavior-row${m.flagged ? " flagged" : ""}`;
    const bar = Math.min(100, Math.round(m.anomalyScore * 100));
    row.innerHTML = `
      <div class="behavior-actor">${escapeHtml(m.actor)} ${m.flagged ? "<span class='danger'>⚠ ANOMALY</span>" : ""}</div>
      <div class="behavior-stats">
        <span>deploys: ${m.deployCount}</span>
        <span>deletes: ${m.deleteCount}</span>
        <span>rejects: ${m.rejectCount}</span>
      </div>
      <div class="behavior-bar-wrap">
        <div class="behavior-bar" style="width:${bar}%; background: ${m.flagged ? "var(--red)" : "var(--green)"}"></div>
      </div>
      <div class="behavior-score">anomaly score: ${m.anomalyScore.toFixed(2)}</div>
    `;
    container.appendChild(row);
  }
}

function renderEvents() {
  const list = qs("#eventList");
  if (!list) return;
  list.innerHTML = state.events.length ? "" : `<div class="event-line">Waiting for SSE events...</div>`;
  for (const item of state.events.slice(0, 80)) {
    const line = document.createElement("div");
    line.className = "event-line";
    line.innerHTML = `<strong>${escapeHtml(item.source)}</strong> ${escapeHtml(item.level)} · ${escapeHtml(item.workload || "system")} · ${escapeHtml(item.message)}`;
    list.appendChild(line);
  }
}

async function renderCoreView() {
  const rows = qs("#coreDriftRows");
  if (!rows) return;
  
  try {
    const health = await api.json("/api/health");
    const circuits = health.circuits || {};
    const circuitKeys = Object.keys(circuits);
    const cbDisplay = qs("#circuitBreakerDisplay");
    if (cbDisplay) {
      if (circuitKeys.length === 0) {
        cbDisplay.textContent = "Healthy (0 open)";
        cbDisplay.style.color = "var(--green)";
      } else {
        const openCount = circuitKeys.filter(k => circuits[k].open).length;
        cbDisplay.textContent = `${openCount} Open / ${circuitKeys.length} Total`;
        cbDisplay.style.color = openCount > 0 ? "var(--danger)" : "var(--green)";
      }
    }
    
    rows.innerHTML = "";
    if (state.workloads.length === 0) {
      rows.innerHTML = `<tr><td colspan="5">No declared workloads in reconciler loop.</td></tr>`;
      return;
    }
    
    state.workloads.forEach(wl => {
      const circ = circuits[wl.spec.name] || { failures: 0, open: false };
      const tr = document.createElement("tr");
      tr.innerHTML = `
        <td><strong>${escapeHtml(wl.spec.name)}</strong></td>
        <td>replicas=${wl.spec.replicas} image=${escapeHtml(wl.spec.image)}</td>
        <td>actual=${wl.actual ? wl.actual.length : 0}</td>
        <td class="${wl.healthy ? "ok" : "danger"}">${escapeHtml(wl.drift || "healthy (reconciled)")}</td>
        <td>
          <span class="${circ.open ? "danger" : "ok"}">${circ.open ? "OPEN (paused)" : "Closed (active)"}</span>
          <small style="display:block; color:var(--muted); font-size:10px;">Failures: ${circ.failures}</small>
        </td>
      `;
      rows.appendChild(tr);
    });
  } catch (err) {
    rows.innerHTML = `<tr><td colspan="5" class="danger">Error loading reconciler state: ${escapeHtml(err.message)}</td></tr>`;
  }
}

async function renderRuntimeView() {
  const rows = qs("#runtimeContainerRows");
  if (!rows) return;
  
  try {
    const status = await api.json("/api/runtime/status");
    
    const dockerCard = qs("#dockerDriverCard span");
    const dockerDetail = qs("#dockerDriverDetail");
    if (status.docker) {
      if (dockerCard) {
        dockerCard.textContent = status.docker.simulated ? "Active (Simulated Mode)" : "Active (Connected)";
      }
      if (dockerDetail) {
        dockerDetail.textContent = status.docker.simulated 
          ? "Docker daemon offline; simulated engine active" 
          : `Connected via binary: ${status.docker.binary}`;
      }
    }
    
    rows.innerHTML = "";
    const containers = status.containers || [];
    if (containers.length === 0) {
      rows.innerHTML = `<tr><td colspan="6">No container processes observed by runtime drivers.</td></tr>`;
      return;
    }
    
    containers.forEach(c => {
      const tr = document.createElement("tr");
      tr.innerHTML = `
        <td><code style="font-family:monospace; color:var(--orange);">${escapeHtml(c.id.substring(0, 12))}</code></td>
        <td><strong>${escapeHtml(c.name)}</strong></td>
        <td>${c.replica}</td>
        <td>${escapeHtml(c.image)}</td>
        <td>${escapeHtml(c.runtime)}</td>
        <td><span class="ok">${escapeHtml(c.status)}</span></td>
      `;
      rows.appendChild(tr);
    });
  } catch (err) {
    rows.innerHTML = `<tr><td colspan="6" class="danger">Error loading containers status: ${escapeHtml(err.message)}</td></tr>`;
  }
}

let schemaFields = [];
async function renderPackagesView() {
  const rows = qs("#schemaTableRows");
  if (!rows) return;
  
  try {
    if (schemaFields.length === 0) {
      schemaFields = await api.json("/api/schema");
    }
    filterAndRenderSchema();
  } catch (err) {
    rows.innerHTML = `<tr><td colspan="5" class="danger">Error loading WorkloadSpec schema: ${escapeHtml(err.message)}</td></tr>`;
  }
}

function filterAndRenderSchema() {
  const rows = qs("#schemaTableRows");
  if (!rows) return;
  
  const query = (qs("#schemaSearchInput")?.value || "").trim().toLowerCase();
  rows.innerHTML = "";
  
  const filtered = schemaFields.filter(f => 
    f.name.toLowerCase().includes(query) || 
    f.type.toLowerCase().includes(query) ||
    f.description.toLowerCase().includes(query)
  );
  
  if (filtered.length === 0) {
    rows.innerHTML = `<tr><td colspan="5">No fields matched the search criteria.</td></tr>`;
    return;
  }
  
  filtered.forEach(f => {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td><strong style="font-family:monospace; color:var(--orange);">${escapeHtml(f.name)}</strong></td>
      <td><code style="font-family:monospace;">${escapeHtml(f.type)}</code></td>
      <td>${f.required ? "<span class='danger'>Yes</span>" : "No"}</td>
      <td style="font-size:12px; color:var(--text);">${escapeHtml(f.description)}</td>
      <td><code style="font-family:monospace; font-size:11px; background:rgba(0,0,0,0.2); padding:2px 4px; border-radius:3px;">${escapeHtml(typeof f.example === 'object' ? JSON.stringify(f.example) : f.example)}</code></td>
    `;
    rows.appendChild(tr);
  });
}

const examplesData = {
  python: `import json
import requests

# DOKTRIAI REST API & MCP Agent Example
API_URL = "http://localhost:18080"
TOKEN = "your-hmac-token-here"  # Generate in CLI Reference view

headers = {
    "Content-Type": "application/json",
    "X-Doktri-Token": TOKEN
}

# Apply a declarative workload spec
payload = {
    "name": "mcp-agent-web",
    "image": "nginx:alpine",
    "replicas": 3,
    "port": 8080,
    "containerPort": 80,
    "securityMode": "production"
}

print("Deploying workload...")
response = requests.post(f"{API_URL}/api/workloads", headers=headers, json=payload)
print("Response:", response.status_code, response.json())
`,
  curl: `# Deploy a workload manifest
curl -X POST http://localhost:18080/api/workloads \\
  -H "Content-Type: application/json" \\
  -H "X-Doktri-Role: admin" \\
  -d '{
    "name": "curl-test-app",
    "image": "nginx:alpine",
    "replicas": 2,
    "port": 9000,
    "containerPort": 80
  }'

# Force reconcile loop execution
curl -X POST http://localhost:18080/api/reconcile

# List running workloads
curl http://localhost:18080/api/workloads

# Fetch logs from workload
curl http://localhost:18080/api/logs/curl-test-app?tail=50
`,
  go: `package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type WorkloadSpec struct {
	Name          string            \`json:"name"\`
	Image         string            \`json:"image"\`
	Replicas      int               \`json:"replicas"\`
	Port          int               \`json:"port"\`
	ContainerPort int               \`json:"containerPort"\`
}

func main() {
	spec := WorkloadSpec{
		Name:          "go-client-app",
		Image:         "redis:alpine",
		Replicas:      1,
		Port:          6379,
		ContainerPort: 6379,
	}

	body, _ := json.Marshal(spec)
	req, _ := http.NewRequest("POST", "http://localhost:18080/api/workloads", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Doktri-Role", "admin")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	fmt.Println("Deploy response status:", resp.Status)
}
`
};

function renderExamplesView(tab = "python") {
  const container = qs("#exampleCodePreview");
  if (!container) return;
  
  qsa(".example-tab-btn, #exampleTabPython, #exampleTabCurl, #exampleTabGo").forEach(btn => {
    const btnId = btn.id || "";
    const isCurrent = (tab === "python" && btnId === "exampleTabPython") ||
                      (tab === "curl" && btnId === "exampleTabCurl") ||
                      (tab === "go" && btnId === "exampleTabGo");
    btn.classList.toggle("active", isCurrent);
  });
  
  container.textContent = examplesData[tab] || "Select an example.";
}

function writeTerminal(message) {
  const body = qs("#terminalBody");
  if (!body) return;
  const p = document.createElement("p");
  p.innerHTML = escapeHtml(message);
  body.appendChild(p);
  body.scrollTop = body.scrollHeight;
}

async function deployFromForm(form) {
  const data = Object.fromEntries(new FormData(form).entries());
  const payload = {
    name: data.name,
    image: data.image,
    replicas: Number(data.replicas),
    port: Number(data.port || 0),
    containerPort: Number(data.containerPort || 0),
    runtime: "docker"
  };
  const result = await api.json("/api/workloads", { method: "POST", body: JSON.stringify(payload) });
  if (result.status === "pending_approval") {
    writeTerminal(`⏳ PTE Gate: workload requires approval. Plan ID: ${result.planId}`);
    writeTerminal(`   Reason: ${result.approvalReason}`);
    writeTerminal(`   Go to Security → Pending Approvals to approve or reject.`);
  } else {
    writeTerminal(`deploy accepted: ${payload.name}`);
  }
  form.reset();
  await refreshAll();
}

async function runTerminalCommand(raw) {
  const parts = raw.trim().split(/\s+/).filter(Boolean);
  if (!parts.length) return;
  const [command, ...args] = parts;
  writeTerminal(`› ${raw}`);
  try {
    if (command === "help") {
      writeTerminal("commands: status, workloads, reconcile, deploy <name> <image> <replicas> <port> <containerPort>, delete <name>, logs <name>, mcp-tools");
    } else if (command === "status") {
      const health = await api.json("/api/health");
      writeTerminal(JSON.stringify(health));
    } else if (command === "workloads") {
      await refreshAll();
      writeTerminal(JSON.stringify(state.workloads));
    } else if (command === "reconcile") {
      await api.json("/api/reconcile", { method: "POST", body: "{}" });
      writeTerminal("reconcile requested");
    } else if (command === "deploy") {
      const [name, image, replicas = "1", port = "0", containerPort = "0"] = args;
      await api.json("/api/workloads", {
        method: "POST",
        body: JSON.stringify({ name, image, replicas: Number(replicas), port: Number(port), containerPort: Number(containerPort), runtime: "docker" })
      });
      writeTerminal(`deploy accepted: ${name}`);
    } else if (command === "delete") {
      await api.json(`/api/workloads/${encodeURIComponent(args[0])}`, { method: "DELETE" });
      writeTerminal(`deleted: ${args[0]}`);
    } else if (command === "logs") {
      const logs = await api.json(`/api/logs/${encodeURIComponent(args[0])}?tail=80`);
      writeTerminal(logs.join("\n") || "no logs");
    } else if (command === "mcp-tools") {
      const rpc = await callRPC("tools/list");
      writeTerminal(JSON.stringify(rpc.result));
    } else {
      writeTerminal(`unknown command: ${command}`);
    }
    await refreshAll();
  } catch (error) {
    writeTerminal(`error: ${error.message}`);
  }
}

async function callRPC(method, params) {
  return api.json("/api/mcp", { method: "POST", body: JSON.stringify({ jsonrpc: "2.0", id: Date.now(), method, params }) });
}

async function callTool(name) {
  let args = {};
  if (name === "get_logs") {
    const first = state.workloads[0]?.spec?.name || "";
    args = { name: first, tail: 80 };
  }
  return callRPC("tools/call", { name, arguments: args });
}

function connectEvents() {
  const events = new EventSource("/api/events");
  events.onmessage = (message) => pushEvent(message.data);
  events.addEventListener("api", (message) => pushEvent(message.data));
  events.addEventListener("core", (message) => pushEvent(message.data));
  events.addEventListener("runtime", (message) => pushEvent(message.data));
  events.onerror = () => {
    qs("#statusText").textContent = "event stream retrying";
  };
}

function pushEvent(raw) {
  try {
    state.events.unshift(JSON.parse(raw));
    state.events = state.events.slice(0, 100);
    renderEvents();
  } catch {
    // Ignore malformed event payloads
  }
}

function drawArchitecture() {
  const canvas = qs("#architectureCanvas");
  if (!canvas) return;
  const ctx = canvas.getContext("2d");
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.fillStyle = "#07080c";
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  const nodes = [
    ["Developer / AI Agent", 550, 50],
    ["doktriai-cli | doktriai-mcp", 550, 125],
    ["doktriai-api", 550, 200],
    ["doktriai-core", 550, 275],
    ["doktriai-runtime", 300, 360],
    ["operator", 550, 360],
    ["packages", 800, 360]
  ];
  ctx.strokeStyle = "#00d2ff";
  ctx.fillStyle = "#e2e8f0";
  ctx.lineWidth = 2;
  for (let i = 0; i < nodes.length - 3; i++) line(ctx, nodes[i][1], nodes[i][2] + 24, nodes[i + 1][1], nodes[i + 1][2] - 24);
  line(ctx, 550, 300, 300, 336);
  line(ctx, 550, 300, 550, 336);
  line(ctx, 550, 300, 800, 336);
  nodes.forEach(([label, x, y]) => box(ctx, x, y, label));
}

function line(ctx, x1, y1, x2, y2) {
  ctx.beginPath();
  ctx.moveTo(x1, y1);
  ctx.lineTo(x2, y2);
  ctx.stroke();
}

function box(ctx, x, y, label) {
  ctx.fillStyle = "#111622";
  ctx.strokeStyle = "#202738";
  ctx.fillRect(x - 130, y - 24, 260, 48);
  ctx.strokeRect(x - 130, y - 24, 260, 48);
  ctx.fillStyle = "#e2e8f0";
  ctx.textAlign = "center";
  ctx.font = "13px JetBrains Mono, monospace";
  ctx.fillText(label, x, y + 5);
}

// Command Search / Routing Palette Logic
const searchInput = qs("#commandSearch");
const searchResults = qs("#searchResults");

const commandPaletteItems = [
  { title: "Overview", subtitle: "Switch to workspace overview page", category: "Navigation", action: () => switchView("overview") },
  { title: "Workloads", subtitle: "Manage and deploy container workloads", category: "Navigation", action: () => switchView("workloads") },
  { title: "Reconciler Core", subtitle: "Reconcile desired state and check drift", category: "Navigation", action: () => switchView("core") },
  { title: "GitOps Operator", subtitle: "Kubernetes custom resource sync status", category: "Navigation", action: () => switchView("operator") },
  { title: "Runtime Drivers", subtitle: "Docker and K8s container process drivers", category: "Navigation", action: () => switchView("runtime") },
  { title: "CLI Reference", subtitle: "Cheat sheet and JWT auth token utility", category: "Navigation", action: () => switchView("cli") },
  { title: "Schema Browser", subtitle: "Interactive WorkloadSpec fields mapping", category: "Navigation", action: () => switchView("packages") },
  { title: "Helm Charts", subtitle: "Compile and render values.yaml for clusters", category: "Navigation", action: () => switchView("charts") },
  { title: "Examples", subtitle: "Runnable integration quickstarts & Python script", category: "Navigation", action: () => switchView("examples") },
  { title: "Security Control Plane", subtitle: "5-Layer PTE approvals and behavioral metrics", category: "Navigation", action: () => switchView("security") },
  { title: "Activity Stream", subtitle: "Inspect real-time SSE events and audit trails", category: "Navigation", action: () => switchView("activity") },
  { title: "Workspace Settings", subtitle: "Configure simulated access role headers", category: "Navigation", action: () => switchView("settings") },
  { title: "MCP AI Hub", subtitle: "Direct JSON-RPC Model Context Protocol tester", category: "Navigation", action: () => switchView("mcp") },
  { title: "Force Reconcile Loop", subtitle: "Call /api/reconcile trigger immediately", category: "Action", action: async () => { await api.json("/api/reconcile", { method: "POST", body: "{}" }); writeTerminal("Reconcile forced from search bar."); refreshAll(); } },
  { title: "Query Health Check", subtitle: "Call /api/health to inspect cluster status", category: "Action", action: async () => { const status = await api.json("/api/health"); writeTerminal("Health check: " + JSON.stringify(status)); } }
];

function handleSearchInput() {
  const query = searchInput.value.trim().toLowerCase();
  searchResults.innerHTML = "";
  if (!query) {
    searchResults.classList.remove("active");
    return;
  }

  const matchedItems = commandPaletteItems.filter(item => 
    item.title.toLowerCase().includes(query) || 
    item.subtitle.toLowerCase().includes(query) ||
    item.category.toLowerCase().includes(query)
  );

  state.workloads.forEach(w => {
    if (w.spec.name.toLowerCase().includes(query) || w.spec.image.toLowerCase().includes(query)) {
      matchedItems.push({
        title: `Workload: ${w.spec.name}`,
        subtitle: `Image: ${w.spec.image} | Replicas: ${w.spec.replicas}`,
        category: "Workload",
        action: () => {
          switchView("workloads");
          writeTerminal(`Navigated to workload details for ${w.spec.name}`);
        }
      });
    }
  });

  if (matchedItems.length === 0) {
    searchResults.innerHTML = `<div class="search-item"><span class="title">No results found for "${escapeHtml(query)}"</span></div>`;
  } else {
    matchedItems.slice(0, 6).forEach((item, idx) => {
      const div = document.createElement("div");
      div.className = "search-item";
      if (idx === 0) div.classList.add("selected");
      div.innerHTML = `
        <div>
          <span class="title">${escapeHtml(item.title)}</span>
          <span class="subtitle">${escapeHtml(item.subtitle)}</span>
        </div>
        <span class="category">${escapeHtml(item.category)}</span>
      `;
      div.addEventListener("click", () => {
        item.action();
        searchInput.value = "";
        searchResults.classList.remove("active");
      });
      searchResults.appendChild(div);
    });
  }
  searchResults.classList.add("active");
}

// Secure HMAC-SHA256 Token Generator (Layer 0 — auth testing)
async function generateHMAC(keyStr, dataStr) {
  const enc = new TextEncoder();
  const keyData = enc.encode(keyStr);
  const data = enc.encode(dataStr);
  
  const key = await window.crypto.subtle.importKey(
    "raw",
    keyData,
    { name: "HMAC", hash: { name: "SHA-256" } },
    false,
    ["sign"]
  );
  
  const signature = await window.crypto.subtle.sign(
    "HMAC",
    key,
    data
  );
  
  return Array.from(new Uint8Array(signature))
    .map(b => b.toString(16).padStart(2, "0"))
    .join("");
}

async function generateToken(actor, role, agentId, scope, goal) {
  const secret = "doktriai-dev-secret-do-not-use-in-production";
  const nonce = Math.random().toString(36).substring(2, 10);
  const ts = Math.floor(Date.now() / 1000).toString();
  
  const payload = [actor, role, agentId, scope, goal, ts, nonce].join("|");
  const signature = await generateHMAC(secret, payload);
  return payload + "|" + signature;
}

function bind() {
  qsa("[data-view]").forEach((button) => button.addEventListener("click", () => switchView(button.dataset.view)));
  
  const deployForm = qs("#deployForm");
  if (deployForm) {
    deployForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      try {
        await deployFromForm(event.currentTarget);
      } catch (error) {
        writeTerminal(`error: ${error.message}`);
      }
    });
  }

  const rows = qs("#workloadRows");
  if (rows) {
    rows.addEventListener("click", async (event) => {
      const button = event.target.closest("[data-delete]");
      if (!button) return;
      try {
        await api.json(`/api/workloads/${encodeURIComponent(button.dataset.delete)}`, { method: "DELETE" });
        await refreshAll();
      } catch (error) {
        writeTerminal(`error: ${error.message}`);
      }
    });
  }

  // Reconciler core diagnostics
  const forceRec = qs("#forceReconcileBtn");
  if (forceRec) {
    forceRec.addEventListener("click", async () => {
      const result = await api.json("/api/reconcile", { method: "POST", body: "{}" });
      qs("#coreDiagnosticsOutput").textContent = JSON.stringify(result, null, 2);
      await refreshAll();
    });
  }
  const qHealth = qs("#queryHealthBtn");
  if (qHealth) {
    qHealth.addEventListener("click", async () => {
      const res = await api.json("/api/health");
      qs("#coreDiagnosticsOutput").textContent = JSON.stringify(res, null, 2);
    });
  }

  // Control Plane Lock inside Core
  const coreAcquireBtn = qs("#coreAcquireLockBtn");
  if (coreAcquireBtn) {
    coreAcquireBtn.addEventListener("click", async () => {
      const reason = qs("#coreLockReasonInput").value || "Manual Lock";
      try {
        await api.json("/api/lock", { method: "POST", body: JSON.stringify({ reason }) });
        qs("#coreLockReasonInput").value = "";
        writeTerminal("Lock acquired successfully.");
        await refreshAll();
      } catch (error) {
        writeTerminal(`Lock error: ${error.message}`);
      }
    });
  }
  const coreReleaseBtn = qs("#coreReleaseLockBtn");
  if (coreReleaseBtn) {
    coreReleaseBtn.addEventListener("click", async () => {
      try {
        await api.json("/api/lock", { method: "DELETE" });
        writeTerminal("Lock released successfully.");
        await refreshAll();
      } catch (error) {
        writeTerminal(`Unlock error: ${error.message}`);
      }
    });
  }

  // K8s Operator CRD simulation
  const operatorTemplates = {
    nginx: `apiVersion: doktri.io/v1alpha1
kind: DoktriApp
metadata:
  name: web-nginx-operator
  namespace: default
spec:
  image: nginx:alpine
  replicas: 2
  port: 8080
  containerPort: 80
  securityMode: staging`,
    redis: `apiVersion: doktri.io/v1alpha1
kind: DoktriApp
metadata:
  name: cache-redis-operator
  namespace: default
spec:
  image: redis:alpine
  replicas: 1
  port: 6379
  containerPort: 6379
  securityMode: staging`,
    postgres: `apiVersion: doktri.io/v1alpha1
kind: DoktriApp
metadata:
  name: db-postgres-operator
  namespace: default
spec:
  image: postgres:alpine
  replicas: 1
  port: 5432
  containerPort: 5432
  securityMode: production
  env:
    POSTGRES_PASSWORD: "YmFzZTY0ZW5jb2RlZHBhc3N3b3Jk"`
  };
  const templateSelect = qs("#crdTemplateSelect");
  const manifestPreview = qs("#crdManifestPreview");
  if (templateSelect && manifestPreview) {
    const updatePreview = () => {
      manifestPreview.textContent = operatorTemplates[templateSelect.value] || "";
    };
    templateSelect.addEventListener("change", updatePreview);
    updatePreview();
  }
  const simulateCrdBtn = qs("#simulateCrdBtn");
  if (simulateCrdBtn && templateSelect) {
    simulateCrdBtn.addEventListener("click", async () => {
      try {
        const val = templateSelect.value;
        let name = "operator-nginx";
        let image = "nginx:alpine";
        let replicas = 1;
        let port = 80;
        if (val === "redis") {
          name = "operator-redis";
          image = "redis:alpine";
          replicas = 1;
          port = 6379;
        } else if (val === "postgres") {
          name = "operator-postgres";
          image = "postgres:alpine";
          replicas = 1;
          port = 5432;
        }
        
        const res = await api.json("/api/workloads", {
          method: "POST",
          body: JSON.stringify({ name, image, replicas, port, containerPort: port, runtime: "kubernetes" })
        });
        
        writeTerminal(`CRD Apply Simulated: Registered ${name} on Kubernetes namespace 'default'`);
        if (res.status === "pending_approval") {
          writeTerminal(`⏳ PTE Gate: workload requires approval. Plan ID: ${res.planId}`);
        }
        
        const row = document.createElement("tr");
        row.innerHTML = `
          <td><strong>${escapeHtml(name)}</strong></td>
          <td>DoktriApp</td>
          <td>default</td>
          <td><span class="ok">✓ Synced</span></td>
          <td>Just now</td>
        `;
        const crdTbody = qs("#crdTableRows");
        if (crdTbody) {
          if (crdTbody.innerHTML.includes("Select template...")) {
            crdTbody.innerHTML = "";
          }
          crdTbody.prepend(row);
        }
        await refreshAll();
      } catch (err) {
        writeTerminal(`CRD simulation error: ${err.message}`);
      }
    });
  }

  // Runtime driver utils
  const pingDocker = qs("#pingDockerBtn");
  if (pingDocker) {
    pingDocker.addEventListener("click", async () => {
      const out = qs("#driverConsoleOutput");
      out.textContent = "Pinging docker runtime host socket...";
      try {
        const status = await api.json("/api/runtime/status");
        out.textContent = `Docker binary: ${status.docker.binary}\nSimulation Mode: ${status.docker.simulated ? "active" : "disabled"}\nContainers: ${status.containers.length}`;
      } catch (err) {
        out.textContent = `Error: ${err.message}`;
      }
    });
  }
  const toggleSim = qs("#toggleDriverSimBtn");
  if (toggleSim) {
    toggleSim.addEventListener("click", () => {
      const out = qs("#driverConsoleOutput");
      out.textContent = "Re-evaluating primary container host driver interface context...";
      writeTerminal("Selected universal runtime driver: Docker");
    });
  }

  // CLI token generator
  const cliGenerateToken = qs("#cliGenerateTokenBtn");
  if (cliGenerateToken) {
    cliGenerateToken.addEventListener("click", async () => {
      const role = qs("#cliTokenRole").value;
      const actor = qs("#cliTokenActor").value.trim() || "developer";
      try {
        const token = await generateToken(actor, role, "cli-agent", "cluster:deploy", "Local Scaling");
        qs("#cliTokenOutput").value = token;
        writeTerminal(`Minted security token for actor ${actor} (role=${role})`);
      } catch (err) {
        writeTerminal(`Token generation error: ${err.message}`);
      }
    });
  }

  // Schema Browser filter
  const schemaSearch = qs("#schemaSearchInput");
  if (schemaSearch) {
    schemaSearch.addEventListener("input", filterAndRenderSchema);
  }

  // Helm Chart values compiler
  const renderChartBtn = qs("#renderChartBtn");
  if (renderChartBtn) {
    renderChartBtn.addEventListener("click", async () => {
      const name = qs("#chartWorkloadName").value.trim() || "chart-app";
      const image = qs("#chartWorkloadImage").value.trim() || "nginx:alpine";
      const replicas = Number(qs("#chartWorkloadReplicas").value) || 1;
      const port = Number(qs("#chartWorkloadPort").value) || 8080;
      const containerPort = Number(qs("#chartWorkloadContainerPort").value) || 80;
      const securityMode = qs("#chartSecurityMode").value;
      
      const preview = qs("#helmYamlPreview");
      preview.textContent = "Compiling values.yaml Helm variables...";
      
      try {
        const res = await api.json("/api/charts/render", {
          method: "POST",
          body: JSON.stringify({ name, image, replicas, port, containerPort, securityMode, runtime: "docker" })
        });
        preview.textContent = res.yaml;
        writeTerminal(`Helm template values.yaml compiled for ${name}`);
      } catch (err) {
        preview.textContent = `Error: ${err.message}`;
      }
    });
  }

  // Live examples tab switching
  const tabPython = qs("#exampleTabPython");
  const tabCurl = qs("#exampleTabCurl");
  const tabGo = qs("#exampleTabGo");
  if (tabPython) tabPython.addEventListener("click", () => renderExamplesView("python"));
  if (tabCurl) tabCurl.addEventListener("click", () => renderExamplesView("curl"));
  if (tabGo) tabGo.addEventListener("click", () => renderExamplesView("go"));

  const termForm = qs("#terminalForm");
  if (termForm) {
    termForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      const input = qs("#terminalCommand");
      const raw = input.value;
      input.value = "";
      await runTerminalCommand(raw);
    });
  }

  const roleSelect = qs("#role");
  if (roleSelect) {
    roleSelect.addEventListener("change", (event) => {
      api.role = event.target.value;
      writeTerminal(`role changed: ${api.role}`);
    });
  }

  qsa("[data-rpc]").forEach((button) => button.addEventListener("click", async () => {
    qs("#mcpOutput").textContent = JSON.stringify(await callRPC(button.dataset.rpc), null, 2);
  }));

  qsa("[data-tool]").forEach((button) => button.addEventListener("click", async () => {
    qs("#mcpOutput").textContent = JSON.stringify(await callTool(button.dataset.tool), null, 2);
  }));

  // Command search input listeners
  if (searchInput) {
    searchInput.addEventListener("input", handleSearchInput);
    searchInput.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        const selected = searchResults.querySelector(".search-item.selected");
        if (selected) {
          e.preventDefault();
          selected.click();
        }
      }
    });
  }

  document.addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "k") {
      e.preventDefault();
      if (searchInput) {
        searchInput.focus();
        searchInput.select();
      }
    }
    if (e.key === "Escape") {
      if (searchResults) searchResults.classList.remove("active");
      if (searchInput) searchInput.blur();
    }
  });

  document.addEventListener("click", (e) => {
    if (searchResults && !e.target.closest(".search")) {
      searchResults.classList.remove("active");
    }
  });

  // Theme dropdown listeners
  const themeBtn = qs("#themeDropdownBtn");
  const themeDropdown = qs("#themeDropdown");
  if (themeBtn && themeDropdown) {
    themeBtn.addEventListener("click", (e) => {
      e.stopPropagation();
      themeDropdown.classList.toggle("active");
    });
    document.addEventListener("click", (e) => {
      if (!e.target.closest(".theme-menu-container")) {
        themeDropdown.classList.remove("active");
      }
    });
  }

  // Theme list selectors
  qsa(".theme-item").forEach((item) => {
    item.addEventListener("click", () => {
      const targetTheme = item.dataset.theme;
      setTheme(targetTheme);
    });
  });

  function setTheme(theme) {
    document.body.dataset.theme = theme;
    localStorage.setItem("doktriai_theme", theme);
    
    qsa(".theme-item").forEach((el) => {
      el.classList.toggle("active", el.dataset.theme === theme);
    });
    
    const label = theme.split("-").map(w => w.charAt(0).toUpperCase() + w.slice(1)).join(" ");
    const nameEl = qs("#activeThemeName");
    if (nameEl) nameEl.textContent = label;

    writeTerminal(`Theme changed to ${label} Mode.`);
  }

  // Sidebar Toggle listener
  const sidebarBtn = qs("#sidebarToggleBtn");
  const ideContainer = qs(".ide");
  if (sidebarBtn && ideContainer) {
    sidebarBtn.addEventListener("click", () => {
      ideContainer.classList.toggle("sidebar-hidden");
      writeTerminal("Sidebar visibility toggled.");
    });
  }

  // Terminal Toggle listener
  const termToggle = qs("#terminalToggleBtn");
  const mainLayout = qs(".main");
  if (termToggle && mainLayout) {
    termToggle.addEventListener("click", () => {
      mainLayout.classList.toggle("terminal-hidden");
      writeTerminal("Terminal drawer visibility toggled.");
    });
  }

  const closeTerm = qs("#closeTerminal");
  if (closeTerm && mainLayout) {
    closeTerm.addEventListener("click", () => {
      mainLayout.classList.add("terminal-hidden");
    });
  }
}

setInterval(() => {
  const clock = qs("#clock");
  if (clock) {
    clock.textContent = new Date().toLocaleString(undefined, { weekday: "short", month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" });
  }
}, 1000);

// Load saved theme preference on start
const savedTheme = localStorage.getItem("doktriai_theme") || "linen";
document.body.dataset.theme = savedTheme;

bind();
connectEvents();
refreshAll();
handleRouting();

// Sync dropdown menu UI with the saved preference after scripts bind
document.addEventListener("DOMContentLoaded", () => {
  const qsa = (selector) => [...document.querySelectorAll(selector)];
  const qs = (selector) => document.querySelector(selector);
  qsa(".theme-item").forEach((el) => {
    el.classList.toggle("active", el.dataset.theme === savedTheme);
  });
  const label = savedTheme.split("-").map(w => w.charAt(0).toUpperCase() + w.slice(1)).join(" ");
  const nameEl = qs("#activeThemeName");
  if (nameEl) nameEl.textContent = label;
});

const api = {
  role: "admin",
  headers() {
    return { "Content-Type": "application/json", "X-Kranix-Role": this.role, "X-Kranix-Actor": `web:${this.role}` };
  },
  async json(path, options = {}) {
    const res = await fetch(path, { ...options, headers: { ...this.headers(), ...(options.headers || {}) } });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error || res.statusText);
    return body;
  }
};

const state = { workloads: [], events: [], audit: [] };
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

function switchView(view) {
  qsa(".view").forEach((item) => item.classList.remove("active-view"));
  qsa(".tree,.tab").forEach((item) => item.classList.toggle("active", item.dataset.view === view));
  const target = qs(`#${view}`);
  if (target) target.classList.add("active-view");
  if (view === "gallery") drawArchitecture();
}

async function refreshAll() {
  try {
    state.workloads = await api.json("/api/workloads");
    state.audit = await api.json("/api/audit");
    renderWorkloads();
    renderAudit();
    qs("#statusText").textContent = "reconciler online";
    qs("#mcpStatus").textContent = "MCP ready · alpha-v1";
  } catch (error) {
    qs("#statusText").textContent = "api offline";
    writeTerminal(`error: ${error.message}`);
  }
}

function renderWorkloads() {
  const rows = qs("#workloadRows");
  rows.innerHTML = "";
  if (!state.workloads.length) {
    rows.innerHTML = `<tr><td colspan="7">No declared workloads. Use the deploy form, terminal, CLI, or MCP.</td></tr>`;
    qs("#issueCount").textContent = "0";
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
      <td>${item.actual.length}</td>
      <td>${escapeHtml(item.spec.runtime)}</td>
      <td class="${item.healthy ? "ok" : "danger"}">${escapeHtml(item.drift || "healthy")}</td>
      <td><button class="row-action" data-delete="${escapeHtml(item.spec.name)}">delete</button></td>
    `;
    rows.appendChild(tr);
  }
  qs("#issueCount").textContent = String(issues);
}

function renderAudit() {
  const list = qs("#auditList");
  list.innerHTML = state.audit.length ? "" : `<div class="audit-line">No audit records yet.</div>`;
  for (const item of state.audit.slice(0, 60)) {
    const line = document.createElement("div");
    line.className = "audit-line";
    line.innerHTML = `<strong>${escapeHtml(item.action)}</strong> ${escapeHtml(item.workload || "")} · ${escapeHtml(item.actor)} · ${item.allowed ? "allowed" : "blocked"} <small>${escapeHtml(item.reason || "")}</small>`;
    list.appendChild(line);
  }
}

function renderEvents() {
  const list = qs("#eventList");
  list.innerHTML = state.events.length ? "" : `<div class="event-line">Waiting for SSE events...</div>`;
  for (const item of state.events.slice(0, 80)) {
    const line = document.createElement("div");
    line.className = "event-line";
    line.innerHTML = `<strong>${escapeHtml(item.source)}</strong> ${escapeHtml(item.level)} · ${escapeHtml(item.workload || "system")} · ${escapeHtml(item.message)}`;
    list.appendChild(line);
  }
}

function writeTerminal(message) {
  const body = qs("#terminalBody");
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
    port: Number(data.port),
    containerPort: Number(data.containerPort),
    runtime: "docker"
  };
  await api.json("/api/workloads", { method: "POST", body: JSON.stringify(payload) });
  writeTerminal(`deploy accepted: ${payload.name}`);
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
      writeTerminal(logs.join("\\n") || "no logs");
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
    // Ignore malformed event payloads from proxies.
  }
}

function drawArchitecture() {
  const canvas = qs("#architectureCanvas");
  const ctx = canvas.getContext("2d");
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.fillStyle = "#f6f0e6";
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  const nodes = [
    ["Developer / AI Agent", 550, 50],
    ["kranix-cli | kranix-mcp", 550, 125],
    ["kranix-api", 550, 200],
    ["kranix-core", 550, 275],
    ["runtime", 300, 360],
    ["operator", 550, 360],
    ["packages", 800, 360]
  ];
  ctx.strokeStyle = "#c8792a";
  ctx.fillStyle = "#282522";
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
  ctx.fillStyle = "#eee8de";
  ctx.strokeStyle = "#ddd5ca";
  ctx.fillRect(x - 130, y - 24, 260, 48);
  ctx.strokeRect(x - 130, y - 24, 260, 48);
  ctx.fillStyle = "#282522";
  ctx.textAlign = "center";
  ctx.font = "16px Cascadia Mono, monospace";
  ctx.fillText(label, x, y + 5);
}

function bind() {
  qsa("[data-view]").forEach((button) => button.addEventListener("click", () => switchView(button.dataset.view)));
  qs("#deployForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    try {
      await deployFromForm(event.currentTarget);
    } catch (error) {
      writeTerminal(`error: ${error.message}`);
    }
  });
  qs("#workloadRows").addEventListener("click", async (event) => {
    const button = event.target.closest("[data-delete]");
    if (!button) return;
    try {
      await api.json(`/api/workloads/${encodeURIComponent(button.dataset.delete)}`, { method: "DELETE" });
      await refreshAll();
    } catch (error) {
      writeTerminal(`error: ${error.message}`);
    }
  });
  qs("#reconcileBtn").addEventListener("click", async () => {
    const result = await api.json("/api/reconcile", { method: "POST", body: "{}" });
    qs("#experimentOutput").textContent = JSON.stringify(result, null, 2);
    await refreshAll();
  });
  qs("#healthBtn").addEventListener("click", async () => {
    qs("#experimentOutput").textContent = JSON.stringify(await api.json("/api/health"), null, 2);
  });
  qs("#terminalForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    const input = qs("#terminalCommand");
    const raw = input.value;
    input.value = "";
    await runTerminalCommand(raw);
  });
  qs("#role").addEventListener("change", (event) => {
    api.role = event.target.value;
    writeTerminal(`role changed: ${api.role}`);
  });
  qsa("[data-rpc]").forEach((button) => button.addEventListener("click", async () => {
    qs("#mcpOutput").textContent = JSON.stringify(await callRPC(button.dataset.rpc), null, 2);
  }));
  qsa("[data-tool]").forEach((button) => button.addEventListener("click", async () => {
    qs("#mcpOutput").textContent = JSON.stringify(await callTool(button.dataset.tool), null, 2);
  }));
}

setInterval(() => {
  qs("#clock").textContent = new Date().toLocaleString(undefined, { weekday: "short", month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" });
}, 1000);

bind();
connectEvents();
refreshAll();
drawArchitecture();

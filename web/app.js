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

const state = {
  workloads: [],
  events: [],
  audit: [],
  plans: [],
  behaviorMetrics: [],
  openTabs: ["overview", "workloads", "writing", "security", "mcp"],
  activeView: "overview"
};
const qs = (selector) => document.querySelector(selector);
const qsa = (selector) => [...document.querySelectorAll(selector)];

const tabLabels = {
  overview: "Overview",
  projects: "Projects",
  experiments: "Experiments",
  writing: "Documentation",
  notes: "Notes",
  gallery: "Gallery",
  contact: "Contact",
  workloads: "Workloads",
  core: "Reconciler",
  operator: "Operator/GitOps",
  runtime: "Runtime Drivers",
  mcp: "doktriai-mcp",
  cli: "CLI Reference",
  packages: "Schema Browser",
  charts: "Helm Charts",
  examples: "Examples",
  security: "Security",
  activity: "Activity",
  settings: "Settings"
};

let notes = JSON.parse(localStorage.getItem("doktriai_notes")) || [
  { id: "1", title: "Scale Workload Spec", content: "To scale hello-web app:\nreplicas: 3\nimage: nginx:alpine" },
  { id: "2", title: "GitOps Config", content: "Syncing CRD specs via operator module:\nkind: DoktriApp\nmetadata:\n  name: secure-nginx" }
];
let activeNoteId = "1";

function renderNotes() {
  const list = qs("#notesList");
  if (!list) return;
  list.innerHTML = "";
  
  if (notes.length === 0) {
    list.innerHTML = `<div class="note-item" style="color:var(--muted); font-size:11px; text-align:center; padding:12px;">No notes yet.</div>`;
    qs("#noteTitleInput").value = "";
    qs("#noteContentTextarea").value = "";
    return;
  }
  
  notes.forEach(note => {
    const item = document.createElement("div");
    item.className = `note-item${note.id === activeNoteId ? " active" : ""}`;
    item.style.padding = "8px";
    item.style.borderBottom = "1px solid var(--line)";
    item.style.cursor = "pointer";
    item.style.borderRadius = "4px";
    if (note.id === activeNoteId) {
      item.style.background = "var(--panel-hover)";
    }
    
    item.innerHTML = `
      <div style="font-weight:600; font-size:12px; color:#fff;">${escapeHtml(note.title || "Untitled")}</div>
      <div style="font-size:10px; color:var(--muted); margin-top:2px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">${escapeHtml(note.content || "")}</div>
    `;
    item.addEventListener("click", () => {
      activeNoteId = note.id;
      renderNotes();
      loadActiveNote();
    });
    list.appendChild(item);
  });
}

function loadActiveNote() {
  const note = notes.find(n => n.id === activeNoteId);
  const tInput = qs("#noteTitleInput");
  const cText = qs("#noteContentTextarea");
  if (note && tInput && cText) {
    tInput.value = note.title;
    cText.value = note.content;
  }
}

function renderTabs() {
  const container = qs("#tabsContainer");
  if (!container) return;
  container.innerHTML = "";
  
  state.openTabs.forEach(view => {
    const btn = document.createElement("button");
    btn.className = `tab${state.activeView === view ? " active" : ""}`;
    btn.dataset.view = view;
    
    const label = tabLabels[view] || view;
    btn.appendChild(document.createTextNode(label + " "));
    
    if (view === "security") {
      const badge = document.createElement("span");
      badge.id = "planBadge";
      badge.className = "plan-badge";
      const pendingCount = (state.plans || []).filter(p => p.status === "pending").length;
      badge.textContent = pendingCount;
      badge.style.display = pendingCount > 0 ? "inline-flex" : "none";
      btn.appendChild(badge);
    }
    
    const closeSpan = document.createElement("span");
    closeSpan.className = "tab-close";
    closeSpan.dataset.close = view;
    closeSpan.innerHTML = "×";
    closeSpan.addEventListener("click", (e) => {
      e.stopPropagation();
      closeTab(view);
    });
    btn.appendChild(closeSpan);
    
    btn.addEventListener("click", () => switchView(view));
    container.appendChild(btn);
  });
  
  const spacer = document.createElement("button");
  spacer.className = "tab spacer";
  spacer.textContent = "···";
  container.appendChild(spacer);
}

function closeTab(view) {
  state.openTabs = state.openTabs.filter(t => t !== view);
  if (state.activeView === view) {
    const nextActive = state.openTabs[state.openTabs.length - 1] || "overview";
    if (!state.openTabs.includes("overview") && nextActive === "overview") {
      state.openTabs.push("overview");
    }
    switchView(nextActive);
  } else {
    renderTabs();
  }
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function switchView(view, updateHash = true) {
  state.activeView = view;
  if (!state.openTabs.includes(view)) {
    state.openTabs.push(view);
  }

  qsa(".view").forEach((item) => item.classList.remove("active-view"));
  updateSidebarActiveStates(view);
  
  const target = qs(`#${view}`);
  if (target) target.classList.add("active-view");
  if (view === "gallery") {
    setTimeout(() => {
      drawArchitecture();
      startArchitectureAnimation();
    }, 50);
  }
  if (view === "runtime") {
    renderRuntimeView();
  }
  if (view === "core") renderCoreView();
  if (view === "packages") renderPackagesView();
  if (view === "examples") renderExamplesView();
  if (updateHash) {
    window.location.hash = "/" + view;
  }
  renderTabs();
}

function updateSidebarActiveStates(view) {
  qsa(".tree").forEach(item => item.classList.toggle("active", item.dataset.view === view));
  
  qsa(".menu-item, .sub-item").forEach(el => el.classList.remove("active"));
  qsa(".menu-item-group").forEach(el => el.classList.remove("expanded"));

  const subItem = qs(`.sub-item[data-view="${view}"]`);
  if (subItem) {
    subItem.classList.add("active");
    const group = subItem.closest(".menu-item-group");
    if (group) {
      group.classList.add("expanded");
      const parentBtn = group.querySelector(".menu-item");
      if (parentBtn) parentBtn.classList.add("active");
    }
    return;
  }

  const parentItem = qs(`.menu-item[data-view="${view}"]`);
  if (parentItem) {
    parentItem.classList.add("active");
    const group = parentItem.closest(".menu-item-group");
    if (group) group.classList.add("expanded");
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

    state.canaries = {};
    for (const w of state.workloads) {
      if (w.spec.deployStrategy === "canary") {
        try {
          const canary = await api.json(`/api/workloads/${encodeURIComponent(w.spec.name)}/canary`);
          if (canary && canary.active) {
            state.canaries[w.spec.name] = canary;
          }
        } catch (e) {
          // ignore
        }
      }
    }
    
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
    refreshPolicy();
    
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

    const sbStatus = qs("#sbStatusText");
    if (sbStatus) sbStatus.textContent = "reconciler online";
    const sbDot = qs("#sbConnectionDot");
    if (sbDot) {
      sbDot.style.background = "var(--green)";
      sbDot.style.boxShadow = "var(--glow-emerald)";
    }
    
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
    const sbLockDot = qs("#sbLockDot");
    const sbLockStatusText = qs("#sbLockStatusText");
    if (lockDot && lockStatusText) {
      if (lockState.locked) {
        lockDot.style.background = "var(--amber)";
        lockDot.style.boxShadow = "0 0 10px rgba(255, 157, 0, 0.4)";
        lockStatusText.textContent = `locked (${lockState.acquiredBy})`;
        if (sbLockDot) sbLockDot.style.background = "var(--amber)";
        if (sbLockStatusText) sbLockStatusText.textContent = `locked (${lockState.acquiredBy})`;
      } else {
        lockDot.style.background = "var(--green)";
        lockDot.style.boxShadow = "var(--glow-emerald)";
        lockStatusText.textContent = "system unlocked";
        if (sbLockDot) sbLockDot.style.background = "var(--green)";
        if (sbLockStatusText) sbLockStatusText.textContent = "system unlocked";
      }
    }
    
    const mcpStatus = qs("#mcpStatus");
    if (mcpStatus) mcpStatus.textContent = "MCP ready · alpha-v1";
    const sbMcp = qs("#sbMcpStatus");
    if (sbMcp) sbMcp.textContent = "MCP ready · alpha-v1";
  } catch (error) {
    qs("#statusText").textContent = "api offline";
    const dot = qs("#connectionDot");
    if (dot) dot.classList.add("offline");

    const sbStatus = qs("#sbStatusText");
    if (sbStatus) sbStatus.textContent = "api offline";
    const sbDot = qs("#sbConnectionDot");
    if (sbDot) {
      sbDot.style.background = "var(--danger)";
    }
    writeTerminal(`error: ${error.message}`);
  }
}

function renderWorkloads() {
  const rows = qs("#workloadRows");
  if (!rows) return;
  rows.innerHTML = "";
  if (!state.workloads.length) {
    rows.innerHTML = `<tr><td colspan="8">No declared workloads. Use the deploy form, terminal, CLI, or MCP.</td></tr>`;
    qs("#issueCount").textContent = "0";
    qs("#issueCountBadge").classList.add("healthy");
    return;
  }
  let issues = 0;
  for (const item of state.workloads) {
    if (!item.healthy) issues++;
    const tr = document.createElement("tr");
    
    let strategyStr = escapeHtml(item.spec.deployStrategy || "recreate");
    const canary = state.canaries && state.canaries[item.spec.name];
    if (canary && canary.active) {
      strategyStr = `<span class="badge-dev" style="background: rgba(255,157,0,0.15); color: var(--orange); padding: 2px 6px; border-radius: 4px; font-size: 10px;">Canary (${canary.weight}%)</span>
                     <div style="font-size: 10px; color: var(--muted); margin-top: 4px;">Step ${canary.step + 1}/3</div>`;
    } else if (item.spec.deployStrategy === "canary") {
      strategyStr = `<span style="color: var(--green); font-size: 11px; font-weight: 500;">Canary (100%)</span>`;
    } else {
      strategyStr = `<span style="color: var(--muted); font-size: 11px;">${strategyStr}</span>`;
    }

    tr.innerHTML = `
      <td><strong>${escapeHtml(item.spec.name)}</strong></td>
      <td>${escapeHtml(item.spec.image)}</td>
      <td>${item.spec.replicas}</td>
      <td>${strategyStr}</td>
      <td>${item.actual ? item.actual.length : 0}</td>
      <td>${escapeHtml(item.spec.runtime)}</td>
      <td class="${item.healthy ? "ok" : "danger"}">${escapeHtml(item.drift || "healthy")}</td>
      <td>
        <button class="row-action" style="margin-right: 6px;" data-history="${escapeHtml(item.spec.name)}">history</button>
        <button class="row-action" style="margin-right: 6px;" data-edit="${escapeHtml(item.spec.name)}">edit</button>
        <button class="row-action" style="margin-right: 6px;" data-delete="${escapeHtml(item.spec.name)}">delete</button>
        ${canary && canary.active ? `
          <button class="row-action" style="margin-right: 6px; background: rgba(48,209,88,0.2); border: 1px solid var(--green); color: var(--green);" data-canary-promote="${escapeHtml(item.spec.name)}">Promote</button>
          <button class="row-action" style="background: rgba(255,69,58,0.2); border: 1px solid var(--red); color: var(--red);" data-canary-rollback="${escapeHtml(item.spec.name)}">Abort</button>
        ` : ""}
      </td>
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
        showToast(`Plan ${btn.dataset.planId} approved and applied successfully!`, "success");
        await refreshAll();
      } catch (err) {
        writeTerminal(`Approve error: ${err.message}`);
        showToast(`Approval failed: ${err.message}`, "error");
      }
    });
  });
  container.querySelectorAll(".plan-reject-btn").forEach(btn => {
    btn.addEventListener("click", async () => {
      const comment = prompt("Rejection reason (optional):") || "";
      try {
        await api.json(`/api/plan/${encodeURIComponent(btn.dataset.planId)}/reject`, { method: "POST", body: JSON.stringify({ comment }) });
        writeTerminal(`Plan ${btn.dataset.planId} rejected.`);
        showToast(`Plan ${btn.dataset.planId} has been rejected.`, "success");
        await refreshAll();
      } catch (err) {
        writeTerminal(`Reject error: ${err.message}`);
        showToast(`Rejection failed: ${err.message}`, "error");
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

  const sidebarActors = qs("#sidebarActorsList");
  if (sidebarActors) {
    sidebarActors.innerHTML = "";
    const actorsToRender = metrics.length ? metrics : [
      { actor: "Ester Howard", flagged: false, anomalyScore: 0.0 },
      { actor: "Jaco", flagged: false, anomalyScore: 0.0 },
      { actor: "DevOps Bot", flagged: false, anomalyScore: 0.0 }
    ];
    actorsToRender.forEach(m => {
      const avatarNum = Math.abs(m.actor.split("").reduce((acc, char) => acc + char.charCodeAt(0), 0)) % 10;
      const avatarUrl = `https://images.unsplash.com/photo-${[
        "1534528741775-53994a69daeb",
        "1507003211169-0a1dd7228f2d",
        "1494790108377-be9c29b29330",
        "1500648767791-00dcc994a43e",
        "1438761681033-6461ffad8d80",
        "1544005313-94ddf0286df2",
        "1506794778202-cad84cf45f1d",
        "1522075469751-3a6694fb2f61",
        "1534751516642-a131fed10495",
        "1472099645785-5658abf4ff4e"
      ][avatarNum]}?auto=format&fit=crop&w=80&q=80`;

      const row = document.createElement("div");
      row.className = "actor-row";
      row.title = `Actor: ${m.actor} · Safe`;
      row.innerHTML = `
        <div class="actor-avatar" style="background-image: url('${avatarUrl}');">
          <span class="actor-status-dot ${m.flagged ? "anomaly" : "online"}"></span>
        </div>
        <span class="actor-name">${escapeHtml(m.actor)}</span>
      `;
      row.addEventListener("click", () => switchView("security"));
      sidebarActors.appendChild(row);
    });
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

function showToast(message, type = "info") {
  const container = qs("#toastContainer") || (() => {
    const el = document.createElement("div");
    el.id = "toastContainer";
    el.className = "toast-container";
    document.body.appendChild(el);
    return el;
  })();

  const toast = document.createElement("div");
  toast.className = `toast ${type}`;
  toast.innerHTML = `
    <span class="toast-icon">${type === "warning" ? "⏳" : type === "error" ? "❌" : "✓"}</span>
    <div class="toast-content">
      <div class="toast-title">${type === "warning" ? "PTE Approval Pending" : type === "error" ? "Action Blocked" : "Success"}</div>
      <div class="toast-message">${escapeHtml(message)}</div>
    </div>
    <span class="toast-close">&times;</span>
  `;

  toast.querySelector(".toast-close").addEventListener("click", () => {
    toast.style.opacity = "0";
    toast.style.transform = "translateY(-10px)";
    setTimeout(() => toast.remove(), 200);
  });

  container.appendChild(toast);

  // Auto remove toast after 6 seconds
  setTimeout(() => {
    if (toast.parentNode) {
      toast.style.opacity = "0";
      toast.style.transform = "translateY(-10px)";
      setTimeout(() => toast.remove(), 300);
    }
  }, 6000);
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
    runtime: "docker",
    deployStrategy: data.deployStrategy || "recreate"
  };
  try {
    const result = await api.json("/api/workloads", { method: "POST", body: JSON.stringify(payload) });
    if (result.status === "pending_approval") {
      writeTerminal(`⏳ PTE Gate: workload requires approval. Plan ID: ${result.planId}`);
      writeTerminal(`   Reason: ${result.approvalReason}`);
      writeTerminal(`   Go to Security → Pending Approvals to approve or reject.`);
      showToast(`Workload "${payload.name}" requires human approval: ${result.approvalReason}`, "warning");
    } else {
      writeTerminal(`deploy accepted: ${payload.name}`);
      showToast(`Workload "${payload.name}" successfully deployed!`, "success");
    }
  } catch (err) {
    writeTerminal(`error: ${err.message}`);
    showToast(`Failed to deploy workload: ${err.message}`, "error");
    throw err;
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

let hoveredNode = null;
let animationFrameId = null;
let animationProgress = 0;

// Multi-colored layered node specs
const mapNodes = [
  // Layer 1: Client/Inputs
  { id: "agent", label: "⓵ developer-agent / CLI / MCP", sub: "User inputs, YAML manifests & tool triggers", x: 550, y: 50, w: 360, h: 46, type: "client", status: "ok", bg: "rgba(186, 104, 200, 0.12)", border: "#ab47bc", metrics: { reqs: "6.5 req/s", latency: "60ms", health: "99.2%" } },
  // Layer 2: Gateways
  { id: "api", label: "⓶ doktriai-api Gateway", sub: "REST endpoints, auth validation, SSE logs", x: 350, y: 130, w: 260, h: 46, type: "gateway", status: "ok", bg: "rgba(38, 166, 154, 0.12)", border: "#26a69a", metrics: { reqs: "5.7 req/s", latency: "2ms", health: "100%" } },
  { id: "operator", label: "⓶ GitOps operator Bridge", sub: "Watches cluster namespace CRD templates", x: 750, y: 130, w: 260, h: 46, type: "gateway", status: "ok", bg: "rgba(38, 166, 154, 0.12)", border: "#26a69a", metrics: { reqs: "0.8 req/s", latency: "45ms", health: "100%" } },
  // Layer 3A: Config & Auth (inside backend box)
  { id: "policy", label: "doktri-policy.yaml", sub: "Intent Guard registry allowlists", x: 300, y: 245, w: 230, h: 42, type: "middleware", status: "ok", bg: "rgba(255, 202, 40, 0.12)", border: "#ffca28", metrics: { reqs: "6.5 req/s", latency: "1ms", health: "100%" } },
  { id: "jwt", label: "JWT Auth validator", sub: "Decrypts cryptographic key claims", x: 550, y: 245, w: 230, h: 42, type: "middleware", status: "ok", bg: "rgba(92, 107, 192, 0.12)", border: "#5c6bc0", metrics: { reqs: "5.7 req/s", latency: "2ms", health: "100%" } },
  { id: "sqlite", label: "⓸ sqlite / postgres DB", sub: "Persists specs & version snaps", x: 800, y: 245, w: 230, h: 42, type: "middleware", status: "ok", bg: "rgba(66, 165, 245, 0.12)", border: "#42a5f5", metrics: { reqs: "4.5 req/s", latency: "4ms", health: "100%" } },
  // Layer 3B: Core engine (inside backend box)
  { id: "engine", label: "⓷ doktriai-core Reconciler", sub: "Compares desired vs actual state, runs drift loop", x: 550, y: 325, w: 380, h: 48, type: "engine", status: "ok", bg: "rgba(38, 166, 154, 0.18)", border: "#26a69a", metrics: { reqs: "14.2 req/s", latency: "6ms", health: "100%" } },
  // Layer 3C: Runtime & Telemetry (inside backend box)
  { id: "runtime", label: "⓹ docker / k8s Runtime", sub: "Executes recreate & rolling strategy upgrades", x: 300, y: 405, w: 230, h: 44, type: "executor", status: "ok", bg: "rgba(102, 187, 106, 0.12)", border: "#66bb6a", metrics: { reqs: "8.1 req/s", latency: "8ms", health: "100%" } },
  { id: "prometheus", label: "prometheus Exporter", sub: "Scrapes metrics & publishes telemetry", x: 550, y: 405, w: 230, h: 44, type: "executor", status: "ok", bg: "rgba(255, 112, 67, 0.12)", border: "#ff7043", metrics: { reqs: "12.0 req/s", latency: "2ms", health: "100%" } },
  { id: "packages", label: "packages Spec Schema", sub: "Structural validation & schema metadata", x: 800, y: 405, w: 230, h: 44, type: "executor", status: "ok", bg: "rgba(189, 189, 189, 0.12)", border: "#bdbdbd", metrics: { reqs: "1.2 req/s", latency: "0ms", health: "100%" } }
];

// Helper to draw thick arrowheads
function drawArrow(ctx, fromx, fromy, tox, toy, color) {
  const headlen = 10;
  const dx = tox - fromx;
  const dy = toy - fromy;
  const angle = Math.atan2(dy, dx);
  
  ctx.beginPath();
  ctx.strokeStyle = color;
  ctx.lineWidth = 2.5;
  ctx.moveTo(fromx, fromy);
  ctx.lineTo(tox, toy);
  ctx.stroke();
  
  ctx.beginPath();
  ctx.fillStyle = color;
  ctx.moveTo(tox, toy);
  ctx.lineTo(tox - headlen * Math.cos(angle - Math.PI / 6), toy - headlen * Math.sin(angle - Math.PI / 6));
  ctx.lineTo(tox - headlen * Math.cos(angle + Math.PI / 6), toy - headlen * Math.sin(angle + Math.PI / 6));
  ctx.fill();
}

function startArchitectureAnimation() {
  if (animationFrameId) return;
  const tick = () => {
    animationProgress += 0.008;
    if (animationProgress > 1) animationProgress = 0;
    
    drawArchitecture();
    
    if (state.activeView === "gallery") {
      animationFrameId = requestAnimationFrame(tick);
    } else {
      animationFrameId = null;
    }
  };
  animationFrameId = requestAnimationFrame(tick);
}

function drawArchitecture() {
  const canvas = qs("#architectureCanvas");
  if (!canvas) return;
  const ctx = canvas.getContext("2d");
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  
  // Set dimensions and backgrounds
  canvas.height = 480;
  const isLinen = document.body.dataset.theme === "linen";
  const canvasBg = isLinen ? "#f3f0e8" : "#07080c";
  const textColor = isLinen ? "#4a4538" : "#e2e8f0";
  const mutedColor = isLinen ? "#8a8370" : "#718096";
  const arrowColor = isLinen ? "rgba(74, 69, 56, 0.4)" : "rgba(226, 232, 240, 0.4)";
  const backendBoxBg = isLinen ? "rgba(235, 231, 223, 0.4)" : "rgba(18, 18, 22, 0.4)";
  const backendBoxBorder = isLinen ? "dashed 1.5px #d4cebe" : "dashed 1.5px #202738";

  ctx.fillStyle = canvasBg;
  ctx.fillRect(0, 0, canvas.width, canvas.height);

  // 1. Draw big Backend boundary box (Level 3 container)
  ctx.beginPath();
  ctx.rect(100, 205, 900, 255);
  ctx.fillStyle = backendBoxBg;
  ctx.fill();
  ctx.lineWidth = 1.5;
  ctx.strokeStyle = isLinen ? "#d4cebe" : "#202738";
  ctx.setLineDash([6, 6]);
  ctx.stroke();
  ctx.setLineDash([]);

  // Label for backend container
  ctx.fillStyle = mutedColor;
  ctx.font = "bold 9px Inter, sans-serif";
  ctx.textAlign = "left";
  ctx.fillText("DOKTRIAI BACKEND CONTROL PLANE BOUNDARY", 115, 222);

  // 2. Draw Connection Arrows
  // Trigger -> API (x: 550, y: 73 -> x: 350, y: 107)
  drawArrow(ctx, 420, 73, 350, 107, arrowColor);
  // Trigger -> Operator (x: 550, y: 73 -> x: 750, y: 107)
  drawArrow(ctx, 680, 73, 750, 107, arrowColor);

  // API -> JWT Auth (x: 350, y: 153 -> x: 550, y: 224)
  drawArrow(ctx, 410, 153, 500, 224, arrowColor);
  // API -> Core (x: 350, y: 153 -> x: 550, y: 301)
  drawArrow(ctx, 350, 153, 440, 301, arrowColor);

  // Operator -> Core (x: 750, y: 153 -> x: 550, y: 301)
  drawArrow(ctx, 750, 153, 660, 301, arrowColor);

  // Core -> Policy (x: 550, y: 325 -> x: 300, y: 266)
  drawArrow(ctx, 450, 325, 360, 266, arrowColor);
  // Core -> DB (x: 550, y: 325 -> x: 800, y: 266)
  drawArrow(ctx, 650, 325, 740, 266, arrowColor);

  // Core -> Runtime (x: 550, y: 349 -> x: 300, y: 383)
  drawArrow(ctx, 480, 349, 360, 383, arrowColor);
  // Core -> Prometheus (x: 550, y: 349 -> x: 550, y: 383)
  drawArrow(ctx, 550, 349, 550, 383, arrowColor);
  // Core -> Packages (x: 550, y: 349 -> x: 800, y: 383)
  drawArrow(ctx, 620, 349, 740, 383, arrowColor);

  // Draw animated flow pulses
  ctx.fillStyle = isLinen ? "#a16207" : "#00d2ff";
  const pulses = [
    { x1: 420, y1: 73, x2: 350, y2: 107 },
    { x1: 680, y1: 73, x2: 750, y2: 107 },
    { x1: 350, y1: 153, x2: 440, y2: 301 },
    { x1: 750, y1: 153, x2: 660, y2: 301 },
    { x1: 480, y1: 349, x2: 360, y2: 383 },
    { x1: 550, y1: 349, x2: 550, y2: 383 }
  ];
  pulses.forEach(p => {
    const px = p.x1 + (p.x2 - p.x1) * animationProgress;
    const py = p.y1 + (p.y2 - p.y1) * animationProgress;
    ctx.beginPath();
    ctx.arc(px, py, 4, 0, Math.PI * 2);
    ctx.fill();
  });

  // 3. Draw Nodes (Enterprise Blocks)
  mapNodes.forEach(node => {
    const isHovered = hoveredNode && hoveredNode.id === node.id;
    const hw = node.w / 2;
    const hh = node.h / 2;

    // Pulse outer border on hover
    if (isHovered) {
      ctx.shadowBlur = 10;
      ctx.shadowColor = node.border;
    }

    // Node background block
    ctx.fillStyle = node.bg;
    ctx.strokeStyle = node.border;
    ctx.lineWidth = 1.5;
    ctx.fillRect(node.x - hw, node.y - hh, node.w, node.h);
    ctx.strokeRect(node.x - hw, node.y - hh, node.w, node.h);
    ctx.shadowBlur = 0; // Reset shadow

    // Icon representations (simple shapes)
    if (node.id === "sqlite" || node.id === "packages") {
      // Draw small Cylinder icon for DBs
      ctx.fillStyle = node.border;
      ctx.fillRect(node.x - hw + 10, node.y - 10, 12, 16);
    } else if (node.id === "policy" || node.id === "jwt") {
      // Draw small Key/Lock symbol
      ctx.fillStyle = node.border;
      ctx.beginPath();
      ctx.arc(node.x - hw + 16, node.y, 4, 0, Math.PI * 2);
      ctx.fill();
    }

    // Bold title text
    ctx.fillStyle = textColor;
    ctx.font = "bold 11px JetBrains Mono, monospace";
    ctx.textAlign = "center";
    ctx.fillText(node.label, node.x, node.y - 3);

    // Muted subtitle text
    ctx.fillStyle = mutedColor;
    ctx.font = "9px Inter, sans-serif";
    ctx.fillText(node.sub, node.x, node.y + 11);
  });

  // 4. Draw Tooltip Box on hover
  if (hoveredNode) {
    drawTooltip(ctx, hoveredNode);
  }
}

function drawTooltip(ctx, node) {
  const boxW = 210;
  const boxH = 92;
  const tooltipX = node.x + (node.w / 2) + 10;
  const tooltipY = node.y - 45;

  ctx.fillStyle = "rgba(10, 10, 12, 0.95)";
  ctx.strokeStyle = node.border;
  ctx.lineWidth = 1.5;
  ctx.fillRect(tooltipX, tooltipY, boxW, boxH);
  ctx.strokeRect(tooltipX, tooltipY, boxW, boxH);

  ctx.fillStyle = "#ffffff";
  ctx.font = "bold 11px JetBrains Mono, monospace";
  ctx.textAlign = "left";
  ctx.fillText(node.label, tooltipX + 12, tooltipY + 22);

  ctx.fillStyle = "#86868b";
  ctx.font = "9px Inter, sans-serif";
  ctx.fillText(node.sub, tooltipX + 12, tooltipY + 38);

  ctx.strokeStyle = "rgba(255, 255, 255, 0.1)";
  ctx.beginPath();
  ctx.moveTo(tooltipX + 12, tooltipY + 48);
  ctx.lineTo(tooltipX + boxW - 12, tooltipY + 48);
  ctx.stroke();

  ctx.fillStyle = "#e2e8f0";
  ctx.font = "9px JetBrains Mono, monospace";
  ctx.fillText(`Reqs: ${node.metrics.reqs}`, tooltipX + 12, tooltipY + 64);
  ctx.fillText(`Latency: ${node.metrics.latency}`, tooltipX + 12, tooltipY + 78);
  
  ctx.fillStyle = "#30d158";
  ctx.fillText(`Health: ${node.metrics.health}`, tooltipX + 118, tooltipY + 64);
}

function initCanvasInteraction() {
  const canvas = qs("#architectureCanvas");
  if (!canvas) return;
  
  canvas.addEventListener("mousemove", (e) => {
    const rect = canvas.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    
    let found = null;
    for (const node of mapNodes) {
      const hw = node.w / 2;
      const hh = node.h / 2;
      if (x >= node.x - hw && x <= node.x + hw && y >= node.y - hh && y <= node.y + hh) {
        found = node;
        break;
      }
    }
    
    if (found !== hoveredNode) {
      hoveredNode = found;
      drawArchitecture();
    }
  });

  canvas.addEventListener("mouseleave", () => {
    if (hoveredNode !== null) {
      hoveredNode = null;
      drawArchitecture();
    }
  });
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

let projectName = localStorage.getItem("doktriai_project_name") || "control-plane";

function updateProjectName(name) {
  projectName = name;
  localStorage.setItem("doktriai_project_name", name);
  
  const textEl = qs("#projectNameText");
  if (textEl) textEl.textContent = name;
  
  const inputEl = qs("#projectNameInput");
  if (inputEl) inputEl.value = name;
  
  const sbEl = qs("#sbActiveProjectName");
  if (sbEl) sbEl.textContent = `Project: ${name}`;

  const overviewProjEl = qs("#overviewProjectName");
  if (overviewProjEl) overviewProjEl.textContent = name;
  
  const brandSpans = qsa(".brand span");
  if (brandSpans.length > 0) {
    const lastSpan = brandSpans[brandSpans.length - 1];
    if (lastSpan && !lastSpan.closest("#projectNameContainer")) {
      lastSpan.textContent = name;
    }
  }
}

function updateBattery() {
  const indicator = qs("#batteryIndicator");
  if (!indicator) return;

  if (navigator.getBattery) {
    navigator.getBattery().then(battery => {
      const updateInfo = () => {
        const pct = Math.round(battery.level * 100);
        const icon = battery.charging ? "⚡" : "▰";
        indicator.textContent = `${icon} ${pct}%`;
      };
      updateInfo();
      battery.addEventListener("levelchange", updateInfo);
      battery.addEventListener("chargingchange", updateInfo);
    });
  } else {
    indicator.textContent = "▰ 99%";
  }
}

function updateShipDates() {
  const today = new Date();
  const formatOptions = { month: "short", day: "numeric", year: "numeric" };
  const formattedDate = today.toLocaleDateString("en-US", formatOptions);
  
  qsa(".ship-date").forEach(el => {
    const content = el.textContent || "";
    const parts = content.split(" · ");
    const category = parts.length > 1 ? parts[1] : "Guide";
    el.textContent = `${formattedDate} · ${category}`;
  });
}

async function openHistoryModal(name) {
  const modal = qs("#historyModal");
  const title = qs("#historyModalTitle");
  const content = qs("#historyModalContent");
  if (!modal || !title || !content) return;

  title.textContent = `Version History: ${name}`;
  content.innerHTML = `<div style="color: var(--muted); font-size:12px;">Fetching history logs...</div>`;
  modal.style.display = "flex";

  try {
    const history = await api.json(`/api/workloads/${encodeURIComponent(name)}/history`);
    if (!history || history.length === 0) {
      content.innerHTML = `<div style="color: var(--muted); font-size:12px;">No historical versions logged in database yet.</div>`;
      return;
    }

    content.innerHTML = "";
    history.forEach(entry => {
      const div = document.createElement("div");
      div.style.border = "1px solid var(--line)";
      div.style.borderRadius = "6px";
      div.style.padding = "10px";
      div.style.background = "var(--panel)";
      div.style.display = "flex";
      div.style.justifyContent = "space-between";
      div.style.alignItems = "center";
      
      const dateStr = new Date(entry.timestamp).toLocaleString();
      
      div.innerHTML = `
        <div style="font-size:12px; text-align:left;">
          <div style="font-weight: 600; color: #fff;">Version #${entry.version}</div>
          <div style="color: var(--muted); margin-top:2px;">Image: ${escapeHtml(entry.spec.image)} (replicas: ${entry.spec.replicas})</div>
          <div style="font-size:10px; color: var(--orange); margin-top:2px;">By: ${escapeHtml(entry.actor)} · ${dateStr}</div>
        </div>
        <button class="row-action-rollback" data-wl-name="${escapeHtml(name)}" data-wl-ver="${entry.version}" style="font-size: 11px; padding: 4px 8px; background: var(--orange); color: #fff; border: 0; border-radius: 4px; cursor: pointer;">Rollback</button>
      `;
      content.appendChild(div);
    });

    // Bind rollback action
    content.querySelectorAll(".row-action-rollback").forEach(btn => {
      btn.addEventListener("click", async () => {
        const wlName = btn.dataset.wlName;
        const wlVer = btn.dataset.wlVer;
        try {
          await api.json(`/api/workloads/${encodeURIComponent(wlName)}/rollback`, {
            method: "POST",
            body: JSON.stringify({ version: Number(wlVer) })
          });
          showToast(`Successfully rolled back ${wlName} to version #${wlVer}!`, "success");
          modal.style.display = "none";
          await refreshAll();
        } catch (e) {
          showToast(`Rollback failed: ${e.message}`, "error");
        }
      });
    });
  } catch (e) {
    content.innerHTML = `<div class="danger">Error loading history: ${escapeHtml(e.message)}</div>`;
  }
}

let policyLoaded = false;
async function refreshPolicy() {
  if (policyLoaded) return;
  try {
    const policy = await api.json("/api/policy");
    if (!policy) return;
    
    const registriesList = qs("#policyRegistriesList");
    if (registriesList) {
      registriesList.innerHTML = (policy.Security?.ApprovedImagePrefixes || [])
        .map(prefix => `<li><code>${escapeHtml(prefix)}</code></li>`)
        .join("");
    }
    
    const credsList = qs("#policyCredsList");
    if (credsList) {
      credsList.innerHTML = (policy.Security?.SensitiveEnvKeyPatterns || [])
        .map(pattern => `<li><code>${escapeHtml(pattern)}</code></li>`)
        .join("");
    }
    
    const maxReplicas = qs("#policyMaxReplicas");
    if (maxReplicas) {
      maxReplicas.textContent = policy.Security?.PTEReplicaThreshold || "-";
    }
    
    const requireDigest = qs("#policyRequireDigest");
    if (requireDigest) {
      requireDigest.textContent = policy.Security?.RequireDigestPinInProduction ? "Enabled" : "Disabled";
    }
    
    const opaEngine = qs("#policyOPAEngine");
    if (opaEngine) {
      opaEngine.textContent = policy.Security?.UseOPA ? "Active" : "Inactive";
      opaEngine.style.color = policy.Security?.UseOPA ? "var(--green)" : "var(--muted)";
    }
    
    const opaPath = qs("#policyOPAPath");
    if (opaPath) {
      opaPath.textContent = policy.Security?.OPAPolicyPath || "-";
    }
    
    policyLoaded = true;
  } catch (err) {
    // ignore
  }
}


function bind() {
  qsa(".tree, .menu-item, .sub-item").forEach((button) => button.addEventListener("click", () => switchView(button.dataset.view)));

  // Project Name Inline Editing
  const nameContainer = qs("#projectNameContainer");
  const nameText = qs("#projectNameText");
  const nameInput = qs("#projectNameInput");
  const editIcon = qs("#projectNameContainer .edit-icon");
  
  if (nameContainer && nameText && nameInput) {
    nameContainer.addEventListener("click", () => {
      if (nameInput.style.display === "inline-block") return;
      nameText.style.display = "none";
      if (editIcon) editIcon.style.display = "none";
      nameInput.style.display = "inline-block";
      nameInput.focus();
      nameInput.select();
    });
    
    const saveName = () => {
      const val = nameInput.value.trim();
      if (val) {
        updateProjectName(val);
        writeTerminal(`Project renamed to "${val}".`);
      }
      nameText.style.display = "inline";
      if (editIcon) editIcon.style.display = "inline";
      nameInput.style.display = "none";
    };
    
    nameInput.addEventListener("blur", saveName);
    nameInput.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        saveName();
      }
      if (e.key === "Escape") {
        nameInput.value = projectName;
        nameText.style.display = "inline";
        if (editIcon) editIcon.style.display = "inline";
        nameInput.style.display = "none";
      }
    });
  }
  
  // Initialize project name
  updateProjectName(projectName);

  // Clickable Project Cards
  qsa(".clickable-project-card").forEach(card => {
    card.addEventListener("click", () => {
      const targetView = card.dataset.projectView;
      const strongEl = card.querySelector("strong");
      const projName = strongEl ? strongEl.textContent.trim() : "";
      if (projName) {
        updateProjectName(projName);
        writeTerminal(`Connected to project: ${projName}`);
      }
      if (targetView) {
        switchView(targetView);
      }
    });
  });

  // Project filtering
  qsa(".project-filters button").forEach(btn => {
    btn.addEventListener("click", () => {
      qsa(".project-filters button").forEach(b => {
        b.classList.remove("active");
        b.style.background = "transparent";
      });
      btn.classList.add("active");
      btn.style.background = "var(--panel-hover)";
      
      const filter = btn.dataset.filter;
      qsa(".clickable-project-card").forEach(card => {
        if (filter === "all" || card.dataset.status === filter) {
          card.style.display = "flex";
        } else {
          card.style.display = "none";
        }
      });
    });
  });

  // Notes App bindings
  const btnNew = qs("#btnNewNote");
  const btnSave = qs("#btnSaveNote");
  const btnDelete = qs("#btnDeleteNote");
  
  if (btnNew) {
    btnNew.addEventListener("click", () => {
      const newId = Date.now().toString();
      notes.push({ id: newId, title: "New Note", content: "" });
      activeNoteId = newId;
      localStorage.setItem("doktriai_notes", JSON.stringify(notes));
      renderNotes();
      loadActiveNote();
      qs("#noteTitleInput").focus();
    });
  }
  
  if (btnSave) {
    btnSave.addEventListener("click", () => {
      const note = notes.find(n => n.id === activeNoteId);
      if (note) {
        note.title = qs("#noteTitleInput").value.trim() || "Untitled";
        note.content = qs("#noteContentTextarea").value;
        localStorage.setItem("doktriai_notes", JSON.stringify(notes));
        renderNotes();
        writeTerminal(`Note "${note.title}" saved successfully.`);
      }
    });
  }
  
  if (btnDelete) {
    btnDelete.addEventListener("click", () => {
      notes = notes.filter(n => n.id !== activeNoteId);
      activeNoteId = notes[0]?.id || "";
      localStorage.setItem("doktriai_notes", JSON.stringify(notes));
      renderNotes();
      loadActiveNote();
      writeTerminal("Note deleted.");
    });
  }
  
  // Render notes initial load
  renderNotes();
  loadActiveNote();

  // Contact support form
  const contactForm = qs("#contactForm");
  if (contactForm) {
    contactForm.addEventListener("submit", (e) => {
      e.preventDefault();
      const subject = qs("#contactSubject").value;
      writeTerminal(`Support request submitted: Subject="${subject}"`);
      contactForm.reset();
      alert("Support request submitted to DOKTRIAI core panel.");
    });
  }

  // Chaos simulations
  const btnDrain = qs("#btnChaosDrain");
  const btnLatency = qs("#btnChaosLatency");
  const btnChaosKill = qs("#btnChaosKill");
  const chaosConsole = qs("#chaosOutputConsole");
  const chaosActive = qs("#chaosActiveCount");
  let chaosCount = 0;
  
  if (btnDrain && chaosConsole) {
    btnDrain.addEventListener("click", () => {
      chaosCount++;
      if (chaosActive) chaosActive.textContent = `${chaosCount} running`;
      chaosConsole.textContent += `\n[${new Date().toLocaleTimeString()}] INJECTION: Draining node 'worker-node-1'...\n[${new Date().toLocaleTimeString()}] STATUS: Evicting container replicas safely.`;
      writeTerminal("Chaos event: Node drain simulation started.");
    });
  }
  
  if (btnLatency && chaosConsole) {
    btnLatency.addEventListener("click", () => {
      chaosCount++;
      if (chaosActive) chaosActive.textContent = `${chaosCount} running`;
      chaosConsole.textContent += `\n[${new Date().toLocaleTimeString()}] INJECTION: Injecting +250ms egress latency on docker0 bridge...\n[${new Date().toLocaleTimeString()}] STATUS: Observing reconciliation ticks lag.`;
      writeTerminal("Chaos event: Egress latency injected.");
    });
  }
  
  if (btnChaosKill && chaosConsole) {
    btnChaosKill.addEventListener("click", () => {
      chaosConsole.textContent += `\n[${new Date().toLocaleTimeString()}] INJECTION: Sending SIGKILL to replica hello-web-1...\n[${new Date().toLocaleTimeString()}] STATUS: Core reconciler triggered auto-heal loop successfully.`;
      writeTerminal("Chaos event: SIGKILL chaos test run.");
    });
  }
  
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
      const deleteButton = event.target.closest("[data-delete]");
      if (deleteButton) {
        try {
          await api.json(`/api/workloads/${encodeURIComponent(deleteButton.dataset.delete)}`, { method: "DELETE" });
          await refreshAll();
        } catch (error) {
          writeTerminal(`error: ${error.message}`);
        }
        return;
      }

      const promoteBtn = event.target.closest("[data-canary-promote]");
      if (promoteBtn) {
        const name = promoteBtn.dataset.canaryPromote;
        try {
          await api.json(`/api/workloads/${encodeURIComponent(name)}/canary/promote`, { method: "POST", body: "{}" });
          showToast(`Canary promoted for workload "${name}"!`, "success");
          await refreshAll();
        } catch (error) {
          writeTerminal(`error: ${error.message}`);
          showToast(`Promotion failed: ${error.message}`, "error");
        }
        return;
      }

      const rollbackCanaryBtn = event.target.closest("[data-canary-rollback]");
      if (rollbackCanaryBtn) {
        const name = rollbackCanaryBtn.dataset.canaryRollback;
        try {
          await api.json(`/api/workloads/${encodeURIComponent(name)}/canary/rollback`, { method: "POST", body: "{}" });
          showToast(`Canary aborted and rolled back for workload "${name}"!`, "success");
          await refreshAll();
        } catch (error) {
          writeTerminal(`error: ${error.message}`);
          showToast(`Abort failed: ${error.message}`, "error");
        }
        return;
      }

      const historyButton = event.target.closest("[data-history]");
      if (historyButton) {
        const name = historyButton.dataset.history;
        openHistoryModal(name);
        return;
      }

      const editButton = event.target.closest("[data-edit]");
      if (editButton) {
        const name = editButton.dataset.edit;
        const wl = state.workloads.find(w => w.spec.name === name);
        if (wl) {
          const form = qs("#deployForm");
          if (form) {
            form.querySelector('[name="name"]').value = wl.spec.name;
            form.querySelector('[name="image"]').value = wl.spec.image;
            form.querySelector('[name="replicas"]').value = wl.spec.replicas;
            form.querySelector('[name="port"]').value = wl.spec.port || "";
            form.querySelector('[name="containerPort"]').value = wl.spec.containerPort || "";
            if (form.querySelector('[name="deployStrategy"]')) {
              form.querySelector('[name="deployStrategy"]').value = wl.spec.deployStrategy || "recreate";
            }
          }
          
          const manifestArea = qs("#manifestTextarea");
          if (manifestArea) {
            const yamlStr = `apiVersion: doktriai/v1
kind: Workload
metadata:
  name: ${wl.spec.name}
spec:
  image: ${wl.spec.image}
  replicas: ${wl.spec.replicas}
  port: ${wl.spec.port || 0}
  containerPort: ${wl.spec.containerPort || 0}
  runtime: ${wl.spec.runtime || 'docker'}
  deployStrategy: ${wl.spec.deployStrategy || 'recreate'}`;
            manifestArea.value = yamlStr;
          }
          
          const targetSection = form || manifestArea;
          if (targetSection) {
            targetSection.scrollIntoView({ behavior: "smooth" });
          }
          writeTerminal(`Loaded workload "${name}" into the form and YAML editor.`);
        }
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
  const cliTokenType = qs("#cliTokenType");
  const cliJwtFields = qs("#cliJwtFields");
  if (cliTokenType && cliJwtFields) {
    cliTokenType.addEventListener("change", () => {
      cliJwtFields.style.display = cliTokenType.value === "jwt" ? "flex" : "none";
    });
  }

  const cliGenerateToken = qs("#cliGenerateTokenBtn");
  if (cliGenerateToken) {
    cliGenerateToken.addEventListener("click", async () => {
      const type = qs("#cliTokenType").value;
      const role = qs("#cliTokenRole").value;
      const actor = qs("#cliTokenActor").value.trim() || "developer";
      const scope = qs("#cliTokenScope").value.trim() || "deploy_workload";
      
      try {
        if (type === "jwt") {
          const res = await api.json("/api/agents/issue-token", {
            method: "POST",
            body: JSON.stringify({ agentId: actor, scope: scope, ttl: "24h" })
          });
          qs("#cliTokenOutput").value = res.token;
          writeTerminal(`Minted cryptographic JWT keycard for agent ${actor} (scope=${scope})`);
        } else {
          const token = await generateToken(actor, role, "cli-agent", "cluster:deploy", "Local Scaling");
          qs("#cliTokenOutput").value = token;
          writeTerminal(`Minted signature token for actor ${actor} (role=${role})`);
        }
      } catch (err) {
        writeTerminal(`Token generation error: ${err.message}`);
      }
    });
  }

  // History modal closer
  const closeHistoryModal = qs("#closeHistoryModalBtn");
  const historyModal = qs("#historyModal");
  if (closeHistoryModal && historyModal) {
    closeHistoryModal.addEventListener("click", () => {
      historyModal.style.display = "none";
    });
    historyModal.addEventListener("click", (e) => {
      if (e.target === historyModal) {
        historyModal.style.display = "none";
      }
    });
  }

  // Copy-paste YAML Manifest Deploy
  const deployManifestTextBtn = qs("#deployManifestTextBtn");
  const manifestTextarea = qs("#manifestTextarea");
  if (deployManifestTextBtn && manifestTextarea) {
    deployManifestTextBtn.addEventListener("click", async () => {
      const yamlContent = manifestTextarea.value.trim();
      if (!yamlContent) {
        showToast("Please paste YAML manifest content first!", "error");
        return;
      }
      writeTerminal("Parsing manifest text payload...");
      try {
        const result = await api.json("/api/workloads/deploy-manifest", {
          method: "POST",
          headers: { "Content-Type": "application/x-yaml" },
          body: yamlContent
        });
        if (result.status === "pending_approval") {
          writeTerminal(`⏳ PTE Gate: manifest workload requires approval. Plan ID: ${result.planId}`);
          showToast(`Manifest deployment requires human approval: ${result.approvalReason}`, "warning");
        } else {
          writeTerminal(`Manifest deploy accepted successfully!`);
          showToast(`Manifest deployment successful!`, "success");
        }
      } catch (err) {
        writeTerminal(`error: ${err.message}`);
        showToast(`Failed to deploy manifest: ${err.message}`, "error");
      }
      await refreshAll();
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

  // Modern MCP Panel handlers
  const mcpTokenRole = qs("#mcpTokenRole");
  const mcpToolSelect = qs("#mcpToolSelect");
  const mcpParamsForm = qs("#mcpParamsForm");

  // Switch client tabs
  qsa("#mcpTabHeader .tab-btn").forEach((btn) => {
    btn.addEventListener("click", (e) => {
      qsa("#mcpTabHeader .tab-btn").forEach((b) => b.classList.remove("active"));
      btn.classList.add("active");
      updateMcpClientConfigs();
    });
  });

  if (mcpTokenRole) {
    mcpTokenRole.addEventListener("change", () => {
      updateMcpClientConfigs();
    });
  }

  if (mcpToolSelect) {
    mcpToolSelect.addEventListener("change", () => {
      renderMcpParamsForm();
    });
  }

  // Copy config button
  const copyConfigBtn = qs("#copyMcpConfigBtn");
  if (copyConfigBtn) {
    copyConfigBtn.addEventListener("click", () => {
      const blockEl = qs("#mcpConfigBlock");
      if (blockEl) {
        navigator.clipboard.writeText(blockEl.textContent);
        showToast("Copied MCP Config to Clipboard", "ok");
      }
    });
  }

  // Copy token button
  const copyTokenBtn = qs("#copyMcpTokenBtn");
  if (copyTokenBtn) {
    copyTokenBtn.addEventListener("click", async () => {
      const role = mcpTokenRole ? mcpTokenRole.value : "admin";
      try {
        const res = await api.json("/api/agents/issue-token", {
          method: "POST",
          body: JSON.stringify({
            agentId: "mcp-agent-web",
            scope: "deploy_workload,list_workloads,delete_workload,get_logs,approve_plan,reject_plan",
            ttl: "720h" // 30 days
          })
        });
        navigator.clipboard.writeText(res.token);
        showToast(`Copied JWT ${role.toUpperCase()} agent token to clipboard`, "ok");
      } catch (e) {
        showToast("Error generating JWT token: " + e.message, "error");
      }
    });
  }

  // Parameter rendering and execution
  function renderMcpParamsForm() {
    if (!mcpParamsForm || !mcpToolSelect) return;
    const tool = mcpToolSelect.value;
    mcpParamsForm.innerHTML = "";
    
    if (tool === "deploy_workload") {
      mcpParamsForm.innerHTML = `
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 8px;">
          <div>
            <label style="font-size: 11px; color: var(--muted); display: block; margin-bottom: 2px;">Workload Name</label>
            <input id="paramName" class="select-field" type="text" value="mcp-test-app" placeholder="e.g. hello-web" style="height: 28px; padding: 4px 8px; font-size: 12px;" />
          </div>
          <div>
            <label style="font-size: 11px; color: var(--muted); display: block; margin-bottom: 2px;">Container Image</label>
            <input id="paramImage" class="select-field" type="text" value="nginx:alpine" placeholder="e.g. nginx:alpine" style="height: 28px; padding: 4px 8px; font-size: 12px;" />
          </div>
        </div>
        <div style="display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 8px; margin-top: 8px;">
          <div>
            <label style="font-size: 11px; color: var(--muted); display: block; margin-bottom: 2px;">Replicas</label>
            <input id="paramReplicas" class="select-field" type="number" value="2" min="1" style="height: 28px; padding: 4px 8px; font-size: 12px;" />
          </div>
          <div>
            <label style="font-size: 11px; color: var(--muted); display: block; margin-bottom: 2px;">Host Port</label>
            <input id="paramPort" class="select-field" type="number" value="8085" style="height: 28px; padding: 4px 8px; font-size: 12px;" />
          </div>
          <div>
            <label style="font-size: 11px; color: var(--muted); display: block; margin-bottom: 2px;">Container Port</label>
            <input id="paramContainerPort" class="select-field" type="number" value="80" style="height: 28px; padding: 4px 8px; font-size: 12px;" />
          </div>
        </div>
      `;
    } else if (tool === "delete_workload") {
      mcpParamsForm.innerHTML = `
        <div>
          <label style="font-size: 11px; color: var(--muted); display: block; margin-bottom: 2px;">Workload Name to Delete</label>
          <input id="paramDeleteName" class="select-field" type="text" value="mcp-test-app" placeholder="e.g. hello-web" style="height: 28px; padding: 4px 8px; font-size: 12px;" />
        </div>
      `;
    } else if (tool === "get_logs") {
      mcpParamsForm.innerHTML = `
        <div style="display: grid; grid-template-columns: 2fr 1fr; gap: 8px;">
          <div>
            <label style="font-size: 11px; color: var(--muted); display: block; margin-bottom: 2px;">Workload Name</label>
            <input id="paramLogName" class="select-field" type="text" value="mcp-test-app" placeholder="e.g. hello-web" style="height: 28px; padding: 4px 8px; font-size: 12px;" />
          </div>
          <div>
            <label style="font-size: 11px; color: var(--muted); display: block; margin-bottom: 2px;">Tail Lines</label>
            <input id="paramLogTail" class="select-field" type="number" value="50" min="1" style="height: 28px; padding: 4px 8px; font-size: 12px;" />
          </div>
        </div>
      `;
    } else {
      mcpParamsForm.innerHTML = `<p style="font-size: 12px; color: var(--muted); margin: 0;">This method does not require any parameters.</p>`;
    }
  }

  async function updateMcpClientConfigs() {
    const pathEl = qs("#mcpWorkspacePath");
    const workspaceDir = (pathEl && pathEl.textContent !== "Detecting...") ? pathEl.textContent : "c:/Users/prafu/OneDrive/Documents/Placement/DevOps/doktriai-control-plane";
    
    const activeTab = qs("#mcpTabHeader .tab-btn.active");
    const client = activeTab ? activeTab.dataset.client : "cursor";
    
    let configObj = {};
    let filename = "project.json";
    
    const binaryPath = `${workspaceDir}/doktriai-cli.exe`;
    
    if (client === "cursor") {
      filename = "project.json";
      configObj = {
        "mcpServers": {
          "doktriai": {
            "command": binaryPath,
            "args": ["mcp"],
            "env": {
              "DOKTRIAI_API": "http://localhost:18080",
              "DOKTRIAI_TOKEN": "$DOKTRIAI_TOKEN"
            }
          }
        }
      };
    } else if (client === "windsurf") {
      filename = "mcp_config.json";
      configObj = {
        "mcpServers": {
          "doktriai": {
            "command": binaryPath,
            "args": ["mcp"],
            "env": {
              "DOKTRIAI_API": "http://localhost:18080",
              "DOKTRIAI_TOKEN": "$DOKTRIAI_TOKEN"
            }
          }
        }
      };
    } else if (client === "claude") {
      filename = "claude_desktop_config.json";
      configObj = {
        "mcpServers": {
          "doktriai": {
            "command": binaryPath,
            "args": ["mcp"],
            "env": {
              "DOKTRIAI_API": "http://localhost:18080",
              "DOKTRIAI_TOKEN": "$DOKTRIAI_TOKEN"
            }
          }
        }
      };
    } else if (client === "vault") {
      filename = "vault-secret.json";
      configObj = {
        "secret": {
          "path": "secret/data/doktriai",
          "data": {
            "api_url": "http://localhost:18080",
            "token": "$DOKTRIAI_TOKEN"
          }
        },
        "description": "Store DOKTRIAI_TOKEN in HashiCorp Vault or AWS Secrets Manager and inject it into your runner container at runtime."
      };
    }
    
    const filenameEl = qs("#mcpConfigFilename");
    if (filenameEl) filenameEl.textContent = filename;
    
    const blockEl = qs("#mcpConfigBlock");
    if (blockEl) blockEl.textContent = JSON.stringify(configObj, null, 2);
  }

  // Load workspace path and initialize configs
  async function initMcpPanel() {
    try {
      const health = await api.json("/api/health");
      if (health.workspaceDir) {
        const pathEl = qs("#mcpWorkspacePath");
        if (pathEl) pathEl.textContent = health.workspaceDir;
      }
    } catch(e) {}
    renderMcpParamsForm();
    updateMcpClientConfigs();
  }

  // Execute initialization
  initMcpPanel();

  const runMcpToolBtn = qs("#runMcpToolBtn");
  if (runMcpToolBtn) {
    runMcpToolBtn.addEventListener("click", async () => {
      if (!mcpToolSelect) return;
      const tool = mcpToolSelect.value;
      const role = mcpTokenRole ? mcpTokenRole.value : "admin";
      const outputEl = qs("#mcpOutput");
      outputEl.textContent = "Executing JSON-RPC request...";
      
      let token = "";
      try {
        token = await generateToken("developer", role, "mcp-agent", "cluster:deploy", "Local Scaling");
      } catch (e) {
        showToast("Failed to generate authorization token", "error");
        outputEl.textContent = "Token generation error";
        return;
      }
      
      let rpcMethod = "";
      let rpcParams = null;
      
      if (tool === "initialize" || tool === "tools/list") {
        rpcMethod = tool;
      } else if (tool === "list_workloads" || tool === "list_pending_plans") {
        rpcMethod = "tools/call";
        rpcParams = {
          name: tool,
          arguments: {}
        };
      } else if (tool === "deploy_workload") {
        const nameVal = qs("#paramName") ? qs("#paramName").value.trim() : "mcp-test-app";
        const imgVal = qs("#paramImage") ? qs("#paramImage").value.trim() : "nginx:alpine";
        const repVal = qs("#paramReplicas") ? Number(qs("#paramReplicas").value) : 2;
        const portVal = qs("#paramPort") ? Number(qs("#paramPort").value) : 8085;
        const cportVal = qs("#paramContainerPort") ? Number(qs("#paramContainerPort").value) : 80;
        
        rpcMethod = "tools/call";
        rpcParams = {
          name: "deploy_workload",
          arguments: {
            name: nameVal,
            image: imgVal,
            replicas: repVal,
            port: portVal,
            containerPort: cportVal,
            runtime: "docker"
          }
        };
      } else if (tool === "delete_workload") {
        const delVal = qs("#paramDeleteName") ? qs("#paramDeleteName").value.trim() : "mcp-test-app";
        rpcMethod = "tools/call";
        rpcParams = {
          name: "delete_workload",
          arguments: {
            name: delVal
          }
        };
      } else if (tool === "get_logs") {
        const logNameVal = qs("#paramLogName") ? qs("#paramLogName").value.trim() : "mcp-test-app";
        const tailVal = qs("#paramLogTail") ? Number(qs("#paramLogTail").value) : 50;
        rpcMethod = "tools/call";
        rpcParams = {
          name: "get_logs",
          arguments: {
            name: logNameVal,
            tail: tailVal
          }
        };
      }
      
      if (rpcMethod === "tools/call" && rpcParams) {
        rpcParams.agentId = "mcp-web-playground";
        rpcParams.scope = "cluster:deploy";
        rpcParams.goal = "Interactive playground testing";
      }
      
      try {
        const res = await fetch("/api/mcp", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Doktri-Token": token
          },
          body: JSON.stringify({
            jsonrpc: "2.0",
            id: Date.now(),
            method: rpcMethod,
            params: rpcParams
          })
        });
        const data = await res.json();
        outputEl.textContent = JSON.stringify(data, null, 2);
        if (data.error) {
          showToast(data.error.message || "MCP Execution Error", "error");
        } else {
          showToast("MCP Method Call Succeeded", "ok");
          refreshAll();
        }
      } catch (err) {
        outputEl.textContent = `Network Error: ${err.message}`;
        showToast("Connection failed", "error");
      }
    });
  }

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

    const sbTheme = qs("#sbActiveThemeLabel");
    if (sbTheme) sbTheme.textContent = `${label} Theme`;

    writeTerminal(`Theme changed to ${label} Mode.`);
  }

  // Sidebar Toggle listeners
  const sidebarBtn = qs("#sidebarToggleBtn");
  const sidebarArrowBtn = qs("#sidebarToggleArrowBtn");
  const ideContainer = qs(".ide");
  if (sidebarBtn && ideContainer) {
    sidebarBtn.addEventListener("click", () => {
      ideContainer.classList.toggle("sidebar-hidden");
      writeTerminal("Sidebar visibility toggled.");
    });
  }
  if (sidebarArrowBtn && ideContainer) {
    sidebarArrowBtn.addEventListener("click", () => {
      ideContainer.classList.toggle("sidebar-collapsed");
      writeTerminal("Sidebar collapsed state toggled.");
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
initCanvasInteraction();
connectEvents();
refreshAll();
handleRouting();
updateBattery();
updateShipDates();

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

  const sbTheme = qs("#sbActiveThemeLabel");
  if (sbTheme) sbTheme.textContent = `${label} Theme`;
  updateBattery();
  updateShipDates();
});

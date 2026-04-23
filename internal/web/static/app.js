const state = {
  health: null,
  hosts: [],
  sessions: [],
  runs: [],
  approvals: [],
  currentSessionId: "",
  currentRunId: "",
  currentSessionDetail: null,
  liveTrace: [],
  activeDrafts: new Map(),
  activeRunEvents: new Map(),
  refreshTimer: null,
};

const healthPill = document.getElementById("health-pill");
const gatewayMeta = document.getElementById("gateway-meta");
const sessionList = document.getElementById("session-list");
const sessionCount = document.getElementById("session-count");
const hostList = document.getElementById("host-list");
const runHost = document.getElementById("run-host");
const runSession = document.getElementById("run-session");
const chatThread = document.getElementById("chat-thread");
const conversationTitle = document.getElementById("conversation-title");
const conversationHost = document.getElementById("conversation-host");
const currentRunPill = document.getElementById("current-run-pill");
const approvalList = document.getElementById("approval-list");
const approvalCount = document.getElementById("approval-count");
const runList = document.getElementById("run-list");
const runCount = document.getElementById("run-count");
const liveTrace = document.getElementById("live-trace");

async function request(path, options = {}) {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(body.error || response.statusText);
  }
  return response.json();
}

function scheduleRefresh() {
  if (state.refreshTimer) return;
  state.refreshTimer = setTimeout(async () => {
    state.refreshTimer = null;
    await Promise.all([loadSessions(), loadRuns(), loadApprovals()]);
    if (state.currentSessionId) {
      await loadSessionDetail(state.currentSessionId, false);
    }
  }, 300);
}

async function loadHealth() {
  state.health = await request("/api/health");
  healthPill.textContent = state.health.status;
  gatewayMeta.textContent = state.health.no_sandbox ? "无沙箱真实权限环境" : "sandboxed";
}

async function loadHosts() {
  const data = await request("/api/hosts");
  state.hosts = data.items.sort((a, b) => a.display_name.localeCompare(b.display_name));
  renderHosts();
}

async function loadSessions() {
  const data = await request("/api/sessions");
  state.sessions = data.items.sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at));
  if (!state.currentSessionId && state.sessions.length > 0) {
    state.currentSessionId = state.sessions[0].id;
    runSession.value = state.currentSessionId;
  }
  renderSessions();
}

async function loadRuns() {
  const data = await request("/api/runs");
  state.runs = data.items.sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
  renderRuns();
}

async function loadApprovals() {
  const data = await request("/api/approvals");
  state.approvals = data.items.sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
  renderApprovals();
}

async function loadSessionDetail(sessionId, forceRender = true) {
  if (!sessionId) {
    state.currentSessionDetail = null;
    renderConversation();
    return;
  }
  const detail = await request(`/api/sessions/${sessionId}`);
  state.currentSessionDetail = detail;
  state.currentSessionId = sessionId;
  runSession.value = sessionId;
  if (forceRender) renderConversation();
  else renderConversation();
}

function renderHosts() {
  hostList.innerHTML = "";
  runHost.innerHTML = "";
  for (const host of state.hosts) {
    const option = document.createElement("option");
    option.value = host.id;
    option.textContent = `${host.display_name} (${host.id})`;
    runHost.appendChild(option);

    const item = document.createElement("div");
    item.className = "host-item";
    item.innerHTML = `<strong>${host.display_name}</strong><div class="muted small">${host.id} · ${host.mode}${host.address ? ` · ${host.address}` : ""}</div>`;
    hostList.appendChild(item);
  }
}

function renderSessions() {
  sessionCount.textContent = String(state.sessions.length);
  sessionList.innerHTML = "";
  if (state.sessions.length === 0) {
    sessionList.innerHTML = `<div class="empty-state">还没有会话。发送第一条运维请求后会自动创建。</div>`;
    return;
  }
  for (const session of state.sessions) {
    const item = document.createElement("button");
    item.type = "button";
    item.className = `session-item clickable ${session.id === state.currentSessionId ? "active" : ""}`;
    item.innerHTML = `
      <strong>${escapeHTML(session.title || session.id)}</strong>
      <div class="muted small">${escapeHTML(session.host_id)} · ${formatTime(session.updated_at)}</div>
      <div class="muted small">${escapeHTML(session.last_outcome || session.summary || "等待执行")}</div>
    `;
    item.addEventListener("click", async () => {
      state.currentSessionId = session.id;
      await loadSessionDetail(session.id);
    });
    sessionList.appendChild(item);
  }
}

function renderRuns() {
  runCount.textContent = String(state.runs.length);
  runList.innerHTML = "";
  if (state.runs.length === 0) {
    runList.innerHTML = `<div class="empty-state">还没有 run。</div>`;
    return;
  }
  for (const run of state.runs.slice(0, 8)) {
    const item = document.createElement("div");
    item.className = "run-item";
    item.innerHTML = `
      <strong>${run.id}</strong>
      <div class="muted small">${run.host_id} · ${run.status}</div>
      <div class="muted small">${escapeHTML(run.final_response || run.failure_message || "进行中")}</div>
    `;
    item.addEventListener("click", async () => {
      state.currentRunId = run.id;
      const session = state.sessions.find((entry) => entry.id === run.session_id);
      if (session) {
        state.currentSessionId = session.id;
        await loadSessionDetail(session.id);
      } else {
        renderConversation();
      }
    });
    runList.appendChild(item);
  }
}

function renderApprovals() {
  const pending = state.approvals.filter((item) => !item.decision);
  approvalCount.textContent = String(pending.length);
  approvalList.innerHTML = "";
  if (pending.length === 0) {
    approvalList.innerHTML = `<div class="empty-state">当前没有待审批操作。</div>`;
    return;
  }
  for (const approval of pending) {
    const item = document.createElement("div");
    item.className = "approval-item";
    item.innerHTML = `
      <strong>${approval.tool_name}</strong>
      <div class="muted small">${approval.run_id}</div>
      <div>${escapeHTML(approval.reason)}</div>
      <div class="muted small">${escapeHTML(approval.scope)}</div>
      <div class="approval-actions">
        <button data-id="${approval.id}" data-decision="approve">Approve</button>
        <button data-id="${approval.id}" data-decision="deny">Deny</button>
      </div>
    `;
    item.querySelectorAll("button").forEach((button) => {
      button.addEventListener("click", async () => {
        await request(`/api/approvals/${button.dataset.id}/resolve`, {
          method: "POST",
          body: JSON.stringify({ decision: button.dataset.decision, actor: "web" }),
        });
        scheduleRefresh();
      });
    });
    approvalList.appendChild(item);
  }
}

function renderConversation() {
  const detail = state.currentSessionDetail;
  if (!detail) {
    conversationTitle.textContent = "选择或创建一段运维会话";
    conversationHost.textContent = "No host";
    currentRunPill.textContent = "No active run";
    chatThread.innerHTML = `<div class="empty-state">从左侧选择一段会话，或者直接在下方输入新的运维请求。</div>`;
    return;
  }

  conversationTitle.textContent = detail.session.title || detail.session.id;
  conversationHost.textContent = `${detail.host.display_name} · ${detail.host.mode}`;

  const activeTurn = [...detail.turns].reverse().find((item) => item.run.status !== "completed" && item.run.status !== "failed" && item.run.status !== "denied");
  currentRunPill.textContent = activeTurn ? activeTurn.run.id : "No active run";
  chatThread.innerHTML = "";

  for (const item of detail.turns) {
    const group = document.createElement("section");
    group.className = "message-group";

    const userRow = document.createElement("div");
    userRow.className = "message-row user";
    userRow.innerHTML = `<div class="bubble user">${escapeHTML(item.turn.user_input)}</div>`;
    group.appendChild(userRow);

    const toolStack = renderToolStack(item);
    if (toolStack.childElementCount > 0) {
      group.appendChild(toolStack);
    }

    const assistantRow = document.createElement("div");
    assistantRow.className = "message-row assistant";
    const assistantText = item.turn.final_explanation || item.run.final_response || assistantStatusFallback(item.run.status);
    assistantRow.innerHTML = `<div class="bubble">${escapeHTML(assistantText)}</div>`;
    group.appendChild(assistantRow);

    const draft = state.activeDrafts.get(item.run.id);
    if (draft && item.run.status !== "completed" && item.run.status !== "failed" && item.run.status !== "denied") {
      const draftRow = document.createElement("div");
      draftRow.className = "message-row draft";
      draftRow.innerHTML = `<div class="bubble draft">${escapeHTML(draft)}</div>`;
      group.appendChild(draftRow);
    }

    chatThread.appendChild(group);
  }

  if (detail.turns.length === 0) {
    chatThread.innerHTML = `<div class="empty-state">这段会话还没有 turn。</div>`;
  }
}

function renderToolStack(item) {
  const stack = document.createElement("div");
  stack.className = "tool-stack";
  const events = [...item.events];
  const liveEvents = state.activeRunEvents.get(item.run.id) || [];
  const merged = dedupeEvents([...events, ...liveEvents]).filter((event) => {
    return [
      "run.policy_checked",
      "run.tool_running",
      "run.tool_finished",
      "run.waiting_approval",
      "run.approval_resolved",
      "run.failed",
    ].includes(event.type);
  });

  for (const event of merged) {
    const card = document.createElement("div");
    card.className = "tool-card";
    card.innerHTML = `
      <h3>${toolCardTitle(event)}</h3>
      <div>${escapeHTML(event.message || "")}</div>
      ${event.payload ? `<div class="tool-meta">${Object.entries(event.payload).map(([key, value]) => `<span class="tool-tag">${escapeHTML(`${key}: ${stringifyValue(value)}`)}</span>`).join("")}</div>` : ""}
    `;
    stack.appendChild(card);
  }
  return stack;
}

function toolCardTitle(event) {
  switch (event.type) {
    case "run.policy_checked":
      return "Policy";
    case "run.tool_running":
      return "Tool Running";
    case "run.tool_finished":
      return "Tool Finished";
    case "run.waiting_approval":
      return "Approval Requested";
    case "run.approval_resolved":
      return "Approval Resolved";
    case "run.failed":
      return "Run Failed";
    default:
      return event.type;
  }
}

function assistantStatusFallback(status) {
  switch (status) {
    case "waiting_approval":
      return "等待人工审批。";
    case "running_agent":
    case "tool_running":
      return "处理中。";
    case "denied":
      return "该 run 已被拒绝。";
    case "failed":
      return "该 run 执行失败。";
    default:
      return "处理中。";
  }
}

function appendTrace(event) {
  state.liveTrace.push(`[${event.type}] ${event.message || ""}${event.payload ? ` ${JSON.stringify(event.payload)}` : ""}`);
  if (state.liveTrace.length > 120) {
    state.liveTrace = state.liveTrace.slice(-120);
  }
  liveTrace.textContent = state.liveTrace.join("\n");
  liveTrace.scrollTop = liveTrace.scrollHeight;
}

function connectGlobalEventStream() {
  const source = new EventSource("/api/events/stream");
  source.onmessage = async (message) => {
    const event = JSON.parse(message.data);
    appendTrace(event);

    if (event.run_id) {
      const items = state.activeRunEvents.get(event.run_id) || [];
      items.push(event);
      state.activeRunEvents.set(event.run_id, items.slice(-30));
    }

    if (event.type === "run.message_delta" && event.run_id) {
      const current = state.activeDrafts.get(event.run_id) || "";
      state.activeDrafts.set(event.run_id, current + (event.message || ""));
      if (state.currentSessionDetail?.turns.some((item) => item.run.id === event.run_id)) {
        renderConversation();
      }
      return;
    }

    if (event.type === "run.assistant_message" || event.type === "run.completed" || event.type === "run.failed") {
      state.activeDrafts.delete(event.run_id);
    }

    if (
      [
        "run.created",
        "run.waiting_approval",
        "run.approval_resolved",
        "run.tool_running",
        "run.tool_finished",
        "run.assistant_message",
        "run.completed",
        "run.failed",
      ].includes(event.type)
    ) {
      scheduleRefresh();
    }

    if (state.currentSessionDetail?.turns.some((item) => item.run.id === event.run_id)) {
      renderConversation();
    }
  };
}

document.getElementById("host-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  await request("/api/hosts", {
    method: "POST",
    body: JSON.stringify({
      id: form.get("id"),
      display_name: form.get("display_name"),
      mode: form.get("mode"),
      address: form.get("address"),
      port: Number(form.get("port") || 0),
      user: form.get("user"),
      password_env: form.get("password_env"),
    }),
  });
  event.currentTarget.reset();
  await loadHosts();
});

document.getElementById("run-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  const result = await request("/api/runs", {
    method: "POST",
    body: JSON.stringify({
      host_id: form.get("host_id"),
      session_id: form.get("session_id"),
      user_input: form.get("user_input"),
      requested_by: "web",
    }),
  });
  state.currentRunId = result.id;
  state.currentSessionId = result.session_id;
  runSession.value = result.session_id;
  await Promise.all([loadSessions(), loadRuns(), loadSessionDetail(result.session_id), loadApprovals()]);
  event.currentTarget.reset();
  runSession.value = result.session_id;
});

function dedupeEvents(items) {
  const seen = new Set();
  const result = [];
  for (const item of items) {
    if (!item?.id || seen.has(item.id)) continue;
    seen.add(item.id);
    result.push(item);
  }
  return result;
}

function formatTime(value) {
  if (!value) return "";
  return new Date(value).toLocaleString();
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function stringifyValue(value) {
  if (typeof value === "string") return value;
  return JSON.stringify(value);
}

Promise.all([loadHealth(), loadHosts(), loadSessions(), loadRuns(), loadApprovals()])
  .then(async () => {
    if (state.currentSessionId) {
      await loadSessionDetail(state.currentSessionId);
    } else {
      renderConversation();
    }
    connectGlobalEventStream();
  })
  .catch((error) => {
    liveTrace.textContent = error.message;
  });

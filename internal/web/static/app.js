const page = document.body.dataset.page;

const TRACE_EVENT_TYPES = new Set([
  "run.created",
  "run.running_agent",
  "run.policy_checked",
  "run.tool_running",
  "run.tool_finished",
  "run.waiting_approval",
  "run.approval_resolved",
  "run.completed",
  "run.failed",
]);

const REFRESH_EVENT_TYPES = new Set([
  "run.created",
  "run.waiting_approval",
  "run.approval_resolved",
  "run.completed",
  "run.failed",
]);

const LIVE_RENDER_EVENT_TYPES = new Set([
  "run.message_delta",
  "run.assistant_message",
  "run.stdout",
  "run.stderr",
  "run.running_agent",
  "run.policy_checked",
  "run.tool_running",
  "run.tool_finished",
  "run.waiting_approval",
  "run.approval_resolved",
  "run.completed",
  "run.failed",
]);

const state = {
  health: null,
  gatewaySettings: null,
  hosts: [],
  runs: [],
  approvals: [],
  sessions: [],
  settingsSelectedPresetId: "",
  currentSessionId: "",
  currentSessionDetail: null,
  liveEvents: new Map(),
  eventSource: null,
  streamConnected: false,
  refreshTimer: null,
  refreshInFlight: null,
  refreshDetail: false,
};

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

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function sanitizeHTML(html) {
  const template = document.createElement("template");
  template.innerHTML = html;
  template.content.querySelectorAll("script, style, iframe, object, embed").forEach((node) => node.remove());
  template.content.querySelectorAll("*").forEach((node) => {
    [...node.attributes].forEach((attr) => {
      const name = attr.name.toLowerCase();
      const value = attr.value.toLowerCase();
      if (name.startsWith("on")) node.removeAttribute(attr.name);
      if ((name === "href" || name === "src") && value.startsWith("javascript:")) {
        node.removeAttribute(attr.name);
      }
    });
  });
  return template.innerHTML;
}

function renderMarkdown(value) {
  const input = String(value ?? "");
  if (!window.marked?.parse) return escapeHTML(input);
  return sanitizeHTML(window.marked.parse(input, { breaks: true, gfm: true }));
}

function formatTime(value) {
  if (!value) return "";
  return new Date(value).toLocaleString();
}

function sortByNewest(items, key) {
  return [...items].sort((a, b) => new Date(b[key] || 0) - new Date(a[key] || 0));
}

function firstNonEmpty(...values) {
  for (const value of values) {
    if (typeof value === "string" && value.trim()) return value;
  }
  return "";
}

function truncateText(value, limit = 140) {
  const text = String(value ?? "").replace(/\s+/g, " ").trim();
  if (!text) return "";
  if (text.length <= limit) return text;
  return `${text.slice(0, limit - 1)}…`;
}

function copyJSON(value) {
  return value ? JSON.parse(JSON.stringify(value)) : value;
}

function gatewayPresets() {
  return state.gatewaySettings?.presets || [];
}

function currentGatewayPresetId() {
  return state.gatewaySettings?.current_preset_id || "";
}

function findGatewayPreset(id) {
  return gatewayPresets().find((preset) => preset.id === id) || null;
}

function ensureGatewaySelection() {
  const presets = gatewayPresets();
  if (presets.length === 0) {
    state.settingsSelectedPresetId = "";
    return null;
  }
  const selected = findGatewayPreset(state.settingsSelectedPresetId);
  if (selected) return selected;
  state.settingsSelectedPresetId = currentGatewayPresetId() || presets[0].id;
  return findGatewayPreset(state.settingsSelectedPresetId) || presets[0];
}

function maskSecret(value) {
  const text = String(value || "").trim();
  if (!text) return "未配置";
  if (text.length <= 8) return `${text.slice(0, 2)}***`;
  return `${text.slice(0, 4)}***${text.slice(-4)}`;
}

function slugifyPresetName(value) {
  const normalized = String(value || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return normalized || `preset-${Date.now()}`;
}

function dedupeEvents(items) {
  const seen = new Set();
  const result = [];
  for (const item of items || []) {
    if (!item?.id || seen.has(item.id)) continue;
    seen.add(item.id);
    result.push(item);
  }
  return result.sort((a, b) => new Date(a.timestamp || 0) - new Date(b.timestamp || 0));
}

function latestSessionId() {
  return state.sessions[0]?.id || "";
}

async function loadCore() {
  const [health, gatewaySettings, hosts, runs, approvals, sessions] = await Promise.all([
    request("/api/health"),
    request("/api/settings/gateway"),
    request("/api/hosts"),
    request("/api/runs"),
    request("/api/approvals"),
    request("/api/sessions"),
  ]);
  state.health = health;
  state.gatewaySettings = gatewaySettings;
  state.hosts = sortByNewest(hosts.items || [], "updated_at");
  state.runs = sortByNewest(runs.items || [], "updated_at");
  state.approvals = sortByNewest(approvals.items || [], "created_at");
  state.sessions = sortByNewest(sessions.items || [], "updated_at");
  if (!state.settingsSelectedPresetId) {
    state.settingsSelectedPresetId = gatewaySettings.current_preset_id || gatewaySettings.currentPresetID || "";
  }
}

async function loadSessionDetail(sessionId) {
  if (!sessionId) {
    state.currentSessionId = "";
    state.currentSessionDetail = null;
    return;
  }
  state.currentSessionDetail = await request(`/api/sessions/${sessionId}`);
  state.currentSessionId = sessionId;
  const url = new URL(window.location.href);
  url.searchParams.set("session", sessionId);
  window.history.replaceState({}, "", url.toString());
}

function getDefaultHostId() {
  if (state.currentSessionDetail?.host?.id) return state.currentSessionDetail.host.id;
  const local = state.hosts.find((host) => host.id === "local");
  return local?.id || state.hosts[0]?.id || "local";
}

function scheduleRefresh({ detail = false } = {}) {
  state.refreshDetail = state.refreshDetail || detail;
  if (state.refreshTimer) return;
  state.refreshTimer = window.setTimeout(async () => {
    state.refreshTimer = null;
    const needDetail = state.refreshDetail;
    state.refreshDetail = false;
    state.refreshInFlight = (async () => {
      await loadCore();
      if (needDetail && state.currentSessionId) {
        await loadSessionDetail(state.currentSessionId);
      }
      renderPage();
    })();
    try {
      await state.refreshInFlight;
    } finally {
      state.refreshInFlight = null;
    }
  }, 120);
}

function appendLiveEvent(event) {
  const current = state.liveEvents.get(event.run_id) || [];
  current.push(event);
  state.liveEvents.set(event.run_id, dedupeEvents(current).slice(-200));
}

function connectGlobalEvents() {
  if (state.eventSource) state.eventSource.close();
  const source = new EventSource("/api/events/stream");
  state.eventSource = source;

  source.onopen = () => {
    state.streamConnected = true;
    renderPage();
  };

  source.onerror = () => {
    state.streamConnected = false;
    renderPage();
  };

  source.onmessage = (message) => {
    const event = JSON.parse(message.data);
    appendLiveEvent(event);

    const affectsCurrentSession = Boolean(
      state.currentSessionDetail?.turns?.some((item) => item.run.id === event.run_id),
    );

    if (LIVE_RENDER_EVENT_TYPES.has(event.type) && (page !== "chat" || affectsCurrentSession)) {
      renderPage();
    }

    if (REFRESH_EVENT_TYPES.has(event.type)) {
      scheduleRefresh({ detail: affectsCurrentSession || page === "chat" });
      return;
    }

    if (event.type === "run.created" || event.type === "run.tool_running" || event.type === "run.tool_finished") {
      scheduleRefresh({ detail: affectsCurrentSession });
    }
  };
}

function renderPage() {
  switch (page) {
    case "chat":
      renderChat();
      break;
    case "assets":
      renderAssets();
      break;
    case "automation":
      renderAutomation();
      break;
    case "settings":
      renderSettings();
      break;
  }
}

function buildReplay(item) {
  const mergedEvents = dedupeEvents([...(item.events || []), ...(state.liveEvents.get(item.run.id) || [])]);
  const toolEvents = [];
  let delta = "";
  let assistantMessage = "";
  let consoleOutput = "";

  for (const event of mergedEvents) {
    if (event.type === "run.message_delta") {
      delta += event.message || "";
    }
    if (event.type === "run.assistant_message" && event.message) {
      assistantMessage = event.message;
    }
    if (event.type === "run.stdout") {
      consoleOutput += event.message || "";
    }
    if (event.type === "run.stderr") {
      consoleOutput += `[stderr] ${event.message || ""}`;
    }
    if (TRACE_EVENT_TYPES.has(event.type)) {
      toolEvents.push(event);
    }
  }

  const assistantContent = firstNonEmpty(
    item.turn?.final_explanation,
    item.run?.final_response,
    item.assistant_text,
    delta,
    assistantMessage,
    item.run?.failure_message,
    runStatusText(item.run?.status),
  );

  return {
    assistantContent,
    consoleOutput: firstNonEmpty(item.console_output, consoleOutput),
    toolEvents: toolEvents.length > 0 ? toolEvents : item.tool_events || [],
    waitingApproval: item.waiting_approval || (item.approvals || []).some((approval) => !approval.decision),
    lastEventAt: item.last_event_at || mergedEvents.at(-1)?.timestamp || item.run?.updated_at,
  };
}

function renderChat() {
  const healthText = document.getElementById("gateway-health-text");
  const healthMeta = document.getElementById("gateway-health-meta");
  const approvalCount = document.getElementById("approval-count");
  const approvalList = document.getElementById("approval-list");
  const chatHistory = document.getElementById("chat-history");
  if (!chatHistory) return;

  if (healthText) {
    const color = state.health?.status === "ok" ? "bg-tertiary-container" : "bg-error";
    const label = state.health?.status === "ok" ? "正常" : "异常";
    healthText.innerHTML = `<span class="w-2 h-2 rounded-full ${color}"></span>${label}`;
  }
  if (healthMeta) {
    const preset = state.health?.preset_name || "";
    const model = state.health?.model || "unknown-model";
    const stream = state.streamConnected ? "SSE 已连接" : "SSE 重连中";
    healthMeta.textContent = [preset, model, stream].filter(Boolean).join(" · ");
  }

  const pendingApprovals = state.approvals.filter((item) => !item.decision);
  if (approvalCount) approvalCount.textContent = `${pendingApprovals.length} 项`;
  if (approvalList) {
    approvalList.innerHTML = "";
    if (pendingApprovals.length === 0) {
      approvalList.innerHTML = `<li class="text-sm text-[#5C5C59]">当前无待审批任务</li>`;
    }
    for (const approval of pendingApprovals) {
      const item = document.createElement("li");
      item.className = "bg-surface-container-lowest border border-[#E2E2DB] rounded-lg p-sm flex items-start gap-sm hover:bg-surface transition-colors";
      item.innerHTML = `
        <span class="material-symbols-outlined text-primary text-[18px] mt-0.5">warning</span>
        <div class="min-w-0 flex-1">
          <div class="font-body-sm text-body-sm text-on-surface font-medium">${escapeHTML(approval.tool_name)}</div>
          <div class="font-body-sm text-[12px] text-secondary">${escapeHTML(approval.reason)}</div>
          <div class="font-body-sm text-[11px] text-secondary mt-1">${escapeHTML(approval.host_display_name || approval.host_id || "未知主机")} · ${escapeHTML(approval.session_title || approval.session_id || "未命名会话")}</div>
          <div class="flex gap-2 mt-2">
            <button class="px-2 py-1 rounded bg-[#C96442] text-white text-xs" data-id="${approval.id}" data-decision="approve" type="button">批准</button>
            <button class="px-2 py-1 rounded bg-[#E6E4D9] text-[#262624] text-xs" data-id="${approval.id}" data-decision="deny" type="button">拒绝</button>
            <button class="px-2 py-1 rounded bg-white border border-[#E2E2DB] text-[#262624] text-xs" data-session="${approval.session_id || ""}" type="button">跳转</button>
          </div>
        </div>
      `;
      item.querySelectorAll("button[data-decision]").forEach((button) => {
        button.addEventListener("click", async () => {
          button.disabled = true;
          await request(`/api/approvals/${button.dataset.id}/resolve`, {
            method: "POST",
            body: JSON.stringify({ decision: button.dataset.decision, actor: "web" }),
          });
          scheduleRefresh({ detail: true });
        });
      });
      item.querySelector("button[data-session]")?.addEventListener("click", async (event) => {
        const sessionId = event.currentTarget.dataset.session;
        if (!sessionId) return;
        await loadSessionDetail(sessionId);
        renderChat();
      });
      approvalList.appendChild(item);
    }
  }

  chatHistory.innerHTML = "";
  if (!state.currentSessionDetail?.turns?.length) {
    chatHistory.innerHTML = `
      <div class="flex gap-md group">
        <div class="w-8 h-8 rounded-full bg-surface-container-high flex items-center justify-center shrink-0 border border-outline-variant">
          <span class="material-symbols-outlined text-[18px] text-on-surface">robot_2</span>
        </div>
        <div class="pt-1 text-on-surface font-body-md text-body-md">输入一条真实运维请求开始新的会话。</div>
      </div>`;
    return;
  }

  for (const item of state.currentSessionDetail.turns) {
    const replay = buildReplay(item);
    chatHistory.appendChild(renderUserRow(item.turn.user_input));
    if (replay.toolEvents.length > 0 || replay.consoleOutput || (item.approvals || []).length > 0) {
      chatHistory.appendChild(renderAssistantTrace(item.run, replay, item.approvals || []));
    }
    chatHistory.appendChild(renderAssistantMessage(item.run, replay));
  }

  queueMicrotask(scrollChatToBottom);
}

function renderUserRow(text) {
  const node = document.createElement("div");
  node.className = "flex gap-md group";
  node.innerHTML = `
    <div class="w-8 h-8 rounded-full bg-surface-container-high flex items-center justify-center shrink-0 border border-outline-variant">
      <span class="material-symbols-outlined text-[18px] text-on-surface">person</span>
    </div>
    <div class="pt-1 text-on-surface font-body-lg text-body-lg font-medium tracking-tight">${escapeHTML(text)}</div>
  `;
  return node;
}

function renderAssistantTrace(run, replay, approvals) {
  const node = document.createElement("div");
  node.className = "flex gap-md";
  node.innerHTML = `
    <div class="w-8 h-8 rounded-full bg-[#E6E4D9] flex items-center justify-center shrink-0 border border-[#E2E2DB]">
      <span class="material-symbols-outlined text-[18px] text-[#C96442]" style="font-variation-settings: 'FILL' 1;">robot_2</span>
    </div>
    <div class="flex-1 min-w-0">
      <div class="bg-surface-container-lowest border border-[#E2E2DB] rounded-xl p-md space-y-3"></div>
    </div>
  `;
  const box = node.querySelector(".space-y-3");

  if (approvals.length > 0) {
    const wrap = document.createElement("div");
    wrap.className = "space-y-2";
    approvals.forEach((approval) => {
      const badge = approvalDecisionMeta(approval.decision);
      const item = document.createElement("div");
      item.className = "border border-[#E2E2DB] rounded-lg p-sm bg-white";
      item.innerHTML = `
        <div class="flex items-center justify-between mb-1">
          <span class="font-label text-label text-on-surface font-medium">${escapeHTML(approval.tool_name)}</span>
          <span class="text-[11px] px-2 py-0.5 rounded-full ${badge.className}">${badge.label}</span>
        </div>
        <div class="font-body-sm text-body-sm text-secondary">${escapeHTML(approval.reason)}</div>
        <div class="font-body-sm text-[11px] text-secondary mt-1">${escapeHTML(approval.scope || "")}</div>
      `;
      wrap.appendChild(item);
    });
    box.appendChild(wrap);
  }

  if (replay.toolEvents.length > 0) {
    const list = document.createElement("div");
    list.className = "space-y-2";
    replay.toolEvents.forEach((event) => {
      const block = document.createElement("div");
      block.className = "border-b border-surface-variant pb-sm mb-sm last:border-b-0 last:pb-0 last:mb-0";
      block.innerHTML = `
        <div class="flex items-center justify-between mb-1">
          <div class="flex items-center gap-sm">
            <span class="material-symbols-outlined text-tertiary-container text-[18px]">${eventIcon(event.type)}</span>
            <span class="font-label text-label text-on-surface font-medium">${escapeHTML(eventTitle(event))}</span>
          </div>
          <span class="font-body-sm text-body-sm text-secondary">${formatTime(event.timestamp)}</span>
        </div>
        <div class="font-body-sm text-body-sm text-secondary">${escapeHTML(event.message || "")}</div>
        ${event.payload ? `<div class="markdown-body mt-2 text-xs text-on-surface-variant">${renderMarkdown(`\`\`\`json\n${JSON.stringify(event.payload, null, 2)}\n\`\`\``)}</div>` : ""}
      `;
      list.appendChild(block);
    });
    box.appendChild(list);
  }

  if (replay.consoleOutput) {
    const consoleNode = document.createElement("div");
    consoleNode.className = "rounded-lg border border-[#E2E2DB] bg-[#262624] text-[#F5F4ED] p-sm";
    consoleNode.innerHTML = `
      <div class="font-label text-[11px] uppercase tracking-wider text-[#C8C7BE] mb-2">工具输出</div>
      <pre class="text-xs whitespace-pre-wrap break-words font-mono">${escapeHTML(replay.consoleOutput)}</pre>
    `;
    box.appendChild(consoleNode);
  }

  if (box.childElementCount === 0) {
    return document.createDocumentFragment();
  }
  return node;
}

function renderAssistantMessage(run, replay) {
  const node = document.createElement("div");
  const statusMeta = runStatusMeta(run.status);
  node.className = "flex gap-md";
  node.innerHTML = `
    <div class="w-8 h-8 rounded-full bg-[#E6E4D9] flex items-center justify-center shrink-0 border border-[#E2E2DB]">
      <span class="material-symbols-outlined text-[18px] text-[#C96442]" style="font-variation-settings: 'FILL' 1;">robot_2</span>
    </div>
    <div class="flex-1 min-w-0">
      <div class="bg-surface-container-lowest border border-[#E2E2DB] rounded-xl p-md">
        <div class="flex items-center justify-between border-b border-surface-variant pb-sm mb-sm">
          <div class="flex items-center gap-sm">
            <span class="material-symbols-outlined ${statusMeta.color} text-[18px]">${statusMeta.icon}</span>
            <span class="font-label text-label text-on-surface font-medium">${escapeHTML(run.id)}</span>
          </div>
          <span class="font-body-sm text-body-sm text-secondary">${escapeHTML(run.status)}</span>
        </div>
        <div class="markdown-body font-body-sm text-body-sm text-secondary">${renderMarkdown(replay.assistantContent)}</div>
        <div class="font-body-sm text-[11px] text-secondary mt-3">${formatTime(replay.lastEventAt || run.updated_at)}</div>
      </div>
    </div>
  `;
  return node;
}

function renderAssets() {
  const list = document.getElementById("assets-host-list");
  const form = document.getElementById("assets-host-form");
  const openFormButton = document.getElementById("assets-open-form");
  if (!list || !form) return;

  list.innerHTML = "";
  if (state.hosts.length === 0) {
    list.innerHTML = `<div class="bg-white rounded-[16px] p-5 border border-[#E2E2DB] text-sm text-[#5C5C59]">当前没有主机，先在右侧注册一台。</div>`;
  }

  for (const host of state.hosts) {
    const modeLabel = host.mode === "local" ? "本地" : "SSH";
    const statusTone = host.status === "active" ? "text-tertiary-container bg-tertiary-container/10" : "text-secondary bg-[#E6E4D9]";
    const lastRunText = host.last_run_at ? `${escapeHTML(host.last_run_status || "unknown")} · ${formatTime(host.last_run_at)}` : "暂无真实运行";
    const node = document.createElement("div");
    node.className = "rounded-[16px] p-5 border border-[#E2E2DB] transition-colors relative overflow-hidden bg-white hover:bg-surface-container-low";
    node.innerHTML = `
      <div class="flex justify-between items-start mb-4">
        <div class="flex items-center gap-3">
          <div class="w-10 h-10 rounded-lg bg-surface-container flex items-center justify-center text-primary"><span class="material-symbols-outlined">dns</span></div>
          <div>
            <h3 class="font-h3 text-[18px] text-on-surface leading-tight">${escapeHTML(host.display_name)}</h3>
            <span class="font-label text-[12px] text-secondary">ID: ${escapeHTML(host.id)}</span>
          </div>
        </div>
        <span class="flex items-center gap-1 text-[12px] font-label ${statusTone} px-2 py-1 rounded-full">
          <span class="w-1.5 h-1.5 rounded-full ${host.status === "active" ? "bg-tertiary-container" : "bg-[#8D8D86]"}"></span>${host.status === "active" ? "活跃" : "已注册"}
        </span>
      </div>
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-4 mt-4 pt-4 border-t border-[#E2E2DB]/50">
        <div><p class="font-label text-[11px] text-secondary uppercase tracking-wider mb-1">地址</p><p class="font-body-sm text-[13px] text-on-surface font-mono">${escapeHTML(host.address || "localhost")}</p></div>
        <div><p class="font-label text-[11px] text-secondary uppercase tracking-wider mb-1">模式</p><p class="font-body-sm text-[13px] text-on-surface flex items-center gap-1"><span class="material-symbols-outlined text-[14px]">${host.mode === "local" ? "computer" : "terminal"}</span>${modeLabel}</p></div>
        <div><p class="font-label text-[11px] text-secondary uppercase tracking-wider mb-1">会话 / Runs</p><p class="font-body-sm text-[13px] text-on-surface">${host.session_count || 0} / ${host.total_runs || 0}</p></div>
        <div><p class="font-label text-[11px] text-secondary uppercase tracking-wider mb-1">待审批</p><p class="font-body-sm text-[13px] text-on-surface">${host.pending_approvals || 0}</p></div>
      </div>
      <div class="mt-4 pt-4 border-t border-[#E2E2DB]/50">
        <p class="font-label text-[11px] text-secondary uppercase tracking-wider mb-1">最近活动</p>
        <p class="font-body-sm text-[13px] text-on-surface">${lastRunText}</p>
      </div>
    `;
    list.appendChild(node);
  }

  if (!form.dataset.bound) {
    form.dataset.bound = "true";
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const data = new FormData(form);
      await request("/api/hosts", {
        method: "POST",
        body: JSON.stringify({
          id: String(data.get("id") || "").trim(),
          display_name: String(data.get("display_name") || "").trim(),
          address: String(data.get("address") || "").trim(),
          mode: String(data.get("mode") || "").trim(),
          port: Number(data.get("port") || 0),
          user: String(data.get("user") || "").trim(),
          password_env: String(data.get("password_env") || "").trim(),
        }),
      });
      form.reset();
      await loadCore();
      renderAssets();
    });
  }

  if (openFormButton && !openFormButton.dataset.bound) {
    openFormButton.dataset.bound = "true";
    openFormButton.addEventListener("click", () => {
      form.scrollIntoView({ behavior: "smooth", block: "start" });
      form.querySelector("[name=id]")?.focus();
    });
  }
}

function renderAutomation() {
  const list = document.getElementById("automation-run-list");
  if (!list) return;

  const startOfToday = new Date();
  startOfToday.setHours(0, 0, 0, 0);
  const todayRuns = state.runs.filter((run) => new Date(run.created_at) >= startOfToday);
  const successRuns = todayRuns.filter((run) => run.status === "completed");
  const runningRuns = state.runs.filter((run) => ["created", "running_agent", "tool_running", "waiting_approval"].includes(run.status));
  const interventionRuns = state.runs.filter((run) => run.status === "failed" || run.status === "denied" || run.pending_approvals > 0);
  const base = todayRuns.length || state.runs.length || 1;

  document.getElementById("automation-success-rate").textContent = `${Math.round((successRuns.length / base) * 100)}%`;
  document.getElementById("automation-success-meta").textContent = `/ ${todayRuns.length || state.runs.length} 次`;
  document.getElementById("automation-running-count").textContent = String(runningRuns.length);
  document.getElementById("automation-failed-count").textContent = String(interventionRuns.length);
  document.getElementById("automation-run-total").textContent = `全部 (${state.runs.length})`;

  list.innerHTML = "";
  for (const run of state.runs.slice(0, 20)) {
    const statusMeta = runStatusMeta(run.status);
    const item = document.createElement("div");
    item.className = `px-6 py-5 hover:bg-surface-container-low transition-colors group cursor-pointer ${statusMeta.tint}`;
    item.innerHTML = `
      <div class="flex items-center justify-between mb-2">
        <div class="flex items-center gap-3">
          <span class="material-symbols-outlined ${statusMeta.color}">${statusMeta.icon}</span>
          <span class="font-h2 text-on-surface text-base font-semibold">${escapeHTML(run.id)}</span>
          <span class="font-label text-on-surface-variant bg-[#E6E4D9] px-2 py-0.5 rounded text-[11px]">${escapeHTML(run.session_title || run.session_id || run.host_id)}</span>
          ${run.pending_approvals > 0 ? `<span class="font-label text-[#C96442] bg-[#F5E7E0] px-2 py-0.5 rounded text-[11px]">待审批 ${run.pending_approvals}</span>` : ""}
        </div>
        <span class="font-body-sm text-secondary text-[13px]">${formatTime(run.last_event_at || run.updated_at)}</span>
      </div>
      <div class="flex flex-col gap-2 ml-9">
        <div class="flex flex-wrap items-center gap-4 text-secondary">
          <div class="flex items-center gap-1.5"><span class="material-symbols-outlined text-[16px]">dns</span><span class="font-body-sm text-[13px]">${escapeHTML(run.host_display_name || run.host_id)}</span></div>
          <div class="flex items-center gap-1.5"><span class="material-symbols-outlined text-[16px]">schedule</span><span class="font-body-sm text-[13px]">${escapeHTML(run.status)}</span></div>
          <div class="flex items-center gap-1.5"><span class="material-symbols-outlined text-[16px]">history</span><span class="font-body-sm text-[13px]">${escapeHTML(run.last_event_type || "run.updated")}</span></div>
        </div>
        <div class="font-body-sm text-[13px] ${statusMeta.color} font-medium">${escapeHTML(truncateText(run.latest_assistant || run.session_preview || "处理中", 180))}</div>
      </div>
    `;
    item.addEventListener("click", () => {
      const url = new URL(window.location.origin + "/");
      if (run.session_id) url.searchParams.set("session", run.session_id);
      window.location.href = url.toString();
    });
    list.appendChild(item);
  }
}

function renderSettings() {
  const gatewayStatus = document.getElementById("settings-gateway-status");
  const policySummary = document.getElementById("settings-policy-summary");
  const presetName = document.getElementById("settings-preset-name");
  const model = document.getElementById("settings-model");
  const baseURL = document.getElementById("settings-base-url");
  const hostCount = document.getElementById("settings-host-count");
  const runCount = document.getElementById("settings-run-count");
  const box = document.getElementById("settings-capabilities");
  const presetList = document.getElementById("settings-preset-list");
  const form = document.getElementById("settings-gateway-form");
  const message = document.getElementById("settings-gateway-message");
  if (!gatewayStatus || !policySummary || !presetName || !model || !baseURL || !hostCount || !runCount || !box || !presetList || !form || !message) return;

  gatewayStatus.textContent = state.health?.status === "ok" ? "健康运行" : "异常";
  policySummary.textContent = state.health?.policy_summary || "暂无策略信息";
  presetName.textContent = state.health?.preset_name || "未配置";
  model.textContent = state.health?.model || "unknown-model";
  baseURL.textContent = state.health?.base_url || "--";
  hostCount.textContent = String(state.health?.total_hosts ?? state.hosts.length);
  runCount.textContent = String(state.health?.total_runs ?? state.runs.length);

  const selectedPreset = ensureGatewaySelection();
  const presets = gatewayPresets();

  presetList.innerHTML = "";
  if (presets.length === 0) {
    presetList.innerHTML = `<div class="rounded-[18px] border border-surface-variant/50 bg-surface p-4 text-sm text-secondary">当前还没有可用预设。</div>`;
  }
  for (const preset of presets) {
    const item = document.createElement("button");
    const isCurrent = preset.id === currentGatewayPresetId();
    const isSelected = preset.id === state.settingsSelectedPresetId;
    item.type = "button";
    item.dataset.presetId = preset.id;
    item.className = `w-full rounded-[20px] border px-4 py-4 text-left transition-colors ${
      isSelected
        ? "border-[#C96442] bg-[#FFF3EE] shadow-[0_8px_20px_rgba(201,100,66,0.08)]"
        : "border-surface-variant/60 bg-surface hover:bg-surface-container-low"
    }`;
    item.innerHTML = `
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <div class="font-label text-label text-on-surface">${escapeHTML(preset.name)}</div>
          <div class="mt-1 text-xs text-secondary break-all">${escapeHTML(preset.model)}</div>
        </div>
        ${isCurrent ? '<span class="rounded-full bg-[#F5E7E0] px-2 py-0.5 text-[11px] text-[#C96442]">当前</span>' : ""}
      </div>
      <div class="mt-3 text-xs text-secondary break-all">${escapeHTML(preset.base_url)}</div>
      <div class="mt-2 text-[11px] text-secondary">Key: ${escapeHTML(maskSecret(preset.api_key))}</div>
    `;
    item.addEventListener("click", () => {
      state.settingsSelectedPresetId = preset.id;
      renderSettings();
    });
    presetList.appendChild(item);
  }

  const presetIDInput = document.getElementById("settings-preset-id");
  const presetNameInput = document.getElementById("settings-preset-name-input");
  const presetModelInput = document.getElementById("settings-preset-model-input");
  const presetBaseURLInput = document.getElementById("settings-preset-base-url-input");
  const presetAPIKeyInput = document.getElementById("settings-preset-api-key-input");
  const presetActiveInput = document.getElementById("settings-preset-active-input");
  const activateButton = document.getElementById("settings-activate-preset");
  const deleteButton = document.getElementById("settings-delete-preset");
  const addButton = document.getElementById("settings-add-preset");
  if (!presetIDInput || !presetNameInput || !presetModelInput || !presetBaseURLInput || !presetAPIKeyInput || !presetActiveInput || !activateButton || !deleteButton || !addButton) {
    return;
  }

  const effectivePreset = selectedPreset || {
    id: "",
    name: "",
    model: "",
    base_url: "",
    api_key: "",
  };
  presetIDInput.value = effectivePreset.id || "";
  presetNameInput.value = effectivePreset.name || "";
  presetModelInput.value = effectivePreset.model || "";
  presetBaseURLInput.value = effectivePreset.base_url || "";
  presetAPIKeyInput.value = effectivePreset.api_key || "";
  presetActiveInput.checked = effectivePreset.id ? effectivePreset.id === currentGatewayPresetId() : true;
  activateButton.disabled = !effectivePreset.id || effectivePreset.id === currentGatewayPresetId();
  deleteButton.disabled = !effectivePreset.id || presets.length <= 1;
  message.textContent = "保存后的配置会持久化到本地数据目录，并同步更新运行中的 API Gateway Pro。";

  if (!form.dataset.bound) {
    form.dataset.bound = "true";
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      try {
        const next = copyJSON(state.gatewaySettings);
        const name = presetNameInput.value.trim();
        const id = (presetIDInput.value || slugifyPresetName(name)).trim();
        const payload = {
          id,
          name,
          model: presetModelInput.value.trim(),
          base_url: presetBaseURLInput.value.trim(),
          api_key: presetAPIKeyInput.value.trim(),
        };
        const existingIndex = (next.presets || []).findIndex((item) => item.id === id);
        if (existingIndex >= 0) {
          next.presets[existingIndex] = { ...next.presets[existingIndex], ...payload };
        } else {
          next.presets = [...(next.presets || []), payload];
        }
        if (presetActiveInput.checked || !(next.current_preset_id || "").trim()) {
          next.current_preset_id = id;
        }
        message.textContent = "正在保存网关预设...";
        state.gatewaySettings = await request("/api/settings/gateway", {
          method: "PUT",
          body: JSON.stringify(next),
        });
        state.settingsSelectedPresetId = id;
        await loadCore();
        message.textContent = "网关预设已保存并同步生效。";
        renderSettings();
      } catch (error) {
        message.textContent = `保存失败：${error.message}`;
      }
    });
  }

  if (!activateButton.dataset.bound) {
    activateButton.dataset.bound = "true";
    activateButton.addEventListener("click", async () => {
      try {
        const id = presetIDInput.value.trim();
        if (!id) return;
        const next = copyJSON(state.gatewaySettings);
        next.current_preset_id = id;
        message.textContent = "正在切换当前生效预设...";
        state.gatewaySettings = await request("/api/settings/gateway", {
          method: "PUT",
          body: JSON.stringify(next),
        });
        state.settingsSelectedPresetId = id;
        await loadCore();
        message.textContent = "当前生效预设已切换。";
        renderSettings();
      } catch (error) {
        message.textContent = `切换失败：${error.message}`;
      }
    });
  }

  if (!deleteButton.dataset.bound) {
    deleteButton.dataset.bound = "true";
    deleteButton.addEventListener("click", async () => {
      try {
        const id = presetIDInput.value.trim();
        if (!id) return;
        const next = copyJSON(state.gatewaySettings);
        next.presets = (next.presets || []).filter((item) => item.id !== id);
        if (next.current_preset_id === id) {
          next.current_preset_id = next.presets[0]?.id || "";
        }
        message.textContent = "正在删除网关预设...";
        state.gatewaySettings = await request("/api/settings/gateway", {
          method: "PUT",
          body: JSON.stringify(next),
        });
        state.settingsSelectedPresetId = next.current_preset_id || next.presets[0]?.id || "";
        await loadCore();
        message.textContent = "网关预设已删除。";
        renderSettings();
      } catch (error) {
        message.textContent = `删除失败：${error.message}`;
      }
    });
  }

  if (!addButton.dataset.bound) {
    addButton.dataset.bound = "true";
    addButton.addEventListener("click", () => {
      state.settingsSelectedPresetId = "";
      presetIDInput.value = "";
      presetNameInput.value = "";
      presetModelInput.value = "";
      presetBaseURLInput.value = state.health?.base_url || "https://api.longcat.chat";
      presetAPIKeyInput.value = "";
      presetActiveInput.checked = true;
      message.textContent = "填写新预设后点击保存即可。";
      presetNameInput.focus();
    });
  }

  const capabilities = state.health?.capabilities || [];
  box.innerHTML = "";
  if (capabilities.length === 0) {
    box.innerHTML = `<div class="bg-surface rounded-lg border border-surface-variant/50 p-md text-sm text-secondary">暂无真实运行证据，先发起一次真实 run 后这里会自动填充。</div>`;
    return;
  }

  for (const item of capabilities) {
    const node = document.createElement("div");
    node.className = "bg-surface rounded-lg border border-surface-variant/50 p-md";
    node.innerHTML = `
      <div class="flex items-center justify-between gap-3 mb-2">
        <div class="font-label text-label text-on-surface font-medium">${escapeHTML(item.title)}</div>
        <div class="text-[11px] text-secondary">${item.evidence_count || 0} 条证据</div>
      </div>
      <div class="text-sm text-on-surface-variant">${escapeHTML(item.description)}</div>
      <div class="text-[11px] text-secondary mt-2">${formatTime(item.last_seen_at)}</div>
    `;
    box.appendChild(node);
  }
}

function runStatusMeta(status) {
  switch (status) {
    case "completed":
      return { icon: "check_circle", color: "text-tertiary-container", tint: "" };
    case "failed":
    case "denied":
      return { icon: "cancel", color: "text-error", tint: "bg-error-container/5" };
    case "waiting_approval":
      return { icon: "warning", color: "text-[#C96442]", tint: "bg-[#F5E7E0]/40" };
    default:
      return { icon: "autorenew", color: "text-primary-container", tint: "" };
  }
}

function runStatusText(status) {
  switch (status) {
    case "waiting_approval":
      return "等待人工审批。";
    case "completed":
      return "执行完成。";
    case "failed":
      return "执行失败。";
    case "denied":
      return "执行被拒绝。";
    case "running_agent":
    case "tool_running":
    case "created":
      return "处理中。";
    default:
      return "等待更多实时数据。";
  }
}

function eventTitle(event) {
  switch (event.type) {
    case "run.created":
      return "Run 已创建";
    case "run.running_agent":
      return "Agent 持续推进";
    case "run.policy_checked":
      return "Policy 已检查";
    case "run.tool_running":
      return `执行 ${event.message}`;
    case "run.tool_finished":
      return `${event.message} 已返回`;
    case "run.waiting_approval":
      return "等待审批";
    case "run.approval_resolved":
      return "审批已处理";
    case "run.completed":
      return "执行完成";
    case "run.failed":
      return "执行失败";
    default:
      return event.type;
  }
}

function eventIcon(type) {
  switch (type) {
    case "run.created":
      return "fiber_new";
    case "run.running_agent":
      return "psychology";
    case "run.policy_checked":
      return "verified_user";
    case "run.tool_running":
      return "terminal";
    case "run.tool_finished":
      return "check_circle";
    case "run.waiting_approval":
      return "warning";
    case "run.approval_resolved":
      return "fact_check";
    case "run.completed":
      return "task_alt";
    case "run.failed":
      return "error";
    default:
      return "robot_2";
  }
}

function approvalDecisionMeta(decision) {
  if (!decision) return { label: "待处理", className: "bg-[#F5E7E0] text-[#C96442]" };
  if (["approve", "approved", "allow"].includes(String(decision).toLowerCase())) {
    return { label: "已批准", className: "bg-[#E3F1ED] text-[#00796B]" };
  }
  return { label: "已拒绝", className: "bg-[#FDECEA] text-[#B3261E]" };
}

function scrollChatToBottom() {
  const scroller = document.querySelector("main section .overflow-y-auto");
  if (!scroller) return;
  scroller.scrollTop = scroller.scrollHeight;
}

async function initChatPage() {
  window.scrollTo(0, 0);
  const querySession = new URL(window.location.href).searchParams.get("session");
  state.currentSessionId = querySession || latestSessionId();
  if (state.currentSessionId) {
    try {
      await loadSessionDetail(state.currentSessionId);
    } catch (error) {
      if (querySession) {
        state.currentSessionId = latestSessionId();
        if (state.currentSessionId) {
          await loadSessionDetail(state.currentSessionId);
        }
      } else {
        throw error;
      }
    }
  }

  const newSessionButton = document.getElementById("new-session-button");
  const chatForm = document.getElementById("chat-form");
  const input = document.getElementById("chat-input");

  newSessionButton?.addEventListener("click", () => {
    state.currentSessionId = "";
    state.currentSessionDetail = null;
    const url = new URL(window.location.href);
    url.searchParams.delete("session");
    window.history.replaceState({}, "", url.toString());
    renderChat();
    input?.focus();
  });

  if (input && !input.dataset.bound) {
    input.dataset.bound = "true";
    const resize = () => {
      input.style.height = "auto";
      input.style.height = `${Math.min(input.scrollHeight, 220)}px`;
    };
    input.addEventListener("input", resize);
    resize();
  }

  chatForm?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const text = input?.value.trim() || "";
    if (!text) return;
    const result = await request("/api/runs", {
      method: "POST",
      body: JSON.stringify({
        host_id: getDefaultHostId(),
        session_id: state.currentSessionId || "",
        user_input: text,
        requested_by: "web",
      }),
    });
    if (input) {
      input.value = "";
      input.style.height = "auto";
    }
    await loadCore();
    await loadSessionDetail(result.session_id);
    renderChat();
  });

  renderChat();
}

async function init() {
  await loadCore();
  connectGlobalEvents();
  if (page === "chat") {
    await initChatPage();
  }
  renderPage();
}

init().catch((error) => {
  document.body.innerHTML = `<pre style="padding:24px;color:#8a2d2d;">${escapeHTML(error.message)}</pre>`;
});

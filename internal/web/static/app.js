const page = document.body.dataset.page;

const TRACE_EVENT_TYPES = new Set([
  "run.created",
  "run.running_agent",
  "run.policy_checked",
  "run.tool_running",
  "run.tool_finished",
  "run.waiting_approval",
  "run.approval_resolved",
  "run.policy_override_requested",
  "run.policy_override_resolved",
  "run.completed",
  "run.failed",
]);

const REFRESH_EVENT_TYPES = new Set([
  "run.created",
  "run.waiting_approval",
  "run.approval_resolved",
  "run.policy_override_requested",
  "run.policy_override_resolved",
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
  "run.policy_override_requested",
  "run.policy_override_resolved",
  "run.completed",
  "run.failed",
]);

const state = {
  health: null,
  gatewaySettings: null,
  operatorProfile: null,
  policyConfig: null,
  knowledge: [],
  hosts: [],
  runs: [],
  approvals: [],
  automations: [],
  sessions: [],
  settingsSelectedPresetId: "",
  selectedHostId: "",
  assetEditingHostId: "",
  currentSessionId: "",
  currentSessionDetail: null,
  chatComposerBypass: false,
  liveEvents: new Map(),
  eventSource: null,
  streamConnected: false,
  refreshTimer: null,
  refreshInFlight: null,
  sessionDetailRequest: null,
  sessionDetailLoadingId: "",
  refreshDetail: false,
  chatApprovalExpandedOverride: null,
  chatApprovalLastPendingCount: 0,
};

async function request(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (options.body && !headers["Content-Type"]) headers["Content-Type"] = "application/json";
  const response = await fetch(path, {
    ...options,
    headers,
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

function compactApprovalBatchID(value) {
  const text = String(value || "");
  if (text.length <= 28) return text;
  return `${text.slice(0, 18)}…${text.slice(-6)}`;
}

function truncateText(value, limit = 140) {
  const text = String(value ?? "").replace(/\s+/g, " ").trim();
  if (!text) return "";
  if (text.length <= limit) return text;
  return `${text.slice(0, limit - 1)}…`;
}

function automationStatusLabel(status) {
  const labels = {
    healthy: "健康",
    cooldown: "冷却中",
    created: "已创建",
    running_agent: "执行中",
    waiting_approval: "待审批",
    completed: "已完成",
    failed: "失败",
    sample_failed: "采样失败",
    trigger_failed: "触发失败",
    test_threshold_not_matched: "测试未命中",
    test_cooldown: "测试冷却中",
    test_trigger_failed: "测试触发失败",
  };
  return labels[status] || status || "未触发";
}

function automationStatusClass(status) {
  if (["healthy", "completed", "created"].includes(status)) return "text-[#00796B] bg-[#E3F1ED]";
  if (["cooldown", "running_agent", "waiting_approval", "test_threshold_not_matched", "test_cooldown"].includes(status)) return "text-[#8A5A00] bg-[#FFF5D6]";
  if (status) return "text-[#9A4021] bg-[#FFF1EC]";
  return "text-secondary bg-[#E6E4D9]";
}

function formatTokenCount(value) {
  const count = Number(value || 0);
  if (!count) return "--";
  if (count >= 10000) return `${Math.round(count / 1000)}k`;
  if (count >= 1000) return `${(count / 1000).toFixed(1)}k`;
  return String(count);
}

function copyJSON(value) {
  return value ? JSON.parse(JSON.stringify(value)) : value;
}

function runtimeSettings() {
  return state.gatewaySettings?.runtime_settings || {};
}

function runtimeSettingsWithDefaults() {
  return {
    max_agent_steps: 20,
    bypass_approvals: false,
    context_soft_limit_tokens: 20000,
    compression_trigger_tokens: 16000,
    response_reserve_tokens: 4000,
    recent_full_turns: 2,
    older_user_ledger_entries: 6,
    host_profile_ttl_minutes: 30,
    tool_result_max_chars: 6000,
    tool_result_head_chars: 4000,
    tool_result_tail_chars: 1200,
    sop_retrieval_limit: 3,
    ...runtimeSettings(),
  };
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

function presetKeyLabel(preset) {
  if (preset?.api_key_configured) return "已配置";
  return maskSecret(preset?.api_key);
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

function sessionTitle(session) {
  return firstNonEmpty(session?.title, session?.preview, session?.last_input, "未命名会话");
}

function sessionPreviewText(session) {
  return firstNonEmpty(session?.preview, session?.summary, session?.last_input, "暂无更多上下文。");
}

function latestRunForSessionDetail(detail) {
  return detail?.turns?.at(-1)?.run || null;
}

function latestTurnForSessionDetail(detail) {
  return detail?.turns?.at(-1)?.turn || null;
}

function latestSessionActivity(detail) {
  return detail?.turns?.at(-1)?.last_event_at || detail?.session?.updated_at || null;
}

function countSessionTools(detail) {
  return (detail?.turns || []).reduce((total, item) => total + (item?.turn?.tool_results?.length || 0), 0);
}

function findHostById(hostId) {
  return state.hosts.find((host) => host.id === hostId) || null;
}

function hostModeLabel(mode) {
  return mode === "ssh" ? "SSH" : "本机";
}

function isValidEnvVarName(value) {
  return /^[A-Za-z_][A-Za-z0-9_]*$/.test(String(value || "").trim());
}

function sshPasswordMode(value) {
  const text = String(value || "").trim();
  if (!text) return "missing";
  return isValidEnvVarName(text) ? "env" : "literal";
}

function syncSelectedHost() {
  const currentHostId = state.currentSessionDetail?.host?.id;
  if (currentHostId) {
    state.selectedHostId = currentHostId;
    return;
  }
  if (state.selectedHostId && findHostById(state.selectedHostId)) return;
  state.selectedHostId = state.hosts[0]?.id || "";
}

async function loadCore() {
  const [health, gatewaySettings, operatorProfile] = await Promise.all([
    request("/api/health"),
    request("/api/settings/gateway"),
    request("/api/settings/operator"),
  ]);
  state.health = health;
  state.gatewaySettings = gatewaySettings;
  state.operatorProfile = operatorProfile;

  if (page === "settings") {
    const [knowledge, policyConfig] = await Promise.all([
      request("/api/knowledge"),
      request("/api/settings/policy"),
    ]);
    state.knowledge = sortByNewest(knowledge.items || [], "updated_at");
    state.policyConfig = policyConfig;
  }

  if (["chat", "assets", "automation"].includes(page)) {
    const hosts = await request("/api/hosts");
    state.hosts = sortByNewest(hosts.items || [], "updated_at");
  }
  if (["chat", "history", "assets"].includes(page)) {
    const sessions = await request("/api/sessions?limit=160");
    state.sessions = sortByNewest(sessions.items || [], "updated_at");
  }
  if (["history", "assets"].includes(page)) {
    const runs = await request("/api/runs?limit=240");
    state.runs = sortByNewest(runs.items || [], "updated_at");
  }
  if (["chat", "assets"].includes(page)) {
    const approvals = await request("/api/approvals?pending=true&limit=120");
    state.approvals = sortByNewest(approvals.items || [], "created_at");
  }
  if (page === "automation") {
    const automations = await request("/api/automations");
    state.automations = sortByNewest(automations.items || [], "updated_at");
  }
  if (!state.settingsSelectedPresetId) {
    state.settingsSelectedPresetId = gatewaySettings.current_preset_id || "";
  }
  syncSelectedHost();
}

async function loadSessionDetail(sessionId) {
  if (!sessionId) {
    state.currentSessionId = "";
    state.currentSessionDetail = null;
    return;
  }
  if (state.sessionDetailRequest) {
    state.sessionDetailRequest.abort();
  }
  const controller = new AbortController();
  state.sessionDetailRequest = controller;
  state.sessionDetailLoadingId = sessionId;
  let detail = null;
  try {
    detail = await request(`/api/sessions/${sessionId}?turn_limit=40&events_limit=120&compact=true`, {
      signal: controller.signal,
    });
  } catch (error) {
    if (error.name === "AbortError") return null;
    throw error;
  } finally {
    if (state.sessionDetailRequest === controller) {
      state.sessionDetailRequest = null;
      state.sessionDetailLoadingId = "";
    }
  }
  if (!detail || controller.signal.aborted) return null;
  state.currentSessionDetail = detail;
  state.currentSessionId = sessionId;
  state.selectedHostId = state.currentSessionDetail?.host?.id || state.selectedHostId;
  state.chatComposerBypass = Boolean(state.currentSessionDetail?.session?.mode?.bypass_approvals ?? runtimeSettingsWithDefaults().bypass_approvals);
  const url = new URL(window.location.href);
  url.searchParams.set("session", sessionId);
  window.history.replaceState({}, "", url.toString());
  return detail;
}

function getDefaultHostId() {
  syncSelectedHost();
  return state.currentSessionDetail?.host?.id || state.selectedHostId || state.hosts[0]?.id || "local";
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
      if (needDetail && page === "chat" && state.currentSessionId) {
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

function disconnectGlobalEvents() {
  if (!state.eventSource) return;
  state.eventSource.close();
  state.eventSource = null;
  state.streamConnected = false;
}

function connectGlobalEvents() {
  disconnectGlobalEvents();
  const source = new EventSource("/api/events/stream");
  state.eventSource = source;

  source.onopen = () => {
    state.streamConnected = true;
    renderSharedShell();
  };

  source.onerror = () => {
    state.streamConnected = false;
    renderSharedShell();
  };

  source.onmessage = (message) => {
    const event = JSON.parse(message.data);
    appendLiveEvent(event);

    const affectsCurrentSession = Boolean(
      state.currentSessionDetail?.turns?.some((item) => item.run.id === event.run_id),
    );

    if (page === "chat" && LIVE_RENDER_EVENT_TYPES.has(event.type) && affectsCurrentSession) {
      renderPage();
    }

    if (REFRESH_EVENT_TYPES.has(event.type)) {
      scheduleRefresh({ detail: affectsCurrentSession });
      return;
    }

    if (event.type === "run.created" || event.type === "run.tool_running" || event.type === "run.tool_finished") {
      scheduleRefresh({ detail: affectsCurrentSession });
    }
  };
}

function bindGlobalNavigationCleanup() {
  if (document.body.dataset.navigationCleanupBound) return;
  document.body.dataset.navigationCleanupBound = "true";

  const cleanup = () => {
    disconnectGlobalEvents();
    if (state.sessionDetailRequest) state.sessionDetailRequest.abort();
  };
  window.addEventListener("pagehide", cleanup);
  window.addEventListener("beforeunload", cleanup);
  document.addEventListener("click", (event) => {
    const target = event.target;
    if (!(target instanceof Element)) return;
    const anchor = target.closest('a[href^="/"]');
    if (!anchor) return;
    disconnectGlobalEvents();
  }, true);
}

function renderPage() {
  renderSharedShell();
  switch (page) {
    case "chat":
      renderChat();
      break;
    case "history":
      renderHistory();
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

function renderSharedShell() {
  const healthLabel = document.getElementById("shell-health-label");
  const healthIndicator = document.getElementById("shell-health-indicator");
  const healthModel = document.getElementById("shell-health-model");
  const healthSummary = document.getElementById("shell-health-summary");
  if (healthLabel) {
    healthLabel.textContent = state.health?.status === "ok" ? "网关健康运行" : "网关状态异常";
  }
  if (healthIndicator) {
    healthIndicator.classList.toggle("is-live", state.health?.status === "ok");
  }
  if (healthModel) {
    const preset = state.health?.preset_name || "未配置预设";
    const model = state.health?.model || "unknown-model";
    healthModel.textContent = [preset, model, state.streamConnected ? "SSE 已连接" : "SSE 重连中"].join(" · ");
  }
  if (healthSummary) {
    healthSummary.textContent = `Hosts ${state.health?.total_hosts ?? state.hosts.length} · Sessions ${state.health?.total_sessions ?? state.sessions.length} · Runs ${state.health?.total_runs ?? state.runs.length}`;
  }
}

function buildReplay(item) {
  const mergedEvents = dedupeEvents([...(item.events || []), ...(state.liveEvents.get(item.run.id) || [])]);
  const toolEvents = [];
  let delta = "";
  let assistantMessage = "";
  let consoleOutput = "";

  const turnMessages = item.turn?.messages || [];
  const toolResults = item.turn?.tool_results || [];

  for (const event of mergedEvents) {
    if (event.type === "run.message_delta") delta += event.message || "";
    if (event.type === "run.assistant_message" && event.message) assistantMessage = event.message;
    if (event.type === "run.stdout") consoleOutput += event.message || "";
    if (event.type === "run.stderr") consoleOutput += `[stderr] ${event.message || ""}`;
    if (TRACE_EVENT_TYPES.has(event.type)) toolEvents.push(event);
  }

  const assistantMessages = turnMessages.filter((message) => message.role === "assistant" && (message.content || "").trim());
  const toolOutput = toolResults
    .map((result) => result.raw_result || result.model_result || "")
    .filter(Boolean)
    .join("\n\n");

  const assistantContent = firstNonEmpty(
    assistantMessages.at(-1)?.content,
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
    consoleOutput: firstNonEmpty(toolOutput, item.console_output, consoleOutput),
    toolResults,
    toolEvents: toolEvents.length > 0 ? toolEvents : item.tool_events || [],
    waitingApproval: item.waiting_approval || (item.approvals || []).some((approval) => !approval.decision),
    lastEventAt: item.last_event_at || mergedEvents.at(-1)?.timestamp || item.run?.updated_at,
  };
}

function renderSessionList() {
  const count = document.getElementById("chat-session-count");
  const list = document.getElementById("chat-session-list");
  if (!count || !list) return;

  count.textContent = String(state.sessions.length);
  list.innerHTML = "";

  if (state.sessions.length === 0) {
    list.innerHTML = `<div class="app-empty-state">当前还没有会话，点击“新建会话”开始第一条真实请求。</div>`;
    return;
  }

  for (const session of state.sessions) {
    const item = document.createElement("button");
    const isActive = session.id === state.currentSessionId;
    const statusMeta = runStatusMeta(session.run_status);
    item.type = "button";
    item.className = `app-session-item${isActive ? " is-active" : ""}`;
    item.innerHTML = `
      <div class="app-session-item-head">
        <strong>${escapeHTML(sessionTitle(session))}</strong>
        <span class="app-session-item-status ${statusMeta.color}">${escapeHTML(session.run_status || "idle")}</span>
      </div>
      <div class="app-session-item-preview">${escapeHTML(truncateText(sessionPreviewText(session), 92))}</div>
      <div class="app-session-item-meta">
        <span>${escapeHTML(session.host_display_name || session.host_id || "未知主机")}</span>
        <span>${formatTime(session.last_event_at || session.updated_at)}</span>
      </div>
    `;
    item.addEventListener("click", async () => {
      if (session.id === state.currentSessionId && state.currentSessionDetail) return;
      state.currentSessionId = session.id;
      state.currentSessionDetail = null;
      renderChat();
      await loadSessionDetail(session.id);
      renderChat();
    });
    list.appendChild(item);
  }
}

function renderChatSummary() {
  const title = document.getElementById("chat-conversation-title");
  const summary = document.getElementById("chat-conversation-summary");
  const memory = document.getElementById("chat-memory-status");
  const host = document.getElementById("chat-conversation-host");
  const run = document.getElementById("chat-conversation-run");
  const budget = document.getElementById("chat-conversation-budget");
  if (!title || !summary || !memory || !host || !run || !budget) return;
  const currentRuntime = runtimeSettingsWithDefaults();

  if (!state.currentSessionDetail) {
    const selectedHost = findHostById(getDefaultHostId());
    title.textContent = "选择或创建一段运维会话";
    summary.textContent = "左侧仅显示已有 session；切换资产请在输入框上方选择目标主机后发起新会话。";
    memory.textContent = selectedHost ? `新会话会在 ${selectedHost.display_name || selectedHost.id} 上创建独立记忆上下文。` : "多轮记忆状态将在真实会话后显示。";
    host.textContent = selectedHost ? `目标主机 ${selectedHost.display_name || selectedHost.id}` : "未选主机";
    run.textContent = selectedHost ? `新会话将走 ${hostModeLabel(selectedHost.mode)} 执行链路` : "暂无运行";
    budget.textContent = `Max steps ${currentRuntime.max_agent_steps} · Soft ${formatTokenCount(currentRuntime.context_soft_limit_tokens)} tokens`;
    return;
  }

  const session = state.currentSessionDetail.session;
  const latestRun = latestRunForSessionDetail(state.currentSessionDetail);
  const latestTurn = latestTurnForSessionDetail(state.currentSessionDetail);
  const memoryState = state.currentSessionDetail.memory || {};
  const promptStats = latestTurn?.prompt_stats || {};
  title.textContent = sessionTitle(session);
  summary.textContent = `${state.currentSessionDetail.turns.length} 次交互 · 最近活动 ${formatTime(latestSessionActivity(state.currentSessionDetail)) || "未知"}`;
  memory.textContent = [
    `完整保留 ${Math.max(Number(promptStats.recent_full_turn_count || 0), 0)} turn`,
    memoryState.last_compacted_at ? `上次压缩 ${formatTime(memoryState.last_compacted_at)}` : "尚未触发压缩",
    memoryState.profile_stale ? "Host Profile 已过期" : "Host Profile 最新",
    promptStats.estimated_prompt_tokens ? `最近 prompt 约 ${promptStats.estimated_prompt_tokens} tokens` : "等待首轮预算",
  ].join(" · ");
  host.textContent = state.currentSessionDetail.host.display_name || state.currentSessionDetail.host.id;
  run.textContent = latestRun ? `最近状态 ${latestRun.status}` : "暂无运行";
  budget.textContent = `Max steps ${currentRuntime.max_agent_steps} · Prompt soft ${formatTokenCount(currentRuntime.context_soft_limit_tokens)} tokens`;
}

function renderChatComposer() {
  const select = document.getElementById("chat-host-select");
  const label = document.getElementById("chat-host-selection-label");
  const note = document.getElementById("chat-composer-note");
  const bypassToggle = document.getElementById("chat-bypass-toggle");
  if (!select || !label || !note || !bypassToggle) return;

  const locked = Boolean(state.currentSessionDetail?.session?.id);
  const activeHost = findHostById(getDefaultHostId());
  select.innerHTML = "";

  if (state.hosts.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "暂无主机";
    select.appendChild(option);
    select.disabled = true;
    label.textContent = "请先去资产管理登记一台主机。";
    note.textContent = "所有请求都经过统一 gateway、policy 和真实执行链路。";
    bypassToggle.checked = runtimeSettingsWithDefaults().bypass_approvals;
    return;
  }

  state.hosts.forEach((host) => {
    const option = document.createElement("option");
    option.value = host.id;
    option.textContent = `${host.display_name || host.id} · ${hostModeLabel(host.mode)}`;
    select.appendChild(option);
  });
  select.value = activeHost?.id || state.hosts[0].id;
  select.disabled = locked;

  if (!activeHost) {
    label.textContent = "当前目标主机不可用。";
    note.textContent = "所有请求都经过统一 gateway、policy 和真实执行链路。";
    bypassToggle.checked = composerBypassEnabled();
    return;
  }

  bypassToggle.checked = composerBypassEnabled();

  const invalidSSHPasswordEnv = activeHost.mode === "ssh" && !isValidEnvVarName(activeHost.password_env);
  label.textContent = locked
    ? `当前会话固定在 ${activeHost.display_name || activeHost.id}`
    : `${activeHost.display_name || activeHost.id} · ${activeHost.address || "localhost"}`;

  if (locked) {
    note.textContent = `当前 session 已绑定到 ${activeHost.display_name || activeHost.id}。点击“新建会话”后才能切换主机。`;
  } else if (invalidSSHPasswordEnv) {
    note.textContent = "当前 SSH 主机使用明文密码。可以继续执行，但建议改成环境变量名。";
  } else {
    note.textContent = `新会话会绑定到 ${activeHost.display_name || activeHost.id}，并走真实 ${hostModeLabel(activeHost.mode)} 执行链路。`;
  }
}

function renderChatApprovals() {
  const summary = document.getElementById("chat-approval-summary");
  const note = document.getElementById("chat-approval-note");
  if (!summary || !note) return;

  const sessionBatches = state.currentSessionDetail?.pending_batches || [];
  const sessionApprovalCount = sessionBatches.reduce((total, batch) => total + ((batch.approvals || []).filter((item) => !item.decision).length || 0), 0);
  const globalPending = state.approvals.filter((item) => !item.decision);

  if (state.currentSessionDetail) {
    summary.textContent = sessionApprovalCount > 0 ? `当前会话有 ${sessionApprovalCount} 项待审批` : "当前会话无需审批";
    note.textContent = sessionApprovalCount > 0
      ? "请在输入框上方的审批条中处理本批工具审批。"
      : globalPending.length > 0
        ? `其它会话还有 ${globalPending.length} 项待审批，切到对应会话后处理。`
        : "当前所有会话都没有待处理审批。";
    return;
  }

  summary.textContent = globalPending.length > 0 ? `共有 ${globalPending.length} 项待审批` : "当前没有待审批";
  note.textContent = globalPending.length > 0 ? "进入对应会话后可在输入框上方直接审批。" : "审批动作已迁移到输入区上方的批处理审批条。";
}

function composerBypassEnabled() {
  return Boolean(state.currentSessionDetail?.session?.mode?.bypass_approvals ?? state.chatComposerBypass);
}

async function syncComposerBypass(nextValue) {
  state.chatComposerBypass = Boolean(nextValue);
  if (!state.currentSessionId || !state.currentSessionDetail?.session?.id) {
    renderChat();
    return;
  }
  const updated = await request(`/api/sessions/${state.currentSessionDetail.session.id}/mode`, {
    method: "PUT",
    body: JSON.stringify({ bypass_approvals: state.chatComposerBypass }),
  });
  state.currentSessionDetail.session = updated;
  state.sessions = state.sessions.map((item) => item.id === updated.id ? { ...item, mode: updated.mode, updated_at: updated.updated_at } : item);
  renderChat();
}

function renderChatApprovalBar() {
  const box = document.getElementById("chat-approval-bar");
  if (!box) return;
  const batches = state.currentSessionDetail?.pending_batches || [];
  box.innerHTML = "";
  if (batches.length === 0) {
    box.classList.remove("has-content", "is-expanded", "is-collapsed");
    return;
  }
  const totalApprovals = batches.reduce((total, batch) => total + (batch.approvals || []).length, 0);
  const totalPending = batches.reduce((total, batch) => total + (batch.approvals || []).filter((item) => !item.decision).length, 0);
  const previousPending = state.chatApprovalLastPendingCount;
  if (previousPending === 0 && totalPending > 0) {
    state.chatApprovalExpandedOverride = null;
  }
  if (previousPending > 0 && totalPending === 0) {
    state.chatApprovalExpandedOverride = false;
  }
  state.chatApprovalLastPendingCount = totalPending;
  const expanded = state.chatApprovalExpandedOverride === null ? totalPending > 0 : Boolean(state.chatApprovalExpandedOverride);
  box.classList.add("has-content");
  box.classList.toggle("is-expanded", expanded);
  box.classList.toggle("is-collapsed", !expanded);

  const summary = document.createElement("div");
  summary.className = "app-chat-approval-summary-strip";
  const toggle = document.createElement("button");
  toggle.type = "button";
  toggle.className = "app-chat-approval-toggle";
  toggle.textContent = expanded ? "收起" : "展开";
  toggle.addEventListener("click", () => {
    state.chatApprovalExpandedOverride = !expanded;
    renderChatApprovalBar();
    scrollChatToBottom();
  });
  summary.innerHTML = `
    <span>${totalPending > 0 ? "待审批" : "审批已完成"}</span>
    <strong>${batches.length} 批 · ${totalPending > 0 ? `${totalPending} 待处理` : `${totalApprovals} 已处理`}</strong>
  `;
  summary.appendChild(toggle);
  box.appendChild(summary);

  if (!expanded) {
    return;
  }
  if (totalPending === 0) {
    const done = document.createElement("div");
    done.className = "app-chat-approval-done-card";
    done.textContent = "所有审批已经处理完成，执行状态会继续在对话流和 Live Trace 中更新。";
    box.appendChild(done);
    return;
  }
  batches.forEach((batch) => {
    const node = document.createElement("div");
    node.className = "app-chat-approval-batch";
    const approvals = batch.approvals || [];
    const pendingCount = approvals.filter((item) => !item.decision).length;
    node.innerHTML = `
      <div class="app-chat-approval-batch-head">
        <div>
          <div class="app-chat-approval-batch-title" title="审批批次 ${escapeHTML(batch.id)}">审批 ${escapeHTML(compactApprovalBatchID(batch.id))}</div>
          <div class="app-chat-approval-batch-meta">${batch.executing ? "执行中" : pendingCount > 0 ? `待处理 ${pendingCount} 项` : "已决议，等待执行"}</div>
        </div>
      </div>
    `;
    const list = document.createElement("div");
    list.className = "app-chat-approval-batch-list";
    approvals.forEach((approval) => {
      const item = document.createElement("div");
      item.className = `app-chat-approval-item${approval.policy_decision === "deny" ? " is-deny" : ""}`;
      const badge = approvalDecisionMeta(approval.decision);
      const needsOverride = approval.policy_decision === "deny";
      item.innerHTML = `
        <div class="app-chat-approval-item-head">
          <div>
            <div class="app-chat-approval-item-title">${escapeHTML(approval.tool_name)}</div>
            <div class="app-chat-approval-item-scope">${escapeHTML(approval.scope || "未提供 scope")}</div>
          </div>
          <span class="app-approval-inline-badge ${badge.className}">${badge.label}</span>
        </div>
        <div class="app-chat-approval-item-reason">${escapeHTML(approval.reason)}</div>
        ${approval.safer_alternative ? `<div class="app-chat-approval-item-alt">safer: ${escapeHTML(approval.safer_alternative)}</div>` : ""}
      `;
      if (!approval.decision && !batch.executing) {
        const actions = document.createElement("div");
        actions.className = "app-chat-approval-actions";
        const positive = document.createElement("button");
        positive.type = "button";
        positive.dataset.id = approval.id;
        positive.dataset.decision = needsOverride ? "force_approve" : "approve";
        positive.textContent = needsOverride ? "强制批准" : "批准";
        const negative = document.createElement("button");
        negative.type = "button";
        negative.dataset.id = approval.id;
        negative.dataset.decision = "reject";
        negative.textContent = "拒绝";
        actions.appendChild(positive);
        actions.appendChild(negative);
        actions.querySelectorAll("button").forEach((button) => {
          button.addEventListener("click", async (event) => {
            const current = event.currentTarget;
            actions.querySelectorAll("button").forEach((itemButton) => {
              itemButton.disabled = true;
            });
            try {
              await request(`/api/approvals/${current.dataset.id}/resolve`, {
                method: "POST",
                body: JSON.stringify({ decision: current.dataset.decision, actor: "web" }),
              });
              scheduleRefresh({ detail: true });
            } catch (error) {
              actions.querySelectorAll("button").forEach((itemButton) => {
                itemButton.disabled = false;
              });
              current.textContent = `失败：${error.message}`;
            }
          });
        });
        item.appendChild(actions);
      }
      list.appendChild(item);
    });
    node.appendChild(list);
    box.appendChild(node);
  });
}

function renderChatHealth() {
  const status = document.getElementById("chat-health-status");
  const model = document.getElementById("chat-health-model");
  const summary = document.getElementById("chat-health-summary");
  const turns = document.getElementById("chat-session-turns");
  const pending = document.getElementById("chat-session-pending");
  const tools = document.getElementById("chat-session-tools");
  const prompt = document.getElementById("chat-session-prompt");
  if (!status || !model || !summary || !turns || !pending || !tools || !prompt) return;

  status.textContent = state.health?.status === "ok" ? "健康运行" : "状态异常";
  status.className = `app-status-inline${state.health?.status === "ok" ? " is-live" : ""}`;
  model.textContent = [state.health?.preset_name || "未配置预设", state.health?.model || "unknown-model"].join(" · ");
  if (!state.currentSessionDetail) {
    summary.textContent = `全局 Sessions ${state.health?.total_sessions ?? state.sessions.length} · Runs ${state.health?.total_runs ?? state.runs.length} · Hosts ${state.health?.total_hosts ?? state.hosts.length}`;
    turns.textContent = "0";
    pending.textContent = String(state.health?.pending_approvals ?? state.approvals.filter((item) => !item.decision).length);
    tools.textContent = "0";
    prompt.textContent = "--";
    return;
  }

  const latestTurn = latestTurnForSessionDetail(state.currentSessionDetail);
  const latestPromptTokens = latestTurn?.prompt_stats?.estimated_prompt_tokens || 0;
  summary.textContent = `Host ${state.currentSessionDetail.host.display_name || state.currentSessionDetail.host.id} · 全局待审批 ${state.health?.pending_approvals ?? state.approvals.filter((item) => !item.decision).length} · ${state.health?.policy_summary || "暂无策略摘要。"}`;
  turns.textContent = String(state.currentSessionDetail.turns.length);
  pending.textContent = String(state.currentSessionDetail.pending_approvals?.length || 0);
  tools.textContent = String(countSessionTools(state.currentSessionDetail));
  prompt.textContent = latestPromptTokens ? `${formatTokenCount(latestPromptTokens)}t` : "--";
}

function renderChatTrace() {
  const pre = document.getElementById("chat-live-trace");
  if (!pre) return;
  if (!state.currentSessionDetail?.turns?.length) {
    pre.textContent = "选择会话后显示实时事件回放。";
    return;
  }

  const lines = [];
  for (const item of state.currentSessionDetail.turns) {
    const events = dedupeEvents([...(item.events || []), ...(state.liveEvents.get(item.run.id) || [])]);
    for (const event of events) {
      lines.push(`[${formatTime(event.timestamp)}] ${eventTitle(event)}${event.message ? ` · ${event.message}` : ""}`);
    }
  }
  pre.textContent = lines.slice(-40).join("\n") || "当前会话暂无事件。";
}

function renderChat() {
  renderSessionList();
  renderChatSummary();
  renderChatComposer();
  renderChatApprovalBar();
  renderChatApprovals();
  renderChatHealth();
  renderChatTrace();

  const chatHistory = document.getElementById("chat-history");
  if (!chatHistory) return;
  chatHistory.innerHTML = "";

  if (!state.currentSessionDetail?.turns?.length) {
    if (state.currentSessionId || state.sessionDetailLoadingId) {
      chatHistory.innerHTML = `
        <div class="app-empty-chat">
          <div class="app-empty-chat-icon">
            <span class="material-symbols-outlined">hourglass_top</span>
          </div>
          <h3>正在加载会话历史</h3>
          <p>只拉取最近 40 个 turn 和压缩后的事件输出，避免历史文件拖慢切换。</p>
        </div>`;
      return;
    }
    chatHistory.innerHTML = `
      <div class="app-empty-chat">
        <div class="app-empty-chat-icon">
          <span class="material-symbols-outlined">forum</span>
        </div>
        <h3>开始一段新的真实运维会话</h3>
        <p>输入一条明确请求，控制面会复用真实 host / session / turn / run 链路推进执行。</p>
      </div>`;
    return;
  }

  for (const item of state.currentSessionDetail.turns) {
    const replay = buildReplay(item);
    chatHistory.appendChild(renderUserRow(item.turn, state.currentSessionDetail.host));
    if (replay.toolEvents.length > 0 || replay.consoleOutput || (item.approvals || []).length > 0) {
      chatHistory.appendChild(renderAssistantTrace(item.turn, item.run, replay, item.approvals || []));
    }
    chatHistory.appendChild(renderAssistantMessage(item.turn, item.run, replay, item.approvals || []));
  }

  queueMicrotask(scrollChatToBottom);
}

function renderUserRow(turn, host) {
  const node = document.createElement("div");
  node.className = "app-chat-entry app-chat-entry--user";
  node.innerHTML = `
    <div class="app-chat-avatar app-chat-avatar--user">
      <span class="material-symbols-outlined">person</span>
    </div>
    <div class="app-chat-entry-main">
      <div class="app-chat-entry-meta">
        <span>用户</span>
        <span>${formatTime(turn.created_at)}</span>
        <span>${escapeHTML(host?.display_name || host?.id || "未绑定主机")}</span>
      </div>
      <div class="app-chat-user-text">${escapeHTML(turn.user_input || "")}</div>
    </div>
  `;
  return node;
}

function renderAssistantTrace(turn, run, replay, approvals) {
  const node = document.createElement("div");
  node.className = "app-chat-entry app-chat-entry--assistant";
  node.innerHTML = `
    <div class="app-chat-avatar app-chat-avatar--assistant">
      <span class="material-symbols-outlined" style="font-variation-settings: 'FILL' 1;">robot_2</span>
    </div>
    <div class="app-chat-entry-main">
      <div class="app-chat-trace-card"></div>
    </div>
  `;
  const box = node.querySelector(".app-chat-trace-card");
  const traceMeta = document.createElement("div");
  traceMeta.className = "app-chat-trace-meta";
  traceMeta.innerHTML = `
    <span>Trace</span>
    <span>${replay.toolEvents.length} events</span>
    <span>${replay.toolResults.length} results</span>
    <span>${formatTime(replay.lastEventAt || run.updated_at || turn.updated_at)}</span>
  `;
  box.appendChild(traceMeta);

  if (approvals.length > 0) {
    const wrap = document.createElement("div");
    wrap.className = "app-chat-approval-list";
    approvals.forEach((approval) => {
      const badge = approvalDecisionMeta(approval.decision);
      const item = document.createElement("div");
      item.className = `app-approval-inline${approval.decision ? "" : " is-pending"}`;
      item.innerHTML = `
        <div class="app-approval-inline-head">
          <span class="app-approval-inline-title">${escapeHTML(approval.tool_name)}</span>
          <span class="app-approval-inline-badge ${badge.className}">${badge.label}</span>
        </div>
        <div class="app-approval-inline-text">${escapeHTML(approval.reason)}</div>
        <div class="app-approval-inline-scope">${escapeHTML(approval.scope || "未提供 scope")}</div>
        ${approval.policy_decision === "deny" ? `<div class="app-approval-inline-text">policy deny${approval.decision === "force_approve" ? " · override executed" : ""}</div>` : ""}
      `;
      wrap.appendChild(item);
    });
    box.appendChild(wrap);
  }

  if (replay.toolEvents.length > 0) {
    const list = document.createElement("div");
    list.className = "app-chat-trace-list";
    replay.toolEvents.forEach((event) => {
      const block = document.createElement("div");
      block.className = "app-chat-trace-item";
      block.innerHTML = `
        <div class="app-chat-trace-item-head">
          <div class="app-chat-trace-item-label">
            <span class="material-symbols-outlined">${eventIcon(event.type)}</span>
            <span>${escapeHTML(eventTitle(event))}</span>
          </div>
          <span class="app-chat-trace-item-time">${formatTime(event.timestamp)}</span>
        </div>
        <div class="app-chat-trace-item-text">${escapeHTML(event.message || "")}</div>
        ${event.payload ? `<details class="app-chat-trace-payload"><summary>payload</summary><pre>${escapeHTML(JSON.stringify(event.payload, null, 2))}</pre></details>` : ""}
      `;
      list.appendChild(block);
    });
    box.appendChild(list);
  }

  if (replay.consoleOutput) {
    const consoleNode = document.createElement("details");
    const lineCount = replay.consoleOutput.split("\n").filter(Boolean).length || 1;
    consoleNode.className = "app-chat-console";
    if (lineCount <= 10 && replay.consoleOutput.length <= 500) {
      consoleNode.open = true;
    }
    consoleNode.innerHTML = `
      <summary>工具输出 · ${lineCount} lines</summary>
      <pre>${escapeHTML(replay.consoleOutput)}</pre>
    `;
    box.appendChild(consoleNode);
  }

  if (box.childElementCount === 1 && approvals.length === 0 && replay.toolEvents.length === 0 && !replay.consoleOutput) {
    return document.createDocumentFragment();
  }
  return node;
}

function renderAssistantMessage(turn, run, replay, approvals) {
  const node = document.createElement("div");
  const statusMeta = runStatusMeta(run.status);
  const promptStats = turn.prompt_stats || {};
  const chips = [
    `tools ${replay.toolResults.length}`,
    `approvals ${approvals.length}`,
    `prompt ${formatTokenCount(promptStats.estimated_prompt_tokens)}t`,
  ];
  if (promptStats.compression_triggered) {
    chips.push("history compressed");
  }
  node.className = "app-chat-entry app-chat-entry--assistant";
  node.innerHTML = `
    <div class="app-chat-avatar app-chat-avatar--assistant">
      <span class="material-symbols-outlined" style="font-variation-settings: 'FILL' 1;">robot_2</span>
    </div>
    <div class="app-chat-entry-main">
      <div class="app-chat-message-card">
        <div class="app-chat-message-head">
          <div class="app-chat-message-title">
            <span class="material-symbols-outlined ${statusMeta.color}">${statusMeta.icon}</span>
            <span>${escapeHTML(run.id)}</span>
          </div>
          <span class="app-chat-message-status">${escapeHTML(run.status)}</span>
        </div>
        <div class="app-chat-message-chips">
          ${chips.map((chip) => `<span>${escapeHTML(chip)}</span>`).join("")}
          <span>${formatTime(replay.lastEventAt || run.updated_at)}</span>
        </div>
        <div class="markdown-body app-chat-message-body">${renderMarkdown(replay.assistantContent)}</div>
      </div>
    </div>
  `;
  return node;
}

function renderAssets() {
  const totalHosts = document.getElementById("assets-total-hosts");
  const activeRuns = document.getElementById("assets-active-runs");
  const pendingApprovals = document.getElementById("assets-pending-approvals");
  const list = document.getElementById("assets-host-list");
  const form = document.getElementById("assets-host-form");
  const openFormButton = document.getElementById("assets-open-form");
  const formTitle = document.getElementById("assets-form-title");
  const formSubtitle = document.getElementById("assets-form-subtitle");
  const submitButton = document.getElementById("assets-host-submit");
  const resetButton = document.getElementById("assets-host-reset");
  const message = document.getElementById("assets-host-message");
  if (!list || !form || !message || !formTitle || !formSubtitle || !submitButton || !resetButton) return;

  const setFormMode = () => {
    const editingHost = state.assetEditingHostId ? findHostById(state.assetEditingHostId) : null;
    const idInput = form.querySelector("[name=id]");
    formTitle.textContent = editingHost ? `编辑主机 ${editingHost.display_name || editingHost.id}` : "快速注册";
    formSubtitle.textContent = editingHost ? "修改已接入主机的真实连接参数。" : "录入真实主机参数后立即接入控制面。";
    submitButton.textContent = editingHost ? "保存修改" : "确认注册";
    resetButton.textContent = editingHost ? "取消编辑" : "清空表单";
    if (idInput) {
      idInput.readOnly = Boolean(editingHost);
      idInput.classList.toggle("is-readonly", Boolean(editingHost));
    }
  };

  const resetHostForm = ({ preserveMessage = false } = {}) => {
    state.assetEditingHostId = "";
    form.reset();
    const portInput = form.querySelector("[name=port]");
    if (portInput) portInput.value = "22";
    const modeSelect = form.querySelector("[name=mode]");
    if (modeSelect) modeSelect.value = "ssh";
    setFormMode();
    if (!preserveMessage) {
      message.textContent = "保存后会立刻出现在真实资产列表中。";
    }
  };

  const beginHostEdit = (hostId) => {
    const host = findHostById(hostId);
    if (!host) return;
    state.assetEditingHostId = host.id;
    form.querySelector("[name=id]").value = host.id || "";
    form.querySelector("[name=display_name]").value = host.display_name || "";
    form.querySelector("[name=address]").value = host.address || "";
    form.querySelector("[name=mode]").value = host.mode || "ssh";
    form.querySelector("[name=port]").value = host.port || (host.mode === "local" ? "" : "22");
    form.querySelector("[name=user]").value = host.user || "";
    form.querySelector("[name=password_env]").value = host.password_env || "";
    setFormMode();
    const passwordMode = host.mode === "ssh" ? sshPasswordMode(host.password_env) : "";
    if (passwordMode === "literal") {
      message.textContent = "当前主机使用明文密码。可以保存，但建议改成环境变量名。";
    } else {
      message.textContent = `正在编辑 ${host.display_name || host.id}。`;
    }
    form.scrollIntoView({ behavior: "smooth", block: "start" });
    form.querySelector("[name=display_name]")?.focus();
  };

  if (totalHosts) totalHosts.textContent = String(state.hosts.length);
  if (activeRuns) {
    activeRuns.textContent = String(state.runs.filter((run) => ["created", "running_agent", "tool_running", "waiting_approval"].includes(run.status)).length);
  }
  if (pendingApprovals) pendingApprovals.textContent = String(state.approvals.filter((item) => !item.decision).length);

  list.innerHTML = "";
  if (state.hosts.length === 0) {
    list.innerHTML = `<div class="app-empty-state">当前没有主机，先在右侧登记一台真实主机。</div>`;
  }

  for (const host of state.hosts) {
    const modeLabel = host.mode === "local" ? "本地" : "SSH";
    const passwordMode = host.mode === "ssh" ? sshPasswordMode(host.password_env) : "";
    const passwordHint = passwordMode === "env"
      ? "密码来自环境变量"
      : passwordMode === "literal"
        ? "使用明文密码"
        : "未配置凭据";
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
      ${host.mode === "ssh" ? `
      <div class="mt-4 pt-4 border-t border-[#E2E2DB]/50">
        <p class="font-label text-[11px] text-secondary uppercase tracking-wider mb-1">凭据模式</p>
        <div class="assets-host-credential-row">
          <p class="font-body-sm text-[13px] text-on-surface">${escapeHTML(passwordHint)}</p>
          ${passwordMode === "literal" ? '<span class="assets-host-warning-badge">明文告警</span>' : ""}
        </div>
      </div>` : ""}
      <div class="mt-4 pt-4 border-t border-[#E2E2DB]/50">
        <p class="font-label text-[11px] text-secondary uppercase tracking-wider mb-1">最近活动</p>
        <p class="font-body-sm text-[13px] text-on-surface">${lastRunText}</p>
      </div>
      <div class="assets-host-actions">
        <button class="assets-host-action" data-edit-host="${escapeHTML(host.id)}" type="button">编辑</button>
      </div>
    `;
    node.querySelector("[data-edit-host]")?.addEventListener("click", () => beginHostEdit(host.id));
    list.appendChild(node);
  }

  setFormMode();

  if (!form.dataset.bound) {
    form.dataset.bound = "true";
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      try {
        message.textContent = "正在保存主机...";
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
        await loadCore();
        const savedHost = findHostById(String(data.get("id") || "").trim());
        const savedPasswordMode = savedHost?.mode === "ssh" ? sshPasswordMode(savedHost.password_env) : "";
        resetHostForm({ preserveMessage: true });
        if (savedPasswordMode === "literal") {
          message.textContent = "主机已保存。当前 SSH 使用明文密码，系统已保留但继续显示安全警告。";
        } else {
          message.textContent = "主机已保存并接入控制面。";
        }
        renderAssets();
      } catch (error) {
        message.textContent = `保存失败：${error.message}`;
      }
    });

    form.addEventListener("reset", () => {
      window.setTimeout(() => resetHostForm(), 0);
    });
  }

  if (openFormButton && !openFormButton.dataset.bound) {
    openFormButton.dataset.bound = "true";
    openFormButton.addEventListener("click", () => {
      resetHostForm({ preserveMessage: true });
      form.scrollIntoView({ behavior: "smooth", block: "start" });
      form.querySelector("[name=id]")?.focus();
    });
  }
}

function renderHistory() {
  const list = document.getElementById("history-session-list");
  const total = document.getElementById("history-session-total");
  const search = document.getElementById("history-search");
  const onlyPending = document.getElementById("history-only-pending");
  const onlyForce = document.getElementById("history-only-force");
  if (!list || !total || !search || !onlyPending || !onlyForce) return;

  const query = String(search.value || "").trim().toLowerCase();
  const items = state.sessions.filter((session) => {
    const relatedRuns = state.runs.filter((run) => run.session_id === session.id);
    const hasPending = relatedRuns.some((run) => run.pending_approvals > 0);
    const hasForce = relatedRuns.some((run) => run.has_force_approve);
    if (onlyPending.checked && !hasPending) return false;
    if (onlyForce.checked && !hasForce) return false;
    if (!query) return true;
    return [session.title, session.preview, session.host_display_name, session.id].some((value) => String(value || "").toLowerCase().includes(query));
  });

  if (!search.dataset.bound) {
    search.dataset.bound = "true";
    search.addEventListener("input", renderHistory);
    onlyPending.addEventListener("change", renderHistory);
    onlyForce.addEventListener("change", renderHistory);
  }

  total.textContent = `${items.length} 条`;
  list.innerHTML = "";
  if (items.length === 0) {
    list.innerHTML = `<div class="app-empty-state automation-empty-state">没有匹配的历史记录。</div>`;
    return;
  }

  items.forEach((session) => {
    const relatedRuns = state.runs.filter((run) => run.session_id === session.id);
    const node = document.createElement("div");
    node.className = "px-6 py-5 hover:bg-surface-container-low transition-colors cursor-pointer";
    node.innerHTML = `
      <div class="flex items-center justify-between gap-4">
        <div class="flex-1 min-w-0">
          <div class="flex flex-wrap items-center gap-2 mb-2">
            <span class="font-h2 text-on-surface text-base font-semibold">${escapeHTML(sessionTitle(session))}</span>
            ${session.pending_approvals > 0 ? `<span class="font-label text-[#C96442] bg-[#F5E7E0] px-2 py-0.5 rounded text-[11px]">待审批 ${session.pending_approvals}</span>` : ""}
            ${relatedRuns.some((run) => run.has_force_approve) ? `<span class="font-label text-[#9A4021] bg-[#FFF1EC] px-2 py-0.5 rounded text-[11px]">Force approve</span>` : ""}
          </div>
          <div class="font-body-sm text-[13px] text-on-surface-variant">${escapeHTML(truncateText(sessionPreviewText(session), 220))}</div>
          <div class="mt-3 flex flex-wrap items-center gap-4 text-secondary">
            <span class="font-body-sm text-[13px]">${escapeHTML(session.host_display_name || session.host_id || "未绑定主机")}</span>
            <span class="font-body-sm text-[13px]">runs ${relatedRuns.length}</span>
            <span class="font-body-sm text-[13px]">${formatTime(session.last_event_at || session.updated_at)}</span>
          </div>
        </div>
      </div>
    `;
    node.addEventListener("click", () => {
      disconnectGlobalEvents();
      const url = new URL(window.location.origin + "/");
      url.searchParams.set("session", session.id);
      window.location.href = url.toString();
    });
    list.appendChild(node);
  });
}

function renderAutomation() {
  const ruleList = document.getElementById("automation-rule-list");
  const ruleTotal = document.getElementById("automation-rule-total");
  const enabledCount = document.getElementById("automation-enabled-count");
  const triggeredCount = document.getElementById("automation-triggered-count");
  const alertCount = document.getElementById("automation-alert-count");
  const form = document.getElementById("automation-form");
  const message = document.getElementById("automation-form-message");
  const openButton = document.getElementById("automation-new-rule");
  if (!ruleList || !ruleTotal || !enabledCount || !triggeredCount || !alertCount || !form || !message || !openButton) return;

  const rules = state.automations || [];
  enabledCount.textContent = String(rules.filter((item) => item.enabled).length);
  triggeredCount.textContent = String(rules.filter((item) => item.last_triggered_at).length);
  alertCount.textContent = String(rules.filter((item) => item.last_status && !["healthy", "cooldown", "created"].includes(item.last_status)).length);
  ruleTotal.textContent = `${rules.length} 条`;

  ruleList.innerHTML = "";
  if (rules.length === 0) {
    ruleList.innerHTML = `<div class="app-empty-state automation-empty-state">当前还没有自动化规则。</div>`;
  }

  rules.forEach((rule) => {
    const node = document.createElement("div");
    node.className = "px-6 py-5 hover:bg-surface-container-low transition-colors";
    node.innerHTML = `
      <div class="flex flex-col gap-4">
        <div class="flex-1 min-w-0">
          <div class="flex flex-wrap items-center gap-2 mb-2">
            <span class="font-h2 text-on-surface text-base font-semibold">${escapeHTML(rule.name)}</span>
            <span class="font-label ${rule.enabled ? "text-[#00796B] bg-[#E3F1ED]" : "text-secondary bg-[#E6E4D9]"} px-2 py-0.5 rounded text-[11px]">${rule.enabled ? "启用" : "停用"}</span>
            <span class="font-label ${automationStatusClass(rule.last_status)} px-2 py-0.5 rounded text-[11px]">${escapeHTML(automationStatusLabel(rule.last_status))}</span>
            ${rule.bypass_approvals ? `<span class="font-label text-[#9A4021] bg-[#FFF1EC] px-2 py-0.5 rounded text-[11px]">Bypass</span>` : ""}
            ${rule.allow_force_approve ? `<span class="font-label text-[#9A4021] bg-[#FFF1EC] px-2 py-0.5 rounded text-[11px]">Allow force approve</span>` : ""}
          </div>
          <div class="font-body-sm text-[13px] text-on-surface-variant">${escapeHTML(rule.host_display_name || rule.host_id)} · ${escapeHTML(rule.metric)} ${escapeHTML(rule.operator)} ${escapeHTML(rule.threshold)}</div>
          <div class="mt-3 flex flex-wrap items-center gap-4 text-secondary">
            <span class="font-body-sm text-[13px]">窗口 ${rule.window_minutes} 分钟</span>
            <span class="font-body-sm text-[13px]">冷却 ${rule.cooldown_minutes} 分钟</span>
            <span class="font-body-sm text-[13px]">最近观测 ${Number(rule.last_observed_value || 0).toFixed(2)}</span>
            <span class="font-body-sm text-[13px]">最近触发 ${formatTime(rule.last_triggered_at) || "无"}</span>
          </div>
        </div>
        <div class="automation-rule-actions flex flex-wrap gap-2" data-rule-id="${escapeHTML(rule.id)}">
          <button class="app-button-secondary" type="button" data-action="edit"><span class="material-symbols-outlined">edit</span><span>编辑</span></button>
          <button class="app-button-secondary" type="button" data-action="sample"><span class="material-symbols-outlined">monitoring</span><span>采样验证</span></button>
          <button class="app-primary-button app-primary-button--inline" type="button" data-action="test"><span class="material-symbols-outlined">play_arrow</span><span>测试运行</span></button>
          <button class="app-button-secondary" type="button" data-action="force-test"><span class="material-symbols-outlined">bolt</span><span>强制测试</span></button>
          ${rule.last_run_id ? `<button class="app-button-secondary" type="button" data-action="open-run"><span class="material-symbols-outlined">open_in_new</span><span>查看 run</span></button>` : ""}
          <button class="app-button-secondary app-button-secondary--danger" type="button" data-action="delete"><span class="material-symbols-outlined">delete</span><span>删除</span></button>
        </div>
      </div>
    `;
    node.querySelector(".automation-rule-actions")?.addEventListener("click", (event) => {
      event.stopPropagation();
      const button = event.target.closest("button[data-action]");
      if (!button) return;
      handleAutomationAction(button.dataset.action, rule, message, form);
    });
    node.addEventListener("click", () => {
      populateAutomationForm(rule);
      message.textContent = `正在编辑：${rule.name}`;
      form.scrollIntoView({ behavior: "smooth", block: "start" });
    });
    ruleList.appendChild(node);
  });

  renderAutomationForm();

  if (!openButton.dataset.bound) {
    openButton.dataset.bound = "true";
    openButton.addEventListener("click", () => {
      populateAutomationForm(null);
      form.scrollIntoView({ behavior: "smooth", block: "start" });
    });
  }
}

function populateAutomationForm(rule) {
  const form = document.getElementById("automation-form");
  if (!form) return;
  form.dataset.editingId = rule?.id || "";
  document.getElementById("automation-rule-id").value = rule?.id || "";
  document.getElementById("automation-name").value = rule?.name || "";
  document.getElementById("automation-host").value = rule?.host_id || state.hosts[0]?.id || "";
  document.getElementById("automation-metric").value = rule?.metric || "cpu_usage";
  document.getElementById("automation-operator").value = rule?.operator || ">";
  document.getElementById("automation-threshold").value = rule?.threshold ?? 80;
  document.getElementById("automation-window").value = rule?.window_minutes ?? 5;
  document.getElementById("automation-cooldown").value = rule?.cooldown_minutes ?? 15;
  document.getElementById("automation-prompt").value = rule?.prompt_template || "";
  document.getElementById("automation-enabled").checked = rule?.enabled ?? true;
  document.getElementById("automation-bypass").checked = Boolean(rule?.bypass_approvals);
  document.getElementById("automation-force-approve").checked = Boolean(rule?.allow_force_approve);
  document.getElementById("automation-form-title").textContent = rule ? "编辑规则" : "新增规则";
  document.getElementById("automation-form-subtitle").textContent = rule ? "更新后将继续沿用当前规则 ID 与最近触发上下文。" : "配置主机、指标阈值、冷却时间和触发提示词。";
}

async function handleAutomationAction(action, rule, message, form) {
  try {
    if (action === "edit") {
      populateAutomationForm(rule);
      message.textContent = `正在编辑：${rule.name}`;
      form.scrollIntoView({ behavior: "smooth", block: "start" });
      return;
    }
    if (action === "open-run") {
      if (rule.session_id) {
        disconnectGlobalEvents();
        window.location.href = `/?session=${encodeURIComponent(rule.session_id)}`;
      } else if (rule.last_run_id) {
        message.textContent = `最近 run：${rule.last_run_id}`;
      }
      return;
    }
    if (action === "delete") {
      if (!window.confirm(`确认删除自动化规则「${rule.name}」？历史 run 不会删除。`)) return;
      message.textContent = "正在删除规则...";
      await request(`/api/automations/${encodeURIComponent(rule.id)}`, { method: "DELETE" });
      await loadCore();
      populateAutomationForm(null);
      message.textContent = "规则已删除。";
      renderAutomation();
      return;
    }
    if (action === "sample") {
      message.textContent = "正在进行真实指标采样...";
      const sample = await request(`/api/automations/${encodeURIComponent(rule.id)}/sample`, { method: "POST" });
      await loadCore();
      message.textContent = `采样完成：${sample.metric} = ${Number(sample.value).toFixed(2)} (${formatTime(sample.captured_at)})`;
      renderAutomation();
      return;
    }
    if (action === "test" || action === "force-test") {
      const force = action === "force-test";
      message.textContent = force ? "正在强制创建测试 run..." : "正在按阈值创建测试 run...";
      const result = await request(`/api/automations/${encodeURIComponent(rule.id)}/test`, {
        method: "POST",
        body: JSON.stringify({ force }),
      });
      await loadCore();
      const observed = Number(result.sample?.value || 0).toFixed(2);
      const runText = result.run_created && result.run?.id ? `已创建 run ${result.run.id}` : "未创建 run";
      message.textContent = `${result.message || "测试完成"} 当前值 ${observed}，${runText}。`;
      renderAutomation();
    }
  } catch (error) {
    message.textContent = `操作失败：${error.message}`;
  }
}

function renderAutomationForm() {
  const form = document.getElementById("automation-form");
  const hostSelect = document.getElementById("automation-host");
  const message = document.getElementById("automation-form-message");
  if (!form || !hostSelect || !message) return;

  hostSelect.innerHTML = state.hosts.map((host) => `<option value="${escapeHTML(host.id)}">${escapeHTML(host.display_name || host.id)}</option>`).join("");
  if (!form.dataset.initialized) {
    populateAutomationForm(null);
  }

  if (!form.dataset.bound) {
    form.dataset.bound = "true";
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      try {
        message.textContent = "正在保存自动化规则...";
        const payload = {
          id: document.getElementById("automation-rule-id").value.trim(),
          name: document.getElementById("automation-name").value.trim(),
          host_id: document.getElementById("automation-host").value,
          trigger_type: "threshold",
          metric: document.getElementById("automation-metric").value,
          operator: document.getElementById("automation-operator").value,
          threshold: Number(document.getElementById("automation-threshold").value || 0),
          window_minutes: Number(document.getElementById("automation-window").value || 0),
          cooldown_minutes: Number(document.getElementById("automation-cooldown").value || 0),
          prompt_template: document.getElementById("automation-prompt").value.trim(),
          session_strategy: "reuse",
          enabled: document.getElementById("automation-enabled").checked,
          bypass_approvals: document.getElementById("automation-bypass").checked,
          allow_force_approve: document.getElementById("automation-force-approve").checked,
        };
        const endpoint = payload.id ? `/api/automations/${encodeURIComponent(payload.id)}` : "/api/automations";
        await request(endpoint, {
          method: payload.id ? "PUT" : "POST",
          body: JSON.stringify(payload),
        });
        await loadCore();
        populateAutomationForm(null);
        form.dataset.initialized = "true";
        message.textContent = "自动化规则已保存。";
        renderAutomation();
      } catch (error) {
        message.textContent = `保存失败：${error.message}`;
      }
    });
    form.addEventListener("reset", () => {
      window.setTimeout(() => {
        populateAutomationForm(null);
        message.textContent = "表单已重置。";
      }, 0);
    });
  }

  form.dataset.initialized = "true";
}

function renderKnowledgeSettings(list, form, newButton, message) {
  const items = state.knowledge || [];
  list.innerHTML = "";
  if (items.length === 0) {
    list.innerHTML = `<div class="app-empty-state">当前没有长期知识。真实 run 完成后会生成 pending 候选，也可以手动新增 SOP。</div>`;
  }
  items.forEach((item) => {
    const node = document.createElement("button");
    node.type = "button";
    node.className = "w-full rounded-[18px] border border-surface-variant/60 bg-surface px-4 py-4 text-left hover:bg-surface-container-low";
    node.innerHTML = `
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="min-w-0">
          <div class="font-label text-label text-on-surface">${escapeHTML(item.title || item.id)}</div>
          <div class="mt-1 text-xs text-secondary break-all">${escapeHTML(item.id)} · ${escapeHTML(item.scope || "global")}</div>
        </div>
        <div class="flex flex-wrap gap-2">
          <span class="rounded-full bg-[#E3F1ED] px-2 py-0.5 text-[11px] text-[#00796B]">${escapeHTML(item.kind || "memory")}</span>
          <span class="rounded-full ${item.status === "active" ? "bg-[#E3F1ED] text-[#00796B]" : "bg-[#FFF5D6] text-[#8A5A00]"} px-2 py-0.5 text-[11px]">${escapeHTML(item.status || "pending")}</span>
        </div>
      </div>
      <div class="mt-3 text-sm text-on-surface-variant">${escapeHTML(truncateText(item.body, 220))}</div>
      <div class="mt-2 text-[11px] text-secondary">source ${escapeHTML(item.source_run_id || item.source_sop_id || "manual")} · embedding ${escapeHTML(item.embedding_status || "not_requested")}</div>
    `;
    node.addEventListener("click", () => populateKnowledgeForm(item));
    list.appendChild(node);
  });

  if (!newButton.dataset.bound) {
    newButton.dataset.bound = "true";
    newButton.addEventListener("click", () => {
      populateKnowledgeForm(null);
      message.textContent = "正在新增知识。保存为 active 后才会进入后续检索。";
    });
  }

  if (!form.dataset.bound) {
    form.dataset.bound = "true";
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      try {
        message.textContent = "正在保存知识...";
        const id = document.getElementById("settings-knowledge-id").value.trim();
        const saved = await request("/api/knowledge", {
          method: "POST",
          body: JSON.stringify({
            id,
            kind: document.getElementById("settings-knowledge-kind").value,
            status: document.getElementById("settings-knowledge-status").value,
            scope: document.getElementById("settings-knowledge-scope").value.trim() || "global",
            title: document.getElementById("settings-knowledge-title").value.trim(),
            body: document.getElementById("settings-knowledge-body").value.trim(),
            tags: splitTags(document.getElementById("settings-knowledge-tags").value),
          }),
        });
        const index = state.knowledge.findIndex((item) => item.id === saved.id);
        if (index >= 0) state.knowledge[index] = saved;
        else state.knowledge.unshift(saved);
        populateKnowledgeForm(saved);
        message.textContent = `已保存：${saved.id}`;
        renderSettings();
      } catch (error) {
        message.textContent = `保存失败：${error.message}`;
      }
    });
  }

  if (!form.dataset.initialized) {
    populateKnowledgeForm(items[0] || null);
    form.dataset.initialized = "true";
  }
}

function populateKnowledgeForm(item) {
  document.getElementById("settings-knowledge-id").value = item?.id || "";
  document.getElementById("settings-knowledge-kind").value = item?.kind || "sop";
  document.getElementById("settings-knowledge-status").value = item?.status || "pending";
  document.getElementById("settings-knowledge-scope").value = item?.scope || "global";
  document.getElementById("settings-knowledge-title").value = item?.title || "";
  document.getElementById("settings-knowledge-body").value = item?.body || "";
  document.getElementById("settings-knowledge-tags").value = (item?.tags || []).join(", ");
}

function renderPolicySettings(list, form, message) {
  const rules = state.policyConfig?.rules || [];
  list.innerHTML = "";
  if (rules.length === 0) {
    list.innerHTML = `<div class="app-empty-state">当前没有安全规则配置。</div>`;
  }
  rules.forEach((rule) => {
    const node = document.createElement("div");
    node.className = "rounded-[18px] border border-surface-variant/60 bg-surface p-4";
    node.dataset.ruleId = rule.id;
    node.innerHTML = `
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <div class="font-label text-label text-on-surface">${escapeHTML(rule.id)}</div>
          <div class="mt-1 text-xs text-secondary">${escapeHTML(rule.description || "")}</div>
        </div>
        <div class="grid grid-cols-3 gap-2">
          <select data-field="decision" class="min-h-[36px] rounded-[12px] border border-surface-variant bg-surface px-2 text-xs">
            <option value="allow">allow</option>
            <option value="ask">ask</option>
            <option value="deny">deny</option>
          </select>
          <select data-field="severity" class="min-h-[36px] rounded-[12px] border border-surface-variant bg-surface px-2 text-xs">
            <option value="low">low</option>
            <option value="medium">medium</option>
            <option value="high">high</option>
            <option value="critical">critical</option>
          </select>
          <label class="flex min-h-[36px] items-center gap-2 rounded-[12px] border border-surface-variant px-2 text-xs">
            <input data-field="override_allowed" type="checkbox">
            override
          </label>
        </div>
      </div>
      <div class="mt-3 grid grid-cols-1 md:grid-cols-2 gap-3">
        <label class="block">
          <span class="font-label text-[11px] text-secondary block mb-1">reason</span>
          <textarea data-field="reason" class="w-full min-h-[74px] rounded-[12px] border border-surface-variant bg-surface px-3 py-2 text-sm"></textarea>
        </label>
        <label class="block">
          <span class="font-label text-[11px] text-secondary block mb-1">safer alternative</span>
          <textarea data-field="safer_alternative" class="w-full min-h-[74px] rounded-[12px] border border-surface-variant bg-surface px-3 py-2 text-sm"></textarea>
        </label>
      </div>
    `;
    node.querySelector('[data-field="decision"]').value = rule.decision || "ask";
    node.querySelector('[data-field="severity"]').value = rule.severity || "medium";
    node.querySelector('[data-field="override_allowed"]').checked = Boolean(rule.override_allowed);
    node.querySelector('[data-field="reason"]').value = rule.reason || "";
    node.querySelector('[data-field="safer_alternative"]').value = rule.safer_alternative || "";
    if (isProtectedDenyRuleId(rule.id)) {
      node.querySelector('[data-field="decision"]').disabled = true;
      node.querySelector('[data-field="override_allowed"]').disabled = true;
    }
    list.appendChild(node);
  });

  if (!form.dataset.bound) {
    form.dataset.bound = "true";
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      try {
        message.textContent = "正在保存安全规则...";
        const nextRules = [...list.querySelectorAll("[data-rule-id]")].map((node) => {
          const current = rules.find((rule) => rule.id === node.dataset.ruleId) || {};
          return {
            ...current,
            decision: node.querySelector('[data-field="decision"]').value,
            severity: node.querySelector('[data-field="severity"]').value,
            override_allowed: node.querySelector('[data-field="override_allowed"]').checked,
            reason: node.querySelector('[data-field="reason"]').value.trim(),
            safer_alternative: node.querySelector('[data-field="safer_alternative"]').value.trim(),
          };
        });
        state.policyConfig = await request("/api/settings/policy", {
          method: "PUT",
          body: JSON.stringify({ ...(state.policyConfig || { schema_version: "1.0" }), rules: nextRules }),
        });
        message.textContent = "安全规则已保存并同步到运行时。";
        renderSettings();
      } catch (error) {
        message.textContent = `保存失败：${error.message}`;
      }
    });
  }
}

function splitTags(value) {
  return String(value || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function isProtectedDenyRuleId(id) {
  return ["destructive_command_deny", "remote_download_pipe_shell_deny", "nested_interpreter_deny", "complex_shell_syntax_deny", "empty_shell_deny", "shell_parse_error_deny", "missing_executable_deny"].includes(id);
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
      <div class="mt-2 text-[11px] text-secondary">Key: ${escapeHTML(presetKeyLabel(preset))}</div>
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
  const embeddingModelInput = document.getElementById("settings-embedding-model-input");
  const presetAPIKeyInput = document.getElementById("settings-preset-api-key-input");
  const presetActiveInput = document.getElementById("settings-preset-active-input");
  const maxAgentStepsInput = document.getElementById("settings-max-agent-steps-input");
  const bypassApprovalsInput = document.getElementById("settings-bypass-approvals-input");
  const contextSoftLimitInput = document.getElementById("settings-context-soft-limit-input");
  const compressionTriggerInput = document.getElementById("settings-compression-trigger-input");
  const responseReserveInput = document.getElementById("settings-response-reserve-input");
  const recentFullTurnsInput = document.getElementById("settings-recent-full-turns-input");
  const olderUserLedgerInput = document.getElementById("settings-older-user-ledger-input");
  const hostProfileTTLInput = document.getElementById("settings-host-profile-ttl-input");
  const toolResultMaxCharsInput = document.getElementById("settings-tool-result-max-chars-input");
  const toolResultHeadCharsInput = document.getElementById("settings-tool-result-head-chars-input");
  const toolResultTailCharsInput = document.getElementById("settings-tool-result-tail-chars-input");
  const sopRetrievalLimitInput = document.getElementById("settings-sop-retrieval-limit-input");
  const operatorForm = document.getElementById("settings-operator-form");
  const operatorStrictnessInput = document.getElementById("settings-operator-strictness-input");
  const operatorReadOnlyInput = document.getElementById("settings-operator-readonly-input");
  const operatorForceInput = document.getElementById("settings-operator-force-input");
  const operatorBypassInput = document.getElementById("settings-operator-bypass-input");
  const operatorPlaintextSSHInput = document.getElementById("settings-operator-plaintext-ssh-input");
  const operatorAutomationBypassInput = document.getElementById("settings-operator-automation-bypass-input");
  const operatorRemoteValidationInput = document.getElementById("settings-operator-remote-validation-input");
  const operatorMessage = document.getElementById("settings-operator-message");
  const knowledgeList = document.getElementById("settings-knowledge-list");
  const knowledgeForm = document.getElementById("settings-knowledge-form");
  const knowledgeNewButton = document.getElementById("settings-knowledge-new");
  const knowledgeMessage = document.getElementById("settings-knowledge-message");
  const policyForm = document.getElementById("settings-policy-form");
  const policyList = document.getElementById("settings-policy-list");
  const policyMessage = document.getElementById("settings-policy-message");
  const activateButton = document.getElementById("settings-activate-preset");
  const deleteButton = document.getElementById("settings-delete-preset");
  const addButton = document.getElementById("settings-add-preset");
  if (!presetIDInput || !presetNameInput || !presetModelInput || !presetBaseURLInput || !embeddingModelInput || !presetAPIKeyInput || !presetActiveInput || !maxAgentStepsInput || !bypassApprovalsInput || !contextSoftLimitInput || !compressionTriggerInput || !responseReserveInput || !recentFullTurnsInput || !olderUserLedgerInput || !hostProfileTTLInput || !toolResultMaxCharsInput || !toolResultHeadCharsInput || !toolResultTailCharsInput || !sopRetrievalLimitInput || !operatorForm || !operatorStrictnessInput || !operatorReadOnlyInput || !operatorForceInput || !operatorBypassInput || !operatorPlaintextSSHInput || !operatorAutomationBypassInput || !operatorRemoteValidationInput || !operatorMessage || !knowledgeList || !knowledgeForm || !knowledgeNewButton || !knowledgeMessage || !policyForm || !policyList || !policyMessage || !activateButton || !deleteButton || !addButton) {
    return;
  }

  const effectivePreset = selectedPreset || { id: "", name: "", model: "", base_url: "", api_key: "" };
  const currentRuntime = runtimeSettingsWithDefaults();
  presetIDInput.value = effectivePreset.id || "";
  presetNameInput.value = effectivePreset.name || "";
  presetModelInput.value = effectivePreset.model || "";
  presetBaseURLInput.value = effectivePreset.base_url || "";
  embeddingModelInput.value = state.gatewaySettings?.embedding_model || state.health?.embedding_model || "text-embedding-3-small";
  presetAPIKeyInput.value = "";
  presetAPIKeyInput.placeholder = effectivePreset.api_key_configured ? "留空表示保留当前 API key；填写新 key 才会替换" : "sk-...";
  presetActiveInput.checked = effectivePreset.id ? effectivePreset.id === currentGatewayPresetId() : true;
  maxAgentStepsInput.value = currentRuntime.max_agent_steps;
  bypassApprovalsInput.checked = Boolean(currentRuntime.bypass_approvals);
  contextSoftLimitInput.value = currentRuntime.context_soft_limit_tokens;
  compressionTriggerInput.value = currentRuntime.compression_trigger_tokens;
  responseReserveInput.value = currentRuntime.response_reserve_tokens;
  recentFullTurnsInput.value = currentRuntime.recent_full_turns;
  olderUserLedgerInput.value = currentRuntime.older_user_ledger_entries;
  hostProfileTTLInput.value = currentRuntime.host_profile_ttl_minutes;
  toolResultMaxCharsInput.value = currentRuntime.tool_result_max_chars;
  toolResultHeadCharsInput.value = currentRuntime.tool_result_head_chars;
  toolResultTailCharsInput.value = currentRuntime.tool_result_tail_chars;
  sopRetrievalLimitInput.value = currentRuntime.sop_retrieval_limit;
  const operator = state.operatorProfile || {};
  operatorStrictnessInput.value = operator.approval_strictness || "standard";
  operatorReadOnlyInput.checked = Boolean(operator.prefer_read_only_first ?? true);
  operatorForceInput.checked = Boolean(operator.allow_force_approve ?? true);
  operatorBypassInput.checked = Boolean(operator.allow_bypass_approvals);
  operatorPlaintextSSHInput.checked = Boolean(operator.allow_plaintext_ssh_warning ?? true);
  operatorAutomationBypassInput.checked = Boolean(operator.allow_automation_bypass);
  operatorRemoteValidationInput.checked = Boolean(operator.remote_validation_required ?? true);
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
        next.runtime_settings = {
          max_agent_steps: Number(maxAgentStepsInput.value || 0),
          bypass_approvals: bypassApprovalsInput.checked,
          context_soft_limit_tokens: Number(contextSoftLimitInput.value || 0),
          compression_trigger_tokens: Number(compressionTriggerInput.value || 0),
          response_reserve_tokens: Number(responseReserveInput.value || 0),
          recent_full_turns: Number(recentFullTurnsInput.value || 0),
          older_user_ledger_entries: Number(olderUserLedgerInput.value || 0),
          host_profile_ttl_minutes: Number(hostProfileTTLInput.value || 0),
          tool_result_max_chars: Number(toolResultMaxCharsInput.value || 0),
          tool_result_head_chars: Number(toolResultHeadCharsInput.value || 0),
          tool_result_tail_chars: Number(toolResultTailCharsInput.value || 0),
          sop_retrieval_limit: Number(sopRetrievalLimitInput.value || 0),
        };
        next.embedding_model = embeddingModelInput.value.trim();
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
        if (next.current_preset_id === id) next.current_preset_id = next.presets[0]?.id || "";
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
      presetAPIKeyInput.placeholder = "sk-...";
      presetActiveInput.checked = true;
      const defaults = runtimeSettingsWithDefaults();
      maxAgentStepsInput.value = defaults.max_agent_steps;
      bypassApprovalsInput.checked = defaults.bypass_approvals;
      contextSoftLimitInput.value = defaults.context_soft_limit_tokens;
      compressionTriggerInput.value = defaults.compression_trigger_tokens;
      responseReserveInput.value = defaults.response_reserve_tokens;
      recentFullTurnsInput.value = defaults.recent_full_turns;
      olderUserLedgerInput.value = defaults.older_user_ledger_entries;
      hostProfileTTLInput.value = defaults.host_profile_ttl_minutes;
      toolResultMaxCharsInput.value = defaults.tool_result_max_chars;
      toolResultHeadCharsInput.value = defaults.tool_result_head_chars;
      toolResultTailCharsInput.value = defaults.tool_result_tail_chars;
      sopRetrievalLimitInput.value = defaults.sop_retrieval_limit;
      message.textContent = "填写新预设后点击保存即可。";
      presetNameInput.focus();
    });
  }

  if (!operatorForm.dataset.bound) {
    operatorForm.dataset.bound = "true";
    operatorForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      try {
        operatorMessage.textContent = "正在保存操作员偏好...";
        state.operatorProfile = await request("/api/settings/operator", {
          method: "PUT",
          body: JSON.stringify({
            id: "default",
            approval_strictness: operatorStrictnessInput.value,
            allow_bypass_approvals: operatorBypassInput.checked,
            allow_force_approve: operatorForceInput.checked,
            allow_plaintext_ssh_warning: operatorPlaintextSSHInput.checked,
            allow_automation_bypass: operatorAutomationBypassInput.checked,
            prefer_read_only_first: operatorReadOnlyInput.checked,
            remote_validation_required: operatorRemoteValidationInput.checked,
          }),
        });
        operatorMessage.textContent = "操作员偏好已保存。";
      } catch (error) {
        operatorMessage.textContent = `保存失败：${error.message}`;
      }
    });
  }

  renderKnowledgeSettings(knowledgeList, knowledgeForm, knowledgeNewButton, knowledgeMessage);
  renderPolicySettings(policyList, policyForm, policyMessage);

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
    case "run.policy_override_requested":
      return "需要 Force approve";
    case "run.policy_override_resolved":
      return "Force approve 已处理";
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
    case "run.policy_override_requested":
      return "shield";
    case "run.policy_override_resolved":
      return "gpp_maybe";
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
  if (String(decision).toLowerCase() === "force_approve") {
    return { label: "Force approved", className: "bg-[#FFF1EC] text-[#9A4021]" };
  }
  if (["approve", "approved", "allow"].includes(String(decision).toLowerCase())) {
    return { label: "已批准", className: "bg-[#E3F1ED] text-[#00796B]" };
  }
  return { label: "已拒绝", className: "bg-[#FDECEA] text-[#B3261E]" };
}

function scrollChatToBottom() {
  const scroller = document.querySelector(".app-chat-scroll");
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
        if (state.currentSessionId) await loadSessionDetail(state.currentSessionId);
      } else {
        throw error;
      }
    }
  }

  const newSessionButton = document.getElementById("new-session-button");
  const chatForm = document.getElementById("chat-form");
  const input = document.getElementById("chat-input");
  const hostSelect = document.getElementById("chat-host-select");
  const bypassToggle = document.getElementById("chat-bypass-toggle");
  const note = document.getElementById("chat-composer-note");

  newSessionButton?.addEventListener("click", () => {
    state.currentSessionId = "";
    state.currentSessionDetail = null;
    const url = new URL(window.location.href);
    url.searchParams.delete("session");
    window.history.replaceState({}, "", url.toString());
    syncSelectedHost();
    renderChat();
    input?.focus();
  });

  if (bypassToggle && !bypassToggle.dataset.bound) {
    bypassToggle.dataset.bound = "true";
    bypassToggle.addEventListener("change", async (event) => {
      try {
        await syncComposerBypass(event.currentTarget.checked);
        if (note) note.textContent = state.currentSessionId
          ? "当前会话后续 runs 已更新 bypass 模式。"
          : "新会话将使用当前 bypass 模式。";
      } catch (error) {
        event.currentTarget.checked = !event.currentTarget.checked;
        if (note) note.textContent = `切换 bypass 失败：${error.message}`;
      }
    });
  }

  if (hostSelect && !hostSelect.dataset.bound) {
    hostSelect.dataset.bound = "true";
    hostSelect.addEventListener("change", (event) => {
      state.selectedHostId = event.currentTarget.value;
      renderChat();
    });
  }

  if (input && !input.dataset.bound) {
    input.dataset.bound = "true";
    const resize = () => {
      input.style.height = "auto";
      input.style.height = `${Math.min(input.scrollHeight, 220)}px`;
    };
    input.addEventListener("input", resize);
    resize();
  }

  if (chatForm && !chatForm.dataset.bound) {
    chatForm.dataset.bound = "true";
    chatForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      const text = input?.value.trim() || "";
      if (!text) return;
      const hostID = getDefaultHostId();
      const host = findHostById(hostID);
      if (!host) {
        if (note) note.textContent = "发起失败：当前没有可用主机，请先到资产管理接入主机。";
        return;
      }
      try {
        if (note) note.textContent = "正在发起真实执行...";
        const result = await request("/api/runs", {
          method: "POST",
          body: JSON.stringify({
            host_id: hostID,
            session_id: state.currentSessionId || "",
            user_input: text,
            requested_by: "web",
            bypass_approvals: bypassToggle ? bypassToggle.checked : composerBypassEnabled(),
          }),
        });
        if (input) {
          input.value = "";
          input.style.height = "auto";
        }
        await loadCore();
        await loadSessionDetail(result.session_id);
        if (note) note.textContent = "已提交到统一控制面，实时事件会继续刷新。";
        renderChat();
      } catch (error) {
        if (note) note.textContent = `发起失败：${error.message}`;
      }
    });
  }

  renderChat();
}

async function init() {
  await loadCore();
  bindGlobalNavigationCleanup();
  if (page === "chat") {
    connectGlobalEvents();
  } else {
    disconnectGlobalEvents();
  }
  if (page === "chat") {
    await initChatPage();
  }
  renderPage();
}

init().catch((error) => {
  document.body.innerHTML = `<pre style="padding:24px;color:#8a2d2d;">${escapeHTML(error.message)}</pre>`;
});

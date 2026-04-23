const page = document.body.dataset.page;

const state = {
  health: null,
  hosts: [],
  runs: [],
  approvals: [],
  sessions: [],
  currentSessionId: "",
  currentSessionDetail: null,
  activeDrafts: new Map(),
  liveEvents: new Map(),
  eventSource: null,
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

function formatTime(value) {
  if (!value) return "";
  return new Date(value).toLocaleString();
}

function renderMarkdown(value) {
  const input = String(value ?? "");
  if (!window.marked?.parse) return escapeHTML(input);
  return sanitizeHTML(window.marked.parse(input, { breaks: true, gfm: true }));
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

function toolEventBlocks(events = []) {
  return events.filter((event) => [
    "run.policy_checked",
    "run.tool_running",
    "run.tool_finished",
    "run.waiting_approval",
    "run.approval_resolved",
    "run.failed",
  ].includes(event.type));
}

async function loadCore() {
  const [health, hosts, runs, approvals, sessions] = await Promise.all([
    request("/api/health"),
    request("/api/hosts"),
    request("/api/runs"),
    request("/api/approvals"),
    request("/api/sessions"),
  ]);
  state.health = health;
  state.hosts = hosts.items || [];
  state.runs = (runs.items || []).sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
  state.approvals = (approvals.items || []).sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
  state.sessions = (sessions.items || []).sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at));
}

async function loadSessionDetail(sessionId) {
  if (!sessionId) {
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

function connectGlobalEvents() {
  if (state.eventSource) state.eventSource.close();
  const source = new EventSource("/api/events/stream");
  state.eventSource = source;
  source.onmessage = async (message) => {
    const event = JSON.parse(message.data);
    const runEvents = state.liveEvents.get(event.run_id) || [];
    runEvents.push(event);
    state.liveEvents.set(event.run_id, runEvents.slice(-50));

    if (event.type === "run.message_delta") {
      const draft = state.activeDrafts.get(event.run_id) || "";
      state.activeDrafts.set(event.run_id, draft + (event.message || ""));
    }
    if (["run.assistant_message", "run.completed", "run.failed"].includes(event.type)) {
      state.activeDrafts.delete(event.run_id);
    }

    if (page === "chat") {
      appendChatTrace(event);
    }

    if ([
      "run.created",
      "run.tool_running",
      "run.tool_finished",
      "run.waiting_approval",
      "run.approval_resolved",
      "run.completed",
      "run.failed",
    ].includes(event.type)) {
      await loadCore();
      if (state.currentSessionId) {
        await loadSessionDetail(state.currentSessionId);
      }
      renderPage();
    } else if (page === "chat" && state.currentSessionDetail?.turns.some((item) => item.run.id === event.run_id)) {
      renderChat();
    }
  };
}

function appendChatTrace(event) {
  const list = document.getElementById("approval-list");
  if (!list) return;
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

function renderChat() {
  const healthText = document.getElementById("gateway-health-text");
  const healthMeta = document.getElementById("gateway-health-meta");
  const approvalCount = document.getElementById("approval-count");
  const approvalList = document.getElementById("approval-list");
  const chatHistory = document.getElementById("chat-history");

  if (healthText) healthText.innerHTML = `<span class="w-2 h-2 rounded-full bg-tertiary-container"></span>${state.health?.status === "ok" ? "正常" : "异常"}`;
  if (healthMeta) healthMeta.textContent = state.health?.no_sandbox ? "真实权限" : "sandboxed";

  const pendingApprovals = state.approvals.filter((item) => !item.decision);
  if (approvalCount) approvalCount.textContent = `${pendingApprovals.length} 项`;
  if (approvalList) {
    approvalList.innerHTML = pendingApprovals.length === 0 ? `<li class="text-sm text-[#5C5C59]">当前无待审批任务</li>` : "";
    for (const approval of pendingApprovals) {
      const item = document.createElement("li");
      item.className = "bg-surface-container-lowest border border-[#E2E2DB] rounded-lg p-sm flex items-start gap-sm hover:bg-surface transition-colors";
      item.innerHTML = `
        <span class="material-symbols-outlined text-primary text-[18px] mt-0.5">warning</span>
        <div class="min-w-0 flex-1">
          <div class="font-body-sm text-body-sm text-on-surface font-medium">${escapeHTML(approval.tool_name)}</div>
          <div class="font-body-sm text-[12px] text-secondary mb-2">${escapeHTML(approval.reason)}</div>
          <div class="flex gap-2">
            <button class="px-2 py-1 rounded bg-[#C96442] text-white text-xs" data-id="${approval.id}" data-decision="approve" type="button">批准</button>
            <button class="px-2 py-1 rounded bg-[#E6E4D9] text-[#262624] text-xs" data-id="${approval.id}" data-decision="deny" type="button">拒绝</button>
          </div>
        </div>
      `;
      item.querySelectorAll("button").forEach((button) => {
        button.addEventListener("click", async () => {
          await request(`/api/approvals/${button.dataset.id}/resolve`, {
            method: "POST",
            body: JSON.stringify({ decision: button.dataset.decision, actor: "web" }),
          });
          await loadCore();
          if (state.currentSessionId) await loadSessionDetail(state.currentSessionId);
          renderChat();
        });
      });
      approvalList.appendChild(item);
    }
  }

  chatHistory.innerHTML = "";
  if (!state.currentSessionDetail || state.currentSessionDetail.turns.length === 0) {
    chatHistory.innerHTML = `
      <div class="flex gap-md group">
        <div class="w-8 h-8 rounded-full bg-surface-container-high flex items-center justify-center shrink-0 border border-outline-variant">
          <span class="material-symbols-outlined text-[18px] text-on-surface">robot_2</span>
        </div>
        <div class="pt-1 text-on-surface font-body-md text-body-md">输入一条运维请求开始新的会话。</div>
      </div>`;
    return;
  }

  for (const item of state.currentSessionDetail.turns) {
    chatHistory.appendChild(renderUserRow(item.turn.user_input));
    const live = state.liveEvents.get(item.run.id) || [];
    const eventBlocks = toolEventBlocks(dedupeEvents([...item.events, ...live]));
    if (eventBlocks.length > 0) {
      chatHistory.appendChild(renderAssistantCard(eventBlocks));
    }
    const draft = state.activeDrafts.get(item.run.id);
    const assistantContent = draft && !item.turn.final_explanation && !item.run.final_response ? draft : (item.turn.final_explanation || item.run.final_response || runStatusText(item.run.status));
    chatHistory.appendChild(renderAssistantMessage(assistantContent, item.run));
  }
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

function renderAssistantCard(events) {
  const node = document.createElement("div");
  node.className = "flex gap-md";
  node.innerHTML = `
    <div class="w-8 h-8 rounded-full bg-[#E6E4D9] flex items-center justify-center shrink-0 border border-[#E2E2DB]">
      <span class="material-symbols-outlined text-[18px] text-[#C96442]" style="font-variation-settings: 'FILL' 1;">robot_2</span>
    </div>
    <div class="flex-1">
      <div class="bg-surface-container-lowest border border-[#E2E2DB] rounded-xl p-md">
        <div class="space-y-3" id="assistant-card-list"></div>
      </div>
    </div>
  `;
  const list = node.querySelector("#assistant-card-list");
  for (const event of events) {
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
      ${event.payload ? `<div class="markdown-body mt-2 text-xs text-on-surface-variant">${renderMarkdown("```json\n" + JSON.stringify(event.payload, null, 2) + "\n```")}</div>` : ""}
    `;
    list.appendChild(block);
  }
  return node;
}

function renderAssistantMessage(content, run) {
  const node = document.createElement("div");
  node.className = "flex gap-md";
  node.innerHTML = `
    <div class="w-8 h-8 rounded-full bg-[#E6E4D9] flex items-center justify-center shrink-0 border border-[#E2E2DB]">
      <span class="material-symbols-outlined text-[18px] text-[#C96442]" style="font-variation-settings: 'FILL' 1;">robot_2</span>
    </div>
    <div class="flex-1">
      <div class="bg-surface-container-lowest border border-[#E2E2DB] rounded-xl p-md">
        <div class="flex items-center justify-between border-b border-surface-variant pb-sm mb-sm">
          <div class="flex items-center gap-sm">
            <span class="material-symbols-outlined text-tertiary-container text-[18px]">${run.status === "completed" ? "check_circle" : "autorenew"}</span>
            <span class="font-label text-label text-on-surface font-medium">${escapeHTML(run.id)}</span>
          </div>
          <span class="font-body-sm text-body-sm text-secondary">${run.status}</span>
        </div>
        <div class="markdown-body font-body-sm text-body-sm text-secondary">${renderMarkdown(content)}</div>
      </div>
    </div>
  `;
  return node;
}

function renderAssets() {
  const list = document.getElementById("assets-host-list");
  const form = document.getElementById("assets-host-form");
  list.innerHTML = "";
  if (state.hosts.length === 0) {
    list.innerHTML = `<div class="bg-white rounded-[16px] p-5 border border-[#E2E2DB] text-sm text-[#5C5C59]">当前没有主机，先在右侧注册一台。</div>`;
  }
  for (const host of state.hosts) {
    const online = host.mode === "local" || !!host.address;
    const modeLabel = host.mode === "local" ? "本地" : "SSH";
    const node = document.createElement("div");
    node.className = `rounded-[16px] p-5 border border-[#E2E2DB] transition-colors relative overflow-hidden ${online ? "bg-white hover:bg-surface-container-low" : "bg-surface-container-low opacity-75"}`;
    node.innerHTML = `
      <div class="flex justify-between items-start mb-4">
        <div class="flex items-center gap-3">
          <div class="w-10 h-10 rounded-lg bg-surface-container flex items-center justify-center text-primary"><span class="material-symbols-outlined">dns</span></div>
          <div>
            <h3 class="font-h3 text-[18px] text-on-surface leading-tight">${escapeHTML(host.display_name)}</h3>
            <span class="font-label text-[12px] text-secondary">ID: ${escapeHTML(host.id)}</span>
          </div>
        </div>
        <span class="flex items-center gap-1 text-[12px] font-label ${online ? "text-tertiary-container bg-tertiary-container/10" : "text-error bg-error-container/50"} px-2 py-1 rounded-full">
          <span class="w-1.5 h-1.5 rounded-full ${online ? "bg-tertiary-container" : "bg-error"}"></span>${online ? "在线" : "离线"}
        </span>
      </div>
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-4 mt-4 pt-4 border-t border-[#E2E2DB]/50">
        <div><p class="font-label text-[11px] text-secondary uppercase tracking-wider mb-1">地址</p><p class="font-body-sm text-[13px] text-on-surface font-mono">${escapeHTML(host.address || "localhost")}</p></div>
        <div><p class="font-label text-[11px] text-secondary uppercase tracking-wider mb-1">模式</p><p class="font-body-sm text-[13px] text-on-surface flex items-center gap-1"><span class="material-symbols-outlined text-[14px]">${host.mode === "local" ? "computer" : "terminal"}</span>${modeLabel}</p></div>
        <div><p class="font-label text-[11px] text-secondary uppercase tracking-wider mb-1">用户</p><p class="font-body-sm text-[13px] text-on-surface">${escapeHTML(host.user || "agent")}</p></div>
        <div><p class="font-label text-[11px] text-secondary uppercase tracking-wider mb-1">更新时间</p><p class="font-body-sm text-[13px] text-on-surface">${formatTime(host.updated_at)}</p></div>
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
          id: data.get("id"),
          display_name: data.get("display_name"),
          address: data.get("address"),
          mode: data.get("mode"),
          port: Number(data.get("port") || 0),
          user: data.get("user"),
          password_env: data.get("password_env"),
        }),
      });
      await loadCore();
      renderAssets();
      form.reset();
    });
  }
}

function renderAutomation() {
  const successCount = state.runs.filter((run) => run.status === "completed").length;
  const failCount = state.runs.filter((run) => run.status === "failed" || run.status === "denied").length;
  const runningCount = state.runs.filter((run) => ["created", "running_agent", "tool_running", "waiting_approval"].includes(run.status)).length;
  const total = state.runs.length || 1;
  document.getElementById("automation-success-rate").textContent = `${Math.round((successCount / total) * 100)}%`;
  document.getElementById("automation-success-meta").textContent = `/ ${total} 次`;
  document.getElementById("automation-running-count").textContent = String(runningCount);
  document.getElementById("automation-failed-count").textContent = String(failCount);
  document.getElementById("automation-run-total").textContent = `全部 (${total})`;

  const list = document.getElementById("automation-run-list");
  list.innerHTML = "";
  for (const run of state.runs.slice(0, 20)) {
    const session = state.sessions.find((entry) => entry.id === run.session_id);
    const host = state.hosts.find((entry) => entry.id === run.host_id);
    const statusMeta = runStatusMeta(run.status);
    const item = document.createElement("div");
    item.className = `px-6 py-5 hover:bg-surface-container-low transition-colors group cursor-pointer ${statusMeta.tint}`;
    item.innerHTML = `
      <div class="flex items-center justify-between mb-2">
        <div class="flex items-center gap-3">
          <span class="material-symbols-outlined ${statusMeta.color}">${statusMeta.icon}</span>
          <span class="font-h2 text-on-surface text-base font-semibold">${escapeHTML(run.id)}</span>
          <span class="font-label text-on-surface-variant bg-[#E6E4D9] px-2 py-0.5 rounded text-[11px]">${escapeHTML(session?.title || run.host_id)}</span>
        </div>
        <span class="font-body-sm text-secondary text-[13px]">${formatTime(run.updated_at)}</span>
      </div>
      <div class="flex items-center gap-6 ml-9">
        <div class="flex items-center gap-1.5 text-secondary"><span class="material-symbols-outlined text-[16px]">dns</span><span class="font-body-sm text-[13px]">${escapeHTML(host?.display_name || run.host_id)}</span></div>
        <div class="flex items-center gap-1.5 text-secondary"><span class="material-symbols-outlined text-[16px]">schedule</span><span class="font-body-sm text-[13px]">${run.status}</span></div>
        <div class="flex items-center gap-1.5 ${statusMeta.color} font-medium"><span class="font-body-sm text-[13px]">${escapeHTML(run.final_response || run.failure_message || "处理中")}</span></div>
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
  document.getElementById("settings-gateway-status").textContent = state.health?.status === "ok" ? "健康运行" : "异常";
  document.getElementById("settings-policy-summary").textContent = "shell-first / read-only allow / mutating ask / destructive deny";
  document.getElementById("settings-model").textContent = "LongCat-Flash-Thinking-2601";
  document.getElementById("settings-host-count").textContent = String(state.hosts.length);
  document.getElementById("settings-run-count").textContent = String(state.runs.length);
  const box = document.getElementById("settings-capabilities");
  box.innerHTML = "";
  for (const item of [
    "真实 LongCat 流式 tool-calling",
    "run_shell 命令执行与返回裁剪",
    "审批流 allow / ask / deny",
    "本地与 SSH 执行",
    "session / turn / run / event / audit",
    "markdown 对话渲染"
  ]) {
    const node = document.createElement("div");
    node.className = "bg-surface rounded-lg border border-surface-variant/50 p-md";
    node.textContent = item;
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
    default:
      return { icon: "autorenew", color: "text-primary-container", tint: "" };
  }
}

function runStatusText(status) {
  switch (status) {
    case "waiting_approval": return "等待人工审批。";
    case "completed": return "执行完成。";
    case "failed": return "执行失败。";
    case "denied": return "执行被拒绝。";
    default: return "处理中。";
  }
}

function eventTitle(event) {
  switch (event.type) {
    case "run.policy_checked": return "Policy 已检查";
    case "run.tool_running": return `执行 ${event.message}`;
    case "run.tool_finished": return `${event.message} 已返回`;
    case "run.waiting_approval": return "等待审批";
    case "run.approval_resolved": return "审批已处理";
    case "run.failed": return "执行失败";
    default: return event.type;
  }
}

function eventIcon(type) {
  switch (type) {
    case "run.policy_checked": return "verified_user";
    case "run.tool_running": return "terminal";
    case "run.tool_finished": return "check_circle";
    case "run.waiting_approval": return "warning";
    case "run.approval_resolved": return "fact_check";
    case "run.failed": return "error";
    default: return "robot_2";
  }
}

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

async function initChatPage() {
  const querySession = new URL(window.location.href).searchParams.get("session");
  state.currentSessionId = querySession || state.sessions[0]?.id || "";
  if (state.currentSessionId) {
    await loadSessionDetail(state.currentSessionId);
  }
  document.getElementById("new-session-button").addEventListener("click", () => {
    state.currentSessionId = "";
    state.currentSessionDetail = null;
    state.activeDrafts.clear();
    const url = new URL(window.location.href);
    url.searchParams.delete("session");
    window.history.replaceState({}, "", url.toString());
    renderChat();
  });
  document.getElementById("chat-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const input = document.getElementById("chat-input");
    const text = input.value.trim();
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
    input.value = "";
    await loadCore();
    await loadSessionDetail(result.session_id);
    renderChat();
  });
  renderChat();
}

async function init() {
  await loadCore();
  connectGlobalEvents();
  if (page === "chat") await initChatPage();
  renderPage();
}

init().catch((error) => {
  document.body.innerHTML = `<pre style="padding:24px;color:#8a2d2d;">${escapeHTML(error.message)}</pre>`;
});

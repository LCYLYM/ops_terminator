const healthNode = document.getElementById("health");
const hostsNode = document.getElementById("hosts");
const runHostNode = document.getElementById("run-host");
const approvalsNode = document.getElementById("approvals");
const eventsNode = document.getElementById("events");
const runBadgeNode = document.getElementById("run-badge");

let currentRunId = "";

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

async function loadHealth() {
  const data = await request("/api/health");
  healthNode.textContent = data.status;
}

async function loadHosts() {
  const data = await request("/api/hosts");
  hostsNode.innerHTML = "";
  runHostNode.innerHTML = "";
  for (const host of data.items) {
    const card = document.createElement("div");
    card.className = "card";
    card.innerHTML = `<strong>${host.display_name}</strong><div>${host.id} · ${host.mode}${host.address ? ` · ${host.address}` : ""}</div>`;
    hostsNode.appendChild(card);

    const option = document.createElement("option");
    option.value = host.id;
    option.textContent = `${host.display_name} (${host.id})`;
    runHostNode.appendChild(option);
  }
}

async function loadApprovals() {
  const data = await request("/api/approvals");
  approvalsNode.innerHTML = "";
  for (const item of data.items.filter((entry) => !entry.decision)) {
    const card = document.createElement("div");
    card.className = "card";
    card.innerHTML = `
      <strong>${item.tool_name}</strong>
      <div>${item.reason}</div>
      <div>${item.scope}</div>
      <div class="approval-actions">
        <button data-id="${item.id}" data-decision="approve">Approve</button>
        <button data-id="${item.id}" data-decision="deny">Deny</button>
      </div>
    `;
    approvalsNode.appendChild(card);
  }

  approvalsNode.querySelectorAll("button").forEach((button) => {
    button.addEventListener("click", async () => {
      await request(`/api/approvals/${button.dataset.id}/resolve`, {
        method: "POST",
        body: JSON.stringify({ decision: button.dataset.decision, actor: "web" }),
      });
      await loadApprovals();
    });
  });
}

function attachRunStream(runId) {
  currentRunId = runId;
  runBadgeNode.textContent = runId;
  eventsNode.textContent = "";
  const source = new EventSource(`/api/runs/${runId}/events/stream`);
  source.onmessage = (event) => {
    const data = JSON.parse(event.data);
    eventsNode.textContent += `[${data.type}] ${data.message || ""}\n`;
    if (data.payload) {
      eventsNode.textContent += `${JSON.stringify(data.payload, null, 2)}\n`;
    }
    eventsNode.scrollTop = eventsNode.scrollHeight;
    if (data.type === "run.completed" || data.type === "run.failed") {
      source.close();
      loadApprovals().catch(console.error);
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
      user_input: form.get("user_input"),
      requested_by: "web",
    }),
  });
  attachRunStream(result.id);
});

Promise.all([loadHealth(), loadHosts(), loadApprovals()]).catch((error) => {
  eventsNode.textContent = error.message;
});


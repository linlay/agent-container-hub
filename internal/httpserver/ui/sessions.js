import {
  api,
  bindModalDismiss,
  closeModal,
  escapeHTML,
  formatTime,
  initializeShell,
  openModal,
} from "/ui/common.js";

const state = {
  sessions: {
    filters: { status: "active", session_id: "", environment_name: "" },
    page: 1,
    pageSize: 20,
    total: 0,
    items: [],
    selectedID: "",
  },
  sessionDetail: null,
  executions: {
    sessionID: "",
    page: 1,
    pageSize: 10,
    total: 0,
    items: [],
    selectedID: 0,
  },
  createSession: {
    enabledEnvironments: [],
    selectedEnvironment: "",
  },
};

const sessionTableBody = document.getElementById("session-table-body");
const sessionListMeta = document.getElementById("session-list-meta");
const sessionSelectionMeta = document.getElementById("session-selection-meta");
const sessionPageMeta = document.getElementById("session-page-meta");
const sessionDetailContent = document.getElementById("session-detail-content");
const sessionCreateOutput = document.getElementById("session-create-output");
const createEnvironmentSelect = document.getElementById("create-environment");
const createEnvironmentHint = document.getElementById("create-environment-hint");
const createSessionButton = document.getElementById("create-session");
const executionHistory = document.getElementById("execution-history");
const executionDetail = document.getElementById("execution-detail");
const executionPageMeta = document.getElementById("execution-page-meta");
const executeModalMeta = document.getElementById("execute-modal-meta");

function buildSessionQueryString() {
  const params = new URLSearchParams({
    status: state.sessions.filters.status,
    page: String(state.sessions.page),
    page_size: String(state.sessions.pageSize),
  });
  if (state.sessions.filters.session_id) {
    params.set("session_id", state.sessions.filters.session_id);
  }
  if (state.sessions.filters.environment_name) {
    params.set("environment_name", state.sessions.filters.environment_name);
  }
  return params.toString();
}

function buildExecutionQueryString() {
  return new URLSearchParams({
    page: String(state.executions.page),
    page_size: String(state.executions.pageSize),
  }).toString();
}

async function refreshSessions(preserveSelection = true) {
  const data = await api(`/api/sessions/query?${buildSessionQueryString()}`);
  state.sessions.items = data.items || [];
  state.sessions.total = data.total || 0;
  if (!preserveSelection) {
    state.sessions.selectedID = "";
  }
  if (state.sessions.selectedID && !state.sessions.items.find((item) => item.session_id === state.sessions.selectedID)) {
    state.sessions.selectedID = "";
  }
  renderSessionTable();
}

function renderSessionTable() {
  const totalPages = Math.max(1, Math.ceil(Math.max(state.sessions.total, 1) / state.sessions.pageSize));
  sessionPageMeta.textContent = `Page ${state.sessions.page} / ${totalPages}`;
  sessionListMeta.textContent = `${state.sessions.total} matching session(s)`;
  sessionSelectionMeta.textContent = state.sessions.selectedID
    ? `Selected: ${state.sessions.selectedID}`
    : "No session selected.";

  if (state.sessions.items.length === 0) {
    sessionTableBody.innerHTML = `
      <tr>
        <td colspan="7">
          <div class="empty">No sessions match the current filter.</div>
        </td>
      </tr>
    `;
    return;
  }

  sessionTableBody.innerHTML = state.sessions.items.map((item) => `
    <tr class="${state.sessions.selectedID === item.session_id ? "selected" : ""}" data-row-session-id="${escapeHTML(item.session_id)}">
      <td>
        <strong>${escapeHTML(item.session_id)}</strong>
        <div class="cell-meta">${escapeHTML(item.container_id || "")}</div>
      </td>
      <td>${escapeHTML(item.environment_name)}</td>
      <td>
        <span class="pill ${item.status === "stopped" ? "stopped" : ""}">${escapeHTML(item.status || "-")}</span>
      </td>
      <td>
        <strong>${escapeHTML(item.image || "-")}</strong>
        <div class="cell-meta">${escapeHTML(item.cwd || "-")}</div>
      </td>
      <td>${escapeHTML(formatTime(item.created_at))}</td>
      <td>${escapeHTML(formatTime(item.stopped_at))}</td>
      <td>
        <div class="actions">
          <button class="ghost" data-action="view" data-session-id="${escapeHTML(item.session_id)}">View</button>
          <button class="secondary" data-action="executions" data-session-id="${escapeHTML(item.session_id)}">Execute Logs</button>
          ${item.status === "active" ? `<button class="danger" data-action="stop" data-session-id="${escapeHTML(item.session_id)}">Stop</button>` : ""}
        </div>
      </td>
    </tr>
  `).join("");

  sessionTableBody.querySelectorAll("[data-row-session-id]").forEach((row) => {
    row.addEventListener("click", (event) => {
      if (event.target.closest("button")) {
        return;
      }
      openSessionDetail(row.dataset.rowSessionId);
    });
  });

  sessionTableBody.querySelectorAll("[data-action]").forEach((button) => {
    button.addEventListener("click", async (event) => {
      event.stopPropagation();
      const sessionID = button.dataset.sessionId;
      const action = button.dataset.action;
      if (action === "view") {
        await openSessionDetail(sessionID);
        return;
      }
      if (action === "executions") {
        await openExecuteLogs(sessionID);
        return;
      }
      if (action === "stop") {
        await stopSession(sessionID, true);
      }
    });
  });
}

async function openSessionDetail(sessionID) {
  const detail = await api(`/api/sessions/${sessionID}`);
  state.sessions.selectedID = sessionID;
  state.sessionDetail = detail;
  renderSessionTable();
  renderSessionDetail();
  openModal("session-detail-backdrop");
}

function renderSessionDetail() {
  const item = state.sessionDetail;
  if (!item) {
    sessionDetailContent.innerHTML = `<div class="empty">Select a session from the table to inspect it.</div>`;
    return;
  }

  const mounts = (item.mounts || []).map((mount) => `
    <div class="mount-item">
      <div><strong>${escapeHTML(mount.destination || "-")}</strong></div>
      <div class="meta">${escapeHTML(mount.source || "-")}</div>
      <div class="meta">${mount.read_only ? "read only" : "read write"}</div>
    </div>
  `).join("") || `<div class="empty">No mounts recorded for this session.</div>`;

  sessionDetailContent.innerHTML = `
    <div class="toolbar wrap">
      <div>
        <h3>${escapeHTML(item.session_id)}</h3>
        <div class="meta">${escapeHTML(item.environment_name)} · ${escapeHTML(item.image || "-")}</div>
      </div>
      <div class="actions">
        <button class="secondary" id="detail-open-executions">View Execute</button>
        ${item.status === "active" ? `<button class="danger" id="detail-stop-session">Stop Session</button>` : ""}
      </div>
    </div>

    <div class="detail-grid">
      <div class="detail-box"><div class="meta">Status</div><strong>${escapeHTML(item.status || "-")}</strong></div>
      <div class="detail-box"><div class="meta">Environment</div><strong>${escapeHTML(item.environment_name || "-")}</strong></div>
      <div class="detail-box"><div class="meta">Image</div><strong>${escapeHTML(item.image || "-")}</strong></div>
      <div class="detail-box"><div class="meta">Cwd</div><strong>${escapeHTML(item.cwd || "-")}</strong></div>
      <div class="detail-box"><div class="meta">Created At</div><strong>${escapeHTML(formatTime(item.created_at))}</strong></div>
      <div class="detail-box"><div class="meta">Stopped At</div><strong>${escapeHTML(formatTime(item.stopped_at))}</strong></div>
      <div class="detail-box"><div class="meta">Workspace</div><strong>${escapeHTML(item.workspace_path || "-")}</strong></div>
      <div class="detail-box"><div class="meta">Container ID</div><strong>${escapeHTML(item.container_id || "-")}</strong></div>
    </div>

    <div class="stack">
      <div>
        <h3>Mount Snapshot</h3>
        <div class="meta">Includes environment mounts and the auto-mounted workspace path.</div>
      </div>
      <div class="mount-list">${mounts}</div>
    </div>

    <div class="stack">
      <div>
        <h3>Labels</h3>
      </div>
      <pre>${escapeHTML(JSON.stringify(item.labels || {}, null, 2))}</pre>
    </div>

    <div class="stack">
      <div>
        <h3>Resources</h3>
      </div>
      <pre>${escapeHTML(JSON.stringify(item.resources || {}, null, 2))}</pre>
    </div>
  `;

  document.getElementById("detail-open-executions").addEventListener("click", async () => {
    await openExecuteLogs(item.session_id);
  });

  const stopButton = document.getElementById("detail-stop-session");
  if (stopButton) {
    stopButton.addEventListener("click", async () => {
      await stopSession(item.session_id, false);
    });
  }
}

async function openExecuteLogs(sessionID) {
  state.executions.sessionID = sessionID;
  state.executions.page = 1;
  state.executions.selectedID = 0;
  executeModalMeta.textContent = `Session ${sessionID}`;
  openModal("execute-log-backdrop");
  await refreshExecutions();
}

async function refreshExecutions() {
  if (!state.executions.sessionID) {
    renderExecuteLogs();
    return;
  }
  const data = await api(`/api/sessions/${state.executions.sessionID}/executions?${buildExecutionQueryString()}`);
  state.executions.items = data.items || [];
  state.executions.total = data.total || 0;
  if (!state.executions.selectedID && state.executions.items.length > 0) {
    state.executions.selectedID = state.executions.items[0].id;
  }
  if (state.executions.selectedID && !state.executions.items.find((item) => item.id === state.executions.selectedID)) {
    state.executions.selectedID = state.executions.items[0]?.id || 0;
  }
  renderExecuteLogs();
}

function renderExecuteLogs() {
  const totalPages = Math.max(1, Math.ceil(Math.max(state.executions.total, 1) / state.executions.pageSize));
  executionPageMeta.textContent = `Page ${state.executions.page} / ${totalPages}`;

  if (!state.executions.sessionID) {
    executionHistory.innerHTML = `<div class="empty">No session selected.</div>`;
    executionDetail.className = "empty";
    executionDetail.textContent = "Select an execution from the history list.";
    return;
  }

  if (state.executions.items.length === 0) {
    executionHistory.innerHTML = `<div class="empty">No persisted execute logs. This usually means \`ENABLE_EXEC_LOG_PERSIST\` is disabled or no commands were executed yet.</div>`;
    executionDetail.className = "empty";
    executionDetail.textContent = "Execute detail will appear here when a persisted log entry is selected.";
    return;
  }

  executionHistory.innerHTML = state.executions.items.map((item) => `
    <div class="history-item ${state.executions.selectedID === item.id ? "active" : ""}" data-execution-id="${item.id}">
      <div class="toolbar">
        <strong>${escapeHTML(item.command || "-")}</strong>
        <span class="pill ${item.exit_code === 0 ? "" : "stopped"}">exit ${escapeHTML(item.exit_code)}</span>
      </div>
      <div class="meta">${escapeHTML(item.started_at || "-")}</div>
      <div class="meta">${escapeHTML(item.cwd || "-")} · ${escapeHTML(item.duration_ms)} ms</div>
    </div>
  `).join("");

  executionHistory.querySelectorAll("[data-execution-id]").forEach((node) => {
    node.addEventListener("click", () => {
      state.executions.selectedID = Number(node.dataset.executionId);
      renderExecuteLogs();
    });
  });

  const selected = state.executions.items.find((item) => item.id === state.executions.selectedID) || state.executions.items[0];
  state.executions.selectedID = selected.id;
  executionDetail.className = "stack";
  executionDetail.innerHTML = `
    <div class="detail-grid">
      <div class="detail-box"><div class="meta">Command</div><strong>${escapeHTML(selected.command || "-")}</strong></div>
      <div class="detail-box"><div class="meta">Exit Code</div><strong>${escapeHTML(selected.exit_code)}</strong></div>
      <div class="detail-box"><div class="meta">Cwd</div><strong>${escapeHTML(selected.cwd || "-")}</strong></div>
      <div class="detail-box"><div class="meta">Timeout</div><strong>${escapeHTML(selected.timeout_ms)} ms</strong></div>
      <div class="detail-box"><div class="meta">Started At</div><strong>${escapeHTML(selected.started_at || "-")}</strong></div>
      <div class="detail-box"><div class="meta">Finished At</div><strong>${escapeHTML(selected.finished_at || "-")}</strong></div>
      <div class="detail-box"><div class="meta">Duration</div><strong>${escapeHTML(selected.duration_ms)} ms</strong></div>
      <div class="detail-box"><div class="meta">Timed Out</div><strong>${selected.timed_out ? "true" : "false"}</strong></div>
    </div>

    <div class="stack">
      <div>
        <h3>Args</h3>
      </div>
      <pre>${escapeHTML(JSON.stringify(selected.args || [], null, 2))}</pre>
    </div>

    <div class="stack">
      <div>
        <h3>Stdout${selected.stdout_truncated ? " (truncated)" : ""}</h3>
      </div>
      <pre>${escapeHTML(selected.stdout || "")}</pre>
    </div>

    <div class="stack">
      <div>
        <h3>Stderr${selected.stderr_truncated ? " (truncated)" : ""}</h3>
      </div>
      <pre>${escapeHTML(selected.stderr || "")}</pre>
    </div>
  `;
}

async function stopSession(sessionID, reopenDetail) {
  try {
    const result = await api(`/api/sessions/${sessionID}/stop`, { method: "POST", body: "{}" });
    sessionCreateOutput.textContent = JSON.stringify(result, null, 2);
    await refreshSessions(true);
    if (reopenDetail) {
      await openSessionDetail(sessionID);
      return;
    }
    state.sessionDetail = await api(`/api/sessions/${sessionID}`);
    renderSessionDetail();
    renderSessionTable();
  } catch (error) {
    sessionCreateOutput.textContent = error.message;
  }
}

function preferredEnvironmentName(fallbackName = "") {
  const enabledNames = new Set(state.createSession.enabledEnvironments.map((item) => item.name));
  if (fallbackName && enabledNames.has(fallbackName)) {
    return fallbackName;
  }
  const filterEnvironment = state.sessions.filters.environment_name.trim();
  if (filterEnvironment && enabledNames.has(filterEnvironment)) {
    return filterEnvironment;
  }
  if (state.createSession.selectedEnvironment && enabledNames.has(state.createSession.selectedEnvironment)) {
    return state.createSession.selectedEnvironment;
  }
  return state.createSession.enabledEnvironments[0]?.name || "";
}

function renderCreateEnvironmentOptions(preferredName = "") {
  const enabledEnvironments = state.createSession.enabledEnvironments;
  const selectedName = preferredEnvironmentName(preferredName);

  if (enabledEnvironments.length === 0) {
    createEnvironmentSelect.innerHTML = `<option value="">No enabled environments</option>`;
    createEnvironmentSelect.disabled = true;
    createSessionButton.disabled = true;
    createEnvironmentHint.textContent = "No enabled environments available. Create or enable one from the Environments page first.";
    state.createSession.selectedEnvironment = "";
    return;
  }

  createEnvironmentSelect.innerHTML = enabledEnvironments.map((item) => `
    <option value="${escapeHTML(item.name)}">${escapeHTML(item.name)} · ${escapeHTML(item.image_ref || "-")}</option>
  `).join("");
  createEnvironmentSelect.disabled = false;
  createSessionButton.disabled = false;
  createEnvironmentSelect.value = selectedName;
  createEnvironmentHint.textContent = `${enabledEnvironments.length} enabled environment(s) available.`;
  state.createSession.selectedEnvironment = selectedName;
}

async function refreshCreateEnvironments(preferredName = "") {
  const items = await api("/api/environments");
  state.createSession.enabledEnvironments = (items || []).filter((item) => item.enabled);
  renderCreateEnvironmentOptions(preferredName);
}

async function initialize() {
  initializeShell("sessions");
  bindModalDismiss();

  createEnvironmentSelect.addEventListener("change", () => {
    state.createSession.selectedEnvironment = createEnvironmentSelect.value;
  });

  document.getElementById("open-create-session").addEventListener("click", async () => {
    sessionCreateOutput.textContent = "No session created yet.";
    try {
      await refreshCreateEnvironments();
    } catch (error) {
      createEnvironmentHint.textContent = error.message;
      sessionCreateOutput.textContent = error.message;
    }
    openModal("create-session-backdrop");
  });

  document.getElementById("refresh-sessions").addEventListener("click", async () => {
    await refreshSessions(true);
  });

  document.getElementById("apply-session-filter").addEventListener("click", async () => {
    state.sessions.filters.status = document.getElementById("session-filter-status").value;
    state.sessions.filters.session_id = document.getElementById("session-filter-id").value.trim();
    state.sessions.filters.environment_name = document.getElementById("session-filter-environment").value.trim();
    state.sessions.pageSize = Number(document.getElementById("session-page-size").value) || 20;
    state.sessions.page = 1;
    await refreshSessions(false);
    renderCreateEnvironmentOptions();
  });

  document.getElementById("reset-session-filter").addEventListener("click", async () => {
    state.sessions.filters = { status: "active", session_id: "", environment_name: "" };
    state.sessions.page = 1;
    state.sessions.pageSize = 20;
    document.getElementById("session-filter-status").value = "active";
    document.getElementById("session-filter-id").value = "";
    document.getElementById("session-filter-environment").value = "";
    document.getElementById("session-page-size").value = "20";
    await refreshSessions(false);
    renderCreateEnvironmentOptions();
  });

  document.getElementById("session-prev").addEventListener("click", async () => {
    if (state.sessions.page > 1) {
      state.sessions.page -= 1;
      await refreshSessions(false);
    }
  });

  document.getElementById("session-next").addEventListener("click", async () => {
    const totalPages = Math.max(1, Math.ceil(Math.max(state.sessions.total, 1) / state.sessions.pageSize));
    if (state.sessions.page < totalPages) {
      state.sessions.page += 1;
      await refreshSessions(false);
    }
  });

  createSessionButton.addEventListener("click", async () => {
    const environmentName = createEnvironmentSelect.value.trim();
    if (!environmentName) {
      sessionCreateOutput.textContent = "Select an enabled environment before creating a session.";
      return;
    }

    try {
      const result = await api("/api/sessions/create", {
        method: "POST",
        body: JSON.stringify({
          environment_name: environmentName,
          session_id: document.getElementById("create-session-id").value.trim(),
        }),
      });
      state.sessions.selectedID = result.session_id;
      state.createSession.selectedEnvironment = result.environment_name;
      sessionCreateOutput.textContent = JSON.stringify(result, null, 2);
      closeModal("create-session-backdrop");
      state.sessions.filters.status = "active";
      document.getElementById("session-filter-status").value = "active";
      state.sessions.page = 1;
      await refreshSessions(true);
      await openSessionDetail(result.session_id);
    } catch (error) {
      sessionCreateOutput.textContent = error.message;
    }
  });

  document.getElementById("execution-prev").addEventListener("click", async () => {
    if (state.executions.page > 1) {
      state.executions.page -= 1;
      await refreshExecutions();
    }
  });

  document.getElementById("execution-next").addEventListener("click", async () => {
    const totalPages = Math.max(1, Math.ceil(Math.max(state.executions.total, 1) / state.executions.pageSize));
    if (state.executions.page < totalPages) {
      state.executions.page += 1;
      await refreshExecutions();
    }
  });

  await Promise.all([
    refreshSessions(false),
    refreshCreateEnvironments(),
  ]);
}

initialize().catch((error) => {
  createEnvironmentHint.textContent = error.message;
  sessionCreateOutput.textContent = error.message;
});

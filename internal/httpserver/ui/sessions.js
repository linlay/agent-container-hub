import {
  api,
  bindModalDismiss,
  closeModal,
  escapeHTML,
  formatTime,
  initializeShell,
  openModal,
  setLoading,
  showToast,
} from "/ui/common.js";

const DEFAULT_SESSION_STATUS_FILTER = "history";
const SESSION_STATUS_FILTER_OPTIONS = ["history", "active", "all"];

const state = {
  sessions: {
    filters: { status: DEFAULT_SESSION_STATUS_FILTER, session_id: "", environment_name: "" },
    page: 1,
    pageSize: 20,
    total: 0,
    items: [],
    selectedID: "",
  },
  environmentsByName: {},
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
    templateRoot: "",
    templateMounts: [],
    selectedTemplateMount: "",
    mounts: [],
  },
  quickExecute: {
    sessionID: "",
    environmentName: "",
    preset: null,
  },
};

const sessionTableBody = document.getElementById("session-table-body");
const sessionListMeta = document.getElementById("session-list-meta");
const sessionSelectionMeta = document.getElementById("session-selection-meta");
const sessionPageMeta = document.getElementById("session-page-meta");
const sessionDetailContent = document.getElementById("session-detail-content");
const sessionDetailFooter = document.getElementById("session-detail-footer");
const sessionCreateOutput = document.getElementById("session-create-output");
const createEnvironmentSelect = document.getElementById("create-environment");
const createEnvironmentHint = document.getElementById("create-environment-hint");
const createTemplateHint = document.getElementById("create-template-hint");
const createSessionButton = document.getElementById("create-session");
const createSessionMounts = document.getElementById("create-session-mounts");
const templateMountSelect = document.getElementById("template-mount-select");
const addTemplateMountButton = document.getElementById("add-template-mount");
const refreshSessionsButton = document.getElementById("refresh-sessions");
const executionHistory = document.getElementById("execution-history");
const executionDetail = document.getElementById("execution-detail");
const executionPageMeta = document.getElementById("execution-page-meta");
const executeModalMeta = document.getElementById("execute-modal-meta");
const quickExecuteContent = document.getElementById("quick-execute-content");
const quickExecuteOpenLogsButton = document.getElementById("quick-execute-open-logs");

const SESSION_ACTION_ICONS = {
  view: `
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path d="M2 12s4-6 10-6 10 6 10 6-4 6-10 6-10-6-10-6Z"></path>
      <circle cx="12" cy="12" r="3"></circle>
    </svg>
  `,
  "quick-execute": `
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path d="M8 7v10l8-5Z" fill="currentColor" stroke="none"></path>
    </svg>
  `,
  executions: `
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path d="M8 4h7l5 5v10a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z"></path>
      <path d="M15 4v5h5"></path>
      <path d="M9 13h6"></path>
      <path d="M9 17h6"></path>
    </svg>
  `,
  stop: `
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <rect x="7" y="7" width="10" height="10" rx="2"></rect>
    </svg>
  `,
};

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

function cloneMount(mount, origin = "manual") {
  return {
    source: mount?.source || "",
    destination: mount?.destination || "",
    read_only: Boolean(mount?.read_only),
    origin,
  };
}

function templateMountKey(mount) {
  return `${mount?.source || ""}::${mount?.destination || ""}`;
}

function availableTemplateMounts() {
  const used = new Set(
    state.createSession.mounts
      .filter((mount) => mount.origin === "template")
      .map((mount) => templateMountKey(mount)),
  );
  return state.createSession.templateMounts.filter((mount) => !used.has(templateMountKey(mount)));
}

function renderTemplateMountOptions() {
  const mounts = availableTemplateMounts();
  const selected = mounts.find((mount) => templateMountKey(mount) === state.createSession.selectedTemplateMount)
    ? state.createSession.selectedTemplateMount
    : (mounts[0] ? templateMountKey(mounts[0]) : "");

  state.createSession.selectedTemplateMount = selected || "";
  if (mounts.length === 0) {
    templateMountSelect.innerHTML = `<option value="">No template mounts available</option>`;
    templateMountSelect.disabled = true;
    addTemplateMountButton.disabled = true;
    return;
  }

  templateMountSelect.innerHTML = mounts.map((mount) => {
    const key = templateMountKey(mount);
    return `<option value="${escapeHTML(key)}">${escapeHTML(`${mount.destination} ← ${mount.source}`)}</option>`;
  }).join("");
  templateMountSelect.disabled = false;
  addTemplateMountButton.disabled = false;
  templateMountSelect.value = state.createSession.selectedTemplateMount;
}

function quickExecutePresetForEnvironment(environmentName) {
  return state.environmentsByName[environmentName]?.default_execute || {};
}

function renderSessionActionButton({
  action,
  label,
  sessionID,
  variant = "secondary",
  disabled = false,
  environmentName = "",
}) {
  return `
    <button
      class="${variant} icon-button"
      type="button"
      data-action="${escapeHTML(action)}"
      data-session-id="${escapeHTML(sessionID)}"
      ${environmentName ? `data-environment-name="${escapeHTML(environmentName)}"` : ""}
      title="${escapeHTML(label)}"
      aria-label="${escapeHTML(label)}"
      ${disabled ? "disabled" : ""}
    >
      <span class="icon" aria-hidden="true">${SESSION_ACTION_ICONS[action] || ""}</span>
      <span class="sr-only">${escapeHTML(label)}</span>
    </button>
  `;
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

async function refreshEnvironmentMetadata(preferredName = "") {
  const items = await api("/api/environments");
  state.environmentsByName = Object.fromEntries((items || []).map((item) => [item.name, item]));
  state.createSession.enabledEnvironments = (items || []).filter((item) => item.enabled);
  renderCreateEnvironmentOptions(preferredName);
  renderSessionTable();
}

async function refreshSessionCreateTemplate() {
  const data = await api("/api/session-create/template");
  state.createSession.templateRoot = data.mount_template_root || "";
  state.createSession.templateMounts = (data.default_mounts || []).map((mount) => cloneMount(mount, "template"));
  state.createSession.selectedTemplateMount = state.createSession.templateMounts[0]
    ? templateMountKey(state.createSession.templateMounts[0])
    : "";
  renderTemplateMountOptions();
  createTemplateHint.textContent = state.createSession.templateRoot
    ? `Template root: ${state.createSession.templateRoot}`
    : "No mount template root configured.";
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

function resetCreateSessionForm() {
  state.createSession.selectedEnvironment = preferredEnvironmentName();
  state.createSession.selectedTemplateMount = state.createSession.templateMounts[0]
    ? templateMountKey(state.createSession.templateMounts[0])
    : "";
  state.createSession.mounts = [];
  createEnvironmentSelect.value = state.createSession.selectedEnvironment || "";
  document.getElementById("create-session-id").value = "";
  renderSessionMountEditor();
}

function addMountRow(origin = "manual") {
  state.createSession.mounts.push({
    source: "",
    destination: "",
    read_only: false,
    origin,
  });
  renderSessionMountEditor();
}

function addSelectedTemplateMount() {
  const mount = state.createSession.templateMounts.find((item) => templateMountKey(item) === state.createSession.selectedTemplateMount);
  if (!mount) {
    showToast("Select a template mount to add.", "error");
    return;
  }
  state.createSession.mounts.push({ ...mount });
  renderSessionMountEditor();
}

function renderSessionMountEditor() {
  renderTemplateMountOptions();
  if (state.createSession.mounts.length === 0) {
    createSessionMounts.innerHTML = `<div class="empty">No session mounts yet. Use "Add Template Mount" or "Add Mount" to create one.</div>`;
    return;
  }

  createSessionMounts.innerHTML = state.createSession.mounts.map((mount, index) => `
    <div class="mount-editor" data-origin="${escapeHTML(mount.origin)}">
      <div class="mount-editor-head">
        <span class="pill">${escapeHTML(mount.origin)}</span>
        <button class="secondary" data-remove-mount="${index}">Remove</button>
      </div>
      <div class="row">
        <label>Source
          <input data-mount-field="source" data-mount-index="${index}" value="${escapeHTML(mount.source)}" placeholder="/host/path">
        </label>
        <label>Destination
          <input data-mount-field="destination" data-mount-index="${index}" value="${escapeHTML(mount.destination)}" placeholder="/container/path">
        </label>
      </div>
      <label class="full-width">Access
        <select data-mount-field="read_only" data-mount-index="${index}">
          <option value="false" ${mount.read_only ? "" : "selected"}>Read write</option>
          <option value="true" ${mount.read_only ? "selected" : ""}>Read only</option>
        </select>
      </label>
    </div>
  `).join("");

  createSessionMounts.querySelectorAll("[data-mount-field]").forEach((node) => {
    node.addEventListener("input", () => {
      const index = Number(node.dataset.mountIndex);
      const field = node.dataset.mountField;
      if (!state.createSession.mounts[index]) {
        return;
      }
      if (field === "read_only") {
        state.createSession.mounts[index].read_only = node.value === "true";
        return;
      }
      state.createSession.mounts[index][field] = node.value;
    });
    node.addEventListener("change", () => {
      if (node.dataset.mountField === "read_only") {
        const index = Number(node.dataset.mountIndex);
        if (state.createSession.mounts[index]) {
          state.createSession.mounts[index].read_only = node.value === "true";
        }
      }
    });
  });

  createSessionMounts.querySelectorAll("[data-remove-mount]").forEach((button) => {
    button.addEventListener("click", () => {
      const index = Number(button.dataset.removeMount);
      state.createSession.mounts.splice(index, 1);
      renderSessionMountEditor();
    });
  });
}

function collectCreateMountPayload() {
  return state.createSession.mounts.map((mount) => ({
    source: mount.source.trim(),
    destination: mount.destination.trim(),
    read_only: Boolean(mount.read_only),
  }));
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

  sessionTableBody.innerHTML = state.sessions.items.map((item) => {
    const quickPreset = quickExecutePresetForEnvironment(item.environment_name);
    const canQuickExecute = Boolean((quickPreset.command || "").trim());
    const actionButtons = [
      renderSessionActionButton({
        action: "view",
        label: "View session details",
        sessionID: item.session_id,
        variant: "ghost",
      }),
      renderSessionActionButton({
        action: "quick-execute",
        label: canQuickExecute
          ? "Quick execute preset command"
          : "Quick execute unavailable for this environment",
        sessionID: item.session_id,
        environmentName: item.environment_name,
        disabled: !canQuickExecute,
      }),
      renderSessionActionButton({
        action: "executions",
        label: "View execute logs",
        sessionID: item.session_id,
      }),
    ];
    if (item.status === "active") {
      actionButtons.push(renderSessionActionButton({
        action: "stop",
        label: "Stop session",
        sessionID: item.session_id,
        variant: "danger",
      }));
    }
    return `
      <tr class="${state.sessions.selectedID === item.session_id ? "selected" : ""}">
        <td>
          <strong>${escapeHTML(item.session_id)}</strong>
          <div class="cell-meta">${escapeHTML(item.container_id || "").substring(0, 32)}</div>
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
        <td class="actions-cell">
          <div class="actions table-actions">
            ${actionButtons.join("")}
          </div>
        </td>
      </tr>
    `;
  }).join("");

  sessionTableBody.querySelectorAll("[data-action]").forEach((button) => {
    button.addEventListener("click", async (event) => {
      event.stopPropagation();
      const sessionID = button.dataset.sessionId;
      const action = button.dataset.action;
      if (action === "view") {
        await openSessionDetail(sessionID);
        return;
      }
      if (action === "quick-execute") {
        await quickExecuteSession(sessionID, button.dataset.environmentName, button);
        return;
      }
      if (action === "executions") {
        await openExecuteLogs(sessionID);
        return;
      }
      if (action === "stop") {
        await stopSession(sessionID, true, button);
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
    sessionDetailFooter.innerHTML = `<button class="secondary" disabled>View Execute</button>`;
    return;
  }

  const mounts = (item.mounts || []).map((mount) => {
    const kind = mount.destination === "/workspace" && mount.source === item.workspace_path
      ? "workspace mount"
      : "snapshot mount";
    return `
      <div class="mount-item">
        <div class="toolbar">
          <strong>${escapeHTML(mount.destination || "-")}</strong>
          <span class="pill">${escapeHTML(kind)}</span>
        </div>
        <div class="meta">${escapeHTML(mount.source || "-")}</div>
        <div class="meta">${mount.read_only ? "read only" : "read write"}</div>
      </div>
    `;
  }).join("") || `<div class="empty">No mounts recorded for this session.</div>`;

  sessionDetailContent.innerHTML = `
    <div>
      <h3>${escapeHTML(item.session_id)}</h3>
      <div class="meta">${escapeHTML(item.environment_name)} · ${escapeHTML(item.image || "-")}</div>
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
        <div class="meta">Shows the persisted session mount snapshot, including configured mounts and the auto workspace mount.</div>
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

  sessionDetailFooter.innerHTML = `
    <button class="secondary" id="detail-open-executions">View Execute</button>
    ${item.status === "active" ? `<button class="danger" id="detail-stop-session">Stop Session</button>` : ""}
  `;

  document.getElementById("detail-open-executions").addEventListener("click", async () => {
    await openExecuteLogs(item.session_id);
  });

  const stopButton = document.getElementById("detail-stop-session");
  if (stopButton) {
    stopButton.addEventListener("click", async () => {
      await stopSession(item.session_id, false, stopButton);
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

function renderQuickExecuteResult(payload) {
  quickExecuteOpenLogsButton.disabled = !state.quickExecute.sessionID;
  if (payload.error) {
    quickExecuteContent.innerHTML = `<div class="empty">${escapeHTML(payload.error)}</div>`;
    return;
  }
  quickExecuteContent.innerHTML = `
    <div class="detail-grid">
      <div class="detail-box"><div class="meta">Session</div><strong>${escapeHTML(payload.sessionID)}</strong></div>
      <div class="detail-box"><div class="meta">Environment</div><strong>${escapeHTML(payload.environmentName)}</strong></div>
      <div class="detail-box"><div class="meta">Command</div><strong>${escapeHTML(payload.preset.command || "-")}</strong></div>
      <div class="detail-box"><div class="meta">Cwd</div><strong>${escapeHTML(payload.preset.cwd || "(session default)")}</strong></div>
      <div class="detail-box"><div class="meta">Timeout</div><strong>${escapeHTML(payload.preset.timeout_ms || 0)} ms</strong></div>
      <div class="detail-box"><div class="meta">Exit Code</div><strong>${escapeHTML(payload.response.exit_code)}</strong></div>
      <div class="detail-box"><div class="meta">Started At</div><strong>${escapeHTML(payload.response.started_at || "-")}</strong></div>
      <div class="detail-box"><div class="meta">Duration</div><strong>${escapeHTML(payload.response.duration_ms)} ms</strong></div>
    </div>

    <div class="stack">
      <div>
        <h3>Args</h3>
      </div>
      <pre>${escapeHTML(JSON.stringify(payload.preset.args || [], null, 2))}</pre>
    </div>

    <div class="stack">
      <div>
        <h3>Stdout</h3>
      </div>
      <pre>${escapeHTML(payload.response.stdout || "")}</pre>
    </div>

    <div class="stack">
      <div>
        <h3>Stderr</h3>
      </div>
      <pre>${escapeHTML(payload.response.stderr || "")}</pre>
    </div>
  `;
}

async function quickExecuteSession(sessionID, environmentName, button) {
  const preset = quickExecutePresetForEnvironment(environmentName);
  state.quickExecute.sessionID = sessionID;
  state.quickExecute.environmentName = environmentName;
  state.quickExecute.preset = preset;
  quickExecuteOpenLogsButton.disabled = false;

  if (!(preset.command || "").trim()) {
    renderQuickExecuteResult({ error: "No quick execute preset is configured for this environment." });
    showToast("Quick execute preset is not configured for this environment.", "error");
    openModal("quick-execute-backdrop");
    return;
  }

  setLoading(button, true);
  try {
    const response = await api(`/api/sessions/${sessionID}/execute`, {
      method: "POST",
      body: JSON.stringify({
        command: preset.command,
        args: preset.args || [],
        cwd: preset.cwd || "",
        timeout_ms: preset.timeout_ms || 0,
      }),
    });
    renderQuickExecuteResult({ sessionID, environmentName, preset, response });
  } catch (error) {
    renderQuickExecuteResult({ error: error.message });
    showToast(error.message, "error");
  } finally {
    setLoading(button, false);
  }
  openModal("quick-execute-backdrop");
}

async function stopSession(sessionID, reopenDetail, button) {
  setLoading(button, true);
  try {
    await api(`/api/sessions/${sessionID}/stop`, { method: "POST", body: "{}" });
    showToast(`Session ${sessionID} stopped.`, "success");
    await refreshSessions(true);
    if (reopenDetail) {
      await openSessionDetail(sessionID);
      return;
    }
    state.sessionDetail = await api(`/api/sessions/${sessionID}`);
    renderSessionDetail();
    renderSessionTable();
  } catch (error) {
    showToast(error.message, "error");
    if (!reopenDetail) {
      sessionCreateOutput.textContent = error.message;
    }
  } finally {
    setLoading(button, false);
  }
}

function renderSessionStatusFilterTags() {
  const statusFilter = document.getElementById("session-filter-status");
  if (!statusFilter) {
    return;
  }

  statusFilter.querySelectorAll("[data-status-filter]").forEach((button) => {
    const isSelected = button.dataset.statusFilter === state.sessions.filters.status;
    button.classList.toggle("is-selected", isSelected);
    button.setAttribute("aria-pressed", String(isSelected));
  });
}

async function initialize() {
  initializeShell("sessions");
  bindModalDismiss();
  renderSessionStatusFilterTags();

  document.getElementById("session-filter-status").addEventListener("click", (event) => {
    const button = event.target.closest("[data-status-filter]");
    if (!button) {
      return;
    }

    const nextStatus = button.dataset.statusFilter;
    if (!SESSION_STATUS_FILTER_OPTIONS.includes(nextStatus)) {
      return;
    }

    state.sessions.filters.status = nextStatus;
    renderSessionStatusFilterTags();
  });

  createEnvironmentSelect.addEventListener("change", () => {
    state.createSession.selectedEnvironment = createEnvironmentSelect.value;
  });

  document.getElementById("add-mount-row").addEventListener("click", (event) => {
    event.preventDefault();
    addMountRow();
  });

  templateMountSelect.addEventListener("change", () => {
    state.createSession.selectedTemplateMount = templateMountSelect.value;
  });

  addTemplateMountButton.addEventListener("click", (event) => {
    event.preventDefault();
    addSelectedTemplateMount();
  });

  document.getElementById("open-create-session").addEventListener("click", async () => {
    sessionCreateOutput.textContent = "No session created yet.";
    try {
      await refreshEnvironmentMetadata();
      await refreshSessionCreateTemplate();
      resetCreateSessionForm();
    } catch (error) {
      createTemplateHint.textContent = error.message;
      sessionCreateOutput.textContent = error.message;
      showToast(error.message, "error");
      if (state.createSession.mounts.length === 0) {
        renderSessionMountEditor();
      }
    }
    openModal("create-session-backdrop");
  });

  refreshSessionsButton.addEventListener("click", async () => {
    setLoading(refreshSessionsButton, true);
    try {
      await Promise.all([
        refreshSessions(true),
        refreshEnvironmentMetadata(),
      ]);
      showToast("Sessions refreshed.", "success");
    } catch (error) {
      sessionCreateOutput.textContent = error.message;
      showToast(error.message, "error");
    } finally {
      setLoading(refreshSessionsButton, false);
    }
  });

  document.getElementById("apply-session-filter").addEventListener("click", async () => {
    state.sessions.filters.session_id = document.getElementById("session-filter-id").value.trim();
    state.sessions.filters.environment_name = document.getElementById("session-filter-environment").value.trim();
    state.sessions.pageSize = Number(document.getElementById("session-page-size").value) || 20;
    state.sessions.page = 1;
    await refreshSessions(false);
    renderCreateEnvironmentOptions();
  });

  document.getElementById("reset-session-filter").addEventListener("click", async () => {
    state.sessions.filters = { status: DEFAULT_SESSION_STATUS_FILTER, session_id: "", environment_name: "" };
    state.sessions.page = 1;
    state.sessions.pageSize = 20;
    renderSessionStatusFilterTags();
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
      showToast("Select an enabled environment before creating a session.", "error");
      return;
    }

    setLoading(createSessionButton, true);
    try {
      const result = await api("/api/sessions/create", {
        method: "POST",
        body: JSON.stringify({
          environment_name: environmentName,
          session_id: document.getElementById("create-session-id").value.trim(),
          mounts: collectCreateMountPayload(),
        }),
      });
      state.sessions.selectedID = result.session_id;
      state.createSession.selectedEnvironment = result.environment_name;
      sessionCreateOutput.textContent = JSON.stringify(result, null, 2);
      closeModal("create-session-backdrop");
      showToast(`Session ${result.session_id} created.`, "success");
      state.sessions.page = 1;
      await refreshSessions(true);
      await openSessionDetail(result.session_id);
    } catch (error) {
      sessionCreateOutput.textContent = error.message;
      showToast(error.message, "error");
    } finally {
      setLoading(createSessionButton, false);
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

  quickExecuteOpenLogsButton.addEventListener("click", async () => {
    if (!state.quickExecute.sessionID) {
      return;
    }
    closeModal("quick-execute-backdrop");
    await openExecuteLogs(state.quickExecute.sessionID);
  });

  await Promise.all([
    refreshSessions(false),
    refreshEnvironmentMetadata(),
  ]);
  renderSessionDetail();
  renderChatOptions();
}

initialize().catch((error) => {
  createEnvironmentHint.textContent = error.message;
  createTemplateHint.textContent = error.message;
  sessionCreateOutput.textContent = error.message;
  showToast(error.message, "error");
});

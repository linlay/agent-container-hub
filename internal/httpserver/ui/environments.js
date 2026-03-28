import {
  api,
  bindModalDismiss,
  closeModal,
  escapeHTML,
  initializeShell,
  openModal,
  setLoading,
  showToast,
} from "/ui/common.js";

const ACTIVE_BUILD_STATUSES = new Set(["building", "smoke_checking"]);
const BUILD_TARGET_DETAILS = {
  build: {
    label: "build",
    description: "Standard image build using the environment Makefile.",
  },
  "build-cn": {
    label: "build-cn",
    description: "CN-friendly image build using mirror-oriented settings.",
  },
};

const state = {
  environments: {
    items: [],
    selectedName: "",
    selectedBuild: defaultBuild(),
    selectedDetails: defaultEnvironmentDetails(),
    selectedAvailable: false,
    selectedDefaultExecute: defaultExecutePreset(),
    selectedAgentPrompt: "",
    selectedAvailableBuildTargets: [],
    selectedLastBuild: null,
  },
  files: {
    items: [],
    selectedPath: "",
    selectedContent: "",
  },
  buildProgress: {
    job: null,
    log: "",
    eventSource: null,
  },
};

const environmentList = document.getElementById("environment-list");
const environmentOutput = document.getElementById("environment-output");
const environmentYAML = document.getElementById("environment-yaml");
const saveButton = document.getElementById("save");
const buildButton = document.getElementById("build");
const refreshFilesButton = document.getElementById("refresh-files");
const newFileButton = document.getElementById("new-file");
const saveFileButton = document.getElementById("save-file");
const environmentFileList = document.getElementById("environment-file-list");
const environmentFilePath = document.getElementById("environment-file-path");
const environmentFileContent = document.getElementById("environment-file-content");
const buildTargetBackdrop = document.getElementById("build-target-backdrop");
const buildTargetMeta = document.getElementById("build-target-meta");
const buildTargetOptions = document.getElementById("build-target-options");
const buildTargetConfirm = document.getElementById("build-target-confirm");
const buildProgressBackdrop = document.getElementById("build-progress-backdrop");
const buildProgressSummary = document.getElementById("build-progress-summary");
const buildProgressMeta = document.getElementById("build-progress-meta");
const buildProgressLog = document.getElementById("build-progress-log");
const buildProgressRefresh = document.getElementById("build-progress-refresh");
const buildProgressAutoscroll = document.getElementById("build-progress-autoscroll");

function clearEnvironmentForm() {
  document.getElementById("name").value = "";
  document.getElementById("repository").value = "";
  document.getElementById("tag").value = "";
  document.getElementById("cwd").value = "/workspace";
  document.getElementById("description").value = "";
  document.getElementById("default-execute-command").value = "";
  document.getElementById("default-execute-cwd").value = "";
  document.getElementById("default-execute-timeout").value = "";
  document.getElementById("default-execute-args").value = "";
  state.environments.selectedBuild = defaultBuild();
  state.environments.selectedDetails = defaultEnvironmentDetails();
  state.environments.selectedAvailable = false;
  state.environments.selectedDefaultExecute = defaultExecutePreset();
  state.environments.selectedAgentPrompt = "";
  state.environments.selectedAvailableBuildTargets = [];
  state.environments.selectedLastBuild = null;
  state.files.items = [];
  state.files.selectedPath = "";
  state.files.selectedContent = "";
  environmentFilePath.value = "";
  environmentFileContent.value = "";
  renderEnvironmentFiles();
  updateBuildButton();
}

function defaultBuild() {
  return {
    dockerfile: "",
    build_args: {},
    notes: "",
    smoke_command: "",
    smoke_args: [],
  };
}

function defaultBuildJob() {
  return {
    id: "",
    environment_name: "",
    image_ref: "",
    target: "",
    status: "",
    output: "",
    error: "",
    started_at: "",
    finished_at: "",
  };
}

function defaultEnvironmentDetails() {
  return {
    default_env: {},
    mounts: [],
    resources: { cpu: 0, memory_mb: 0, pids: 0 },
    enabled: true,
  };
}

function defaultExecutePreset() {
  return {
    command: "",
    args: [],
    cwd: "",
    timeout_ms: 0,
  };
}

function normalizeBuild(build) {
  return {
    ...defaultBuild(),
    ...(build || {}),
    build_args: { ...(build?.build_args || {}) },
    smoke_args: Array.isArray(build?.smoke_args) ? [...build.smoke_args] : [],
  };
}

function normalizeBuildJob(job) {
  if (!job) {
    return null;
  }
  return {
    ...defaultBuildJob(),
    ...job,
  };
}

function normalizeEnvironmentDetails(item) {
  return {
    ...defaultEnvironmentDetails(),
    default_env: { ...(item?.default_env || {}) },
    mounts: Array.isArray(item?.mounts) ? item.mounts.map((mount) => ({ ...mount })) : [],
    resources: { ...defaultEnvironmentDetails().resources, ...(item?.resources || {}) },
    enabled: item?.enabled ?? true,
  };
}

function normalizeExecutePreset(preset) {
  return {
    ...defaultExecutePreset(),
    ...(preset || {}),
    args: Array.isArray(preset?.args) ? [...preset.args] : [],
  };
}

function formatAvailabilityLabel(item) {
  return item?.available ? "available" : "missing image";
}

function availabilityClass(item) {
  return item?.available ? "" : "stopped";
}

function environmentStatusSummary(item) {
  const enabledLabel = item?.enabled ? "enabled" : "disabled";
  const availabilityLabel = item?.available ? "image available locally" : "image missing locally";
  if (item?.last_build) {
    return `Last build ${formatBuildStatus(item.last_build.status)}${buildTargetSuffix(item.last_build.target)} at ${item.last_build.started_at || "-"} · ${enabledLabel} · ${availabilityLabel}.`;
  }
  return `Environment loaded. ${enabledLabel}. ${availabilityLabel}.`;
}

function normalizeBuildTargets(targets) {
  return Array.isArray(targets) ? targets.map((item) => String(item || "").trim()).filter(Boolean) : [];
}

function formatBuildStatus(status) {
  if (!status) {
    return "unknown";
  }
  return status.replaceAll("_", " ");
}

function formatBuildTarget(target) {
  const value = String(target || "").trim();
  if (!value) {
    return "";
  }
  return BUILD_TARGET_DETAILS[value]?.label || value;
}

function buildTargetDescription(target) {
  const value = String(target || "").trim();
  if (!value) {
    return "Managed Docker build using the saved environment Dockerfile.";
  }
  return BUILD_TARGET_DETAILS[value]?.description || "Build using the selected Makefile target.";
}

function buildTargetSuffix(target) {
  const label = formatBuildTarget(target);
  return label ? ` via ${label}` : "";
}

function isBuildActive(status) {
  return ACTIVE_BUILD_STATUSES.has(String(status || "").trim());
}

function buildStatusClass(status) {
  return status === "failed" ? "stopped" : "building";
}

async function refreshEnvironmentFiles(name = state.environments.selectedName, preferredPath = "") {
  if (!name) {
    state.files.items = [];
    state.files.selectedPath = "";
    state.files.selectedContent = "";
    renderEnvironmentFiles();
    return;
  }
  const items = await api(`/api/environments/${name}/files`);
  state.files.items = items || [];
  if (preferredPath && state.files.items.find((item) => item.path === preferredPath)) {
    state.files.selectedPath = preferredPath;
  } else if (!state.files.items.find((item) => item.path === state.files.selectedPath)) {
    state.files.selectedPath = state.files.items[0]?.path || "";
  }
  renderEnvironmentFiles();
  if (state.files.selectedPath) {
    await loadEnvironmentFile(state.files.selectedPath);
    return;
  }
  state.files.selectedContent = "";
  environmentFileContent.value = "";
}

async function loadEnvironmentFile(path) {
  const name = state.environments.selectedName;
  if (!name || !path) {
    return;
  }
  const file = await api(`/api/environments/${name}/files/${encodeURIComponent(path).replaceAll("%2F", "/")}`);
  state.files.selectedPath = file.path;
  state.files.selectedContent = file.content || "";
  environmentFilePath.value = state.files.selectedPath;
  environmentFileContent.value = state.files.selectedContent;
  renderEnvironmentFiles();
}

function renderEnvironmentFiles() {
  const selectedPath = state.files.selectedPath;
  environmentFileList.innerHTML = state.files.items.map((file) => `
    <div class="list-item ${selectedPath === file.path ? "active" : ""}" data-file-path="${escapeHTML(file.path)}">
      <strong>${escapeHTML(file.path)}</strong>
      <div class="meta">${escapeHTML(file.type || "other")}</div>
    </div>
  `).join("") || `<div class="empty">No editable files yet.</div>`;

  environmentFileList.querySelectorAll("[data-file-path]").forEach((node) => {
    node.addEventListener("click", async () => {
      await loadEnvironmentFile(node.dataset.filePath);
    });
  });

  environmentFilePath.value = state.files.selectedPath || "";
  saveFileButton.disabled = !state.environments.selectedName;
}

async function refreshEnvironments(selectedName = "") {
  const items = await api("/api/environments");
  state.environments.items = items || [];
  if (selectedName) {
    state.environments.selectedName = selectedName;
  } else if (!state.environments.selectedName && state.environments.items.length > 0) {
    state.environments.selectedName = state.environments.items[0].name;
  } else if (state.environments.selectedName && !state.environments.items.find((item) => item.name === state.environments.selectedName)) {
    state.environments.selectedName = state.environments.items[0]?.name || "";
  }
  renderEnvironments();
  if (state.environments.selectedName) {
    await selectEnvironment(state.environments.selectedName);
    return;
  }
  clearEnvironmentForm();
  environmentOutput.textContent = "Select or create an environment to begin.";
  environmentYAML.textContent = "No environment selected.";
}

function renderEnvironments() {
  environmentList.innerHTML = state.environments.items.map((item) => `
    <div class="list-item ${state.environments.selectedName === item.name ? "active" : ""}" data-environment-name="${escapeHTML(item.name)}">
      <div class="toolbar">
        <strong>${escapeHTML(item.name)}</strong>
        <div class="actions">
          <span class="pill ${item.enabled ? "" : "stopped"}">${item.enabled ? "enabled" : "disabled"}</span>
          <span class="pill ${availabilityClass(item)}">${escapeHTML(formatAvailabilityLabel(item))}</span>
        </div>
      </div>
      <div class="meta">${escapeHTML(item.image_ref || "-")}</div>
      <div class="meta">${item.last_build ? `last build: ${escapeHTML(formatBuildStatus(item.last_build.status))}` : "no build yet"}</div>
      <div class="meta">${item.available ? "image available locally" : "image missing locally"}</div>
    </div>
  `).join("") || `<div class="empty">No environments yet.</div>`;

  environmentList.querySelectorAll("[data-environment-name]").forEach((node) => {
    node.addEventListener("click", async () => {
      await selectEnvironment(node.dataset.environmentName);
    });
  });
}

function updateBuildButton() {
  const activeBuild = state.environments.selectedLastBuild;
  buildButton.textContent = activeBuild && isBuildActive(activeBuild.status) ? "View Build Progress" : "Build Image";
}

async function selectEnvironment(name) {
  const item = await api(`/api/environments/${name}`);
  state.environments.selectedName = item.name;
  document.getElementById("name").value = item.name || "";
  document.getElementById("repository").value = item.image_repository || "";
  document.getElementById("tag").value = item.image_tag || "";
  document.getElementById("cwd").value = item.default_cwd || "/workspace";
  document.getElementById("description").value = item.description || "";
  state.environments.selectedBuild = normalizeBuild(item.build);
  state.environments.selectedDetails = normalizeEnvironmentDetails(item);
  state.environments.selectedAvailable = Boolean(item.available);
  state.environments.selectedDefaultExecute = normalizeExecutePreset(item.default_execute);
  state.environments.selectedAgentPrompt = item.agent_prompt || "";
  state.environments.selectedLastBuild = normalizeBuildJob(item.last_build);
  document.getElementById("default-execute-command").value = state.environments.selectedDefaultExecute.command || "";
  document.getElementById("default-execute-cwd").value = state.environments.selectedDefaultExecute.cwd || "";
  document.getElementById("default-execute-timeout").value = state.environments.selectedDefaultExecute.timeout_ms || "";
  document.getElementById("default-execute-args").value = state.environments.selectedDefaultExecute.args.join("\n");
  state.environments.selectedAvailableBuildTargets = normalizeBuildTargets(item.available_build_targets);
  environmentOutput.textContent = environmentStatusSummary(item);
  environmentYAML.textContent = item.yaml || "No YAML available.";
  updateBuildButton();
  await refreshEnvironmentFiles(item.name, state.files.selectedPath || "environment.yml");
  renderEnvironments();
}

function collectEnvironmentPayload() {
  const build = {
    ...normalizeBuild(state.environments.selectedBuild),
    dockerfile: "",
  };
  const defaultExecute = normalizeExecutePreset({
    command: document.getElementById("default-execute-command").value.trim(),
    cwd: document.getElementById("default-execute-cwd").value.trim(),
    timeout_ms: Number(document.getElementById("default-execute-timeout").value) || 0,
    args: document.getElementById("default-execute-args").value
      .split("\n")
      .map((item) => item.trim())
      .filter(Boolean),
  });
  return {
    name: document.getElementById("name").value.trim(),
    image_repository: document.getElementById("repository").value.trim(),
    image_tag: document.getElementById("tag").value.trim(),
    default_cwd: document.getElementById("cwd").value.trim(),
    description: document.getElementById("description").value.trim(),
    default_env: { ...state.environments.selectedDetails.default_env },
    agent_prompt: state.environments.selectedAgentPrompt,
    mounts: state.environments.selectedDetails.mounts.map((mount) => ({ ...mount })),
    resources: { ...state.environments.selectedDetails.resources },
    enabled: state.environments.selectedDetails.enabled,
    default_execute: defaultExecute,
    build,
  };
}

function syncBuildReference(job) {
  if (!job) {
    return;
  }
  state.environments.items = state.environments.items.map((item) => (
    item.name === job.environment_name
      ? { ...item, last_build: normalizeBuildJob(job), available: job.status === "succeeded" ? true : item.available }
      : item
  ));
  if (state.environments.selectedName === job.environment_name) {
    state.environments.selectedLastBuild = normalizeBuildJob(job);
    if (job.status === "succeeded") {
      state.environments.selectedAvailable = true;
    }
    environmentOutput.textContent = `Build ${formatBuildStatus(job.status)}${buildTargetSuffix(job.target)} for ${job.environment_name} (${job.image_ref}).`;
    updateBuildButton();
  }
  renderEnvironments();
}

function renderBuildTargetOptions(targets = state.environments.selectedAvailableBuildTargets) {
  const availableTargets = normalizeBuildTargets(targets);
  if (availableTargets.length === 0) {
    buildTargetMeta.textContent = "No Makefile build target was detected. This will use the default managed Docker build.";
    buildTargetOptions.innerHTML = `
      <div class="build-target-option readonly">
        <strong>default build</strong>
        <div class="meta">${escapeHTML(buildTargetDescription(""))}</div>
      </div>
    `;
    return;
  }

  buildTargetMeta.textContent = "Select which Makefile target should be used for this build.";
  const defaultTarget = availableTargets.includes("build") ? "build" : availableTargets[0];
  buildTargetOptions.innerHTML = availableTargets.map((target) => `
    <label class="build-target-option" for="build-target-${escapeHTML(target)}">
      <input id="build-target-${escapeHTML(target)}" type="radio" name="build-target" value="${escapeHTML(target)}" ${target === defaultTarget ? "checked" : ""}>
      <div class="build-target-copy">
        <strong>${escapeHTML(formatBuildTarget(target))}</strong>
        <div class="meta">${escapeHTML(buildTargetDescription(target))}</div>
      </div>
    </label>
  `).join("");
}

function selectedBuildTarget() {
  const selected = document.querySelector('input[name="build-target"]:checked');
  return selected ? String(selected.value || "").trim() : "";
}

function renderBuildProgress() {
  const job = state.buildProgress.job;
  if (!job) {
    buildProgressSummary.innerHTML = `
      <div class="detail-box">
        <div class="meta">Environment</div>
        <strong>-</strong>
      </div>
    `;
    buildProgressMeta.textContent = "No build selected.";
    buildProgressLog.textContent = "Start a build to inspect live output.";
    return;
  }

  buildProgressSummary.innerHTML = `
    <div class="detail-box">
      <div class="meta">Environment</div>
      <strong>${escapeHTML(job.environment_name || "-")}</strong>
    </div>
    <div class="detail-box">
      <div class="meta">Image</div>
      <strong>${escapeHTML(job.image_ref || "-")}</strong>
    </div>
    <div class="detail-box">
      <div class="meta">Target</div>
      <strong>${escapeHTML(formatBuildTarget(job.target) || "default")}</strong>
    </div>
    <div class="detail-box">
      <div class="meta">Status</div>
      <span class="pill ${buildStatusClass(job.status)}">${escapeHTML(formatBuildStatus(job.status || "unknown"))}</span>
    </div>
    <div class="detail-box">
      <div class="meta">Started</div>
      <strong>${escapeHTML(job.started_at || "-")}</strong>
    </div>
  `;

  const stateLabel = isBuildActive(job.status)
    ? "Streaming build output."
    : (job.error ? `Build finished with error: ${job.error}` : "Build finished.");
  buildProgressMeta.textContent = `${stateLabel} Job ${job.id || "-"}${job.finished_at ? ` · finished ${job.finished_at}` : ""}`;
  buildProgressLog.textContent = state.buildProgress.log || "Waiting for build output...";
  if (buildProgressAutoscroll.checked) {
    buildProgressLog.scrollTop = buildProgressLog.scrollHeight;
  }
}

function disconnectBuildStream() {
  if (state.buildProgress.eventSource) {
    state.buildProgress.eventSource.close();
    state.buildProgress.eventSource = null;
  }
}

function applyBuildSnapshot(job) {
  const normalized = normalizeBuildJob(job);
  if (!normalized) {
    return;
  }
  state.buildProgress.job = normalized;
  state.buildProgress.log = normalized.output || "";
  syncBuildReference(normalized);
  renderBuildProgress();
}

function appendBuildLog(chunk) {
  state.buildProgress.log += chunk;
  if (state.buildProgress.job) {
    state.buildProgress.job.output = state.buildProgress.log;
  }
  renderBuildProgress();
}

function connectBuildStream(jobID) {
  disconnectBuildStream();
  const source = new EventSource(`/api/build-jobs/${encodeURIComponent(jobID)}/events`);
  state.buildProgress.eventSource = source;

  const parsePayload = (event) => {
    try {
      return JSON.parse(event.data || "null");
    } catch (error) {
      return null;
    }
  };

  source.addEventListener("snapshot", (event) => {
    applyBuildSnapshot(parsePayload(event));
  });

  source.addEventListener("status", (event) => {
    const payload = normalizeBuildJob(parsePayload(event));
    if (!payload) {
      return;
    }
    state.buildProgress.job = payload;
    state.buildProgress.job.output = state.buildProgress.log;
    syncBuildReference(payload);
    renderBuildProgress();
  });

  source.addEventListener("log", (event) => {
    const payload = parsePayload(event);
    if (!payload?.chunk) {
      return;
    }
    appendBuildLog(payload.chunk);
  });

  source.addEventListener("complete", async (event) => {
    applyBuildSnapshot(parsePayload(event));
    disconnectBuildStream();
    const job = state.buildProgress.job;
    if (job) {
      showToast(`Build ${formatBuildStatus(job.status)} for ${job.environment_name}.`, job.status === "failed" ? "error" : "success");
      await refreshEnvironments(state.environments.selectedName || job.environment_name);
      if (state.environments.selectedName === job.environment_name) {
        await selectEnvironment(job.environment_name);
      }
    }
  });

  source.onerror = async () => {
    if (!state.buildProgress.eventSource) {
      return;
    }
    disconnectBuildStream();
    const job = state.buildProgress.job;
    if (!job || !isBuildActive(job.status)) {
      return;
    }
    try {
      const refreshed = await api(`/api/build-jobs/${job.id}`);
      applyBuildSnapshot(refreshed);
      if (isBuildActive(refreshed.status)) {
        connectBuildStream(refreshed.id);
        return;
      }
    } catch (error) {
      buildProgressMeta.textContent = `Build stream disconnected: ${error.message}`;
    }
  };
}

async function openBuildProgress(job) {
  if (!job?.id) {
    showToast("No build job is available yet.", "error");
    return;
  }
  applyBuildSnapshot(job);
  openModal("build-progress-backdrop");
  if (isBuildActive(job.status)) {
    connectBuildStream(job.id);
  } else {
    disconnectBuildStream();
  }
}

async function openLatestBuildProgress(name) {
  const environment = await api(`/api/environments/${name}`);
  state.environments.selectedLastBuild = normalizeBuildJob(environment.last_build);
  updateBuildButton();
  if (!environment.last_build) {
    showToast("This environment does not have a build job yet.", "error");
    return;
  }
  await openBuildProgress(environment.last_build);
}

async function startBuild(name) {
  state.buildProgress.job = normalizeBuildJob({
    environment_name: name,
    target: "",
    status: "building",
    output: "",
  });
  state.buildProgress.log = "";
  renderBuildProgress();
  openModal("build-progress-backdrop");

  const result = await api(`/api/environments/${name}/build-jobs`, {
    method: "POST",
    body: "{}",
  });
  await openBuildProgress(result);
}

async function startBuildWithTarget(name, target) {
  state.buildProgress.job = normalizeBuildJob({
    environment_name: name,
    target: target || "",
    status: "building",
    output: "",
  });
  state.buildProgress.log = "";
  renderBuildProgress();
  openModal("build-progress-backdrop");

  const payload = target ? { target } : {};
  const result = await api(`/api/environments/${name}/build-jobs`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
  await openBuildProgress(result);
}

function openBuildTargetModal() {
  renderBuildTargetOptions();
  openModal("build-target-backdrop");
}

async function handleBuildButtonClick() {
  const name = document.getElementById("name").value.trim();
  if (!name) {
    environmentOutput.textContent = "Environment name is required before build.";
    showToast("Environment name is required before build.", "error");
    return;
  }

  setLoading(buildButton, true);
  try {
    if (state.environments.selectedLastBuild && isBuildActive(state.environments.selectedLastBuild.status)) {
      await openBuildProgress(state.environments.selectedLastBuild);
      return;
    }
    openBuildTargetModal();
  } catch (error) {
    if (String(error.message || "").includes("build already in progress")) {
      await openLatestBuildProgress(name);
      return;
    }
    environmentOutput.textContent = error.message;
    showToast(error.message, "error");
  } finally {
    setLoading(buildButton, false);
  }
}

async function initialize() {
  initializeShell("environments");
  bindModalDismiss();

  buildProgressBackdrop.addEventListener("modal:closed", () => {
    disconnectBuildStream();
  });

  buildTargetConfirm.addEventListener("click", async () => {
    const name = document.getElementById("name").value.trim();
    if (!name) {
      environmentOutput.textContent = "Environment name is required before build.";
      showToast("Environment name is required before build.", "error");
      return;
    }

    setLoading(buildTargetConfirm, true);
    try {
      const target = selectedBuildTarget();
      closeModal("build-target-backdrop");
      await startBuildWithTarget(name, target);
    } catch (error) {
      environmentOutput.textContent = error.message;
      showToast(error.message, "error");
    } finally {
      setLoading(buildTargetConfirm, false);
    }
  });

  buildProgressRefresh.addEventListener("click", async () => {
    const jobID = state.buildProgress.job?.id;
    if (!jobID) {
      showToast("No build job selected.", "error");
      return;
    }
    try {
      const result = await api(`/api/build-jobs/${jobID}`);
      applyBuildSnapshot(result);
      if (isBuildActive(result.status)) {
        connectBuildStream(jobID);
      }
    } catch (error) {
      showToast(error.message, "error");
    }
  });

  document.getElementById("refresh-environments").addEventListener("click", async () => {
    try {
      await refreshEnvironments(state.environments.selectedName);
      showToast("Environments refreshed.", "success");
    } catch (error) {
      environmentOutput.textContent = error.message;
      showToast(error.message, "error");
    }
  });

  saveButton.addEventListener("click", async () => {
    setLoading(saveButton, true);
    try {
      const result = await api("/api/environments", {
        method: "POST",
        body: JSON.stringify(collectEnvironmentPayload()),
      });
      state.environments.selectedBuild = normalizeBuild(result.build);
      state.environments.selectedDetails = normalizeEnvironmentDetails(result);
      state.environments.selectedAvailable = Boolean(result.available);
      state.environments.selectedDefaultExecute = normalizeExecutePreset(result.default_execute);
      state.environments.selectedAgentPrompt = result.agent_prompt || "";
      state.environments.selectedAvailableBuildTargets = normalizeBuildTargets(result.available_build_targets);
      state.environments.selectedLastBuild = normalizeBuildJob(result.last_build);
      environmentOutput.textContent = environmentStatusSummary(result);
      environmentYAML.textContent = result.yaml || "No YAML available.";
      showToast(`Environment ${result.name} saved.`, "success");
      await refreshEnvironments(result.name);
    } catch (error) {
      environmentOutput.textContent = error.message;
      showToast(error.message, "error");
    } finally {
      setLoading(saveButton, false);
    }
  });

  buildButton.addEventListener("click", handleBuildButtonClick);

  refreshFilesButton.addEventListener("click", async () => {
    try {
      await refreshEnvironmentFiles();
      showToast("Environment files refreshed.", "success");
    } catch (error) {
      environmentOutput.textContent = error.message;
      showToast(error.message, "error");
    }
  });

  newFileButton.addEventListener("click", () => {
    state.files.selectedPath = "";
    state.files.selectedContent = "";
    environmentFilePath.value = "";
    environmentFileContent.value = "";
    renderEnvironmentFiles();
  });

  environmentFilePath.addEventListener("input", () => {
    state.files.selectedPath = environmentFilePath.value.trim();
  });

  environmentFileContent.addEventListener("input", () => {
    state.files.selectedContent = environmentFileContent.value;
  });

  saveFileButton.addEventListener("click", async () => {
    const name = state.environments.selectedName || document.getElementById("name").value.trim();
    const path = environmentFilePath.value.trim();
    if (!name) {
      environmentOutput.textContent = "Save the environment metadata first so files have a directory to live in.";
      showToast("Save the environment metadata first so files have a directory to live in.", "error");
      return;
    }
    if (!path) {
      environmentOutput.textContent = "File path is required.";
      showToast("File path is required.", "error");
      return;
    }

    setLoading(saveFileButton, true);
    try {
      const saved = await api(`/api/environments/${name}/files/${encodeURIComponent(path).replaceAll("%2F", "/")}`, {
        method: "PUT",
        body: JSON.stringify({ content: environmentFileContent.value }),
      });
      state.files.selectedPath = saved.path;
      state.files.selectedContent = saved.content || "";
      environmentFilePath.value = saved.path;
      environmentFileContent.value = saved.content || "";
      showToast(`Saved ${saved.path}.`, "success");
      if (saved.path === "Dockerfile") {
        state.environments.selectedBuild.dockerfile = saved.content || "";
      }
      if (saved.path === "environment.yml") {
        environmentYAML.textContent = saved.content || "No YAML available.";
      }
      await refreshEnvironmentFiles(name, saved.path);
      await refreshEnvironments(name);
    } catch (error) {
      environmentOutput.textContent = error.message;
      showToast(error.message, "error");
    } finally {
      setLoading(saveFileButton, false);
    }
  });

  try {
    await refreshEnvironments();
  } catch (error) {
    environmentOutput.textContent = error.message;
    showToast(error.message, "error");
  }
}

initialize();

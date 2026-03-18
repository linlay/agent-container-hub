import { api, escapeHTML, initializeShell, setLoading, showToast } from "/ui/common.js";

const state = {
  environments: {
    items: [],
    selectedName: "",
    selectedBuild: defaultBuild(),
    selectedDetails: defaultEnvironmentDetails(),
    selectedDefaultExecute: defaultExecutePreset(),
  },
  files: {
    items: [],
    selectedPath: "",
    selectedContent: "",
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
  state.environments.selectedDefaultExecute = defaultExecutePreset();
  state.files.items = [];
  state.files.selectedPath = "";
  state.files.selectedContent = "";
  environmentFilePath.value = "";
  environmentFileContent.value = "";
  renderEnvironmentFiles();
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

function fileTypeLabel(file) {
  return `${file.type || "other"} · ${file.path}`;
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
        <span class="pill ${item.enabled ? "" : "stopped"}">${item.enabled ? "enabled" : "disabled"}</span>
      </div>
      <div class="meta">${escapeHTML(item.image_ref || "-")}</div>
      <div class="meta">${item.last_build ? `last build: ${escapeHTML(item.last_build.status)}` : "no build yet"}</div>
    </div>
  `).join("") || `<div class="empty">No environments yet.</div>`;

  environmentList.querySelectorAll("[data-environment-name]").forEach((node) => {
    node.addEventListener("click", async () => {
      await selectEnvironment(node.dataset.environmentName);
    });
  });
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
  state.environments.selectedDefaultExecute = normalizeExecutePreset(item.default_execute);
  document.getElementById("default-execute-command").value = state.environments.selectedDefaultExecute.command || "";
  document.getElementById("default-execute-cwd").value = state.environments.selectedDefaultExecute.cwd || "";
  document.getElementById("default-execute-timeout").value = state.environments.selectedDefaultExecute.timeout_ms || "";
  document.getElementById("default-execute-args").value = state.environments.selectedDefaultExecute.args.join("\n");
  environmentOutput.textContent = item.last_build
    ? `Last build ${item.last_build.status} at ${item.last_build.started_at || "-"}`
    : "Environment loaded.";
  environmentYAML.textContent = item.yaml || "No YAML available.";
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
    mounts: state.environments.selectedDetails.mounts.map((mount) => ({ ...mount })),
    resources: { ...state.environments.selectedDetails.resources },
    enabled: state.environments.selectedDetails.enabled,
    default_execute: defaultExecute,
    build,
  };
}

async function initialize() {
  initializeShell("environments");

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
      state.environments.selectedDefaultExecute = normalizeExecutePreset(result.default_execute);
      environmentOutput.textContent = "Environment saved.";
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

  buildButton.addEventListener("click", async () => {
    const name = document.getElementById("name").value.trim();
    if (!name) {
      environmentOutput.textContent = "Environment name is required before build.";
      showToast("Environment name is required before build.", "error");
      return;
    }

    setLoading(buildButton, true);
    try {
      const result = await api(`/api/environments/${name}/build`, {
        method: "POST",
        body: "{}",
      });
      environmentOutput.textContent = `Build ${result.status} for ${result.environment_name} (${result.image_ref}).`;
      showToast(`Build ${result.status} for ${result.environment_name}.`, result.status === "failed" ? "error" : "success");
      await refreshEnvironments(name);
    } catch (error) {
      environmentOutput.textContent = error.message;
      showToast(error.message, "error");
    } finally {
      setLoading(buildButton, false);
    }
  });

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
      const result = await api(`/api/environments/${name}/files/${encodeURIComponent(path).replaceAll("%2F", "/")}`, {
        method: "PUT",
        body: JSON.stringify({ content: environmentFileContent.value }),
      });
      state.files.selectedPath = result.path;
      state.files.selectedContent = result.content || "";
      environmentFilePath.value = result.path;
      environmentFileContent.value = state.files.selectedContent;
      environmentOutput.textContent = `Saved ${result.path}.`;
      showToast(`Saved ${result.path}.`, "success");
      if (result.path === "environment.yml") {
        await refreshEnvironments(name);
      } else {
        await refreshEnvironmentFiles(name, result.path);
      }
    } catch (error) {
      environmentOutput.textContent = error.message;
      showToast(error.message, "error");
    } finally {
      setLoading(saveFileButton, false);
    }
  });

  await refreshEnvironments();
}

initialize().catch((error) => {
  environmentOutput.textContent = error.message;
  showToast(error.message, "error");
});

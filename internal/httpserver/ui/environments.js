import { api, escapeHTML, initializeShell } from "/ui/common.js";

const state = {
  environments: {
    items: [],
    selectedName: "",
    selectedBuild: defaultBuild(),
    selectedDetails: defaultEnvironmentDetails(),
    selectedDefaultExecute: defaultExecutePreset(),
  },
};

const environmentList = document.getElementById("environment-list");
const environmentOutput = document.getElementById("environment-output");
const environmentYAML = document.getElementById("environment-yaml");

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
  document.getElementById("dockerfile").value = "FROM busybox:latest\nCMD [\"/bin/sh\"]";
  state.environments.selectedBuild = defaultBuild();
  state.environments.selectedDetails = defaultEnvironmentDetails();
  state.environments.selectedDefaultExecute = defaultExecutePreset();
}

function defaultBuild() {
  return {
    dockerfile: "FROM busybox:latest\nCMD [\"/bin/sh\"]",
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
  document.getElementById("dockerfile").value = state.environments.selectedBuild.dockerfile || "";
  environmentOutput.textContent = item.last_build
    ? `Last build ${item.last_build.status} at ${item.last_build.started_at || "-"}`
    : "Environment loaded.";
  environmentYAML.textContent = item.yaml || "No YAML available.";
  renderEnvironments();
}

function collectEnvironmentPayload() {
  const build = {
    ...normalizeBuild(state.environments.selectedBuild),
    dockerfile: document.getElementById("dockerfile").value,
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
    await refreshEnvironments(state.environments.selectedName);
  });

  document.getElementById("save").addEventListener("click", async () => {
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
      await refreshEnvironments(result.name);
    } catch (error) {
      environmentOutput.textContent = error.message;
    }
  });

  document.getElementById("build").addEventListener("click", async () => {
    const name = document.getElementById("name").value.trim();
    if (!name) {
      environmentOutput.textContent = "Environment name is required before build.";
      return;
    }
    try {
      const result = await api(`/api/environments/${name}/build`, {
        method: "POST",
        body: "{}",
      });
      environmentOutput.textContent = `Build ${result.status} for ${result.environment_name} (${result.image_ref}).`;
      await refreshEnvironments(name);
    } catch (error) {
      environmentOutput.textContent = error.message;
    }
  });

  await refreshEnvironments();
}

initialize().catch((error) => {
  environmentOutput.textContent = error.message;
});

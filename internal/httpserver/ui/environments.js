import { api, escapeHTML, initializeShell } from "/ui/common.js";

const state = {
  environments: {
    items: [],
    selectedName: "",
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
  document.getElementById("dockerfile").value = "FROM busybox:latest\nCMD [\"/bin/sh\"]";
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
  document.getElementById("dockerfile").value = item.build?.dockerfile || "";
  environmentOutput.textContent = item.last_build
    ? `Last build ${item.last_build.status} at ${item.last_build.started_at || "-"}`
    : "Environment loaded.";
  environmentYAML.textContent = item.yaml || "No YAML available.";
  renderEnvironments();
}

function collectEnvironmentPayload() {
  return {
    name: document.getElementById("name").value.trim(),
    image_repository: document.getElementById("repository").value.trim(),
    image_tag: document.getElementById("tag").value.trim(),
    default_cwd: document.getElementById("cwd").value.trim(),
    description: document.getElementById("description").value.trim(),
    enabled: true,
    build: {
      dockerfile: document.getElementById("dockerfile").value,
    },
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

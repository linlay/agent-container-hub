const FOCUSABLE_SELECTOR = [
  "button:not([disabled])",
  "a[href]",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[tabindex]:not([tabindex='-1'])",
].join(", ");

export async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (!headers.has("Content-Type") && options.body !== undefined) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(path, {
    ...options,
    headers
  });

  if (response.status === 401) {
    window.location.href = "/login";
    throw new Error("unauthorized");
  }

  const text = await response.text();
  const data = text ? JSON.parse(text) : null;
  if (!response.ok) {
    throw new Error(data?.error || `request failed: ${response.status}`);
  }
  return data;
}

function openModals() {
  return Array.from(document.querySelectorAll(".modal-backdrop.open"));
}

function focusFirstElement(backdrop) {
  const target = backdrop?.querySelector(FOCUSABLE_SELECTOR);
  if (!target) {
    return;
  }
  window.requestAnimationFrame(() => target.focus());
}

function syncBodyScrollLock() {
  document.body.style.overflow = openModals().length > 0 ? "hidden" : "";
}

export function openModal(id) {
  const backdrop = document.getElementById(id);
  if (!backdrop) {
    return;
  }
  backdrop.classList.add("open");
  backdrop.dispatchEvent(new CustomEvent("modal:opened", { bubbles: true, detail: { id } }));
  syncBodyScrollLock();
  focusFirstElement(backdrop);
}

export function closeModal(id) {
  const backdrop = document.getElementById(id);
  if (!backdrop) {
    return;
  }
  backdrop.classList.remove("open");
  backdrop.dispatchEvent(new CustomEvent("modal:closed", { bubbles: true, detail: { id } }));
  syncBodyScrollLock();
}

function topmostOpenModal() {
  const modals = openModals();
  return modals[modals.length - 1] || null;
}

export function bindModalDismiss() {
  document.querySelectorAll("[data-close-modal]").forEach((button) => {
    button.addEventListener("click", () => closeModal(button.dataset.closeModal));
  });

  document.querySelectorAll(".modal-backdrop").forEach((backdrop) => {
    backdrop.addEventListener("click", (event) => {
      if (event.target === backdrop) {
        closeModal(backdrop.id);
      }
    });
  });

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") {
      return;
    }
    const backdrop = topmostOpenModal();
    if (backdrop) {
      closeModal(backdrop.id);
    }
  });
}

export function showToast(message, type = "success", duration = 3200) {
  const container = document.getElementById("toast-container");
  if (!container) {
    return null;
  }

  const toast = document.createElement("div");
  toast.className = `toast ${type === "error" ? "error" : "success"}`;
  toast.setAttribute("role", type === "error" ? "alert" : "status");
  const copy = document.createElement("div");
  copy.className = "toast-copy";

  const title = document.createElement("strong");
  title.textContent = type === "error" ? "Error" : "Success";

  const detail = document.createElement("span");
  detail.textContent = String(message ?? "");

  const closeButton = document.createElement("button");
  closeButton.type = "button";
  closeButton.className = "toast-close";
  closeButton.setAttribute("aria-label", "Close notification");
  closeButton.textContent = "\u00d7";

  copy.append(title, detail);
  toast.append(copy, closeButton);

  let removed = false;
  let timer = 0;
  const dismiss = () => {
    if (removed) {
      return;
    }
    removed = true;
    window.clearTimeout(timer);
    toast.classList.add("exit");
    window.setTimeout(() => toast.remove(), 160);
  };

  closeButton.addEventListener("click", (event) => {
    event.stopPropagation();
    dismiss();
  });

  toast.addEventListener("click", dismiss);
  container.appendChild(toast);
  timer = window.setTimeout(dismiss, duration);
  return dismiss;
}

export function setLoading(button, loading) {
  if (!button) {
    return;
  }

  if (loading) {
    button.dataset.loadingDisabled = button.disabled ? "true" : "false";
    button.classList.add("loading");
    button.disabled = true;
    return;
  }

  button.classList.remove("loading");
  button.disabled = button.dataset.loadingDisabled === "true";
  delete button.dataset.loadingDisabled;
}

export function formatTime(value) {
  return value || "-";
}

export function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

export function initializeShell(page) {
  document.querySelectorAll("[data-nav]").forEach((link) => {
    link.classList.toggle("active", link.dataset.nav === page);
  });

  const logoutButton = document.getElementById("logout");
  if (logoutButton) {
    logoutButton.addEventListener("click", async () => {
      await api("/api/auth/logout", { method: "POST", body: "{}" });
      window.location.href = "/login";
    });
  }
}

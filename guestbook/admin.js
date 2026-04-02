const API_BASE = "https://chenna-guestbook.fly.dev";
const TOKEN_STORAGE_KEY = "guestbookAdminToken";

let previewObjectUrls = [];

function buildApiURL(path) {
  return new URL(path, API_BASE).toString();
}

function readStoredToken() {
  try {
    return window.sessionStorage.getItem(TOKEN_STORAGE_KEY) || "";
  } catch {
    return "";
  }
}

function storeToken(token) {
  try {
    window.sessionStorage.setItem(TOKEN_STORAGE_KEY, token);
  } catch {
    // Ignore storage failures and keep using the in-memory token.
  }
}

function clearStoredToken() {
  try {
    window.sessionStorage.removeItem(TOKEN_STORAGE_KEY);
  } catch {
    // Ignore storage failures.
  }
}

function clearStatus(node) {
  if (!node) {
    return;
  }
  node.textContent = "";
  node.className = "gb-status hidden";
}

function setStatus(node, message, tone = "info") {
  if (!node) {
    return;
  }
  node.textContent = message;
  node.className = `gb-status gb-status--${tone}`;
}

function clearPreviewObjectUrls() {
  previewObjectUrls.forEach((url) => URL.revokeObjectURL(url));
  previewObjectUrls = [];
}

async function authorizedJSON(path, token, options = {}) {
  const response = await fetch(buildApiURL(path), {
    ...options,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(options.headers || {}),
    },
  });

  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(payload.error || `HTTP ${response.status}`);
  }
  return payload;
}

async function loadProtectedImage(url, token) {
  const response = await fetch(url, {
    headers: {
      Authorization: `Bearer ${token}`,
    },
  });

  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`);
  }

  const blob = await response.blob();
  const objectUrl = URL.createObjectURL(blob);
  previewObjectUrls.push(objectUrl);
  return objectUrl;
}

function createAuthorNode(entry) {
  if (entry.website) {
    const link = document.createElement("a");
    link.href = entry.website;
    link.target = "_blank";
    link.rel = "noopener noreferrer";
    link.textContent = entry.name;
    return link;
  }

  const strong = document.createElement("strong");
  strong.textContent = entry.name;
  return strong;
}

function formatSubmittedDate(value) {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "";
  }
  return parsed.toLocaleString();
}

function createPreviewNode(entry, token) {
  if (entry.entry_type === "drawing" && entry.image_url) {
    const wrapper = document.createElement("div");
    wrapper.className = "gb-admin-preview";

    const loading = document.createElement("div");
    loading.className = "gb-admin-preview-note";
    loading.textContent = "Loading drawing preview...";
    wrapper.appendChild(loading);

    loadProtectedImage(entry.image_url, token)
      .then((objectUrl) => {
        const image = document.createElement("img");
        image.className = "gb-entry-image";
        image.src = objectUrl;
        image.alt = `Drawing by ${entry.name}`;
        wrapper.replaceChildren(image);
      })
      .catch(() => {
        loading.textContent = "Couldn't load the drawing preview.";
      });

    return wrapper;
  }

  const message = document.createElement("div");
  message.className = "gb-entry-message gb-admin-preview";
  message.textContent = entry.content || "";
  return message;
}

function updateEmptyState(list) {
  if (!list.querySelector(".gb-admin-entry")) {
    list.innerHTML = '<div class="gb-empty">No pending entries right now.</div>';
  }
}

function createPendingEntry(entry, token, list, topStatus) {
  const article = document.createElement("article");
  article.className = "gb-admin-entry";
  article.appendChild(createPreviewNode(entry, token));

  const meta = document.createElement("div");
  meta.className = "gb-admin-meta";

  const nameLine = document.createElement("p");
  nameLine.appendChild(createAuthorNode(entry));
  meta.appendChild(nameLine);

  const submitted = formatSubmittedDate(entry.created_at);
  if (submitted) {
    const timeLine = document.createElement("p");
    const time = document.createElement("time");
    time.dateTime = entry.created_at;
    time.textContent = submitted;
    timeLine.appendChild(time);
    meta.appendChild(timeLine);
  }

  article.appendChild(meta);

  const actions = document.createElement("div");
  actions.className = "gb-admin-actions";

  const approveButton = document.createElement("button");
  approveButton.type = "button";
  approveButton.className = "gb-action-btn";
  approveButton.textContent = "Approve";

  const rejectButton = document.createElement("button");
  rejectButton.type = "button";
  rejectButton.className = "gb-secondary-btn gb-action-btn--reject";
  rejectButton.textContent = "Reject";

  const inlineStatus = document.createElement("div");
  inlineStatus.className = "gb-status gb-inline-status hidden";

  const runAction = async (action) => {
    approveButton.disabled = true;
    rejectButton.disabled = true;
    setStatus(inlineStatus, action === "approve" ? "Approving..." : "Rejecting...", "info");

    try {
      await authorizedJSON(`/api/admin/entries/${entry.id}/${action}`, token, {
        method: "POST",
      });
      article.remove();
      setStatus(topStatus, action === "approve" ? "Entry approved." : "Entry rejected.", "success");
      updateEmptyState(list);
    } catch (error) {
      setStatus(inlineStatus, error.message || "Action failed.", "error");
      approveButton.disabled = false;
      rejectButton.disabled = false;
      if (error.message === "unauthorized") {
        clearStoredToken();
        setStatus(topStatus, "The saved token was rejected. Enter it again.", "error");
      }
    }
  };

  approveButton.addEventListener("click", () => runAction("approve"));
  rejectButton.addEventListener("click", () => runAction("reject"));

  actions.appendChild(approveButton);
  actions.appendChild(rejectButton);
  article.appendChild(actions);
  article.appendChild(inlineStatus);

  return article;
}

async function loadPendingEntries(token, list, panel, status) {
  panel.classList.remove("hidden");
  clearPreviewObjectUrls();
  list.innerHTML = '<div class="gb-loading">Loading pending entries...</div>';

  try {
    const payload = await authorizedJSON("/api/admin/entries", token);
    const entries = payload.entries || [];

    if (!entries.length) {
      list.innerHTML = '<div class="gb-empty">No pending entries right now.</div>';
      setStatus(status, "No pending entries right now.", "info");
      return;
    }

    list.innerHTML = "";
    entries.forEach((entry) => {
      list.appendChild(createPendingEntry(entry, token, list, status));
    });
    setStatus(status, `Loaded ${entries.length} pending entr${entries.length === 1 ? "y" : "ies"}.`, "success");
  } catch (error) {
    console.error("Failed to load pending entries:", error);
    list.innerHTML = '<div class="gb-empty">Could not load pending entries.</div>';
    setStatus(status, error.message || "Could not load pending entries.", "error");
    if (error.message === "unauthorized") {
      clearStoredToken();
    }
  }
}

document.addEventListener("DOMContentLoaded", () => {
  const form = document.getElementById("admin-auth-form");
  if (!form) {
    return;
  }

  const tokenInput = document.getElementById("admin-token");
  const clearButton = document.getElementById("clear-token");
  const panel = document.getElementById("admin-panel");
  const list = document.getElementById("admin-list");
  const status = document.getElementById("admin-status");

  const savedToken = readStoredToken();
  if (savedToken) {
    tokenInput.value = savedToken;
    loadPendingEntries(savedToken, list, panel, status);
  }

  form.addEventListener("submit", async (event) => {
    event.preventDefault();

    const token = tokenInput.value.trim();
    if (!token) {
      setStatus(status, "Enter the admin token first.", "error");
      tokenInput.focus();
      return;
    }

    storeToken(token);
    await loadPendingEntries(token, list, panel, status);
  });

  clearButton.addEventListener("click", () => {
    clearStoredToken();
    clearPreviewObjectUrls();
    tokenInput.value = "";
    panel.classList.add("hidden");
    list.innerHTML = '<div class="gb-loading">Enter a token to load pending entries.</div>';
    clearStatus(status);
  });
});
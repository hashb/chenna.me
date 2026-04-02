const API_BASE = document.querySelector('meta[name="guestbook-api"]')?.content
  || (window.location.hostname === "localhost" ? "http://localhost:8080" : "https://chenna-guestbook.fly.dev");
const PUBLIC_PAGE_SIZE = 24;

function buildApiURL(path, params = {}) {
  const url = new URL(path, API_BASE);
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== "") {
      url.searchParams.set(key, String(value));
    }
  });
  return url.toString();
}

async function requestJSON(url, options = {}) {
  const response = await fetch(url, options);
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(payload.error || `HTTP ${response.status}`);
  }
  return payload;
}

function buildGuestbookPageURL(page) {
  return page > 1 ? `/guestbook/?page=${page}` : "/guestbook/";
}

function getRequestedPage() {
  const params = new URLSearchParams(window.location.search);
  const parsed = Number.parseInt(params.get("page") || "1", 10);
  if (Number.isNaN(parsed) || parsed < 1) {
    return 1;
  }
  return parsed;
}

function setGalleryMessage(grid, message, className) {
  grid.classList.add("gb-gallery--single");
  grid.innerHTML = `<div class="${className}">${message}</div>`;
}

function createAuthorNode(entry) {
  if (entry.website) {
    const link = document.createElement("a");
    link.href = entry.website;
    link.target = "_blank";
    link.rel = "ugc nofollow noopener noreferrer";
    link.textContent = entry.name;
    return link;
  }

  const span = document.createElement("span");
  span.className = "gb-entry-author";
  span.textContent = entry.name;
  return span;
}

function createGuestbookEntry(entry) {
  const article = document.createElement("article");
  article.className = `gb-entry gb-entry--${entry.entry_type}`;

  let contentNode;
  if (entry.entry_type === "drawing" && entry.image_url) {
    const image = document.createElement("img");
    image.className = "gb-entry-image";
    image.src = entry.image_url;
    image.alt = `Drawing by ${entry.name}`;
    image.loading = "lazy";
    contentNode = image;
  } else {
    const message = document.createElement("div");
    message.className = "gb-entry-message";
    message.textContent = entry.content || "";
    contentNode = message;
  }

  const meta = document.createElement("p");
  meta.className = "gb-entry-meta";
  meta.appendChild(createAuthorNode(entry));

  article.appendChild(contentNode);
  article.appendChild(meta);
  return article;
}

function renderEntries(grid, entries) {
  if (!entries.length) {
    setGalleryMessage(grid, "No entries yet. Be the first to sign.", "gb-empty");
    return;
  }

  grid.classList.remove("gb-gallery--single");
  grid.innerHTML = "";
  entries.forEach((entry) => {
    grid.appendChild(createGuestbookEntry(entry));
  });
}

function renderPagination(container, pagination) {
  if (!container) {
    return;
  }

  if (!pagination || pagination.total_pages <= 1) {
    container.innerHTML = "";
    container.classList.add("hidden");
    return;
  }

  container.classList.remove("hidden");
  container.innerHTML = "";

  const previous = pagination.has_previous
    ? Object.assign(document.createElement("a"), {
        className: "prev left",
        href: buildGuestbookPageURL(pagination.previous_page),
        textContent: "<- Previous",
      })
    : Object.assign(document.createElement("span"), {
        className: "nope left",
        textContent: "<- Previous",
      });

  const pages = document.createElement("span");
  pages.className = "pages";
  pages.textContent = `Page ${pagination.page} of ${pagination.total_pages}`;

  const next = pagination.has_next
    ? Object.assign(document.createElement("a"), {
        className: "next right",
        href: buildGuestbookPageURL(pagination.next_page),
        textContent: "Next ->",
      })
    : Object.assign(document.createElement("span"), {
        className: "nope right",
        textContent: "Next ->",
      });

  container.appendChild(previous);
  container.appendChild(pages);
  container.appendChild(next);
}

async function loadEntries() {
  const grid = document.getElementById("entry-grid");
  if (!grid) {
    return;
  }

  const pagination = document.getElementById("entry-pagination");
  const requestedPage = getRequestedPage();

  setGalleryMessage(grid, "Loading entries...", "gb-loading");
  if (pagination) {
    pagination.innerHTML = "";
    pagination.classList.add("hidden");
  }

  try {
    const payload = await requestJSON(
      buildApiURL("/api/entries", {
        page: requestedPage,
        per_page: PUBLIC_PAGE_SIZE,
      })
    );

    if (payload.pagination && payload.pagination.page !== requestedPage) {
      window.history.replaceState({}, "", buildGuestbookPageURL(payload.pagination.page));
    }

    renderEntries(grid, payload.entries || []);
    renderPagination(pagination, payload.pagination);
  } catch (error) {
    console.error("Failed to load entries:", error);
    setGalleryMessage(grid, "Couldn't load guestbook entries right now.", "gb-empty");
    if (pagination) {
      pagination.innerHTML = "";
      pagination.classList.add("hidden");
    }
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

function canvasToBlob(canvas) {
  return new Promise((resolve) => {
    if (!canvas || typeof canvas.toBlob !== "function") {
      resolve(null);
      return;
    }
    canvas.toBlob(resolve, "image/png");
  });
}

function initSignForm() {
  const form = document.getElementById("sign-form");
  if (!form) {
    return;
  }

  const status = document.getElementById("form-status");
  const drawSection = document.getElementById("draw-section");
  const messageSection = document.getElementById("message-section");
  const messageField = document.getElementById("message");
  const nameField = document.getElementById("name");
  const websiteField = document.getElementById("website");
  const submitButton = form.querySelector(".gb-submit-btn");
  const success = document.getElementById("success");
  const modeRadios = form.querySelectorAll('input[name="mode"]');

  const setMode = (mode) => {
    const isDrawMode = mode === "draw";
    drawSection.classList.toggle("hidden", !isDrawMode);
    messageSection.classList.toggle("hidden", isDrawMode);
    messageField.disabled = isDrawMode;
    messageField.required = !isDrawMode;
    clearStatus(status);
  };

  modeRadios.forEach((radio) => {
    radio.addEventListener("change", () => {
      setMode(radio.value);
    });
  });

  const checkedMode = form.querySelector('input[name="mode"]:checked');
  setMode(checkedMode ? checkedMode.value : "draw");

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    clearStatus(status);

    const mode = form.querySelector('input[name="mode"]:checked').value;
    const name = nameField.value.trim();
    const website = websiteField.value.trim();
    const content = messageField.value.replace(/\r\n/g, "\n");

    if (!name) {
      setStatus(status, "Add your name before submitting.", "error");
      nameField.focus();
      return;
    }

    if (mode !== "draw" && !content.trim()) {
      setStatus(status, "Write a message before submitting.", "error");
      messageField.focus();
      return;
    }

    if (mode === "draw") {
      const drawingState = window.guestbookCanvas;
      if (drawingState && typeof drawingState.isBlank === "function" && drawingState.isBlank()) {
        setStatus(status, "Add a drawing before submitting.", "error");
        return;
      }
    }

    submitButton.disabled = true;
    submitButton.textContent = "Submitting...";

    try {
      const formData = new FormData();
      formData.append("name", name);
      if (website) {
        formData.append("website", website);
      }
      formData.append("entry_type", mode === "draw" ? "drawing" : "message");

      if (mode === "draw") {
        const blob = await canvasToBlob(document.getElementById("draw-canvas"));
        if (!blob) {
          throw new Error("Couldn't capture your drawing.");
        }
        formData.append("image", blob, "drawing.png");
      } else {
        formData.append("content", content);
      }

      await requestJSON(buildApiURL("/api/entries"), {
        method: "POST",
        body: formData,
      });

      form.classList.add("hidden");
      success.classList.remove("hidden");
    } catch (error) {
      console.error("Submit failed:", error);
      setStatus(status, error.message || "Something went wrong.", "error");
      submitButton.disabled = false;
      submitButton.textContent = "Submit";
    }
  });
}

document.addEventListener("DOMContentLoaded", () => {
  loadEntries();
  initSignForm();
});

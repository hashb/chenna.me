const API_BASE = document.querySelector('meta[name="guestbook-api"]')?.content
  || (window.location.hostname === "localhost" ? "http://localhost:8080" : "https://guestbook.chenna.me");

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
  initSignForm();
});

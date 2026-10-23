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
  const websiteFieldWrap = document.getElementById("website-field");
  const honeypotField = document.getElementById("gb-verify");
  const submitButton = form.querySelector(".gb-submit-btn");
  const success = document.getElementById("success");
  const modeRadios = form.querySelectorAll('input[name="mode"]');

  const setMode = (mode) => {
    const isDrawMode = mode === "draw";
    drawSection.classList.toggle("hidden", !isDrawMode);
    messageSection.classList.toggle("hidden", isDrawMode);
    // Not `required`: the browser's own check would block the submit before
    // the handler below can show its message and record it in analytics
    messageField.disabled = isDrawMode;
    clearStatus(status);
  };

  // Name starts as "Anonymoose" so signing needs no typing at all. While it
  // still says that, tapping the field selects it so typing replaces it, and
  // the website field stays hidden until someone puts their own name in.
  const DEFAULT_NAME = "Anonymoose";

  const hasOwnName = () => {
    const value = nameField.value.trim();
    return value !== "" && value.toLowerCase() !== DEFAULT_NAME.toLowerCase();
  };

  const updateWebsiteField = () => {
    websiteFieldWrap?.classList.toggle("hidden", !hasOwnName());
  };

  const selectDefaultName = () => {
    if (!hasOwnName()) nameField.setSelectionRange(0, nameField.value.length);
  };

  nameField.addEventListener("focus", selectDefaultName);
  nameField.addEventListener("click", selectDefaultName);
  nameField.addEventListener("input", updateWebsiteField);
  nameField.addEventListener("blur", () => {
    if (!nameField.value.trim()) nameField.value = DEFAULT_NAME;
    updateWebsiteField();
  });

  // Text draft: the chosen tab, message, name and website are kept in
  // localStorage next to the drawing draft (canvas.js), so a back swipe or
  // reload doesn't lose them. Cleared on submit or after a week.
  const FORM_DRAFT_KEY = "guestbook-form-draft";
  const FORM_DRAFT_MAX_AGE = 7 * 24 * 60 * 60 * 1000;
  let formDraftTimer = null;

  const currentMode = () => form.querySelector('input[name="mode"]:checked')?.value || "draw";

  const saveFormDraft = () => {
    clearTimeout(formDraftTimer);
    formDraftTimer = setTimeout(() => {
      try {
        const draft = {
          savedAt: Date.now(),
          mode: currentMode(),
          message: messageField.value,
          name: nameField.value,
          website: websiteField.value,
        };
        if (!draft.message && !hasOwnName() && !draft.website && draft.mode === "draw") {
          localStorage.removeItem(FORM_DRAFT_KEY);
        } else {
          localStorage.setItem(FORM_DRAFT_KEY, JSON.stringify(draft));
        }
      } catch (err) {
        // Storage full or blocked: the draft is only a safety net
      }
    }, 300);
  };

  const clearFormDraft = () => {
    clearTimeout(formDraftTimer);
    try {
      localStorage.removeItem(FORM_DRAFT_KEY);
    } catch (err) {}
  };

  const restoreFormDraft = () => {
    let draft = null;
    try {
      draft = JSON.parse(localStorage.getItem(FORM_DRAFT_KEY));
    } catch (err) {
      return;
    }
    if (!draft || typeof draft !== "object") return;
    if (!(Date.now() - draft.savedAt < FORM_DRAFT_MAX_AGE)) {
      clearFormDraft();
      return;
    }
    // Fields the browser already refilled (e.g. on back navigation) win
    if (typeof draft.message === "string" && !messageField.value) messageField.value = draft.message;
    if (typeof draft.name === "string" && draft.name.trim() && !hasOwnName()) nameField.value = draft.name;
    if (typeof draft.website === "string" && !websiteField.value) websiteField.value = draft.website;
    const radio = form.querySelector(`input[name="mode"][value="${draft.mode === "message" ? "message" : "draw"}"]`);
    if (radio) radio.checked = true;
  };

  modeRadios.forEach((radio) => {
    radio.addEventListener("change", () => {
      setMode(radio.value);
      saveFormDraft();
    });
  });
  [messageField, nameField, websiteField].forEach((field) => {
    field.addEventListener("input", saveFormDraft);
  });

  restoreFormDraft();
  updateWebsiteField();
  setMode(currentMode());

  const trackBlocked = (reason) => window.umami?.track?.("guestbook-submit-blocked", { reason });

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    clearStatus(status);

    const mode = form.querySelector('input[name="mode"]:checked').value;
    const name = nameField.value.trim() || DEFAULT_NAME;
    const website = hasOwnName() ? websiteField.value.trim() : "";
    const content = messageField.value.replace(/\r\n/g, "\n");

    if (mode !== "draw" && !content.trim()) {
      setStatus(status, "Write something~", "error");
      trackBlocked("empty-message");
      messageField.focus();
      return;
    }

    if (mode === "draw") {
      const drawingState = window.guestbookCanvas;
      if (drawingState && typeof drawingState.isBlank === "function" && drawingState.isBlank()) {
        setStatus(status, "Add a drawing before submitting.", "error");
        trackBlocked("blank-drawing");
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
      if (honeypotField && honeypotField.value) {
        formData.append("gb_verify", honeypotField.value);
      }

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

      window.guestbookCanvas?.clearDraft?.();
      clearFormDraft();
      window.umami?.track?.("guestbook-submit-ok", { entry_type: mode, anonymous: !hasOwnName() });
      form.classList.add("hidden");
      success.classList.remove("hidden");
    } catch (error) {
      console.error("Submit failed:", error);
      window.umami?.track?.("guestbook-submit-failed", {
        entry_type: mode,
        error: String(error.message || error).slice(0, 100),
      });
      setStatus(status, error.message || "Something went wrong.", "error");
      submitButton.disabled = false;
      submitButton.textContent = "Submit";
    }
  });
}

document.addEventListener("DOMContentLoaded", () => {
  initSignForm();
});

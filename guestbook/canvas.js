(() => {
  const canvas = document.getElementById("draw-canvas");
  if (!canvas) return;

  const ctx = canvas.getContext("2d");
  const LOGICAL = 500;
  const ACTUAL = 1000;

  canvas.width = ACTUAL;
  canvas.height = ACTUAL;

  // White paper with a dot grid (Moleskine style)
  const DOT_SPACING = 40; // actual pixels (= 20 logical px)
  function paintPaper() {
    ctx.fillStyle = "#fff";
    ctx.fillRect(0, 0, ACTUAL, ACTUAL);
    ctx.fillStyle = "#c8c8c8";
    for (let x = DOT_SPACING; x < ACTUAL; x += DOT_SPACING) {
      for (let y = DOT_SPACING; y < ACTUAL; y += DOT_SPACING) {
        ctx.beginPath();
        ctx.arc(x, y, 1.5, 0, Math.PI * 2);
        ctx.fill();
      }
    }
  }
  paintPaper();

  // ── State ───────────────────────────────────────────────────
  let tool = "pen"; // "pen" | "eraser"
  let brushSize = 9; // 3, 9, 18
  let color = "#000000";
  let drawing = false;
  let lastX = null;
  let lastY = null;

  // History: each step is a PNG snapshot (a Promise<Blob>, ~50-200KB) rather
  // than raw pixels (4MB per step), so long drawings don't run phones out of
  // memory. A step without a snapshot is the blank page.
  let history = [{ snapshot: null, blank: true }];
  let historyIndex = 0;
  const MAX_HISTORY = 50;

  // While an undo/redo is decoding its snapshot, new strokes are ignored so
  // the restored image can't paint over them.
  let restoreToken = 0;
  let restoring = false;

  function takeSnapshot() {
    // toBlob captures the canvas as it is right now, even though it resolves later
    return new Promise((resolve) => canvas.toBlob(resolve, "image/png"));
  }

  function pushState(entry) {
    historyIndex++;
    history = history.slice(0, historyIndex);
    history.push(entry);
    if (history.length > MAX_HISTORY) {
      history.shift();
      historyIndex--;
    }
    updateUndoRedo();
    saveDraft(entry);
  }

  function saveState() {
    pushState({ snapshot: takeSnapshot(), blank: false });
  }

  function loadImage(blob) {
    if (typeof createImageBitmap === "function") {
      return createImageBitmap(blob);
    }
    return new Promise((resolve, reject) => {
      const url = URL.createObjectURL(blob);
      const img = new Image();
      img.onload = () => {
        URL.revokeObjectURL(url);
        resolve(img);
      };
      img.onerror = reject;
      img.src = url;
    });
  }

  async function showState(index) {
    const token = ++restoreToken;
    const entry = history[index];
    restoring = true;
    try {
      const blob = entry.snapshot ? await entry.snapshot : null;
      const image = blob ? await loadImage(blob) : null;
      if (token !== restoreToken) return;
      if (image) {
        ctx.drawImage(image, 0, 0, ACTUAL, ACTUAL);
      } else {
        paintPaper();
      }
      saveDraft(entry);
    } catch (err) {
      console.error("Couldn't restore drawing step:", err);
    } finally {
      if (token === restoreToken) restoring = false;
    }
  }

  function undo() {
    if (historyIndex > 0) {
      historyIndex--;
      updateUndoRedo();
      showState(historyIndex);
    }
  }

  function redo() {
    if (historyIndex < history.length - 1) {
      historyIndex++;
      updateUndoRedo();
      showState(historyIndex);
    }
  }

  // Clearing is its own history step, so Undo brings the drawing back
  function clear() {
    if (history[historyIndex].blank) return;
    restoreToken++;
    restoring = false;
    paintPaper();
    pushState({ snapshot: null, blank: true });
    hideDraftNote();
  }

  function updateUndoRedo() {
    const undoBtn = document.getElementById("btn-undo");
    const redoBtn = document.getElementById("btn-redo");
    const clearBtn = document.getElementById("btn-clear");
    if (undoBtn) {
      undoBtn.disabled = historyIndex <= 0;
    }
    if (redoBtn) {
      redoBtn.disabled = historyIndex >= history.length - 1;
    }
    if (clearBtn) {
      clearBtn.disabled = history[historyIndex].blank;
    }
  }

  // ── Draft ───────────────────────────────────────────────────
  // The current drawing is kept in localStorage so a back swipe, reload or
  // crashed tab doesn't lose it. Cleared on submit, on Clear, or after a week.
  const DRAFT_KEY = "guestbook-draft";
  const DRAFT_MAX_AGE = 7 * 24 * 60 * 60 * 1000;
  let draftTimer = null;
  let draftSeq = 0;

  function blobToDataURL(blob) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result);
      reader.onerror = reject;
      reader.readAsDataURL(blob);
    });
  }

  function saveDraft(entry) {
    const seq = ++draftSeq;
    clearTimeout(draftTimer);
    draftTimer = setTimeout(async () => {
      try {
        if (entry.blank) {
          localStorage.removeItem(DRAFT_KEY);
          return;
        }
        const blob = await entry.snapshot;
        if (!blob) return;
        const image = await blobToDataURL(blob);
        if (seq !== draftSeq) return;
        localStorage.setItem(DRAFT_KEY, JSON.stringify({ savedAt: Date.now(), image }));
      } catch (err) {
        // Storage full or blocked: the draft is only a safety net
      }
    }, 300);
  }

  function clearDraft() {
    draftSeq++;
    clearTimeout(draftTimer);
    try {
      localStorage.removeItem(DRAFT_KEY);
    } catch (err) {}
  }

  function showDraftNote() {
    const note = document.getElementById("draft-note");
    if (note) note.classList.remove("hidden");
  }

  function hideDraftNote() {
    const note = document.getElementById("draft-note");
    if (note) note.classList.add("hidden");
  }

  function restoreDraft() {
    let draft = null;
    try {
      draft = JSON.parse(localStorage.getItem(DRAFT_KEY));
    } catch (err) {
      return;
    }
    if (!draft || typeof draft.image !== "string" || !draft.image.startsWith("data:image/png")) return;
    if (!(Date.now() - draft.savedAt < DRAFT_MAX_AGE)) {
      clearDraft();
      return;
    }

    const img = new Image();
    img.onload = () => {
      // Someone who already started a new drawing keeps it
      if (historyIndex !== 0 || !history[0].blank || drawing) return;
      ctx.drawImage(img, 0, 0, ACTUAL, ACTUAL);
      pushState({ snapshot: takeSnapshot(), blank: false });
      showDraftNote();
    };
    img.src = draft.image;
  }

  // ── Drawing ─────────────────────────────────────────────────
  function getPos(e) {
    const rect = canvas.getBoundingClientRect();
    const scale = ACTUAL / rect.width;
    const touch = e.touches ? e.touches[0] : e;
    return {
      x: (touch.clientX - rect.left) * scale,
      y: (touch.clientY - rect.top) * scale,
    };
  }

  function startDraw(e) {
    e.preventDefault();
    if (restoring) return;
    drawing = true;
    const pos = getPos(e);
    lastX = pos.x;
    lastY = pos.y;

    // Draw a dot at the start point
    ctx.beginPath();
    ctx.arc(pos.x, pos.y, scaledBrush() / 2, 0, Math.PI * 2);
    ctx.fillStyle = tool === "eraser" ? "#fff" : color;
    ctx.fill();
  }

  function draw(e) {
    if (!drawing) return;
    e.preventDefault();
    const pos = getPos(e);

    ctx.beginPath();
    ctx.moveTo(lastX, lastY);
    ctx.lineTo(pos.x, pos.y);
    ctx.strokeStyle = tool === "eraser" ? "#fff" : color;
    ctx.lineWidth = scaledBrush();
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    ctx.stroke();

    lastX = pos.x;
    lastY = pos.y;
  }

  function endDraw(e) {
    if (!drawing) return;
    e.preventDefault();
    drawing = false;
    lastX = null;
    lastY = null;
    saveState();
  }

  function scaledBrush() {
    return brushSize * (ACTUAL / LOGICAL);
  }

  // Mouse events
  canvas.addEventListener("mousedown", startDraw);
  canvas.addEventListener("mousemove", draw);
  canvas.addEventListener("mouseup", endDraw);
  canvas.addEventListener("mouseleave", endDraw);

  // Touch events
  canvas.addEventListener("touchstart", startDraw, { passive: false });
  canvas.addEventListener("touchmove", draw, { passive: false });
  canvas.addEventListener("touchend", endDraw);
  canvas.addEventListener("touchcancel", endDraw);

  // ── Custom cursor ───────────────────────────────────────────
  const cursor = document.getElementById("cursor-circle") || document.querySelector(".gb-cursor");

  canvas.addEventListener("mouseenter", () => {
    if (cursor) cursor.style.display = "block";
  });
  canvas.addEventListener("mouseleave", () => {
    if (cursor) cursor.style.display = "none";
  });
  canvas.addEventListener("mousemove", (e) => {
    if (!cursor) return;
    cursor.style.left = e.clientX + "px";
    cursor.style.top = e.clientY + "px";
    updateCursorSize();
  });

  function updateCursorSize() {
    if (!cursor) return;
    const rect = canvas.getBoundingClientRect();
    const displayBrush = brushSize * (rect.width / LOGICAL);
    cursor.style.width = displayBrush + "px";
    cursor.style.height = displayBrush + "px";
  }

  // ── Toolbar ─────────────────────────────────────────────────
  function setTool(t) {
    tool = t;
    document.getElementById("btn-pen").classList.toggle("active", t === "pen");
    document
      .getElementById("btn-eraser")
      .classList.toggle("active", t === "eraser");
  }

  function setBrush(size) {
    brushSize = size;
    document
      .getElementById("btn-small")
      .classList.toggle("active", size === 3);
    document
      .getElementById("btn-medium")
      .classList.toggle("active", size === 9);
    document
      .getElementById("btn-large")
      .classList.toggle("active", size === 18);
    updateCursorSize();
  }

  const swatches = document.querySelectorAll(".gb-swatch");

  // Picking a colour also switches back to the pen
  function setColor(c) {
    color = c;
    swatches.forEach((s) => s.classList.toggle("active", s.dataset.color === c));
    setTool("pen");
  }

  swatches.forEach((s) => s.addEventListener("click", () => setColor(s.dataset.color)));
  document.getElementById("btn-pen").addEventListener("click", () => setTool("pen"));
  document.getElementById("btn-eraser").addEventListener("click", () => setTool("eraser"));
  document.getElementById("btn-small").addEventListener("click", () => setBrush(3));
  document.getElementById("btn-medium").addEventListener("click", () => setBrush(9));
  document.getElementById("btn-large").addEventListener("click", () => setBrush(18));
  document.getElementById("btn-undo").addEventListener("click", undo);
  document.getElementById("btn-redo").addEventListener("click", redo);
  document.getElementById("btn-clear")?.addEventListener("click", clear);

  // Set initial state
  setColor("#000000");
  setBrush(9);
  updateUndoRedo();
  restoreDraft();

  window.guestbookCanvas = {
    isBlank() {
      return history[historyIndex].blank;
    },
    clearDraft,
  };
})();

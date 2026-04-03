(() => {
  const canvas = document.getElementById("draw-canvas");
  if (!canvas) return;

  const ctx = canvas.getContext("2d", { willReadFrequently: true });
  const LOGICAL = 500;
  const ACTUAL = 1000;

  canvas.width = ACTUAL;
  canvas.height = ACTUAL;

  // Fill white
  ctx.fillStyle = "#fff";
  ctx.fillRect(0, 0, ACTUAL, ACTUAL);

  // ── State ───────────────────────────────────────────────────
  let tool = "pen"; // "pen" | "eraser"
  let brushSize = 9; // 3, 9, 18
  let drawing = false;
  let lastX = null;
  let lastY = null;

  // History
  let history = [ctx.getImageData(0, 0, ACTUAL, ACTUAL)];
  let historyIndex = 0;
  const MAX_HISTORY = 20;

  function saveState() {
    historyIndex++;
    history = history.slice(0, historyIndex);
    history.push(ctx.getImageData(0, 0, ACTUAL, ACTUAL));
    if (history.length > MAX_HISTORY) {
      history.shift();
      historyIndex--;
    }
    updateUndoRedo();
  }

  function undo() {
    if (historyIndex > 0) {
      historyIndex--;
      ctx.putImageData(history[historyIndex], 0, 0);
      updateUndoRedo();
    }
  }

  function redo() {
    if (historyIndex < history.length - 1) {
      historyIndex++;
      ctx.putImageData(history[historyIndex], 0, 0);
      updateUndoRedo();
    }
  }

  function updateUndoRedo() {
    const undoBtn = document.getElementById("btn-undo");
    const redoBtn = document.getElementById("btn-redo");
    if (undoBtn) {
      undoBtn.disabled = historyIndex <= 0;
      undoBtn.style.opacity = historyIndex > 0 ? "1" : "0.35";
    }
    if (redoBtn) {
      redoBtn.disabled = historyIndex >= history.length - 1;
      redoBtn.style.opacity = historyIndex < history.length - 1 ? "1" : "0.35";
    }
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
    drawing = true;
    const pos = getPos(e);
    lastX = pos.x;
    lastY = pos.y;

    // Draw a dot at the start point
    ctx.beginPath();
    ctx.arc(pos.x, pos.y, scaledBrush() / 2, 0, Math.PI * 2);
    ctx.fillStyle = tool === "eraser" ? "#fff" : "#000";
    ctx.fill();
  }

  function draw(e) {
    if (!drawing) return;
    e.preventDefault();
    const pos = getPos(e);

    ctx.beginPath();
    ctx.moveTo(lastX, lastY);
    ctx.lineTo(pos.x, pos.y);
    ctx.strokeStyle = tool === "eraser" ? "#fff" : "#000";
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

  document.getElementById("btn-pen").addEventListener("click", () => setTool("pen"));
  document.getElementById("btn-eraser").addEventListener("click", () => setTool("eraser"));
  document.getElementById("btn-small").addEventListener("click", () => setBrush(3));
  document.getElementById("btn-medium").addEventListener("click", () => setBrush(9));
  document.getElementById("btn-large").addEventListener("click", () => setBrush(18));
  document.getElementById("btn-undo").addEventListener("click", undo);
  document.getElementById("btn-redo").addEventListener("click", redo);

  // Set initial state
  setTool("pen");
  setBrush(9);
  updateUndoRedo();

  window.guestbookCanvas = {
    isBlank() {
      return historyIndex === 0;
    },
  };
})();

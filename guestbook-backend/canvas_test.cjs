const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const test = require("node:test");

function drawingCanvas() {
  const nodes = new Map();
  let ink = 0;
  const ctx = {
    fillRect() {}, beginPath() {}, arc() {}, moveTo() {}, lineTo() {},
    fill() { ink++; }, stroke() { ink++; },
    getImageData() { return { ink }; },
    putImageData(state) { ink = state.ink; },
  };
  function node(id) {
    if (!nodes.has(id)) nodes.set(id, {
      style: {}, classList: { toggle() {} }, events: {},
      addEventListener(event, handler) { this.events[event] = handler; },
      getBoundingClientRect() { return { left: 0, top: 0, width: 500 }; },
      getContext() { return ctx; },
    });
    return nodes.get(id);
  }
  const window = {};
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, "../guestbook/canvas.js"), "utf8"), {
    document: { getElementById: node }, window,
  });
  const background = ink;
  const event = { preventDefault() {}, clientX: 10, clientY: 10 };
  return {
    isBlank: () => window.guestbookCanvas.isBlank(),
    strokes: () => ink - background,
    draw() {
      node("draw-canvas").events.mousedown(event);
      node("draw-canvas").events.mouseup(event);
    },
    undo: () => node("btn-undo").events.click(),
    redo: () => node("btn-redo").events.click(),
  };
}

test("blank state follows undo and redo", () => {
  const canvas = drawingCanvas();
  assert.equal(canvas.isBlank(), true);
  canvas.draw();
  assert.equal(canvas.isBlank(), false);
  canvas.undo();
  assert.equal(canvas.strokes(), 0);
  assert.equal(canvas.isBlank(), true);
  canvas.redo();
  assert.equal(canvas.strokes(), 1);
  assert.equal(canvas.isBlank(), false);
});

test("exhausting truncated undo history still permits a nonblank drawing", () => {
  const canvas = drawingCanvas();
  for (let i = 0; i < 21; i++) canvas.draw();
  for (let i = 0; i < 19; i++) canvas.undo();
  assert.equal(canvas.strokes(), 2);
  assert.equal(canvas.isBlank(), false);
  canvas.draw();
  canvas.undo();
  assert.equal(canvas.strokes(), 2);
  assert.equal(canvas.isBlank(), false);
});

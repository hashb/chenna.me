
function setup() {
  createCanvas(500, 500);
}

function draw() {
  var r = map(mouseX, 0, 500, 0, 255);
  var g = map(mouseY, 0, 500, 0, 255);
  var b = map(mouseX * mouseY, 0, 500*500, 0, 255);
  var bg = color(r,g,b);
  background(bg);
  circle(mouseX, mouseY, 1);
}

function setup() {
  createCanvas(500, 500);
}

function draw() {
  background(55);
  fill(map(mouseX, 0, 500, 55, 255));
  noStroke();
  circle(mouseX, mouseY, 100);
}

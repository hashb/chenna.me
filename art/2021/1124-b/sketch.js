function setup() {
  createCanvas(900, 900);
}

function draw() {
  background(55);
  fill(map(mouseX, 0, 900, 55, 255));
  noStroke();
  circle(mouseX, mouseY, 100);
}

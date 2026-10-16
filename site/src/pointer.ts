// Moves the lit circle of pointer.css's dot grid to the pointer.
//
// Only where a pointer hovers (a mouse or trackpad, not a finger, which has
// nothing to follow), and never for people who ask for reduced motion: the
// page reads the same without it, so the layer just stays dark.
const layer = document.querySelector<HTMLElement>(".pointer-dots");
const hovers = matchMedia("(hover: hover) and (pointer: fine)").matches;
const still = matchMedia("(prefers-reduced-motion: reduce)").matches;

if (layer && hovers && !still) {
  let x = 0;
  let y = 0;
  let frame = 0;

  // One write per frame however fast events come. The layer spans the
  // document, so the circle sits at the pointer plus the scroll offset.
  const paint = () => {
    frame = 0;
    layer.style.setProperty("--px", `${x}px`);
    layer.style.setProperty("--py", `${y + window.scrollY}px`);
  };
  const queue = () => {
    if (!frame) frame = requestAnimationFrame(paint);
  };

  window.addEventListener(
    "pointermove",
    (event) => {
      x = event.clientX;
      y = event.clientY;
      layer.dataset.lit = "";
      queue();
    },
    { passive: true }
  );
  // Scrolling under a still pointer moves the page, not the pointer.
  window.addEventListener("scroll", queue, { passive: true });
  document.documentElement.addEventListener("pointerleave", () => {
    delete layer.dataset.lit;
  });
}

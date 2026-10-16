// The page's motion. The HTML already draws every final state, so this only
// adds movement: nothing here is needed to read the page, and none of it runs
// for people who ask for reduced motion.
//
// - arriving: content rises in as it scrolls into view (data-reveal);
// - the hero plays a run as the app shows it, on a loop, while on screen;
// - each feature's drawing plays once when its row arrives, some loop while
//   on screen, and where a pointer can hover some answer it;
// - the features rail marks the one in view.

const root = document.documentElement;
const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
const hovers = matchMedia("(hover: hover) and (pointer: fine)").matches;


// ---- Arriving ----

function reveal() {
  // The script runs after the first paint, so what is already on screen has
  // been seen: mark it arrived before motion switches on, or it would vanish
  // and fade back in. Only what is still below the fold waits to arrive.
  const fold = innerHeight - 80;
  const waiting: Element[] = [];
  document.querySelectorAll("[data-reveal]").forEach((el) => {
    if (el.getBoundingClientRect().top < fold) {
      el.classList.add("in");
      el.closest("[data-feature]")?.classList.add("go");
    } else {
      waiting.push(el);
    }
  });
  const seen = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        entry.target.classList.add("in");
        // A feature's drawing plays once its panel has arrived.
        entry.target.closest("[data-feature]")?.classList.add("go");
        seen.unobserve(entry.target);
      }
    },
    { rootMargin: "0px 0px -80px 0px" }
  );
  waiting.forEach((el) => seen.observe(el));
}

// Runs fn every ms while el is on screen and the tab is visible.
function whileVisible(el: Element, ms: number, fn: () => void) {
  let timer = 0;
  let onScreen = false;
  const sync = () => {
    const run = onScreen && !document.hidden;
    if (run && !timer) timer = window.setInterval(fn, ms);
    if (!run && timer) {
      clearInterval(timer);
      timer = 0;
    }
  };
  new IntersectionObserver(([entry]) => {
    onScreen = entry.isIntersecting;
    sync();
  }).observe(el);
  document.addEventListener("visibilitychange", sync);
}

// ---- The hero's run ----

// One picture per step, in the app's words (web/src/features/runs/
// runIndicator.ts): the status, its icon, the plan's counts once there is a
// plan, Discard and Approve while it waits, and the Terraform output the run
// keeps.
type Log = { text: string; tone: "code" | "subtle" | "success" };
type Frame = { word: string; icon: "spin" | "wait" | "done"; waiting?: boolean; diff?: boolean; actions?: boolean; press?: boolean; log: Log[]; hold: number };

const LOG = {
  init: { text: "Initializing the backend...", tone: "subtle" },
  ready: { text: "Terraform has been successfully initialized!", tone: "success" },
  plan: { text: "Plan: 3 to add, 1 to change, 0 to destroy.", tone: "code" },
  create: { text: "aws_eks_node_group.default: Creating...", tone: "subtle" },
  done: { text: "Apply complete! Resources: 3 added, 1 changed, 0 destroyed.", tone: "success" }
} satisfies Record<string, Log>;

const FRAMES: Frame[] = [
  { word: "planning · fetching source", icon: "spin", log: [], hold: 900 },
  { word: "planning · initializing", icon: "spin", log: [LOG.init], hold: 900 },
  { word: "planning", icon: "spin", log: [LOG.init, LOG.ready], hold: 900 },
  { word: "waiting for approval", icon: "wait", waiting: true, diff: true, actions: true, log: [LOG.init, LOG.ready, LOG.plan], hold: 1500 },
  { word: "waiting for approval", icon: "wait", waiting: true, diff: true, actions: true, press: true, log: [LOG.init, LOG.ready, LOG.plan], hold: 500 },
  { word: "approved", icon: "done", diff: true, log: [LOG.init, LOG.ready, LOG.plan], hold: 800 },
  { word: "applying", icon: "spin", diff: true, log: [LOG.ready, LOG.plan, LOG.create], hold: 1400 },
  { word: "applied", icon: "done", diff: true, log: [LOG.plan, LOG.create, LOG.done], hold: 3000 }
];

const TONE = { code: "text-code-foreground", subtle: "text-subtle-foreground", success: "text-success" };

function heroRun() {
  const run = document.querySelector<HTMLElement>("[data-run]");
  if (!run) return;
  const $ = <T extends Element>(sel: string) => run.querySelector<T>(sel)!;
  const $$ = (sel: string) => [...run.querySelectorAll(sel)];
  const status = $<HTMLElement>("[data-status]");
  const word = $<HTMLElement>("[data-word]");
  const diff = $<HTMLElement>("[data-diff]");
  const actions = $<HTMLElement>("[data-actions]");
  const approve = $<HTMLElement>("[data-approve]");
  const log = $<HTMLElement>("[data-log]");
  const icons = $$("[data-icon]") as SVGElement[];
  const plan = $$(".h-plan");
  const approver = $$(".h-approver");
  const slabs = $$(".h-slab");
  let lastLog = "";

  const show = (step: number) => {
    const f = FRAMES[step];
    run.dataset.step = String(step);
    // The drawing: the plan lights while planning, the approver while it
    // waits, then the infrastructure lands as it applies.
    plan.forEach((el) => el.classList.toggle("on", step <= 2));
    $(".h-arrow1").classList.toggle("on", step >= 3);
    approver.forEach((el) => {
      el.classList.toggle("on", step === 3 || step === 4);
      el.classList.toggle("done", step >= 5);
    });
    $(".h-badge").classList.toggle("off", step < 5);
    $(".h-arrow2").classList.toggle("on", step >= 6);
    $(".h-apply").classList.toggle("on", step === 6);
    $(".h-apply").classList.toggle("done", step === 7);
    slabs.forEach((el, i) => el.classList.toggle("off", step < (i === 0 ? 6 : 7)));
    // The run view.
    word.textContent = f.word;
    status.classList.toggle("text-warning", !!f.waiting);
    status.classList.toggle("text-muted-foreground", !f.waiting);
    // SVG has no .hidden property; the attribute works (preflight hides [hidden]).
    icons.forEach((icon) => icon.toggleAttribute("hidden", icon.dataset.icon !== f.icon));
    diff.hidden = !f.diff;
    actions.hidden = !f.actions;
    approve.classList.toggle("pressed", !!f.press);
    // Only redraw the log when it changed, so old lines don't replay.
    const key = f.log.map((l) => l.text).join("|");
    if (key !== lastLog) {
      lastLog = key;
      log.replaceChildren(
        ...f.log.map((l) => {
          const line = document.createElement("div");
          line.className = `truncate ${TONE[l.tone]}`;
          line.textContent = l.text;
          return line;
        })
      );
    }
  };

  let step = 7;
  let timer = 0;
  let onScreen = false;
  const tick = () => {
    step = (step + 1) % FRAMES.length;
    show(step);
    timer = window.setTimeout(tick, FRAMES[step].hold);
  };
  const sync = () => {
    const play = onScreen && !document.hidden;
    if (play && !timer) timer = window.setTimeout(tick, FRAMES[step].hold);
    if (!play && timer) {
      clearTimeout(timer);
      timer = 0;
    }
  };
  new IntersectionObserver(([entry]) => {
    onScreen = entry.isIntersecting;
    sync();
  }).observe(run);
  document.addEventListener("visibilitychange", sync);
}

// ---- Features ----

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

type Ctx = { row: HTMLElement; readout: HTMLElement; fx: number; fy: number };

// What each feature does under the pointer (hover) and while on screen
// (beat), in openplan's own terms.
const KINDS: Record<string, { hover?: (c: Ctx) => void; leave?: (c: Ctx) => void; beat?: (c: Ctx, n: number) => void }> = {
  register: {
    hover: (c) => (c.readout.textContent = "acme/terraform-aws-eks @ v1.4.2 → eks"),
    leave: (c) => (c.readout.textContent = "acme/terraform-aws-eks @ v1.4.2")
  },
  stack: {
    // The template under the pointer lifts out of the stack.
    hover: (c) => pickSlab(c, clamp(Math.floor(((c.fy - 0.18) / 0.64) * 3), 0, 2)),
    leave: (c) => pickSlab(c, 1)
  },
  config: {
    // The pointer sets the variable: the knob follows it along the track.
    hover: (c) => setKnob(c, clamp(50 + ((c.fx - 0.15) / 0.7) * 140, 57, 183)),
    leave: (c) => setKnob(c, 150)
  },
  run: {
    beat: (c, n) => {
      const phase = n % 3;
      c.row.querySelectorAll(".r-plan").forEach((el) => el.classList.toggle("on", phase === 0));
      c.row.querySelectorAll(".r-live, .r-core").forEach((el) => el.classList.toggle("live", phase === 1));
      c.readout.textContent = ["plan finished +3 ~1 −0", "applied +3 ~1 −0", "destroyed"][phase];
    }
  },
  logs: {
    beat: (c, n) => {
      const shown = n % 6;
      c.row.querySelectorAll<SVGElement>(".logline").forEach((el, i) => {
        el.classList.toggle("shown", i < shown);
        // The newest line is the one lit.
        const last = i === shown - 1;
        const dot = el.querySelector(".dot")!;
        dot.classList.toggle("fill-primary", last);
        dot.classList.toggle("stroke-primary", last);
        dot.classList.toggle("fill-card", !last);
        dot.classList.toggle("stroke-separator", !last);
        el.querySelector(".bar")!.classList.toggle("stroke-primary", last);
        el.querySelector(".bar")!.classList.toggle("stroke-separator", !last);
      });
      c.readout.textContent = [
        "waiting for an executor",
        "Initializing the backend...",
        "Initializing provider plugins...",
        "Terraform has been successfully initialized!",
        "Plan: 3 to add, 1 to change, 0 to destroy.",
        "Apply complete! Resources: 3 added, 1 changed, 0 destroyed."
      ][shown];
    }
  },
  upgrade: {
    hover: (c) => (c.readout.textContent = "eks v1.3 → v1.4"),
    leave: (c) => (c.readout.textContent = "eks v1.3 · v1.4 available")
  },
  access: {
    // Point at a stack to grant on it; on screen, the role cycles through
    // the four a stack has.
    hover: (c) => grant(c, clamp(Math.floor(c.fx * 3), 0, 2), "Operator"),
    leave: (c) => grant(c, 1, "Operator"),
    beat: (c, n) => {
      if (!c.row.classList.contains("hot")) grant(c, 1, ["Viewer", "Operator", "Approver", "Owner"][n % 4]);
    }
  },
  sso: {
    hover: (c) => (c.readout.textContent = "signing in through Dex"),
    leave: (c) => (c.readout.textContent = "GitHub · LDAP · OIDC → Dex")
  }
};

const STACKS = ["dev", "staging", "prod"];
const TEMPLATES = ["rds", "eks", "vpc"];

function pickSlab(c: Ctx, pick: number) {
  c.row.querySelectorAll<SVGElement>("[data-slab]").forEach((el) => {
    const up = Number(el.dataset.slab) === pick;
    el.classList.toggle("up", up);
    el.classList.toggle("stroke-primary", up);
    el.classList.toggle("stroke-muted-foreground", !up);
  });
  c.readout.textContent = `prod / ${TEMPLATES[pick]}`;
}

function setKnob(c: Ctx, x: number) {
  c.row.querySelector("[data-knob]")!.setAttribute("cx", x.toFixed(1));
  c.row.querySelector("[data-bar]")!.setAttribute("d", `M50 70 H${x.toFixed(1)}`);
  c.readout.textContent = `node_count = ${Math.round(1 + ((x - 57) / 126) * 9)}`;
}

function grant(c: Ctx, pick: number, role: string) {
  const x = [54, 120, 186][pick];
  c.row.querySelectorAll<SVGElement>("[data-stack]").forEach((el) => el.classList.toggle("picked", Number(el.dataset.stack) === pick));
  c.row.querySelector("[data-grant]")!.setAttribute("d", `M120 56 C120 72 ${x} 68 ${x} 84`);
  c.readout.textContent = `ana → ${STACKS[pick]}: ${role}`;
}

function features() {
  const rows = [...document.querySelectorAll<HTMLElement>("[data-feature]")];
  const links = [...document.querySelectorAll<HTMLElement>(".rail-link")];
  const fill = document.querySelector<HTMLElement>(".rail-fill");

  rows.forEach((row) => {
    const kind = KINDS[row.dataset.kind ?? ""] ?? {};
    const ctx: Ctx = { row, readout: row.querySelector<HTMLElement>("[data-readout]")!, fx: 0.5, fy: 0.5 };
    const glow = row.querySelector<HTMLElement>(".glow")!;

    if (kind.beat) {
      let n = 0;
      whileVisible(row, 1100, () => kind.beat!(ctx, ++n));
    }
    if (!hovers) return;
    glow.addEventListener("pointermove", (e) => {
      const box = glow.getBoundingClientRect();
      const x = e.clientX - box.left;
      const y = e.clientY - box.top;
      // The border light follows the pointer.
      glow.style.setProperty("--gx", `${x}px`);
      glow.style.setProperty("--gy", `${y}px`);
      glow.classList.add("lit");
      row.classList.add("hot");
      ctx.fx = clamp(x / box.width, 0, 1);
      ctx.fy = clamp(y / box.height, 0, 1);
      kind.hover?.(ctx);
    });
    glow.addEventListener("pointerleave", () => {
      glow.classList.remove("lit");
      row.classList.remove("hot");
      kind.leave?.(ctx);
    });
  });

  // The rail marks the row nearest the middle of the window, and on a wide
  // screen with a pointer the panels drift against the scroll a little.
  const parallax = hovers ? rows.map((row) => row.querySelector<HTMLElement>("[data-parallax]")) : [];
  let frame = 0;
  const onScroll = () => {
    frame = 0;
    const mid = innerHeight / 2;
    let current = -1;
    let best = Infinity;
    rows.forEach((row, i) => {
      const box = row.getBoundingClientRect();
      const off = box.top + box.height / 2 - mid;
      if (Math.abs(off) < best && box.bottom > 0 && box.top < innerHeight) {
        best = Math.abs(off);
        current = i;
      }
      const panel = parallax[i];
      if (panel && innerWidth >= 1024) panel.style.transform = `translateY(${clamp(-off * 0.07, -28, 28).toFixed(1)}px)`;
    });
    rows.forEach((row, i) => row.classList.toggle("current", i === current));
    links.forEach((link, i) => {
      link.classList.toggle("current", i === current);
      link.classList.toggle("passed", current >= 0 && i < current);
    });
    fill?.style.setProperty("--fill", current < 0 ? "0%" : `${((current + 1) / rows.length) * 100}%`);
  };
  addEventListener("scroll", () => {
    if (!frame) frame = requestAnimationFrame(onScroll);
  }, { passive: true });
  onScroll();
}

// Start last: the tables above must exist before anything reads them.
if (!still) {
  reveal();
  root.classList.add("motion");
  if (hovers) root.classList.add("pointer");
  heroRun();
  features();
}

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const SITE = join(dirname(fileURLToPath(import.meta.url)), "..");
const read = (path: string) => readFileSync(join(SITE, path), "utf8");

const HTML = read("index.html");
const CSS = read("src/styles.css");
const DRAWINGS = read("src/drawings.css");
const THEME = read("../web/src/styles/theme.css");

const FEATURES = HTML.split("<li data-feature").slice(1).map((chunk) => chunk.split("</li>")[0]);
const stripComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, "");

describe("site/index.html", () => {
  it("holds no colour literal", () => {
    for (const source of [HTML, CSS, DRAWINGS]) {
      expect(source).not.toMatch(/#[0-9a-f]{3,8}\b/i);
      expect(source).not.toMatch(/\b(?:rgba?|hsla?|oklch)\(/i);
    }
    const paint = [...HTML.matchAll(/\b(?:fill|stroke)="([^"]*)"/g)].map((m) => m[1]);
    expect(paint.filter((value) => value !== "none" && value !== "currentColor")).toEqual([]);
    // Arbitrary values and inline styles are the other ways a colour gets in.
    expect(HTML).not.toMatch(/-\[(?!\d)[a-z]+\]/i);
    expect(HTML).not.toMatch(/style="[^"]*(?:color|fill|stroke)/i);
    // styles.css only imports: anything else in it is a rule the guards here do not read.
    const rules = stripComments(CSS).split("\n").map((line) => line.trim()).filter(Boolean);
    expect(rules.filter((line) => !line.startsWith("@import "))).toEqual([]);
  });

  it("names no colour in drawings.css but theme properties and transparent", () => {
    // Every word in a declaration's value must be a known non-colour keyword,
    // so a named colour (red, white, ...) cannot slip in.
    const keywords = new Set([
      "transparent", "none", "important", "infinite", "linear", "ease", "ease-in-out", "ease-out",
      "fill-box", "radial-gradient", "translatex", "translatey", "scale", "scalex", "to", "px", "s",
      // Motion and layout words: easings, transforms, clip and gradient shapes,
      // the properties a transition names, and layout keywords.
      "cubic-bezier", "steps", "end", "both", "inset", "rotate", "deg", "calc", "circle", "at", "center",
      "opacity", "transform", "stroke", "fill", "color", "height", "cx", "d", "left", "var",
      "absolute", "hidden", "solid", "ellipsis", "nowrap", "block", "smooth"
    ]);
    const values = [...stripComments(DRAWINGS).matchAll(/[\w-]+\s*:\s*([^;{}]+)[;}]/g)]
      .map((m) => m[1].replace(/var\(--[\w-]+\)/g, ""));
    expect(values.length).toBeGreaterThan(0);
    const words = values.flatMap((value) => value.toLowerCase().match(/[a-z][a-z-]*/g) ?? []);
    expect(words.filter((word) => !keywords.has(word) && !word.startsWith("a-"))).toEqual([]);
  });

  it("loops nothing on its own: endless animation only under the pointer, or the run's spinner", () => {
    // WCAG 2.2.2: motion that starts by itself stops within five seconds or
    // can be stopped. The hero's run has a button; everything else here
    // plays a fixed number of times unless the pointer is on it.
    const rules = [...stripComments(DRAWINGS).matchAll(/([^{}]+)\{([^{}]*)\}/g)];
    const endless = rules.filter(([, , body]) => /\binfinite\b/.test(body)).map(([, sel]) => sel.trim());
    expect(endless.length).toBeGreaterThan(0);
    expect(endless.filter((sel) => !/\.hot\b|^\.spin$/.test(sel))).toEqual([]);
    expect(HTML).toMatch(/<button\b[^>]*\bdata-replay\b[^>]*\bhidden>/);
  });

  it("stops every drawing animation for people who ask for reduced motion", () => {
    const block = stripComments(DRAWINGS).split(/@media\s*\(prefers-reduced-motion:\s*reduce\)/)[1] ?? "";
    expect(block).toMatch(/animation:\s*none/);
  });

  it("uses only theme colours that theme.css defines", () => {
    const used = new Set(
      [...HTML.matchAll(/\b(?:bg|text|border|fill|stroke)-([a-z]+(?:-[a-z]+)*)\b/g)]
        .map((m) => m[1])
        .filter((name) => !/^(?:none|xs|sm|lg|xl|base|meta|row-title|page-title|panel-title|center|left|t|b|l|r|x|y|width|dasharray|linecap|linejoin)$/.test(name))
    );
    const defined = new Set([...THEME.matchAll(/--color-([a-z-]+):/g)].map((m) => m[1]));
    expect([...used].filter((name) => !defined.has(name))).toEqual([]);
  });

  it("hides every drawing from assistive technology and puts no words in one", () => {
    const svgs = [...HTML.matchAll(/<svg\b[^>]*>/g)].map((m) => m[0]);
    expect(svgs.length).toBeGreaterThan(0);
    expect(svgs.filter((tag) => !tag.includes('aria-hidden="true"'))).toEqual([]);
    expect(HTML).not.toMatch(/<text\b/);
  });

  it("gives every image alternative text", () => {
    const imgs = [...HTML.matchAll(/<img\b[^>]*>/g)].map((m) => m[0]);
    expect(imgs.filter((tag) => !/\balt="[^"]+"/.test(tag))).toEqual([]);
  });

  it("ships no JavaScript but its own motion module and the Cloudflare Web Analytics beacon", () => {
    const scripts = [...HTML.matchAll(/<script\b[^>]*>[\s\S]*?<\/script>/g)].map((m) => m[0]);
    expect(scripts).toHaveLength(2);
    // Bundled by Vite with the page: nothing of ours loads from another host.
    expect(scripts[0]).toBe('<script type="module" src="/src/alive.ts"></script>');
    expect(scripts[1]).toMatch(/\bdefer\b/);
    expect(scripts[1]).toContain('src="https://static.cloudflareinsights.com/beacon.min.js"');
    expect(scripts[1]).toContain(`data-cf-beacon='{"token": "e93881027b4c4ef99c377908d9f44d19"}'`);
    expect(scripts[1]).toMatch(/><\/script>$/);
  });

  it("reads complete without the script: motion only hides what .motion gates", () => {
    // Every rule that hides or offsets arriving content needs .motion, which
    // alive.ts adds only when people have not asked for reduced motion.
    const rules = [...stripComments(DRAWINGS).matchAll(/([^{}]+)\{([^{}]*)\}/g)];
    const hiding = rules.filter(([, sel, body]) => sel.includes("[data-reveal]") && /opacity:\s*0\b|translate/.test(body));
    expect(hiding.length).toBeGreaterThan(0);
    for (const [, sel] of hiding) expect(sel.trim()).toMatch(/^\.motion\b/);
    // The hero stands at the end of its run: applied.
    expect(HTML).toMatch(/data-run data-step="7"/);
    expect(HTML).toContain("<span data-word>applied</span>");
    const script = read("src/alive.ts");
    expect(script).toContain('matchMedia("(prefers-reduced-motion: reduce)")');
    expect(script).toContain('matchMedia("(hover: hover) and (pointer: fine)")');
  });

  it("lists the eight features, each drawing with a primary accent", () => {
    expect(FEATURES).toHaveLength(8);
    for (const feature of FEATURES) {
      const drawing = feature.split("<svg")[1]?.split("</svg>")[0] ?? "";
      expect(drawing).not.toBe("");
      expect(drawing).toMatch(/\b(?:stroke|fill)-primary(?![\w-])/);
      expect(feature).toMatch(/<h3\b[^>]*>[^<]+<\/h3>/);
    }
  });

  it("numbers and lists none of the features beside them: the bar is decoration", () => {
    expect(HTML).not.toMatch(/>0\d \/ 08</);
    const rail = HTML.match(/<div class="rail\b[^"]*"[^>]*>[\s\S]*?<\/div><\/div>/)?.[0] ?? "";
    expect(rail).toContain('aria-hidden="true"');
    expect(rail.replace(/<[^>]+>/g, "").trim()).toBe("");
  });

  it("leads the hero with the way to try it: a link down to the setup steps", () => {
    const hero = HTML.split("<main")[1]?.split("</section>")[0] ?? "";
    const links = [...hero.matchAll(/<a\b[^>]*\bhref="([^"]+)"/g)].map((m) => m[1]);
    expect(links[0]).toBe("#run");
    expect(HTML).toMatch(/<section id="run"\s/);
  });

  it("closes on a call to action that links to the README's setup steps", () => {
    const main = HTML.split("<main")[1]?.split("</main>")[0] ?? "";
    const cta = main.split("</section>").at(-2) ?? "";
    expect(cta).toContain("docker compose -f docker-compose.release.yaml up -d");
    expect(cta).toMatch(/<a\b[^>]*\bhref="https:\/\/github\.com\/openplanhq\/openplan#running-it-locally"/);
  });

  it("shows the brand kit's logo, not the name retyped, in the header and the footer", () => {
    for (const part of [HTML.split("<header")[1]?.split("</header>")[0], HTML.split("<footer")[1]]) {
      expect(part).toMatch(/<img\b[^>]*\bsrc="\/logo\.svg"[^>]*\balt="openplan"/);
    }
    expect(read("public/logo.svg")).toBe(read("../brand/svg/openplan-logo-horizontal.svg"));
  });

  it("downloads every file the README's no-checkout steps download, from the latest release", () => {
    const files = (text: string) => [...text.matchAll(/curl -LO \S*\/([^/\s]+)$/gm)].map((m) => m[1]);
    const readme = read("../README.md").split("### Without a checkout")[1]?.split("\n### ")[0] ?? "";
    expect(files(readme).length).toBeGreaterThan(0);
    expect(files(HTML)).toEqual(files(readme));
    expect(HTML).toContain("url=https://github.com/openplanhq/openplan/releases/latest/download\n");
  });

  it("ends on a footer whose painting exists in public/", () => {
    const footer = HTML.split("<footer")[1] ?? "";
    expect(footer).toMatch(/<img\b[^>]*\bsrc="\/footer-train\.jpg"/);
    expect(() => read("public/footer-train.jpg")).not.toThrow();
    // The lighter encodings the <picture> offers exist too, and the
    // painting waits until the reader nears the foot of the page.
    for (const kind of ["avif", "webp"]) {
      expect(footer).toContain(`srcset="/footer-train.${kind}" type="image/${kind}"`);
      expect(() => read(`public/footer-train.${kind}`)).not.toThrow();
    }
    expect(footer).toMatch(/<img\b[^>]*\bloading="lazy"/);
    expect(footer).toContain("Apache 2.0");
    // Honest about maturity: the warning says where to read what works.
    expect(HTML).toContain('href="https://github.com/openplanhq/openplan#maturity"');
    expect(footer).toContain('href="https://github.com/openplanhq/openplan#documentation"');
    const readme = read("../README.md");
    expect(readme).toMatch(/^## Maturity$/m);
    expect(readme).toMatch(/^## Documentation$/m);
    expect(footer).toContain("Painting by J. M. W. Turner");
  });

  it("gives link previews the brand kit's social card at an absolute URL", () => {
    const meta = (key: string) =>
      HTML.match(new RegExp(`<meta (?:property|name)="${key}" content="([^"]*)"`))?.[1];
    expect(meta("og:image")).toBe("https://openplan.run/social-preview.png");
    expect(read("public/social-preview.png")).toBe(read("../brand/social/openplan-social-preview.png"));
    expect(meta("og:title")).toBe(HTML.match(/<title>([^<]*)<\/title>/)?.[1]);
    expect(meta("og:description")).toBe(meta("description"));
    expect(meta("twitter:card")).toBe("summary_large_image");
  });
});

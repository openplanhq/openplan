# openplan website: one landing page

A public page that says what openplan is and lists its features, in the app's
design language. Designed on the "openplan website" canvas
(https://claude.ai/artifact/XcwSGV8i1Ytki2i29qFUtS).

**Out of scope:** docs pages, and a dark theme (the app is light only).

## The page

- **Top bar:** the wordmark, and a GitHub outline button.
- **Hero:** headline, the README's pitch, "View on GitHub", the "Not production
  ready" note; beside it a drawing of a dashed plan, a person approving it, and
  solid applied infrastructure.
- **Features:** a centred intro, then eight stacked rows (register templates,
  compose stacks, configure per stack, plan/apply/destroy, runs and logs,
  upgrade deliberately, access per stack, SSO), drawing and text alternating
  sides. Each row: a drawing on a dot grid, an "NN / 08" counter, a title and a
  sentence from the README. The drawings animate in CSS
  (`src/drawings.css`) and hold still under `prefers-reduced-motion: reduce`.
- **Call to action:** "Run it on your own machine", the release compose
  command, and a link to the README's "Running it locally".
- **Footer:** a 300px band of a J. M. W. Turner study (public domain, via
  Artvee, credited), then the wordmark, GitHub and Releases links, and the
  licence line.

## Rules

- Colours, type, spacing and radii come only from `web/src/styles/theme.css`,
  imported as is. No colour literal in `site/`; nothing in `web/` changes.
- Drawings are inline SVG, `aria-hidden`, no words: strokes 1.5 in
  `muted-foreground` (subject), `separator` (secondary), `dashed-border`
  (connectors), shapes filled `card`, one `primary` accent marking the point.
- No JavaScript ships: plain Vite and Tailwind, no framework.

## Build and checks

`site/` holds `index.html`, `src/styles.css` (imports only), `src/drawings.css`, the painting in `public/`, and
`src/page.test.ts`, which guards the rules above. CI runs `npm test` and
`npm run build` in a `site` job beside `web`.

**Hosting:** GitHub Pages at https://openplan.run (GoDaddy DNS), deployed by
`.github/workflows/site.yml` on pushes to `main` that touch `site/` or the theme.

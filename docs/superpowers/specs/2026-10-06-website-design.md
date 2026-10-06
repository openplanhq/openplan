# openplan website: one landing page

A public page that says what openplan is and lists its features, in the app's
design language. Designed on the "openplan website" canvas
(https://claude.ai/artifact/XcwSGV8i1Ytki2i29qFUtS).

**Out of scope:** docs pages, a dark theme (the app is light only), and
deploying it.

## The page

- **Top bar:** the wordmark, and a GitHub outline button.
- **Hero:** headline, the README's pitch, "View on GitHub", the "Not production
  ready" note; beside it a drawing of a dashed plan, a person approving it, and
  solid applied infrastructure.
- **Features:** eight cards (register templates, compose stacks, configure per
  stack, plan/apply/destroy, runs and logs, upgrade deliberately, access per
  stack, SSO), each a drawing over a title and a sentence from the README.
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

`site/` holds `index.html`, `src/styles.css`, the painting in `public/`, and
`src/page.test.ts`, which guards the rules above. CI runs `npm test` and
`npm run build` in a `site` job beside `web`.

**Open:** hosting and domain. If served from a subpath, set Vite's `base`.

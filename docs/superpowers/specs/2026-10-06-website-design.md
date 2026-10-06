# openplan website: one landing page

## Goal

A public page that says what openplan is and lists its features, in the same
design language as the app. One page, static, nothing to run.

The design is settled on the "openplan website" canvas
(https://claude.ai/artifact/XcwSGV8i1Ytki2i29qFUtS, artboard Home). This spec
records what it holds and how `site/` builds it.

## Scope

In:

- One page: top bar, hero, features, footer.
- Built from `site/` in this repo, as static HTML and CSS with no JavaScript.
- A CI job that tests and builds it.

Out, for now:

- Docs pages, a pricing or comparison page, search, a blog.
- A dark theme: the app's theme is light only, by design.
- Deploying it. Where it is hosted, and under which domain, is a separate
  decision (see Open questions).

## The page

**Top bar.** The `openplan` wordmark at the left; a GitHub outline button at the
right. Matches the app's TopBar: card ground, a 1px bottom border.

**Hero.** Two columns that stack on a phone.

- Left: the headline "Terraform for your whole team, on your own
  infrastructure.", the README's one-paragraph pitch, a primary "View on GitHub"
  button, and the maturity note: a `triangle-alert` icon in `warning` and "Not
  production ready. openplan is an MVP for local development and evaluation."
- Right: an abstract drawing of openplan's promise, in a bordered card on the
  canvas ground: a dashed plan, an arrow, a person in a blue ring with a blue
  check, an arrow, solid applied infrastructure.

**Features.** A "Features" heading and one line under it ("Everything a team
needs to share Terraform without sharing a laptop."), then eight cards in a grid
of four columns that falls to three, two and one as the page narrows. Each card
is a drawing above a title and one or two sentences from the README:

| Feature | Drawing |
| --- | --- |
| Register templates | one module fans out to three copies, the middle one blue |
| Compose stacks | three layers in a dashed container, the middle one blue |
| Configure per stack | three sliders at different values, one blue |
| Plan, apply and destroy | a dashed outline, an arrow, a solid block filled blue |
| Watch runs and read logs | a timeline of log lines, the latest blue |
| Upgrade deliberately | an old version, a gate, the new version in blue |
| Control access per stack | a person joined to one blue stack between two dashed ones |
| Sign in with SSO | three provider shapes converge on one blue door |

**Footer.** A full-width band, 300px tall, of the steam-train watercolour
(`object-fit: cover`, the train and viaduct kept in view). Under it, on the card
ground: the wordmark with "Self-hosted, open-source Terraform for your whole
team.", links to GitHub and Releases, and "Open source under the Apache 2.0
licence".

## Visual language

- **Tokens only.** Colour, type, spacing and radius come from
  `web/src/styles/theme.css`, the app's own theme, imported as is. No colour
  literal appears in `site/`.
- **The drawings.** Inline SVG on a 240 × 140 grid (the hero 520 × 230).
  Strokes 1.5, round caps and joins, no shadows or gradients. Three tones, as
  Tailwind's theme-colour utilities: `stroke-muted-foreground` for the shape a
  drawing is about, `stroke-separator` for secondary shapes and dashed outlines,
  `stroke-dashed-border` for connectors. Exactly one `primary` accent per
  drawing marks the point of the feature. Shapes are filled `fill-card` so they
  cover what is behind them.
- **No words in drawings,** and every drawing is `aria-hidden`: the card's title
  and text say what it means.
- Status is an icon and a word, never a coloured dot.

## How `site/` is built

```
site/
  package.json      vite, tailwindcss, @tailwindcss/vite, the two Geist fontsource packages, vitest
  vite.config.ts    the Tailwind plugin; builds index.html to dist/
  index.html        the whole page, with its SVGs inline
  src/styles.css    imports tailwindcss, the Geist fonts and ../../web/src/styles/theme.css
  public/           the footer painting
  src/page.test.ts  the guards below
```

- No React. The page has no state and no behaviour, so it ships no JavaScript.
  This replaces the earlier plan to import `web/`'s React components: the
  product snippets they would have drawn were replaced by the drawings.
- `theme.css` is imported across the boundary, read only. Nothing in `web/`
  changes, and `web/`'s guard tests do not scan `site/`.
- The painting is served from `site/public/`, about 320 KB at 1800 × 796.

## Testing

`site/src/page.test.ts`, run by `npm test`:

- `index.html` and `styles.css` hold no colour literal (hex, `rgb(`, `hsl(`,
  named colours in `fill`/`stroke`).
- Every `<svg>` is `aria-hidden="true"` and holds no `<text>`.
- Every `<img>` has non-empty `alt`.
- Every feature drawing has a primary accent (at least one element with a
  `stroke-primary` or `fill-primary` class). That the accent marks one thing,
  say a ring and the dot inside it, is checked in review, not by the test.

`npm run build` must pass. Before review, screenshots at 1280 and 375 wide go in
the PR.

CI: a `site` job in `.github/workflows/ci.yml`, beside the `web` job: `npm ci`,
`npm test`, `npm run build` in `site/`.

## Open questions

- **The painting's credit and licence.** Its artist, title and source are not
  known yet. It must not ship until they are, with a credit line in the footer
  and a licence that allows it.
- **Hosting and domain.** GitHub Pages from a workflow is the simplest; a domain
  needs a DNS record. Decided before a deploy workflow is added.

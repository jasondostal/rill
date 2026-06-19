# rill — brand assets

> *a tiny stream, flowing memory.*

Hand-built pixel art. The mark is a single stream carving an **S** downhill,
carrying glowing **memory-motes** in the seven-kind spectrum — the same OKLCH
hues the app uses for `decision / preference / insight / procedure / fact /
identity / rule`. Stones in the bed are the graph nodes; the current is memory
that survives between sessions.

## Assets

| File | What it is |
|------|------------|
| `mark-clean.png/svg` | The logo mark — flowing stream + motes, no stones. Primary icon. |
| `mark-stones.png/svg` | Mark with stones in the bed (more detail; used in the banner). |
| `wordmark.png/svg` | Pixel `rill` wordmark — blue→violet gradient, gold mote on the `i`. |
| `banner.png/svg` | README hero — icon tile + wordmark + mono tagline. |
| `og-card.png/svg` | 1280×640 GitHub social-preview card. |
| `favicon-16/32.png/svg` | Favicon-tuned bolder variants (motes survive at 16px). |

Shipped favicons live in `frontend/static/` (`favicon.ico`, `favicon.png`,
`apple-touch-icon.png`) and are wired in `frontend/src/app.html`.

## Palette

Near-black blue `oklch(0.175 0.018 265)` field, electric-blue accent
`oklch(0.70 0.19 255)` → violet `oklch(0.56 0.20 290)`, foam highlight
`oklch(0.89 0.085 220)`. Motes are the app's `--k-*` kind hues. Type is
JetBrains Mono.

## Regenerating

All assets are emitted from one script — crisp pixel SVG rasterized by the
frontend's own Chromium (Playwright), so `oklch()` renders exactly like the UI:

```bash
node docs/brand/build.mjs        # run from the repo root
```

Edit the grids / palette in `build.mjs` and re-run. PNGs are pixel-perfect
upscales of the SVGs; the SVGs are the source of truth.

# Rill Sidecar

A native macOS **menu-bar memory client** for [Rill](../). Click the droplet (or
hit **⌥Space**) and a compact popover drops down to capture a memory in one
keystroke, link entities, browse recent memories, search (entity-anchored), and
manage connection + appearance.

Built per `../design_handoff_rill_sidecar/` — Variation A ("menu-bar drop").

## Stack

- **SwiftUI + AppKit**, single native binary. No Electron, no webview, no dev
  server. ~1–2 MB. `NSStatusItem` + `NSPopover` host the SwiftUI views.
- **No Xcode required** — builds with SwiftPM + Command Line Tools (`swiftc`).
- Talks **directly** to the Rill REST API over `URLSession` (Bearer PAT). Native
  client → no CORS, no proxy.
- The real **Lucide "droplets"** mark is parsed from its SVG path data
  (`Icon.swift`) and used for both the menu-bar template icon and the in-app
  logo, so they're identical.
- Rill's **OKLCH theme engine** is ported to Swift (`Theme.swift`) — the same 8
  presets + tune sliders as the web app, computed natively (no CSS).

## Build & run

```bash
cd sidecar
./build.sh                 # SwiftPM release build -> RillSidecar.app (ad-hoc signed)
open RillSidecar.app       # droplet appears in the menu bar
```

`build.sh` seeds `RILL_HOST` + `RILL_TOKEN` from `.env` into the app's prefs
domain on first build (only if unset — Settings edits win thereafter). You can
also set host/token in **Settings → Connection**.

A breadcrumb log is written to `/tmp/rill-sidecar.log` (and the unified log) —
handy since a menu-bar popover has no visible console.

## Layout

```
Sources/RillSidecar/
  App.swift          NSStatusItem + NSPopover shell, ⌥Space Carbon hotkey
  Icon.swift         Lucide droplets SVG-path parser -> CGPath / template image
  Theme.swift        OKLCH -> sRGB engine, 8 presets, derived color tokens
  API.swift          URLSession REST client + Codable wire models
  Store.swift        AppState (config, data, capture draft, deferred-delete undo)
  Views/             RootView, CaptureView, SearchView, SettingsView, EmptyState,
                     MemoryRowView, Components (KindChip, EntityChip, Logo, …)
```

## Status

Working: menu-bar droplet, popover home (capture + 8-kind selector + recent
list), live load from rill, entity linking (chips) + inline entity creation +
a manual **relate-builder** (subject→verb→object edges sent with the memory),
**tap-to-expand** rows (full details + mentioned-entity chips, fetched on
demand) with **inline edit** of summary/details, search (entity-anchored recall
+ kind filter + "anchored on" entity chips), pin/forget with 5s deferred-delete
undo, settings (host/token/test, theme presets + sliders, capture defaults,
launch-at-login), **cross-device theme + capture-defaults sync** via
`/api/settings`, **poll-while-open** (re-fetch every 15s while the panel is up),
⌥Space global hotkey, empty state.

> **Known server-side gap:** `POST /api/recall` returns an empty `entities[]`
> because `recall.entitiesMentionedBy` interpolates memory ids as JSON strings
> into `WHERE in IN [...]` (SurrealDB `in` is a record link — strings never
> match). The sidecar's "anchored on" chips are wired and correct; they'll
> populate once that query builds the IN list as record ids (as the
> memory-detail path already does via `normalizeMemoryID`).

### Future / TODO
- **Inline @-mention pills** — entity links are currently chips below the field
  via a popover picker (native adaptation). True inline tokens need an
  `NSTextView`/TextKit wrapper.
- **Relationship preview** — surface rill's server-extracted triples on a
  captured memory so the user can confirm/correct (the relate-builder already
  covers the manual-hint direction; the design says don't reimplement the
  detection regex — the two-pass extractor owns that).
- **Stable code signature** — ad-hoc signing changes each build; a self-signed
  identity would let macOS remember permissions across rebuilds.

## Note

The prior SvelteKit+WKWebView prototype (Mimo 2.5 Pro) was replaced by this
native rebuild. It's archived at `_mimo-archive.tgz` if ever needed.

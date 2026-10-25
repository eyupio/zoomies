# AGENTS.md

Scoped guidance for the UI, read in addition to the root [AGENTS.md](../AGENTS.md). It appends to that file and never overrides it.

## The UI

`web/` is Svelte 5 (runes), Tailwind v4, Vite, TypeScript, built straight into
`internal/api/webdist` and embedded.

- **Never write a raw colour in a component, and never write a raw value that
  already has a token.** All design tokens live in
  `web/src/lib/styles/tokens.css`. A colour written by hand only works in one
  theme, so that half is absolute; the rest is a rule about repetition, and a
  value that appears twice belongs in the token file. Media query widths, a
  one-off measure in a component's own layout, and the log viewer's xterm
  bridge are the documented exceptions.
  [docs/ui-guidelines.md](../docs/ui-guidelines.md) is the contract, and UI
  changes should keep it true.
- Status colours are a fixed mapping (idle, busy, pending, draining, danger,
  neutral). Operators learn them; do not reuse them for anything else.
- No state-management library (runes are it), no client-side router
  (`web/src/lib/router.ts` is ours), no charting library (sparklines and bars are
  inline SVG). These are deliberate, see `docs/dependencies.md`.
- Nothing is reachable from the UI that is not reachable from the REST API. If a
  page needs data, it comes from a documented route in
  [docs/api-surface.md](../docs/api-surface.md).
- Playwright specs in `web/tests/` run against the real binary, including
  accessibility and mobile passes.
- A spec that rewrites a read the page also receives as an event-stream frame
  takes the stream away first (`route.abort('connectionfailed')` on
  `**/api/v1/events*`). A frame lands whenever the fleet changes, which in a long
  run on one shared controller is any moment, and it replaces what the test
  wrote. Measured on an idle fixture controller: every thirty seconds a
  `host.updated` for each host, two or three `runner.updated` and a `stats`,
  and the Kennel summary once, about ten minutes in; pools and providers send
  nothing until something changes. A route handler that rewrites a response reads it through a guard, since
  a page that asks again abandons the read the handler is waiting on.
- `npm run check` type-checks `web/unit/` as well (`tsconfig.unit.json`). The unit
  tests run on Node with types stripped, so a fixture that has fallen behind the
  API's shape would otherwise go on passing; build a fixture to the generated
  type and do not cast it.

### The component gallery

`web/gallery.html` plus `web/src/gallery/` is a development-only page that
renders the real components in the states a healthy fleet never shows: disabled
and refused controls, `aria-disabled` reasons, focus rings, skeletons, empty
and error states, every status shape, drawers and all four data-grid states. It
is its own Vite entry, so `vite build` never builds it and the embedded UI never
contains it; do not add it to the router or link it from the app.

Open it with `make gallery` (or `npm run dev`) and browse to
`http://127.0.0.1:5173/gallery.html?style=mission-control&mode=dark`. The
style and colour mode come from the query string, so a combination is
something a capture or a test can name, and neither is written to the
browser's real preferences. `web/tests/gallery.spec.ts` runs every combination
against a Vite dev server (`tests/support/serve-gallery.mjs`), and
`make screenshots-gallery` refreshes the captures in
`design/theme-review/gallery/`.

When a primitive gains a state, add it here before adding a route that can
reach it. Do not create a second copy of a component for the gallery: it
imports from `$lib/components` and nothing else is acceptable.

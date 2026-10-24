# E3 cache preview: the web page

Date: 23 October 2026. Package: ZF-244. Follows the cache retention and API.

## Outcome

An operator can choose a recent successful push job, review the exact setup
block a cache change would alter, declare that they reviewed it, and keep the
preview for fourteen days. A kept preview has a link of its own. Nothing is
typed for the main path and nothing is published.

## Decisions

* **One page, the existing picker.** `/optimisations/cache` reuses the checkout
  run picker and the one-press optimisation gate. The picker lists only
  successful push jobs, because the source and lockfile are read at that commit.
* **The block is the whole review.** The page shows the package manager, the
  root lockfile, the setup step's literal job and step names, the commit and both
  digests, the number of runs and the mean setup time. The `with:` block is
  quoted as untrusted text; the full workflow is never returned by the API and
  never rendered.
* **A declaration gates keeping.** The operator must tick that they reviewed the
  setup block and that the repository's tooling and downstream consumers can use
  the package cache. Keeping sends the exact selection, head, workflow digest and
  lockfile digest the server showed.
* **A kept preview is private and plain.** The card shows the insertion, the
  evidence summary, the preview ID and who kept it. It says verification is
  inconclusive, currentness not checked, and nothing was published. There is no
  approval, publication, comparison link or mutation on the card.
* **No new server contract.** The page uses the inspect, capture and replay
  routes from the retention slice. No model, GitHub history list or extra
  requests are added.

## Not in this slice

CLI, MCP and Eli access; approval; publication; E2 verification; cache
verification. The page is not added to navigation beyond a link from Checkout
preview.

## Verification

Browser tests on desktop and phone cover picking a successful push job without
typing IDs, the exact inspect and capture bodies, the escaped insertion block,
the declaration gating the button, a kept preview opening from its link, and no
horizontal overflow. No live GitHub, model or fleet.

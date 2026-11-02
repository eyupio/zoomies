# 0015: A band draws its sentence beside it, in one blue

**Status**: accepted. Taken on 1 November 2026 on the owner's instruction, from
mock-ups of the Overview band and then of every other band, choosing between
them one decision at a time.

## Context

Every page opens on a band: a sentence about the fleet, a few figures, and the
dog on the right. When the Overview's activity grid moved down into the History
section the band's right half was empty, and the dog is hidden by default, so
most operators saw a sentence and a blank. The same was true to a lesser degree
on every page, because the sentence and its figures take a third of the width
and the dog took the rest only when it was shown.

The options were a smaller activity grid in the gap, a sparkline, a set of
tiles, or nothing. A status-coloured grid was tried and rejected: the band is
the one surface that uses the stage palette (Zoomies Black with the brand blue),
and the fixed status colours (idle, busy, pending, draining, danger) would have
put a second colour language on the one surface that had none.

## Decision

Each band draws the sentence beside it as one small picture, in the brand blue
alone, and every band uses the same grammar:

* **Solid blue** is the thing counted; **half-shade** is on its way; **faint**
  is leaving or clean; **hollow** is headroom (a slot or machine the ceiling
  allows and nothing fills); **dashed** is not seen (no heartbeat, no reading,
  not yet evaluated).
* **A hole punched through** a square is the only shape a failure has: a failed
  job, a machine that needs review, a repository with an error open. Nothing on
  a stage is red.
* **An edge in white** marks the one row an operator should look at first: a pool
  at its ceiling, a connection below a tenth of its quota.
* The picture **agrees with the band's sentence**, counting what the sentence
  counts, and it **costs no request the page is not already making** unless the
  page's own data would make it lie (the Workflows picture asks for every run in
  flight because the grid shows only the view chosen).
* Each picture is **a way in**: an hour on the punch card opens that hour in
  History or narrows the Jobs list, a slot-map row opens its host, the
  repository squares open the repositories page worst first.
* A list that does not fit says **"and n more"** rather than shrinking.

The pictures are: the seven-day hourly punch card (Overview) and its one-day
form (Jobs); the slot map by occupancy (Hosts) and by runner state (Runners);
pool bars against their ceiling (Pools); the queue sparkline over the hour
(Queue); usage columns over the range (Usage); the work in flight by
repository (Workflows); machines against each provider's ceiling (Providers);
API quota left this hour (Installations); and repositories worst first (Kennel
Club). The History section stays as the detail view; the punch card is its
preview, not a second copy.

Appearance gains a **Picture** switch beside the dog's, per page with a global
default, synced to the account like the other band preferences.

## Consequences

The band is now a row of three: words, picture, art, and the picture is hidden
below 800px as the art is. The sentence keeps the left third and the picture is
held to 46% (34% with the art shown), which cost the Runners band its seventh
tile on a 1440px screen; the tile wraps rather than the picture shrinking.

Every picture's layout is a pure function in `web/src/lib/bands/*.ts`, held to
its band's sentence by a unit test built on the generated types without casts,
and `web/tests/page-bands.spec.ts` checks each page against the real binary.
The component gallery's "Band pictures" section shows every square and bar in
every kind, on the stage, so a change to the grammar is reviewed once there and
not page by page.

Adding a band picture means adding to the grammar, not beside it: a new kind
needs a shade or a shape the list above does not already use, and a new colour
is not available. Status colours stay out of the stage.

`docs/ui-guidelines.md` describes the pictures and the switch, the docs
screenshots were recaptured, and the pictures are not drawn on a phone; that is
left open.

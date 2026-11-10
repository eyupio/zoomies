# Command centre implementation

10 November 2026. Captures from the built Zoomies controller and its seeded
demonstration fleet, not generated mockups. They come from `make screenshots`'s
own script (`web/tests/support/screenshots.mjs`, shots `command` and
`command-phone-375`) with authentication on and an administrator signed in, in
the en-GB locale with motion reduced. Values and problems are demonstration
state and move as the fixture runs. The specification they implement is the
[design](../README.md).

| Style and mode | Desktop, 1440 by 900 | Phone, 375 by 900 |
| --- | --- | --- |
| Classic light | [Capture](classic-light-1440.png) | [Capture](classic-light-375.png) |
| Classic dark | [Capture](classic-dark-1440.png) | [Capture](classic-dark-375.png) |
| Mission Control light | [Capture](mission-control-light-1440.png) | [Capture](mission-control-light-375.png) |
| Mission Control dark | [Capture](mission-control-dark-1440.png) | [Capture](mission-control-dark-375.png) |

Each shows only the visible viewport, taken at twice the pixel density and
scaled to the size in the table. The phone captures stop at the fold, so the
lanes, Just finished and the ticker below it are not shown; the same bands are
exercised by the browser specifications.

## Implemented behaviour

- `/command` is an immersive route: no sidebar, top bar or footer, only its own
  bar with the wordmark, the clock, Full screen and Exit. The palette, the
  problems drawer and the shortcut sheet remain. `g d` reaches it, it follows
  Overview in the navigation, and it is in the menu More opens on a phone.
- One verdict, a ring of slots in use, three figures and an hour of running
  jobs and queue depth, all computed from the same snapshot as the lanes.
- A lane for each host with a tile for each slot, so the grid does not reflow
  when a runner comes or goes; a lane says in words why it is full, cordoned or
  unhealthy.
- Waiting, Needs attention and Just finished in a rail, and a ticker of recent
  events. Every item links to its detail; a job opens the drawer in place.
- A page whose stream has dropped says "Not live.", stops its clock and
  reconciles everything it holds before it goes on counting.
- A visitor session sees a compact "Public, read-only view" note in the bar.

## Departures from the specification

The specification is corrected to match. The third figure is the success share
of the stats window ("Succeeded, last hour"), not of today. A tile shows
repository and branch, not a pull request, which a job does not carry. The
finished-jobs read does not send `include_steps=false`. Needs attention lists
the problems not snoozed or dismissed. Waiting and Needs attention precede the
lanes in reading order. The accessibility pass uses the repository's own
checks, not axe.

## Verification

Validated: the production UI and Go binary build; `npm run check`, `npm run
lint` and `npm run test:unit` pass; at commit 7125d4c, seven browser
specifications that touch the page, navigation and shortcuts passed 272 tests
with none failed, against the real built controller; the captures above were
taken from that controller in both styles and both colour modes at both widths.

Full browser suite: at commit 7507c54 the whole suite ran once, on both
projects: 2531 passed, 104 skipped and 18 failed. One failure was this change's
(the page had no Refresh, which every page in the navigation owes) and is
fixed in ce64af1, with its own tests. The rest were shown to fail the same way
on the base commit: one host specification whose clean-up left a host behind,
the specifications that then counted one host too many, and three that depend
on the order or timing of a fifty-minute run. The whole suite has not been run
again since.

Not validated: no usability study was done, and nobody has watched the page on
a wall display over hours. The captures verify layout, not real infrastructure or the
behaviour of a browser's full-screen and wake-lock controls.

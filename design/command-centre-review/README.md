# Command centre design

10 November 2026. Design specification, written before any application change,
and since corrected where building it settled a question. It is implemented. The
implementation plan is
[`roadmap/plans/2026-11-10-command-centre.md`](../../roadmap/plans/2026-11-10-command-centre.md)
and captures of the built page, with what was and was not validated, are in
[`implemented/`](implemented/README.md).

## What it is for

An operator wants one screen that answers "what is happening?" without
reading a table: a view to switch to, leave on a second monitor or a wall, and
glance at. The Runners page answers "which runners exist" and the Queue page
"what is waiting", each as a list to work through. The Overview answers "does
anything need me" and is arranged for investigation, with history and
diagnostics. None of them is built to be watched.

The command centre is a full-screen page that shows, at once and live:

1. one plain verdict on the fleet,
2. every host and the work in each of its slots,
3. what is waiting and why,
4. what needs attention,
5. what has just finished.

Success is an operator who can tell from across a room whether work is moving,
and who can get from anything on the screen to its detail in one click.

## Concepts reviewed

Four boards were reviewed as live, animated mock-ups on a design canvas, all in
Mission Control dark with sample data taken from a real busy fleet.

| Board | Idea | Outcome |
| --- | --- | --- |
| A, Flight Deck | The fleet by host: a lane for each host, a tile for each runner in its slot, a side rail for the queue and problems | The basis of the chosen design |
| B, Flow | Jobs moving left to right through Waiting, Starting, Running and Just finished | Not chosen: it shows whether work is stuck but not which host is the bottleneck. Its Just finished list was kept |
| C, Pulse | One large ring, a one-sentence verdict, runners as cells by pool | Not chosen as a whole: too little detail for each runner. Its ring and verdict were kept |
| D, Flight Deck with the Pulse hero | A's lanes and rail, C's ring and verdict in place of a row of stat tiles, B's Just finished in the rail | **Chosen** |

D was chosen because it is the only one that puts the answer ("busy and
healthy") and the reason ("precision is full, two jobs wait for it") on the
same screen: a full host and the jobs waiting for it sit side by side. It grows
with the fleet by adding lanes.

The boards are sketches. Their numbers are samples, and the rules below govern
the implementation where the two differ. Captures of the built page replace
them in [`implemented/`](implemented/README.md), as
`design/overview-review/implemented` did for the Overview.

## Layout

Four bands, top to bottom, at desktop width:

| Band | Contents |
| --- | --- |
| Bar | Wordmark, "Command centre", the live indicator, the clock and date, the full-screen control and Exit. A visitor session, which the controller allows to read without signing in, sees a compact "Public, read-only view" note with a Sign in link, because the shell's own notice is chrome this page does without |
| Hero | The ring, the verdict, three figures, the last hour's chart |
| Deck | Host lanes on the left (the wide column), the rail on the right: Waiting, Needs attention, Just finished. In reading and Tab order Waiting and Needs attention come before the lanes on every width, so one source order serves the phone's stack and the keyboard |
| Ticker | Recent events, newest first |

### Hero

* **The ring** is slots in use out of slots the fleet has, drawn as an arc,
  with the number of running jobs inside it. Two halos pulse outward from it
  while anything is running and hold still when nothing is.
* **The verdict** is one short headline and one sentence, computed from the
  same snapshot as everything else on the page. The headline is the worst
  thing that is true, in this order:

  | Condition | Headline | Sentence names |
  | --- | --- | --- |
  | The event stream is down | Not live. | When the page last heard from the controller |
  | A problem is open | Needs a look. | How many problems, and the first one's target |
  | Jobs wait and no slot is free for them | Busy, with a queue. | How many wait, the longest wait, the pool they wait for |
  | Jobs are running | Busy and healthy. | Slots in use, and anything waiting on a runner that is starting |
  | Nothing is running or waiting | Quiet. | How many slots stand ready |

  The phrases inside the sentence that carry a count ("3 jobs wait", "one host
  needs a look") take their status colour and are links.
* **Three figures**: median queue wait (with p95), runner start time (p50 with
  p95) and the share of the stats window's finished jobs that succeeded (with
  the count), labelled "Succeeded, last hour" because the `stats` payload is
  windowed and a label has to match its number. They are the `stats` payload's figures for the fleet's own jobs, not every job
  in the organisation.
* **The chart** is running jobs and queue depth over the last hour, as inline
  SVG. Running is the busy colour and a solid line; queued is the pending
  colour and a dashed one, so the two are told apart without colour.

### Host lanes

One lane for each host that is not retired, ordered by slots, largest first. A
lane's header carries the host's name, its slot sentence and its measured CPU.
Under it is a grid with a tile for each runner and one cell for the room left:

* a **runner tile** for a live runner: status mark, job name, repository and
  branch (the job record carries no pull request number), age, and a CPU bar;
* a **starting tile** for a runner that is provisioning or registering, in the
  pending shape, saying which step it is at;
* one **free cell** that counts the host's free slots ("14 free slots", "1 free
  slot"), a dashed outline with the idle mark, the height of a runner tile and
  the width of one. A full host has none, and a wholly idle host is its header
  and that one cell.

A lane reports its own state where the verdict would otherwise have to: a full
host with jobs waiting for it says "full, 2 jobs wait" and takes a pending
border; a throttled, unhealthy or cordoned host says so in its own words and
takes the matching status border. The verdict's sentence and the lane it means
therefore carry the same colour.

An earlier version drew a cell for every slot, so that nothing reflowed; an idle
host of fourteen slots was then fourteen identical dashed boxes, and the page
was mostly empty outlines, worst on a phone. Now a tile entering or leaving
moves the ones after it along. Each tile is keyed by its runner, so it keeps its
identity when one before it leaves; a new tile fades in where it lands, and no
position is animated, which keeps the page inside the rule that state changes
animate colour and the mark, never layout.

Two small hosts share a row; a host with more than one row of cells takes the
full width.

### The rail

* **Waiting**: queued jobs the fleet has a hand in, longest wait first, each
  with its job name, repository, live wait and the reason it waits, in the
  scheduler's own words as the Queue page shows them. Five at most, then a link
  with the count of the rest.
* **Needs attention**: the problems that are not snoozed or dismissed (the set
  the verdict counts, so the list and the headline agree), most severe first, each with its title,
  one line of detail and its fix link. Three at most, then a link to the
  problems drawer. The section is absent, not empty, when nothing is wrong.
* **Just finished**: the last five jobs to finish in the last ten minutes, with
  a tick or a failure triangle, the job, its repository and how long it took,
  and the count for the window in the heading. A failed job's name takes the
  danger colour as well as the triangle.

### Ticker

The feed's most recent entries on one line, each with its time, its status
mark and its sentence. It scrolls slowly. It pauses while the pointer is over
it or focus is inside it, and it has a pause control, because content that
moves for more than five seconds must be stoppable.

## Everything is a link

Every item on the page opens the detail behind it. Nothing is decorative, and
nothing that looks pressable is inert.

| Item | Opens |
| --- | --- |
| The ring and its number | Jobs, filtered to running |
| "N jobs wait" in the verdict | The Queue |
| "N hosts need a look" in the verdict | The problems drawer |
| Median wait | The Queue |
| Runner start | Runners |
| Succeeded, last hour | Jobs, filtered to jobs completed in the stats window |
| The chart | The Overview's history |
| A host lane's header | That host |
| A runner tile | That runner, which carries its job and its log |
| A starting tile | That runner |
| A free cell | Its host |
| A waiting job | The job drawer, opened in place |
| A problem | Its target (the host, pool or installation it names) |
| A finished job | The job drawer, opened in place |
| "and N more" under a list | The list, filtered the same way |
| A ticker entry | The feed entry's own target |

Rules that make this hold:

* **Navigation is a real link.** Anything that goes to another page is an
  `<a href>`, so it can be opened in a new tab, copied and reached by Tab. A
  whole tile is the link; it holds no second control inside it.
* **Jobs open in the job drawer, in place.** A job has no page of its own; the
  drawer is how the Overview and the Jobs page already show one, and it keeps
  the wall where it is. These rows are buttons.
* **Coming back works.** The page is a route like any other, so Back from a
  host or a runner returns to it. Client-side navigation does not leave
  full-screen mode, so a wall display stays full screen through a visit to a
  detail page and back.
* **Hover and focus say where a click goes.** A tile lifts its border to the
  accent and shows the focus ring; the shared tooltip names the destination for
  the figures and the ring, whose targets are not obvious from their labels.
* **Hover and focus say what an item is.** Every tile, cell, lane header,
  figure, rail row and ticker entry shows the shared tooltip's structured card
  (a title and a few label and value rows, only those that are known), so the
  wall can be questioned without leaving it; a tap on a touch screen still
  follows the link.
* **Targets are at least 44px** in the smaller dimension on touch screens.

The page reads and links. It does not act: draining a runner or cancelling a
job is done on the page a click leads to, where the confirmation lives.

## Getting there and leaving

* A route, `/command`, titled "Command centre". It is an immersive route: no
  sidebar, top bar or footer, because the page is its own chrome. It is not a
  `bare` route, which would also lose the problems drawer, the command palette
  and the shortcut sheet, and the page needs all three.
* An entry in the navigation directly after Overview and, like Overview,
  above the groups and in none of them: the two answer the same question at
  different distances, so they sit together. It is second in `SECTIONS`, takes
  the chord `g d` (`c` already goes to AI Context), and has an entry in the
  command palette. The phone's bottom
  bar keeps its four; the entry is in the menu it opens.
* A control in the page's bar asks the browser for full screen. A browser only
  grants that to a press, so the page never enters it by itself. Where the
  browser offers a screen wake lock the page holds one while it is full screen,
  so a wall display does not dim.
* **Exit** returns to the page the operator came from, or the Overview when
  there is none. `Esc` leaves full screen first (the browser's own behaviour),
  closes an open drawer next, and then exits.

## Data

No new API route. Everything is already served and already live:

| Shown | Source |
| --- | --- |
| Hosts, runners, pools | The fleet state store, kept current by `host.*`, `runner.*` and `pool.*` frames |
| Running and waiting counts, wait and start timings, the window's results | `GET /api/v1/stats` and the `stats` frame |
| Waiting jobs and why | `GET /api/v1/jobs` with the Queue page's own filters, kept current by `job.updated` |
| Just finished | `GET /api/v1/jobs` with `state=completed`, `completed_since` ten minutes back, `managed=true`, kept current by `job.updated`. It does not send `include_steps=false`: the job summary lacks what the drawer and the merge rule need |
| Problems | `GET /api/v1/problems` and `problems.updated` |
| The chart | `GET /api/v1/samples?window=1h`, extended by each `stats` frame |
| The ticker | The feed store the Overview's event panel reads |

Ages and waits are worked out in the browser from the timestamps and one
shared one-second tick, not from a frame for each second.

A wall display that has silently stopped updating is the failure this page
must not have. When the event stream drops:

* the live indicator changes from "Live" in the busy colour to "Reconnecting"
  in the pending colour and shape,
* the verdict becomes "Not live." and says when the page last heard anything,
* ages and waits stop counting, and the pulsing stops,

and all of it resumes, after a refetch, when the stream returns.

## Structure

The page is thin and its judgement is in pure functions that can be tested
without a browser.

| Unit | Purpose |
| --- | --- |
| `web/src/routes/CommandCentre.svelte` | Lays out the four bands and owns full screen, the wake lock and Exit |
| `web/src/lib/command/verdict.ts` | Pure: snapshot in, headline, sentence parts and their links out |
| `web/src/lib/command/lanes.ts` | Pure: hosts, runners and queued jobs in, lanes with their cells and lane state out |
| `web/src/lib/command/links.ts` | Pure: the one place the table above is written down |
| `web/src/lib/command/Hero.svelte`, `Ring.svelte`, `HourChart.svelte` | The hero band |
| `web/src/lib/command/HostLane.svelte`, `RunnerTile.svelte` | The deck |
| `web/src/lib/command/WaitingList.svelte`, `AttentionList.svelte`, `FinishedList.svelte` | The rail |
| `web/src/lib/command/Ticker.svelte` | The ticker and its pause |

Existing pieces are used, not copied: `RunnerStatus` for a tile's status mark
(so the Off, Cute and Standard vocabularies all work), `JobDrawer`, the
problems drawer, the feed store, and the Queue page's reason strings. No
library is added: the ring and the chart are inline SVG, and there is no
charting or animation dependency.

## Motion

All of it uses the existing duration and easing tokens, and all of it stops
under `prefers-reduced-motion`.

| Moves | When | Why |
| --- | --- | --- |
| The live indicator breathes | While the stream is up | Proof the page is live |
| The ring's halos | While any job runs | The fleet's pulse, and the page's signature |
| A busy mark's pulse and a sweep along the CPU bar | On a running tile | Only in-progress states move |
| The pending ring turns | On a waiting job or a starting tile | The same |
| A tile, a row or a figure eases in | When it first appears | Row insert, at the base duration |
| A bar's width and the ring's arc | When the value changes | A change the eye can follow |
| The chart draws itself | Once, on arrival | |
| The ticker scrolls | Unless paused | |

Each runner's animation is offset by `dogPhase`, as the rows of the Runners
page are, so a deck of fourteen tiles does not pulse in step. Idle, failed and
free cells hold still. Under reduced motion the ticker is a still list of the
latest three entries.

## Styles and sizes

* Both visual styles (Mission Control and Classic) and both colour modes.
  Every colour is a token; the status colours keep their fixed meanings and
  each travels with its shape. Any measure the page needs twice (the ring's
  size, the tile's height) is added to `tokens.css` and recorded in
  `docs/ui-guidelines.md`.
* At desktop width the page is exactly the window, in full screen and out of
  it, and the document never scrolls: the bar and the hero keep their height
  at the top, the ticker is pinned at the foot, and the deck and the rail share
  what is left. A larger fleet, or a longer rail, scrolls inside the deck or
  the rail, each on its own; nothing is clipped, and lists in the rail cap as
  described.
* On a phone the bands stack: hero, Waiting, Needs attention, the lanes, Just
  finished, with the ticker last. The ring shrinks and the figures sit three
  across.

## States to build before the happy path

* **Loading**: skeletons in the shape of each band.
* **Empty fleet** (no hosts): the verdict says so and links to adding a host;
  the deck is absent.
* **Quiet**: every host one free cell, the halos still, Waiting and Just finished
  showing their own one-line empty sentences.
* **Not live**: as described under Data.
* **Trouble**: a failed runner tile in the danger shape with its fault's fix
  as the link target, a host that is down, several problems at once.
* **A request fails**: the band that asked says so and offers to try again; the
  other bands stay.

## Testing

* Unit tests for `verdict.ts`, `lanes.ts` and `links.ts`, table-driven, with
  fixtures built to the generated API types.
* A Playwright spec, `web/tests/command-centre.spec.ts`, against the real
  binary: the page renders from a fleet, each kind of item goes where the table
  says, a job row opens the drawer in place, Exit and `Esc` behave in the order
  given, and the page says "Not live." when the stream is taken away. It takes
  the stream away before rewriting any read the stream also carries.
* The page joins the accessibility, keyboard, mobile and navigation passes. The
  accessibility pass runs the repository's own checks in its a11y spec; no
  automated checker was added, because no dependency may be.
* New tile and lane states are added to the component gallery first.

## Documentation that changes with it

`docs/ui-guidelines.md` (the information architecture, the keyboard chord and
any new token), the screenshots (`make screenshots`), the shortcut sheet, and
this folder, which gains the built page's captures.

## Not in this change

* Acting on a runner, a job or a host from the page.
* Arranging or hiding the page's sections.
* Rotating between views unattended, or a choice of layouts.
* Any new API route, event kind or stored setting.

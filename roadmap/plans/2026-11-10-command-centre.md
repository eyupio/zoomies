# Command centre implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** built on 10 November 2026, Tasks 1 to 10. The spec is corrected to match
and captures are in `design/command-centre-review/implemented/`. It departed from
the plan in these places: the third figure is the success share of the stats
window ("Succeeded, last hour"), not of today, and links to that window's jobs; a
runner tile shows repository and branch, because a job carries no pull request
number; the finished-jobs read does not send `include_steps=false`, because the
job summary lacks what the drawer and the merge rule need; Needs attention lists
the problems not snoozed or dismissed, so it agrees with the verdict; Waiting and
Needs attention precede the lanes in document order on every width; the
accessibility pass uses the repository's own a11y checks, since no axe is
installed and no dependency may be added; `elapsedMs` is in `time.ts` and the
clock in `clock.svelte.ts`, the pure job merges in `jobs-merge.ts`; the empty
fleet's link reads "Add a host"; a visitor session sees a compact "Public,
read-only view" note in the bar. `/command` is left off the hand-written sitemap
in `internal/api`, because the plan forbade Go changes.

**Goal:** A full-screen page at `/command` that shows the fleet's verdict, every host with the work in each slot, what waits, what needs attention and what has just finished, live, with every item a link to its detail.

**Architecture:** One route component lays out four bands and owns full screen. Everything the page decides (the verdict, the lanes, where a click goes, the chart's path) is a pure function in `web/src/lib/command/` with a unit test; the Svelte components only render what those return. All data comes from the stores and routes that exist: no Go, no API and no schema change.

**Tech stack:** Svelte 5 runes, TypeScript, Tailwind v4 tokens, inline SVG, `node:test` unit tests, Playwright against the real binary.

**Spec:** [`design/command-centre-review/README.md`](../../design/command-centre-review/README.md). Read it first; this plan does not repeat its tables.

## Global constraints

* No new dependency, no charting or animation library, no new API route, event kind or stored setting.
* No raw colour in a component. A measure used twice is a token in `web/src/lib/styles/tokens.css`, in both themes, recorded in `docs/ui-guidelines.md`.
* Status colours keep their fixed meanings and always travel with their shape. Information uses `--z-accent`.
* Motion uses `--z-motion-fast`, `--z-motion-base`, `--z-motion-slow` and `--z-ease` only, and stops under `prefers-reduced-motion`. State changes animate colour and the mark, never layout.
* Anything that navigates is an `<a href>`. Jobs open in `JobDrawer`, in place, from a `<button>`.
* Job, workflow, repository, branch, host and runner names are untrusted text: rendered as text, never as markup, and cut with an ellipsis, never allowed to widen a cell.
* British spelling; no em dashes and no spaced double hyphen in prose, comments or UI strings. An absent value is `NO_VALUE` from `web/src/lib/format.ts`.
* Test names are sentences about the behaviour. Fixtures are built to the generated API types and never cast.
* Before each commit: `npm run check`, `npm run lint` and `npm run test:unit` in `web/`.

## Two places this plan departs from the spec

Both were found reading the code, and the spec is corrected in the same change as this plan.

1. **The chord is `g d`, not `g c`.** `c` already goes to AI Context (`GO_KEYS` in `web/src/lib/keys.ts`).
2. **The route is `immersive`, not `bare`.** A `bare` route is rendered without the problems drawer, the command palette, the shortcut sheet and Eli, and the page needs the first three. A new flag keeps them and drops only the sidebar, the top bar and the footer.

## Review focus

The conditions the spec implies and a person will meet, most likely first. Each has its test in the task named.

1. **A runner with no job, or a job with a timestamp missing.** An idle runner is a tile that says "Idle" with no repository line; a missing `started_at` shows `NO_VALUE`, not `NaN` (Task 4).
2. **The browser's clock is behind the controller's.** An age or a wait is never negative: it is clamped at zero (Task 2).
3. **A fleet far larger than the boards.** Forty hosts and two hundred runners make the deck taller and the page scroll; no lane is clipped and none is dropped for a count (Task 4, Task 9).
4. **Names written to break the page.** A job called `<img src=x onerror=...>` or four hundred characters long is one line of text with an ellipsis (Task 9).
5. **Full screen is not on offer.** Where `document.fullscreenEnabled` is false (an iPhone), the control is absent and Exit still works (Task 1).

## File structure

| File | Responsibility |
| --- | --- |
| `web/src/routes/CommandCentre.svelte` | The four bands, loading and error states, Exit, full screen |
| `web/src/lib/command/links.ts` | Where every item goes |
| `web/src/lib/command/time.ts` | `elapsedMs`, clamped, and the page's one-second tick |
| `web/src/lib/command/verdict.ts` | The headline and sentence |
| `web/src/lib/command/lanes.ts` | Hosts into lanes of cells |
| `web/src/lib/command/chart.ts` | Samples into SVG paths |
| `web/src/lib/command/fullscreen.svelte.ts` | Full screen and the wake lock |
| `web/src/lib/command/jobs.svelte.ts` | The waiting and just-finished lists, fetched and kept live |
| `web/src/lib/command/{Hero,Ring,HourChart,HostLane,RunnerTile,WaitingList,AttentionList,FinishedList,Ticker}.svelte` | Rendering only |
| `web/unit/command-*.test.ts` | One file for each pure module |
| `web/tests/command-centre.spec.ts` | The page against the real binary |

---

### Task 1: The route, the immersive layout and the way in

**Files:**
- Create: `web/src/routes/CommandCentre.svelte`, `web/src/lib/command/fullscreen.svelte.ts`
- Modify: `web/src/lib/router.ts` (`RouteDef`, `ROUTES`), `web/src/App.svelte` (layout branches), `web/src/lib/shell/sections.ts` (`SECTIONS`), `web/src/lib/keys.ts` (`GO_KEYS`), `web/src/lib/shell/CommandPalette.svelte`, `web/tests/support/fixtures.ts` (`SECTIONS`)
- Test: `web/unit/command-navigation.test.ts`, `web/tests/command-centre.spec.ts`

**Interfaces:**
- Produces: `RouteDef.immersive?: boolean`; route `{ name: 'command', path: '/command', title: 'Command centre', immersive: true }`; `fullscreen` with `supported: boolean`, `active: boolean`, `toggle(el: HTMLElement): Promise<void>` and `exit(): Promise<void>`.

- [ ] **Step 1: Write the failing unit test**

```ts
test('the command centre is the second section, in no group, on g d', () => {
  assert.equal(SECTIONS[1].path, '/command');
  assert.equal(SECTIONS[1].label, 'Command centre');
  assert.equal(SECTIONS[1].group, undefined);
  assert.equal(SECTIONS[1].key, 'd');
  assert.deepEqual(GO_KEYS[1], { key: 'd', path: '/command', label: 'Command centre' });
});
test('no two sections and no two go keys share a letter', () => {
  assert.equal(new Set(GO_KEYS.map((k) => k.key)).size, GO_KEYS.length);
});
test('the command centre route is immersive and is not bare', () => {
  const route = ROUTES.find((r) => r.name === 'command');
  assert.equal(route?.immersive, true);
  assert.equal(route?.bare, undefined);
});
```

- [ ] **Step 2: Run it and see it fail**

Run: `npm run test:unit` in `web/`. Expected: the three tests fail.

- [ ] **Step 3: Add the route, the section and the key**

The section: icon `MonitorPlay` from `@lucide/svelte`, hint `The whole fleet on one screen, live.`, not `primary` (the phone's bottom bar keeps its four). The palette gains a "Command centre" command beside "Overview".

- [ ] **Step 4: Add the immersive branch to `App.svelte`**

A branch after the `bare` one and before the default: `<main id="main" tabindex="-1">` holding the same error, loading and `<Page />` cases as the default branch, with no `Nav`, `InstanceNotice` bar, `TopBar` or `AppFooter`, followed by `CommandPalette`, `ProblemsDrawer`, `ShortcutSheet`, and `EliWidget` only while `fullscreen.active` is false. Shortcuts stay installed.

- [ ] **Step 5: Write the page's bar and `fullscreen.svelte.ts`**

The bar: the wordmark, "Command centre" as the page's `<h1>`, the clock, a "Full screen" button rendered only when `fullscreen.supported`, and Exit. Exit is `router.back()` when the tab has history inside the app, else `router.navigate('/')`. `Esc` with no layer open and no full screen calls Exit; the existing layer stack in `keys.ts` already gives an open drawer the first `Esc`. `toggle` requests full screen on the page's root and, where `navigator.wakeLock` exists, holds a `screen` lock until full screen ends; a refused lock is ignored.

- [ ] **Step 6: Write the Playwright tests**

In `web/tests/command-centre.spec.ts`: the nav entry after Overview opens the page with the heading "Command centre" and no navigation landmark; `g d` opens it from the Overview; Exit returns to the Overview; with `document.fullscreenEnabled` overridden to `false` by an init script the "Full screen" button has count 0 and Exit still works; `Ctrl+K` opens the palette on the page.

- [ ] **Step 7: Run and commit**

Run: `npm run check && npm run lint && npm run test:unit`, then `make test-ui` from the root with `--trace off`. Update `web/tests/navigation.spec.ts` and `web/tests/mobile.spec.ts` expectations where they count sections. Commit: "Give the fleet a full-screen page of its own".

---

### Task 2: Where a click goes, and how long ago

**Files:**
- Create: `web/src/lib/command/links.ts`, `web/src/lib/command/time.ts`
- Test: `web/unit/command-links.test.ts`, `web/unit/command-time.test.ts`

**Interfaces:**
- Produces:

```ts
// links.ts
export const RUNNING_HREF = '/jobs?state=in_progress';
export const QUEUE_HREF = '/queue';
export const RUNNERS_HREF = '/runners';
export const HISTORY_HREF = '/#history';
export function finishedTodayHref(now: Date): string; // /jobs?state=completed&since=<local midnight, ISO>
export function hostHref(id: string): string;         // /hosts/<id>
export function runnerHref(id: string): string;       // /runners/<id>
export function problemHref(problem: Problem): string | null;
// time.ts
export function elapsedMs(since: string | undefined, now: number): number | null;
export const clock: { readonly now: number; start(): void; stop(): void };
```

- [ ] **Step 1: Write the failing tests**

```ts
test('a time in the future is no time at all, not a negative one', () => {
  assert.equal(elapsedMs('2026-11-10T14:00:10Z', Date.parse('2026-11-10T14:00:00Z')), 0);
});
test('a missing or unreadable time is null', () => {
  assert.equal(elapsedMs(undefined, 0), null);
  assert.equal(elapsedMs('not a date', 0), null);
});
test('an id is encoded into its link', () => {
  assert.equal(hostHref('a/b'), '/hosts/a%2Fb');
  assert.equal(runnerHref('r 1'), '/runners/r%201');
});
test('today starts at local midnight', () => {
  const href = finishedTodayHref(new Date(2026, 10, 10, 14, 4));
  assert.equal(new URL(href, 'http://x').searchParams.get('since'), new Date(2026, 10, 10).toISOString());
});
```

Add one case for each `target_kind` a `Problem` can carry, asserting the page it opens, and one asserting `null` for a problem with no target.

- [ ] **Step 2: Run, fail, implement, pass**

`problemHref` uses the same mapping the problems drawer uses for a problem's target; find it in `web/src/lib/problems/` and call it, or move it there to a `.ts` file both import if it is inline in a component. `HISTORY_HREF` must open the Overview's History disclosure: check how `revealHash` in `web/src/lib/bands/jump.ts` is used and use the id the Overview gives that panel. `clock` is one `setInterval` of one second behind a `$state`; `stop()` freezes `now`.

- [ ] **Step 3: Commit**: "Write down where everything on the command centre leads".

---

### Task 3: The verdict

**Files:**
- Create: `web/src/lib/command/verdict.ts`
- Test: `web/unit/command-verdict.test.ts`

**Interfaces:**
- Consumes: `QUEUE_HREF` from Task 2.
- Produces:

```ts
export type VerdictKind = 'not-live' | 'attention' | 'queue' | 'busy' | 'quiet' | 'empty';
export interface VerdictPart {
  text: string;
  tone?: 'pending' | 'danger';
  href?: string;
  opens?: 'problems';
}
export interface Verdict { kind: VerdictKind; headline: string; parts: VerdictPart[] }
export interface VerdictInput {
  live: boolean;
  lastHeardAt: number | null;
  now: number;
  hosts: number;
  slots: number;
  busy: number;
  starting: number;
  waiting: number;
  waitingWithoutSlot: number;
  longestWaitMs: number;
  waitingPool: string | null;
  problems: number;
  firstProblemTarget: string | null;
}
export function verdict(input: VerdictInput): Verdict;
```

- [ ] **Step 1: Write the failing table test**

One row for each kind, in the spec's order of precedence, asserting `kind` and `headline`:

| Input | `kind` | `headline` |
| --- | --- | --- |
| `live: false`, everything else busy | `not-live` | `Not live.` |
| `hosts: 0` | `empty` | `No hosts yet.` |
| `problems: 2`, jobs waiting | `attention` | `Needs a look.` |
| `waitingWithoutSlot: 2`, no problems | `queue` | `Busy, with a queue.` |
| `busy: 14`, `waiting: 1`, `waitingWithoutSlot: 0` | `busy` | `Busy and healthy.` |
| nothing running or waiting | `quiet` | `Quiet.` |

Then assert on parts:

```ts
test('the waiting count is a pending link to the queue', () => {
  const v = verdict({ ...base, busy: 14, slots: 16, waiting: 3, waitingWithoutSlot: 2, longestWaitMs: 42000, waitingPool: 'precision' });
  const part = v.parts.find((p) => p.text === '3 jobs wait');
  assert.deepEqual(part, { text: '3 jobs wait', tone: 'pending', href: '/queue' });
  assert.equal(v.parts.map((p) => p.text).join(''), '14 of 16 slots are working. 3 jobs wait, the longest for 42s, for precision.');
});
test('one job waits and one host needs a look are singular', () => { /* '1 job waits', '1 problem is open' */ });
test('a problem count opens the problems drawer and names its first target', () => {
  const v = verdict({ ...base, problems: 1, firstProblemTarget: 'm93p-1' });
  assert.deepEqual(v.parts.find((p) => p.opens), { text: '1 problem is open', tone: 'danger', opens: 'problems' });
});
test('not live says when it last heard, and never claims a count', () => {
  const v = verdict({ ...base, live: false, lastHeardAt: 0, now: 95000, busy: 14 });
  assert.equal(v.parts.map((p) => p.text).join(''), 'Last heard from the controller 1m 35s ago.');
});
```

- [ ] **Step 2: Run, fail, implement, pass**

Durations use `formatDuration` from `web/src/lib/format.ts`. The quiet sentence is `N slots stand ready.`; the empty one is `Add a host to give the fleet somewhere to run.` with the last four words a link to `/hosts/new`.

- [ ] **Step 3: Commit**: "Say in one sentence how the fleet is doing".

---

### Task 4: The lanes

**Files:**
- Create: `web/src/lib/command/lanes.ts`
- Test: `web/unit/command-lanes.test.ts`

**Interfaces:**
- Consumes: `hostHref`, `runnerHref` from Task 2; `slotsOf` from `web/src/lib/hosts/slots.ts`.
- Produces:

```ts
export type CellKind = 'runner' | 'starting' | 'free';
export interface Cell { key: string; kind: CellKind; href: string; runner?: Runner }
export type LaneState = 'ok' | 'full' | 'throttled' | 'unreachable' | 'cordoned';
export interface Lane {
  host: Host;
  href: string;
  cells: Cell[];
  state: LaneState;
  tone: 'pending' | 'danger' | 'draining' | null;
  note: string;   // '5 of 6 slots', 'full, 2 jobs wait', 'throttled', 'not answering', 'cordoned'
  waiting: number;
  wide: boolean;  // more than WIDE_FROM slots
}
export const WIDE_FROM = 4;
export function lanes(hosts: readonly Host[], runners: readonly Runner[], queued: readonly Job[]): Lane[];
```

- [ ] **Step 1: Write the failing tests**

Build `host()` and `runner()` fixtures as `web/unit/band-slot-map.test.ts` does. Assert:

```ts
test('a host is one cell for each slot: its runners first, then starting ones, then free', () => {
  const [lane] = lanes([host('a', 4)], [runner('a', 'busy'), runner('a', 'provisioning')], []);
  assert.deepEqual(lane.cells.map((c) => c.kind), ['runner', 'starting', 'free', 'free']);
});
test('the cells keep their place when a runner leaves', () => { /* keys are `${host.id}:${index}` */ });
test('more runners than slots are all shown, and no free cell is invented', () => {});
test('a retired host has no lane', () => {});
test('the largest host comes first, and ties keep the name order', () => {});
test('a full host with jobs waiting for a pool that runs on it says how many', () => {
  // two queued jobs whose pool_name matches a pool with a live runner on the host
  assert.equal(lane.state, 'full'); assert.equal(lane.note, 'full, 2 jobs wait'); assert.equal(lane.tone, 'pending');
});
test('a throttled host says so before it says it is full', () => { /* tone 'danger', note 'throttled' */ });
test('a host that has stopped answering is danger, and a cordoned one is draining', () => {});
test('forty hosts are forty lanes', () => {
  assert.equal(lanes(Array.from({ length: 40 }, (_, i) => host(`h${i}`, 5)), [], []).length, 40);
});
test('an idle runner is a runner cell with no job', () => {
  assert.equal(lane.cells[0].runner?.current_job, undefined);
});
```

- [ ] **Step 2: Run, fail, implement, pass**

A queued job counts towards a host when its `pool_name` names a pool with at least one live runner on that host. That is an approximation of where the scheduler would place it, and it is used only for the lane's note; say so in the function's comment. Lane state precedence: unreachable, throttled, cordoned, full, ok.

- [ ] **Step 3: Commit**: "Lay the fleet out as a lane for each host and a cell for each slot".

---

### Task 5: The hero band

**Files:**
- Create: `web/src/lib/command/chart.ts`, `Hero.svelte`, `Ring.svelte`, `HourChart.svelte`
- Modify: `web/src/lib/styles/tokens.css`, `docs/ui-guidelines.md`, `web/src/routes/CommandCentre.svelte`
- Test: `web/unit/command-chart.test.ts`, `web/unit/design-tokens.test.ts` if it lists tokens

**Interfaces:**
- Consumes: `Verdict` (Task 3); `RUNNING_HREF`, `QUEUE_HREF`, `RUNNERS_HREF`, `HISTORY_HREF`, `finishedTodayHref` (Task 2); `fleet.stats`, `listSamples`.
- Produces: `linePath(values: readonly number[], width: number, height: number, max: number): string`, `areaPath(...)` with the same parameters, `ringFraction(busy: number, slots: number): number`.

- [ ] **Step 1: Write the failing tests**

```ts
test('a line runs from the left edge to the right edge', () => {
  assert.equal(linePath([0, 10], 100, 50, 10), 'M0 50 L100 0');
});
test('one sample is a flat line, and none is an empty path', () => {
  assert.equal(linePath([5], 100, 50, 10), 'M0 25 L100 25');
  assert.equal(linePath([], 100, 50, 10), '');
});
test('a zero maximum does not divide by zero', () => {
  assert.equal(linePath([0, 0], 100, 50, 0), 'M0 50 L100 50');
});
test('the ring is empty with no slots and never past full', () => {
  assert.equal(ringFraction(3, 0), 0);
  assert.equal(ringFraction(9, 6), 1);
});
```

- [ ] **Step 2: Run, fail, implement, pass, then build the components**

* Tokens, both themes: `--z-command-ring: 9.25rem`, `--z-command-ring-stroke: 0.625rem`, `--z-command-tile-height: 3.5rem`. Record them in section 1 of `docs/ui-guidelines.md`.
* `Ring` is an SVG circle with `stroke-dasharray` from `ringFraction`, inside one `<a href={RUNNING_HREF}>` whose accessible name is `N jobs running, B of S slots in use`. Two halo circles animate only while `busy > 0` and the stream is live. The arc's length transitions at `--z-motion-slow`.
* `Hero` renders the `Verdict`: a part with `href` is a link, a part with `opens: 'problems'` is a button that sets `notifications.open = true`, and either takes its tone's colour. The headline is an `<h2>`; the sentence is in an `aria-live="polite"` region so a change of verdict is announced once.
* Three figures, each a link with the shared tooltip naming its destination: median wait (`stats.median_wait_ms`, note `p95` from `p95_wait_ms`) to `QUEUE_HREF`; runner start (`p50_startup_ms`, `p95_startup_ms`) to `RUNNERS_HREF`; succeeded today (`succeeded / completed` as a whole percentage, note the count) to `finishedTodayHref`. Use the `fleet`-scoped figures of the `stats` payload, as `web/src/lib/overview/metric-stats.ts` does. A figure with no samples shows `NO_VALUE`.
* `HourChart` fetches `listSamples({ window: '1h' })` on mount, appends a point on each `stats` frame, and draws running solid in `--z-busy` and queued dashed in `--z-pending`. The whole chart is one link to `HISTORY_HREF` with `role="img"` text naming the latest two values.

- [ ] **Step 3: Check and commit**: `npm run check && npm run lint && npm run test:unit`. Commit: "Open the command centre with a ring and a verdict".

---

### Task 6: The deck

**Files:**
- Create: `web/src/lib/command/HostLane.svelte`, `RunnerTile.svelte`
- Modify: `web/src/routes/CommandCentre.svelte`, `web/src/gallery/sections/` (a "Command centre" section), `web/tests/gallery.spec.ts` if it lists sections

**Interfaces:**
- Consumes: `Lane`, `Cell` (Task 4); `elapsedMs`, `clock` (Task 2); `RunnerStatus` from `web/src/lib/runners/RunnerStatus.svelte`; `dogPhase` from `web/src/lib/mascot/dog-motion.ts`; `readableJobText` from `web/src/lib/jobs/name.ts`.

- [ ] **Step 1: Add the gallery states first**

A lane in each `LaneState`, and a tile in each state: busy, idle, starting (provisioning and registering), draining, failed, and one with a four-hundred-character job name.

- [ ] **Step 2: Build `RunnerTile`**

The whole tile is one `<a href={cell.href}>` of height `--z-command-tile-height`. First line: `RunnerStatus` mark, the job name (or `Idle`, or the step for a starting runner), and the age from `current_job.started_at` (the runner's `created_at` when it has no job) through `elapsedMs` and `formatDuration`, `NO_VALUE` when null. Second line: `repo` and the pull request or `head_branch`, in the mono face; absent for a runner with no job. Third: a CPU bar from `cpu_percent` over the runner's allocated CPUs, clamped to 100%, with a sweep that runs only on a busy tile and is offset by `dogPhase(runner.id)`. A failed tile takes the danger border. A free cell is an `<a>` to the host with the idle mark and "Free slot". Hover and focus lift the border to `--z-accent`.

- [ ] **Step 3: Build `HostLane`**

A `<section>` named by the host. Its header is a link to the host: name, `lane.note` in `lane.tone`'s colour, measured CPU from `host.usage` only while `usage_fresh`. The border takes the tone. The cells are a grid keyed by `cell.key`; a tile entering a cell fades at `--z-motion-base` and nothing else moves. The deck is a two-column grid; a `wide` lane spans both.

- [ ] **Step 4: Check in the gallery and commit**

Run `make gallery`, open `gallery.html?style=mission-control&mode=dark` and the three other combinations, then `npm run check && npm run lint`. Commit: "Show every host as a lane and every slot as a tile".

---

### Task 7: The rail

**Files:**
- Create: `web/src/lib/command/jobs.svelte.ts`, `WaitingList.svelte`, `AttentionList.svelte`, `FinishedList.svelte`
- Modify: `web/src/routes/CommandCentre.svelte`
- Test: `web/unit/command-jobs.test.ts`

**Interfaces:**
- Consumes: `listJobs`, `events` from `web/src/lib/api/sse.ts`, `fleet.problems`, `problemHref`, `JobDrawer`, `queuedActivity`.
- Produces:

```ts
export const WAITING_SHOWN = 5;
export const PROBLEMS_SHOWN = 3;
export const FINISHED_SHOWN = 5;
export const FINISHED_WINDOW_MS = 10 * 60 * 1000;
export function mergeWaiting(list: readonly Job[], frame: Job): Job[];
export function mergeFinished(list: readonly Job[], frame: Job, now: number): Job[];
export const commandJobs: { readonly waiting: Job[]; readonly finished: Job[]; readonly finishedCount: number;
  readonly error: ApiError | null; start(): void; stop(): void; retry(): void };
```

- [ ] **Step 1: Write the failing tests for the two merges**

```ts
test('a queued job joins the waiting list, longest wait first', () => {});
test('a job that starts leaves the waiting list', () => {});
test('a job that finishes joins the finished list, newest first', () => {});
test('a finished job older than ten minutes is dropped', () => {});
test('the same job arriving twice is one row', () => {});
test('a frame that would move a job backwards is ignored', () => {
  // completed, then a late in_progress frame for the same id: still completed
});
```

- [ ] **Step 2: Run, fail, implement, pass**

`start()` fetches waiting with the same query the Queue page sends for its default view (read `web/src/routes/Queue.svelte` and share the query through an exported constant, not a copy), and finished with `{ state: 'completed', completed_since: <now minus ten minutes>, managed: true, include_steps: false, limit: 50 }`, then subscribes to `job.updated`. The finished list is pruned on each `clock` minute.

- [ ] **Step 3: Build the three lists**

* `WaitingList`: each row a `<button>` opening `JobDrawer`; job name, repository, the wait from `queued_at` in the pending colour, and the reason from `queuedActivity` called as the Queue page calls it. After `WAITING_SHOWN`, a link `and N more` to `QUEUE_HREF`. Empty: `Nothing is waiting.`
* `AttentionList`: absent when `fleet.problems` is empty. Each problem's title, one line of detail, and a link to `problemHref(problem)`; a problem with no target opens the problems drawer. After `PROBLEMS_SHOWN`, a button `and N more` that opens the drawer.
* `FinishedList`: heading count is `finishedCount`; each row a `<button>` opening `JobDrawer`, with a tick or a triangle carrying the accessible name `Succeeded` or `Failed`, the job, the repository and `duration_ms`. A failed job's name is in `--z-danger`. A cancelled or skipped job takes the neutral mark. After `FINISHED_SHOWN`, a link to `/jobs?state=completed&completed_since=<the window's start>`. Empty: `Nothing has finished in the last ten minutes.`

One `JobDrawer` is mounted by the route and shared by both lists. The open job is replaced when a `job.updated` frame for it arrives, as `Jobs.svelte` does.

- [ ] **Step 4: Commit**: "Put what waits, what is wrong and what just finished beside the deck".

---

### Task 8: The ticker, and the page that says when it is not live

**Files:**
- Create: `web/src/lib/command/Ticker.svelte`
- Modify: `web/src/routes/CommandCentre.svelte`
- Test: `web/tests/command-centre.spec.ts`

**Interfaces:**
- Consumes: `feed.entries`, `fleet.connection`, `clock`, `verdict`.

- [ ] **Step 1: Build `Ticker`**

The latest twenty `feed.entries`, newest first, in a list. Each is a link when its `target.href` exists and plain text when it does not. The strip scrolls by a CSS animation whose duration is proportional to its width; it has `animation-play-state: paused` while hovered, while focus is inside it, and while a "Pause" toggle button is pressed. Under `prefers-reduced-motion` it is the latest three entries, still.

- [ ] **Step 2: Wire the live state**

In the route: `live` is `fleet.connection` being the connected status. When it is not, call `clock.stop()`, record `lastHeardAt`, pass `live: false` to `verdict`, show "Reconnecting" with the pending mark where "Live" was, and set `data-live="false"` on the root, under which the halo, pulse and sweep animations are off. When it returns, call `fleet.reconcile()` and `commandJobs.retry()`, then `clock.start()`.

- [ ] **Step 3: Write the Playwright test**

```ts
test('the page says it is not live when the stream goes away', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  await expect(page.getByText('Live', { exact: true })).toBeVisible();
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  await page.evaluate(() => window.dispatchEvent(new Event('offline')));
  await expect(page.getByRole('heading', { name: 'Not live.' })).toBeVisible();
  await expect(page.getByText('Reconnecting')).toBeVisible();
});
```

If the stream does not drop on `offline`, read `web/tests/live.spec.ts` for how that spec ends a stream and do the same.

- [ ] **Step 4: Commit**: "Never let a wall display go quietly stale".

---

### Task 9: States, sizes and the whole page under test

**Files:**
- Modify: `web/src/routes/CommandCentre.svelte` and the components
- Test: `web/tests/command-centre.spec.ts`, `web/tests/a11y.spec.ts`, `web/tests/mobile.spec.ts`, `web/tests/keyboard.spec.ts`, `web/tests/hostile-input.spec.ts`

- [ ] **Step 1: Write the failing Playwright tests**

* Each item goes where the spec's table says: click the ring, a figure, a host header, a runner tile and a free cell, and assert the URL; click a waiting job and a finished job and assert the job drawer is open and the URL is still `/command`.
* Back from a runner returns to `/command`.
* `Esc` closes an open drawer and leaves the page; a second `Esc` exits to the Overview.
* Loading: with `**/api/v1/jobs*` held, the rail shows skeletons and the deck still renders.
* A failed read: with `**/api/v1/jobs*` answering 500, the rail says so with a "Try again" button and the hero and deck remain. Take the event stream away first, as `web/AGENTS.md` requires of a spec that rewrites a read.
* Hostile names: follow `web/tests/hostile-input.spec.ts` to plant a job named `<img src=x onerror=window.__x=1>` and one of four hundred characters; assert `window.__x` is undefined, the name is visible as text, and the tile's `scrollWidth` does not exceed its `clientWidth`.
* A large fleet: with `**/api/v1/hosts*` and `**/api/v1/runners*` rewritten to forty hosts of five slots, forty lanes are in the document, `document.documentElement.scrollHeight` exceeds the viewport and no lane has zero height.
* At 375px wide the order of landmarks is hero, Waiting, Needs attention (when present), the lanes, Just finished, the ticker, and the page has no horizontal scroll.
* All four style and mode combinations render with no page error, as `web/tests/calm-overview.spec.ts` loops them.

- [ ] **Step 2: Make them pass**

Skeletons in each band's shape while `fleet.loaded` is false. Below the width the Overview already uses for its phone layout, the deck and rail become one column in the order above, the ring uses `calc(var(--z-command-ring) * 0.75)`, and every link and button is at least 44px tall.

- [ ] **Step 3: Add the page to the shared passes**

`/command` joins the route lists in `a11y.spec.ts` (axe, both themes), `keyboard.spec.ts` (every item reachable by Tab, focus ring visible) and `mobile.spec.ts`.

- [ ] **Step 4: Run everything and commit**

Run: `npm run check && npm run lint && npm run test:unit`, then `make test-ui` with `--trace off`, then `make lint` and `make test-short` from the root. Commit: "Hold the command centre to its empty, broken and crowded states".

---

### Task 10: The record

**Files:**
- Modify: `docs/ui-guidelines.md` (information architecture: the page and its bands; Keyboard: `d` in the chord list; the immersive layout), `web/src/lib/keys.ts` `SHORTCUTS` if the sheet lists chords by hand, `design/command-centre-review/README.md`, `docs/screenshots` through `make screenshots`
- Create: `design/command-centre-review/implemented/README.md` and its captures

- [ ] **Step 1:** Update the guidelines and run `make screenshots`; read `.github/AGENTS.md` for the files CI diffs against their sources and regenerate any this change touches.
- [ ] **Step 2:** Capture the page at 1440 and 375 in all four style and mode combinations into `design/command-centre-review/implemented/`, with a README recording what was validated and what was not, as `design/overview-review/implemented/README.md` does.
- [ ] **Step 3:** Mark the spec implemented, set this plan's status line to what was built and what departed from it, and run `go test ./internal/docs/`.
- [ ] **Step 4:** Commit: "Record the command centre as built".

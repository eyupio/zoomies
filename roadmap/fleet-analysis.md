# The fleet as it moves: a recent window and analyses over MCP

Status: design approved in conversation, 12 October 2026. Two packages,
ZF-240 (the window) and ZF-241 (the analyses), each its own plan and pull
requests. ROADMAP.md section 8 carries the pointers.

## 1. Intent

An agent connected over MCP, and Eli inside the UI, can read what the fleet
is now and what it did over a period, but not what is happening *as it
happens*, and not why a period was slow or a release worse. The REST API has
the material: the live event stream with a replay ring, the scheduler's
decisions with their reasons, the Overview's minute samples per fleet and per
host, a runner's state history, workflow runs, webhook deliveries, the audit
log and the support bundle. None of it is offered to a model, and the
interesting questions (what changed in the last ten minutes, why are jobs
waiting right now, did the last release make things worse, is this host
unwell when it is busy) take more tool calls than Eli may spend and more
context than an answer is worth.

Two things fix that, in order:

1. **ZF-240, the window.** The recent past over MCP, bounded, with one
   tool that blocks briefly until something changes. No push, no sessions:
   the transport stays stateless, as `internal/mcp` and `docs/connect-claude.md`
   promise.
2. **ZF-241, the analyses.** Answers computed on the controller, pure
   functions over a snapshot in the mould of `internal/scheduler`, each a
   REST route first and an MCP tool second so the UI, the CLI, Eli and an
   agent render one answer and cannot disagree.

A platform team operating an instance for a product team asks these
questions from an agent, and the product team's developers ask Eli; both
are served by the same tools.

## 2. Decisions taken with the owner

- **Pull, not push.** MCP resource subscriptions and a server-initiated event
  stream are deferred and recorded in section 9. Today's clients only
  invalidate a cache on `notifications/resources/updated`, and none wakes a
  model on one; the cost would be sessions and per-session buffers in a
  transport that is stateless on purpose.
- **Eli is offered the window and the analyses, not the operator's
  reads.** This is the owner's choice, not a role: the webhook deliveries
  and the audit log are viewer routes (`webhooks.read`, `audit.read`) and
  only the support bundle is an administrator's (`diagnostics.read`). A
  delivery is GitHub's whole payload, who did what, by name, is not for a
  hosted model, and the support bundle is the whole instance in one
  document. All three stay off Eli's list whoever is asking, named as left
  off in `TestEveryReadToolIsOnElisListOrLeftOffOnPurpose`. (Corrected 12
  October: this bullet first called the audit log an administrator route.)
- **All four analyses:** queue-wait breakdown, release comparison, host
  pressure against failures, repeated runner-startup failures.
- **Two packages**, so the window ships and is accepted before an analysis
  is argued over.

## 3. What exists and is reused

| Source | Route | Store read | Notes |
| --- | --- | --- | --- |
| Live events | `GET /events` (SSE) | `events.Bus` ring, 256 deep, ids `<epoch>.<seq>` | Replays from `Last-Event-ID`; a `resync` frame when it cannot |
| Scaling decisions | `GET /scaling-events` | `ListScalingEvents` | The scheduler's reason sentences, kept for `retention.scaling_events` |
| Fleet samples | `GET /samples` | `ListSamples` | One row a minute, `retention.samples` |
| Host samples | `GET /hosts/samples` | `ListHostSamples` | CPU, load, memory, disk, reserved; gaps not zeros |
| Runner timeline | `GET /runners/{id}/timeline` | `RunnerTimeline` | States and stages with durations |
| Workflow runs | `GET /workflow-runs` | `ListWorkflowRuns` | Derived from jobs, never stored |
| Webhook deliveries | `GET /webhook-deliveries` | | viewer; whole payloads |
| Audit | `GET /audit` | `ListAudit` | viewer |
| Support bundle | `GET /diagnostics/bundle` | | admin, sectioned, capped |
| Job statistics | `GET /jobs/stats` | `JobStats` | Percentiles by six group keys, peaks |
| Explanation | `GET /jobs/{id}/explanation` | | The failure classes ZF-231 made |

Every new MCP tool is one call to one of these routes, or to a new route
under section 5, through the `API` interface, with the caller's own
credential. Nothing in `internal/mcp` learns about the store or the bus.

## 4. ZF-240: the window

### 4.1 One new route

`GET /events/recent` answers the ring as JSON rather than as a stream, so a
stateless MCP tool, the CLI and a test can read it. Parameters: `since`
(an event id as the stream sends it, a duration such as `10m`, or `now`,
which answers no events and the present as `next`), `kinds`,
`topic` and `limit` (at most 200). The answer is `{items, next, complete}`:
`next` is the id to continue from, and `complete` is false when the ring no
longer reaches back to `since` or the id is from another run of the process,
which is the same judgement the stream makes before sending `resync`. The
bus gains a `Recent(since uint64, opts) ([]Event, complete bool)` read over
the ring under its lock; the stream's replay and this route call the one
function, so they cannot disagree about what was lost. Viewer role, the same
`events.read` the stream requires.

`wait` on the same route is the blocking form: `?wait=20s` holds the request
until an event matching the filter arrives or the time is up, and answers at
once when `since` already has events past it. It subscribes to the bus for
the wait, counted against `limits.event_subscribers` like a stream, and
answers `{items: [], next, complete: true, timed_out: true}` on the deadline.
The wait is capped at 25 seconds so one call sits inside Eli's 30-second
tool timeout and inside any proxy's patience.

### 4.2 The tools

| Tool | Reads | Role | Eli |
| --- | --- | --- | --- |
| `recent_events` | `/events/recent` | viewer | yes |
| `wait_for_change` | `/events/recent?wait=` | viewer | yes, live |
| `scaling_decisions` | `/scaling-events` | viewer | yes |
| `fleet_samples` | `/samples` | viewer | yes |
| `host_samples` | `/hosts/samples` | viewer | yes |
| `runner_timeline` | `/runners/{id}/timeline` | viewer | yes |
| `list_workflow_runs` | `/workflow-runs` | viewer | yes |
| `list_webhook_deliveries` | `/webhook-deliveries` | viewer | left off |
| `list_audit` | `/audit` | viewer | left off |
| `support_bundle` | `/diagnostics/bundle` | admin | left off |

Each answers two content blocks: a short rendering for a model (one line
per event or row: the time, the kind, the resource's name and the sentence
the frame carries, never the whole resource JSON) and the JSON under a
`structuredContent` object with an `outputSchema` on the tool, which the
2025-06-18 revision added and the server already answers to. The rendering
is the block Eli reads inside its 16 KiB ceiling; the JSON is what an agent
with room parses.

Workflow-written text (job, step and workflow names, branch names, commit
text, log lines in a frame) goes in the untrusted block the log tools
already use, with the same notice before it. `recent_events` therefore
renders an event by its resource's *id and state* and quotes the name in
the untrusted block only.

Every window is capped: at most 200 events or rows and 64 KiB of rendered
text per call, truncated with `truncated: true` and the id to continue
from, the way `get_runner_log` caps a log.

`support_bundle` takes `sections` and returns only those, because the whole
document is larger than any model should be handed; `sections` left out
returns the index of sections and their sizes.

As built (12 October): the three left off Eli's list are left off by the
owner's choice, as section 2 says, not by their role; `list_audit` and
`list_webhook_deliveries` answer any viewer, and only `support_bundle` needs
an administrator. Only `recent_events` and `wait_for_change` carry
`structuredContent` and an `outputSchema`; the other eight answer JSON as
text, with names inline where the rows carry them, and their descriptions say
those names are untrusted, so the untrusted block is the two event tools'
alone. That structured copy holds the frames the text shows, with the text's
cursor, and is held to 64 KiB as the text is (one event larger than that on
its own comes whole); it carries workflow-written
names outside the untrusted block, and the schema's description of `data`
says they are untrusted; `docs/connect-claude.md` tells a client that hands
it to a model to treat them as data. Eli reads the window tools' text held to
12 KiB a block, under its 16 KiB ceiling. `wait_for_change` with no `kinds`
waits for every kind but `stats`, `host.updated`, `kennel.summary`,
`updates.updated`, `audit` and `webhook.delivery`: the first four move as
time passes (`host.updated` and `updates.updated` carry real changes as
well, and are named in `kinds` to wait on those), and the last two are
activity rather than a change to the fleet.

### 4.3 Eli

- `AssistantFleetTools` gains the seven tools marked yes. The three marked
  left off are named in the left-off map with their reason.
- A tool definition gains `live bool` (`internal/mcp`), carried through
  `assistant.Tool` as `Live`. `runTool` skips the `made` cache for a live tool,
  so a second `wait_for_change` or `recent_events` with the same arguments
  runs again; `callKey` is unchanged for every other tool.
- `wait_for_change` with no `since` answers the current `next` id at once
  and nothing else, so a model's first call establishes a cursor and its
  second waits. The description says so. It asks the route for `since=now`
  without `wait`, so a busy ring is not paged through first.
- Three recorded scenarios in `eli_eval_test.go`: *what changed in the last
  ten minutes* (rests on `recent_events`, at most 2 calls), *why are jobs
  waiting right now* (rests on `scaling_decisions` and `list_problems`),
  *is this host unwell when it is busy* (rests on `host_samples`). Each
  plants a job name carrying an instruction and asserts it is not followed.
- The opening suggestions on a fleet with a problem gain *What changed in
  the last ten minutes?* when Eli may read the fleet.

### 4.4 Prompts and resources

- Prompt `what_changed` (argument: a duration, default `10m`): read
  `recent_events`, then `scaling_decisions` for the same period, then
  `list_problems`; say what moved and what is still open.
- Resource `zoomies://fleet/now`: the `fleet_status` document, so a client
  can load the shape of the fleet without a tool call. Read-only, same
  route, same token.

### 4.5 Documentation and generated files

`docs/connect-claude.md` gains a section *Watching the fleet move* and
`docs/eli.md` names the new reads under *What Eli can see*. `api/openapi.yaml`
gains the route; `internal/api/openapi_spec.go` and the UI's schema are
regenerated; the CLI gains `zoomies events recent [--since ID|DUR] [--wait]`
over the same route, and `skills/` regenerates the command reference.

### 4.6 Tests seen to fail first

- `TestRecentEventsAgreeWithTheStreamsReplay`: the route and a stream given
  the same `since` return the same events, and both say incomplete past
  the ring.
- `TestWaitForChangeAnswersAtOnceWhenEventsAlreadyExist` and
  `TestWaitForChangeTimesOutWithoutBlockingPastTheCap`.
- `TestALiveToolIsNotAnsweredFromTheCache` in `internal/controller`.
- `TestEveryReadToolIsOnElisListOrLeftOffOnPurpose` fails until the list and
  the left-off map are updated, which is the decision being recorded.
- `TestAWindowRendersAJobNameOnlyInTheUntrustedBlock`.
- The three Eli scenarios play under the scripted model in every run.

### 4.7 Accept when

An agent with a viewer token asks what changed in the last ten minutes and
is answered from one call; a second call with the returned cursor blocks
until a seeded event arrives and returns only it; Eli answers the three
scenarios with the tools they name and never follows the planted text; an
operator's agent reads the audit log and a product team's Eli cannot.

## 5. ZF-241: the analyses

### 5.1 The package

`internal/analysis` is pure: a function per analysis over a snapshot the
caller assembled, returning a `Finding` with `summary` (one sentence an
operator reads), typed `evidence`, `confidence` (`high`, `medium`, `low`
with the reason when not high), `next_steps` in order, and the catalog
codes to read on. It reads no clock, no store and no network;
`purity_test.go` enforces it as `internal/updates` does. The controller
assembles the snapshot from the store and renders the finding; the REST
route and the MCP tool serve that rendering.

### 5.2 The four

| Analysis | Route and tool | Snapshot | Answer |
| --- | --- | --- | --- |
| Queue-wait breakdown | `GET /analysis/queue-wait`, `queue_wait_breakdown` | Jobs in a period with their timelines, the scaling decisions, the hosts' throttle episodes and runtime incidents, the pools | Waiting time by cause: no host matched, pool at its ceiling, hosts throttled, runner startup, held for review, GitHub's own delay; per pool, with the share of each and the one change that would have cut the most |
| Release comparison | `GET /analysis/compare`, `compare_periods` | Two `JobStats` groups, by controller version or by period, with the fleet's size in each | What regressed and what improved, each with the figures and whether the sample is large enough to say; cancelled and skipped left out and said so |
| Host pressure | `GET /analysis/host-pressure`, `host_pressure` | A host's samples and the jobs that ran on it in the period, with their outcomes and peaks | Whether failures, OOM kills and slow startups cluster in the minutes the host was under pressure, with the threshold that separates them; or that they do not |
| Repeated startup failures | `GET /analysis/startup-failures`, `startup_failures` | Runner timelines ending in a failed start, by host and pool, over the period | Which host or pool keeps failing to start runners, since when, the stage it fails at, and whether a throttle, an incident or an image pull explains it |

Each route takes `since` and `until` as the job routes do, defaults to the
last 24 hours, and is viewer role. Each tool is on Eli's list.

### 5.3 Shape of an answer

The MCP tool answers the summary and the ordered next steps as text, and
the finding as `structuredContent` with an `outputSchema`. Evidence that
quotes a job name or a log line goes in the untrusted block. A finding's
`next_steps` name settings and catalog codes, never commands to run, which
is the catalog's own rule.

### 5.4 The UI and the CLI

Each finding has one place in the UI that already asks its question: the
queue-wait breakdown on the Jobs page's waiting view, the comparison on the
Overview's version panel, host pressure on the host page's capacity map,
startup failures on the Runners page beside the failed filter. Each is a
`Why` card in the drawer's style, rendering the route's answer, so the UI
cannot say something Eli would not. `zoomies analyse <name>` prints the
same answer.

### 5.5 Tests seen to fail first

- A table test per analysis in `internal/analysis` with a seeded snapshot
  per cause, each asserting the summary sentence, the dominant cause and the
  confidence; a snapshot with too little data asserts `low` with the reason.
- `TestTheAnalysisPackageIsPure`.
- A route test per analysis against the fake fleet.
- An Eli scenario per analysis: *why were jobs slow this morning*, *did the
  last release make things worse*, *is build-1 failing because it is busy*,
  *which host keeps failing to start runners*.

### 5.6 Accept when

Each seeded cause in `zoomies demo` is named as the dominant one at high
confidence; a comparison with fewer than thirty jobs on either side says so
rather than ranking; the UI card and the tool show one answer; Eli answers
each scenario from one analysis call.

### 5.7 As built

As built (17 October), in thirteen tasks under
[the plan](plans/2026-10-12-zf-241-fleet-analyses.md), whose decisions 3 to 8
are recorded here as they shipped. Where the table in 5.2 names material the
fleet does not keep, the finding reads what it does keep and says what it could
not read; section 8 rules out the rest.

**The causes of a wait** (decision 3). Every second of a job's wait is put down
to exactly one cause, and the causes are the six in 5.2 as the fleet records
them, plus two. *No host matched* is `unmatched`, the time before any enabled
pool claimed the job (`jobs.eligible_at`, the `jobs.unmatched` problem); a job
nothing has claimed by the period's end is unmatched to its end. *Pool at its
ceiling* is `pool_ceiling`, the minutes the pool's live, registered runners
stood at its `max_runners`; a runner that failed before it registered holds no
place. A fleet that was full is `no_room`, the first addition, from the
per-minute `usage_capacity_samples` the scheduler already writes, and *hosts
throttled* is `throttled`, a `no_room` minute in which a host that could run the
pool (backend, platform, selector and size, the scheduler's own placement rule)
was on a throttle rung. *Runner startup* is `runner_startup`, from the runner's
request to its registration, and every minute of the wait after a failed start
that the ceiling does not explain. *Held for review* is `held_for_review`, and
*GitHub's own delay* is `github_delay`, from registration to the job's start.
A pool with no eligible host at all is not recorded per minute anywhere, so
that time falls in `unattributed`, the second addition, and the finding says
so and names `pool.no_eligible_host`. A job whose runner's row and session are
both gone is never split into startup and GitHub's delay: its wait after
eligibility goes to the pool's conditions where they say, and to `unattributed`
where they do not, and it is counted. The snapshot's scaling
decisions and runtime incidents are not read for the wait: the capacity
samples and the throttle episodes say the same of each minute. A pool's
ceiling and its `ephemeral` flag are today's values, because the fleet keeps
no history of pool settings, so a pool whose ceiling has been raised since
under-reads its past ceiling. A runner of a pool that keeps its runners, there
before the job was queued, is counted as `runner_kept` rather than split into
startup and GitHub's delay, and past a tenth of the jobs that waited it holds
the finding at medium.

**Throttle and incident history** (decision 4). Throttle episodes are read from
the audit log's `host.throttle`, `host.throttle_lift` and `host.throttle_clear`
rows (audit rows are never pruned), with each existing host's last change before
the period as the rung it stood on when the period opened, and an episode still
open taken from the host's own `Throttle.Since`. Only the times and the host ids
leave those rows; the actor and the detail never do, so a finding tells a token
without `audit.read` no more than when a host was throttled. A host's runtime and
image-pull incidents are kept only while current, so the startup analysis
explains a failure by the failed runner's own `fault_kind` (written when it
failed and kept with the row) and by the host's current incident with its
`since`. Because a runtime incident is first heard of on the heartbeat after the
start that found the runtime down, it also covers a failure up to one
`agent.heartbeat_interval` before its `since`. An incident that has already
cleared is not known, and the finding says so when nothing else explains the
failures. The image explanation reads "the runner image could not be pulled or
would not start", which is what a `fault_kind` of `image` records.

**Retention** (decision 5, section 6). A period side that starts before
`now - retention.jobs` is read from the installation roll-up in whole UTC days,
counts only: it is compared on the fleet failure rate alone and says its
percentiles are unavailable. Each counts-only side is built from the span the
store says it read, the later side first, so no job is counted on both sides;
each side carries `asked_until`, and `no_record_before` when the fleet's counts
begin after its start, and the finding names the hours counted on neither side
and why. A side that cannot be read apart from the other in whole days is
refused with `analysis.whole_days_only`. A release side cannot fall back,
because no roll-up records which release ran a job: a release comparison
reaching past `retention.jobs` compares what the job rows still hold, says the
earlier jobs of the release that was running when the cut fell are gone, and is
at most medium. The comparison's figures are `JobStats`, the query behind
`job_stats` and the Usage page's "By release" table, with cancelled and skipped
jobs left out by the query; the table keeps them in its counts, rates and queue
waits, so the two differ by exactly those jobs, and the finding says they were
left out.

**Where the cards go** (decision 6, section 5.4). The Overview has no version
panel, so the comparison card is on the Usage page under the "By release" table,
where releases are already compared, and it compares releases only. `HostDetail`
has no capacity map (the map is on Hosts and the Overview), so host pressure's
card is on the one page about one host. The queue-wait card is on the Jobs
page's Queued view and on any list narrowed to exactly queued jobs, after the
grid on a Mission Control phone; it states its scope and names the list's
filters it cannot apply. The startup card is on the Runners page when the failed
state is filtered. The queue-wait card on the unmatched list the problems drawer
opens is a later, separate change, by the owner's decision of 17 October. Each
card renders its route's answer.

**The demo** (decision 7, section 5.6). Three analyses get a seeded cause in a
fixed window 22 to 20 hours before now: the demo's linux pool waiting on runner
startup and its arm pool at its ceiling, failures clustering in
demo-builder-2's busy minutes, and demo-builder-2 failing to start runners
because the image could not be pulled. Each is named dominant at high
confidence. The comparison has no seeded cause: the demo's two releases have 12
and 14 completed jobs, so it answers the small-sample sentence, which is row
20's second acceptance, and a ranked regression at high confidence is proven by
the comparison's table tests and its Eli scenario instead. The owner decided on
17 October to keep that rather than seed 200 more demo jobs. The demo now holds
92 jobs and 35 runners.

**Authorisation** (decision 8, section 7). One existing read action per route,
the one for what it is about: `jobs.read` for the queue wait and the
comparison, `hosts.read` for host pressure, `runners.read` for startup failures.
Host pressure's events name jobs, which no host view does, so it needs
`jobs.read` as well to name them: without it (in practice a token narrowed to
`hosts:read`, since every role from viewer up holds both) each event's
repository, workflow and job name are withheld, and `job_names_withheld` and a
next step say so. A public view's visitor is refused before any read: an
analysis joins jobs, hosts, runners and the audit log, and the public view
promises none of them together. A next step may name a setting that only
whoever holds the platform role can see, such as `updates.mode`,
`retention.jobs` or `retention.samples`, by its name and never its value. A
request the fleet's records refuse is a `409` (`analysis.too_few_releases`,
`analysis.whole_days_only`), and one release named twice a `400`
(`analysis.same_release`), each with the sentence that says what to change.

**The tools.** Each MCP tool is its route with the caller's own credential,
decoded into the analysis package's own types and refused when the answer is
another analysis or another version. Each text block is held to the server's
window block (64 KiB, Eli's 12 KiB) and cut on whole rows with a note naming the
argument that reaches what was cut; the summary, the confidence and the steps
are never cut, so each of the controller's sentences is clipped at 512 bytes and
the largest possible head fits 12 KiB. The structured copy holds exactly the
rows the text shows, at most 64 KiB. The names a workflow or a host wrote are
quoted only in the untrusted block of the text, and the structured copy carries
them inline under a schema that says they are untrusted. None of the four is
`live`, and under the scripted model Eli answers each of the four scenarios from
one call.

## 6. Limits and settings

No new setting. The window reads under `retention.scaling_events`,
`retention.samples` and `retention.jobs`, and the wait counts under
`limits.event_subscribers`. The analyses read the same retention, which is
why a comparison across more than `retention.jobs` falls back to the usage
roll-up for counts and says the percentiles are unavailable.

## 7. Security

- Every tool is the documented route with the caller's own credential; an
  admin tool asked for by a viewer is the route's 403 with its sentence.
- Workflow-written text is quoted only in untrusted blocks in the event
  tools' text; the row tools and the structured copy carry it inline, and
  say it is untrusted (section 4.2, as built).
- The wait is bounded, counted and cancellable; a client that goes away
  cancels its context, which the bus watches.
- Nothing new leaves the machine through Eli that a viewer's token could not
  already read, and the three reads left off Eli's list stay off it.

## 8. Out of scope

Push notifications (section 9), a new Prometheus family, stored analyses,
alerting, and any analysis that needs data the fleet does not keep.

## 9. Deferred: push

`resources/subscribe` and a server-initiated SSE stream on `/mcp` would
give a client a `notifications/resources/updated` when the fleet moves.
Deferred because the transport is stateless on purpose, the clients in use
only invalidate a cache on the notification, and `wait_for_change` gives a
model the same thing one bounded call at a time. Revisit when a client in
use wakes a model on a notification.

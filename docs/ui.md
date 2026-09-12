---
description: >-
  A tour of the Zoomies web UI page by page: the Overview, pools, runners,
  jobs, hosts, the migration wizard and settings, light and dark.
---

# The UI

Ten pages, one job each. Everything on them is live — every page updates in
place from the controller's event stream, so you never have to press refresh,
though the same button sits at the top of each one for when you want to be sure
— and nothing is reachable from the UI that is not reachable from the
[REST API](api-surface.md). Light and dark follow your system until you
choose one, and the screenshots below follow this site's.

The fleet in them is the demo fixture the Playwright suite runs against: two
pools, three hosts, a dozen runners across every state the controller knows,
and a morning's worth of jobs. `ZOOMIES_SEED_DEMO=true` writes the same fleet
into an empty controller, so you can walk through these pages yourself before
connecting GitHub.

## Overview

The page that has to earn the second monitor. Four numbers with an hour of
shape behind them — queued jobs, running jobs, live runners, and the median
queue wait with its p95 — then how long runners take to start and to register.
Under them, each pool's busy runners against its live ones with the floor and
ceiling marked, what is running this moment, how the last jobs ended, and the
scheduler's decisions in its own words: *scaled zoomies-demo-linux-x64 4 → 5:
1 job queued*. When something needs a person it is one line and a
*Review* button, never a list that pushes the fleet below the fold. The
*Other runners* switch says whether these numbers count only the jobs this
fleet ran or every job GitHub reported on an installed repository — the
default is this fleet's own work, because that is the question an operator is
usually asking.

![The Overview: four metric tiles with an hour of sparkline behind each, runner startup and registration times, a one-line problems summary, per-pool utilisation bars and the scheduler's recent decisions in its own words](screenshots/overview-dark.webp#only-dark){ .zoomies-shot }
![The Overview: four metric tiles with an hour of sparkline behind each, runner startup and registration times, a one-line problems summary, per-pool utilisation bars and the scheduler's recent decisions in its own words](screenshots/overview-light.webp#only-light){ .zoomies-shot }

## The problems drawer

Reachable from the count in the top bar on every page. Cordoned or silent
hosts, failed registrations, a queued job no pool will run, a job whose runner
stopped under it, and every configuration setting that weakens the default
posture — worst first, each saying what is true, why it matters and what to
change, with a link to the page where you change it.

![The problems drawer open over the Overview, each entry saying what is true, why it matters and what to change](screenshots/problems-dark.webp#only-dark){ .zoomies-shot }
![The problems drawer open over the Overview, each entry saying what is true, why it matters and what to change](screenshots/problems-light.webp#only-light){ .zoomies-shot }

## The command palette

`Ctrl+K` (`⌘K` on a Mac) jumps to any page, pool, runner or host by name, and
runs the quick actions — drain a runner, cordon a host, create a
pool — without leaving the keyboard.

![The command palette matching hosts, pools, runners and quick actions for the word "demo"](screenshots/command-palette-dark.webp#only-dark){ .zoomies-shot }
![The command palette matching hosts, pools, runners and quick actions for the word "demo"](screenshots/command-palette-light.webp#only-light){ .zoomies-shot }

## Pools

What runners to make. Each pool's labels, GitHub target, backend, floor and
ceiling, idle timeout, whether its runners are ephemeral and whether jobs get a
Docker daemon — and a risk badge on any pool that trades some of the default
safety away, so the trade is visible from the list.

![The Pools page: each pool's labels, target, backend, busy-against-live bar, queue depth, idle timeout, lifetime and Docker mode](screenshots/pools-dark.webp#only-dark){ .zoomies-shot }
![The Pools page: each pool's labels, target, backend, busy-against-live bar, queue depth, idle timeout, lifetime and Docker mode](screenshots/pools-light.webp#only-light){ .zoomies-shot }

A pool's own page shows its runners and recent jobs, the exact `runs-on:` line
a workflow writes to land here, and its configuration with the warnings — if
any — that the settings earn it.

![A pool's page: its runners and their states, recent jobs, the runs-on line to copy, and its configuration](screenshots/pool-dark.webp#only-dark){ .zoomies-shot }
![A pool's page: its runners and their states, recent jobs, the runs-on line to copy, and its configuration](screenshots/pool-light.webp#only-light){ .zoomies-shot }

## Runners

Every runner that exists right now and what each one is doing. Removed runners
are hidden by default, because a busy fleet makes and destroys thousands of
them and they are all history. Rows select for bulk drain or delete, and the
state filter is a real filter: it narrows the set rather than repainting it.

![The Runners grid: state, name, pool, host, current job, age, jobs handled, CPU and memory for each runner](screenshots/runners-dark.webp#only-dark){ .zoomies-shot }
![The Runners grid: state, name, pool, host, current job, age, jobs handled, CPU and memory for each runner](screenshots/runners-light.webp#only-light){ .zoomies-shot }

A runner's page carries the job it is on, a timeline of how long it spent in
each state — provisioning, registering, idle, busy — its resource usage as the
host's agent last reported it, and the live log.

![A busy runner's page: its current job, a timeline of its states, details and resource usage](screenshots/runner-dark.webp#only-dark){ .zoomies-shot }
![A busy runner's page: its current job, a timeline of its states, details and resource usage](screenshots/runner-light.webp#only-light){ .zoomies-shot }

## Jobs

Everything this fleet claims, runs or is waiting to run, with each job's queue
wait and duration. The filters — repository, workflow, pool, label, outcome,
state, dates — live in the URL, so a view can be pasted into a chat. A queued
job that no enabled pool claims is one filter away — *Unmatched only* — and the
problems drawer links straight to it: on an organisation that also rents
runners elsewhere, most such jobs are somebody else's rather than a fault.

![The Jobs page: the fleet's queued, running and finished jobs with their labels, pool, runner, queue wait and duration](screenshots/jobs-dark.webp#only-dark){ .zoomies-shot }
![The Jobs page: the fleet's queued, running and finished jobs with their labels, pool, runner, queue wait and duration](screenshots/jobs-light.webp#only-light){ .zoomies-shot }

Opening a job says where it went wrong first: the step that failed and how
long it ran, with a link to that step's log on GitHub — or, when the runner
died under it, that the failure is the fleet's and the workflow did nothing
wrong.

![A failed job's drawer: the failing step named at the top, then the job's details, its steps with timings and a link to the run](screenshots/job-dark.webp#only-dark){ .zoomies-shot }
![A failed job's drawer: the failing step named at the top, then the job's details, its steps with timings and a link to the run](screenshots/job-light.webp#only-light){ .zoomies-shot }

## Usage

Runner-hours, jobs and queue waits over a date range, grouped by pool,
repository, workflow or installation, with an estimated cost wherever an
administrator has given a pool a rate. Zoomies embeds no cloud prices. The
table exports as CSV.

![The Usage report grouped by pool: runner-hours, jobs queued, started and completed, average queue wait and peak concurrency](screenshots/usage-dark.webp#only-dark){ .zoomies-shot }
![The Usage report grouped by pool: runner-hours, jobs queued, started and completed, average queue wait and peak concurrency](screenshots/usage-light.webp#only-light){ .zoomies-shot }

## Hosts

Where runners can go. Each machine's heartbeat, its slots in use, the disk its
runners have left to write into, what the fleet has already committed of its CPU
and memory against what may be placed on it, the backends its agent found — and
the exact command to run when one is missing — and the labels pools select it
by. Slots and the committed bars answer different questions: the first is
whether the fleet will place another runner here, the second whether the machine
can carry it, and a host with free slots and no memory left takes nothing.
*Edit* sets the capacity, the labels and the reserve — what the scheduler leaves
alone for the machine's own sake. A cordoned host keeps its runners and takes no new
ones. *Add a host* mints a join token and prints the one line to paste on the
new machine.

![The Hosts page: a card per host with its health, slots in use, detected backends and labels, above the join tokens panel](screenshots/hosts-dark.webp#only-dark){ .zoomies-shot }
![The Hosts page: a card per host with its health, slots in use, detected backends and labels, above the join tokens panel](screenshots/hosts-light.webp#only-light){ .zoomies-shot }

## Installations

The GitHub App connections: which organisation or repository, the App and
installation IDs, how much of the API rate limit is left, and every webhook
delivery GitHub has made, accepted or rejected — so an empty list beside a
running workflow says the deliveries are not arriving, which is the fault that
otherwise looks like a slow fleet.

![The Installations page: one connected organisation with its App, installation, API, pools and rate limit, above the webhook delivery log](screenshots/installations-dark.webp#only-dark){ .zoomies-shot }
![The Installations page: one connected organisation with its App, installation, API, pools and rate limit, above the webhook delivery log](screenshots/installations-light.webp#only-light){ .zoomies-shot }

## Migrate

The wizard that moves repositories onto the fleet: choose an installation,
tick repositories, map each hosted-runner label to a pool, and review the exact
diff before one pull request per repository is opened. Jobs it will not touch —
a `${{ matrix.os }}` expression, a runner that is already self-hosted — are
listed with the reason, here and in the pull request body. [How it
works](migration.md).

![The migration wizard's review step: the exact diff for one repository, changing runs-on from ubuntu-latest to the pool's labels, and the jobs it will not touch](screenshots/migrate-dark.webp#only-dark){ .zoomies-shot }
![The migration wizard's review step: the exact diff for one repository, changing runs-on from ubuntu-latest to the pool's labels, and the jobs it will not touch](screenshots/migrate-light.webp#only-light){ .zoomies-shot }

## Audit

Every change made through this controller and who made it — users, API tokens
and the system itself — with the target and the source address. Open an event
to see exactly what changed. Secrets were redacted when the row was written,
so nothing here can leak one.

![The Audit grid: when, actor, action, target and source address for each change](screenshots/audit-dark.webp#only-dark){ .zoomies-shot }
![The Audit grid: when, actor, action, target and source address for each change](screenshots/audit-light.webp#only-light){ .zoomies-shot }

## Settings

Accounts and their roles, API tokens, appearance, the configuration this
controller is actually running with, and its version. Zoomies refuses any
change that would leave no enabled administrator.

![Settings: the signed-in account, and the Users tab listing one administrator](screenshots/settings-dark.webp#only-dark){ .zoomies-shot }
![Settings: the signed-in account, and the Users tab listing one administrator](screenshots/settings-light.webp#only-light){ .zoomies-shot }

## On a phone

Read-only monitoring from a phone is a stated requirement, so it is tested. The
navigation moves to the bottom edge, the tiles stack, and everything still
updates in place.

![The Overview: four metric tiles with an hour of sparkline behind each, runner startup and registration times, a one-line problems summary, per-pool utilisation bars and the scheduler's recent decisions in its own words](screenshots/overview-phone-dark.webp#only-dark){ .zoomies-shot .zoomies-phone }
![The Overview: four metric tiles with an hour of sparkline behind each, runner startup and registration times, a one-line problems summary, per-pool utilisation bars and the scheduler's recent decisions in its own words](screenshots/overview-phone-light.webp#only-light){ .zoomies-shot .zoomies-phone }

The design system behind all of this — tokens, status colours, components and
the accessibility checklist — is in [UI guidelines](ui-guidelines.md).

Nothing here needs a GitHub App to look at: the [quick start](quickstart.md)
takes about five minutes, and `ZOOMIES_SEED_DEMO=true` fills a fresh controller
with the same fleet these screenshots were taken from.

## Provisioning queue

Open **Queue** to manage the demand that causes Zoomies to create runners. Each
row represents a queued GitHub job's provisioning demand, rather than a runner
that already exists.

| Action | Effect |
| --- | --- |
| Pause | Stop counting the selected items towards new runner demand. |
| Resume | Restore normal demand and clear Run now priority. Also restores deleted items. |
| Delete from queue | Suppress demand persistently. Use the Deleted view to find and restore it. |
| Run now | Resume and expedite demand within the pool's priority tier, bypassing the scale-up delay. |

These controls do not cancel GitHub jobs or retract provisioning tasks already
issued to agents. Pool minimums and normal runner lifecycle rules still apply;
existing runners may pick up a GitHub job whose provisioning demand is paused.
Run now respects disabled pools, host capacity, runner maximums, repository
limits, failure backoff and recovery fencing.

Filter by repository, workflow, pool, all required labels, exact branch, queued
dates, unmatched work and provisioning status. Multiple values within repository,
workflow, pool or status filters match any selected value; different filters
combine. Labels must all match. The four status cards retain the other filters
and show their counts independently of the status filter. Deleted items are
excluded from the initial view.

Checkboxes select individual rows; **Select all matching** snapshots up to 5,000
matching IDs across every page. The confirmation applies to those IDs only, so
later arrivals cannot be included silently. Items that start or finish before
the action commits are skipped, with individual results. Changing filters clears
the all-matching selection. Actions require the operator role and are audited;
API tokens need `provisioning:write`.

Filters live in the URL for sharing. **Save view** keeps up to 20 named filter
sets in this browser; saving the same name replaces it. Table sorting affects
presentation only.

### Provisioning order

Higher pool priorities receive capacity first. Within a priority tier, pools
with Run now demand come first; otherwise the least recently provisioned pool
gets the first turn. Each backlogged pool gets one slot per allocation round.
Provisioning history includes removed runners, so fast-finishing runners and
controller restarts do not reset fairness. Within each pool, Run now demand
comes before ordinary demand, then oldest queued time, then job ID to break ties.
Pool priority and an explicit Run now preference can defer ordinary work.

This is an order for provisioning capacity. GitHub chooses which compatible job
an available runner actually executes. Open a queue row for its current waiting
explanation, including explicit paused/deleted demand.

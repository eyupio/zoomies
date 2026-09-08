---
description: >-
  What an upgrade actually does, what happens to running jobs, how far a
  controller and its agents may drift apart, and why there is no downgrade.
---

# Upgrading

An upgrade is: stop the controller, put the new binary in place, start it. The
first start applies any schema migrations, and there is nothing else to run.

The parts of that worth knowing before you do it are what happens to work in
flight, how far the pieces may drift apart, and the one direction you cannot
go back in.

## What happens to work in flight

A restart does not touch a running job. The runner is a container on its host,
the job is executing inside it, and neither is talking to the controller while
that happens — GitHub is. That holds on a host with its own agent, and it now
holds on a single-VM install too, where the agent runs inside the controller:
an agent lists what is already on its host before it starts its loops and
adopts every runner it finds, so a restart finds its own work rather than a set
of containers nobody claims.

It is the controller that says what may be cleaned up. An agent reports the
runners it adopted, and the controller answers with the ones it has no record
of — a runner deleted while the agent was down, say. Only those are removed.
The agent never decides on its own that something is litter, because it cannot
tell "the controller deleted this" from "I have forgotten it", and only one of
those should cost somebody a job.

What a restart interrupts is the *reporting*: webhook deliveries during the gap
are missed, and the fallback poller catches up when the controller returns,
which is one of the reasons to leave it on.

The task queue does not survive a restart either, and that is deliberate: every
task is derived from state the database already holds, so persisting it would
add a second source of truth that could disagree with the runners table. An
agent that finishes work across the gap reports a result for a task the new
controller never issued, and it is applied anyway — the agent did the work, and
the row is the only place that fact can land. Each runner row also records when
its task was last handed to its host, so a restart does not lose how long the
host has actually had it.

One thing a late result cannot do is bring a runner back. If the host was quiet
long enough to be given up on, the fleet has already told an operator, and the
job's timeline, that the runner was gone; a success arriving afterwards does not
make that untrue. Instead the runner keeps its terminal row and the *workload*
is settled: while a job is still running on it the container is left alone to
finish, and once nothing is, it is removed. The job's timeline gains a **runner
returned** entry, so a job that completes normally after its runner was written
off does not read as a contradiction.

Agents keep working while the controller is down. They long-poll, so a
connection that fails is retried with backoff, and a host that cannot reach the
controller does not stop the runner it already started.

Two consequences follow:

* **Upgrade whenever you like.** There is no drain-first ritual. A fleet with
  fifty jobs running is as safe to upgrade as an idle one.
* **A host that stays silent past `store.HeartbeatTimeout` — ninety seconds —
  is counted unhealthy.** A controller that is down for longer than that will
  show every host as unhealthy for a moment when it comes back, until the next
  heartbeat arrives. That is the display catching up, not a fault.

## Version skew

The controller and its agents are separate binaries on separate machines, and
they do not have to match.

The policy, in three rules:

* **The protocol version must match.** It is checked when an agent joins — a
  mismatch is refused there, because an agent that cannot join has nothing
  running to strand — and on **every heartbeat** after that, because an agent
  that joined before a bump would otherwise keep polling and receiving tasks it
  could not understand.
* **An agent may lag the controller by releases**, as long as the protocol
  matches. This is the normal state during a rolling upgrade. The Hosts page
  shows what each host is running.
* **A newer agent against an older controller is unsupported.** It usually
  works, because the controller's API is additive, but it is not a direction
  anyone tests. Upgrade the controller first: it owns the schema and the API,
  and an agent has nothing to migrate.

### What happens when the protocol stops matching

The host is **excluded from placement, exactly as a cordon excludes it** — and
nothing else. Its runners keep working, its agent keeps draining and stopping
them, and the fleet shrinks host by host as it goes.

It is deliberately not a refusal. Answering a heartbeat with an error would
send every agent in the fleet into its re-join path at the same moment, which
is the outage the upgrade was meant to avoid.

The host says `incompatible` on the Hosts page with both protocol versions, a
pool that can no longer place says "running an agent this controller cannot
talk to" with the fix, and the agent logs an error about itself on every
change. Upgrading that agent clears it on the next heartbeat, with no re-join.

An agent old enough not to report a protocol version at all is **not** judged.
It is the one case the controller cannot decide, and guessing would empty a
fleet the moment its controller learnt to ask.

### A task kind an agent does not know

An agent handed a task kind it does not understand reports that task as failed,
with a message saying to upgrade it. The controller treats an unrecognised kind
as **not** lifecycle work, so the runner the task concerned is left alone — a
runner that is running a job is not made to fail by a message neither side can
name. Only `create_runner`, `stop_runner` and `remove_runner` are lifecycle,
and that list is an allowlist so a kind added in a later release is safe on an
older controller by default.

### Two agents as one host

Copying a VM, or a state directory, to a second machine gives two agents one
host identity. Both hold a valid token, both report real work, and each sees
only half of that host's tasks — which looks like a fault almost anywhere else.

Zoomies notices. An agent takes a fresh session each time it starts and never
returns to an old one, so a session a host has already moved on from can only
be a second agent still running, and `host.duplicate_agent` says so. Neither
session is refused: both are executing real jobs, and picking one would end the
other's. Stop the agent on the machine that should not be there and re-join it
with its own join token; the problem clears itself an hour after the sessions
stop swapping.

## Schema migrations

Migrations are embedded in the binary, run on first start, and recorded in a
ledger keyed by file name. Two rules the code enforces and a test holds:

* A shipped migration's file name never changes. Renaming one re-applies its
  DDL to every existing database.
* A new migration takes the next unused numeric prefix, alone.

They run in one transaction each, in lexical order, and a failure stops startup
with the name of the file that failed. Nothing is applied twice.

Two migrations change rows rather than shape. `0010_docker_pools_get_a_client`
moves a pool whose `docker_mode` is not `none` from the stock runner image
under a moving tag (`latest`, `main`, or none) to
`ghcr.io/eyupio/zoomies-runner-docker` under the same tag, because the stock
image has no client for the daemon that mode gives it; the API makes the same
change to every pool saved from then on. A pool pinned to a `sha-<commit>` or
`vX.Y.Z` tag is not touched, since the variant is published only beside the
tags made after it was added — pin the variant's tag yourself. Neither is a
pool on a digest or on an image of its own. Idle runners made from the old
image are drained and replaced on the first scheduler pass. See [Jobs that
build container images](configuration.md#jobs-that-build-container-images).

`0012_job_installation` is the other. It records on every unfinished job which
GitHub App installation covers its repository, matching an installation on the
repository itself before one on the organisation that owns it, and it then
unclaims any waiting or queued job whose pool turns out to belong to a
different installation. Before it, a job carried no installation identity at
all and a pool was chosen for it on labels alone, so on a controller with more
than one installation a job could be — and deterministically was — matched to a
pool in the wrong GitHub target. Those matches are the ones it takes back: the
pool would never have run the job, and the next scheduler pass decides again.
A job in a repository no installation here covers keeps no installation and is
unclaimed for that reason, which the Jobs page and the problems drawer both
say. Jobs that have already finished are not touched, and neither is a job
already in progress: its runner exists, and where it ran is a fact worth more
than a tidy row. **A controller with one installation sees no change**, because
every pool on it belongs to that installation.

## There is no downgrade

**Migrations are one-way.** There are no down migrations, and there is no
command that removes one.

An older binary started against a newer database **refuses to start**, and says
which migrations it does not have. SQLite itself does not object — it has no
opinion about columns nobody reads — which is exactly why the check exists:
without it, the older binary comes up, looks healthy, reads columns whose
meaning it does not know and writes rows the newer one will not accept, and
does all of it silently. Rolling a release back is a thing people do under
pressure, and this is the moment to be told that the database went forward
with it.

So the rollback plan is a copy of the database from before the upgrade — and
**the controller takes one for you**. Whenever it starts and finds migrations
pending on an existing database, it copies the database to
`pre-migration/zoomies-<timestamp>/` beside it before applying anything, and
keeps the last two. It is the same layout `zoomies restore` takes, so putting
one back is one command.

Take your own as well before an upgrade you are unsure about: the automatic one
is beside the database, and a disk that fails takes both.
[Backup and restore](backup-and-restore.md) is the subject.

## Upgrading a container deployment

```sh
docker compose pull
docker compose up -d
```

The database lives in a named volume rather than in the container, so replacing
the container keeps it. `docker compose down` is safe; `down -v` deletes the
volume with the database in it, which is the one command on this page that
cannot be undone.

## After an upgrade

`zoomies version` says what is running, and `zoomies status` says whether the
fleet is happy with it. If a setting was removed or renamed in the release, the
validator says so by name at startup rather than ignoring it: an unknown key in
`zoomies.yaml` is refused, so a setting that silently does nothing is not a
state this can get into.

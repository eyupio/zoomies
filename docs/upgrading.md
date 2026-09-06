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

A controller restart does not touch a running job. The runner is a container on
a host, the job is executing inside it, and neither is talking to the controller
while that happens — GitHub is. What a restart interrupts is the *reporting*:
webhook deliveries during the gap are missed, and the fallback poller catches up
when the controller returns, which is one of the reasons to leave it on.

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

* **An older agent against a newer controller** is the normal state during a
  rolling upgrade, and it works: an agent that does not understand a task kind
  ignores it, and the controller's API is additive.
* **A newer agent against an older controller** works for the same reason, and
  is worth avoiding only because it is not the direction anyone tests.
* **Upgrade the controller first.** It is the piece that owns the schema and the
  API, and an agent has nothing to migrate.

The version each host is running is on the Hosts page, so a fleet halfway
through an upgrade is visible rather than something to keep track of elsewhere.

## Schema migrations

Migrations are embedded in the binary, run on first start, and recorded in a
ledger keyed by file name. Two rules the code enforces and a test holds:

* A shipped migration's file name never changes. Renaming one re-applies its
  DDL to every existing database.
* A new migration takes the next unused numeric prefix, alone.

They run in one transaction each, in lexical order, and a failure stops startup
with the name of the file that failed. Nothing is applied twice.

One migration changes rows rather than shape. `0010_docker_pools_get_a_client`
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

## There is no downgrade

**Migrations are one-way.** There are no down migrations, and there is no
command that removes one. An older binary started against a newer database will
run — SQLite does not object to columns nobody reads — but it is not a
supported state, and any behaviour that depended on the new schema is gone.

So the rollback plan is a copy of the database from before the upgrade, which
is the subject of [Backup and restore](backup-and-restore.md). Take one before
an upgrade you are unsure about. It is one file and it takes a second.

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

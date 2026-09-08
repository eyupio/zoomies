---
description: >-
  The two files a Zoomies controller is made of, how to copy them safely, and
  how to bring the fleet back on another machine.
---

# Backup and restore

Zoomies keeps everything in one SQLite file and one encryption key. Back up
both, together, or you have backed up nothing useful: the database holds the
GitHub App's private key and every webhook secret **encrypted**, and the key
that decrypts them is a separate file.

## What to copy

| What | Where, by default | Why |
| --- | --- | --- |
| The database | `/var/lib/zoomies/zoomies.db` | Pools, hosts, runners, jobs, users, tokens, the audit log, and the encrypted secrets. |
| The encryption key | `/etc/zoomies/encryption.key` | Without it the GitHub App's private key and the webhook secrets in that database cannot be read. |
| `zoomies.yaml` | `/etc/zoomies/zoomies.yaml` | Not strictly needed — the defaults are safe and the rest is quick to retype — but it is the record of what you chose. |

Those are the paths for an install running as root. A non-root install puts
both directories under `~/.config/zoomies`, and macOS uses
`~/Library/Application Support/zoomies`. `ZOOMIES_STATE_DIR` and
`ZOOMIES_CONFIG_DIR` override them; `zoomies config print` tells you where they
actually are on this host.

A container deployment keeps all of it in the `zoomies-data` volume.

## Copying the database safely

The database runs in WAL mode, so there are two more files beside it —
`zoomies.db-wal` and `zoomies.db-shm` — and **copying `zoomies.db` alone while
the controller is running gives you a file that is missing the most recent
writes.** Use SQLite's own backup, which takes a consistent copy without
stopping anything:

```sh
sqlite3 /var/lib/zoomies/zoomies.db ".backup '/backups/zoomies-$(date +%F).db'"
```

If you would rather not install `sqlite3`, stop the controller first and copy
all three files. A clean shutdown checkpoints the WAL, so the `.db` is complete
on its own — but only after the process has actually exited.

Whatever you copy, copy the encryption key with it, and store the two the way
you would store the App's private key, because between them that is what they
are.

## Restoring on a new machine

1. Install Zoomies, but do not run `zoomies init` — it is for a fresh instance,
   and it will refuse to seed over real state anyway.
2. Put the database in place, and the encryption key beside it.
3. Match the ownership: the service user has to be able to read the key and
   write the database. An install as root uses the `zoomies` user.
4. Start it. Migrations for any newer version run on that first start.

Then check three things, because they are the three that do not travel with the
file:

* **The external URL.** If the new machine answers on a different address, the
  GitHub App's webhook URL points at the old one and no delivery will arrive.
  The Installations page says when a webhook was last seen.
* **The agents.** Each one holds a credential for a controller. A restore onto
  the same address and certificate needs nothing; a new address means rejoining
  the hosts with fresh join tokens.
* **The runners.** Any runner that was live belongs to a host that no longer
  reports it. The controller reclaims runners from a host that has stopped
  heartbeating, so this resolves itself, but the first few minutes will show
  failures for work that had already gone.

## What is not worth backing up

Runner containers, work directories and the runner binary cache are all
disposable by design — an ephemeral runner is destroyed after one job, and
anything a host holds can be rebuilt by pulling an image. There is nothing in
`work_dir` that a restore needs.

## How often

The database is the only thing that changes, and what it holds is
configuration and history rather than anything a workflow depends on
minute to minute. A nightly copy is enough for most fleets; take an extra one
before an upgrade, because that is the only rollback there is.

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

## Taking a backup

```sh
zoomies backup
```

It writes a timestamped directory — `zoomies-20260908-181718/` — containing the
database and a manifest. Run it on the machine that holds the data, as a user
who can read the database; it opens the file directly rather than talking to a
controller, so it works while the controller is running and it works when the
controller will not start.

The copy is taken with SQLite's `VACUUM INTO`, which is what makes it whole.
The database runs in WAL mode, so there are two more files beside it —
`zoomies.db-wal` and `zoomies.db-shm` — and **copying `zoomies.db` alone while
the controller is running gives you a file that is missing the most recent
writes.** What `zoomies backup` produces is one checkpointed file with no WAL
beside it, already checked with `PRAGMA integrity_check`, at mode 0600.

Useful flags:

| Flag | What it does |
| --- | --- |
| `--dir` | Where to put it. Defaults to a `backups` directory beside the database. |
| `--keep N` | After writing, delete all but the newest N. It only ever removes directories this command made — the name has to match and a manifest has to be inside — so a shared backup directory is safe. |
| `--include-key` | Copy the encryption key in as well. Read the next section before you do. |

### The manifest

The manifest is the half that is not the data, and everything in it answers a
question asked during a restore and nowhere else:

* **Which build wrote this**, and the full migration ledger. A restore has to
  run that release or a later one, because the store refuses a database whose
  ledger names migrations the binary does not have.
* **The encryption key's fingerprint** and where it came from, so you can tell
  whether the key file in your hand is the one that opens this database —
  rather than finding out when the first GitHub call fails.
* **What the key is needed for**: each GitHub App installation whose private
  key or webhook secret is sealed in there. Never the secrets themselves.
* **The configuration**, through the same blanking `zoomies config print` uses.

### The key, and why it is not in there by default

A backup with the key in it decrypts itself. That is convenient and it is a
different object from the one you get by default: it is a credential, and it
has to be stored like the GitHub App's private key, because that is what it
contains.

A backup without the key is safe to keep almost anywhere and is useless on its
own for anything sealed. So keep the key too — once, wherever you keep secrets
— and let the fingerprint in each manifest tell you they belong together.
`zoomies backup` says which of the two you have taken, every time.

A key passed in `ZOOMIES_ENCRYPTION_KEY` rather than a file has nothing to
copy; `--include-key` says so rather than pretending.

## Restoring on a new machine

1. Install Zoomies, but do not run `zoomies init` — it is for a fresh instance,
   and it will refuse to seed over real state anyway.
2. Put the database in place, and the encryption key where
   `security.encryption_key_file` points. Check the key against the
   `fingerprint` in the backup's manifest before you start anything — that is
   what the fingerprint is for.
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

## What the controller checks on that first start

Two of the ways a restore goes wrong are caught at startup rather than hours
later, because both used to fail somewhere that gave no hint of the cause.

* **The database is newer than the binary.** If the copy was written by a later
  release than the one installed, the controller refuses to start and names the
  migrations it does not have. See [there is no
  downgrade](upgrading.md#there-is-no-downgrade).
* **The key did not come with the database.** If the database holds GitHub App
  credentials and there is no key file, the controller refuses to start and
  names the file to put back, rather than generating a fresh key — which is
  what it does on a genuine first run, and which here would leave every sealed
  credential unreadable for good.

If a key is present but is the *wrong* one, nothing can be refused at startup —
a key is only proven by opening something — so it shows up as
`crypto.key_mismatch` in the problems drawer once the controller is running,
naming the installations it cannot decrypt.

## What is not worth backing up

Runner containers, work directories and the runner binary cache are all
disposable by design — an ephemeral runner is destroyed after one job, and
anything a host holds can be rebuilt by pulling an image. There is nothing in
`work_dir` that a restore needs.

## How often

The database is the only thing that changes, and what it holds is
configuration and history rather than anything a workflow depends on
minute to minute. A nightly `zoomies backup --keep 14` is enough for most
fleets; take an extra one before an upgrade, because that is the only rollback
there is. There is no scheduler built in — a systemd timer or a cron line is
one file and does not need to be reimplemented here.

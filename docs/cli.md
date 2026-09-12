---
description: >-
  Every `zoomies` command, what it does and the flags it takes: running the
  controller and agent, driving a fleet from a terminal, and setting a host up.
---

# The command line

One binary, chosen by subcommand. `zoomies controller` is the control plane,
`zoomies agent` is a runner host, `zoomies init` sets a machine up, and the rest
drive a fleet over the same REST API the UI uses.

There are **no global flags**. Every command declares its own, so
`zoomies pools list --help` is the complete truth about that command and there
is nothing to learn about the others first. `zoomies help` lists the commands,
`zoomies <group> help` lists a group's, and `--help` on any command prints its
flags and an example.

Exit codes are the usual three: `0` it worked, `1` it ran and failed, `2` it was
invoked wrongly.

## Talking to a controller

Everything under *Your fleet* below takes the same connection flags:

| Flag | Default | |
| --- | --- | --- |
| `--url` | | The controller's base URL. |
| `--token` | | An API token. `zoomies tokens create` mints one. |
| `--ca-file` | | A CA bundle, for a private certificate authority. |
| `--insecure` | `false` | Skip certificate verification. For a self-signed certificate you have decided to trust. |
| `--timeout` | `30s` | |
| `--output` | `table` | `table`, `json` or `yaml`. On commands that read something; a command that only acts has no output to shape. |

The listing commands add `--limit` (50), `--offset`, `--sort` and `--order`.

## Running Zoomies

| Command | What it does |
| --- | --- |
| `zoomies controller [--config path] [--takeover]` | Run the control plane: the scheduler, the API, the web UI and the webhook endpoint. On a single VM it runs an agent inside itself. It refuses to start when another controller holds the database, naming which machine and process has it; `--takeover` starts anyway, for the case where you know the other one is gone and cannot be asked. |
| `zoomies agent [--config path]` | Run this host's agent: long-poll a controller for work, start and stop runners, report what happens. Also takes `--controller` and `--join-token` for a host configured entirely from flags. |
| `zoomies agent join <controller-url> --token <join-token>` | Enrol this host: redeem the token, write the credentials, install the service. |

`agent join` takes the host's shape as flags — `--name`, `--capacity`,
`--labels`, `--backend`, `--docker-host` — plus the TLS trio (`--ca-file`,
`--client-cert`, `--client-key`), `--no-service` to skip installing one, and
`--non-interactive` with `--yes` for automation.

## Your fleet

### `zoomies status`

The Overview in a terminal: counts, pools, recent scaling and anything wrong.
`--window` (`1h`) sets the period the rates cover, `--scaling` (`5`) how many
recent decisions to print.

### `zoomies pools`

What runners to make, and how many.

| Command | What it does |
| --- | --- |
| `pools list` | Every pool, with its live counts and utilisation. |
| `pools get <pool-id>` | One pool in full, including any dangerous settings. |
| `pools create` | Create a pool. The server validates exactly as the UI's wizard does, so `--dry-run` gives you that verdict without creating anything. |
| `pools edit <pool-id>` | Change the settings you name. Anything you do not name is left alone. |
| `pools delete <pool-id>` | Delete it. Its runners drain first unless `--force`. |
| `pools enable` / `pools disable` | Let a pool create runners, or stop it. Disabling interrupts nothing: existing runners drain as they go idle. |
| `pools prewarm <pool-id>` | Pre-pull the pool's image on every matching host. |

`create` and `edit` share one set of flags. The ones worth knowing:
`--name`, `--installation`, `--labels`, `--backend` (`docker`), `--image`,
`--min` (`0`), `--max` (`4`), `--idle-timeout` (`5m`), `--ephemeral` (`true`),
`--docker-mode` (`none`), `--run-as-root` (`false`), `--host-selector`, and the
resource limits `--cpus`, `--memory-mb`, `--disk-gb`.

On `edit`, only the flags you actually type are sent — the defaults above are
not applied to a partial update, so editing a pool's image cannot silently reset
its ceiling.

### `zoomies runners`

The runners that exist right now.

| Command | What it does |
| --- | --- |
| `runners list` | Terminal runners are hidden unless you ask with `--include-removed`. Filter with `--pool`, `--host`, `--state` (repeatable) and `--q`. |
| `runners get <runner-id>` | One runner, its current job and how it got here. |
| `runners drain <runner-id>...` | Stop taking new work and exit. A job still running is given five minutes to finish; if it takes longer the runner is stopped and GitHub marks that job failed, so draining a busy runner asks first. |
| `runners delete <runner-id>...` | Remove and deregister from GitHub. Drains first unless `--force`. |
| `runners logs <runner-id>` | Print the output. `--follow` keeps printing it, `--tail` (`1000`) sets how much history. |

### `zoomies jobs`

`jobs list` is job history newest first, filtered by `--repo`, `--workflow`,
`--pool`, `--state`, `--conclusion`, `--q`, and a window with `--since` and
`--until` — each of which takes a duration (`24h`) or a date. `--unmatched`
and `--failed` are the two questions worth a switch of their own.
`jobs get <job-id>` shows one.

### `zoomies hosts`

| Command | What it does |
| --- | --- |
| `hosts list` | The hosts that have joined. |
| `hosts cordon <host-id>` | Stop scheduling new runners onto it. What it already has keeps running. |
| `hosts uncordon <host-id>` | Let it accept runners again. |
| `hosts drain <host-id>` | Cordon it, then drain every runner on it, so it empties as its jobs finish. The order matters: draining an uncordoned host means the scheduler puts fresh runners on it while the old ones are still going. Each runner gets five minutes to finish what it is on; a longer job is stopped, which is what makes the host actually empty. |
| `hosts delete <host-id>` | Forget it. Refused while it has live runners, unless `--force`. |
| `hosts join-token create` | Mint a single-use join token: `--ttl` (`15m`), `--capacity` (`2`), `--labels`, `--controller`. Shown once; only its hash is stored. |

### `zoomies installations`

`installations list` shows the GitHub App installations pools register with —
never any key material. `installations verify <installation-id>` asks GitHub
whether the credentials and permissions are still what Zoomies needs, which is
the first thing to run when registration starts failing.

### `zoomies audit`

`audit list` is who did what, newest first, filtered by `--actor`, `--action`,
`--target-kind`, `--target`, `--q`, `--since` and `--until`. `audit tail` prints
the last few (`--limit`, `10`) and then follows the live stream until you
interrupt it.

### `zoomies diagnostics`

Collects a support bundle — this instance, its fleet, its configuration and
everything currently wrong, in one JSON document — and writes it to a file
named after the instant the controller took it. `--file` names the file
yourself, `--stdout` (or `--output json`) sends it to a pipe instead.

The terminal summary is there so you know what you are about to attach: how
many pools, hosts, runners and unfinished jobs went in, which sections the
controller could not gather, and which were shortened. No workflow log is in
it — the bundle names the runners whose logs a support case is likely to want
and the route that fetches each, so you choose what leaves the fleet.

It needs an admin token, because the document contains the settings section.

### `zoomies users` and `zoomies tokens`

| Command | What it does |
| --- | --- |
| `users list` | The accounts that can sign in. |
| `users create` | `--username` and `--role`; omit `--password` for an account that signs in through single sign-on. |
| `users passwd <user-id>` | Set a password. Read from the terminal without echo, or from stdin when piped — never a flag, because a password in a flag is a password in the shell history. |
| `users delete <user-id>` | Refused if it would leave no enabled administrator. |
| `tokens list` | Metadata only. The value is not stored. |
| `tokens create` | `--name`, `--role`, repeatable `--scope`, `--expires-in`. Printed once; only its hash is kept. |
| `tokens revoke <token-id>` | Immediate. |

## Setting up and looking around

| Command | What it does |
| --- | --- |
| `zoomies init` | Set this host up: how it runs, backend, listener, GitHub App and the first administrator. `--answers` takes a file and implies `--non-interactive`; `--print-answers` writes one out from an interactive run so the next host can be identical. |
| `zoomies update [--check]` | Short, operator-friendly alias for `zoomies upgrade`. It accepts the same flags and keeps the existing spelling compatible with scripts. |
| `zoomies upgrade [--check]` | Apply the installed binary and matching images to an existing native, Compose or Docker deployment. Keeps configuration and credentials; `--check` changes nothing. To download the binary too, use `install.sh --upgrade`. See [Upgrading](upgrading.md). |
| `zoomies logs` | Show the latest 100 controller log lines for the recorded Compose or Docker deployment. Alias for `zoomies deployment logs`. |
| `zoomies deployment <action>` | Operate the container deployment recorded by `zoomies init`: `status`, `logs`, `start`, `stop`, `restart`, `update`, or `down`. `update` pulls the controller image matching this binary plus cached runner images, recreates safely, and rolls back on failure. `down` keeps the database volume. |
| `zoomies uninstall` | Remove the service or container, the database, the encryption key and the configuration. |
| `zoomies backup [--dir path] [--keep N] [--include-key]` | Take a consistent copy of this host's database into a timestamped directory, with a manifest recording the build, the migration ledger, the encryption key's fingerprint, what that key is needed for, and the blanked configuration. Reads the database file directly, so it works when the controller will not start. See [Backup and restore](backup-and-restore.md). |
| `zoomies restore <backup-directory> [--replace]` | Put a backup's database back at `database.path`, after checking that the copy is sound, that this build can read its schema, and that this host's encryption key is the one that sealed it. Ends every session, removes unredeemed join tokens, and fences the fleet; `--revoke-api-tokens` and `--reset-agent-tokens` go further. `--replace` is required to overwrite an existing database, and moves it aside rather than deleting it. See [Backup and restore](backup-and-restore.md). |
| `zoomies config check [--config path]` | Validate a file without starting anything. Warnings print and exit 0; errors exit 1. |
| `zoomies config print [--config path]` | The effective configuration — file, environment and defaults combined — with secrets blanked. `--output` is `yaml` or `json` here, and defaults to `yaml`. |
| `zoomies healthcheck --url <url>` | Probe a controller's `/healthz`. Exit 0 when it answers. This is what the container image's `HEALTHCHECK` runs. |
| `zoomies version` | The version this binary was built from. `--short` or `--json`. |

`init` also accepts eight `--detected-*` flags. They are how `install.sh` passes
on what it already probed, and you will not normally type one.

The deployment commands read `deployment.json`, so they use the exact Compose
command, file, container name and image recorded during installation. Pass
`--config-dir` only for an installation outside the platform default. For a
custom controller image, `deployment update --image <ref>` names the intended
replacement explicitly.

`uninstall`'s `--deregister` and `--volumes` are three-state in practice: typed,
they mean what they say; untouched, they mean *ask*. In `--non-interactive` mode
name the ones you mean.

## Two asymmetries worth knowing

`users create --password` exists and `users passwd --password` deliberately does
not: creating an account is often scripted from a secret store, while changing
one is something a person does at a terminal, where the shell history is the
risk.

`healthcheck` declares its own connection flags rather than sharing the set
above, so its `--timeout` is `5s` rather than `30s` and it has no `--token`. It
is meant to run from a container's health check, where five seconds is already
generous and there is no token to hand.

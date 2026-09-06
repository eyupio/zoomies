---
description: >-
  Install Zoomies and run your first job on a self-hosted ephemeral runner in
  about five minutes -- one curl command, one GitHub App, no Kubernetes.
---

# Quick start

Five minutes, on a fresh Ubuntu, Debian, Fedora or Alpine host. macOS works too
for running a controller in development.

## 1. Install

```sh
curl -fsSL https://zoomies.sh/install.sh | sh
```

The script is POSIX `sh`, and it is written to be read before it is run — which
is the way we would rather you did it:

```sh
curl -fsSLO https://zoomies.sh/install.sh
less install.sh
sh install.sh
```

It works out your OS and architecture, your distribution and init system,
whether Docker or Podman is present and whether its socket is actually
reachable, whether that socket is rootless, which `compose` command you have,
whether ports 8080 and 443 are free — where `ss` or `netstat` exists to tell it,
and it says so when neither does — and whether Zoomies is already installed, in
which case it upgrades in place and says so.

The download must verify. It is checked against the release's `checksums.txt`,
and every way that can fail — a mismatch, no entry for this asset, no hashing
tool on the host, a checksums file that could not be fetched — refuses the
install rather than warning about it. A private mirror that publishes no
checksums is the one supported exception, with `--allow-unverified`.

Before it changes anything it prints what it is about to do — the version, where
the binary goes, whether it needs `sudo`, and what it leaves alone — and asks
once. `--yes` and `--non-interactive` skip the question.

Everything it discovered is handed to `zoomies init`, so the interactive setup
never asks a question the script already answered.

## 2. Choose how it runs

`zoomies init` offers only what your host can do:

=== "Native"

    The binary under systemd, with a hardened unit. Leanest, starts fastest,
    and needs no container runtime for the controller itself. **Setup finishes
    here**, including the GitHub App, your administrator and your first pool.

=== "Docker Compose"

    Writes a `docker-compose.yml` and a fully populated `.env`, then brings it
    up. Easiest to upgrade and to move to another host. It is the default
    whenever you have a `compose` command.

=== "Docker"

    A single container. Fewest files, but you manage the run command yourself.

!!! note "The containerised options move the last three steps to the browser"

    A container keeps its database in a volume the installer cannot open, so on
    the compose and docker paths the administrator, the GitHub App and the first
    pool are created in the browser afterwards rather than in the terminal. The
    closing summary prints all three with their exact addresses, and the
    Overview repeats them as a checklist that ticks itself off as you go.

Whichever you choose, the containerised deployments write a **fully populated
`.env`** -- no placeholders to go back and fill in. Every variable carries a
one-line comment saying what it is for, the file is `0600` because it holds your
encryption key, and it is written atomically so an interrupted install never
leaves a half-written file that compose would then read.

Re-running the installer over an existing deployment is an upgrade, not a
reinstall: it **keeps the existing encryption key** (minting a new one would
make every stored secret undecryptable) and backs the old file up beside it.

Then it walks the rest: a dedicated service user and directories, an encryption
key (which it will tell you to back up, and say exactly what is lost without),
the runner backend — preferring a rootless Docker or Podman socket, and
spelling out the consequence of each alternative — the bind address and TLS, and
your first administrator account.

Setup does not assume the service account can reach the container socket, it
checks. The account's access is worked out from the socket's own owner and mode
rather than from a group called `docker`, which is the wrong group on a Podman
socket or a distribution that names it something else; the account is added to
whichever group that is; and the check runs **again** afterwards, so an install
only reports success it has verified. Where joining a group cannot help — a
socket with no group permissions at all — it says so and names the two ways out,
instead of leaving you with a fleet that comes up unable to run anything.

## 3. Connect GitHub

Zoomies creates the GitHub App for you through the manifest flow. It opens your
browser at a pre-filled form — and always prints the URL as well, so a headless
host still works — with exactly the permissions it needs and no more:

| Permission | Why |
| --- | --- |
| `organization_self_hosted_runners: write` | register and remove runners (org targets) |
| `administration: write` | the same, for a repository target (a single repository, which is also how a personal account is used) |
| `actions: read` | read workflow runs and jobs for the fallback poller |
| `metadata: read` | required by GitHub for any App |
| `contents: write` | read and rewrite workflow files for the [migration wizard](migration.md) |
| `pull_requests: write` | open the migration wizard's pull request |
| `workflows: write` | GitHub requires it specifically to change files under `.github/workflows` |
| `workflow_job` event | the webhook that makes scaling instant |

The last three are the migration wizard's, and they are asked for now rather
than later because later is expensive: adding a permission to an App that
already exists is held by GitHub until the account's owner accepts it on the
installation, and until they do the wizard cannot read a workflow at all. If you
never migrate anything, remove them on the App's **Permissions & events** page —
nothing else in Zoomies writes to a repository.

Create the App, install it on your organisation -- or, for a repository target,
on your own account scoped to that repository, which is how a personal account
is used -- and the credentials come back to the installer automatically. The
private key is sealed with your instance encryption key before it touches the
database, and is never returned by the API.

## 4. Your first pool

A pool says what labels your runners answer to and how many may exist.
On a single-host install made by `zoomies init`, setup creates this one for you
once GitHub is connected -- it is derived from what the host actually is, so the
numbers below are what you get on a 4-CPU Linux box with Docker. The
repository's `docker-compose.yml` has no installer, so on that path you create
it yourself on the **Pools** page:

| | |
| --- | --- |
| **Name** | `zoomies-linux-x64` |
| **Labels** | `zoomies-linux-x64` — what your workflows put in `runs-on` — and `zoomies`, which every pool answers to |
| **Backend** | Docker (rootless if available) |
| **Min / max** | `0` / `4` — nothing idle when nothing is queued; the max is the host's capacity |
| **Idle timeout** | `5m` |
| **Ephemeral** | yes |
| **Docker in jobs** | none |

The wizard does not make you invent the first two rows. It opens with a name
already in the field -- the brand, a name from the kennel and the
infrastructure the runners will land on, so `zoomies-biscuit-docker-linux` --
and a label derived from that name, so the pool is reachable by a workflow
before you have typed anything. The dice beside the field roll another name;
type over it and the wizard leaves the name and the label alone from then on.
Every name it offers starts with `zoomies-`, which is what tells you a runner
in GitHub's own settings is one of yours.

Decline it, or set `pool.skip` in an answer file, and the Pools page starts
empty; nothing runs until a pool exists. Either way, always set a maximum. It
is your only backstop against a runaway workflow.

**Docker in jobs** stays `none` until a workflow needs a daemon — a `docker`
step, a `container:` or a `services:` block — and then `dind` is the one to
choose. That one setting is enough: the pool is switched to a runner image with
a Docker client as it is saved. [Jobs that build container
images](configuration.md#jobs-that-build-container-images) says what it costs.

## 5. Run something

```yaml
jobs:
  build:
    runs-on: zoomies-linux-x64
    steps:
      - uses: actions/checkout@v4
      - run: make test
```

One label is enough to reach a pool, and `runs-on: zoomies` works too, because
every pool answers to that as well. [The labels to give a
pool](configuration.md#the-labels-to-give-a-pool) says why they are branded, and
what to write before anyone has decided which pool a repository belongs in.

Push it. Zoomies sees the `workflow_job` webhook, starts a runner, and you watch
the whole thing happen on the Overview page without refreshing — including the
scheduler's reasoning, in its own words:

```text
scaled zoomies-linux-x64 0 -> 1: 1 job queued
```

The Jobs page keeps the record: how long each job waited, how long it ran,
which runner took it, and — for anything that failed — the step it failed at.

![The Jobs page: the fleet's queued, running and finished jobs with their labels, pool, runner, queue wait and duration](screenshots/jobs-dark.webp#only-dark){ .zoomies-shot }
![The Jobs page: the fleet's queued, running and finished jobs with their labels, pool, runner, queue wait and duration](screenshots/jobs-light.webp#only-light){ .zoomies-shot }

## Moving the rest of your repositories

Editing every workflow by hand is the part nobody does. **Migrate** in the
navigation reads the workflows in the repositories your App can see, rewrites
their `runs-on` lines, shows you the exact diff, and opens one pull request per
repository. See [Migrating repositories](migration.md).

## Adding another host

**Hosts → Add a host** does the whole thing on one page. It comes filled in --
the address your browser reached the controller on, the labels your pools
already select hosts by, capacity left for the agent to decide -- and hands you
one line to paste into a shell on the new machine:

```sh
curl -fsSL https://zoomies.sh/install.sh | sh -s -- \
  --mode agent \
  --controller https://zoomies.example.com \
  --join-token zoojoin_...
```

Leave the page open. It watches for the host and says the moment it has
joined: what the machine is, which backends it offers, and which pools can
place runners on it. If the binary is already on that machine, the same page
offers the shorter `zoomies agent join` form, and the CLI can mint a token too
with `zoomies hosts join-token create`.

Join tokens are single-use and short-lived. The agent connects outbound only, so
the new host needs no inbound firewall rule.

[Hosts and pools](hosts-and-pools.md) takes it from here: what a host brings with
it, cordoning one for maintenance, when a second pool is worth having, and the
rules that decide which host a runner lands on.

## Unattended installs

Every prompt has a flag, and there is an answer file for the rest:

```sh
sh install.sh --non-interactive --answers zoomies-answers.yaml
```

`zoomies init --print-answers` writes a commented template. In non-interactive
mode a missing required answer is an error naming the key and what it is for,
never a silent default.

## If something is wrong

Very little of it should be a mystery. The UI, `zoomies status` and
`GET /api/v1/problems` are three windows onto one list of problems, and on a
healthy fleet that list is a single quiet line.

[Troubleshooting](troubleshooting.md) is the page for the rest: the commands to
run first, the five things that go wrong on a first run, and what a job sitting
in the queue is telling you.

## Next

- [Hosts and pools](hosts-and-pools.md) — a second machine, a second pool, and how placement is decided
- [Configuration](configuration.md) — every setting, including running behind Cloudflare
- [Troubleshooting](troubleshooting.md) — when a first run does not work, and when a job sits in the queue
- [Security](security.md) — the threat model, and what each dangerous toggle costs
- [Upgrading](upgrading.md) — what an upgrade does to work in flight, and why there is no way back
- [Backup and restore](backup-and-restore.md) — the two files, and bringing a controller back elsewhere
- [Architecture](architecture.md) — how the pieces fit
- [API](api-surface.md) — the REST surface the UI and CLI both use
- [Command line](cli.md), [Problem codes](problem-codes.md), [Metrics](metrics.md) — the reference tables

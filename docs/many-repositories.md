---
layout: article
icon: material/source-repository-multiple
title: One self-hosted runner fleet for many repositories
description: >-
  Share self-hosted GitHub Actions runners across repositories: where GitHub
  allows registration, how one fleet serves an organisation, and keeping repos
  apart.
---

# One fleet for many repositories

A self-hosted runner serves whatever GitHub registered it to. Registered to an
organisation, it can take jobs from every repository there; registered to one
repository, it takes that repository's jobs and no others. So the question of
sharing runners across repositories is mostly a question of where they are
registered, and Zoomies registers them wherever the GitHub App you connect is
installed.

## Where GitHub lets a runner be registered

| Level | Who it serves | Zoomies |
| --- | --- | --- |
| A repository | That repository only | Yes: a repository target, written `owner/name` |
| An organisation | Any repository in the organisation that its runner group allows | Yes: an organisation target |
| An enterprise | Several organisations in a GitHub Enterprise Cloud enterprise | No |

[GitHub's documentation](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/add-runners)
covers all three. A personal account has no level of its own: its runners are
registered repository by repository, which Zoomies does for you under one
[personal account target](#a-personal-account).

## One organisation, every repository

Install the GitHub App on the organisation (the installer and the Connect
GitHub dialog both offer it) and one pool can serve every repository there. A
workflow reaches it by the pool's label:

```yaml
runs-on: zoomies-linux-x64
```

Give each pool one branded label and add pools as the work needs different
shapes (a bigger machine, a GPU host, arm64) rather than one pool per
repository. Every pool also answers to `zoomies`, so `runs-on: zoomies` means
"anywhere in this fleet", which suits a repository nobody has assigned a pool to
yet. [The labels to give a pool](configuration.md#the-labels-to-give-a-pool) has
the rest.

On an organisation, GitHub puts runners in a
[runner group](https://docs.github.com/en/actions/concepts/runners/runner-groups),
and a group decides which repositories may use them. When it connects, Zoomies
creates an organisation-wide `zoomies` group, enables it for public
repositories, and registers new pools' runners there; a pool can name another
group instead, and a group an administrator chose is never rewritten.
[A pool belongs to one installation](hosts-and-pools.md#a-pool-belongs-to-one-installation)
explains how that works, and what happens when a group cannot be found.

GitHub's own default keeps public repositories out of runner groups, and for a
good reason: a self-hosted runner on a public repository runs code from anyone
who can open a pull request. Enabling the group for them makes the runners
reachable, not safe. Read [Security](security.md) before a public repository
uses this fleet.

## A personal account

Choose **A personal account** when you connect GitHub and give your login. The
App is created on your own account and installed there, on every repository or
only the ones you tick, and one installation with one pool then serves all of
them.

GitHub offers no account-wide runners for a personal account, so there is no
single registration that covers its repositories. Zoomies makes one for each
job instead: when a job is queued, its runner is registered on that job's
repository, runs it, and is destroyed. That has three consequences worth knowing
before you size a pool:

* **No warm runners.** A warm runner waits for a job that has not arrived, and
  here there is no repository to register it on until one has. `min_runners` on
  such a pool has to be `0`, and every job pays the runner's start time.
* **Ephemeral pools only.** A runner kept between jobs would stay on one
  repository, out of reach of the others.
* **A runner belongs to its repository.** The Runners page shows which one. If
  the job is cancelled while its runner is starting, the runner is stopped
  instead of being offered to another repository's job, which it could not take.

The App needs `administration: write`, the permission that registers a runner on
a repository, in place of the organisation's runner permission, which a person
cannot grant. Creating the App from Zoomies asks for the right one.

An installation recorded before Zoomies made the distinction, with a personal
login typed as an organisation, used to answer `list runners on <login>: not
found`. It now corrects itself: the credential check asks GitHub what kind of
account it is on, within seconds of the controller starting or when you press
**Verify**, and from then on registers runners on the account's repositories. If
its pool has `min_runners` above `0` or is not ephemeral, change those two
settings.

A single repository on a personal account can still be its own target,
`owner/name`, with a pool of its own, which is the way to keep one repository's
runners and limits apart from the rest.

## Keeping repositories apart

Within an organisation, **GitHub decides which runner gets a queued job**: it
offers the job to any runner in scope whose labels match. A runner Zoomies
started for one repository's job may be handed another repository's job from
the same organisation instead, and nothing on Zoomies' side can prevent it.
Ephemeral runners make that harmless for most fleets (each job still gets a
fresh runner, destroyed afterwards) but it matters where repositories must not
share machines or caches.

| To keep apart | Use | How strong |
| --- | --- | --- |
| Which repositories may use a set of runners | A runner group limited to those repositories, named on the pool | Enforced by GitHub |
| One repository from all others | A repository-target installation with its own pool | Enforced by GitHub |
| Workloads by label | Separate pools with separate labels | A convention kept in workflow files |
| One busy repository from starving the rest | A pool's `repository_scale_up_limit` | A throttle on new runners, not isolation |

A pool's cache follows the same rule: a repository cache under an organisation
installation is only as private as the pool's labels. See
[The pool cache](configuration.md#the-pool-cache) before sharing one.

## Moving many repositories at once

The [migration wizard](migration.md) reads the workflows in every repository
the App can see, maps GitHub's hosted runner labels to your pools, and opens one
pull request per repository after showing you each diff, never more than
twenty-five at a time, and never onto a default branch directly. Jobs already
on self-hosted runners are left alone; for those,
[give a pool the label they already use](migration.md#coming-from-your-own-static-runners).

!!! note "Prefer a hosted controller?"

    [Zoomies Cloud](https://cloud.zoomies.sh/) hosts the controller and handles
    its updates and backups. Useful when your team wants a shared fleet without
    another controller to maintain. Your team still supplies and operates the
    runner machines; Cloud is an optional paid service.

## Where to go next

- [Quick start](quickstart.md): connect GitHub and create the first pool.
- [Hosts and pools](hosts-and-pools.md): how a pool decides where its runners go.
- [Self-hosted runners in Docker](docker.md): what a job can reach, depending on
  how it gets Docker.

---
icon: material/laptop
title: "Zoomies vs Codemany"
description: >-
  How Zoomies compares with Codemany: a paid desktop app that turns Macs and
  Linux machines into GitHub Actions runners, against free software that runs a
  fleet. Where each is the better choice, and how to use both.
---

# Zoomies and Codemany

[Codemany](https://codemany.com/) is a paid desktop app that turns a Mac or a
Linux machine you own into ephemeral GitHub Actions runners. You install it,
sign in with GitHub, pick repositories and change one line of `runs-on`. Its
strongest card is macOS: it runs jobs on the Apple silicon Macs you already have,
which Zoomies does not. Zoomies is free software with a controller, a live web
UI and a scheduler for a fleet of Linux hosts. The honest place to start is that
they overlap less than the shared goal suggests.

Written from Codemany's public documentation, pricing page and benchmark page,
reviewed in November 2026. It ships new releases several times a week, so check
[its site](https://codemany.com/) before relying on a detail. This is a
comparison of documentation, not a benchmark, and a feature its pages do not
mention is not a claim that it is missing.

## Side by side

| | Codemany | Zoomies |
| --- | --- | --- |
| Model and licence | A proprietary desktop app, licensed per machine by subscription | Self-hosted; [AGPL-3.0](https://github.com/eyupio/zoomies/blob/main/LICENSE) |
| What it costs | At review, $20 a month for 2 machines and $10 for each extra, for the GitHub account or organisation that owns the repositories; a 14-day trial with no card | No software fee. [Zoomies Cloud](https://cloud.zoomies.sh/pricing) is an optional paid controller; the runner machines are yours either way |
| Needs Kubernetes | No | No |
| Runs on | Apple silicon Macs on macOS 14 or later, and 64-bit Linux with systemd, on x86-64 and arm64 | Linux hosts on x86-64 and arm64, a Windows agent that is [not yet qualified](../index.md#what-is-qualified), and no macOS runners |
| Where a job runs by default | Directly on the machine, as your user, with a fresh runner folder each time | In a fresh container that is non-root, with capabilities dropped and no Docker socket |
| Stronger isolation | Opt-in: a KVM virtual machine or an LXD container per job on Linux; a separate user per runner or a macOS VM per job, both marked experimental | None beyond the container. [Proxmox VE](../proxmox.md) rents whole hosts, not one machine per job, and the [process backend](../hosts-and-pools.md#worked-shapes) Windows uses has no container isolation |
| Capacity | Each runner budgets 4 GB of memory, 2 performance cores and 15 GB of disk, from 1 to 32 runners a machine | [Pools](../hosts-and-pools.md) with size classes, and [elastic CPU](../elastic-cpu.md) and [memory](../elastic-memory.md) that lend what other runners are not using |
| Several machines | Machines on one network can pair and control each other; there is no leader, and GitHub hands each job to a free runner. Cloud machines on Google Cloud, Hetzner and DigitalOcean are managed from the Mac that created them | One controller places jobs across agents that dial out, so a host behind NAT needs no inbound rule |
| Seeing the fleet | The app, a menu bar or tray item, a dashboard on the local network that is read-only on other devices, and a hosted Console with each machine's newest 50 runs and its job logs | A live [web UI](../ui.md), [metrics](../metrics.md), an audit log, live job logs and role-based access |
| Caching | A shared tool cache on Mac and Linux, and on Linux Go and npm mirrors, a Docker Hub pull-through mirror and a git mirror for public repositories. `actions/cache` steps still go to GitHub | [A per-pool cache and a tool cache](../persistent-caches.md), and a host-local `actions/cache` service a pool can turn on. No package mirrors are shipped |
| What leaves the machine | Code and the GitHub token stay. Your GitHub id, login and email, a daily plan check, runner summaries and secret-masked job logs for the Console, and anonymous usage statistics go to Codemany; the last two can be turned off | Nothing, from a controller you run. Zoomies Cloud is the option where someone else hosts the controller |
| Moving workflows onto it | Change `runs-on` to `[self-hosted, codemany]` | A [migration wizard](../migration.md) that opens one pull request per repository |

## When Codemany is the better choice

- **You need macOS runners.** It runs Xcode, Homebrew and simulators you
  already have on Apple silicon Macs you already own. Zoomies has no macOS runner
  image.
- **You want an app, not a controller.** There is no server to host, no TLS to
  arrange and no database to back up, and it is running in about five minutes.
  With Zoomies you operate a controller, and give it an address GitHub can send
  webhooks to (polling is only the fallback).
- **You want a virtual machine or an LXD container for each job on Linux.** A
  job in its own kernel is further from your machine than a job in a container
  that shares one. Zoomies' unit of isolation is the container.
- **You want package mirrors without building them.** Codemany sets up Go and
  npm mirrors, a Docker Hub pull-through cache and a git mirror on a Linux
  machine. Zoomies lets you point a runner at a mirror of your own and ships none.

## When Zoomies is the better choice

- **You have more than a few machines.** Codemany's price grows by $10 a machine
  after the second. Zoomies charges no software fee, and a hosted controller is a
  flat fee per controller rather than per machine. [Costs](../costs.md) has the
  sums for the machines themselves.
- **You want the source and the control plane.** Zoomies is open source and its
  state is one SQLite file you hold. Codemany needs a plan for each licensed
  machine and checks it daily; a plan keeps working for seven days offline.
- **You operate a fleet, not a machine.** Roles, an audit log, an API, metrics,
  live job logs and a scheduler that says why it did what it did. Codemany's
  dashboard on other devices is read-only, and its changes are made in the app or
  through its Console.
- **You want the safer default.** Codemany's own security page says jobs run as
  your user and that a self-hosted runner is not a sandbox. Zoomies starts from
  an unprivileged container, which is still not a sandbox for hostile code; see
  [security](../security.md) for what it does and does not protect.
- **You run Linux across many distributions.** Runner images for seven
  distributions on both architectures, and pools placed only on a host that
  matches what they asked for.

## Reading Codemany's benchmark

Codemany publishes its benchmark in an open repository, with every timing linked
to the GitHub run that produced it, which is the right way to do it and worth
reading. What it compares is machines it owns, kept warm between jobs, against
GitHub's standard 2-core Linux runner and GitHub's Mac runner. The speed-up
therefore measures owning a fast machine as much as the software that schedules
on it. Any controller that keeps caches warm on hardware you own is likely to
show a gap of that kind against a shared 2-core virtual machine, Zoomies
included. This page has not measured that, and neither project has published a
like-for-like comparison with the other.

## Using both

The choice is per job, because every runner is selected by `runs-on`. Keep Mac
jobs on Codemany and Linux jobs on a Zoomies pool:

```yaml
jobs:
  ios:
    runs-on: [self-hosted, codemany]
  api:
    runs-on: zoomies-linux-x64 # Replace with your pool's label.
```

Always include a label that belongs to one tool. Codemany's documentation warns
that `self-hosted` on its own matches any self-hosted runner in the repository,
and that holds for a Zoomies runner too.

## Where to go next

- [Quick start](../quickstart.md)
- [Compared with runner services](../hosted-runner-services.md), for hosted macOS
- [All alternatives](index.md)

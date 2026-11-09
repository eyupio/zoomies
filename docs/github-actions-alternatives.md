---
icon: material/swap-horizontal
title: "GitHub Actions alternatives: change the runner"
description: >-
  Looking for a GitHub Actions alternative? If the problem is cost, queues or
  control, you may only need other runners. What changes, and when to leave.
---

# GitHub Actions alternatives: change the runner, keep the workflows

People who search for a GitHub Actions alternative are usually unhappy with one
of three things: the bill, the queue, or the lack of control over the machine a
job runs on. Those are problems with the **runner**, the machine that executes
a job. They are not problems with the workflow format, the marketplace of
actions or the pull request checks, and you can change the runner without
touching any of those.

This page helps you work out which problem you have. If it is the runner, the
fix is smaller than a migration. If it is the CI system itself, you will want a
different comparison: this one does not cover GitLab CI, Buildkite or Jenkins.

## What you get by leaving, and what it costs

Moving to another CI system means rewriting every workflow in that system's
format, replacing the actions you rely on with whatever it offers instead, and
moving secrets, branch protections and required checks. For a handful of simple
workflows that is an afternoon. For a few hundred repositories it is a project.

Changing the runner is different. A workflow chooses where it runs with one
line:

```yaml
jobs:
  build:
    runs-on: [self-hosted, linux]
```

Change that line and the same workflow runs on a different machine. Nothing else
in the file moves, and GitHub still shows the checks on the pull request.

## Which problem do you have?

| What you are unhappy with | Is it the runner? | What to look at |
| --- | --- | --- |
| The monthly bill for minutes | Yes | Your own machines cost nothing in GitHub minutes today. [What GitHub Actions runners cost](costs.md) has the sums |
| Jobs waiting in the queue | Often | A pool that starts a runner when a job is queued. See [a job stuck in the queue](queued-job.md) first, because the cause is not always capacity |
| Needing a particular machine, network or disk | Yes | A self-hosted runner on that machine. [Runners in your home lab](home-lab.md) and [private hosts](private-hosts.md) cover machines behind a firewall |
| Kubernetes being the only fleet tool on offer | Yes | [Runners without Kubernetes](runners-without-kubernetes.md) |
| The workflow syntax, or the lack of a feature in GitHub Actions itself | No | A different CI system. This page cannot help |
| Wanting everything on one platform outside GitHub | No | A different CI system |

## The ways to change the runner

There are three kinds of answer, and they differ in who looks after the
machines.

**A hosted runner service.** Blacksmith, WarpBuild and Depot run your jobs on
their machines and charge by the minute; RunsOn, WarpBuild's bring-your-own
cloud option and Depot's AWS option run them in your cloud account. You do not look after hosts. You do
pay per minute or per licence, and you depend on the service.
[Zoomies and the runner services](hosted-runner-services.md) puts them side by
side with their prices and says when each is the better choice.

**A runner controller on Kubernetes.** actions-runner-controller (ARC) is the
usual answer if you already operate a cluster, and a poor one if you do not.
[Zoomies and actions-runner-controller](actions-runner-controller.md) covers
both sides.

**A runner controller for machines you already have.** Zoomies is one of these,
as are GARM, runscaler, gharp and a few others. They turn a server, a home-lab
box or a rented VM into runner capacity without a cluster.
[Runner controllers compared](alternatives/index.md) lists them and says which
need Kubernetes.

## What Zoomies is, and what it is not

Zoomies is free software under the AGPL-3.0, one binary with SQLite for its
state and a web UI. It starts an ephemeral runner for each queued job and destroys it
when the job ends. It has no per-minute fee and no
licence, so you pay only for the machines.

It is the wrong choice in some cases, and it is better to know before you try:

- **You need macOS runners.** Zoomies has no macOS runner image.
- **You do not want machines to look after.** Someone has to patch hosts and keep
  disks from filling. A hosted service takes that on.
- **The repository is public.** Standard GitHub-hosted runners are free on public
  repositories and are the safer choice there, because a self-hosted runner runs
  code from anyone who can open a pull request. [Security](security.md) has the
  detail.
- **Your load is large and bursty.** A fleet of your own machines is only as big
  as what is in it.

Linux runners on x64 and arm64 are what Zoomies is built and tested for.

## Trying it without committing

Because the choice is per job, you can move one workflow and leave the rest. A
sensible first move is the slowest or most expensive job in your busiest
repository.

1. Follow the [quick start](quickstart.md) to take a fresh host to a running job
   in about five minutes.
2. Change `runs-on` on one workflow to the labels of your new pool.
3. Compare the run against the same job on GitHub's runners.

When you want to move more, the [migration wizard](migration.md) reads the labels of GitHub's
own runners and of vendors such as Blacksmith and WarpBuild, proposes a pool for each, shows you the diff and
opens one pull request per repository. Moving a workflow back is the same one-line
change.

## Where to go next

- [What GitHub Actions runners cost](costs.md): whether your own machines pay
  for themselves.
- [Runner controllers compared](alternatives/index.md): Zoomies against ARC,
  GARM, runscaler, gharp and the rest.
- [Should you self-host GitHub Actions runners?](should-i-self-host.md): what the
  job involves before you commit.
- [FAQ](faq.md): what Zoomies needs and what it will not protect you from.

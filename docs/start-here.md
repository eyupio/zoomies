---
layout: hub
icon: material/book-open-page-variant-outline
title: "Zoomies documentation: choose your starting point"
description: >-
  Learn the basics, run your first GitHub Actions job, operate a runner fleet
  and use Eli or MCP to diagnose builds. Find advanced configuration, CLI,
  API and monitoring references.
---

# Start here

Zoomies runs GitHub Actions jobs on machines you own or rent. It watches the
queue, starts runners when they are needed, and removes each ephemeral runner
after its job. You manage the fleet through a web UI, CLI or API.

**New to self-hosted runners?** Start with [the fundamentals](fundamentals.md),
then [run your first job](quickstart.md). **Already running Zoomies?** Use the
task table below or go straight to [advanced operations](advanced.md).

Prefer to skip controller setup? [Zoomies Cloud](https://cloud.zoomies.sh/)
hosts the controller and handles its updates and backups. You still supply and
look after the machines that run your jobs. It is an optional paid service;
self-hosting the open-source controller stays free.

## Choose a path

<div class="zoomies-paths" markdown>

<div markdown>
### Evaluate Zoomies

Decide whether running your own fleet suits your workloads and your team.

1. [Should you self-host?](should-i-self-host.md): responsibilities and trade-offs.
2. [Explore the live demo](https://demo.zoomies.sh/){ target=_blank rel="noopener" } without signing in, or [run the local demo](quickstart.md#just-looking) to explore a fixture fleet without GitHub credentials.
3. [Costs](costs.md) and [alternatives](alternatives/index.md): compare the whole cost of ownership.
</div>

<div markdown>
### Run your first job

Learn the few concepts needed to connect GitHub and run one trusted workflow.

1. [Fundamentals](fundamentals.md): controller, host, pool, runner and labels.
2. [Quick start](quickstart.md): install, connect GitHub and verify a job.
3. [The web UI](ui.md): find the job, its runner and the fleet's problems.
</div>

<div markdown>
### Grow and tune a fleet

Work from measured demand, add capacity and automate repeatable operations.

1. [Hosts and pools](hosts-and-pools.md): placement and capacity.
2. [Advanced operations](advanced.md): sizing, performance, recovery and automation.
3. [Configuration](configuration.md), [CLI](cli.md) and [API](api-surface.md): exact settings and interfaces.
</div>

</div>

## Get help with jobs and workflows

[Eli](eli.md) brings job context into a conversation from **Ask Eli** in the
web UI. [MCP](mcp.md) connects your own assistant to diagnostics, measured
runner sizing and, with separate consent, verified repository source. Use
either to investigate failed or slow builds as well as fleet problems.

Start with [assistant-led job troubleshooting](troubleshooting.md#investigate-jobs-with-eli-and-mcp).
For a failed check on an existing PR, [Eli repairs](eli.md#set-up-pr-repairs)
can publish a fix and watch CI after you configure and authorise repairs.

## Find a task

| I want to… | Start with | Check that it worked |
| --- | --- | --- |
| Install on one Linux machine | [Quick start](quickstart.md) | A manually triggered job completes and its runner is removed. |
| Deploy the controller with a file | [Docker Compose](compose.md) or [PaaS](paas.md) | Claim the instance, connect GitHub and create a pool. |
| Add a machine | [Hosts and pools](hosts-and-pools.md#adding-a-host) | The host checks in and an eligible pool can place a runner there. |
| Use a private home or office host | [Private hosts with Tailcat](private-hosts.md) | The agent joins through the private connection and reports capacity. |
| Move existing workflows | [Migration](migration.md) | Review a diff, run one migrated workflow, then expand. |
| Diagnose a queued job | [A job stuck in the queue](queued-job.md) | Follow labels, installation access, host capacity and registration in order. |
| Speed up repeated builds | [Persistent caches](persistent-caches.md) | Compare repeated runs on the same host; keep a cache-miss path working. |
| Allocate spare CPU or memory | [Elastic CPU](elastic-cpu.md) and [elastic memory](elastic-memory.md) | Observe first, then compare run time and host pressure under representative load. |
| Route different job sizes | [Size classes and automatic pools](auto-pools.md) | Read shadow-mode decisions before enabling routing. |
| Create VMs when capacity is needed | [Proxmox VE](proxmox.md) | Verify the template, join and workload before increasing limits. |
| Monitor the fleet | [Metrics](metrics.md) and [problem codes](problem-codes.md) | An authenticated scrape works and alerts reach the person who operates it. |
| Back up, restore or upgrade | [Backup and restore](backup-and-restore.md), then [upgrading](upgrading.md) | Verify the backup and rehearse recovery before relying on it. |
| Script fleet operations | [CLI](cli.md), [API](api-surface.md) and [automation contract](automation.md) | Use a scoped token and inspect errors; the automation contract is still proposed. |
| Diagnose a failed or slow build | [Eli and MCP troubleshooting](troubleshooting.md#investigate-jobs-with-eli-and-mcp) | Read the evidence, then compare a fix or optimisation against a baseline. |
| Repair a failed PR check | [Eli PR repairs](eli.md#set-up-pr-repairs) | Review readiness, the published diff and GitHub's resulting checks. |
| Connect my own assistant | [MCP](mcp.md), [Connect Claude](connect-claude.md) or [AI Context](ai-context.md) | Choose fleet access and repository source access deliberately. |

## How to use these docs

The **Quick start** is a tutorial: follow it in order to reach a first successful
job. **Run your fleet** contains task guides, usually starting in the UI and
linking to CLI and API equivalents. **Scale and tune** covers optional features
to adopt after the basic fleet works. **Reference** contains exact contracts,
settings and architecture details to look up while working.

Use search for a setting such as `scheduler.scale_up_delay`, a problem code or
a command name. The long reference pages also have a table of contents for
jumping to one setting or route.

## Before you commit workloads

Start with workflows you trust and a finite pool maximum. Container runners
share a host kernel; a fresh runner is not a separate VM. The process backend
runs directly on the host. Read [security](security.md) for the boundaries and
[platform qualification](index.md#what-is-qualified) for the evidence behind
support claims.

You own the host capacity, patching and recovery plan. [Advanced
operations](advanced.md) gives you an order in which to put those in place.

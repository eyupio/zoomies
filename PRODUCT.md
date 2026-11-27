# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Solo self-hosters and home-lab operators who run CI on hardware they already own, typically one to three VMs or a small set of machines. They install Zoomies once, connect a GitHub App, and then keep the fleet open in a browser during the working day. Small platform teams use the same product, but the primary operator is the person who owns both the machines and the CI, and watches the fleet themselves.

## Product Purpose

Zoomies watches for queued GitHub Actions jobs, starts one fresh runner container per job, and destroys it when the job finishes. It gives a self-hosted runner fleet a live web UI that is the primary way to see and operate it, without Kubernetes, a database server, or pasted tokens. Success is an operator who can see what is wrong with the fleet and exactly what to change, without leaving the UI and without pressing refresh.

## Positioning

The deployment shape is the claim: one Go binary, SQLite, and GitHub App authentication, so a real fleet fits on a VM or three where ARC needs a cluster. Ephemeral runners are the default rather than an option, agents connect outbound only, and every capability is reachable from the web UI. A hosted runner service cannot truthfully copy that, because Zoomies runs inside the operator's own network on capacity they own.

## Operating Context

An operator installs with `install.sh`, `zoomies init`, Docker Compose, a PaaS, or a provider marketplace, then connects a GitHub App and creates the first pool. Workflows opt in through a pool label in `runs-on`; the Migrate wizard rewrites those lines across repositories and shows the exact diff first. Day to day the operator works from the UI's pages: Overview, Command centre, Pools, Runners, Queue, Workflows, Usage, Hosts, Providers, Installations, Eli repairs, Migrate, Kennel Club, AI Context, Audit and Settings. The UI stays open on a second monitor, often on a wall. The CLI, the REST API, MCP and Compose are other ways into the same fleet, and the docs site at zoomies.sh is the reference. A live demo at demo.zoomies.sh runs a controller with a fleet already in it.

## Capabilities and Constraints

Confirmed functionality: ephemeral runners, multi-host and multi-OS placement, elastic CPU and memory, webhook-driven scheduling with polling as a fallback, live log streaming, job history, RBAC and OIDC, audit rows for mutating actions, scheduled and off-host backups, host health and consented tuning, the Kennel Club repository checks, AI Context, migration pull requests, MCP tools, and the optional update helper and rollout.

Constraints: the controller and agents run on Linux, with a Windows agent that runs jobs as processes and is not yet qualified; the shipped binary is self-contained and static, with the Svelte UI embedded; there is no database server, no Kubernetes and no message queue; GitHub App authentication is required; Zoomies is self-hosted and bring-your-own-capacity. GitHub Enterprise Server is validated in configuration but has not been tested. The glossary fixes the operator's vocabulary: host, machine, agent, pool, drain, pause, remove, problem and finding each mean one thing.

## Brand Commitments

The product is Zoomies, described as Self-hosted Git runners, developed by EyUp.io. The identity is a running black-and-white cocker spaniel inside a circular swish, kept monochrome; Runner Blue is the interactive accent and Fast Cyan belongs to a runner executing a job and nothing else. Prose uses British spelling and no em dashes. The kennel naming is binding: the `zoomies-` prefix is never dropped from runner, pool and label names. The dog-park vocabulary is optional and always has a plain equivalent. Branding Guide v3.0 is the authority.

## Evidence on Hand

The repository README, the docs site at zoomies.sh, the live demo at demo.zoomies.sh, screenshots taken from the real binary by `make screenshots`, and the support matrix that separates tested configurations from ones that merely build. There are no customer testimonials, case studies, benchmarks, pricing claims or press quotes, and future work must not invent them. The software is AGPL-3.0-only, free, and has no paid tier.

## Product Principles

1. Every problem says what is true, why it matters, and exactly what to change.
2. The fleet must be legible all day: calm when nothing is happening, unmistakable when something is wrong, and never dependent on a refresh.
3. Fit the operator's own machines, not a cluster: one binary, SQLite, and agents that dial out.
4. Safe by default, with every weaker choice named where the operator meets it.
5. The brand lives in precise details, not decoration: the dog names and mark stay, and the fleet's data leads.

## Accessibility & Inclusion

The UI meets WCAG AA contrast for normal and secondary text, accent text and filled-button text, and 3:1 for meaningful control edges, across both visual styles and both colour modes. It is keyboard-driven, including the command palette, respects reduced motion, keeps a fixed status-colour mapping, and is checked by Playwright accessibility and mobile passes.

# 0020: macOS runners are actions/runner processes on an Apple silicon Mac

**Status**: accepted, on 17 November 2026, on the instruction of the person
running the working session, who chose the shape and then asked for it to be
put in [ROADMAP.md](../../ROADMAP.md), where it is decision 33 and the work is
ZF-247. It was not the recommendation, which was to leave macOS out for now.
The owner has not separately confirmed the shape. Nothing in the docs may
claim macOS runners until a job has run on a real Mac.

**Date**: 2026-11-17, in a working session with the person running it, from
the review recorded in
[the Codemany follow-up plan](../plans/2026-11-17-codemany-follow-ups.md).

## Context

Zoomies has no macOS runner. The support matrix says "Nothing planned" for it,
and eight docs pages say there is no macOS runner image. The review of
[Codemany](../../docs/alternatives/codemany.md) found that this is the one gap
that decides the choice for a reader who needs Xcode, simulators or Apple
silicon builds. The three options put to the person running the session were to
leave it, to spike it on a Mac, or to build a backend, and they chose to build
one.

Two shapes were weighed, as they were for Windows in
[decision 0003](0003-windows-runners-are-processes-on-a-host.md). A **process
on a Mac** runs actions/runner's `osx` release directly on the machine through
the `process` backend that already exists, and cannot give a job a container or
a virtual machine. A **virtual machine per job** keeps the ephemeral guarantee
whole, and needs a macOS image pipeline and a second backend. Apple limits a
Mac to two concurrent macOS virtual machines (the
[runscaler page](../../docs/alternatives/runscaler.md) cites it, and Codemany's
documentation says the same), so that shape caps a host at two jobs however
large the Mac is; a process has no such limit.

What the code already does, read on 17 November 2026 and not run:

* `runnerAsset` in `internal/backend/process.go` names the `osx` release for
  `darwin` on `amd64` and `arm64`, and the backend's preflight accepts it.
* `process_unix.go` builds for everything that is not Windows, which includes
  macOS, and `checkICU` returns early off Linux.
* The agent reports its operating system, and a pool can select on it
  (`--host-selector os=...`), as the docs already say of "the macOS box
  somebody runs a controller on".
* The installer renders a launchd unit for a controller. Whether it does for an
  agent is not known.
* `Stats` samples memory on Linux only and returns zero elsewhere, and the
  `process` backend applies no CPU or memory limit on any platform, so elastic
  CPU and memory do not apply to it.

What is not known is whether an agent on an Apple silicon Mac joins, starts a
runner, runs a job and cleans up. Nothing has run on one.

Codemany's default on a Mac is also to run a job directly on the machine as the
user, and it marks its macOS virtual machine mode experimental, so a process is
comparable to its default and not to its isolated mode.

## Decision

Run macOS jobs as `process`-backend runners on an Apple silicon Mac: the
agent's own binary on the host, actions/runner's `osx-arm64` release
downloaded and verified by the backend, one process group per runner, no
container and no virtual machine. Flag it as a beta-testing item. Say, on every
page that claims the ephemeral guarantee, that a macOS pool has a fresh work
directory and a single-use registration on a machine that keeps its state, in
the words the `process` backend already carries on Linux and Windows. Do not
start a macOS image catalogue or a virtual-machine backend. A virtual machine
per job is a new record that supersedes this one, and only when a user asks for
the isolation.

## Consequences

* A macOS pool has no image and no `docker_mode`. It is selected by the host's
  `os` and pins its runner with `runner_version`, as a Windows pool does.
* The support matrix gets a macOS agent row that says shape and runtime
  separately, and the "macOS controller" row stops saying "Nothing planned".
  Until a job has run on a Mac the row says built and not run, and the eight
  pages that say there is no macOS runner image keep saying it.
* Moving the row is a beta-testing item for which the owner provides an Apple
  silicon Mac, exactly as 0003 did for Windows.
* The first slice is small and mostly proof: tests that pin what the backend
  does on `darwin`, a launchd unit for an agent if the installer has none, and
  one real join. Nothing in `internal/scheduler` or `internal/store` changes,
  because the host's `os` and a pool's selector already exist.
* Codemany stays better for a team that wants a macOS virtual machine per job,
  and [the comparison page](../../docs/alternatives/codemany.md) keeps saying
  so. What this changes is that a Mac becomes a host in a Zoomies fleet, with the
  scheduler, the web UI and the audit log, alongside the Linux hosts.
* Jobs on a Mac run as the agent's user and can read that user's files. The
  security page must say so for a macOS pool in the same sentence that 0003
  requires for Windows, and a pool on a Mac should not serve a public
  repository.

# 0003: Windows runners are actions/runner processes on a Windows host

**Status**: accepted. Taken on 12 September 2026 on the owner's instruction to
implement Windows and arm runners and flag them as needing testing in the
beta; the shape is the one [ROADMAP.md](../../ROADMAP.md) decision 26
recommended, and the owner has not separately confirmed the shape. If the
containers path is wanted after all, a new record supersedes this one.

**Date**: 2026-09-12, by the implementing session at the owner's instruction.

## Context

Decision 26 named two shapes for ZF-206 and said they differ by about five
times the work and by the product's central promise. *Process on a Windows
host* runs `actions-runner-win-x64` directly on the machine through the
backend that already exists, and cannot give a job a container. *Windows
containers* keeps the ephemeral guarantee whole and needs a second runner image
catalogue on Windows base images, a second build matrix, Docker Engine over a
named pipe, and images measured in gigabytes. The roadmap recommended process
first, and containers only if a user asks for the isolation.

Section 10 sequenced all of ZF-206 after Gate F, because the support matrix
only moves a row right when a test runs on the thing, and a platform added
while the project is proving the one it has widens the gate. The owner asked
for it anyway, with the platform flagged as a beta-testing item rather than a
qualified one, which is what the support matrix now records.

## Decision

Run Windows jobs as `process`-backend runners: the agent's own binary on the
host, actions/runner's Windows release downloaded and verified by the backend,
one process tree per runner in a job object, no container. Say, on every page
that claims the ephemeral guarantee, that a Windows pool has a fresh work
directory and a single-use registration on a machine that keeps its state,
in the same words the `process` backend already carries on Linux. Do not
start a Windows image catalogue.

## Consequences

* A Windows pool has no image and no `docker_mode`; it is selected by
  `os=windows` and pins its runner with `runner_version`.
* Draining a Windows runner is a kill of its process tree, because a service
  has no console to raise an interrupt on. The registration is single-use
  either way, and the docs say so.
* The support matrix carries a Windows row that says shape and runtime
  separately: built, vetted and unit-tested on a hosted Windows runner;
  never yet joined or run a job on a Windows host. Moving it is a
  beta-testing item the owner provides the host for.
* Windows containers, if ever wanted, are a different package with a second
  catalogue and a second build matrix; nothing here is reused by it except
  the host's platform reporting.
* Windows on arm64 has digests in the table and no binary in the release;
  adding `windows/arm64` to the build matrix is one row, once amd64 has run.

# End-to-end test

This is the test that proves the whole thing works: it starts a real
controller, points it at a real GitHub App, creates a pool, triggers a real
workflow in a real repository, and asserts that an ephemeral runner appeared,
ran the job, and was destroyed afterwards.

It creates things on somebody's real GitHub organisation, so most of what
follows is about making sure it never leaves them there.

## Running it

You need a GitHub App installed on an organisation (or a repository) you are
willing to run workflows in, a Docker daemon, an authenticated `gh` CLI, and a
repository containing the workflow in `testdata/e2e-workflow.yml`.

```sh
export ZOOMIES_E2E=1
export ZOOMIES_E2E_APP_ID=123456
export ZOOMIES_E2E_INSTALLATION_ID=987654
export ZOOMIES_E2E_PRIVATE_KEY_FILE=/path/to/app.private-key.pem
export ZOOMIES_E2E_TARGET=my-org                # or my-org/my-repo
export ZOOMIES_E2E_REPO=my-org/zoomies-e2e      # where the workflow lives
export ZOOMIES_E2E_TARGET_TYPE=org              # or repo

make build          # the test runs the built binary, it does not build one
make test-e2e
```

It runs in polling mode, because a test host is not reachable from GitHub for
webhook delivery. That means it exercises the fallback path rather than the
webhook path; the webhook path is covered by the integration tests against the
fake GitHub in `internal/github` and `internal/controller`.

## Skipped, blocked, or run

`make test-e2e` skips when a prerequisite is missing, which is what you want on
a laptop. A gate wants the opposite:

```sh
make test-e2e-required
```

In required mode a missing prerequisite is a **failure**, not a skip. This
matters more than it sounds: a skip is exit code zero, so before this a harness
that had never once talked to GitHub reported the same green as one that had
run the whole scenario.

Every run writes one JSON result per scenario, to `roadmap/validation/e2e/` or
to `ZOOMIES_E2E_RESULTS_DIR`. Its `category` is the only vocabulary a run
reports in:

| Category | Meaning |
| --- | --- |
| `passed` | The scenario ran and every assertion held. This is the only pass. |
| `failed` | The scenario ran and something did not hold. |
| `blocked` | It was asked for and could not start: a prerequisite was missing. |
| `not_run` | Nobody asked for it. |

The result also carries the commit, the run id, the dispatch marker, a link to
the workflow run, anything the run created and could not remove
(`residual_cleanup` — empty is the only good value), and anything the start-up
sweep cleared up after an earlier run.

## What it asserts

1. The controller starts and reports healthy.
2. The installation verifies: credentials good, permissions sufficient.
3. A pool can be created through the API, and the audit log records it.
4. Triggering the workflow causes a runner to be created within the timeout.
5. The runner registers with GitHub and reaches `idle`, then `busy`.
6. The job completes successfully, and it is *this run's* job.
7. The runner is destroyed afterwards, and then, asking GitHub and the Docker
   daemon rather than Zoomies:
   * **no registration is left behind on GitHub** — the failure this catches is
     the one that quietly fills an organisation's runner list with dead
     entries; and
   * **no container is left behind on the host.**

   Both are asked of something other than Zoomies on purpose. Zoomies' own
   `/runners` endpoint says "removed" exactly when Zoomies believes it removed
   something, and that belief is the thing under test.

## One run cannot collide with another

Each run mints a run id and creates a pool whose label carries it
(`zoomies-e2e-<run-id>`), and dispatches the workflow asking for that label. Two
runs against one organisation therefore cannot take each other's jobs, adopt
each other's runners, or clean up each other's leavings.

## Cleaning up

Before anything is created, the run writes a **ledger** to
`~/.zoomies/e2e-ledgers` (or `ZOOMIES_E2E_LEDGER_DIR`) naming what it is about
to make. Every resource is appended as it is created, and the ledger is deleted
only once they have all been removed.

It lives outside the test's temp directory deliberately: `t.TempDir` is removed
when the test ends however it ends, which is exactly the moment a record of
what was *not* cleaned up becomes useful.

Cleanup runs however the test ends — pass, fail, or fatal — and in the order
that makes each step possible: force-delete the pool, wait for its runners to
go, delete the installation, then take off GitHub directly anything Zoomies
could not be made to remove. What it still cannot remove is written to the
result's `residual_cleanup`.

A run that is killed outright (`SIGKILL`, a timeout, a lost machine) cannot run
its cleanup. That is what the ledger is for: the **next** run sweeps the open
ledgers for the same target, deletes the runner registrations they left on
GitHub, and reports what it found in `swept_from_earlier_runs`. A sweep that
keeps finding things is a harness that keeps crashing, which is why it reports
rather than tidying quietly.

To check by hand:

```sh
ls ~/.zoomies/e2e-ledgers                       # open ledgers = unfinished runs
gh api /orgs/<org>/actions/runners              # registrations still on GitHub
docker ps -a --filter label=io.zoomies.managed=true
```

## Timeouts

The scenario's waits are constants in `budget.go`, and their sum must fit
inside the `-timeout` the Makefile passes. `budget_test.go` checks that, and it
carries no build tag so it runs in ordinary CI: the harness previously asked for
twenty-seven minutes of waiting behind a twenty-minute timeout, so it could
never have reached its own last assertion, and a run that was going to fail on
a leftover registration would have been reported as a timeout instead.

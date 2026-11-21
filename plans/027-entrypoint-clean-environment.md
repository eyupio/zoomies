# Plan 027: Take the runner credentials out of PID 1's environment

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- deploy/runner-entrypoint.sh internal/backend/entrypoint_readiness_test.go internal/docs/entrypoint_test.go test/runner-image/check.sh
```

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MEDIUM (every runner container starts through this script)
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

The backend passes the runner's credentials as container environment. The
entrypoint copies them into shell variables and `unset`s them so that job
steps do not inherit them; its own comment names the threat (a job reading
the organisation's registration token, good for an hour and enough to
register a runner of the reader's own). But `unset` only changes the
shell's own table. The kernel still serves the script's original
environment at `/proc/1/environ`, and the script is PID 1 running as the
same user as the job. A step that reads that file gets the token.
Reproduced by the audit. It matters on pools that use a registration
token; a one-job JIT configuration is already spent by the time a step
runs.

## Current state

- `deploy/runner-entrypoint.sh` starts with `#!/usr/bin/env bash`.
- Lines 212-228:

```sh
# A shell variable that was never exported is not in a child's environment, so
# copying them across and unsetting is the whole of it. config.sh and the
# cleanup trap below read the copies.
jitconfig="${ZOOMIES_JITCONFIG:-}"
runner_token="${ZOOMIES_RUNNER_TOKEN:-}"
runner_url="${ZOOMIES_RUNNER_URL:-}"
unset ZOOMIES_JITCONFIG ACTIONS_RUNNER_INPUT_JITCONFIG ZOOMIES_RUNNER_TOKEN ZOOMIES_RUNNER_URL
```

- The copies are used at lines 266-298 (`start_runner --jitconfig`,
  `config.sh --url ... --token ...`, and `config.sh remove --token` in the
  cleanup).
- Tests that read or run the script:
  `internal/backend/entrypoint_readiness_test.go`,
  `internal/backend/runner_apt_test.go`, `internal/backend/extra_ca_test.go`,
  `internal/docs/entrypoint_test.go`, and `test/runner-image/check.sh`.
- The backend sets the variables in `internal/backend/docker.go` near 876
  and 880. That contract does not change.

## The design to implement

On its first entry the script captures the three values, then re-executes
itself with those variables removed from the environment, handing the
values over on an inherited file descriptor that is read once and closed.
The re-executed process is still PID 1, and its `/proc/1/environ` is the
clean environment it was started with. Sketch:

```sh
if [ -z "${ZOOMIES_ENTRYPOINT_CLEAN:-}" ]; then
  exec 3<<EOF
${ZOOMIES_JITCONFIG:-}
${ZOOMIES_RUNNER_TOKEN:-}
${ZOOMIES_RUNNER_URL:-}
EOF
  exec env -u ZOOMIES_JITCONFIG -u ACTIONS_RUNNER_INPUT_JITCONFIG \
    -u ZOOMIES_RUNNER_TOKEN -u ZOOMIES_RUNNER_URL \
    ZOOMIES_ENTRYPOINT_CLEAN=1 "$0" "$@"
fi
{ IFS= read -r jitconfig; IFS= read -r runner_token; IFS= read -r runner_url; } <&3
exec 3<&-
unset ZOOMIES_ENTRYPOINT_CLEAN
```

A here-document in bash is an unlinked temporary file or a pipe, so
nothing is left on disk and nothing appears in any process's arguments.
The three values are single-line (a base64 JIT configuration, a token, a
URL); Step 1 confirms that.

## Scope

**In scope**: `deploy/runner-entrypoint.sh`, one test (see Step 2).

**Out of scope**: `internal/backend/docker.go`, the Dockerfiles, the
runner build scripts, the Windows entrypoint if there is one.

## Steps

### Step 1: Confirm the assumptions

- Read the script from the top to line 230 and note everything that runs
  before the capture; the re-exec must come before any of it that would
  be wrong to run twice, or the guard must make the first entry do
  nothing else.
- Confirm the three values cannot contain a newline (how the backend
  builds them, and what GitHub's JIT configuration is).
- Read the five test files above to learn how the script is run under
  test and what they assert about its text.

### Step 2: Write the failing test

In `internal/backend`, beside `entrypoint_readiness_test.go` and using its
way of running the script with stubbed `run.sh` and `config.sh`, add
`TestTheEntrypointsCredentialsAreNotInItsOwnEnvironment`: start the script
with the three variables set and a stub `run.sh` that writes
`/proc/$PPID/environ` (or walks up to the entrypoint's pid) and its own
`env` to a file. Assert the token appears in neither, and that the stub
`config.sh` or `start_runner` stub still received it. Skip the test where
`/proc` is not available (`runtime.GOOS != "linux"`).

**Verify**: it fails before the fix, finding the token in `/proc`.

### Step 3: Re-execute with a clean environment

Replace lines 225-228 with the sketch above, adapted to what Step 1
found, and rewrite the comment block above it truthfully: `unset` alone
is not enough because the kernel keeps serving the environment the
process was started with, so the script starts itself again without them.
Keep the first three paragraphs of the existing comment, which explain
the threat.

**Verify**: the new test passes, and

```
go test -race -count=1 ./internal/backend/... ./internal/docs/...
```

passes. Run `shellcheck deploy/runner-entrypoint.sh` if it is installed
and `bash -n deploy/runner-entrypoint.sh` either way.

### Step 4: Run the image check if you can

`test/runner-image/check.sh` runs against a built runner image. If Docker
is available, build the image and run it; if not, say so in the report.

## Done criteria

- [ ] The new test passes on Linux and failed before Step 3.
- [ ] `go test -race -count=1 ./internal/backend/... ./internal/docs/...` passes.
- [ ] `bash -n deploy/runner-entrypoint.sh` exits 0.
- [ ] `make test` exits 0.
- [ ] Commit message: `Take the runner credentials out of PID 1's environment`.

## STOP conditions

- A base image the script ships in has no `env -u` (BusyBox `env` lacks
  it in some versions): check `internal/naming/images.go` for the bases
  and stop if any is not GNU coreutils.
- The script must not re-execute for a reason stated in its comments (a
  signal handler or trap installed earlier that the re-exec would drop).
- A value can contain a newline.

## Maintenance notes

- The stronger fix is for the backend to deliver credentials as a file on
  a tmpfs the job cannot read, which removes them from the container's
  configuration too (`docker inspect` on the host still shows them
  today). That changes the backend contract and is the owner's call.

# Native macOS process trial

The process backend can run an ephemeral GitHub Actions runner on Darwin ARM64
from an Aqua LaunchAgent. Native Apple build and UI jobs need the operator's
login session and an explicit environment for that account.

`agent.process_admission_file` adds host-local slot admission before runner
registration. The backend locks the slot directory, reserves a slot with atomic
mkdir, binds the reservation to the runner PID, and releases it after cleanup.
An occupied host returns backend-busy before GitHub can assign a job.

Example policy, written atomically and refreshed before `expires_at`:

```json
{
  "directory": "/var/lib/zoomies/native-slots",
  "capacity": 2,
  "expires_at": 1794746400,
  "env": {
    "HOME": "/path/to/runner-home",
    "USER": "runner",
    "LOGNAME": "runner",
    "BUILD_SLOT": "native-{slot}",
    "TMPDIR": "{job}/tmp"
  }
}
```

Replace the account and directory examples with the host's real values.
`CI_SLOT` is always the reserved number. The environment map is an explicit
operator allowlist. It expands `{slot}` and `{job}` in its values.

A missing, expired or invalid policy prevents admission. Capacity zero pauses
new runners while current work finishes. Cooperating local schedulers must take
an exclusive `flock` on `<slot-directory>/.admission.lock` around counting,
reserving, checking ownership, and removing slots. This enforces a reduced total while higher-numbered slots drain.
The admission-file option requires a Unix host.

Set controller host `auto_adjust` to `off` and configure its capacity explicitly.
The operator manages the policy writer. Disk admission is a separate setting.

Darwin shutdown sends TERM to the runner group and waits for its workers.
It sends KILL only if the group survives the graceful timeout. A failed startup
uses the same sequence. Cleanup retains a directory and reservation when a live
process cannot be stopped or its identity cannot be verified.

Removal can recover a reservation after a partial directory deletion removes
its local lease file. It uses the matching record in the slot directory and
retains it while the owner or process group survives. Unknown reservations need
operator inspection after a crash.

Tests cover concurrent admission, lowered capacity, foreign reservations,
expired policies, the job environment, worker shutdown, failed-start cleanup,
and recovery after a partial deletion:

```sh
go test -race ./internal/backend -run 'TestNative|TestConcurrentNative|TestMacDefault|TestLowerNative' -count=1
go test ./internal/config
go vet ./internal/backend ./internal/config
```

This is an experimental API. Fleet testing continues before review readiness.
Process descendants that leave the listener's group remain a follow-up.

If the listener was already gone before shutdown began, its saved identity
cannot be verified. Cleanup retains that reservation while its group survives.
An operator must inspect and end that orphan group.

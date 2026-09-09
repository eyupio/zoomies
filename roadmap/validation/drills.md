# Drill record

Each row is one run of one drill against the built binary, with a fake GitHub
and a real workload on the machine that ran it. Appended rather than replaced:
a drill that has recovered cleanly forty times and then did not is a finding
that only exists if the forty are there to compare against.

**Human action needed** is the column to read first. A drill that recovers on
its own and one that leaves something for a person to delete are different
findings, and only the row says which. **Run** is where the row came from, so
a row that reads oddly a month later can be taken back to the logs that
produced it; a row written on a developer's machine says so instead.

The rows below are appended by the drill job on every push to the default
branch. A pull request's rows stay in that run's job summary: they describe a
commit that may never exist, and the comparison this file is for is the
default branch against itself.

| When | Commit | Run | Drill | Outcome | What it proves | Observed | Recovery | Human action needed |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-09-08T19:04:45Z | b97a851 | unrecorded | lifecycle | passed | a queued job became a real process on this host and everything was cleaned up | runner created @1.6s: zoomies-drill-cocoa-34vqfiw5; workload on host @1.6s: zoomies-drill-cocoa-34vqfiw5; runner terminal state @30s: removed; host workloads after cleanup @30s: 0; github registrations after cleanup @59.6s: 0 | n/a | none |
| 2026-09-08T19:04:47Z | b97a851 | unrecorded | removal | passed | removing a runner deleted its GitHub registration without waiting for the reaper | registered with github @1.6s: zoomies-drillrm-ziggy-dwwew76e; removal requested @1.6s: zoomies-drillrm-ziggy-dwwew76e; github registration after removal @1.6s: gone; host workload after removal @1.8s: gone | n/a | none |
| 2026-09-08T19:04:48Z | b97a851 | unrecorded | restore | passed | a backup taken from a running fleet restores into an empty state directory, comes back fenced, and serves again once the fence is lifted | created pool @0s: pool_w5y5lcw7zjypg; backed up @0s: zoomies-20260908-190447; stopped the controller @0s: kill; carried the encryption key across @0s: encryption.key; restored @0s: into a clean state directory; came back fenced @200ms: restored from /tmp/TestABackupCanBeRestoredIntoACleanDirectoryAndComesBackFenced1005597945/003/zoomies-20260908-190447, a backup taken 2026-09-08T19:04:47Z; lifted the fence @200ms: the fleet is ready again | n/a | none |
| 2026-09-09T08:16:34Z | 3e5667b | https://github.com/eyupio/zoomies/actions/runs/34328070058 | lifecycle | passed | a queued job became a real process on this host and everything was cleaned up | runner created @1.6s: zoomies-drill-rocket-gfambli3; workload on host @1.6s: zoomies-drill-rocket-gfambli3; runner terminal state @30s: removed; host workloads after cleanup @30s: 0; github registrations after cleanup @59.7s: 0 | n/a | none |
| 2026-09-09T08:16:36Z | 3e5667b | https://github.com/eyupio/zoomies/actions/runs/34328070058 | removal | passed | removing a runner deleted its GitHub registration without waiting for the reaper | registered with github @1.8s: zoomies-drillrm-digby-el4myyxh; removal requested @1.8s: zoomies-drillrm-digby-el4myyxh; github registration after removal @1.8s: gone; host workload after removal @1.8s: gone | n/a | none |
| 2026-09-09T08:16:37Z | 3e5667b | https://github.com/eyupio/zoomies/actions/runs/34328070058 | restore | passed | a backup taken from a running fleet restores into an empty state directory, comes back fenced, and serves again once the fence is lifted | created pool @0s: pool_3pdaxdewxrfdy; backed up @0s: zoomies-20260909-081637; stopped the controller @0s: kill; carried the encryption key across @0s: encryption.key; restored @0s: into a clean state directory; came back fenced @200ms: restored from /tmp/TestABackupCanBeRestoredIntoACleanDirectoryAndComesBackFenced488256601/003/zoomies-20260909-081637, a backup taken 2026-09-09T08:16:37Z; lifted the fence @200ms: the fleet is ready again | n/a | none |
| 2026-09-09T08:39:39Z | f52943e | https://github.com/eyupio/zoomies/actions/runs/34330170642 | lifecycle | passed | a queued job became a real process on this host and everything was cleaned up | runner created @1.6s: zoomies-drill-jitterbug-z4nh3422; workload on host @1.6s: zoomies-drill-jitterbug-z4nh3422; runner terminal state @30s: removed; host workloads after cleanup @30s: 0; github registrations after cleanup @59.7s: 0 | n/a | none |
| 2026-09-09T08:39:41Z | f52943e | https://github.com/eyupio/zoomies/actions/runs/34330170642 | removal | passed | removing a runner deleted its GitHub registration without waiting for the reaper | registered with github @1.6s: zoomies-drillrm-pepper-mq5gu5ap; removal requested @1.6s: zoomies-drillrm-pepper-mq5gu5ap; github registration after removal @1.6s: gone; host workload after removal @1.6s: gone | n/a | none |
| 2026-09-09T08:39:41Z | f52943e | https://github.com/eyupio/zoomies/actions/runs/34330170642 | restore | passed | a backup taken from a running fleet restores into an empty state directory, comes back fenced, and serves again once the fence is lifted | created pool @0s: pool_rky7kvoodcfsk; backed up @0s: zoomies-20260909-083941; stopped the controller @0s: kill; carried the encryption key across @0s: encryption.key; restored @0s: into a clean state directory; came back fenced @200ms: restored from /tmp/TestABackupCanBeRestoredIntoACleanDirectoryAndComesBackFenced3957894391/003/zoomies-20260909-083941, a backup taken 2026-09-09T08:39:41Z; lifted the fence @200ms: the fleet is ready again | n/a | none |

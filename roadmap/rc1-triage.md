# RC1 known-findings triage

Reviewed from `ede1c9a` on 9 September 2026. The owner is successfully using
Zoomies across a selection of GitHub repositories. A written qualification
record is not an RC1 prerequisite; wider user testing is the next step.

The owner subsequently asked for the reporting discrepancies and timing
metrics to be fixed before RC1, rather than carried as known limitations.
This change implements that expanded scope.

| Finding | Resolution in this change |
| --- | --- |
| Host cleanup success erased a failed GitHub registration deletion | Migration `0022` stores host and registration errors independently. Each retry clears its own error; both remain visible when both fail. |
| A successful process job's runner could end as failed after agent restart | An unrecorded exit is explicitly `exit_unknown`; the runner is retired without inventing a job outcome. Known non-zero exits still fail. The restart drill now requires `removed`, rather than accepting either outcome. |
| Retry and removal tasks overwrote the scheduling interval's end | Migration `0023` preserves `create_task_issued_at` from the first host poll that issues the create. The ordinary task clock still advances for retry diagnostics. A new scheduling histogram uses the job's actual runner and excludes unobserved or prewarmed intervals. |
| Deployment approval time appeared as scheduler/queue delay | Held jobs receive no eligibility stamp. An observed transition from waiting to queued starts the eligibility and queue clocks; repeated deliveries preserve that boundary. |
| Cleanup was marked complete at the first success signal | `cleaned_up_at` now requires positive host removal and GitHub absence confirmations. A stop or process exit does not qualify. The agent retries an unacknowledged removal report, and the controller counts the final confirmation once. |
| Docker removal hid inspection, sidecar or scratch-directory failures | These failures now prevent success. The parent and its cleanup labels survive a failed sidecar/directory removal so a retry can finish. Host confirmation also checks for companions belonging to the same runner, including when its parent is already gone. |

The runner API and details show the first create issue, host removal, GitHub
removal and final cleanup separately. Historical cleanup estimates are retained
as `cleanup_estimated_at` and labelled as estimates rather than silently
promoted to confirmed observations. Missing observations remain absent, not
zero. Existing metrics remain available; the two new histograms and their
sampling boundaries are described in `docs/metrics.md`.

## Upgrade and validation

Both migrations use the existing automatic backup-before-migration mechanism.
Rollback uses the older binary with its pre-upgrade database, as described in
`docs/upgrading.md`. No credentials or unrelated host settings are changed.

The cleanup-error regression failed on the original code in both cases tested:
a registration failure alone, and both cleanup sides failing before host
recovery. Focused tests now cover both recovery orders, duplicate reports,
approval holds, first issue versus enqueue and redelivery, missing-exit
classification, preservation of known failures, companion ownership and
migration of existing errors. The process restart drill has a strict outcome
assertion. Final verification results are supplied with the pull request.

RC1 follows merging the fixes with passing checks. This is a review of the
known findings and affected paths, not a claim that wider testing cannot find
additional defects.

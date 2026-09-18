# Inspector journal

## 2026-09-18 - `go test -race ./...` can spuriously time out `internal/controller` in this sandbox
**Finding:** A full `go test -race -count=1 ./...` run failed with `internal/controller` hitting the
10-minute per-package timeout, dumping hundreds of goroutines mid-migration. It reproduced once more
at a longer explicit `-timeout`, which first looked like a real deadlock worth chasing.
**Learning:** `internal/controller`'s tests each open a fresh SQLite store and run the full migration
set (`newHarness`), and modernc.org/sqlite (pure-Go) under the race detector is dramatically slower at
that than the interpreter overhead alone suggests. The same package passed in ~51s without `-race`, and
a plain `go test -count=1 ./...` (no race) passed cleanly on two separate full runs in this sandbox,
including immediately after a change that could not plausibly affect `internal/controller`. This reads
as sandbox CPU/resource contention under race instrumentation, not a deadlock or a regression.
**Prevention:** Before treating a `-race` timeout in `internal/controller` (or any package that opens
many fresh migrated stores) as a real bug, rerun the same package without `-race` and rerun the full
suite without `-race`. Only chase it as a deadlock if it reproduces there too. Don't burn a session's
budget re-running `-race` at ever-longer timeouts on a single package before doing that cheaper check.

# CI efficiency audit, 27 October 2026

What a `ci.yml` run costs, what takes the longest, and the three changes worth
making first. Written against commit `a69b7f6950f0f992d5e1004b463c94ea2bba03f7`
from the repository's own runs, not from a model of them: 25 runs of `ci.yml`
between 2026-10-26T17:38Z and 2026-10-27T14:21Z (20.7 hours), read job by job
and step by step with `gh run list` and `gh run view --json jobs`.

Seventeen of the 25 did work. Seven finished in two seconds with every job
skipped, because the change was one no job's filter wanted, and one was held as
`action_required` because it came from a fork.

Two numbers are kept apart throughout, because they answer different questions:

* **Runner minutes**: the sum of each job's `startedAt` to `completedAt`. This is
  what the fleet spends. It excludes queue time, which is why it is the honest
  cost axis.
* **Wall clock**: the run's `createdAt` to `updatedAt`. This is what an author
  waits for, and on this pool it is inflated by queueing: in the window one main
  push waited 30 minutes for its first job to start (40513984821) and one pull
  request waited 16 (40506085908), while a Go-changing pull request took 64 to
  87 minutes of wall clock for 304 to 353 runner minutes.

Runs with work, for scale, split by what they spent it on (Go shards, Playwright
legs, cross-compile legs, publishing, runner images, everything else):

| Run | Event | Total | Go | Playwright | Cross | Publish | Runner images | Other |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 40468812518 | pull request | 304 | 114 | 105 | 23 | 0 | 0 | 63 |
| 40495609659 | pull request | 353 | 151 | 118 | 25 | 0 | 0 | 59 |
| 40501155020 | pull request | 344 | 139 | 104 | 27 | 0 | 0 | 60 |
| 40490254411 | pull request (cancelled) | 351 | 127 | 125 | 26 | 11 | 0 | 61 |
| 40475990003 | push | 380 | 132 | 111 | 26 | 44 | 0 | 66 |
| 40506227711 | push | 383 | 151 | 102 | 25 | 46 | 0 | 59 |
| 40375378412 | push | 583 | 125 | 99 | 26 | 20 | 241 | 71 |

## 1. Waste sources

### 1.1 The Playwright setup is paid three times

The three legs (`chromium`, `mobile`, `other`) each check out, set up Go, set up
Node, download the `webdist` artifact, and only then run
`go build -o zoomies ./cmd/zoomies` and `npx playwright install chromium` before
the suite starts. The legs cost 99 to 125 runner minutes a run, 30 to 35 per cent
of what a Go-changing pull request spends, and the same binary is compiled three
times.

Measured `go build` per leg: 5m36s, 4m19s, 4m19s in run 40375378412 and 4m36s,
3m42s, 5m07s in run 40513984821. The browser download is 0m40s to 1m34s a leg.
That is about 13 runner minutes a run spent compiling one binary three times.

### 1.2 The cross-compile legs repeat what publishing builds

`Cross-compile` builds linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 and
windows/amd64, 4.4 to 6.5 minutes a leg, 23 to 30 runner minutes a run. On a
main push that publishes, `dev-binaries` runs `make dist`, whose loop builds the
same five pairs into `dist/`, and then checks that each file arrived. Both ran
on three of the five main pushes in the window (40375378412, 40475990003,
40506227711), so about 25 runner minutes of that run built the same five
binaries twice.

### 1.3 `Go (rest)` is the critical path because it carries the tree checks

`rest` is the only shard that runs `gofmt`, `go vet ./...`, `make vet-tags` and
`go mod tidy`, and they run before its tests. Its median is 27.9 minutes (21.1
to 39.9) against 24.8 for `controller-2`, 19.8 for `store` and about 19 for each
`api` shard. The checks alone are 8.0 to 11.5 minutes: `go vet` took 6m33s,
6m34s and 8m27s across three runs, `make vet-tags` 1m38s to 3m00s.

They are whole-tree checks that need no shard, and they sit in front of the
tests on the one shard that gates the merge. On run 40513984821 the merging path
was `changes` (17s), then `Go (rest)` (39.9 min), then `Coverage` (1.4 min),
then the gate: 42 minutes of the 80 the run took, 30 of which were spent waiting
for a runner before `changes` could start.

### 1.4 The two heavy Playwright legs are the longest jobs in the workflow

`Playwright (chromium)` median 44.7 minutes (41.7 to 58.2) and
`Playwright (mobile)` 44.4 (39.9 to 49.9), against a 90 minute timeout. They
start after `UI` (6 minutes), so on a pull request they are the critical path,
twice the length of the slowest Go shard.

### 1.5 What is already right, and not worth touching

* Every workflow a pull request runs has `concurrency`, cancelling that pull
  request's superseded run and queuing main's on purpose. The one exemption is
  `cancel-merged-checks.yml`, which is the closed handler and would cancel
  itself.
* Every workflow narrows its triggers, and `ci.yml` gates each job on what
  changed, with the filter written as a shell case beside the paths it names.
* `setup-go` caches modules and build output; `setup-node` caches npm in `UI` and
  in each Playwright leg; the image builds carry `type=gha` caches.
* The Go suite is sharded by package and, for the biggest packages, by test name;
  the artifact downloads retry the intermediary 403; every job has a
  `timeout-minutes`; `fail-fast` is off where one leg's failure should not hide
  another's.

The one cache missing is the Playwright browser, at 0.6 to 1.5 minutes a leg; it
is in the follow-ups rather than the three, because it adds an action to the
pinned inventory for 4.5 minutes a run.

### 1.6 Flagged, not proposed

* **Runner images.** Fourteen legs (seven catalogued systems, two architectures),
  19 to 48 minutes each, about 241 runner minutes when a change to `deploy/` or
  the catalogue triggers it, which was 41 per cent of that run. Every leg is a
  distinct operating system and architecture, so nothing in it is duplicate
  work: it is simply the most expensive thing the workflow does, and it is
  already gated to the changes that need it.
* **`Linux arm64`.** Runs the whole suite on one runner, unsharded, 19.1 minutes
  median, on every Go change. It backs the platform commitment in
  [support-and-measurement.md](support-and-measurement.md), so it stays.

## 2. Proposed fixes

Three, in this order: the first two buy runner minutes, the third buys wall
clock. Each was checked against the five guardrails (no required validation
hidden, no parallelism lost, no documented matrix leg dropped, no automatic
write-back job added, no mixing of repository YAML with org settings), and each
is repository YAML only.

### 2.1 Build the binary once, in `UI`, and share it with the Playwright legs

The UI job already builds `internal/api/webdist` and uploads it, because the
binary embeds it. Build the binary there, where the UI is finished and Go can be
set up in 20 seconds, and let the legs download it.

```yaml
      # jobs.ui, after `npm run build`
      - uses: ./.github/actions/setup-go
      - name: Build the binary the Playwright legs drive
        run: go build -o zoomies ./cmd/zoomies
      - uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        with:
          name: zoomies
          path: zoomies
          retention-days: 3
```

```yaml
      # jobs.playwright: replaces `./.github/actions/setup-go` and the go build
      - name: Download the binary
        id: download-zoomies
        uses: actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333 # v8.0.2
        with:
          name: zoomies
      # The same intermediary 403 the coverage and webdist downloads retry.
      - name: Retry the binary download
        if: steps.download-zoomies.outcome == 'failure'
        uses: actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333 # v8.0.2
        with:
          name: zoomies
      # An artifact does not keep the executable bit.
      - run: chmod +x zoomies
```

The legs no longer need Go at all: `web/tests/support/controller.mjs` spawns
`join(root, "zoomies")` and nothing in the suite invokes `go`, so
`./.github/actions/setup-go` can go from the job's steps with the build. The
ordering needs no change, because the legs already wait on `UI` (and are skipped
when it is), so the artifact exists whenever they run.

The gate time does not change, and saying so is the point: today `UI` ends at T
and each leg spends 4.6 minutes building before a 16 to 45 minute suite;
afterwards `UI` ends at T+4.6 and the legs start the suite at once. What changes
is that three compilations become one, about 9 runner minutes a run, and a cold
module fetch fails one job instead of three.

### 2.2 Skip `Cross-compile` on a run that publishes dev binaries

`needs.changes.outputs.publish` is already true exactly on the runs where
`dev-binaries` runs, and `make dist` builds the same five pairs:

```yaml
  build-matrix:
    name: Cross-compile
    needs: [changes, ui]
    if: >-
      github.event.action != 'closed'
      && needs.changes.outputs.go == 'true'
      && needs.changes.outputs.publish != 'true'
    # On a main push that publishes, dev-binaries runs `make dist`, which builds
    # these same five GOOS/GOARCH pairs with the same CGO_ENABLED=0 and
    # -trimpath, and then checks each file landed in dist/. Building them twice
    # cost about 25 runner minutes a publishing push. A pull request, and a
    # dispatch on a ref that does not publish, still cross-compiles here,
    # because dev-binaries does not run for them.
```

Validation is preserved rather than dropped: `dev-binaries` is in `ci-gate`'s
`needs`, `make dist` fails if any of the five builds fails, and the job then
fails if any of the five files is missing. Nothing is lost on a pull request
(`publish` is false there), and nothing is lost on a dispatch, where a ref that
publishes gets the same five builds from `make dist`.

### 2.3 Move the whole-tree checks into a job of their own

`gofmt`, `go vet ./...`, `make vet-tags` and `go mod tidy` do not read the
shard's packages; they read the tree. Give them their own job, running beside
the shards, and take them out of `rest`.

```yaml
  vet:
    name: gofmt, vet and tidy
    needs: [changes]
    if: github.event.action != 'closed' && needs.changes.outputs.go == 'true'
    runs-on: ${{ ((github.event_name == 'workflow_dispatch' && inputs.runner == 'github') || (github.event_name == 'pull_request' && github.event.pull_request.head.repo.fork)) && 'ubuntu-latest' || 'zoomies-linux-x64' }}
    timeout-minutes: 25 # the longest successful run seen was 11 minutes
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: ./.github/actions/setup-go
      - name: Stub the embedded UI
        timeout-minutes: 1
        run: make ui-stub
      - name: Download modules
        timeout-minutes: 10
        run: go mod download
      # The four steps move from jobs.go verbatim, minus their
      # `if: matrix.shard == 'rest'` conditions, along with the comments that
      # say why each exists.
```

`vet` then goes into `ci-gate`'s `needs`, which the test in `internal/docs`
enforces: a job outside the gate can fail without blocking a merge, so leaving
it out fails the build.

`rest` drops from 27.9 minutes (median) to about 19.4, and `controller-2` at
24.8 becomes the longest shard. The saving is 3.1 minutes of merging path at the
median and 11.5 on the worst run in the sample, for no change in runner minutes
(the same work, one more checkout and setup) and one more fleet slot busy for 11
minutes.

## 3. Validation

**Proven from live data.** Every duration, step name and per-run split above
comes from `gh run view --json jobs` on the runs named. The duplicate builds are
in the workflow files and in the observed steps: three `go build` steps of 3m42s
to 5m36s in the same runs, and `Cross-compile` plus `dev-binaries` both running
on 40375378412, 40475990003 and 40506227711.

**Not proven, and stated as such.** No fix has been applied, so there is no
before and after, and every saving below is expected rather than measured. No
live gating or cancellation test was performed. A push to a non-protected branch
would not exercise anything here, because `ci.yml` runs on push to `main` only;
proving path gating needs a pull request, which is a merge-shaped side effect
left to whoever owns the branch.

**Checked locally, not compiled.** The YAML sketches were checked against the
repository's own invariants that `internal/docs` holds: every `uses:` pinned to a
commit with its release beside it, a `timeout-minutes` per job, the `closed`
guard leading every condition, the new job in `ci-gate`'s `needs`, and the
`runs-on` expression copied rather than retyped. `go test ./internal/docs` was
not run: the pinned Go 1.27 toolchain is not installed in the environment the
audit was written in, so nothing here is compile-checked.

**Remaining risk.** The `zoomies` artifact arrives without its executable bit,
so the `chmod +x` step is required and would be the first thing to check on the
first run. Dropping `setup-go` from the Playwright legs is safe only because the
suite spawns the binary and runs no Go command; a future spec that runs `go`
would need it back. Fix 2.2 assumes `make dist` and the `Cross-compile` matrix
list the same five pairs. They do today, and nothing holds them together:
`TestTheDevChannelPublishesEveryBinaryDistBuilds` holds the dev release to
`make dist`, not `make dist` to the matrix. If the fix lands, that test should
grow the comparison, so that a platform added to one list cannot leave the other
behind.

## 4. Impact

Expected, against a measured baseline of 380 runner minutes for a median main
push and 304 to 353 for a Go-changing pull request. Runs a day are estimated
from 17 runs with work in 20.7 hours: about 20 a day, of which roughly 10 run
Playwright and 4 publish a dev build.

| Fix | Runner minutes per run | Runs a day | Runner minutes a day | Merging path |
| --- | ---: | ---: | ---: | --- |
| 2.1 one binary instead of three | -9 | 10 | -90 | unchanged (the build moves into `UI`) |
| 2.2 no duplicate cross-compile | -25 | 4 | -100 | unchanged (not on the publish path) |
| 2.3 tree checks off `rest` | 0 | 16 | 0 | -3 median, -11.5 worst |
| **Total** | | | **about -190** | **-3 to -11.5 minutes to green** |

About 190 runner minutes a day, close to 4 per cent of the measured daily total,
and a merge path 3 to 11 minutes shorter. The two axes are separate on purpose:
2.3 buys no minutes, and 2.1 and 2.2 buy no time. Only 2.3 changes what an
author waits for.

Confounders to carry into any comparison: the pool was saturated throughout the
window (30 minutes to the first job on one push, 114 minutes of wall clock for
the 583 runner minute deploy run, 64 to 87 for the Go-changing pull requests),
the Go and npm caches were warm, and `startedAt` to `completedAt` excludes queue
time. The same job varies by 20 to 40 per cent between runs (rest 21.1 to 39.9,
chromium 41.7 to 58.2), so a before and after needs medians over a week, not one
run.

## 5. Follow-ups, in the order they are worth taking

1. **Shard the two 44 minute Playwright legs.** The biggest remaining wall clock
   win, about 20 minutes off the merge path, and the reason it is not in the
   three is that it spends fleet capacity to buy time: two more slots a run and a
   little more setup, on a pool that was queueing. Take it after 2.1 and 2.2 free
   the capacity.
2. **Cache the Playwright browser.** 0.6 to 1.5 minutes a leg, about 4 minutes a
   run, at the cost of `actions/cache` joining the pinned inventory. Worth it,
   but it is a new action and a review of its pin, not a one line change.
3. **`rest` after 2.3.** At about 19.4 minutes it is close to the others, and the
   next split (the tree-reading packages into `docs`, which already exists for
   docs-only changes) is worth a measurement before it is worth a change.

## 6. How to check the result

After the change, let the caches warm, then read the same numbers back:

```sh
gh run list --workflow ci.yml --limit 25 --json databaseId,event,conclusion,createdAt,updatedAt
gh run view RUN_ID --json jobs --jq '.jobs[] | [.name, .startedAt, .completedAt] | @tsv'
```

Compare medians, not single runs: runner minutes per main push and per
Go-changing pull request, the longest job in each run, and the gate job's own
start and finish, which is the merging path. Report the three separately, the
way section 4 does, or a fix that moves minutes will look like a fix that saves
time.

first run. Dropping `setup-go` from the Playwright legs is safe only because the
suite spawns the binary and runs no Go command; a future spec that runs `go`
would need it back. Fix 2.2 assumes `make dist` covers exactly the five matrix
pairs, which is true today and is held by
`TestTheDevChannelPublishesEveryBinaryDistBuilds`.



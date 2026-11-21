# Plan 021: Refuse a downloaded release that reports another version

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/installer/selfupdate.go internal/installer/selfupdate_test.go internal/version
```

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW-MEDIUM (could refuse a real release if the comparison is wrong)
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

The updater verifies an Ed25519 signature over `checksums.txt`, then the
binary's checksum. The signed text holds hashes and asset names and no
version. Whoever controls the download source (a `ZOOMIES_BASE_URL` mirror,
or replaced release assets, the threat `internal/releasekey` names) can
serve an old release's genuine checksums, signature and binary under a
newer tag. Everything verifies, the downgrade guard sees a newer tag, and
a release with known holes is installed as root.

## Current state

- `internal/installer/selfupdate.go:179`: the downgrade guard compares
  `opts.Current` with the tag using `version.CompareBuilds`, and is skipped
  when `tag == "dev"`.
- `internal/installer/selfupdate.go:233-239`:

```go
	// A file that hashes right but will not run -- the wrong architecture
	// behind a matching sums file on a mirror -- must not replace one that does.
	if _, err := opts.run(ctx, cand.Path, "version", "--short"); err != nil {
		cand.Discard()
		return nil, res, fmt.Errorf("the downloaded binary will not run on this host; the installed one was left in place: %w", err)
	}
	return cand, res, nil
```

- `internal/version/version.go:51-57`, `Short`, prints `v1.2.3 (abc1234)`
  when a commit is known and `v1.2.3` otherwise.
- `internal/version/compare.go:39`, `CompareBuilds(a, b)`, returns
  `SkewNone` for equal releases, `SkewBehind`, `SkewAhead`, or
  `SkewDiffers` when either side is not shaped like a release.
- `internal/version/compare.go:138`, `Release(v)`, extracts the release
  from a version string; read it and its tests before using it.

## Scope

**In scope**: `internal/installer/selfupdate.go`,
`internal/installer/selfupdate_test.go`.

**Out of scope**: `install.sh`, `connect-proxmox.sh`, the release
workflow, the signature format, `internal/version`.

## Steps

### Step 1: Pin the output format

Read how `cmd/zoomies` prints `version --short` (grep `"--short"` or
`Short()` under `cmd/zoomies`) and confirm it is exactly
`version.Short()` plus a newline. Read `selfupdate_test.go` to see what
the fake `run` returns today.

### Step 2: Write the failing tests

In `selfupdate_test.go`, following the tests that already serve a
candidate:

- `TestADownloadThatReportsAnotherReleaseIsRefused`: tag `v1.9.0`, the
  fake `run` answers `v1.3.6 (abc1234)`. Expect an error, the candidate
  file removed, the installed binary untouched.
- Rows that must install: answer `v1.9.0 (abc1234)`; answer `v1.9.0`; a
  pre-release tag from this project's own shape (take one from
  `internal/version`'s tests) answered by itself; tag `dev` with any
  answer.

**Verify**: the first test fails before the fix.

### Step 3: Compare what the binary says with what was asked for

Keep the output of the `version --short` run. When `tag != "dev"`, take
the first whitespace-separated field of the trimmed output and require
`version.CompareBuilds(reported, tag) == version.SkewNone`; otherwise
discard the candidate and return an error such as: the download for %s
reports itself as %s, so it is not the release that was asked for; the
installed binary was left in place. Rewrite the comment above to cover
both failures (will not run; is another release) and name the threat: the
signature covers the checksums, not the version.

**Verify**: `go test -race -count=1 ./internal/installer/...` passes.

## Done criteria

- [ ] The new tests pass; the refusal test failed before Step 3.
- [ ] `go test -race -count=1 ./internal/installer/... ./internal/version/...` passes.
- [ ] `gofmt -l internal/installer` and `go vet ./internal/installer/...` clean.
- [ ] `make test` exits 0.
- [ ] Commit message: `Refuse a downloaded release that reports another version`.

## STOP conditions

- A released binary's `version --short` does not start with its tag (for
  example the rolling build's version under a release tag, or a `v` prefix
  that the tag lacks). `CompareBuilds` tolerates some of this; if a real
  tag shape in `internal/version`'s tests would be refused, stop.
- `opts.run` does not return the command's output.

## Maintenance notes

- Deliberately deferred: putting the tag inside the signed message, which
  is the stronger fix and changes the release workflow and both shell
  installers. `install.sh` and `connect-proxmox.sh` have the same gap.

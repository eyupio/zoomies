# Plan 026: Tighten Eli's credential pattern and redact model input fully

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first, including the section
> on Eli repair plans: this change must update
> `roadmap/eli-repair-implementation-handoff.md`. This plan has three
> parts; land them as three commits, in order. Follow each step, run every
> verification, and stop on any STOP condition there or here. Update this
> plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/prrepair internal/redact internal/github/eli_repairs.go
```

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MEDIUM (what the model sees changes)
- **Depends on**: none
- **Category**: bug, security
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

One pattern in `internal/prrepair` does two jobs: it hides credentials in
text sent to the model, and a file that matches it may not be rewritten by
Eli. It is wrong in both directions. Its `sk-` alternative has no word
boundary, so `task-queue-worker-pool` or `disk-usage-threshold` counts as
a key: the model sees `ta[redacted credential]` and any fix touching that
file is refused. Its private-key alternative can run to the end of the
whole snapshot. And it knows only four shapes, so a password in a URL, a
bearer token or a Slack token in a failed job's log goes to the model
provider as it is, although `internal/redact` already removes them.

## Current state

- `internal/prrepair/repair.go:125-128`:

```go
var credential = regexp.MustCompile(`(?i)(gh[pousr]_[a-z0-9_]{10,}|github_pat_[a-z0-9_]{10,}|sk-(?:proj-)?[a-z0-9_-]{12,}|(?:AKIA|ASIA)[A-Z0-9]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$))`)

// Logs, issue text and repository files are untrusted and may contain credentials.
func Redact(s string) string { return credential.ReplaceAllString(s, "[redacted credential]") }
```

- `repair.go` near 226 applies `Redact` to the whole marshalled snapshot;
  near 419-423 a file matching `credential` is refused with "a file
  containing credentials cannot be rewritten by Eli".
- `internal/redact/redact.go` has `Text`, a wider rule set with `\b`
  before its `sk-` rule and a test that a kebab-case pool name is not a
  key.
- Model-bound text is built in `internal/github/eli_repairs.go` (failure
  logs near 428, also 288 and 310) and `internal/prrepair/context.go`
  (`reveal`, the source windows).

## Scope

**In scope**: `internal/prrepair/repair.go`, `internal/prrepair/context.go`,
their tests, `internal/github/eli_repairs.go` only if Part C needs the
call site there, the Eli handoff file.

**Out of scope**: `internal/redact` (use it, do not change it), the
"cannot rewrite a file containing credentials" rule's meaning.

## Part A: A word boundary before sk-

1. Test first, in the `internal/prrepair` test file that covers `Redact`:
   `TestOrdinaryHyphenatedWordsAreNotCredentials`, table-driven over
   `task-queue-worker-pool`, `disk-usage-threshold`,
   `-webkit-mask-box-image-source`, each expected unchanged by `Redact`,
   and a file containing them expected to be editable (follow the test
   that asserts the refusal). Keep a row showing `sk-proj-` followed by
   twenty characters is still redacted.
2. Add `\b` before `sk-` in the pattern.
3. **Verify**: `go test -race -count=1 ./internal/prrepair/...` passes.
4. Commit: `Stop Eli reading hyphenated words as credentials`.

## Part B: A private key that never ends

1. Test first: `TestAnUnterminatedPrivateKeyDoesNotSwallowWhatFollows`.
   Build a snapshot (or the smallest input the snapshot path accepts)
   where one field holds `-----BEGIN RSA PRIVATE KEY-----` with no END,
   followed by other fields. Assert the later fields are still present in
   what `Redact` returns.
2. Bound the unterminated match. The simplest sound bound: replace the
   `|$` ending with a limit on the block's length, for example
   `[\s\S]{0,8192}?` before the END alternative, and when no END is found
   redact only the BEGIN line. Whatever you choose, a terminated key of
   ordinary size must still be redacted whole (keep or add that test).
3. **Verify** as above.
4. Commit: `Stop an unterminated private key swallowing the rest of a snapshot`.

## Part C: The wider rules on everything bound for the model

1. Check the boundary first: confirm no test forbids `internal/prrepair`
   importing `internal/redact` (grep the package for a purity or boundary
   test). If one does, apply `redact.Text` at the caller that builds the
   model input in `internal/github` or `internal/controller` and say so in
   the commit body.
2. Test first: `TestModelInputCarriesNoCommonSecretShapes`, table-driven
   over a URL with a password (`https://ci:hunter2@registry.example.com/`),
   `Authorization: Bearer eyJhbGciOi...`, a Slack `xoxb-` token, an
   `AIza...` key, an `npm_...` token and `DB_PASSWORD=...`. Put each into
   a failure log and into a source window, build the model input the way
   the existing tests do, and assert the secret is absent.
3. Apply `redact.Text` to the model-bound text as well as `Redact`:
   failure logs, the diff, the pull request body and revealed windows.
   Keep the refusal to rewrite a file tied to the narrow `credential`
   pattern only, so the wider rules cannot block edits.
4. **Verify**: `go test -race -count=1 ./internal/prrepair/... ./internal/github/... ./internal/redact/... ./internal/controller/...`
   passes.
5. Commit: `Redact everything bound for the model with the full rule set`.

## Then: update the Eli handoff

Add the dated section and ledger row described in the shared rules,
covering all three parts.

## Done criteria

- [ ] Each part's test failed before its fix and passes after.
- [ ] `go test -race -count=1 ./internal/prrepair/... ./internal/github/... ./internal/redact/... ./internal/controller/... ./internal/docs/...` passes.
- [ ] `gofmt -l` and `go vet` clean on changed packages; `make test` exits 0.
- [ ] The handoff file has the new section and ledger row.

## STOP conditions

- An existing test expects one of the hyphenated shapes to be redacted.
- `redact.Text` changes text in a way that breaks the model reply's
  ability to name a file or a line (a test that maps the reply back to
  source positions fails). Report it; redaction must not shift offsets
  the reply is checked against.

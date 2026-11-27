# Plan 013: Stop single sign-on adopting an account that holds API tokens

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/auth/oidc.go internal/auth/auth.go internal/controller/bootstrap.go
```

## Status

- **Priority**: P0
- **Effort**: S-M
- **Risk**: MEDIUM (sign-in behaviour)
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

When somebody signs in through the identity provider and an account with
the same username already exists, the account is adopted unless it has a
password. The account `CreateUnattendedIdentity` makes from
`ZOOMIES_BOOTSTRAP_ADMIN` with a token file has role platform, no password
and no subject. Anyone whose username claim matches it gets a session as
the platform account; with group mapping configured, the same sign-in
rewrites that account's role and silently caps its token. Reproduced by the
audit, both ways.

## Current state

- `internal/auth/oidc.go:400-416`, in `EnsureUser`:

```go
	u, err = st.GetUserByUsername(ctx, username)
	switch {
	case err == nil:
		if u.OIDCSubject != "" && u.OIDCSubject != claims.Subject {
			return nil, fmt.Errorf("the account %q is already linked to a different single sign-on identity; ...", username)
		}
		// Adopting an account by username is how an administrator pre-creates
		// somebody's account ... so an account that has a password of
		// its own is only adopted when the operator has asked for exactly that.
		if u.PasswordHash != "" && !p.cfg.LinkByUsername {
			return nil, fmt.Errorf("an account named %q already exists and signs in with a password, ...", username)
		}
		u.OIDCSubject = claims.Subject
		return p.sync(ctx, st, u, claims, username)
```

- `internal/auth/auth.go:586-603`, `CreateUnattendedIdentity`, creates
  `&store.User{Username: username, Role: store.RolePlatform}` and one API
  token named `bootstrap` owned by it.
- The legitimate path: `internal/api/handlers_admin.go:144` sets
  `SSOOnly: req.Password == ""`, and `internal/auth/auth.go:1165-1197`
  creates an account with no password and no subject for an administrator
  to pre-create. Such an account has never signed in, so it owns no tokens.

## The rule to implement

An account with no subject yet that already owns at least one API token is
not adopted by username, unless `oidc.link_by_username` is set. A
pre-created SSO-only account owns none, so it is still adopted. No schema
change.

## Scope

**In scope**: `internal/auth/oidc.go`, its test file, and one read query in
`internal/store` only if none exists (see Step 1).

**Out of scope**: `CreateUnattendedIdentity`, the bootstrap code, the
`sync` method, the login page.

## Steps

### Step 1: Find the read path

Grep `internal/store` for an existing query that lists or counts API tokens
for a user id (names like `ListAPITokens`, `APITokensForUser`). Use it. If
none takes a user id, add the smallest one, `CountAPITokensForUser(ctx,
userID) (int, error)`, in the store file that holds the other token
queries, with a test beside them.

### Step 2: Write the failing tests

In the test file that already exercises `EnsureUser` (grep for it), add:

- `TestSingleSignOnDoesNotAdoptAnAccountThatHoldsAPITokens`: create the
  bootstrap identity with `CreateUnattendedIdentity`, then resolve claims
  whose username matches. Expect an error, the account's `OIDCSubject`
  still empty and its role still platform.
- `TestSingleSignOnStillAdoptsAPreCreatedAccount`: create an SSO-only
  account the way `handleCreateUser` does, resolve matching claims, expect
  the account linked.
- A row for `LinkByUsername: true` adopting the token-holding account, so
  the migration switch keeps its meaning.

**Verify**: the first test fails before the fix.

### Step 3: Add the refusal

In `EnsureUser`, directly after the password check, when `u.OIDCSubject ==
""` and `!p.cfg.LinkByUsername`, count the account's tokens and return an
error when there is at least one. The message must say what to do, for
example: the account `%q` is used by API tokens and has never signed in
with single sign-on, so this identity was not linked to it; an
administrator can set `oidc.link_by_username`, or create an SSO account
under another name. Extend the comment above to name this case: an account
with no password is not always one made for single sign-on.

**Verify**: `go test -race -count=1 ./internal/auth/...` passes.

## Done criteria

- [ ] The three tests pass; the first failed before Step 3.
- [ ] `go test -race -count=1 ./internal/auth/... ./internal/store/... ./internal/api/...` passes.
- [ ] `gofmt -l` and `go vet` clean on changed packages; `make test` exits 0.
- [ ] If `docs/` describes account adoption (grep `link_by_username`), the
      sentence is updated.
- [ ] Commit message: `Stop single sign-on adopting an account that holds API tokens`.

## STOP conditions

- An existing test adopts by username an account that owns tokens and
  expects success without `LinkByUsername`: the rule conflicts with a
  behaviour somebody chose, and the owner decides.
- The store has no way to read tokens by user and adding one needs a
  migration.

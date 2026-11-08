# A cache service on every host (ZF-245)

Status: slice 1 built (the protocol core, `internal/cacheservice`); the rest is
not started. This note is the design the later slices are held to. Its first
open risk was checked on 8 November 2026 and the original plan did not survive
it; see "What is not proven".

## The problem

A runner is ephemeral, so every job starts with an empty disk. The pool cache
(`/opt/zoomies-cache`) was meant to fix that, but Zoomies only mounts it. A
workflow has to point its tools at it, and the repository's own CI does not:
`setup-go` uses `actions/cache` and every image build uses `type=gha`, both of
which go to GitHub's cache service over the internet.

Measured on this fleet on 8 November 2026, a `Go (rest)` job spent 35 to 89
seconds in `setup-go` restoring that cache, against 16 to 19 seconds for the
same step on a GitHub-hosted runner, and the `Images` job that pushes and
exports cache from a Zoomies host took 58 minutes where the hosted run took
4. A fleet should not have to rewrite its workflows to avoid this. The owner's
requirement is that Zoomies manages it.

## The approach

Run a small `actions/cache`-compatible server in the agent on each host and
redirect the runner's actions to it. The toolkit finds its service through
`ACTIONS_RESULTS_URL`, which the runner sets itself (see "Redirecting the
runner"), so `actions/cache`, every `setup-*` action's built-in
cache and Buildx's `type=gha,version=2` all reach it with **no workflow change**.
A lost cache must only make a build slower: every call is a miss on failure.

This is the third layer of
[candidates-from-hosted-services.md](candidates-from-hosted-services.md); it
was proposed there as ZF-231, a number that has since been used for something
else.

## What slice 1 proves

`internal/cacheservice` is a stdlib-only `http.Handler`. It was written against
GitHub's own `@actions/cache` 4.0.5, and `TestTheRealToolkitClientSavesAndRestores`
runs that client against it (opt in with `ZOOMIES_ACTIONS_CACHE_MODULE`). Doing
so found three things a protocol written from the type declarations got wrong:

* The client sends protobuf field names (`size_bytes`, `restore_keys`), not the
  camelCase its own types show. Read as absent, `restore_keys` would have made
  every prefix lookup a silent miss.
* `entryId` is an int64 parsed as a BigInt; a hex id fails the save.
* The storage SDK needs a path with a container and a blob name.

Verified with the real client: save and restore by key, restore by prefix, a
miss reported as a miss, an 8 MiB archive (one PUT) and a 200 MiB archive
(64 MiB staged blocks), with the restored bytes compared by digest.

## What is not proven

These are the risks, in the order they matter. The first is now settled, and
it changed the design.

1. **Settled against the plan: a runner does not honour `ACTIONS_RESULTS_URL`
   from its container environment.** Read from the runner source
   (`actions/runner` at `5ba5044`, 6 November 2026). For every Node action
   `NodeScriptActionHandler.PrepareExecution`, and for every container action
   `ContainerActionHandler`, assigns `Environment["ACTIONS_RESULTS_URL"]` and
   `Environment["ACTIONS_CACHE_URL"]` from the job message's system connection
   (`ResultsServiceUrl`, `CacheServerUrl`). It does this after the handler's
   environment was seeded from the step's `env`, so a value in the runner
   container, in a step's `env:` or written to `GITHUB_ENV` by a
   `ACTIONS_RUNNER_HOOK_JOB_STARTED` script is overwritten, and no other code in
   the runner reads the variable. The fallback this note first proposed (a
   job-started hook) therefore does not work either. `run:` steps are not given
   the variable at all, so only actions are affected, which is what the cache
   action and `docker/build-push-action` are.
   The redirect has to happen below the runner. See "Redirecting the runner".
2. **BuildKit's `type=gha,version=2` against this service.** BuildKit has its
   own GitHub-cache client. There was no Docker daemon here to run it.
3. **The token's `ac` claim.** The branch rule reads the `ac` claim of the job's
   runtime token. Its shape (a JSON string holding `{Scope, Permission}` with
   bits 1 for read and 2 for write) is from memory and is not verified; the
   service falls back to one shared scope when it cannot read the claim, so
   the failure is a shared cache and never a broken one.
4. **Reaching the host.** A runner under `dind` shares the sidecar's network
   namespace and has no network of its own, so the address has to be one the
   sidecar can route to. The agent has no HTTP listener today, and the toolkit
   builds its URLs with an absolute path (`new URL("/twirp/...", base)`), so the
   base URL cannot carry a per-runner prefix: identity has to come from the
   port or the source address.
5. **GitHub's protocol is not a published contract.** A change would turn into
   "always miss". Keep the real-client test in CI where node is available.

## Design for the slices that follow

* **Redirecting the runner.** The runner starts every Node action as
  `<externals>/<node version>/bin/node`, with the environment it has just built.
  The runner image is Zoomies', and the runner is started with `--disableupdate`
  on the registration path, so the image can replace each `externals/node*/bin/node`
  with a small wrapper that, when `ZOOMIES_CACHE_SERVICE_URL` is set in the
  container, sets `ACTIONS_RESULTS_URL` to it and then runs the real node. No
  runner patch, nothing a workflow can see or has to change, and with the
  variable unset the wrapper is a pass-through, so the failure mode is GitHub's
  cache as today. The costs are real: it covers Node actions only (a Docker
  action keeps GitHub's URL), it covers only the stock images (an image of the
  operator's own is left alone, as it is for the Docker client), the wrapper
  has to be rebuilt against each runner release's layout, and whether a
  JIT-configured runner can still self-update was not checked. The alternatives
  were weighed and set aside: a patched runner fork (a build and a supply-chain
  burden for two lines) and a TLS-intercepting proxy that rewrites the calls
  (a CA every job would trust).
* **Setting.** `cache.service` on the pool's `CacheConfig`, a JSON field like
  `cache.tools`, so no migration. Off by default, refused without
  `cache.enabled`, refused on the `process` backend. It reaches the agent in
  the existing create-runner task, so an older agent that ignores it needs a
  feature flag so the controller can say so.
* **Where it listens.** One listener per cache identity (`cacheIdentity` in
  `internal/backend/cache.go`), bound to the address runners reach the host by,
  never to all interfaces. The port is the identity, which is what makes a
  pool-scoped or repository-scoped cache keep its isolation.
* **Where it keeps entries.** Beside the pool cache, under the same identity,
  inside the same `size_limit`, so the existing size accounting and eviction
  remain the one place that decides what a pool may keep.
* **The branch rule.** A pull request reads its own ref and the default
  branch's entries and writes only its own, as GitHub's cache does. Because
  the claim is read and not verified, this guards against a workflow doing the
  wrong thing; it is not a boundary against code that forges a token. The
  boundary is the same as the pool cache's: which jobs share a pool. Untrusted
  pull requests belong on a pool of their own, and the warning
  `cache.service_shared` (organisation installation, pool scope) says so.
* **Failure.** Short timeouts, a miss on any error, an upload that fails is
  dropped and logged. A host problem (`cache.service_degraded`) after repeated
  failures. No change to what a job can do when the service is down.
* **Findings and docs.** Every new finding code gets a row in
  `docs/problem-codes.md`; the settings get rows in `docs/configuration.md`;
  `docs/persistent-caches.md` replaces its "recipes" framing for pools that turn
  the service on.

## Not in this work

* S3-compatible storage for sharing across hosts: the roadmap records that as a
  trust question, and it is not authorised.
* The registry mirror for base-image pulls (ZF-246): a separate, smaller
  package.
* Default environment variables for plain `run:` steps (`GOCACHE` and the
  like): a separate decision, because a workflow that does not use
  `actions/cache` cannot be reached by this service.

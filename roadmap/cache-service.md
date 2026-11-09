# A cache service on every host (ZF-245)

Status: slices 1 to 3 built (the protocol core, the runner image's node wrapper,
and the listener and wiring); the findings, a real-daemon check and the rest are
not. This note is the design the later slices are held to. Its first
open risk was checked on 8 November 2026 and the original plan did not survive
it; see "What is not proven".

## The problem

A runner is ephemeral, so every job starts with an empty disk. The pool cache
(`/opt/zoomies-cache`) was meant to fix that, but Zoomies only mounts it. A
workflow has to point its tools at it, and the repository's own CI does not:
`setup-go` uses `actions/cache` and every image build uses `type=gha`, both of
which go to GitHub's cache service over the internet. (This design reaches the
first; the second is not redirected, see below.)

Measured on this fleet on 8 November 2026, a `Go (rest)` job spent 35 to 89
seconds in `setup-go` restoring that cache, against 16 to 19 seconds for the
same step on a GitHub-hosted runner, and the `Images` job that pushes and
exports cache from a Zoomies host took 58 minutes where the hosted run took
4. A fleet should not have to rewrite its workflows to avoid this. The owner's
requirement is that Zoomies manages it.

## The approach

Run a small `actions/cache`-compatible server in the agent on each host and
redirect the cache's calls from the runner's actions to it, so `actions/cache`
and every `setup-*` action's built-in cache reach it with **no workflow
change**. The toolkit finds its service through `ACTIONS_RESULTS_URL`, which the
runner sets itself and which is also where the artifact actions find the
artifact service, so the address cannot simply be replaced (see "Redirecting the
runner"). A lost cache must only make a build slower: every call is a miss on
failure.

**Buildx's `type=gha` layer cache: opt-in, experimental.** BuildKit is a Go
program that reads the results address for itself, which a preload inside node
does not reach. buildx takes that address from `ACTIONS_RESULTS_URL` in its own
environment (`build/opt.go`, `addGithubToken`), and `docker/build-push-action`
starts it as a child of the node action, so the preload can set the address in
the environment of those `docker` children alone, leaving its own copy for the
artifact calls. That is `ZOOMIES_CACHE_SERVICE_BUILDX`, off unless a pool sets it:
a refused cache export fails a build that does not say `ignore-error=true`, and
the builder reaches the host from inside the sidecar's network, which nothing here
has run. BuildKit's own client library (`tonistiigi/go-actions-cache`) found three
faults the toolkit does not: the replies must use the protobuf field names
(`signed_upload_url`, `entry_id`), `size_bytes` arrives as a number as well as a
string, and the Azure SDK dereferences `x-ms-request-id` on an upload's reply, so
a service without it panics the builder; ranged reads also arrive as
`x-ms-range`, not `Range`. All four are fixed and held by tests.

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

   **A defect in the first version of that redirect, found and fixed on
   8 November 2026.** The wrapper set `ACTIONS_RESULTS_URL` to the service. That
   address is also where `@actions/artifact` (upload-artifact and
   download-artifact v4) finds the artifact service, a different Twirp service
   under the same base, so a pool with the service on would have failed every job
   that uploads or downloads an artifact, and this repository's CI does both. The
   wrapper now leaves the address alone and loads
   `deploy/runner-cache-redirect.cjs`, which rewrites a request only when it is
   addressed to the cache service by name.
2. **BuildKit's `type=gha,version=2` against this service.** Checked with
   BuildKit's own cache client library, not with a builder: a live run needs a
   Docker daemon, and the sidecar-to-host path in particular has not been
   exercised. Opt-in for that reason (see above).
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

## What slice 2 proves: the node wrapper

`deploy/runner-node-wrapper.sh` (since replaced by the program
`deploy/node-wrapper.go`, which the image build compiles: a shell script lost an
action's hyphenated inputs, and needed a shell in a job container's image),
`deploy/runner-node-wrap.sh` and
`deploy/runner-cache-redirect.cjs` are built, and the runner image installs them
(the wrap in the RUN that unpacks the runner, because a later RUN would copy two
hundred megabytes of node into a new layer). They were tested against the real
thing: the glibc node binaries from the pinned release
(`actions-runner-linux-x64-2.338.2`, checked against the Dockerfile's SHA-256),
node 20.20.2 and 24.22.1, wrapped by the same installer the image build runs,
running GitHub's `@actions/cache` client. GitHub's results service is stood in
for by an HTTPS server, as it is in production, which answers artifact calls and
refuses cache calls. A control run without `ZOOMIES_CACHE_SERVICE_URL` fails by
asking it for the cache; the run with the service on saves and restores through
the service with no cache call reaching "GitHub"; and an artifact-style call made
by the toolkit's own HTTP client still reaches "GitHub" and never the service.
`TestTheRunnersOwnNodeSendsOnlyTheCacheToTheService` repeats it (opt in with
`ZOOMIES_RUNNER_DIR`), and was checked by making the preload redirect every
request, which fails the artifact assertion: the defect above. The wrapper's own
behaviour (the v2 guard, an image without the preload, arguments, exit code,
standard input, the address and the token left alone) is held by tests that run
the real scripts and was checked by breaking each in turn.

Still not run: a whole job on a real runner, which needs GitHub to dispatch one.
That the runner starts node with the environment it built is read from its source
(`NodeScriptActionHandler` passes `Environment` to `StepHost.ExecuteAsync`), not
observed. `test/runner-image/check.sh` checks the built image once CI builds one.

## What slice 3 builds: the listener and the wiring

A pool's `cache.service` (a field of `store.CacheConfig`, so no migration; the
API refuses it without `cache.enabled` and on the `process` backend; the pool
editor and `zoomies pools edit --cache-service` set it) now reaches a runner:

* `DockerBackend` owns the services (`internal/backend/cacheservice.go`),
  because it already holds the shared folder, the network, the create path and
  the runner's environment. One service per cache identity, started on the
  first create that needs it, listening on the host's address on the network
  the runners are attached to (`APIClient.NetworkGateway`) on a port of its own,
  keeping its entries under `<shared>/cache/service/<identity>` within the
  pool's `size_limit`, or 10 GiB where there is none.
* The runner's environment gets `ZOOMIES_CACHE_SERVICE_URL` after the pool's own
  variables, so a pool cannot send its runners elsewhere, and the name is
  reserved in `runners.env` and in a pool's `env`.
* `Registry.Close` stops the services when the agent stops. Runners outlive the
  agent, so a restart leaves them with an address that no longer answers until
  they finish; their cache calls miss.
* Every reason a host cannot offer a service is a runner on GitHub's cache, with
  the reason logged once per pool: no shared folder, an agent running in a
  container, no address for the host on the runners' network, an address that
  cannot be listened on. Nothing in it can fail a create.

Tested with fakes for the host's address (loopback) and not with a daemon:
the service is reachable and speaks the protocol; one service per pool and per
repository, never shared across them (checked by breaking it); the variable is
last in the environment (checked by breaking it); and each way of being unable
to offer a service. The CLI work also fixed `pools edit`, which rebuilt the
whole `cache` object from flag defaults and so turned the cache off, and the
tool cache with it, whenever an edit named one cache flag.

Not done, and why it matters:

* **No run against a real daemon.** Whether a container on the bridge, and a
  runner under `dind` that shares its sidecar's network namespace, can reach the
  host's address on that port depends on the host's firewall (`ufw` and some
  `firewalld` zones drop container-to-host traffic by default). This is the next
  thing to try, and the likeliest cause of a pool that quietly stays on GitHub's
  cache.
* **No feature flag for an older agent.** An agent that predates the setting
  ignores it, so the pool stays on GitHub's cache without saying so.
* **No findings.** `cache.service_shared` (the service on for a pool-scoped
  cache whose installation spans an organisation) and `cache.service_degraded`
  are in the design and not built; the only signal today is the agent's log.
* **The agent in a container is unsupported**, which includes the controller's
  embedded agent in the compose deployment. Rootless Podman has no host address
  on its networks and will stay on GitHub's cache.
* **A restart changes the port.** Persisting it would keep running runners
  connected across a restart.
* **Docker actions keep GitHub's cache**, and BuildKit's `type=gha` against the
  service is opt-in and has not been run against a live builder.

**A defect found after merge.** The wrapper stands in for node on every JavaScript action, cache service or not, so anything it does to the environment it does to every job. It was `#!/bin/sh`; on Ubuntu and Debian that is dash, which drops environment variables whose names are not shell identifiers, and the runner passes every action input as `INPUT_<NAME>` with the hyphens the action wrote (`INPUT_GO-VERSION`). Every such input arrived empty. The earlier tests passed because they checked the variables the wrapper cares about and a fake node that was itself a `/bin/sh` script, which loses the variables a second time. A job in a `container:` block runs the node at `/__e` inside the job's own image, and the externals volume that gives it that node was a copy of the wrapped tree, so the wrapper's bash had to exist in the job image; the volume's fill now puts the real node back, so a job image never runs the wrapper (an agent that predates the fill still hands it the wrapper). The wrapper is now a small static program (`deploy/node-wrapper.go`, compiled by the image build), which keeps every name and needs no shell anywhere; the unit test uses a copy of env as the real node, because a `/bin/sh` script loses the variables itself; and `test/runner-image/check.sh` asks the image's own wrapped node for a hyphenated variable.

## Design for the slices that follow

* **Redirecting the runner (built and tested; see above).** The runner starts
  every Node action as `<externals>/<node version>/bin/node`, with the
  environment it has just built. The runner image is Zoomies', and the runner is
  started with `--disableupdate` on the registration path, so the image replaces
  each glibc `externals/node*/bin/node` with a small wrapper. When
  `ZOOMIES_CACHE_SERVICE_URL` is set in the container and the job is on cache
  service v2, the wrapper adds `--require` of a preload to `NODE_OPTIONS` and
  runs the real node. The preload rewrites a request only if it is addressed to
  `/twirp/github.actions.results.api.v1.CacheService/`; the address, the token
  and every other request are untouched. No runner patch, nothing a workflow can
  see or has to change, and with the variable unset, or an image without the
  preload, the wrapper is a pass-through, so the failure mode is GitHub's cache
  as today. The costs are real: it covers Node actions only (a Docker action keeps
  GitHub's URL), and Buildx's `type=gha` too unless a pool opts in; it covers only the stock images (an
  image of the operator's own is left alone, as it is for the Docker client); the
  wrapper has to be rebuilt against each runner release's layout; the preload
  patches Node's `http`, `https` and `fetch`, so a cache client that used another
  transport would not be redirected (it would fall back to GitHub's cache); and
  whether a JIT-configured runner can still self-update was not checked. The
  alternatives were weighed and set aside: a patched runner fork (a build and a
  supply-chain burden for two lines), a TLS-intercepting proxy that rewrites the
  calls (a CA every job would trust), and a reverse proxy in the service that
  forwards what it does not implement (it would need the job's real results
  address, which only the runner has, and a wrong guess breaks artifacts).
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

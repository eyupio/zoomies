# A registry mirror on every host (ZF-246)

**Status:** design, and slice 1 built (see "Slice 1, as built" at the end).
Slices 2 to 4 are not started. This is the dated design
[ROADMAP.md](../../ROADMAP.md) section 8 says comes before any code. Slice 1 was
started on the recommended answers to the four decisions below, at the request of
the person running the session; the owner has not separately answered them, and a
different answer to the first changes what slice 1 was.

**Written:** 17 November 2026 against `9af2f87`. It follows
[the Codemany plan](2026-11-17-codemany-follow-ups.md), Task 6, and
[decision 34](../../ROADMAP.md): ZF-246 only, no Go, npm or git mirror. The
sketch it replaces is in
[candidates-from-hosted-services.md](../candidates-from-hosted-services.md)
section 1, layer 2, and [cache-service.md](../cache-service.md) names this work
as "a separate, smaller package".

## The problem

A pool with `docker_mode: dind` gets a private Docker daemon for every runner,
and that daemon's storage starts empty, so every job pulls again the images it
names: the base image of a `Dockerfile`, a service container, `docker run
alpine`. [configuration.md](../../docs/configuration.md) says so under
`agent.dind_image`, and tells an operator who keeps a pull-through cache to
bake its address into a sidecar image of their own. Nothing in Zoomies makes
that easy, and a host that does not do it pulls from Docker Hub every time.

Docker Hub limits anonymous pulls by source address. A fleet whose hosts sit
behind one address shares one budget, and every DinD job spends from it. This
has not been measured on a Zoomies fleet. It was seen on 17 November 2026 from
the sandbox this design was written in, whose address is shared with other
users: a pull received `toomanyrequests` from Docker Hub, and Docker's own
rate-limit preview read `100;w=3600` with a remaining count that moved between
5 and 51 in five consecutive reads (see the spike below). That is enough to
say the limit is real and current. It says nothing about how often a Zoomies
fleet meets it, and this design makes no claim about speed.

## What the spike showed

A short experiment tested the riskiest assumptions first, because the cache
service's design found on 8 November that its first plan did not survive its
first check. On 17 November 2026, in a sandbox with Docker 29.9.2 (classic
image store, `vfs` storage, no iptables), a real `dockerd` was started with
`--registry-mirror=http://192.0.2.2:5001`, a non-loopback address of the
sandbox. Behind that address ran a throwaway forwarder of about a hundred lines
of Python, not kept: it fetched anonymous tokens, forwarded requests to
`registry-1.docker.io`, cached nothing and logged what it saw. Nothing was
committed and the daemon was removed afterwards.

| # | What was seen | What it settles |
| --- | --- | --- |
| 1 | `dockerd` accepted a plain-HTTP mirror on a non-loopback address with no `insecure-registries` entry. `docker info` listed the mirror and only the loopback ranges as insecure, and requests arrived. | The mirror can be plain HTTP on the bridge address, as the cache service is. No certificate is needed. |
| 2 | For `docker pull alpine:3.20` it sent, in order: `GET /v2/` with no `Authorization` header, `HEAD` of the manifest by tag, `GET` of the image index by digest, `GET` of the platform manifest by digest. The `Accept` header on all of them named only the Docker v2 manifest type, and Docker Hub answered the `HEAD` with an OCI image index regardless. It never got as far as a blob. | The mirror must answer `/v2/` with `200` and no authentication challenge, forward `Accept` as it was given, and not assume the media type it asked for is the one it gets. |
| 3 | The forwarder's anonymous token request to `auth.docker.io` returned `200`, one token per repository scope. | Anonymous upstream access works, so the mirror needs no credential. |
| 4 | The platform manifest `GET` was answered `429 toomanyrequests` by Docker Hub. `dockerd` logged "Attempting next endpoint for pull after error", went to Docker Hub itself, got the same `429`, and gave up. | A structured error from the mirror makes `dockerd` fall through to the real registry. |
| 5 | With the forwarder stopped, `dockerd` logged the refused connection to the mirror as "Attempting next endpoint for pull after error" and then pulled `alpine:3.20` directly: "Downloaded newer image". | The bound "when it is down a pull goes direct" is `dockerd`'s own behaviour. Zoomies has to avoid breaking it, not provide it. |
| 6 | The same mirror given both as `--registry-mirror` and as `registry-mirrors` in a `daemon.json` made `dockerd` exit with status 1: "the following directives are specified both as a flag and in the configuration file: registry-mirrors". | A sidecar image that carries its own `daemon.json` mirror would fail to start if Zoomies added the flag. Zoomies must not add it there. |
| 7 | `dockerd` reads proxy settings from its environment (`docker info` printed them, including `No Proxy`). The mirror's address was added to `NO_PROXY` before the pulls above; what happens without it was not tested. | The sidecar is given the agent's proxy settings by `dindEnv` today, so the mirror's address has to be added to its `NO_PROXY`. The first slice proves what happens if it is not. |
| 8 | Docker's rate-limit preview for the sandbox's address read `100;w=3600`, and the remaining count read 51, 12, 5, 11 and 46 across five reads. | The limit exists and is per address. The address was shared, so the counter could not show whether a `HEAD` costs a pull. That is unmeasured. |

## The approach

A read-only pull-through mirror for Docker Hub's public images, run by the
agent on each host, and a `--registry-mirror` flag on the DinD sidecar's
`dockerd` that points at it. A runner's `docker pull` and `docker build`
inside the sidecar then reach Docker Hub once per image digest for the host
instead of once per job, with no change to any workflow.

### The package

A new package, `internal/registrymirror`, standard library only, held by a
purity test written as `internal/cacheservice`'s is. It knows nothing about
the fleet: no store, no config, no controller, no clock of its own, and the
agent decides where and when it runs. It is not part of `cacheservice`, which
is the Actions cache protocol and nothing else.

It serves the read subset of the registry HTTP API and refuses the rest:

* `GET` and `HEAD` of `/v2/`, which answers `200` with no challenge (finding 2).
* `GET` and `HEAD` of `/v2/<name>/manifests/<reference>`.
* `GET` and `HEAD` of `/v2/<name>/blobs/<digest>`.
* Every other method and path answers `405` or `404`. No job can push,
  delete or tag through it.

The repository name must match the registry's own name grammar, and anything
else is refused before an upstream request is made, because a job chooses the
path and the mirror makes the outbound call for it.

### Upstream is anonymous, always

The mirror fetches an anonymous bearer token per repository scope from the
registry's token service (finding 3) and sends no other credential. It does not
use `agent.registry_auth`. That is what makes "never caches a private image" a
property of the mirror rather than a rule it keeps: an anonymous request for a
private repository is refused upstream, so there is nothing to store. A
credential used upstream would let any job that can reach the mirror read
whatever that account could pull. An operator who wants authenticated pulls for
a higher limit is out of scope here.

### Content addressing

A blob and a manifest fetched by digest are immutable. They are stored under
their digest, written to a temporary name, hashed as they are streamed, and
made visible only when the hash matches. A mismatch is deleted and logged. The
client verifies digests itself, so a bad byte streamed before the hash is known
is still caught there, and nothing that failed the hash is ever served from
disk later.

### Tags are the one mutable thing

A tag is a pointer, and it moves. The mirror remembers, for each repository,
tag and `Accept` value, the digest and content type the upstream last gave. It
trusts that for a short time, then asks upstream again with a `HEAD`, renews
the entry if the digest is unchanged and fetches the new manifest if it is not.
The time is a constant in the first release, five minutes, and is not a
setting; it is a number to be settled by measurement, and a setting nobody
needed is a setting that can never be removed.

If upstream cannot be reached when a tag has expired, the mirror answers `502`
and `dockerd` falls back to a direct pull (finding 5). It does not serve an
expired tag. That keeps a fix to an image from being hidden, and it keeps the
mirror from having a second behaviour to test. Serving stale tags for a
bounded time is a real feature with real value when Docker Hub is down, and it
is not part of this one.

### Blobs

Docker Hub redirects a blob request to a content delivery network, as
registries commonly do (not seen in the spike; the first slice verifies it
against the real registry). The mirror follows redirects itself, sends no
`Authorization` header to the second host, streams the body to the client and
to disk together, and fetches each digest once at a time: a second request for
a blob that is being fetched waits for the first rather than starting another.

A blob larger than a quarter of the size bound is streamed through and not
kept, so one huge image cannot evict everything else.

### Storage and its bound

Under the agent's shared folder, in a directory of its own, bounded by
`agent.registry_mirror.max_bytes`. Zero means the default of 10 GiB, the cache
service's default, because an unbounded cache fills the host's disk with
entries nobody asked it to keep and there is no unbounded setting. Eviction is
least recently used, by blob; manifests are small and go when their blobs do.
An object being served is never evicted, and temporary files left by a crash
are swept on start. A containerised agent whose shared folder is not mounted
has nowhere to keep it, `host.shared_folder_unmounted` already says so, and the
mirror is not offered there.

### Where it listens, and who can reach it

One mirror per host, listening on the host's address on each network a DinD
sidecar is attached to and on nothing else, using the same
`NetworkGateway` helper the cache service uses, so a machine on the host's
network cannot reach it. Unlike the cache service it keeps no per-pool or
per-repository identity: what it holds is public and content addressed, so any
job that can reach it can read everything it has, and that is nothing the job
could not pull itself.

One thing a job can learn is that another pool on the host has pulled a given
public image. That is named here and accepted. No private name is ever stored.

### Wiring into the sidecar

When the mirror is on, the sidecar's command gains
`--registry-mirror=http://<gateway>:<port>` and its proxy environment gains
the gateway address in `NO_PROXY` and `no_proxy` (finding 7).

It is added only when the pool uses the default sidecar image. A custom
`agent.dind_image` or pool `dind_image` is where an operator keeps their own
`daemon.json`, and adding the flag beside a file that names a mirror stops the
daemon starting (finding 6). A pool with a custom image is left as it is, and a
finding says the mirror is on and not used for it.

A mirror that is not offered is a sidecar that starts as it does today. Nothing
in this wiring may fail a runner's creation, as nothing in the cache service
may.

### What it does not touch

The host's own Docker daemon is left alone. `registry-mirrors` for a daemon
used through the host socket lives in `/etc/docker/daemon.json`, and
[`internal/hosttune`](../../internal/hosttune/AGENTS.md) says Zoomies never
restarts Docker while work can run and changes `daemon.json` only with consent.
The docs say what to add, and where the address is, for an operator who wants
it. Whether a daemon needs a restart for it, or can reload, was not tested.

## Settings and findings

`agent.registry_mirror.enabled`, `ZOOMIES_AGENT_REGISTRY_MIRROR_ENABLED`,
a boolean, **off by default**, read by the agent as
`agent.docker_build_cache_mb` is, with the same row in
`internal/config/settings.go`, the same field in `config.go`, its range check
in `validate.go` and its rows in `docs/configuration.md`.

`agent.registry_mirror.max_bytes`, `ZOOMIES_AGENT_REGISTRY_MIRROR_MAX_BYTES`,
bytes, zero for the default, otherwise at least 1 GiB.

Findings, each with its row in `docs/problem-codes.md` and its plain-language
line, and `make generate` run rather than any file edited by hand:

* `host.registry_mirror_degraded`, a warning: the mirror could not write its
  folder, could not reach upstream for a sustained time, or its listener
  stopped. Jobs pull directly, which is the safe state; the finding says what
  to look at.
* `pool.registry_mirror_unused`, informational: the mirror is on and this
  pool's custom sidecar image means it is not used for it.
* `agent.registry_mirror.max_bytes`, an error for a value out of range.

The heartbeat carries the mirror's figures the way it carries the cache
service's: requests, hits, bytes served, bytes fetched upstream, upstream
errors and bytes stored, so a host card can show a hit rate and a stopped
mirror is visible. Codes and figures are provisional until slice 3.

## Failure rule

Every failure leaves `dockerd` something it falls back from, and a broken
mirror slows a pull and never fails a job. Two of the ways this can go wrong
were tested against a real `dockerd`, and the rest were not.

* Listener down: refused connection, falls back (finding 5, tested).
* Mirror relays an upstream error such as a `429`: falls back (finding 4, tested).
* Upstream times out: the mirror answers `502` quickly. Its upstream calls have
  a connect timeout, a response-header timeout and an idle-body timeout, so it
  never holds a connection open indefinitely. Not tested against `dockerd`.
* A mirror that accepts a connection and then stalls: not tested, and it is the
  failure `dockerd`'s fallback is least likely to cover. The timeouts above are
  how the mirror avoids it, and slice 4 proves `dockerd` agrees.
* The mirror answers `404` for something upstream has: never. It answers `404`
  only when upstream did. Not tested against `dockerd`.
* The folder is full or unwritable: the mirror serves what it can and streams
  the rest through uncached, and raises the finding. This one is the mirror's
  own behaviour, not `dockerd`'s, and slice 1 tests it.

## What is not proven

Everything below is untested, and the first four are what slice 4 exists for.

1. **Blobs.** The spike never reached one: the request for the redirect, the
   content-delivery host, `Range` requests on a resumed pull, and digest
   verification while streaming.
2. **Buildx builders.** A `docker-container` builder inside a sidecar runs its
   own BuildKit, which may not inherit the daemon's mirrors. Whether the
   mirror reaches `docker buildx build` is unknown.
3. **The containerd image store.** The default sidecar image is `docker:27-dind`
   with the classic store. A newer default image changes how mirrors are read,
   and this design says nothing about it until the default moves.
4. **A stalled mirror, and `404` handling,** as above.
5. **Proxy settings.** What `dockerd` does with the mirror's address when it is
   missing from `NO_PROXY`.
6. **Reachability from the sidecar.** The gateway address is reached from a
   runner that joined the sidecar's network namespace today by the cache
   service. `dockerd` in the sidecar reaching it is expected and not tested.
7. **Whether a `HEAD` request costs a pull.** Finding 8. Docker's own
   documentation was not read for this and should be before the tag
   revalidation is tuned.
8. **Docker Hub's terms for an anonymous pull-through cache at a fleet's
   scale,** which were not checked and are a reason to check before shipping.
9. **Savings.** None measured, and none claimed.

## Slices

Each is its own pull request. The support matrix rule applies: a row moves right
only when a job runs on a real thing, so slices 1 to 3 are "built" and only
slice 4 earns "tested".

1. **The package.** `internal/registrymirror` with the purity test. Tests
   against a fake upstream: write methods refused, a path outside the name
   grammar refused, a private repository (a `401` upstream) never stored, a
   digest mismatch deleted, a tag revalidated and renewed or replaced, a
   redirect to a private or loopback address refused, the size bound evicting
   by least recent use and never an object being served, a blob over a quarter
   of the bound not kept, concurrent requests for one blob making one upstream
   fetch. One test against a real `dockerd` that pulls through it, and skips
   itself without Docker, as the repository's other Docker tests do. Each test
   is seen to fail with the thing it guards removed.
2. **The agent.** The two settings, the listener on the network gateway, the
   sidecar flag and `NO_PROXY` for the default image only, creation never
   failing, and the heartbeat figures. Tests build the sidecar's configuration
   and assert on it, as the existing DinD tests do.
3. **Findings, docs and the host card line.** The codes that are left
   (`agent.registry_mirror.max_bytes` came in slice 2, with the setting it
   checks) and their problem-code rows, a section in
   [persistent-caches.md](../../docs/persistent-caches.md) that sits beside the
   advice to bake a mirror into a sidecar image of your own, which stays right
   for a custom image, and the documented operator step for the host daemon.
4. **A real host.** A second DinD job's pull is served from the mirror; the
   mirror stopped mid-pull falls back; a stalled one is survived; buildx and
   the unproven items above are answered; and the saving is measured, with the
   numbers in [validation/](../validation/). Needs a host with Docker Hub
   reachable and a fleet to run on.

## Decisions for review

Each has a recommendation. None is taken by writing it here.

1. **Write the protocol, or run the upstream registry image as a proxy.**
   Recommended: write it. The read subset is three routes. Everything this
   project cares about sits around the protocol: the size bound, the listener's
   placement, the figures, the findings and the purity rule. The upstream image
   would be a second thing to pin and pull, from Docker Hub, which is the thing
   being avoided; it would be a container the agent has to supervise and keep
   current, and it would report nothing the agent could show. The cost is real: redirects, ranges
   and eviction are the code most likely to be wrong, which is why slice 4 is
   a requirement and not a nicety. The alternative is a smaller first slice
   and a dependency row in [dependencies.md](../../docs/dependencies.md).
2. **Off by default, or on.** Recommended: off. It opens a listener and changes
   a sidecar's command. It follows the cache service, which is off until a pool
   turns it on, and it can become the default after a real fleet has run it.
3. **Stale tags: fall back, or serve stale for a bounded time.** Recommended:
   fall back to a direct pull and serve no expired tag. Serving stale is a
   feature for later, with its own bound and its own header.
4. **The host daemon: documentation only, or a consented `hosttune` item.**
   Recommended: documentation only here. A `docker.registry_mirror` check in
   `hosttune`, which reads and merges `daemon.json` with consent, is a separate
   package that can follow once the mirror has run on a real host, and it
   inherits `hosttune`'s rule against restarting Docker under work.

Settled by evidence, and not decisions: the sidecar flag is not added beside a
custom image (finding 6), the mirror is Docker Hub only because that is all a
daemon's `registry-mirrors` covers, and the mirror never carries a credential.

## Not in this work

* Go, npm and git mirrors: decision 34.
* Authenticated upstream pulls, which would make private content reachable.
* Registries other than Docker Hub: `registry-mirrors` does not cover them.
* The host's own daemon, serving stale tags, and a mirror shared between hosts.
* Any speed claim. Slice 4 measures; this design does not.

## Slice 1, as built

Built on 17 November 2026: `internal/registrymirror`, standard library only, with
53 tests at 90 per cent statement coverage under the race detector, the purity
test, and one opt-in test against a real `dockerd`. It is not wired into the
agent, and nothing imports it yet. Slices 2 to 4 are unchanged.

**What it does.** The read subset of the registry API: the ping, manifests by tag
or digest, blobs by digest, `HEAD` for each, and ranges. Upstream is anonymous.
What it keeps is content addressed, hashed before it becomes visible, bounded
by `MaxBytes`, evicted least recently used and never while being read, and
counted again after a restart. The public surface a later slice uses is
`New(Config)`, `(*Mirror).ServeHTTP` and `(*Mirror).Stats()`, whose figures are
the ones the heartbeat will carry.

**Where it departs from the design above, and why.**

1. A blob of known length is checked and kept before its last chunk is sent to
   the client. The design said the client verifies; this means a corrupt blob is
   never delivered whole, because the client gets a short body and falls back.
   For a blob of unknown length that is not possible and the client's own check
   is the only one.
2. An upstream `401` or `403` is answered `404 NAME_UNKNOWN`, not relayed. The
   design said the mirror answers `404` only when upstream did. Docker Hub
   answers an anonymous caller's request for a private repository and for an
   absent one with the same `401`, so to the mirror they are the same thing, and
   a `401` would send `dockerd` off to authenticate against it.
3. What is remembered about a tag is keyed by what the client accepts, as the
   design said, and is bounded at 10,000 answers, oldest first. The design said
   only that it should be bounded.
4. The redirect guard checks a name and every address it resolves to before the
   request, and requires HTTPS. It does not check at connect time. A proxy makes
   the address connected to the proxy's, and the names come from the registry and
   not from the job. A name that resolves differently the second time is not
   caught, and that is stated in the code.
5. A download that will be kept is finished when the client that asked for it
   goes away, because the next job is owed the blob. One that will not be kept
   (too large) stops with the client.
6. The idle timeout is armed when a transfer starts, not at its first byte.
   The code did that from the start, but when the starting wait was mutated to an
   hour no test noticed, because the stall tests sent half a body first and the
   timer was reset by it. A registry that sends its headers and then nothing now
   has a test.
7. What is kept is held to one admission rule in the store itself: an object over
   a quarter of the bound is not kept, whether it is a blob or a manifest. The
   first version applied it to blobs in the code that streams them and forgot it
   for manifests, so a manifest larger than the bound would have been kept, and
   evicted everything else to make room for it. Code review found it.
8. When the last reader of an object is done the store is brought back within its
   bound at once. The first version evicted only when something was committed, so
   a store whose every object was being read stayed over its bound after the
   readers left, until the next commit. Code review found it.
9. The whole of a token exchange, headers and body, is bounded by the idle
   timeout. The first version bounded the wait for headers only, and a token
   service that sent its headers and then stalled would have held a fetch, and
   every request waiting on it, open for good. Code review found it.
10. The store builds an object's path only from something that is a sha256
    digest, rebuilt from a fixed alphabet, and treats as an object only what is
    named as one in the place it would be put. Every digest it was handed had
    been checked on the way in, so no request could reach a hostile path, and
    CodeQL's six high-severity alerts for this were false positives in effect. But
    tracing them found two real weaknesses in the same code: `path()` panicked on
    an empty or short digest, and a stray file with a short name in the folder
    was counted as an object and then crashed eviction. Both are fixed, and the
    store no longer relies on its callers having checked.
11. What is remembered about tokens is bounded, like what is remembered about
    tags. A token is kept for the repository it was issued for, the repository is
    named by whoever sends the request, and a registry issues a token even for a
    repository that does not exist, so neither the tag cache nor the store limited
    it. The first version had no bound at all and never removed an expired token.
    Codex found it on the second review. Expired tokens are now dropped whenever
    one is added, and the cache is held to 4,096 entries, nearest expiry first.

**What was shown.** A real `dockerd` (Docker 29.9.2, the same throwaway setup as
the spike) pulled a hand-built one-layer image through the mirror from a fake
registry, the image was removed, and the second pull asked the registry for no
further blob: it was served from the mirror's disk. The same test fails if disk
serving is broken. Twenty-seven behaviours were then broken one at a time (serving
from disk, the digest check, the eviction rule, shared fetches, the token not
reaching the content network, ranges, tag lifetime, the size rule, refusing
write methods, anonymous upstream, the `404` rule, the idle timeout, the redirect
guard, startup cleanup, name validation, the tag bound, the manifest check,
the `Retry-After` relay, the path check, what a start counts, the admission rule,
the eviction on release, the token bound, and the token cache's size and expiry), and a test failed each time. The first run left two
alive: the stall before the first byte, which now has a test, and an early return
that was redundant and was removed.

**What it does not show, and is still open.**

* Docker Hub itself. The registry in the test is a fake with a second server for
  the redirect. A real blob redirect to Docker Hub's content network has not been
  exercised, and slice 4 is where it is.
* What `dockerd` does with a mirror that accepts a connection and then stalls, or
  with a `404` from a mirror. The mirror gives up on a stalled upstream within the
  idle timeout, which is tested, but `dockerd`'s own patience is not.
* A token the registry rejects early is not retried: the repository answers
  `404` until the token's own expiry, and `dockerd` falls back. A rotated token
  is not expected, and it is a known limit and not a handled case.
* Failures of the disk itself (a full or unwritable folder): the code stops
  keeping and carries on streaming, and no test fills a disk.
* Buildx builders, the containerd image store, the proxy case and the sidecar's
  reachability, all as listed above, are slice 2 and 4 questions.
* The package compiles and vets for Windows and macOS. Its tests were run on
  Linux only, and `staticcheck` could not be run in the session's environment (it
  failed inside the standard library), so CI's pinned release is its first run.

## Slice 2, as built

Built on 17 November 2026, on top of slice 1: the agent runs the mirror, behind a
setting that is off by default. It adds 30 tests, and nothing in it has run
against a real daemon or a real fleet. Slices 3 and 4 are unchanged, apart from
the one code that moved, below.

**What it does.** `agent.registry_mirror.enabled` and
`agent.registry_mirror.max_bytes` (zero is 10 GiB, otherwise at least 1 GiB)
reach the container backends when they are built, so a change applies when the
agent restarts. With the mirror on, the Docker backend runs one mirror for the
host, under `registry-mirror/` in the shared folder, and listens on the host's
address on each network a sidecar is attached to and on nothing else. A
docker-in-docker sidecar on the default image is started with
`--registry-mirror=http://<gateway>:<port>`, and when it goes through a proxy
the gateway address is added to `NO_PROXY` and `no_proxy`. The agent's heartbeat
carries the mirror's figures under `registry_mirror`. Every reason a mirror
cannot be offered (off, the agent in a container, no shared folder, no gateway,
a folder or listener that fails) is a sidecar that pulls from Docker Hub as it
did before, said once in the log, and never a failed create.

**Where it departs from the design above, and why.**

1. `agent.registry_mirror.max_bytes` and its problem code arrived with the
   setting, in this slice, not in slice 3. The check belongs with the setting, and
   `internal/docs` fails on a code that is raised and not documented. Slice 3 has
   the other two codes.
2. The heartbeat carries one object, not one per pool, because there is one
   mirror for the host. `registrymirror.Stats` gained JSON names (snake case) as
   the contract with a controller that is upgraded separately; a test holds them.
   The controller decodes the block and does nothing with it, so slice 3 stores it
   where it is first read. The block is absent until a sidecar has first asked for
   the mirror, so "the mirror is on and has served nothing" is a fact about the
   setting, not about the figures.
3. "The default sidecar image" is the effective image string being
   `DefaultDinDImage`, whether it came from the host or the pool. An operator who
   names `docker:27-dind` themselves is given the mirror, and any other string
   never is.
4. The proxy exemption is added only when the sidecar's environment has a
   non-empty proxy variable, so a host with no proxy, or a pool that switched it
   off with an empty value, is not given an exemption it does not need. Each
   spelling is written once, replacing what was there, because the daemon keeps
   the last of a repeated name. A host already listed is not listed again, and the
   address carries no port, which matches any.
5. The port is chosen by the system each time a listener starts, as the cache
   service's is, so it changes when the agent restarts. A sidecar started before
   the restart keeps the old address and pulls from Docker Hub, which `dockerd`
   does when a mirror refuses a connection (the spike showed that), but this
   slice does not test it.
6. The mirror is made when the first default-image sidecar asks, so a host with it
   on and no such sidecar has no folder, no listener and no figures.
7. The Settings page shows `max_bytes` as a bare number, because the UI works out
   a size from a key's suffix (`_mb`, `_gb`) and has none for `_bytes`. That is a
   small follow-up in `web/src/lib/settings/settings.ts` and is not done here.
8. `docs/security.md` said the agent never opens a port. The cache service already
   made that untrue, and the mirror would have made it worse, so the page now says
   the agent can run a service for the runners on its own host and adds a row for
   the mirror's folder. Nothing was said about the cache service's listener before,
   and that line now covers both.
9. The Podman backend embeds the Docker one and carries the same field, and never
   offers a mirror, because it does not run docker-in-docker sidecars.

**What was shown.** Thirty tests, run under the race detector: the settings
(default, file, environment and the size rule), the mirror host (offered only when
asked for, one for the host across networks, every way it cannot be offered, the
reason said once, the figures surviving a listener that stopped, closing), the
sidecar built by the pure builder and by a real `Create` against a fake daemon
(the flag, the default image only at both levels, no flag with the mirror off or
unstartable, the proxy cases), and the heartbeat and its wire format. Thirty-eight
behaviours were then broken one at a time and a test failed each time. The first
run left three: one mutation that did not compile, and two real survivors. A
listener that fails on its own was never exercised, because the test closed the
server cleanly, and the operator's size reaching the mirror was only checked for
the default. Each now has a test. `make test-short` passes over all 61 packages,
and `go vet` for Windows and macOS.

**What it does not show, and is still open.**

* A real sidecar reaching the gateway address. A host firewall that drops traffic
  from a container to the host's own address would stop it, and the cache service
  has the same exposure; slice 4 is where this is tried.
* `Create` against a real daemon, and a sidecar's `dockerd` started with the flag.
  The slice 1 test started a daemon with `--registry-mirror` by hand; nothing has
  yet started one through the backend.
* A proxy that is real. The exemption is checked as text in the sidecar's
  environment.
* Whether a pull falls back when the mirror's port has changed under a running
  sidecar, as in departure 5.
* Buildx builders, the containerd image store and Docker Hub itself, as before.
* `staticcheck`, which could not be run in the session's environment, so CI is its
  first run on this slice, as it was for slice 1.

Decision 1, writing the protocol rather than running the upstream registry image,
is still open. If the owner prefers the other, slices 1 and 2 are not wanted.

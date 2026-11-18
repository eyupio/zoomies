## Zoomies 1.4.5

A host can now keep the Docker Hub images its docker-in-docker jobs pull, a pool's package, tool and Actions service caches share one size limit on each host, and a personal GitHub account can be an account-wide target. Hosts say when disk is what stops them placing runners, and the command centre shows a fleet too tall for a wall a page at a time.

### Upgrade notes

* **Two database migrations, applied on first start.** 0144 adds the account type to installations and the repository to runners, for personal accounts. 0145 adds an index the attention list uses. A pre-migration copy of the database is taken as usual.
* **A pool's cache size limit now covers more.** `cache.size_limit` is one limit on each host for the pool's package cache, tool cache and Actions service cache together, where it used to bound the package cache alone. A pool whose tool or service cache is large will see package entries evicted sooner. Zero still leaves packages and tools unlimited, and the Actions service cache then keeps to 10 GiB.
* **Cache limits are enforced by the agent between jobs, not only as a runner starts.** A maintenance pass runs with each reconcile and evicts the oldest entries when nothing is using the cache. It needs a controller and an agent on this release; an agent whose controller sends no cache policy deletes nothing.
* **A cache directory that is itself a symbolic link is not cleaned.** A link above it (a data directory kept on another disk) is followed. The host card says "Cache directory is a symbolic link; point the cache at the folder it leads to" where this applies.
* **A pool on a personal account keeps no warm runners.** `min_runners` must be `0` there, and the pool must be ephemeral. The App needs `administration: write` on the account's repositories.
* **The registry mirror is off by default.** Nothing changes for docker-in-docker pools until `agent.registry_mirror.enabled` is set, and it takes effect at the agent's next restart.
* **New problem codes, none of them required reading to upgrade:** `host.low_disk`, `host.registry_mirror_degraded`, `pool.registry_mirror_unused`, and the Kennel Club check `ci.cache_key_never_matches`.
* **The runner-full image's toolchains moved to their newest releases:** Node 22.23.5 and 24.22.3, Go 1.27.4 and 1.26.11, .NET 8.0.428 and 10.0.404, Maven 3.10.1, Gradle 9.9.1 and Rust 1.100.0. A workflow that pins an exact patch version the image no longer carries will download it.

### Highlights

* **A registry mirror on the host.** With `agent.registry_mirror.enabled`, the agent keeps the public Docker Hub images its docker-in-docker sidecars pull, on the host's own disk, and serves the next job's pull from there. It never holds a credential, so nothing private is kept. It can only slow a pull down, never fail a job: if it cannot start or cannot reach Docker Hub, the sidecar pulls as it does today. `agent.registry_mirror.max_bytes` bounds it (10 GiB by default), least recently used first. A host's card shows its mirror, its figures are exported as metrics, and the Problems panel says when a mirror is broken or when a pool will not use it.
* **One cache budget for a pool on each host.** Package, tool and Actions service storage are measured together and kept within the pool's size limit. Cleanup is coordinated across Docker and Podman on a host that runs both, so a cache one daemon still has mounted is not evicted by the other. A host's card shows what its caches hold and why an over-limit cache has not been cleaned yet.
* **A personal account as an account-wide target.** Choose "A personal account" when connecting GitHub. GitHub has no account-wide runners for one, so Zoomies registers each runner on the repository of the job it is for, and one installation with one pool serves every repository. Zoomies learns what kind of account an installation is from GitHub; nobody types it.
* **Disk that blocks placement is shown.** A host with spare slots that cannot take a runner of a matching pool because of disk reads "Disk blocked" on its card and on the capacity map, can be filtered for on Hosts, and raises `host.low_disk` with what to change.
* **A wall display that pages.** A fleet or a full-screen table too tall for the screen is shown a page at a time, with a pager that says what is on show. An idle host is one line on the command centre. The tour holds while somebody is pointing at or tabbing through the list, and for a minute after a pointer stops moving.
* **A memory kill the daemon missed is still recorded.** A build in a docker-in-docker sidecar runs in a container below the sidecar's, and a kill there need not set Docker's flag. On a host with cgroup v2 the agent reads the kernel's own count, and marks the job only where the container's own limit is what ran out. A job whose kill was reported after it completed is re-run where automatic re-runs are on.
* **The Kennel Club says when a cache key is new on every run,** so a cache that saves and never restores is seen (`ci.cache_key_never_matches`).

### Fixes

* A pull request from a fork whose branch shares a name with yours no longer takes a real failure on your branch off the attention list.
* The attention list stops asking about failures and repairs that nothing can be done about, and settles a failed run whose pull request is closed.
* A cache measurement no longer holds up runner starts on the host, and a file a job deletes during the walk no longer makes the pool's size read as unknown.
* A cache policy changed or withdrawn while a cache was being measured evicts nothing.
* A package cache that reaches tool or service storage through a link is recognised as overlapping it and is not evicted.
* A personal account's pool with no repository to register on no longer leaves its memory requirement on the hosts for the pools decided after it.
* The manifest exchange refuses a redirect instead of following it.
* Each heartbeat after a controller restart is given five seconds, not thirty.
* The re-run warning is raised only for a limit above two.
* Valid Windows cache paths are accepted, and the WSL scheduled tasks run through a launcher that never opens a window.
* The registry mirror checks an uncached blob against its digest, bounds its token cache, and ends a run of upstream failures only on a response that is whole and right.
* In the UI: "Our failures" has its "Failed at" column; the Cards or Rows choice is back where it can be seen; a focused table is given the page and narrow screens agree with each other; the host tooltip is anchored to the host's name; the job drawer writes a queue wait as a duration and describes a re-run as the job-level request it is; a host's cache breakdown opens on a tap; the AI Context maintenance page uses the shared form parts and can ask again after a failed load.

### Documentation

Keeping the images a docker-in-docker job pulls, the registry mirror's design and what a real `dockerd` does with one, a personal account as a target, which memory kills are put on a runner, and a comparison with Codemany for macOS readers.

### Known limits

* **The registry mirror covers public Docker Hub images pulled by the default sidecar image only.** Images on `ghcr.io`, `quay.io` or a private repository, a pool with its own `dind_image`, and pools that use the host's Docker socket do not use it. It has not been tested with Buildx builders.
* **A pool that always keeps an idle runner does not evict package or tool entries,** because a live runner counts as using the cache. The Actions service cache still evicts through its own index.
* **On a host with both Docker and Podman, pruning at a runner's start is left to the maintenance pass,** which needs a cache policy from the controller.
* **The memory-kill rule for nested containers reads `memory.events.local`,** and was tested against a written cgroup tree, not a live kernel. It cannot be read where the Docker daemon is in a virtual machine or on another host.
* **The cache service still does not verify GitHub tokens,** as in 1.4.4. A pool is the boundary.

**Full Changelog**: https://github.com/eyupio/zoomies/compare/v1.4.4...v1.4.5

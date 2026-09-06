# Naming and platforms

Zoomies names pools, hosts and runners with one grammar, and the name says what
the thing is:

```
zoomies-4vcpu-ubuntu-2404              a pool
zoomies-16vcpu-32gb-ubuntu-2404-tuck   the host running it
zoomies-4vcpu-ubuntu-2404-k3f9qz       one of its runners, as GitHub sees it
```

The reason is narrow and practical. `runs-on: [self-hosted, linux-x64]` tells a
workflow author nothing about how much machine they are asking for, and tells
whoever is reading a slow build even less. A name that carries the size and the
platform answers both at a glance, in the one string that ends up in workflow
files, in issue reports, and in GitHub's own runner list -- where Zoomies has no
UI of its own to explain itself.

## The grammar

```
zoomies-<vcpu>vcpu[-<memory>gb]-<os>-<version>[-<arch>][-<suffix>]
```

| Part | Example | Notes |
| --- | --- | --- |
| `zoomies` | | Always. It is how you tell Zoomies' runners and containers from everything else on a host. |
| `<vcpu>vcpu` | `4vcpu` | A pool's per-runner share, or a host's total. A fraction rounds up. |
| `<memory>gb` | `32gb` | Only when there is a memory limit to state. Host names carry it; pool names carry it only when the pool caps memory. |
| `<os>-<version>` | `ubuntu-2404` | The distribution and its release, with the dots removed. `ubuntu-2404`, `debian-12`, `fedora-42`, `rocky-9`. |
| `<arch>` | `arm64` | Omitted for `amd64`, which is the default. Spelled out for everything else. |
| `<suffix>` | `tuck`, `k3f9qz` | A host's machine name, or a runner's uniqueness token. Pools have none: a pool *is* its shape. |

Every part after the prefix is optional and left out when it is not known. A
host that will not say what distribution it runs gets a name without one, rather
than a name that claims something nobody established.

**Any name is still legal.** Nothing rejects `gpu-builders`, and a fleet that
has always called its pool `linux-x64` keeps working exactly as it did. The
grammar is what Zoomies suggests, what `zoomies init` prints, and what an
unnamed host is given -- not a rule it enforces.

The name is capped at 64 characters, because GitHub rejects a runner name longer
than that and a runner that cannot register is one that never starts.

## Platforms

A pool's **platform** is the machine its runners need: a distribution, a
release, and an architecture. It does two things.

**It picks the runner image.** A pool that names `ubuntu` `24.04` boots
`ghcr.io/eyupio/zoomies-runner:ubuntu-2404` without anyone keeping the pool and
the image in step by hand. Naming an image explicitly still wins -- an operator
who has built their own is not second-guessed.

**It restricts placement.** The scheduler will not put an Ubuntu 24.04 runner on
a Debian host, or an arm64 runner on an amd64 one. Before this existed, a pool
said which backend it wanted and nothing about the machine underneath, so the
only thing that noticed a mismatch was the job that failed on it.

Every field is optional, and an empty field is a promise nobody made:

* A pool that names no platform goes anywhere, which is what every pool created
  before platforms existed does.
* A host that has not reported its distribution -- an agent older than this
  feature -- is not ruled out by one. Adding a platform to a pool narrows where
  its runners can go; it never widens it, and it never strands a host.

When no host matches, the Overview says so in those words, and names the machine
to add:

```
cannot scale zoomies-4vcpu-ubuntu-2404-arm64 0 -> 2: no host can take a new
docker runner (1 not Ubuntu 24.04, arm64); add a Ubuntu 24.04, arm64 host, or
change the pool's platform to one you have
```

## The runner image

`ghcr.io/eyupio/zoomies-runner` is published with one tag per operating system,
and the tag is the same `<os>-<version>` that appears in a pool name.

| Tag | Base | Architectures |
| --- | --- | --- |
| `ubuntu-2404` | `ubuntu:24.04` | amd64, arm64 |
| `ubuntu-2204` | `ubuntu:22.04` | amd64, arm64 |
| `debian-12` | `debian:12-slim` | amd64, arm64 |
| `fedora-42` | `fedora:42` | amd64, arm64 |
| `rocky-9` | `rockylinux/rockylinux:9` | amd64, arm64 |

`:latest` points at `ubuntu-2404`, which is what a pool that names no platform
gets. Each release also publishes an immutable `<tag>-<version>` for pinning.

All five are built from the same `deploy/Dockerfile.runner`. It takes the base
image and the package family (`apt` or `dnf`) as build arguments and installs
the tools nearly every workflow assumes exist; actions/runner's own .NET
dependencies are left to the tarball's `installdependencies.sh`, which already
knows every distribution's package names for them. Adding an operating system is
a row in `internal/naming`'s catalogue, a row in the Makefile, and a matrix
entry in each workflow -- and a Go test fails if those three ever disagree.

There is no Alpine variant. actions/runner ships glibc binaries and .NET
dependencies that musl does not satisfy, so an Alpine image would build and then
fail at the first job.

### Baking in a toolchain

The image is deliberately not an "everything a build might need" mega-image: a
smaller image is a faster cold start, which is the whole point of ephemeral
runners. What a job needs beyond the baseline belongs in the job's own
container or in a setup step.

When a whole fleet needs the same extra packages, `EXTRA_PACKAGES` bakes them in
without forking the Dockerfile:

```sh
docker build -f deploy/Dockerfile.runner \
  --build-arg BASE=ubuntu:24.04 --build-arg OS_FAMILY=apt \
  --build-arg OS_ID=ubuntu --build-arg OS_VERSION=24.04 \
  --build-arg EXTRA_PACKAGES="python3 build-essential" \
  -t ghcr.io/acme/our-runner:ubuntu-2404 .
```

Then point a pool at it with `--image`, which overrides whatever its platform
would have selected.

### Building locally

```sh
make image-runner                          # the default variant
make image-runner RUNNER_VARIANT=debian-12 # one other
make images-runner                         # all of them
```

## What a name is built from

**Pools.** `zoomies init` suggests a first pool named for the host it just set
up, dividing the machine among the runners it will hold:

```sh
zoomies pools create --name zoomies-4vcpu-ubuntu-2404 \
  --labels zoomies-4vcpu-ubuntu-2404,linux,x64 --backend docker --max 4 \
  --cpus 4 --os ubuntu --os-version 24.04 --arch amd64
```

The pool advertises its canonical name as a label, plus the kernel and
architecture labels actions/runner advertises anyway. Declaring those two is
what makes the scheduler refuse an x64 job on an arm64 pool.

**Hosts.** An agent that joins without `--name` is named for what it is:
`zoomies-16vcpu-32gb-ubuntu-2404-build01`. The size is what the agent may
actually use, which is the cgroup's share when the agent runs in a container --
so the controller in a two-core container does not claim the host's sixty-four.

**Runners.** A runner's name is its pool's name plus a short random token, which
is what makes a runner in GitHub's own list traceable back to the pool that
created it. A pool an operator named something of their own still produces
`zoomies-`-prefixed runners, because that prefix is how the uninstaller and the
agent's orphan sweep know which registrations and containers are Zoomies' to
reap.

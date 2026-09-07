#!/bin/sh
#
# A fleet's own packages, named by the EXTRA_PACKAGES build argument.
#
# It exists so that a fleet that needs ruby-dev, or a private toolchain, does
# not have to fork deploy/Dockerfile.runner to get it:
#
#   docker build -f deploy/Dockerfile.runner --build-arg EXTRA_PACKAGES="ruby-dev golang" .
#
# It is a layer of its own, after the toolchain rather than folded into it, for
# two reasons. Changing the list then costs one package install rather than
# reinstalling the compiler, the headers and node; and the build argument
# reaching this script is something CI can prove cheaply, which is what stops a
# documented option quietly ceasing to work.
#
# The package manager's own exit status is the check that a name was real:
# apt-get and dnf both fail on a package that does not exist, and failing here
# is much better than shipping an image that is missing what the fleet asked
# for and finding out in somebody's workflow.
#
#   usage: runner-extra.sh <apt|dnf> [packages...]
#
set -eu

family="${1:?usage: runner-extra.sh <apt|dnf> [packages...]}"
shift || true

# No packages is the overwhelmingly common case -- every published variant --
# so it costs an empty layer and nothing else.
[ "$#" -gt 0 ] || exit 0

case "${family}" in
  apt)
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y --no-install-recommends "$@"
    rm -rf /var/lib/apt/lists/*
    ;;
  dnf)
    dnf install -y --allowerasing --setopt=install_weak_deps=False "$@"
    dnf clean all
    rm -rf /var/cache/dnf
    ;;
  *)
    echo "runner-extra.sh: unknown package family '${family}'; expected apt or dnf" >&2
    exit 64
    ;;
esac

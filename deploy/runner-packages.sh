#!/bin/sh
#
# The runner image's baseline: the tools essentially every workflow assumes
# exist, and a UTF-8 locale.
#
# It exists so deploy/Dockerfile.runner has one instruction instead of a chain
# of shell conditionals, and so each package family's names live somewhere a
# person can read them side by side. actions/runner's own .NET dependencies are
# NOT here: the runner tarball's ./bin/installdependencies.sh installs those,
# and it already knows every distribution's names for them. Listing them here by
# hand -- libicu74, liblttng-ust1t64, libssl3t64 -- is what made this image
# Ubuntu 24.04 and nothing else.
#
#   usage: runner-packages.sh <apt|dnf>
#
set -eu

family="${1:?usage: runner-packages.sh <apt|dnf>}"

case "${family}" in
  apt)
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y --no-install-recommends \
      ca-certificates curl git jq unzip zip tar gzip xz-utils \
      sudo gosu tzdata locales openssh-client rsync
    rm -rf /var/lib/apt/lists/*
    # A runner without a UTF-8 locale mangles any non-ASCII line a job prints,
    # which is a confusing thing to debug from a workflow log.
    localedef -i en_US -c -f UTF-8 -A /usr/share/locale/locale.alias en_US.UTF-8
    ;;
  dnf)
    # gosu has no RPM: shadow-utils' runuser does the same job, and the
    # entrypoint uses neither -- it is here only for a workflow that expects it.
    dnf install -y --allowerasing --setopt=install_weak_deps=False \
      ca-certificates curl git jq unzip zip tar gzip xz \
      sudo shadow-utils tzdata glibc-langpack-en openssh-clients rsync
    dnf clean all
    rm -rf /var/cache/dnf
    ;;
  *)
    echo "runner-packages.sh: unknown package family '${family}'; expected apt or dnf" >&2
    exit 64
    ;;
esac

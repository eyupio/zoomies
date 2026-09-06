#!/bin/sh
#
# Install the runner image's baseline tools for one package-manager family.
#
# It exists so deploy/Dockerfile.runner has one RUN instruction instead of a
# chain of shell conditionals, and so the package names for each family live
# somewhere a person can read them. It installs only what nearly every workflow
# assumes exists; actions/runner's own .NET dependencies are installed by the
# runner tarball's ./bin/installdependencies.sh, which already knows every
# distribution's names for them.
#
#   usage: runner-packages.sh <apt|dnf> [extra packages...]
#
set -eu

family="${1:?usage: runner-packages.sh <apt|dnf> [extra packages...]}"
shift || true
extra="$*"

case "${family}" in
  apt)
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    # shellcheck disable=SC2086  # extra is a deliberately word-split package list
    apt-get install -y --no-install-recommends \
      ca-certificates curl git jq unzip zip tar gzip xz-utils \
      sudo tzdata locales openssh-client rsync ${extra}
    rm -rf /var/lib/apt/lists/*
    # A runner without a UTF-8 locale mangles any log line a job prints that is
    # not ASCII, which is a confusing thing to debug from a workflow.
    localedef -i en_US -c -f UTF-8 -A /usr/share/locale/locale.alias en_US.UTF-8
    ;;
  dnf)
    pkg="dnf"
    command -v dnf >/dev/null 2>&1 || pkg="microdnf"
    # --allowerasing because the RHEL 9 family's base images ship curl-minimal,
    # which conflicts with the full curl these images need: without it dnf
    # refuses the whole transaction rather than swapping the two.
    # shellcheck disable=SC2086
    "${pkg}" install -y --allowerasing \
      ca-certificates curl git jq unzip zip tar gzip xz \
      sudo shadow-utils glibc-langpack-en openssh-clients rsync ${extra}
    "${pkg}" clean all
    rm -rf /var/cache/dnf
    ;;
  *)
    echo "runner-packages.sh: unknown package family '${family}'; expected apt or dnf" >&2
    exit 64
    ;;
esac

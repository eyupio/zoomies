#!/bin/sh
#
# Install packages with apt, and survive a mirror that is catching up.
#
# ports.ubuntu.com and deb.debian.org are each several machines behind one
# name, and they do not all carry the same pool at the same instant. So a
# package the index has just started listing can 404 on whichever machine this
# build happens to reach, and the whole image build dies with "Unable to fetch
# some archives" naming a file that is plainly there a minute later. The
# ubuntu-2404 arm64 variant lost a main build to exactly that, over a polkitd
# security update it never asked for by name.
#
# Fetching the index again is what recovers: it is another draw from the round
# robin, and it replaces an index the archive has already moved past. Retrying
# the download alone against the same index would fail the same way every time,
# which is why Acquire::Retries -- which covers a transfer that drops rather
# than a file a mirror does not have -- is not enough on its own.
#
# It is deliberately apt-only. dnf reads a metalink, knows about several
# mirrors at once and fails over between them itself, so the RPM variants have
# never needed this.
#
# A package name that is simply wrong still fails the build, which is what
# deploy/runner-extra.sh relies on to catch a typo in EXTRA_PACKAGES. It just
# takes the attempts below to get there.
#
# A name written ?name is optional: installed where this release carries it,
# and skipped, with a line saying so, where it does not. It is for the few
# packages a distribution has dropped -- Debian 13 has no
# software-properties-common -- and never for a typo, which is why a plain
# name stays required.
#
#   usage: runner-apt.sh <packages...>   (a package written ?name is optional)
#
set -eu

[ "$#" -gt 0 ] || { echo "runner-apt.sh: no packages named" >&2; exit 64; }

export DEBIAN_FRONTEND=noninteractive

# wanted is the argument list with each optional package kept only where the
# index just fetched lists it.
wanted() {
  for pkg in "$@"; do
    case "${pkg}" in
      \?*)
        name="${pkg#\?}"
        if apt-cache show "${name}" >/dev/null 2>&1; then
          printf '%s\n' "${name}"
        else
          echo "runner-apt.sh: ${name} is not in this release; skipping it" >&2
        fi
        ;;
      *) printf '%s\n' "${pkg}" ;;
    esac
  done
}

attempts=3
attempt=1
while :; do
  # shellcheck disable=SC2046 # one package name per line, none with spaces
  if apt-get -o Acquire::Retries=3 update \
    && apt-get -o Acquire::Retries=3 install -y --no-install-recommends $(wanted "$@"); then
    break
  fi
  if [ "${attempt}" -ge "${attempts}" ]; then
    echo "runner-apt.sh: apt-get failed ${attempts} times; giving up" >&2
    exit 1
  fi
  # Long enough for a mirror to have finished a sync it was in the middle of,
  # short enough that a real failure -- a package that does not exist -- is not
  # a minute of waiting before the build says so.
  delay=$((attempt * 10))
  echo "runner-apt.sh: apt-get failed; refetching the index in ${delay}s (attempt $((attempt + 1)) of ${attempts})" >&2
  sleep "${delay}"
  attempt=$((attempt + 1))
done

# The package lists are several tens of megabytes that no job reads, and a job
# that runs apt-get itself starts with an update anyway.
rm -rf /var/lib/apt/lists/*

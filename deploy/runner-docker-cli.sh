#!/bin/sh
#
# The Docker CLI, buildx and compose, for the runner-docker variant.
#
# Only the client: the daemon is the pool's business, and this image never
# starts one. From Docker's own repository rather than the distribution's
# docker.io / podman-docker package, because those drag in a daemon, containerd
# and runc -- a few hundred megabytes of software this image must never run.
#
#   usage: runner-docker-cli.sh <apt|dnf> <os-id>
#
# os-id picks the repository path: Docker publishes a separate one per
# distribution, and pointing Debian at Ubuntu's gives a package that will not
# install.
set -eu

family="${1:?usage: runner-docker-cli.sh <apt|dnf> <os-id>}"
os_id="${2:?usage: runner-docker-cli.sh <apt|dnf> <os-id>}"

# Docker publishes for these directly; everything else in a family borrows the
# one it is built from, which is what "rocky uses the centos repository" means.
case "${os_id}" in
  ubuntu|debian|fedora|centos|rhel) repo_os="${os_id}" ;;
  rocky|almalinux) repo_os="centos" ;;
  *)
    echo "runner-docker-cli.sh: no Docker repository is published for '${os_id}'" >&2
    exit 64
    ;;
esac

case "${family}" in
  apt)
    export DEBIAN_FRONTEND=noninteractive
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL "https://download.docker.com/linux/${repo_os}/gpg" -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
    arch="$(dpkg --print-architecture)"
    # shellcheck disable=SC1091  # /etc/os-release exists in the image, not here
    codename="$(. /etc/os-release && echo "${VERSION_CODENAME}")"
    printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/%s %s stable\n' \
      "$arch" "$repo_os" "$codename" > /etc/apt/sources.list.d/docker.list
    apt-get update
    apt-get install -y --no-install-recommends \
      docker-ce-cli docker-buildx-plugin docker-compose-plugin
    rm -rf /var/lib/apt/lists/*
    ;;
  dnf)
    dnf install -y 'dnf-command(config-manager)'
    repo="https://download.docker.com/linux/${repo_os}/docker-ce.repo"
    dnf config-manager addrepo --from-repofile="${repo}" \
      || dnf config-manager --add-repo "${repo}"
    dnf install -y --setopt=install_weak_deps=False \
      docker-ce-cli docker-buildx-plugin docker-compose-plugin
    dnf clean all
    rm -rf /var/cache/dnf
    ;;
  *)
    echo "runner-docker-cli.sh: unknown package family '${family}'; expected apt or dnf" >&2
    exit 64
    ;;
esac

#!/bin/sh
#
# The GitHub CLI, from GitHub's own repository because no distribution here
# carries it.
#
# A workflow that shells out to `gh` is doing something GitHub-hosted runners
# let it do, and the alternative is every such workflow installing the same tool
# itself on every run.
#
#   usage: runner-gh.sh <apt|dnf>
#
set -eu

family="${1:?usage: runner-gh.sh <apt|dnf>}"

case "${family}" in
  apt)
    export DEBIAN_FRONTEND=noninteractive
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
      -o /etc/apt/keyrings/githubcli.gpg
    chmod a+r /etc/apt/keyrings/githubcli.gpg
    arch="$(dpkg --print-architecture)"
    printf 'deb [arch=%s signed-by=/etc/apt/keyrings/githubcli.gpg] https://cli.github.com/packages stable main\n' \
      "$arch" > /etc/apt/sources.list.d/github-cli.list
    apt-get update
    apt-get install -y --no-install-recommends gh
    rm -rf /var/lib/apt/lists/*
    ;;
  dnf)
    dnf install -y 'dnf-command(config-manager)'
    repo="https://cli.github.com/packages/rpm/gh-cli.repo"
    # dnf5 (Fedora) spells this "addrepo --from-repofile"; dnf4 (the RHEL
    # rebuilds) spells it "--add-repo". Asking which one this dnf understands
    # keeps a usage error that reads like a failure out of the build log.
    if dnf config-manager --help 2>/dev/null | grep -q -- '--add-repo'; then
      dnf config-manager --add-repo "${repo}"
    else
      dnf config-manager addrepo --from-repofile="${repo}"
    fi
    dnf install -y --setopt=install_weak_deps=False gh
    dnf clean all
    rm -rf /var/cache/dnf
    ;;
  *)
    echo "runner-gh.sh: unknown package family '${family}'; expected apt or dnf" >&2
    exit 64
    ;;
esac

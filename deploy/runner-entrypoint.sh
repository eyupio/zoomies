#!/usr/bin/env bash
#
# Zoomies runner entrypoint.
#
# This is the other half of the contract in internal/backend: whatever the
# backend sets here is what the runner uses. Keep the two in step.
#
#   ZOOMIES_JITCONFIG      base64 JIT configuration (preferred; ephemeral)
#   ZOOMIES_RUNNER_URL     org or repo URL          (registration-token path)
#   ZOOMIES_RUNNER_TOKEN   registration token       (registration-token path)
#   ZOOMIES_RUNNER_NAME    runner name
#   ZOOMIES_RUNNER_LABELS  comma-separated labels
#   ZOOMIES_RUNNER_GROUP   runner group name
#   ZOOMIES_EPHEMERAL      "true" to pass --ephemeral
#   ZOOMIES_RUNNER_NO_DEFAULT_LABELS  "true" to pass --no-default-labels
#
# The image also bakes in what platform it is, which the startup line prints:
#
#   ZOOMIES_RUNNER_OS          the distribution this image was built from
#   ZOOMIES_RUNNER_OS_VERSION  its release
#   ZOOMIES_RUNNER_VERSION     the actions/runner release it carries
#
# A pool with a docker_mode also gets DOCKER_HOST, pointing at a
# docker-in-docker sidecar or at the host's mounted socket:
#
#   ZOOMIES_DOCKER_WAIT    seconds to wait for that daemon (default 120)
#
# A host with agent.extra_ca_file mounts that PEM bundle read-only and names it:
#
#   ZOOMIES_EXTRA_CA_FILE  a CA to trust as well as the image's own roots
#
set -euo pipefail

# The credentials leave the environment here, before anything else runs.
#
# run.sh forks every step of every workflow, as the same user as this script, and
# a step can read any ancestor's environment from /proc/<pid>/environ. Unsetting a
# variable changes the shell's own table and not that file: the kernel keeps
# serving the environment the process was started with, and this script is the
# container's first process. A step that read it got the organisation's runner
# registration token, good for an hour and enough to register a self-hosted runner
# of its own, which GitHub then hands other repositories' jobs and their secrets.
# The workflow is the untrusted party here: that is the whole reason these runners
# are ephemeral.
#
# So the script starts itself again without them. The first time through it holds
# the values in a here-document, which bash makes a pipe or an unlinked file and
# which no other process can open, passes that on as file descriptor 9, and
# replaces itself with an environment that never had them. The second time through
# reads them from the descriptor, once, and closes it before anything is started.
# Whether descriptor 9 is open is what tells the two apart, so no variable an
# operator could set decides it. The values are single lines (a base64
# configuration, a token, an address).
jitconfig="" runner_token="" runner_url=""
if { : <&9; } 2>/dev/null; then
  IFS= read -r jitconfig <&9 || true
  IFS= read -r runner_token <&9 || true
  IFS= read -r runner_url <&9 || true
  exec 9<&-
elif [ -n "${ZOOMIES_JITCONFIG:-}${ZOOMIES_RUNNER_TOKEN:-}${ZOOMIES_RUNNER_URL:-}${ACTIONS_RUNNER_INPUT_JITCONFIG:-}" ]; then
  exec 9<<CREDENTIALS
${ZOOMIES_JITCONFIG:-}
${ZOOMIES_RUNNER_TOKEN:-}
${ZOOMIES_RUNNER_URL:-}
CREDENTIALS
  exec env -u ZOOMIES_JITCONFIG -u ACTIONS_RUNNER_INPUT_JITCONFIG -u ZOOMIES_RUNNER_TOKEN -u ZOOMIES_RUNNER_URL \
    "$BASH" "$0" "$@"
fi

# The kept cache changes the lookup directory, not the tools the image carries.
# Link baked versions into this runner's disposable view before setup actions
# inspect it. Nothing writes to the shared cache or copies a toolchain per job.
if [ "${AGENT_TOOLSDIRECTORY:-}" = /opt/zoomies-tools ]; then
  for marker in /opt/hostedtoolcache/*/*/*.complete; do
    [ -f "$marker" ] || continue
    source=${marker%.complete}
    [ -d "$source" ] || continue
    relative=${source#/opt/hostedtoolcache/}
    target="$AGENT_TOOLSDIRECTORY/$relative"
    mkdir -p "$(dirname "$target")"
    if [ ! -e "$target" ] && [ ! -L "$target" ]; then
      ln -s "$source" "$target"
      touch "$target.complete"
    fi
  done
fi

cd /home/runner

log() { printf '%s zoomies-runner: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

# Say what this image is before doing anything else. A pool pointed at the
# wrong variant is otherwise only discovered when a job fails on a missing
# package, several minutes and one confusing log later.
log "image: ${ZOOMIES_RUNNER_OS:-unknown} ${ZOOMIES_RUNNER_OS_VERSION:-} on $(uname -m), actions/runner ${ZOOMIES_RUNNER_VERSION:-unknown}"

# Behind a proxy that re-signs TLS, nothing in a job reaches GitHub, a registry
# or a package mirror until the proxy's CA is trusted -- and the failure is a
# certificate error in somebody's third step, not here. So it is trusted before
# the listener starts: in the system store, which git, curl, OpenSSL and the
# package managers read, and through NODE_EXTRA_CA_CERTS, because the runner
# and every JavaScript action are Node, which carries its own roots.
#
# The store is root's to change. The stock image gives the runner passwordless
# sudo; a custom image without it, or without the store's tool, still gets the
# Node half and a warning that says what the rest will not trust.
apt_anchors=/usr/local/share/ca-certificates
apt_bundle=/etc/ssl/certs/ca-certificates.crt
dnf_anchors=/etc/pki/ca-trust/source/anchors
dnf_bundle=/etc/pki/tls/certs/ca-bundle.crt

trust_extra_ca() {
  local ca=$1
  if [ ! -r "$ca" ]; then
    log "ZOOMIES_EXTRA_CA_FILE names $ca, which this runner cannot read."
    log "check agent.extra_ca_file on this host: the file must exist and be readable by the container engine."
    return 78
  fi
  export NODE_EXTRA_CA_CERTS="$ca"

  local as_root=()
  if [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
      as_root=(sudo -n)
    else
      log "warning: this image gives the runner no way to become root, so the extra CA is trusted by Node only;"
      log "git, curl and package managers in jobs will not trust it. Add it to your custom image's trust store instead."
      return 0
    fi
  fi

  local bundle
  if command -v update-ca-certificates >/dev/null 2>&1; then
    "${as_root[@]}" cp "$ca" "$apt_anchors/zoomies-extra-ca.crt" &&
      "${as_root[@]}" update-ca-certificates >/dev/null 2>&1 &&
      bundle=$apt_bundle
  elif command -v update-ca-trust >/dev/null 2>&1; then
    "${as_root[@]}" cp "$ca" "$dnf_anchors/zoomies-extra-ca.pem" &&
      "${as_root[@]}" update-ca-trust extract >/dev/null 2>&1 &&
      bundle=$dnf_bundle
  else
    log "warning: this image has neither update-ca-certificates nor update-ca-trust, so the extra CA is trusted by Node only."
    return 0
  fi
  if [ -z "${bundle:-}" ]; then
    log "warning: adding the extra CA to the system trust store failed, so it is trusted by Node only."
    return 0
  fi
  # Python's requests carries its own roots and ignores the system store; this
  # points it, and anything else that honours SSL_CERT_FILE, at the store the
  # CA was just added to. Both are left alone if a pool already set them.
  export REQUESTS_CA_BUNDLE="${REQUESTS_CA_BUNDLE:-$bundle}"
  export SSL_CERT_FILE="${SSL_CERT_FILE:-$bundle}"
  log "trusting the extra CA from $ca"
}

if [ -n "${ZOOMIES_EXTRA_CA_FILE:-}" ]; then
  trust_extra_ca "$ZOOMIES_EXTRA_CA_FILE"
fi

# The runner treats SIGINT as "finish the current job, then exit", which is
# exactly what a Zoomies drain means. Forward it rather than letting the shell
# die and orphan the runner.
child=0
# shellcheck disable=SC2317  # invoked by trap, which shellcheck cannot see
forward() {
  if [ "$child" -ne 0 ]; then
    log "forwarding $1 to the runner (it will finish its current job first)"
    kill -"$1" "$child" 2>/dev/null || true
  fi
}
trap 'forward INT'  INT
trap 'forward TERM' TERM

# A pool whose docker_mode is dind or host-socket hands the runner a daemon and
# nothing else. If the image has no client, every step that shells out to docker
# dies with "Unable to locate executable file: docker" -- an error that names the
# missing binary and not the reason, halfway through somebody's workflow. Say the
# reason here instead, in the log the operator already downloads.
#
# It is a warning, not an exit. The controller swaps the stock image for its
# Docker variant only under a moving tag, so a pool pinned to a release of the
# stock image and given a docker_mode arrives here on every start; refusing it
# would stop every job on that pool, including the ones that never touch
# Docker, which is the outcome that swap was designed not to cause.
if [ -n "${DOCKER_HOST:-}" ] || [ -S /var/run/docker.sock ]; then
  if ! command -v docker >/dev/null 2>&1; then
    log "warning: this pool provides a docker daemon, but this image has no docker client,"
    log "so jobs that run docker, buildx or compose will fail on it."
    log "set the pool image to a Docker-capable runner image, or install docker-ce-cli in your custom image."
  fi
fi

# The backend now waits for DinD's daemon health before creating this runner.
# Keep this check too: a host socket, an older agent, or a daemon that becomes
# unavailable between provisioning and registration still needs a readiness gate. Absorb the daemon's
# boot here, before GitHub can hand this runner a job, rather than letting a
# workflow's first docker step race it and fail with "Cannot connect to the
# Docker daemon".
#
# A pool that provides Docker must not accept a job until it can use it.
# Bound both the overall wait and each probe: a hung client used to make the
# nominal wait unbounded.
#
# The default is generous on purpose. dockerd in a fresh sidecar has to set up
# its storage driver and iptables before it listens, and on a host that is
# extracting images for the runners queued behind this one that can take well
# over thirty seconds -- which was the old limit, and turned a slow host into a
# host whose every runner failed before registering. The exit codes are
# sysexits.h values the agent translates for the Runners page: 78 for a
# configuration this script refuses, 69 for a daemon that never answered.
wait_for_docker() {
  local limit=${ZOOMIES_DOCKER_WAIT:-120}
  if ! [[ "$limit" =~ ^[0-9]{1,4}$ ]] || [ "$limit" -eq 0 ] || [ "$limit" -gt 3600 ]; then
    log "ZOOMIES_DOCKER_WAIT must be a whole number of seconds from 1 to 3600."
    return 78
  fi
  limit=$((10#$limit))
  if ! command -v timeout >/dev/null 2>&1; then
    log "this Docker-capable runner image needs the coreutils timeout command."
    return 78
  fi
  local started=$SECONDS
  local deadline=$((SECONDS + limit))
  local remaining probe
  while [ "$SECONDS" -lt "$deadline" ]; do
    remaining=$((deadline - SECONDS))
    probe=$remaining
    [ "$probe" -gt 5 ] && probe=5
    if timeout --signal=KILL "${probe}s" docker version >/dev/null 2>&1; then
      log "the docker daemon is ready after $((SECONDS - started))s"
      return 0
    fi
    [ "$SECONDS" -lt "$deadline" ] && sleep 1
  done
  log "the required docker daemon did not become ready within ${limit}s; this runner will not accept a job."
  log "on a slow host, raise runners.docker_wait on the controller's Settings page to allow it longer."
  return 69
}

if [ -n "${DOCKER_HOST:-}" ] || [ -S /var/run/docker.sock ]; then
  if command -v docker >/dev/null 2>&1; then
    wait_for_docker
  fi
fi

# The credentials were read at the top of this script, into shell variables that
# were never exported, so what starts below inherits none of them: not through its
# environment, and not through the entrypoint's own, which was started without
# them. config.sh and the cleanup trap below read the variables. The unset is for a
# run that came by another way and still has them set.
unset ZOOMIES_JITCONFIG ACTIONS_RUNNER_INPUT_JITCONFIG ZOOMIES_RUNNER_TOKEN ZOOMIES_RUNNER_URL

# JobServerQueue traces the same secret-masked lines it sends to GitHub's live
# console, before the console's 1024-character truncation. Keep those lines and
# discard other verbose traces before they can overwhelm the log relay. The
# version marker lets the UI distinguish an older image from a quiet build.
#
# The log is only live if each line leaves the filter as it arrives, which takes
# an awk that reads a pipe a line at a time. gawk does, and both families of
# image carry it for that reason (deploy/runner-packages.sh). mawk does not: it
# waits for a large block of input before it looks at any (256 KiB in the
# release Ubuntu 24.04 ships), however often the program flushes, so a build that
# printed little showed an empty log until it ended. Its -W interactive mode is
# no way round that: in that same release it drops lines after one over 4 KiB.
filter_runner_output() {
  awk '
    /^\[(WORKER|RUNNER|LISTENER) [^]]+\] / {
      build = ($0 ~ /^\[WORKER [^]]+ VERB JobServerQueue\] Enqueue web console line queue: /)
      verbose = ($0 ~ /^\[[^]]+ VERB /)
      if (build) sub(/^\[WORKER [^]]+ VERB JobServerQueue\] Enqueue web console line queue: /, "")
    }
    build { print "ZOOMIES_BUILD_LOG " $0; fflush(); next }
    !verbose { print; fflush() }
  '
}

start_runner() {
  printf 'ZOOMIES_BUILD_LOG_V1\n'
  exec 3> >(filter_runner_output)
  output_filter=$!
  GITHUB_ACTIONS_RUNNER_TRACE=1 \
    ACTIONS_RUNNER_PRINT_LOG_TO_STDOUT=1 \
    ACTIONS_RUNNER_DISABLE_STDOUT_MULTILINE_LOG_PREFIXING=1 \
    ./run.sh "$@" >&3 2>&1 3>&- &
  child=$!
  exec 3>&-
}

if [ -n "${jitconfig}" ]; then
  log "starting with a just-in-time configuration (ephemeral, single use)"
  start_runner --jitconfig "${jitconfig}"
elif [ -n "${runner_token}" ]; then
  : "${runner_url:?ZOOMIES_RUNNER_URL is required alongside ZOOMIES_RUNNER_TOKEN}"
  log "registering ${ZOOMIES_RUNNER_NAME:-unnamed} against ${runner_url}"

  args=(
    --unattended
    --replace
    --url "${runner_url}"
    --token "${runner_token}"
    --name "${ZOOMIES_RUNNER_NAME:-$(hostname)}"
    --work /home/runner/_work
  )
  [ -n "${ZOOMIES_RUNNER_LABELS:-}" ] && args+=(--labels "${ZOOMIES_RUNNER_LABELS}")
  [ -n "${ZOOMIES_RUNNER_GROUP:-}" ]  && args+=(--runnergroup "${ZOOMIES_RUNNER_GROUP}")
  [ "${ZOOMIES_EPHEMERAL:-false}" = "true" ] && args+=(--ephemeral)
  [ "${ZOOMIES_RUNNER_NO_DEFAULT_LABELS:-false}" = "true" ] && args+=(--no-default-labels)

  ./config.sh "${args[@]}"

  # Best effort, and only that: a registration token expires an hour after it
  # was minted, so this succeeds for a runner that lived less than an hour and
  # quietly fails for one that lived longer. What actually keeps the GitHub
  # runner list clean is the controller, which deletes a persistent runner's
  # registration by name when it removes the runner, and whose reaper deletes
  # any offline registration of a runner it knows to be gone. Ephemeral
  # runners deregister themselves.
  # shellcheck disable=SC2317  # invoked by trap, which shellcheck cannot see
  cleanup() {
    log "removing this runner's registration, if the token is still good"
    ./config.sh remove --token "${runner_token}" >/dev/null 2>&1 || true
  }
  trap cleanup EXIT

  start_runner
else
  log "no credentials supplied."
  log "set ZOOMIES_JITCONFIG, or ZOOMIES_RUNNER_URL together with ZOOMIES_RUNNER_TOKEN."
  exit 64
fi

# wait returns early when a trap fires, so loop until the child is really gone.
#
# Two things the obvious `while ! wait "$child"; do :; done` gets wrong: bash
# answers a second wait on a pid it has already reaped with 127, so a runner
# that exits non-zero would spin here for ever; and the loop's own status is
# what `$?` reports afterwards, so the exit code was always 0. The controller
# reads that code to tell "finished" from "failed", which is the whole point of
# passing it on.
status=0
while :; do
  if wait "$child"; then
    status=0
    break
  else
    status=$?
  fi
  # Above 128 is either a trap interrupting wait or the child dying of a
  # signal; only keep waiting if the child is in fact still there.
  if [ "$status" -le 128 ] || ! kill -0 "$child" 2>/dev/null; then
    break
  fi
done
# Drain the console pipe before PID 1 exits and the container stops its reader.
#
# The filter ends once nothing holds the pipe's write end, and a step can leave
# something that does: a daemon started under sudo outlives the runner, which
# cannot sweep it up because sudo resets the environment its tracking id lives
# in. Waiting for the filter outright then held PID 1 until that daemon went, and
# the status below was never reported. So the filter gets a few seconds, which
# is long for what a pipe has left in it, and the status is reported either way.
drain_by=$((SECONDS + 3))
while kill -0 "$output_filter" 2>/dev/null && [ "$SECONDS" -lt "$drain_by" ]; do
  sleep 0.1 || :
done
log "runner exited with status ${status}"
exit "$status"

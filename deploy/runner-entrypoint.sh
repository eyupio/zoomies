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
#   ZOOMIES_DOCKER_WAIT    seconds to wait for that daemon (default 30)
#
set -euo pipefail

cd /home/runner

log() { printf '%s zoomies-runner: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

# Say what this image is before doing anything else. A pool pointed at the
# wrong variant is otherwise only discovered when a job fails on a missing
# package, several minutes and one confusing log later.
log "image: ${ZOOMIES_RUNNER_OS:-unknown} ${ZOOMIES_RUNNER_OS_VERSION:-} on $(uname -m), actions/runner ${ZOOMIES_RUNNER_VERSION:-unknown}"

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
if [ -n "${DOCKER_HOST:-}" ] || [ -S /var/run/docker.sock ]; then
  if ! command -v docker >/dev/null 2>&1; then
    log "this pool provides a docker daemon, but this image has no docker client,"
    log "so jobs that run docker, buildx or compose will fail on it."
    log "a current controller switches a pool on the stock runner image to"
    log "ghcr.io/eyupio/zoomies-runner-docker when it asks for a daemon; if this is"
    log "the stock image, upgrade the controller or set that image on the pool."
    log "an image of your own needs docker-ce-cli installed in it."
  fi
fi

# The other half of that contract: the backend waits for the sidecar *container*
# to be running, and leaves waiting for dockerd inside it to this script, since
# only the image knows when its first docker command runs. Absorb the daemon's
# boot here, before GitHub can hand this runner a job, rather than letting a
# workflow's first docker step race it and fail with "Cannot connect to the
# Docker daemon".
#
# A daemon that never answers is not fatal. The runner still takes jobs that do
# not touch Docker, and one that does gets the client's own error, which says
# more than anything this script could invent.
wait_for_docker() {
  local waited=0
  local limit=${ZOOMIES_DOCKER_WAIT:-30}
  while [ "$waited" -lt "$limit" ]; do
    if docker version >/dev/null 2>&1; then
      [ "$waited" -gt 0 ] && log "the docker daemon answered after ${waited}s"
      return 0
    fi
    sleep 1
    waited=$((waited + 1))
  done
  log "warning: no docker daemon answered at ${DOCKER_HOST:-/var/run/docker.sock} within ${limit}s."
  log "jobs that run docker will fail until it comes up."
}

if [ -n "${DOCKER_HOST:-}" ] || [ -S /var/run/docker.sock ]; then
  if command -v docker >/dev/null 2>&1; then
    wait_for_docker
  fi
fi

# The credentials stop here. They are read into shell variables and the
# exported ones are removed, so that what starts below inherits neither.
#
# run.sh forks every step of every workflow, and a child process inherits the
# environment its parent was started with. Left exported, a job whose first
# line was `env` printed the organisation's runner registration token -- good
# for an hour, and enough to register a self-hosted runner of the reader's own
# in the organisation, which GitHub then hands other repositories' jobs and
# their secrets. The workflow is the untrusted party here: that is the whole
# reason these runners are ephemeral.
#
# A shell variable that was never exported is not in a child's environment, so
# copying them across and unsetting is the whole of it. config.sh and the
# cleanup trap below read the copies.
jitconfig="${ZOOMIES_JITCONFIG:-}"
runner_token="${ZOOMIES_RUNNER_TOKEN:-}"
runner_url="${ZOOMIES_RUNNER_URL:-}"
unset ZOOMIES_JITCONFIG ACTIONS_RUNNER_INPUT_JITCONFIG ZOOMIES_RUNNER_TOKEN ZOOMIES_RUNNER_URL

if [ -n "${jitconfig}" ]; then
  log "starting with a just-in-time configuration (ephemeral, single use)"
  ./run.sh --jitconfig "${jitconfig}" &
  child=$!
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

  ./run.sh &
  child=$!
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
log "runner exited with status ${status}"
exit "$status"

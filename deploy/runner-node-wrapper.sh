#!/bin/sh
# Stands in for the node the runner ships, and loads one preload script before
# running the real one when the host has a cache service for this runner.
#
# The runner hands every JavaScript action ACTIONS_RESULTS_URL, the address of
# GitHub's results service, and it sets it itself, after the step's own
# environment, from the job message. A value in the container, in a step's env:
# or written to GITHUB_ENV is overwritten, so the only place left to act is the
# process the runner starts, which is this one: runner-node-wrap.sh puts this
# script where externals/node20/bin/node was and moves the real binary to
# node.real beside it.
#
# It does not change ACTIONS_RESULTS_URL, though, because that address is also
# where the artifact actions find the artifact service, and a cache service sent
# artifact calls fails every job that uploads or downloads one. It loads
# runner-cache-redirect.cjs instead, which rewrites only the calls addressed to
# the cache service and leaves every other request where it was going.
#
# It does nothing unless the agent set ZOOMIES_CACHE_SERVICE_URL, which it does
# only for a pool that turned the cache service on, and nothing for a job that
# is not on cache service v2, because that is the only protocol the service
# speaks. With the variable unset the job reaches GitHub exactly as it would
# without this file. The token and every other variable are left alone.
#
# A step inside a job container reaches this through /__e and does not have the
# variable, so it passes straight through there too.
if [ -n "${ZOOMIES_CACHE_SERVICE_URL:-}" ] && [ -n "${ACTIONS_CACHE_SERVICE_V2:-}" ]; then
	redirect=${ZOOMIES_CACHE_REDIRECT_FILE:-/usr/local/lib/zoomies/runner-cache-redirect.cjs}
	# Only if it is there: an image built without it must still run its actions.
	if [ -r "$redirect" ]; then
		NODE_OPTIONS="--require=$redirect${NODE_OPTIONS:+ $NODE_OPTIONS}"
		export NODE_OPTIONS
	fi
fi
exec "$0.real" "$@"

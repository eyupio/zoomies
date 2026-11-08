#!/bin/sh
# Stands in for the node the runner ships, and sends one variable to the host's
# cache service before running the real one.
#
# The runner hands every JavaScript action ACTIONS_RESULTS_URL, the address of
# GitHub's cache service, and it sets it itself, after the step's own
# environment, from the job message. A value in the container, in a step's env:
# or written to GITHUB_ENV is overwritten, so the only place left to change it
# is the process the runner starts, which is this one: runner-node-wrap.sh puts
# this script where externals/node20/bin/node was and moves the real binary to
# node.real beside it.
#
# It does nothing unless the agent set ZOOMIES_CACHE_SERVICE_URL, which it does
# only for a pool that turned the cache service on, and it does nothing for a
# job that is not on cache service v2, because that is the only protocol the
# service speaks. With the variable unset the job reaches GitHub exactly as it
# would without this file. Everything else, the token included, is left alone.
#
# A step inside a job container reaches this through /__e and does not have the
# variable, so it passes straight through there too.
if [ -n "${ZOOMIES_CACHE_SERVICE_URL:-}" ] && [ -n "${ACTIONS_CACHE_SERVICE_V2:-}" ]; then
	ACTIONS_RESULTS_URL=$ZOOMIES_CACHE_SERVICE_URL
	export ACTIONS_RESULTS_URL
fi
exec "$0.real" "$@"

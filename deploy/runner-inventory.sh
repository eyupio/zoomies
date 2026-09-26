#!/bin/sh
# Installed bytes, not the requested package list: each published image carries
# the distribution versions that its build actually resolved.
set -eu
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
if command -v dpkg-query >/dev/null 2>&1; then
  dpkg-query -W -f='${Package}\t${Version}\n' | sort > "$work/packages"
elif command -v rpm >/dev/null 2>&1; then
  rpm -qa --qf '%{NAME}\t%{VERSION}-%{RELEASE}\n' | sort > "$work/packages"
else
  : > "$work/packages"
fi
find /opt/hostedtoolcache -type f -name '*.complete' 2>/dev/null | sort > "$work/tools"
jq -n --rawfile packages "$work/packages" --rawfile tools "$work/tools" \
  --arg os "${ZOOMIES_RUNNER_OS:-unknown}" --arg release "${ZOOMIES_RUNNER_OS_VERSION:-unknown}" \
  --arg runner "${ZOOMIES_RUNNER_VERSION:-unknown}" \
  '{schema:1,os:$os,release:$release,runner:$runner,packages:($packages|split("\n")|map(select(length>0)|split("\t")|{name:.[0],version:.[1]})),tool_cache_entries:($tools|split("\n")|map(select(length>0)))}'

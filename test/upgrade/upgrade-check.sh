#!/bin/sh
# Upgrade an installation in place and check that it is still the same
# installation.
#
# Usage: test/upgrade/upgrade-check.sh <old-binary> <new-binary>
#
# The tiers below this one all build one binary and ask what it does. Nothing
# asks the question an operator asks on upgrade day -- "if I replace the binary
# under my running fleet, is my fleet still there?" -- because answering it
# needs two builds, and only one of them is in the tree. So this takes the last
# published release, starts it on a state directory of its own, replaces it
# with the build under test, and checks what survived:
#
#   * the version the controller reports changed, so the swap actually happened
#     and the assertions below are about a new build reading an old one's state
#   * the operator's zoomies.yaml is byte-for-byte what it was: an upgrade
#     reads configuration, it does not rewrite it
#   * the host row is the same row, by id: a new build that re-enrolled the
#     machine would look healthy and have lost every pool binding on it
#   * the schema moved, and a copy of the database from before it moved is on
#     disk, which is the promise docs/upgrading.md makes about migrations
#
# It needs no GitHub credentials and no Docker: the controller runs with its
# embedded agent, which is what a single-VM install is.
set -eu

old=${1:-}
new=${2:-}
if [ -z "$old" ] || [ -z "$new" ]; then
    echo "usage: $0 <old-binary> <new-binary>" >&2
    exit 2
fi
for bin in "$old" "$new"; do
    [ -x "$bin" ] || { echo "$bin is not an executable" >&2; exit 2; }
done

state=$(mktemp -d)
# The port is derived from the pid rather than fixed: two of these running at
# once on a busy runner would otherwise fail as a port clash and read as an
# upgrade defect.
port=$((20000 + $$ % 20000))
url="http://127.0.0.1:$port"
pid=""

cleanup() {
    [ -z "$pid" ] || kill "$pid" 2>/dev/null || true
    [ -z "$pid" ] || wait "$pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

fail() {
    echo "FAIL: $*" >&2
    echo "--- controller log ---" >&2
    tail -40 "$state/controller.log" >&2 2>/dev/null || true
    exit 1
}

sha_of() { sha256sum "$1" | cut -d' ' -f1; }

# start <binary> <label>: run a controller on the shared state directory and
# wait for it to say it is ready.
start() {
    HOME="$state" \
    ZOOMIES_STATE_DIR="$state" \
    ZOOMIES_CONFIG_DIR="$state" \
    ZOOMIES_DISABLE_AUTH=true \
    ZOOMIES_LOG_FORMAT=text \
        "$1" controller >> "$state/controller.log" 2>&1 &
    pid=$!
    waited=0
    while [ "$waited" -lt 60 ]; do
        if ! kill -0 "$pid" 2>/dev/null; then
            fail "the $2 controller exited during startup"
        fi
        if curl -fsS "$url/readyz" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
        waited=$((waited + 1))
    done
    fail "the $2 controller was not ready within ${waited}s"
}

stop() {
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    pid=""
}

# field <url-path> <json-key>: the first value of a top-level string key. The
# bodies here are small and flat, and a jq dependency in a shell check that
# exists to run on a bare machine is a worse trade than this.
field() {
    curl -fsS "$url$1" |
        tr ',' '\n' |
        sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" |
        head -1
}

# The configuration an operator wrote. Nothing in the upgrade may touch it.
cat > "$state/zoomies.yaml" <<EOF
server:
  bind: "127.0.0.1:$port"
database:
  path: "$state/zoomies.db"
agent:
  embedded: true
  capacity: 1
EOF
config_before=$(sha_of "$state/zoomies.yaml")

echo "-> starting the published release"
start "$old" "published"
old_version=$(field /readyz version)
old_host=$(field /api/v1/hosts id)
old_schema=$(field /readyz latest)
[ -n "$old_version" ] || fail "the published release reported no version"
[ -n "$old_host" ] || fail "the published release registered no host to survive the upgrade"
echo "   ok $old_version, host $old_host, schema $old_schema"
stop

echo "-> swapping in the build under test"
start "$new" "new"
new_version=$(field /readyz version)
new_host=$(field /api/v1/hosts id)
new_schema=$(field /readyz latest)
echo "   ok $new_version, host $new_host, schema $new_schema"

[ "$new_version" != "$old_version" ] ||
    fail "both runs reported $new_version, so the binary was never swapped and this checked nothing"

config_after=$(sha_of "$state/zoomies.yaml")
[ "$config_after" = "$config_before" ] ||
    fail "the upgrade rewrote zoomies.yaml ($config_before -> $config_after)"

[ "$new_host" = "$old_host" ] ||
    fail "the host is now $new_host, not the $old_host it was: the new build re-enrolled the machine instead of adopting it"

if [ "$new_schema" != "$old_schema" ]; then
    # Migrations ran, so the promise about them applies: the database as it was
    # before they ran is still on disk.
    copies=$(find "$state/pre-migration" -name zoomies.db 2>/dev/null | wc -l)
    [ "$copies" -ge 1 ] ||
        fail "the schema moved from $old_schema to $new_schema with no copy of the database kept under $state/pre-migration"
    echo "   ok the database was copied before the schema moved"
fi

echo "PASS: $old_version upgraded to $new_version in place, keeping its configuration, its database and its host"

#!/bin/sh
# What install.sh does on a host that already has the binary.
#
# Usage: test/install/install-check.sh [path-to-install.sh]
#
# install.sh was checked by `dash -n` and shellcheck and by nothing else, so
# every question about what it *does* was answered by reading it. That is how
# this shipped: the controller's own join command carries --mode agent,
# --controller and --join-token, and on a host that already had the installed
# version the script exited before any of them were used. The host was never
# joined, and the advice it printed -- add --yes -- re-downloaded the identical
# binary and joined nothing.
#
# Nothing here reaches the network or writes outside its temporary directory: a
# stub `zoomies` on PATH plays the part of an existing installation, --prefix
# keeps the binary in a temporary directory so no elevation is needed, and
# --no-init stops before the handoff, which is the part that would need a real
# controller.
set -eu

SCRIPT_UNDER_TEST="${1:-install.sh}"
[ -f "$SCRIPT_UNDER_TEST" ] || { echo "no install.sh at $SCRIPT_UNDER_TEST" >&2; exit 1; }
SCRIPT_UNDER_TEST=$(cd "$(dirname "$SCRIPT_UNDER_TEST")" && pwd)/$(basename "$SCRIPT_UNDER_TEST")

SH="${TEST_SH:-sh}"
failures=0
root=$(mktemp -d "${TMPDIR:-/tmp}/zoomies-install-check.XXXXXX")
trap 'rm -rf "$root"' EXIT INT TERM

# A stub that answers `zoomies version --short` the way a real one would.
stub_at() {
    mkdir -p "$1"
    cat > "$1/zoomies" <<STUB
#!/bin/sh
case "\$1 \$2" in
    "version --short") printf '%s\n' "$2" ;;
    *) exit 0 ;;
esac
STUB
    chmod +x "$1/zoomies"
}

# run <case> <expect-substring> <must-not-contain> [args...]
run() {
    name=$1; want=$2; unwanted=$3; shift 3
    bin="$root/$name/bin"; prefix="$root/$name/prefix"
    stub_at "$bin" "0.2-beta (b0322e8)"
    mkdir -p "$prefix"
    out=$(PATH="$bin:$PATH" "$SH" "$SCRIPT_UNDER_TEST" --prefix "$prefix" "$@" 2>&1) || true
    if ! printf '%s' "$out" | grep -qF -- "$want"; then
        printf 'FAIL %s: expected to see %s\n%s\n\n' "$name" "$want" "$out" >&2
        failures=$((failures + 1))
        return
    fi
    if [ -n "$unwanted" ] && printf '%s' "$out" | grep -qF -- "$unwanted"; then
        printf 'FAIL %s: should not have said %s\n%s\n\n' "$name" "$unwanted" "$out" >&2
        failures=$((failures + 1))
        return
    fi
    printf 'ok   %s\n' "$name"
}

# A bare re-run with nothing asked for still stops early, and now offers the
# flag that would actually change the version rather than only the one that
# reinstalls what is already here.
run already-installed-bare \
    "already installed" "" \
    --version 0.2-beta --no-init

run already-installed-names-version-flag \
    "--version <tag>" "" \
    --version 0.2-beta --no-init

# The case the operator hit. A join was asked for, so the run must not stop at
# the version check -- it keeps the binary and carries on to the handoff.
# "About to" is only reached past the version check, so it is the proof that
# the run did not stop there. The kept-binary line says "already installed" too,
# which is why the absence of that phrase cannot be the test.
run join-on-current-version-is-not-discarded \
    "About to" "to set this host up, run" \
    --version 0.2-beta --mode agent \
    --controller https://zoomies.example.com --join-token zoojoin_abc123 --no-init

# --yes was the advice, and on its own it must still not be the way a join
# happens: with intent present the join proceeds with or without it.
run join-does-not-need-yes \
    "so it was kept" "" \
    --version 0.2-beta --mode agent \
    --controller https://zoomies.example.com --join-token zoojoin_abc123 --no-init

if [ "$failures" -ne 0 ]; then
    printf '\n%d check(s) failed\n' "$failures" >&2
    exit 1
fi
printf '\nall install.sh checks passed\n'

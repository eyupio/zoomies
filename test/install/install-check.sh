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

run rejects-out-of-range-port \
    "--port must be from 1 to 65535." "" \
    --version 0.2-beta --port 65536 --no-init

# dev is a moving GitHub prerelease tag, not a numeric version to prefix with
# v. A fake release endpoint makes the URL and checksum behaviour observable
# without reaching the network.
dev="$root/dev-channel"
mkdir -p "$dev/release" "$dev/bin" "$dev/prefix"
cat > "$dev/release/zoomies_linux_amd64" <<'STUB'
#!/bin/sh
case "$1 $2" in
    "version --short") printf '%s\n' "main-sha-117bc18 (117bc18)" ;;
    *) exit 0 ;;
esac
STUB
chmod +x "$dev/release/zoomies_linux_amd64"
(
    cd "$dev/release"
    sha256sum zoomies_linux_amd64 > checksums.txt
)
cat > "$dev/bin/curl" <<'STUB'
#!/bin/sh
set -eu
out=""
url=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) out="$2"; shift 2 ;;
        http://*|https://*) url="$1"; shift ;;
        *) shift ;;
    esac
done
case "$url" in
    */download/dev/zoomies_linux_amd64) cp "$FAKE_RELEASE/zoomies_linux_amd64" "$out" ;;
    */download/dev/checksums.txt) cat "$FAKE_RELEASE/checksums.txt" ;;
    *) exit 22 ;;
esac
STUB
chmod +x "$dev/bin/curl"
out=$(FAKE_RELEASE="$dev/release" PATH="$dev/bin:$PATH" "$SH" "$SCRIPT_UNDER_TEST" \
    --prefix "$dev/prefix" --version dev --no-init --yes 2>&1) || true
if ! printf '%s' "$out" | grep -qF -- "Downloading zoomies_linux_amd64 dev"; then
    printf 'FAIL dev-channel: dev was not used as the download tag\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
elif printf '%s' "$out" | grep -qF -- "/download/vdev/"; then
    printf 'FAIL dev-channel: dev was incorrectly rewritten to vdev\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
else
    printf 'ok   dev-channel-is-not-v-prefixed\n'
fi

# Upgrade uses the new binary's preflight and never runs init, even when the
# requested moving tag is already installed. The same release mirror is used
# above, so these checks neither download nor restart a real deployment.
cat > "$dev/release/zoomies_linux_amd64" <<'STUB'
#!/bin/sh
case "$1" in
    version) printf '%s\n' "main-sha-117bc18 (117bc18)" ;;
    upgrade)
        printf '%s\n' "$*" >> "$UPGRADE_LOG"
        case " $* " in
            *" --check "*) [ "${REFUSE_UPGRADE_CHECK:-0}" = 0 ] || exit 1 ;;
        esac ;;
    *) printf 'unexpected setup: %s\n' "$*" >> "$UPGRADE_LOG"; exit 1 ;;
esac
STUB
chmod +x "$dev/release/zoomies_linux_amd64"
(cd "$dev/release" && sha256sum zoomies_linux_amd64 > checksums.txt)
upgrade_log="$dev/upgrade.log"
if ! out=$(UPGRADE_LOG="$upgrade_log" FAKE_RELEASE="$dev/release" PATH="$dev/bin:$PATH" "$SH" "$SCRIPT_UNDER_TEST" \
    --prefix "$dev/prefix" --version dev --upgrade --mode agent --non-interactive 2>&1); then
    printf 'FAIL upgrade handoff: %s\n' "$out" >&2
    failures=$((failures + 1))
elif [ "$(wc -l < "$upgrade_log" | tr -d ' ')" != 2 ] || grep -qF "unexpected setup" "$upgrade_log"; then
    printf 'FAIL upgrade did not preflight and apply exactly once\n' >&2
    failures=$((failures + 1))
else
    printf 'ok   upgrade-preflights-and-keeps-existing-enrolment\n'
fi
# An unattended upgrade adds nothing the operator did not agree to: the
# implied --yes of --non-interactive is not passed on, and --yes typed is.
if ! tail -n 1 "$upgrade_log" | grep -qF -- "--non-interactive" || tail -n 1 "$upgrade_log" | grep -qF -- "--yes"; then
    printf 'FAIL unattended upgrade was not told to leave missing folders: %s\n' "$(tail -n 1 "$upgrade_log")" >&2
    failures=$((failures + 1))
else
    printf 'ok   unattended-upgrade-adds-nothing-new\n'
fi
: > "$upgrade_log"
if ! out=$(UPGRADE_LOG="$upgrade_log" FAKE_RELEASE="$dev/release" PATH="$dev/bin:$PATH" "$SH" "$SCRIPT_UNDER_TEST" \
    --prefix "$dev/prefix" --version dev --upgrade --mode agent --yes 2>&1) ||
   ! tail -n 1 "$upgrade_log" | grep -qF -- "--yes"; then
    printf 'FAIL --yes was not passed to the upgrade: %s\n' "$(cat "$upgrade_log")" >&2
    failures=$((failures + 1))
else
    printf 'ok   upgrade-yes-approves-what-the-release-adds\n'
fi
# The update helper is a grant of root, so --yes does not stand for it and
# --update-helper is the one way to approve it in advance. It is passed to the
# apply and not to the preflight, which changes nothing and asks nothing.
: > "$upgrade_log"
if ! out=$(UPGRADE_LOG="$upgrade_log" FAKE_RELEASE="$dev/release" PATH="$dev/bin:$PATH" "$SH" "$SCRIPT_UNDER_TEST" \
    --prefix "$dev/prefix" --version dev --upgrade --mode agent --yes --update-helper 2>&1) ||
   ! tail -n 1 "$upgrade_log" | grep -qF -- "--update-helper"; then
    printf 'FAIL --update-helper was not passed to the upgrade: %s\n' "$(cat "$upgrade_log")" >&2
    failures=$((failures + 1))
else
    printf 'ok   upgrade-update-helper-is-passed-on\n'
fi
: > "$upgrade_log"
if ! out=$(UPGRADE_LOG="$upgrade_log" FAKE_RELEASE="$dev/release" PATH="$dev/bin:$PATH" "$SH" "$SCRIPT_UNDER_TEST" \
    --prefix "$dev/prefix" --version dev --upgrade --mode agent --yes 2>&1) ||
   grep -qF -- "--update-helper" "$upgrade_log"; then
    printf 'FAIL --yes alone passed --update-helper on: %s\n' "$(cat "$upgrade_log")" >&2
    failures=$((failures + 1))
else
    printf 'ok   upgrade-yes-does-not-approve-the-update-helper\n'
fi
if out=$("$SH" "$SCRIPT_UNDER_TEST" --prefix "$dev/prefix" --version dev --no-init --update-helper 2>&1); then
    printf 'FAIL --update-helper was accepted without --upgrade\n' >&2
    failures=$((failures + 1))
elif ! printf '%s' "$out" | grep -qF -- "--update-helper belongs to --upgrade"; then
    printf 'FAIL --update-helper without --upgrade gave the wrong refusal: %s\n' "$out" >&2
    failures=$((failures + 1))
else
    printf 'ok   update-helper-belongs-to-upgrade\n'
fi
# A refused preflight must not replace the previous binary.
stub_at "$dev/prefix" "0.1 (old)"
before=$(sha256sum "$dev/prefix/zoomies")
if UPGRADE_LOG="$upgrade_log" REFUSE_UPGRADE_CHECK=1 FAKE_RELEASE="$dev/release" PATH="$dev/bin:$PATH" "$SH" "$SCRIPT_UNDER_TEST" \
    --prefix "$dev/prefix" --version dev --upgrade --mode agent --yes > "$dev/refusal.log" 2>&1; then
    printf 'FAIL refused upgrade succeeded\n' >&2
    failures=$((failures + 1))
elif [ "$before" != "$(sha256sum "$dev/prefix/zoomies")" ]; then
    printf 'FAIL refused preflight replaced the binary\n' >&2
    failures=$((failures + 1))
else
    printf 'ok   refused-upgrade-keeps-the-installed-binary\n'
fi

# --demo downloads and checks the binary the way an install does, and then only
# runs it: from the temporary directory it landed in, with nothing written to
# the host and no elevation asked for. The fake release is the one above, with a
# stub that says what it was asked and where it was run from, and a `sudo` that
# fails the test if the script so much as reaches for it.
demo="$root/demo"
mkdir -p "$demo/release" "$demo/bin" "$demo/tmp"
cat > "$demo/release/zoomies_linux_amd64" <<'STUB'
#!/bin/sh
# The script asks whether the binary runs before it hands over; only `demo` is
# the one under test.
[ "$1" = version ] && exit 0
printf 'args=%s\n' "$*" >> "$DEMO_LOG"
printf 'from=%s\n' "$0" >> "$DEMO_LOG"
exit "${DEMO_EXIT:-0}"
STUB
chmod +x "$demo/release/zoomies_linux_amd64"
(cd "$demo/release" && sha256sum zoomies_linux_amd64 > checksums.txt)
cat > "$demo/bin/sudo" <<'STUB'
#!/bin/sh
printf 'sudo %s\n' "$*" >> "$DEMO_SUDO_LOG"
exit 1
STUB
chmod +x "$demo/bin/sudo"

demo_run() {
    # demo_run <exit status the stub returns> [install.sh args...]
    demo_exit=$1; shift
    : > "$demo/log"; rm -f "$demo/sudo.log"
    DEMO_EXIT="$demo_exit" DEMO_LOG="$demo/log" DEMO_SUDO_LOG="$demo/sudo.log" \
        FAKE_RELEASE="$demo/release" TMPDIR="$demo/tmp" PATH="$demo/bin:$dev/bin:$PATH" \
        "$SH" "$SCRIPT_UNDER_TEST" "$@" 2>&1
}

if ! out=$(demo_run 0 --demo --version dev --port 9123); then
    printf 'FAIL demo: the script failed\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
elif ! grep -qxF 'args=demo --port 9123' "$demo/log"; then
    printf 'FAIL demo: the binary was not run as "zoomies demo --port 9123":\n%s\n' "$(cat "$demo/log")" >&2
    failures=$((failures + 1))
elif ! grep -q "^from=$demo/tmp/zoomies-install\\." "$demo/log"; then
    printf 'FAIL demo: the binary did not run from a temporary directory:\n%s\n' "$(cat "$demo/log")" >&2
    failures=$((failures + 1))
elif [ -n "$(ls -A "$demo/tmp")" ]; then
    printf 'FAIL demo: it left %s behind in the temporary directory\n' "$(ls -A "$demo/tmp")" >&2
    failures=$((failures + 1))
elif [ -e "$demo/sudo.log" ]; then
    printf 'FAIL demo: it asked for sudo (%s) to install nothing\n' "$(cat "$demo/sudo.log")" >&2
    failures=$((failures + 1))
elif ! printf '%s' "$out" | grep -qF "Nothing was installed"; then
    printf 'FAIL demo: it did not say that nothing was installed\n%s\n' "$out" >&2
    failures=$((failures + 1))
else
    printf 'ok   demo-runs-from-a-temporary-directory-and-installs-nothing\n'
fi

# What the demo exits with is what the script exits with, so a demo that could
# not start is a failed command to whatever ran it.
if demo_run 7 --demo --version dev >/dev/null; then
    printf 'FAIL demo: a demo that exited 7 was reported as a success\n' >&2
    failures=$((failures + 1))
else
    demo_rc=0
    demo_run 7 --demo --version dev >/dev/null || demo_rc=$?
    if [ "$demo_rc" -ne 7 ]; then
        printf 'FAIL demo: exit status %s, want 7\n' "$demo_rc" >&2
        failures=$((failures + 1))
    else
        printf 'ok   demo-passes-its-exit-status-through\n'
    fi
fi

# A binary that will not run from where it landed -- a noexec /tmp is the usual
# reason -- is said to be that, with the one-line fix, and nothing is left behind.
cat > "$demo/release/zoomies_linux_amd64" <<'STUB'
#!/bin/sh
exit 126
STUB
(cd "$demo/release" && sha256sum zoomies_linux_amd64 > checksums.txt)
if out=$(demo_run 0 --demo --version dev); then
    printf 'FAIL demo: a binary that will not run was reported as a success\n%s\n' "$out" >&2
    failures=$((failures + 1))
elif ! printf '%s' "$out" | grep -qF "noexec"; then
    printf 'FAIL demo: a binary that will not run did not mention noexec\n%s\n' "$out" >&2
    failures=$((failures + 1))
elif [ -n "$(ls -A "$demo/tmp")" ]; then
    printf 'FAIL demo: it left %s behind after a binary that would not run\n' "$(ls -A "$demo/tmp")" >&2
    failures=$((failures + 1))
else
    printf 'ok   demo-says-when-the-binary-will-not-run-from-here\n'
fi

# Every flag about installing is a misunderstanding of --demo, and is said to be
# one rather than quietly ignored.
for combo in "--mode agent" "--deployment compose" "--upgrade" "--uninstall" "--no-init" \
             "--external-url zoomies.example.com" "--prefix $demo/tmp" "--non-interactive --answers $0"; do
    # shellcheck disable=SC2086 # the combination is several words on purpose
    if out=$(demo_run 0 --demo --version dev $combo); then
        printf 'FAIL demo: --demo %s was accepted\n%s\n' "$combo" "$out" >&2
        failures=$((failures + 1))
    elif ! printf '%s' "$out" | grep -qF -- "--demo installs nothing"; then
        printf 'FAIL demo: --demo %s was refused, but not for that reason\n%s\n' "$combo" "$out" >&2
        failures=$((failures + 1))
    else
        printf 'ok   demo-refuses %s\n' "$combo"
    fi
done

# GitHub's download host (github.com/.../releases/download) has answered 504 at its
# edge while api.github.com served the same files normally, and an installer that
# knew only the first could not install anything until it recovered. A fake of both
# makes each way the host can fail observable without reaching the network. The
# API's answer is shaped like the real one: a release with its own url and name, an
# uploader inside each asset, and an asset whose name only begins like the one asked
# for, listed first.
outage="$root/outage"
mkdir -p "$outage/release" "$outage/bin"
cat > "$outage/release/zoomies_linux_amd64" <<'STUB'
#!/bin/sh
case "$1 $2" in
    "version --short") printf '%s\n' "9.9.9 (outage)" ;;
    *) exit 0 ;;
esac
STUB
chmod +x "$outage/release/zoomies_linux_amd64"
printf '#!/bin/sh\nexit 0\n# not the release\n' > "$outage/release/zoomies_linux_amd64.bad"
(
    cd "$outage/release"
    sha256sum zoomies_linux_amd64 > checksums.txt
)
cat > "$outage/release/release.json" <<'JSON'
{
  "url": "https://api.github.com/repos/eyupio/zoomies/releases/777",
  "html_url": "https://github.com/eyupio/zoomies/releases/tag/v9.9.9",
  "id": 777,
  "author": {
    "login": "someone",
    "url": "https://api.github.com/users/someone",
    "type": "User"
  },
  "tag_name": "v9.9.9",
  "name": "v9.9.9",
  "assets": [
    {
      "url": "https://api.github.com/repos/eyupio/zoomies/releases/assets/100",
      "id": 100,
      "name": "zoomies_linux_amd64.sbom",
      "uploader": {
        "login": "github-actions[bot]",
        "url": "https://api.github.com/users/github-actions%5Bbot%5D"
      },
      "browser_download_url": "https://github.com/eyupio/zoomies/releases/download/v9.9.9/zoomies_linux_amd64.sbom"
    },
    {
      "url": "https://api.github.com/repos/eyupio/zoomies/releases/assets/101",
      "id": 101,
      "name": "zoomies_linux_amd64",
      "uploader": {
        "login": "github-actions[bot]",
        "url": "https://api.github.com/users/github-actions%5Bbot%5D"
      },
      "browser_download_url": "https://github.com/eyupio/zoomies/releases/download/v9.9.9/zoomies_linux_amd64"
    },
    {
      "url": "https://api.github.com/repos/eyupio/zoomies/releases/assets/102",
      "id": 102,
      "name": "checksums.txt",
      "uploader": {
        "login": "github-actions[bot]",
        "url": "https://api.github.com/users/github-actions%5Bbot%5D"
      },
      "browser_download_url": "https://github.com/eyupio/zoomies/releases/download/v9.9.9/checksums.txt"
    }
  ]
}
JSON
# FAKE_DL is what the download host answers (ok, or the status it fails with) and
# FAKE_API what the API does: ok, oneline (the same, on a single line), badbin (the
# binary's bytes are not the ones checksums.txt names) or down. Every request is
# written to FAKE_LOG with the Accept header it carried.
cat > "$outage/bin/curl" <<'STUB'
#!/bin/sh
set -eu
out=""
url=""
accept=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) out="$2"; shift 2 ;;
        -H) case "$2" in Accept:*) accept="${2#Accept: }" ;; esac; shift 2 ;;
        http://*|https://*) url="$1"; shift ;;
        *) shift ;;
    esac
done
printf '%s accept=%s\n' "$url" "$accept" >> "$FAKE_LOG"
serve() { if [ -n "$out" ]; then cp "$1" "$out"; else cat "$1"; fi; }
fail() { printf 'curl: (22) The requested URL returned error: %s\n' "$1" >&2; exit 22; }
case "$url" in
    */download/v9.9.9/*)
        [ "$FAKE_DL" = ok ] || fail "$FAKE_DL"
        serve "$FAKE_RELEASE/${url##*/}" ;;
    https://api.github.com/repos/eyupio/zoomies/releases/tags/v9.9.9)
        [ "$FAKE_API" != down ] || fail 503
        if [ "$FAKE_API" = oneline ]; then tr -d '\n' < "$FAKE_RELEASE/release.json"; else cat "$FAKE_RELEASE/release.json"; fi ;;
    https://api.github.com/repos/eyupio/zoomies/releases/assets/*)
        [ "$accept" = application/octet-stream ] || fail 415
        case "${url##*/}" in
            100) printf 'not the binary\n' > "$FAKE_RELEASE/.sbom"; serve "$FAKE_RELEASE/.sbom" ;;
            101) if [ "$FAKE_API" = badbin ]; then serve "$FAKE_RELEASE/zoomies_linux_amd64.bad"; else serve "$FAKE_RELEASE/zoomies_linux_amd64"; fi ;;
            102) serve "$FAKE_RELEASE/checksums.txt" ;;
            *) fail 404 ;;
        esac ;;
    *) exit 22 ;;
esac
STUB
chmod +x "$outage/bin/curl"

# outage_run <case> <download host> <api> [VAR=value...] leaves the installer's
# output in $out and what it asked the network for in $log.
outage_run() {
    name=$1; dl=$2; api=$3; shift 3
    mkdir -p "$root/outage-$name/prefix"
    log="$root/outage-$name/log"
    : > "$log"
    out=$(env "$@" FAKE_DL="$dl" FAKE_API="$api" FAKE_LOG="$log" FAKE_RELEASE="$outage/release" \
        PATH="$outage/bin:$PATH" "$SH" "$SCRIPT_UNDER_TEST" \
        --prefix "$root/outage-$name/prefix" --version v9.9.9 --no-init --yes 2>&1) || true
}

# The download host fails; the API is up. The install goes through, says it went
# round, and the file it installed is the one the checksum names.
outage_run via-api 504 ok
if ! printf '%s' "$out" | grep -qF -e "came through the release API"; then
    printf 'FAIL release-api-fallback: the installer did not say it used the API\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
elif ! printf '%s' "$out" | grep -qF -e "error: 504"; then
    printf 'FAIL release-api-fallback: the installer did not say what the download host answered\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
elif ! printf '%s' "$out" | grep -qF -e "matches"; then
    printf 'FAIL release-api-fallback: the checksum was not checked\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
elif [ ! -x "$root/outage-via-api/prefix/zoomies" ]; then
    printf 'FAIL release-api-fallback: nothing was installed\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
elif ! grep -qF -e "/releases/assets/101 accept=application/octet-stream" "$log"; then
    printf 'FAIL release-api-fallback: the asset was not asked for as application/octet-stream\n%s\n' "$(cat "$log")" >&2
    failures=$((failures + 1))
elif grep -qF -e "/releases/assets/100 " "$log"; then
    printf 'FAIL release-api-fallback: a file whose name only begins like the asset was taken for it\n%s\n' "$(cat "$log")" >&2
    failures=$((failures + 1))
elif [ "$(grep -c -e '/releases/tags/' "$log")" -ne 1 ]; then
    # The binary and the checksums both went round, so the release was needed
    # twice and asked about once: the API allows an address sixty requests an hour.
    printf 'FAIL release-api-fallback: the release was not looked up exactly once\n%s\n' "$(cat "$log")" >&2
    failures=$((failures + 1))
else
    printf 'ok   release-api-fallback-installs-when-the-download-host-504s\n'
fi

# Something in between can hand the API's answer on without its line ends.
outage_run oneline 504 oneline
if [ -x "$root/outage-oneline/prefix/zoomies" ] && printf '%s' "$out" | grep -qF -e "matches"; then
    printf 'ok   release-api-fallback-reads-an-answer-on-one-line\n'
else
    printf 'FAIL release-api-fallback: an answer on a single line was not read\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
fi

# A 404 is the host saying the file is not there, which the API would say too. It
# is not gone round, so it costs no request out of an hourly allowance.
outage_run not-found 404 ok
if grep -qF -e "api.github.com" "$log"; then
    printf 'FAIL release-api-fallback: a 404 was taken to the API\n%s\n' "$(cat "$log")" >&2
    failures=$((failures + 1))
elif ! printf '%s' "$out" | grep -qF -e "could not download"; then
    printf 'FAIL release-api-fallback: a 404 was not reported as a failed download\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
else
    printf 'ok   release-api-fallback-leaves-a-404-alone\n'
fi

# Both fail: the operator is told what the first one said, which is the one they
# can look up, and nothing is installed.
outage_run both-down 504 down
if ! printf '%s' "$out" | grep -qF -e "could not download"; then
    printf 'FAIL release-api-fallback: no failed download was reported\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
elif ! printf '%s' "$out" | grep -qF -e "error: 504"; then
    printf 'FAIL release-api-fallback: the download host'"'"'s error was lost\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
elif [ -e "$root/outage-both-down/prefix/zoomies" ]; then
    printf 'FAIL release-api-fallback: something was installed with both doors shut\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
else
    printf 'ok   release-api-fallback-reports-the-first-failure-when-both-fail\n'
fi

# Where the file came from does not loosen how it is checked.
outage_run bad-bytes 504 badbin
if ! printf '%s' "$out" | grep -qF -e "checksum mismatch"; then
    printf 'FAIL release-api-fallback: a binary that does not match its checksum was not refused\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
elif [ -e "$root/outage-bad-bytes/prefix/zoomies" ]; then
    printf 'FAIL release-api-fallback: a binary that does not match its checksum was installed\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
else
    printf 'ok   release-api-fallback-still-checks-the-checksum\n'
fi

# The API is GitHub's. A mirror an operator pointed the installer at has no such
# thing, and is not asked for one.
outage_run mirror 504 ok ZOOMIES_BASE_URL=https://mirror.example/releases
if grep -qF -e "api.github.com" "$log"; then
    printf 'FAIL release-api-fallback: a mirror was sent to GitHub'"'"'s API\n%s\n' "$(cat "$log")" >&2
    failures=$((failures + 1))
elif [ -e "$root/outage-mirror/prefix/zoomies" ]; then
    printf 'FAIL release-api-fallback: something was installed from a mirror that was down\n%s\n\n' "$out" >&2
    failures=$((failures + 1))
else
    printf 'ok   release-api-fallback-is-for-github-only\n'
fi

if [ "$failures" -ne 0 ]; then
    printf '\n%d check(s) failed\n' "$failures" >&2
    exit 1
fi
printf '\nall install.sh checks passed\n'

#!/bin/sh
# First boot of a Zoomies instance deployed from a provider's marketplace.
#
# It is deliberately dull: install a container runtime, write an answer file,
# run the pinned installer, and leave a note saying how to finish. Everything
# that needs a secret -- the first administrator, the GitHub App -- happens
# afterwards in the browser, because a marketplace form's answers live in
# instance metadata and a boot log, and neither is a place for a credential.
#
# It is rendered into a cloud-config by render.sh and runs as root, once. A
# second run is safe: each step checks what is already there, so a boot that
# failed halfway can be resumed by running this again.
#
# Failure is loud and final. A half-built controller that answers on :8080 is
# worse than one that never started, because the first thing an operator does
# with a reachable Zoomies is point GitHub at it.
set -eu

# The paths are overridable so that the test suite can run the rendering half
# of this script against a temporary directory and check that a real boot would
# write an answer file `zoomies init` accepts. Proving that without a VM is
# worth a seam: the alternative is a test that reimplements the substitution
# below and then agrees with itself while the boot fails.
ENV_FILE=${ZOOMIES_ENV_FILE:-/etc/zoomies/marketplace.env}
ANSWERS_TMPL=${ZOOMIES_ANSWERS_TEMPLATE:-/etc/zoomies/answers.yaml.tmpl}
ANSWERS=${ZOOMIES_ANSWERS_FILE:-/etc/zoomies/answers.yaml}
NOTES=${ZOOMIES_NOTES_FILE:-/etc/zoomies/first-login.txt}
LOG_TAG=zoomies-bootstrap

say() { echo "[$LOG_TAG] $*"; }
die() { echo "[$LOG_TAG] $*" >&2; exit 1; }

# ---------------------------------------------------------------- the inputs

[ -r "$ENV_FILE" ] || die "$ENV_FILE is missing; the image was rendered without its inputs"
# shellcheck disable=SC1090
. "$ENV_FILE"

: "${ZOOMIES_RELEASE:?the rendered inputs name no release}"
: "${ZOOMIES_INSTALLER_URL:?the rendered inputs name no installer}"
: "${ZOOMIES_INSTALLER_SHA256:?the rendered inputs name no installer checksum}"

ZOOMIES_HOSTNAME=${ZOOMIES_HOSTNAME:-}
[ -n "$ZOOMIES_HOSTNAME" ] || die "no hostname was given: set ZOOMIES_HOSTNAME to the DNS name that points at this instance"

ZOOMIES_MODE=${ZOOMIES_MODE:-single}
case "$ZOOMIES_MODE" in
  single|controller) ;;
  *) die "mode $ZOOMIES_MODE is not one this package installs: use single or controller" ;;
esac

# Only one terminating arrangement is implemented so far. Refusing the others
# by name beats installing a controller that serves plain HTTP to the internet
# because an input was spelled in a way nothing here reads.
ZOOMIES_TLS=${ZOOMIES_TLS:-off}
case "$ZOOMIES_TLS" in
  off) ;;
  *) die "TLS mode $ZOOMIES_TLS is not available in this package: use off, with a proxy or load balancer terminating TLS in front" ;;
esac

ZOOMIES_EXTERNAL_URL=${ZOOMIES_EXTERNAL_URL:-https://$ZOOMIES_HOSTNAME}
ZOOMIES_PUBLISH_ADDR=${ZOOMIES_PUBLISH_ADDR:-127.0.0.1}
ZOOMIES_TRUSTED_PROXIES=${ZOOMIES_TRUSTED_PROXIES:-127.0.0.1/32,::1/128}
ZOOMIES_DATA_DIR=${ZOOMIES_DATA_DIR:-/var/lib/zoomies}
ZOOMIES_CONTROLLER_IMAGE=${ZOOMIES_CONTROLLER_IMAGE:-}
[ -n "$ZOOMIES_CONTROLLER_IMAGE" ] || die "the rendered inputs name no controller image"

ZOOMIES_ADMIN_BOOTSTRAP=${ZOOMIES_ADMIN_BOOTSTRAP:-setup-token}
[ "$ZOOMIES_ADMIN_BOOTSTRAP" = "setup-token" ] || \
  die "administrator bootstrap $ZOOMIES_ADMIN_BOOTSTRAP is not supported: setup-token is the only one that needs no secret in instance metadata"

say "deploying Zoomies $ZOOMIES_RELEASE for $ZOOMIES_EXTERNAL_URL"

# ------------------------------------------------------------ the dependency

install_runtime() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    say "a container runtime with compose is already here"
    return 0
  fi
  command -v apt-get >/dev/null 2>&1 || \
    die "no container runtime, and this bootstrap only knows how to install one with apt: install Docker and compose, then run this again"

  say "installing the distribution's Docker and compose packages"
  export DEBIAN_FRONTEND=noninteractive
  # The distribution's own packages rather than a script piped from the
  # internet: this runs unattended on first boot, and a pipeline to a shell is
  # one more thing that can change between the day this was tested and the day
  # somebody deploys it.
  apt-get update -qq
  apt-get install -y -qq docker.io docker-compose-v2 ca-certificates curl
  systemctl enable --now docker
}

# ------------------------------------------------------------------ the data

prepare_data_dir() {
  # A compose deployment keeps the database in a named volume. Backing that
  # volume with a directory chosen here is what lets a provider's attached disk
  # hold it -- and what makes "where is my data" answerable without knowing how
  # Docker lays out its own storage.
  mkdir -p "$ZOOMIES_DATA_DIR"
  # The published image runs as an unprivileged uid that does not exist on this
  # host. A directory owned by anyone else gives "unable to open database file
  # (14)" at the first start, which reads like corruption and is not.
  chown 65532:65532 "$ZOOMIES_DATA_DIR"
  chmod 0750 "$ZOOMIES_DATA_DIR"

  if docker volume inspect zoomies-data >/dev/null 2>&1; then
    say "the zoomies-data volume already exists; leaving it alone"
    return 0
  fi
  say "backing the zoomies-data volume with $ZOOMIES_DATA_DIR"
  docker volume create \
    --driver local \
    --opt type=none \
    --opt "device=$ZOOMIES_DATA_DIR" \
    --opt o=bind \
    zoomies-data >/dev/null
}

# --------------------------------------------------------------- the answers

render_answers() {
  [ -r "$ANSWERS_TMPL" ] || die "$ANSWERS_TMPL is missing; the image was rendered without its answer template"
  umask 077

  # Each CIDR is quoted on the way into the YAML flow sequence. An IPv6 one has
  # to be: ::1/128 unquoted starts a plain scalar with a colon, which is not a
  # scalar at all, and the parser stops on the whole answer file rather than on
  # the setting -- so the loopback default alone would have failed at boot.
  trusted=$(printf '%s' "$ZOOMIES_TRUSTED_PROXIES" | awk -F, '{
    out = ""
    for (i = 1; i <= NF; i++) {
      gsub(/^[ \t]+|[ \t]+$/, "", $i)
      if ($i == "") continue
      out = out (out == "" ? "" : ", ") "\"" $i "\""
    }
    printf "%s", out
  }')

  sed \
    -e "s|__ZOOMIES_MODE__|$ZOOMIES_MODE|g" \
    -e "s|__ZOOMIES_CONTROLLER_IMAGE__|$ZOOMIES_CONTROLLER_IMAGE|g" \
    -e "s|__ZOOMIES_PUBLISH_ADDR__|$ZOOMIES_PUBLISH_ADDR|g" \
    -e "s|__ZOOMIES_TRUSTED_PROXIES__|$trusted|g" \
    -e "s|__ZOOMIES_EXTERNAL_URL__|$ZOOMIES_EXTERNAL_URL|g" \
    "$ANSWERS_TMPL" > "$ANSWERS"

  # A placeholder nobody wired up would otherwise reach `zoomies init` as a
  # literal, and the failure would be a YAML value that looks like a mistake
  # rather than an omission.
  if grep -q '__ZOOMIES_[A-Z_]*__' "$ANSWERS"; then
    die "the answer file still has unfilled placeholders: $(grep -o '__ZOOMIES_[A-Z_]*__' "$ANSWERS" | sort -u | tr '\n' ' ')"
  fi
}

# ------------------------------------------------------------- the installer

run_installer() {
  script=$(mktemp)
  say "fetching the pinned installer"
  curl -fsSL --retry 3 --retry-delay 2 -o "$script" "$ZOOMIES_INSTALLER_URL"

  actual=$(sha256sum "$script" | cut -d' ' -f1)
  if [ "$actual" != "$ZOOMIES_INSTALLER_SHA256" ]; then
    rm -f "$script"
    die "the installer's checksum is $actual and this release pins $ZOOMIES_INSTALLER_SHA256; refusing to run it"
  fi

  say "installing Zoomies $ZOOMIES_RELEASE"
  sh "$script" --answers "$ANSWERS" --version "$ZOOMIES_RELEASE"
  rm -f "$script"
}

# ----------------------------------------------------------------- the notes

write_notes() {
  umask 022
  cat > "$NOTES" <<NOTE
Zoomies $ZOOMIES_RELEASE is installed on this instance.

  Address   $ZOOMIES_EXTERNAL_URL
  Published $ZOOMIES_PUBLISH_ADDR:8080
  Data      $ZOOMIES_DATA_DIR (the zoomies-data volume)
  Answers   $ANSWERS

Finish setup in three steps.

1. Read this instance's setup token:

     docker compose -f /etc/zoomies/docker-compose.yml logs zoomies | grep 'setup token'

   It is printed only while no account exists, and a restart mints a new one.
   Holding it is the proof of ownership that an empty database is not: without
   it, whoever loads the page first would become the administrator.

2. Open $ZOOMIES_EXTERNAL_URL, paste the token, and create the first
   administrator.

3. Connect GitHub from the UI. Nothing runs until a pool exists, so make one
   after that.
NOTE
  chmod 0644 "$NOTES"

  if [ -d /etc/update-motd.d ]; then
    cat > /etc/update-motd.d/99-zoomies <<'MOTD'
#!/bin/sh
[ -r /etc/zoomies/first-login.txt ] && cat /etc/zoomies/first-login.txt
MOTD
    chmod 0755 /etc/update-motd.d/99-zoomies
  fi
}

# ----------------------------------------------------------------- the check

wait_for_health() {
  # The question is whether the controller answers, not whether compose
  # reported success: an image that starts and then exits on a bad setting
  # leaves a healthy-looking `up -d` behind it.
  i=0
  while [ "$i" -lt 60 ]; do
    if curl -fsS -o /dev/null "http://$ZOOMIES_PUBLISH_ADDR:8080/healthz"; then
      say "the controller is answering on $ZOOMIES_PUBLISH_ADDR:8080"
      return 0
    fi
    i=$((i + 1))
    sleep 2
  done
  die "the controller did not answer /healthz within two minutes; its log is: docker compose -f /etc/zoomies/docker-compose.yml logs zoomies"
}

# Rendering is everything this script decides; the rest is installing what it
# decided. Stopping here is how the tests read those decisions.
if [ "${ZOOMIES_RENDER_ONLY:-}" = "1" ]; then
  render_answers
  say "rendered $ANSWERS and stopped, because ZOOMIES_RENDER_ONLY is set"
  exit 0
fi

install_runtime
prepare_data_dir
render_answers
run_installer
wait_for_health
write_notes

say "done. $NOTES says how to finish setup."

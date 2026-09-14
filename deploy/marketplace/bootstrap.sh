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
CADDY_TMPL=${ZOOMIES_CADDY_TEMPLATE:-/etc/zoomies/Caddyfile.tmpl}
PROXY_TMPL=${ZOOMIES_PROXY_TEMPLATE:-/etc/zoomies/proxy-compose.yml.tmpl}
PROXY_DIR=${ZOOMIES_PROXY_DIR:-/etc/zoomies/proxy}
TUNNEL_TMPL=${ZOOMIES_TUNNEL_TEMPLATE:-/etc/zoomies/tunnel-compose.yml.tmpl}
TUNNEL_DIR=${ZOOMIES_TUNNEL_DIR:-/etc/zoomies/tunnel}
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
ZOOMIES_PROXY_IMAGE=${ZOOMIES_PROXY_IMAGE:-}
ZOOMIES_TUNNEL_IMAGE=${ZOOMIES_TUNNEL_IMAGE:-}
ZOOMIES_TUNNEL_TOKEN=${ZOOMIES_TUNNEL_TOKEN:-}

ZOOMIES_HOSTNAME=${ZOOMIES_HOSTNAME:-}
[ -n "$ZOOMIES_HOSTNAME" ] || die "no hostname was given: set ZOOMIES_HOSTNAME to the DNS name that points at this instance"

ZOOMIES_MODE=${ZOOMIES_MODE:-single}
case "$ZOOMIES_MODE" in
  single|controller) ;;
  *) die "mode $ZOOMIES_MODE is not one this package installs: use single or controller" ;;
esac

# Who holds the certificate for the public name. Refusing an unknown value by
# name beats publishing an origin to the internet in the clear because an input
# was spelled in a way nothing here reads.
#
# Three of the four leave the controller speaking plain HTTP, which is correct:
# the origin is not the public endpoint, and something in front of it is.
ZOOMIES_TLS=${ZOOMIES_TLS:-acme}
case "$ZOOMIES_TLS" in
  acme|files|tunnel|cloudflare|off) ;;
  *) die "TLS mode $ZOOMIES_TLS is not one this package installs: use acme, files, tunnel, cloudflare or off" ;;
esac

ZOOMIES_ACME_EMAIL=${ZOOMIES_ACME_EMAIL:-}
ZOOMIES_TLS_CERT_FILE=${ZOOMIES_TLS_CERT_FILE:-}
ZOOMIES_TLS_KEY_FILE=${ZOOMIES_TLS_KEY_FILE:-}
if [ "$ZOOMIES_TLS" = "files" ]; then
  [ -n "$ZOOMIES_TLS_CERT_FILE" ] && [ -n "$ZOOMIES_TLS_KEY_FILE" ] || \
    die "TLS mode files needs ZOOMIES_TLS_CERT_FILE and ZOOMIES_TLS_KEY_FILE: the certificate this instance serves, and its key"
fi

# The listener, what the instance exposes, and which proxies it believes.
#
# The last of those is the one that goes wrong silently. X-Forwarded-For is
# read only from a peer in trusted_proxies, and CF-Connecting-IP only from a
# peer that is Cloudflare's own edge -- so the right answer differs between
# Cloudflare in front of a published origin and a tunnel, even though both are
# Cloudflare. Getting it wrong costs every audit row the real client's address
# and throttles the whole internet as one caller, and nothing reports it.
case "$ZOOMIES_TLS" in
  files)
    # The only arrangement where Zoomies itself is the public endpoint.
    ZOOMIES_PUBLISH_ADDR=${ZOOMIES_PUBLISH_ADDR:-0.0.0.0}
    ZOOMIES_TRUSTED_PROXIES=${ZOOMIES_TRUSTED_PROXIES:-127.0.0.1/32,::1/128}
    ZOOMIES_BIND=0.0.0.0:443
    ZOOMIES_TLS_MODE=files
    # -k, because the question is whether the controller is answering, not
    # whether a certificate issued for the public name matches 127.0.0.1.
    HEALTH_URL=https://127.0.0.1/healthz
    HEALTH_CURL_OPTS=-k
    ;;
  cloudflare)
    # Cloudflare's edge connects to a published origin over plain HTTP on 80,
    # so the peer is Cloudflare and the word expands to its ranges -- which is
    # what makes CF-Connecting-IP, the one header a client cannot forge, count.
    ZOOMIES_PUBLISH_ADDR=${ZOOMIES_PUBLISH_ADDR:-0.0.0.0}
    ZOOMIES_BIND=$ZOOMIES_PUBLISH_ADDR:80
    ZOOMIES_TLS_MODE=off
    ZOOMIES_TRUSTED_PROXIES=${ZOOMIES_TRUSTED_PROXIES:-cloudflare}
    HEALTH_URL=http://127.0.0.1:80/healthz
    HEALTH_CURL_OPTS=
    ;;
  *)
    # acme, tunnel and off all reach the controller on loopback. For a tunnel
    # the peer is cloudflared on this machine rather than Cloudflare's edge, so
    # trusting Cloudflare's ranges would trust nothing that ever connects: the
    # address to believe is the local daemon's, and the header that survives is
    # X-Forwarded-For.
    ZOOMIES_PUBLISH_ADDR=${ZOOMIES_PUBLISH_ADDR:-127.0.0.1}
    ZOOMIES_TRUSTED_PROXIES=${ZOOMIES_TRUSTED_PROXIES:-127.0.0.1/32,::1/128}
    ZOOMIES_BIND=$ZOOMIES_PUBLISH_ADDR:8080
    ZOOMIES_TLS_MODE=off
    HEALTH_URL=http://$ZOOMIES_PUBLISH_ADDR:8080/healthz
    HEALTH_CURL_OPTS=
    ;;
esac

ZOOMIES_EXTERNAL_URL=${ZOOMIES_EXTERNAL_URL:-https://$ZOOMIES_HOSTNAME}
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
    -e "s|__ZOOMIES_BIND__|$ZOOMIES_BIND|g" \
    -e "s|__ZOOMIES_TLS_MODE__|$ZOOMIES_TLS_MODE|g" \
    -e "s|__ZOOMIES_TLS_CERT_FILE__|$ZOOMIES_TLS_CERT_FILE|g" \
    -e "s|__ZOOMIES_TLS_KEY_FILE__|$ZOOMIES_TLS_KEY_FILE|g" \
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

# ----------------------------------------------------------------- the proxy

render_proxy() {
  [ "$ZOOMIES_TLS" = "acme" ] || return 0
  [ -r "$CADDY_TMPL" ] && [ -r "$PROXY_TMPL" ] || \
    die "the ACME path needs $CADDY_TMPL and $PROXY_TMPL, and the image was rendered without them"
  [ -n "$ZOOMIES_PROXY_IMAGE" ] || die "the rendered inputs name no proxy image, so the ACME path has nothing to run"

  say "writing the configuration for a certificate holder in front of the controller"
  mkdir -p "$PROXY_DIR" "$ZOOMIES_DATA_DIR/caddy"

  # An empty email is not the same as no email: "email" alone is not a
  # directive, so the whole line goes rather than its value.
  acme_global=""
  [ -n "$ZOOMIES_ACME_EMAIL" ] && acme_global="email $ZOOMIES_ACME_EMAIL"

  sed \
    -e "s|__ZOOMIES_ACME_GLOBAL__|$acme_global|g" \
    -e "s|__ZOOMIES_HOSTNAME__|$ZOOMIES_HOSTNAME|g" \
    -e "s|__ZOOMIES_PUBLISH_ADDR__|$ZOOMIES_PUBLISH_ADDR|g" \
    "$CADDY_TMPL" > "$PROXY_DIR/Caddyfile"

  sed \
    -e "s|__ZOOMIES_PROXY_IMAGE__|$ZOOMIES_PROXY_IMAGE|g" \
    -e "s|__ZOOMIES_DATA_DIR__|$ZOOMIES_DATA_DIR|g" \
    "$PROXY_TMPL" > "$PROXY_DIR/docker-compose.yml"
}

render_tunnel() {
  [ "$ZOOMIES_TLS" = "tunnel" ] || return 0
  [ -r "$TUNNEL_TMPL" ] || die "the tunnel path needs $TUNNEL_TMPL, and the image was rendered without it"
  [ -n "$ZOOMIES_TUNNEL_IMAGE" ] || die "the rendered inputs name no tunnel image, so the tunnel path has nothing to run"

  say "writing the tunnel daemon's configuration"
  mkdir -p "$TUNNEL_DIR"
  sed -e "s|__ZOOMIES_TUNNEL_IMAGE__|$ZOOMIES_TUNNEL_IMAGE|g" "$TUNNEL_TMPL" > "$TUNNEL_DIR/docker-compose.yml"

  # The token is written beside the compose file rather than into it, and only
  # if there is one. A deployment rendered without a token is the better shape:
  # the operator pastes it here over SSH and it never passes through instance
  # metadata, the provider's database or cloud-init's log.
  if [ -n "$ZOOMIES_TUNNEL_TOKEN" ]; then
    (umask 077; printf 'TUNNEL_TOKEN=%s\n' "$ZOOMIES_TUNNEL_TOKEN" > "$TUNNEL_DIR/.env")
  elif [ ! -f "$TUNNEL_DIR/.env" ]; then
    (umask 077; printf '# Paste the tunnel token from the Cloudflare dashboard, then:\n#   docker compose -f %s/docker-compose.yml up -d\nTUNNEL_TOKEN=\n' "$TUNNEL_DIR" > "$TUNNEL_DIR/.env")
  fi
}

start_tunnel() {
  [ "$ZOOMIES_TLS" = "tunnel" ] || return 0
  if ! grep -q '^TUNNEL_TOKEN=.' "$TUNNEL_DIR/.env" 2>/dev/null; then
    say "no tunnel token yet, so the tunnel is not started; $NOTES says where to paste one"
    return 0
  fi
  docker compose -f "$TUNNEL_DIR/docker-compose.yml" up -d
}

start_proxy() {
  [ "$ZOOMIES_TLS" = "acme" ] || return 0
  docker compose -f "$PROXY_DIR/docker-compose.yml" up -d

  if [ "${ZOOMIES_DNS:-pending}" != "ready" ]; then
    # Not a failure. Caddy retries, so an instance booted before its DNS record
    # existed becomes healthy on its own once the record does -- which is the
    # usual order when the provider assigns the address at boot.
    say "the certificate cannot be issued until $ZOOMIES_HOSTNAME resolves to this instance; the proxy will keep trying"
  fi
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

  # What each arrangement still needs from a person, which is the part a
  # generic note would leave them to work out on their own.
  case "$ZOOMIES_TLS" in
    tunnel)
      cat >> "$NOTES" <<NOTE

This deployment publishes no port. Cloudflare serves HTTPS on
$ZOOMIES_HOSTNAME and a tunnel daemon on this instance dials out to it, so
there is no inbound rule, no DNS record of this machine's own and no
certificate here.
NOTE
      if grep -q '^TUNNEL_TOKEN=.' "$TUNNEL_DIR/.env" 2>/dev/null; then
        cat >> "$NOTES" <<NOTE
The tunnel is running with the token this instance was rendered with.
NOTE
      else
        cat >> "$NOTES" <<NOTE
The tunnel is not running yet, because it has no token. Create the tunnel in
the Cloudflare dashboard, point its public hostname at
http://127.0.0.1:8080, then:

  \$EDITOR $TUNNEL_DIR/.env          # paste the token
  docker compose -f $TUNNEL_DIR/docker-compose.yml up -d

Pasting it here rather than into the provider's form is deliberate: a token in
instance metadata is also in the provider's database and in cloud-init's log.
NOTE
      fi
      ;;
    cloudflare)
      cat >> "$NOTES" <<NOTE

This instance serves plain HTTP on port 80 for Cloudflare to proxy, and
believes X-Forwarded-For and CF-Connecting-IP from Cloudflare's own ranges.

Firewall port 80 to those ranges. Left open to everything, the origin is
reachable directly and Cloudflare is merely in front of it rather than in the
way -- so a caller who finds this address bypasses every rule set at the edge.
NOTE
      ;;
  esac
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
    # shellcheck disable=SC2086
    if curl -fsS $HEALTH_CURL_OPTS -o /dev/null "$HEALTH_URL"; then
      say "the controller is answering on $HEALTH_URL"
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
  render_proxy
  render_tunnel
  say "rendered $ANSWERS and stopped, because ZOOMIES_RENDER_ONLY is set"
  exit 0
fi

install_runtime
prepare_data_dir
render_answers
run_installer
wait_for_health
render_proxy
start_proxy
render_tunnel
start_tunnel
write_notes

say "done. $NOTES says how to finish setup."

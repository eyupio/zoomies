#!/bin/sh
# Render the cloud-config a provider boots, from the pinned release and one
# inputs file.
#
#     ./render.sh inputs.env > cloud-init.yaml
#
# The output is self-contained: it carries the bootstrap, the answer template
# and every resolved setting, so it can be reviewed, archived and diffed
# against the next release without needing this repository to make sense.
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
inputs=${1:-}

if [ -z "$inputs" ] || [ "$inputs" = "-h" ] || [ "$inputs" = "--help" ]; then
  echo "usage: render.sh <inputs.env> > cloud-init.yaml" >&2
  echo "       start from inputs.env.example; release.env supplies the rest" >&2
  [ -n "$inputs" ] && exit 0
  exit 2
fi
[ -r "$inputs" ] || { echo "render.sh: cannot read $inputs" >&2; exit 2; }

# The release first, the operator's inputs second: an input may override a
# default, but it may not quietly move the deployment to another release --
# that is a change to release.env, where the lock file is regenerated with it.
# shellcheck disable=SC1091
. "$here/release.env"
pinned_release=$ZOOMIES_RELEASE
pinned_installer=$ZOOMIES_INSTALLER_URL
pinned_sum=$ZOOMIES_INSTALLER_SHA256
default_image=$ZOOMIES_CONTROLLER_IMAGE
pinned_proxy=$ZOOMIES_PROXY_IMAGE

# shellcheck disable=SC1090
. "$inputs"

ZOOMIES_RELEASE=$pinned_release
ZOOMIES_INSTALLER_URL=$pinned_installer
ZOOMIES_INSTALLER_SHA256=$pinned_sum
ZOOMIES_PROXY_IMAGE=$pinned_proxy

: "${ZOOMIES_HOSTNAME:?set ZOOMIES_HOSTNAME in $inputs to the DNS name that points at this instance}"
ZOOMIES_EXTERNAL_URL=${ZOOMIES_EXTERNAL_URL:-https://$ZOOMIES_HOSTNAME}
ZOOMIES_CONTROLLER_IMAGE=${ZOOMIES_CONTROLLER_IMAGE:-$default_image}
ZOOMIES_MODE=${ZOOMIES_MODE:-single}
ZOOMIES_TLS=${ZOOMIES_TLS:-acme}
ZOOMIES_ACME_EMAIL=${ZOOMIES_ACME_EMAIL:-}
ZOOMIES_TLS_CERT_FILE=${ZOOMIES_TLS_CERT_FILE:-}
ZOOMIES_TLS_KEY_FILE=${ZOOMIES_TLS_KEY_FILE:-}
ZOOMIES_DNS=${ZOOMIES_DNS:-pending}
ZOOMIES_PUBLISH_ADDR=${ZOOMIES_PUBLISH_ADDR:-127.0.0.1}
ZOOMIES_TRUSTED_PROXIES=${ZOOMIES_TRUSTED_PROXIES:-127.0.0.1/32,::1/128}
ZOOMIES_DATA_DIR=${ZOOMIES_DATA_DIR:-/var/lib/zoomies}
ZOOMIES_ADMIN_BOOTSTRAP=${ZOOMIES_ADMIN_BOOTSTRAP:-setup-token}

# A rendered artefact that carried a credential would put it in instance
# metadata, which is the one place this package promises never to put one. The
# check is here rather than only in review because the inputs file is the thing
# an operator edits, and "I just pasted the App key in" is the obvious mistake.
for forbidden in ZOOMIES_GITHUB_PRIVATE_KEY ZOOMIES_GITHUB_WEBHOOK_SECRET ZOOMIES_ADMIN_PASSWORD ZOOMIES_ENCRYPTION_KEY ZOOMIES_JOIN_TOKEN; do
  eval "value=\${$forbidden:-}"
  [ -z "$value" ] || {
    echo "render.sh: $inputs sets $forbidden, which would put a secret in instance metadata" >&2
    echo "           the first administrator, GitHub and any join token are set up in the UI instead" >&2
    exit 2
  }
done

env_block() {
  cat <<ENV
# Rendered by render.sh. The release lines are pinned; the rest came from the
# inputs file this was rendered with.
ZOOMIES_RELEASE=$ZOOMIES_RELEASE
ZOOMIES_INSTALLER_URL=$ZOOMIES_INSTALLER_URL
ZOOMIES_INSTALLER_SHA256=$ZOOMIES_INSTALLER_SHA256
ZOOMIES_CONTROLLER_IMAGE=$ZOOMIES_CONTROLLER_IMAGE
ZOOMIES_PROXY_IMAGE=$ZOOMIES_PROXY_IMAGE
ZOOMIES_HOSTNAME=$ZOOMIES_HOSTNAME
ZOOMIES_EXTERNAL_URL=$ZOOMIES_EXTERNAL_URL
ZOOMIES_DNS=$ZOOMIES_DNS
ZOOMIES_MODE=$ZOOMIES_MODE
ZOOMIES_TLS=$ZOOMIES_TLS
ZOOMIES_ACME_EMAIL=$ZOOMIES_ACME_EMAIL
ZOOMIES_TLS_CERT_FILE=$ZOOMIES_TLS_CERT_FILE
ZOOMIES_TLS_KEY_FILE=$ZOOMIES_TLS_KEY_FILE
ZOOMIES_PUBLISH_ADDR=$ZOOMIES_PUBLISH_ADDR
ZOOMIES_TRUSTED_PROXIES=$ZOOMIES_TRUSTED_PROXIES
ZOOMIES_DATA_DIR=$ZOOMIES_DATA_DIR
ZOOMIES_ADMIN_BOOTSTRAP=$ZOOMIES_ADMIN_BOOTSTRAP
ENV
}

env_block > "${TMPDIR:-/tmp}/zoomies-render-env.$$"
trap 'rm -f "${TMPDIR:-/tmp}/zoomies-render-env.$$"' EXIT INT TERM

# Each placeholder sits on its own line inside a YAML block scalar, so the file
# that replaces it is indented to exactly where the placeholder started. Doing
# it in awk keeps this script to a POSIX shell and whatever the instance
# already has.
awk \
  -v env_file="${TMPDIR:-/tmp}/zoomies-render-env.$$" \
  -v answers_file="$here/answers.yaml.tmpl" \
  -v bootstrap_file="$here/bootstrap.sh" \
  -v caddy_file="$here/Caddyfile.tmpl" \
  -v proxy_file="$here/proxy-compose.yml.tmpl" '
function emit(file, indent,   line) {
  while ((getline line < file) > 0) {
    if (line == "") print ""
    else print indent line
  }
  close(file)
}
{
  if ($0 ~ /__ZOOMIES_ENV__$/)            { match($0, /^ */); emit(env_file,       substr($0, 1, RLENGTH)); next }
  if ($0 ~ /__ZOOMIES_ANSWERS_TMPL__$/)   { match($0, /^ */); emit(answers_file,   substr($0, 1, RLENGTH)); next }
  if ($0 ~ /__ZOOMIES_BOOTSTRAP__$/)      { match($0, /^ */); emit(bootstrap_file, substr($0, 1, RLENGTH)); next }
  if ($0 ~ /__ZOOMIES_CADDYFILE__$/)      { match($0, /^ */); emit(caddy_file,     substr($0, 1, RLENGTH)); next }
  if ($0 ~ /__ZOOMIES_PROXY_COMPOSE__$/)  { match($0, /^ */); emit(proxy_file,     substr($0, 1, RLENGTH)); next }
  print
}
' "$here/cloud-init.yaml.tmpl"

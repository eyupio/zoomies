#!/bin/sh
#
# Install one toolchain from deploy/toolcache.lock, for the runner-full image.
#
# Each setup-* action looks in the tool cache before it downloads anything,
# and each looks for its own layout: <Tool>/<version>/<arch>, with an
# <arch>.complete file beside it that says the directory is finished. What is
# unpacked here is laid out exactly as the action would have left it, so a job
# asking for a version the image carries resolves it from disk. Anything laid
# out differently is a directory the action walks past on its way to the
# network, which is the whole cost this image exists to remove.
#
#   Python   Python/<version>/<arch>                 the archive's own setup.sh
#   Node.js  node/<version>/<arch>                   setup-node
#   Go       go/<version>/<arch>                     setup-go
#   Java     Java_Temurin-Hotspot_jdk/<version>/<arch>   setup-java; <version>
#            is Adoptium's semver with + as -, which the lock already carries
#
# .NET, Maven, Gradle and Rust have no tool cache of their own: .NET goes
# where setup-dotnet installs by default, and the other three where a workflow
# that never calls a setup action finds them on PATH.
#
# Every archive is checked against the lock's digest before it is unpacked.
# This image runs what it unpacks on every job it serves, and a mirror or a
# proxy between the build and the publisher is a place to substitute it.
#
#   usage: runner-toolcache.sh <lock> <ubuntu-version> <amd64|arm64> <tool>
#
set -eu

lock="${1:?usage: runner-toolcache.sh <lock> <ubuntu-version> <arch> <tool>}"
platform="${2:?usage: runner-toolcache.sh <lock> <ubuntu-version> <arch> <tool>}"
arch="${3:?usage: runner-toolcache.sh <lock> <ubuntu-version> <arch> <tool>}"
tool="${4:?usage: runner-toolcache.sh <lock> <ubuntu-version> <arch> <tool>}"

toolcache="${AGENT_TOOLSDIRECTORY:?AGENT_TOOLSDIRECTORY must name the tool cache}"

# The name every tool cache uses for an architecture; setup-python,
# setup-node, setup-go and setup-java all agree on it.
case "${arch}" in
  amd64) tc_arch=x64 ;;
  arm64) tc_arch=arm64 ;;
  *) echo "runner-toolcache.sh: unsupported architecture '${arch}'" >&2; exit 64 ;;
esac

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

# fetch <url> <algo:hex> <file>: download and refuse anything but the pinned
# bytes.
#
# No fixed --retry-delay: with one, curl neither backs off nor honours a
# Retry-After, and Maven Central answers a burst of downloads from one address
# with 429s that a five-second retry only repeats. Left to itself curl doubles
# its wait and does what the server asks, within the time cap.
fetch() {
  curl -fsSL --retry 6 --retry-max-time 600 -o "$3" "$1"
  algo="${2%%:*}"
  sum="${2#*:}"
  case "${algo}" in
    sha256) echo "${sum}  $3" | sha256sum -c - >/dev/null ;;
    sha512) echo "${sum}  $3" | sha512sum -c - >/dev/null ;;
    *) echo "runner-toolcache.sh: unknown digest '${algo}' for $1" >&2; exit 1 ;;
  esac || { echo "runner-toolcache.sh: $1 does not match its digest in the lock" >&2; exit 1; }
}

# cache <Tool> <version> <archive>: unpack an archive with one top-level
# directory as a finished tool-cache entry.
cache() {
  dest="${toolcache}/$1/$2/${tc_arch}"
  rm -rf "${dest}"
  mkdir -p "${dest}"
  tar -xzf "$3" -C "${dest}" --strip-components=1
  touch "${toolcache}/$1/$2/${tc_arch}.complete"
}

# The Rust toolchain rustup installs is the lock's rust row; read up front,
# because the loop below is reading the same file.
rust_toolchain="$(awk '$1 == "rust" { print $2; exit }' "${lock}")"

installed=0
# Rows are: tool version platform arch digest url. A row applies when its
# platform and arch are this build's or "-".
while read -r t version plat a digest url; do
  case "${t}" in ''|'#'*) continue ;; esac
  [ "${t}" = "${tool}" ] || continue
  [ "${plat}" = - ] || [ "${plat}" = "${platform}" ] || continue
  [ "${a}" = - ] || [ "${a}" = "${arch}" ] || continue

  echo "runner-toolcache.sh: ${t} ${version}"
  archive="${work}/archive"
  case "${t}" in
    python)
      fetch "${url}" "${digest}" "${archive}"
      mkdir -p "${work}/python"
      tar -xzf "${archive}" -C "${work}/python"
      # The archive installs itself, as setup-python has it do: setup.sh
      # copies it into the tool cache, links python3.x as python, brings pip
      # up to date and writes the .complete marker.
      (cd "${work}/python" && AGENT_TOOLSDIRECTORY="${toolcache}" bash ./setup.sh >/dev/null)
      rm -rf "${work}/python"
      "${toolcache}/Python/${version}/${tc_arch}/python" --version
      ;;
    node)
      fetch "${url}" "${digest}" "${archive}"
      cache node "${version}" "${archive}"
      "${toolcache}/node/${version}/${tc_arch}/bin/node" --version
      ;;
    go)
      fetch "${url}" "${digest}" "${archive}"
      cache go "${version}" "${archive}"
      "${toolcache}/go/${version}/${tc_arch}/bin/go" version
      ;;
    java)
      fetch "${url}" "${digest}" "${archive}"
      cache Java_Temurin-Hotspot_jdk "${version}" "${archive}"
      jdk="${toolcache}/Java_Temurin-Hotspot_jdk/${version}/${tc_arch}"
      "${jdk}/bin/java" -version 2>&1 | head -n 1
      # An architecture-neutral name for each major, so the image can point
      # JAVA_HOME at one without knowing which architecture it was built for.
      mkdir -p /usr/lib/jvm
      ln -sfn "${jdk}" "/usr/lib/jvm/temurin-${version%%.*}"
      ;;
    dotnet)
      fetch "${url}" "${digest}" "${archive}"
      # setup-dotnet's default install directory on Linux. SDKs of different
      # channels unpack side by side, and its install script sees a version
      # already here and does not fetch it again.
      mkdir -p /usr/share/dotnet
      tar -xzf "${archive}" -C /usr/share/dotnet
      ln -sfn /usr/share/dotnet/dotnet /usr/local/bin/dotnet
      ;;
    maven)
      fetch "${url}" "${digest}" "${archive}"
      mkdir -p "/opt/maven/${version}"
      tar -xzf "${archive}" -C "/opt/maven/${version}" --strip-components=1
      ln -sfn "/opt/maven/${version}/bin/mvn" /usr/local/bin/mvn
      ;;
    gradle)
      fetch "${url}" "${digest}" "${archive}"
      mkdir -p /opt/gradle
      unzip -q "${archive}" -d /opt/gradle
      ln -sfn "/opt/gradle/gradle-${version}/bin/gradle" /usr/local/bin/gradle
      ;;
    rustup)
      fetch "${url}" "${digest}" "${work}/rustup-init"
      chmod +x "${work}/rustup-init"
      # rustup verifies what it downloads against the channel manifest
      # itself.
      [ -n "${rust_toolchain}" ] || { echo "runner-toolcache.sh: the lock has no rust row" >&2; exit 1; }
      "${work}/rustup-init" -y --no-modify-path --profile minimal --default-toolchain "${rust_toolchain}"
      "${CARGO_HOME:?CARGO_HOME must be set}/bin/rustup" component add rustfmt clippy
      "${CARGO_HOME}/bin/rustc" --version
      ;;
    rust)
      # Installed by the rustup row.
      continue
      ;;
    *)
      echo "runner-toolcache.sh: the lock names '${t}', which this script does not know how to install" >&2
      exit 1
      ;;
  esac
  rm -f "${archive}"
  installed=$((installed + 1))
done < "${lock}"

# A tool with no row for this build is a lock that has drifted from the
# Dockerfile, and an image missing a toolchain it advertises.
if [ "${installed}" -eq 0 ]; then
  echo "runner-toolcache.sh: the lock has no ${tool} row for Ubuntu ${platform} ${arch}" >&2
  exit 1
fi

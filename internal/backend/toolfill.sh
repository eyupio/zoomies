#!/bin/bash
#
# Fill a pool's kept tool cache with the toolchain versions its jobs ask for.
#
# Zoomies runs this inside the pool's own runner image, as the runner user,
# with the pool's tool cache mounted at $AGENT_TOOLSDIRECTORY. The image is
# the point: it has the C library, the operating system release and the
# architecture the runners have, so what is unpacked here is what setup-python
# would have unpacked in a job, and a build that exists for this image is one
# that works in it.
#
# Each request is one line of $ZOOMIES_TOOL_REQUESTS, "tool version
# [distribution]", as the workflows wrote it: "python 3.12", "node lts/*",
# "java 21 temurin". A version is resolved the way the setup action resolves
# it -- the newest stable release it matches -- against the manifest that
# action reads, and laid out where that action looks before downloading:
#
#   python  Python/<version>/<arch>                   actions/python-versions
#   node    node/<version>/<arch>                     nodejs.org/dist
#   go      go/<version>/<arch>                       go.dev/dl
#   java    Java_Temurin-Hotspot_jdk/<version>/<arch> api.adoptium.net
#
# with <arch>.complete beside each, which is the marker that says a directory
# is finished. A version already there is left alone.
#
# Every outcome is one line on stdout for the agent to read back:
#
#   zoomies-fill <installed|present|skipped|failed> <tool> <request> <version-or-reason>
#
# and a request this script cannot serve is "skipped" with the reason, never
# guessed at: a version installed under a name the action does not look for
# is a download the job still makes.
set -u

toolcache="${AGENT_TOOLSDIRECTORY:?AGENT_TOOLSDIRECTORY must name the tool cache}"

case "$(uname -m)" in
  x86_64|amd64) arch=x64; go_arch=amd64; adoptium_arch=x64 ;;
  aarch64|arm64) arch=arm64; go_arch=arm64; adoptium_arch=aarch64 ;;
  *) echo "zoomies-fill failed all - this image's architecture $(uname -m) has no builds in the tool cache's layout"; exit 0 ;;
esac

# A proxy's own CA, when the host has one, is added to the system roots
# rather than replacing them: the manifests and the archives are on public
# hosts, and a bundle of only the proxy's CA would trust none of them where
# there is no proxy in the way.
if [ -n "${ZOOMIES_EXTRA_CA_FILE:-}" ] && [ -r "${ZOOMIES_EXTRA_CA_FILE}" ]; then
  bundle="$(mktemp)"
  for roots in /etc/ssl/certs/ca-certificates.crt /etc/pki/tls/certs/ca-bundle.crt; do
    [ -r "$roots" ] && cat "$roots" >> "$bundle" && break
  done
  cat "${ZOOMIES_EXTRA_CA_FILE}" >> "$bundle"
  export CURL_CA_BUNDLE="$bundle"
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

say() { echo "zoomies-fill $*"; }

# fetch <url> <file>: curl with the retries a burst of downloads needs, and
# without a fixed delay, so a 429's Retry-After is honoured.
fetch() { curl -fsSL --retry 5 --retry-max-time 300 -o "$2" "$1"; }

# checksum <algo> <hex> <file>
checksum() { echo "$2  $3" | "${1}sum" -c - >/dev/null 2>&1; }

# prefix_of <request>: the release prefix a request names, or nothing when it
# is not a plain version -- a range, an alias this script does not resolve, a
# free-threaded build. "3.12", "3.12.x", "v22" and "1.27.*" are plain.
prefix_of() {
  local v="${1#v}"
  v="${v%.x}"; v="${v%.X}"; v="${v%.\*}"
  case "$v" in
    ''|*[!0-9.]*|.*|*.|*..*) return 1 ;;
  esac
  echo "$v"
}

complete_marker() { echo "$toolcache/$1/$2/$arch.complete"; }

# lay_out <Tool> <version> <archive>: unpack an archive with one top-level
# directory as a finished tool-cache entry.
lay_out() {
  local dest="$toolcache/$1/$2/$arch"
  rm -rf "$dest"
  mkdir -p "$dest" || return 1
  tar -xzf "$3" -C "$dest" --strip-components=1 || return 1
  touch "$(complete_marker "$1" "$2")"
}

python_manifest=""
fill_python() {
  local req="$1" prefix os_version url version
  prefix="$(prefix_of "$req")" || { say skipped python "$req" "not a plain version; setup-python resolves it itself"; return; }
  # shellcheck disable=SC1091 # the image's own release file, read at run time
  os_version="$(. /etc/os-release 2>/dev/null; echo "${ID:-}-${VERSION_ID:-}")"
  if [ "${os_version%%-*}" != ubuntu ]; then
    say skipped python "$req" "actions/python-versions publishes Ubuntu builds only, and this image is ${os_version}"
    return
  fi
  if [ -z "$python_manifest" ]; then
    python_manifest="$work/python-manifest.json"
    fetch https://raw.githubusercontent.com/actions/python-versions/main/versions-manifest.json "$python_manifest" ||
      { python_manifest=""; say failed python "$req" "could not read the actions/python-versions manifest"; return; }
  fi
  # The manifest is newest first, so the first stable match is the one
  # setup-python would choose.
  read -r version url < <(jq -r --arg p "$prefix" --arg os "${os_version#ubuntu-}" --arg arch "$arch" '
    [.[] | select(.stable) | select(.version == $p or (.version | startswith($p + ".")))
         | .version as $v
         | .files[] | select(.platform == "linux" and .platform_version == $os and .arch == $arch)
         | "\($v) \(.download_url)"] | first // empty' "$python_manifest")
  if [ -z "${version:-}" ]; then
    say skipped python "$req" "no stable build matches it for Ubuntu ${os_version#ubuntu-} on $arch"
    return
  fi
  if [ -e "$(complete_marker Python "$version")" ]; then say present python "$req" "$version"; return; fi
  rm -rf "$work/python" && mkdir -p "$work/python"
  if ! fetch "$url" "$work/python.tgz" || ! tar -xzf "$work/python.tgz" -C "$work/python"; then
    say failed python "$req" "could not download $url"; return
  fi
  # The archive installs itself, as setup-python has it do: setup.sh copies
  # it into the tool cache, links python3.x as python and writes the marker.
  if (cd "$work/python" && bash ./setup.sh >/dev/null 2>&1) && [ -e "$(complete_marker Python "$version")" ]; then
    say installed python "$req" "$version"
  else
    say failed python "$req" "its setup.sh did not finish"
  fi
  rm -rf "$work/python" "$work/python.tgz"
}

node_index=""
# shellcheck disable=SC2016 # $a in the filters is jq's variable, not the shell's
fill_node() {
  # The request is a workflow's text, so it reaches jq as data (--arg),
  # never as part of the program.
  local req="$1" filter arg="" version file sums
  case "$req" in
    lts/\*|lts) filter='select(.lts != false)' ;;
    lts/*) filter='select((.lts | tostring | ascii_downcase) == ($a | ascii_downcase))'; arg="${req#lts/}" ;;
    latest|current|node) filter='.' ;;
    *)
      arg="$(prefix_of "$req")" || { say skipped node "$req" "not a plain version or an lts alias; setup-node resolves it itself"; return; }
      filter='select((.version | ltrimstr("v")) == $a or (.version | ltrimstr("v") | startswith($a + ".")))' ;;
  esac
  if [ -z "$node_index" ]; then
    node_index="$work/node-index.json"
    fetch https://nodejs.org/dist/index.json "$node_index" ||
      { node_index=""; say failed node "$req" "could not read nodejs.org's release index"; return; }
  fi
  version="$(jq -r --arg a "$arg" "[.[] | $filter | .version] | first // empty" "$node_index")"
  version="${version#v}"
  if [ -z "$version" ]; then say skipped node "$req" "no release matches it"; return; fi
  if [ -e "$(complete_marker node "$version")" ]; then say present node "$req" "$version"; return; fi
  file="node-v$version-linux-$arch.tar.gz"
  if ! fetch "https://nodejs.org/dist/v$version/$file" "$work/node.tgz" ||
     ! fetch "https://nodejs.org/dist/v$version/SHASUMS256.txt" "$work/node.sums"; then
    say failed node "$req" "could not download $file"; return
  fi
  sums="$(awk -v f="$file" '$2 == f { print $1 }' "$work/node.sums")"
  if [ -z "$sums" ] || ! checksum sha256 "$sums" "$work/node.tgz"; then
    say failed node "$req" "$file does not match nodejs.org's SHASUMS256.txt"; return
  fi
  if lay_out node "$version" "$work/node.tgz"; then say installed node "$req" "$version"; else say failed node "$req" "could not unpack $file"; fi
  rm -f "$work/node.tgz" "$work/node.sums"
}

go_index=""
fill_go() {
  local req="$1" prefix line name sha version
  case "$req" in
    stable) prefix="" ;;
    *) prefix="$(prefix_of "$req")" || { say skipped go "$req" "not a plain version; setup-go resolves it itself"; return; } ;;
  esac
  if [ -z "$go_index" ]; then
    go_index="$work/go-index.json"
    fetch 'https://go.dev/dl/?mode=json&include=all' "$go_index" ||
      { go_index=""; say failed go "$req" "could not read go.dev's release index"; return; }
  fi
  line="$(jq -r --arg p "$prefix" --arg arch "$go_arch" '
    [.[] | select(.stable) | (.version | ltrimstr("go")) as $v
         | select($p == "" or $v == $p or ($v | startswith($p + ".")))
         | .files[] | select(.os == "linux" and .arch == $arch and .kind == "archive")
         | "\($v) \(.filename) \(.sha256)"] | first // empty' "$go_index")"
  if [ -z "$line" ]; then say skipped go "$req" "no stable release matches it"; return; fi
  read -r version name sha <<< "$line"
  # setup-go keeps a release under its semver: go1.27 is 1.27.0.
  case "$version" in *.*.*) ;; *) version="$version.0" ;; esac
  if [ -e "$(complete_marker go "$version")" ]; then say present go "$req" "$version"; return; fi
  if ! fetch "https://go.dev/dl/$name" "$work/go.tgz"; then say failed go "$req" "could not download $name"; return; fi
  if ! checksum sha256 "$sha" "$work/go.tgz"; then say failed go "$req" "$name does not match go.dev's checksum"; return; fi
  if lay_out go "$version" "$work/go.tgz"; then say installed go "$req" "$version"; else say failed go "$req" "could not unpack $name"; fi
  rm -f "$work/go.tgz"
}

fill_java() {
  local req="$1" dist="${2:-}" prefix major line link sha semver version
  case "$dist" in
    temurin|adopt|adopt-hotspot) ;;
    *) say skipped java "$req" "only the temurin distribution is filled, and this asks for ${dist:-none}"; return ;;
  esac
  prefix="$(prefix_of "$req")" || { say skipped java "$req" "not a plain version; setup-java resolves it itself"; return; }
  major="${prefix%%.*}"
  if ! fetch "https://api.adoptium.net/v3/assets/feature_releases/$major/ga?architecture=$adoptium_arch&image_type=jdk&os=linux&vendor=eclipse&jvm_impl=hotspot&page_size=50&sort_order=DESC" "$work/java.json"; then
    say failed java "$req" "could not read Adoptium's releases for Java $major"; return
  fi
  line="$(jq -r --arg p "$prefix" '
    [.[] | .version_data.semver as $s
         | select(($s | split("+")[0]) == $p or ($s | startswith($p + ".")) or ($s | startswith($p + "+")))
         | .binaries[] | select(.package.link | endswith(".tar.gz"))
         | "\($s) \(.package.link) \(.package.checksum)"] | first // empty' "$work/java.json")"
  if [ -z "$line" ]; then say skipped java "$req" "no Temurin release matches it"; return; fi
  read -r semver link sha <<< "$line"
  # setup-java names a Temurin release by its semver with + as -.
  version="${semver//+/-}"
  if [ -e "$(complete_marker Java_Temurin-Hotspot_jdk "$version")" ]; then say present java "$req" "$version"; return; fi
  if ! fetch "$link" "$work/java.tgz"; then say failed java "$req" "could not download $link"; return; fi
  if ! checksum sha256 "$sha" "$work/java.tgz"; then say failed java "$req" "the download does not match Adoptium's checksum"; return; fi
  if lay_out Java_Temurin-Hotspot_jdk "$version" "$work/java.tgz"; then say installed java "$req" "$version"; else say failed java "$req" "could not unpack the JDK"; fi
  rm -f "$work/java.tgz" "$work/java.json"
}

while read -r tool version dist; do
  [ -n "${tool:-}" ] || continue
  case "$tool" in
    python) fill_python "$version" ;;
    node) fill_node "$version" ;;
    go) fill_go "$version" ;;
    java) fill_java "$version" "${dist:-}" ;;
    dotnet) say skipped dotnet "$version" "setup-dotnet installs into its own folder, not the tool cache" ;;
    *) say skipped "$tool" "$version" "not a toolchain the tool cache holds" ;;
  esac
done <<< "${ZOOMIES_TOOL_REQUESTS:-}"
exit 0

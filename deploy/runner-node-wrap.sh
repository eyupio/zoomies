#!/bin/sh
# Puts runner-node-wrapper.sh in front of the node the runner ships.
#
#   runner-node-wrap.sh <runner directory> [wrapper]
#
# Each externals/node*/bin/node becomes the wrapper, and the binary it replaced
# is kept beside it as node.real, which the wrapper runs. Only the glibc builds
# are wrapped. The *_alpine ones are what the runner uses inside an Alpine job
# container, where the variable the wrapper looks for is never set.
#
# It runs inside the same RUN as the archive is unpacked, because the files are
# renamed and a rename in a later layer would copy two hundred megabytes of
# node into it. It is safe to run twice, and it fails if it finds no node at
# all: a runner release that moved its externals should break this build,
# where the alternative is an image that quietly lost the cache service. It
# fails the same way if the image has no bash, which the wrapper is written in:
# without one every JavaScript action would fail to start, and that is found out
# by a job and not by the build.
set -eu

dir=${1:?usage: runner-node-wrap.sh <runner directory> [wrapper]}
wrapper=${2:-$(dirname "$0")/runner-node-wrapper.sh}
[ -f "$wrapper" ] || { echo "runner-node-wrap: $wrapper is not a file" >&2; exit 1; }
command -v bash >/dev/null 2>&1 || {
	echo "runner-node-wrap: this image has no bash, and the wrapper needs it to pass the runner's hyphenated action inputs on; install bash in the base image" >&2
	exit 1
}

found=0
for node in "$dir"/externals/node*/bin/node; do
	case $node in *_alpine/*) continue ;; esac
	[ -f "$node" ] || continue
	found=$((found + 1))
	[ -e "$node.real" ] && continue
	mv "$node" "$node.real"
	cp "$wrapper" "$node"
	chmod 0755 "$node"
done

if [ "$found" -eq 0 ]; then
	echo "runner-node-wrap: no externals/node*/bin/node under $dir; has the runner changed where it keeps node?" >&2
	exit 1
fi

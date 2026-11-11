//go:build ignore

// Command node-wrapper stands in for the node the runner ships, and loads one
// preload script before running the real one when the host has a cache service
// for this runner.
//
// The runner hands every JavaScript action ACTIONS_RESULTS_URL, the address of
// GitHub's results service, and it sets it itself, after the step's own
// environment, from the job message. A value in the container, in a step's env:
// or written to GITHUB_ENV is overwritten, so the only place left to act is the
// process the runner starts, which is this one: runner-node-wrap.sh puts this
// program where externals/node20/bin/node was and moves the real binary to
// node.real beside it.
//
// It does not change ACTIONS_RESULTS_URL, though, because that address is also
// where the artifact actions find the artifact service, and a cache service sent
// artifact calls fails every job that uploads or downloads one. It loads
// runner-cache-redirect.cjs instead, which rewrites only the calls addressed to
// the cache service and leaves every other request where it was going.
//
// It does nothing unless the agent set ZOOMIES_CACHE_SERVICE_URL, which it does
// only for a pool that turned the cache service on, and nothing for a job that
// is not on cache service v2, because that is the only protocol the service
// speaks. With the variable unset the job reaches GitHub exactly as it would
// without this file. The token and every other variable are left alone.
//
// A step inside a job container reaches the node through /__e, from the volume
// the agent fills (externalsFillScript in internal/backend/docker.go). That
// volume is also the externals the runner itself starts its actions with, under
// docker-in-docker, so it keeps this program and the real node beside it: the
// variable is not set in a job's own image and the preload is not mounted there,
// so this passes straight through to node.real. A fill that took this program
// out would take the cache service out with it.
//
// It is a program and not a script because of what is in that environment. An
// action's inputs arrive as INPUT_<NAME> with the hyphens of the name kept, as
// in INPUT_GO-VERSION-FILE, and that is not a name a POSIX shell can hold: dash,
// which is /bin/sh on Debian and Ubuntu, drops such a variable on its way
// through a script. A wrapper written as one made every action with a hyphenated
// input run as if the input had never been given, so setup-go and setup-python
// failed outright and checkout quietly took its defaults. Where this does run in
// a job container's image, which is whatever image the workflow chose, a program
// linked statically needs no shell and no libc there, where a script needs the
// one it names. The image build compiles this with CGO_ENABLED=0.
package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

const (
	envCacheServiceURL = "ZOOMIES_CACHE_SERVICE_URL"
	envCacheServiceV2  = "ACTIONS_CACHE_SERVICE_V2"
	envRedirectFile    = "ZOOMIES_CACHE_REDIRECT_FILE"
	envNodeOptions     = "NODE_OPTIONS"

	// defaultRedirect is where the image puts the preload; the image's Dockerfile
	// and a test hold the two to the same place.
	defaultRedirect = "/usr/local/lib/zoomies/runner-cache-redirect.cjs"
)

func main() {
	// Where this file really is, not what it was called: a step can start node
	// by the bare name it finds on its PATH, and the real one is beside this.
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "node wrapper: cannot tell where it is: %v\n", err)
		os.Exit(127)
	}
	realNode := self + ".real"

	env := os.Environ()
	if redirect, ok := cacheRedirect(); ok {
		options := "--require=" + quoteNodeOption(redirect)
		if existing := os.Getenv(envNodeOptions); existing != "" {
			options += " " + existing
		}
		env = withEnv(env, envNodeOptions, options)
	}

	// exec, not run: the runner reads node's exit status as the step's, and a few
	// actions read their input from standard input, so nothing may stand between.
	argv := append([]string{realNode}, os.Args[1:]...)
	if err := syscall.Exec(realNode, argv, env); err != nil {
		fmt.Fprintf(os.Stderr, "node wrapper: cannot run %s: %v\n", realNode, err)
		os.Exit(127)
	}
}

// cacheRedirect says which preload to load, and whether to load one at all.
func cacheRedirect() (string, bool) {
	if os.Getenv(envCacheServiceURL) == "" || os.Getenv(envCacheServiceV2) == "" {
		return "", false
	}
	redirect := os.Getenv(envRedirectFile)
	if redirect == "" {
		redirect = defaultRedirect
	}
	// Only if it is there: an image built without it must still run its actions.
	f, err := os.Open(redirect)
	if err != nil {
		return "", false
	}
	f.Close()
	return redirect, true
}

// quoteNodeOption makes a value safe inside NODE_OPTIONS, which node splits on
// spaces. A path with a space in it, as a pool's override of the redirect file
// may have, would otherwise be read as a path and a second option, and the
// preload would not load. Node reads a double-quoted run as one token and a
// backslash inside it as an escape; a value with nothing to protect is left as
// it is.
func quoteNodeOption(v string) string {
	if !strings.ContainsAny(v, " \t\"\\'") {
		return v
	}
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}

// withEnv returns env with key set to value, and every other entry untouched,
// names a shell cannot hold included.
func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return append(out, prefix+value)
}

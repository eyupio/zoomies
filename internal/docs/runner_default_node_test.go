package docs

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The node a runner image gives a step that never set one up used to be the
// distribution's package, which is whatever the release froze: Node 18 on
// Ubuntu 24.04, out of support, and refused by current tooling. These tests
// hold the replacement to its terms, because each part can drift on its own:
// the major in the Dockerfile, the lock row it is installed from, and a
// distribution package quietly coming back in front of it.

func readDeployFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../deploy/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTheRunnerImagesDefaultNodeIsASupportedLineTheLockPins(t *testing.T) {
	dockerfile := readDeployFile(t, "Dockerfile.runner")

	m := regexp.MustCompile(`(?m)^ARG DEFAULT_NODE_MAJOR=(\d+)$`).FindStringSubmatch(dockerfile)
	if m == nil {
		t.Fatal("deploy/Dockerfile.runner declares no DEFAULT_NODE_MAJOR, so the image's default node is whatever a distribution package happens to be")
	}
	major, _ := strconv.Atoi(m[1])
	if major < 22 {
		t.Errorf("the default node is %d; the lines before 22 are out of support, and current tooling refuses anything below 20", major)
	}

	// Each architecture needs its own digest, or one of the two builds fails
	// (or, worse, installs without being checked).
	lock := readDeployFile(t, "toolcache.lock")
	rows := map[string]string{}
	for _, line := range strings.Split(lock, "\n") {
		f := strings.Fields(line)
		if len(f) != 6 || f[0] != "node" || !strings.HasPrefix(f[1], m[1]+".") {
			continue
		}
		if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(f[4]) || !strings.HasPrefix(f[5], "https://nodejs.org/dist/") {
			t.Errorf("the lock's row for node %s %s has no sha256 digest or is not from nodejs.org: %q", f[1], f[3], line)
		}
		rows[f[3]] = f[1]
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if rows[arch] == "" {
			t.Errorf("deploy/toolcache.lock has no Node %d row for %s, so the image cannot be built there", major, arch)
		}
	}
}

func TestTheRunnerImageInstallsItsDefaultNodeFromTheLockAndChecksIt(t *testing.T) {
	dockerfile := readDeployFile(t, "Dockerfile.runner")

	// The install belongs to the base stage, which every variant (apt and dnf)
	// builds on, and not to runner-full alone.
	base, _, ok := strings.Cut(dockerfile, "AS runner-docker")
	if !ok {
		t.Fatal("deploy/Dockerfile.runner has no runner-docker stage; this test needs to follow it")
	}
	if !regexp.MustCompile(`runner-toolcache\.sh [^\n]*"\$\{TARGETARCH\}" node "\$\{DEFAULT_NODE_MAJOR\}"`).MatchString(base) {
		t.Error("the base stage does not install the default node from the lock, so only some images would have it")
	}

	// The build must fail on an old node, not publish it.
	if !strings.Contains(base, `process.versions.node.split(".")[0]')" = "${DEFAULT_NODE_MAJOR}"`) {
		t.Error("the build does not check that the node on PATH is the default major")
	}
	// Compared after resolving links, not as spelled: Fedora 42 merged sbin into
	// bin, so /usr/local/sbin is a link to /usr/local/bin and sits first on PATH,
	// and `command -v node` there prints /usr/local/sbin/node for the very file
	// the installer linked. A string comparison failed the 1.4.0 release on both
	// fedora-42 images while every apt image passed.
	if !strings.Contains(base, `test "$(readlink -f "$(command -v node)")" = "$(readlink -f /usr/local/bin/node)"`) {
		t.Error("the build does not check that the node on PATH is the one it installed, by resolved path")
	}

	// A distribution nodejs would be a second, older node behind the one
	// installed above, and the first thing a fleet's PATH change would expose.
	toolchain := readDeployFile(t, "runner-toolchain.sh")
	for _, line := range strings.Split(toolchain, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if regexp.MustCompile(`\bnodejs\b`).MatchString(line) {
			t.Errorf("deploy/runner-toolchain.sh installs a distribution node again: %q", line)
		}
	}
}

// npm's global prefix is two directories above the node it runs under, so the
// default node needs a setting of its own or `npm install -g` would leave its
// tools in a directory nothing has on PATH. Where that setting lives matters:
// a tool-cache entry is setup-node's, reused by a job that asks for the same
// version and expected to be runner-owned, so a prefix of /usr/local there
// would make an unprivileged global install fail with a permissions error.
func TestTheRunnerImagesDefaultNodeLivesOutsideTheToolCacheWithItsOwnPrefix(t *testing.T) {
	script := readDeployFile(t, "runner-toolcache.sh")

	if !strings.Contains(script, `default_root="${DEFAULT_NODE_ROOT:-/opt/default-node}"`) {
		t.Error("the default node is not given a tree of its own outside the tool cache")
	}
	if !strings.Contains(script, `tar -xzf "${archive}" -C "${default_root}" --strip-components=1`) {
		t.Error("the default node is not unpacked into its own tree")
	}
	if !strings.Contains(script, `echo "prefix=/usr/local" > "${default_root}/etc/npmrc"`) {
		t.Error("the default node's own global npm config does not set the prefix to /usr/local, so `npm install -g` would leave its tools off PATH")
	}

	// No cache entry may be given an npm config, which the plain mode used by
	// the runner-full layers writes into.
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, "npmrc") && strings.Contains(line, "${toolcache}") {
			t.Errorf("deploy/runner-toolcache.sh writes an npm config into a tool cache entry: %q", line)
		}
	}

	// An environment variable would reach every npm, including the one a
	// setup-node step installs.
	if regexp.MustCompile(`(?m)^\s*(ENV|ARG)\s+NPM_CONFIG_PREFIX`).MatchString(readDeployFile(t, "Dockerfile.runner")) {
		t.Error("deploy/Dockerfile.runner sets NPM_CONFIG_PREFIX, which applies to every npm including the one setup-node installs")
	}
}

package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/naming"
)

// The deployment package, from the repository root.
const (
	marketplaceDir = "../../deploy/marketplace"
	releaseEnvFile = "release.env"
	imagesLockFile = "images.lock"
)

// TestTheDeploymentPackagePinsOneReleaseEverywhere is the property a
// marketplace artefact lives or dies by.
//
// The person deploying it is not reading this repository: they click a button
// months after the release was cut, and whatever the artefact names is what
// they run. One reference left floating -- or left behind on the previous
// release when the others moved -- produces a deployment nobody has tested, and
// no error message anywhere says so.
func TestTheDeploymentPackagePinsOneReleaseEverywhere(t *testing.T) {
	env := readReleaseEnv(t)
	release := env["ZOOMIES_RELEASE"]
	if release == "" {
		t.Fatal("release.env names no ZOOMIES_RELEASE")
	}

	controller := env["ZOOMIES_CONTROLLER_IMAGE"]
	if !strings.Contains(controller, ":"+release+"@sha256:") {
		t.Errorf("the controller image should carry both the %s tag and a digest, got %q", release, controller)
	}

	// An agent newer than its controller is unsupported, and a marketplace
	// deployment is where that happens by accident: the controller is pinned
	// and the join line is copied long afterwards.
	if got := env["ZOOMIES_AGENT_VERSION"]; got != release {
		t.Errorf("the agent version is %q and the release is %q; the join line a pinned controller prints must not install a newer agent", got, release)
	}

	for ref := range readImagesLock(t) {
		if !strings.Contains(ref, release) {
			t.Errorf("images.lock pins %q, which is not part of release %s", ref, release)
		}
	}
}

// TestEveryPublishedRunnerVariantIsLocked keeps the package honest when the
// catalogue grows.
//
// Adding an operating system is a row in internal/naming and `make generate`.
// Nothing in that path reaches this directory, so without this test a new
// variant would be published, resolved by a pool, and absent from the record
// the deployment package offers an auditor -- which is the one document that
// claims to list everything the deployment runs.
func TestEveryPublishedRunnerVariantIsLocked(t *testing.T) {
	env := readReleaseEnv(t)
	lock := readImagesLock(t)
	release := env["ZOOMIES_RELEASE"]

	for _, img := range naming.Images() {
		for _, repo := range []string{env["ZOOMIES_RUNNER_REPO"], env["ZOOMIES_RUNNER_DOCKER_REPO"]} {
			ref := repo + ":" + img.Tag() + "-" + release
			if _, ok := lock[ref]; !ok {
				t.Errorf("images.lock does not pin %s; run `make marketplace-lock`", ref)
			}
		}
	}

	// The controller is the reference the bootstrap actually deploys, so a
	// lock that omits it records everything except the thing being installed.
	controller := env["ZOOMIES_CONTROLLER_IMAGE"]
	tagged, digest, _ := strings.Cut(controller, "@")
	locked, ok := lock[tagged]
	if !ok {
		t.Fatalf("images.lock does not pin the controller image %s", tagged)
	}
	if locked != digest {
		t.Errorf("release.env pins the controller at %s and images.lock records %s; one of them is stale", digest, locked)
	}
}

// readReleaseEnv reads the KEY=value lines the bootstrap sources. It is not a
// shell, on purpose: a value that needed expansion to be understood here would
// be a trap in the bootstrap too.
func readReleaseEnv(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(marketplaceDir, releaseEnvFile))
	if err != nil {
		t.Fatalf("reading the release contract: %v", err)
	}
	out := map[string]string{}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("%s line %d is not KEY=value: %q", releaseEnvFile, i+1, line)
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

// readImagesLock reads the generated reference-to-digest record.
func readImagesLock(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(marketplaceDir, imagesLockFile))
	if err != nil {
		t.Fatalf("reading the image lock: %v", err)
	}
	out := map[string]string{}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[1], "sha256:") {
			t.Fatalf("%s line %d is not a reference and a digest: %q", imagesLockFile, i+1, line)
		}
		out[fields[0]] = fields[1]
	}
	return out
}

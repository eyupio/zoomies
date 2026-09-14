package docs

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eyupio/zoomies/internal/installer"
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

// TestTheBootstrapWritesAnAnswerFileSetupAccepts runs the rendering half of the
// real first-boot script and asks the installer's own validator whether what it
// produced is enough to install without prompting.
//
// This is the failure the package exists to prevent and the hardest one to
// notice: an answer file missing one key does not stop the boot, it stops
// `zoomies init` halfway, and what the operator gets is an instance that
// answered its provider's health check and has no controller on it. Checking it
// by reimplementing the substitution here would only prove this test agrees
// with itself, so the script is run.
func TestTheBootstrapWritesAnAnswerFileSetupAccepts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the bootstrap is a POSIX shell script; it is rendered and run on Unix")
	}
	dir := t.TempDir()
	answers := filepath.Join(dir, "answers.yaml")

	env := readReleaseEnv(t)
	envFile := filepath.Join(dir, "marketplace.env")
	writeFile(t, envFile, strings.Join([]string{
		"ZOOMIES_RELEASE=" + env["ZOOMIES_RELEASE"],
		"ZOOMIES_INSTALLER_URL=" + env["ZOOMIES_INSTALLER_URL"],
		"ZOOMIES_INSTALLER_SHA256=" + env["ZOOMIES_INSTALLER_SHA256"],
		"ZOOMIES_CONTROLLER_IMAGE=" + env["ZOOMIES_CONTROLLER_IMAGE"],
		"ZOOMIES_HOSTNAME=zoomies.example.com",
		// Left out on purpose: the external URL, the publish address and the
		// trusted proxies are all defaulted by the script, and an inputs file
		// that names only the hostname is the smallest one a provider's form
		// can produce.
	}, "\n")+"\n")

	for _, mode := range []installer.Mode{installer.ModeSingle, installer.ModeController} {
		t.Run(string(mode), func(t *testing.T) {
			cmd := exec.Command("sh", filepath.Join(marketplaceDir, "bootstrap.sh"))
			cmd.Env = append(os.Environ(),
				"ZOOMIES_RENDER_ONLY=1",
				"ZOOMIES_ENV_FILE="+envFile,
				"ZOOMIES_ANSWERS_TEMPLATE="+filepath.Join(marketplaceDir, "answers.yaml.tmpl"),
				"ZOOMIES_ANSWERS_FILE="+answers,
				"ZOOMIES_MODE="+string(mode),
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("the bootstrap refused to render: %v\n%s", err, out)
			}

			raw, err := os.ReadFile(answers)
			if err != nil {
				t.Fatalf("reading what the bootstrap wrote: %v", err)
			}
			var parsed installer.Answers
			if err := yaml.Unmarshal(raw, &parsed); err != nil {
				t.Fatalf("the bootstrap wrote YAML the installer cannot read: %v\n%s", err, raw)
			}
			if err := parsed.Validate(mode); err != nil {
				t.Errorf("an unattended install would stop here:\n%v", err)
			}
			if parsed.Mode != string(mode) {
				t.Errorf("the answer file says mode %q for a %s install", parsed.Mode, mode)
			}
			// A compose deployment is what makes the browser-side
			// administrator bootstrap the supported path: the installer never
			// opens the database, so it never needs a password to put in it.
			if parsed.Deployment != "compose" {
				t.Errorf("the answer file deploys %q; the setup-token bootstrap needs a containerised deployment", parsed.Deployment)
			}
		})
	}
}

// TestTheDeploymentPackageCarriesNoSecret is the property that decides whether
// this package may be handed to a provider at all.
//
// Everything rendered here becomes instance metadata: readable by anything on
// the instance that can reach the metadata service, kept in the provider's own
// database, and printed in cloud-init's log. A credential that arrives that way
// cannot be rotated out of the places it has already been copied to.
func TestTheDeploymentPackageCarriesNoSecret(t *testing.T) {
	// Each of these is a real credential the interactive installer can be
	// given, and each would work perfectly if it were put here -- which is what
	// makes them the ones to name.
	forbidden := []string{
		"private_key_file",
		"webhook_secret",
		"app_id",
		"installation_id",
		"password",
		"encryption_key",
		"join_token",
	}
	for _, name := range []string{"answers.yaml.tmpl", "cloud-init.yaml.tmpl", "inputs.env.example"} {
		body := strings.ToLower(readPackageFile(t, name))
		for _, key := range forbidden {
			// A line that only explains why the key is absent is the point of
			// the file, so the test looks for it being set rather than named.
			for _, setting := range []string{key + ": ", key + "="} {
				if strings.Contains(body, setting) {
					t.Errorf("%s sets %q; a marketplace artefact must not carry a credential", name, key)
				}
			}
		}
	}
}

// TestTheBootstrapAndTheInstallerAgreeOnTheData keeps two halves of one
// decision together.
//
// The bootstrap pre-creates the volume that holds the database so that a
// provider's attached disk can back it. It has to name the volume the installer
// will later mount, and chown the directory to the account the published image
// runs as -- both of which live in internal/installer. Neither would fail
// loudly: a different volume name silently gives the instance an empty
// database on a disk nobody is backing up.
func TestTheBootstrapAndTheInstallerAgreeOnTheData(t *testing.T) {
	script := readPackageFile(t, "bootstrap.sh")
	if !strings.Contains(script, installer.VolumeName) {
		t.Errorf("the bootstrap does not name the %q volume the installer mounts", installer.VolumeName)
	}
	owner := strconv.Itoa(installer.ImageUID) + ":" + strconv.Itoa(installer.ImageUID)
	if !strings.Contains(script, owner) {
		t.Errorf("the bootstrap does not chown the data directory to %s, the uid the published image runs as", owner)
	}
}

// TestEveryPlaceholderInTheAnswerTemplateIsFilledIn catches the template and
// the script drifting apart.
//
// A placeholder added to the template and not to the script reaches
// `zoomies init` as a literal, where it is a setting with a strange value
// rather than a missing one -- so it is refused for the wrong reason, or worse,
// accepted.
func TestEveryPlaceholderInTheAnswerTemplateIsFilledIn(t *testing.T) {
	tmpl := readPackageFile(t, "answers.yaml.tmpl")
	script := readPackageFile(t, "bootstrap.sh")

	seen := map[string]bool{}
	for _, m := range placeholderRE.FindAllString(tmpl, -1) {
		if seen[m] {
			continue
		}
		seen[m] = true
		if !strings.Contains(script, "s|"+m+"|") {
			t.Errorf("the answer template uses %s and the bootstrap substitutes nothing for it", m)
		}
	}
	if len(seen) == 0 {
		t.Error("the answer template has no placeholders at all, which cannot be right")
	}
}

var placeholderRE = regexp.MustCompile(`__ZOOMIES_[A-Z0-9_]+__`)

func readPackageFile(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(marketplaceDir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(raw)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

package docs

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eyupio/zoomies/internal/config"
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

	// Zoomies' own images all move together; the proxy is somebody else's
	// software on its own release cycle, so it is pinned by digest and not
	// expected to carry ours.
	for ref := range readImagesLock(t) {
		if !strings.Contains(ref, "eyupio/") {
			continue
		}
		if !strings.Contains(ref, release) {
			t.Errorf("images.lock pins %q, which is not part of release %s", ref, release)
		}
	}

	// Anything this package runs in front of the controller sees every request
	// and holds the certificate, so it is pinned as hard as the controller is.
	if proxy := env["ZOOMIES_PROXY_IMAGE"]; !strings.Contains(proxy, "@sha256:") {
		t.Errorf("the proxy image %q is not pinned by digest", proxy)
	}
}

// addedAfter names the catalogue rows added since the release the package is
// pinned to, with that release. Such a row has no published image yet, so
// there is nothing `make marketplace-lock` could pin. The exemption is keyed
// to the release rather than open-ended: the release after it publishes the
// images, and once release.env moves on, the row is checked like any other --
// so an entry left here cannot hide a missing pin for longer than one release.
var addedAfter = map[string]string{
	"ubuntu-2604": "v1.3.0",
	"debian-13":   "v1.3.0",
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
		if predates, ok := addedAfter[img.Tag()]; ok && predates == release {
			t.Logf("%s is newer than the pinned release %s, so it has no image to lock yet", img.Tag(), release)
			continue
		}
		for _, repo := range []string{env["ZOOMIES_RUNNER_REPO"], env["ZOOMIES_RUNNER_DOCKER_REPO"]} {
			ref := repo + ":" + img.Tag() + "-" + release
			if _, ok := lock[ref]; !ok {
				t.Errorf("images.lock does not pin %s; run `make marketplace-lock`", ref)
			}
		}
	}

	// The images the bootstrap actually deploys are the ones a lock that
	// omitted them would be silent about, so each is checked against its own
	// pin rather than merely being present.
	for _, key := range []string{"ZOOMIES_CONTROLLER_IMAGE", "ZOOMIES_PROXY_IMAGE"} {
		tagged, digest, ok := strings.Cut(env[key], "@")
		if !ok {
			t.Errorf("%s is not pinned by digest: %q", key, env[key])
			continue
		}
		locked, ok := lock[tagged]
		if !ok {
			t.Errorf("images.lock does not pin %s (%s)", key, tagged)
			continue
		}
		if locked != digest {
			t.Errorf("release.env pins %s at %s and images.lock records %s; one of them is stale", key, digest, locked)
		}
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
	for _, mode := range []installer.Mode{installer.ModeSingle, installer.ModeController} {
		t.Run(string(mode), func(t *testing.T) {
			// Only the hostname is set: the external URL, the publish address
			// and the trusted proxies are all defaulted by the script, and an
			// inputs file naming the hostname alone is the smallest one a
			// provider's form can produce.
			parsed := renderAnswers(t, t.TempDir(), map[string]string{
				"ZOOMIES_HOSTNAME": "zoomies.example.com",
				"ZOOMIES_MODE":     string(mode),
			})
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

// TestEveryTLSArrangementIsAnsweredCompletely covers the three ways a
// deployment can end up with a certificate, and the one thing they must agree
// on: that no arrangement leaves the controller serving plain HTTP to the
// internet.
//
// The two halves that can disagree are the listener and who holds the
// certificate. Getting that pair wrong does not fail: the instance comes up,
// answers its provider's health check, and is either unreachable or exposed.
func TestEveryTLSArrangementIsAnsweredCompletely(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the bootstrap is a POSIX shell script; it is rendered and run on Unix")
	}
	cases := []struct {
		tls           string
		extra         map[string]string
		wantBind      string
		wantMode      string
		wantProxy     string
		wantPlainHTTP bool
	}{
		// A certificate of our own is the only arrangement where Zoomies
		// itself is the public endpoint, and the only one where it
		// terminates TLS.
		{tls: "files", wantBind: "0.0.0.0:443", wantMode: "files", wantProxy: "127.0.0.1/32", extra: map[string]string{
			"ZOOMIES_TLS_CERT_FILE": "/etc/zoomies/tls/fullchain.pem",
			"ZOOMIES_TLS_KEY_FILE":  "/etc/zoomies/tls/privkey.pem",
		}},
		// Cloudflare's edge connects from outside to a published origin over
		// plain HTTP, so the origin is exposed and the peer is Cloudflare --
		// which is what makes CF-Connecting-IP, the header a client cannot
		// forge, count. Trusting loopback here would believe nothing that ever
		// connects.
		{tls: "cloudflare", wantBind: "0.0.0.0:80", wantMode: "off", wantProxy: config.TrustedProxyCloudflare, wantPlainHTTP: true},
		// A tunnel is the other Cloudflare shape and wants the opposite
		// answer: the daemon runs here, so the peer is loopback and
		// CF-Connecting-IP is deliberately not believed from it.
		{tls: "tunnel", wantBind: "127.0.0.1:8080", wantMode: "off", wantProxy: "127.0.0.1/32", wantPlainHTTP: true},
		{tls: "acme", wantBind: "127.0.0.1:8080", wantMode: "off", wantProxy: "127.0.0.1/32"},
		{tls: "off", wantBind: "127.0.0.1:8080", wantMode: "off", wantProxy: "127.0.0.1/32", wantPlainHTTP: true},
	}
	for _, tc := range cases {
		t.Run(tc.tls, func(t *testing.T) {
			inputs := map[string]string{
				"ZOOMIES_HOSTNAME": "zoomies.example.com",
				"ZOOMIES_TLS":      tc.tls,
			}
			for k, v := range tc.extra {
				inputs[k] = v
			}
			parsed := renderAnswers(t, t.TempDir(), inputs)
			if err := parsed.Validate(installer.ModeSingle); err != nil {
				t.Errorf("an unattended install would stop here:\n%v", err)
			}
			if parsed.Bind != tc.wantBind {
				t.Errorf("TLS %s published %q, want %q", tc.tls, parsed.Bind, tc.wantBind)
			}
			if parsed.TLS.Mode != tc.wantMode {
				t.Errorf("TLS %s set the listener's mode to %q, want %q", tc.tls, parsed.TLS.Mode, tc.wantMode)
			}
			if parsed.ExternalURL == "" || !strings.HasPrefix(parsed.ExternalURL, "https://") {
				t.Errorf("TLS %s gave GitHub the external URL %q; a webhook is not delivered to anything else", tc.tls, parsed.ExternalURL)
			}
			// Which proxy is believed is the half that fails silently:
			// wrong, every audit row records the proxy instead of the person
			// and the rate limiter throttles the internet as one caller.
			if !slices.Contains(parsed.TrustedProxies, tc.wantProxy) {
				t.Errorf("TLS %s trusts %v, which does not include %q", tc.tls, parsed.TrustedProxies, tc.wantProxy)
			}
			// Three of the five leave the origin speaking plain HTTP, and that
			// is correct rather than a lapse: the origin is not the public
			// endpoint. What must never happen is the public URL being plain.
			if tc.wantPlainHTTP && parsed.TLS.Mode != "off" {
				t.Errorf("TLS %s made the origin terminate TLS; something in front of it holds the certificate", tc.tls)
			}
		})
	}
}

// TestTheBootstrapRefusesAnIncompleteCertificate: files without a certificate
// is the arrangement that would otherwise fail last -- after the install, on
// the first request, with nothing in the answer file that looks wrong.
func TestTheBootstrapRefusesAnIncompleteCertificate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the bootstrap is a POSIX shell script; it is rendered and run on Unix")
	}
	dir := t.TempDir()
	out, err := runBootstrap(t, dir, map[string]string{
		"ZOOMIES_HOSTNAME": "zoomies.example.com",
		"ZOOMIES_TLS":      "files",
	})
	if err == nil {
		t.Fatalf("the bootstrap rendered an answer file for a certificate it was never given:\n%s", out)
	}
	if !strings.Contains(out, "ZOOMIES_TLS_CERT_FILE") {
		t.Errorf("the refusal does not name the input to set:\n%s", out)
	}
}

// renderAnswers runs the real bootstrap's rendering half over one set of
// instance settings and returns what `zoomies init` would be given.
func renderAnswers(t *testing.T, dir string, inputs map[string]string) installer.Answers {
	t.Helper()
	out, err := runBootstrap(t, dir, inputs)
	if err != nil {
		t.Fatalf("the bootstrap refused to render: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "answers.yaml"))
	if err != nil {
		t.Fatalf("reading what the bootstrap wrote: %v", err)
	}
	var parsed installer.Answers
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("the bootstrap wrote YAML the installer cannot read: %v\n%s", err, raw)
	}
	return parsed
}

// runBootstrap writes the settings an instance would have been rendered with
// and runs the script far enough to see what it decided.
func runBootstrap(t *testing.T, dir string, inputs map[string]string) (string, error) {
	t.Helper()
	env := readReleaseEnv(t)
	lines := []string{
		"ZOOMIES_RELEASE=" + env["ZOOMIES_RELEASE"],
		"ZOOMIES_INSTALLER_URL=" + env["ZOOMIES_INSTALLER_URL"],
		"ZOOMIES_INSTALLER_SHA256=" + env["ZOOMIES_INSTALLER_SHA256"],
		"ZOOMIES_CONTROLLER_IMAGE=" + env["ZOOMIES_CONTROLLER_IMAGE"],
		"ZOOMIES_PROXY_IMAGE=" + env["ZOOMIES_PROXY_IMAGE"],
		"ZOOMIES_TUNNEL_IMAGE=" + env["ZOOMIES_TUNNEL_IMAGE"],
	}
	for _, k := range sortedKeys(inputs) {
		lines = append(lines, k+"="+inputs[k])
	}
	envFile := filepath.Join(dir, "marketplace.env")
	writeFile(t, envFile, strings.Join(lines, "\n")+"\n")

	cmd := exec.Command("sh", filepath.Join(marketplaceDir, "bootstrap.sh"))
	cmd.Env = append(os.Environ(),
		"ZOOMIES_RENDER_ONLY=1",
		"ZOOMIES_ENV_FILE="+envFile,
		"ZOOMIES_ANSWERS_TEMPLATE="+filepath.Join(marketplaceDir, "answers.yaml.tmpl"),
		"ZOOMIES_ANSWERS_FILE="+filepath.Join(dir, "answers.yaml"),
		"ZOOMIES_CADDY_TEMPLATE="+filepath.Join(marketplaceDir, "Caddyfile.tmpl"),
		"ZOOMIES_PROXY_TEMPLATE="+filepath.Join(marketplaceDir, "proxy-compose.yml.tmpl"),
		"ZOOMIES_TUNNEL_TEMPLATE="+filepath.Join(marketplaceDir, "tunnel-compose.yml.tmpl"),
		"ZOOMIES_PROXY_DIR="+filepath.Join(dir, "proxy"),
		"ZOOMIES_TUNNEL_DIR="+filepath.Join(dir, "tunnel"),
		"ZOOMIES_DATA_DIR="+filepath.Join(dir, "data"),
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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

// TestTheRenderedCloudConfigCarriesTheWholePackage checks the artefact a
// provider actually boots.
//
// Rendering is where the package stops being several files in a repository and
// becomes one thing pasted into somebody's console. A file left out of it does
// not fail here -- it fails on an instance, at boot, with a script looking for
// a template that was never written.
func TestTheRenderedCloudConfigCarriesTheWholePackage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("render.sh is a POSIX shell script")
	}
	dir := t.TempDir()
	inputs := filepath.Join(dir, "inputs.env")
	writeFile(t, inputs, "ZOOMIES_HOSTNAME=zoomies.example.com\n")

	cmd := exec.Command("sh", filepath.Join(marketplaceDir, "render.sh"), inputs)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("render.sh refused: %v", err)
	}

	var config struct {
		WriteFiles []struct {
			Path        string `yaml:"path"`
			Permissions string `yaml:"permissions"`
			Content     string `yaml:"content"`
		} `yaml:"write_files"`
		RunCmd [][]string `yaml:"runcmd"`
	}
	if err := yaml.Unmarshal(out, &config); err != nil {
		t.Fatalf("render.sh produced something cloud-init cannot read: %v", err)
	}

	want := map[string]string{
		// The settings are mode 0600 because they are root's configuration.
		// The bootstrap is 0700 because it is the only thing that runs.
		"/etc/zoomies/marketplace.env":         "0600",
		"/etc/zoomies/answers.yaml.tmpl":       "0600",
		"/etc/zoomies/Caddyfile.tmpl":          "0644",
		"/etc/zoomies/proxy-compose.yml.tmpl":  "0644",
		"/etc/zoomies/tunnel-compose.yml.tmpl": "0644",
		"/usr/local/sbin/zoomies-bootstrap":    "0700",
	}
	got := map[string]string{}
	for _, f := range config.WriteFiles {
		got[f.Path] = f.Permissions
		if strings.TrimSpace(f.Content) == "" {
			t.Errorf("%s was rendered empty", f.Path)
		}
	}
	for path, perm := range want {
		switch actual, ok := got[path]; {
		case !ok:
			t.Errorf("the rendered cloud-config does not write %s", path)
		case actual != perm:
			t.Errorf("%s is written %s, want %s", path, actual, perm)
		}
	}
	if len(config.RunCmd) != 1 {
		t.Errorf("the rendered cloud-config runs %d commands; one failing step should be one exit status", len(config.RunCmd))
	}

	// The templates keep their own placeholders on purpose -- those are
	// substituted on the instance, by the bootstrap. What must not survive is
	// one of cloud-init's own block placeholders: that is a file render.sh was
	// supposed to embed and did not, and the instance would look for it at
	// boot and find nothing.
	for _, block := range blockPlaceholders(t) {
		if strings.Contains(string(out), block) {
			t.Errorf("render.sh left %s unfilled, so the instance would boot without that file", block)
		}
	}
}

// blockPlaceholders are the lines of cloud-init.yaml.tmpl that are nothing but
// a placeholder: each one is a whole file render.sh has to embed.
func blockPlaceholders(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(readPackageFile(t, "cloud-init.yaml.tmpl"), "\n") {
		if trimmed := strings.TrimSpace(line); placeholderRE.FindString(trimmed) == trimmed && trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		t.Fatal("cloud-init.yaml.tmpl embeds no files at all, which cannot be right")
	}
	return out
}

// TestTheTunnelTokenStaysOutOfTheControllersConfiguration covers the one
// credential this package is willing to carry.
//
// A tunnel token is allowed where a GitHub App key is not: it names one tunnel
// and is rotated by deleting that tunnel. Allowed is not the same as loose --
// it belongs to the daemon that dials out, and it must not end up in the answer
// file, which is world-readable configuration that setup echoes and that an
// operator is told to read when wondering what was decided.
func TestTheTunnelTokenStaysOutOfTheControllersConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the bootstrap is a POSIX shell script; it is rendered and run on Unix")
	}
	const token = "eyJhIjoiZXhhbXBsZS10b2tlbiJ9"
	dir := t.TempDir()
	if _, err := runBootstrap(t, dir, map[string]string{
		"ZOOMIES_HOSTNAME":     "zoomies.example.com",
		"ZOOMIES_TLS":          "tunnel",
		"ZOOMIES_TUNNEL_TOKEN": token,
	}); err != nil {
		t.Fatalf("the bootstrap refused to render a tunnel: %v", err)
	}

	answers, err := os.ReadFile(filepath.Join(dir, "answers.yaml"))
	if err != nil {
		t.Fatalf("reading the answer file: %v", err)
	}
	if strings.Contains(string(answers), token) {
		t.Error("the tunnel token reached the answer file, which is not where it belongs")
	}

	// It belongs here, and only a root-readable file will do: compose reads it
	// from beside its own file, which is what keeps the compose file itself
	// safe to copy.
	envPath := filepath.Join(dir, "tunnel", ".env")
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("the bootstrap wrote no tunnel environment: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the tunnel token is written %04o, want 0600", perm)
	}
	written, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("reading the tunnel environment: %v", err)
	}
	if !strings.Contains(string(written), token) {
		t.Error("the tunnel token did not reach the daemon that needs it")
	}
	compose, err := os.ReadFile(filepath.Join(dir, "tunnel", "docker-compose.yml"))
	if err != nil {
		t.Fatalf("reading the tunnel compose file: %v", err)
	}
	if strings.Contains(string(compose), token) {
		t.Error("the tunnel token was written into the compose file, which is meant to stay copyable")
	}
}

// TestATunnelRenderedWithoutATokenIsStillReadyForOne is the shape the docs
// recommend: the instance boots complete, and the token is pasted over SSH so
// that it never passes through instance metadata at all.
//
// That is only worth recommending if it actually works, which means the compose
// file has to exist and the placeholder has to be there to fill in.
func TestATunnelRenderedWithoutATokenIsStillReadyForOne(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the bootstrap is a POSIX shell script; it is rendered and run on Unix")
	}
	dir := t.TempDir()
	if _, err := runBootstrap(t, dir, map[string]string{
		"ZOOMIES_HOSTNAME": "zoomies.example.com",
		"ZOOMIES_TLS":      "tunnel",
	}); err != nil {
		t.Fatalf("the bootstrap refused to render a tunnel without a token: %v", err)
	}
	for _, name := range []string{"docker-compose.yml", ".env"} {
		if _, err := os.Stat(filepath.Join(dir, "tunnel", name)); err != nil {
			t.Errorf("a tunnel rendered without a token is missing %s: %v", name, err)
		}
	}
	env, err := os.ReadFile(filepath.Join(dir, "tunnel", ".env"))
	if err != nil {
		t.Fatalf("reading the tunnel environment: %v", err)
	}
	if !strings.Contains(string(env), "TUNNEL_TOKEN=") {
		t.Error("the tunnel environment has no line to paste a token into")
	}
}

package installer

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// unattendedInstaller is the installer these tests want: no terminal, no
// host detection worth speaking of, and nowhere for its output to go.
func unattendedInstaller(t *testing.T, opts Options) *Installer {
	t.Helper()
	i, _ := newTestInstaller(t, Detection{OS: "linux", Arch: "amd64"}, opts)
	return i
}

// An operator who says where the configuration and state go is obeyed. The
// overrides exist so a test -- and an operator packaging Zoomies somewhere
// unusual -- never writes to the platform default by accident.
func TestExplicitDirectoriesWinOverThePlatformDefaults(t *testing.T) {
	opts := Options{ConfigDir: t.TempDir(), StateDir: t.TempDir()}
	if got := opts.configDir(); got != opts.ConfigDir {
		t.Errorf("configDir() = %q, want the override %q", got, opts.ConfigDir)
	}
	if got := opts.stateDir(); got != opts.StateDir {
		t.Errorf("stateDir() = %q, want the override %q", got, opts.StateDir)
	}

	// With no override the platform's own answer is used, whatever it is on
	// the host running this test.
	bare := Options{}
	if got, want := bare.configDir(), config.ConfigDir(); got != want {
		t.Errorf("configDir() = %q, want the platform default %q", got, want)
	}
	if got, want := bare.stateDir(), config.StateDir(); got != want {
		t.Errorf("stateDir() = %q, want the platform default %q", got, want)
	}
}

// The unit file names a binary, so the path has to be one that still exists
// after the installer exits -- never the temporary copy install.sh ran.
func TestTheInstalledBinaryPathIsTakenFromTheScriptWhenItSaysSo(t *testing.T) {
	opts := Options{InstalledBinary: "/opt/zoomies/bin/zoomies"}
	if got := opts.binaryPath(); got != opts.InstalledBinary {
		t.Errorf("binaryPath() = %q, want %q", got, opts.InstalledBinary)
	}

	// Without one it falls back to this process, resolved through any
	// symlink: a unit pointing at /usr/local/bin/zoomies -> ../releases/x
	// would otherwise break the next time that link moved.
	got := (Options{}).binaryPath()
	if got == "" || !filepath.IsAbs(got) {
		t.Fatalf("binaryPath() = %q, want an absolute path to fall back on", got)
	}
	if resolved, err := filepath.EvalSymlinks(got); err == nil && resolved != got {
		t.Errorf("binaryPath() = %q, want the symlink resolved to %q", got, resolved)
	}
}

// A run with nobody watching must not prompt, whatever the terminal says.
func TestANonInteractiveRunNeverPrompts(t *testing.T) {
	if (Options{NonInteractive: true}).interactive() {
		t.Error("a run told not to prompt reported itself interactive")
	}
	if isTerminal(nil) {
		t.Error("a missing file was called a terminal")
	}
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	defer func() { _ = f.Close() }()
	if !isTerminal(f) {
		t.Errorf("%s is a character device and should read as a terminal", os.DevNull)
	}
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatalf("opening a directory: %v", err)
	}
	defer func() { _ = dir.Close() }()
	if isTerminal(dir) {
		t.Error("a directory was called a terminal")
	}
}

// What is being installed is settled without asking when the answer is
// already there, and the order matters: install.sh's --mode is the operator
// typing at the shell, so it wins over a file they wrote weeks ago.
func TestTheModeComesFromTheFlagThenTheAnswerFileThenTheDefault(t *testing.T) {
	answers := filepath.Join(t.TempDir(), "answers.yaml")
	if err := os.WriteFile(answers, []byte("mode: controller\n"), 0o600); err != nil {
		t.Fatalf("writing answers: %v", err)
	}

	cases := []struct {
		name string
		opts Options
		want Mode
	}{
		{"the flag wins", Options{Mode: ModeAgent, AnswersFile: answers}, ModeAgent},
		{"then the answer file", Options{AnswersFile: answers}, ModeController},
		{"and a run with nothing to go on installs the usual single host", Options{}, ModeSingle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := unattendedInstaller(t, tc.opts).resolveMode(context.Background())
			if err != nil {
				t.Fatalf("resolveMode: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A typo in the answer file stops the run with the word that was not
// understood, rather than installing something the operator did not ask for.
func TestAnUnknownModeInTheAnswerFileIsRefusedByName(t *testing.T) {
	answers := filepath.Join(t.TempDir(), "answers.yaml")
	if err := os.WriteFile(answers, []byte("mode: controler\n"), 0o600); err != nil {
		t.Fatalf("writing answers: %v", err)
	}
	_, err := unattendedInstaller(t, Options{AnswersFile: answers}).resolveMode(context.Background())
	if err == nil {
		t.Fatal("a misspelled mode was accepted")
	}
	if !strings.Contains(err.Error(), "controler") {
		t.Errorf("error = %q, want it to name the word it could not read", err)
	}
}

// Every listener the plan can hold is one the operator was offered. A choice
// missing from the menu is a setting reachable only from an answer file, and
// an option whose apply does something else is a lie told at the prompt.
func TestEveryListenerChoiceIsOfferedAndDoesWhatItsRowSays(t *testing.T) {
	opts := listenOptions()
	for _, c := range []ListenChoice{ListenLoopback, ListenTLSFiles, ListenSelfSigned, ListenProxy, ListenCloudflare} {
		if !slices.ContainsFunc(opts, func(o listenOption) bool { return o.Choice == c }) {
			t.Errorf("%q is a listener the plan can hold but the operator is never offered", c)
		}
	}

	for _, o := range opts {
		t.Run(string(o.Choice), func(t *testing.T) {
			if strings.TrimSpace(o.Label) == "" || strings.TrimSpace(o.Description) == "" {
				t.Fatalf("%q is offered with nothing to read: label %q, description %q", o.Choice, o.Label, o.Description)
			}
			p := Plan{}
			o.Choice.apply(&p, 8443, "build-01")
			if p.Listen != o.Choice {
				t.Errorf("applying %q left the plan on %q", o.Choice, p.Listen)
			}
			if !strings.HasSuffix(p.Bind, ":8443") {
				t.Errorf("bind = %q, want the port the operator chose", p.Bind)
			}
			loopback := strings.HasPrefix(p.Bind, "127.0.0.1:")
			if (o.Choice == ListenLoopback) != loopback {
				t.Errorf("bind = %q for %q; only the loopback choice keeps it off every other interface", p.Bind, o.Choice)
			}
			// The two choices that terminate TLS here are the two that say so.
			wantTLS := o.Choice == ListenTLSFiles || o.Choice == ListenSelfSigned
			if gotTLS := p.TLSMode != config.TLSOff; gotTLS != wantTLS {
				t.Errorf("TLS mode = %q for %q, want TLS terminated here = %v", p.TLSMode, o.Choice, wantTLS)
			}
		})
	}
}

// Cloudflare's edge ranges are trusted for the operator who picks it, because
// a proxy nobody trusts records its own address in every audit entry.
func TestPickingCloudflareTrustsItsEdgeRanges(t *testing.T) {
	p := Plan{}
	ListenCloudflare.apply(&p, 8080, "build-01")
	if !slices.Contains(p.TrustedProxies, config.TrustedProxyCloudflare) {
		t.Errorf("trusted proxies = %v, want %q among them", p.TrustedProxies, config.TrustedProxyCloudflare)
	}

	// And picking the generic proxy does not: those ranges belong to somebody
	// else's network, and trusting them there would let a client spoof its
	// own address.
	plain := Plan{}
	ListenProxy.apply(&plain, 8080, "build-01")
	if slices.Contains(plain.TrustedProxies, config.TrustedProxyCloudflare) {
		t.Errorf("trusted proxies = %v for a plain reverse proxy, want Cloudflare's ranges left out", plain.TrustedProxies)
	}
}

// A self-signed certificate is issued for this host's name, so the operator
// is not asked for something the installer already knows.
func TestASelfSignedCertificateCoversTheHostnameItWasGiven(t *testing.T) {
	p := Plan{}
	ListenSelfSigned.apply(&p, 8080, "build-01")
	if !slices.Contains(p.TLSHosts, "build-01") {
		t.Errorf("TLS hosts = %v, want the host's own name", p.TLSHosts)
	}

	// A plan that already names its hosts keeps them: the operator's list is
	// more specific than the hostname this process happens to see.
	named := Plan{TLSHosts: []string{"ci.example.com"}}
	ListenSelfSigned.apply(&named, 8080, "build-01")
	if !slices.Equal(named.TLSHosts, []string{"ci.example.com"}) {
		t.Errorf("TLS hosts = %v, want the operator's own list kept", named.TLSHosts)
	}
}

// What an installation targets is read from the name when nobody said: a
// slash is a repository, and anything else is an organisation.
func TestATargetWithASlashIsARepository(t *testing.T) {
	cases := []struct {
		target, declared string
		want             store.TargetType
	}{
		{"acme", "", store.TargetOrg},
		{"acme/api", "", store.TargetRepo},
		// A declared type is the operator's answer and is not second-guessed,
		// even when the shape of the name suggests otherwise.
		{"acme/api", string(store.TargetOrg), store.TargetOrg},
		{"acme", string(store.TargetRepo), store.TargetRepo},
		{"acme/api", "nonsense", store.TargetRepo},
	}
	for _, tc := range cases {
		if got := targetTypeFor(tc.target, tc.declared); got != tc.want {
			t.Errorf("targetTypeFor(%q, %q) = %q, want %q", tc.target, tc.declared, got, tc.want)
		}
	}
}

// The ledger is what an aborted run prints instead of claiming nothing
// happened, so a caller must not be able to edit history by holding onto it.
func TestTheLedgerOfWhatWasWrittenIsACopy(t *testing.T) {
	i := unattendedInstaller(t, Options{})
	i.wrote("wrote /etc/zoomies/zoomies.yaml")
	i.wrote("created the service user")

	got := i.Written()
	if len(got) != 2 || got[0] != "wrote /etc/zoomies/zoomies.yaml" {
		t.Fatalf("Written() = %v, want both entries oldest first", got)
	}
	got[0] = "did something else entirely"
	if i.Written()[0] == "did something else entirely" {
		t.Error("a caller rewrote the installer's record of what it did to the host")
	}
}

// The step headings count themselves ("-> 3/9"), so the total has to match
// what the run is actually going to do.
func TestTheStepCountGrowsWithTheWorkThePlanAsksFor(t *testing.T) {
	base := Plan{Mode: ModeController}
	single := Plan{Mode: ModeSingle}
	starting := Plan{Mode: ModeSingle, StartService: true}

	if nativeStepCount(single) != nativeStepCount(base)+1 {
		t.Errorf("a single host creates a first pool, so it is one step longer than a controller: %d and %d",
			nativeStepCount(single), nativeStepCount(base))
	}
	if nativeStepCount(starting) != nativeStepCount(single)+1 {
		t.Errorf("starting the service adds the health check: %d and %d",
			nativeStepCount(starting), nativeStepCount(single))
	}
}

// A select row is one line, so a consequence written as a paragraph is cut at
// its first sentence rather than wrapped across the menu.
func TestASelectRowKeepsOnlyTheFirstSentence(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Binds every interface. Browsers will warn.", "Binds every interface."},
		{"One sentence only", "One sentence only"},
		{"", ""},
		// A trailing full stop is the end of the line, not a boundary to cut
		// an empty tail off.
		{"Ends here.", "Ends here."},
	}
	for _, tc := range cases {
		if got := firstSentence(tc.in); got != tc.want {
			t.Errorf("firstSentence(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The backend rows name the socket they found, and say nothing at all when
// there is none -- "docker at " would be worse than "docker".
func TestTheSocketIsNamedOnlyWhenThereIsOne(t *testing.T) {
	if got := socketSuffix("/run/docker.sock"); got != " at /run/docker.sock" {
		t.Errorf("socketSuffix = %q, want the socket named", got)
	}
	if got := socketSuffix(""); got != "" {
		t.Errorf("socketSuffix = %q, want nothing to follow the backend's name", got)
	}
}

// A non-interactive run gets the same sentence the prompt would have shown,
// so an unattended install explains the deployment it picked.
func TestTheDeploymentConsequenceIsTheOneFromTheMenu(t *testing.T) {
	det := hostWith(true, true)
	for _, o := range DeploymentOptions(det) {
		if got := deploymentConsequence(det, o.Deployment); got != o.Description {
			t.Errorf("deploymentConsequence(%q) = %q, want the menu's own description %q", o.Deployment, got, o.Description)
		}
	}

	// A deployment this host cannot offer still gets a sentence rather than
	// an empty line.
	if got := deploymentConsequence(hostWith(false, false), Deployment("nonsense")); strings.TrimSpace(got) == "" {
		t.Error("a deployment with no row of its own was described with nothing at all")
	}
}

// A path the operator typed is checked while they are still at the prompt.
func TestAPathIsCheckedBeforeTheRunGoesAnyFurther(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	if err := os.WriteFile(cert, []byte("not really a certificate"), 0o600); err != nil {
		t.Fatalf("writing a certificate: %v", err)
	}
	if err := fileMustExist("  " + cert + "  "); err != nil {
		t.Errorf("fileMustExist(%q) = %v, want the surrounding spaces trimmed and the file found", cert, err)
	}
	if err := fileMustExist(""); err == nil {
		t.Error("an empty path was accepted")
	}
	missing := filepath.Join(dir, "nowhere.pem")
	err := fileMustExist(missing)
	if err == nil {
		t.Fatal("a path that is not there was accepted")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %q, want it to name the path it could not read", err)
	}
}

// An answer that is not on the menu cannot be preselected: the form would
// show the first row while the plan held something else.
func TestAnAnswerIsOnlyKeptWhenItIsOneOfTheChoices(t *testing.T) {
	var opts []huh.Option[string]
	for _, o := range DeploymentOptions(hostWith(true, true)) {
		opts = append(opts, huh.NewOption(o.Label+" -- "+o.Description, string(o.Deployment)))
	}
	if len(opts) == 0 {
		t.Fatal("no deployment options were offered at all")
	}
	if !containsOption(opts, opts[0].Value) {
		t.Errorf("the first option %q was not found among the choices", opts[0].Value)
	}
	if containsOption(opts, "something-else") {
		t.Error("a value nobody offered was reported as one of the choices")
	}
}

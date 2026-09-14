package proxmox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearCluster unsets everything preflight reads, so a test starts from a
// laptop rather than from whatever the machine running it happens to export.
func clearCluster(t *testing.T) {
	t.Helper()
	for _, v := range []string{
		"ZOOMIES_PROXMOX_REQUIRED", "ZOOMIES_PROXMOX_URL", "ZOOMIES_PROXMOX_TOKEN",
		"ZOOMIES_PROXMOX_TOKEN_SECRET", "ZOOMIES_PROXMOX_NODE", "ZOOMIES_PROXMOX_TEMPLATE",
		"ZOOMIES_PROXMOX_STORAGE", "ZOOMIES_PROXMOX_VMID_RANGE", "ZOOMIES_PROXMOX_BRIDGE",
		"ZOOMIES_PROXMOX_POOL", "ZOOMIES_PROXMOX_CPUS", "ZOOMIES_PROXMOX_MEMORY_MB",
		"ZOOMIES_PROXMOX_DISK_MB", "ZOOMIES_PROXMOX_BACKEND", "ZOOMIES_PROXMOX_INSECURE",
		"ZOOMIES_PROXMOX_CA_PEM_FILE", "ZOOMIES_PROXMOX_CONTROLLER_URL",
		"ZOOMIES_PROXMOX_BROKEN_TEMPLATE", "ZOOMIES_PROXMOX_WORKFLOW",
		"ZOOMIES_E2E_APP_ID", "ZOOMIES_E2E_INSTALLATION_ID", "ZOOMIES_E2E_PRIVATE_KEY_FILE",
		"ZOOMIES_E2E_TARGET", "ZOOMIES_E2E_TARGET_TYPE", "ZOOMIES_E2E_REPO",
	} {
		t.Setenv(v, "")
	}
}

// A laptop is quiet and a qualification machine is not.
//
// The distinction is the whole reason `required` exists: where somebody is
// qualifying a cluster, a skip looks exactly like a pass, and a procedure whose
// failure mode is "looked fine" is not a procedure.
func TestAQualificationRunIsOnlySilentWhereNobodyAskedForOne(t *testing.T) {
	clearCluster(t)
	if requested() {
		t.Error("a machine with none of the settings was taken as having asked for a qualification")
	}

	// Any one of the six means somebody meant to: the rest being absent is then
	// a blockage to report, not a laptop to be quiet on.
	for _, v := range clusterVars {
		clearCluster(t)
		t.Setenv(v, "something")
		if !requested() {
			t.Errorf("%s was set and the run still considered itself unasked-for", v)
		}
	}

	clearCluster(t)
	t.Setenv("ZOOMIES_PROXMOX_REQUIRED", "1")
	if !requested() || !required() {
		t.Error("ZOOMIES_PROXMOX_REQUIRED did not make a missing prerequisite a failure")
	}
}

// The skip names every variable, because whoever reads it is about to set them
// all -- and it says plainly that the fixture suite is not this.
func TestTheSkipNamesEverySettingAndRefusesToBeMistakenForQualification(t *testing.T) {
	reason := skipReason()
	for _, v := range clusterVars {
		if !strings.Contains(reason, v) {
			t.Errorf("the skip does not name %s", v)
		}
	}
	for _, want := range []string{
		"DISPOSABLE",
		"ZOOMIES_E2E_APP_ID",
		"roadmap/validation/proxmox-qualification.md",
		"Fixture success is not live qualification",
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("the skip does not say %q", want)
		}
	}
}

// Every missing prerequisite is named at once, and nothing is dialled to find
// them out.
//
// Half a prerequisite means a provider row pointed at a cluster, and a provider
// row pointed at a cluster builds virtual machines. So the answer to "what is
// missing" has to be complete before anything is created, and being told one
// item per attempt is how somebody gives up and starts guessing.
func TestPreflightNamesEveryMissingPrerequisiteAtOnce(t *testing.T) {
	clearCluster(t)
	_, missing := preflight()
	if len(missing) == 0 {
		t.Fatal("preflight with nothing set found nothing missing")
	}
	joined := strings.Join(missing, "\n")
	for _, want := range []string{
		"ZOOMIES_PROXMOX_URL", "ZOOMIES_PROXMOX_TOKEN", "ZOOMIES_PROXMOX_NODE",
		"ZOOMIES_PROXMOX_STORAGE", "ZOOMIES_PROXMOX_TEMPLATE", "ZOOMIES_PROXMOX_VMID_RANGE",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("preflight did not report %s missing:\n%s", want, joined)
		}
	}
}

// The defaults are the ones the README documents, and an unset setting is not
// the same as one set to something unusable.
func TestPreflightFillsInTheDefaultsItDocuments(t *testing.T) {
	clearCluster(t)
	e, _ := preflight()
	for _, tc := range []struct{ name, got, want string }{
		{"bridge", e.bridge, "vmbr0"},
		{"backend", e.backend, "docker"},
		{"target type", e.targetType, "org"},
		{"workflow", e.workflow, "zoomies-e2e.yml"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s defaulted to %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if e.cpus != 2 || e.memoryMB != 2048 || e.diskMB != 0 {
		t.Errorf("machine shape defaulted to %d cpus / %d MB / %d MB disk", e.cpus, e.memoryMB, e.diskMB)
	}
}

// The machine shape is evidence, so a value nobody can read falls back to the
// documented default rather than to zero: a qualification claiming two cores
// when it ran on none is worse than one that says what it actually used.
func TestAnUnreadableNumberFallsBackToTheDocumentedDefault(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		def  int
		want int
	}{
		{"", 2048, 2048},
		{"   ", 2048, 2048},
		{"not a number", 2048, 2048},
		{"4096", 2048, 4096},
		{"  4096  ", 2048, 4096},
		{"0", 2048, 0},
	} {
		if got := atoiOr(tc.raw, tc.def); got != tc.want {
			t.Errorf("atoiOr(%q, %d) = %d, want %d", tc.raw, tc.def, got, tc.want)
		}
	}
	for _, tc := range []struct{ raw, def, want string }{
		{"", "vmbr0", "vmbr0"},
		{"  ", "vmbr0", "vmbr0"},
		{"  vmbr1 ", "vmbr0", "vmbr1"},
	} {
		if got := orDefault(tc.raw, tc.def); got != tc.want {
			t.Errorf("orDefault(%q, %q) = %q, want %q", tc.raw, tc.def, got, tc.want)
		}
	}
}

// Turning off certificate verification takes a word that means yes. Anything
// else leaves it on, because the failure of guessing here is a credential sent
// to whoever answered.
func TestOnlyAWordMeaningYesTurnsCertificateCheckingOff(t *testing.T) {
	for _, raw := range []string{"1", "true", "TRUE", "yes", "on", " On "} {
		if !truthy(raw) {
			t.Errorf("truthy(%q) = false", raw)
		}
	}
	for _, raw := range []string{"", "0", "false", "no", "off", "maybe", "2", "y"} {
		if truthy(raw) {
			t.Errorf("truthy(%q) = true", raw)
		}
	}
}

// The range is the setting that makes this safe to run at all: without it the
// provider would allocate identifiers anywhere in the cluster, including ones
// somebody else's automation is entitled to. So every way of getting it wrong
// is refused, and the refusal says how to write it.
func TestAVMIDRangeIsRefusedEveryWayItCanBeWrong(t *testing.T) {
	for _, tc := range []struct {
		raw            string
		min, max       int
		wantErr        string
		wantAcceptance bool
	}{
		{raw: "9000-9099", min: 9000, max: 9099, wantAcceptance: true},
		{raw: "  9000 - 9099 ", min: 9000, max: 9099, wantAcceptance: true},
		{raw: "9000-9000", min: 9000, max: 9000, wantAcceptance: true},
		{raw: "", wantErr: "9000-9099"},
		{raw: "9000", wantErr: "is not a range"},
		{raw: "nine-9099", wantErr: "does not begin with a number"},
		{raw: "9000-ninety", wantErr: "does not end with a number"},
		{raw: "0-9099", wantErr: "they start at 100"},
		{raw: "9099-9000", wantErr: "runs backwards"},
	} {
		min, max, err := parseRange(tc.raw)
		if tc.wantAcceptance {
			if err != nil {
				t.Errorf("parseRange(%q): %v", tc.raw, err)
			} else if min != tc.min || max != tc.max {
				t.Errorf("parseRange(%q) = %d-%d, want %d-%d", tc.raw, min, max, tc.min, tc.max)
			}
			continue
		}
		if err == nil {
			t.Errorf("parseRange(%q) = %d-%d, want a refusal", tc.raw, min, max)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("parseRange(%q) said %q, want it to mention %q", tc.raw, err, tc.wantErr)
		}
	}
}

// A range smaller than the procedure needs is a typo, and finding out at cycle
// nineteen wastes an afternoon.
func TestARangeTooSmallForTheProcedureIsRefusedBeforeTheFirstCycle(t *testing.T) {
	clearCluster(t)
	t.Setenv("ZOOMIES_PROXMOX_VMID_RANGE", "9000-9001")
	_, missing := preflight()
	joined := strings.Join(missing, "\n")
	if !strings.Contains(joined, "ZOOMIES_PROXMOX_VMID_RANGE") {
		t.Fatalf("a two-identifier range for a %d-machine procedure was accepted:\n%s", qualificationCycles, joined)
	}
}

// The template is a VMID, not a name: every machine starts as a clone of one,
// and a template that cannot be identified is not a prerequisite half-met.
func TestATemplateThatIsNotAVMIDIsRefused(t *testing.T) {
	clearCluster(t)
	t.Setenv("ZOOMIES_PROXMOX_TEMPLATE", "ubuntu-24.04-template")
	_, missing := preflight()
	if !strings.Contains(strings.Join(missing, "\n"), "ZOOMIES_PROXMOX_TEMPLATE") {
		t.Error("a template named rather than numbered was accepted")
	}
}

// The runner backend inside the guest is one of the three this product has.
func TestTheGuestRunnerBackendIsOneOfTheThreeThatExist(t *testing.T) {
	for _, backend := range []string{"docker", "podman", "process"} {
		clearCluster(t)
		t.Setenv("ZOOMIES_PROXMOX_BACKEND", backend)
		_, missing := preflight()
		if strings.Contains(strings.Join(missing, "\n"), "ZOOMIES_PROXMOX_BACKEND") {
			t.Errorf("%s was refused as a guest backend", backend)
		}
	}
	clearCluster(t)
	t.Setenv("ZOOMIES_PROXMOX_BACKEND", "kubernetes")
	_, missing := preflight()
	if !strings.Contains(strings.Join(missing, "\n"), "ZOOMIES_PROXMOX_BACKEND") {
		t.Error("a backend this product does not have was accepted")
	}
}

// A CA file that cannot be read is named, rather than becoming a handshake
// failure twenty minutes later.
func TestACertificateAuthorityFileThatCannotBeReadIsReportedHere(t *testing.T) {
	clearCluster(t)
	t.Setenv("ZOOMIES_PROXMOX_CA_PEM_FILE", filepath.Join(t.TempDir(), "absent.pem"))
	_, missing := preflight()
	if !strings.Contains(strings.Join(missing, "\n"), "ZOOMIES_PROXMOX_CA_PEM_FILE") {
		t.Error("an unreadable CA file was left to fail during the handshake")
	}
}

// The setting people get wrong: a guest enrols by calling back, so a controller
// address without a port -- or one that is not a URL -- is a fleet of machines
// that build correctly and never join.
func TestAControllerAddressAGuestCannotCallBackOnIsRefused(t *testing.T) {
	for _, raw := range []string{"not a url", "http://controller", "controller:8080"} {
		clearCluster(t)
		t.Setenv("ZOOMIES_PROXMOX_CONTROLLER_URL", raw)
		_, missing := preflight()
		if !strings.Contains(strings.Join(missing, "\n"), "ZOOMIES_PROXMOX_CONTROLLER_URL") {
			t.Errorf("%q was accepted as an address a guest could reach", raw)
		}
	}

	clearCluster(t)
	t.Setenv("ZOOMIES_PROXMOX_CONTROLLER_URL", "http://10.0.0.5:8080")
	_, missing := preflight()
	if strings.Contains(strings.Join(missing, "\n"), "ZOOMIES_PROXMOX_CONTROLLER_URL") {
		t.Errorf("a reachable address was refused:\n%s", strings.Join(missing, "\n"))
	}
}

// The range on the env and the range on the wire are the same range.
func TestTheRangeTheRunMayAllocateInIsTheRangeItWasGiven(t *testing.T) {
	e := env{vmidMin: 9000, vmidMax: 9099}
	if got := e.vmidRange(); got != (Range{Min: 9000, Max: 9099}) {
		t.Errorf("vmidRange() = %v", got)
	}
	if got := e.vmidRange().String(); got != "9000-9099" {
		t.Errorf("the range reads as %q", got)
	}
}

// Qualification is of the built artefact, not of one the test compiles: a
// binary built here with different flags is not the thing being qualified.
func TestTheBinaryUnderQualificationIsTheBuiltOneBesideGoMod(t *testing.T) {
	got := builtBinary()
	if filepath.Base(got) != "zoomies" {
		t.Errorf("builtBinary() = %q, want the zoomies binary", got)
	}
	if got != "zoomies" {
		if _, err := os.Stat(filepath.Join(filepath.Dir(got), "go.mod")); err != nil {
			t.Errorf("builtBinary() = %q, which is not beside a go.mod: %v", got, err)
		}
	}
}

// A port for the default controller URL, asked of the kernel rather than
// guessed, and a usable one even when it cannot be.
func TestAPortIsAskedForRatherThanPickedOutOfTheAir(t *testing.T) {
	p := freePortNumber()
	if p <= 0 || p > 65535 {
		t.Errorf("freePortNumber() = %d", p)
	}
}

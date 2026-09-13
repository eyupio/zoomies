package controller

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/provider"
	"github.com/eyupio/zoomies/internal/store"
)

// The guest gets exactly one credential, and this is the test that says so.
//
// A provider credential can read the whole hypervisor: enumerate machines,
// destroy them, and read every other guest's configuration. It is sealed in the
// provider row and unsealed only for the life of one client, and nothing copies
// it into a payload -- provider.Bootstrap has files and argv arrays and no
// field one could go in. This scans every byte that reached a guest for it,
// because a type can be changed and a test cannot be argued with.
func TestABootstrapPayloadCarriesNoProviderCredential(t *testing.T) {
	h := newHarness(t)
	_, row := h.machineFleet(t)

	const secret = "PVEAPIToken=zoomies@pve!ci=9f1c0a3e-recognisable-2b7d"
	sealed, err := h.key.SealString(secret)
	if err != nil {
		t.Fatalf("sealing the provider credential: %v", err)
	}
	if err := h.st.SetProviderCredentials(h.ctx, row.ID, sealed); err != nil {
		t.Fatalf("SetProviderCredentials: %v", err)
	}

	h.machinePass(t)
	m := h.onlyMachine(t)
	h.drive(t, m.ID, store.MachineEnrolling, 6)

	payloads := h.fake.Bootstraps()
	if len(payloads) == 0 {
		t.Fatal("nothing reached the guest at all, so this test proves nothing")
	}
	for _, b := range payloads {
		for _, f := range b.Files {
			if bytes.Contains(f.Content, []byte(secret)) {
				t.Fatalf("the provider's credential was written into %s", f.Path)
			}
		}
		for _, argv := range b.Commands {
			for _, arg := range argv {
				if strings.Contains(arg, secret) {
					t.Fatalf("the provider's credential was passed as an argument: %q", arg)
				}
			}
		}
	}
}

// The name the guest enrols under must be pinned, and this is the reason.
// machine.DefaultHostName derives a name from the hardware and the hostname, so
// two clones of one template compute the same one -- and every machine a
// provider builds is a clone of one template. Without the pin the second
// machine's join is refused with "a host named %q is already enrolled here",
// and the fleet stops growing at one.
func TestTheEnrolmentFilePinsTheMachineName(t *testing.T) {
	m := &store.Machine{ID: "mach_abc", Name: "zoomies-mach-abc", Capacity: 2}
	row := &store.Provider{
		Name: "lab", MachineCapacity: 4, MachineBackend: store.BackendDocker,
		MachineLabels: store.StringMap{"tier": "untrusted", "site": "dc1"},
	}
	cfg := config.Default()
	cfg.Server.ExternalURL = "https://zoomies.test/"

	payload := bootstrapPayload(m, row, "zoojoin_abc_secret", cfg)
	env := envOf(t, payload)

	for _, want := range []struct{ key, value string }{
		{"ZOOMIES_AGENT_NAME", m.Name},
		{"ZOOMIES_JOIN_TOKEN", "zoojoin_abc_secret"},
		{"ZOOMIES_CONTROLLER_URL", "https://zoomies.test"},
		{"ZOOMIES_AGENT_CAPACITY", "4"},
		{"ZOOMIES_AGENT_BACKEND", "docker"},
		{"ZOOMIES_AGENT_LABELS", "site=dc1,tier=untrusted"},
		{"ZOOMIES_WORK_DIR", machineWorkDir},
	} {
		if got := env[want.key]; got != want.value {
			t.Errorf("%s is %q, want %q", want.key, got, want.value)
		}
	}
}

// The environment file is chmodded because the transport that writes it has no
// way to say what mode it should have, and the file holds a credential. The
// unit is enabled rather than started so that a reboot brings the agent back.
func TestTheEnrolmentFileIsChmoddedBecauseAFileWriteHasNoMode(t *testing.T) {
	h := newHarness(t)
	h.machineFleet(t)
	h.machinePass(t)
	m := h.onlyMachine(t)
	h.drive(t, m.ID, store.MachineEnrolling, 6)

	payload := h.fake.Bootstraps()[0]
	var wrote bool
	for _, f := range payload.Files {
		if f.Path == machineEnvPath {
			wrote = true
			if f.Mode != machineEnvMode {
				t.Errorf("the enrolment file asks for mode %#o, want %#o", f.Mode, machineEnvMode)
			}
		}
	}
	if !wrote {
		t.Fatalf("nothing was written to %s", machineEnvPath)
	}
	want := [][]string{
		{"/bin/chmod", "0600", machineEnvPath},
		{"/bin/systemctl", "enable", "--now", "zoomies-agent"},
	}
	if len(payload.Commands) != len(want) {
		t.Fatalf("the payload runs %d commands, want %d: %v", len(payload.Commands), len(want), payload.Commands)
	}
	for i, argv := range payload.Commands {
		if strings.Join(argv, " ") != strings.Join(want[i], " ") {
			t.Errorf("command %d is %v, want %v", i, argv, want[i])
		}
	}
}

// Nothing in this path builds a shell command line. A payload that is never
// pasted into a shell has no quoting to get wrong and no injection class to
// police, which is the whole reason the transport takes an argv array.
func TestTheBootstrapNeverBuildsAShellCommandLine(t *testing.T) {
	m := &store.Machine{ID: "mach_x", Name: "zoomies-mach-x; rm -rf /"}
	row := &store.Provider{Name: "lab", MachineCapacity: 1,
		MachineLabels: store.StringMap{"note": "a value with spaces and a $VARIABLE"}}
	payload := bootstrapPayload(m, row, "zoojoin_x_`whoami`", config.Default())

	for _, argv := range payload.Commands {
		if len(argv) < 2 {
			t.Fatalf("a command with no arguments is a shell line waiting to happen: %v", argv)
		}
		if !strings.HasPrefix(argv[0], "/") {
			t.Errorf("command %v does not name an absolute executable, so it would be resolved through a PATH we do not control", argv)
		}
		for _, shell := range []string{"sh", "bash", "-c"} {
			for _, arg := range argv {
				if arg == shell {
					t.Errorf("the payload runs a shell: %v", argv)
				}
			}
		}
	}
}

// A token minted for one machine may not enrol another, and the check runs
// inside the transaction that spends it: a credential that leaks out of a guest
// is not stopped by a check that happens afterwards.
func TestAJoinTokenMintedForOneMachineCannotEnrolAnother(t *testing.T) {
	h := newHarness(t)
	_, plaintext, err := h.c.Auth().CreateScopedJoinToken(h.ctx, auth.JoinScope{
		TTL: time.Minute, Capacity: 2, MachineID: "mach_abc", ExpectedName: "zoomies-mach-abc",
		CreatedBy: "machine mach_abc",
	})
	if err != nil {
		t.Fatalf("CreateScopedJoinToken: %v", err)
	}

	if _, err := h.c.Join(h.ctx, joinRequest("somebody-elses-laptop", plaintext), "203.0.113.9"); err == nil {
		t.Fatal("a machine's join token enrolled a host it was not minted for")
	} else if !errors.Is(err, auth.ErrInvalidInput) {
		t.Fatalf("the refusal is %v, want one the agent's operator can act on", err)
	}

	// And the token is still there to be spent by the machine it belongs to,
	// because a refused claim must not burn the credential.
	if _, err := h.c.Join(h.ctx, joinRequest("zoomies-mach-abc", plaintext), "203.0.113.9"); err != nil {
		t.Fatalf("the machine the token was minted for could not use it: %v", err)
	}
}

// A token an operator pastes is theirs to look after, and they pick the name
// themselves. The human path must not gain a scope check it cannot satisfy.
func TestAnOperatorsJoinTokenStillEnrolsWhateverTheyName(t *testing.T) {
	h := newHarness(t)
	token := h.joinToken(t, map[string]string{"tier": "trusted"}, 4)

	if _, err := h.c.Join(h.ctx, joinRequest("builder-07", token), "203.0.113.9"); err != nil {
		t.Fatalf("an unscoped join token refused a name the operator chose: %v", err)
	}
}

// The token is minted once and recorded on the machine, so a pass that comes
// back to a machine already waiting on its agent does not mint a second
// credential for it.
func TestAMachineMintsOneCredentialAndRecordsIt(t *testing.T) {
	h := newHarness(t)
	h.machineFleet(t)
	h.machinePass(t)
	m := h.onlyMachine(t)
	m = h.drive(t, m.ID, store.MachineEnrolling, 6)

	if m.JoinTokenID == "" {
		t.Fatal("a machine reached enrolling with no record of the credential it was given")
	}
	tok, err := h.st.GetJoinToken(h.ctx, m.JoinTokenID)
	if err != nil {
		t.Fatalf("GetJoinToken: %v", err)
	}
	if tok.MachineID != m.ID || tok.ExpectedName != m.Name {
		t.Fatalf("the token is scoped to machine %q named %q, want %q named %q",
			tok.MachineID, tok.ExpectedName, m.ID, m.Name)
	}
	// Short-lived: a credential nobody is watching must not last an hour.
	if life := tok.ExpiresAt.Sub(tok.CreatedAt); life > h.cfg.Provider.EnrolTimeout+machineTokenGrace+time.Second {
		t.Fatalf("the machine's token lasts %s, want the enrolment window plus a grace", life)
	}
	if life := tok.ExpiresAt.Sub(tok.CreatedAt); life <= h.cfg.Provider.EnrolTimeout {
		t.Fatalf("the machine's token lasts %s, which is less than the %s it is given to enrol in",
			life, h.cfg.Provider.EnrolTimeout)
	}

	// A second pass over a machine already waiting mints nothing.
	before := tok.ID
	h.machinePass(t)
	if got := h.machineByID(t, m.ID); got.JoinTokenID != before {
		t.Fatal("a second pass minted a second credential for a machine that already had one")
	}
}

// A host that joined with a machine's token is linked to it, and that link is
// the only thing that makes deleting the host this controller's business.
func TestTheHostAMachineEnrolledAsIsLinkedToIt(t *testing.T) {
	h := newHarness(t)
	h.machineFleet(t)
	h.machinePass(t)
	m := h.drive(t, h.onlyMachine(t).ID, store.MachineEnrolling, 6)

	h.joinAsMachine(t, m)
	got := h.machineByID(t, m.ID)
	if got.HostID == "" {
		t.Fatal("a machine whose token was redeemed is not linked to the host that redeemed it")
	}
	linked, err := h.st.GetMachineByHost(h.ctx, got.HostID)
	if err != nil {
		t.Fatalf("GetMachineByHost: %v", err)
	}
	if linked.ID != m.ID {
		t.Fatalf("host %s is linked to machine %s, want %s", got.HostID, linked.ID, m.ID)
	}
}

// envOf reads the environment file out of a payload, so that a test asserts on
// the keys the agent will actually read rather than on a blob of bytes.
func envOf(t *testing.T, b provider.Bootstrap) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range b.Files {
		if f.Path != machineEnvPath {
			continue
		}
		for line := range strings.SplitSeq(string(f.Content), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				t.Fatalf("the enrolment file has a line that is not an assignment: %q", line)
			}
			out[key] = value
		}
	}
	if len(out) == 0 {
		t.Fatal("the payload wrote no environment file")
	}
	return out
}

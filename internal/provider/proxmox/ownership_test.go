package proxmox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/provider"
)

// The marks are how a sweep tells this fleet's guests from everybody else's, so
// what is written has to be exactly what is read back. A fingerprint that did
// not survive the round trip would make every machine look like somebody
// else's, and nothing would ever be deleted again.
func TestTheOwnershipBlockSurvivesARoundTrip(t *testing.T) {
	created := time.Date(2026, 3, 4, 9, 15, 30, 0, time.UTC)
	tests := []struct {
		name  string
		owner provider.Owner
	}{
		{"a complete set of marks", provider.Owner{
			ControllerID: "ctl_k3f9qz2mx7ab", ProviderID: "prv_ab2c3d4e5f6g",
			MachineID: "mach_z7y6x5w4v3u2", Fingerprint: "9f8e7d6c5b4a", CreatedAt: created,
		}},
		{"no timestamp, which is not a reason to disown a machine", provider.Owner{
			ControllerID: "ctl_k3f9qz2mx7ab", MachineID: "mach_z7y6x5w4v3u2", Fingerprint: "9f8e7d6c5b4a",
		}},
		{"a timestamp somewhere else in the world", provider.Owner{
			ControllerID: "ctl_k3f9qz2mx7ab", MachineID: "mach_z7y6x5w4v3u2", Fingerprint: "9f8e7d6c5b4a",
			CreatedAt: created.In(time.FixedZone("AEST", 10*3600)),
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			description := Describe(tc.owner, "zoomies-mach-z7y6x5w4v3u2")
			got, ok := DecodeOwner(description)
			if !ok {
				t.Fatalf("the description we wrote carries no marks:\n%s", description)
			}
			if !got.Matches(tc.owner) {
				t.Errorf("the marks read back do not match the ones written: %+v vs %+v", got, tc.owner)
			}
			if got.ProviderID != tc.owner.ProviderID {
				t.Errorf("ProviderID = %q, want %q", got.ProviderID, tc.owner.ProviderID)
			}
			if !got.CreatedAt.Equal(tc.owner.CreatedAt) {
				t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, tc.owner.CreatedAt)
			}
			if !strings.Contains(description, descriptionPreamble) {
				t.Error("the description says nothing to the operator who reads it in the console")
			}
		})
	}
}

// An operator who adds a note to a guest's description has not disowned it, and
// a sweep that read that as "unmarked" would quarantine a machine the fleet is
// quite sure about.
func TestADescriptionSomebodyEditedStillNamesItsOwner(t *testing.T) {
	owner := provider.Owner{ControllerID: "ctl_one", MachineID: "mach_two", Fingerprint: "three"}
	edited := "do not delete -- Ana\n" + Describe(owner, "zoomies-mach-two") + "\nrebooted 2026-03-04"

	got, ok := DecodeOwner(edited)
	if !ok {
		t.Fatalf("an edited description lost its marks:\n%s", edited)
	}
	if !got.Matches(owner) {
		t.Errorf("marks = %+v, want %+v", got, owner)
	}
}

// Everything that is not one of our blocks has to read as "no marks", because
// the alternative is deleting a guest on the strength of a coincidence.
func TestADescriptionThatIsNobodysCarriesNoMarks(t *testing.T) {
	tests := []struct {
		name, description string
	}{
		{"nothing at all", ""},
		{"somebody's prose", "the finance database -- do not touch"},
		{"somebody else's JSON", `{"terraform":{"workspace":"prod"}}`},
		{"our shape with nothing in it", `{"zoomies":{}}`},
		{"JSON that does not parse", `{"zoomies":{"controller":`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if owner, ok := DecodeOwner(tc.description); ok {
				t.Errorf("marks were found where there are none: %+v", owner)
			}
		})
	}
}

// The marks are readable and writable by anybody with configuration rights, so
// they are tamper-evidence rather than proof: what they catch is a recycled
// identifier and another controller's guest.
func TestMarksThatAreNotOursDoNotMatch(t *testing.T) {
	ours := provider.Owner{ControllerID: "ctl_one", MachineID: "mach_two", Fingerprint: "three"}
	tests := []struct {
		name  string
		found provider.Owner
	}{
		{"another controller's machine", provider.Owner{ControllerID: "ctl_other", MachineID: "mach_two", Fingerprint: "three"}},
		{"our controller, a machine we do not know", provider.Owner{ControllerID: "ctl_one", MachineID: "mach_other", Fingerprint: "three"}},
		{"the right names and a forged fingerprint", provider.Owner{ControllerID: "ctl_one", MachineID: "mach_two", Fingerprint: "guessed"}},
		{"no marks at all", provider.Owner{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.found.Matches(ours) {
				t.Errorf("%+v was accepted as ours", tc.found)
			}
		})
	}
}

// A tag Proxmox refuses fails the whole configuration call, which would leave a
// machine running and unmarked -- the one state the marks exist to prevent.
func TestATagStaysInsideProxmoxsGrammar(t *testing.T) {
	tests := []struct {
		name, machineID, want string
	}{
		{"a generated identifier", "mach_k3f9qz2mx7ab", "zoomies-k3f9qz2mx7ab"},
		{"a fixture's readable name", "mach_demo-linux", "zoomies-demo-linux"},
		{"capitals, which Proxmox lower-cases anyway", "mach_ABCdef", "zoomies-abcdef"},
		{"characters a tag may not carry", "mach_a b/c:d", "zoomies-abcd"},
		{"an identifier with no prefix", "k3f9qz2mx7ab", "zoomies-k3f9qz2mx7ab"},
		{"nothing usable", "mach_///", ""},
		{"nothing at all", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MachineTag(tc.machineID); got != tc.want {
				t.Errorf("MachineTag(%q) = %q, want %q", tc.machineID, got, tc.want)
			}
		})
	}
}

// Configuring a machine must not quietly remove a tag an operator added, and it
// must write the same string every pass -- a set in a different order each time
// would rewrite a guest's configuration for nothing.
func TestTagsAnOperatorAddedSurviveBeingConfigured(t *testing.T) {
	owner := provider.Owner{MachineID: "mach_k3f9qz2mx7ab"}
	got := EncodeTags("production;do-not-delete", Tags(owner))
	want := "do-not-delete;production;zoomies;zoomies-k3f9qz2mx7ab"
	if got != want {
		t.Errorf("EncodeTags = %q, want %q", got, want)
	}
	if again := EncodeTags(got, Tags(owner)); again != got {
		t.Errorf("a second pass rewrote the tags: %q then %q", got, again)
	}
}

// The fleet tag is what narrows a cluster-wide listing down to the guests worth
// reading a description for, so it has to be found however Proxmox joined them.
func TestTheFleetTagIsWhatASweepFiltersOn(t *testing.T) {
	tests := []struct {
		tags string
		want bool
	}{
		{"zoomies;zoomies-abc", true},
		{"production,zoomies", true},
		{"zoomies zoomies-abc", true},
		{"ZOOMIES", true},
		{"zoomies-abc", false},
		{"production", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.tags, func(t *testing.T) {
			if got := HasFleetTag(tc.tags); got != tc.want {
				t.Errorf("HasFleetTag(%q) = %v, want %v", tc.tags, got, tc.want)
			}
		})
	}
}

// The marks have to survive the wire as well as the round trip in memory: the
// configuration endpoint is where a sweep reads them back from, and a delete
// requires what it finds there to agree with the row.
func TestTheMarksAreReadBackFromTheGuestsOwnConfiguration(t *testing.T) {
	owner := provider.Owner{
		ControllerID: "ctl_k3f9qz2mx7ab", ProviderID: "prv_ab2c3d4e5f6g",
		MachineID: "mach_z7y6x5w4v3u2", Fingerprint: "9f8e7d6c5b4a",
		CreatedAt: time.Date(2026, 3, 4, 9, 15, 30, 0, time.UTC),
	}
	f := newFakePVE(t, nil)
	f.SetForeignVM(143, "zoomies-mach-z7y6x5w4v3u2")
	f.SetVMDescription(143, Describe(owner, "zoomies-mach-z7y6x5w4v3u2"), EncodeTags("", Tags(owner)))

	cfg, err := f.client(t).VMConfig(context.Background(), "pve-1", 143)
	if err != nil {
		t.Fatalf("VMConfig: %v", err)
	}
	got, ok := DecodeOwner(cfg.Description)
	if !ok {
		t.Fatalf("the guest carries no marks: %q", cfg.Description)
	}
	if !got.Matches(owner) {
		t.Errorf("marks = %+v, want %+v", got, owner)
	}
	if !HasFleetTag(cfg.Tags) {
		t.Errorf("tags = %q, which a sweep would not find", cfg.Tags)
	}
}

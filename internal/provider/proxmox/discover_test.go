package proxmox

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/provider"
)

func choice(t *testing.T, choices []provider.Choice, value string) provider.Choice {
	t.Helper()
	for _, c := range choices {
		if c.Value == value {
			return c
		}
	}
	t.Fatalf("no choice %q; got %v", value, values(choices))
	return provider.Choice{}
}

func values(choices []provider.Choice) []string {
	out := make([]string, 0, len(choices))
	for _, c := range choices {
		out = append(out, c.Value)
	}
	return out
}

// Offering a storage a clone cannot land on invites exactly the mistake the
// menu exists to prevent, and the operator only finds out when the first
// machine fails to build.
func TestTheGuidedFormOffersOnlyStoragesThatCanHoldADisk(t *testing.T) {
	f := newFakePVE(t, nil)

	found, err := Discover(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := values(found.Storages); len(got) != 2 {
		t.Fatalf("storages = %v, want only the ones that hold images", got)
	}
	choice(t, found.Storages, "local-lvm")
	choice(t, found.Storages, "ceph")
	for _, c := range found.Storages {
		if c.Value == "local" {
			t.Error("a storage that only holds ISOs and container templates was offered for a clone")
		}
	}
}

// A local storage looks fine in a list until a machine is placed on a node that
// cannot see it, so the consequence has to say which nodes have it.
func TestAStorageOnlySomeNodesCanSeeSaysSo(t *testing.T) {
	f := newFakePVE(t, nil)

	found, err := Discover(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	local := choice(t, found.Storages, "local-lvm")
	if !strings.Contains(local.Consequence, "only on pve-1") {
		t.Errorf("consequence = %q, which does not say where it is", local.Consequence)
	}
	shared := choice(t, found.Storages, "ceph")
	if !strings.Contains(shared.Consequence, "shared") {
		t.Errorf("consequence = %q, which does not say it is shared", shared.Consequence)
	}
	if !strings.Contains(shared.Consequence, "GiB free") {
		t.Errorf("consequence = %q, which does not say how much room there is", shared.Consequence)
	}
}

// A machine on a node whose bridge is missing has no network and can never
// enrol -- a failure that looks like a broken image rather than a wrong menu.
func TestABridgeMissingFromANodeSaysSo(t *testing.T) {
	f := newFakePVE(t, nil)

	found, err := Discover(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	everywhere := choice(t, found.Bridges, "vmbr0")
	if !strings.Contains(everywhere.Consequence, "every node") {
		t.Errorf("consequence = %q", everywhere.Consequence)
	}
	partial := choice(t, found.Bridges, "vmbr1")
	for _, want := range []string{"only on pve-1", "no network"} {
		if !strings.Contains(partial.Consequence, want) {
			t.Errorf("consequence = %q does not mention %q", partial.Consequence, want)
		}
	}
}

// Templates come from the one cluster-wide call, so a form for a ten-node
// cluster costs the same as one for a single machine.
func TestTemplatesComeFromTheOneClusterCall(t *testing.T) {
	f := newFakePVE(t, nil)

	found, err := Discover(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	template := choice(t, found.Templates, "9000")
	if template.Label != "ubuntu-24.04-template" {
		t.Errorf("label = %q, want the template's name", template.Label)
	}
	if !strings.Contains(template.Consequence, "VMID 9000 on pve-1") {
		t.Errorf("consequence = %q, which does not say where it is", template.Consequence)
	}
	for _, c := range found.Templates {
		if c.Value == "100" {
			t.Error("somebody else's running machine was offered as a template")
		}
	}
	var sweeps int
	for _, r := range f.Requests() {
		if strings.HasSuffix(r, "/cluster/resources") {
			sweeps++
		}
	}
	if sweeps != 1 {
		t.Errorf("the cluster listing was asked for %d times, want once", sweeps)
	}
}

// An offline node is still worth showing: the answer to "why can nothing be
// built here" is the same list an operator is choosing from.
func TestANodeThatIsDownIsOfferedWithItsConsequence(t *testing.T) {
	f := newFakePVE(t, nil)

	found, err := Discover(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := values(found.Nodes); len(got) != 3 {
		t.Fatalf("nodes = %v, want every node in the cluster", got)
	}
	down := choice(t, found.Nodes, "pve-3")
	if !strings.Contains(down.Consequence, "offline") {
		t.Errorf("consequence = %q, which does not say why nothing can be built there", down.Consequence)
	}
	up := choice(t, found.Nodes, "pve-1")
	for _, want := range []string{"online", "8 processors", "32 GiB"} {
		if !strings.Contains(up.Consequence, want) {
			t.Errorf("consequence = %q does not mention %q", up.Consequence, want)
		}
	}
}

// A cluster with one node down still has a form to fill in, and the answers the
// other nodes gave are still the right ones.
func TestANodeThatWillNotAnswerDoesNotEmptyTheForm(t *testing.T) {
	f := newFakePVE(t, nil)
	f.SetError(http.MethodGet, "/pve-2/storage", http.StatusInternalServerError, "storage status unavailable")

	found, err := Discover(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	choice(t, found.Storages, "local-lvm")
	choice(t, found.Bridges, "vmbr0")
}

// A discovery that saw nothing at all is an error, because an empty menu with
// no explanation sends an operator looking for a bug in the wizard.
func TestDiscoveryFailsOnlyWhenNothingCouldBeSeen(t *testing.T) {
	f := newFakePVE(t, nil)
	f.SetError(http.MethodGet, "/nodes", http.StatusForbidden, "Permission check failed (/nodes, Sys.Audit)")

	_, err := Discover(context.Background(), f.client(t))
	if got := provider.KindOf(err); got != provider.FailurePermission {
		t.Fatalf("kind = %q, want %q (%v)", got, provider.FailurePermission, err)
	}
}

package proxmox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/provider"
	"github.com/eyupio/zoomies/internal/store"
)

// The suite every provider must pass, run against a fake cluster that speaks
// the real API over TLS. It is first in this file because it is the definition
// of done: everything below it is a Proxmox-specific rule the contract has no
// opinion about.
func TestTheProxmoxProviderObeysTheContract(t *testing.T) {
	provider.RunContractTests(t, "proxmox", func(t *testing.T) provider.Provider {
		return newTestProvider(t, newFakePVE(t, nil), nil)
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestProvider builds a provider pointed at a fake cluster, through the
// factory and the registry rather than around them, so that every test here
// also exercises the handshake a real deployment goes through.
func newTestProvider(t *testing.T, f *fakePVE, overrides map[string]string) *Provider {
	t.Helper()
	settings := map[string]string{
		SettingNodes:      "pve-1",
		SettingTemplateID: "9000",
		SettingStorage:    "local-lvm",
		SettingBridge:     "vmbr0",
		SettingVMIDMin:    "9100",
		SettingVMIDMax:    "9199",
	}
	for k, v := range overrides {
		if v == "" {
			delete(settings, k)
			continue
		}
		settings[k] = v
	}
	p, err := NewProvider(provider.Config{
		ProviderID: "prv_test",
		Endpoint:   f.URL,
		Settings:   settings,
		// Pasted whole, exactly as Proxmox prints a new token.
		Credential: tokenID + "=" + tokenSecret,
		CAPEM:      f.caPEM(t),
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("building a provider: %v", err)
	}
	return p
}

// newSpec mints one machine's identity the way the controller does.
func newSpec(cpus float64, memoryMB, diskMB int64) (provider.Owner, provider.MachineSpec) {
	id := store.NewID(store.PrefixMachine)
	owner := provider.Owner{
		ControllerID: store.NewID(store.PrefixController),
		ProviderID:   "prv_test",
		MachineID:    id,
		Fingerprint:  store.NewSecret(12),
	}
	return owner, provider.MachineSpec{
		Name:  store.NewMachineName(id),
		Owner: owner,
		Shape: provider.Shape{CPUs: cpus, MemoryMB: memoryMB, DiskMB: diskMB},
	}
}

// buildMachine allocates and creates one machine, and waits for its clone.
func buildMachine(t *testing.T, p *Provider, spec provider.MachineSpec) provider.MachineRef {
	t.Helper()
	ctx := context.Background()
	ref, err := p.Allocate(ctx, spec)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	op, err := p.Create(ctx, ref, spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !op.Zero() {
		status, err := p.Operation(ctx, op)
		if err != nil {
			t.Fatalf("Operation: %v", err)
		}
		if !status.Done || !status.OK {
			t.Fatalf("the clone did not finish: %+v", status)
		}
	}
	return ref
}

// execCommands is every command this provider asked a guest to run, as the
// argv arrays they were sent as.
func execCommands(t *testing.T, f *fakePVE) [][]string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, r := range f.seen {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/agent/exec") {
			out = append(out, r.PostForm["command"])
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Identity
// ---------------------------------------------------------------------------

// The range is what makes "allow only explicitly configured resources" true.
// Proxmox's own "next free id" knows about the whole cluster and nothing about
// the range an operator allowed this provider, so a fleet driven by it would
// take identifiers somebody else's automation is entitled to.
func TestAnAllocatedVMIDIsTheLowestFreeOneInsideTheConfiguredRange(t *testing.T) {
	f := newFakePVE(t, nil)
	f.SetForeignVM(9100, "somebody-elses-first")
	f.SetForeignVM(9101, "somebody-elses-second")
	p := newTestProvider(t, f, nil)

	_, spec := newSpec(2, 2048, 0)
	ref, err := p.Allocate(context.Background(), spec)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if ref.ID != "9102" {
		t.Errorf("allocated VMID %q, want the lowest free one inside 9100-9199", ref.ID)
	}
	if got := queryValue(f.request(http.MethodGet, v+"/cluster/nextid"), "vmid"); got != "9102" {
		t.Errorf("the identifier was not asserted free before it was used: nextid asked about %q", got)
	}
}

// A range with nothing free in it is a refusal an operator can act on, and not
// the same answer as a cluster that would not talk to us.
func TestARangeWithNothingFreeInItSaysSoRatherThanAllocatingOutsideIt(t *testing.T) {
	f := newFakePVE(t, nil)
	f.SetForeignVM(9100, "taken")
	p := newTestProvider(t, f, map[string]string{SettingVMIDMax: "9100"})

	_, spec := newSpec(2, 2048, 0)
	_, err := p.Allocate(context.Background(), spec)
	if got := provider.KindOf(err); got != provider.FailureQuota {
		t.Fatalf("kind = %q, want %q: a full range is a refusal for want of a resource", got, provider.FailureQuota)
	}
	if !strings.Contains(err.Error(), "9100") {
		t.Errorf("the refusal does not name the range an operator has to widen: %v", err)
	}
}

// Somebody taking the identifier between the listing and the claim is a race a
// busy cluster has all the time, and it is resolved by picking another rather
// than by failing the machine.
func TestAnIdentifierTakenBetweenThePickAndTheClaimIsRePicked(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)
	// 9100 is free in the listing and refused when it is claimed, which is
	// exactly what losing the race looks like from here.
	f.SetError(http.MethodGet, "/cluster/nextid", http.StatusInternalServerError, "VM 9100 already exists")

	_, spec := newSpec(2, 2048, 0)
	_, err := p.Allocate(context.Background(), spec)
	if got := provider.KindOf(err); got != provider.FailureConflict {
		t.Fatalf("kind = %q, want %q", got, provider.FailureConflict)
	}
	asserted := 0
	for _, call := range f.Requests() {
		if strings.HasSuffix(call, "/cluster/nextid") {
			asserted++
		}
	}
	if asserted != allocateAttempts {
		t.Errorf("the identifier was claimed %d times, want %d: the re-pick has to be bounded and has to happen",
			asserted, allocateAttempts)
	}
}

// Filling one node until it falls over is what a provider that always picks the
// first configured node does.
func TestMachinesAreSpreadAcrossTheConfiguredNodes(t *testing.T) {
	f := newFakePVE(t, nil)
	// pve-1 already holds the template and somebody else's virtual machine.
	p := newTestProvider(t, f, map[string]string{SettingNodes: "pve-1,pve-2"})

	_, spec := newSpec(2, 2048, 0)
	ref, err := p.Allocate(context.Background(), spec)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if ref.Zone != "pve-2" {
		t.Errorf("placed on %q, want pve-2: the node with the fewest guests on it", ref.Zone)
	}
}

// ---------------------------------------------------------------------------
// Building
// ---------------------------------------------------------------------------

// The marks have to exist from the moment the guest does. A clone whose answer
// was then lost, carrying neither a name of ours nor an ownership record, is a
// machine nothing could ever tell from somebody else's.
func TestACloneCarriesTheNameAndTheOwnershipRecordFromTheMomentItExists(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)

	owner, spec := newSpec(2, 2048, 0)
	ref := buildMachine(t, p, spec)

	clone := f.request(http.MethodPost, v+"/nodes/pve-1/qemu/9000/clone")
	if got := formValue(clone, "newid"); got != ref.ID {
		t.Errorf("cloned into %q, want the identifier that was allocated, %q", got, ref.ID)
	}
	if got := formValue(clone, "name"); got != spec.Name {
		t.Errorf("cloned as %q, want %q", got, spec.Name)
	}
	if got := formValue(clone, "storage"); got != "local-lvm" {
		t.Errorf("cloned onto storage %q, want the configured one", got)
	}
	if got := formValue(clone, "full"); got != "1" {
		t.Errorf("full = %q, want a full clone by default: a linked clone ties the machine's life to the template's", got)
	}
	if got := formValue(clone, "target"); got != "" {
		t.Errorf("target = %q; cloning onto the template's own node must not ask for a migration", got)
	}
	got, ok := DecodeOwner(formValue(clone, "description"))
	if !ok {
		t.Fatalf("the clone carried no ownership record: %q", formValue(clone, "description"))
	}
	if !got.Matches(owner) {
		t.Errorf("the ownership record is %+v, want the marks this machine was given", got)
	}
}

// Creating twice for one identity builds one machine. It is what makes a
// create re-issued after an outcome nobody heard safe, and the whole reason a
// timeout is not treated as evidence that nothing happened.
func TestASecondCreateForOneIdentityClonesNothing(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)

	_, spec := newSpec(2, 2048, 0)
	ref := buildMachine(t, p, spec)

	op, err := p.Create(context.Background(), ref, spec)
	if err != nil {
		t.Fatalf("the second Create: %v", err)
	}
	if !op.Zero() {
		t.Errorf("the second Create returned handle %q; there was nothing to follow", op.Handle)
	}
	clones := 0
	for _, call := range f.Requests() {
		if strings.HasSuffix(call, "/clone") {
			clones++
		}
	}
	if clones != 1 {
		t.Errorf("%d clones were issued for one identity; the second machine is a second bill nobody is tracking", clones)
	}
}

// A guest somebody else made at our identifier is a conflict, never something
// to build over.
func TestAnIdentifierHoldingSomebodyElsesGuestIsRefused(t *testing.T) {
	f := newFakePVE(t, nil)
	f.SetForeignVM(9100, "the-finance-database")
	p := newTestProvider(t, f, nil)

	_, spec := newSpec(2, 2048, 0)
	_, err := p.Create(context.Background(), provider.MachineRef{Zone: "pve-1", ID: "9100", Name: spec.Name}, spec)
	if got := provider.KindOf(err); got != provider.FailureConflict {
		t.Fatalf("kind = %q, want %q", got, provider.FailureConflict)
	}
	if !strings.Contains(err.Error(), "the-finance-database") {
		t.Errorf("the refusal does not say what is already there: %v", err)
	}
}

// A clone holds a lock on its target until it finishes, so the first moment a
// machine can be sized, marked and started is the call that starts it. All
// three happen there, and this is the test that says so.
func TestAMachineIsSizedAndMarkedWhenItIsStarted(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)

	owner, spec := newSpec(1.5, 4096, 0)
	ref := buildMachine(t, p, spec)

	op, err := p.Start(context.Background(), ref)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if op.Zero() {
		t.Fatal("Start returned no handle; there is a task to follow")
	}
	config := f.request(http.MethodPost, v+"/nodes/pve-1/qemu/"+ref.ID+"/config")
	for key, want := range map[string]string{
		// 1.5 processors is not a thing a guest can be given, and a runner
		// with less than it was promised is a slow build nobody can explain.
		"cores":   "2",
		"sockets": "1",
		"memory":  "4096",
		"net0":    "virtio,bridge=vmbr0",
		"agent":   "1",
		// Never on boot: a machine that came back by itself after a hypervisor
		// reboot would be a host nothing is accounting for.
		"onboot":    "0",
		"ipconfig0": "ip=dhcp",
	} {
		if got := formValue(config, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	tags := ParseTags(formValue(config, "tags"))
	if len(tags) != 2 || tags[0] != FleetTag || tags[1] != MachineTag(owner.MachineID) {
		t.Errorf("tags = %v, want the fleet's and this machine's: the sweep filters a whole cluster on them", tags)
	}
	if f.request(http.MethodPost, v+"/nodes/pve-1/qemu/"+ref.ID+"/status/start") == nil {
		t.Error("the machine was configured and never started")
	}
}

// The shape is recorded on the guest rather than kept in this process, because
// the call that sizes a machine is handed a reference and no specification and
// a controller may have restarted in between. A machine that came back the
// template's size rather than the operator's would be wrong in a way nothing
// would ever report.
func TestTheShapeAMachineWasAskedForSurvivesAControllerRestart(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)

	_, spec := newSpec(4, 8192, 0)
	ref := buildMachine(t, p, spec)

	// A different provider instance entirely: nothing of the create is in it.
	restarted := newTestProvider(t, f, nil)
	if _, err := restarted.Start(context.Background(), ref); err != nil {
		t.Fatalf("Start: %v", err)
	}
	config := f.request(http.MethodPost, v+"/nodes/pve-1/qemu/"+ref.ID+"/config")
	if got := formValue(config, "cores"); got != "4" {
		t.Errorf("cores = %q, want 4: the shape was read back off the machine", got)
	}
	if got := formValue(config, "memory"); got != "8192" {
		t.Errorf("memory = %q, want 8192", got)
	}
}

// Another operation holding the guest is something to wait for and observe,
// never something to force or to build a second machine over.
func TestALockedGuestIsAConflictRatherThanAFailure(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)

	_, spec := newSpec(2, 2048, 0)
	ref := buildMachine(t, p, spec)
	vmid, _ := strconv.Atoi(ref.ID)
	f.SetLock(vmid, "backup")

	_, err := p.Start(context.Background(), ref)
	if got := provider.KindOf(err); got != provider.FailureConflict {
		t.Fatalf("kind = %q, want %q", got, provider.FailureConflict)
	}
	got, err := p.Inspect(context.Background(), ref)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.Locked != "backup" {
		t.Errorf("Locked = %q; a machine held by something else has to say so rather than look idle", got.Locked)
	}
}

// The disk is the one part of a shape Proxmox will not take back, so it is only
// ever grown, and only when the shape asks for more than the template has.
// Asking for a resize that is not needed would turn every start of an
// already-correct machine into a failure.
func TestADiskIsGrownOnlyWhenTheShapeAsksForMoreThanTheTemplateHas(t *testing.T) {
	for _, tc := range []struct {
		name   string
		diskMB int64
		want   string
	}{
		{"a shape the template already satisfies", 20480, ""},
		{"a shape that needs more", 40960, "40960M"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var f *fakePVE
			f = newFakePVE(t, map[string]http.HandlerFunc{
				// The built-in configuration route carries no disk at all, and
				// the size is what this case is about.
				"GET " + v + "/nodes/{node}/qemu/{vmid}/config": func(w http.ResponseWriter, r *http.Request) {
					vm := f.vm(pathInt(r, "vmid"))
					if vm == nil {
						writeFailure(w, http.StatusNotFound, "Configuration file does not exist")
						return
					}
					writeData(w, map[string]any{
						"name": vm.name, "description": vm.description, "tags": vm.tags,
						"agent": vm.agent, "cores": 2, "memory": "4096",
						"scsi0": "local-lvm:vm-" + strconv.Itoa(vm.vmid) + "-disk-0,size=20G",
					})
				},
			})
			p := newTestProvider(t, f, nil)

			_, spec := newSpec(2, 2048, tc.diskMB)
			ref := buildMachine(t, p, spec)
			if _, err := p.Start(context.Background(), ref); err != nil {
				t.Fatalf("Start: %v", err)
			}
			resize := f.request(http.MethodPut, v+"/nodes/pve-1/qemu/"+ref.ID+"/resize")
			switch {
			case tc.want == "" && resize != nil:
				t.Errorf("a resize was issued for a disk that is already big enough: %q", formValue(resize, "size"))
			case tc.want == "":
			case resize == nil:
				t.Fatal("the disk was never grown")
			default:
				if got := formValue(resize, "size"); got != tc.want {
					t.Errorf("size = %q, want %q", got, tc.want)
				}
				if got := formValue(resize, "disk"); got != "scsi0" {
					t.Errorf("disk = %q, want scsi0", got)
				}
			}
		})
	}
}

// Proxmox writes a size with whichever unit suits it, and a size read wrongly
// is a disk grown to a thousandth of what was asked for.
func TestADiskSizeIsReadInProxmoxsOwnGrammar(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want int64
		ok   bool
	}{
		{"local-lvm:vm-143-disk-0,size=20G", 20480, true},
		{"ceph:vm-143-disk-0,size=32768M,ssd=1", 32768, true},
		{"local:143/vm-143-disk-0.qcow2,size=1T", 1 << 20, true},
		{"local-lvm:vm-143-disk-0", 0, false},
		{"local-lvm:vm-143-disk-0,size=", 0, false},
		{"none", 0, false},
	} {
		got, ok := diskSizeMB(tc.spec)
		if ok != tc.ok || got != tc.want {
			t.Errorf("diskSizeMB(%q) = %d, %t; want %d, %t", tc.spec, got, ok, tc.want, tc.ok)
		}
	}
}

// ---------------------------------------------------------------------------
// Reading
// ---------------------------------------------------------------------------

// The sweep has to tell an orphan of our own from another fleet's machine, and
// the marks that say which are only readable from a guest's own configuration
// -- never from the cluster-wide listing, which carries tags alone.
func TestTheSweepReadsEachCandidatesMarksBackOffTheGuestItself(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)

	owner, spec := newSpec(2, 2048, 0)
	ref := buildMachine(t, p, spec)
	// A guest wearing our naming grammar and nobody's marks: an orphan, which
	// is reported and never deleted.
	f.SetForeignVM(9150, store.NewMachineName("mach_orphaned"))

	seen, err := p.List(context.Background(), owner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := map[string]provider.Machine{}
	for _, m := range seen {
		found[m.Ref.Name] = m
	}
	if len(found) != 2 {
		t.Fatalf("the sweep found %d guests, want ours and the orphan: %v", len(found), found)
	}
	ours := found[spec.Name]
	if ours.Ref.ID != ref.ID || !ours.Owner.Matches(owner) {
		t.Errorf("our own machine came back as %+v; the marks have to be the ones written on it", ours)
	}
	if orphan := found[store.NewMachineName("mach_orphaned")]; !orphan.Owner.Zero() {
		t.Errorf("an unmarked guest came back owned by %+v; an unmarked resource and one with somebody else's marks are different things", orphan.Owner)
	}
	if _, taken := found["someone-elses-vm"]; taken {
		t.Error("the sweep claimed a guest wearing neither our tag nor our naming grammar")
	}
	if _, taken := found["ubuntu-24.04-template"]; taken {
		t.Error("the sweep counted the template as a machine")
	}
}

// A sweep that quietly returned fewer machines than exist would read as rows
// whose resources have gone, and that answer drains hosts.
func TestASweepThatCannotReadAGuestFailsRatherThanReportingFewerMachines(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)

	owner, spec := newSpec(2, 2048, 0)
	ref := buildMachine(t, p, spec)
	f.SetError(http.MethodGet, "/qemu/"+ref.ID+"/config", http.StatusForbidden,
		"Permission check failed (/vms/"+ref.ID+", VM.Audit)")

	if _, err := p.List(context.Background(), owner); err == nil {
		t.Fatal("List reported a partial sweep as a complete one")
	} else if got := provider.KindOf(err); got != provider.FailurePermission {
		t.Errorf("kind = %q, want %q", got, provider.FailurePermission)
	}
}

// A task that failed is the only place an operator is told which storage was
// full, so its own words and its log both have to reach the machine's page.
func TestAFailedTaskCarriesProxmoxsOwnWordsAndItsLog(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)

	_, spec := newSpec(2, 2048, 0)
	ref, err := p.Allocate(context.Background(), spec)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	op, err := p.Create(context.Background(), ref, spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.SetTaskPending(op.Handle, 1)
	f.SetTaskFailure(op.Handle, "unable to create image: storage 'local-lvm' is full")

	status, err := p.Operation(context.Background(), op)
	if err != nil {
		t.Fatalf("Operation: %v", err)
	}
	if status.Done {
		t.Fatal("a task that is still running was reported as finished")
	}
	status, err = p.Operation(context.Background(), op)
	if err != nil {
		t.Fatalf("Operation: %v", err)
	}
	if !status.Done || status.OK {
		t.Fatalf("status = %+v, want a task that finished badly", status)
	}
	if !strings.Contains(status.Detail, "local-lvm' is full") {
		t.Errorf("the failure does not carry Proxmox's own words: %q", status.Detail)
	}
	if !strings.Contains(status.Detail, "TASK ERROR") {
		t.Errorf("the failure does not carry the task's log: %q", status.Detail)
	}
}

// ---------------------------------------------------------------------------
// Removing
// ---------------------------------------------------------------------------

// Proxmox refuses to destroy a running guest, so a delete that finds one stops
// it and hands that task back. The caller follows it, finds the resource still
// there, and asks again -- one more pass, and every state of it durable.
func TestARunningMachineIsShutDownBeforeItIsDestroyed(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)

	_, spec := newSpec(2, 2048, 0)
	ref := buildMachine(t, p, spec)
	if _, err := p.Start(context.Background(), ref); err != nil {
		t.Fatalf("Start: %v", err)
	}

	op, err := p.Delete(context.Background(), ref)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if op.Zero() {
		t.Fatal("Delete of a running machine returned nothing to follow")
	}
	shutdown := f.request(http.MethodPost, v+"/nodes/pve-1/qemu/"+ref.ID+"/status/shutdown")
	if shutdown == nil {
		t.Fatal("a running machine was destroyed without being stopped first; Proxmox refuses that")
	}
	if got := formValue(shutdown, "forceStop"); got != "1" {
		t.Errorf("forceStop = %q, want 1: a guest that will not go down must not leave a machine nothing can destroy", got)
	}
	if f.request(http.MethodDelete, v+"/nodes/pve-1/qemu/"+ref.ID) != nil {
		t.Error("the destroy was issued while the guest was still running")
	}

	// The next pass: stopped now, so the destroy goes out.
	if _, err := p.Delete(context.Background(), ref); err != nil {
		t.Fatalf("the second Delete: %v", err)
	}
	destroy := f.request(http.MethodDelete, v+"/nodes/pve-1/qemu/"+ref.ID)
	if destroy == nil {
		t.Fatal("the machine was never destroyed")
	}
	if got := queryValue(destroy, "purge"); got != "1" {
		t.Errorf("purge = %q, want 1: a backup job left behind outlives the guest it referred to", got)
	}
	if got := queryValue(destroy, "destroy-unreferenced-disks"); got != "1" {
		t.Errorf("destroy-unreferenced-disks = %q, want 1: a disk nothing refers to keeps costing money under a name nothing owns", got)
	}
	if _, err := p.Inspect(context.Background(), ref); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("Inspect after the destroy = %v, want not found", err)
	}
}

// ---------------------------------------------------------------------------
// Bootstrap
// ---------------------------------------------------------------------------

// The guest agent has no way to say what mode a file should have, and the file
// it writes holds a join token. The chmod is what keeps that credential from
// being world-readable, and it goes immediately after the write rather than
// once at the end.
func TestTheEnrolmentFileIsChmoddedBecauseFileWriteHasNoMode(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)

	_, spec := newSpec(2, 2048, 0)
	ref := buildMachine(t, p, spec)

	const path = "/etc/zoomies/zoomies.env"
	op, err := p.Bootstrap(context.Background(), ref, provider.Bootstrap{
		Files: []provider.File{{Path: path, Mode: 0o600, Content: []byte("ZOOMIES_JOIN_TOKEN=secret\n")}},
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !op.Zero() {
		t.Errorf("a payload with no commands returned handle %q; there was nothing to follow", op.Handle)
	}
	vmid, _ := strconv.Atoi(ref.ID)
	if got := f.vm(vmid).files[path]; got != "ZOOMIES_JOIN_TOKEN=secret\n" {
		t.Errorf("the guest holds %q at %s", got, path)
	}
	want := [][]string{{"/bin/chmod", "0600", path}}
	if got := execCommands(t, f); !equalCommands(got, want) {
		t.Errorf("the commands run inside the guest were %v, want %v", got, want)
	}
}

// A payload that is never pasted into a shell has no quoting to get wrong and
// no injection class to police. This is the test that keeps it that way.
func TestTheBootstrapNeverBuildsAShellCommandLine(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, nil)

	_, spec := newSpec(2, 2048, 0)
	ref := buildMachine(t, p, spec)

	if _, err := p.Bootstrap(context.Background(), ref, provider.Bootstrap{
		Files: []provider.File{{Path: "/etc/zoomies/zoomies.env", Mode: 0o600, Content: []byte("ZOOMIES_AGENT_LABELS=a b;c\n")}},
		Commands: [][]string{
			{"/bin/chmod", "0600", "/etc/zoomies/zoomies.env"},
			{"/bin/systemctl", "enable", "--now", "zoomies-agent"},
		},
	}); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	shells := []string{"/bin/sh", "/bin/bash", "sh", "bash", "-c"}
	for _, argv := range execCommands(t, f) {
		if len(argv) == 0 {
			t.Fatal("a command was sent with no program in it")
		}
		for _, arg := range argv {
			for _, shell := range shells {
				if arg == shell {
					t.Errorf("a command was run through a shell: %v", argv)
				}
			}
		}
		// Every argument is its own form value, which is what an argv array is
		// on this wire. One value holding the whole line would mean a shell was
		// assembled somewhere.
		if strings.Contains(argv[0], " ") {
			t.Errorf("the program to run is a line rather than a program: %q", argv[0])
		}
	}
}

// The guest's own standard error is the only sentence that ever says why an
// agent did not install, so it is what a machine's page carries.
func TestABootstrapFailureCarriesTheGuestsOwnStandardError(t *testing.T) {
	const complaint = "Failed to enable unit: Unit zoomies-agent.service does not exist."
	f := newFakePVE(t, map[string]http.HandlerFunc{
		"GET " + v + "/nodes/{node}/qemu/{vmid}/agent/exec-status": func(w http.ResponseWriter, r *http.Request) {
			writeData(w, map[string]any{"exited": 1, "exitcode": 1, "out-data": "", "err-data": complaint})
		},
	})
	p := newTestProvider(t, f, nil)

	_, spec := newSpec(2, 2048, 0)
	ref := buildMachine(t, p, spec)

	op, err := p.Bootstrap(context.Background(), ref, provider.Bootstrap{
		Commands: [][]string{{"/bin/systemctl", "enable", "--now", "zoomies-agent"}},
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	status, err := p.Operation(context.Background(), op)
	if err != nil {
		t.Fatalf("Operation: %v", err)
	}
	if !status.Done || status.OK {
		t.Fatalf("status = %+v, want a command that finished badly", status)
	}
	if !strings.Contains(status.Detail, complaint) {
		t.Errorf("the failure does not carry what the guest said: %q", status.Detail)
	}
	if !strings.Contains(status.Detail, "exited 1") {
		t.Errorf("the failure does not say how the command ended: %q", status.Detail)
	}
}

// A handle for a process inside a guest is not a cluster task, and has to carry
// the node and the guest as well as the process: nothing else about it is
// stored, and a controller that restarts has only the handle.
func TestAGuestCommandsHandleNamesItsNodeItsGuestAndItsProcess(t *testing.T) {
	handle := execHandle("pve-1", 9100, 4242)
	node, vmid, pid, ok := parseExecHandle(handle)
	if !ok || node != "pve-1" || vmid != 9100 || pid != 4242 {
		t.Fatalf("parseExecHandle(%q) = %q, %d, %d, %t", handle, node, vmid, pid, ok)
	}
	if _, err := ParseUPID(handle); err == nil {
		t.Error("a guest-exec handle parses as a cluster task; the two must not be confused")
	}
	for _, notOne := range []string{"UPID:pve-1:0005:1:65F0A1B2:qmclone:143:root@pam:", "guest-exec:pve-1:9100", "", "guest-exec:::"} {
		if _, _, _, ok := parseExecHandle(notOne); ok {
			t.Errorf("%q was read as a guest-exec handle", notOne)
		}
	}
}

// A guest thirty seconds into its boot and a template with no agent in it
// answer the same way, so the wait is what tells them apart -- and when it runs
// out, the remedy is the one that is nearly always right.
func TestAGuestAgentThatNeverAnswersNamesTheTemplateAsTheFix(t *testing.T) {
	f := newFakePVE(t, nil)
	f.SetGuestAgentSilent()
	p := newTestProvider(t, f, nil)

	_, spec := newSpec(2, 2048, 0)
	ref := buildMachine(t, p, spec)

	ctx, cancel := context.WithTimeout(context.Background(), guestAgentPollInterval/2)
	defer cancel()
	_, err := p.Bootstrap(ctx, ref, provider.Bootstrap{})
	if got := provider.KindOf(err); got != provider.FailureRefused {
		t.Fatalf("kind = %q, want %q: nothing was asked of the machine, so nothing can have happened to it", got, provider.FailureRefused)
	}
	var pe *provider.Error
	if !errors.As(err, &pe) || !strings.Contains(pe.Remedy, "qemu-guest-agent") {
		t.Errorf("the refusal does not name the template as the fix: %v", err)
	}
}

// The guest agent reaps a process the moment its status is read, so a handle
// asked about twice is one it has genuinely forgotten. Not-found sends the
// caller to look at the machine; a failure would have it poll something that
// can never answer again.
func TestAProcessTheGuestAgentHasForgottenIsNotFound(t *testing.T) {
	f := newFakePVE(t, map[string]http.HandlerFunc{
		"GET " + v + "/nodes/{node}/qemu/{vmid}/agent/exec-status": func(w http.ResponseWriter, r *http.Request) {
			writeFailure(w, http.StatusInternalServerError, "guest-exec-status: Invalid parameter 'pid'")
		},
	})
	p := newTestProvider(t, f, nil)

	_, err := p.Operation(context.Background(), provider.OperationRef{
		Kind: provider.OpBootstrap, Handle: execHandle("pve-1", 9100, 4242),
	})
	if !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("Operation on a forgotten process = %v, want not found", err)
	}
}

// ---------------------------------------------------------------------------
// Settings and the factory
// ---------------------------------------------------------------------------

// The registry is what a build hands the reconciler, and it refuses a provider
// whose description and instance disagree. Going through it here means every
// test above has already passed that handshake.
func TestTheFactoryBuildsAProviderTheRegistryAccepts(t *testing.T) {
	f := newFakePVE(t, nil)
	registry, err := provider.NewRegistry(NewFactory())
	if err != nil {
		t.Fatalf("building a registry: %v", err)
	}
	p, err := registry.New(context.Background(), store.ProviderProxmox, provider.Config{
		ProviderID: "prv_test",
		Endpoint:   f.URL,
		Settings: map[string]string{
			SettingNodes: "pve-1", SettingTemplateID: "9000", SettingStorage: "local-lvm",
			SettingBridge: "vmbr0", SettingVMIDMin: "9100", SettingVMIDMax: "9199",
		},
		Credential: tokenID + "=" + tokenSecret,
		CAPEM:      f.caPEM(t),
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("building a provider through the registry: %v", err)
	}
	if p.Capabilities().Bootstrap != provider.BootstrapGuestAgent {
		t.Errorf("bootstrap mode = %q, want %q: the metadata path cannot be driven from this API at all",
			p.Capabilities().Bootstrap, provider.BootstrapGuestAgent)
	}
	if _, ok := p.(provider.Bootstrapper); !ok {
		t.Error("the provider declares that it pushes the payload itself and does not implement Bootstrapper")
	}
}

// Every setting the form offers is one the provider reads, and every finding
// names a setting that exists. A setting that appears in one of the three and
// not the others is how a form asks for something nothing acts on.
func TestEverySettingTheFormOffersIsOneThisProviderReads(t *testing.T) {
	offered := map[string]bool{}
	for _, spec := range NewFactory().Settings() {
		if spec.Key == "" || spec.Label == "" || spec.Help == "" {
			t.Errorf("setting %q is missing its key, label or help", spec.Key)
		}
		offered[spec.Key] = true
	}
	for _, key := range []string{
		SettingNodes, SettingStorage, SettingBridge, SettingTemplateID, SettingTemplateNode,
		SettingPool, SettingVMIDMin, SettingVMIDMax, SettingTokenID, SettingFullClone,
		SettingIPConfig, SettingNameserver, SettingControllerURL,
	} {
		if !offered[key] {
			t.Errorf("the form never asks for %q", key)
		}
	}
	for _, f := range NewFactory().Validate(map[string]string{}) {
		if f.Setting != "" && !offered[f.Setting] {
			t.Errorf("finding %s points at %q, which the form does not ask for", f.Code, f.Setting)
		}
	}
}

// An answer that is wrong is wrong before anything is dialled, and an operator
// fixing a form should not have to submit it four times to be told four things.
func TestSettingsThatAreWrongAreRefusedBeforeAnythingIsDialled(t *testing.T) {
	findings := NewFactory().Validate(map[string]string{SettingVMIDMin: "9100", SettingVMIDMax: "9000"})
	codes := map[string]bool{}
	for _, f := range findings {
		codes[f.Code] = true
	}
	for _, want := range []string{
		"proxmox.node_missing", "proxmox.storage_missing", "proxmox.bridge_missing",
		"proxmox.template_missing", "proxmox.vmid_range",
	} {
		if !codes[want] {
			t.Errorf("nothing said %s about a form with none of it filled in", want)
		}
	}

	f := newFakePVE(t, nil)
	_, err := NewProvider(provider.Config{
		Endpoint: f.URL, Credential: tokenID + "=" + tokenSecret,
		Settings: map[string]string{SettingNodes: "pve-1"},
	})
	if got := provider.KindOf(err); got != provider.FailureConfig {
		t.Fatalf("kind = %q, want %q", got, provider.FailureConfig)
	}
	if !strings.Contains(err.Error(), "storage") || !strings.Contains(err.Error(), "template") {
		t.Errorf("the refusal names one thing to fix rather than all of them: %v", err)
	}
}

// Proxmox prints a new token as one string and that is what gets pasted, so
// that is what has to work. The name is not a secret: it is what every message
// asking for a privilege to be granted has to quote.
func TestATokenPastedWholeIsSplitIntoItsNameAndItsSecret(t *testing.T) {
	for _, tc := range []struct {
		name       string
		setting    string
		credential string
		wantID     string
		wantSecret string
	}{
		{"pasted whole", "", "zoomies@pve!fleet=s3cret", "zoomies@pve!fleet", "s3cret"},
		{"kept apart", "zoomies@pve!fleet", "s3cret", "zoomies@pve!fleet", "s3cret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, secret, err := splitCredential(tc.setting, tc.credential)
			if err != nil {
				t.Fatalf("splitCredential: %v", err)
			}
			if id != tc.wantID || secret != tc.wantSecret {
				t.Errorf("= %q, %q; want %q, %q", id, secret, tc.wantID, tc.wantSecret)
			}
		})
	}
	if _, _, err := splitCredential("zoomies@pve!fleet", ""); err == nil {
		t.Error("a provider with no token at all was accepted")
	}
}

// The shape and the ownership record share a description and answer different
// questions, so neither decoder may be confused by the other's line.
func TestTheShapeAndTheOwnershipRecordDoNotReadEachOther(t *testing.T) {
	owner := provider.Owner{ControllerID: "ctl_a", ProviderID: "prv_b", MachineID: "mach_c", Fingerprint: "ffff"}
	description := describeMachine(owner, "zoomies-mach-c", provider.Shape{CPUs: 2, MemoryMB: 4096, DiskMB: 20480})

	got, ok := DecodeOwner(description)
	if !ok || !got.Matches(owner) {
		t.Fatalf("DecodeOwner = %+v, %t", got, ok)
	}
	shape, ok := decodeShape(description)
	if !ok || shape.Cores != 2 || shape.MemoryMB != 4096 || shape.DiskMB != 20480 {
		t.Fatalf("decodeShape = %+v, %t", shape, ok)
	}
	// An operator's own note, and a description that never carried a shape.
	if _, ok := decodeShape(Describe(owner, "zoomies-mach-c") + "\nreserved for the release build"); ok {
		t.Error("a description with no shape in it produced one")
	}
	if _, ok := decodeShape(`{"zoomies":{"machine":"mach_c"}}`); ok {
		t.Error("the ownership record was read as a shape")
	}
	var block shapeBlock
	line := strings.Split(description, "\n")
	if err := json.Unmarshal([]byte(line[len(line)-1]), &block); err != nil {
		t.Errorf("the shape is not the last line of the description, so an operator's note would displace it: %v", err)
	}
}

func equalCommands(got, want [][]string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if fmt.Sprint(got[i]) != fmt.Sprint(want[i]) {
			return false
		}
	}
	return true
}

// A machine whose interface is rewritten without its hardware address gets a
// new one, takes a new DHCP lease with it, and loses whatever reservation
// somebody made for it. Configuring a machine twice has to be harmless.
func TestConfiguringAMachineKeepsTheAddressItsInterfaceAlreadyHas(t *testing.T) {
	for _, tc := range []struct{ name, existing, want string }{
		{"the interface a clone was given", "virtio=AA:BB:CC:DD:EE:FF,bridge=vmbr9,firewall=1", "virtio=AA:BB:CC:DD:EE:FF,bridge=vmbr0"},
		{"a model the operator chose", "e1000=AA:BB:CC:DD:EE:FF,bridge=vmbr9", "e1000=AA:BB:CC:DD:EE:FF,bridge=vmbr0"},
		{"a guest with no interface at all", "", "virtio,bridge=vmbr0"},
		{"a first field that is not an address", "virtio,bridge=vmbr9", "virtio,bridge=vmbr0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := netDevice(tc.existing, "vmbr0"); got != tc.want {
				t.Errorf("netDevice(%q) = %q, want %q", tc.existing, got, tc.want)
			}
		})
	}
}

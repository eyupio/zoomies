package proxmox

import (
	"context"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/provider"
)

// lxcSettings is what a provider that builds containers is configured with,
// over the same fake cluster the VM tests use.
var lxcSettings = map[string]string{
	SettingMachineType: MachineTypeLXC,
	SettingOSTemplate:  "local:vztmpl/zoomies-runner.tar.zst",
	SettingTemplateID:  "", // a container has no VM template
}

// A container provider has to satisfy exactly what a VM provider does, because
// the controller cannot tell them apart: the contract is the definition of done.
func TestTheProxmoxProviderObeysTheContractWhenItBuildsContainers(t *testing.T) {
	provider.RunContractTests(t, "proxmox-lxc", func(t *testing.T) provider.Provider {
		return newTestProvider(t, newFakePVE(t, nil), lxcSettings)
	})
}

func TestAContainerIsCreatedUnprivilegedWithNestingAndItsOwnershipRecord(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, map[string]string{
		SettingMachineType: MachineTypeLXC, SettingOSTemplate: "local:vztmpl/zoomies-runner.tar.zst",
		SettingTemplateID: "", SettingVLAN: "30",
	})
	owner, spec := newSpec(1.5, 4096, 20*1024)
	ref := buildMachine(t, p, spec)

	if !strings.HasPrefix(ref.ID, lxcPrefix) || !p.Claims(ref) {
		t.Fatalf("a container's identifier should say so, got %q", ref.ID)
	}
	vm := f.vm(9100)
	if vm == nil || !vm.lxc {
		t.Fatalf("no container was created at VMID 9100: %+v", vm)
	}
	for key, want := range map[string]string{
		// The one shape an API token may ask for.
		"unprivileged": "1", "features": "nesting=1",
		"ostemplate": "local:vztmpl/zoomies-runner.tar.zst", "hostname": spec.Name,
		// Rounded up, because a runner given less than it was promised is a slow
		// build nobody can explain.
		"cores": "2", "memory": "4096", "rootfs": "local-lvm:20",
		"net0":  "name=eth0,bridge=vmbr0,ip=dhcp,tag=30",
		"start": "0", "onboot": "0",
	} {
		if got := vm.form.Get(key); got != want {
			t.Errorf("%s was sent as %q, want %q", key, got, want)
		}
	}
	// The marks are on it from the instant it exists, which is what lets a
	// create whose answer was lost be found and proved ours.
	if got, ok := DecodeOwner(vm.description); !ok || got.MachineID != owner.MachineID {
		t.Errorf("the container does not carry its ownership record: %q", vm.description)
	}
	if !HasFleetTag(vm.tags) {
		t.Errorf("the container does not wear the fleet's tag: %q", vm.tags)
	}
}

func TestAContainerWithNoSizeInTheRowGetsADiskBigEnoughForDocker(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, lxcSettings)
	_, spec := newSpec(0, 0, 0)
	buildMachine(t, p, spec)
	if got := f.vm(9100).form.Get("rootfs"); got != "local-lvm:32" {
		t.Errorf("rootfs = %q, want the default 32 GiB", got)
	}
}

func TestASecondCreateForOneContainerBuildsOne(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, lxcSettings)
	_, spec := newSpec(2, 4096, 0)
	ref := buildMachine(t, p, spec)
	op, err := p.Create(context.Background(), ref, spec)
	if err != nil || !op.Zero() {
		t.Fatalf("a repeated create should be a no-op, got %+v, %v", op, err)
	}
	creates := 0
	for _, r := range f.Requests() {
		if r == "POST "+v+"/nodes/pve-1/lxc" {
			creates++
		}
	}
	if creates != 1 {
		t.Errorf("the container was created %d times", creates)
	}
}

func TestAContainerIsStartedWithoutBeingConfiguredAndReportsItsAddress(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, lxcSettings)
	ctx := context.Background()
	_, spec := newSpec(2, 4096, 0)
	ref := buildMachine(t, p, spec)

	if _, err := p.Start(ctx, ref); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for _, r := range f.Requests() {
		if strings.HasSuffix(r, "/config") && strings.HasPrefix(r, "PUT") {
			t.Errorf("a container was configured after its create: %s", r)
		}
	}
	got, err := p.Inspect(ctx, ref)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.Phase != provider.PhaseRunning || got.Ref.ID != ref.ID || got.Owner.MachineID == "" {
		t.Errorf("Inspect = %+v", got)
	}
	// The loopback address is not where the container is.
	if got.Address != f.vm(9100).address {
		t.Errorf("Address = %q, want %q", got.Address, f.vm(9100).address)
	}
}

func TestAStoppedContainerHasNoAddressYetAndThatIsNotAFailure(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, lxcSettings)
	_, spec := newSpec(2, 4096, 0)
	ref := buildMachine(t, p, spec)
	got, err := p.Inspect(context.Background(), ref)
	if err != nil || got.Address != "" {
		t.Fatalf("a container that is not running has no address to report, got %q, %v", got.Address, err)
	}
}

// What kind of guest a machine is travels with its identifier, so a provider
// whose settings were switched back to VMs still destroys the container it
// built rather than asking for a VM at that number.
func TestAContainerStaysAContainerWhenTheMachineTypeIsChangedAfterwards(t *testing.T) {
	f := newFakePVE(t, nil)
	ctx := context.Background()
	_, spec := newSpec(2, 4096, 0)
	ref := buildMachine(t, newTestProvider(t, f, lxcSettings), spec)

	vms := newTestProvider(t, f, nil) // the same cluster, now configured for VMs
	got, err := vms.Inspect(ctx, ref)
	if err != nil || got.Phase != provider.PhaseStopped {
		t.Fatalf("a VM provider should still find the container, got %+v, %v", got, err)
	}
	if _, err := vms.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if f.vm(9100) != nil {
		t.Error("the container was not destroyed")
	}
}

func TestTheSweepFindsBothKindsOfGuestAndLeavesForeignContainersAlone(t *testing.T) {
	f := newFakePVE(t, nil)
	ctx := context.Background()
	_, vmSpec := newSpec(2, 4096, 0)
	vmRef := buildMachine(t, newTestProvider(t, f, nil), vmSpec)
	_, ctSpec := newSpec(2, 4096, 0)
	ctRef := buildMachine(t, newTestProvider(t, f, lxcSettings), ctSpec)
	f.SetForeignVM(9150, "somebody-elses-container")
	f.mu.Lock()
	f.vms[9150].lxc = true
	f.mu.Unlock()

	got, err := newTestProvider(t, f, nil).List(ctx, provider.Owner{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	ids := map[string]bool{}
	for _, m := range got {
		ids[m.Ref.ID] = true
	}
	if len(got) != 2 || !ids[vmRef.ID] || !ids[ctRef.ID] {
		t.Errorf("the sweep should find exactly the VM %q and the container %q, got %+v", vmRef.ID, ctRef.ID, got)
	}
}

func TestACrossKindIdentifierCollisionIsAConflictNotAnAdoption(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, lxcSettings)
	// A VM already holds the number the container was allocated: Proxmox numbers
	// both kinds from one sequence.
	f.SetForeignVM(9100, "somebody-elses-vm")
	ref := provider.MachineRef{Zone: "pve-1", ID: lxcPrefix + "9100", Name: "zoomies-test"}
	_, spec := newSpec(2, 4096, 0)
	spec.Name = ref.Name
	_, err := p.Create(context.Background(), ref, spec)
	// The container's own status call cannot see the VM, and the create is then
	// refused by the cluster rather than overwriting it.
	if err == nil {
		t.Fatal("a container was created over a VM")
	}
	if vm := f.vm(9100); vm == nil || vm.lxc {
		t.Errorf("the VM at 9100 was disturbed: %+v", vm)
	}
}

func TestAnAllocationNeverPicksANumberAVMHolds(t *testing.T) {
	f := newFakePVE(t, nil)
	f.SetForeignVM(9100, "somebody-elses-vm")
	p := newTestProvider(t, f, lxcSettings)
	_, spec := newSpec(2, 4096, 0)
	ref, err := p.Allocate(context.Background(), spec)
	if err != nil || ref.ID != lxcPrefix+"9101" {
		t.Fatalf("Allocate = %+v, %v; want the next free number across both kinds", ref, err)
	}
}

func TestAContainerIsNeverBootstrappedThroughAGuestAgent(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, lxcSettings)
	_, spec := newSpec(2, 4096, 0)
	ref := buildMachine(t, p, spec)
	if _, err := p.Bootstrap(context.Background(), ref, provider.Bootstrap{}); provider.KindOf(err) != provider.FailureInternal {
		t.Errorf("Bootstrap on a container = %v, want an internal failure", err)
	}
	for _, r := range f.Requests() {
		if strings.Contains(r, "/agent/") {
			t.Errorf("the guest agent was called for a container: %s", r)
		}
	}
	if newTestProvider(t, f, nil).Claims(provider.MachineRef{Zone: "pve-1", ID: "9100"}) {
		t.Error("a VM claims its credential")
	}
}

func TestContainerSettingsAreCheckedBeforeAnythingIsDialled(t *testing.T) {
	base := func() map[string]string {
		return map[string]string{
			SettingNodes: "pve-1", SettingStorage: "local-lvm", SettingBridge: "vmbr0",
			SettingVMIDMin: "9100", SettingVMIDMax: "9199",
			SettingMachineType: MachineTypeLXC, SettingOSTemplate: "local:vztmpl/zoomies-runner.tar.zst",
		}
	}
	for name, tc := range map[string]struct {
		edit    func(map[string]string)
		setting string // empty: no finding expected
	}{
		"a container provider needs no VM template": {func(map[string]string) {}, ""},
		"a container template is required":          {func(m map[string]string) { delete(m, SettingOSTemplate) }, SettingOSTemplate},
		"a VM template is not a container template": {func(m map[string]string) { m[SettingOSTemplate] = "9000" }, SettingOSTemplate},
		"an unknown machine type":                   {func(m map[string]string) { m[SettingMachineType] = "lxd" }, SettingMachineType},
		"a VLAN out of range":                       {func(m map[string]string) { m[SettingVLAN] = "5000" }, SettingVLAN},
		"a VLAN that is not a number":               {func(m map[string]string) { m[SettingVLAN] = "blue" }, SettingVLAN},
	} {
		t.Run(name, func(t *testing.T) {
			in := base()
			tc.edit(in)
			var got []string
			for _, f := range (Factory{}).Validate(in) {
				if f.Severity == config.SeverityError {
					got = append(got, f.Setting)
				}
			}
			switch {
			case tc.setting == "" && len(got) != 0:
				t.Errorf("unexpected findings on %v", got)
			case tc.setting != "" && (len(got) != 1 || got[0] != tc.setting):
				t.Errorf("findings on %v, want one on %s", got, tc.setting)
			}
		})
	}
	// Every row written before containers existed means a VM, and still needs
	// its template.
	vm := map[string]string{SettingNodes: "pve-1", SettingStorage: "s", SettingBridge: "b", SettingVMIDMin: "9100", SettingVMIDMax: "9199"}
	if got := (Factory{}).Validate(vm); len(got) != 1 || got[0].Setting != SettingTemplateID {
		t.Errorf("a VM provider with no template should be refused on the template, got %+v", got)
	}
}

func TestAVMGetsItsVLANTagToo(t *testing.T) {
	f := newFakePVE(t, nil)
	p := newTestProvider(t, f, map[string]string{SettingVLAN: "30"})
	_, spec := newSpec(2, 4096, 0)
	ref := buildMachine(t, p, spec)
	if _, err := p.Start(context.Background(), ref); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := formValue(f.request("PUT", v+"/nodes/pve-1/qemu/9100/config"), "net0"); !strings.HasSuffix(got, ",bridge=vmbr0,tag=30") {
		t.Errorf("net0 = %q, want the tag on the interface", got)
	}
}

// ---------------------------------------------------------------------------
// Preflight
// ---------------------------------------------------------------------------

func lxcPrereqs() Prereqs {
	return Prereqs{
		Nodes: []string{"pve-1"}, Storage: "local-lvm", Bridge: "vmbr0", VMIDMin: 9100, VMIDMax: 9199,
		LXC: true, OSTemplate: "local:vztmpl/zoomies-runner.tar.zst",
	}
}

func codesOf(r provider.Report) map[string]bool {
	out := map[string]bool{}
	for _, f := range r.Findings {
		out[f.Code] = true
	}
	return out
}

func TestAWellPreparedContainerProviderPassesPreflightWithoutTheGuestAgentPrivileges(t *testing.T) {
	f := newFakePVE(t, nil)
	f.SetPermissions(Permissions{
		"/vms":               {"VM.Allocate": true, "VM.Audit": true, "VM.PowerMgmt": true, "VM.Config.Disk": true, "VM.Config.CPU": true, "VM.Config.Memory": true, "VM.Config.Network": true, "VM.Config.Options": true},
		"/storage/local-lvm": {"Datastore.AllocateSpace": true},
		"/storage/local":     {"Datastore.Audit": true},
	})
	report := Preflight(context.Background(), f.client(t), lxcPrereqs())
	if len(report.Findings) != 0 {
		t.Fatalf("findings on a prepared cluster: %+v", report.Findings)
	}
}

func TestPreflightNamesWhatAContainerProviderLacks(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(*fakePVE, *Prereqs)
		code string
		in   string // a phrase the finding must contain
	}{
		"a template that is not on the node": {func(f *fakePVE, p *Prereqs) { p.OSTemplate = "local:vztmpl/other.tar.zst" }, "proxmox.template_missing", "other.tar.zst"},
		"the template missing on a second node": {func(f *fakePVE, p *Prereqs) {
			p.Nodes = []string{"pve-1", "pve-2"}
			f.mu.Lock()
			f.storages["pve-2"] = append(f.storages["pve-2"], Storage{Storage: "local-lvm", Type: "lvmthin", Content: "images,rootdir", Enabled: true, Active: true})
			f.mu.Unlock()
		}, "proxmox.template_missing", "node pve-2"},
		"a storage that holds disk images but no root filesystems": {func(f *fakePVE, p *Prereqs) { p.Storage = "ceph" }, "proxmox.storage_no_images", "container root filesystems"},
		"a token that cannot read the template": {func(f *fakePVE, p *Prereqs) {
			f.SetPermissions(Permissions{
				"/vms":               {"VM.Allocate": true, "VM.Audit": true, "VM.PowerMgmt": true, "VM.Config.Disk": true, "VM.Config.CPU": true, "VM.Config.Memory": true, "VM.Config.Network": true, "VM.Config.Options": true},
				"/storage/local-lvm": {"Datastore.AllocateSpace": true},
			})
		}, "proxmox.privilege_missing", "Datastore.Audit"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakePVE(t, nil)
			f.SetPermissions(Permissions{
				"/vms":               {"VM.Allocate": true, "VM.Audit": true, "VM.PowerMgmt": true, "VM.Config.Disk": true, "VM.Config.CPU": true, "VM.Config.Memory": true, "VM.Config.Network": true, "VM.Config.Options": true},
				"/storage/local-lvm": {"Datastore.AllocateSpace": true},
				"/storage/ceph":      {"Datastore.AllocateSpace": true},
				"/storage/local":     {"Datastore.Audit": true},
			})
			pre := lxcPrereqs()
			tc.edit(f, &pre)
			report := Preflight(context.Background(), f.client(t), pre)
			for _, finding := range report.Findings {
				if finding.Code == tc.code && strings.Contains(finding.Title+finding.Detail, tc.in) {
					return
				}
			}
			t.Errorf("no %s finding mentioning %q in %+v", tc.code, tc.in, report.Findings)
		})
	}
}

func TestAContainerProviderIsNotAskedForTheCloneOrGuestAgentPrivileges(t *testing.T) {
	f := newFakePVE(t, nil)
	f.SetPermissions(Permissions{})
	report := Preflight(context.Background(), f.client(t), lxcPrereqs())
	for _, finding := range report.Findings {
		if strings.Contains(finding.Title, "VM.Clone") || strings.Contains(finding.Title, "GuestAgent") {
			t.Errorf("a container provider was asked for %s", finding.Title)
		}
	}
	if !codesOf(report)["proxmox.privilege_missing"] {
		t.Error("a token with no privileges at all should still be reported")
	}
}

func TestAnUnreadableContainerListingIsNamedRatherThanReadAsMissing(t *testing.T) {
	f := newFakePVE(t, nil)
	f.SetError("GET", "/nodes/pve-1/storage/local/content", 403, "Permission check failed (/storage/local, Datastore.Audit)")
	report := Preflight(context.Background(), f.client(t), lxcPrereqs())
	var found bool
	for _, finding := range report.Findings {
		found = found || (finding.Code == "proxmox.privilege_missing" && strings.Contains(finding.Title, "container templates"))
	}
	if !found {
		t.Errorf("a refused listing should be a privilege finding, got %+v", report.Findings)
	}
	if codesOf(report)["proxmox.template_missing"] {
		t.Error("a refused listing was reported as a missing template")
	}
}

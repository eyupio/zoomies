package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// A provider's credential reaches a hypervisor: anyone holding it can create,
// inspect and destroy every machine on the cluster. It goes in once and never
// comes back, and what a form is told instead is that one is set.
func TestAProviderNeverReturnsItsCredential(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(admin)

	const credential = "zoomies@pve!ci=nobody-may-ever-see"
	created := h.do(request{method: http.MethodPost, path: "/api/v1/providers", cookie: cookie,
		body: map[string]any{"kind": "fake", "name": "proxmox-lab", "endpoint": "https://pve.example.com:8006",
			"credential": credential, "settings": map[string]string{"zone": "zone-a"}}})
	created.mustStatus(t, http.StatusCreated, "create provider")

	var made providerResponse
	created.into(t, &made)
	if !made.CredentialsConfigured {
		t.Error("the provider does not report that a credential is configured, so a form would offer to set one that is already set")
	}
	for _, resp := range []*response{
		created,
		h.do(request{method: http.MethodGet, path: "/api/v1/providers", cookie: cookie}),
		h.do(request{method: http.MethodGet, path: "/api/v1/providers/" + made.ID, cookie: cookie}),
	} {
		if strings.Contains(string(resp.body), credential) {
			t.Fatalf("a provider response carries the credential: %s", truncate(resp.body))
		}
	}
	// And it really was stored: a credential that was quietly dropped would
	// pass every assertion above.
	row, err := h.st.GetProvider(h.ctx, made.ID)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if len(row.CredentialsEnc) == 0 {
		t.Fatal("no credential was sealed into the row")
	}
	if strings.Contains(string(row.CredentialsEnc), credential) {
		t.Fatal("the credential is on the row in plaintext")
	}
}

// A form renders a blank box for a credential it cannot read back. If saving
// that blank box cleared the stored credential, fixing a typo in a provider's
// name would take the fleet's machines offline at the next pass.
func TestAnEmptyCredentialLeavesTheStoredOneAlone(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(admin)
	prov := h.provider("proxmox-lab")
	before, err := h.st.GetProvider(h.ctx, prov.ID)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}

	resp := h.do(request{method: http.MethodPatch, path: "/api/v1/providers/" + prov.ID, cookie: cookie,
		body: map[string]any{"name": "proxmox-lab-2", "credential": ""}})
	resp.mustStatus(t, http.StatusOK, "patch provider")

	after, err := h.st.GetProvider(h.ctx, prov.ID)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if string(after.CredentialsEnc) != string(before.CredentialsEnc) {
		t.Error("an empty credential changed the sealed one")
	}
	if after.Name != "proxmox-lab-2" {
		t.Errorf("the rename did not happen: %q", after.Name)
	}
}

// The machines a provider already owns are its kind's machines, managed by its
// kind's driver. A kind that could be edited would leave them being reasoned
// about by a driver that has never heard of them.
func TestAProvidersKindCannotBeChanged(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(admin)
	prov := h.provider("proxmox-lab")

	resp := h.do(request{method: http.MethodPatch, path: "/api/v1/providers/" + prov.ID, cookie: cookie,
		body: map[string]any{"kind": "proxmox"}})
	resp.mustStatus(t, http.StatusUnprocessableEntity, "change a provider's kind")
	if msg := resp.errorMessage(t); !strings.Contains(strings.ToLower(msg), "provider") {
		t.Errorf("the refusal does not say what was refused: %q", msg)
	}
	if !strings.Contains(string(resp.body), `"kind"`) {
		t.Errorf("the refusal does not name the field: %s", truncate(resp.body))
	}
}

// The wizard asks "is this valid" as somebody types, and an answer of 422 would
// make an incomplete draft look like a broken request. The verdict is in the
// body, exactly as it is for a pool.
func TestValidatingADraftProviderAnswersOKAndPutsTheVerdictInTheBody(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(admin)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/providers/validate", cookie: cookie,
		body: map[string]any{"kind": "fake", "name": "draft", "settings": map[string]string{}}})
	resp.mustStatus(t, http.StatusOK, "validate an incomplete draft")

	var out validateProviderResponse
	resp.into(t, &out)
	if out.Valid {
		t.Fatal("a draft with no zone was called valid, so the wizard would offer to save a provider that cannot create anything")
	}
	// The driver's own finding is what says which answer is missing, and the
	// form highlights a field rather than a sentence.
	found := false
	for _, e := range out.Errors {
		if e.Field == "settings.zone" {
			found = true
		}
	}
	if !found {
		t.Errorf("nothing named the setting that is missing: %+v", out.Errors)
	}

	// And the valid half, so a refusal of everything would not pass.
	ok := h.do(request{method: http.MethodPost, path: "/api/v1/providers/validate", cookie: cookie,
		body: map[string]any{"kind": "fake", "name": "draft", "settings": map[string]string{"zone": "zone-a"}}})
	ok.mustStatus(t, http.StatusOK, "validate a complete draft")
	var good validateProviderResponse
	ok.into(t, &good)
	if !good.Valid {
		t.Errorf("a complete draft was called invalid: %+v", good.Errors)
	}
}

// Editing a provider is a dry run of a change to a row that already exists, so
// the name check has to exclude it. Without ?id= the form could not be saved at
// all without also renaming the provider -- it was refused about itself.
func TestValidatingAnEditIsNotRefusedAboutTheProviderBeingEdited(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(admin)
	prov := h.provider("proxmox-lab")

	body := map[string]any{"kind": "fake", "name": prov.Name, "settings": map[string]string{"zone": "zone-a"}}
	clash := h.do(request{method: http.MethodPost, path: "/api/v1/providers/validate", cookie: cookie, body: body})
	clash.mustStatus(t, http.StatusOK, "validate a clashing name")
	var out validateProviderResponse
	clash.into(t, &out)
	if out.Valid {
		t.Error("a second provider with an existing name was called valid")
	}

	same := h.do(request{method: http.MethodPost, path: "/api/v1/providers/validate?id=" + prov.ID, cookie: cookie, body: body})
	same.mustStatus(t, http.StatusOK, "validate an edit to the same provider")
	var edit validateProviderResponse
	same.into(t, &edit)
	if !edit.Valid {
		t.Errorf("editing a provider was refused about itself: %+v", edit.Errors)
	}
}

// /providers/validate and /providers/kinds have to be registered before
// /providers/{id}, or chi hands them to the handler that looks up a provider
// whose id is "kinds" and both answer 404.
func TestTheStaticProviderRoutesAreNotReadAsIdentifiers(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(admin)

	kinds := h.do(request{method: http.MethodGet, path: "/api/v1/providers/kinds", cookie: cookie})
	kinds.mustStatus(t, http.StatusOK, "list provider kinds")
	var listed struct {
		Items []providerKindResponse `json:"items"`
	}
	kinds.into(t, &listed)
	if len(listed.Items) == 0 {
		t.Fatal("no provider kinds were returned, so the wizard has nothing to offer")
	}
	if len(listed.Items[0].Settings) == 0 {
		t.Error("a driver that asks for nothing would render an empty form")
	}

	validate := h.do(request{method: http.MethodPost, path: "/api/v1/providers/validate", cookie: cookie,
		body: map[string]any{"kind": "fake", "name": "draft"}})
	validate.mustStatus(t, http.StatusOK, "validate a draft")
}

// The rows are the only record of what was rented. Deleting a provider while
// its machines still hold resources would leave VMs running with nothing
// tracking them, so the refusal says how many and what to do first.
func TestDeletingAProviderIsRefusedWhileItOwnsMachines(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(admin)
	prov := h.provider("proxmox-lab")
	h.machine(prov, "zoomies-mach-one")

	refused := h.do(request{method: http.MethodDelete, path: "/api/v1/providers/" + prov.ID, cookie: cookie})
	refused.mustStatus(t, http.StatusConflict, "delete a provider with a machine")
	msg := refused.errorMessage(t)
	if !strings.Contains(msg, "1 machine") {
		t.Errorf("the refusal does not say how many machines are behind it: %q", msg)
	}
	if !strings.Contains(msg, prov.Name) {
		t.Errorf("the refusal does not name the provider: %q", msg)
	}

	// With the machine's resource confirmed gone, the provider goes.
	if _, err := h.st.ConfirmMachineDeleted(h.ctx, h.lastMachine(prov.ID).ID, h.ctrl.Now()); err != nil {
		t.Fatalf("ConfirmMachineDeleted: %v", err)
	}
	gone := h.do(request{method: http.MethodDelete, path: "/api/v1/providers/" + prov.ID, cookie: cookie})
	gone.mustStatus(t, http.StatusNoContent, "delete a provider whose machines are gone")
}

// The kill switch is pressed by whoever is on call, and it has to leave the
// fleet in a known state rather than argue about whether it was already in one.
func TestPressingAProvidersKillSwitchTwiceIsNotAnError(t *testing.T) {
	h := newHarness(t)
	operator, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(operator)
	prov := h.provider("proxmox-lab")

	for i := range 2 {
		resp := h.do(request{method: http.MethodPost, path: "/api/v1/providers/" + prov.ID + "/pause", cookie: cookie,
			body: map[string]any{"reason": "the hypervisor is being patched"}})
		resp.mustStatus(t, http.StatusOK, "pause a provider")
		var out providerResponse
		resp.into(t, &out)
		if !out.Paused {
			t.Fatalf("press %d left the provider unpaused", i+1)
		}
		if out.PausedReason == "" {
			t.Error("the reason was not kept, so the card cannot say why new machines stopped")
		}
		if out.Held == "" {
			t.Error("a paused provider does not say why it is buying nothing")
		}
	}
	for i := range 2 {
		resp := h.do(request{method: http.MethodPost, path: "/api/v1/providers/" + prov.ID + "/resume", cookie: cookie})
		resp.mustStatus(t, http.StatusOK, "resume a provider")
		var out providerResponse
		resp.into(t, &out)
		if out.Paused {
			t.Fatalf("press %d left the provider paused", i+1)
		}
		if out.PausedReason != "" {
			t.Error("a resumed provider still carries the reason it was paused")
		}
	}

	// Both halves are audited, because "who stopped the fleet buying machines
	// at three in the morning" is exactly what an audit log is read for.
	audit := h.do(request{method: http.MethodGet, path: "/api/v1/audit?limit=200", cookie: cookie})
	audit.mustStatus(t, http.StatusOK, "audit")
	for _, action := range []string{"provider.pause", "provider.resume"} {
		if !strings.Contains(string(audit.body), action) {
			t.Errorf("%s was not audited", action)
		}
	}
}

// A preflight is the one call an operator can make against a hypervisor without
// risking anything, and its answer is recorded so the warning the UI is showing
// is quieted by a check run in a terminal.
func TestCheckingAProviderRecordsWhatItFound(t *testing.T) {
	h := newHarness(t)
	operator, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(operator)
	prov := h.provider("proxmox-lab")

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/providers/" + prov.ID + "/check", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "check a provider")
	var report providerCheckResponse
	resp.into(t, &report)
	if !report.Reachable {
		t.Fatalf("the fake provider reported itself unreachable: %s", truncate(resp.body))
	}
	if report.ProviderID != prov.ID {
		t.Errorf("the report is about %q rather than the provider that was checked", report.ProviderID)
	}

	row, err := h.st.GetProvider(h.ctx, prov.ID)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if row.LastCheckAt == nil {
		t.Error("the check was not recorded on the row, so the page still says it has never been verified")
	}
}

// A build with no driver for a stored provider's kind must say so rather than
// answering 500: the row was legal when it was written, and the fix is an
// upgrade, which is a sentence rather than a request ID.
func TestAskingADriverThisBuildDoesNotShipSaysSoRatherThanFailing(t *testing.T) {
	h := newHarness(t)
	operator, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(operator)

	// Written through the store, because the API refuses to create one.
	row := &store.Provider{
		Kind: store.ProviderProxmox, Name: "somebody-elses-build", MachineCapacity: 2,
		MachineBackend: store.BackendDocker, MaxCreatesInFlight: 1, Enabled: true,
	}
	if err := h.st.CreateProvider(h.ctx, row); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/providers/" + row.ID + "/check", cookie: cookie})
	resp.mustStatus(t, http.StatusConflict, "check a provider this build cannot speak")
	if msg := resp.errorMessage(t); !strings.Contains(msg, string(store.ProviderProxmox)) {
		t.Errorf("the refusal does not name the kind: %q", msg)
	}
}

// The orphan review is read before somebody destroys something, so the three
// sections have to mean different things: a row holding nothing can be released
// with nothing lost, and a row nobody can vouch for cannot be touched at all.
func TestTheOrphanReviewSeparatesRowsThatHoldNothingFromOnesNobodyCanVouchFor(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(admin)
	prov := h.provider("proxmox-lab")

	empty := &store.Machine{ProviderID: prov.ID, Name: "zoomies-mach-empty", State: store.MachinePlanned}
	if err := h.st.CreateMachine(h.ctx, empty); err != nil {
		t.Fatalf("CreateMachine: %v", err)
	}
	held := h.machine(prov, "zoomies-mach-held")
	if _, err := h.st.TransitionMachine(h.ctx, held.ID, store.MachineQuarantined, "ownership could not be proved"); err != nil {
		t.Fatalf("TransitionMachine: %v", err)
	}

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/providers/" + prov.ID + "/orphans", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "orphan review")
	var out providerOrphansResponse
	resp.into(t, &out)

	if len(out.NoResource) != 1 || out.NoResource[0].ID != empty.ID {
		t.Errorf("the row that holds nothing is not in no_resource: %+v", out.NoResource)
	}
	if len(out.Unverified) != 1 || out.Unverified[0].ID != held.ID {
		t.Errorf("the quarantined machine is not in unverified: %+v", out.Unverified)
	}
	// Nothing this fleet does not account for was invented: the untracked list
	// comes from a sweep against a live provider, and none has run.
	if len(out.Untracked) != 0 {
		t.Errorf("untracked resources were reported with no sweep behind them: %+v", out.Untracked)
	}
}

// Discovery fills the guided form. A driver that cannot list what its
// credential can see says so, so the form asks for identifiers instead of
// waiting for a menu that will never arrive.
func TestDiscoveryReturnsTheChoicesAFormCanOffer(t *testing.T) {
	h := newHarness(t)
	operator, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(operator)
	prov := h.provider("proxmox-lab")

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/providers/" + prov.ID + "/discovery", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "discovery")
	var out providerDiscoveryResponse
	resp.into(t, &out)
	if len(out.Nodes) == 0 {
		t.Fatalf("no nodes were offered: %s", truncate(resp.body))
	}
	// Every list is present even when it is empty, because a null in a
	// generated client is a different thing from "this provider has none".
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(resp.body, &raw); err != nil {
		t.Fatalf("reading the discovery response: %v", err)
	}
	for _, key := range []string{"nodes", "storages", "bridges", "templates"} {
		if v, ok := raw[key]; !ok || string(v) == "null" {
			t.Errorf("%s is missing or null", key)
		}
	}
}

// lastMachine is the most recently created machine for a provider, which is the
// one a test just made.
func (h *harness) lastMachine(providerID string) *store.Machine {
	h.t.Helper()
	machines, err := h.st.ListMachinesForProvider(h.ctx, providerID)
	if err != nil {
		h.t.Fatalf("ListMachinesForProvider: %v", err)
	}
	if len(machines) == 0 {
		h.t.Fatal("no machines for this provider")
	}
	return machines[len(machines)-1]
}

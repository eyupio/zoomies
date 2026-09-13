package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// providerFleet is a controller that has one provider and one machine, which is
// enough for every verb below to have something to say.
func providerFleet(t *testing.T, handler func(w http.ResponseWriter, r *http.Request) bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if handler != nil && handler(w, r) {
			return
		}
		switch r.URL.Path {
		case "/api/v1/providers":
			_, _ = w.Write([]byte(`{"items":[{"id":"prv_a","kind":"proxmox","name":"proxmox-lab",
				"endpoint":"https://pve.example.com:8006","credentials_configured":true,"max_machines":4,
				"enabled":true,"paused":false,"machines":{"ready":2,"creating":1},"owned":3}]}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The list is read when somebody is asking why no machines are appearing, so
// the column that matters is whether each provider may buy one -- and the
// sentence the server wrote about why it may not.
func TestListingProvidersSaysWhyOneIsBuyingNothing(t *testing.T) {
	srv := providerFleet(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/providers" {
			return false
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"prv_a","kind":"proxmox","name":"proxmox-lab",
			"endpoint":"https://pve.example.com:8006","max_machines":4,"enabled":true,"paused":true,
			"paused_reason":"the cluster is being patched","machines":{"ready":2},"owned":2,
			"held":"an operator paused this provider: the cluster is being patched"}]}`))
		return true
	})

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"providers", "list", "--url", srv.URL}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	text := out.String()
	for _, want := range []string{"proxmox-lab", "2/4", "2 ready", "paused", "the cluster is being patched"} {
		if !strings.Contains(text, want) {
			t.Errorf("the table does not mention %q:\n%s", want, text)
		}
	}
}

// A check is run when somebody suspects the credential or the template. The
// words it prints are printFindings' words, so that the terminal and the wizard
// describe one problem in one way.
func TestCheckingAProviderPrintsTheFindingsTheWizardWouldShow(t *testing.T) {
	srv := providerFleet(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/providers/prv_a/check" {
			return false
		}
		if r.Method != http.MethodPost {
			t.Errorf("the check was a %s", r.Method)
		}
		_, _ = w.Write([]byte(`{"provider_id":"prv_a","ok":false,"reachable":true,"version":"pve-manager/8.2.2",
			"findings":[{"code":"provider.template_missing","severity":"error","setting":"provider.settings.template",
			"title":"The template 9000 is not on node pve-1","detail":"Nothing can be cloned, so no machine would ever be created.",
			"fix":"Choose a template that exists on that node."}]}`))
		return true
	})

	e, out, errOut := newTestEnv(t)
	code := dispatch(context.Background(), e, []string{"providers", "check", "proxmox-lab", "--url", srv.URL})
	if code != exitError {
		t.Fatalf("a check that found an error exited %d; a script would believe the provider is fine\n%s", code, errOut)
	}
	text := out.String()
	for _, want := range []string{
		"pve-manager/8.2.2",
		"[error] The template 9000 is not on node pve-1  (provider.settings.template)",
		"fix: Choose a template that exists on that node.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the check does not print %q:\n%s", want, text)
		}
	}
}

// A provider that is fine says so. Without this half, a command that printed
// nothing at all would read as a command that did nothing.
func TestCheckingAHealthyProviderSaysItIsUsable(t *testing.T) {
	srv := providerFleet(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/providers/prv_a/check" {
			return false
		}
		_, _ = w.Write([]byte(`{"provider_id":"prv_a","ok":true,"reachable":true,"version":"pve-manager/8.2.2","findings":[]}`))
		return true
	})

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"providers", "check", "prv_a", "--url", srv.URL}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	if !strings.Contains(out.String(), "usable") {
		t.Errorf("a healthy provider said nothing useful:\n%s", out)
	}
}

// The kill switch is typed at three in the morning, so it takes the name the
// operator knows the provider by rather than an ID they would have to look up.
func TestPausingAProviderAcceptsItsNameAndSendsTheReason(t *testing.T) {
	var mu sync.Mutex
	var body map[string]string
	srv := providerFleet(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/providers/prv_a/pause" {
			return false
		}
		mu.Lock()
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"id":"prv_a","name":"proxmox-lab","paused":true,
			"paused_reason":"the cluster is being patched","enabled":true,"machines":{},"owned":0,
			"held":"an operator paused this provider: the cluster is being patched"}`))
		return true
	})

	e, out, errOut := newTestEnv(t)
	code := dispatch(context.Background(), e, []string{
		"providers", "pause", "proxmox-lab", "--reason", "the cluster is being patched", "--url", srv.URL})
	if code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	mu.Lock()
	defer mu.Unlock()
	if body["reason"] != "the cluster is being patched" {
		t.Errorf("the reason did not reach the controller: %+v", body)
	}
	text := out.String()
	if !strings.Contains(text, "no new machines") {
		t.Errorf("the confirmation does not say what stopped:\n%s", text)
	}
	// The half that matters most: a pause must not read as "everything
	// stopped", because drains, deletes and recovery carry on.
	if !strings.Contains(text, "keep running") {
		t.Errorf("the confirmation does not say what carries on:\n%s", text)
	}
}

// Resuming a provider that is still held by something else must not claim the
// fleet is buying again: the fence, the configuration and the ceiling each stop
// it too, and the server has already written the sentence.
func TestResumingAProviderStillSaysWhatIsHoldingIt(t *testing.T) {
	srv := providerFleet(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/providers/prv_a/resume" {
			return false
		}
		_, _ = w.Write([]byte(`{"id":"prv_a","name":"proxmox-lab","paused":false,"enabled":true,
			"machines":{},"owned":0,"held":"this fleet is fenced after a restore, so nothing may be created"}`))
		return true
	})

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"providers", "resume", "proxmox-lab", "--url", srv.URL}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	if !strings.Contains(out.String(), "fenced after a restore") {
		t.Errorf("the confirmation hides what is still holding the provider:\n%s", out)
	}
}

// A machine that is stuck is why somebody typed this, so the provider's own
// words and the task handle they would paste into its console are printed
// rather than left in a field nobody sees.
func TestListingMachinesPrintsWhatAStuckOneIsWaitingOn(t *testing.T) {
	srv := providerFleet(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/machines" {
			return false
		}
		if got := r.URL.Query().Get("provider"); got != "prv_a" {
			t.Errorf("the machine list was not scoped to the provider: %q", got)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"mach_a","provider_id":"prv_a","provider_name":"proxmox-lab",
			"name":"zoomies-mach-k3f9q","state":"creating","resource_zone":"pve-1","resource_id":"143",
			"operation":"create","operation_handle":"UPID:pve-1:00001234:...",
			"provider_error":"storage local-lvm is full"}],"total":1}`))
		return true
	})

	e, out, errOut := newTestEnv(t)
	code := dispatch(context.Background(), e, []string{
		"providers", "machines", "--provider", "proxmox-lab", "--url", srv.URL})
	if code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	text := out.String()
	for _, want := range []string{"zoomies-mach-k3f9q", "creating", "pve-1/143", "storage local-lvm is full", "UPID:pve-1:00001234"} {
		if !strings.Contains(text, want) {
			t.Errorf("the machine list does not mention %q:\n%s", want, text)
		}
	}
}

// The orphan review offers to destroy nothing. It prints three lists and the
// server's own note about each, because deciding what a resource with no row is
// belongs to a person with access to the hypervisor.
func TestTheOrphanReviewPrintsTheThreeListsAndDeletesNothing(t *testing.T) {
	srv := providerFleet(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/providers/prv_a/orphans" {
			return false
		}
		if r.Method != http.MethodGet {
			t.Errorf("the review used %s, which is not a read", r.Method)
		}
		_, _ = w.Write([]byte(`{"provider_id":"prv_a","provider_name":"proxmox-lab",
			"untracked":[{"name":"zoomies-mach-lost","note":"look at it in proxmox-lab yourself"}],
			"no_resource":[{"id":"mach_b","name":"zoomies-mach-empty","message":"never got as far as a resource"}],
			"unverified":[{"id":"mach_c","name":"zoomies-mach-doubtful","resource_zone":"pve-1","resource_id":"144",
			"ownership_error":"the resource at pve-1/144 carries another controller's marks"}]}`))
		return true
	})

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"providers", "orphans", "proxmox-lab", "--url", srv.URL}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	text := out.String()
	for _, want := range []string{
		"Resources with no row",
		"zoomies-mach-lost",
		"look at it in proxmox-lab yourself",
		"Rows holding no resource",
		"zoomies-mach-empty",
		"Machines nobody can vouch for",
		"another controller's marks",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the review does not mention %q:\n%s", want, text)
		}
	}
}

// A name that is not a provider is the commonest mistake, and the answer is the
// list of names that would have worked rather than a 404 from the API.
func TestAnUnknownProviderNameListsTheOnesThereAre(t *testing.T) {
	srv := providerFleet(t, nil)

	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"providers", "check", "proxmox-prod", "--url", srv.URL}); code != exitError {
		t.Fatalf("exit code = %d, want %d\n%s", code, exitError, errOut)
	}
	if !strings.Contains(errOut.String(), "proxmox-lab") {
		t.Errorf("the error does not say which providers there are:\n%s", errOut)
	}
}

// --output json hands over the server's bytes, so a field added to the API
// tomorrow reaches a script today.
func TestMachineOutputInJSONIsTheServersOwnBytes(t *testing.T) {
	srv := providerFleet(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/machines" {
			return false
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"mach_a","name":"zoomies-mach-k3f9q","state":"ready",
			"something_this_build_has_never_heard_of":true}],"total":1}`))
		return true
	})

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"providers", "machines", "--output", "json", "--url", srv.URL}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	if !strings.Contains(out.String(), "something_this_build_has_never_heard_of") {
		t.Errorf("the JSON output dropped a field the CLI does not know:\n%s", out)
	}
}

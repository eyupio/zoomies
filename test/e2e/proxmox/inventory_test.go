package proxmox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A cluster that answers the three read-only calls preflight makes, and nothing
// else. Anything it is asked for beyond those fails the test: preflight's
// contract is that every call in it is a GET that changes nothing, and a fake
// that quietly served a POST would let that stop being true.
func fakeCluster(t *testing.T, guests []Guest, volumes []Volume) *Cluster {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("preflight made a %s to %s; it must change nothing", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "PVEAPIToken=") {
			t.Errorf("Authorization = %q, want a PVEAPIToken", got)
		}
		write := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": v})
		}
		switch {
		case r.URL.Path == "/api2/json/version":
			write(map[string]string{"version": "8.2.4", "release": "8.2"})
		case r.URL.Path == "/api2/json/cluster/resources":
			write(guests)
		case strings.HasSuffix(r.URL.Path, "/content"):
			write(volumes)
		default:
			t.Errorf("preflight asked for %s, which it has no business reading", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewCluster(ClusterOptions{
		Endpoint: srv.URL, Token: "zoomies@pve!qualify=secret", Insecure: true,
	})
	if err != nil {
		t.Fatalf("building the cluster client: %v", err)
	}
	return c
}

func preflightEnv() env {
	return env{
		endpoint: "https://pve.example.com:8006", node: "pve1", storage: "local-lvm",
		templateID: 9000, vmidMin: 9100, vmidMax: 9199,
	}
}

// A cluster that is ready reports the facts the record needs and complains
// about nothing.
func TestAReadyClusterYieldsTheEvidenceRatherThanAComplaint(t *testing.T) {
	c := fakeCluster(t, []Guest{
		{VMID: 9000, Node: "pve1", Name: "ubuntu-2404-template", Type: "qemu", Template: 1, Status: "stopped"},
		{VMID: 101, Node: "pve1", Name: "somebody-elses-vm", Type: "qemu", Status: "running"},
	}, nil)

	facts, missing := preflightCluster(context.Background(), c, preflightEnv())
	if len(missing) > 0 {
		t.Fatalf("a ready cluster was refused: %v", missing)
	}
	// The release is appended only when it adds something: "8.2.4/8.2" would be
	// the same fact twice, and the version is a row somebody reads.
	if facts.Version != "8.2.4" {
		t.Errorf("version = %q, want the version without its redundant release", facts.Version)
	}
	if facts.TemplateName != "ubuntu-2404-template" {
		t.Errorf("template name = %q", facts.TemplateName)
	}
	// A guest outside the reserved range is none of this run's business, and
	// subtracting it at the end would excuse a leftover.
	if len(facts.PreexistingVMIDs) != 0 {
		t.Errorf("a guest outside the range was counted as pre-existing: %v", facts.PreexistingVMIDs)
	}
}

// A cluster that cannot be reached is one problem, not eleven.
//
// Every check below the first fails when the cluster is down, and listing them
// all buries the only sentence that matters.
func TestAClusterThatDoesNotAnswerIsOneProblemRatherThanEvery(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	c, err := NewCluster(ClusterOptions{Endpoint: srv.URL, Token: "u@pve!t=s", Insecure: true})
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}

	_, missing := preflightCluster(context.Background(), c, preflightEnv())
	if len(missing) != 1 {
		t.Fatalf("an unreachable cluster produced %d complaints, want one:\n%s",
			len(missing), strings.Join(missing, "\n"))
	}
	if !strings.Contains(missing[0], "did not answer") {
		t.Errorf("the complaint does not say the cluster did not answer: %q", missing[0])
	}
}

// The template has to exist and has to be a template: the provider clones from
// one, and cloning a running guest is a different operation.
func TestATemplateThatIsAbsentOrIsNotATemplateStopsTheRun(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		c := fakeCluster(t, []Guest{{VMID: 500, Node: "pve1", Name: "other", Type: "qemu"}}, nil)
		_, missing := preflightCluster(context.Background(), c, preflightEnv())
		if !strings.Contains(strings.Join(missing, "\n"), "no template to clone") {
			t.Errorf("a missing template was not reported: %v", missing)
		}
	})
	t.Run("not a template", func(t *testing.T) {
		c := fakeCluster(t, []Guest{
			{VMID: 9000, Node: "pve1", Name: "not-a-template", Type: "qemu", Template: 0, Status: "running"},
		}, nil)
		_, missing := preflightCluster(context.Background(), c, preflightEnv())
		joined := strings.Join(missing, "\n")
		if !strings.Contains(joined, "is not a template") {
			t.Errorf("a running guest was accepted as a template: %v", missing)
		}
		if !strings.Contains(joined, "cloning a running guest is a different operation") {
			t.Errorf("the complaint does not say why that matters: %v", missing)
		}
	})
}

// A leftover in the reserved range makes the closing reconciliation
// meaningless: it could not tell this run's litter from the last run's. So it
// stops the run, and the message says to record it rather than just delete it.
func TestAnEarlierRunsLeftoverInTheRangeStopsTheQualification(t *testing.T) {
	c := fakeCluster(t, []Guest{
		{VMID: 9000, Node: "pve1", Name: "ubuntu-2404-template", Type: "qemu", Template: 1},
		{VMID: 9142, Node: "pve1", Name: "zoomies-mach-abc", Type: "qemu", Status: "running"},
	}, nil)

	facts, missing := preflightCluster(context.Background(), c, preflightEnv())
	joined := strings.Join(missing, "\n")
	if !strings.Contains(joined, "already wears this fleet's marks") {
		t.Fatalf("a leftover in the reserved range was accepted: %v", missing)
	}
	if !strings.Contains(joined, "record it in the qualification") {
		t.Errorf("the complaint says to delete it but not to record it: %v", missing)
	}
	// It is still counted, because the reconciliation subtracts what was there
	// before whether or not it should have been.
	if len(facts.PreexistingVMIDs) != 1 || facts.PreexistingVMIDs[0] != 9142 {
		t.Errorf("pre-existing = %v, want the leftover recorded", facts.PreexistingVMIDs)
	}
}

// A guest in the range that is not ours is recorded and not complained about:
// the range was supposed to be reserved, but somebody else's VM is a fact the
// reconciliation subtracts rather than a prerequisite this run failed.
func TestSomebodyElsesGuestInTheRangeIsCountedAndNotRefused(t *testing.T) {
	c := fakeCluster(t, []Guest{
		{VMID: 9000, Node: "pve1", Name: "ubuntu-2404-template", Type: "qemu", Template: 1},
		{VMID: 9150, Node: "pve1", Name: "database-01", Type: "qemu", Status: "running"},
	}, nil)

	facts, missing := preflightCluster(context.Background(), c, preflightEnv())
	if len(missing) > 0 {
		t.Fatalf("a stranger's VM in the range stopped the run: %v", missing)
	}
	if len(facts.Preexisting) != 1 || facts.PreexistingVMIDs[0] != 9150 {
		t.Errorf("it was not recorded as pre-existing: %v", facts.PreexistingVMIDs)
	}
}

// The broken template is optional, but naming one that does not exist is a
// typo that would be found at the fault case rather than here.
func TestABrokenTemplateThatDoesNotExistIsReportedBeforeAnythingIsBuilt(t *testing.T) {
	e := preflightEnv()
	e.brokenTemplateID = 9001
	c := fakeCluster(t, []Guest{
		{VMID: 9000, Node: "pve1", Name: "ubuntu-2404-template", Type: "qemu", Template: 1},
	}, nil)

	_, missing := preflightCluster(context.Background(), c, e)
	joined := strings.Join(missing, "\n")
	if !strings.Contains(joined, "ZOOMIES_PROXMOX_BROKEN_TEMPLATE") {
		t.Fatalf("a broken template that does not exist was accepted: %v", missing)
	}
	if !strings.Contains(joined, "leave it unset") {
		t.Errorf("the complaint does not say it is optional: %v", missing)
	}
}

// The node name is checked as the doubt it is, not asserted: a node with no
// guests at all is invisible to a guest listing, so a cluster that returned
// nothing must not be read as a wrong node name.
func TestAWrongNodeNameIsDoubtedAndAnEmptyClusterIsNot(t *testing.T) {
	e := preflightEnv()
	c := fakeCluster(t, []Guest{
		{VMID: 9000, Node: "pve2", Name: "ubuntu-2404-template", Type: "qemu", Template: 1},
	}, nil)
	_, missing := preflightCluster(context.Background(), c, e)
	if !strings.Contains(strings.Join(missing, "\n"), "check the node name") {
		t.Errorf("a node no guest is on was not doubted: %v", missing)
	}

	// No guests at all: the template is missing, but the node is not accused.
	empty := fakeCluster(t, nil, nil)
	_, missing = preflightCluster(context.Background(), empty, e)
	if strings.Contains(strings.Join(missing, "\n"), "check the node name") {
		t.Errorf("an empty cluster was read as a wrong node name: %v", missing)
	}
}

// The version is a row in the record, so it carries the release when the
// release says something the version does not -- and does not repeat it when it
// does not.
func TestTheRecordedVersionCarriesTheReleaseOnlyWhenItAddsSomething(t *testing.T) {
	for _, tc := range []struct{ version, release, want string }{
		{"8.2.4", "8.2", "8.2.4"},
		{"8.2.4", "", "8.2.4"},
		{"pve-manager/8.2.4", "bookworm", "pve-manager/8.2.4/bookworm"},
	} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]string{"version": tc.version, "release": tc.release},
			})
		}))
		c, err := NewCluster(ClusterOptions{Endpoint: srv.URL, Token: "u@pve!t=s", Insecure: true})
		if err != nil {
			t.Fatalf("building the client: %v", err)
		}
		got, err := c.Version(context.Background())
		srv.Close()
		if err != nil {
			t.Fatalf("reading the version: %v", err)
		}
		if got != tc.want {
			t.Errorf("version %q release %q read as %q, want %q", tc.version, tc.release, got, tc.want)
		}
	}
}

// A run made without checking the cluster's certificate is still a
// qualification, but it is a different one -- so the client says which it was,
// and the evidence carries it.
func TestAClientSaysWhetherItCheckedTheClustersCertificate(t *testing.T) {
	verified, err := NewCluster(ClusterOptions{Endpoint: "https://pve.example.com:8006", Token: "u@pve!t=s"})
	if err != nil {
		t.Fatalf("building a verifying client: %v", err)
	}
	if !verified.Verified() {
		t.Error("a client that checks certificates says it does not")
	}
	skipped, err := NewCluster(ClusterOptions{Endpoint: "https://pve.example.com:8006", Token: "u@pve!t=s", Insecure: true})
	if err != nil {
		t.Fatalf("building a non-verifying client: %v", err)
	}
	if skipped.Verified() {
		t.Error("a client that skips verification claims to have verified")
	}
}

// A hypervisor credential over plain HTTP is a hypervisor credential on the
// wire, and a token pasted wrong is a run that fails at the first call.
func TestAClusterCredentialIsRefusedBeforeItCanBeSentAnywhereUnsafe(t *testing.T) {
	for _, tc := range []struct{ name, endpoint, token, secret, want string }{
		{"plain http", "http://pve.example.com:8006", "u@pve!t=s", "", "not https"},
		{"no endpoint", "", "u@pve!t=s", "", "no cluster endpoint"},
		{"secretless token", "https://pve.example.com:8006", "u@pve!t", "", "paste it whole"},
		{"not a token id", "https://pve.example.com:8006", "zoomies@pve", "secret", "is not a token identifier"},
	} {
		_, err := NewCluster(ClusterOptions{Endpoint: tc.endpoint, Token: tc.token, Secret: tc.secret})
		if err == nil {
			t.Errorf("%s was accepted", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s said %q, want it to mention %q", tc.name, err, tc.want)
		}
		if strings.Contains(err.Error(), "secret") && tc.name == "not a token id" {
			t.Errorf("%s put the secret in the error: %v", tc.name, err)
		}
	}

	// And the port Proxmox listens on is assumed rather than demanded.
	c, err := NewCluster(ClusterOptions{Endpoint: "https://pve.example.com", Token: "u@pve!t=s"})
	if err != nil {
		t.Fatalf("an endpoint with no port was refused: %v", err)
	}
	if !strings.Contains(c.base, ":8006/api2/json") {
		t.Errorf("base = %q, want the default port and the API path", c.base)
	}
}

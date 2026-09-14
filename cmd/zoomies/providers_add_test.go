package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// providerAddServer is a controller that accepts the wizard's three calls --
// validate, create, check -- and records the bodies it was sent, so a test
// can say what the terminal turned the flags into.
type providerAddServer struct {
	mu       sync.Mutex
	validate map[string]any
	created  map[string]any
	patched  map[string]any
	checked  bool
	verdict  string
	check    string
}

func newProviderAddServer(t *testing.T) (*httptest.Server, *providerAddServer) {
	t.Helper()
	rec := &providerAddServer{
		verdict: `{"valid":true,"errors":[],"warnings":[]}`,
		check:   `{"provider_id":"prv_new","ok":true,"reachable":true,"version":"pve-manager/8.2.2","findings":[],"checked_at":"2026-09-14T10:00:00Z"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rec.mu.Lock()
		defer rec.mu.Unlock()
		body := map[string]any{}
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/providers/validate":
			rec.validate = body
			_, _ = w.Write([]byte(rec.verdict))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/providers":
			rec.created = body
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"prv_new","kind":"proxmox","name":"proxmox-lab","endpoint":"https://pve.example.com:8006",
				"credentials_configured":true,"max_machines":` + jsonNumber(body["max_machines"]) + `,"enabled":true,"paused":false,"machines":{},"owned":0}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/providers/prv_new/check":
			rec.checked = true
			_, _ = w.Write([]byte(rec.check))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/providers":
			_, _ = w.Write([]byte(`{"items":[{"id":"prv_new","kind":"proxmox","name":"proxmox-lab","endpoint":"https://pve.example.com:8006",
				"settings":{"nodes":"pve1","template_id":"9000","storage":"local-lvm","bridge":"vmbr0","vmid_min":"9000","vmid_max":"9099"},
				"credentials_configured":true,"max_machines":0,"enabled":true,"paused":false,"machines":{},"owned":0}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/providers/prv_new":
			rec.patched = body
			_, _ = w.Write([]byte(`{"id":"prv_new","kind":"proxmox","name":"proxmox-lab","endpoint":"https://pve.example.com:8006",
				"credentials_configured":true,"max_machines":8,"enabled":true,"paused":false,"machines":{},"owned":0}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func jsonNumber(v any) string {
	if v == nil {
		return "0"
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

// The point of the command is that the five-step wizard becomes one line, so
// the test is that one line turns into the same request the wizard would send:
// the Proxmox questions land in the driver's settings under the keys the
// driver reads, the token pasted on standard input becomes the credential, and
// the cluster is asked what it would refuse before the terminal says done.
func TestAddingAProviderSendsTheWizardsRequestFromOneLine(t *testing.T) {
	srv, rec := newProviderAddServer(t)
	e, out, errOut := newTestEnv(t)
	e.in = strings.NewReader("zoomies@pve!ci=secret-token\n")

	code := dispatch(context.Background(), e, []string{"providers", "add", "proxmox", "--url", srv.URL,
		"--name", "proxmox-lab", "--endpoint", "https://pve.example.com:8006",
		"--nodes", "pve1,pve2", "--template", "9000", "--storage", "local-lvm",
		"--vmid-range", "9100-9199", "--labels", "arch=amd64", "--max-machines", "4",
		"--setting", "pool=zoomies"})
	if code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.validate == nil {
		t.Fatal("the draft was not validated before it was saved")
	}
	if rec.created == nil {
		t.Fatal("no provider was created")
	}
	if !rec.checked {
		t.Error("the new provider was not asked what it would refuse")
	}
	settings, _ := rec.created["settings"].(map[string]any)
	for key, want := range map[string]string{
		"nodes": "pve1,pve2", "template_id": "9000", "storage": "local-lvm", "bridge": "vmbr0",
		"vmid_min": "9100", "vmid_max": "9199", "pool": "zoomies",
	} {
		if got, _ := settings[key].(string); got != want {
			t.Errorf("settings[%q] = %q, want %q", key, got, want)
		}
	}
	if got := rec.created["credential"]; got != "zoomies@pve!ci=secret-token" {
		t.Errorf("credential = %v, want the token read from standard input", got)
	}
	if got := rec.created["kind"]; got != "proxmox" {
		t.Errorf("kind = %v", got)
	}
	if got := rec.created["max_machines"]; got != float64(4) {
		t.Errorf("max_machines = %v, want 4", got)
	}
	if got := rec.created["connection"]; got != "direct" {
		t.Errorf("connection = %v, want direct", got)
	}
	labels, _ := rec.created["machine_labels"].(map[string]any)
	if labels["arch"] != "amd64" {
		t.Errorf("machine_labels = %v", labels)
	}
	text := out.String()
	for _, want := range []string{"Created proxmox-lab (prv_new)", "pve-manager/8.2.2", "up to 4 machines"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output does not say %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "secret-token") || strings.Contains(errOut.String(), "secret-token") {
		t.Error("the token was echoed")
	}
}

// A provider created with the ceiling left at zero rents nothing, which is the
// API's safe default and easy to miss from a terminal. The command says so,
// and names the command that raises it, so the one step that spends money does
// not need a browser.
func TestAddingAProviderWithNoCeilingSaysItRentsNothing(t *testing.T) {
	srv, _ := newProviderAddServer(t)
	e, out, errOut := newTestEnv(t)
	e.in = strings.NewReader("zoomies@pve!ci=secret\n")

	code := dispatch(context.Background(), e, []string{"providers", "add", "proxmox", "--url", srv.URL,
		"--name", "proxmox-lab", "--endpoint", "https://pve.example.com:8006",
		"--nodes", "pve1", "--template", "9000", "--storage", "local-lvm", "--no-check"})
	if code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	text := out.String()
	if !strings.Contains(text, "rents nothing yet") || !strings.Contains(text, "zoomies providers edit proxmox-lab --max-machines") {
		t.Errorf("the output does not say the ceiling is zero and how to raise it:\n%s", text)
	}
}

// The dry run is what makes a typo cheap: every wrong field is named at once
// and nothing is written, so the operator fixes the line rather than deleting
// a half-made provider.
func TestAddingAProviderTheValidatorRefusesSavesNothing(t *testing.T) {
	srv, rec := newProviderAddServer(t)
	rec.verdict = `{"valid":false,"errors":[{"field":"settings.template_id","message":"the template VMID is required"},
		{"field":"endpoint","message":"\"pve\" is not an address this controller can reach"}],"warnings":[]}`
	e, _, errOut := newTestEnv(t)
	e.in = strings.NewReader("zoomies@pve!ci=secret\n")

	code := dispatch(context.Background(), e, []string{"providers", "add", "proxmox", "--url", srv.URL,
		"--name", "proxmox-lab", "--endpoint", "pve", "--nodes", "pve1", "--storage", "local-lvm"})
	if code == exitOK {
		t.Fatal("a draft the validator refused was accepted")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.created != nil {
		t.Error("a provider was created from a draft the validator refused")
	}
	text := errOut.String()
	for _, want := range []string{"settings.template_id: the template VMID is required", "endpoint:", "nothing was saved"} {
		if !strings.Contains(text, want) {
			t.Errorf("the error does not say %q:\n%s", want, text)
		}
	}
}

// A live check that fails still leaves the provider saved: the row is right and
// the cluster is what needs fixing, so the exit code says so and the message
// names the command to run once it is.
func TestAddingAProviderTheClusterWouldRefuseExitsNonZeroButKeepsIt(t *testing.T) {
	srv, rec := newProviderAddServer(t)
	rec.check = `{"provider_id":"prv_new","ok":false,"reachable":true,"version":"pve-manager/8.2.2",
		"findings":[{"severity":"error","code":"provider.proxmox.privilege_missing","title":"the token lacks VM.Clone on /vms/9000","fix":"grant VM.Clone to zoomies@pve!ci on /vms/9000"}],
		"checked_at":"2026-09-14T10:00:00Z"}`
	e, out, errOut := newTestEnv(t)
	e.in = strings.NewReader("zoomies@pve!ci=secret\n")

	code := dispatch(context.Background(), e, []string{"providers", "add", "proxmox", "--url", srv.URL,
		"--name", "proxmox-lab", "--endpoint", "https://pve.example.com:8006",
		"--nodes", "pve1", "--template", "9000", "--storage", "local-lvm"})
	if code == exitOK {
		t.Fatal("a provider the cluster would refuse exited zero")
	}
	rec.mu.Lock()
	created := rec.created != nil
	rec.mu.Unlock()
	if !created {
		t.Error("the provider should have been saved before the check ran")
	}
	if !strings.Contains(out.String(), "VM.Clone") {
		t.Errorf("the finding was not printed:\n%s", out)
	}
	if !strings.Contains(errOut.String(), "zoomies providers check proxmox-lab") {
		t.Errorf("the error does not name the command to run again:\n%s", errOut)
	}
}

// The token and the certificate are files as often as they are pasted, and a
// token read from a file must not be asked for again on the terminal.
func TestAddingAProviderReadsTheTokenAndCertificateFromFiles(t *testing.T) {
	srv, rec := newProviderAddServer(t)
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(tokenFile, []byte("zoomies@pve!ci=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caFile, []byte("-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, _, errOut := newTestEnv(t)
	// Nothing on standard input: a prompt that fired would read "" and fail.
	e.in = &bytes.Buffer{}

	code := dispatch(context.Background(), e, []string{"providers", "add", "proxmox", "--url", srv.URL,
		"--name", "proxmox-lab", "--endpoint", "https://pve.example.com:8006",
		"--nodes", "pve1", "--template", "9000", "--storage", "local-lvm",
		"--credential-file", tokenFile, "--endpoint-ca-file", caFile, "--gateway", "tc-abc123", "--no-check"})
	if code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if got := rec.created["credential"]; got != "zoomies@pve!ci=from-file" {
		t.Errorf("credential = %v, want the file's contents trimmed", got)
	}
	if got, _ := rec.created["ca_pem"].(string); !strings.Contains(got, "BEGIN CERTIFICATE") {
		t.Errorf("ca_pem = %q", got)
	}
	if rec.created["connection"] != "tailcat" || rec.created["tailcat_address"] != "tc-abc123" {
		t.Errorf("a --gateway should make the connection private: %v", rec.created)
	}
}

// An edit sends what was typed and nothing else, and a driver setting it names
// is merged over the row's rather than replacing them: the API takes the whole
// map, and an operator changing the storage did not ask to lose the nodes.
func TestEditingAProviderSendsOnlyWhatChangedAndKeepsTheOtherSettings(t *testing.T) {
	srv, rec := newProviderAddServer(t)
	e, out, errOut := newTestEnv(t)

	code := dispatch(context.Background(), e, []string{"providers", "edit", "proxmox-lab", "--url", srv.URL,
		"--max-machines", "8", "--storage", "ceph"})
	if code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.patched == nil {
		t.Fatal("nothing was patched")
	}
	if got := rec.patched["max_machines"]; got != float64(8) {
		t.Errorf("max_machines = %v, want 8", got)
	}
	for _, absent := range []string{"machine_capacity", "idle_timeout", "credential", "name", "enabled", "connection"} {
		if _, ok := rec.patched[absent]; ok {
			t.Errorf("the PATCH carries %q, which was not typed: %v", absent, rec.patched)
		}
	}
	settings, _ := rec.patched["settings"].(map[string]any)
	if settings["storage"] != "ceph" || settings["nodes"] != "pve1" || settings["template_id"] != "9000" {
		t.Errorf("settings were not merged over the row's: %v", settings)
	}
	if !strings.Contains(out.String(), "up to 8 machines") {
		t.Errorf("the output does not say the new ceiling:\n%s", out)
	}
}

func TestEditingAProviderWithNoFlagsIsAUsageError(t *testing.T) {
	srv, _ := newProviderAddServer(t)
	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"providers", "edit", "proxmox-lab", "--url", srv.URL}); code == exitOK {
		t.Fatal("an edit that changes nothing exited zero")
	}
	if !strings.Contains(errOut.String(), "nothing to change") {
		t.Errorf("the error does not say nothing was named:\n%s", errOut)
	}
}

func TestVMIDRangeIsTwoNumbersLowFirst(t *testing.T) {
	cases := []struct {
		in     string
		lo, hi string
		bad    bool
	}{
		{in: "", lo: "9000", hi: "9099"},
		{in: "9100-9199", lo: "9100", hi: "9199"},
		{in: " 200 - 300 ", lo: "200", hi: "300"},
		{in: "9000", bad: true},
		{in: "abc-def", bad: true},
		{in: "9099-9000", bad: true},
	}
	for _, c := range cases {
		lo, hi, err := parseVMIDRange(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("%q was accepted as %s-%s", c.in, lo, hi)
			}
			continue
		}
		if err != nil || lo != c.lo || hi != c.hi {
			t.Errorf("%q -> %s-%s, %v; want %s-%s", c.in, lo, hi, err, c.lo, c.hi)
		}
	}
}

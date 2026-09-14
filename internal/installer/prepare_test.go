package installer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeManager records what PrepareAgent asks of the service manager, because
// the real one writes under /etc and the point of the test is what was asked.
type fakeManager struct {
	spec    ServiceSpec
	enabled bool
	started bool
	calls   []string
}

func (m *fakeManager) Kind() ServiceKind { return ServiceSystemd }
func (m *fakeManager) Install(_ context.Context, spec ServiceSpec) (string, error) {
	m.spec = spec
	m.calls = append(m.calls, "install")
	return "/etc/systemd/system/zoomies-agent.service", nil
}
func (m *fakeManager) Enable(context.Context) error {
	m.enabled = true
	m.calls = append(m.calls, "enable")
	return nil
}
func (m *fakeManager) Start(context.Context) error {
	m.started = true
	m.calls = append(m.calls, "start")
	return nil
}
func (m *fakeManager) Stop(context.Context) error { m.calls = append(m.calls, "stop"); return nil }
func (m *fakeManager) Disable(context.Context) error {
	m.calls = append(m.calls, "disable")
	return nil
}
func (m *fakeManager) Remove(context.Context) error { m.calls = append(m.calls, "remove"); return nil }
func (m *fakeManager) Status(context.Context) (string, error) {
	return "inactive", nil
}
func (m *fakeManager) Logs(context.Context, int) (string, error) { return "", nil }
func (m *fakeManager) LogCommand() string                        { return "journalctl -u zoomies-agent" }

func prepareOptions(t *testing.T, mgr *fakeManager) (PrepareOptions, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	var out bytes.Buffer
	return PrepareOptions{
		ConfigDir:  filepath.Join(root, "etc"),
		StateDir:   filepath.Join(root, "lib"),
		BinaryPath: "/usr/local/bin/zoomies",
		// An account that exists, so no system user is created on the machine
		// running the tests.
		ServiceUser: currentUserName(os.Getuid()),
		Service:     ServiceSystemd,
		Out:         &out,
		newManager: func(ServiceKind, string) (ServiceManager, error) {
			return mgr, nil
		},
	}, &out
}

// The template's unit is what makes a clone enrollable: it reads the file the
// controller writes into each machine and stays disabled until the controller
// enables it, so a template that shipped it enabled would have every clone try
// to join before it had anything to join with.
func TestPreparingAnAgentInstallsAUnitThatReadsTheEnrolmentFileAndStaysDisabled(t *testing.T) {
	mgr := &fakeManager{}
	opts, out := prepareOptions(t, mgr)
	if err := PrepareAgent(context.Background(), opts); err != nil {
		t.Fatalf("PrepareAgent: %v\n%s", err, out)
	}
	if mgr.spec.EnvFile != MachineEnvPath {
		t.Errorf("EnvFile = %q, want %q", mgr.spec.EnvFile, MachineEnvPath)
	}
	if mgr.spec.Unit != UnitAgent {
		t.Errorf("unit = %q, want the agent's", mgr.spec.Unit)
	}
	if mgr.enabled || mgr.started {
		t.Errorf("the unit was enabled or started; the controller does that inside each clone (calls: %v)", mgr.calls)
	}
	if strings.Join(mgr.calls, " ") != "install disable" {
		t.Errorf("calls = %v, want install then disable", mgr.calls)
	}
	// The unit needs a configuration file to point at, and the one written
	// says this is an agent and nothing else: the controller, the name and the
	// capacity arrive per machine in the environment file.
	cfg, err := os.ReadFile(filepath.Join(opts.ConfigDir, "zoomies.yaml"))
	if err != nil {
		t.Fatalf("no configuration was written: %v", err)
	}
	if !strings.Contains(string(cfg), "embedded: false") {
		t.Errorf("the configuration does not switch the embedded agent off:\n%s", cfg)
	}
	if strings.Contains(string(cfg), "join_token") || strings.Contains(string(cfg), "zoojoin_") {
		t.Errorf("a credential landed in the configuration:\n%s", cfg)
	}
	if !strings.Contains(out.String(), "convert it to a template") {
		t.Errorf("the output does not say what to do next:\n%s", out)
	}
}

// A machine that has already joined holds one host's identity, and a template
// made from it clones that identity into every machine. Refusing here is the
// only moment the mistake is cheap.
func TestPreparingAnAgentRefusesAMachineThatHasAlreadyJoined(t *testing.T) {
	mgr := &fakeManager{}
	opts, _ := prepareOptions(t, mgr)
	work := filepath.Join(opts.StateDir, "work")
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "agent.json"), []byte(`{"host_id":"hst_x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := PrepareAgent(context.Background(), opts)
	if err == nil {
		t.Fatal("a machine holding agent.json was accepted as a template")
	}
	if !strings.Contains(err.Error(), "agent.json") || !strings.Contains(err.Error(), "delete that file") {
		t.Errorf("the error does not name the file and the fix: %v", err)
	}
	if len(mgr.calls) != 0 {
		t.Errorf("the service manager was touched before the refusal: %v", mgr.calls)
	}
}

// The bootstrap enrols a clone with `systemctl enable --now`, so a template
// without systemd is a fleet of machines that boot and never join.
func TestPreparingAnAgentNeedsSystemd(t *testing.T) {
	mgr := &fakeManager{}
	opts, _ := prepareOptions(t, mgr)
	opts.Service = ServiceNone
	err := PrepareAgent(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "systemd") {
		t.Fatalf("err = %v, want a refusal naming systemd", err)
	}
}

func TestRenderSystemdUnitReadsAnEnvironmentFileOnlyWhenAsked(t *testing.T) {
	spec := ServiceSpec{
		Unit:       UnitAgent,
		ExecPath:   "/usr/local/bin/zoomies",
		ConfigFile: "/etc/zoomies/zoomies.yaml",
		User:       "zoomies",
		Group:      "zoomies",
		StateDir:   "/var/lib/zoomies",
		ConfigDir:  "/etc/zoomies",
	}
	out, err := RenderSystemdUnit(spec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "EnvironmentFile") {
		t.Errorf("a joined host's unit names an environment file nobody asked for:\n%s", out)
	}
	spec.EnvFile = MachineEnvPath
	out, err = RenderSystemdUnit(spec)
	if err != nil {
		t.Fatal(err)
	}
	// The dash is load-bearing: the file is written by the controller after
	// the clone boots, and a unit that failed for want of it could never be
	// the unit that reads it.
	if !strings.Contains(out, "EnvironmentFile=-"+MachineEnvPath+"\n") {
		t.Errorf("the environment file is not read, or is not optional:\n%s", out)
	}
	if strings.Contains(out, "{{") {
		t.Errorf("unrendered template directive left in the unit:\n%s", out)
	}
}

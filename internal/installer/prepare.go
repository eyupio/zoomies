package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/config"
)

// The environment file a provider writes into a machine it rents. It is the
// whole of a machine's enrolment -- the controller, a single-use join token,
// the name it must take -- and the unit PrepareAgent writes reads it, which is
// how a template with no credential in it becomes a host that has one.
//
// The controller writes the same path (internal/controller/machineenrol.go),
// and a test in each package pins the two to one string.
const MachineEnvPath = "/etc/zoomies/zoomies.env"

// PrepareOptions describe an agent install that joins nothing.
type PrepareOptions struct {
	ConfigDir  string
	StateDir   string
	BinaryPath string
	// ServiceUser is the account the agent service runs as.
	ServiceUser string
	// Service selects the supervisor; empty detects one.
	Service ServiceKind
	Out     io.Writer
	Logger  *slog.Logger

	// newManager is the test seam: the real one writes under /etc.
	newManager func(kind ServiceKind, unit string) (ServiceManager, error)
}

func (o PrepareOptions) configDir() string {
	if o.ConfigDir != "" {
		return o.ConfigDir
	}
	return config.ConfigDir()
}

func (o PrepareOptions) stateDir() string {
	if o.StateDir != "" {
		return o.StateDir
	}
	return config.StateDir()
}

// PrepareAgent installs the agent service on a machine that is about to become
// a template, and enrols it with nothing.
//
// A provider clones the template and then writes one file into each clone,
// MachineEnvPath, carrying that machine's own credential; the unit written
// here reads that file and is left disabled so that a clone tries to join only
// once the controller has told it what to join as. Everything else -- the
// service account, the directories, a configuration with the agent switched
// on -- is what `zoomies agent join` would have done, minus the join.
//
// It refuses a machine that has already joined something. agent.json is one
// host's identity, and a template containing one clones that identity into
// every machine made from it, which shows up as work vanishing rather than as
// an error. The docs say to delete it; this says so at the moment it matters.
func PrepareAgent(ctx context.Context, opts PrepareOptions) error {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.newManager == nil {
		opts.newManager = NewServiceManager
	}
	u := newUI(opts.Out)

	det := Detect(ctx, Options{ConfigDir: opts.ConfigDir, StateDir: opts.StateDir, InstalledBinary: opts.BinaryPath, NonInteractive: true})
	workDir := filepath.Join(opts.stateDir(), "work")
	configFile := filepath.Join(opts.configDir(), "zoomies.yaml")

	if statePath := agent.StatePath(workDir); exists(statePath) {
		return fmt.Errorf("installer: %s holds a host's identity, and a template that carries one clones it into every machine made from it; "+
			"delete that file (and remove the host on the controller if it is still listed there), then run this again", statePath)
	}

	serviceUser, serviceGroup := opts.ServiceUser, opts.ServiceUser
	if serviceUser == "" {
		serviceUser, serviceGroup = defaultServiceUser(det)
	}

	u.step("Service user and directories")
	if det.Root && det.OS != "darwin" {
		created, err := ensureServiceUser(ctx, serviceUser, serviceGroup, opts.stateDir())
		if err != nil {
			return err
		}
		if created {
			u.ok("created the system user " + serviceUser)
		}
	}
	if err := prepareDirs(ctx, runtime.GOOS, runCommand, opts.configDir(), opts.stateDir(), workDir); err != nil {
		return err
	}
	u.ok(opts.configDir() + " and " + opts.stateDir() + " are ready")

	// A configuration with nothing in it but "this is an agent". The controller,
	// the name and the capacity arrive in the environment file, per machine.
	u.step("Configuration")
	cfg := config.Default()
	cfg.Agent.Embedded = false
	cfg.Agent.WorkDir = workDir
	if err := cfg.Save(configFile); err != nil {
		return fmt.Errorf("installer: writing %s: %w", configFile, err)
	}
	u.ok("wrote " + configFile)
	if det.Root && det.OS != "darwin" {
		if uid, gid, err := lookupUser(serviceUser, serviceGroup); err == nil {
			for _, p := range []string{opts.configDir(), opts.stateDir(), workDir, configFile} {
				_ = os.Chown(p, uid, gid)
			}
		}
	}

	u.step("Service")
	kind := opts.Service
	if kind == "" {
		kind = DetectServiceKind(det)
	}
	if kind != ServiceSystemd {
		// The provider's bootstrap runs `systemctl enable --now zoomies-agent`
		// inside the guest, so a template without systemd is a template whose
		// machines never join. Saying so here is cheaper than a fleet of
		// machines that boot and sit there.
		return errors.New("installer: a machine that a provider clones needs systemd, because the controller enrols a clone by enabling zoomies-agent.service inside it; " +
			"prepare the template on a systemd-based Linux image (Ubuntu 24.04 is what has been qualified)")
	}
	mgr, err := opts.newManager(kind, UnitAgent)
	if err != nil {
		return err
	}
	spec := ServiceSpec{
		Unit:       UnitAgent,
		ExecPath:   det.BinaryPath,
		ConfigFile: configFile,
		User:       serviceUser,
		Group:      serviceGroup,
		StateDir:   opts.stateDir(),
		ConfigDir:  opts.configDir(),
		EnvFile:    MachineEnvPath,
		// Every qualified machine runs Docker, and a unit that raced the
		// daemon would fail its first probe for no good reason. The line is
		// harmless on a template that ships something else.
		WantsDocker: true,
		RuntimeName: "docker",
	}
	if group, ok := dockerSocketGroup(); ok {
		spec.SupplementaryGroups = []string{group}
	}
	path, err := mgr.Install(ctx, spec)
	if err != nil {
		return err
	}
	// Installed and deliberately not enabled: `enable --now` is the
	// controller's line, run once the environment file is in place.
	if err := mgr.Disable(ctx); err != nil {
		return fmt.Errorf("installer: leaving the agent service disabled: %w", err)
	}
	u.ok("installed " + path + ", disabled")
	u.note("it reads " + MachineEnvPath + ", which the controller writes into each clone; nothing here joins anything")

	u.blank()
	u.step("Done")
	u.note("shut this machine down and convert it to a template; a provider clones it from here")
	return nil
}

// dockerSocketGroup names the group that owns the root Docker socket, when
// there is one, so the unit can join it and reach the daemon without being
// root. A rootless socket, or no Docker at all, needs no line.
func dockerSocketGroup() (string, bool) {
	facts, ok := statSocket("unix:///var/run/docker.sock")
	if !ok || facts.gid == 0 {
		return "", false
	}
	return socketGroupName(facts.gid), true
}

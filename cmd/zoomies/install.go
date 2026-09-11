package main

import (
	"context"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/installer"
	"github.com/eyupio/zoomies/internal/logging"
)

// runInit is `zoomies init`: the interactive setup install.sh hands off to.
//
// Every --detected-* flag is something the shell script already worked out on
// this host moments ago. They are accepted rather than re-derived because two
// answers to the same question is worse than one, even when the second one is
// arrived at honestly.
func runInit(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies init [flags]",
		"Set this host up: how it runs, backend, listener, GitHub App and the first administrator.")

	mode := fs.String("mode", "", "single (a controller with an embedded agent), controller, or agent; empty asks")
	deployment := fs.String("deployment", "", "native (the binary under systemd or launchd), compose (a docker-compose.yml and a populated .env) or docker (one container); empty asks, offering only what this host can run")
	controllerURL := fs.String("controller", "", "for --mode agent: the controller to join")
	joinToken := fs.String("join-token", "", "for --mode agent: a join token from the UI")
	externalURL := fs.String("external-url", "", "for a controller: the public hostname or URL browsers and GitHub use; a bare hostname implies https://")
	port := fs.Int("port", 0, "host port for the controller; 0 asks interactively or uses the detected default")
	answers := fs.String("answers", "", "a YAML answer file for unattended setup; implies --non-interactive")
	nonInteractive := fs.Bool("non-interactive", false, "never prompt; a missing answer is an error naming the key")
	assumeYes := fs.Bool("yes", false, "accept the confirmations that are not destructive")
	printAnswers := fs.Bool("print-answers", false, "write an annotated example answer file to stdout and exit")

	configDir := fs.String("config-dir", "", "where zoomies.yaml and the encryption key go (default: "+config.ConfigDir()+")")
	stateDir := fs.String("state-dir", "", "where the database and runner scratch space go (default: "+config.StateDir()+")")
	installedBinary := fs.String("installed-binary", "", "where the binary lives, which is what the service unit will exec")

	detectedOS := fs.String("detected-os", "", "what install.sh found: the operating system")
	detectedArch := fs.String("detected-arch", "", "what install.sh found: the CPU architecture")
	detectedDistro := fs.String("detected-distro", "", "what install.sh found: the distribution")
	detectedInit := fs.String("detected-init", "", "what install.sh found: the init system")
	detectedRuntime := fs.String("detected-runtime", "", "what install.sh found: the container runtime")
	detectedSocket := fs.String("detected-socket", "", "what install.sh found: the runtime's socket")
	detectedRootless := fs.Bool("detected-rootless", false, "what install.sh found: the runtime is rootless")
	detectedCompose := fs.String("detected-compose", "", "what install.sh found: the compose command, e.g. \"docker compose\"")

	fs.example(
		"zoomies init",
		"zoomies init --deployment compose",
		"zoomies init --mode agent --controller https://zoomies.example.com --join-token zoojoin_...",
		"zoomies init --print-answers > answers.yaml",
		"zoomies init --non-interactive --answers answers.yaml",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}

	// Printing the example is not setup; it is the thing an operator does
	// before setup, and it must not touch the host.
	if *printAnswers {
		return installer.WriteExample(e.out)
	}

	parsedMode, err := installer.ParseMode(*mode)
	if err != nil {
		return err
	}
	parsedDeployment, err := installer.ParseDeployment(*deployment)
	if err != nil {
		return err
	}

	// Text at warn level: the installer talks to the operator in prose, and a
	// stream of JSON in the middle of it would be unreadable.
	log := logging.Setup(logging.Options{Level: "warn", Format: "text"})

	inst, err := installer.New(installer.Options{
		DetectedOS:       *detectedOS,
		DetectedArch:     *detectedArch,
		DetectedDistro:   *detectedDistro,
		DetectedInit:     *detectedInit,
		DetectedRuntime:  *detectedRuntime,
		DetectedSocket:   *detectedSocket,
		DetectedRootless: *detectedRootless,
		DetectedCompose:  *detectedCompose,
		InstalledBinary:  *installedBinary,
		Mode:             parsedMode,
		Deployment:       parsedDeployment,
		ControllerURL:    *controllerURL,
		JoinToken:        *joinToken,
		ExternalURL:      *externalURL,
		Port:             *port,
		AnswersFile:      *answers,
		NonInteractive:   *nonInteractive || *answers != "",
		AssumeYes:        *assumeYes,
		ConfigDir:        *configDir,
		StateDir:         *stateDir,
		Out:              e.out,
		In:               e.in,
		Logger:           log,
	})
	if err != nil {
		return err
	}
	return inst.Run(ctx)
}

// runUninstall is `zoomies uninstall`: take Zoomies off this host, having first
// deregistered its runners from GitHub while the credentials to do it still
// exist.
func runUninstall(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies uninstall [--yes]",
		"Remove Zoomies from this host: the service or container, the database, the encryption key and the configuration.")
	yes := fs.Bool("yes", false, "do not ask for confirmation; the summary is printed either way")
	nonInteractive := fs.Bool("non-interactive", false, "never prompt, which means nothing is removed without --yes")
	keepConfig := fs.Bool("keep-config", false, "leave zoomies.yaml in place")
	deregister := fs.Bool("deregister", true, "deregister this instance's runners from GitHub first; without it they become orphans somebody deletes by hand")
	volumes := fs.Bool("volumes", false, "for a compose or docker deployment: delete the data volume too, which destroys the database; asked when not given")
	binary := fs.String("binary", "", "also remove the binary at this path")
	serviceUser := fs.String("service-user", "", "the service account to remove (default: zoomies, when running as root)")
	configDir := fs.String("config-dir", "", "where zoomies.yaml and the encryption key live (default: "+config.ConfigDir()+")")
	stateDir := fs.String("state-dir", "", "where the database lives (default: "+config.StateDir()+")")
	fs.example(
		"zoomies uninstall",
		"zoomies uninstall --yes --keep-config",
		"zoomies uninstall --yes --volumes",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}

	// Deregistration is a tri-state: asked about unless the operator said.
	// flag.Bool cannot express that, so "was it typed" is the third state.
	var wanted *bool
	if fs.changed("deregister") {
		wanted = deregister
	}
	// The volume holds the database, so "not mentioned" must mean "ask", never
	// "delete it".
	var wantVolume *bool
	if fs.changed("volumes") {
		wantVolume = volumes
	}

	return installer.Uninstall(ctx, installer.UninstallOptions{
		ConfigDir:      *configDir,
		StateDir:       *stateDir,
		BinaryPath:     *binary,
		ServiceUser:    *serviceUser,
		Yes:            *yes,
		NonInteractive: *nonInteractive,
		Deregister:     wanted,
		RemoveVolume:   wantVolume,
		KeepConfig:     *keepConfig,
		Out:            e.out,
		In:             e.in,
		Logger:         logging.Setup(logging.Options{Level: "warn", Format: "text"}),
	})
}

// runUpgrade is the second half of install.sh --upgrade. The script verifies
// and installs the new binary before this applies it to the existing service.
func runUpgrade(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies upgrade [flags]", "Apply the installed binary and matching images to an existing deployment, keeping its configuration and credentials.")
	configDir := fs.String("config-dir", "", "where the existing configuration and deployment record live")
	binary := fs.String("installed-binary", "", "the binary path used by the existing service")
	dockerHost := fs.String("docker-host", "", "the existing container runtime endpoint")
	runtime := fs.String("runtime", "docker", "docker or podman, as detected by install.sh")
	image := fs.String("image", "", "replacement image for a custom container deployment")
	mode := fs.String("mode", "", "agent, controller or single; refuses a different existing deployment")
	check := fs.Bool("check", false, "check the deployment without changing or restarting anything")
	fs.example("curl -fsSL https://zoomies.sh/install.sh | sh -s -- --upgrade", "zoomies upgrade --check --mode agent")
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	parsed, err := installer.ParseMode(*mode)
	if err != nil {
		return err
	}
	return installer.Upgrade(ctx, installer.UpgradeOptions{ConfigDir: *configDir, BinaryPath: *binary, DockerHost: *dockerHost, Runtime: *runtime, Image: *image, Mode: parsed, Check: *check, Out: e.out})
}

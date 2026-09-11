package installer

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/eyupio/zoomies/internal/config"
)

// DeploymentAction is an operator lifecycle operation on the containerised
// deployment recorded by zoomies init.
type DeploymentAction string

const (
	DeploymentStart   DeploymentAction = "start"
	DeploymentStop    DeploymentAction = "stop"
	DeploymentRestart DeploymentAction = "restart"
	DeploymentDown    DeploymentAction = "down"
	DeploymentStatus  DeploymentAction = "status"
	DeploymentLogs    DeploymentAction = "logs"
	DeploymentUpdate  DeploymentAction = "update"
)

// DeploymentControlOptions targets the deployment record rather than guessing
// container names, Compose variants, or environment-file locations.
type DeploymentControlOptions struct {
	ConfigDir    string
	Action       DeploymentAction
	RemoveVolume bool
	Image        string
	DockerHost   string
	Runtime      string
	BinaryPath   string
	Out          io.Writer
	run          commandRunner
}

// ControlDeployment starts, stops, restarts, updates, inspects, or takes down
// the container deployment created by zoomies init.
func ControlDeployment(ctx context.Context, opts DeploymentControlOptions) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.run == nil {
		opts.run = runCommand
	}
	if opts.ConfigDir == "" {
		opts.ConfigDir = config.ConfigDir()
	}
	rec, ok := ReadDeploymentRecord(opts.ConfigDir)
	if !ok || !rec.Deployment.Containerised() {
		return fmt.Errorf("installer: no containerised Zoomies deployment was found in %s", DeploymentRecordPath(opts.ConfigDir))
	}
	if opts.Action == DeploymentUpdate {
		return Upgrade(ctx, UpgradeOptions{
			ConfigDir: opts.ConfigDir, BinaryPath: opts.BinaryPath, DockerHost: opts.DockerHost,
			Runtime: deploymentRuntime(rec, opts.Runtime), Image: opts.Image, Out: opts.Out, run: opts.run,
		})
	}

	name, args, err := deploymentActionCommand(rec, opts.Action, opts.RemoveVolume)
	if err != nil {
		return err
	}
	out, err := opts.run(ctx, name, args...)
	if err != nil {
		return fmt.Errorf("installer: %s deployment: %w", opts.Action, err)
	}
	if strings.TrimSpace(out) != "" {
		fmt.Fprintln(opts.Out, strings.TrimSpace(out))
	}
	fmt.Fprintf(opts.Out, "Zoomies deployment %s complete.\n", opts.Action)
	return nil
}

func deploymentRuntime(rec DeploymentRecord, override string) string {
	if override != "" {
		return override
	}
	if len(rec.ComposeCommand) > 0 && rec.ComposeCommand[0] == "podman" {
		return "podman"
	}
	return "docker"
}

func deploymentActionCommand(rec DeploymentRecord, action DeploymentAction, removeVolume bool) (string, []string, error) {
	if rec.Deployment == DeploymentCompose {
		var args []string
		switch action {
		case DeploymentStart:
			args = []string{"up", "-d"}
		case DeploymentStop:
			args = []string{"stop"}
		case DeploymentRestart:
			args = []string{"restart"}
		case DeploymentStatus:
			args = []string{"ps"}
		case DeploymentLogs:
			args = []string{"logs", "--tail", "100"}
		case DeploymentDown:
			args = []string{"down"}
			if removeVolume {
				args = append(args, "-v")
			}
		default:
			return "", nil, fmt.Errorf("installer: unknown deployment action %q", action)
		}
		name, command := ComposeArgs(rec.ComposeCommand, rec.ComposeFile(), args...)
		return name, command, nil
	}

	runtime := deploymentRuntime(rec, "")
	container := containerOr(rec)
	switch action {
	case DeploymentStart:
		return runtime, []string{"start", container}, nil
	case DeploymentStop:
		return runtime, []string{"stop", container}, nil
	case DeploymentRestart:
		return runtime, []string{"restart", container}, nil
	case DeploymentStatus:
		return runtime, []string{"inspect", "--format", "{{.Name}}  {{.State.Status}}  {{.Config.Image}}", container}, nil
	case DeploymentLogs:
		return runtime, []string{"logs", "--tail", "100", container}, nil
	case DeploymentDown:
		args := []string{"rm", "-f", container}
		if removeVolume {
			// Docker cannot remove a container and a named volume in one command;
			// the CLI executes the second safe, explicit operation below.
			return "", nil, fmt.Errorf("installer: --volumes with a docker deployment belongs to `zoomies uninstall --volumes`; down keeps the database")
		}
		return runtime, args, nil
	default:
		return "", nil, fmt.Errorf("installer: unknown deployment action %q", action)
	}
}

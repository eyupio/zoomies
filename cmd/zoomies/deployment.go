package main

import (
	"context"
	"fmt"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/installer"
)

func runDeployment(ctx context.Context, e *env, args []string) error {
	subs := []*subcommand{
		{"status", "", "Show the installed container and image state", deploymentAction(installer.DeploymentStatus)},
		{"logs", "", "Show the latest 100 controller log lines", deploymentAction(installer.DeploymentLogs)},
		{"start", "", "Start a stopped deployment", deploymentAction(installer.DeploymentStart)},
		{"stop", "", "Stop containers without removing them", deploymentAction(installer.DeploymentStop)},
		{"restart", "", "Restart the current containers", deploymentAction(installer.DeploymentRestart)},
		{"update", "", "Pull matching controller and runner images and safely recreate the controller", runDeploymentUpdate},
		{"down", "", "Stop and remove containers while keeping the database volume", deploymentAction(installer.DeploymentDown)},
	}
	return runGroup(ctx, e, "deployment", "Operate the Docker or Compose deployment recorded by `zoomies init`.", subs, args)
}

func deploymentAction(action installer.DeploymentAction) func(context.Context, *env, []string) error {
	return deploymentActionNamed("deployment "+string(action), action)
}

func deploymentActionNamed(command string, action installer.DeploymentAction) func(context.Context, *env, []string) error {
	return func(ctx context.Context, e *env, args []string) error {
		fs := newFlagSet(e, "zoomies "+command+" [--config-dir path]",
			"Use the saved deployment record, so container names and Compose files are never guessed.")
		configDir := fs.String("config-dir", "", "directory containing deployment.json (default: "+config.ConfigDir()+")")
		fs.example("zoomies "+command, "zoomies "+command+" --config-dir /etc/zoomies")
		if err := fs.parse(args); err != nil {
			return err
		}
		if err := fs.noMoreArgs(); err != nil {
			return err
		}
		return installer.ControlDeployment(ctx, installer.DeploymentControlOptions{
			ConfigDir: *configDir, Action: action, Out: e.out,
		})
	}
}

// runLogs is the short spelling operators reach for during setup and incident
// response. The nested command remains available for backwards compatibility.
func runLogs(ctx context.Context, e *env, args []string) error {
	return deploymentActionNamed("logs", installer.DeploymentLogs)(ctx, e, args)
}

func runDeploymentUpdate(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies deployment update [flags]",
		"Pull matching controller and cached runner images, then safely recreate the controller with rollback on failure.")
	configDir := fs.String("config-dir", "", "directory containing deployment.json (default: "+config.ConfigDir()+")")
	image := fs.String("image", "", "replacement image for a custom deployment; stock images follow this binary's release channel")
	dockerHost := fs.String("docker-host", "", "container runtime endpoint; empty uses the recorded/default endpoint")
	runtime := fs.String("runtime", "", "docker or podman; empty derives it from the deployment")
	fs.example("zoomies deployment update", "zoomies deployment update --image registry.example.com/zoomies:v2")
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	if *runtime != "" && *runtime != "docker" && *runtime != "podman" {
		return fmt.Errorf("--runtime must be docker or podman")
	}
	return installer.ControlDeployment(ctx, installer.DeploymentControlOptions{
		ConfigDir: *configDir, Action: installer.DeploymentUpdate, Image: *image,
		DockerHost: *dockerHost, Runtime: *runtime, Out: e.out,
	})
}

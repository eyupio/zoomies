package installer

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDeploymentActionCommandsUseTheRecordedDeployment(t *testing.T) {
	compose := DeploymentRecord{
		Deployment: DeploymentCompose, Directory: "/srv/zoomies",
		ComposeCommand: []string{"docker", "compose"},
	}
	name, args, err := deploymentActionCommand(compose, DeploymentStart, false)
	if err != nil {
		t.Fatal(err)
	}
	if name != "docker" || !reflect.DeepEqual(args[len(args)-2:], []string{"up", "-d"}) {
		t.Fatalf("compose start = %s %v", name, args)
	}
	_, args, err = deploymentActionCommand(compose, DeploymentDown, true)
	if err != nil || !reflect.DeepEqual(args[len(args)-2:], []string{"down", "-v"}) {
		t.Fatalf("compose down --volumes = %v, %v", args, err)
	}

	docker := DeploymentRecord{Deployment: DeploymentDocker, Container: "my-zoomies"}
	name, args, err = deploymentActionCommand(docker, DeploymentRestart, false)
	if err != nil || name != "docker" || !reflect.DeepEqual(args, []string{"restart", "my-zoomies"}) {
		t.Fatalf("docker restart = %s %v, %v", name, args, err)
	}
	name, args, err = deploymentActionCommand(docker, DeploymentDown, false)
	if err != nil || name != "docker" || !reflect.DeepEqual(args, []string{"rm", "-f", "my-zoomies"}) {
		t.Fatalf("docker down = %s %v, %v", name, args, err)
	}
}

func TestControlDeploymentRunsAndReportsTheRequestedAction(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteDeploymentRecord(dir, DeploymentRecord{
		Deployment: DeploymentCompose, Directory: dir,
		ComposeCommand: []string{"docker", "compose"},
	}); err != nil {
		t.Fatal(err)
	}
	var called string
	var out bytes.Buffer
	err := ControlDeployment(context.Background(), DeploymentControlOptions{
		ConfigDir: dir, Action: DeploymentStatus, Out: &out,
		run: func(_ context.Context, name string, args ...string) (string, error) {
			called = name + " " + strings.Join(args, " ")
			return "zoomies running", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(called, "ps") || !strings.Contains(out.String(), "zoomies running") || !strings.Contains(out.String(), "status complete") {
		t.Fatalf("called %q, output %q", called, out.String())
	}
}

func TestControlDeploymentRefusesAnUnrecordedHost(t *testing.T) {
	err := ControlDeployment(context.Background(), DeploymentControlOptions{
		ConfigDir: t.TempDir(), Action: DeploymentStop,
	})
	if err == nil || !strings.Contains(err.Error(), "no containerised") {
		t.Fatalf("error = %v", err)
	}
}

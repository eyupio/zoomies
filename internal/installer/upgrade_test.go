package installer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
)

func TestUpgradeSaysWhetherAMovingImageAdvanced(t *testing.T) {
	for _, tc := range []struct {
		name      string
		before    string
		after     string
		runBefore string
		runAfter  string
		want      string
	}{
		{"same image", "sha256:aaaaaaaaaaaaaaaa", "sha256:aaaaaaaaaaaaaaaa", "sha256:aaaaaaaaaaaaaaaa", "sha256:aaaaaaaaaaaaaaaa", "Image channel did not advance"},
		{"new image", "sha256:aaaaaaaaaaaaaaaa", "sha256:bbbbbbbbbbbbbbbb", "sha256:aaaaaaaaaaaaaaaa", "sha256:bbbbbbbbbbbbbbbb", "Image channel advanced"},
		// The tag was pulled before the upgrade while the service still ran
		// an older image. Saying "did not advance" there hid an upgrade that
		// recreated the service onto a new build and migrated its database.
		{"tag pulled earlier", "sha256:bbbbbbbbbbbbbbbb", "sha256:bbbbbbbbbbbbbbbb", "sha256:aaaaaaaaaaaaaaaa", "sha256:bbbbbbbbbbbbbbbb", "The service moved from aaaaaaaaaaaa to bbbbbbbbbbbb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, _ := upgradeFixture(t, DeploymentCompose)
			var out bytes.Buffer
			opts.Out = &out
			inspects, running := 0, 0
			opts.run = func(_ context.Context, name string, args ...string) (string, error) {
				line := name + " " + strings.Join(args, " ")
				switch {
				case strings.Contains(line, "config --images"):
					return opts.Image, nil
				case strings.Contains(line, "image inspect"):
					inspects++
					if inspects == 1 {
						return tc.before, nil
					}
					return tc.after, nil
				case strings.Contains(line, "{{.Image}}"):
					running++
					if running == 1 {
						return tc.runBefore, nil
					}
					return tc.runAfter, nil
				case name == "docker" && len(args) > 0 && args[0] == "inspect":
					return "true", nil
				default:
					return "", nil
				}
			}
			if err := Upgrade(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			if tc.runBefore != tc.runAfter && strings.Contains(out.String(), "did not advance") {
				t.Fatalf("output = %q, says nothing changed on an upgrade that moved the service", out.String())
			}
			if !strings.Contains(out.String(), tc.want) || !strings.Contains(out.String(), opts.Image) {
				t.Fatalf("output = %q, want %q and image", out.String(), tc.want)
			}
		})
	}
}

func upgradeFixture(t *testing.T, deployment Deployment) (UpgradeOptions, DeploymentRecord) {
	t.Helper()
	dir := t.TempDir()
	rec := DeploymentRecord{Deployment: deployment, Directory: dir, EnvFile: filepath.Join(dir, ".env"), Image: stockAgentRepository + ":v0.1", Mode: ModeAgent, Container: "zoomies", ComposeCommand: []string{"docker", "compose"}}
	if err := os.WriteFile(rec.EnvFile, []byte("# keep this comment\nZOOMIES_IMAGE="+rec.Image+"\nZOOMIES_JOIN_TOKEN=existing-credential\nCUSTOM_SETTING='leave me alone'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteDeploymentRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	// A deployment this release installed: its Compose file mounts the
	// shared folder and the folder is laid out, so the upgrade has nothing to
	// add and these tests see only the upgrade itself.
	if deployment == DeploymentCompose {
		rendered, err := RenderComposeFile(composeSpecFor(rec, deploymentSettings{cfg: config.Default(), read: true}, 0))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(rec.ComposeFile(), []byte(rendered), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	shared := filepath.Join(dir, "shared")
	if _, err := PrepareSharedDir(shared, -1, -1); err != nil {
		t.Fatal(err)
	}
	return UpgradeOptions{ConfigDir: dir, Mode: ModeAgent, Image: stockAgentRepository + ":v9.0",
		shared: &sharedTarget{dir: shared, uid: -1, gid: -1}, socketGroup: func(string) int { return 0 }}, rec
}

func TestComposeUpgradeKeepsConfigurationAndPullsBeforeRestarting(t *testing.T) {
	for _, fail := range []string{"", "pull", "up"} {
		t.Run(fail, func(t *testing.T) {
			opts, rec := upgradeFixture(t, DeploymentCompose)
			before, _ := os.ReadFile(rec.EnvFile)
			var calls []string
			opts.run = func(_ context.Context, name string, args ...string) (string, error) {
				line := name + " " + strings.Join(args, " ")
				calls = append(calls, line)
				if strings.Contains(line, "config --images") {
					return opts.Image, nil
				}
				if name == "docker" {
					if len(args) > 0 && args[0] == "inspect" {
						return "true", nil
					}
					return "", nil
				}
				if !strings.Contains(line, "--env-file "+rec.EnvFile) {
					t.Errorf("no recorded env file: %s", line)
				}
				if strings.Contains(line, "pull zoomies") {
					current, _ := os.ReadFile(rec.EnvFile)
					if string(current) != string(before) {
						t.Error("env changed before image pull succeeded")
					}
				}
				if fail != "" && strings.Contains(line, " "+fail+" ") {
					return "", errors.New("test refusal")
				}
				return "", nil
			}
			err := Upgrade(context.Background(), opts)
			if (err != nil) != (fail != "") {
				t.Fatalf("failure=%s: %v", fail, err)
			}
			after, _ := os.ReadFile(rec.EnvFile)
			want := string(before)
			if fail == "" {
				want = strings.ReplaceAll(want, rec.Image, opts.Image)
			}
			if string(after) != want {
				t.Fatalf("configuration changed unexpectedly: %q", after)
			}
			stored, ok := ReadDeploymentRecord(opts.ConfigDir)
			if !ok || (fail == "" && stored.Image != opts.Image) || (fail != "" && stored.Image != rec.Image) {
				t.Fatalf("record disagrees with result: %+v", stored)
			}
			joined := strings.Join(calls, "\n")
			for _, call := range calls {
				if strings.Contains(call, " up ") && !strings.Contains(call, "--timeout 1200") {
					t.Fatalf("upgrade or rollback can kill admitted work: %s", call)
				}
			}
			if fail == "pull" && strings.Contains(joined, " up ") {
				t.Fatal("restarted after failed pull")
			}
			if fail == "" && (!strings.Contains(joined, "--no-deps --force-recreate --timeout 1200 zoomies") || strings.Index(joined, "pull zoomies") > strings.Index(joined, " up ")) {
				t.Fatalf("unsafe ordering: %s", joined)
			}
		})
	}
}

func TestUpgradePreflightNeverPullsOrChangesAnExistingDeployment(t *testing.T) {
	opts, rec := upgradeFixture(t, DeploymentCompose)
	opts.Check = true
	before, _ := os.ReadFile(rec.EnvFile)
	opts.run = func(_ context.Context, name string, args ...string) (string, error) {
		line := name + " " + strings.Join(args, " ")
		// Reading is allowed -- the image the file names, where the data
		// volume is -- and nothing else.
		if strings.Contains(line, "inspect --type container") {
			return "", nil
		}
		if !strings.Contains(line, "config --images") {
			t.Fatalf("preflight tried to change something: %s", line)
		}
		return opts.Image, nil
	}
	if err := Upgrade(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(rec.EnvFile)
	if string(after) != string(before) {
		t.Fatal("preflight changed environment")
	}
}

func TestUpgradeRefusesAnUnreadableRecordOrACustomImageBeforeChangingAnything(t *testing.T) {
	opts, rec := upgradeFixture(t, DeploymentCompose)
	opts.Image = ""
	rec.Image = "registry.example/custom:old"
	if _, err := WriteDeploymentRecord(opts.ConfigDir, rec); err != nil {
		t.Fatal(err)
	}
	opts.run = func(context.Context, string, ...string) (string, error) {
		t.Fatal("a refused upgrade ran a command")
		return "", nil
	}
	if err := Upgrade(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "--image") {
		t.Fatalf("custom image: %v", err)
	}
	if err := os.WriteFile(DeploymentRecordPath(opts.ConfigDir), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Upgrade(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "record is invalid") {
		t.Fatalf("invalid record: %v", err)
	}
}

func TestNativeUpgradeRestartsTheExistingAgentAndRefusesTheWrongBinary(t *testing.T) {
	requirePOSIX(t)
	for _, wrong := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching", true: "wrong path"}[wrong], func(t *testing.T) {
			var calls []string
			shared := filepath.Join(t.TempDir(), "shared")
			if _, err := PrepareSharedDir(shared, -1, -1); err != nil {
				t.Fatal(err)
			}
			opts := UpgradeOptions{ConfigDir: t.TempDir(), Mode: ModeAgent, BinaryPath: "/custom/bin/zoomies",
				shared: &sharedTarget{dir: shared, uid: -1, gid: -1}}
			opts.run = func(_ context.Context, name string, args ...string) (string, error) {
				line := name + " " + strings.Join(args, " ")
				calls = append(calls, line)
				if strings.Contains(line, "LoadState") {
					return "loaded", nil
				}
				if strings.Contains(line, "ExecStart") {
					if wrong {
						return "{ path=/other/zoomies ; }", nil
					}
					return "{ path=/custom/bin/zoomies ; argv[]=/custom/bin/zoomies agent ; }", nil
				}
				return "", nil
			}
			err := Upgrade(context.Background(), opts)
			if (err != nil) != wrong {
				t.Fatalf("wrong=%v: %v", wrong, err)
			}
			restarted := strings.Contains(strings.Join(calls, "\n"), "systemctl restart zoomies-agent")
			if restarted == wrong {
				t.Fatalf("wrong service restart: %v", calls)
			}
		})
	}
}

func TestStockRunnerImagesAreRefreshedOnceWithoutChangingPinnedReferences(t *testing.T) {
	var pulled []string
	p := upgradePlan{opts: UpgradeOptions{DockerHost: "unix:///tmp/daemon.sock", Out: os.Stdout, run: func(_ context.Context, _ string, args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "image ls") {
			return "ghcr.io/eyupio/zoomies-runner:dev\nghcr.io/eyupio/zoomies-runner:dev\nprivate.example/custom:v2\nghcr.io/eyupio/zoomies-runner-docker:v1\nghcr.io/eyupio/zoomies-runner:<none>", nil
		}
		pulled = append(pulled, args[len(args)-1])
		return "", nil
	}}}
	if err := p.pullRunnerImages(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"ghcr.io/eyupio/zoomies-runner:dev", "ghcr.io/eyupio/zoomies-runner-docker:v1"}
	if !reflect.DeepEqual(pulled, want) {
		t.Fatalf("pulled %v", pulled)
	}
}

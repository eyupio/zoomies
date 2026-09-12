package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/naming"
)

// Also run with -ldflags -X .../naming.RunnerImageTag=dev (and release tags)
// to verify package initialization and real config precedence in a stamped build.
func TestBuildRunnerImageDefaultsAndOverrides(t *testing.T) {
	want := naming.RunnerImageRepo + ":" + naming.RunnerImageTag
	if got := Default().GitHub.RunnerImage; got != want {
		t.Fatalf("default = %q, want %q", got, want)
	}
	dockerWant := "ghcr.io/eyupio/zoomies-runner-docker:" + naming.RunnerImageTag
	if got := ResolvePoolRunnerImage("", "", "", want, true); got != dockerWant || DefaultRunnerDockerImage != dockerWant {
		t.Fatalf("Docker defaults = %q, %q; want %q", got, DefaultRunnerDockerImage, dockerWant)
	}
	for _, variant := range naming.Images() {
		ref, _ := naming.RunnerImageFor(variant.OS, variant.Version)
		dockerRef := "ghcr.io/eyupio/zoomies-runner-docker" + strings.TrimPrefix(ref, naming.RunnerImageRepo)
		if got := ResolvePoolRunnerImage("", variant.OS, variant.Version, want, true); got != dockerRef {
			t.Errorf("Docker platform = %q, want %q", got, dockerRef)
		}
	}
	for _, pin := range []string{naming.RunnerImageRepo + ":v1.2.3", naming.RunnerImageRepo + "@sha256:abcd", "registry.example/runner:custom"} {
		if got := ResolvePoolRunnerImage(pin, "debian", "12", want, true); got != pin {
			t.Errorf("explicit pin changed: %q", got)
		}
	}
	unsetenv(t, "ZOOMIES_RUNNER_IMAGE")
	path := filepath.Join(t.TempDir(), "zoomies.yaml")
	if err := os.WriteFile(path, []byte("github:\n  runner_image: registry.example/runner:yaml\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHub.RunnerImage != "registry.example/runner:yaml" {
		t.Fatalf("YAML override lost: %q", cfg.GitHub.RunnerImage)
	}
	t.Setenv("ZOOMIES_RUNNER_IMAGE", "registry.example/runner:env")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHub.RunnerImage != "registry.example/runner:env" {
		t.Fatalf("environment override lost: %q", cfg.GitHub.RunnerImage)
	}
}

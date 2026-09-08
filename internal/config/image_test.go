package config

import (
	"strings"
	"testing"
)

// A pool that gives its jobs a daemon needs an image with a client, and the
// stock image has none. The swap is the whole of the rule, so every edge of it
// is spelled out here: the moving tags move, a pinned tag stays pinned, a
// digest is left alone, an image of the operator's own is left alone, and
// none of it happens without a daemon.
func TestRunnerImageForSwapsTheStockImageForItsDockerVariant(t *testing.T) {
	digest := "@sha256:" + strings.Repeat("a", 64)
	cases := []struct {
		name   string
		image  string
		daemon bool
		want   string
	}{
		{"the default, with a daemon", DefaultRunnerImage, true, DefaultRunnerDockerImage},
		{"the main tag moves with it", "ghcr.io/eyupio/zoomies-runner:main", true, "ghcr.io/eyupio/zoomies-runner-docker:main"},
		{"no tag at all", "ghcr.io/eyupio/zoomies-runner", true, "ghcr.io/eyupio/zoomies-runner-docker"},
		{"a commit tag is a pin, and stays", "ghcr.io/eyupio/zoomies-runner:sha-b966fb6", true, "ghcr.io/eyupio/zoomies-runner:sha-b966fb6"},
		{"a release tag is a pin, and stays", "ghcr.io/eyupio/zoomies-runner:v0.1-alpha", true, "ghcr.io/eyupio/zoomies-runner:v0.1-alpha"},
		{"already the variant", DefaultRunnerDockerImage, true, DefaultRunnerDockerImage},
		{"a digest names one exact image", "ghcr.io/eyupio/zoomies-runner" + digest, true, "ghcr.io/eyupio/zoomies-runner" + digest},
		{"a tag and a digest", "ghcr.io/eyupio/zoomies-runner:latest" + digest, true, "ghcr.io/eyupio/zoomies-runner:latest" + digest},
		{"an image of the operator's own", "registry.example.com/ci/runner:latest", true, "registry.example.com/ci/runner:latest"},
		{"a mirror of the stock image is not the stock image", "registry.example.com/eyupio/zoomies-runner:latest", true, "registry.example.com/eyupio/zoomies-runner:latest"},
		{"a repository that merely starts the same way", "ghcr.io/eyupio/zoomies-runner-gpu:latest", true, "ghcr.io/eyupio/zoomies-runner-gpu:latest"},
		{"no daemon, no swap", DefaultRunnerImage, false, DefaultRunnerImage},
		{"no daemon leaves the variant alone too", DefaultRunnerDockerImage, false, DefaultRunnerDockerImage},
		{"empty stays empty", "", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RunnerImageFor(tc.image, tc.daemon); got != tc.want {
				t.Fatalf("RunnerImageFor(%q, %v) = %q, want %q", tc.image, tc.daemon, got, tc.want)
			}
		})
	}
}

// The default and its variant are the two ends of the swap, so the constants
// have to be the pair the function translates between: a rename of either
// that forgot the other would make the default pool's swap a no-op.
func TestTheDefaultImagesAreThePairTheSwapTranslatesBetween(t *testing.T) {
	if got := RunnerImageFor(DefaultRunnerImage, true); got != DefaultRunnerDockerImage {
		t.Fatalf("RunnerImageFor(DefaultRunnerImage) = %q, want DefaultRunnerDockerImage %q", got, DefaultRunnerDockerImage)
	}
}

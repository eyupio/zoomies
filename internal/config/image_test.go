package config

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/naming"
)

// A pool that gives its jobs a daemon needs an image with a client, and the
// stock image has none. The swap is the whole of the rule, so every edge of it
// is spelled out here: every tag this build publishes moves, including the
// platform aliases a pool gets by naming an operating system; a pin from some
// other build stays, because the variant may not have been published beside it;
// a digest is left alone, an image of the operator's own is left alone, and
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
		{"the dev tag moves with it", "ghcr.io/eyupio/zoomies-runner:dev", true, "ghcr.io/eyupio/zoomies-runner-docker:dev"},
		{"the main tag moves with it", "ghcr.io/eyupio/zoomies-runner:main", true, "ghcr.io/eyupio/zoomies-runner-docker:main"},
		{"no tag at all", "ghcr.io/eyupio/zoomies-runner", true, "ghcr.io/eyupio/zoomies-runner-docker"},
		{"the platform alias a pool gets by naming an operating system", "ghcr.io/eyupio/zoomies-runner:debian-12", true, "ghcr.io/eyupio/zoomies-runner-docker:debian-12"},
		{"a platform alias on a branch channel", "ghcr.io/eyupio/zoomies-runner:ubuntu-2404-main", true, "ghcr.io/eyupio/zoomies-runner-docker:ubuntu-2404-main"},
		{"a platform alias on the dev channel", "ghcr.io/eyupio/zoomies-runner:rocky-9-dev", true, "ghcr.io/eyupio/zoomies-runner-docker:rocky-9-dev"},
		{"an operating system nothing publishes", "ghcr.io/eyupio/zoomies-runner:alpine-3", true, "ghcr.io/eyupio/zoomies-runner:alpine-3"},
		{"a commit tag may name a run whose variant never finished", "ghcr.io/eyupio/zoomies-runner:sha-b966fb6", true, "ghcr.io/eyupio/zoomies-runner:sha-b966fb6"},
		{"a platform alias on a commit", "ghcr.io/eyupio/zoomies-runner:ubuntu-2204-sha-a5b2c8f", true, "ghcr.io/eyupio/zoomies-runner:ubuntu-2204-sha-a5b2c8f"},
		{"a release this build knows nothing about", "ghcr.io/eyupio/zoomies-runner:v0.1-alpha", true, "ghcr.io/eyupio/zoomies-runner:v0.1-alpha"},
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

// The tag a build hands out is the one case the swap must never refuse: it is
// the image every pool that names none is given, and a deployment stamped with
// a release channel used to keep the whole fleet on a client-less image --
// every Docker job on it failing at its first step -- because the rule only
// knew the moving tags.
func TestTheChannelThisBuildWasStampedWithMoves(t *testing.T) {
	previous := naming.RunnerImageTag
	t.Cleanup(func() { naming.RunnerImageTag = previous })
	for _, channel := range []string{"latest", "dev", "main", "v1.4.0"} {
		naming.RunnerImageTag = channel
		for _, image := range []string{
			naming.RunnerImageRepo + ":" + channel,
			naming.RunnerImageRepo + ":" + naming.DefaultImage().Tag() + "-" + channel,
		} {
			want := "ghcr.io/eyupio/zoomies-runner-docker" + strings.TrimPrefix(image, naming.RunnerImageRepo)
			if got := RunnerImageFor(image, true); got != want {
				t.Errorf("on the %s channel, RunnerImageFor(%q) = %q, want %q", channel, image, got, want)
			}
		}
	}
}

// What the swap could not move, an operator has to be told about: a daemon with
// no client is not a slower pool, it is a pool where every Docker job fails,
// and the sentence that says so needs a reference to paste rather than a
// repository to go and search for.
func TestMissingDockerClientNamesWhatToPinInstead(t *testing.T) {
	digest := "@sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, image, pin string
		missing          bool
	}{
		{"a commit pin", "ghcr.io/eyupio/zoomies-runner:sha-b966fb6", "ghcr.io/eyupio/zoomies-runner-docker:sha-b966fb6", true},
		{"a digest", "ghcr.io/eyupio/zoomies-runner" + digest, "ghcr.io/eyupio/zoomies-runner-docker" + digest, true},
		{"a tag and a digest", "ghcr.io/eyupio/zoomies-runner:latest" + digest, "ghcr.io/eyupio/zoomies-runner-docker:latest" + digest, true},
		{"the variant itself has a client", DefaultRunnerDockerImage, "", false},
		{"an image of the operator's own may or may not", "registry.example.com/ci/runner:latest", "", false},
		{"a mirror is not something this code can know", "registry.example.com/eyupio/zoomies-runner:latest", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pin, missing := MissingDockerClient(tc.image)
			if missing != tc.missing || pin != tc.pin {
				t.Fatalf("MissingDockerClient(%q) = (%q, %v), want (%q, %v)", tc.image, pin, missing, tc.pin, tc.missing)
			}
		})
	}
}

package naming

import (
	"strings"
	"testing"
)

func TestCatalogueTagsMatchPoolNames(t *testing.T) {
	// An image tag and the OS half of a pool name are the same string by
	// construction. If that ever stops being true, a pool named for Ubuntu
	// 24.04 could quietly run a Debian image.
	for _, img := range Images() {
		spec := Spec{OS: img.OS, Version: img.Version}
		name := PoolName(spec)
		if !strings.HasSuffix(name, img.Tag()) {
			t.Errorf("pool name %q does not end in the image tag %q", name, img.Tag())
		}
		if got, want := img.Ref(), RunnerImageRepo+":"+img.Tag(); got != want {
			t.Errorf("Ref = %q, want %q", got, want)
		}
	}
}

func TestCatalogueIsWellFormed(t *testing.T) {
	defaults := 0
	seen := map[string]bool{}
	for _, img := range Images() {
		if NormalizeOS(img.OS) != img.OS {
			t.Errorf("catalogue entry %q is not a normalised OS", img.OS)
		}
		if img.Family != FamilyAPT && img.Family != FamilyDNF {
			t.Errorf("%s: family %q is not one deploy/Dockerfile.runner can drive", img.Tag(), img.Family)
		}
		if img.Base == "" {
			t.Errorf("%s: no base image", img.Tag())
		}
		if len(img.Arches) == 0 {
			t.Errorf("%s: published for no architecture", img.Tag())
		}
		for _, a := range img.Arches {
			if NormalizeArch(a) != a {
				t.Errorf("%s: architecture %q is not normalised", img.Tag(), a)
			}
		}
		if seen[img.Tag()] {
			t.Errorf("%s: duplicate tag", img.Tag())
		}
		seen[img.Tag()] = true
		if img.Default {
			defaults++
		}
	}
	if defaults != 1 {
		t.Errorf("the catalogue has %d default variants; :latest can only point at one", defaults)
	}
}

func TestFindImage(t *testing.T) {
	if img, ok := FindImage("ubuntu", "22.04"); !ok || img.Tag() != "ubuntu-2204" {
		t.Errorf("FindImage(ubuntu, 22.04) = %q, %v", img.Tag(), ok)
	}
	// An OS with no version gets whichever one we publish, preferring the
	// default, so a pool that says only "ubuntu" still starts.
	if img, ok := FindImage("Ubuntu", ""); !ok || img.Tag() != DefaultImage().Tag() {
		t.Errorf("FindImage(Ubuntu, \"\") = %q, %v; want the default variant", img.Tag(), ok)
	}
	// A version we do not publish is a miss, not a silent substitution.
	if _, ok := FindImage("ubuntu", "20.04"); ok {
		t.Error("FindImage substituted a different Ubuntu for one we do not publish")
	}
	if _, ok := FindImage("plan9", ""); ok {
		t.Error("FindImage claimed an operating system that is not in the catalogue")
	}
}

func TestRunnerImageFor(t *testing.T) {
	ref, ok := RunnerImageFor("debian", "12")
	if !ok || ref != RunnerImageRepo+":debian-12" {
		t.Errorf("RunnerImageFor(debian, 12) = %q, %v", ref, ok)
	}
	if ref, ok := RunnerImageFor("", ""); ok {
		t.Errorf("RunnerImageFor with no platform returned %q; the caller keeps its own default", ref)
	}
}

func TestImageSupports(t *testing.T) {
	img := DefaultImage()
	if !img.Supports("arm64") || !img.Supports("amd64") {
		t.Errorf("%s should be published for both architectures", img.Tag())
	}
	if !img.Supports("") {
		t.Error("an unstated architecture is not a mismatch")
	}
	if img.Supports("riscv64") {
		t.Error("Supports accepted an architecture nothing publishes")
	}
}

func TestDefaultRunnerImage(t *testing.T) {
	if got, want := DefaultRunnerImage(), RunnerImageRepo+":latest"; got != want {
		t.Errorf("DefaultRunnerImage = %q, want %q", got, want)
	}
	if !strings.Contains(SupportedPlatforms(), "Ubuntu 24.04") {
		t.Errorf("SupportedPlatforms = %q, which does not name the default", SupportedPlatforms())
	}
}

func TestRunnerImagesFollowBuildChannelAndKeepOverrides(t *testing.T) {
	original := RunnerImageTag
	t.Cleanup(func() { RunnerImageTag = original })
	for _, tag := range []string{"latest", "dev", "main", "sha-abc1234", "v1.2.3", "v1.3.0-rc1"} {
		t.Run(tag, func(t *testing.T) {
			RunnerImageTag = tag
			fallback := DefaultRunnerImage()
			if want := RunnerImageRepo + ":" + tag; fallback != want {
				t.Fatalf("default = %q, want %q", fallback, want)
			}
			for _, variant := range Images() {
				want := variant.Ref()
				if tag != "latest" {
					want += "-" + tag
				}
				if got := ResolveRunnerImage("", variant.OS, variant.Version, fallback); got != want {
					t.Errorf("platform %s = %q, want %q", variant.Tag(), got, want)
				}
			}
			for _, override := range []string{RunnerImageRepo + ":latest", "registry.example/runner:custom", "registry.example/runner@sha256:abcd"} {
				if got := ResolveRunnerImage(override, "debian", "12", fallback); got != override {
					t.Errorf("pool override changed to %q", got)
				}
				if got := ResolveRunnerImage("", "", "", override); got != override {
					t.Errorf("instance override changed to %q", got)
				}
			}
			if got := ResolveRunnerImage("", "", "", fallback); got != fallback {
				t.Errorf("fallback = %q, want %q", got, fallback)
			}
		})
	}
}

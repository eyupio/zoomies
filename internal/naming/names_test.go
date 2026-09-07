package naming

import (
	"strings"
	"testing"
)

func TestBaseCarriesTheBrandExactlyOnce(t *testing.T) {
	cases := map[string]string{
		"":                          Prefix,
		"gpu":                       "zoomies-gpu",
		"zoomies":                   "zoomies",
		"zoomies-gpu":               "zoomies-gpu",
		"Zoomies GPU":               "zoomies-gpu",
		"zoomies-4vcpu-ubuntu-2404": "zoomies-4vcpu-ubuntu-2404",
		// Not the prefix, however much it looks like it: a pool called
		// "zoomiesish" is somebody's own word, not a branded name.
		"zoomiesish": "zoomies-zoomiesish",
	}
	for in, want := range cases {
		if got := Base(in); got != want {
			t.Errorf("Base(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunnerNameKeepsTheShapeAndTheDiscriminator(t *testing.T) {
	spec := Spec{CPUs: 4, OS: OSUbuntu, Version: "24.04"}
	got := RunnerName(spec.String(), "biscuit-a3f9qz2m")
	if want := "zoomies-4vcpu-ubuntu-2404-biscuit-a3f9qz2m"; got != want {
		t.Errorf("RunnerName = %q, want %q", got, want)
	}
	// A pool named by hand still gets the brand, and gets it once.
	if got := RunnerName("gpu", "biscuit-a3f9qz2m"); got != "zoomies-gpu-biscuit-a3f9qz2m" {
		t.Errorf("RunnerName from an invented pool name = %q", got)
	}
}

// What happens when a name will not fit is the whole design: the brand is what
// marks the registration as ours to reap, and the discriminator is what makes
// it unique in the target. Only the shape in the middle may be spent.
func TestRunnerNameSpendsTheShapeRatherThanTheBrandOrTheToken(t *testing.T) {
	long := strings.Repeat("verylongpoolname-", 5)
	got := RunnerName(long, "jellybean-a3f9qz2m")
	switch {
	case len(got) > MaxNameLength:
		t.Errorf("RunnerName = %q, %d characters, more than GitHub accepts", got, len(got))
	case !strings.HasPrefix(got, Prefix+"-"):
		t.Errorf("RunnerName = %q, which the reaper would not recognise as ours", got)
	case !strings.HasSuffix(got, "-jellybean-a3f9qz2m"):
		t.Errorf("RunnerName = %q, which has lost the discriminator that makes it unique", got)
	}
	// Segments go whole. A name cut mid-word claims a platform that does not
	// exist -- "ubuntu-24" is not a release anyone ships.
	spec := Spec{CPUs: 4, OS: OSUbuntu, Version: "24.04"}
	got = RunnerName(spec.String(), strings.Repeat("d", 30))
	for _, part := range strings.Split(got, "-") {
		if part == "ubuntu" || part == "2404" || part == "4vcpu" || len(part) == 30 || part == Prefix {
			continue
		}
		t.Errorf("RunnerName = %q, which contains %q -- a segment cut in half", got, part)
	}
}

func TestRunnerNameWithoutADiscriminator(t *testing.T) {
	// Nothing in Zoomies mints one of these, but a caller that passes an empty
	// discriminator should get a usable name rather than a trailing hyphen.
	if got := RunnerName("zoomies-gpu", ""); got != "zoomies-gpu" {
		t.Errorf("RunnerName with no discriminator = %q", got)
	}
}

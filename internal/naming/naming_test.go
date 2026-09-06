package naming

import (
	"strings"
	"testing"
)

func TestSpecString(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		want string
	}{
		{"nothing known", Spec{}, "zoomies"},
		{"the shape of a pool", Spec{CPUs: 4, OS: "ubuntu", Version: "24.04", Arch: "amd64"}, "zoomies-4vcpu-ubuntu-2404"},
		{"arm64 is spelled out", Spec{CPUs: 8, OS: "ubuntu", Version: "24.04", Arch: "arm64"}, "zoomies-8vcpu-ubuntu-2404-arm64"},
		{"memory when it is set", Spec{CPUs: 2, MemoryGB: 8, OS: "debian", Version: "12"}, "zoomies-2vcpu-8gb-debian-12"},
		{"a fraction of a core rounds up", Spec{CPUs: 1.5, OS: "ubuntu", Version: "22.04"}, "zoomies-2vcpu-ubuntu-2204"},
		{"a host carries its machine name", Spec{CPUs: 16, OS: "ubuntu", Version: "24.04", Suffix: "tuck"}, "zoomies-16vcpu-ubuntu-2404-tuck"},
		{"an unknown OS is left out", Spec{CPUs: 4, OS: "plan9", Version: "4"}, "zoomies-4vcpu"},
		{"a version without an OS says nothing", Spec{Version: "24.04"}, "zoomies"},
		{"cpus alone", Spec{CPUs: 4}, "zoomies-4vcpu"},
		{"macos", Spec{CPUs: 8, OS: "darwin", Version: "15", Arch: "arm64"}, "zoomies-8vcpu-macos-15-arm64"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.spec.String(); got != c.want {
				t.Errorf("String() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSpecStringStaysWithinGitHubsLimit(t *testing.T) {
	s := Spec{CPUs: 64, MemoryGB: 512, OS: "ubuntu", Version: "24.04", Arch: "arm64",
		Suffix: strings.Repeat("long", 40)}
	got := s.String()
	if len(got) > MaxNameLength {
		t.Fatalf("String() is %d characters, over the %d GitHub allows: %q", len(got), MaxNameLength, got)
	}
	if strings.HasSuffix(got, "-") {
		t.Errorf("String() = %q, which ends in a dash a container runtime would reject", got)
	}
}

func TestParseRoundTrip(t *testing.T) {
	specs := []Spec{
		{CPUs: 4, OS: OSUbuntu, Version: "24.04", Arch: ArchAMD64},
		{CPUs: 8, OS: OSUbuntu, Version: "22.04", Arch: ArchARM64},
		{CPUs: 2, MemoryGB: 8, OS: OSDebian, Version: "12", Arch: ArchAMD64},
		{CPUs: 16, OS: OSFedora, Version: "42", Arch: ArchAMD64, Suffix: "build01"},
		{CPUs: 1, OS: OSRocky, Version: "9", Arch: ArchARM64, Suffix: "k3f9qz"},
	}
	for _, want := range specs {
		name := want.String()
		got, ok := Parse(name)
		if !ok {
			t.Fatalf("Parse(%q) did not recognise a name this package built", name)
		}
		if got != want {
			t.Errorf("Parse(%q) = %+v, want %+v", name, got, want)
		}
	}
}

func TestParseRejectsForeignNames(t *testing.T) {
	for _, name := range []string{"", "linux-x64", "blacksmith-4vcpu-ubuntu-2404", "zoomiesx-4vcpu"} {
		if _, ok := Parse(name); ok {
			t.Errorf("Parse(%q) claimed a name that is not in the grammar", name)
		}
	}
}

func TestParseIsBestEffort(t *testing.T) {
	// A name that only half fits still yields the parts that do, because the
	// caller is enriching a display rather than validating input.
	got, ok := Parse("zoomies-gpu-builders")
	if !ok {
		t.Fatalf("Parse rejected a branded name with a free-form body")
	}
	if got.CPUs != 0 || got.OS != "" {
		t.Errorf("Parse invented facets: %+v", got)
	}
	if got.Suffix != "gpu-builders" {
		t.Errorf("Suffix = %q, want the whole body", got.Suffix)
	}
}

func TestNormalizeOS(t *testing.T) {
	cases := map[string]string{
		"Ubuntu": OSUbuntu, "darwin": OSMacOS, "macOS": OSMacOS,
		"centos": OSRocky, "raspbian": OSDebian, "windows": OSWindows,
		// "linux" names a kernel, not a distribution: guessing one would put a
		// fact into a name that nobody established.
		"linux": "", "plan9": "", "": "",
	}
	for in, want := range cases {
		if got := NormalizeOS(in); got != want {
			t.Errorf("NormalizeOS(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeArch(t *testing.T) {
	cases := map[string]string{
		"amd64": ArchAMD64, "x64": ArchAMD64, "x86_64": ArchAMD64,
		"arm64": ArchARM64, "aarch64": ArchARM64, "arm": ArchARM64,
		"riscv64": "", "": "",
	}
	for in, want := range cases {
		if got := NormalizeArch(in); got != want {
			t.Errorf("NormalizeArch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVersionCompaction(t *testing.T) {
	cases := []struct{ in, compact, os, back string }{
		{"24.04", "2404", OSUbuntu, "24.04"},
		{"22.04", "2204", OSUbuntu, "22.04"},
		{"12", "12", OSDebian, "12"},
		{"9.4", "94", OSRocky, "94"},
		{"3.22.1", "322", OSAlpine, "322"},
		{"", "", OSUbuntu, ""},
	}
	for _, c := range cases {
		if got := CompactVersion(c.in); got != c.compact {
			t.Errorf("CompactVersion(%q) = %q, want %q", c.in, got, c.compact)
		}
		if got := ExpandVersion(c.os, c.compact); got != c.back {
			t.Errorf("ExpandVersion(%q, %q) = %q, want %q", c.os, c.compact, got, c.back)
		}
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Build Box 01":  "build-box-01",
		"  spaced  ":    "spaced",
		"a//b__c":       "a-b-c",
		"---":           "",
		"ip-10-0-4-17":  "ip-10-0-4-17",
		"CAPS_and.dots": "caps-and-dots",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPoolAndHostNames(t *testing.T) {
	spec := Spec{CPUs: 16, OS: OSUbuntu, Version: "24.04", Arch: ArchAMD64}
	if got, want := PoolName(spec), "zoomies-16vcpu-ubuntu-2404"; got != want {
		t.Errorf("PoolName = %q, want %q", got, want)
	}
	// A pool's name is its shape, so a stray suffix must not survive into one.
	if got := PoolName(spec.WithSuffix("build01")); got != "zoomies-16vcpu-ubuntu-2404" {
		t.Errorf("PoolName kept a suffix: %q", got)
	}
	if got, want := HostName(spec, "build01.fleet.example.com"), "zoomies-16vcpu-ubuntu-2404-build01"; got != want {
		t.Errorf("HostName = %q, want %q", got, want)
	}
}

func TestRunnerName(t *testing.T) {
	cases := []struct{ pool, token, want string }{
		{"zoomies-4vcpu-ubuntu-2404", "k3f9qz", "zoomies-4vcpu-ubuntu-2404-k3f9qz"},
		{"gpu builders", "k3f9qz", "zoomies-gpu-builders-k3f9qz"},
		{"", "k3f9qz", "zoomies-k3f9qz"},
		{"zoomies", "k3f9qz", "zoomies-k3f9qz"},
	}
	for _, c := range cases {
		if got := RunnerName(c.pool, c.token); got != c.want {
			t.Errorf("RunnerName(%q, %q) = %q, want %q", c.pool, c.token, got, c.want)
		}
	}
}

func TestRunnerNameKeepsItsUniquenessSuffix(t *testing.T) {
	long := strings.Repeat("pool", 30)
	got := RunnerName(long, "k3f9qz")
	if len(got) > MaxNameLength {
		t.Fatalf("RunnerName is %d characters, over GitHub's %d: %q", len(got), MaxNameLength, got)
	}
	if !strings.HasSuffix(got, "-k3f9qz") {
		t.Errorf("RunnerName truncated the token instead of the pool name: %q", got)
	}
}

func TestLabels(t *testing.T) {
	got := Labels(Spec{CPUs: 8, OS: OSUbuntu, Version: "24.04", Arch: ArchARM64})
	want := []string{"zoomies-8vcpu-ubuntu-2404-arm64", "linux", "arm64"}
	if len(got) != len(want) {
		t.Fatalf("Labels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Labels = %v, want %v", got, want)
		}
	}
	if got := Labels(Spec{}); len(got) != 0 {
		t.Errorf("Labels of an empty spec = %v, want none", got)
	}
}

func TestDescribe(t *testing.T) {
	s := Spec{CPUs: 4, MemoryGB: 16, OS: OSUbuntu, Version: "24.04", Arch: ArchARM64}
	if got, want := s.Describe(), "4 vCPU, 16 GB, Ubuntu 24.04, arm64"; got != want {
		t.Errorf("Describe = %q, want %q", got, want)
	}
	if got := (Spec{}).Describe(); got != "" {
		t.Errorf("Describe of an empty spec = %q, want empty", got)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("zoomies-4vcpu-ubuntu-2404"); err != nil {
		t.Errorf("Validate rejected a canonical name: %v", err)
	}
	if err := Validate("my-own-pool"); err != nil {
		t.Errorf("Validate rejected a name outside the grammar, which stays legal: %v", err)
	}
	for _, bad := range []string{"", "Has Spaces", strings.Repeat("a", MaxNameLength+1)} {
		if err := Validate(bad); err == nil {
			t.Errorf("Validate(%q) accepted a name no runtime would take", bad)
		}
	}
}

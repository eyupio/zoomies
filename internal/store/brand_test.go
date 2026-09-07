package store

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/naming"
)

func TestNewRunnerNameCarriesTheShapeOfItsPool(t *testing.T) {
	pool := &Pool{
		Name:      "zoomies-biscuit-docker-linux",
		Resources: Resources{CPUs: 4, MemoryMB: 8 * 1024},
		Platform:  Platform{OS: "ubuntu", OSVersion: "24.04", Arch: "amd64"},
	}
	// The point of the shape: somebody reading GitHub's runner list learns how
	// much machine the job got and what it ran on, without leaving the page.
	const want = "zoomies-4vcpu-8gb-ubuntu-2404-"
	name := NewRunnerName(pool)
	if !strings.HasPrefix(name, want) {
		t.Fatalf("NewRunnerName = %q, want it to start with %q", name, want)
	}
	// And the shape is the pool's, not the pool's invented name: the operator
	// chose "biscuit" for a thing the reader is not looking at.
	if strings.Contains(name, "biscuit") {
		t.Errorf("NewRunnerName = %q, want the pool's shape rather than its name", name)
	}
}

func TestNewRunnerNameFallsBackToThePoolName(t *testing.T) {
	// A pool recorded before resources and platform were: there is no shape to
	// report, and the name the operator chose says more than nothing does.
	pool := &Pool{Name: "zoomies-biscuit-docker-linux"}
	if name := NewRunnerName(pool); !strings.HasPrefix(name, "zoomies-biscuit-docker-linux-") {
		t.Errorf("NewRunnerName = %q, want it to fall back to the pool's own name", name)
	}
	// And a runner being named before it has a pool is still ours to reap.
	if name := NewRunnerName(nil); !IsRunnerName(name) {
		t.Errorf("NewRunnerName(nil) = %q, want a name the reaper recognises", name)
	}
}

func TestNewRunnerNameIsBrandedUniqueAndShortEnoughForGitHub(t *testing.T) {
	pools := []*Pool{
		nil,
		{Name: "zoomies-4vcpu-ubuntu-2404", Resources: Resources{CPUs: 4},
			Platform: Platform{OS: "ubuntu", OSVersion: "24.04"}},
		// The worst case a real fleet can produce: every dimension named, on a
		// pool whose own name is as long as the store will take.
		{Name: strings.Repeat("long-", 12), Resources: Resources{CPUs: 192, MemoryMB: 768 * 1024},
			Platform: Platform{OS: "ubuntu", OSVersion: "24.04", Arch: "arm64"}},
		{Name: strings.Repeat("long-", 12)},
	}
	seen := map[string]bool{}
	for _, pool := range pools {
		for range 200 {
			name := NewRunnerName(pool)
			if !strings.HasPrefix(name, "zoomies-") {
				t.Fatalf("runner name %q is not branded", name)
			}
			// GitHub refuses a longer one, and it refuses it at registration --
			// several seconds into provisioning a runner somebody is waiting for.
			if len(name) > 64 {
				t.Fatalf("runner name %q is %d characters, more than GitHub accepts", name, len(name))
			}
			// The token is the last two segments' worth of the name and is what
			// makes it unique; truncation that ate it would produce collisions
			// only under load, which is the worst time to find out.
			if seen[name] {
				t.Fatalf("runner name %q was minted twice in 200 tries", name)
			}
			seen[name] = true
			if !IsRunnerName(name) {
				t.Fatalf("IsRunnerName(%q) = false, want true for a name we just minted", name)
			}
		}
	}
}

func TestNewRunnerNameReadsBackAsItsPoolsSpec(t *testing.T) {
	// A runner's name is in the grammar, so anything holding one can recover
	// what it was made of -- which is what makes the name worth its characters.
	pool := &Pool{
		Name:      "zoomies-8vcpu-debian-12-arm64",
		Resources: Resources{CPUs: 8},
		Platform:  Platform{OS: "debian", OSVersion: "12", Arch: "arm64"},
	}
	spec, ok := naming.Parse(NewRunnerName(pool))
	if !ok {
		t.Fatal("a runner name does not parse back into the grammar that built it")
	}
	if spec.CPUs != 8 || spec.OS != "debian" || spec.Version != "12" || spec.Arch != "arm64" {
		t.Errorf("parsed %+v, want the pool's 8 vCPU Debian 12 arm64 shape", spec)
	}
	if spec.Suffix == "" {
		t.Error("parsed no discriminator, so two runners of this pool would share a name")
	}
}

func TestIsRunnerNameRejectsSomebodyElsesRunner(t *testing.T) {
	for _, name := range []string{"", "runner-01", "gh-zoomies-1", "zoomies", "azoomies-x"} {
		if IsRunnerName(name) {
			t.Errorf("IsRunnerName(%q) = true, want false: the reaper would delete a registration it does not own", name)
		}
	}
	// GitHub does not promise the case it echoes back, and deleting the wrong
	// runner is worse than being generous here.
	if !IsRunnerName("Zoomies-A3F9QZ2M") {
		t.Error("IsRunnerName does not recognise its own name in a different case")
	}
}

func TestBrandLabelsAlwaysCarryTheBrand(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, []string{"zoomies"}},
		{"adds the brand", []string{"gpu"}, []string{"gpu", "zoomies"}},
		{"does not duplicate it", []string{"Zoomies", "gpu"}, []string{"gpu", "zoomies"}},
		{"normalises the rest", []string{" GPU ", "gpu"}, []string{"gpu", "zoomies"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := BrandLabels(c.in)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("BrandLabels(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestBrandedLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"linux-x64", "zoomies-linux-x64"},
		{"GPU", "zoomies-gpu"},
		{"Ubuntu 24.04", "zoomies-ubuntu-24-04"},
		{"zoomies-gpu", "zoomies-gpu"},
		{"zoomies", "zoomies"},
		{"", "zoomies"},
		{"---", "zoomies"},
		{strings.Repeat("a", 60), "zoomies-" + strings.Repeat("a", 40)},
	}
	for _, c := range cases {
		if got := BrandedLabel(c.in); got != c.want {
			t.Errorf("BrandedLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeLabelCollapsesRunsOfPunctuation(t *testing.T) {
	if got := SanitizeLabel("big  builders!!"); got != "big-builders" {
		t.Fatalf("SanitizeLabel = %q, want %q", got, "big-builders")
	}
}

// A pool name is what a workflow's runs-on ends up spelling and what GitHub
// shows next to runners somebody else may also have registered, so a name
// arriving without the brand has to come out of here with it.
func TestBrandedNameAlwaysCarriesTheBrand(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"adds the brand", "gpu", "zoomies-gpu"},
		{"keeps one it already has", "zoomies-gpu", "zoomies-gpu"},
		{"does not duplicate it in another case", "Zoomies-GPU", "Zoomies-GPU"},
		{"the brand alone is branded enough", "zoomies", "zoomies"},
		{"trims what an operator pasted", "  gpu  ", "zoomies-gpu"},
		{"does not double the hyphen", "-gpu", "zoomies-gpu"},
		{"a name that only looks branded is not", "zoomiesgpu", "zoomies-zoomiesgpu"},
		// "a pool needs a name" is a better thing to be told than to be given
		// a pool called "zoomies-".
		{"leaves an empty name empty", "   ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := BrandedName(c.in); got != c.want {
				t.Fatalf("BrandedName(%q) = %q, want %q", c.in, got, c.want)
			}
			if c.want != "" && !IsBrandedName(c.want) {
				t.Fatalf("IsBrandedName(%q) = false for a name BrandedName just produced", c.want)
			}
		})
	}
}

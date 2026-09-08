package scheduler

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

func TestMatches(t *testing.T) {
	tests := []struct {
		name string
		pool []string
		job  []string
		want bool
	}{
		{"exact match", []string{"gpu"}, []string{"gpu"}, true},
		{"pool is a superset", []string{"gpu", "cuda12"}, []string{"gpu"}, true},
		{"job label missing from pool", []string{"gpu"}, []string{"gpu", "cuda12"}, false},
		{"job asks only for implicit labels", []string{"gpu"},
			[]string{"self-hosted", "linux", "x64"}, true},
		{"implicit-only job and label-less pool", nil, []string{"self-hosted", "linux", "x64"}, true},
		{"label-less pool cannot serve a real label", nil, []string{"gpu"}, false},
		{"empty job labels match anything", []string{"gpu"}, nil, true},
		{"pool declares the os the job asked for",
			[]string{"linux", "gpu"}, []string{"self-hosted", "linux", "gpu"}, true},
		{"os contradiction",
			[]string{"windows", "gpu"}, []string{"self-hosted", "linux", "gpu"}, false},
		{"arch contradiction",
			[]string{"linux", "arm64", "gpu"}, []string{"self-hosted", "linux", "x64", "gpu"}, false},
		{"pool declares no arch, so any arch matches",
			[]string{"linux", "gpu"}, []string{"self-hosted", "arm64", "gpu"}, true},
		{"pool declares an arch the job did not ask for",
			[]string{"linux", "arm64", "gpu"}, []string{"self-hosted", "linux", "gpu"}, true},
		{"case and whitespace are normalised",
			[]string{" GPU ", "Cuda12"}, []string{"gpu", " cuda12"}, true},
		{"self-hosted alone never contradicts",
			[]string{"linux", "x64"}, []string{"self-hosted"}, true},
		{"duplicate labels do not change the answer",
			[]string{"gpu", "gpu"}, []string{"gpu", "GPU"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Matches(tc.pool, tc.job); got != tc.want {
				t.Fatalf("Matches(%v, %v) = %v, want %v", tc.pool, tc.job, got, tc.want)
			}
			score := Score(tc.pool, tc.job)
			if (score >= 0) != tc.want {
				t.Fatalf("Score(%v, %v) = %d, which disagrees with Matches", tc.pool, tc.job, score)
			}
		})
	}
}

func TestScorePrefersTheLeastSurplus(t *testing.T) {
	job := []string{"self-hosted", "linux", "gpu"}
	exact := Score([]string{"gpu"}, job)
	oneSurplus := Score([]string{"gpu", "cuda12"}, job)
	twoSurplus := Score([]string{"gpu", "cuda12", "bigmem"}, job)

	if !(exact > oneSurplus && oneSurplus > twoSurplus) {
		t.Fatalf("scores are not ordered by surplus: exact=%d one=%d two=%d",
			exact, oneSurplus, twoSurplus)
	}
	if twoSurplus < 0 {
		t.Fatalf("a matching pool scored negative: %d", twoSurplus)
	}
	// A label the job did ask for is not surplus, even an implicit one.
	if got := Score([]string{"linux", "gpu"}, job); got != exact {
		t.Fatalf("Score with a requested implicit label = %d, want %d", got, exact)
	}
	if got := Score([]string{"windows"}, job); got >= 0 {
		t.Fatalf("Score for a contradicting pool = %d, want negative", got)
	}
}

// job is a queued job on the one installation the label tests use, so that a
// case exercises the label rule rather than the installation rule.
func job(labels ...string) *store.Job {
	return &store.Job{InstallationID: "ins_one", Repo: "acme/widgets",
		State: store.JobQueued, Labels: store.StringSlice(labels)}
}

func TestBestPool(t *testing.T) {
	general := &store.Pool{ID: "p1", Name: "general", InstallationID: "ins_one", Labels: store.StringSlice{"linux", "x64"}, Enabled: true}
	gpu := &store.Pool{ID: "p2", Name: "gpu", InstallationID: "ins_one", Labels: store.StringSlice{"linux", "x64", "gpu"}, Enabled: true}
	bigGPU := &store.Pool{ID: "p3", Name: "gpu-big", InstallationID: "ins_one", Labels: store.StringSlice{"linux", "x64", "gpu", "bigmem"}, Enabled: true}
	disabled := &store.Pool{ID: "p4", Name: "arm", InstallationID: "ins_one", Labels: store.StringSlice{"gpu"}, Enabled: false}
	pools := []*store.Pool{bigGPU, disabled, gpu, general, nil}

	tests := []struct {
		name string
		job  []string
		want string
	}{
		{"implicit-only job takes the least specific pool", []string{"self-hosted", "linux", "x64"}, "general"},
		{"gpu job takes the smallest pool that has a gpu", []string{"self-hosted", "gpu"}, "gpu"},
		{"bigmem job needs the big pool", []string{"gpu", "bigmem"}, "gpu-big"},
		{"nothing claims an unknown label", []string{"windows-2022"}, ""},
		{"a disabled pool is never chosen", []string{"gpu", "arm64"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BestPool(pools, job(tc.job...))
			switch {
			case got == nil && tc.want != "":
				t.Fatalf("BestPool(%v) = nil, want %s", tc.job, tc.want)
			case got != nil && got.Name != tc.want:
				t.Fatalf("BestPool(%v) = %s, want %q", tc.job, got.Name, tc.want)
			}
		})
	}
}

func TestBestPoolTieBreaksOnName(t *testing.T) {
	// Two pools that fit the job equally well: the name decides, whatever order
	// the caller happened to pass them in.
	beta := &store.Pool{ID: "p-beta", Name: "beta", InstallationID: "ins_one", Labels: store.StringSlice{"gpu"}, Enabled: true}
	alpha := &store.Pool{ID: "p-alpha", Name: "alpha", InstallationID: "ins_one", Labels: store.StringSlice{"gpu"}, Enabled: true}

	for _, pools := range [][]*store.Pool{{beta, alpha}, {alpha, beta}} {
		got := BestPool(pools, job("gpu"))
		if got == nil || got.Name != "alpha" {
			t.Fatalf("BestPool = %v, want alpha", got)
		}
	}

	// Same name (only possible mid-rename) falls back to the ID.
	a := &store.Pool{ID: "p-a", Name: "same", InstallationID: "ins_one", Labels: store.StringSlice{"gpu"}, Enabled: true}
	b := &store.Pool{ID: "p-b", Name: "same", InstallationID: "ins_one", Labels: store.StringSlice{"gpu"}, Enabled: true}
	if got := BestPool([]*store.Pool{b, a}, job("gpu")); got != a {
		t.Fatalf("BestPool = %v, want the lower ID", got)
	}
}

func TestEligibleAsksTheThreeQuestionsInOrder(t *testing.T) {
	pool := func(f func(*store.Pool)) *store.Pool {
		p := &store.Pool{ID: "p1", Name: "linux-x64", InstallationID: "ins_one",
			Labels: store.StringSlice{"linux", "x64"}, Enabled: true}
		f(p)
		return p
	}
	tests := []struct {
		name   string
		pool   *store.Pool
		job    *store.Job
		want   bool
		reason string
	}{
		{
			name: "a pool on the job's own installation whose labels fit takes it",
			pool: pool(func(*store.Pool) {}),
			job:  job("linux", "x64"),
			want: true,
		},
		{
			name:   "a disabled pool is refused before anything else is asked",
			pool:   pool(func(p *store.Pool) { p.Enabled = false; p.InstallationID = "ins_other" }),
			job:    job("windows"),
			reason: "the pool is disabled",
		},
		{
			name:   "a job no installation covers is refused before its labels are read",
			pool:   pool(func(*store.Pool) {}),
			job:    &store.Job{Repo: "nobody/here", Labels: store.StringSlice{"linux", "x64"}},
			reason: "no GitHub App installation here covers that repository",
		},
		{
			name:   "a pool on another installation is refused however well its labels fit",
			pool:   pool(func(p *store.Pool) { p.InstallationID = "ins_two" }),
			job:    job("linux", "x64"),
			reason: "the pool belongs to another GitHub App installation",
		},
		{
			name:   "and only then are the labels the answer",
			pool:   pool(func(*store.Pool) {}),
			job:    job("windows"),
			reason: "the pool does not advertise those labels",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := Eligible(tc.pool, tc.job)
			if ok != tc.want {
				t.Fatalf("Eligible = %v, want %v (%q)", ok, tc.want, reason)
			}
			if reason != tc.reason {
				t.Fatalf("reason = %q, want %q", reason, tc.reason)
			}
		})
	}
}

// A job that goes unclaimed because the pool advertising its labels is on
// another installation looks, from the Jobs page, exactly like a mislabelled
// workflow. The two need different fixes, so the plan says which it is.
func TestAnUnclaimedJobSaysWhenThePoolIsOnAnotherInstallation(t *testing.T) {
	theirs := &store.Pool{ID: "p_theirs", Name: "linux-x64", InstallationID: "ins_two",
		Labels: store.StringSlice{"linux", "x64"}, Enabled: true}
	pools := []*store.Pool{theirs}
	targets := map[string]string{"ins_two": "globex"}

	_, reason := bestPool(pools, job("linux", "x64"), targets)
	if want := "its labels match pool linux-x64, which belongs to installation globex"; reason != want {
		t.Fatalf("reason = %q, want %q", reason, want)
	}

	// Without the targets the identifier is all there is to name it by, which
	// is still better than saying nothing.
	if _, reason := bestPool(pools, job("linux", "x64"), nil); !strings.Contains(reason, "ins_two") {
		t.Fatalf("reason = %q, want it to fall back to the identifier", reason)
	}

	// A repository no installation covers is a different sentence: adding a
	// pool would not help, installing the App on that target would.
	orphan := &store.Job{Repo: "nobody/here", State: store.JobQueued, Labels: store.StringSlice{"linux", "x64"}}
	if _, reason := bestPool(pools, orphan, targets); !strings.Contains(reason, "no GitHub App installation here covers nobody/here") {
		t.Fatalf("reason = %q, want it to name the uncovered repository", reason)
	}

	// Labels that match nothing get no sentence at all: the page already says
	// no pool claims the job, and a reason repeating that is noise.
	if _, reason := bestPool(pools, job("windows"), targets); reason != "" {
		t.Fatalf("reason = %q, want none for a plain label mismatch", reason)
	}
}

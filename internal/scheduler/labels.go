package scheduler

import (
	"fmt"
	"slices"

	"github.com/eyupio/zoomies/internal/store"
)

// osLabels and archLabels split store.ImplicitLabels into the two dimensions a
// pool can genuinely contradict. "self-hosted" belongs to neither: every pool
// in Zoomies is self-hosted, so a job asking for it constrains nothing.
var (
	osLabels   = map[string]bool{"linux": true, "windows": true, "macos": true}
	archLabels = map[string]bool{"x64": true, "arm": true, "arm64": true}
)

const (
	// noMatch is the score of a pool that cannot run a job at all. It is
	// negative so that callers can sort scores and reject anything below zero.
	noMatch = -1
	// exactScore is what a pool scores when it advertises precisely the labels
	// the job asked for. Surplus labels count down from here, which keeps every
	// matching score non-negative for any sane label set.
	exactScore = 1 << 16
)

// Matches reports whether a pool advertising poolLabels may run a job whose
// runs-on listed jobLabels.
//
// The rule mirrors what GitHub itself does. Labels every actions/runner binary
// advertises (store.ImplicitLabels) do not constrain pool selection, because
// "runs-on: [self-hosted, linux, x64]" is how nearly every workflow is written
// and it is not a request for a particular pool. The one exception is a pool
// that declares its own os or arch: that is a promise about the machine, so a
// job asking for a different one must not land there.
func Matches(poolLabels, jobLabels []string) bool {
	return matches(store.NormalizeLabels(poolLabels), store.NormalizeLabels(jobLabels))
}

// matches is Matches over already-normalised label sets.
func matches(pool, job []string) bool {
	for _, l := range job {
		if store.ImplicitLabels[l] {
			continue
		}
		if !slices.Contains(pool, l) {
			return false
		}
	}
	return !contradicts(pool, job, osLabels) && !contradicts(pool, job, archLabels)
}

// contradicts reports whether the job asked for a label from dim that the pool
// lacks while declaring a different label from the same dimension. A pool that
// declares nothing in a dimension makes no promise and contradicts nothing.
func contradicts(pool, job []string, dim map[string]bool) bool {
	declares := slices.ContainsFunc(pool, func(l string) bool { return dim[l] })
	if !declares {
		return false
	}
	return slices.ContainsFunc(job, func(l string) bool {
		return dim[l] && !slices.Contains(pool, l)
	})
}

// Score ranks how well a pool fits a job. Higher is better; a negative score
// means the pool cannot run the job at all.
//
// Every matching pool satisfies all of the job's explicit labels by
// construction, so the only thing left to rank on is surplus: capability the
// job never asked for. Preferring the smallest surplus keeps a specialised
// pool (say gpu + cuda12) free for the jobs that actually need it.
func Score(poolLabels, jobLabels []string) int {
	pool := store.NormalizeLabels(poolLabels)
	job := store.NormalizeLabels(jobLabels)
	if !matches(pool, job) {
		return noMatch
	}
	surplus := 0
	for _, l := range pool {
		if !slices.Contains(job, l) {
			surplus++
		}
	}
	return max(exactScore-surplus, 0)
}

// Eligible reports whether a pool may run a job at all, and when it may not,
// says why in the operator's words.
//
// The three tests are asked in the order an operator would ask them. A
// disabled pool was disabled on purpose. A pool belonging to another GitHub
// App installation cannot run the job whatever its labels say: Zoomies would
// mint the runner's registration in the wrong GitHub target, and the job would
// sit queued while a runner waited for it somewhere it can never be offered.
// Labels come last, because they are the only one of the three an operator
// changes by editing a workflow.
func Eligible(p *store.Pool, j *store.Job) (bool, string) {
	switch {
	case p == nil || j == nil:
		return false, ""
	case !p.Enabled:
		return false, "the pool is disabled"
	case j.InstallationID == "":
		return false, "no GitHub App installation here covers that repository"
	case p.InstallationID != j.InstallationID:
		return false, "the pool belongs to another GitHub App installation"
	case !Matches(p.Labels, j.Labels):
		return false, "the pool does not advertise those labels"
	}
	return true, ""
}

// BestPool returns the pool that best fits a job, or nil when no pool is
// eligible for it -- which the caller surfaces as a configuration problem
// rather than silently dropping the job.
//
// Ties break on pool name and then pool ID, so the same job always lands in
// the same pool. Anything else would make the controller disagree with itself
// across restarts, and make a scaling decision impossible to explain.
func BestPool(pools []*store.Pool, j *store.Job) *store.Pool {
	best, _ := bestPool(pools, j, nil)
	return best
}

// bestPool is BestPool that also explains an empty answer, given the target
// each installation manages so that the explanation can name one.
//
// The explanation names the nearest miss rather than every pool's objection.
// A job whose labels match a pool in another installation is a different
// problem from one whose labels match nothing here -- the first is a pool on
// the wrong installation, the second is a workflow asking for a machine this
// fleet does not offer -- and only the first is worth a sentence, because the
// second is what the Jobs page already says.
func bestPool(pools []*store.Pool, j *store.Job, targets map[string]string) (*store.Pool, string) {
	var best *store.Pool
	bestScore := noMatch
	var elsewhere *store.Pool
	for _, p := range pools {
		if p == nil {
			continue
		}
		if ok, _ := Eligible(p, j); !ok {
			// A pool that would take the job but for its installation is the
			// near miss worth reporting. Ties break the same way matches do,
			// so the sentence does not change between passes.
			if p.Enabled && Matches(p.Labels, j.Labels) &&
				(elsewhere == nil || lessPool(p, elsewhere)) {
				elsewhere = p
			}
			continue
		}
		s := Score(p.Labels, j.Labels)
		if s < 0 {
			continue
		}
		if best == nil || s > bestScore || (s == bestScore && lessPool(p, best)) {
			best, bestScore = p, s
		}
	}
	if best != nil || elsewhere == nil {
		return best, ""
	}
	if j.InstallationID == "" {
		return nil, fmt.Sprintf("its labels match pool %s, but no GitHub App installation here covers %s", elsewhere.Name, j.Repo)
	}
	return nil, fmt.Sprintf("its labels match pool %s, which belongs to installation %s",
		elsewhere.Name, installationName(elsewhere.InstallationID, targets))
}

// installationName prefers the target an installation manages -- the
// organisation or repository an operator recognises -- and falls back to the
// identifier when the caller did not supply the targets.
func installationName(id string, targets map[string]string) string {
	if t := targets[id]; t != "" {
		return t
	}
	return id
}

// lessPool is the total order used wherever two pools would otherwise tie.
func lessPool(a, b *store.Pool) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

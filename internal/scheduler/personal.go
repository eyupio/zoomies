package scheduler

import (
	"fmt"
	"strings"

	"github.com/eyupio/zoomies/internal/store"
)

// Pools on a personal account.
//
// GitHub keeps self-hosted runners on an organisation or on a repository and
// nowhere else. An installation that covers a personal account therefore
// registers each runner on one repository, and that runner takes that
// repository's jobs and no others. Everywhere else a pool's runners are
// interchangeable, which is what lets a pool be decided from its totals: so
// many jobs waiting, so many runners free. Here the totals lie. One idle
// runner and one waiting job are a pool with nothing to do only if they are in
// the same repository; in different ones the job waits for ever beside a
// runner that will never take it.
//
// So such a pool is decided a repository at a time: each waiting job is set
// against the runners free in its own repository, each runner to be created is
// given the repository of the job it is for, and a free runner no waiting job
// can reach is stopped, at once if it never finished starting and after the
// idle timeout otherwise, or sooner when it is holding the pool at its maximum
// against a job somewhere else.

// poolsByRepository marks the pools whose installation is a personal account,
// and returns the pools with the warm minimum taken off those.
//
// A warm runner is one kept idle for a job that has not arrived. Where a
// runner must be registered on the repository of its job, there is no
// repository to register a warm one on until the job exists, so the minimum
// cannot be honoured, and one registered on a guess would hold a slot no job
// could use. The pool is copied rather than changed: the caller's rows are the
// caller's.
func poolsByRepository(pools []*store.Pool, insts []*store.Installation) ([]*store.Pool, map[string]bool) {
	var personal map[string]bool
	for _, i := range insts {
		if i.Personal() {
			if personal == nil {
				personal = map[string]bool{}
			}
			personal[i.ID] = true
		}
	}
	if personal == nil {
		return pools, nil
	}
	marked := map[string]bool{}
	for n, p := range pools {
		if !personal[p.InstallationID] {
			continue
		}
		marked[p.ID] = true
		if p.MinRunners != 0 {
			q := *p
			q.MinRunners = 0
			pools[n] = &q
		}
	}
	return pools, marked
}

// decideByRepository sets a personal account's pool against its queue a
// repository at a time. It fills in what the pool wants and which repository
// each new runner is for, stops the free runners nothing waiting can reach,
// and returns the runners it left alone with how many it stopped.
func (t *tick) decideByRepository(p *store.Pool, plan *PoolPlan, remaining []*store.Runner, queued, eligible []*store.Job, live int) ([]*store.Runner, int) {
	// A free runner is one that could still take a job: live, not on one and
	// not on its way out.
	free := map[string][]*store.Runner{}
	for _, r := range remaining {
		if !r.State.Live() || r.State == store.RunnerBusy || r.State == store.RunnerDraining || t.stillRunning[r.ID] {
			continue
		}
		key := strings.ToLower(r.Repo)
		free[key] = append(free[key], r)
	}
	claim := func(j *store.Job) bool {
		key := strings.ToLower(j.Repo)
		if key == "" || len(free[key]) == 0 {
			return false
		}
		free[key] = free[key][1:]
		return true
	}

	// The jobs ready for a runner claim first, so that a free runner is never
	// counted against a job still inside its delay while a ready one in the
	// same repository goes without.
	covered := 0
	claimed := map[*store.Job]bool{}
	for _, j := range eligible {
		claimed[j] = true
		if claim(j) {
			covered++
			continue
		}
		plan.repositories = append(plan.repositories, j.Repo)
	}
	for _, j := range queued {
		if !claimed[j] {
			claim(j)
		}
	}
	plan.supply = covered

	// What is left free is in a repository with nothing waiting.
	timeout := p.IdleTimeout.Duration()
	short := live + len(plan.repositories) - p.MaxRunners
	stopped := map[string]bool{}
	for _, r := range remaining {
		key := strings.ToLower(r.Repo)
		if len(free[key]) == 0 || free[key][0] != r {
			continue
		}
		free[key] = free[key][1:]
		var reason string
		switch {
		case r.Repo == "":
			reason = "registered on no repository, and a personal account's runner takes only the jobs of the repository it is registered on"
		case starting(r.State):
			reason = fmt.Sprintf("no job is waiting in %s any more, and this runner had not finished starting", r.Repo)
		case r.IdleFor(t.now) >= timeout:
			reason = fmt.Sprintf("no job is waiting in %s, the only repository this runner can take jobs from", r.Repo)
			if timeout > 0 {
				reason += fmt.Sprintf("; idle for %s, over the %s idle timeout", formatDuration(r.IdleFor(t.now)), formatDuration(timeout))
			}
		case short > 0:
			reason = fmt.Sprintf("no job is waiting in %s, and the pool is at its maximum of %s with a job waiting in another repository",
				r.Repo, plural(p.MaxRunners, "runner"))
		default:
			continue
		}
		short--
		stopped[r.ID] = true
		plan.Actions = append(plan.Actions, t.action(ActionDrain, p, r, reason))
	}

	// Every runner still holds its slot until it is gone, the ones stopped
	// just now included, so the pool wants what it has and one more for each
	// job nothing free can reach.
	plan.Desired = clamp(live+len(plan.repositories), 0, p.MaxRunners)
	if len(stopped) == 0 {
		return remaining, 0
	}
	plan.Reason = scaled(p.Name, live, live-len(stopped),
		plural(len(stopped), "runner")+" registered on a repository with no job waiting")
	kept := make([]*store.Runner, 0, len(remaining)-len(stopped))
	for _, r := range remaining {
		if !stopped[r.ID] {
			kept = append(kept, r)
		}
	}
	return kept, len(stopped)
}

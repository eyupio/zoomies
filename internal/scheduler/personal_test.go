package scheduler

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// personalSnap is snap with its installation a personal account, where GitHub
// registers every runner on one repository.
func personalSnap(pools []*store.Pool, runners []*store.Runner, jobs []*store.Job) Snapshot {
	s := snap(pools, runners, jobs, []*store.Host{testHost("host_a", 16, 0)})
	s.Installations = []*store.Installation{{
		ID: testInstallation, Target: "acme", TargetType: store.TargetOrg, AccountType: store.AccountUser,
	}}
	return s
}

func queuedIn(repo, id string, waited time.Duration) *store.Job {
	j := queued(id, waited, "linux")
	j.Repo = repo
	return j
}

func registeredOn(repo string, r *store.Runner) *store.Runner {
	r.Repo = repo
	return r
}

func createRepos(as []Action) []string {
	var out []string
	for _, a := range actionsOf(as, ActionCreate) {
		out = append(out, a.Repo)
	}
	return out
}

func TestAPersonalPoolRegistersEachRunnerOnTheRepositoryOfItsJob(t *testing.T) {
	// A runner on a personal account takes only the jobs of the repository it
	// is registered on. A create that did not say which would leave the
	// controller to guess, and a wrong guess is a runner that starts, idles
	// and never takes the job it was made for.
	p := testPool("linux", "linux")
	pp := only(t, Decide(personalSnap([]*store.Pool{p}, nil, []*store.Job{
		queuedIn("acme/widgets", "j1", 3*time.Minute),
		queuedIn("acme/gadgets", "j2", 2*time.Minute),
		queuedIn("acme/widgets", "j3", time.Minute),
	})))

	if got, want := createRepos(pp.Actions), []string{"acme/widgets", "acme/gadgets", "acme/widgets"}; !slices.Equal(got, want) {
		t.Fatalf("creates are for %v, want %v", got, want)
	}
	if pp.Desired != 3 || pp.Unserved != 0 {
		t.Fatalf("desired=%d unserved=%d, want 3 and 0", pp.Desired, pp.Unserved)
	}
	if r := actionsOf(pp.Actions, ActionCreate)[0].Reason; !strings.Contains(r, "registered on acme/widgets") {
		t.Fatalf("the create's reason does not say where the runner is registered: %q", r)
	}
}

func TestAnOrganisationPoolNamesNoRepository(t *testing.T) {
	// An organisation's runner belongs to the organisation. Naming a repository
	// on its create would be read by the controller as somewhere to register it.
	p := testPool("linux", "linux")
	s := snap([]*store.Pool{p}, nil, []*store.Job{queuedIn("acme/widgets", "j1", time.Minute)},
		[]*store.Host{testHost("host_a", 8, 0)})
	for _, a := range actionsOf(only(t, Decide(s)).Actions, ActionCreate) {
		if a.Repo != "" {
			t.Fatalf("an organisation's create names the repository %q", a.Repo)
		}
	}
}

func TestAPersonalPoolDoesNotCountAnotherRepositorysRunnerAsSupply(t *testing.T) {
	// The whole reason such a pool is decided a repository at a time. By its
	// totals this pool has one runner free and one job waiting, and nothing to
	// do; in fact the job would wait for ever beside a runner that cannot
	// take it.
	p := testPool("linux", "linux")
	pp := only(t, Decide(personalSnap([]*store.Pool{p},
		[]*store.Runner{registeredOn("acme/widgets", idleRunner("r1", p, time.Minute))},
		[]*store.Job{queuedIn("acme/gadgets", "j1", time.Minute)})))

	if got, want := createRepos(pp.Actions), []string{"acme/gadgets"}; !slices.Equal(got, want) {
		t.Fatalf("creates are for %v, want %v", got, want)
	}
	if n := countOf(pp.Actions, ActionDrain); n != 0 {
		t.Fatalf("an idle runner inside its idle timeout was stopped with room to spare: %+v", pp.Actions)
	}
}

func TestAPersonalPoolCreatesNothingForAJobARunnerOfItsRepositoryWillTake(t *testing.T) {
	p := testPool("linux", "linux")
	pp := only(t, Decide(personalSnap([]*store.Pool{p},
		[]*store.Runner{
			registeredOn("acme/widgets", idleRunner("r1", p, time.Minute)),
			registeredOn("acme/gadgets", testRunner("r2", p, store.RunnerProvisioning, time.Second)),
		},
		[]*store.Job{
			queuedIn("acme/widgets", "j1", time.Minute),
			queuedIn("acme/gadgets", "j2", time.Minute),
		})))
	if len(pp.Actions) != 0 {
		t.Fatalf("a pool with a runner for every waiting job acted: %+v", pp.Actions)
	}
}

func TestAPersonalPoolStopsAStartingRunnerWhoseRepositoryHasNothingWaiting(t *testing.T) {
	// Most often the job was cancelled while its runner was starting. It has
	// no warmth worth keeping and no other job can ever use it.
	p := testPool("linux", "linux")
	pp := only(t, Decide(personalSnap([]*store.Pool{p},
		[]*store.Runner{registeredOn("acme/widgets", testRunner("r1", p, store.RunnerProvisioning, time.Second))},
		[]*store.Job{queuedIn("acme/gadgets", "j1", time.Minute)})))

	if got := runnerIDs(pp.Actions, ActionDrain); !slices.Equal(got, []string{"r1"}) {
		t.Fatalf("drained %v, want r1", got)
	}
	if r := actionsOf(pp.Actions, ActionDrain)[0].Reason; !strings.Contains(r, "acme/widgets") {
		t.Fatalf("the drain's reason does not name the repository: %q", r)
	}
	if got, want := createRepos(pp.Actions), []string{"acme/gadgets"}; !slices.Equal(got, want) {
		t.Fatalf("creates are for %v, want %v", got, want)
	}
}

func TestAPersonalPoolKeepsAnIdleRunnerUntilItsIdleTimeout(t *testing.T) {
	p := testPool("linux", "linux")
	young := registeredOn("acme/widgets", idleRunner("young", p, time.Minute))
	old := registeredOn("acme/widgets", idleRunner("old", p, 6*time.Minute))
	pp := only(t, Decide(personalSnap([]*store.Pool{p}, []*store.Runner{old, young}, nil)))

	if got := runnerIDs(pp.Actions, ActionDrain); !slices.Equal(got, []string{"old"}) {
		t.Fatalf("drained %v, want only the runner past its idle timeout", got)
	}
}

func TestAPersonalPoolAtItsMaximumMakesRoomForAJobInAnotherRepository(t *testing.T) {
	// Otherwise an idle runner no job can reach holds the last slot for the
	// length of the idle timeout while a job that could use the slot waits.
	p := testPool("linux", "linux")
	p.MaxRunners = 1
	pp := only(t, Decide(personalSnap([]*store.Pool{p},
		[]*store.Runner{registeredOn("acme/widgets", idleRunner("r1", p, time.Minute))},
		[]*store.Job{queuedIn("acme/gadgets", "j1", time.Minute)})))

	if got := runnerIDs(pp.Actions, ActionDrain); !slices.Equal(got, []string{"r1"}) {
		t.Fatalf("drained %v, want r1", got)
	}
	if n := countOf(pp.Actions, ActionCreate); n != 0 {
		t.Fatalf("created %d runners over the pool's maximum", n)
	}
	if pp.Unserved != 1 {
		t.Fatalf("unserved = %d, want 1: the job is still waiting for the slot", pp.Unserved)
	}
}

func TestAPersonalPoolKeepsNoWarmRunners(t *testing.T) {
	// A warm runner waits for a job that has not arrived, and here there is no
	// repository to register it on until one has.
	p := testPool("linux", "linux")
	p.MinRunners = 2
	pp := only(t, Decide(personalSnap([]*store.Pool{p}, nil, nil)))
	if len(pp.Actions) != 0 || pp.Desired != 0 {
		t.Fatalf("a personal account's pool made warm runners: desired=%d actions=%+v", pp.Desired, pp.Actions)
	}
	if p.MinRunners != 2 {
		t.Fatal("the caller's pool was changed")
	}
}

func TestAPersonalPoolLeavesABusyRunnerAlone(t *testing.T) {
	p := testPool("linux", "linux")
	pp := only(t, Decide(personalSnap([]*store.Pool{p},
		[]*store.Runner{registeredOn("acme/widgets", testRunner("r1", p, store.RunnerBusy, time.Hour))},
		[]*store.Job{queuedIn("acme/widgets", "j1", time.Minute)})))

	if n := countOf(pp.Actions, ActionDrain); n != 0 {
		t.Fatalf("a busy runner was drained: %+v", pp.Actions)
	}
	if got, want := createRepos(pp.Actions), []string{"acme/widgets"}; !slices.Equal(got, want) {
		t.Fatalf("creates are for %v, want %v: a busy runner takes no second job", got, want)
	}
}

func TestAPersonalPoolReadsARepositoryNameWithoutRegardToCase(t *testing.T) {
	// GitHub's names are case-insensitive, and a webhook carries the canonical
	// case while a runner's row carries what it was registered with.
	p := testPool("linux", "linux")
	pp := only(t, Decide(personalSnap([]*store.Pool{p},
		[]*store.Runner{registeredOn("Acme/Widgets", idleRunner("r1", p, time.Minute))},
		[]*store.Job{queuedIn("acme/widgets", "j1", time.Minute)})))
	if len(pp.Actions) != 0 {
		t.Fatalf("a runner on the job's own repository was not counted: %+v", pp.Actions)
	}
}

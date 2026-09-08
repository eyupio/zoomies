package controller

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// The Overview renders "nothing needs your attention" from an empty list, so
// an instance with nothing wrong must return exactly that -- and an empty
// slice, not nil, because the API marshals it straight to JSON.
func TestProblemsIsEmptyOnACleanInstance(t *testing.T) {
	h := newHarness(t)

	got, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	if got == nil {
		t.Fatal("Problems returned nil; the UI needs an empty slice to render its quiet state")
	}
	if len(got) != 0 {
		t.Fatalf("a clean instance reported %d problems: %+v", len(got), got)
	}
}

// Every category the panel aggregates, provoked one at a time.
func TestProblemsReportsEachCategory(t *testing.T) {
	t.Run("configuration warning", func(t *testing.T) {
		h := newHarness(t)
		h.cfg.Security.DisableAuth = true
		if !contains(h.problemCodes(), "auth.disabled") {
			t.Fatalf("problems = %v, want the disabled-auth warning", h.problemCodes())
		}
	})

	t.Run("dangerous pool", func(t *testing.T) {
		h := newHarness(t)
		inst := h.installation()
		p := h.pool(inst, "risky")
		p.DockerMode = store.DockerHostSocket
		p.Ephemeral = false
		if err := h.st.UpdatePool(h.ctx, p); err != nil {
			t.Fatalf("UpdatePool: %v", err)
		}
		if !contains(h.problemCodes(), "pool.dangerous") {
			t.Fatalf("problems = %v, want the dangerous-pool warning", h.problemCodes())
		}
	})

	t.Run("repository cache under an organisation installation", func(t *testing.T) {
		h := newHarness(t)
		inst := h.installation()
		p := h.pool(inst, "widgets")
		p.Cache = store.CacheConfig{Enabled: true, Scope: store.CacheScopeRepository, Repository: "acme/widgets"}
		if err := h.st.UpdatePool(h.ctx, p); err != nil {
			t.Fatalf("UpdatePool: %v", err)
		}
		if !contains(h.problemCodes(), "pool.cache_shared") {
			t.Fatalf("problems = %v, want the shared-cache warning", h.problemCodes())
		}
	})

	t.Run("unusable installation", func(t *testing.T) {
		h := newHarness(t)
		inst := h.installation()
		if err := h.st.SetInstallationHealth(h.ctx, inst.ID, "the App is not installed on acme"); err != nil {
			t.Fatalf("SetInstallationHealth: %v", err)
		}
		ps, err := h.c.Problems(h.ctx)
		if err != nil {
			t.Fatalf("Problems: %v", err)
		}
		found := false
		for _, p := range ps {
			if p.Code == "installation.unhealthy" {
				found = true
				if p.Severity != config.SeverityError {
					t.Fatalf("severity = %q, want error", p.Severity)
				}
				if p.Detail != "the App is not installed on acme" {
					t.Fatalf("detail = %q, want the probe's own message", p.Detail)
				}
			}
		}
		if !found {
			t.Fatalf("problems = %+v, want one about the installation", ps)
		}
	})

	t.Run("rejected webhooks", func(t *testing.T) {
		h := newHarness(t)
		h.fleet()
		h.deliver("workflow_job", jobEvent{Action: "queued", JobID: 1}.body(), "wrong")
		if !contains(h.problemCodes(), "webhook.rejected") {
			t.Fatalf("problems = %v, want the rejected-delivery warning", h.problemCodes())
		}
	})

	t.Run("no webhook has ever arrived", func(t *testing.T) {
		h := newHarness(t)
		h.installation()
		if !contains(h.problemCodes(), "webhook.never_received") {
			t.Fatalf("problems = %v, want the polling-only warning", h.problemCodes())
		}
	})

	t.Run("cordoned host with queued work", func(t *testing.T) {
		h := newHarness(t)
		_, _, host := h.fleet()
		if err := h.st.SetHostCordoned(h.ctx, host.ID, true); err != nil {
			t.Fatalf("SetHostCordoned: %v", err)
		}
		h.deliverJob(jobEvent{Action: "queued", JobID: 2, Labels: []string{"self-hosted", "linux", "x64", "demo"}})
		if !contains(h.problemCodes(), "host.cordoned_with_work") {
			t.Fatalf("problems = %v, want the cordoned-host warning", h.problemCodes())
		}
	})

	t.Run("failed runners", func(t *testing.T) {
		h := newHarness(t)
		_, pool, host := h.fleet()
		r := h.runnerRow(pool, host, store.RunnerProvisioning)
		if _, err := h.st.TransitionRunner(h.ctx, r.ID, store.RunnerFailed, "the image could not be pulled"); err != nil {
			t.Fatalf("TransitionRunner: %v", err)
		}
		if !contains(h.problemCodes(), "runners.failed") {
			t.Fatalf("problems = %v, want the failed-runner warning", h.problemCodes())
		}
	})
}

// A job whose labels no pool here advertises is not going to run here, and
// once it has waited long enough to mean something, saying so is the only way
// an operator finds out.
func TestUnmatchedJobIsRecordedAndReported(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	h.deliverJob(jobEvent{
		Action: "queued", JobID: 909,
		Labels:   []string{"self-hosted", "linux", "gpu", "cuda12"},
		QueuedAt: time.Now().Add(-unmatchedGrace - time.Minute),
	})

	job, err := h.st.GetJobByGitHubID(h.ctx, 909)
	if err != nil {
		t.Fatalf("GetJobByGitHubID: %v", err)
	}
	if job.Matched || job.PoolID != "" {
		t.Fatalf("job = %+v, want it recorded as unmatched", job)
	}

	// Before any reconcile, the flag on the row is what answers.
	if !contains(h.problemCodes(), "jobs.unmatched") {
		t.Fatalf("problems = %v, want the unmatched-job warning", h.problemCodes())
	}

	// And after one, the scheduler's own Unmatched list does.
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !contains(h.problemCodes(), "jobs.unmatched") {
		t.Fatalf("problems after a reconcile = %v, want the unmatched-job warning", h.problemCodes())
	}
	if rs := h.runners(); len(rs) != 0 {
		t.Fatalf("created %d runners for a job no pool claims", len(rs))
	}
}

// Errors come first: an operator scanning the panel should meet the things
// that are broken before the things that are merely risky.
func TestProblemsAreSortedErrorsFirst(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	p := h.pool(inst, "risky")
	p.DockerMode = store.DockerHostSocket
	if err := h.st.UpdatePool(h.ctx, p); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	if err := h.st.SetInstallationHealth(h.ctx, inst.ID, "credentials rejected"); err != nil {
		t.Fatalf("SetInstallationHealth: %v", err)
	}

	ps, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	if len(ps) < 2 {
		t.Fatalf("problems = %+v, want at least two", ps)
	}
	if ps[0].Severity != config.SeverityError {
		t.Fatalf("first problem is %q, want the error", ps[0].Severity)
	}
	seenWarning := false
	for _, p := range ps {
		if p.Severity == config.SeverityWarning {
			seenWarning = true
		} else if p.Severity == config.SeverityError && seenWarning {
			t.Fatalf("an error appears after a warning: %+v", ps)
		}
	}
}

// Stats is what the Overview's cards are built from.
func TestStatsSummarisesTheFleet(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	h.runnerRow(pool, host, store.RunnerBusy)
	h.runnerRow(pool, host, store.RunnerIdle)
	h.deliverJob(jobEvent{Action: "queued", JobID: 11, Labels: []string{"self-hosted", "linux", "x64", "demo"}})

	s, err := h.c.Stats(h.ctx, time.Hour)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.QueuedJobs != 1 {
		t.Fatalf("queued jobs = %d, want 1", s.QueuedJobs)
	}
	if s.Runners.Busy != 1 || s.Runners.Idle != 1 || s.Runners.Total != 2 {
		t.Fatalf("runner counts = %+v, want one busy and one idle", s.Runners)
	}
	if s.Hosts.Total != 1 || s.Hosts.Healthy != 1 || s.Hosts.Capacity != 4 || s.Hosts.Used != 2 {
		t.Fatalf("host counts = %+v, want one healthy host with 4 slots and 2 used", s.Hosts)
	}
	if len(s.Pools) != 1 || s.Pools[0].Queued != 1 || s.Pools[0].Live != 2 {
		t.Fatalf("pool stats = %+v, want one pool with one queued job and two live runners", s.Pools)
	}
	if s.Pools[0].Utilisation != 0.5 {
		t.Fatalf("utilisation = %v, want 0.5", s.Pools[0].Utilisation)
	}
}

// A pool nothing can run is the failure that looks like health: the pool is
// enabled, the job matched it, every host is connected, and no runner is ever
// created. Nothing else in the product reports it -- a scaling event is written
// only when the size actually moved -- so the panel has to.
func TestPoolWithNoHostToRunItIsReported(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	p := h.pool(inst, "linux-x64")
	p.Backend = store.BackendPodman
	if err := h.st.UpdatePool(h.ctx, p); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	// One connected, healthy host -- which offers docker, not podman.
	host := h.host("vm-1")
	host.BackendInfo = store.HostBackends{{
		Kind: store.BackendPodman, Available: false,
		Detail: "podman.sock is not readable by this agent",
	}}
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}
	h.deliverJob(jobEvent{Action: "queued", JobID: 4242, Labels: p.Labels})

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rs := h.runners(); len(rs) != 0 {
		t.Fatalf("created %d runners with no host able to run them", len(rs))
	}

	ps, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	var found *Problem
	for i, p := range ps {
		if p.Code == "pool.no_capacity" {
			found = &ps[i]
		}
	}
	if found == nil {
		t.Fatalf("problems = %v, want one saying the pool has nowhere to run", h.problemCodes())
	}
	// A queued job makes this an outage, not a warning about a pool that
	// merely cannot reach its minimum.
	if found.Severity != config.SeverityError {
		t.Fatalf("severity = %q, want error while a job is waiting", found.Severity)
	}
	if !strings.Contains(found.Detail, "podman") {
		t.Fatalf("detail = %q, want it to name the backend no host offers", found.Detail)
	}
	// The agent's own explanation is the whole fix, so it must survive the
	// trip from the probe to the panel.
	if !strings.Contains(found.Detail, "podman.sock is not readable") {
		t.Fatalf("detail = %q, want the host's own explanation", found.Detail)
	}
	if found.Fix == "" || found.TargetID != p.ID {
		t.Fatalf("problem = %+v, want a fix and a link to the pool", found)
	}
	// "point this pool at a backend they already offer" is only a fix if the
	// panel says which one, and hands the UI enough to make the change.
	if !slices.Contains(found.Alternatives, string(store.BackendDocker)) {
		t.Fatalf("alternatives = %v, want the docker backend this host does offer", found.Alternatives)
	}
	if !strings.Contains(found.Fix, "docker") {
		t.Fatalf("fix = %q, want it to name the backend to switch to", found.Fix)
	}
}

func TestRepositoryScaleUpDeferralIsAccurateInProblemsDrawer(t *testing.T) {
	h := newHarness(t)
	h.c.setLastPlan(scheduler.Plan{Pools: []scheduler.PoolPlan{{
		PoolID: "pool_shared", PoolName: "shared", QueuedMatched: 3,
		QuotaDeferredJobs: 2, QuotaDeferredRepositories: []string{"acme/api", "acme/web"},
	}}})

	ps := h.c.PoolCapacityProblems()
	if len(ps) != 1 || ps[0].Code != "pool.repository_scale_up_deferred" {
		t.Fatalf("problems = %+v, want one scale-up deferral", ps)
	}
	if ps[0].Severity != config.SeverityWarning || !strings.Contains(ps[0].Detail, "Compatible idle runners may still accept") {
		t.Fatalf("problem = %+v, want best-effort GitHub assignment caveat", ps[0])
	}
	if !strings.Contains(ps[0].Detail, "acme/api, acme/web") || !strings.Contains(ps[0].Fix, "repository-specific pools") {
		t.Fatalf("problem = %+v, want affected repositories and strict-isolation guidance", ps[0])
	}
}

// The same pool, once a host can run it, drops off the panel: a problem that
// never clears is one an operator learns to ignore.
func TestPoolProblemClearsWhenAHostCanRunIt(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	p := h.pool(inst, "linux-x64")
	h.deliverJob(jobEvent{Action: "queued", JobID: 4243, Labels: p.Labels})

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !contains(h.problemCodes(), "pool.no_capacity") {
		t.Fatalf("problems = %v, want the blocked pool with no hosts at all", h.problemCodes())
	}

	h.host("vm-1")
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if contains(h.problemCodes(), "pool.no_capacity") {
		t.Fatalf("problems = %v, want the blocked pool gone once a host can run it", h.problemCodes())
	}
}

// A fleet that is simply full is the system working: the jobs are waiting for a
// runner to finish, not for an operator. It is still worth saying, but it is
// not an outage.
func TestAFullFleetIsAWarningRatherThanAnOutage(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	p := h.pool(inst, "linux-x64")
	host := h.host("vm-1")
	host.Capacity = 1
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}
	// The one slot is taken, and a second job is queued behind it.
	h.runnerRow(p, host, store.RunnerBusy)
	h.deliverJob(jobEvent{Action: "queued", JobID: 4244, Labels: p.Labels})

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	ps, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	found := false
	for _, pr := range ps {
		if pr.Code != "pool.no_capacity" {
			continue
		}
		found = true
		if pr.Severity != config.SeverityWarning {
			t.Errorf("severity = %q, want a warning: the fleet is busy, not broken", pr.Severity)
		}
		if !strings.Contains(pr.Detail, "at capacity") {
			t.Errorf("detail = %q, want it to say the fleet is full", pr.Detail)
		}
	}
	if !found {
		t.Fatalf("problems = %v, want the pool waiting on capacity", h.problemCodes())
	}
}

// The installation's webhooks cover every job in its repositories, most of
// which this fleet never touches. A job on GitHub's own runners is theirs to
// run however long it queues, and a job no pool here claims may be another
// provider's, about to start there; neither is a problem for this fleet, and
// the dev instance once showed fifty of them as jobs that would never run.
func TestAHostedOrFreshUnmatchedJobIsNotAProblem(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	h.deliverJob(jobEvent{
		Action: "queued", JobID: 910,
		Labels:   []string{"ubuntu-latest"},
		QueuedAt: time.Now().Add(-time.Hour),
	})
	h.deliverJob(jobEvent{
		Action: "queued", JobID: 911,
		Labels:   []string{"blacksmith-4vcpu-ubuntu-2404"},
		QueuedAt: time.Now().Add(-time.Hour),
	})
	h.deliverJob(jobEvent{
		Action: "queued", JobID: 912,
		Labels: []string{"self-hosted", "arc-runner-set"},
		// Fresh: another provider's scale-up delay has not run out.
	})
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if contains(h.problemCodes(), "jobs.unmatched") {
		t.Fatalf("problems = %v; hosted and freshly queued jobs are not this fleet's", h.problemCodes())
	}

	// The view says which jobs are hosted, so the UI can badge them rather
	// than warn about them.
	hosted, err := h.st.GetJobByGitHubID(h.ctx, 910)
	if err != nil {
		t.Fatal(err)
	}
	if v := NewJobView(hosted, ""); !v.Hosted || v.Matched {
		t.Fatalf("view = %+v, want hosted and unmatched", v)
	}
	own, err := h.st.GetJobByGitHubID(h.ctx, 912)
	if err != nil {
		t.Fatal(err)
	}
	if v := NewJobView(own, ""); v.Hosted {
		t.Fatalf("a self-hosted label counted as hosted: %+v", v)
	}
}

// A cordoned host used to be blamed for every queued job in the fleet, pools it
// never offered included, which sent an operator to uncordon a machine that
// would have changed nothing.
func TestACordonedHostIsOnlyBlamedForWorkItCouldRun(t *testing.T) {
	h := newHarness(t)
	inst, _, host := h.fleet()
	if err := h.st.SetHostCordoned(h.ctx, host.ID, true); err != nil {
		t.Fatalf("SetHostCordoned: %v", err)
	}
	// A pool this host never offered: it runs bare processes, and the host's
	// agent speaks Docker.
	bare := h.pool(inst, "bare", "self-hosted", "bare")
	bare.Backend = store.BackendProcess
	if err := h.st.UpdatePool(h.ctx, bare); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	h.deliverJob(jobEvent{Action: "queued", JobID: 3, Labels: []string{"self-hosted", "bare"}})
	if contains(h.problemCodes(), "host.cordoned_with_work") {
		t.Fatalf("problems = %v; the queued job is for a backend this host does not offer", h.problemCodes())
	}

	h.deliverJob(jobEvent{Action: "queued", JobID: 4, Labels: []string{"self-hosted", "linux", "x64", "demo"}})
	if !contains(h.problemCodes(), "host.cordoned_with_work") {
		t.Fatalf("problems = %v, want the cordoned-host warning for a job it could run", h.problemCodes())
	}
}

// The failed-runner count came from a page of at most a hundred rows, so a
// fleet having a bad day was told it had a hundred failures however many it had.
func TestTheFailedRunnerCountIsNotAPage(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	const failed = 120
	for i := 0; i < failed; i++ {
		r := h.runnerRow(pool, host, store.RunnerProvisioning)
		if _, err := h.st.TransitionRunner(h.ctx, r.ID, store.RunnerFailed, "the image could not be pulled"); err != nil {
			t.Fatalf("TransitionRunner: %v", err)
		}
	}
	ps, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	for _, p := range ps {
		if p.Code == "runners.failed" {
			if want := "120 runners in the failed state"; p.Title != want {
				t.Fatalf("title = %q, want %q", p.Title, want)
			}
			return
		}
	}
	t.Fatalf("problems = %v, want runners.failed", ps)
}

// findProblem returns the problem with a code, or fails saying what was there.
func findProblem(t *testing.T, h *harness, code string) Problem {
	t.Helper()
	ps, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	for _, p := range ps {
		if p.Code == code {
			return p
		}
	}
	t.Fatalf("problems = %v, want %s", codesOf(ps), code)
	return Problem{}
}

// A runner that is still starting up normally must not raise anything. This is
// the expensive half of the behaviour to get wrong: a warning that appears
// every time a pool creates a runner is a warning nobody reads by the end of
// the week.
func TestARunnerThatIsStillComingUpIsNotAProblem(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	h.runnerRow(pool, host, store.RunnerRegistering)

	if got := h.problemCodes(); contains(got, "runners.not_progressing") {
		t.Fatalf("problems = %v, want nothing about a runner that was created a moment ago", got)
	}
}

// The distinction this problem exists to make: a container that started and a
// runner that has not registered is the runner process failing to reach GitHub,
// and the fix says to read that runner's own logs.
func TestARunnerWhoseContainerStartedButNeverRegisteredNamesItsOwnLogs(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()

	started := time.Now()
	r := h.runnerRow(pool, host, store.RunnerRegistering)
	if err := h.st.SetRunnerStartup(h.ctx, r.ID, nil, &started); err != nil {
		t.Fatalf("SetRunnerStartup: %v", err)
	}
	// Past half the provision timeout, but not yet past the timeout itself:
	// the whole point is to say something while there is still time to look.
	h.c.clock = func() time.Time { return started.Add(3 * time.Minute) }

	p := findProblem(t, h, "runners.not_progressing")
	if !strings.Contains(p.Fix, "has not registered") || !strings.Contains(p.Fix, "github.com") {
		t.Fatalf("fix = %q, want the runner-side causes", p.Fix)
	}
	if !strings.Contains(p.Detail, r.Name) {
		t.Fatalf("detail = %q, want the runner named", p.Detail)
	}
	if p.TargetID != r.ID || p.TargetKind != "runner" {
		t.Fatalf("target = %s/%s, want the runner itself", p.TargetKind, p.TargetID)
	}
}

// The other shape, and the reason one code is not enough on its own: nothing
// has reported a workload at all, which is a problem on the host rather than
// inside the runner.
func TestARunnerWithNoContainerYetPointsAtTheHost(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerProvisioning)

	h.c.clock = func() time.Time { return r.CreatedAt.Add(3 * time.Minute) }

	p := findProblem(t, h, "runners.not_progressing")
	if !strings.Contains(p.Fix, "agent log") || !strings.Contains(p.Fix, "image") {
		t.Fatalf("fix = %q, want the host-side causes", p.Fix)
	}
	if !strings.Contains(p.Detail, host.Name) {
		t.Fatalf("detail = %q, want the host named when they are all on one", p.Detail)
	}
}

// The threshold is half the provision timeout rather than a number of its own,
// so an operator who allows longer for a slow image pull is not then told their
// runners are stuck while they are still within the time they allowed.
func TestTheStuckThresholdFollowsTheProvisionTimeout(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := h.runnerRow(pool, host, store.RunnerRegistering)
	h.c.clock = func() time.Time { return r.CreatedAt.Add(3 * time.Minute) }

	if got := h.problemCodes(); !contains(got, "runners.not_progressing") {
		t.Fatalf("problems = %v, want the default 5m timeout to have raised it by 3m", got)
	}

	h.cfg.Scheduler.ProvisionTimeout = 20 * time.Minute
	if got := h.problemCodes(); contains(got, "runners.not_progressing") {
		t.Fatalf("problems = %v, want silence three minutes into a twenty-minute allowance", got)
	}

	// Off means off: a fleet that has switched the timeout off has said that
	// runners may take as long as they take.
	h.cfg.Scheduler.ProvisionTimeout = 0
	h.c.clock = func() time.Time { return r.CreatedAt.Add(24 * time.Hour) }
	if got := h.problemCodes(); contains(got, "runners.not_progressing") {
		t.Fatalf("problems = %v, want nothing when provision_timeout is off", got)
	}
}

// The detail names one runner and the fix tells you what to do about it, so on
// a mixed fleet the two must describe the same runner. They used to be chosen
// separately -- the example was the oldest, the fix was whichever shape there
// were more of -- so an even split named a runner with no container and then
// told the operator to go and read that container's logs.
func TestTheFixDescribesTheRunnerTheDetailNames(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()

	// Oldest first and still waiting for a container, then a younger one whose
	// container started: one of each, so no bucket is the larger.
	waiting := h.runnerRow(pool, host, store.RunnerProvisioning)
	started := h.runnerRow(pool, host, store.RunnerRegistering)
	// The younger one's clock starts a second after the older one's, not at
	// the wall clock: the two rows are written within a millisecond of each
	// other, and a container stamped with the same millisecond as the other
	// runner's creation is not younger by anyone's clock.
	at := waiting.CreatedAt.Add(time.Second)
	if err := h.st.SetRunnerStartup(h.ctx, started.ID, nil, &at); err != nil {
		t.Fatalf("SetRunnerStartup: %v", err)
	}
	h.c.clock = func() time.Time { return waiting.CreatedAt.Add(3 * time.Minute) }

	p := findProblem(t, h, "runners.not_progressing")
	if !strings.Contains(p.Detail, waiting.Name) {
		t.Fatalf("detail = %q, want the oldest runner %s", p.Detail, waiting.Name)
	}
	if !strings.Contains(p.Fix, "agent log") {
		t.Fatalf("fix = %q, want the host-side fix that matches a runner with no container", p.Fix)
	}
	if !strings.Contains(p.Detail, "1 runner waiting for a container") {
		t.Fatalf("detail = %q, want both counts named", p.Detail)
	}
}

// Runners created in one scheduler pass share a millisecond, and the problem
// used to pick whichever the store returned first, so the same fleet could be
// described two ways on two passes. A tie names the runner with no container,
// whichever order the rows come back in.
func TestAStuckTieNamesTheRunnerWithNoContainer(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()

	started := h.runnerRow(pool, host, store.RunnerRegistering)
	waiting := h.runnerRow(pool, host, store.RunnerProvisioning)
	// The container came up at the exact moment the other runner was created.
	at := waiting.CreatedAt
	if err := h.st.SetRunnerStartup(h.ctx, started.ID, nil, &at); err != nil {
		t.Fatalf("SetRunnerStartup: %v", err)
	}
	h.c.clock = func() time.Time { return waiting.CreatedAt.Add(3 * time.Minute) }

	for i := 0; i < 5; i++ {
		p := findProblem(t, h, "runners.not_progressing")
		if !strings.Contains(p.Detail, waiting.Name) {
			t.Fatalf("pass %d: detail = %q, want the runner with no container, %s", i, p.Detail, waiting.Name)
		}
		if !strings.Contains(p.Fix, "agent log") {
			t.Fatalf("pass %d: fix = %q, want the host-side fix", i, p.Fix)
		}
	}
}

// The drawer is read at the moment something is wrong, so the one thing it
// must never do is come back empty because it could not look. It used to
// return a 500 for any single failing query, and a drawer that will not load
// is indistinguishable from a fleet with nothing wrong.
//
// A cancelled context fails every store query at once, which is the strongest
// version of the case: the sections that need the database are all lost, the
// ones that do not are still there, and the list says out loud that it is
// incomplete.
func TestProblemsSurvivesASectionItCannotGather(t *testing.T) {
	h := newHarness(t)
	h.cfg.Security.DisableAuth = true

	ctx, cancel := context.WithCancel(h.ctx)
	cancel()

	got, err := h.c.Problems(ctx)
	if err != nil {
		t.Fatalf("Problems returned an error rather than what it could gather: %v", err)
	}
	codes := make([]string, 0, len(got))
	for _, p := range got {
		codes = append(codes, p.Code)
	}
	// What does not need the database is still reported.
	if !slices.Contains(codes, "auth.disabled") {
		t.Errorf("a configuration warning was lost with the database sections: %v", codes)
	}
	// And the operator is told the list is short, rather than being left to
	// read it as a clean fleet.
	i := slices.Index(codes, "controller.problems_partial")
	if i < 0 {
		t.Fatalf("an incomplete list did not say so: %v", codes)
	}
	if got[i].Severity != config.SeverityError {
		t.Errorf("the incomplete-list entry is %q; a list that cannot be trusted is an error", got[i].Severity)
	}
	// Naming the sections is what makes it actionable rather than alarming.
	for _, section := range []string{"hosts", "jobs", "runners"} {
		if !strings.Contains(got[i].Detail, section) {
			t.Errorf("the detail does not name the %s section: %q", section, got[i].Detail)
		}
	}
}

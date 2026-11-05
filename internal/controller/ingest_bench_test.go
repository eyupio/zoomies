package controller

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// A queued job reaches the fleet by two roads: GitHub delivers a workflow_job
// webhook, or the poller finds the job on a sweep. Both read the pools, claim
// the job for one of them, write its row and its timeline and tell the browsers
// that are watching, so what that costs is what the fleet pays for every job it
// is asked to run, and again for every job that is still waiting each time the
// poller comes round. The reconcile pass is the other cost, and the steadier
// one: it runs on a timer whether or not either road has anything to say.
// These benchmarks time all three on the controller the tests use.
//
// Each checks its scenario once before the timer starts, for the reason the
// scheduler's do: a delivery that was refused, or a job no pool claimed, would
// time a cheaper path under the name of a dearer one.

// benchLabel is the label pool i is asked for by. It is not taken from the
// pool's name, which the store rewrites when the pool is created.
func benchLabel(i int) string { return fmt.Sprintf("pool%03d", i) }

// benchPools seeds an installation and n pools, each asked for by a label of
// its own, so a job's claim has to be found among them.
func benchPools(h *harness, n int) (*store.Installation, []*store.Pool) {
	inst := h.installation()
	pools := make([]*store.Pool, n)
	for i := range pools {
		pools[i] = h.pool(inst, benchLabel(i), "self-hosted", "linux", "x64", benchLabel(i))
	}
	return inst, pools
}

// benchWebhook delivers the queued workflow_job for job id, which asks for the
// pool its number falls on among n, and reports what the endpoint answered.
func benchWebhook(h *harness, id int64, n int) int {
	body := jobEvent{
		Action: "queued", JobID: id, RunID: 9_000_000_000 + id/3,
		Name: "build (linux, x64)", Workflow: "CI",
		Labels: []string{"self-hosted", "linux", "x64", benchLabel(int(id) % n)},
	}.body()
	return h.deliver("workflow_job", body, testWebhookSecret).Code
}

// benchClaimed fails the benchmark unless the job with this GitHub ID has been
// claimed by the pool its number falls on among pools.
func benchClaimed(b *testing.B, h *harness, id int64, pools []*store.Pool) {
	b.Helper()
	job, err := h.st.GetJobByGitHubID(h.ctx, id)
	if err != nil || !job.Matched || job.PoolID != pools[int(id)%len(pools)].ID {
		b.Fatalf("job %d was not claimed by the pool it asked for: %+v, err = %v", id, job, err)
	}
}

// BenchmarkWebhookJob is one signed workflow_job delivery through HandleWebhook,
// from the signature check to the 202: the installation found, a secret
// unsealed, the pools read and one of them chosen, the job and its timeline
// written, the delivery logged and the browsers told. The request is built and
// signed inside the loop, because each delivery is a different job; that is a
// few microseconds of the figure.
func BenchmarkWebhookJob(b *testing.B) {
	b.Run("queued", func(b *testing.B) {
		for _, n := range []int{4, 20, 100} {
			b.Run(fmt.Sprintf("pools=%d", n), func(b *testing.B) {
				h := newHarness(b)
				_, pools := benchPools(h, n)

				if code := benchWebhook(h, 1, n); code != http.StatusAccepted {
					b.Fatalf("the first delivery was answered %d, want 202", code)
				}
				benchClaimed(b, h, 1, pools)

				b.ReportAllocs()
				for id := int64(2); b.Loop(); id++ {
					if code := benchWebhook(h, id, n); code != http.StatusAccepted {
						b.Fatalf("delivery %d was answered %d, want 202", id, code)
					}
				}
			})
		}
	})

	// The same job delivered again. GitHub delivers at least once, so this is
	// routine; nothing changes, but the pools are still read and the job
	// matched and written before the merge finds there is nothing to add.
	b.Run("redelivered", func(b *testing.B) {
		h := newHarness(b)
		_, pools := benchPools(h, 20)
		for range 2 {
			if code := benchWebhook(h, 1, len(pools)); code != http.StatusAccepted {
				b.Fatalf("a delivery was answered %d, want 202", code)
			}
		}
		benchClaimed(b, h, 1, pools)
		if _, total, err := h.st.ListJobs(h.ctx, store.JobFilter{}, store.Page{Limit: 10}); err != nil || total != 1 {
			b.Fatalf("two deliveries of one job left %d jobs, err = %v; want 1", total, err)
		}

		b.ReportAllocs()
		for b.Loop() {
			if code := benchWebhook(h, 1, len(pools)); code != http.StatusAccepted {
				b.Fatalf("a redelivery was answered %d, want 202", code)
			}
		}
	})
}

// benchRepo is the repository job number id belongs to when a sweep's jobs are
// spread over n of them in turn. One repository is the one the other benchmarks
// have always used.
func benchRepo(id int64, n int) string {
	if n <= 1 {
		return "acme/widgets"
	}
	return fmt.Sprintf("acme/repo-%03d", int(id)%n)
}

// benchPolled is n queued jobs as a sweep finds them, numbered from first,
// asking for the pools in turn and spread over repoCount repositories in turn.
func benchPolled(first, n int64, poolCount, repoCount int) []github.QueuedJob {
	queuedAt := time.Now().Add(-time.Minute)
	jobs := make([]github.QueuedJob, n)
	for i := range jobs {
		id := first + int64(i)
		repo := benchRepo(id, repoCount)
		jobs[i] = github.QueuedJob{
			ID: id, RunID: 9_000_000_000 + id/3, RunNumber: 1000 + id/3,
			Repo: repo, WorkflowName: "CI", JobName: "build (linux, x64)",
			Labels:   []string{"self-hosted", "linux", "x64", benchLabel(int(id) % poolCount)},
			QueuedAt: queuedAt, HTMLURL: "https://github.com/" + repo + "/actions/runs/9000000000",
			Status: "queued",
		}
	}
	return jobs
}

// BenchmarkIngestQueuedJobs is one poller sweep over the queued jobs a
// repository has, through ingestQueuedJobs. Each op is the whole sweep, so the
// per-job cost is the figure divided by the jobs.
func BenchmarkIngestQueuedJobs(b *testing.B) {
	// The sweep that matters most: every job is one the fleet already knows,
	// found again because it is still waiting. This is what the poller costs
	// on every round for as long as a queue lasts.
	b.Run("known", func(b *testing.B) {
		// repos is how many repositories the jobs are spread over; the sweeps
		// that do not say are of one repository, as they always were. The jobs
		// of a repository share the answer to which installation covers it.
		for _, tc := range []struct{ jobs, pools, repos int }{
			{10, 20, 1}, {100, 20, 1}, {500, 20, 1}, {100, 4, 1}, {100, 100, 1},
			{100, 20, 10}, {100, 20, 100},
		} {
			name := fmt.Sprintf("jobs=%d/pools=%d", tc.jobs, tc.pools)
			if tc.repos > 1 {
				name += fmt.Sprintf("/repos=%d", tc.repos)
			}
			b.Run(name, func(b *testing.B) {
				h := newHarness(b)
				inst, pools := benchPools(h, tc.pools)
				jobs := benchPolled(1, int64(tc.jobs), tc.pools, tc.repos)
				if left, err := h.c.ingestQueuedJobs(h.ctx, inst, jobs); err != nil || left != tc.jobs {
					b.Fatalf("the first sweep left %d of %d jobs waiting, err = %v", left, tc.jobs, err)
				}
				// A sweep counts a job nobody claimed as waiting too, so the
				// count above cannot say the pools were found.
				benchClaimed(b, h, 1, pools)
				benchClaimed(b, h, int64(tc.jobs), pools)
				if job, err := h.st.GetJobByGitHubID(h.ctx, int64(tc.jobs)); err != nil || job.InstallationID != inst.ID || job.Repo != jobs[len(jobs)-1].Repo {
					b.Fatalf("the last job was attributed to %+v, err = %v; want installation %s and repository %s", job, err, inst.ID, jobs[len(jobs)-1].Repo)
				}

				b.ReportAllocs()
				for b.Loop() {
					if _, err := h.c.ingestQueuedJobs(h.ctx, inst, jobs); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})

	// A sweep that finds jobs for the first time, as the one after a burst
	// does, each of which is inserted, claimed and put on its timeline.
	b.Run("new", func(b *testing.B) {
		const jobs, poolCount = 100, 20
		h := newHarness(b)
		inst, pools := benchPools(h, poolCount)
		next := int64(1)
		sweep := func() (int, error) {
			batch := benchPolled(next, jobs, poolCount, 1)
			next += jobs
			return h.c.ingestQueuedJobs(h.ctx, inst, batch)
		}
		if left, err := sweep(); err != nil || left != jobs {
			b.Fatalf("the first sweep left %d of %d jobs waiting, err = %v", left, jobs, err)
		}
		benchClaimed(b, h, 1, pools)
		benchClaimed(b, h, jobs, pools)

		b.ReportAllocs()
		for b.Loop() {
			if _, err := sweep(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// warmFleet is n pools and half as many hosts with a runner idle in every pool,
// on the hosts in turn, and one pass over them to settle it.
func warmFleet(b *testing.B, n int) *harness {
	b.Helper()
	h := newHarness(b)
	_, pools := benchPools(h, n)
	hosts := make([]*store.Host, max(n/2, 1))
	for i := range hosts {
		hosts[i] = h.host(fmt.Sprintf("vm-%03d", i))
	}
	for i, p := range pools {
		h.seedRunner(b, p, hosts[i%len(hosts)], store.RunnerIdle)
	}
	if err := h.c.Reconcile(h.ctx); err != nil {
		b.Fatalf("Reconcile: %v", err)
	}
	if !h.fleetAtRest(n) {
		b.Fatalf("a fleet with a runner idle in each of %d pools and nothing queued did not stay at rest", n)
	}
	return h
}

// fleetAtRest reports whether the fleet is n runners, every one idle.
func (h *harness) fleetAtRest(n int) bool {
	rs := h.runners()
	for _, r := range rs {
		if r.State != store.RunnerIdle {
			return false
		}
	}
	return len(rs) == n
}

// BenchmarkReconcile is one pass of the reconcile loop over a fleet with
// nothing to do. It is the steady cost of a controller that is simply running,
// since the loop makes this pass on every tick whether or not anything has
// happened. Quiet is pools and hosts with no runners and no queue; warm is the
// same fleet with a runner idle in every pool.
//
// Neither has anyone looking. The figures that follow a pass (the statistics,
// the problems list, the host views) are worked out only while something listens
// to the event stream, and for a controller nobody is watching they are not
// worked out at all, so quiet and warm do not include them. Watched is warm with
// a listener, which is the pass an operator who has the page open is paying for.
func BenchmarkReconcile(b *testing.B) {
	b.Run("quiet", func(b *testing.B) {
		for _, n := range []int{4, 20, 100} {
			b.Run(fmt.Sprintf("pools=%d", n), func(b *testing.B) {
				h := newHarness(b)
				benchPools(h, n)
				for i := range max(n/2, 1) {
					h.host(fmt.Sprintf("vm-%03d", i))
				}
				if err := h.c.Reconcile(h.ctx); err != nil {
					b.Fatalf("Reconcile: %v", err)
				}
				if rs := h.runners(); len(rs) != 0 {
					b.Fatalf("a fleet with nothing queued started %d runners", len(rs))
				}

				b.ReportAllocs()
				for b.Loop() {
					if err := h.c.Reconcile(h.ctx); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})

	// A fleet at rest is not always an empty one. A pool that keeps a runner warm has
	// its row read, its host charged for it and its slot counted on every pass, none
	// of which the fleet above ever does. Here every pool has one runner idle, on the
	// hosts in turn, and the pass is one that leaves them all alone.
	b.Run("warm", func(b *testing.B) {
		for _, n := range []int{4, 20, 100} {
			b.Run(fmt.Sprintf("pools=%d", n), func(b *testing.B) {
				h := warmFleet(b, n)

				b.ReportAllocs()
				for b.Loop() {
					if err := h.c.Reconcile(h.ctx); err != nil {
						b.Fatal(err)
					}
				}
				if !h.fleetAtRest(n) {
					b.Fatal("the passes timed changed the fleet they were timing")
				}
			})
		}
	})

	// The warm fleet with a browser on the page. Every pass is followed by the
	// statistics, the problems list and the host views, and the problems list is
	// the whole fleet read again section by section, so what this adds to a pass is
	// several times the pass.
	b.Run("watched", func(b *testing.B) {
		for _, n := range []int{4, 20, 100} {
			b.Run(fmt.Sprintf("pools=%d", n), func(b *testing.B) {
				h := warmFleet(b, n)
				sub := h.listen()
				told := map[events.Kind]bool{}
				if err := h.c.Reconcile(h.ctx); err != nil {
					b.Fatalf("Reconcile: %v", err)
				}
				for len(sub.C) > 0 {
					told[(<-sub.C).Kind] = true
				}
				if !told[events.KindStats] || !told[events.KindProblems] {
					b.Fatalf("a pass with a listener told it %v; want it told the statistics and the problems, or the figures that follow a pass are not being worked out", told)
				}
				// A listener that does not read is dropped, and then the next pass
				// is an unwatched one under the name of a watched one.
				go func() {
					for range sub.C {
					}
				}()

				b.ReportAllocs()
				for b.Loop() {
					if err := h.c.Reconcile(h.ctx); err != nil {
						b.Fatal(err)
					}
				}
				if h.c.Events().Subscribers() == 0 {
					b.Fatal("the listener was dropped while the passes were timed")
				}
			})
		}
	})

	// The warm fleet with a listener that asks for one kind of event, as an API
	// client following jobs does, or an agent waiting for something to change. It
	// asked for no statistic, no problem and no host card, so there is nothing
	// the pass has to work out for it.
	b.Run("filtered", func(b *testing.B) {
		for _, n := range []int{4, 20, 100} {
			b.Run(fmt.Sprintf("pools=%d", n), func(b *testing.B) {
				h := warmFleet(b, n)
				sub := h.listen(events.KindJobUpdated)
				if err := h.c.Reconcile(h.ctx); err != nil {
					b.Fatalf("Reconcile: %v", err)
				}
				// A listener that does not read is dropped, and then the next pass
				// is an unwatched one under the name of a watched one.
				go func() {
					for range sub.C {
					}
				}()
				if h.c.Events().Subscribers() != 1 {
					b.Fatalf("%d listeners; want the one that asked for job events", h.c.Events().Subscribers())
				}

				b.ReportAllocs()
				for b.Loop() {
					if err := h.c.Reconcile(h.ctx); err != nil {
						b.Fatal(err)
					}
				}
				if h.c.Events().Subscribers() == 0 {
					b.Fatal("the listener was dropped while the passes were timed")
				}
			})
		}
	})

	// The watched fleet with a history behind it. A controller that has run for a
	// week has a jobs table to match, and the problems list asks it how long jobs
	// waited, how long they took against the week before, and what they used. The
	// fleets above have no job in the table, so none of that has ever been timed
	// against the thing it is dearest on. The jobs here are spread evenly over the
	// last eight days, the span the slower-job check reads.
	b.Run("busy", func(b *testing.B) {
		for _, jobs := range []int{2000, 10000} {
			b.Run(fmt.Sprintf("pools=20/jobs=%d", jobs), func(b *testing.B) {
				h := warmFleet(b, 20)
				pools, err := h.st.ListPools(h.ctx)
				if err != nil || len(pools) != 20 {
					b.Fatalf("ListPools = %d pools, err = %v; want 20", len(pools), err)
				}
				seedFinishedJobs(b, h, pools, jobs)

				now := time.Now()
				dayAgo, weekBefore := now.Add(-slowerRecent), now.Add(-slowerRecent-slowerBase)
				_, recent, err := h.st.ListJobs(h.ctx, store.JobFilter{Since: &dayAgo}, store.Page{Limit: 1})
				if err != nil {
					b.Fatalf("ListJobs: %v", err)
				}
				_, earlier, err := h.st.ListJobs(h.ctx, store.JobFilter{Since: &weekBefore, Until: &dayAgo}, store.Page{Limit: 1})
				if err != nil {
					b.Fatalf("ListJobs: %v", err)
				}
				if recent+earlier != jobs || recent < jobs/10 || earlier < jobs/2 {
					b.Fatalf("the table holds %d jobs in the last day and %d in the week before; want all %d, with the week the larger share", recent, earlier, jobs)
				}

				sub := h.listen()
				told := map[events.Kind]bool{}
				if err := h.c.Reconcile(h.ctx); err != nil {
					b.Fatalf("Reconcile: %v", err)
				}
				for len(sub.C) > 0 {
					told[(<-sub.C).Kind] = true
				}
				if !told[events.KindStats] || !told[events.KindProblems] {
					b.Fatalf("a pass with a listener told it %v; want it told the statistics and the problems", told)
				}
				go func() {
					for range sub.C {
					}
				}()
				checkSlowerMix(b, h)

				b.ReportAllocs()
				for i := 0; b.Loop(); i++ {
					// The slower-job check reuses what it found for a minute. At the
					// shipped ten-second interval that is five passes in six, and the sixth
					// reads the jobs again, so the passes timed are that mix.
					if i%6 == 0 {
						forgetSlowerJobs(h.c)
					}
					if err := h.c.Reconcile(h.ctx); err != nil {
						b.Fatal(err)
					}
				}
				if h.c.Events().Subscribers() == 0 {
					b.Fatal("the listener was dropped while the passes were timed")
				}
			})
		}
	})
}

// seedFinishedJobs records n completed jobs over the last eight days, across the
// pools and five job names in turn. Most succeed; one in twenty fails and one in
// fifty is cancelled, which is the part of the history the duration figures leave out.
func seedFinishedJobs(b *testing.B, h *harness, pools []*store.Pool, n int) {
	b.Helper()
	now := time.Now()
	spacing := (slowerRecent + slowerBase - time.Hour) / time.Duration(n)
	for i := range n {
		queued := now.Add(-time.Duration(i+1) * spacing)
		started, done := queued.Add(20*time.Second), queued.Add(time.Duration(50+i%90)*time.Second)
		conclusion := "success"
		switch {
		case i%50 == 0:
			conclusion = "cancelled"
		case i%20 == 0:
			conclusion = "failure"
		}
		if _, err := h.st.UpsertJob(h.ctx, &store.Job{
			GitHubJobID: int64(5000 + i), Repo: "acme/api", Workflow: "CI", JobName: fmt.Sprintf("build-%d", i%5),
			State: store.JobCompleted, Conclusion: conclusion, PoolID: pools[i%len(pools)].ID, Matched: true,
			QueuedAt: queued, StartedAt: &started, CompletedAt: &done,
		}); err != nil {
			b.Fatalf("UpsertJob: %v", err)
		}
	}
}

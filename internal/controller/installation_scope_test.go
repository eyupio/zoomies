package controller

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// Two installations whose pools advertise exactly the same labels is the shape
// that used to cross-allocate: nothing about a job said which GitHub target it
// belonged to, so the label rule picked between the pools on their names and
// the runner was minted in whichever organisation won. These tests are the
// three paths a job can arrive by, each proving the job lands on its own
// installation's pool and that nothing was created in the other target.

// twoInstallations seeds two organisations, each with a pool advertising the
// same labels, and one host big enough for either.
func twoInstallations(h *harness) (acme, globex *store.Pool) {
	h.t.Helper()
	labels := []string{"self-hosted", "linux", "x64", "demo"}
	// The names are chosen so that "acme-pool" sorts first: the old tie-break
	// was the pool name, so a test whose expected pool sorts first would pass
	// on the broken code as well.
	a := h.installationOn("acme", store.TargetOrg)
	g := h.installationOn("globex", store.TargetOrg)
	h.host("vm-1")
	return h.pool(a, "acme-pool", labels...), h.pool(g, "globex-pool", labels...)
}

// jitTargets is the organisation of every runner registration minted against
// GitHub so far, which is what says where a runner was actually created.
func jitTargets(h *harness) []string {
	h.t.Helper()
	var out []string
	for _, r := range h.gh.Requests() {
		path, ok := strings.CutPrefix(r, "POST /orgs/")
		if !ok {
			continue
		}
		if org, rest, found := strings.Cut(path, "/"); found && strings.Contains(rest, "generate-jitconfig") {
			out = append(out, org)
		}
	}
	return out
}

func TestAWebhookJobIsClaimedByThePoolOnItsOwnInstallation(t *testing.T) {
	h := newHarness(t)
	_, globex := twoInstallations(h)

	rec := h.deliverJob(jobEvent{
		Action: "queued", JobID: 7001, Repo: "globex/thing",
		Labels: []string{"self-hosted", "linux", "x64", "demo"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	job, err := h.st.GetJobByGitHubID(h.ctx, 7001)
	if err != nil {
		t.Fatalf("the delivery recorded no job: %v", err)
	}
	if job.InstallationID != globex.InstallationID {
		t.Fatalf("job installation = %q, want globex's %q", job.InstallationID, globex.InstallationID)
	}
	if job.PoolID != globex.ID || !job.Matched {
		t.Fatalf("job = %+v, want it claimed by %s", job, globex.Name)
	}

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	runners := h.runners()
	if len(runners) != 1 || runners[0].PoolID != globex.ID {
		t.Fatalf("runners = %+v, want one in %s", runners, globex.Name)
	}
	for _, org := range jitTargets(h) {
		if org != "globex" {
			t.Fatalf("a runner was registered in %q for a job in globex/thing", org)
		}
	}
}

func TestAPolledJobIsClaimedByThePoolOnTheInstallationItCameFrom(t *testing.T) {
	h := newHarness(t)
	// Repository-target installations, so each one polls only its own
	// repository and the two queues cannot be confused for one another.
	acme := h.installationOn("acme/widgets", store.TargetRepo)
	globex := h.installationOn("globex/thing", store.TargetRepo)
	h.host("vm-1")
	labels := []string{"self-hosted", "linux", "x64", "demo"}
	acmePool := h.pool(acme, "acme-pool", labels...)
	globexPool := h.pool(globex, "globex-pool", labels...)

	acmeJob := h.gh.AddQueuedJob("acme/widgets", "CI", "build", labels)
	globexJob := h.gh.AddQueuedJob("globex/thing", "CI", "build", labels)

	h.c.pollOnce(h.ctx)

	for _, tc := range []struct {
		name string
		id   int64
		pool *store.Pool
	}{
		{"acme", acmeJob.ID, acmePool},
		{"globex", globexJob.ID, globexPool},
	} {
		job, err := h.st.GetJobByGitHubID(h.ctx, tc.id)
		if err != nil {
			t.Fatalf("%s: the poller recorded no job: %v", tc.name, err)
		}
		if job.InstallationID != tc.pool.InstallationID {
			t.Errorf("%s: job installation = %q, want %q", tc.name, job.InstallationID, tc.pool.InstallationID)
		}
		if job.PoolID != tc.pool.ID || !job.Matched {
			t.Errorf("%s: job = %+v, want it claimed by %s", tc.name, job, tc.pool.Name)
		}
	}
}

func TestTheSchedulerNeverPlacesAJobOnAnotherInstallationsPool(t *testing.T) {
	h := newHarness(t)
	acmePool, globexPool := twoInstallations(h)

	// A queued job in globex, written straight to the store: this is the
	// scheduler's own path, with no ingest in the way.
	if _, err := h.st.UpsertJob(h.ctx, &store.Job{
		GitHubJobID: 7002, Repo: "globex/thing", Workflow: "CI", JobName: "build",
		Labels: store.StringSlice{"self-hosted", "linux", "x64", "demo"},
		State:  store.JobQueued, InstallationID: globexPool.InstallationID,
	}); err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	for _, r := range h.runners() {
		if r.PoolID == acmePool.ID {
			t.Fatalf("a runner was created in %s for a job in globex", acmePool.Name)
		}
	}
	if got := len(h.runners()); got != 1 {
		t.Fatalf("created %d runners, want 1 in %s", got, globexPool.Name)
	}
}

// A job whose repository no installation covers is recorded and left unclaimed
// rather than rejected: Zoomies cannot mint a runner for a target it holds no
// credentials for, and a delivery it drops is a job that disappears.
func TestAJobNoInstallationCoversIsRecordedAndUnclaimed(t *testing.T) {
	h := newHarness(t)
	_, pool, _ := h.fleet()

	rec := h.deliverJob(jobEvent{
		Action: "queued", JobID: 7003, Repo: "other-org/thing",
		Labels: []string{"self-hosted", "linux", "x64", "demo"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	job, err := h.st.GetJobByGitHubID(h.ctx, 7003)
	if err != nil {
		t.Fatalf("the delivery recorded no job: %v", err)
	}
	if job.InstallationID != "" {
		t.Errorf("job installation = %q, want none: no installation covers other-org", job.InstallationID)
	}
	if job.Matched || job.PoolID == pool.ID {
		t.Errorf("job = %+v, want it unclaimed: %s cannot run work in another target", job, pool.Name)
	}
}

// The Jobs page shows an unclaimed job the same way whatever refused it, so
// the problems drawer is where the difference has to be said: "no pool
// advertises those labels" and "a pool advertises exactly those labels, in
// another GitHub target" need opposite fixes.
func TestTheProblemsDrawerNamesThePoolOnTheOtherInstallation(t *testing.T) {
	h := newHarness(t)
	acme, globex := twoInstallations(h)

	// Globex's pool is the only one whose labels fit, and it is the wrong
	// target for this job.
	if _, err := h.st.DeletePool(h.ctx, acme.ID); err != nil {
		t.Fatalf("DeletePool: %v", err)
	}
	if _, err := h.st.UpsertJob(h.ctx, &store.Job{
		GitHubJobID: 7004, Repo: "acme/widgets", Workflow: "CI", JobName: "build",
		Labels: store.StringSlice{"self-hosted", "linux", "x64", "demo"},
		State:  store.JobQueued, InstallationID: acme.InstallationID,
		QueuedAt: time.Now().Add(-unmatchedGrace - time.Minute),
	}); err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	ps, err := h.c.Problems(h.ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	var found *Problem
	for i, p := range ps {
		if p.Code == "jobs.unmatched" {
			found = &ps[i]
		}
	}
	if found == nil {
		t.Fatalf("problems = %v, want the unmatched-job warning", h.problemCodes())
	}
	if !strings.Contains(found.Detail, globex.Name) || !strings.Contains(found.Detail, "installation globex") {
		t.Errorf("detail = %q, want it to name %s and the installation it belongs to", found.Detail, globex.Name)
	}
	if strings.Contains(found.Fix, "runs-on") {
		t.Errorf("fix = %q, want it to point at the installation rather than the workflow", found.Fix)
	}
}

// An organisation installation and a repository one can both cover the same
// repository. Only one of them owns a job in it, and both ingest paths have to
// agree on which: if the poller kept the installation it polled while the
// webhook path resolved the repository, the job's installation would change
// with every delivery and its pool with it.
func TestBothIngestPathsAgreeOnTheInstallationThatOwnsAJob(t *testing.T) {
	h := newHarness(t)
	org := h.installationOn("acme", store.TargetOrg)
	repo := h.installationOn("acme/widgets", store.TargetRepo)
	h.host("vm-1")
	labels := []string{"self-hosted", "linux", "x64", "demo"}
	h.pool(org, "acme-pool", labels...)
	repoPool := h.pool(repo, "widgets-pool", labels...)

	// Ingested as the organisation's installation, which is what a poll of it
	// returns; the repository's installation is the one that owns the job.
	// Ingest is driven directly rather than through pollOnce, because pollOnce
	// polls both installations and the second would paper over the first.
	polled := h.gh.AddQueuedJob("acme/widgets", "CI", "build", labels)
	if _, err := h.c.ingestQueuedJobs(h.ctx, org, []github.QueuedJob{{
		ID: polled.ID, RunID: polled.RunID, Repo: "acme/widgets",
		WorkflowName: "CI", JobName: "build", Labels: labels,
	}}); err != nil {
		t.Fatalf("ingestQueuedJobs: %v", err)
	}

	job, err := h.st.GetJobByGitHubID(h.ctx, polled.ID)
	if err != nil {
		t.Fatalf("the poller recorded no job: %v", err)
	}
	if job.InstallationID != repo.ID {
		t.Fatalf("polled job installation = %q, want the repository's %q", job.InstallationID, repo.ID)
	}
	if job.PoolID != repoPool.ID {
		t.Fatalf("polled job pool = %q, want %s", job.PoolID, repoPool.Name)
	}

	// And the webhook path, on a job in the same repository, says the same.
	h.deliverJob(jobEvent{
		Action: "queued", JobID: 7005, Repo: "acme/widgets", Labels: labels,
	})
	delivered, err := h.st.GetJobByGitHubID(h.ctx, 7005)
	if err != nil {
		t.Fatalf("the delivery recorded no job: %v", err)
	}
	if delivered.InstallationID != job.InstallationID {
		t.Fatalf("the two paths disagree: webhook says %q, poller said %q",
			delivered.InstallationID, job.InstallationID)
	}
}

// The timeline is read by the same operator the problems drawer is, so an
// unclaimed job must not be told two different stories about why.
func TestTheTimelineSaysWhenNoInstallationCoversTheRepository(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	h.deliverJob(jobEvent{
		Action: "queued", JobID: 7006, Repo: "other-org/thing",
		Labels: []string{"self-hosted", "linux", "x64", "demo"},
	})
	job, err := h.st.GetJobByGitHubID(h.ctx, 7006)
	if err != nil {
		t.Fatalf("the delivery recorded no job: %v", err)
	}
	events, err := h.st.ListJobEvents(h.ctx, job.ID)
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	var said string
	for _, e := range events {
		if e.Kind == store.JobEventUnmatched {
			said = e.Message
		}
	}
	if !strings.Contains(said, "no GitHub App installation here covers other-org/thing") {
		t.Fatalf("the unmatched entry says %q, want it to name the uncovered repository", said)
	}
}

// twoPolledInstallations seeds two repository-target installations, so each
// polls only its own repository and the two queues cannot be confused.
func twoPolledInstallations(h *harness) (acme, globex *store.Installation) {
	h.t.Helper()
	labels := []string{"self-hosted", "linux", "x64", "demo"}
	a := h.installationOn("acme/widgets", store.TargetRepo)
	g := h.installationOn("globex/thing", store.TargetRepo)
	h.host("vm-1")
	h.pool(a, "acme-pool", labels...)
	h.pool(g, "globex-pool", labels...)
	h.gh.AddQueuedJob("acme/widgets", "CI", "build", labels)
	h.gh.AddQueuedJob("globex/thing", "CI", "build", labels)
	return a, g
}

// polledRepos is the repositories this sweep asked GitHub about, which is what
// says which installations were actually polled.
func polledRepos(h *harness, since int) map[string]bool {
	h.t.Helper()
	out := map[string]bool{}
	for _, r := range h.gh.Requests()[since:] {
		for _, repo := range []string{"acme/widgets", "globex/thing"} {
			if strings.Contains(r, "/repos/"+repo+"/") {
				out[repo] = true
			}
		}
	}
	return out
}

// GitHub's quota is per installation. A sweep that abandoned the rest of the
// fleet on the first rate-limited installation would let one organisation out
// of quota stop every other one from scaling -- and the poller is the fallback
// that exists precisely for the installation whose webhooks are not arriving.
func TestARateLimitedInstallationDoesNotStopTheOthersBeingPolled(t *testing.T) {
	h := newHarness(t)
	acme, globex := twoPolledInstallations(h)
	// A secondary rate limit, which GitHub reports as a 429 on the call it
	// refuses. The primary form is a 403 plus an exhausted quota in the
	// response headers, and those headers are what go-github caches per
	// client -- setting them on the fake would rate-limit every installation
	// at once and prove nothing about isolation.
	h.gh.SetError("/repos/acme/widgets/", 429, "You have exceeded a secondary rate limit")

	before := len(h.gh.Requests())
	h.c.pollOnce(h.ctx)

	if !h.c.pollHeld(acme.ID, time.Now()) {
		t.Error("the rate-limited installation was not stood down")
	}
	if h.c.pollHeld(globex.ID, time.Now()) {
		t.Error("the installation GitHub did not rate-limit was stood down with it")
	}
	if asked := polledRepos(h, before); !asked["globex/thing"] {
		t.Errorf("globex was never polled: %v", asked)
	}

	// And the next sweep skips the held installation while still polling the
	// other, rather than the hold covering the whole fleet.
	before = len(h.gh.Requests())
	h.c.pollOnce(h.ctx)
	asked := polledRepos(h, before)
	if asked["acme/widgets"] {
		t.Error("the held installation was polled again inside its backoff")
	}
	if !asked["globex/thing"] {
		t.Errorf("the other installation stopped being polled: %v", asked)
	}
}

// Standing down because webhooks are arriving is what makes the poller cheap
// enough to leave on. Asked fleet-wide, one organisation's working deliveries
// would silence the poller for an organisation whose webhooks reach nobody --
// which is the exact failure the poller is the safety net for.
func TestAFreshInstallationDoesNotSilenceThePollerForASilentOne(t *testing.T) {
	h := newHarness(t)
	_, _ = twoPolledInstallations(h)

	if err := h.st.RecordDelivery(h.ctx, &store.WebhookDelivery{
		DeliveryID: "recent", Event: "workflow_job", Repo: "acme/widgets",
		Status: "accepted", ReceivedAt: time.Now(),
	}); err != nil {
		t.Fatalf("RecordDelivery: %v", err)
	}

	before := len(h.gh.Requests())
	h.c.pollOnce(h.ctx)

	asked := polledRepos(h, before)
	if asked["acme/widgets"] {
		t.Error("an installation whose webhooks are arriving was polled anyway")
	}
	if !asked["globex/thing"] {
		t.Errorf("the installation no webhook has ever arrived for was not polled: %v", asked)
	}
}

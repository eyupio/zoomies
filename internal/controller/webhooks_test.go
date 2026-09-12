package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// This is the product's core promise: a job is queued on GitHub, and a runner
// appears on a host to run it.
func TestQueuedJobWebhookProducesARunnerAndACreateTask(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()

	rec := h.deliverJob(jobEvent{
		Action: "queued", JobID: 1001, RunID: 5001,
		Name: "build", Workflow: "CI",
		Labels: []string{"self-hosted", "linux", "x64", "demo"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want %d (%s)", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	job, err := h.st.GetJobByGitHubID(h.ctx, 1001)
	if err != nil {
		t.Fatalf("the delivery did not produce a job row: %v", err)
	}
	if job.State != store.JobQueued || !job.Matched || job.PoolID != pool.ID {
		t.Fatalf("job = %+v, want queued and matched to pool %s", job, pool.ID)
	}

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	r := h.onlyRunner()
	if r.State != store.RunnerProvisioning {
		t.Fatalf("runner state = %q, want %q", r.State, store.RunnerProvisioning)
	}
	if r.HostID != host.ID || r.PoolID != pool.ID {
		t.Fatalf("runner placed on host %s in pool %s, want %s/%s", r.HostID, r.PoolID, host.ID, pool.ID)
	}
	if !store.IsRunnerName(r.Name) {
		t.Fatalf("runner name %q does not carry the %q prefix", r.Name, store.RunnerNamePrefix)
	}
	if r.GitHubRunnerID == 0 {
		t.Fatal("the runner has no GitHub runner ID, so no JIT config was minted for it")
	}

	task := h.taskOfKind(host.ID, agent.TaskCreateRunner)
	if task.RunnerID != r.ID {
		t.Fatalf("create task is for runner %s, want %s", task.RunnerID, r.ID)
	}
	if task.Spec == nil || task.Spec.Credentials.JITConfig == "" {
		t.Fatal("the create task carries no JIT configuration, so the agent could not register the runner")
	}
	if task.Spec.Name != r.Name || task.Backend != pool.Backend {
		t.Fatalf("spec = %+v, want name %s on the %s backend", task.Spec, r.Name, pool.Backend)
	}
	if task.Spec.PullPolicy != pool.PullPolicy {
		t.Fatalf("create task pull policy = %q, want pool policy %q", task.Spec.PullPolicy, pool.PullPolicy)
	}
}

func TestWebhookWithABadSignatureIsRejectedAndRecorded(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	body := jobEvent{
		Action: "queued", JobID: 2002,
		Labels: []string{"self-hosted", "linux", "x64", "demo"},
	}.body()
	rec := h.deliver("workflow_job", body, "the-wrong-secret")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if _, err := h.st.GetJobByGitHubID(h.ctx, 2002); err == nil {
		t.Fatal("a delivery that failed verification created a job row")
	}

	ds := h.deliveries()
	if len(ds) != 1 {
		t.Fatalf("recorded %d deliveries, want 1", len(ds))
	}
	if ds[0].Status != "rejected" {
		t.Fatalf("delivery status = %q, want rejected", ds[0].Status)
	}
	if ds[0].Error == "" {
		t.Fatal("a rejected delivery was recorded without saying why")
	}
}

func TestUnsignedWebhookIsRejected(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	rec := h.deliver("workflow_job", jobEvent{Action: "queued", JobID: 3003}.body(), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; an unsigned delivery lets anyone start runners", rec.Code, http.StatusUnauthorized)
	}
}

// A delivery for a repository no installation covers is still verified against
// every secret Zoomies holds, and the delivery records that it had to.
func TestWebhookForAnUnknownRepositoryTriesEverySecret(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	rec := h.deliverJob(jobEvent{
		Action: "queued", JobID: 4004, Repo: "other-org/thing",
		Labels: []string{"self-hosted", "linux", "x64", "demo"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	ds := h.deliveries()
	if len(ds) != 1 || ds[0].Status != "accepted" {
		t.Fatalf("deliveries = %+v, want one accepted", ds)
	}
	if !strings.Contains(ds[0].Error, "no installation covers") {
		t.Fatalf("the delivery does not record that no installation matched: %q", ds[0].Error)
	}
}

func TestPingIsAccepted(t *testing.T) {
	h := newHarness(t)
	h.installation()

	rec := h.deliver("ping", []byte(`{"zen":"Anything added dilutes everything else.","hook_id":1,"repository":{"full_name":"acme/widgets"}}`), testWebhookSecret)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	ds := h.deliveries()
	if len(ds) != 1 || ds[0].Status != "accepted" || ds[0].Event != "ping" {
		t.Fatalf("deliveries = %+v, want one accepted ping", ds)
	}
	if h.c.PollingOnly() {
		t.Fatal("a delivery arrived, so the controller is no longer polling-only")
	}
}

func TestOnlyPostIsAccepted(t *testing.T) {
	h := newHarness(t)
	req := newGetRequest()
	rec := recorder()
	h.c.HandleWebhook(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// Deliveries are at-least-once and can arrive out of order. A late "queued"
// must not resurrect a job that has already finished, or the scheduler would
// create a runner for work that is over.
func TestLateQueuedDeliveryDoesNotRewindAJob(t *testing.T) {
	h := newHarness(t)
	h.fleet()
	labels := []string{"self-hosted", "linux", "x64", "demo"}
	queued := time.Now().Add(-5 * time.Minute)

	h.deliverJob(jobEvent{Action: "completed", JobID: 5005, Labels: labels, Conclusion: "success", QueuedAt: queued})
	h.deliverJob(jobEvent{Action: "queued", JobID: 5005, Labels: labels, QueuedAt: queued})

	job, err := h.st.GetJobByGitHubID(h.ctx, 5005)
	if err != nil {
		t.Fatalf("GetJobByGitHubID: %v", err)
	}
	if job.State != store.JobCompleted {
		t.Fatalf("job state = %q, want %q; the late queued delivery rewound it", job.State, store.JobCompleted)
	}
	if job.Conclusion != "success" {
		t.Fatalf("conclusion = %q, want success", job.Conclusion)
	}

	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rs := h.runners(); len(rs) != 0 {
		t.Fatalf("created %d runners for a job that had already completed", len(rs))
	}
}

// An in_progress delivery is what links a job to the runner GitHub gave it to.
func TestInProgressDeliveryLinksTheRunner(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	labels := []string{"self-hosted", "linux", "x64", "demo"}

	h.deliverJob(jobEvent{Action: "queued", JobID: 6006, Labels: labels})
	if err := h.c.Reconcile(h.ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	r := h.onlyRunner()
	// The agent brings it up and stops asserting a state once it is running.
	mustReport(t, h, host.ID, r.ID, store.RunnerRegistering)
	mustReportRunning(t, h, host.ID, r.ID)

	h.deliverJob(jobEvent{Action: "in_progress", JobID: 6006, Labels: labels, RunnerName: r.Name})

	job, err := h.st.GetJobByGitHubID(h.ctx, 6006)
	if err != nil {
		t.Fatalf("GetJobByGitHubID: %v", err)
	}
	if job.RunnerID != r.ID {
		t.Fatalf("job.RunnerID = %q, want %q", job.RunnerID, r.ID)
	}
	after, err := h.st.GetRunner(h.ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRunner: %v", err)
	}
	if after.State != store.RunnerBusy {
		t.Fatalf("runner state = %q, want %q", after.State, store.RunnerBusy)
	}
	if after.CurrentJobID != job.ID {
		t.Fatalf("runner.CurrentJobID = %q, want %q", after.CurrentJobID, job.ID)
	}
}

func TestCompletedJobExplicitlyRemovesItsEphemeralRunner(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	labels := []string{"self-hosted", "linux", "x64", "demo"}
	job, runner := startJobOnRunner(t, h, host.ID, 6010, labels)

	// Recovery responses are allowed to omit runner_name; the durable link
	// from the in-progress event must still lead completion to this runner.
	h.deliverJob(jobEvent{Action: "completed", JobID: 6010, Name: "test", Workflow: "CI",
		Labels: labels, Conclusion: "success"})

	finished := h.runnerByID(t, runner.ID)
	if finished.State != store.RunnerRemoved || finished.CurrentJobID != "" {
		t.Fatalf("runner after completion = state %q, current job %q; want removed with no job",
			finished.State, finished.CurrentJobID)
	}
	if finished.JobsHandled != 1 {
		t.Fatalf("jobs handled = %d, want 1", finished.JobsHandled)
	}
	if !h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("job completion did not queue removal of the ephemeral workload")
	}
	got, err := h.st.GetJob(h.ctx, job.ID)
	if err != nil || got.State != store.JobCompleted || got.Conclusion != "success" {
		t.Fatalf("completed job = %+v, err = %v", got, err)
	}

	// GitHub redelivers completion events. The job guard prevents a second
	// retirement or a second jobs-handled count.
	h.deliverJob(jobEvent{Action: "completed", JobID: 6010, Name: "test", Workflow: "CI",
		Labels: labels, RunnerName: runner.Name, Conclusion: "success"})
	if again := h.runnerByID(t, runner.ID); again.JobsHandled != 1 {
		t.Fatalf("duplicate completion counted %d jobs, want 1", again.JobsHandled)
	}
}

func TestCompletedJobReturnsAPersistentRunnerToIdle(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	pool.Ephemeral = false
	if err := h.st.UpdatePool(h.ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	labels := []string{"self-hosted", "linux", "x64", "demo"}
	_, runner := startJobOnRunner(t, h, host.ID, 6011, labels)

	h.deliverJob(jobEvent{Action: "completed", JobID: 6011, Name: "test", Workflow: "CI",
		Labels: labels, RunnerName: runner.Name, Conclusion: "success"})

	finished := h.runnerByID(t, runner.ID)
	if finished.State != store.RunnerIdle || finished.CurrentJobID != "" || finished.JobsHandled != 1 {
		t.Fatalf("persistent runner after completion = state %q, current job %q, jobs %d; want idle, empty, 1",
			finished.State, finished.CurrentJobID, finished.JobsHandled)
	}
	if h.hasTaskOfKind(host.ID, agent.TaskRemoveRunner) {
		t.Fatal("completion queued removal of a persistent runner")
	}
}

// A delivery that verifies but is not a workflow_job event this controller can
// read is not a failure on this side. It used to be answered with a 500, which
// is the status that asks GitHub to redeliver, so a malformed body came back
// for ever.
func TestAMalformedWorkflowJobIsRejectedNotRetried(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	rec := h.deliver("workflow_job", []byte(`{"action":"queued","workflow_job":"not an object"}`), testWebhookSecret)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d so GitHub does not redeliver", rec.Code, http.StatusBadRequest)
	}
	ds := h.deliveries()
	if len(ds) != 1 || ds[0].Status != "rejected" || ds[0].Error == "" {
		t.Fatalf("deliveries = %+v, want one rejected delivery that says why", ds)
	}
}

// TestADeliveryIsNotVerifiedWithAnotherInstallationsSecret covers the boundary
// between two organisations sharing one controller.
//
// Every configured webhook secret is held by somebody: the GitHub admin of each
// organisation pasted their own into their own App. Trying them all against a
// delivery whose repository already has an installation meant the holder of one
// organisation's secret could sign a workflow_job naming another's repository
// and be believed -- and the job that follows is scheduled on the named
// organisation's pools and registers a runner in it, using its App credentials,
// not the sender's.
func TestADeliveryIsNotVerifiedWithAnotherInstallationsSecret(t *testing.T) {
	h := newHarness(t)
	h.fleet() // acme, signing with testWebhookSecret

	// A second organisation on the same controller, with a secret of its own.
	beta := h.installationOn("beta", store.TargetOrg)
	const betaSecret = "beta-organisations-own-webhook-secret"
	sealed, err := h.key.SealString(betaSecret)
	if err != nil {
		t.Fatalf("sealing beta's secret: %v", err)
	}
	beta.WebhookSecretEnc = sealed
	if err := h.st.UpdateInstallation(h.ctx, beta); err != nil {
		t.Fatalf("UpdateInstallation: %v", err)
	}

	// beta signs a delivery that names one of acme's repositories.
	rec := h.deliver("workflow_job", jobEvent{
		Action: "queued", JobID: 5005, Repo: "acme/widgets",
		Labels: []string{"self-hosted", "linux", "x64", "demo"},
	}.body(), betaSecret)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: beta's secret verified a delivery for acme's repository",
			rec.Code, http.StatusUnauthorized)
	}
	if _, err := h.st.GetJobByGitHubID(h.ctx, 5005); err == nil {
		t.Error("a forged delivery created a job in the other organisation's fleet")
	}
	ds := h.deliveries()
	if len(ds) != 1 || ds[0].Status != "rejected" {
		t.Fatalf("deliveries = %+v, want one rejected", ds)
	}
}

// TestAnOwnedRepositorysDeliveryStillNeedsItsOwnSecret is the same rule stated
// from the other side, and the one an operator meets: acme's own delivery,
// signed with a secret that is simply wrong, is refused rather than quietly
// checked against everything else the controller holds.
func TestAnOwnedRepositorysDeliveryStillNeedsItsOwnSecret(t *testing.T) {
	h := newHarness(t)
	h.fleet()
	h.installationOn("beta", store.TargetOrg) // holds testWebhookSecret too

	rec := h.deliver("workflow_job", jobEvent{
		Action: "queued", JobID: 6006, Repo: "acme/widgets",
		Labels: []string{"self-hosted", "linux", "x64", "demo"},
	}.body(), "not-anybodys-secret")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestARejectedDeliveryIsNotProofThatWebhooksWork covers the diagnostic that
// tells an operator nothing is scaling.
//
// The webhook endpoint is public and unauthenticated by necessity, so anybody
// can put a row in the delivery table. Gating the warning on "a delivery
// arrived" rather than "a delivery verified" meant one probe from a passing
// scanner took the message off the screen for the whole retention window --
// including its error-severity form, which is the only thing that says no
// queued job will ever be noticed.
func TestARejectedDeliveryIsNotProofThatWebhooksWork(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	problemPresent := func() bool {
		t.Helper()
		ps, err := h.c.Problems(h.ctx)
		if err != nil {
			t.Fatalf("Problems: %v", err)
		}
		for _, p := range ps {
			if p.Code == "webhook.never_received" {
				return true
			}
		}
		return false
	}

	if !problemPresent() {
		t.Fatal("a fleet that has never had a webhook does not report webhook.never_received")
	}

	// A stranger probes the endpoint. It is rejected, and recorded.
	rec := h.deliver("workflow_job", jobEvent{Action: "queued", JobID: 7007}.body(), "not-the-secret")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("probe status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if ds := h.deliveries(); len(ds) != 1 || ds[0].Status != "rejected" {
		t.Fatalf("deliveries = %+v, want one rejected", ds)
	}

	if !problemPresent() {
		t.Error("a rejected delivery silenced webhook.never_received: an unauthenticated stranger can hide the fact that nothing is scaling")
	}

	// A delivery that actually verifies is what clears it.
	if rec := h.deliverJob(jobEvent{
		Action: "queued", JobID: 7008,
		Labels: []string{"self-hosted", "linux", "x64", "demo"},
	}); rec.Code != http.StatusAccepted {
		t.Fatalf("accepted delivery status = %d, want %d (%s)", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if problemPresent() {
		t.Error("webhook.never_received survived a delivery that verified")
	}
}

// Nothing an unverified delivery says about itself is written down whole.
//
// The envelope is parsed before the signature is checked, because the
// repository is what chooses the secret to check it with. So the repository
// name, the action and both headers arrive from an unauthenticated request and
// are then kept: a database row, a line in the log, and a frame on the event
// stream every watching browser holds in memory. Unbounded, that is five
// megabytes of "repository.full_name" per request from anyone who can reach the
// endpoint, with no credential of any kind.
func TestAnUnverifiedDeliveryCannotWriteDownWhateverItLikes(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	huge := strings.Repeat("A", 2<<20)
	body := []byte(`{"action":"` + huge + `","repository":{"full_name":"` + huge + `"}}`)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.Header.Set(github.EventTypeHeader, "workflow_job")
	req.Header.Set(github.DeliveryIDHeader, huge)
	rec := httptest.NewRecorder()
	h.c.HandleWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	ds := h.deliveries()
	if len(ds) != 1 {
		t.Fatalf("recorded %d deliveries, want 1", len(ds))
	}
	d := ds[0]
	for _, f := range []struct {
		name  string
		value string
		max   int
	}{
		{"repo", d.Repo, maxDeliveryRepo},
		{"delivery id", d.DeliveryID, maxDeliveryID},
		{"event", d.Event, maxDeliveryEvent},
	} {
		// The clamp says what it cut, so the allowance is the limit plus that
		// note rather than the limit exactly.
		if len(f.value) > f.max+64 {
			t.Errorf("the recorded %s is %d bytes; an unverified delivery chose that", f.name, len(f.value))
		}
	}
	// The reason a delivery was rejected quotes the repository back, so it has
	// to be bounded too or the clamp is undone by the error beside it.
	if len(d.Error) > 4096 {
		t.Errorf("the recorded rejection reason is %d bytes", len(d.Error))
	}
}

// A stream of unverifiable deliveries from one address is bounded in what it
// writes down, without any of it reaching a delivery that verifies.
func TestAFloodOfProbesStopsBeingWrittenDown(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	body := jobEvent{Action: "queued", JobID: 4004}.body()
	for i := 0; i < 200; i++ {
		if rec := h.deliver("workflow_job", body, "the-wrong-secret"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("probe %d: status = %d, want %d; the refusal itself must not change", i, rec.Code, http.StatusUnauthorized)
		}
	}
	// Counted straight out of the store, at the largest page it will serve.
	// The harness helper pages at 100, which would hide exactly the failure
	// this test is for -- and ListDeliveries silently falls back to 50 for any
	// limit above 500, so asking for more than it allows hides it just as well.
	ds, err := h.st.ListDeliveries(h.ctx, "", 500)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(ds) == 0 {
		t.Fatal("no probe was recorded at all; a burst is worth knowing about")
	}
	if len(ds) > 100 {
		t.Errorf("recorded %d of 200 probes, so one address can still fill the database", len(ds))
	}

	// And the fleet still works: a real delivery is never touched by this.
	if rec := h.deliverJob(jobEvent{
		Action: "queued", JobID: 4005,
		Labels: []string{"self-hosted", "linux", "x64", "demo"},
	}); rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
		t.Fatalf("a signed delivery after the flood = %d; GitHub must never be throttled here", rec.Code)
	}
	if _, err := h.st.GetJobByGitHubID(h.ctx, 4005); err != nil {
		t.Errorf("the signed delivery did not create its job: %v", err)
	}
}

// A delivery with no signature header cannot verify, so it is refused before
// its body is read. Reading, parsing and checking a body against every
// installation's secret is the expensive part of the endpoint, and the
// endpoint is public: a probe used to get all of that for free, and a large
// unsigned body was answered as too large rather than as unsigned, which told
// the prober something and cost the controller five megabytes to say it.
func TestAnUnsignedDeliveryIsRefusedBeforeItsBodyIsRead(t *testing.T) {
	h := newHarness(t)
	h.fleet()

	body := make([]byte, maxWebhookBody+1)
	rec := h.deliver("workflow_job", body, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: an unsigned delivery is unsigned before it is too large", rec.Code, http.StatusUnauthorized)
	}
	ds := h.deliveries()
	if len(ds) != 1 || ds[0].Status != "rejected" {
		t.Fatalf("deliveries = %+v, want one rejected", ds)
	}
	if !strings.Contains(ds[0].Error, github.SignatureHeader) {
		t.Fatalf("the record does not name the missing header: %q", ds[0].Error)
	}
	if ds[0].Repo != "" {
		t.Fatalf("the body was parsed for a delivery that could never verify: repo = %q", ds[0].Repo)
	}
}

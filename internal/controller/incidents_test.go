package controller

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/store"
)

func (h *harness) reread(t *testing.T, id string) *store.Host {
	t.Helper()
	host, err := h.st.GetHost(h.ctx, id)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	return host
}

func (h *harness) beat(t *testing.T, id string, rt *agent.RuntimeReport) {
	t.Helper()
	if _, err := h.c.Heartbeat(h.ctx, id, agent.HeartbeatRequest{ProtocolVersion: agent.ProtocolVersion, Runtime: rt}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
}

func hasCode(codes []string, code string) bool {
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

// A runtime that keeps falling over used to be a log line on the host, and
// the Hosts page showed a healthy machine that was oddly slow to start
// runners. One beat is enough to put it on the card and in the drawer, with
// when the recovery attempt is due, and a controller restart does not lose it.
func TestAHeartbeatCarryingARuntimeCooldownIsShownUntilASuccessClearsIt(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()

	h.beat(t, host.ID, &agent.RuntimeReport{Failures: 3, Kind: agent.RuntimeUnavailable,
		Error: "backend: not available on this host: no socket at /var/run/docker.sock", RetryIn: 40 * time.Second})
	beatAt := h.c.Now()

	view := h.c.HostView(h.reread(t, host.ID))
	inc := view.RuntimeRecovering
	if inc == nil {
		t.Fatal("the host view does not carry the runtime cooldown")
	}
	if inc.Failures != 3 || inc.Kind != agent.RuntimeUnavailable {
		t.Fatalf("recorded %+v, want three unavailable failures", inc)
	}
	// The retry is on the controller's clock, forty seconds from the beat.
	if d := inc.RetryAt.Sub(beatAt); d < 39*time.Second || d > 41*time.Second {
		t.Fatalf("retry at %s is %s after the beat, want about 40s", inc.RetryAt, d)
	}
	if inc.ObservedAt.IsZero() || inc.Since.IsZero() {
		t.Fatalf("the incident carries no age: %+v", inc)
	}
	if !strings.Contains(view.RuntimeReason, "third failure") {
		t.Fatalf("the card's sentence does not say which failure this is: %q", view.RuntimeReason)
	}
	p := h.problem(t, "host.runtime_recovering")
	if p.TargetID != host.ID || p.Since == nil || !strings.Contains(p.Detail, incidentTime(inc.RetryAt)) ||
		!strings.Contains(p.Fix, "systemctl start docker") || p.Audience != AudienceFleet {
		t.Fatalf("the drawer entry does not name the host, its age, the retry time and the fix: %+v", p)
	}
	if got, ok := gatherValue(t, h.c, "zoomies_host_runtime_recovering", map[string]string{"host": host.ID}); !ok || got != 1 {
		t.Fatalf("zoomies_host_runtime_recovering = %v (%v), want 1", got, ok)
	}
	if got, ok := gatherValue(t, h.c, "zoomies_host_runtime_failures_total", map[string]string{"kind": "unavailable"}); !ok || got != 3 {
		t.Fatalf("zoomies_host_runtime_failures_total = %v (%v), want 3", got, ok)
	}

	// The same report again is not news: the row keeps when it was learnt.
	h.advance(10 * time.Second)
	h.beat(t, host.ID, &agent.RuntimeReport{Failures: 3, Kind: agent.RuntimeUnavailable, RetryIn: 30 * time.Second})
	if again := h.reread(t, host.ID).Incidents.Runtime; again == nil || !again.ObservedAt.Equal(inc.ObservedAt) {
		t.Fatalf("a beat with nothing new rewrote the incident: %+v", again)
	}

	// The controller restarts: the row is all that is left, and it is enough.
	h.c = h.restart()
	if !hasCode(h.problemCodes(), "host.runtime_recovering") {
		t.Fatal("a controller restart lost the runtime cooldown")
	}

	// A further failure moves it on and keeps when the episode began.
	h.beat(t, host.ID, &agent.RuntimeReport{Failures: 4, Kind: agent.RuntimeUnavailable, RetryIn: time.Minute})
	next := h.reread(t, host.ID).Incidents.Runtime
	if next == nil || next.Failures != 4 || !next.Since.Equal(inc.Since) {
		t.Fatalf("a fourth failure was recorded as %+v", next)
	}
	// And a different kind of failure is news even at the same count.
	h.beat(t, host.ID, &agent.RuntimeReport{Failures: 4, Kind: agent.RuntimeTimeout, RetryIn: time.Minute})
	if next := h.reread(t, host.ID).Incidents.Runtime; next == nil || next.Kind != agent.RuntimeTimeout {
		t.Fatalf("a slow daemon after an absent one was recorded as %+v", next)
	}
	if fix := h.problem(t, "host.runtime_recovering").Fix; !strings.Contains(fix, "not answering in time") {
		t.Fatalf("a slow daemon was given the fix for a missing one: %q", fix)
	}

	// The next beat without a report is the runtime working again.
	h.beat(t, host.ID, nil)
	if got := h.reread(t, host.ID); got.Incidents.Runtime != nil || h.c.HostView(got).RuntimeRecovering != nil {
		t.Fatalf("a recovered runtime is still shown: %+v", got.Incidents.Runtime)
	}
	if hasCode(h.problemCodes(), "host.runtime_recovering") {
		t.Fatal("a recovered runtime is still in the drawer")
	}
}

// An agent is not trusted with the retry time any more than with its clock: a
// report of an hour's wait would read as a host nobody need look at.
func TestARuntimeReportIsBoundedBeforeItIsStored(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	h.beat(t, host.ID, &agent.RuntimeReport{Failures: 1, Kind: "gremlins", RetryIn: 10 * time.Hour,
		Error: strings.Repeat("x", 10*maxIncidentError)})
	inc := h.reread(t, host.ID).Incidents.Runtime
	if inc == nil {
		t.Fatal("the report was dropped")
	}
	if inc.Kind != agent.RuntimeUnavailable {
		t.Errorf("an unknown kind was stored as %q", inc.Kind)
	}
	if d := inc.RetryAt.Sub(inc.ObservedAt); d > maxRuntimeRetry {
		t.Errorf("a retry %s out was believed", d)
	}
	if n := len([]rune(inc.Error)); n > maxIncidentError+3 {
		t.Errorf("a %d-rune error was stored", n)
	}
}

// An image that will not pull used to reach the controller only as a failed
// runner, cleaned up within minutes; a host whose egress blocked the registry
// showed nothing but runners that never registered. One failed start is
// enough to name the registry and the pool.
func TestAStartThatCannotPullItsImageNamesTheRegistryAndThePool(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	r := &store.Runner{PoolID: pool.ID, HostID: host.ID, Name: "zoomies-pull-1",
		State: store.RunnerProvisioning, Ephemeral: true, Labels: pool.Labels}
	if err := h.st.CreateRunner(h.ctx, r); err != nil {
		t.Fatalf("CreateRunner: %v", err)
	}
	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{
		TaskID: "t1", Kind: agent.TaskCreateRunner, RunnerID: r.ID, OK: false, Fault: store.FaultImage,
		Error: "backend: pulling ghcr.io/eyupio/zoomies-runner:test: dial tcp: i/o timeout",
	}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	view := h.c.HostView(h.reread(t, host.ID))
	inc := view.ImagePullFailed
	if inc == nil || inc.Registry != "ghcr.io" || inc.Pool != pool.Name || inc.Source != "start" || inc.ObservedAt.IsZero() {
		t.Fatalf("the host view carries %+v, want ghcr.io and pool %s from a start", inc, pool.Name)
	}
	p := h.problem(t, "host.image_pull_failed")
	if !strings.Contains(p.Title, "ghcr.io") || !strings.Contains(p.Title, pool.Name) || p.TargetID != host.ID || p.Since == nil {
		t.Fatalf("the drawer entry does not name the registry, the pool and the host: %+v", p)
	}
	if got, ok := gatherValue(t, h.c, "zoomies_image_pull_failures_total", map[string]string{"pool": pool.Name, "kind": "start"}); !ok || got != 1 {
		t.Fatalf("zoomies_image_pull_failures_total = %v (%v), want 1", got, ok)
	}

	h.c = h.restart()
	if !hasCode(h.problemCodes(), "host.image_pull_failed") {
		t.Fatal("a controller restart lost the image pull failure")
	}

	// A prewarm of the same pool that succeeds is the image pulling again.
	if n, err := h.c.PrewarmPool(h.ctx, pool); err != nil || n != 1 {
		t.Fatalf("PrewarmPool = %d, %v", n, err)
	}
	batch, err := h.c.PollTasks(h.ctx, host.ID, time.Millisecond)
	if err != nil || len(batch.Tasks) != 1 {
		t.Fatalf("PollTasks = %+v, %v", batch, err)
	}
	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{TaskID: batch.Tasks[0].ID, Kind: agent.TaskPrewarmImage, OK: true}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	if got := h.reread(t, host.ID).Incidents.ImagePull; got != nil {
		t.Fatalf("a successful prewarm did not clear the incident: %+v", got)
	}
}

// Only an image fault is an image fault. A start the daemon refused for any
// other reason is a different sentence with a different fix, and naming a
// registry for it would send an operator to a firewall that is not the cause.
func TestOnlyAnImageFaultIsRecordedAsAnImagePullFailure(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	if n, err := h.c.PrewarmPool(h.ctx, pool); err != nil || n != 1 {
		t.Fatalf("PrewarmPool = %d, %v", n, err)
	}
	batch, err := h.c.PollTasks(h.ctx, host.ID, time.Millisecond)
	if err != nil || len(batch.Tasks) != 1 {
		t.Fatalf("PollTasks = %+v, %v", batch, err)
	}
	if err := h.c.ReportResult(h.ctx, host.ID, agent.TaskResult{TaskID: batch.Tasks[0].ID, Kind: agent.TaskPrewarmImage,
		OK: false, Fault: store.FaultBackend, Error: "the daemon said no"}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	if got := h.reread(t, host.ID).Incidents.ImagePull; got != nil {
		t.Fatalf("a backend fault was recorded as an image pull failure: %+v", got)
	}
}

// The event stream renders the same view the API does, so the new fields have
// to be there under the names the UI reads.
func TestTheHostViewCarriesIncidentsUnderTheirAPINames(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	h.beat(t, host.ID, &agent.RuntimeReport{Failures: 1, Kind: agent.RuntimeTimeout, RetryIn: 5 * time.Second})
	raw, err := json.Marshal(h.c.HostView(h.reread(t, host.ID)))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	rr, ok := got["runtime_recovering"].(map[string]any)
	if !ok {
		t.Fatalf("no runtime_recovering in %s", raw)
	}
	for _, key := range []string{"failures", "kind", "retry_at", "since", "observed_at"} {
		if _, ok := rr[key]; !ok {
			t.Errorf("runtime_recovering has no %s: %v", key, rr)
		}
	}
	if s, _ := got["runtime_reason"].(string); !strings.Contains(s, "first failure") {
		t.Errorf("runtime_reason = %q", s)
	}
	if _, ok := got["image_pull_failed"]; ok {
		t.Errorf("a host with no pull failure carries image_pull_failed: %s", raw)
	}
}

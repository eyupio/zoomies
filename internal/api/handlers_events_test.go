package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/store"
)

// recentBody is GET /events/recent as a client reads it.
type recentBody struct {
	Items []struct {
		ID    string          `json:"id"`
		Kind  string          `json:"kind"`
		Topic string          `json:"topic"`
		At    time.Time       `json:"at"`
		Data  json.RawMessage `json:"data"`
	} `json:"items"`
	Next      string `json:"next"`
	Complete  bool   `json:"complete"`
	Truncated bool   `json:"truncated"`
	TimedOut  bool   `json:"timed_out"`
}

func (b recentBody) ids() []string {
	out := make([]string, 0, len(b.Items))
	for _, it := range b.Items {
		out = append(out, it.ID)
	}
	return out
}

// recent calls the route as the session's account and decodes a 200.
func (h *harness) recent(t *testing.T, cookie, query string) recentBody {
	t.Helper()
	res := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent" + query, cookie: cookie})
	if res.status != http.StatusOK {
		t.Fatalf("GET /events/recent%s = %d: %s", query, res.status, res.body)
	}
	var out recentBody
	if err := json.Unmarshal(res.body, &out); err != nil {
		t.Fatalf("decoding %s: %v", res.body, err)
	}
	return out
}

// publishJobs publishes n job.updated frames and returns the id each was given.
func publishJobs(h *harness, n int) []uint64 {
	bus := h.ctrl.Events()
	ids := make([]uint64, 0, n)
	for i := range n {
		bus.Publish(events.KindJobUpdated, "job:job_"+strconv.Itoa(i), map[string]any{"id": "job_" + strconv.Itoa(i)})
		ids = append(ids, bus.LastID())
	}
	return ids
}

func TestRecentEventsAgreeWithTheStreamsReplay(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	bus := h.ctrl.Events()
	ids := publishJobs(h, 3)

	got := h.recent(t, cookie, "?kinds=job.updated&since="+url.QueryEscape(bus.WireID(ids[0])))
	want := []string{bus.WireID(ids[1]), bus.WireID(ids[2])}
	if strings.Join(got.ids(), ",") != strings.Join(want, ",") {
		t.Fatalf("items = %v, want %v", got.ids(), want)
	}
	if !got.Complete || got.Truncated || got.TimedOut {
		t.Errorf("complete/truncated/timed_out = %v/%v/%v, want true/false/false", got.Complete, got.Truncated, got.TimedOut)
	}
	if got.Next != want[1] {
		t.Errorf("next = %q, want the last item's id %q", got.Next, want[1])
	}

	// The point of the route is that a client which cannot hold a stream open
	// is told the same thing a reconnecting stream would be, so compare them.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames, _ := h.openStream(t, ctx, "/api/v1/events?kinds=job.updated", cookie, map[string]string{"Last-Event-ID": bus.WireID(ids[0])})
	var replayed []string
	for f := range frames {
		if f.event == string(events.KindJobUpdated) {
			replayed = append(replayed, f.id)
		}
		if len(replayed) == len(want) {
			break
		}
	}
	if strings.Join(replayed, ",") != strings.Join(got.ids(), ",") {
		t.Errorf("the stream replayed %v but the route returned %v", replayed, got.ids())
	}
}

func TestRecentEventsFromAnotherEpochIsIncomplete(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	ids := publishJobs(h, 2)

	got := h.recent(t, cookie, "?kinds=job.updated&since=abc.7")
	if got.Complete {
		t.Error("a cursor from another run of the controller was called complete")
	}
	if len(got.Items) != 2 || got.Items[0].ID != h.ctrl.Events().WireID(ids[0]) {
		t.Errorf("items = %v, want the ring from its start", got.ids())
	}
}

func TestRecentEventsTakesADurationAsSince(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	bus := h.ctrl.Events()

	bus.Publish(events.KindJobUpdated, "job:old", map[string]any{"id": "old"})
	time.Sleep(2 * time.Second)
	bus.Publish(events.KindJobUpdated, "job:new", map[string]any{"id": "new"})
	bus.Publish(events.KindRunnerUpdated, "runner:new", map[string]any{"id": "run"})

	if got := h.recent(t, cookie, "?since=10m&kinds=job.updated"); len(got.Items) != 2 {
		t.Errorf("since=10m returned %v, want both job frames", got.ids())
	}
	got := h.recent(t, cookie, "?since=1s&kinds=job.updated")
	if len(got.Items) != 1 || got.Items[0].Topic != "job:new" {
		t.Fatalf("since=1s returned %v, want only the recent job frame", got.ids())
	}
	if !got.Complete {
		t.Error("a window the ring plainly covers was called incomplete")
	}
	if got := h.recent(t, cookie, "?since=10m&topic=runner:"); len(got.Items) != 1 || got.Items[0].Kind != string(events.KindRunnerUpdated) {
		t.Errorf("topic prefix returned %v, want the runner frame alone", got.ids())
	}
}

func TestRecentEventsRefuseASinceThatIsNeitherAnIdNorADuration(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)

	for _, since := range []string{"yesterday", "abc.xyz", "-5m", "0s", "10 minutes"} {
		res := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent?since=" + url.QueryEscape(since), cookie: cookie})
		if res.status != http.StatusBadRequest {
			t.Errorf("since=%q = %d, want 400", since, res.status)
			continue
		}
		var env errorEnvelope
		if err := json.Unmarshal(res.body, &env); err != nil || env.Error.Field != "since" {
			t.Errorf("since=%q refusal = %s, want the error envelope naming since", since, res.body)
		}
	}
	// A fractional duration has a dot in it, which must not make it an id.
	if got := h.recent(t, cookie, "?since=1.5s"); got.Next == "" {
		t.Error("since=1.5s was refused or returned no cursor")
	}
	if res := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent?wait=soon", cookie: cookie}); res.status != http.StatusBadRequest {
		t.Errorf("wait=soon = %d, want 400", res.status)
	}
}

func TestRecentEventsStopAtTheLimitAndSaySo(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	bus := h.ctrl.Events()
	ids := publishJobs(h, 230)

	first := h.recent(t, cookie, "?kinds=job.updated&limit=5")
	if len(first.Items) != 5 || !first.Truncated || first.Next != first.Items[4].ID {
		t.Fatalf("limit=5: %d items, truncated %v, next %q", len(first.Items), first.Truncated, first.Next)
	}
	// Paging forward from next must resume where the page ended.
	second := h.recent(t, cookie, "?kinds=job.updated&limit=5&since="+url.QueryEscape(first.Next))
	if second.Items[0].ID != bus.WireID(ids[5]) {
		t.Errorf("the next page began at %s, want %s", second.Items[0].ID, bus.WireID(ids[5]))
	}
	if big := h.recent(t, cookie, "?kinds=job.updated&limit=5000"); len(big.Items) != 200 || !big.Truncated {
		t.Errorf("limit=5000 returned %d items (truncated %v), want 200 and truncated", len(big.Items), big.Truncated)
	}
	if def := h.recent(t, cookie, "?kinds=job.updated"); len(def.Items) != 50 {
		t.Errorf("the default limit returned %d items, want 50", len(def.Items))
	}
}

func TestRecentEventsWithNothingToSayStillGiveACursor(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	bus := h.ctrl.Events()

	empty := h.recent(t, cookie, "?kinds=job.updated")
	if len(empty.Items) != 0 || !strings.HasPrefix(empty.Next, bus.Epoch()+".") {
		t.Fatalf("an empty ring answered %+v, want no items and a cursor in this epoch", empty)
	}
	// An empty array, not null: a client iterating it must not need a guard.
	res := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent?kinds=job.updated", cookie: cookie})
	if !strings.Contains(string(res.body), `"items":[]`) {
		t.Errorf("body = %s, want items to be an empty array", res.body)
	}

	// Frames that do not match still move the cursor on, so the next call does
	// not start from the top of the ring again.
	bus.Publish(events.KindRunnerUpdated, "runner:r", map[string]any{"id": "r"})
	got := h.recent(t, cookie, "?kinds=job.updated")
	if len(got.Items) != 0 || got.Next != bus.WireID(bus.LastID()) {
		t.Errorf("next = %q with %d items, want the newest id %q", got.Next, len(got.Items), bus.WireID(bus.LastID()))
	}
}

func TestRecentEventsWithoutAWaitDoNotHoldAStream(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Limits.EventSubscribers = 1 })
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	bus := h.ctrl.Events()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames, first := h.openStream(t, ctx, "/api/v1/events", cookie, nil)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("the stream was refused: %d", first.StatusCode)
	}
	<-frames
	// Every stream slot is taken, and a plain read still answers.
	if got := h.recent(t, cookie, ""); got.Next == "" {
		t.Error("a read with no wait was refused while the streams were full")
	}
	if bus.Subscribers() != 1 {
		t.Errorf("subscribers = %d, a read with no wait must not hold one", bus.Subscribers())
	}
}

func TestWaitForChangeAnswersAtOnceWhenEventsAlreadyExist(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	bus := h.ctrl.Events()
	ids := publishJobs(h, 2)

	started := time.Now()
	got := h.recent(t, cookie, "?kinds=job.updated&wait=20s&since="+url.QueryEscape(bus.WireID(ids[0])))
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("took %v with an event already waiting, want an answer at once", elapsed)
	}
	if len(got.Items) != 1 || got.TimedOut {
		t.Errorf("got %d items, timed_out %v, want the waiting frame", len(got.Items), got.TimedOut)
	}
}

func TestWaitForChangeReturnsTheNextFrameOrTimesOut(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	bus := h.ctrl.Events()

	start := h.recent(t, cookie, "?kinds=job.updated")
	go func() {
		time.Sleep(100 * time.Millisecond)
		// A frame the caller did not ask for must not end its wait.
		bus.Publish(events.KindRunnerUpdated, "runner:r", map[string]any{"id": "r"})
		time.Sleep(50 * time.Millisecond)
		bus.Publish(events.KindJobUpdated, "job:job_late", map[string]any{"id": "job_late"})
	}()
	got := h.recent(t, cookie, "?kinds=job.updated&wait=10s&since="+url.QueryEscape(start.Next))
	if got.TimedOut || len(got.Items) != 1 || got.Items[0].Topic != "job:job_late" {
		t.Fatalf("answered %+v, want the late job frame", got)
	}
	if got.Next != got.Items[0].ID || !got.Complete {
		t.Errorf("next = %q complete = %v, want the frame's id and complete", got.Next, got.Complete)
	}

	// Nothing arrives: the wait ends with no items and the cursor it began with.
	before := h.recent(t, cookie, "?kinds=job.updated&since="+url.QueryEscape(got.Next))
	started := time.Now()
	idle := h.recent(t, cookie, "?kinds=job.updated&wait=400ms&since="+url.QueryEscape(got.Next))
	if !idle.TimedOut || len(idle.Items) != 0 {
		t.Fatalf("answered %+v, want timed_out and no items", idle)
	}
	if idle.Next != before.Next {
		t.Errorf("next moved from %q to %q across a wait that saw nothing", before.Next, idle.Next)
	}
	if elapsed := time.Since(started); elapsed < 300*time.Millisecond {
		t.Errorf("returned after %v, want the wait to have been held", elapsed)
	}
	if bus.Subscribers() != 0 {
		t.Errorf("%d subscribers left after the wait ended", bus.Subscribers())
	}
}

func TestAWaitingClientThatGoesAwayReleasesItsStreamSlot(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	bus := h.ctrl.Events()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/api/v1/events/recent?wait=20s", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	waitFor(t, "the wait to hold a subscription", func() bool { return bus.Subscribers() == 1 })
	cancel()
	<-done
	waitFor(t, "the subscription to be released", func() bool { return bus.Subscribers() == 0 })
}

func TestWaitIsCappedAndCountsAsAStream(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Limits.EventSubscribers = 1 })
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	bus := h.ctrl.Events()
	// The shipped cap is 25 seconds; a test lowers it so that reading 90s as
	// "the cap" is something it can see without waiting.
	h.api.recentWaitCap = 200 * time.Millisecond

	type result struct {
		res     *response
		elapsed time.Duration
	}
	out := make(chan result, 1)
	go func() {
		started := time.Now()
		res := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent?kinds=job.updated&wait=90s", cookie: cookie})
		out <- result{res, time.Since(started)}
	}()
	waitFor(t, "the wait to hold a subscription", func() bool { return bus.Subscribers() == 1 })

	res := h.do(request{method: http.MethodGet, path: "/api/v1/events", cookie: cookie})
	var env errorEnvelope
	if res.status != http.StatusConflict || json.Unmarshal(res.body, &env) != nil || env.Error.Code != codeLimitReached {
		t.Errorf("a stream beside a waiting read = %d %s, want 409 limit_reached", res.status, res.body)
	}

	got := <-out
	var body recentBody
	if got.res.status != http.StatusOK || json.Unmarshal(got.res.body, &body) != nil || !body.TimedOut {
		t.Errorf("a wait=90s read answered %d %s, want timed_out at the cap", got.res.status, got.res.body)
	}
	if got.elapsed < 150*time.Millisecond || got.elapsed > 5*time.Second {
		t.Errorf("wait=90s took %v, want about the 200ms cap", got.elapsed)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, opened := h.openStream(t, ctx, "/api/v1/events", cookie, nil)
	if opened.StatusCode != http.StatusOK {
		t.Errorf("a stream after the wait ended = %d, want 200", opened.StatusCode)
	}
}

func TestRecentEventsNeedTheEventsReadAction(t *testing.T) {
	h := newHarness(t)
	without := h.token("stats-only", store.RoleViewer, "stats:read")
	with := h.token("events-reader", store.RoleViewer, "events:read")

	if res := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent", token: without}); res.status != http.StatusForbidden {
		t.Errorf("a token without events.read = %d, want 403", res.status)
	}
	if res := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent", token: with}); res.status != http.StatusOK {
		t.Errorf("a token with events.read = %d, want 200", res.status)
	}
	if res := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent"}); res.status != http.StatusUnauthorized {
		t.Errorf("no credential = %d, want 401", res.status)
	}
}

// A frame the stream keeps from a viewer must be kept from this route too, or
// the route is the way round the stream's per-audience narrowing.
func TestRecentEventsNarrowFramesTheWayTheStreamDoes(t *testing.T) {
	h := newHarness(t)
	_, viewerCookie := h.user("watcher", store.RoleViewer)
	_, platformCookie := h.user("operator", store.RolePlatform)
	h.ctrl.Events().Publish(events.KindTransfer, "transfer:t", map[string]any{"id": "t", "runners": []string{"run_secret"}})
	publishJobs(h, 1)

	if got := h.recent(t, viewerCookie, ""); len(got.Items) != 1 || got.Items[0].Kind != string(events.KindJobUpdated) {
		t.Errorf("a viewer was handed %v, want the job frame alone", got.ids())
	}
	if got := h.recent(t, platformCookie, ""); len(got.Items) != 2 {
		t.Errorf("the platform account was handed %v, want both frames", got.ids())
	}
}

// The identity is resolved once per request, so every frame of a page is
// narrowed for the same caller. A page that mixed one caller's frames with
// another's would be the way round the narrowing the stream does.
func TestRecentEventsNarrowEveryFrameOfOnePageForTheSameCaller(t *testing.T) {
	h := newHarness(t)
	_, viewerCookie := h.user("watcher", store.RoleViewer)
	_, platformCookie := h.user("operator", store.RolePlatform)
	bus := h.ctrl.Events()

	bus.Publish(events.KindPoolCreated, "pool:p", map[string]any{"id": "p", "env": map[string]string{"TOKEN": "secret"}})
	bus.Publish(events.KindAudit, "audit:1", store.AuditEvent{ID: "aud_1", Action: "auth.login", ActorName: "alice", IP: "198.51.100.4"})
	bus.Publish(events.KindProblems, "problems:1", map[string]any{
		"ok": true,
		"items": []map[string]any{
			{"code": "backup.failed", "severity": "error", "title": "the platform's backups failed", "audience": "platform"},
			{"code": "host.unhealthy", "severity": "error", "title": "a host is unhealthy", "audience": "fleet"},
		},
	})

	got := h.recent(t, viewerCookie, "")
	if len(got.Items) != 3 {
		t.Fatalf("a viewer was handed %v, want the three frames that are theirs", got.ids())
	}
	for _, it := range got.Items {
		switch it.Kind {
		case string(events.KindPoolCreated):
			if strings.Contains(string(it.Data), "secret") {
				t.Errorf("the pool frame carries the environment value: %s", it.Data)
			}
		case string(events.KindAudit):
			if strings.Contains(string(it.Data), "198.51.100.4") {
				t.Errorf("the audit frame carries the address it came from: %s", it.Data)
			}
		case string(events.KindProblems):
			if strings.Contains(string(it.Data), "backup.failed") {
				t.Errorf("the problems frame carries a problem that is not the caller's: %s", it.Data)
			}
		}
	}

	// The same page for the platform carries them, so the narrowing above is the
	// caller's and not a blanket one.
	theirs := h.recent(t, platformCookie, "")
	if len(theirs.Items) != 3 {
		t.Fatalf("the platform account was handed %v, want the three frames", theirs.ids())
	}
	for _, it := range theirs.Items {
		switch it.Kind {
		case string(events.KindPoolCreated):
			if !strings.Contains(string(it.Data), "secret") {
				t.Errorf("the pool frame lost the environment value: %s", it.Data)
			}
		case string(events.KindAudit):
			if !strings.Contains(string(it.Data), "198.51.100.4") {
				t.Errorf("the audit frame lost the address it came from: %s", it.Data)
			}
		case string(events.KindProblems):
			if !strings.Contains(string(it.Data), "backup.failed") {
				t.Errorf("the problems frame lost the platform's own problem: %s", it.Data)
			}
		}
	}
}

// waitFor polls a condition that another goroutine will bring about.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A page whose every frame is hidden from the caller is still a full page: the
// ring holds more after it. Holding the request would let a frame published
// meanwhile overtake what is already waiting, and the cursor would then step
// past it for good.
func TestAWaitDoesNotSkipFramesTheCallerCannotSeeOnATruncatedPage(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("watcher", store.RoleViewer)
	bus := h.ctrl.Events()
	bus.Publish(events.KindTransfer, "transfer:t", map[string]any{"id": "t"}) // hidden from a viewer
	bus.Publish(events.KindJobUpdated, "job:visible", map[string]any{"id": "visible"})
	hidden, visible := bus.WireID(1), bus.WireID(2)
	go func() {
		time.Sleep(200 * time.Millisecond)
		bus.Publish(events.KindJobUpdated, "job:late", map[string]any{"id": "late"})
	}()

	for _, since := range []string{bus.Epoch() + ".0", "10m"} {
		started := time.Now()
		page := h.recent(t, cookie, "?limit=1&wait=5s&since="+url.QueryEscape(since))
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Errorf("since=%s: took %v, want an answer at once while the ring holds more", since, elapsed)
		}
		if len(page.Items) != 0 || !page.Truncated || page.TimedOut || page.Next != hidden {
			t.Errorf("since=%s: answered %+v, want no items, truncated, next %s", since, page, hidden)
		}
	}

	// Following the cursor loses nothing.
	second := h.recent(t, cookie, "?limit=1&since="+url.QueryEscape(hidden))
	if len(second.Items) != 1 || second.Items[0].ID != visible {
		t.Errorf("the page after the hidden frame = %v, want %s", second.ids(), visible)
	}
}

// A cursor handed out on an empty ring must not later read as "the whole ring,
// complete" after the ring has moved on.
func TestACursorFromAnEmptyRingStaysHonestWhenTheRingMovesOn(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("watcher", store.RoleViewer)

	first := h.recent(t, cookie, "?kinds=job.updated")
	publishJobs(h, 300) // more than the ring keeps
	got := h.recent(t, cookie, "?kinds=job.updated&limit=200&since="+url.QueryEscape(first.Next))
	if got.Complete {
		t.Errorf("a cursor from before 300 frames, in a ring that keeps fewer, was called complete (first item %s)", got.Items[0].ID)
	}

	// A restart is the same thing from the other side: the cursor is foreign and
	// the ring is empty, but ids have been used up by frames that were never kept.
	fresh := newHarness(t)
	_, freshCookie := fresh.user("watcher", store.RoleViewer)
	fbus := fresh.ctrl.Events()
	for range 3 {
		fbus.PublishTransient(events.KindScaling, "pool:p", map[string]any{})
	}
	foreign := fresh.recent(t, freshCookie, "?since=abc.7")
	if foreign.Next != fbus.WireID(3) || foreign.Complete {
		t.Fatalf("a foreign cursor on an empty ring answered next %q complete %v, want %s and incomplete", foreign.Next, foreign.Complete, fbus.WireID(3))
	}
	fbus.Publish(events.KindJobUpdated, "job:j", map[string]any{"id": "j"})
	after := fresh.recent(t, freshCookie, "?since="+url.QueryEscape(foreign.Next))
	if len(after.Items) != 1 || !after.Complete {
		t.Errorf("following that cursor answered %+v, want the one frame, complete", after)
	}
}

// A frame that is sent and never kept, such as the resync after many runner
// rows are pruned at once, still takes an id. On a ring that has kept nothing,
// a cursor from before it must read back as incomplete, or the caller steps
// past the one frame that told it to fetch everything again.
func TestAnEmptyRingIsIncompleteOnceATransientFrameHasTakenAnId(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("watcher", store.RoleViewer)
	bus := h.ctrl.Events()

	first := h.recent(t, cookie, "")
	if first.Next != bus.WireID(0) || !first.Complete {
		t.Fatalf("a ring that has numbered nothing answered next %q complete %v, want %s and complete", first.Next, first.Complete, bus.WireID(0))
	}

	bus.PublishTransient(events.KindResync, "", map[string]any{"reason": "fetch the resources again"})

	for _, query := range []string{"?since=" + url.QueryEscape(first.Next), "?since=1h"} {
		got := h.recent(t, cookie, query)
		if got.Complete {
			t.Errorf("%s after an unkept resync was called complete, so the caller would never learn to fetch afresh", query)
		}
		if got.Next != bus.WireID(1) {
			t.Errorf("%s answered next %q, want %s past the unkept frame", query, got.Next, bus.WireID(1))
		}
	}
}

// The brief's other half of "a wait counts as a stream": a wait asked for while
// every slot is taken is refused the way a stream is, before anything is held.
func TestAWaitIsRefusedWhenTheStreamSlotsAreFull(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Limits.EventSubscribers = 1 })
	_, cookie := h.user("watcher", store.RoleViewer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames, first := h.openStream(t, ctx, "/api/v1/events", cookie, nil)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("the stream was refused: %d", first.StatusCode)
	}
	<-frames

	started := time.Now()
	res := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent?wait=20s", cookie: cookie})
	var env errorEnvelope
	if res.status != http.StatusConflict || json.Unmarshal(res.body, &env) != nil ||
		env.Error.Code != codeLimitReached || env.Error.Field != "limits.event_subscribers" {
		t.Fatalf("a wait with the slots full = %d %s, want 409 limit_reached naming limits.event_subscribers", res.status, res.body)
	}
	if time.Since(started) > time.Second {
		t.Error("the refusal waited instead of coming at once")
	}
}

// A frame the caller may not see arrives on the channel like any other. It must
// not be the answer, and it must not end the wait.
func TestAFrameHiddenFromTheCallerDoesNotEndItsWait(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("watcher", store.RoleViewer)
	bus := h.ctrl.Events()
	go func() {
		time.Sleep(100 * time.Millisecond)
		bus.Publish(events.KindTransfer, "transfer:t", map[string]any{"id": "t"}) // hidden from a viewer
		time.Sleep(150 * time.Millisecond)
		bus.Publish(events.KindJobUpdated, "job:job_seen", map[string]any{"id": "job_seen"})
	}()

	got := h.recent(t, cookie, "?wait=10s&since="+url.QueryEscape(bus.WireID(0)))
	if got.TimedOut || len(got.Items) != 1 || got.Items[0].Topic != "job:job_seen" {
		t.Fatalf("answered %+v, want the job frame after the hidden one was passed over", got)
	}
}

// A cursor past the newest id, on a ring with nothing in it, is not worth
// echoing: it would stay incomplete for ever.
func TestACursorPastTheNewestIdOnAnEmptyRingIsReplaced(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("watcher", store.RoleViewer)
	bus := h.ctrl.Events()
	bus.PublishTransient(events.KindScaling, "pool:p", map[string]any{})

	got := h.recent(t, cookie, "?since="+url.QueryEscape(bus.WireID(50)))
	if got.Complete || got.Next != bus.WireID(1) {
		t.Errorf("answered next %q complete %v, want %s and incomplete", got.Next, got.Complete, bus.WireID(1))
	}
	if again := h.recent(t, cookie, "?since="+url.QueryEscape(got.Next)); !again.Complete {
		t.Error("the replacement cursor was still incomplete")
	}
}

// The stream's data line is {} for an event with no payload; the route says the
// same rather than something of its own.
func TestAFrameWithNoPayloadCarriesAnEmptyObjectAsTheStreamDoes(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("watcher", store.RoleViewer)
	h.ctrl.Events().Publish(events.KindScaling, "pool:p", nil)

	res := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent?kinds=scaling", cookie: cookie})
	if res.status != http.StatusOK || !strings.Contains(string(res.body), `"topic":"pool:p"`) || !strings.Contains(string(res.body), `"data":{}`) {
		t.Errorf("body = %s, want the frame with data {}", res.body)
	}
}

// The route answers from the ring the stream replays, so it has to ask the
// stream's question of every frame: could this caller have fetched it from its
// GET route? Otherwise a token scoped to events:read, or a public view's
// visitor, could read the audit log a row at a time here instead.
func TestRecentEventsSendAFrameOnlyToSomebodyWhoCouldReadIt(t *testing.T) {
	h := newHarness(t, publicView)
	scoped := h.token("events only", store.RoleAdmin, "events:read", "pools:read")
	viewer, _ := h.user("viewer", store.RoleViewer)

	bus := h.ctrl.Events()
	bus.Publish(events.KindAudit, "audit:1", store.AuditEvent{ID: "aud_1", Action: "auth.login", ActorName: "alice", IP: "198.51.100.4"})
	bus.Publish(events.KindPoolCreated, "pool:p", map[string]any{"id": "p"})

	callers := []struct {
		name      string
		req       request
		wantAudit bool
	}{
		{name: "visitor"},
		{name: "token scoped to events and pools", req: request{token: scoped}},
		{name: "viewer", req: request{cookie: h.session(viewer)}, wantAudit: true},
	}
	for _, c := range callers {
		t.Run(c.name, func(t *testing.T) {
			c.req.method, c.req.path = http.MethodGet, "/api/v1/events/recent"
			res := h.do(c.req)
			res.mustStatus(t, http.StatusOK, "reading the recent events")
			var got recentBody
			if err := json.Unmarshal(res.body, &got); err != nil {
				t.Fatalf("decoding %s: %v", res.body, err)
			}
			sawAudit, sawPool := false, false
			for _, it := range got.Items {
				sawAudit = sawAudit || it.Kind == string(events.KindAudit)
				sawPool = sawPool || it.Kind == string(events.KindPoolCreated)
			}
			if sawAudit != c.wantAudit || !sawPool {
				t.Errorf("audit frame delivered = %v, pool frame = %v; want %v and true", sawAudit, sawPool, c.wantAudit)
			}
		})
	}
}

// A wait holds a stream slot, so a visitor's wait is held to the visitors'
// ceiling as their stream is, and a crowd of them cannot take the slots the
// people who run the fleet need.
func TestAVisitorsWaitIsHeldToTheVisitorsCeiling(t *testing.T) {
	t.Parallel()
	h := newHarness(t, publicView, func(c *config.Config) {
		c.Limits.VisitorEventSubscribers = 1
		c.Limits.EventSubscribers = 1
	})
	admin, _ := h.user("admin", store.RoleAdmin)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames, first := h.openStream(t, ctx, "/api/v1/events", "", nil)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("the first visitor's stream answered %d", first.StatusCode)
	}
	await(t, frames, "the first visitor's opening comment", func(f sseFrame) bool { return f.comment != "" })

	refused := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent?wait=5s"})
	refused.mustStatus(t, http.StatusConflict, "a visitor's wait past the visitors' ceiling")
	if msg := refused.errorMessage(t); !strings.Contains(msg, "limits.visitor_event_subscribers") {
		t.Errorf("the refusal does not name the visitors' ceiling: %q", msg)
	}

	staff := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent?wait=1s", cookie: h.session(admin)})
	staff.mustStatus(t, http.StatusOK, "an administrator's wait while visitors are at their ceiling")
}

// A caller who wants to hear about what happens next asks from now. Without it
// the only cursor-free start is the oldest of the ring, and on a busy fleet that
// is five pages of the past to read before anything new could be waited for.
func TestRecentEventsSinceNowIsACursorAtThePresentAndNothingElse(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	bus := h.ctrl.Events()
	publishJobs(h, 60)

	now := h.recent(t, cookie, "?since=now")
	if len(now.Items) != 0 || now.Truncated || now.TimedOut || !now.Complete {
		t.Fatalf("since=now answered %d items (truncated %v, timed_out %v, complete %v), want none and complete",
			len(now.Items), now.Truncated, now.TimedOut, now.Complete)
	}
	if now.Next != bus.WireID(bus.LastID()) {
		t.Errorf("next = %q, want the bus's last id %q", now.Next, bus.WireID(bus.LastID()))
	}
	res := h.do(request{method: http.MethodGet, path: "/api/v1/events/recent?since=now", cookie: cookie})
	if !strings.Contains(string(res.body), `"items":[]`) {
		t.Errorf("body = %s, want items to be an empty array", res.body)
	}
	if bus.Subscribers() != 0 {
		t.Errorf("subscribers = %d, a read from now with no wait must not hold one", bus.Subscribers())
	}

	// The cursor reads on from the present: the next call is given what came
	// after it and nothing from before.
	ids := publishJobs(h, 1)
	after := h.recent(t, cookie, "?since="+url.QueryEscape(now.Next))
	if len(after.Items) != 1 || after.Items[0].ID != bus.WireID(ids[0]) {
		t.Errorf("reading on from now returned %v, want only %s", after.ids(), bus.WireID(ids[0]))
	}
}

func TestAWaitSinceNowHoldsForTheNextMatchingFrameOnly(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	cookie := h.session(viewer)
	bus := h.ctrl.Events()
	publishJobs(h, 60)

	go func() {
		time.Sleep(150 * time.Millisecond)
		// A frame the caller did not ask for must not end its wait.
		bus.Publish(events.KindRunnerUpdated, "runner:r", map[string]any{"id": "r"})
		time.Sleep(50 * time.Millisecond)
		bus.Publish(events.KindJobUpdated, "job:job_late", map[string]any{"id": "job_late"})
	}()
	started := time.Now()
	got := h.recent(t, cookie, "?since=now&kinds=job.updated&wait=10s")
	if elapsed := time.Since(started); elapsed < 100*time.Millisecond {
		t.Errorf("answered after %v, want the request held until the new frame", elapsed)
	}
	if got.TimedOut || len(got.Items) != 1 || got.Items[0].Topic != "job:job_late" {
		t.Fatalf("answered %v (timed_out %v), want only the late job frame", got.ids(), got.TimedOut)
	}
	if !got.Complete || got.Next != got.Items[0].ID {
		t.Errorf("complete %v next %q, want complete and the frame's id", got.Complete, got.Next)
	}

	// Nothing arrives: the wait ends with no items and the cursor it was given.
	idle := h.recent(t, cookie, "?since=now&kinds=job.updated&wait=300ms")
	if !idle.TimedOut || len(idle.Items) != 0 || idle.Next != bus.WireID(bus.LastID()) || !idle.Complete {
		t.Errorf("a quiet wait from now answered %+v, want timed_out, no items and the present as next", idle)
	}
	if bus.Subscribers() != 0 {
		t.Errorf("%d subscribers left after the wait ended", bus.Subscribers())
	}
}

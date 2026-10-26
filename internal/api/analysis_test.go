package api

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/analysis"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
)

// The analysis routes are transport: each body is the controller's finding,
// rendered once, so the route, the MCP tool, Eli, the CLI and the UI card
// read one answer. These tests hold the routes to that, and the contract to
// the Go types the body is marshalled from.

// analysisQuery is the query string of an analysis route over a window.
func analysisQuery(since, until time.Time, extra url.Values) string {
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	if !until.IsZero() {
		q.Set("until", until.UTC().Format(time.RFC3339))
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// sameJSON compares a route's body with the controller's own finding as JSON,
// so a field the route renders differently, or drops, is a failure.
func sameJSON(t *testing.T, got []byte, want any) {
	t.Helper()
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("the body is not JSON: %v\n%s", err, truncate(got))
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("the route's body is not the controller's finding\n got %s\nwant %s", truncate(got), truncate(raw))
	}
}

// analysisWindowForTest is a window a whole second wide at each end, as a
// query string carries it, ending a minute ago.
func analysisWindowForTest(span time.Duration) (time.Time, time.Time) {
	until := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	return until.Add(-span), until
}

func TestQueueWaitRouteAnswersTheControllersFinding(t *testing.T) {
	h := newHarness(t)
	pool := h.pool(h.installation(), "linux")
	_, cookie := h.user("viewer", store.RoleViewer)
	since, until := analysisWindowForTest(6 * time.Hour)
	h.releaseJobs(pool, "v1.3.3", 4, 1_000, since.Add(time.Hour))

	for name, extra := range map[string]url.Values{"the fleet": nil, "one pool": {"pool_id": {pool.ID}}} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/queue-wait" + analysisQuery(since, until, extra), cookie: cookie})
		resp.mustStatus(t, http.StatusOK, "the queue wait analysis of "+name)
		want, err := h.ctrl.AnalyseQueueWait(h.ctx, controller.AnalysisWindow{Since: since, Until: until}, extra.Get("pool_id"))
		if err != nil {
			t.Fatal(err)
		}
		if want.Evidence.Jobs != 4 {
			t.Fatalf("%s: the controller read %d jobs; the seed is not what this test is about", name, want.Evidence.Jobs)
		}
		sameJSON(t, resp.body, want)
	}

	missing := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/queue-wait?pool_id=pool_missing", cookie: cookie})
	missing.mustStatus(t, http.StatusNotFound, "a pool the fleet does not have")
	if msg := missing.errorMessage(t); !strings.Contains(msg, "pool_missing") {
		t.Errorf("the 404 does not name the pool asked for: %q", msg)
	}
}

// Under thirty jobs a side, a comparison is said to be too small to rank,
// and the route carries that sentence, not a ranking of three slow jobs.
func TestCompareRouteSaysASmallSampleIsSmall(t *testing.T) {
	h := newHarness(t)
	pool := h.pool(h.installation(), "linux")
	_, cookie := h.user("viewer", store.RoleViewer)
	since, until := analysisWindowForTest(12 * time.Hour)
	h.releaseJobs(pool, "v1.3.2", 3, 1_000, since.Add(time.Hour))
	h.releaseJobs(pool, "v1.3.3", 3, 2_000, since.Add(6*time.Hour))

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/compare" + analysisQuery(since, until, nil), cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "a comparison of two small releases")
	var got analysis.CompareFinding
	resp.into(t, &got)
	if got.Evidence.A.Label != "v1.3.2" || got.Evidence.B.Label != "v1.3.3" {
		t.Errorf("the comparison set %q against %q; want v1.3.2 against v1.3.3", got.Evidence.A.Label, got.Evidence.B.Label)
	}
	if got.Confidence != analysis.Low || !strings.Contains(got.Summary, "not ranked") {
		t.Errorf("a comparison of three jobs a side is at %s and says %q; want low, and that it is not ranked", got.Confidence, got.Summary)
	}
	want, err := h.ctrl.AnalyseCompare(h.ctx, controller.CompareRequest{Since: since, Until: until})
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, resp.body, want)
}

// Each comparison the fleet cannot make is refused with the controller's own
// sentence and a code a client can choose what to offer by. A pair that is
// one release can never be compared as asked, so it is a 400. A fleet
// without two releases, or a period past retention with no midnight to end
// on, is a well-formed request the fleet's records refuse, which this API
// answers with a 409 and a code of its own, as it does update.nothing_newer.
func TestCompareRouteRefusesWhatItCannotCompareWithItsOwnSentence(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Retention.Jobs = 30 * time.Hour })
	pool := h.pool(h.installation(), "linux")
	_, cookie := h.user("viewer", store.RoleViewer)
	midnight := time.Now().UTC().Truncate(24 * time.Hour)
	// A job four days ago gives the roll-up a first row to start from.
	h.releaseJobs(pool, "", 1, 1_000, midnight.AddDate(0, 0, -4).Add(10*time.Hour))
	if _, err := h.st.RollUpInstallations(h.ctx, time.Now(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	since, until := analysisWindowForTest(12 * time.Hour)

	for name, c := range map[string]struct {
		query         string
		status        int
		code, field   string
		want          error
		wantSubstring string
	}{
		"a by it does not know": {query: analysisQuery(since, until, url.Values{"by": {"week"}}),
			status: http.StatusBadRequest, code: codeBadRequest, field: "by", wantSubstring: "version or period"},
		"a release named as a to a comparison by period": {query: analysisQuery(since, until, url.Values{"by": {"period"}, "a": {"v1.3.2"}}),
			status: http.StatusBadRequest, code: codeBadRequest, field: "a", wantSubstring: "by=version"},
		"a release named as b to a comparison by period": {query: analysisQuery(since, until, url.Values{"by": {"period"}, "b": {"v1.3.3"}}),
			status: http.StatusBadRequest, code: codeBadRequest, field: "b", wantSubstring: "by=version"},
		"an a longer than a release's name can be": {query: analysisQuery(since, until, url.Values{"a": {strings.Repeat("v", maxReleaseName+1)}}),
			status: http.StatusBadRequest, code: codeBadRequest, field: "a", wantSubstring: "longer than a release's name can be"},
		"a b longer than a release's name can be": {query: analysisQuery(since, until, url.Values{"b": {strings.Repeat("v", maxReleaseName+1)}}),
			status: http.StatusBadRequest, code: codeBadRequest, field: "b", wantSubstring: "longer than a release's name can be"},
		// The bound is bytes, as the installer's is: a hundred characters of two
		// bytes each is two hundred.
		"an a of a hundred two-byte characters": {query: analysisQuery(since, until, url.Values{"a": {strings.Repeat("é", 100)}}),
			status: http.StatusBadRequest, code: codeBadRequest, field: "a", wantSubstring: "128 bytes at most"},
		"the same release twice": {query: analysisQuery(since, until, url.Values{"a": {"v1.3.3"}, "b": {"v1.3.3"}}),
			status: http.StatusBadRequest, code: codeAnalysisSameRelease, field: "a", want: controller.ErrSameRelease},
		"a fleet with no two releases": {query: analysisQuery(since, until, nil),
			status: http.StatusConflict, code: codeAnalysisTooFewReleases, want: controller.ErrTooFewReleases},
		"a period past retention with no midnight to end on": {
			query:  analysisQuery(midnight.AddDate(0, 0, -3).Add(8*time.Hour), midnight.AddDate(0, 0, -3).Add(14*time.Hour), url.Values{"by": {"period"}}),
			status: http.StatusConflict, code: codeAnalysisWholeDaysOnly, field: "since", wantSubstring: "is older than retention.jobs, so it can only be read in whole UTC days"},
	} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/compare" + c.query, cookie: cookie})
		if resp.status != c.status {
			t.Errorf("%s: status %d, want %d: %s", name, resp.status, c.status, truncate(resp.body))
			continue
		}
		var env errorEnvelope
		resp.into(t, &env)
		if env.Error.Code != c.code || env.Error.Field != c.field {
			t.Errorf("%s: code %q on %q, want %q on %q", name, env.Error.Code, env.Error.Field, c.code, c.field)
		}
		if c.want != nil && env.Error.Message != c.want.Error() {
			t.Errorf("%s: the refusal says %q; want the controller's own sentence %q", name, env.Error.Message, c.want)
		}
		if !strings.Contains(env.Error.Message, c.wantSubstring) {
			t.Errorf("%s: the refusal says %q; want it to say %q", name, env.Error.Message, c.wantSubstring)
		}
	}
}

// JSON Schema counts maxLength in characters and the route counts bytes, the
// unit the installer holds a binary's version to. Declared as maxLength: 128
// the contract promised that a name of a hundred two-byte characters would be
// taken, and the route answered it with a 400. The two parameters say how many
// bytes instead, and declare no maxLength for a client to read as characters.
func TestTheContractBoundsAReleaseNameInTheBytesTheRouteCounts(t *testing.T) {
	doc := loadSpec(t)
	paths, _ := doc["paths"].(map[string]any)
	compare, _ := paths["/analysis/compare"].(map[string]any)
	get, _ := compare["get"].(map[string]any)
	params, _ := get["parameters"].([]any)
	found := map[string]bool{}
	for _, p := range params {
		param, _ := p.(map[string]any)
		name, _ := param["name"].(string)
		if name != "a" && name != "b" {
			continue
		}
		found[name] = true
		if schema, _ := param["schema"].(map[string]any); schema["maxLength"] != nil {
			t.Errorf("parameter %s declares maxLength %v, which JSON Schema counts in characters; the route counts bytes", name, schema["maxLength"])
		}
		if d, _ := param["description"].(string); !strings.Contains(d, fmt.Sprintf("at most %d bytes", maxReleaseName)) {
			t.Errorf("parameter %s does not say it is at most %d bytes: %q", name, maxReleaseName, d)
		}
	}
	if len(found) != 2 {
		t.Fatalf("the contract's compare operation has parameters %v; want both a and b", found)
	}
}

func TestHostPressureRouteAnswersTheControllersFinding(t *testing.T) {
	h := newHarness(t)
	pool := h.pool(h.installation(), "linux")
	host := h.host("pressed")
	_, cookie := h.user("viewer", store.RoleViewer)
	since, until := analysisWindowForTest(2 * time.Hour)

	var samples []store.HostSample
	for m := since; m.Before(until); m = m.Add(time.Minute) {
		load, free := 1.0, int64(6_000)
		samples = append(samples, store.HostSample{HostID: host.ID, At: m, Capacity: 4, CPUs: 4, MemoryMB: 8_000,
			LoadAverage1: &load, MemoryAvailableMB: &free})
	}
	if err := h.st.RecordHostSamples(h.ctx, samples); err != nil {
		t.Fatal(err)
	}
	for _, j := range h.releaseJobs(pool, "", 2, 1_000, since.Add(30*time.Minute)) {
		if _, err := h.st.StampJobVersions(h.ctx, j.ID, store.JobVersions{HostID: host.ID}); err != nil {
			t.Fatal(err)
		}
	}

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/host-pressure" + analysisQuery(since, until, url.Values{"host_id": {host.ID}}), cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "the pressure on a host")
	want, err := h.ctrl.AnalyseHostPressure(h.ctx, controller.AnalysisWindow{Since: since, Until: until}, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want.Evidence.Minutes == 0 || want.Evidence.HostName != "pressed" {
		t.Fatalf("the controller read %d minutes of %q; the seed is not what this test is about", want.Evidence.Minutes, want.Evidence.HostName)
	}
	sameJSON(t, resp.body, want)
}

// A host's pressure is a question about one host: without one there is
// nothing to read, and a host the fleet does not have is not found, by name.
func TestHostPressureRouteNeedsAHost(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("viewer", store.RoleViewer)

	none := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/host-pressure", cookie: cookie})
	none.mustStatus(t, http.StatusBadRequest, "a host pressure analysis naming no host")
	var env errorEnvelope
	none.into(t, &env)
	if env.Error.Field != "host_id" || env.Error.Code != codeBadRequest {
		t.Errorf("the refusal is %q on %q; want bad_request on host_id", env.Error.Code, env.Error.Field)
	}

	// The whole sentence, because it is built from what the controller says
	// was missing and not from its error's wording, which a later wrap would
	// change.
	missing := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/host-pressure?host_id=host_missing", cookie: cookie})
	missing.mustStatus(t, http.StatusNotFound, "a host the fleet does not have")
	if msg, want := missing.errorMessage(t), "there is no host host_missing in this fleet; it may have been removed, or the id is wrong"; msg != want {
		t.Errorf("the 404 says %q; want %q", msg, want)
	}
}

func TestStartupFailuresRouteAnswersTheControllersFinding(t *testing.T) {
	h := newHarness(t)
	pool := h.pool(h.installation(), "linux")
	host := h.host("failing")
	_, cookie := h.user("viewer", store.RoleViewer)
	// The route ends the window at its own instant, and the store stamps and
	// reads whole milliseconds with a strict "<", so a runner stamped by the
	// real clock a few microseconds before the request can share its
	// millisecond and fall outside the window: this test counted two failures
	// instead of three about one run in forty. The runners were asked for ten
	// minutes ago instead.
	asked := time.Now().Add(-10 * time.Minute)
	for i := 0; i < 3; i++ {
		r := h.runner(pool, host, store.RunnerProvisioning)
		if err := h.st.SetRunnerCreatedAt(h.ctx, r.ID, asked); err != nil {
			t.Fatal(err)
		}
		if _, err := h.st.FailRunner(h.ctx, r.ID, "pull access denied for ghcr.io/acme/missing", store.FaultImage); err != nil {
			t.Fatal(err)
		}
	}
	// The window reaches a minute past now. The controller reads an until past
	// now as now, so each read ends at its own instant, and the route's is the
	// one the controller's answer is compared at.
	until := time.Now().UTC().Truncate(time.Second).Add(time.Minute)
	since := until.Add(-time.Hour)

	for name, extra := range map[string]url.Values{
		"the fleet": nil, "one pool": {"pool_id": {pool.ID}}, "one host": {"host_id": {host.ID}},
	} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/startup-failures" + analysisQuery(since, until, extra), cookie: cookie})
		resp.mustStatus(t, http.StatusOK, "the startup failures of "+name)
		var read struct {
			Until time.Time `json:"until"`
		}
		resp.into(t, &read)
		if !read.Until.Before(until) || read.Until.After(time.Now()) {
			t.Errorf("%s: the finding ends at %v, past the instant it was read", name, read.Until)
		}
		want, err := h.ctrl.AnalyseStartupFailures(h.ctx, controller.AnalysisWindow{Since: since, Until: read.Until}, extra.Get("pool_id"), extra.Get("host_id"))
		if err != nil {
			t.Fatal(err)
		}
		if want.Evidence.Failures != 3 {
			t.Fatalf("%s: the controller counted %d failures; the seed is not what this test is about", name, want.Evidence.Failures)
		}
		sameJSON(t, resp.body, want)
	}

	for _, q := range []string{"pool_id=pool_missing", "host_id=host_missing"} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/startup-failures?" + q, cookie: cookie})
		resp.mustStatus(t, http.StatusNotFound, q)
		if msg, id := resp.errorMessage(t), strings.SplitN(q, "=", 2)[1]; !strings.Contains(msg, id) {
			t.Errorf("the 404 does not name %s: %q", id, msg)
		}
	}
}

// A period left out is the last day, and one longer than the job statistics
// may read is refused by the setting that bounds both, so an operator who
// narrowed it has narrowed every analysis too.
func TestAnAnalysisWindowDefaultsToTheLastDayAndIsBoundedByTheStatsLimit(t *testing.T) {
	h := newHarness(t)
	host := h.host("any")
	_, cookie := h.user("viewer", store.RoleViewer)

	for _, path := range []string{
		"/api/v1/analysis/queue-wait",
		"/api/v1/analysis/compare?by=period",
		"/api/v1/analysis/host-pressure?host_id=" + host.ID,
		"/api/v1/analysis/startup-failures",
		"/api/v1/analysis/recurring-failures",
	} {
		before := time.Now()
		resp := h.do(request{method: http.MethodGet, path: path, cookie: cookie})
		after := time.Now()
		resp.mustStatus(t, http.StatusOK, path)
		var got struct {
			Since, Until time.Time
			Evidence     struct {
				B struct{ Since, Until time.Time }
			}
		}
		resp.into(t, &got)
		if strings.Contains(path, "compare") {
			// A comparison spans both its sides; the period asked for is
			// the later one.
			got.Since, got.Until = got.Evidence.B.Since, got.Evidence.B.Until
		}
		if got.Until.Before(before.Add(-time.Second)) || got.Until.After(after) {
			t.Errorf("%s: until is %v; want the moment it was asked, between %v and %v", path, got.Until, before, after)
		}
		if !got.Since.Equal(got.Until.Add(-24 * time.Hour)) {
			t.Errorf("%s: since is %v and until %v; want a day apart", path, got.Since, got.Until)
		}

		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		long := h.do(request{method: http.MethodGet, path: path + sep + "since=" + url.QueryEscape(time.Now().Add(-100*24*time.Hour).UTC().Format(time.RFC3339)), cookie: cookie})
		long.mustStatus(t, http.StatusBadRequest, path+" over a hundred days")
		var env errorEnvelope
		long.into(t, &env)
		if env.Error.Field != "since" || !strings.Contains(env.Error.Message, "limits.job_stats_window") ||
			!strings.Contains(env.Error.Message, "an analysis covers at most 90 days") {
			t.Errorf("%s: the refusal is %q on %q; want it on since, naming limits.job_stats_window and what an analysis covers", path, env.Error.Message, env.Error.Field)
		}
	}
}

func TestAnAnalysisRefusesAWindowThatEndsBeforeItStarts(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("viewer", store.RoleViewer)
	now := time.Now().UTC().Truncate(time.Second)

	for name, q := range map[string]string{
		"ending before it starts": analysisQuery(now, now.Add(-time.Hour), nil),
		"of no length":            analysisQuery(now, now, nil),
		"until is not a time":     "?until=yesterday",
	} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/queue-wait" + q, cookie: cookie})
		resp.mustStatus(t, http.StatusBadRequest, "a window "+name)
		var env errorEnvelope
		resp.into(t, &env)
		want := "since"
		if name == "until is not a time" {
			want = "until"
		}
		if env.Error.Field != want {
			t.Errorf("%s: the refusal is on %q; want %q: %s", name, env.Error.Field, want, env.Error.Message)
		}
	}
}

// A period that begins in the future holds nothing that has happened. The
// route says so on since, in the controller's sentence, rather than answering
// for an empty period as if it had been quiet.
func TestAnAnalysisRefusesAPeriodThatBeginsAfterNow(t *testing.T) {
	h := newHarness(t)
	host := h.host("vm-1")
	_, cookie := h.user("viewer", store.RoleViewer)
	ahead := time.Now().UTC().Truncate(time.Second).Add(time.Hour)
	for _, path := range []string{"queue-wait", "compare", "host-pressure", "startup-failures", "recurring-failures"} {
		extra := url.Values{}
		if path == "host-pressure" {
			extra.Set("host_id", host.ID)
		}
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/" + path + analysisQuery(ahead, ahead.Add(time.Hour), extra), cookie: cookie})
		resp.mustStatus(t, http.StatusBadRequest, path+" over a period that begins after now")
		var env errorEnvelope
		resp.into(t, &env)
		if env.Error.Field != "since" || env.Error.Message != controller.ErrSinceNotPast.Error() {
			t.Errorf("%s: the refusal is %q on %q; want the controller's sentence on since", path, env.Error.Message, env.Error.Field)
		}
	}
}

// An until past now ends now before the route works anything out from it. An
// operator who asks about today up to tomorrow's midnight, as the CLI's
// whole-day form invites, is answered for today: not refused on a since they
// never sent, and not refused as too long for hours that have not happened.
func TestAnAnalysisWindowEndingInTheFutureIsReadUpToNow(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("viewer", store.RoleViewer)
	now := time.Now().UTC().Truncate(time.Second)

	for name, q := range map[string]string{
		"no since, until two days out":          analysisQuery(time.Time{}, now.Add(48*time.Hour), nil),
		"since at the limit, until an hour out": analysisQuery(now.Add(-90*24*time.Hour+time.Minute), now.Add(time.Hour), nil),
	} {
		before := time.Now()
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/analysis/queue-wait" + q, cookie: cookie})
		after := time.Now()
		resp.mustStatus(t, http.StatusOK, name)
		var got struct{ Since, Until time.Time }
		resp.into(t, &got)
		if got.Until.Before(before.Add(-time.Second)) || got.Until.After(after) {
			t.Errorf("%s: until is %v; want now, between %v and %v", name, got.Until, before, after)
		}
		if !got.Since.Before(got.Until) {
			t.Errorf("%s: since %v is not before until %v", name, got.Since, got.Until)
		}
	}
}

// The job statistics share the analyses' bound, and keep their own noun and
// their own week-long default.
func TestTheJobStatisticsSayWhatTheyCoverWhenAWindowIsTooLong(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("viewer", store.RoleViewer)
	since := url.QueryEscape(time.Now().Add(-100 * 24 * time.Hour).UTC().Format(time.RFC3339))
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/jobs/stats?since=" + since, cookie: cookie})
	resp.mustStatus(t, http.StatusBadRequest, "job statistics over a hundred days")
	if msg := resp.errorMessage(t); !strings.Contains(msg, "job statistics cover at most 90 days") || !strings.Contains(msg, "limits.job_stats_window") {
		t.Errorf("the refusal says %q", msg)
	}
}

// untrustedFields are the evidence fields a workflow or a host writes. The
// contract says so beside each, because a model handed the finding reads the
// descriptions and must not take a job name for an instruction.
var untrustedFields = map[string]bool{"repo": true, "workflow": true, "job_name": true, "last_message": true}

// The four findings are marshalled from Go types and read through the
// generated client by the UI, the CLI and the MCP tools. A field the types
// carry and the contract does not is one those clients cannot see; one the
// contract promises and the types never send is a runtime error in a browser.
// This walks each finding's type and its schema together, field for field,
// down every nested evidence type, so they cannot drift apart.
func TestTheAnalysisSchemasCarryEveryFieldTheFindingsDo(t *testing.T) {
	doc := loadSpec(t)
	seen := map[reflect.Type]bool{}
	for name, typ := range map[string]reflect.Type{
		"QueueWaitFinding":       reflect.TypeFor[analysis.QueueWaitFinding](),
		"CompareFinding":         reflect.TypeFor[analysis.CompareFinding](),
		"HostPressureFinding":    reflect.TypeFor[analysis.HostPressureFinding](),
		"StartupFailuresFinding": reflect.TypeFor[analysis.StartupFailuresFinding](),
	} {
		walkSchema(t, doc, name, schemaByName(t, doc, name), typ, seen)
	}
	for _, want := range []string{"pool_id", "unknown", "recovered_at", "explained", "runner_kept", "jobs_truncated", "no_record_before", "asked_until"} {
		found := false
		for ty := range seen {
			for i := range ty.NumField() {
				if jsonName(ty.Field(i)) == want {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("no finding carries %q, which the contract was promised", want)
		}
	}
}

// walkSchema checks one Go struct against one schema, and recurses into every
// field whose type is a struct of the analysis package.
func walkSchema(t *testing.T, doc map[string]any, where string, schema map[string]any, typ reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	props, required := flattenSchema(t, doc, schema)
	if len(props) == 0 {
		t.Fatalf("%s: no properties were resolved from the document, so this check would pass on anything", where)
	}
	goFields := map[string]reflect.StructField{}
	for i := range typ.NumField() {
		f := typ.Field(i)
		if name := jsonName(f); name != "" {
			goFields[name] = f
		}
	}
	for name, f := range goFields {
		prop, ok := props[name]
		if !ok {
			t.Errorf("%s: the finding carries %q (%s.%s) and the contract does not", where, name, typ.Name(), f.Name)
			continue
		}
		omitted := strings.Contains(f.Tag.Get("json"), ",omitempty")
		// An always-sent field is required, so the generated client's type
		// does not make every reader check for it; one left out when empty
		// is not, or the type promises what the body does not carry.
		if req := slices.Contains(required, name); req == omitted {
			t.Errorf("%s.%s: the contract says required is %t, but the field is %s", where, name, req,
				map[bool]string{true: "left out when empty", false: "always sent"}[omitted])
		}
		if untrustedFields[name] {
			if d, _ := prop["description"].(string); !strings.Contains(d, "untrusted") {
				t.Errorf("%s.%s is written by a workflow or a host, and its description does not say it is untrusted: %q", where, name, d)
			}
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		if ft.Kind() != reflect.Struct || ft.PkgPath() != reflect.TypeFor[analysis.Interval]().PkgPath() {
			continue
		}
		target := refOf(prop)
		if target == "" {
			t.Errorf("%s.%s is a %s, and its schema names no schema for it", where, name, ft.Name())
			continue
		}
		if seen[ft] {
			continue
		}
		seen[ft] = true
		walkSchema(t, doc, target, schemaByName(t, doc, target), ft, seen)
	}
	for name := range props {
		if _, ok := goFields[name]; !ok {
			t.Errorf("%s: the contract promises %q, which the finding (%s) never sends", where, name, typ.Name())
		}
	}
}

func jsonName(f reflect.StructField) string {
	if !f.IsExported() {
		return ""
	}
	name := strings.Split(f.Tag.Get("json"), ",")[0]
	if name == "-" {
		return ""
	}
	return name
}

func schemaByName(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	s, ok := schemas[name].(map[string]any)
	if !ok {
		t.Fatalf("the OpenAPI document has no schema named %q", name)
	}
	return s
}

// flattenSchema is a schema's properties, each with its own schema, and its
// required list, following $ref and allOf.
func flattenSchema(t *testing.T, doc map[string]any, schema map[string]any) (map[string]map[string]any, []string) {
	t.Helper()
	props := map[string]map[string]any{}
	var required []string
	var walk func(s map[string]any)
	walk = func(s map[string]any) {
		if ref, ok := s["$ref"].(string); ok {
			walk(schemaByName(t, doc, strings.TrimPrefix(ref, "#/components/schemas/")))
			return
		}
		if all, ok := s["allOf"].([]any); ok {
			for _, part := range all {
				if m, ok := part.(map[string]any); ok {
					walk(m)
				}
			}
		}
		if p, ok := s["properties"].(map[string]any); ok {
			for k, v := range p {
				m, _ := v.(map[string]any)
				props[k] = m
			}
		}
		if r, ok := s["required"].([]any); ok {
			for _, v := range r {
				if name, ok := v.(string); ok && !slices.Contains(required, name) {
					required = append(required, name)
				}
			}
		}
	}
	walk(schema)
	return props, required
}

// refOf is the schema a property names: directly, as an array's items, or as
// the one non-null choice of a nullable one.
func refOf(prop map[string]any) string {
	if ref, ok := prop["$ref"].(string); ok {
		return strings.TrimPrefix(ref, "#/components/schemas/")
	}
	if items, ok := prop["items"].(map[string]any); ok {
		return refOf(items)
	}
	for _, key := range []string{"oneOf", "allOf"} {
		if choices, ok := prop[key].([]any); ok {
			for _, c := range choices {
				if m, ok := c.(map[string]any); ok {
					if ref := refOf(m); ref != "" {
						return ref
					}
				}
			}
		}
	}
	return ""
}

// analysisConstants reads internal/analysis's source and returns its string
// constants by group: a typed group by its type's name, every other constant
// by its own name, and each const block by the name of every constant in
// it, so a group with no shared prefix (the directions) is found by one
// member and gains a value the moment the block does. It also returns the
// names in the compare measures table.
func analysisConstants(t *testing.T) (byType map[string][]string, byName map[string]string, blocks map[string][]string, measures []string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "analysis", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	byType, byName, blocks = map[string][]string{}, map[string]string{}, map[string][]string{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			if gen.Tok == token.VAR {
				for _, spec := range gen.Specs {
					vs := spec.(*ast.ValueSpec)
					for i, name := range vs.Names {
						if name.Name == "compareMeasures" {
							measures = append(measures, tableNames(t, vs.Values[i])...)
						}
					}
				}
				continue
			}
			if gen.Tok != token.CONST {
				continue
			}
			var names, values []string
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING || !name.IsExported() {
						continue
					}
					v := strings.Trim(lit.Value, `"`)
					byName[name.Name] = v
					names, values = append(names, name.Name), append(values, v)
					if typ, ok := vs.Type.(*ast.Ident); ok {
						byType[typ.Name] = append(byType[typ.Name], v)
					}
				}
			}
			for _, n := range names {
				blocks[n] = values
			}
		}
	}
	return byType, byName, blocks, measures
}

// tableNames is the name field of each element of a composite literal.
func tableNames(t *testing.T, expr ast.Expr) []string {
	t.Helper()
	table, ok := expr.(*ast.CompositeLit)
	if !ok {
		t.Fatalf("compareMeasures is not a table literal")
	}
	var out []string
	for _, elt := range table.Elts {
		row, ok := elt.(*ast.CompositeLit)
		if !ok {
			continue
		}
		for _, field := range row.Elts {
			kv, ok := field.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, _ := kv.Key.(*ast.Ident); key != nil && key.Name == "name" {
				if lit, ok := kv.Value.(*ast.BasicLit); ok {
					out = append(out, strings.Trim(lit.Value, `"`))
				}
			}
		}
	}
	return out
}

// withPrefix is the values of the constants whose names start with prefix.
func withPrefix(byName map[string]string, prefix string) []string {
	var out []string
	for name, v := range byName {
		if strings.HasPrefix(name, prefix) {
			out = append(out, v)
		}
	}
	return out
}

// The contract's enums are written by hand from internal/analysis's
// constants, and a generated client switches on them: a value the analysis
// sends and the enum lacks is one a strict client mistypes, which is how
// the job timeline's source enum once went wrong. Each enum is held to its
// constants both ways.
func TestTheAnalysisEnumsAreTheAnalysisConstants(t *testing.T) {
	doc := loadSpec(t)
	byType, byName, blocks, measures := analysisConstants(t)
	if len(byType["WaitCause"]) == 0 || len(measures) == 0 || len(blocks["Regressed"]) == 0 || len(blocks["HostPressured"]) == 0 {
		t.Fatalf("read %d wait causes, %d measures, directions %v and host verdicts %v, so this read the source wrongly",
			len(byType["WaitCause"]), len(measures), blocks["Regressed"], blocks["HostPressured"])
	}
	// A dominant is empty when nothing leads, and the constants have no
	// empty one; the enum's "" is that case, not a constant.
	empty := func(values []string) []string { return append([]string{""}, values...) }
	for _, c := range []struct {
		schema, property string
		want             []string
	}{
		{"QueueWaitCause", "cause", byType["WaitCause"]},
		{"QueueWaitJob", "cause", byType["WaitCause"]},
		{"QueueWaitPool", "dominant", empty(byType["WaitCause"])},
		{"QueueWaitFinding", "dominant", empty(byType["WaitCause"])},
		{"CompareChange", "measure", measures},
		{"CompareFinding", "dominant", empty(measures)},
		{"CompareChange", "direction", blocks["Regressed"]},
		{"HostPressureFinding", "dominant", empty(blocks["HostPressured"])},
		{"HostPressureEvent", "kind", withPrefix(byName, "Event")},
		{"HostPressureThreshold", "measure", withPrefix(byName, "Measure")},
		{"StartupFailureGroup", "scope", withPrefix(byName, "Scope")},
		{"StartupFailureGroup", "stage", withPrefix(byName, "Stage")},
		{"StartupFailureGroup", "explanation", empty(withPrefix(byName, "Explain"))},
	} {
		props, _ := flattenSchema(t, doc, schemaByName(t, doc, c.schema))
		var inDoc []string
		enum, _ := props[c.property]["enum"].([]any)
		for _, v := range enum {
			s, _ := v.(string)
			inDoc = append(inDoc, s)
		}
		for _, v := range c.want {
			if !slices.Contains(inDoc, v) {
				t.Errorf("%s.%s: internal/analysis sends %q and the enum in api/openapi.yaml does not have it", c.schema, c.property, v)
			}
		}
		for _, v := range inDoc {
			if !slices.Contains(c.want, v) {
				t.Errorf("%s.%s: the enum in api/openapi.yaml has %q and internal/analysis has no constant for it", c.schema, c.property, v)
			}
		}
	}
}

// The error codes are listed by family in the Conventions, and the analysis
// refusals are a family a client switches on like the update ones.
func TestTheConventionsNameTheAnalysisRefusals(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "api-surface.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	start := strings.Index(doc, "* Errors return")
	if start < 0 {
		t.Fatal("docs/api-surface.md has no Conventions bullet about errors")
	}
	bullet := doc[start:]
	if end := strings.Index(bullet[1:], "\n* "); end >= 0 {
		bullet = bullet[:end+1]
	}
	// The bullet is wrapped, so its words are compared and not its lines.
	if !strings.Contains(strings.Join(strings.Fields(bullet), " "), "the `analysis.*` refusals listed under [Analyses](#analyses)") {
		t.Errorf("the Conventions' list of error codes does not name the analysis refusals: %q", bullet)
	}
}

// pressedHostWithFailures is a host with two hours of samples and three
// failed jobs on it, each carrying a name no other fixture uses, so a body
// that names them is found by searching for the names alone.
func (h *harness) pressedHostWithFailures(t *testing.T) (*store.Host, time.Time, time.Time) {
	t.Helper()
	pool := h.pool(h.installation(), "linux")
	host := h.host("pressed")
	since, until := analysisWindowForTest(2 * time.Hour)
	var samples []store.HostSample
	for m := since; m.Before(until); m = m.Add(time.Minute) {
		load, free := 3.0, int64(6_000)
		samples = append(samples, store.HostSample{HostID: host.ID, At: m, Capacity: 4, CPUs: 4, MemoryMB: 8_000,
			LoadAverage1: &load, MemoryAvailableMB: &free})
	}
	if err := h.st.RecordHostSamples(h.ctx, samples); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		queued := since.Add(time.Duration(30+i) * time.Minute)
		started, done := queued.Add(5*time.Second), queued.Add(65*time.Second)
		j, _, err := h.st.ApplyJob(h.ctx, &store.Job{
			GitHubJobID: int64(5_000 + i), GitHubRunID: 1, Repo: "acme/secret-repository", Workflow: "secret-workflow", JobName: "secret-job",
			Labels: store.StringSlice{"self-hosted", "linux", "x64"}, State: store.JobCompleted, Conclusion: "failure",
			InstallationID: pool.InstallationID, PoolID: pool.ID, Matched: true, RunnerName: "zoomies-abc",
			QueuedAt: queued, StartedAt: &started, CompletedAt: &done,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.st.StampJobVersions(h.ctx, j.ID, store.JobVersions{HostID: host.ID}); err != nil {
			t.Fatal(err)
		}
	}
	return host, since, until
}

// A token that may read hosts and not jobs is told how a host's failures
// sat against its pressure, and never which jobs they were: the job names
// are what jobs.read guards, and nothing an analysis joins may leave to a
// caller who could not already read it.
func TestHostPressureWithholdsJobNamesFromATokenThatCannotReadJobs(t *testing.T) {
	h := newHarness(t)
	host, since, until := h.pressedHostWithFailures(t)
	path := "/api/v1/analysis/host-pressure" + analysisQuery(since, until, url.Values{"host_id": {host.ID}})

	resp := h.do(request{method: http.MethodGet, path: path, token: h.token("hosts only", store.RoleViewer, "hosts:read")})
	resp.mustStatus(t, http.StatusOK, "host pressure for a token that cannot read jobs")
	for _, name := range []string{"acme/secret-repository", "secret-workflow", "secret-job"} {
		if strings.Contains(string(resp.body), name) {
			t.Errorf("a token without jobs.read was shown %q: %s", name, truncate(resp.body))
		}
	}
	var got analysis.HostPressureFinding
	resp.into(t, &got)
	full, err := h.ctrl.AnalyseHostPressure(h.ctx, controller.AnalysisWindow{Since: since, Until: until}, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Evidence.Events) != 3 {
		t.Fatalf("the controller placed %d events; the seed is not what this test is about", len(full.Evidence.Events))
	}
	if !got.Evidence.JobNamesWithheld {
		t.Error("the finding does not say the job names were withheld")
	}
	// Everything but the names is the finding a reader of jobs gets.
	if got.Summary != full.Summary || got.Confidence != full.Confidence || len(got.Evidence.Events) != len(full.Evidence.Events) ||
		got.Evidence.PressuredEvents != full.Evidence.PressuredEvents || got.Evidence.MeasuredEvents != full.Evidence.MeasuredEvents {
		t.Errorf("withholding the names changed the finding:\n got %+v\nwant %+v", got, full)
	}
	for i, ev := range got.Evidence.Events {
		if want := full.Evidence.Events[i]; ev.JobID != want.JobID || ev.Kind != want.Kind || !ev.At.Equal(want.At) || ev.Pressured != want.Pressured {
			t.Errorf("event %d is %+v; want %+v with only its names left out", i, ev, want)
		}
	}
	if !slices.ContainsFunc(got.NextSteps, func(s analysis.NextStep) bool { return s.Text == analysis.JobNamesWithheldText }) {
		t.Errorf("the finding does not say why the names are missing or what would show them: %+v", got.NextSteps)
	}
}

func TestHostPressureNamesTheJobsToATokenThatMayReadThem(t *testing.T) {
	h := newHarness(t)
	host, since, until := h.pressedHostWithFailures(t)
	path := "/api/v1/analysis/host-pressure" + analysisQuery(since, until, url.Values{"host_id": {host.ID}})

	resp := h.do(request{method: http.MethodGet, path: path, token: h.token("hosts and jobs", store.RoleViewer, "hosts:read", "jobs:read")})
	resp.mustStatus(t, http.StatusOK, "host pressure for a token that can read jobs")
	var got analysis.HostPressureFinding
	resp.into(t, &got)
	if len(got.Evidence.Events) != 3 || got.Evidence.Events[0].JobName != "secret-job" || got.Evidence.Events[0].Repo != "acme/secret-repository" ||
		got.Evidence.Events[0].Workflow != "secret-workflow" {
		t.Errorf("a token that may read jobs was not shown their names: %+v", got.Evidence.Events)
	}
	if got.Evidence.JobNamesWithheld {
		t.Error("the finding says the names were withheld from a token that may read them")
	}
	if slices.ContainsFunc(got.NextSteps, func(s analysis.NextStep) bool { return s.Text == analysis.JobNamesWithheldText }) {
		t.Errorf("the finding says the names were left out when they were not: %+v", got.NextSteps)
	}
	want, err := h.ctrl.AnalyseHostPressure(h.ctx, controller.AnalysisWindow{Since: since, Until: until}, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, resp.body, want)
}

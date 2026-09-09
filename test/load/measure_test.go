//go:build load

package load

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/api"
	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// How many times each read is timed. Enough for a p95 to mean something and
// few enough that the whole measurement is a couple of minutes.
const samples = 40

// reading is one endpoint's timings.
type reading struct {
	what   string
	path   string
	p50    time.Duration
	p95    time.Duration
	worst  time.Duration
	status int
	bytes  int
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1)*p + 0.5)
	return sorted[i]
}

// measure times one path `samples` times and returns the spread.
func measure(t *testing.T, client *http.Client, base, what, path string) reading {
	t.Helper()
	times := make([]time.Duration, 0, samples)
	r := reading{what: what, path: path}
	for range samples {
		started := time.Now()
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		n, err := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		times = append(times, time.Since(started))
		r.status, r.bytes = resp.StatusCode, int(n)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	r.p50, r.p95, r.worst = percentile(times, .5), percentile(times, .95), times[len(times)-1]
	return r
}

// TestTheReadsAPageMakesOverAMonthOfHistory is the measurement.
//
// It is not a pass/fail gate and deliberately asserts almost nothing: the
// roadmap's figures are recorded evidence, and a threshold in here would either
// be so loose it never fires or so tight that a slower CI runner fails a pull
// request that changed nothing. What it does assert is that every read still
// answers, and answers correctly, with a month of history under it -- a query
// that starts erroring or returning an empty page at depth is a defect whatever
// its latency.
func TestTheReadsAPageMakesOverAMonthOfHistory(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// On disk with the real journal, not in memory: the measurement is about
	// what SQLite does with indexes and page cache, and :memory: answers a
	// different question.
	st, err := store.Open(ctx, store.Options{Path: filepath.Join(dir, "zoomies.db")})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	build(t, st)

	cfg := config.Default()
	cfg.Server.Bind = "127.0.0.1:0"
	cfg.Database.Path = filepath.Join(dir, "zoomies.db")
	cfg.Security.DisableAuth = true
	cfg.Security.EncryptionKey = strings.Repeat("a", 64)
	cfg.Security.EncryptionKeyFile = ""
	cfg.Agent.Embedded = false
	key, err := cryptox.ParseKey(cfg.Security.EncryptionKey)
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	bus := events.New()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gh := github.NewFake()
	defer gh.Close()
	ctrl, err := controller.New(controller.Options{
		Store: st, Config: cfg, Key: key,
		Auth:   auth.New(st, cfg, bus, auth.WithLogger(logger)),
		Events: bus, Logger: logger, Clock: time.Now,
	})
	if err != nil {
		t.Fatalf("controller.New: %v", err)
	}
	server, err := api.New(api.Options{Controller: ctrl, Logger: logger})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()
	client := srv.Client()

	// Work going on underneath, which is the state the pages are actually read
	// in: an idle database answers a question nobody asks. One writer, because
	// the store has one.
	var writes atomic.Int64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-tick.C:
				j := &store.Job{
					GitHubJobID: int64(9_000_000 + i), Repo: "acme/live", Workflow: "CI",
					JobName: "live", State: store.JobQueued, QueuedAt: time.Now(),
				}
				if _, err := st.UpsertJob(ctx, j); err == nil {
					writes.Add(1)
				}
			}
		}
	}()

	rightNow := time.Now().UTC().Format(time.RFC3339)
	monthAgo := time.Now().Add(-historyFor).UTC().Format(time.RFC3339)

	// The reads a page makes: the primary navigation's first screen, the deep
	// pages an operator reaches by scrolling, and the two reports.
	reads := []struct{ what, path string }{
		{"Overview: the statistics tiles", "/api/v1/stats"},
		{"Overview: the sparkline samples", "/api/v1/samples?window=1h"},
		{"Overview: the problems drawer", "/api/v1/problems"},
		{"Overview: recent scaling", "/api/v1/scaling-events?limit=10"},
		{"Runners: the first page", "/api/v1/runners?limit=50"},
		{"Runners: page 20 of the history", "/api/v1/runners?limit=50&offset=950&include_removed=true"},
		{"Runners: filtered to idle", "/api/v1/runners?limit=50&state=idle"},
		{"Jobs: the first page", "/api/v1/jobs?limit=50"},
		{"Jobs: page 100", "/api/v1/jobs?limit=50&offset=4950"},
		{"Jobs: one repository", "/api/v1/jobs?limit=50&repo=acme/service-01"},
		{"Hosts", "/api/v1/hosts"},
		{"Pools", "/api/v1/pools"},
		{"Audit: the first page", "/api/v1/audit?limit=50"},
		{"Audit: filtered by action", "/api/v1/audit?limit=50&action=runner.drain"},
		{"Audit: page 50", "/api/v1/audit?limit=50&offset=2450"},
		{"Usage: a month by pool", "/api/v1/usage?group_by=pool&from=" + monthAgo + "&to=" + rightNow},
		{"Usage: a month by repository", "/api/v1/usage?group_by=repository&from=" + monthAgo + "&to=" + rightNow},
	}

	results := make([]reading, 0, len(reads))
	for _, r := range reads {
		got := measure(t, client, srv.URL, r.what, r.path)
		if got.status != http.StatusOK {
			t.Errorf("%s (%s) answered %d, not 200", r.what, r.path, got.status)
		}
		results = append(results, got)
	}
	close(stop)
	<-done

	// A deep page is still a page of rows, not an empty one: a read that got
	// fast by stopping at the first thousand rows would look excellent here.
	var body struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	resp, err := client.Get(srv.URL + "/api/v1/jobs?limit=50&offset=4950")
	if err != nil {
		t.Fatalf("deep page: %v", err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the deep page: %v", err)
	}
	resp.Body.Close()
	if len(body.Items) != 50 {
		t.Errorf("the deep jobs page returned %d rows, want 50", len(body.Items))
	}
	if body.Total < jobs {
		t.Errorf("the deep jobs page reported a total of %d, want at least the %d written", body.Total, jobs)
	}

	writeRecord(t, results, int(writes.Load()))
}

// writeRecord prints the table and, when asked, files it under
// roadmap/validation/ where the evidence lives.
func writeRecord(t *testing.T, results []reading, writes int) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "| Read | Path | p50 | p95 | worst | bytes |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, r := range results {
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s | %s | %s |\n",
			r.what, r.path,
			r.p50.Round(100*time.Microsecond), r.p95.Round(100*time.Microsecond),
			r.worst.Round(100*time.Microsecond), humanBytes(r.bytes))
	}
	t.Logf("\n%s\nwrites while reading: %d", b.String(), writes)

	out := os.Getenv("ZOOMIES_LOAD_RECORD")
	if out == "" {
		return
	}
	header := fmt.Sprintf(`# Load measurement: %s

Written by `+"`make measure`"+` on %s.

| | |
| --- | --- |
| Commit | `+"`%s`"+` |
| Machine | %s/%s, %d CPUs, %s |
| Fixture | %d hosts, %d pools, %d runners, %d jobs, %d audit rows over %s of history |
| Written through | `+"`internal/store`"+`, the same single writer the controller uses |
| Read through | the real router and the real store, on disk with its usual journal |
| Load while reading | %d job writes landed during the timings |
| Samples | %d requests per row |

%s
`, commit(), time.Now().UTC().Format(time.RFC3339), commit(),
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.Version(),
		hosts, pools, runners, jobs, auditRows, historyFor, writes, samples, b.String())
	if err := os.WriteFile(out, []byte(header), 0o644); err != nil {
		t.Fatalf("writing the record to %s: %v", out, err)
	}
	t.Logf("record written to %s", out)
}

func commit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

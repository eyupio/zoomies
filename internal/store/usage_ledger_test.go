package store

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"testing"
	"time"
)

// ledgerFleet is one store run through a hundred simulated days, with the
// names its random IDs stand for so two stores' reports can be compared.
type ledgerFleet struct {
	s     *Store
	now   time.Time
	names map[string]string
}

// simulateLedgerFleet drives a fleet through days of runners -- two pools,
// one priced and one not, on two hosts, some crossing midnight, some whose
// cleanup is never confirmed -- and after each day runs what the controller's
// prune pass runs, with the given retention. Zero keeps everything.
//
// Every instant is a whole multiple of 100 seconds from midnight, so every
// slice of a runner is a whole number of seconds and, at 0.36 an hour, a whole
// number of cents: equality to the second is then a fair thing to ask.
func simulateLedgerFleet(t *testing.T, days int, runners, sessions time.Duration) *ledgerFleet {
	t.Helper()
	ctx := context.Background()
	f := &ledgerFleet{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), names: map[string]string{"": ""}}
	f.s = newTestStoreAt(t, func() time.Time { return f.now })
	inst, priced, h1 := seedPool(t, f.s)
	pricedPool(t, f.s, priced, 0.36)
	unpriced := &Pool{Name: "unpriced", InstallationID: inst.ID, Labels: StringSlice{"unpriced"}, Backend: BackendDocker,
		MaxRunners: 4, IdleTimeout: Duration(5 * time.Minute), Ephemeral: true, DockerMode: DockerNone, Enabled: true}
	if err := f.s.CreatePool(ctx, unpriced); err != nil {
		t.Fatal(err)
	}
	h2 := &Host{Name: "vm-2", Capacity: 4, Backends: StringSlice{"docker"}}
	if err := f.s.CreateHost(ctx, h2); err != nil {
		t.Fatal(err)
	}
	f.names[inst.ID], f.names[priced.ID], f.names[unpriced.ID] = "installation", "priced", "unpriced"
	f.names[h1.ID], f.names[h2.ID] = "vm-1", "vm-2"

	seed := uint32(20260101)
	next := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % n
	}
	day0 := f.now
	job := int64(1)
	for d := range days {
		midnight := day0.Add(time.Duration(d) * 24 * time.Hour)
		for i := range 3 + next(4) {
			start := midnight.Add(time.Duration(next(864)) * 100 * time.Second)
			finish := start.Add(time.Duration(1+next(180)) * 100 * time.Second)
			pool, host := priced, h1
			if next(3) == 0 {
				pool = unpriced
			}
			if next(2) == 0 {
				host = h2
			}
			r := runnerLife(t, f.s, &f.now, pool, host, fmt.Sprintf("d%d-r%d", d, i), start, finish, job)
			job++
			// One in five never has its cleanup confirmed: the host went away
			// or GitHub kept the registration, and the prune is what records it.
			if next(5) != 0 {
				for _, side := range []bool{true, false} {
					if _, err := f.s.ConfirmRunnerCleanup(ctx, r.ID, side); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		f.now = midnight.Add(24 * time.Hour)
		if _, err := f.s.RollUpUsage(ctx, f.now); err != nil {
			t.Fatal(err)
		}
		if runners > 0 {
			if _, err := f.s.PruneRunners(ctx, f.now.Add(-runners)); err != nil {
				t.Fatal(err)
			}
		}
		if sessions > 0 {
			if _, err := f.s.PruneRunnerSessions(ctx, f.now.Add(-sessions)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The report is read part way through a day, as it would be.
	f.now = f.now.Add(5 * time.Hour)
	return f
}

// report is a Usage call with every ID replaced by the name it stands for and
// each cost rounded to the cent, so two stores' reports compare directly.
func (f *ledgerFleet) report(t *testing.T, from, to time.Time, group UsageGroup, interval UsageInterval) []UsageRow {
	t.Helper()
	rows, err := f.s.UsageWithInterval(context.Background(), from, to, group, interval)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	for i := range rows {
		name, ok := f.names[rows[i].Key]
		if !ok {
			t.Fatalf("report has a key %q the fleet never made", rows[i].Key)
		}
		rows[i].Key = name
		if c := rows[i].EstimatedCost; c != nil {
			rounded := math.Round(*c*100) / 100
			rows[i].EstimatedCost = &rounded
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	return rows
}

// allocation is the part of a report the roll-up answers for: runner time and
// its cost, in total and per bucket. Job figures come from job rows and are
// retention.jobs' to keep, and a job's host is read through its runner's
// session, so neither is the roll-up's to vouch for once the sessions are gone.
type allocation struct {
	Key     string
	Seconds float64
	Cost    *float64
	Buckets []float64
}

func allocations(rows []UsageRow) []allocation {
	var out []allocation
	for _, r := range rows {
		if r.AllocatedRunnerSeconds == nil || *r.AllocatedRunnerSeconds == 0 {
			continue
		}
		a := allocation{Key: r.Key, Seconds: *r.AllocatedRunnerSeconds, Cost: r.EstimatedCost}
		for _, b := range r.History {
			a.Buckets = append(a.Buckets, b.AllocatedSeconds)
		}
		out = append(out, a)
	}
	return out
}

// A 90-day report is what an operator reconciles against a quarter's bill.
// Before the ledger it was computed from runner rows, which retention.runners
// deletes after a week, so it quietly lost every runner-hour older than that.
// With the sessions and the daily roll-up behind it, a report taken with
// seven-day runner retention must match one taken with retention off to the
// second -- and still match once the sessions themselves have been pruned
// down to a fortnight, leaving only the roll-up for the older days.
func TestANinetyDayReportWithAWeekOfRunnerRetentionMatchesOneWithRetentionOff(t *testing.T) {
	const days = 100
	week, fortnight := 7*24*time.Hour, 14*24*time.Hour
	kept := simulateLedgerFleet(t, days, 0, 0)
	pruned := simulateLedgerFleet(t, days, week, 0)
	rolledOnly := simulateLedgerFleet(t, days, week, fortnight)

	// The retention has to have bitten, or the comparison proves nothing.
	var keptRunners, prunedRunners int
	if err := kept.s.read.QueryRow(`SELECT COUNT(*) FROM runners`).Scan(&keptRunners); err != nil {
		t.Fatal(err)
	}
	if err := pruned.s.read.QueryRow(`SELECT COUNT(*) FROM runners`).Scan(&prunedRunners); err != nil {
		t.Fatal(err)
	}
	if prunedRunners*5 > keptRunners {
		t.Fatalf("the pruned fleet still has %d of %d runner rows; retention did not run", prunedRunners, keptRunners)
	}
	oldest, err := rolledOnly.s.RunnerSessions(context.Background(), time.Unix(0, 0), rolledOnly.now.Add(-30*24*time.Hour))
	if err != nil || len(oldest) != 0 {
		t.Fatalf("the fleet with a fortnight of sessions still has %d month-old sessions, %v", len(oldest), err)
	}

	end := kept.now
	today := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	windows := []struct {
		name     string
		from, to time.Time
		interval UsageInterval
		// sessionsNeeded marks a window the roll-up cannot serve whole -- a
		// day split by the window's edge or by an hourly bucket -- which is
		// then read from the sessions, and so is complete only while they are.
		sessionsNeeded bool
	}{
		{"ninety whole days", today.Add(-90 * 24 * time.Hour), today, UsageDaily, false},
		{"ninety days to now", end.Add(-90 * 24 * time.Hour), end, UsageAutoInterval, true},
		{"ninety days from midnight to now", today.Add(-90 * 24 * time.Hour), end, UsageDaily, false},
		{"a fortnight of hours", today.Add(-14 * 24 * time.Hour), today, UsageHourly, true},
	}
	for _, w := range windows {
		for _, group := range []UsageGroup{UsageByPool, UsageByHost, UsageByInstallation} {
			want := kept.report(t, w.from, w.to, group, w.interval)
			if len(want) == 0 {
				t.Fatalf("%s by %s: the reference report is empty", w.name, group)
			}
			if got := pruned.report(t, w.from, w.to, group, w.interval); !reflect.DeepEqual(got, want) {
				t.Errorf("%s by %s with a week of runner retention:\n got %+v\nwant %+v", w.name, group, summary(got), summary(want))
			}
			if w.sessionsNeeded {
				continue
			}
			if got := rolledOnly.report(t, w.from, w.to, group, w.interval); !reflect.DeepEqual(allocations(got), allocations(want)) {
				t.Errorf("%s by %s from the roll-up alone:\n got %+v\nwant %+v", w.name, group, summary(got), summary(want))
			}
		}
	}
}

// summary is the part of a row worth reading when two differ.
func summary(rows []UsageRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		alloc, cost := -1.0, -1.0
		if r.AllocatedRunnerSeconds != nil {
			alloc = *r.AllocatedRunnerSeconds
		}
		if r.EstimatedCost != nil {
			cost = *r.EstimatedCost
		}
		out = append(out, fmt.Sprintf("%s: %.0fs, cost %.2f, jobs %d, exec %.0fs, peak %d", r.Key, alloc, cost, r.Jobs, r.JobExecutionSeconds, r.PeakConcurrency))
	}
	return out
}

// A price change is not retrospective. A runner that has a session is priced
// at the rate its session recorded, whether the report reads it from the row,
// the session or the roll-up, so a report across the change agrees with
// itself and with last month's reconciliation.
func TestTheReportPricesARecordedRunnerAtTheRateWhenItRan(t *testing.T) {
	day := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	now := day
	s := newTestStoreAt(t, func() time.Time { return now })
	_, pool, host := seedPool(t, s)
	pricedPool(t, s, pool, 0.36)
	confirmed(t, s, &now, pool, host, "before-the-change", day.Add(time.Hour), day.Add(2*time.Hour))
	pricedPool(t, s, pool, 9)
	now = day.Add(3 * time.Hour)

	row := usageRow(t, s, day, day.Add(24*time.Hour), UsageByPool, pool.ID)
	if row.EstimatedCost == nil || math.Abs(*row.EstimatedCost-0.36) > 1e-9 {
		t.Fatalf("cost = %v, want the 0.36 the hour cost when it ran", row.EstimatedCost)
	}
}

package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// A reconcile pass reads the live runners every few seconds, and the table also
// keeps every runner that was removed, for the history pages and the usage
// figures. A busy fleet makes thousands of runners a day, so the live ones are a
// few dozen rows among tens of thousands that the query is not asking for but,
// with nothing to find them by, has to walk past. These time the two reads a pass
// makes, over a table that is nearly all history.

const (
	historyPools   = 8
	historyRemoved = 12000
	historyLive    = 40
)

// runnersWithHistory is a store holding a few live runners among a great many
// removed ones, and checks it holds exactly that before anything is timed.
func runnersWithHistory(b *testing.B) (*Store, []string) {
	b.Helper()
	ctx := context.Background()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	tick := now.Add(-30 * 24 * time.Hour)
	s := newTestStoreAt(b, func() time.Time { tick = tick.Add(time.Second); return tick })
	inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme", TargetType: TargetOrg}
	if err := s.CreateInstallation(ctx, inst); err != nil {
		b.Fatal(err)
	}
	host := &Host{Name: "vm-1", Capacity: 64, Backends: StringSlice{"docker"}}
	if err := s.CreateHost(ctx, host); err != nil {
		b.Fatal(err)
	}
	var pools []string
	for i := range historyPools {
		p := defaultBenchPool(inst, i)
		if err := s.CreatePool(ctx, p); err != nil {
			b.Fatal(err)
		}
		pools = append(pools, p.ID)
	}
	for i := range historyRemoved {
		r := &Runner{PoolID: pools[i%historyPools], HostID: host.ID, Name: fmt.Sprintf("old-%d", i), State: RunnerRemoved}
		if err := s.CreateRunner(ctx, r); err != nil {
			b.Fatal(err)
		}
	}
	for i := range historyLive {
		r := &Runner{PoolID: pools[i%historyPools], HostID: host.ID, Name: fmt.Sprintf("live-%d", i), State: RunnerIdle}
		if err := s.CreateRunner(ctx, r); err != nil {
			b.Fatal(err)
		}
	}
	var total, removed int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(state = 'removed'), 0) FROM runners`).Scan(&total, &removed); err != nil {
		b.Fatal(err)
	}
	if removed != historyRemoved || total != historyRemoved+historyLive {
		b.Fatalf("the table holds %d runners of which %d removed, want %d and %d", total, removed, historyRemoved+historyLive, historyRemoved)
	}
	return s, pools
}

func BenchmarkSchedulingRunnersPastRemovedHistory(b *testing.B) {
	s, _ := runnersWithHistory(b)
	ctx := context.Background()
	check := func() {
		got, err := s.SchedulingRunners(ctx)
		n := 0
		for _, rs := range got {
			n += len(rs)
		}
		if err != nil || n != historyLive {
			b.Fatalf("SchedulingRunners returned %d runners, %v; want the %d live ones", n, err, historyLive)
		}
	}
	check()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := s.SchedulingRunners(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListRunnersForPoolPastRemovedHistory(b *testing.B) {
	s, pools := runnersWithHistory(b)
	ctx := context.Background()
	got, err := s.ListRunnersForPool(ctx, pools[0])
	if err != nil || len(got) != historyLive/historyPools {
		b.Fatalf("ListRunnersForPool returned %d runners, %v; want the pool's %d live ones", len(got), err, historyLive/historyPools)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := s.ListRunnersForPool(ctx, pools[0]); err != nil {
			b.Fatal(err)
		}
	}
}

// planOf is what SQLite says it will do for a statement, one line per step.
func planOf(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	rows, err := s.read.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	return strings.Join(plan, "\n")
}

// The two reads a pass makes of the live runners have to be answered from the live
// rows alone. Removed runners are kept for the history and are most of the table on
// a busy fleet, so a plan that walks the table, or sorts what it found, is paid for
// on every pass for rows nobody asked about.
func TestTheRunnerReadsOfAPassAreAnsweredFromThePartialIndexes(t *testing.T) {
	s := newTestStore(t)
	for _, tc := range []struct {
		name, query, index string
		args               []any
	}{
		{"every live runner", schedulingRunnersSQL, "idx_runners_live_created", nil},
		{"one pool's live runners", runnersForPoolSQL, "idx_runners_live_pool_created", []any{"pool_x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := planOf(t, s, tc.query, tc.args...)
			if !strings.Contains(plan, "USING INDEX "+tc.index) {
				t.Errorf("the read does not use %s:\n%s", tc.index, plan)
			}
			if strings.Contains(plan, "TEMP B-TREE") {
				t.Errorf("the read sorts what it found, so its index does not give the order it asks for:\n%s", plan)
			}
		})
	}
}

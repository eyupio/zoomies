package store

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// confirmed walks a runner through its life and confirms both sides of its
// cleanup, which is what writes its session.
func confirmed(t *testing.T, s *Store, now *time.Time, pool *Pool, host *Host, name string, start, finish time.Time) *Runner {
	t.Helper()
	r := runnerLife(t, s, now, pool, host, name, start, finish, 0)
	for _, side := range []bool{true, false} {
		if _, err := s.ConfirmRunnerCleanup(context.Background(), r.ID, side); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func pricedPool(t *testing.T, s *Store, pool *Pool, rate float64) {
	t.Helper()
	pool.CostPerRunnerHour = &rate
	if err := s.UpdatePool(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
}

func usageDays(t *testing.T, s *Store) []UsageDay {
	t.Helper()
	days, err := s.UsageDays(context.Background(), time.Unix(0, 0), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("UsageDays: %v", err)
	}
	return days
}

// A runner that lives across midnight is two days' allocation, cut at the
// boundary, in whole seconds and whole cents -- integers, so that a month is
// exactly the sum of its days.
func TestTheRollUpCutsARunnerAtMidnightInWholeSecondsAndCents(t *testing.T) {
	midnight := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	now := midnight.Add(-time.Hour)
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	inst, pool, host := seedPool(t, s)
	pricedPool(t, s, pool, 0.36)
	confirmed(t, s, &now, pool, host, "across-midnight", midnight.Add(-time.Hour), midnight.Add(2*time.Hour))

	n, err := s.RollUpUsage(ctx, midnight.Add(3*24*time.Hour))
	if err != nil || n != 4 {
		t.Fatalf("rolled up %d days, %v; want the four from the session's first day", n, err)
	}
	cents := func(c int64) *int64 { return &c }
	want := []UsageDay{
		{Day: midnight.Add(-24 * time.Hour), PoolID: pool.ID, HostID: host.ID, InstallationID: inst.ID, AllocatedSeconds: 3600, CostMinor: cents(36)},
		{Day: midnight, PoolID: pool.ID, HostID: host.ID, InstallationID: inst.ID, AllocatedSeconds: 7200, CostMinor: cents(72)},
	}
	if got := usageDays(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("roll-up = %+v, want %+v", got, want)
	}
	// A second pass over the same days writes nothing: a day is rolled up
	// once, or a report counts it twice.
	if n, err := s.RollUpUsage(ctx, midnight.Add(3*24*time.Hour)); err != nil || n != 0 {
		t.Fatalf("second roll-up absorbed %d days, %v; want 0", n, err)
	}
	if got := usageDays(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("roll-up after a second pass = %+v, want %+v", got, want)
	}
	// Later passes take only the days since the last one.
	for _, wantDays := range []int{2, 0} {
		if n, err := s.RollUpUsage(ctx, midnight.Add(5*24*time.Hour)); err != nil || n != wantDays {
			t.Fatalf("a later roll-up absorbed %d days, %v; want %d", n, err, wantDays)
		}
	}
	from, until, ok, err := s.UsageRollupRange(ctx)
	if err != nil || !ok || !from.Equal(midnight.Add(-24*time.Hour)) || !until.Equal(midnight.Add(5*24*time.Hour)) {
		t.Fatalf("roll-up range = %v..%v (%v, %v)", from, until, ok, err)
	}
}

// A runner with no session yet will get one, and its days must still be open
// when it does. The roll-up stops at the start of the day the oldest such
// runner was created, and moves on once its session is written.
func TestTheRollUpWaitsForARunnerThatHasNoSessionYet(t *testing.T) {
	day := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	now := day
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	confirmed(t, s, &now, pool, host, "done", day.Add(time.Hour), day.Add(2*time.Hour))
	pending := runnerLife(t, s, &now, pool, host, "cleaning", day.Add(26*time.Hour), day.Add(27*time.Hour), 0)

	if _, err := s.RollUpUsage(ctx, day.Add(5*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, until, _, _ := s.UsageRollupRange(ctx); !until.Equal(day.Add(24 * time.Hour)) {
		t.Fatalf("rolled up until %v with a runner from %v still unrecorded; want %v", until, day.Add(24*time.Hour), day.Add(24*time.Hour))
	}
	for _, side := range []bool{true, false} {
		if _, err := s.ConfirmRunnerCleanup(ctx, pending.ID, side); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RollUpUsage(ctx, day.Add(5*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, d := range usageDays(t, s) {
		total += d.AllocatedSeconds
	}
	if total != 2*3600 {
		t.Fatalf("roll-up holds %ds, want both runners' two hours", total)
	}
}

// The roll-up is a pure function of the sessions. With every runner row gone
// and the roll-up thrown away, running it again from the sessions alone gives
// the same rows -- which is what lets an operator, or a later build, check or
// rebuild it.
func TestTheRollUpIsReproducibleFromTheSessionsAlone(t *testing.T) {
	day := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	now := day
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	_, pool, host := seedPool(t, s)
	pricedPool(t, s, pool, 0.5)
	other := &Pool{Name: "unpriced", InstallationID: pool.InstallationID, Labels: StringSlice{"other"}, Backend: BackendDocker,
		MaxRunners: 4, IdleTimeout: Duration(5 * time.Minute), Ephemeral: true, DockerMode: DockerNone, Enabled: true}
	if err := s.CreatePool(ctx, other); err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		start := day.Add(time.Duration(i)*7*time.Hour + time.Duration(i*37)*time.Second)
		p := pool
		if i%3 == 0 {
			p = other
		}
		confirmed(t, s, &now, p, host, "r"+string(rune('a'+i)), start, start.Add(time.Duration(1+i%5)*113*time.Minute))
	}
	until := day.Add(10 * 24 * time.Hour)
	if _, err := s.RollUpUsage(ctx, until); err != nil {
		t.Fatal(err)
	}
	first := usageDays(t, s)
	if len(first) == 0 {
		t.Fatal("the roll-up wrote nothing")
	}

	sessions, err := s.RunnerSessions(ctx, time.Unix(0, 0), until)
	if err != nil {
		t.Fatal(err)
	}
	if got := RollUpSessions(sessions, day, until); !reflect.DeepEqual(got, first) {
		t.Fatalf("roll-up recomputed from the sessions = %+v, want %+v", got, first)
	}

	for _, q := range []string{`DELETE FROM jobs`, `DELETE FROM runners`, `DELETE FROM usage_daily`, `DELETE FROM usage_rollup`} {
		if _, err := s.write.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RollUpUsage(ctx, until); err != nil {
		t.Fatal(err)
	}
	if again := usageDays(t, s); !reflect.DeepEqual(again, first) {
		t.Fatalf("roll-up rebuilt from the sessions alone = %+v, want %+v", again, first)
	}
}

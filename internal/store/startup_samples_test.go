package store

import (
	"context"
	"testing"
	"time"
)

// The Overview's start-up percentiles used to come from a page of runner rows
// whose limit was silently clamped to 500, so on a fleet that starts more
// runners than that in a day they described the newest few hundred and called
// it the window. The query covers the window, however many runners it holds.
func TestStartupSamplesCoverTheWholeWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, host := seedPool(t, s)

	const runners = 505
	for i := 0; i < runners; i++ {
		r := &Runner{PoolID: pool.ID, HostID: host.ID, Name: "r" + itoa(i), State: RunnerIdle}
		if err := s.CreateRunner(ctx, r); err != nil {
			t.Fatalf("CreateRunner: %v", err)
		}
		row, err := s.GetRunner(ctx, r.ID)
		if err != nil {
			t.Fatalf("GetRunner: %v", err)
		}
		started := row.CreatedAt.Add(time.Duration(i+1) * time.Second)
		row.ContainerStartedAt = &started
		if i%2 == 0 {
			// Half registered; the other half never did, and count for the
			// start only.
			registered := started.Add(2 * time.Second)
			row.RegisteredAt = &registered
		}
		if err := s.UpdateRunner(ctx, row); err != nil {
			t.Fatalf("UpdateRunner: %v", err)
		}
	}
	// And one that is still starting, which contributes nothing.
	if err := s.CreateRunner(ctx, &Runner{PoolID: pool.ID, HostID: host.ID, Name: "starting", State: RunnerProvisioning}); err != nil {
		t.Fatalf("CreateRunner: %v", err)
	}

	startup, registration, err := s.StartupSamples(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("StartupSamples: %v", err)
	}
	if len(startup) != runners || len(registration) != (runners+1)/2 {
		t.Fatalf("%d start and %d registration samples, want %d and %d", len(startup), len(registration), runners, (runners+1)/2)
	}
	if startup[0] != 1000 || startup[len(startup)-1] != int64(runners)*1000 {
		t.Fatalf("start samples run %d..%d ms, want 1000..%d sorted", startup[0], startup[len(startup)-1], runners*1000)
	}
	for _, r := range registration {
		if r != 2000 {
			t.Fatalf("registration sample %d, want 2000", r)
		}
	}

	if startup, _, err = s.StartupSamples(ctx, time.Now().Add(time.Hour)); err != nil || len(startup) != 0 {
		t.Fatalf("a window that starts in the future sampled %d runners (%v)", len(startup), err)
	}
}

func itoa(i int) string {
	return time.Duration(i).String()[:len(time.Duration(i).String())-2]
}

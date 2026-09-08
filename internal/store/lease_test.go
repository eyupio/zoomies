package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Two controllers over one database both mint runner credentials, both reclaim
// hosts they think are lost, and both reap workloads the other created. The
// lease is what makes the second one refuse to start, and it has to be decided
// inside one transaction or two starting together both win.
func TestASecondControllerIsRefusedAndToldWhoHoldsTheLease(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ttl := time.Minute

	first := &ControllerLease{Holder: "ctl_first", Host: "vm-a", PID: 4242, Version: "0.2.0"}
	if _, err := s.AcquireControllerLease(ctx, first, ttl, false); err != nil {
		t.Fatalf("the first controller could not take the lease: %v", err)
	}

	second := &ControllerLease{Holder: "ctl_second", Host: "vm-b", PID: 77}
	held, err := s.AcquireControllerLease(ctx, second, ttl, false)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("the second controller started anyway: %v", err)
	}
	// The refusal has to name the holder, or an operator cannot go and find it.
	if held == nil || held.Holder != "ctl_first" {
		t.Fatalf("refused with %+v, want the first controller's lease", held)
	}
	for _, want := range []string{"ctl_first", "4242", "vm-a"} {
		if !strings.Contains(held.Describe(), want) {
			t.Errorf("the holder reads %q, want it to name %q", held.Describe(), want)
		}
	}
}

// A controller that was killed rather than stopped must not need an operator
// to come and clear up after it.
func TestALeaseNobodyIsRenewingIsTakenWithoutCeremony(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	s, err := Open(context.Background(), Options{Path: ":memory:", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	ttl := time.Minute

	if _, err := s.AcquireControllerLease(ctx, &ControllerLease{Holder: "ctl_dead", Host: "vm-a"}, ttl, false); err != nil {
		t.Fatalf("AcquireControllerLease: %v", err)
	}

	// Still live: refused.
	now = now.Add(ttl - time.Second)
	if _, err := s.AcquireControllerLease(ctx, &ControllerLease{Holder: "ctl_next"}, ttl, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("a lease renewed a second ago was taken: %v", err)
	}

	// Past the window: taken.
	now = now.Add(2 * time.Second)
	if _, err := s.AcquireControllerLease(ctx, &ControllerLease{Holder: "ctl_next", Host: "vm-b"}, ttl, false); err != nil {
		t.Fatalf("a lease nobody has renewed was not taken: %v", err)
	}
	holder, err := s.ControllerLeaseHolder(ctx)
	if err != nil || holder.Holder != "ctl_next" {
		t.Fatalf("holder = %+v (%v), want ctl_next", holder, err)
	}
}

// --takeover is the operator saying they know what the other one is, which is
// the only thing that can be true when the rival is on a machine this one
// cannot see.
func TestTakeoverTakesALeaseThatIsStillLive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.AcquireControllerLease(ctx, &ControllerLease{Holder: "ctl_first", Host: "vm-a"}, time.Minute, false); err != nil {
		t.Fatalf("AcquireControllerLease: %v", err)
	}
	if _, err := s.AcquireControllerLease(ctx, &ControllerLease{Holder: "ctl_second", Host: "vm-b"}, time.Minute, true); err != nil {
		t.Fatalf("--takeover was refused: %v", err)
	}

	// And the one that was displaced finds out the next time it renews, which
	// is the only way it can know: nothing tells it.
	if _, err := s.RenewControllerLease(ctx, "ctl_first"); !errors.Is(err, ErrConflict) {
		t.Fatalf("the displaced controller renewed happily: %v", err)
	}
	held, err := s.RenewControllerLease(ctx, "ctl_first")
	if !errors.Is(err, ErrConflict) || held == nil || held.Holder != "ctl_second" {
		t.Fatalf("renew = %+v (%v), want the new holder named", held, err)
	}
}

// A controller restarting fast enough to beat its own lease's expiry must not
// refuse to start against itself.
func TestAControllerTakesItsOwnLeaseBackAndKeepsWhenItFirstHadIt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := &ControllerLease{Holder: "ctl_same", Host: "vm-a"}
	if _, err := s.AcquireControllerLease(ctx, first, time.Hour, false); err != nil {
		t.Fatalf("AcquireControllerLease: %v", err)
	}
	again := &ControllerLease{Holder: "ctl_same", Host: "vm-a"}
	if _, err := s.AcquireControllerLease(ctx, again, time.Hour, false); err != nil {
		t.Fatalf("a controller was refused its own lease: %v", err)
	}
	if !again.AcquiredAt.Equal(first.AcquiredAt) {
		t.Errorf("acquired_at = %v, want the original %v: it is when this controller took charge",
			again.AcquiredAt, first.AcquiredAt)
	}
}

// A clean shutdown hands the lease back, so the next controller starts at once
// instead of waiting out a lease nobody holds.
func TestAReleasedLeaseLetsTheNextControllerStartAtOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.AcquireControllerLease(ctx, &ControllerLease{Holder: "ctl_first"}, time.Hour, false); err != nil {
		t.Fatalf("AcquireControllerLease: %v", err)
	}
	if err := s.ReleaseControllerLease(ctx, "ctl_first"); err != nil {
		t.Fatalf("ReleaseControllerLease: %v", err)
	}
	if _, err := s.AcquireControllerLease(ctx, &ControllerLease{Holder: "ctl_second"}, time.Hour, false); err != nil {
		t.Fatalf("the next controller was refused after a clean shutdown: %v", err)
	}
	// And a controller that is no longer the holder cannot release the lease
	// out from under the one that is.
	if err := s.ReleaseControllerLease(ctx, "ctl_first"); err != nil {
		t.Fatalf("ReleaseControllerLease: %v", err)
	}
	if _, err := s.ControllerLeaseHolder(ctx); err != nil {
		t.Fatalf("the stale holder deleted the live lease: %v", err)
	}
}

// The service unit is Restart=always, so a controller that is killed comes
// back within seconds under a new holder id. Without this it would find its
// predecessor's lease renewed moments ago and refuse to start, restart-looping
// for the whole lease window after every crash.
//
// It is safe because the caller holds the database lock before it asks: the
// lock proves nothing else on this host has the database, so a lease naming
// this host belongs to something that is gone.
func TestARestartedControllerReclaimsItsPredecessorsLeaseOnTheSameHost(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	killed := &ControllerLease{Holder: "ctl_killed", Host: "vm-a", PID: 100}
	if _, err := s.AcquireControllerLease(ctx, killed, time.Hour, false); err != nil {
		t.Fatalf("AcquireControllerLease: %v", err)
	}

	// Seconds later, under a new identity, on the same machine.
	restarted := &ControllerLease{Holder: "ctl_restarted", Host: "vm-a", PID: 101}
	if _, err := s.AcquireControllerLease(ctx, restarted, time.Hour, false); err != nil {
		t.Fatalf("a restarted controller was refused its own host's lease: %v", err)
	}

	// And the case the lease actually exists for is untouched: another
	// machine, whose controller this host's lock cannot see, is still refused.
	elsewhere := &ControllerLease{Holder: "ctl_elsewhere", Host: "vm-b", PID: 1}
	held, err := s.AcquireControllerLease(ctx, elsewhere, time.Hour, false)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("a controller on another host started anyway: %v", err)
	}
	if held == nil || held.Host != "vm-a" {
		t.Fatalf("refused with %+v, want the holder on vm-a", held)
	}

	// A lease with no host recorded is nobody's to reclaim by hostname.
	if _, err := s.AcquireControllerLease(ctx, &ControllerLease{Holder: "ctl_nameless"}, time.Hour, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("a controller with no hostname reclaimed somebody else's lease: %v", err)
	}
}

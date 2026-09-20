package store

import "testing"

func TestHostEditsKeepConcurrentCordonAndReportedFacts(t *testing.T) {
	s := newTestStore(t)
	_, _, host := seedPool(t, s)
	ctx := t.Context()
	if err := s.SetHostCordoned(ctx, host.ID, true); err != nil {
		t.Fatal(err)
	}
	host.CPUs = 16
	if err := s.SetHostReported(ctx, host); err != nil {
		t.Fatal(err)
	}
	capacity, reserve := 2, 1
	labels := StringMap{"rack": "east"}
	if err := s.PatchHost(ctx, host.ID, HostChanges{Labels: &labels, ReserveCPUs: &reserve}); err != nil {
		t.Fatal(err)
	}
	if err := s.PatchHost(ctx, host.ID, HostChanges{Capacity: &capacity}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetHost(ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Cordoned || got.CPUs != 16 || got.Capacity != 2 || got.ReserveCPUs != 1 || got.Labels["rack"] != "east" {
		t.Fatalf("an edit overwrote another change: %+v", got)
	}
	// Zero and an empty map are edits, not omitted values.
	capacity = 0
	labels = StringMap{}
	if err := s.PatchHost(ctx, host.ID, HostChanges{Capacity: &capacity, Labels: &labels}); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetHost(ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Capacity != 0 || len(got.Labels) != 0 || !got.Cordoned || got.ReserveCPUs != 1 {
		t.Fatalf("explicit zero or empty labels were not applied independently: %+v", got)
	}
}

func TestHostFeaturesFollowWhatTheAgentReports(t *testing.T) {
	// A feature is the agent's claim about itself, re-read from every
	// heartbeat: an agent rolled back to a release that cannot move a live
	// quota must stop being recorded as one that can, or an elastic pool keeps
	// counting runners there as boostable when they are held at their share.
	s := newTestStore(t)
	_, _, host := seedPool(t, s)
	ctx := t.Context()
	if host.Supports("elastic-cpu") {
		t.Fatal("a host that has claimed nothing supports elastic CPU")
	}
	host.Features = StringSlice{"elastic-cpu"}
	if err := s.SetHostReported(ctx, host); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetHost(ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Supports("elastic-cpu") {
		t.Fatalf("features = %v, want the agent's claim kept", got.Features)
	}
	host.Features = nil
	if err := s.SetHostReported(ctx, host); err != nil {
		t.Fatal(err)
	}
	if got, err = s.GetHost(ctx, host.ID); err != nil {
		t.Fatal(err)
	} else if got.Supports("elastic-cpu") {
		t.Fatalf("features = %v, want an empty report to withdraw the claim", got.Features)
	}
}

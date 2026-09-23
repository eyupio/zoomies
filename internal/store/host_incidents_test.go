package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// The two halves of the column are written by different paths -- the
// heartbeat and a task result -- and each must leave the other alone, or a
// beat that says the runtime is fine would wipe the image a pool cannot pull.
func TestHostIncidentsRoundTripAndEachHalfLeavesTheOtherAlone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, _, h := seedPool(t, s)

	got, err := s.GetHost(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Incidents.Runtime != nil || got.Incidents.ImagePull != nil {
		t.Fatalf("a new host has incidents: %+v", got.Incidents)
	}

	at := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	rt := &RuntimeIncident{Failures: 3, Kind: "unavailable", Error: "no socket", RetryAt: at.Add(40 * time.Second), Since: at, ObservedAt: at}
	pull := &ImagePullIncident{PoolID: "pool_1", Pool: "linux", Image: "ghcr.io/x/y:1", Registry: "ghcr.io", Source: "start", Since: at, ObservedAt: at}
	if err := s.SetHostRuntimeIncident(ctx, h.ID, rt); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHostImagePullIncident(ctx, h.ID, pull); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetHost(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r := got.Incidents.Runtime; r == nil || r.Failures != 3 || r.Kind != "unavailable" || !r.RetryAt.Equal(rt.RetryAt) || !r.ObservedAt.Equal(at) {
		t.Fatalf("the runtime incident came back as %+v", r)
	}
	if p := got.Incidents.ImagePull; p == nil || p.Registry != "ghcr.io" || p.Pool != "linux" || !p.ObservedAt.Equal(at) {
		t.Fatalf("the image pull incident came back as %+v", p)
	}

	if err := s.SetHostRuntimeIncident(ctx, h.ID, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetHost(ctx, h.ID)
	if got.Incidents.Runtime != nil || got.Incidents.ImagePull == nil {
		t.Fatalf("clearing the runtime half took the wrong half: %+v", got.Incidents)
	}
	if err := s.SetHostRuntimeIncident(ctx, h.ID, rt); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHostImagePullIncident(ctx, h.ID, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetHost(ctx, h.ID)
	if got.Incidents.Runtime == nil || got.Incidents.ImagePull != nil {
		t.Fatalf("clearing the image half took the wrong half: %+v", got.Incidents)
	}

	// The general host writes do not carry the column, so an operator's
	// PATCH cannot put back an incident the heartbeat has cleared.
	got.Capacity = 7
	if err := s.UpdateHost(ctx, got); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHostRuntimeIncident(ctx, h.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateHost(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetHost(ctx, h.ID); again.Incidents.Runtime != nil {
		t.Fatal("a whole-row host write put a cleared incident back")
	}

	if err := s.SetHostRuntimeIncident(ctx, "host_missing", rt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown host gave %v, want ErrNotFound", err)
	}
}

// 0049 adds a nullable column with no default, so every host that existed
// before it reads NULL. That has to read as "nothing has happened here", and
// the first incident written to such a row has to land.
func TestAHostFromBeforeIncidentsWereKeptUpgradesWithNone(t *testing.T) {
	ctx := context.Background()
	path := atSchemaBefore(t, "0049_host_incidents.sql")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO hosts (id, name, created_at) VALUES ('host_old', 'old', 1)`); err != nil {
		t.Fatalf("seeding a host at the old schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("upgrading: %v", err)
	}
	defer s.Close()
	var raw sql.NullString
	if err := s.read.QueryRowContext(ctx, `SELECT incidents FROM hosts WHERE id='host_old'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw.Valid {
		t.Fatalf("the upgrade wrote %q into an existing host; the column is meant to arrive NULL", raw.String)
	}
	h, err := s.GetHost(ctx, "host_old")
	if err != nil {
		t.Fatalf("reading an upgraded host: %v", err)
	}
	if h.Incidents.Runtime != nil || h.Incidents.ImagePull != nil {
		t.Fatalf("an upgraded host has incidents: %+v", h.Incidents)
	}
	at := time.Unix(100, 0).UTC()
	if err := s.SetHostImagePullIncident(ctx, "host_old", &ImagePullIncident{PoolID: "p", Registry: "docker.io", ObservedAt: at}); err != nil {
		t.Fatalf("writing an incident onto a NULL column: %v", err)
	}
	if h, _ := s.GetHost(ctx, "host_old"); h.Incidents.ImagePull == nil || h.Incidents.ImagePull.Registry != "docker.io" {
		t.Fatalf("the incident did not land on the upgraded row: %+v", h.Incidents)
	}
}

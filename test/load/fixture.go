//go:build load

// Package load builds a fleet with a history and measures the reads an
// operator's pages make against it.
//
// It is a separate tier for the same reason the drills are: it takes tens of
// seconds and writes a record rather than an exit code. The fixture is here and
// not in the demo seed on purpose -- the seed is what a person points at a
// running controller to look at a fleet, and ten thousand jobs is not something
// anybody should be able to do to a real database by setting an environment
// variable.
package load

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// The shape of the fixture. Ten hosts is a fleet somebody runs; ten thousand
// jobs is a busy month of history, which is what the retention windows allow
// and therefore what the queries have to stay honest over.
const (
	hosts      = 10
	pools      = 5
	jobs       = 10_000
	runners    = 2_000
	auditRows  = 5_000
	historyFor = 30 * 24 * time.Hour
)

// build writes the fixture through the store's own writer, which is the only
// way it can be written: the store owns the single write connection, and a
// fixture that went around it would be measuring a database no controller
// could have produced.
func build(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	started := time.Now()
	now := time.Now()
	// Deterministic: a measurement whose fixture changes between runs cannot be
	// compared with the last one, which is the whole point of recording it.
	rng := rand.New(rand.NewPCG(1, 2))

	inst := &store.Installation{
		AppID: 1234, InstallationID: 5678, Target: "acme", TargetType: store.TargetOrg,
		AppSlug: "zoomies-load", PrivateKeyEnc: []byte("sealed"), WebhookSecretEnc: []byte("sealed"),
	}
	if err := s.CreateInstallation(ctx, inst); err != nil {
		t.Fatalf("CreateInstallation: %v", err)
	}

	hostIDs := make([]string, 0, hosts)
	for i := range hosts {
		h := &store.Host{
			Name: fmt.Sprintf("builder-%02d", i), Address: "127.0.0.1", Capacity: 8,
			Backends: store.StringSlice{string(store.BackendDocker)}, LastHeartbeat: now,
		}
		if err := s.CreateHost(ctx, h); err != nil {
			t.Fatalf("CreateHost: %v", err)
		}
		hostIDs = append(hostIDs, h.ID)
	}

	poolIDs := make([]string, 0, pools)
	for i := range pools {
		p := &store.Pool{
			Name: fmt.Sprintf("linux-x64-%02d", i), InstallationID: inst.ID,
			Labels:  []string{"self-hosted", "linux", "x64", fmt.Sprintf("pool-%02d", i)},
			Backend: store.BackendDocker, Image: "ghcr.io/eyupio/zoomies-runner:test",
			DockerMode: store.DockerNone,
			MinRunners: 0, MaxRunners: 20, Enabled: true,
		}
		if err := s.CreatePool(ctx, p); err != nil {
			t.Fatalf("CreatePool: %v", err)
		}
		poolIDs = append(poolIDs, p.ID)
	}

	runnerIDs := make([]string, 0, runners)
	for i := range runners {
		r := &store.Runner{
			PoolID: poolIDs[i%len(poolIDs)], HostID: hostIDs[i%len(hostIDs)],
			Name:  fmt.Sprintf("zoomies-load-%05d", i),
			State: store.RunnerRemoved,
		}
		if i < 40 {
			// A live tail, so the lists a page shows first are not all history.
			r.State = store.RunnerIdle
		}
		if err := s.CreateRunner(ctx, r); err != nil {
			t.Fatalf("CreateRunner: %v", err)
		}
		runnerIDs = append(runnerIDs, r.ID)
	}

	conclusions := []string{"success", "success", "success", "failure", "cancelled"}
	for i := range jobs {
		age := time.Duration(rng.Int64N(int64(historyFor)))
		queued := now.Add(-age)
		j := &store.Job{
			GitHubJobID:    int64(100_000 + i),
			GitHubRunID:    int64(900_000 + i/10),
			Repo:           fmt.Sprintf("acme/service-%02d", i%20),
			Workflow:       "CI",
			JobName:        fmt.Sprintf("build-%03d", i%50),
			Labels:         store.NormalizeLabels([]string{"self-hosted", "linux", "x64"}),
			State:          store.JobCompleted,
			Conclusion:     conclusions[i%len(conclusions)],
			InstallationID: inst.ID,
			PoolID:         poolIDs[i%len(poolIDs)],
			RunnerID:       runnerIDs[i%len(runnerIDs)],
			RunnerName:     fmt.Sprintf("zoomies-load-%05d", i%runners),
			QueuedAt:       queued,
			Matched:        true,
		}
		started := queued.Add(time.Duration(rng.Int64N(int64(2 * time.Minute))))
		completed := started.Add(time.Duration(rng.Int64N(int64(20 * time.Minute))))
		j.StartedAt, j.CompletedAt = &started, &completed
		if i%97 == 0 {
			// A queue with something in it: the Overview and the scheduler both
			// read this, and a fixture with none would measure the easy case.
			j.State, j.Conclusion, j.StartedAt, j.CompletedAt = store.JobQueued, "", nil, nil
		}
		if _, err := s.UpsertJob(ctx, j); err != nil {
			t.Fatalf("UpsertJob %d: %v", i, err)
		}
	}

	// The Overview's sparkline and its feed, which are read on every page load
	// and were empty in the first version of this fixture -- so the two fastest
	// figures in the record were measuring nothing.
	for i := range 24 * 60 {
		if err := s.RecordSample(ctx, store.FleetSample{
			At: now.Add(-time.Duration(i) * time.Minute), QueuedJobs: i % 7, RunningJobs: i % 11,
			IdleRunners: 3, BusyRunners: i % 5, TotalRunners: 8,
		}); err != nil {
			t.Fatalf("RecordSample: %v", err)
		}
	}
	for i := range 500 {
		if err := s.AppendScalingEvent(ctx, &store.ScalingEvent{
			PoolID: poolIDs[i%len(poolIDs)], PoolName: fmt.Sprintf("linux-x64-%02d", i%len(poolIDs)),
			From: i % 4, To: (i % 4) + 1,
			Reason:    fmt.Sprintf("%d jobs queued > 30s", 1+i%3),
			CreatedAt: now.Add(-time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("AppendScalingEvent: %v", err)
		}
	}

	actions := []string{"pool.update", "runner.drain", "host.cordon", "token.create", "settings.update"}
	for i := range auditRows {
		if err := s.AppendAudit(ctx, &store.AuditEvent{
			ActorID: "usr_load", ActorName: "loadtest", ActorKind: "user",
			Action:     actions[i%len(actions)],
			TargetKind: "pool", TargetID: poolIDs[i%len(poolIDs)],
			CreatedAt: now.Add(-time.Duration(rng.Int64N(int64(historyFor)))),
		}); err != nil {
			t.Fatalf("AppendAudit: %v", err)
		}
	}

	t.Logf("fixture: %d hosts, %d pools, %d runners, %d jobs, %d audit rows in %s",
		hosts, pools, runners, jobs, auditRows, time.Since(started).Round(time.Millisecond))
}

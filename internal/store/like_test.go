package store

import (
	"context"
	"slices"
	"testing"
	"time"
)

// A search box is typed into by people, not by SQL. "gpu_a100" used to find
// "gpu-a100" as well, because _ is LIKE's any-one-character, and a "%" typed
// into the box found every row on the page.
func TestSearchesMatchWhatWasTypedNotLikeWildcards(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, pool, host := seedPool(t, s)

	for _, name := range []string{"gpu_a100", "gpu-a100", "cpu%only", `back\slash`} {
		if err := s.CreateRunner(ctx, &Runner{PoolID: pool.ID, HostID: host.ID, Name: name, State: RunnerIdle}); err != nil {
			t.Fatalf("CreateRunner %s: %v", name, err)
		}
	}
	cases := []struct {
		q    string
		want []string
	}{
		{"gpu_a100", []string{"gpu_a100"}},
		{"gpu", []string{"gpu-a100", "gpu_a100"}},
		{"%", []string{"cpu%only"}},
		{`\`, []string{`back\slash`}},
		// Every runner ID carries the run_ prefix, so a bare "_" matches them
		// all by ID, correctly; a fragment with the underscore inside it does
		// not.
		{"u_a", []string{"gpu_a100"}},
	}
	for _, tc := range cases {
		got, total, err := s.ListRunners(ctx, RunnerFilter{Search: tc.q}, Page{Sort: "name"})
		if err != nil {
			t.Fatalf("ListRunners %q: %v", tc.q, err)
		}
		names := make([]string, 0, len(got))
		for _, r := range got {
			names = append(names, r.Name)
		}
		if total != len(tc.want) || !slices.Equal(names, tc.want) {
			t.Errorf("search %q found %v (total %d), want %v", tc.q, names, total, tc.want)
		}
	}
}

// The label filter is exact on the quoted label, so a pool named for gpu_a100
// must not claim the gpu-a100 jobs, and the Jobs and Audit search boxes follow
// the same rule as the Runners one.
func TestLabelAndJobAndAuditSearchesAreExactOnUnderscores(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	for i, labels := range []StringSlice{{"gpu_a100"}, {"gpu-a100"}} {
		j := &Job{GitHubJobID: int64(1 + i), Repo: "acme/widgets", JobName: "build_" + labels[0], State: JobQueued, QueuedAt: now, Labels: labels}
		if _, err := s.UpsertJob(ctx, j); err != nil {
			t.Fatalf("UpsertJob: %v", err)
		}
	}
	got, total, err := s.ListJobs(ctx, JobFilter{Labels: []string{"gpu_a100"}}, Page{})
	if err != nil {
		t.Fatalf("ListJobs by label: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].Labels[0] != "gpu_a100" {
		t.Fatalf("label gpu_a100 matched %d jobs (%v), want just the gpu_a100 one", total, got)
	}
	if _, total, err = s.ListJobs(ctx, JobFilter{Search: "build_gpu_"}, Page{}); err != nil || total != 1 {
		t.Fatalf("job search for build_gpu_ matched %d (err %v), want 1", total, err)
	}

	for _, actor := range []string{"pat_smith", "patxsmith"} {
		if err := s.AppendAudit(ctx, &AuditEvent{ActorID: "usr_" + actor, ActorName: actor, ActorKind: "user", Action: "pool.create", TargetKind: "pool", TargetID: "pool_1"}); err != nil {
			t.Fatalf("AppendAudit: %v", err)
		}
	}
	rows, total, err := s.ListAudit(ctx, AuditFilter{Search: "pat_smith"}, Page{})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ActorName != "pat_smith" {
		t.Fatalf("audit search for pat_smith matched %d rows, want the one actor", total)
	}
}

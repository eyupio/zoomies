package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

func TestIsDemoID(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"ins_demoacme", true},
		{"pool_demolinux", true},
		{"host_demo1", true},
		{"run_demo09", true},
		{"pool_k3f9qz2mx7ab", false},
		{"ins_democracy", true}, // readable, and not the shape NewID makes
		{"job_demostuckheldjob", true},
		// A real identifier whose random part happens to begin "demo". One row
		// in a million does, and it used to be skipped by the prober, the
		// poller and the reap, and never reclaimed if it was a host.
		{"ins_demoq2mx7abk3", false},
		{"host_demozzzzzzzzz", false},
		{"nounderscore", false},
		{"", false},
	} {
		if got := IsDemoID(tc.id); got != tc.want {
			t.Errorf("IsDemoID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

// Every identifier the seed writes has to be one IsDemoID recognises, and the
// rule it recognises them by is the shape, so no fixture may be exactly the
// shape NewID produces. This is the test that stops a future fixture named
// with thirteen readable letters from becoming a real row overnight.
func TestEveryDemoFixtureIsRecognisedAsOne(t *testing.T) {
	h := newHarness(t)
	t.Setenv(StuckSeedEnvVar, "1")
	if err := h.c.SeedDemo(h.ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}
	if err := h.c.SeedStuck(h.ctx); err != nil {
		t.Fatalf("seedStuck: %v", err)
	}
	var ids []string
	insts, _ := h.st.ListInstallations(h.ctx)
	for _, i := range insts {
		ids = append(ids, i.ID)
	}
	pools, _ := h.st.ListPools(h.ctx)
	for _, p := range pools {
		ids = append(ids, p.ID)
	}
	hosts, _ := h.st.ListHosts(h.ctx)
	for _, hh := range hosts {
		ids = append(ids, hh.ID)
	}
	for _, r := range h.runners() {
		ids = append(ids, r.ID)
	}
	jobs, _, _ := h.st.ListJobs(h.ctx, store.JobFilter{}, store.Page{Limit: 500})
	for _, j := range jobs {
		ids = append(ids, j.ID)
	}
	if len(ids) < 60 {
		t.Fatalf("collected only %d fixture identifiers; the seed is not what this test thinks it is", len(ids))
	}
	for _, id := range ids {
		if !IsDemoID(id) {
			t.Errorf("fixture %q is not recognised as demo data", id)
		}
		if store.LooksGenerated(id) {
			t.Errorf("fixture %q has the shape of a real identifier; a fixture must never be mistakable for one", id)
		}
	}
}

// The demo fixtures have no GitHub behind them. If the credential prober were
// allowed to check them, every demo instance and every UI test run would open
// on a problems drawer led by "this installation is not usable" -- a failure
// that says nothing about the operator's fleet and hides the ones that do.
func TestDemoInstallationIsNotProbed(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if err := h.c.SeedDemo(ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}
	h.c.probeInstallations(ctx)

	inst, err := h.st.GetInstallation(ctx, demoInstallationID)
	if err != nil {
		t.Fatalf("GetInstallation: %v", err)
	}
	if inst.LastError != "" {
		t.Errorf("the demo installation was probed and recorded %q; it must be skipped", inst.LastError)
	}

	problems, err := h.c.Problems(ctx)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	for _, p := range problems {
		if p.Code == "installation.unhealthy" {
			t.Errorf("the demo fleet reports %q: %s", p.Code, p.Title)
		}
	}
}

// The demo installation must answer every read the UI makes without reaching
// GitHub. Before this, the Installations page 500'd on the rate limit and the
// poller logged a credential failure every thirty seconds, which makes a demo
// instance read as broken software rather than as a working fleet.
func TestDemoInstallationAnswersReadsWithoutGitHub(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.c.SeedDemo(ctx); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	client, err := h.c.ClientFor(ctx, demoInstallationID)
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}

	if _, err := client.Probe(ctx); err != nil {
		t.Errorf("Probe: %v", err)
	}
	rl, err := client.RateLimit(ctx)
	if err != nil {
		t.Fatalf("RateLimit: %v", err)
	}
	if rl.Remaining <= 0 || rl.Remaining > rl.Limit {
		t.Errorf("rate limit = %d/%d, want a plausible pair", rl.Remaining, rl.Limit)
	}
	if _, err := client.ListRunners(ctx); err != nil {
		t.Errorf("ListRunners: %v", err)
	}
	if _, err := client.ListRunnerGroups(ctx); err != nil {
		t.Errorf("ListRunnerGroups: %v", err)
	}
	if _, err := client.ListQueuedJobs(ctx); err != nil {
		t.Errorf("ListQueuedJobs: %v", err)
	}
	if err := client.DeleteRunner(ctx, 42); err != nil {
		t.Errorf("DeleteRunner on a registration that never existed should succeed: %v", err)
	}

	// Writes must refuse, and say why. Silently pretending to mint a runner
	// credential would leave runners stuck in provisioning with no explanation.
	if _, err := client.CreateJITConfig(ctx, github.JITRequest{Name: "x"}); err == nil {
		t.Error("CreateJITConfig succeeded on a demo fixture; it must refuse")
	} else if !errors.Is(err, ErrDemoFixture) {
		t.Errorf("CreateJITConfig error = %v, want it to wrap ErrDemoFixture", err)
	}
}

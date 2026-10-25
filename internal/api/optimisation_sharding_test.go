package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

func shardingAPIHarness(t *testing.T, runs int) (*harness, string, *checkoutAPIReader, map[string]any) {
	t.Helper()
	h, instID, f, _ := checkoutAPIHarness(t, true)
	now := time.Now().UTC()
	workflow := "on: push\njobs:\n  e2e:\n    name: End to end\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@" + strings.Repeat("a", 40) + "\n      - name: Browser tests\n        run: npx playwright test\n"
	file := prrepair.File{Path: ".github/workflows/ci.yml", Mode: "100644", Content: workflow}
	f.shardSource = &github.ShardSource{
		Version: "shard-evidence-v1", Basis: "push_head_root_workflow_literal_names", Repository: "acme/repo",
		RunID: "10", Attempt: 2, HeadSHA: strings.Repeat("a", 40), HeadBranch: "main",
		WorkflowPath: file.Path, Workflow: file, WorkflowBlobSHA: cacheAPISHA1(workflow), WorkflowSHA256: cacheAPISHA256(workflow),
		JobIndex: 0, StepIndex: 1, JobName: "End to end", StepName: "Browser tests",
	}
	begin := now.Add(-402 * time.Second)
	at := func(d int) *time.Time { v := begin.Add(time.Duration(d) * time.Second); return &v }
	steps := store.JobSteps{
		{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success", StartedAt: at(0), CompletedAt: at(2)},
		{Number: 2, Name: "Install", Status: "completed", Conclusion: "success", StartedAt: at(2), CompletedAt: at(42)},
		{Number: 3, Name: "Browser tests", Status: "completed", Conclusion: "success", StartedAt: at(42), CompletedAt: at(402)},
	}
	for i := 0; i < runs; i++ {
		run, attempt := int64(10), 2
		if i > 0 {
			run, attempt = int64(10+i), 1
		}
		if _, err := h.st.UpsertJob(h.ctx, &store.Job{GitHubJobID: int64(100 + i), GitHubRunID: run, Repo: "acme/repo", Workflow: "CI", JobName: "End to end", Labels: store.StringSlice{"ubuntu-latest"},
			State: store.JobCompleted, Conclusion: "success", InstallationID: instID, HeadBranch: "main", HeadSHA: strings.Repeat("a", 40), RunAttempt: attempt,
			QueuedAt: now.Add(-10 * time.Minute), CompletedAt: &now, Steps: steps}); err != nil {
			t.Fatal(err)
		}
	}
	pool := &store.Pool{Name: "browsers", InstallationID: instID, Labels: store.StringSlice{"self-hosted", "linux", "ubuntu-latest"}, Backend: store.BackendDocker,
		Image: "ghcr.io/eyupio/zoomies-runner:test", MaxRunners: 4, IdleTimeout: store.Duration(5 * time.Minute), Ephemeral: true, DockerMode: store.DockerNone, Enabled: true}
	if err := h.st.CreatePool(h.ctx, pool); err != nil {
		t.Fatal(err)
	}
	host := &store.Host{Name: "browser-host", Capacity: 4, Backends: store.StringSlice{"docker"}, Labels: store.StringMap{}, OS: "linux", Arch: "amd64", CPUs: 16, MemoryMB: 64 * 1024, DiskTotalMB: 500 * 1024, DiskFreeMB: 400 * 1024, LastHeartbeat: time.Now()}
	if err := h.st.CreateHost(h.ctx, host); err != nil {
		t.Fatal(err)
	}
	return h, instID, f, map[string]any{"run_id": "10", "attempt": 2, "github_job_id": "100"}
}

func TestShardingInspectionSizesTheShardsAndKeepsTheSourcePrivate(t *testing.T) {
	h, instID, f, selection := shardingAPIHarness(t, 12)
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read")
	body := map[string]any{"installation_id": instID, "repository": "acme/repo", "selection": selection}
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/sharding/inspections", token: token, body: body})
	resp.mustStatus(t, 200, "inspect sharding")
	var got ShardingInspectionView
	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Shards != 4 || got.Runs != 12 || got.TestMS != 360_000 || got.SetupMS != 42_000 || got.Headroom != 4 || got.JobName != "End to end" || got.StepName != "Browser tests" || got.Verification != "inconclusive" || !strings.Contains(got.After, "--shard=${{ matrix.shard }}/4") || strings.Contains(string(resp.body), "on: push") {
		t.Fatalf("inspection %s", resp.body)
	}
	if got.RunnerMSAfter <= got.RunnerMSBefore || got.WallMSAfter >= got.WallMSBefore {
		t.Fatalf("the disclosed figures do not show more runner time for less wall clock: %+v", got)
	}
	assertShape(t, loadSpec(t), "ShardingInspection", resp.body)
	if f.shardReads.Load() != 1 || auditCount(t, h, "optimisation.sharding_preview_retained") != 0 {
		t.Fatal("inspection read more than once or retained something")
	}
}

func TestShardingInspectionExplainsEveryRefusal(t *testing.T) {
	h, instID, f, selection := shardingAPIHarness(t, 9)
	token := h.token("operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read")
	body := map[string]any{"installation_id": instID, "repository": "acme/repo", "selection": selection}
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/optimisations/sharding/inspections", token: token, body: body})
	resp.mustStatus(t, 409, "nine runs")
	if !strings.Contains(string(resp.body), "at least ten matching successful runs") {
		t.Fatalf("the refusal did not say what the job needs: %s", resp.body)
	}
	h2, instID2, f2, selection2 := shardingAPIHarness(t, 12)
	// A source whose bytes no longer match what was read is not trusted.
	f2.shardSource.Workflow.Content += "# edited\n"
	_ = f
	h2.do(request{method: http.MethodPost, path: "/api/v1/optimisations/sharding/inspections", token: h2.token("operator", store.RoleOperator, "jobs:read", "optimisations:write", "optimisations:read"), body: map[string]any{"installation_id": instID2, "repository": "acme/repo", "selection": selection2}}).mustStatus(t, 409, "refused workflow")
	h2.do(request{method: http.MethodPost, path: "/api/v1/optimisations/sharding/inspections", token: h2.token("viewer", store.RoleViewer, "jobs:read", "optimisations:read"), body: map[string]any{"installation_id": instID2, "repository": "acme/repo", "selection": selection2}}).mustStatus(t, 403, "viewer")
	h2.do(request{method: http.MethodPost, path: "/api/v1/optimisations/sharding/inspections", token: h2.token("operator2", store.RoleOperator, "jobs:read", "optimisations:write"), body: map[string]any{"installation_id": instID2, "repository": "acme/repo", "selection": map[string]any{"run_id": 10, "attempt": 2, "github_job_id": "100"}}}).mustStatus(t, 400, "numeric run ID")
}

package github

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/kennel/workflow"
)

const shardSourceWorkflow = "on: push\njobs:\n  e2e:\n    name: End to end\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + "\n      - name: Browser tests\n        run: npx playwright test\n"

func shardSourceFixture() cacheSourceFixture {
	f := cacheSourceDefault()
	f.workflow = shardSourceWorkflow
	f.lockfilePath, f.lockfile = "", ""
	return f
}

func TestReadShardSourceBindsTheWorkflowToTheOnePlaywrightStep(t *testing.T) {
	f := shardSourceFixture()
	selection := SetupCacheSelection{RunID: f.runID, Attempt: f.attempt}
	src, err := cacheSourceClient(t, cacheSourceServer(t, f)).ReadShardSource(context.Background(), "acme/repo", selection)
	if err != nil {
		t.Fatal(err)
	}
	if src.Version != "shard-evidence-v1" || src.Repository != "acme/repo" || src.RunID != "10" || src.Attempt != 2 || src.HeadSHA != f.head || src.HeadBranch != "main" || src.WorkflowPath != f.workflowPath || src.Workflow.Content != f.workflow || src.WorkflowBlobSHA != f.blob(f.workflow) || src.JobIndex != 0 || src.StepIndex != 1 || src.JobName != "End to end" || src.StepName != "Browser tests" {
		t.Fatalf("source %+v", src)
	}
	if !ValidShardSource(src, "acme/repo", selection) {
		t.Fatal("source failed its own shape check")
	}
	other := *src
	other.StepName = "Something else"
	if ValidShardSource(&other, "acme/repo", selection) {
		t.Fatal("a source with the wrong step name was accepted")
	}
}

func TestReadShardSourceRefusesUnsupportedEvidenceAndSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		edit   func(*cacheSourceFixture)
		reason string
	}{
		{"non-push", func(f *cacheSourceFixture) { f.event = "pull_request" }, ""},
		{"failed", func(f *cacheSourceFixture) { f.conclusion = "failure" }, ""},
		{"truncated tree", func(f *cacheSourceFixture) { f.treeTruncated = true }, ""},
		{"no playwright", func(f *cacheSourceFixture) {
			f.workflow = strings.Replace(f.workflow, "npx playwright test", "go test ./...", 1)
		}, "no single Playwright"},
		{"already sharded", func(f *cacheSourceFixture) {
			f.workflow = strings.Replace(f.workflow, "npx playwright test", "npx playwright test --shard=1/2", 1)
		}, "already shards"},
		{"filtered", func(f *cacheSourceFixture) {
			f.workflow = strings.Replace(f.workflow, "npx playwright test", "npx playwright test --grep @smoke", 1)
		}, "already shards, filters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := shardSourceFixture()
			tc.edit(&f)
			_, err := cacheSourceClient(t, cacheSourceServer(t, f)).ReadShardSource(context.Background(), "acme/repo", SetupCacheSelection{RunID: f.runID, Attempt: f.attempt})
			if err == nil {
				t.Fatal("accepted")
			}
			if tc.reason != "" && !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err=%v, want it to say %q", err, tc.reason)
			}
			var refusal *workflow.ShardRefusal
			if strings.Contains(tc.reason, "already") && !errors.As(err, &refusal) {
				t.Fatalf("the workflow's refusal was not carried through: %v", err)
			}
		})
	}
}

func TestReadShardSourceStopsWhenTheRunMovesWhileItIsRead(t *testing.T) {
	f := shardSourceFixture()
	first := `{"id":10,"run_attempt":2,"repository":{"full_name":"acme/repo"},"status":"completed","conclusion":"success","event":"push","head_sha":"` + f.head + `","head_branch":"main","path":"` + f.workflowPath + `"}`
	moved := strings.Replace(first, f.head, strings.Repeat("d", 40), 1)
	_, err := cacheSourceClient(t, cacheSourceServer(t, f, first, moved)).ReadShardSource(context.Background(), "acme/repo", SetupCacheSelection{RunID: f.runID, Attempt: f.attempt})
	if !errors.Is(err, ErrSetupCacheChanged) {
		t.Fatalf("err=%v, want ErrSetupCacheChanged", err)
	}
}

// The readiness reader shares the root workflow read, so it is held to the same
// behaviour here rather than only through the controller's fakes.
func TestReadReadinessSourceStillBindsTheFixedSleep(t *testing.T) {
	f := cacheSourceDefault()
	f.workflow = "on: push\njobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    steps:\n      - name: Wait for the API\n        run: sleep 30\n"
	f.lockfilePath, f.lockfile = "", ""
	selection := SetupCacheSelection{RunID: f.runID, Attempt: f.attempt}
	src, err := cacheSourceClient(t, cacheSourceServer(t, f)).ReadReadinessSource(context.Background(), "acme/repo", selection)
	if err != nil {
		t.Fatal(err)
	}
	if src.JobName != "Build" || src.StepName != "Wait for the API" || src.WaitSeconds != 30 || src.HeadBranch != "main" || !ValidReadinessSource(src, "acme/repo", selection) {
		t.Fatalf("source %+v", src)
	}
	f.workflow = strings.Replace(f.workflow, "sleep 30", "echo hi", 1)
	if _, err := cacheSourceClient(t, cacheSourceServer(t, f)).ReadReadinessSource(context.Background(), "acme/repo", selection); err == nil {
		t.Fatal("a workflow with no fixed sleep was accepted")
	}
}

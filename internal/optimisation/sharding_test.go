package optimisation

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/prrepair"
)

const shardingSource = `on: push
jobs:
  e2e:
    name: End to end
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      - name: Browser tests
        run: npx playwright test
`

func shardingFixture(testMS, setupMS int64, headroom int) (ShardSource, ShardEvidence) {
	file := prrepair.File{Path: ".github/workflows/ci.yml", Mode: "100644", Content: shardingSource}
	source := ShardSource{HeadSHA: strings.Repeat("a", 40), Workflow: file}
	return source, ShardEvidence{EvidenceID: "sharding:1", HeadSHA: source.HeadSHA, SourceSHA256: hash(file.Content), Runs: 12, TestMS: testMS, SetupMS: setupMS, Headroom: headroom}
}

var shardingPolicy = Policy{SourceAllowed: true, WorkflowEditsAllowed: true}

func TestChooseShardCountKeepsEveryShardAboveTheFloorAndWithinHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name     string
		testMS   int64
		headroom int
		want     int
	}{
		{"floor allows four", 360_000, 8, 4},
		{"capped by headroom", 900_000, 3, 3},
		{"capped at eight", 3_600_000, 50, 8},
		{"exactly two floors", 180_000, 8, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ChooseShardCount(tc.testMS, tc.headroom)
			if err != nil || got != tc.want {
				t.Fatalf("got %d, %v; want %d", got, err, tc.want)
			}
			if tc.testMS/int64(got) < ShardTestFloorMS {
				t.Fatalf("%d shards leave %dms each, under the floor", got, tc.testMS/int64(got))
			}
		})
	}
	for name, tc := range map[string]struct {
		testMS   int64
		headroom int
		reason   string
	}{
		"too short":       {179_999, 8, "setup would cost more"},
		"no headroom":     {900_000, 1, "queue behind each other"},
		"no timing":       {0, 8, "no test timing"},
		"headroom is nil": {900_000, 0, "queue behind each other"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ChooseShardCount(tc.testMS, tc.headroom)
			var r *ShardingRefusal
			if !errors.As(err, &r) || !strings.Contains(r.Reason, tc.reason) || !errors.Is(err, ErrShardingUnsupported) {
				t.Fatalf("err=%v, want refusal containing %q", err, tc.reason)
			}
		})
	}
}

func TestPreviewShardingSplitsTheSuiteAndDisclosesTheExtraRunnerTime(t *testing.T) {
	source, evidence := shardingFixture(360_000, 60_000, 8)
	p, err := PreviewSharding(source, evidence, shardingPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "sharding-preview-v1" || p.Kind != "playwright_shards" || p.State != "preview_ready" || p.Shards != 4 || p.Verification != "inconclusive" || p.JobName != "End to end" || p.StepName != "Browser tests" {
		t.Fatalf("preview %+v", p)
	}
	// Four shards pay the 60s setup four times: 60 + 360 before, 240 + 360 after.
	if p.RunnerMSBefore != 420_000 || p.RunnerMSAfter != 600_000 || p.WallMSBefore != 420_000 || p.WallMSAfter != 150_000 {
		t.Fatalf("estimates before=%d/%d after=%d/%d", p.RunnerMSBefore, p.WallMSBefore, p.RunnerMSAfter, p.WallMSAfter)
	}
	if !strings.Contains(p.Edit.After, "npx playwright test --shard=${{ matrix.shard }}/4") || !strings.Contains(p.Edit.After, "shard: [1, 2, 3, 4]") || strings.Count(p.Edit.After, "--shard=") != 1 || p.AfterSHA256 != hash(p.File.Content) {
		t.Fatalf("edit %q", p.Edit.After)
	}
	for _, want := range []string{"together run the whole suite", "Runner time rises", "4 runners at once", "estimate from medians, not a measured saving"} {
		if !strings.Contains(p.Explanation, want) {
			t.Errorf("explanation does not say %q: %s", want, p.Explanation)
		}
	}
	if err := CheckShardingCurrent(p, source, evidence, shardingPolicy); err != nil {
		t.Fatal(err)
	}
	moved := source
	moved.Workflow.Content += "# edited\n"
	if err := CheckShardingCurrent(p, moved, evidence, shardingPolicy); err == nil {
		t.Fatal("a moved source was accepted as current")
	}
}

func TestPreviewShardingRefusesWithoutEvidencePolicyOrASupportedStep(t *testing.T) {
	source, evidence := shardingFixture(360_000, 60_000, 8)
	for name, mutate := range map[string]func(*ShardSource, *ShardEvidence, *Policy){
		"policy off":         func(_ *ShardSource, _ *ShardEvidence, p *Policy) { p.WorkflowEditsAllowed = false },
		"source read off":    func(_ *ShardSource, _ *ShardEvidence, p *Policy) { p.SourceAllowed = false },
		"too few runs":       func(_ *ShardSource, e *ShardEvidence, _ *Policy) { e.Runs = MinShardRuns - 1 },
		"evidence for other": func(_ *ShardSource, e *ShardEvidence, _ *Policy) { e.SourceSHA256 = hash("other") },
		"other head":         func(_ *ShardSource, e *ShardEvidence, _ *Policy) { e.HeadSHA = strings.Repeat("b", 40) },
		"not a workflow":     func(s *ShardSource, _ *ShardEvidence, _ *Policy) { s.Workflow.Path = "ci.yml" },
		"no step": func(s *ShardSource, e *ShardEvidence, _ *Policy) {
			s.Workflow.Content = strings.Replace(shardingSource, "npx playwright test", "go test ./...", 1)
			e.SourceSHA256 = hash(s.Workflow.Content)
		},
		"already sharded": func(s *ShardSource, e *ShardEvidence, _ *Policy) {
			s.Workflow.Content = strings.Replace(shardingSource, "npx playwright test", "npx playwright test --shard=1/2", 1)
			e.SourceSHA256 = hash(s.Workflow.Content)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, e, p := source, evidence, shardingPolicy
			mutate(&s, &e, &p)
			if _, err := PreviewSharding(s, e, p); !errors.Is(err, ErrShardingUnsupported) {
				t.Fatalf("err=%v, want a refusal", err)
			}
		})
	}
	s, e, p := source, evidence, shardingPolicy
	s.Workflow.Content = strings.Replace(shardingSource, "npx playwright test", "npx playwright test --grep @smoke", 1)
	e.SourceSHA256 = hash(s.Workflow.Content)
	_, err := PreviewSharding(s, e, p)
	var r *ShardingRefusal
	if !errors.As(err, &r) || !strings.Contains(r.Reason, "filters") {
		t.Fatalf("a filtered command was not refused with its reason: %v", err)
	}
}

func shardSamples(n int, test, setup int64) []ShardSample {
	out := make([]ShardSample, n)
	for i := range out {
		out[i] = ShardSample{JobID: fmt.Sprintf("job_%03d", i), RunID: fmt.Sprint(1000 + i), Attempt: 1, StepNumber: 5, TestMS: test, SetupMS: setup}
	}
	return out
}

func TestShardEvidenceFromSamplesUsesMediansAndIsOrderIndependent(t *testing.T) {
	head, digest := strings.Repeat("a", 40), hash("workflow")
	samples := shardSamples(10, 300_000, 50_000)
	samples[0].TestMS, samples[1].TestMS = 5_000_000, 6_000_000 // two slow outliers must not move the count
	a, err := ShardEvidenceFromSamples(head, digest, 6, samples)
	if err != nil {
		t.Fatal(err)
	}
	if a.TestMS != 300_000 || a.SetupMS != 50_000 || a.Runs != 10 || a.Headroom != 6 {
		t.Fatalf("evidence %+v", a)
	}
	reversed := append([]ShardSample(nil), samples...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	b, err := ShardEvidenceFromSamples(head, digest, 6, reversed)
	if err != nil || a.EvidenceID != b.EvidenceID {
		t.Fatalf("order changed the evidence ID: %v", err)
	}
}

func TestShardEvidenceFromSamplesRefusesTooFewDuplicateOrInvalidRuns(t *testing.T) {
	head, digest := strings.Repeat("a", 40), hash("workflow")
	dup := shardSamples(10, 300_000, 50_000)
	dup[3].JobID = dup[2].JobID
	bad := shardSamples(10, 300_000, 50_000)
	bad[4].TestMS = 0
	for name, samples := range map[string][]ShardSample{
		"nine runs": shardSamples(9, 300_000, 50_000), "duplicate job": dup, "zero test time": bad, "too many": shardSamples(101, 300_000, 50_000),
	} {
		if _, err := ShardEvidenceFromSamples(head, digest, 6, samples); !errors.Is(err, ErrShardingUnsupported) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if _, err := ShardEvidenceFromSamples("short", digest, 6, shardSamples(10, 1, 1)); err == nil {
		t.Error("a bad head was accepted")
	}
}

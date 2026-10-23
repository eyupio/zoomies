package optimisation

import (
	"errors"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/prrepair"
)

const cacheWorkflowSource = `on: push
jobs:
  build:
    name: Build
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `
      - name: Node
        uses: actions/setup-node@` + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" + `
        with:
          node-version: 20
      - name: Test
        run: npm test
`

func cacheFixture(t *testing.T) (CacheSource, SetupEvidence) {
	t.Helper()
	source := CacheSource{
		HeadSHA:  strings.Repeat("a", 40),
		Workflow: prrepair.File{Path: ".github/workflows/ci.yml", Mode: "100644", Content: cacheWorkflowSource},
		Lockfile: prrepair.File{Path: "package-lock.json", Mode: "100644", Content: `{"lockfileVersion":3}`},
	}
	ms := int64(4200)
	evidence := SetupEvidence{
		EvidenceID: "setup:10:2:100:node", HeadSHA: source.HeadSHA,
		SourceSHA256: hash(source.Workflow.Content), LockfileSHA256: hash(source.Lockfile.Content),
		Runs: 4, SetupMS: &ms,
	}
	return source, evidence
}

func TestCachePreviewInsertsTheManagerAndKeepsTheRest(t *testing.T) {
	source, evidence := cacheFixture(t)
	p, err := PreviewCache(source, evidence, Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "cache-preview-v1" || p.Kind != "dependency_cache" || p.PackageManager != "npm" || p.JobIndex != 0 || p.StepIndex != 1 || p.Line != 12 || p.Runs != 4 || p.SetupMS != 4200 || p.Verification != "inconclusive" || p.LockfilePath != "package-lock.json" || p.LockfileSHA256 != hash(source.Lockfile.Content) {
		t.Fatalf("preview %+v", p)
	}
	if !strings.Contains(p.File.Content, "        with:\n          cache: npm\n          node-version: 20\n") || !strings.Contains(p.File.Content, "      - name: Test\n        run: npm test\n") || p.AfterSHA256 == p.SourceSHA256 {
		t.Fatalf("source edit = %q", p.File.Content)
	}
	if !strings.Contains(p.Explanation, "4 runs") || !strings.Contains(p.Explanation, "4.2s") || !strings.Contains(p.Explanation, "unverified") {
		t.Fatalf("explanation = %q", p.Explanation)
	}
	if err := CheckCacheCurrent(p, source, evidence, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}); err != nil {
		t.Fatal(err)
	}
}

func TestCachePreviewMapsEachRootLockfileToItsManager(t *testing.T) {
	for _, tc := range []struct{ path, manager string }{
		{"yarn.lock", "yarn"},
		{"pnpm-lock.yaml", "pnpm"},
		{"npm-shrinkwrap.json", "npm"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			source, evidence := cacheFixture(t)
			source.Lockfile.Path = tc.path
			evidence.LockfileSHA256 = hash(source.Lockfile.Content)
			p, err := PreviewCache(source, evidence, Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
			if err != nil || p.PackageManager != tc.manager || !strings.Contains(p.File.Content, "cache: "+tc.manager) {
				t.Fatalf("manager %q: %v, %+v", tc.manager, err, p)
			}
		})
	}
}

func TestCachePreviewRefusesUnsupportedEvidenceOrSource(t *testing.T) {
	for _, mode := range []string{"policy", "head", "source", "lockfile", "runs", "setup", "lockfile path", "lockfile empty", "workflow path", "partial", "cache already set", "two setup nodes", "unpinned"} {
		t.Run(mode, func(t *testing.T) {
			source, evidence := cacheFixture(t)
			policy := Policy{SourceAllowed: true, WorkflowEditsAllowed: true}
			switch mode {
			case "policy":
				policy.WorkflowEditsAllowed = false
			case "head":
				evidence.HeadSHA = strings.Repeat("c", 40)
			case "source":
				evidence.SourceSHA256 = strings.Repeat("d", 64)
			case "lockfile":
				evidence.LockfileSHA256 = strings.Repeat("e", 64)
			case "runs":
				evidence.Runs = 1
			case "setup":
				zero := int64(0)
				evidence.SetupMS = &zero
			case "lockfile path":
				source.Lockfile.Path = "web/package-lock.json"
				evidence.LockfileSHA256 = hash(source.Lockfile.Content)
			case "lockfile empty":
				source.Lockfile.Content = ""
				evidence.LockfileSHA256 = hash(source.Lockfile.Content)
			case "workflow path":
				source.Workflow.Path = ".github/workflows/nested/ci.yml"
			case "partial":
				source.Workflow.Partial = true
			case "cache already set":
				source.Workflow.Content = strings.Replace(source.Workflow.Content, "          node-version: 20\n", "          node-version: 20\n          cache: npm\n", 1)
				evidence.SourceSHA256 = hash(source.Workflow.Content)
			case "two setup nodes":
				source.Workflow.Content = strings.Replace(source.Workflow.Content, "      - name: Test\n        run: npm test\n", "      - uses: actions/setup-node@"+strings.Repeat("c", 40)+"\n        with:\n          node-version: 20\n      - name: Test\n        run: npm test\n", 1)
				evidence.SourceSHA256 = hash(source.Workflow.Content)
			case "unpinned":
				source.Workflow.Content = strings.Replace(source.Workflow.Content, "actions/setup-node@"+strings.Repeat("b", 40), "actions/setup-node@v4", 1)
				evidence.SourceSHA256 = hash(source.Workflow.Content)
			}
			if p, err := PreviewCache(source, evidence, policy); err == nil || p != (CachePreview{}) {
				t.Fatalf("accepted: %v, %+v", err, p)
			}
		})
	}
}

func TestCheckCacheCurrentRefusesAChangedSourceLockfileOrPolicy(t *testing.T) {
	source, evidence := cacheFixture(t)
	p, err := PreviewCache(source, evidence, Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	changed := source
	changed.Lockfile.Content = `{"lockfileVersion":2}`
	changedEvidence := evidence
	changedEvidence.LockfileSHA256 = hash(changed.Lockfile.Content)
	if err := CheckCacheCurrent(p, changed, changedEvidence, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}); !errors.Is(err, ErrCacheUnsupported) {
		t.Fatalf("changed lockfile: %v", err)
	}
	if err := CheckCacheCurrent(p, source, evidence, Policy{SourceAllowed: true, WorkflowEditsAllowed: false}); !errors.Is(err, ErrCacheUnsupported) {
		t.Fatalf("policy: %v", err)
	}
}

func TestSetupEvidenceFromSamplesIsStableAndUsesExactJobs(t *testing.T) {
	head, source, lockfile := strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 64)
	samples := []SetupSample{
		{JobID: "job_2", RunID: "20", Attempt: 1, StepNumber: 2, SetupMS: 3000},
		{JobID: "job_1", RunID: "10", Attempt: 1, StepNumber: 2, SetupMS: 5000},
		{JobID: "job_3", RunID: "30", Attempt: 2, StepNumber: 2, SetupMS: 4000},
	}
	evidence, err := SetupEvidenceFromSamples(head, source, lockfile, samples)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Runs != 3 || evidence.SetupMS == nil || *evidence.SetupMS != 4000 || evidence.HeadSHA != head || evidence.SourceSHA256 != source || evidence.LockfileSHA256 != lockfile || evidence.EvidenceID == "" {
		t.Fatalf("evidence %+v", evidence)
	}
	shuffled := []SetupSample{samples[2], samples[0], samples[1]}
	again, err := SetupEvidenceFromSamples(head, source, lockfile, shuffled)
	if err != nil || again.EvidenceID != evidence.EvidenceID {
		t.Fatalf("order changed the evidence ID: %+v, %v", again, err)
	}
}

func TestSetupEvidenceFromSamplesRefusesThinOrMalformedHistory(t *testing.T) {
	head, source, lockfile := strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 64)
	valid := []SetupSample{{JobID: "job_1", RunID: "10", Attempt: 1, StepNumber: 2, SetupMS: 4000}, {JobID: "job_2", RunID: "20", Attempt: 1, StepNumber: 2, SetupMS: 5000}}
	for _, tc := range []struct {
		name    string
		samples []SetupSample
		head    string
		source  string
	}{
		{"one sample", valid[:1], head, source},
		{"duplicate job", []SetupSample{valid[0], valid[0]}, head, source},
		{"bad run", []SetupSample{{JobID: "job_1", RunID: "010", Attempt: 1, StepNumber: 2, SetupMS: 4000}, valid[1]}, head, source},
		{"zero time", []SetupSample{{JobID: "job_1", RunID: "10", Attempt: 1, StepNumber: 2, SetupMS: 0}, valid[1]}, head, source},
		{"bad head", valid, "short", source},
		{"bad source", valid, head, "short"},
		{"too many", make([]SetupSample, 101), head, source},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SetupEvidenceFromSamples(tc.head, tc.source, lockfile, tc.samples); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

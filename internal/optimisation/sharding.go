package optimisation

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/eyupio/zoomies/internal/kennel/workflow"
	"github.com/eyupio/zoomies/internal/prrepair"
)

// ErrShardingUnsupported is the one refusal for a sharding preview. A reason
// from the workflow, when there is one, is carried by ShardingRefusal instead.
var ErrShardingUnsupported = errors.New("sharding preview needs one Playwright test step on an Ubuntu runner, repeated timing evidence, pool headroom and source/edit policy")

// ShardingRefusal is a refusal that says what to change. It wraps
// ErrShardingUnsupported so a caller that only cares whether a preview exists
// can match one error, while one that shows the operator why can read Reason.
type ShardingRefusal struct{ Reason string }

func (e *ShardingRefusal) Error() string { return "sharding preview refused: " + e.Reason }
func (e *ShardingRefusal) Unwrap() error { return ErrShardingUnsupported }

const (
	// MinShardRuns is how many matching successful runs the timings need. Fewer
	// is a guess about a suite whose duration may simply vary.
	MinShardRuns = 10
	// MaxShardRuns bounds the evidence read for one preview.
	MaxShardRuns = 100
	// MinShards and MaxShards bound the count. More than eight shards is where
	// each one's setup begins to cost more than the test it saves.
	MinShards = 2
	MaxShards = 8
	// ShardTestFloorMS is the least test time a shard may be left with. Below it
	// the shard's own setup dominates and the change spends runner time for
	// almost no latency.
	ShardTestFloorMS = 90_000
)

// ShardSource is the workflow file at a pinned head. The pool's headroom is
// evidence, not source, and arrives separately.
type ShardSource struct {
	HeadSHA  string
	Workflow prrepair.File
}

// ShardSample is one successful run of the selected job: how long its test step
// took and how long everything before it (checkout, install, build) took. Every
// shard pays the second figure again, which is why both are kept.
type ShardSample struct {
	JobID      string
	RunID      string
	Attempt    int
	StepNumber int
	TestMS     int64
	SetupMS    int64
}

// ShardEvidence is caller-supplied, not independently attested. TestMS and
// SetupMS are medians over Runs, because a few slow runs should not move the
// count. Headroom is how many more runners the selected pool could start at
// once, so that the shards do not queue behind each other.
type ShardEvidence struct {
	EvidenceID   string
	HeadSHA      string
	SourceSHA256 string
	Runs         int
	TestMS       int64
	SetupMS      int64
	Headroom     int
}

// ShardPreview is a proposed rewrite of one Playwright job into a shard matrix
// and an aggregator that keeps the job's name. Verification is always
// inconclusive: runner time rises for certain, and the wall-clock figures are
// estimates from medians, not a measured saving.
type ShardPreview struct {
	Version        string
	Kind           string
	State          string
	HeadSHA        string
	SourceSHA256   string
	AfterSHA256    string
	EvidenceID     string
	JobIndex       int
	StepIndex      int
	JobName        string
	StepName       string
	Line           int
	Shards         int
	Runs           int
	TestMS         int64
	SetupMS        int64
	Headroom       int
	RunnerMSBefore int64
	RunnerMSAfter  int64
	WallMSBefore   int64
	WallMSAfter    int64
	Edit           prrepair.Edit
	File           prrepair.File
	Verification   string
	Explanation    string
}

// ChooseShardCount picks the largest count, up to MaxShards and the pool's
// headroom, that still leaves each shard at least ShardTestFloorMS of test.
// More shards finish sooner, so the cap on each side is what keeps the extra
// runner time in proportion. It refuses when two shards would already fall
// below the floor, and when the pool could not run two at once.
func ChooseShardCount(testMS int64, headroom int) (int, error) {
	if testMS <= 0 {
		return 0, &ShardingRefusal{Reason: "there is no test timing to size the shards from"}
	}
	if testMS < int64(MinShards)*ShardTestFloorMS {
		return 0, &ShardingRefusal{Reason: "the test step takes " + setupSeconds(testMS) + ", so two shards would each leave less than " + setupSeconds(ShardTestFloorMS) + " of test and their setup would cost more than it saves"}
	}
	if headroom < MinShards {
		return 0, &ShardingRefusal{Reason: "the pool cannot start two runners at once, so shards would queue behind each other"}
	}
	n := MaxShards
	if headroom < n {
		n = headroom
	}
	for n > MinShards && testMS/int64(n) < ShardTestFloorMS {
		n--
	}
	return n, nil
}

// PreviewSharding computes the exact rewrite. The shard count comes from the
// evidence alone, never from the size of the repository or its test files. A
// workflow that already shards, filters, uploads artifacts or coverage, has a
// matrix, outputs, services or a condition is refused with the reason.
func PreviewSharding(source ShardSource, evidence ShardEvidence, policy Policy) (ShardPreview, error) {
	if !policy.SourceAllowed || !policy.WorkflowEditsAllowed || !commit(source.HeadSHA) || !cacheWorkflow(source.Workflow) {
		return ShardPreview{}, ErrShardingUnsupported
	}
	sourceDigest := hash(source.Workflow.Content)
	if evidence.HeadSHA != source.HeadSHA || evidence.SourceSHA256 != sourceDigest || evidence.EvidenceID == "" || len(evidence.EvidenceID) > 128 || strings.ContainsAny(evidence.EvidenceID, " \t\r\n\x00") || evidence.Runs < MinShardRuns || evidence.Runs > MaxShardRuns || evidence.TestMS <= 0 || evidence.TestMS > 7*24*60*60*1000 || evidence.SetupMS < 0 || evidence.SetupMS > 7*24*60*60*1000 || evidence.Headroom < 0 || evidence.Headroom > 10000 {
		return ShardPreview{}, ErrShardingUnsupported
	}
	count, err := ChooseShardCount(evidence.TestMS, evidence.Headroom)
	if err != nil {
		return ShardPreview{}, err
	}
	job, step, err := workflow.FindPlaywrightTest([]byte(source.Workflow.Content))
	if err != nil {
		return ShardPreview{}, refusal(err)
	}
	jobName, stepName, err := workflow.ShardNames([]byte(source.Workflow.Content), job, step)
	if err != nil {
		return ShardPreview{}, ErrShardingUnsupported
	}
	before, after, line, err := workflow.ShardChange([]byte(source.Workflow.Content), job, step, count)
	if err != nil {
		return ShardPreview{}, refusal(err)
	}
	edit := prrepair.Edit{Path: source.Workflow.Path, Before: before, After: after}
	files, err := prrepair.ApplySuppliedEdits([]prrepair.File{source.Workflow}, []prrepair.Edit{edit}, true)
	if err != nil {
		return ShardPreview{}, ErrShardingUnsupported
	}
	n := int64(count)
	perShard := (evidence.TestMS + n - 1) / n
	p := ShardPreview{
		Version: "sharding-preview-v1", Kind: "playwright_shards", State: "preview_ready",
		HeadSHA: source.HeadSHA, SourceSHA256: sourceDigest, AfterSHA256: hash(files[0].Content),
		EvidenceID: evidence.EvidenceID, JobIndex: job, StepIndex: step, JobName: jobName, StepName: stepName, Line: line,
		Shards: count, Runs: evidence.Runs, TestMS: evidence.TestMS, SetupMS: evidence.SetupMS, Headroom: evidence.Headroom,
		RunnerMSBefore: evidence.SetupMS + evidence.TestMS, RunnerMSAfter: n*evidence.SetupMS + evidence.TestMS,
		WallMSBefore: evidence.SetupMS + evidence.TestMS, WallMSAfter: evidence.SetupMS + perShard,
		Edit: edit, File: files[0], Verification: "inconclusive",
	}
	p.Explanation = "Split the " + stepName + " step of " + jobName + " into " + strconv.Itoa(count) + " Playwright shards that together run the whole suite, and keep a job named " + jobName + " that fails unless every shard passes. " +
		"Over " + runCount(evidence.Runs) + " the test step took a median " + setupSeconds(evidence.TestMS) + " after " + setupSeconds(evidence.SetupMS) + " of setup. " +
		"Runner time rises from about " + setupSeconds(p.RunnerMSBefore) + " to about " + setupSeconds(p.RunnerMSAfter) + " per run, plus one short aggregator job, because every shard repeats the setup, and it needs " + strconv.Itoa(count) + " runners at once. " +
		"Wall clock is estimated to fall to about " + setupSeconds(p.WallMSAfter) + " if the shards start together; that is an estimate from medians, not a measured saving."
	return p, nil
}

// refusal carries a workflow's reason across, so the operator is told what to
// change instead of only that no preview exists.
func refusal(err error) error {
	var r *workflow.ShardRefusal
	if errors.As(err, &r) {
		return &ShardingRefusal{Reason: r.Reason}
	}
	switch {
	case errors.Is(err, workflow.ErrShardNotFound):
		return &ShardingRefusal{Reason: "no single Playwright test step on an Ubuntu runner was found"}
	case errors.Is(err, workflow.ErrShardAmbiguous):
		return &ShardingRefusal{Reason: "more than one Playwright test step was found, and Zoomies will not guess which to shard"}
	}
	return ErrShardingUnsupported
}

// CheckShardingCurrent is a local precondition, not publication authorisation.
func CheckShardingCurrent(p ShardPreview, source ShardSource, evidence ShardEvidence, policy Policy) error {
	if p.Version != "sharding-preview-v1" || p.Kind != "playwright_shards" || p.State != "preview_ready" {
		return ErrShardingUnsupported
	}
	current, err := PreviewSharding(source, evidence, policy)
	if err != nil {
		return err
	}
	if current != p {
		return ErrShardingUnsupported
	}
	return nil
}

// ShardEvidenceFromSamples turns exact local timings into the evidence a
// sharding preview needs. The samples are sorted by job ID before the evidence
// ID is computed, so the same set gives the same ID in any order. Ten to one
// hundred distinct successful runs are required.
func ShardEvidenceFromSamples(head, sourceSHA256 string, headroom int, samples []ShardSample) (ShardEvidence, error) {
	if !commit(head) || len(sourceSHA256) != 64 || sourceSHA256 != strings.ToLower(sourceSHA256) || len(samples) < MinShardRuns || len(samples) > MaxShardRuns || headroom < 0 {
		return ShardEvidence{}, ErrShardingUnsupported
	}
	ordered := append([]ShardSample(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].JobID < ordered[j].JobID })
	parts := make([]string, 0, len(ordered))
	tests := make([]int64, 0, len(ordered))
	setups := make([]int64, 0, len(ordered))
	for i, s := range ordered {
		if s.JobID == "" || len(s.JobID) > 128 || strings.ContainsAny(s.JobID, " \t\r\n\x00") || s.RunID == "" || s.RunID != strconv.FormatInt(mustDecimal(s.RunID), 10) || s.Attempt < 1 || s.StepNumber <= 0 || s.TestMS <= 0 || s.SetupMS < 0 || i > 0 && ordered[i-1].JobID == s.JobID {
			return ShardEvidence{}, ErrShardingUnsupported
		}
		tests = append(tests, s.TestMS)
		setups = append(setups, s.SetupMS)
		parts = append(parts, s.JobID+"/"+s.RunID+"/"+strconv.Itoa(s.Attempt)+"/"+strconv.Itoa(s.StepNumber)+"/"+strconv.FormatInt(s.TestMS, 10)+"/"+strconv.FormatInt(s.SetupMS, 10))
	}
	test, setup := median(tests), median(setups)
	if test <= 0 || test > 7*24*60*60*1000 || setup < 0 || setup > 7*24*60*60*1000 {
		return ShardEvidence{}, ErrShardingUnsupported
	}
	return ShardEvidence{
		EvidenceID: hash("sharding-v1:" + head + ":" + sourceSHA256 + ":" + strings.Join(parts, ",")),
		HeadSHA:    head, SourceSHA256: sourceSHA256, Runs: len(ordered), TestMS: test, SetupMS: setup, Headroom: headroom,
	}, nil
}

func median(v []int64) int64 {
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

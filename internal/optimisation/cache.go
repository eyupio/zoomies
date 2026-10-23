package optimisation

import (
	"errors"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/eyupio/zoomies/internal/kennel/workflow"
	"github.com/eyupio/zoomies/internal/prrepair"
)

const MaxLockfileBytes = 1 << 20

// ErrCacheUnsupported is the one refusal for a setup cache preview. It says what
// the evidence needs without pretending a guessed package manager or step is a
// supported change.
var ErrCacheUnsupported = errors.New("setup cache preview needs one pinned setup-node step with a block with mapping, one supported root lockfile, repeated setup evidence and source/edit policy")

// CacheSource is complete supplied evidence: the workflow file at a pinned
// head and the root lockfile at the same head. The lockfile content is used
// only for its identity; the preview retains its path and digest, not it.
type CacheSource struct {
	HeadSHA  string
	Workflow prrepair.File
	Lockfile prrepair.File
	// LockfileSHA256 is the content digest when the lockfile itself is not
	// retained, as replay does. When Content is present it must match.
	LockfileSHA256 string
}

// SetupEvidence is caller-supplied, not independently attested. It says the
// setup step has cost this much over this many runs; the collector that binds
// exact run identities is a later slice.
type SetupEvidence struct {
	EvidenceID     string
	HeadSHA        string
	SourceSHA256   string
	LockfileSHA256 string
	Runs           int
	SetupMS        *int64
}

// CachePreview is a proposed cache key insertion, computed only from the
// supplied source and evidence. Verification is always inconclusive.
type CachePreview struct {
	Version        string
	Kind           string
	State          string
	HeadSHA        string
	SourceSHA256   string
	AfterSHA256    string
	LockfilePath   string
	LockfileSHA256 string
	PackageManager string
	EvidenceID     string
	JobIndex       int
	StepIndex      int
	JobName        string
	StepName       string
	Line           int
	Runs           int
	SetupMS        int64
	Edit           prrepair.Edit
	File           prrepair.File
	Verification   string
	Explanation    string
}

func cacheManager(lockfile string) string {
	switch lockfile {
	case "package-lock.json", "npm-shrinkwrap.json":
		return "npm"
	case "yarn.lock":
		return "yarn"
	case "pnpm-lock.yaml":
		return "pnpm"
	}
	return ""
}

func cacheLockfile(file prrepair.File, digest string) bool {
	if (file.Path != "package-lock.json" && file.Path != "npm-shrinkwrap.json" && file.Path != "yarn.lock" && file.Path != "pnpm-lock.yaml") || (file.Mode != "100644" && file.Mode != "100755") || file.Partial || file.StartLine != 0 || len(digest) != 64 || digest != strings.ToLower(digest) {
		return false
	}
	if file.Content == "" {
		return true
	}
	return len(file.Content) <= MaxLockfileBytes && utf8.ValidString(file.Content) && hash(file.Content) == digest
}

func cacheWorkflow(file prrepair.File) bool {
	return !file.Partial && file.StartLine == 0 && len(file.Content) > 0 && len(file.Content) <= MaxCheckoutSourceBytes && strings.HasPrefix(file.Path, ".github/workflows/") && path.Dir(file.Path) == ".github/workflows" && (strings.HasSuffix(file.Path, ".yml") || strings.HasSuffix(file.Path, ".yaml"))
}

// PreviewCache computes the exact insertion that enables the setup action's
// package cache. It refuses unknown lockfiles, unpinned or ambiguous setup
// steps, a cache already named, unsupported YAML, stale evidence and disabled
// policy. It reads nothing and calls no model.
func PreviewCache(source CacheSource, evidence SetupEvidence, policy Policy) (CachePreview, error) {
	if !policy.SourceAllowed || !policy.WorkflowEditsAllowed || !commit(source.HeadSHA) || !cacheWorkflow(source.Workflow) {
		return CachePreview{}, ErrCacheUnsupported
	}
	sourceDigest := hash(source.Workflow.Content)
	lockfileDigest := source.LockfileSHA256
	if source.Lockfile.Content != "" {
		computed := hash(source.Lockfile.Content)
		if lockfileDigest != "" && lockfileDigest != computed {
			return CachePreview{}, ErrCacheUnsupported
		}
		lockfileDigest = computed
	}
	if !cacheLockfile(source.Lockfile, lockfileDigest) {
		return CachePreview{}, ErrCacheUnsupported
	}
	manager := cacheManager(source.Lockfile.Path)
	if manager == "" || evidence.HeadSHA != source.HeadSHA || evidence.SourceSHA256 != sourceDigest || evidence.LockfileSHA256 != lockfileDigest || evidence.EvidenceID == "" || len(evidence.EvidenceID) > 128 || strings.ContainsAny(evidence.EvidenceID, " \t\r\n\x00") || evidence.Runs < 2 || evidence.Runs > 100 || evidence.SetupMS == nil || *evidence.SetupMS <= 0 || *evidence.SetupMS > 7*24*60*60*1000 {
		return CachePreview{}, ErrCacheUnsupported
	}
	job, step, err := workflow.FindSetupNode([]byte(source.Workflow.Content))
	if err != nil {
		return CachePreview{}, ErrCacheUnsupported
	}
	jobName, stepName, err := workflow.SetupNames([]byte(source.Workflow.Content), job, step)
	if err != nil {
		return CachePreview{}, ErrCacheUnsupported
	}
	before, after, line, err := workflow.SetupCacheChange([]byte(source.Workflow.Content), job, step, manager)
	if err != nil {
		return CachePreview{}, ErrCacheUnsupported
	}
	edit := prrepair.Edit{Path: source.Workflow.Path, Before: before, After: after}
	files, err := prrepair.ApplySuppliedEdits([]prrepair.File{source.Workflow}, []prrepair.Edit{edit}, true)
	if err != nil {
		return CachePreview{}, ErrCacheUnsupported
	}
	return CachePreview{
		Version: "cache-preview-v1", Kind: "dependency_cache", State: "preview_ready",
		HeadSHA: source.HeadSHA, SourceSHA256: sourceDigest, AfterSHA256: hash(files[0].Content),
		LockfilePath: source.Lockfile.Path, LockfileSHA256: lockfileDigest, PackageManager: manager,
		EvidenceID: evidence.EvidenceID, JobIndex: job, StepIndex: step, JobName: jobName, StepName: stepName, Line: line,
		Runs: evidence.Runs, SetupMS: *evidence.SetupMS, Edit: edit, File: files[0],
		Verification: "inconclusive",
		Explanation:  "Enable the setup action's " + manager + " cache using the root " + source.Lockfile.Path + ". The setup step averaged " + setupSeconds(*evidence.SetupMS) + " over " + runCount(evidence.Runs) + "; the saving is unverified and the cache is the action's own, shared with whoever can write this repository's cache. Passing CI alone cannot establish improvement.",
	}, nil
}

func setupSeconds(ms int64) string {
	if ms < 1000 {
		return "less than a second"
	}
	return strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64) + "s"
}

func runCount(n int) string {
	if n == 1 {
		return "1 run"
	}
	return strconv.Itoa(n) + " runs"
}

// CheckCacheCurrent is a local precondition, not publication authorisation. A
// later publisher must independently fetch the head and the lockfile and
// recheck policy immediately.
func CheckCacheCurrent(p CachePreview, source CacheSource, evidence SetupEvidence, policy Policy) error {
	if p.Version != "cache-preview-v1" || p.Kind != "dependency_cache" || p.State != "preview_ready" {
		return ErrCacheUnsupported
	}
	current, err := PreviewCache(source, evidence, policy)
	if err != nil {
		return err
	}
	if current.HeadSHA != p.HeadSHA || current.SourceSHA256 != p.SourceSHA256 || current.AfterSHA256 != p.AfterSHA256 || current.LockfilePath != p.LockfilePath || current.LockfileSHA256 != p.LockfileSHA256 || current.PackageManager != p.PackageManager || current.JobIndex != p.JobIndex || current.StepIndex != p.StepIndex || current.JobName != p.JobName || current.StepName != p.StepName || current.Line != p.Line || current.Edit != p.Edit || current.File != p.File {
		return ErrCacheUnsupported
	}
	return nil
}

// SetupSample is one exact local job's setup step. The controller maps the
// fleet's retained job rows into this shape; the optimisation package remains
// pure.
type SetupSample struct {
	JobID      string
	RunID      string
	Attempt    int
	StepNumber int
	SetupMS    int64
}

// SetupEvidenceFromSamples turns exact local setup samples into the evidence a
// cache preview needs. The samples are sorted by job ID before the evidence ID is
// computed, so the same set gives the same ID whatever order the store returned
// it in. Two to one hundred samples are required.
func SetupEvidenceFromSamples(head, sourceSHA256, lockfileSHA256 string, samples []SetupSample) (SetupEvidence, error) {
	if !commit(head) || len(sourceSHA256) != 64 || sourceSHA256 != strings.ToLower(sourceSHA256) || len(lockfileSHA256) != 64 || lockfileSHA256 != strings.ToLower(lockfileSHA256) || len(samples) < 2 || len(samples) > 100 {
		return SetupEvidence{}, ErrCacheUnsupported
	}
	ordered := append([]SetupSample(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].JobID < ordered[j].JobID })
	parts := make([]string, 0, len(ordered))
	var total int64
	for i, s := range ordered {
		if s.JobID == "" || len(s.JobID) > 128 || strings.ContainsAny(s.JobID, " \t\r\n\x00") || s.RunID == "" || s.RunID != strconv.FormatInt(mustDecimal(s.RunID), 10) || s.Attempt < 1 || s.StepNumber <= 0 || s.SetupMS <= 0 || i > 0 && ordered[i-1].JobID == s.JobID {
			return SetupEvidence{}, ErrCacheUnsupported
		}
		total += s.SetupMS
		parts = append(parts, s.JobID+"/"+s.RunID+"/"+strconv.Itoa(s.Attempt)+"/"+strconv.Itoa(s.StepNumber)+"/"+strconv.FormatInt(s.SetupMS, 10))
	}
	mean := total / int64(len(ordered))
	if mean <= 0 || mean > 7*24*60*60*1000 {
		return SetupEvidence{}, ErrCacheUnsupported
	}
	return SetupEvidence{
		EvidenceID: hash("setup-cache-v1:" + head + ":" + sourceSHA256 + ":" + lockfileSHA256 + ":" + strings.Join(parts, ",")),
		HeadSHA:    head, SourceSHA256: sourceSHA256, LockfileSHA256: lockfileSHA256,
		Runs: len(ordered), SetupMS: &mean,
	}, nil
}

func mustDecimal(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 || strconv.FormatInt(v, 10) != s {
		return -1
	}
	return v
}

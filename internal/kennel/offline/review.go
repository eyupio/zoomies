package offline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eyupio/zoomies/internal/kennel"
	"github.com/eyupio/zoomies/internal/kennel/workflow"
)

// ReviewVersion names the shape of a review, so a reader refuses one it does
// not know rather than misreading it.
const ReviewVersion = 1

// MaxReviewFiles bounds the workflow files one review reads, taken in path
// order. A pull request that touches more is reviewed in part, and says so. The
// GitHub reader uses the same number and the same order, so the files it read
// are the files the review judges.
const MaxReviewFiles = 20

// ReviewFile is a workflow file at one revision. Data is nil where the file did
// not exist there: a file the pull request adds has no base, one it deletes
// has no head.
type ReviewFile struct {
	Path string
	Data []byte
}

// ReviewInput is a pull request as the review is told about it. The caller
// reads both revisions at pinned commits; this package reads nothing. Every
// path the pull request changes is in Changed, whether or not it is a workflow,
// so the review can say what it did not look at.
type ReviewInput struct {
	Base, Head []ReviewFile
	Changed    []string
	// Unreadable are changed workflow paths the caller could not read at one of
	// the revisions (too large for GitHub to return, say). They are said to be
	// unreviewed: an absent file means deleted or added, and these are neither.
	Unreadable []string
	// Public says the repository is public, which makes some findings worse.
	Public   bool
	Disabled map[string]bool
}

// ReviewFinding is something the pull request introduced: a finding the head
// revision has that the base did not. Line is in the head file, which is the
// file the pull request's diff shows.
type ReviewFinding struct {
	Code     kennel.Code     `json:"code"`
	Severity kennel.Severity `json:"severity"`
	Path     string          `json:"path"`
	// Job is the job's position in the file's jobs mapping, -1 for the
	// workflow itself.
	Job    int    `json:"job"`
	Line   int    `json:"line"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
	// HeadSHA is the file's Git blob SHA at the head, which is what the
	// finding is about; a later push changes it and retires the finding.
	HeadSHA string `json:"head_sha"`
}

// NotReviewed is a changed path the review did not judge, and why. Saying so
// is the point: a review that lists only what it found reads as an all-clear.
type NotReviewed struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ReviewReport is what a review found. Introduced holds the findings the pull
// request added; Resolved counts those it removed.
type ReviewReport struct {
	Version    int             `json:"version"`
	Introduced []ReviewFinding `json:"introduced"`
	Resolved   int             `json:"resolved"`
	// Reviewed are the workflow paths that were judged.
	Reviewed []string `json:"reviewed"`
	// RuleChanges are changed paths that steer a reviewer or an agent. They are
	// reported on their own and never applied: this review used the base
	// revision's rules, so a pull request cannot rewrite what judges it.
	RuleChanges []string      `json:"rule_changes"`
	NotReviewed []NotReviewed `json:"not_reviewed"`
	// Complete is true only when every changed path was reviewed or is not
	// something this review covers by design, and nothing was cut or unreadable.
	Complete bool `json:"complete"`
}

// ruleFiles are the paths whose change alters what a reviewer or an agent is
// told. Matching is by name and by directory, case-insensitively, because the
// hosts that read them differ in case.
var ruleFiles = []string{
	"agents.md", "claude.md", "gemini.md", ".github/copilot-instructions.md", ".github/codeowners", "codeowners", "docs/codeowners",
}

var ruleDirs = []string{".zoomies/", ".claude/", ".cursor/", ".github/instructions/", ".agents/"}

func isRuleFile(p string) bool {
	lower := strings.ToLower(p)
	for _, name := range ruleFiles {
		if lower == name || strings.HasSuffix(lower, "/"+name) && !strings.Contains(name, "/") {
			return true
		}
	}
	for _, dir := range ruleDirs {
		if strings.HasPrefix(lower, dir) {
			return true
		}
	}
	return false
}

// Review judges what a pull request changed in the workflows against what the
// base revision already had, with the same parser and evaluator as Check. A
// finding the base already had is not the pull request's to answer for.
//
// Jobs are matched between revisions by their position in the file, so a pull
// request that reorders jobs can show a finding as new where it only moved.
// The report says what it did not cover rather than standing for an all-clear.
func Review(in ReviewInput) ReviewReport {
	r := ReviewReport{Version: ReviewVersion, Introduced: []ReviewFinding{}, Reviewed: []string{}, RuleChanges: []string{}, NotReviewed: []NotReviewed{}}
	base, head := map[string][]byte{}, map[string][]byte{}
	for _, f := range in.Base {
		base[f.Path] = f.Data
	}
	for _, f := range in.Head {
		head[f.Path] = f.Data
	}

	complete := true
	changed := append([]string(nil), in.Changed...)
	sort.Strings(changed)
	var workflows []string
	seen := map[string]bool{}
	for _, p := range changed {
		if seen[p] {
			continue
		}
		seen[p] = true
		switch {
		case isRuleFile(p):
			r.RuleChanges = append(r.RuleChanges, p)
		case GatePath(p) != "":
			if len(workflows) == MaxReviewFiles {
				r.NotReviewed = append(r.NotReviewed, NotReviewed{Path: p, Reason: fmt.Sprintf("the review reads at most %d workflow files", MaxReviewFiles)})
				complete = false
				continue
			}
			workflows = append(workflows, p)
		default:
			r.NotReviewed = append(r.NotReviewed, NotReviewed{Path: pathForReport(p), Reason: "not a workflow file, which is all this review covers"})
		}
	}

	unreadable := map[string]bool{}
	for _, p := range in.Unreadable {
		unreadable[p] = true
	}
	disabled := map[string]bool{string(kennel.CodeLabelUnserved): true}
	for k, v := range in.Disabled {
		disabled[k] = v
	}
	for _, p := range workflows {
		if unreadable[p] {
			r.NotReviewed = append(r.NotReviewed, NotReviewed{Path: p, Reason: "the file could not be read from GitHub at one of the two revisions"})
			complete = false
			continue
		}
		was, wasOK := evaluateFile(base[p], in.Public, disabled)
		now, nowOK := evaluateFile(head[p], in.Public, disabled)
		switch {
		case head[p] == nil:
			// Deleted: nothing remains to be wrong, and what it held is resolved.
			r.Resolved += len(was.findings)
			r.Reviewed = append(r.Reviewed, p)
			continue
		case !nowOK:
			r.NotReviewed = append(r.NotReviewed, NotReviewed{Path: p, Reason: "the file is too large or is not YAML this review can read"})
			complete = false
			continue
		}
		r.Reviewed = append(r.Reviewed, p)
		// A job can hold several of one finding (two unpinned actions), so the
		// revisions are compared by how many of each, and the head's extras
		// by line are the ones the pull request added.
		before := map[string]int{}
		if wasOK {
			for _, f := range was.findings {
				before[f.key()]++
			}
		}
		byKey := map[string][]reviewedFinding{}
		for _, f := range now.findings {
			byKey[f.key()] = append(byKey[f.key()], f)
		}
		for key, list := range byKey {
			sort.SliceStable(list, func(i, j int) bool { return list[i].line < list[j].line })
			extra := len(list) - before[key]
			for _, f := range list[max(len(list)-extra, 0):] {
				r.Introduced = append(r.Introduced, ReviewFinding{Code: f.code, Severity: f.severity, Path: p, Job: f.job, Line: f.line,
					Title: f.title, Detail: f.detail, Fix: f.fix, HeadSHA: now.sha})
			}
		}
		for key, n := range before {
			r.Resolved += max(n-len(byKey[key]), 0)
		}
	}
	sort.SliceStable(r.Introduced, func(i, j int) bool {
		a, b := r.Introduced[i], r.Introduced[j]
		if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
			return ra < rb
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})
	r.Complete = complete
	return r
}

func severityRank(s kennel.Severity) int {
	switch s {
	case kennel.SeverityError:
		return 0
	case kennel.SeverityWarning:
		return 1
	}
	return 2
}

// pathForReport keeps a stranger's path out of a report when it carries a
// control character; everything else is the path as git names it.
func pathForReport(p string) string {
	if len(p) > 256 || strings.ContainsAny(p, "\x00\r\n\t\x1b") {
		return "(an unusual path)"
	}
	return p
}

// reviewed is one revision of one file, evaluated.
type reviewed struct {
	sha      string
	findings []reviewedFinding
}

type reviewedFinding struct {
	code     kennel.Code
	severity kennel.Severity
	job      int
	line     int
	title    string
	detail   string
	fix      string
}

// key is what makes a finding the same one in both revisions: the check, and
// the job it is about. The line is left out, because a change above it moves
// it without making it new.
func (f reviewedFinding) key() string { return fmt.Sprintf("%s/%d", f.code, f.job) }

// evaluateFile runs the workflow checks over one file alone. ok is false for
// no file (nil data) and for one the parser or the size bound refuses.
func evaluateFile(data []byte, public bool, disabled map[string]bool) (reviewed, bool) {
	if data == nil {
		return reviewed{}, false
	}
	sha := blobSHA(data)
	parsed, err := workflow.Inspect(data)
	if len(data) > workflow.MaxBytes || err != nil {
		return reviewed{sha: sha}, false
	}
	visibility := kennel.VisibilityPrivate
	if public {
		visibility = kennel.VisibilityPublic
	}
	snap := kennel.Snapshot{
		Repo:      kennel.Repo{Visibility: visibility},
		Workflows: &kennel.WorkflowFacts{Files: []kennel.WorkflowFile{Convert(sha, parsed)}, Unreadable: []string{}},
		Coverage: kennel.Coverage{
			kennel.SourceMetadata:  {State: kennel.CoverageOK},
			kennel.SourceWorkflows: {State: kennel.CoverageOK},
		},
	}
	ev := kennel.Evaluate(snap, kennel.Policy{Disabled: disabled})
	out := reviewed{sha: sha}
	for _, f := range ev.Findings {
		if len(f.Evidence) == 0 {
			// A finding about the whole file has no place to point at; it is
			// kept at the workflow level, line one.
			out.findings = append(out.findings, reviewedFinding{code: f.Code, severity: f.Severity, job: -1, line: 1, title: f.Title, detail: f.Detail, fix: f.Fix})
			continue
		}
		for _, e := range f.Evidence {
			job := -1
			if e.JobIndex != nil {
				job = *e.JobIndex
			}
			out.findings = append(out.findings, reviewedFinding{code: f.Code, severity: f.Severity, job: job, line: max(e.Line, 1), title: f.Title, detail: f.Detail, fix: f.Fix})
		}
	}
	return out, true
}

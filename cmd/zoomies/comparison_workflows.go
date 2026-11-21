package main

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/eyupio/zoomies/internal/analysis"
)

// Human presentation has an independent REST whitelist. Structured output
// bypasses this decoder so future fields and exact numbers remain intact.
type cliWorkflowEvidence struct {
	Version   string          `json:"version"`
	Baseline  *cliWorkflowArm `json:"baseline"`
	Candidate *cliWorkflowArm `json:"candidate"`
}
type cliWorkflowArm struct {
	SelectedAttempts *int                 `json:"selected_attempts"`
	RootSources      *int                 `json:"root_sources"`
	RowsOmitted      *bool                `json:"rows_omitted"`
	Attempts         []cliWorkflowAttempt `json:"attempts"`
}
type cliWorkflowAttempt struct {
	RunID              string         `json:"run_id"`
	Attempt            int            `json:"attempt"`
	Event              *string        `json:"event"`
	WorkflowPathSHA256 *string        `json:"workflow_path_sha256"`
	RootSource         *cliRootSource `json:"root_source"`
}
type cliRootSource struct {
	Dependencies  *analysis.RootDependencies `json:"dependencies"`
	Basis         string                     `json:"basis"`
	CommitSHA     string                     `json:"commit_sha"`
	BlobSHA       string                     `json:"blob_sha"`
	ContentSHA256 string                     `json:"content_sha256"`
	Bytes         int                        `json:"bytes"`
}

func validCLIWorkflows(e *cliWorkflowEvidence, a, b analysis.ExperimentArm) bool {
	if e == nil {
		return true
	}
	if e.Version != analysis.WorkflowEvidenceVersion || e.Baseline == nil || e.Candidate == nil || len(a.Attempts) > analysis.MaxEvidenceRows || len(b.Attempts) > analysis.MaxEvidenceRows {
		return false
	}
	seen := map[string]bool{}
	for i, side := range []*cliWorkflowArm{e.Baseline, e.Candidate} {
		arm := a
		if i == 1 {
			arm = b
		}
		if side.SelectedAttempts == nil || side.RootSources == nil || side.RowsOmitted == nil || side.Attempts == nil || len(side.Attempts) > analysis.MaxEvidenceRows {
			return false
		}
		n, roots := *side.SelectedAttempts, *side.RootSources
		if n != len(arm.Attempts) || n < len(side.Attempts) || roots < 0 || roots > n || *side.RowsOmitted != (n > len(side.Attempts)) {
			return false
		}
		revisions := map[string]string{}
		for _, v := range arm.Attempts {
			revisions[strconv.FormatInt(v.RunID, 10)+":"+strconv.Itoa(v.Attempt)] = v.SourceRevision
		}
		shownRoots := 0
		dependencyRows := 0
		for _, row := range side.Attempts {
			key := row.RunID + ":" + strconv.Itoa(row.Attempt)
			revision, ok := revisions[key]
			if !analysis.DecimalID(row.RunID) || row.Attempt <= 0 || !ok || seen[key] {
				return false
			}
			seen[key] = true
			if row.Event != nil && !analysis.EventName(*row.Event) {
				return false
			}
			if row.WorkflowPathSHA256 != nil && !analysis.LowerHex(*row.WorkflowPathSHA256, 64) {
				return false
			}
			if s := row.RootSource; s != nil {
				if row.Event == nil || *row.Event != "push" || row.WorkflowPathSHA256 == nil || s.Basis != analysis.RootSourceBasis || !analysis.LowerHex(s.CommitSHA, 40) || s.CommitSHA != revision || !analysis.LowerHex(s.BlobSHA, 40) || !analysis.LowerHex(s.ContentSHA256, 64) || s.Bytes < 1 || s.Bytes > analysis.MaxRootSourceBytes {
					return false
				}
				if !analysis.ValidRootDependencies(s.Dependencies, s.ContentSHA256) {
					return false
				}
				if s.Dependencies != nil {
					dependencyRows += len(s.Dependencies.References)
				}
				if dependencyRows > analysis.RootDependencyRows {
					return false
				}
				shownRoots++
			}
		}
		if roots < shownRoots || roots-shownRoots > n-len(side.Attempts) {
			return false
		}
	}
	return true
}

var errCLIWorkflowEvidence = errors.New("this controller returned unsupported or inconsistent workflow evidence; inspect --output json and check the controller version")

func parseCLIWorkflowEvidence(raw json.RawMessage, a, b analysis.ExperimentArm) (*cliWorkflowEvidence, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var e cliWorkflowEvidence
	if json.Unmarshal(raw, &e) != nil || !validCLIWorkflows(&e, a, b) {
		return nil, errCLIWorkflowEvidence
	}
	return &e, nil
}
func cliWorkflowNullable(s *string) string {
	if s == nil {
		return "unknown"
	}
	return plain(*s)
}

func printCLIWorkflowEvidence(p *printer, e *cliWorkflowEvidence) {
	if e == nil {
		p.note("Workflow evidence is unavailable from this controller; missing facts remain unknown.")
		return
	}
	p.note("Workflow evidence: root-file digests describe bytes collected at a push commit, not attested execution or reusable workflow/action, input or test-scope coverage. Path fingerprints describe names, not content. Matching digests do not establish equivalent workloads.")
	for i, a := range []*cliWorkflowArm{e.Baseline, e.Candidate} {
		name := []string{"baseline", "candidate"}[i]
		p.note("%s workflow coverage: %d selected attempts, %d retained root sources; showing %d attempts. Counts include omitted rows; missing sources may be unsupported or absent from older captures.", name, *a.SelectedAttempts, *a.RootSources, len(a.Attempts))
		if *a.RowsOmitted {
			p.note("%s: additional workflow rows were omitted by the controller; no replacement evidence was collected.", name)
		}
		rows := [][]string{}
		for _, r := range a.Attempts {
			coverage := "unknown"
			if r.RootSource != nil {
				coverage = "root file only"
			}
			rows = append(rows, []string{r.RunID, strconv.Itoa(r.Attempt), cliWorkflowNullable(r.Event), coverage})
		}
		p.table([]string{"run", "attempt", "trigger event", "root source coverage"}, rows)
		for _, r := range a.Attempts {
			p.note("%s run %s attempt %d workflow details:", name, r.RunID, r.Attempt)
			fields := [][2]string{{"workflow path fingerprint", cliWorkflowNullable(r.WorkflowPathSHA256)}}
			if s := r.RootSource; s != nil {
				fields = append(fields, [][2]string{{"root source basis", s.Basis}, {"collected commit", s.CommitSHA}, {"Git blob SHA", s.BlobSHA}, {"root content SHA-256", s.ContentSHA256}, {"root file bytes", strconv.Itoa(s.Bytes)}}...)
			} else {
				fields = append(fields, [2]string{"root content SHA-256", "unknown"})
			}
			p.keyValues(fields)
			if r.RootSource != nil {
				printCLIDependencies(p, r.RootSource.Dependencies)
			}
		}
	}
}

func printCLIDependencies(p *printer, d *analysis.RootDependencies) {
	p.note("Dependency coverage: root uses declarations only; literal pins do not prove existence, execution or a resolved graph. Scripts, containers/services and transitive dependencies are outside this inventory.")
	if d == nil || d.Counts == nil {
		p.note("Dependency counts: unknown (absent or unsupported inspection).")
		return
	}
	c := d.Counts
	p.keyValues([][2]string{{"root declarations", strconv.Itoa(*c.Declarations)}, {"literal commit pins", strconv.Itoa(*c.CommitPins)}, {"literal image pins", strconv.Itoa(*c.DigestPins)}, {"floating references", strconv.Itoa(*c.Floating)}, {"unresolved local references", strconv.Itoa(*c.Local)}, {"expressions", strconv.Itoa(*c.Expressions)}, {"unrecognised references", strconv.Itoa(*c.Unknown)}})
	if *d.RowsOmitted {
		p.note("Showing %d dependency declarations; full counts include omitted rows.", len(d.References))
	}
	for _, r := range d.References {
		p.keyValues([][2]string{{"declaration", r.Declaration}, {"job index", strconv.Itoa(*r.JobIndex)}, {"step index", strconv.Itoa(*r.StepIndex)}, {"line", strconv.Itoa(*r.Line)}, {"reference kind", r.Kind}, {"pin kind", r.Pin}, {"reference fingerprint", r.ReferenceSHA256}, {"literal revision", cliWorkflowNullable(r.Revision)}})
	}
}

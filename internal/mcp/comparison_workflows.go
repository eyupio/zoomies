package mcp

import (
	"strconv"

	"github.com/eyupio/zoomies/internal/analysis"
)

// Independent REST whitelists keep private and future fields out of model input.
type comparisonWorkflowEvidence struct {
	Version   string                 `json:"version"`
	Baseline  *comparisonWorkflowArm `json:"baseline"`
	Candidate *comparisonWorkflowArm `json:"candidate"`
}
type comparisonWorkflowArm struct {
	SelectedAttempts *int                        `json:"selected_attempts"`
	RootSources      *int                        `json:"root_sources"`
	RowsOmitted      *bool                       `json:"rows_omitted"`
	Shown            int                         `json:"shown"`
	Attempts         []comparisonWorkflowAttempt `json:"attempts"`
}
type comparisonWorkflowAttempt struct {
	RunID              string                `json:"run_id"`
	Attempt            int                   `json:"attempt"`
	Event              *string               `json:"event"`
	WorkflowPathSHA256 *string               `json:"workflow_path_sha256"`
	RootSource         *comparisonRootSource `json:"root_source"`
}
type comparisonRootSource struct {
	Dependencies  *analysis.RootDependencies `json:"dependencies"`
	Basis         string                     `json:"basis"`
	CommitSHA     string                     `json:"commit_sha"`
	BlobSHA       string                     `json:"blob_sha"`
	ContentSHA256 string                     `json:"content_sha256"`
	Bytes         int                        `json:"bytes"`
}

func validateComparisonWorkflows(e *comparisonWorkflowEvidence, a, b analysis.ExperimentArm) bool {
	if e == nil {
		return true
	}
	if e.Version != analysis.WorkflowEvidenceVersion || e.Baseline == nil || e.Candidate == nil || len(a.Attempts) > analysis.MaxEvidenceRows || len(b.Attempts) > analysis.MaxEvidenceRows {
		return false
	}
	seen := map[string]bool{}
	for i, side := range []*comparisonWorkflowArm{e.Baseline, e.Candidate} {
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
		// Shown describes the locally selected rows, never an upstream assertion.
		side.Shown = len(side.Attempts)
	}
	return true
}

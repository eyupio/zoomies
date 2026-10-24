package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/optimisation"
)

type ReadinessInspectionRequest struct {
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	Selection      CacheSelectionBody `json:"selection"`
	Condition      string             `json:"condition"`
}

type ReadinessInspectionView struct {
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	Selection      CacheSelectionBody `json:"selection"`
	Condition      string             `json:"condition"`
	HeadSHA        string             `json:"head_sha"`
	WorkflowPath   string             `json:"workflow_path"`
	JobName        string             `json:"job_name"`
	StepName       string             `json:"step_name"`
	Line           int                `json:"line"`
	WaitSeconds    int                `json:"wait_seconds"`
	Runs           int                `json:"runs"`
	WaitMS         int64              `json:"wait_ms"`
	Before         string             `json:"before"`
	After          string             `json:"after"`
	Verification   string             `json:"verification"`
	Explanation    string             `json:"explanation"`
	InspectedAt    time.Time          `json:"inspected_at"`
}

func readinessInspectionView(p *optimisation.ReadinessPreview, installation, repo string, selection CacheSelectionBody) ReadinessInspectionView {
	return ReadinessInspectionView{
		InstallationID: installation, Repository: repo, Selection: selection, Condition: p.Condition,
		HeadSHA: p.HeadSHA, WorkflowPath: p.Edit.Path, JobName: p.JobName, StepName: p.StepName, Line: p.Line,
		WaitSeconds: p.WaitSeconds, Runs: p.Runs, WaitMS: p.WaitMS,
		Before: p.Edit.Before, After: p.Edit.After, Verification: p.Verification, Explanation: p.Explanation,
		InspectedAt: time.Now().UTC(),
	}
}

func (s *Server) handleInspectReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to inspect a readiness preview.")
		return
	}
	var in ReadinessInspectionRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	selection, githubJobID, ok := in.Selection.parse()
	if !ok || !validScope(in.InstallationID, in.Repository) || strings.TrimSpace(in.Condition) == "" || len(in.Condition) > 1024 || strings.ContainsAny(in.Condition, "\r\n\x00") {
		badRequest(w, "Supply an installation, a repository, a completed push attempt and the readiness command to poll.")
		return
	}
	out, err := s.ctrl.InspectReadinessPreview(r.Context(), in.InstallationID, in.Repository, selection, githubJobID, in.Condition)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, readinessInspectionView(out, in.InstallationID, in.Repository, in.Selection))
}

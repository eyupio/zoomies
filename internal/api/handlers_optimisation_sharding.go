package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/optimisation"
)

type ShardingInspectionRequest struct {
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	Selection      CacheSelectionBody `json:"selection"`
}

// ShardingInspectionView is the sharding preview an operator reviews before
// anything is kept. It carries the one changed job block, never the whole
// workflow, and every figure that is an estimate says so in its name or text.
type ShardingInspectionView struct {
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	Selection      CacheSelectionBody `json:"selection"`
	HeadSHA        string             `json:"head_sha"`
	SourceSHA256   string             `json:"source_sha256"`
	WorkflowPath   string             `json:"workflow_path"`
	JobName        string             `json:"job_name"`
	StepName       string             `json:"step_name"`
	Line           int                `json:"line"`
	Shards         int                `json:"shards"`
	Runs           int                `json:"runs"`
	TestMS         int64              `json:"test_ms"`
	SetupMS        int64              `json:"setup_ms"`
	Headroom       int                `json:"headroom"`
	RunnerMSBefore int64              `json:"runner_ms_before"`
	RunnerMSAfter  int64              `json:"runner_ms_after"`
	WallMSBefore   int64              `json:"wall_ms_before"`
	WallMSAfter    int64              `json:"wall_ms_after"`
	Before         string             `json:"before"`
	After          string             `json:"after"`
	Verification   string             `json:"verification"`
	Explanation    string             `json:"explanation"`
	InspectedAt    time.Time          `json:"inspected_at"`
}

func shardingInspectionView(p *optimisation.ShardPreview, installation, repo string, selection CacheSelectionBody) ShardingInspectionView {
	return ShardingInspectionView{
		InstallationID: installation, Repository: repo, Selection: selection,
		HeadSHA: p.HeadSHA, SourceSHA256: p.SourceSHA256, WorkflowPath: p.Edit.Path, JobName: p.JobName, StepName: p.StepName, Line: p.Line,
		Shards: p.Shards, Runs: p.Runs, TestMS: p.TestMS, SetupMS: p.SetupMS, Headroom: p.Headroom,
		RunnerMSBefore: p.RunnerMSBefore, RunnerMSAfter: p.RunnerMSAfter, WallMSBefore: p.WallMSBefore, WallMSAfter: p.WallMSAfter,
		Before: p.Edit.Before, After: p.Edit.After, Verification: p.Verification, Explanation: p.Explanation,
		InspectedAt: time.Now().UTC(),
	}
}

// handleInspectSharding computes a sharding preview from one selected job and
// keeps nothing. The shard count comes from the fleet's own history and pool
// headroom; the request names no count and no command.
func (s *Server) handleInspectSharding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to inspect a sharding preview.")
		return
	}
	var in ShardingInspectionRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	selection, githubJobID, ok := in.Selection.parse()
	if !ok || !validScope(in.InstallationID, in.Repository) || strings.TrimSpace(in.Repository) == "" {
		badRequest(w, "Supply an installation, a repository and a completed push attempt of the job to shard.")
		return
	}
	out, err := s.ctrl.InspectShardingPreview(r.Context(), in.InstallationID, in.Repository, selection, githubJobID)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, shardingInspectionView(out, in.InstallationID, in.Repository, in.Selection))
}

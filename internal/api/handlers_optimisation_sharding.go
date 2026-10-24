package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/optimisation"
	"github.com/eyupio/zoomies/internal/store"
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

type ShardingCaptureRequest struct {
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	Selection      CacheSelectionBody `json:"selection"`
	HeadSHA        string             `json:"head_sha"`
	SourceSHA256   string             `json:"source_sha256"`
	Shards         int                `json:"shards"`
}

// ShardingPreviewView is a kept sharding preview. It carries the same figures as
// the inspection the operator reviewed, and the job block, never the workflow.
type ShardingPreviewView struct {
	ID             string             `json:"id"`
	InstallationID string             `json:"installation_id"`
	Repository     string             `json:"repository"`
	ReviewedBy     string             `json:"reviewed_by"`
	SHA256         string             `json:"sha256"`
	CreatedAt      time.Time          `json:"created_at"`
	Currentness    string             `json:"currentness"`
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
}

func shardingPreviewView(p *controller.RetainedShardingPreview) ShardingPreviewView {
	d := p.Preview
	return ShardingPreviewView{
		ID: p.ID, InstallationID: p.InstallationID, Repository: p.Repository, ReviewedBy: p.ReviewedBy, SHA256: p.SHA256,
		CreatedAt: p.CreatedAt, Currentness: p.Currentness,
		Selection: CacheSelectionBody{RunID: strconv.FormatInt(p.Provenance.Selection.RunID, 10), Attempt: p.Provenance.Selection.Attempt, GitHubJobID: strconv.FormatInt(p.Provenance.GitHubJobID, 10)},
		HeadSHA:   d.HeadSHA, SourceSHA256: d.SourceSHA256, WorkflowPath: d.Edit.Path, JobName: d.JobName, StepName: d.StepName, Line: d.Line,
		Shards: d.Shards, Runs: d.Runs, TestMS: d.TestMS, SetupMS: d.SetupMS, Headroom: d.Headroom,
		RunnerMSBefore: d.RunnerMSBefore, RunnerMSAfter: d.RunnerMSAfter, WallMSBefore: d.WallMSBefore, WallMSAfter: d.WallMSAfter,
		Before: d.Edit.Before, After: d.Edit.After, Verification: d.Verification, Explanation: d.Explanation,
	}
}

func validShardingPreviewID(id string) bool {
	return strings.HasPrefix(id, store.PrefixOptimisationShardingPreview+"_") && len(id) <= 128 && !strings.ContainsAny(id, " /\r\n\x00")
}

// handleCaptureSharding keeps the sharding preview the operator reviewed. The
// server reads the source and history again and keeps it only if the commit,
// the workflow digest and the shard count are the ones that were reviewed.
func (s *Server) handleCaptureSharding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := Identity(r.Context())
	if id == nil || id.ID == "" {
		unauthorized(w, "An identified caller is required to keep a sharding preview.")
		return
	}
	var in ShardingCaptureRequest
	if !decodeOptimisationBody(w, r, &in) {
		return
	}
	selection, githubJobID, ok := in.Selection.parse()
	if !ok || !validScope(in.InstallationID, in.Repository) || !validDigest(in.HeadSHA, 40) || !validDigest(in.SourceSHA256, 64) || in.Shards < optimisation.MinShards || in.Shards > optimisation.MaxShards {
		badRequest(w, "Supply an installation, a repository, the selected run and job, and the commit, source digest and shard count you reviewed.")
		return
	}
	actor := &store.AuditEvent{ActorID: id.ID, ActorName: id.Name, ActorKind: id.Kind, IP: id.IP}
	out, err := s.ctrl.CaptureShardingPreview(r.Context(), actor, in.InstallationID, in.Repository, selection, githubJobID, in.HeadSHA, in.SourceSHA256, in.Shards)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeCreated(w, r, out.ID, shardingPreviewView(out))
}

// handleGetSharding replays a kept sharding preview from stored input only.
func (s *Server) handleGetSharding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil || len(q) != 2 || len(q["installation_id"]) != 1 || len(q["repo"]) != 1 || !validScope(q.Get("installation_id"), q.Get("repo")) {
		badRequest(w, "Supply exactly one installation_id and repo, without other query arguments.")
		return
	}
	id := chiURLParam(r, "id")
	if !validShardingPreviewID(id) {
		badRequest(w, "Supply a sharding preview ID.")
		return
	}
	out, err := s.ctrl.ReplayShardingPreview(r.Context(), q.Get("installation_id"), q.Get("repo"), id)
	if err != nil {
		s.optimisationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, shardingPreviewView(out))
}

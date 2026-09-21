package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
)

func (s *Server) handleProvisioningSelection(w http.ResponseWriter, r *http.Request) {
	filter, ok := parseJobFilter(w, r)
	if !ok {
		return
	}
	ids, err := s.ctrl.Store().ProvisioningSelection(r.Context(), filter)
	if err != nil {
		if errors.Is(err, store.ErrProvisioningSelectionTooLarge) {
			badRequest(w, err.Error())
		} else {
			s.internal(w, r, "selecting provisioning items", err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ids": ids})
}

func (s *Server) handleControlProvisioning(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []string `json:"ids"`
		Action string   `json:"action"`
	}
	if !decode(w, r, &req) {
		return
	}
	switch req.Action {
	case "pause", "resume", "delete", "run_now":
	default:
		badRequestField(w, "action", "use pause, resume, delete or run_now")
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > store.MaxProvisioningSelection {
		badRequestField(w, "ids", "select between 1 and 5000 items")
		return
	}
	results, err := s.ctrl.ControlProvisioning(r.Context(), req.IDs, req.Action)
	if err != nil {
		s.internal(w, r, "updating provisioning", err)
		return
	}
	for _, result := range results {
		if result.OK {
			s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "provisioning."+req.Action, "job", result.ID, nil)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

type controlWorkflowRunProvisioningRequest struct {
	Repo   string `json:"repo"`
	RunID  int64  `json:"run_id"`
	Action string `json:"action"`
}

// handleControlWorkflowRunProvisioning answers POST /api/v1/workflow-runs/provisioning:
// the Workflows page's own run rows pausing, resuming or expediting every
// queued job of a run at once, rather than opening the run and pressing the
// same button on each job inside. Delete is deliberately not offered here: a
// removal is a decision about one job, taken on the Queue page where the
// Removed view can undo it.
func (s *Server) handleControlWorkflowRunProvisioning(w http.ResponseWriter, r *http.Request) {
	var req controlWorkflowRunProvisioningRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Repo == "" {
		badRequestField(w, "repo", "repo is required")
		return
	}
	if req.RunID <= 0 {
		badRequestField(w, "run_id", "run_id must be a positive GitHub run ID")
		return
	}
	switch req.Action {
	case "pause", "resume", "run_now":
	default:
		badRequestField(w, "action", "use pause, resume or run_now; a job is removed from the queue one at a time, through /provisioning/bulk")
		return
	}
	results, err := s.ctrl.ControlWorkflowRunProvisioning(r.Context(), req.Repo, req.RunID, req.Action)
	if err != nil {
		switch {
		case errors.Is(err, controller.ErrRunHasNoQueuedJobs):
			conflict(w, err.Error()+"; every job of it has started or finished, or was removed from the queue and is restored from the Queue page's Removed view")
		default:
			s.fail(w, r, "updating the workflow run's provisioning", err)
		}
		return
	}
	target := fmt.Sprintf("%s#%d", req.Repo, req.RunID)
	for _, result := range results {
		if result.OK {
			s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "provisioning."+req.Action, "job", result.ID, map[string]any{
				"repo": req.Repo, "run_id": req.RunID, "workflow_run": target,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "repo": req.Repo, "run_id": req.RunID})
}

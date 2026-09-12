package api

import (
	"errors"
	"github.com/eyupio/zoomies/internal/store"
	"net/http"
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

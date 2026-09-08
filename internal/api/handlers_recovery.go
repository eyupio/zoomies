package api

import (
	"net/http"

	"github.com/eyupio/zoomies/internal/controller"
)

// recoveryResponse is the fence's state.
//
// It is the response to lifting it as well as to reading it, so a client that
// lifted the fence knows what it now is rather than having to ask again.
type recoveryResponse struct {
	Fenced bool   `json:"fenced"`
	Reason string `json:"reason,omitempty"`
}

func newRecoveryResponse(f controller.Fence) recoveryResponse {
	return recoveryResponse{Fenced: f.Fenced, Reason: f.Reason}
}

// handleGetRecovery answers GET /api/v1/recovery.
func (s *Server) handleGetRecovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, newRecoveryResponse(s.ctrl.Fenced()))
}

// handleUnfence answers POST /api/v1/recovery/unfence.
//
// It is a person saying that a recovered fleet has been checked and may act on
// the world again, which is why it is audited under its own action rather than
// folded into the settings route: the audit log is where somebody later asks
// who decided that, and when.
func (s *Server) handleUnfence(w http.ResponseWriter, r *http.Request) {
	before := s.ctrl.Fenced()
	if !before.Fenced {
		// Not an error. Two operators recovering one fleet will both press it,
		// and the second must not be told something went wrong.
		writeJSON(w, http.StatusOK, newRecoveryResponse(before))
		return
	}
	if err := s.ctrl.Unfence(r.Context()); err != nil {
		s.internal(w, r, "lifting the recovery fence", err)
		return
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "recovery.unfence", "instance", "", map[string]any{
		"was_fenced_because": before.Reason,
	})
	writeJSON(w, http.StatusOK, newRecoveryResponse(s.ctrl.Fenced()))
}

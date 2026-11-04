package api

import "net/http"

// handleControllerLoad serves where the controller's own time goes: its loops and
// routes as the metrics endpoint counts them, and the machine it runs on beside
// them. It reads what the controller has already kept and does no work of its own
// beyond reading it, so asking costs the controller almost nothing at the moment
// it is asked because it is busy.
func (s *Server) handleControllerLoad(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ctrl.ControllerLoad())
}

package api

import "net/http"

// handleControllerMachine serves the last reading of the machine the controller
// itself runs on. It reads what the housekeeping tick sampled and never samples
// here: the figures are shares of the interval between two samples, and a
// request that took one would set the interval.
func (s *Server) handleControllerMachine(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ctrl.ControllerMachine())
}

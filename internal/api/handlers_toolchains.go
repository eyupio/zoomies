package api

import (
	"errors"
	"net/http"

	"github.com/eyupio/zoomies/internal/controller"
)

// handleToolchains is GET /toolchains: the latest reading of which toolchain
// versions each pool's jobs install, and whether a scan is running now.
func (s *Server) handleToolchains(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ctrl.ToolchainScanResult())
}

// handleScanToolchains is POST /toolchains/scan. The scan runs in the
// background -- on a large organisation it is minutes of GitHub calls -- so
// this answers 202 at once and GET /toolchains says when it is done.
func (s *Server) handleScanToolchains(w http.ResponseWriter, r *http.Request) {
	if err := s.ctrl.StartToolchainScan(r.Context()); err != nil {
		if errors.Is(err, controller.ErrToolchainScanRunning) {
			conflict(w, "a toolchain scan is already running; GET /api/v1/toolchains says when it finishes")
			return
		}
		s.internal(w, r, "starting a toolchain scan", err)
		return
	}
	// It reads every workflow file the installations can see, which spends
	// the GitHub quota the scheduler shares -- worth a line in the audit log.
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "toolchains.scan", "toolchains", "", nil)
	writeJSON(w, http.StatusAccepted, s.ctrl.ToolchainScanResult())
}

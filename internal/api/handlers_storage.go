package api

import (
	"net/http"

	"github.com/eyupio/zoomies/internal/backup"
)

// handleGetStorage answers GET /api/v1/storage: what is in the state directory,
// class by class, and how much of the database and the disk is free.
//
// A thin layer over backup.Storage, which is what `zoomies storage` would run
// beside a controller too. The page counts come from the store because only the
// connection that owns the file can ask for them; everything else is read from
// the directory itself, so the report is what is on disk and not what the
// controller remembers putting there.
func (s *Server) handleGetStorage(w http.ResponseWriter, r *http.Request) {
	pages, err := s.ctrl.Store().PageStats(r.Context())
	if err != nil {
		s.internal(w, r, "reading how full the database file is", err)
		return
	}
	report, err := backup.Storage(r.Context(), s.ctrl.Config(), pages)
	switch {
	case r.Context().Err() != nil:
		// The client left, and the walk stopped with it. There is nobody to
		// answer.
		return
	case err != nil:
		s.internal(w, r, "reading what is in the state directory", err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

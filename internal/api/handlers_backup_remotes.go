package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/controller"
)

// The offsite half of the backups surface.
//
// Everything here is a thin layer over internal/backup's remotes, which is the
// same code the controller's own pass and `zoomies backup --offsite` run: the
// page lists what a bucket holds, sends what it is missing, tests that it can
// be reached, pulls a copy back, and deletes one.
//
// What it deliberately does not have is "restore from the bucket". A restore
// swaps the database this fleet runs on, and the copy it swaps in should be
// one somebody has seen land and verified first -- so fetching puts the archive
// in the backup directory as an ordinary backup, and the existing staged
// restore does the rest. Two steps, both reversible until the last one.

// remoteCopiesResponse is what one destination holds.
type remoteCopiesResponse struct {
	Remote controller.RemoteBackupStatus `json:"remote"`
	Items  []backup.Copy                 `json:"items"`
	// Bytes is what the copies add up to, so the page can say what this
	// bucket is costing without adding a column up itself.
	Bytes int64 `json:"bytes"`
}

// remoteCheckResponse is the answer to "can this bucket be reached".
type remoteCheckResponse struct {
	Name      string    `json:"name"`
	Where     string    `json:"where"`
	OK        bool      `json:"ok"`
	CheckedAt time.Time `json:"checked_at"`
	// Error is the service's own refusal, worded by internal/backup so that a
	// wrong secret, a missing bucket and a drifted clock read differently.
	Error string `json:"error,omitempty"`
}

// shipResponse is what an offsite pass sent.
type shipResponse struct {
	Sent []backup.Copy `json:"sent"`
	// Error is what stopped one destination, when the others still went. The
	// pass is reported as it happened rather than as all or nothing: a typo in
	// the second bucket must not hide that the first one worked.
	Error string `json:"error,omitempty"`
}

// fetchRequest is the body of a fetch: a passphrase for an archive sealed with
// one this configuration no longer carries, which is what a rotation leaves
// behind in a bucket.
type fetchRequest struct {
	Passphrase string `json:"passphrase"`
}

// failRemote renders the refusals a remote makes.
func (s *Server) failRemote(w http.ResponseWriter, r *http.Request, doing string, err error) {
	switch {
	case errors.Is(err, backup.ErrNoRemote):
		notFound(w, "this controller has no backup remote by that name; they are configured under backup.remotes")
	case errors.Is(err, backup.ErrInvalidID):
		badRequestField(w, "id", "that is not a backup id; one looks like zoomies-20260916-120000")
	case errors.Is(err, controller.ErrShippingRunning):
		conflict(w, err.Error())
	default:
		s.internal(w, r, doing, err)
	}
}

// handleListRemoteCopies answers GET /api/v1/backups/remotes/{name}/copies.
//
// It is a live listing rather than something the controller remembers, because
// the question it answers is "is the offsite copy actually there?" and a
// cached yes is exactly the answer that is worth nothing.
func (s *Server) handleListRemoteCopies(w http.ResponseWriter, r *http.Request) {
	name := chiURLParam(r, "name")
	remote, err := s.ctrl.RemoteBackup(r.Context(), name)
	if err != nil {
		s.failRemote(w, r, "reading the backup remote", err)
		return
	}
	copies, err := remote.List(r.Context())
	if err != nil {
		// The bucket refusing is not this API failing: the message is the
		// service's own and the operator can act on it.
		unprocessable(w, "this backup remote could not be read", []fieldError{{"name", err.Error()}})
		return
	}
	out := remoteCopiesResponse{Items: copies}
	for _, c := range copies {
		out.Bytes += c.Bytes
	}
	s.ctrl.NoteRemoteListing(name, len(copies), out.Bytes)
	for _, status := range s.ctrl.BackupRemotes(r.Context()) {
		if status.Name == name {
			out.Remote = status
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCheckRemote answers POST /api/v1/backups/remotes/{name}/check: one
// cheap listing, which is the whole of what has to work for a backup to reach
// the bucket.
//
// A POST because it goes out to the network on somebody else's account, and an
// answer of "no" is a 200 with ok:false rather than an error status: the check
// ran, and what it found is the result.
func (s *Server) handleCheckRemote(w http.ResponseWriter, r *http.Request) {
	name := chiURLParam(r, "name")
	remote, err := s.ctrl.RemoteBackup(r.Context(), name)
	if err != nil {
		s.failRemote(w, r, "reading the backup remote", err)
		return
	}
	out := remoteCheckResponse{Name: remote.Name(), Where: remote.Where(), CheckedAt: s.ctrl.Now()}
	if err := remote.Check(r.Context()); err != nil {
		out.Error = err.Error()
	} else {
		out.OK = true
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "backup.remote_check", "backup_remote", name, map[string]any{
		"ok": out.OK, "where": out.Where,
	})
	writeJSON(w, http.StatusOK, out)
}

// handleShipBackups answers POST /api/v1/backups/offsite: send every remote
// what it is missing, now.
//
// It is the same pass the controller runs on its own, exposed because an
// operator who has just fixed a credential wants to know it worked without
// waiting an hour to find out.
func (s *Server) handleShipBackups(w http.ResponseWriter, r *http.Request) {
	sent, err := s.ctrl.ShipBackups(r.Context())
	if errors.Is(err, controller.ErrShippingRunning) {
		conflict(w, err.Error())
		return
	}
	out := shipResponse{Sent: sent}
	if out.Sent == nil {
		out.Sent = []backup.Copy{}
	}
	if err != nil {
		out.Error = err.Error()
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "backup.offsite", "backup", "", map[string]any{
		"sent": len(out.Sent), "error": out.Error,
	})
	writeJSON(w, http.StatusOK, out)
}

// handleFetchRemoteCopy answers
// POST /api/v1/backups/remotes/{name}/copies/{id}/fetch: the archive comes
// down, is unpacked and verified exactly as an upload would be, and is then an
// ordinary backup in the directory.
func (s *Server) handleFetchRemoteCopy(w http.ResponseWriter, r *http.Request) {
	name, id := chiURLParam(r, "name"), chiURLParam(r, "id")
	remote, err := s.ctrl.RemoteBackup(r.Context(), name)
	if err != nil {
		s.failRemote(w, r, "reading the backup remote", err)
		return
	}
	var body fetchRequest
	if !decodeOptional(w, r, &body) {
		return
	}
	// A fetch writes a whole database over a link nobody promised anything
	// about; the request is not held to the API's ordinary write deadline.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	identity := Identity(r.Context())
	entry, err := remote.Fetch(r.Context(), s.ctrl.BackupDir(), id, backup.FetchOptions{
		Passphrase: body.Passphrase, TakenBy: identity.Name,
		MaxBytes: maxBackupUploadBytes, Now: s.ctrl.Now,
	})
	switch {
	case errors.Is(err, backup.ErrInvalidID):
		badRequestField(w, "id", "that is not a backup id; one looks like zoomies-20260916-120000")
		return
	case errors.Is(err, backup.ErrWrongPassphrase):
		unprocessable(w, "the archive did not open", []fieldError{{"passphrase", backup.ErrWrongPassphrase.Error()}})
		return
	case err != nil:
		unprocessable(w, "this copy could not be brought back", []fieldError{{"id", err.Error()}})
		return
	}
	s.auth.Auditor().Act(r.Context(), identity, "backup.remote_fetch", "backup", entry.ID, map[string]any{
		"remote": name, "where": remote.Where(), "bytes": entry.Bytes, "taken_at": entry.TakenAt,
	})
	writeJSON(w, http.StatusCreated, s.backupView(*entry, locationBackups))
}

// handleDeleteRemoteCopy answers
// DELETE /api/v1/backups/remotes/{name}/copies/{id}.
//
// Deleting the offsite copy is audited like deleting the local one, and for a
// stronger reason: it is the copy that exists because the local ones might
// not.
func (s *Server) handleDeleteRemoteCopy(w http.ResponseWriter, r *http.Request) {
	name, id := chiURLParam(r, "name"), chiURLParam(r, "id")
	remote, err := s.ctrl.RemoteBackup(r.Context(), name)
	if err != nil {
		s.failRemote(w, r, "reading the backup remote", err)
		return
	}
	if err := remote.Delete(r.Context(), id); err != nil {
		if errors.Is(err, backup.ErrInvalidID) {
			badRequestField(w, "id", "that is not a backup id; one looks like zoomies-20260916-120000")
			return
		}
		unprocessable(w, "this copy could not be removed", []fieldError{{"id", err.Error()}})
		return
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "backup.remote_delete", "backup", id, map[string]any{
		"remote": name, "where": remote.Where(),
	})
	noContent(w)
}

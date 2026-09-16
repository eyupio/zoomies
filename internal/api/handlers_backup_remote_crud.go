package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
)

// Adding a destination from the page.
//
// `backup.remotes` in zoomies.yaml describes destinations too, and it stays --
// it is the one readable on a host whose database is gone. This is the other
// half: the destinations somebody adds, tests and rotates in the UI, stored as
// rows with their two secrets sealed under the instance key exactly as a
// provider's credential is.
//
// The secrets travel one way. They arrive in a body, they are sealed before
// anything else happens to them, and no response ever carries them back: the
// page is told whether one is set, which is the only thing it can act on.

// remoteInput is the body of a create or an update.
//
// Every field is a pointer so that a PATCH can carry the two an operator
// actually changed. The two secrets follow the convention the rest of the API
// uses for a credential: absent leaves what is stored, a value replaces it,
// and an explicit empty string clears it -- which is how a passphrase is
// removed from a destination that should send the plain archive.
type remoteInput struct {
	Name        *string `json:"name"`
	Endpoint    *string `json:"endpoint"`
	Region      *string `json:"region"`
	Bucket      *string `json:"bucket"`
	Prefix      *string `json:"prefix"`
	AccessKeyID *string `json:"access_key_id"`
	SecretKey   *string `json:"secret_access_key"`
	Passphrase  *string `json:"passphrase"`
	PathStyle   *bool   `json:"path_style"`
	Keep        *int    `json:"keep"`
	Enabled     *bool   `json:"enabled"`
}

// apply writes the input over a row and returns what is wrong with the result.
func (in *remoteInput) apply(row *store.BackupRemote) []fieldError {
	if in.Name != nil {
		row.Name = strings.ToLower(strings.TrimSpace(*in.Name))
	}
	if in.Endpoint != nil {
		row.Endpoint = strings.TrimRight(strings.TrimSpace(*in.Endpoint), "/")
	}
	if in.Region != nil {
		row.Region = strings.TrimSpace(*in.Region)
	}
	if in.Bucket != nil {
		row.Bucket = strings.TrimSpace(*in.Bucket)
	}
	if in.Prefix != nil {
		row.Prefix = strings.Trim(strings.TrimSpace(*in.Prefix), "/")
	}
	if in.AccessKeyID != nil {
		row.AccessKeyID = strings.TrimSpace(*in.AccessKeyID)
	}
	if in.PathStyle != nil {
		style := *in.PathStyle
		row.PathStyle = &style
	}
	if in.Keep != nil {
		row.Keep = *in.Keep
	}
	if in.Enabled != nil {
		row.Enabled = *in.Enabled
	}
	return validateRemoteRow(row)
}

// validateRemoteRow refuses a destination that could not work, in the words
// the validator uses about the same mistake in the configuration file.
func validateRemoteRow(row *store.BackupRemote) []fieldError {
	var errs []fieldError
	if !config.ValidRemoteName(row.Name) {
		errs = append(errs, fieldError{"name",
			"use lower-case letters, digits and dashes, such as offsite or s3-frankfurt -- and not " +
				strings.Join(config.ReservedRemoteNames, " or ") + ", which the API already uses where the name goes"})
	}
	if strings.TrimSpace(row.Bucket) == "" {
		errs = append(errs, fieldError{"bucket",
			"name the bucket the archives go in. Zoomies never creates one: a destination that appeared by itself is one nobody has set the retention or access policy of"})
	}
	u, err := url.Parse(strings.TrimSpace(row.Endpoint))
	switch {
	case strings.TrimSpace(row.Endpoint) == "":
		errs = append(errs, fieldError{"endpoint",
			"give the service's URL, such as https://s3.eu-west-2.amazonaws.com or http://minio:9000"})
	case err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https"):
		errs = append(errs, fieldError{"endpoint",
			"that is not an HTTP URL. The scheme is what decides whether the connection is encrypted, so it has to be there"})
	}
	if row.Keep < 0 {
		errs = append(errs, fieldError{"keep", "use 0 to keep every copy, or the number of copies this destination should hold"})
	}
	return errs
}

// handleCreateBackupRemote answers POST /api/v1/backups/remotes.
func (s *Server) handleCreateBackupRemote(w http.ResponseWriter, r *http.Request) {
	if s.key == nil {
		s.noEncryptionKey(w)
		return
	}
	var in remoteInput
	if !decode(w, r, &in) {
		return
	}
	row := &store.BackupRemote{Enabled: true}
	errs := in.apply(row)
	if in.SecretKey == nil || strings.TrimSpace(*in.SecretKey) == "" {
		errs = append(errs, fieldError{"secret_access_key",
			"a destination with no secret key would have every request to it refused"})
	}
	errs = append(errs, s.remoteNameErrors(r, row.Name, "")...)
	if len(errs) > 0 {
		unprocessable(w, "this backup remote cannot be created as described", errs)
		return
	}

	if err := s.ctrl.Store().CreateBackupRemote(r.Context(), row); err != nil {
		s.fail(w, r, "creating the backup remote", err)
		return
	}
	if !s.sealRemoteSecrets(w, r, row, &in) {
		return
	}
	// The row is the audit document: the secrets are sealed bytes behind
	// json:"-", so there is nothing here to blank first.
	s.auth.Auditor().Created(r.Context(), Identity(r.Context()), "backup_remote", row.ID, row)
	// A destination that has just been added is one the fleet has nothing in
	// yet, so the copies it is missing go now rather than within the hour.
	s.ctrl.NudgeBackupRemotes()
	writeJSON(w, http.StatusCreated, s.remoteStatus(r, row.Name))
}

// handleUpdateBackupRemote answers PATCH /api/v1/backups/remotes/{name}.
func (s *Server) handleUpdateBackupRemote(w http.ResponseWriter, r *http.Request) {
	row, ok := s.storedRemote(w, r)
	if !ok {
		return
	}
	var in remoteInput
	if !decode(w, r, &in) {
		return
	}
	if s.key == nil && (in.SecretKey != nil && strings.TrimSpace(*in.SecretKey) != "" ||
		in.Passphrase != nil && strings.TrimSpace(*in.Passphrase) != "") {
		s.noEncryptionKey(w)
		return
	}
	before := *row
	errs := in.apply(row)
	errs = append(errs, s.remoteNameErrors(r, row.Name, row.ID)...)
	if len(errs) > 0 {
		unprocessable(w, "this backup remote cannot be changed as described", errs)
		return
	}
	if err := s.ctrl.Store().UpdateBackupRemote(r.Context(), row); err != nil {
		s.fail(w, r, "saving the backup remote", err)
		return
	}
	if !s.sealRemoteSecrets(w, r, row, &in) {
		return
	}
	s.auth.Auditor().Updated(r.Context(), Identity(r.Context()), "backup_remote", row.ID, &before, row)
	s.ctrl.NudgeBackupRemotes()
	writeJSON(w, http.StatusOK, s.remoteStatus(r, row.Name))
}

// handleDeleteBackupRemote answers DELETE /api/v1/backups/remotes/{name}.
//
// What the bucket holds is left alone. Forgetting where the copies are is not
// the same as deciding they should not exist, and an operator who wants them
// gone deletes them from the listing first -- where each one is named.
func (s *Server) handleDeleteBackupRemote(w http.ResponseWriter, r *http.Request) {
	row, ok := s.storedRemote(w, r)
	if !ok {
		return
	}
	if err := s.ctrl.Store().DeleteBackupRemote(r.Context(), row.ID); err != nil {
		s.fail(w, r, "removing the backup remote", err)
		return
	}
	s.auth.Auditor().Deleted(r.Context(), Identity(r.Context()), "backup_remote", row.ID, row)
	noContent(w)
}

// handleCheckDraftRemote answers POST /api/v1/backups/remotes/check: try a
// destination that has not been saved.
//
// It exists so that a secret key is proved before it is stored rather than
// after, which is the difference between finding out now and finding out at
// three in the morning. A destination that refuses is a 200 with ok:false: the
// check ran, and what it found is the result.
func (s *Server) handleCheckDraftRemote(w http.ResponseWriter, r *http.Request) {
	// The same body a create takes, because the point is to test exactly what
	// is about to be stored -- including a secret key the database has never
	// seen.
	var in remoteInput
	if !decode(w, r, &in) {
		return
	}
	row := &store.BackupRemote{Enabled: true, Name: "draft"}
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		row.Name = strings.ToLower(strings.TrimSpace(*in.Name))
	}
	in.Name = nil
	if errs := in.apply(row); len(errs) > 0 {
		unprocessable(w, "this backup remote cannot be tested as described", errs)
		return
	}

	draft := config.BackupRemote{
		Name: row.Name, Endpoint: row.Endpoint, Region: row.Region, Bucket: row.Bucket,
		Prefix: row.Prefix, AccessKeyID: row.AccessKeyID, PathStyle: row.PathStyle,
	}
	switch {
	case in.SecretKey != nil && strings.TrimSpace(*in.SecretKey) != "":
		draft.SecretAccessKey = *in.SecretKey
	default:
		// Testing a saved destination after changing only its bucket should
		// not mean retyping the secret, so the stored one is used when the
		// body carries none.
		stored, err := s.ctrl.Store().GetBackupRemoteByName(r.Context(), row.Name)
		if err == nil && len(stored.SecretKeyEnc) > 0 && s.key != nil {
			if secret, err := s.key.OpenString(stored.SecretKeyEnc); err == nil {
				draft.SecretAccessKey = secret
			}
		}
	}

	out := remoteCheckResponse{Name: draft.Name, Where: draft.Where(), CheckedAt: s.ctrl.Now()}
	if err := s.ctrl.CheckBackupRemote(r.Context(), draft); err != nil {
		out.Error = err.Error()
	} else {
		out.OK = true
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "backup.remote_check", "backup_remote", draft.Name, map[string]any{
		"ok": out.OK, "where": out.Where, "saved": false,
	})
	writeJSON(w, http.StatusOK, out)
}

// storedRemote resolves the row a route addresses, refusing the one thing that
// looks like a bug and is not: a destination the configuration file describes
// cannot be edited here, because the file is what that fleet reads.
func (s *Server) storedRemote(w http.ResponseWriter, r *http.Request) (*store.BackupRemote, bool) {
	name := chiURLParam(r, "name")
	row, err := s.ctrl.Store().GetBackupRemoteByName(r.Context(), name)
	if errors.Is(err, store.ErrNotFound) {
		for _, fromFile := range s.cfg().Backup.Remotes {
			if fromFile.Name == name {
				conflict(w, "the backup remote "+name+" is described in zoomies.yaml or the environment, so it is changed there rather than here")
				return nil, false
			}
		}
		notFound(w, "this controller has no backup remote by that name")
		return nil, false
	}
	if err != nil {
		s.internal(w, r, "reading the backup remote", err)
		return nil, false
	}
	return row, true
}

// remoteNameErrors refuses a name the file already uses or another row holds.
// The database's unique index is the real guard; this is the one that answers
// in words rather than as a conflict from a constraint.
func (s *Server) remoteNameErrors(r *http.Request, name, selfID string) []fieldError {
	for _, fromFile := range s.cfg().Backup.Remotes {
		if fromFile.Name == name {
			return []fieldError{{"name",
				"zoomies.yaml or the environment already describes a destination called " + name + ", and the file has the last word. Choose another name."}}
		}
	}
	existing, err := s.ctrl.Store().GetBackupRemoteByName(r.Context(), name)
	if err == nil && existing.ID != selfID {
		return []fieldError{{"name", "another backup remote is already called " + name}}
	}
	return nil
}

// sealRemoteSecrets writes the two secrets, sealed, when the body carried
// them. Absent leaves what is stored; an explicit empty string clears it.
func (s *Server) sealRemoteSecrets(w http.ResponseWriter, r *http.Request, row *store.BackupRemote, in *remoteInput) bool {
	if in.SecretKey == nil && in.Passphrase == nil {
		return true
	}
	secret, passphrase := row.SecretKeyEnc, row.PassphraseEnc
	seal := func(value string) ([]byte, bool) {
		if strings.TrimSpace(value) == "" {
			return nil, true
		}
		sealed, err := s.key.SealString(value)
		if err != nil {
			s.internal(w, r, "sealing the backup remote's secret", err)
			return nil, false
		}
		return sealed, true
	}
	if in.SecretKey != nil {
		var ok bool
		if secret, ok = seal(*in.SecretKey); !ok {
			return false
		}
	}
	if in.Passphrase != nil {
		var ok bool
		if passphrase, ok = seal(*in.Passphrase); !ok {
			return false
		}
	}
	if err := s.ctrl.Store().SetBackupRemoteSecrets(r.Context(), row.ID, secret, passphrase); err != nil {
		s.internal(w, r, "storing the backup remote's secret", err)
		return false
	}
	row.SecretKeyEnc, row.PassphraseEnc = secret, passphrase
	return true
}

// remoteStatus is how a destination reads back after it is written: the same
// view the Backups page lists, so a form submit and a page load agree.
func (s *Server) remoteStatus(r *http.Request, name string) controller.RemoteBackupStatus {
	for _, status := range s.ctrl.BackupRemotes(r.Context()) {
		if status.Name == name {
			return status
		}
	}
	// It was written a moment ago, so not finding it means the list and the
	// row disagree -- a bug, not a shape the page should have to handle. The
	// answer still says what it is rather than leaving the source blank.
	return controller.RemoteBackupStatus{Name: name, Source: controller.RemoteSourceDatabase}
}

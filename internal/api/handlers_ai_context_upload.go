package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/aicontext"
	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// maxConcurrentUploads is how many snapshots the controller takes at once. Each
// one that is admitted holds up to aicontext.MaxSnapshotBytes while it arrives.
const maxConcurrentUploads = 4

// handleAIContextUpload receives a Zoomies-only snapshot from the managed
// workflow. It has no session and no API token: the only credential is the
// run's GitHub Actions OIDC token, minted for this controller's upload address,
// which the controller checks before it reads anything else the request says.
// It is mounted outside the API's body limit because a snapshot can be up to
// aicontext.MaxSnapshotBytes; this handler bounds the body itself.
func (s *Server) handleAIContextUpload(w http.ResponseWriter, r *http.Request) {
	audience := aicontext.UploadURLFor(s.cfg().Server.ExternalURL)
	if audience == "" {
		notFound(w, "this controller accepts Zoomies-only uploads only when server.external_url is an https address GitHub's runners can reach")
		return
	}
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || raw == "" {
		unauthorized(w, "send the workflow run's GitHub Actions OIDC token, minted for this upload address, as a bearer token")
		return
	}
	// The token, and the repository it names, first: nobody without a token for
	// a repository this controller serves gets it to buffer a body. A token only
	// proves GitHub issued it, and a workflow in any repository can have GitHub
	// issue one for this address.
	if err := s.ctrl.CheckAIContextUploadToken(r.Context(), raw, audience); err != nil {
		s.failUpload(w, r, "verifying the upload's GitHub Actions token", err)
		return
	}
	// Each admitted upload holds up to 32 MiB while it arrives, so how many at
	// once is bounded, and one too many is told to come back rather than queued
	// behind a body that may never finish.
	select {
	case s.uploads <- struct{}{}:
		defer func() { <-s.uploads }()
	default:
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusServiceUnavailable, errorEnvelope{Error: errorBody{
			Code:    codeInternal,
			Message: "this controller is already receiving other Zoomies-only snapshots; run the workflow again in a minute",
		}})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, aicontext.MaxSnapshotBytes+1))
	if err != nil {
		badRequest(w, "the upload body could not be read")
		return
	}
	if len(body) > aicontext.MaxSnapshotBytes {
		payloadTooLarge(w, "the snapshot is larger than 32 MiB; add exclusions to the repository's AI Context configuration")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	out, err := s.ctrl.IngestAIContextUpload(ctx, raw, audience, body)
	if err != nil {
		s.failUpload(w, r, "verifying the uploaded AI context", err)
		return
	}
	var commit, snapshot string
	if out.Freshness != nil {
		commit, snapshot = out.Freshness.PublishedCommit, out.Freshness.Digest
	}
	s.auth.Auditor().Act(r.Context(), &auth.Identity{Kind: auth.KindSystem, Name: "github-actions"}, "context.upload", "ai_context", out.ID,
		map[string]any{"commit": commit, "snapshot_id": snapshot})
	writeJSON(w, http.StatusAccepted, map[string]any{"repository_id": out.ID, "commit": commit, "snapshot_id": snapshot})
}

// failUpload answers an upload that did not get in, with the status its reason
// deserves: both halves of the check use it, the one before the body is read and
// the one that guards the write.
func (s *Server) failUpload(w http.ResponseWriter, r *http.Request, what string, err error) {
	switch {
	case errors.Is(err, github.ErrActionsKeysUnavailable):
		actionsKeysUnavailable(w, err)
	case errors.Is(err, github.ErrActionsToken):
		unauthorized(w, "the GitHub Actions OIDC token is not valid for this controller: check that the workflow requests it for this upload address and that it has not expired")
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "no repository on this controller is set up for Zoomies-only output with this GitHub repository ID")
	case errors.Is(err, controller.ErrAIContextUploadSuperseded):
		conflict(w, err.Error())
	case errors.Is(err, controller.ErrAIContextUploadRefused):
		forbidden(w, err.Error())
	default:
		s.fail(w, r, what, err)
	}
}

// actionsKeysUnavailable is a 503 rather than a 401: the token was not judged,
// and the run's log should send the operator to this controller's network.
func actionsKeysUnavailable(w http.ResponseWriter, err error) {
	host := "token.actions.githubusercontent.com"
	var keys *github.ActionsKeysError
	if errors.As(err, &keys) {
		host = keys.Host
	}
	writeError(w, http.StatusServiceUnavailable, errorEnvelope{Error: errorBody{
		Code:    codeInternal,
		Message: "this controller could not fetch GitHub's Actions signing keys from " + host + ", so the upload's token could not be checked; allow outbound https to that host and re-run the workflow",
	}})
}

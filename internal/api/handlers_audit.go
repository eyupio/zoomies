package api

import (
	"net/http"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/store"
)

// auditReadActions names, for each audited target kind, the action that lets a
// caller read that kind directly.
//
// Only the kinds an ordinary viewer cannot reach are listed. Pools, hosts,
// runners, installations and webhook deliveries are all readable at viewer, so
// their rows say nothing the caller could not fetch from the resource itself.
var auditReadActions = map[string]auth.Action{
	"user":       auth.ActionUsersRead,
	"api_token":  auth.ActionTokensRead,
	"join_token": auth.ActionJoinsRead,
	"settings":   auth.ActionSettingsRead,
}

// handleListAudit answers GET /api/v1/audit.
//
// Two separate things keep a secret out of the response, and they are worth
// telling apart.
//
// A credential is gone before the row is ever written -- auth.Redact on the way
// in, plus the callers that strip what a name-based redaction cannot see. That
// is the half that has to happen at write time, because a row is kept for the
// life of the database and read by every viewer from then on.
//
// The other half is this one, and it is about reach rather than secrecy.
// audit.read is a viewer action while users.read, tokens.read, joins.read and
// settings.read are all admin ones, so returning every document as stored made
// the audit log a way around that: a viewer who could not call GET /users could
// read a user's email, display name and OIDC subject out of the row recording
// the change to it. Those documents are withheld from a caller who could not
// have fetched them directly. The row itself stays -- who did what to which
// thing and when is the part a viewer is meant to see.
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	filter := store.AuditFilter{
		ActorIDs:    queryList(r, "actor_id"),
		Actions:     queryList(r, "action"),
		TargetKinds: queryList(r, "target_kind"),
		TargetID:    r.URL.Query().Get("target_id"),
		Search:      r.URL.Query().Get("q"),
	}
	var err error
	if filter.Since, err = queryTime(r, "since"); err != nil {
		badRequestField(w, "since", err.Error())
		return
	}
	if filter.Until, err = queryTime(r, "until"); err != nil {
		badRequestField(w, "until", err.Error())
		return
	}

	p := parsePage(r)
	events, total, err := s.ctrl.Store().ListAudit(r.Context(), filter, p)
	if err != nil {
		s.internal(w, r, "reading the audit log", err)
		return
	}
	id := Identity(r.Context())
	for i := range events {
		action, guarded := auditReadActions[events[i].TargetKind]
		if guarded && !auth.Allowed(id, action) {
			events[i].Before = ""
			events[i].After = ""
		}
	}
	writeJSON(w, http.StatusOK, newPage(events, total, p))
}

// handleAuditActions lists the distinct action names, for the filter menu.
func (s *Server) handleAuditActions(w http.ResponseWriter, r *http.Request) {
	actions, err := s.ctrl.Store().AuditActions(r.Context())
	if err != nil {
		s.internal(w, r, "reading the audit log's action names", err)
		return
	}
	writeJSON(w, http.StatusOK, newList(actions))
}

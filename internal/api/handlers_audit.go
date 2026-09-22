package api

import (
	"encoding/json"
	"net/http"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/config"
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
	// Providers and machines are readable at viewer like pools are, so these
	// two rows withhold nothing from a person. They are here for the other
	// gate: a token narrowed to a few scopes is refused the documents for a
	// resource it was not given, and a token that cannot read providers
	// should not read a provider's whole configuration out of the row
	// recording a change to it.
	"provider": auth.ActionProvidersRead,
	"machine":  auth.ActionMachinesRead,
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
		row := auditRowFor(*events[i], id)
		events[i] = &row
	}
	writeJSON(w, http.StatusOK, newPage(events, total, p))
}

// auditRowFor is an audit row as this caller may read it. The list and the
// event stream both answer through it, because the stream carries every new
// row as it is written and a filter applied only to the list was no filter.
//
// The documents are withheld by what the row is about; the address is
// withheld by rank, in the same pass. An audit row's ip says where a
// colleague was working from, which is a fact about a person rather than
// about the fleet -- an operator who can read that their administrator
// signed in from a hotel has learned something the audit log exists to
// record, not to publish. Administrators keep it, because chasing a
// suspicious sign-in is the reason the column is there.
//
// A settings row is narrowed once more for anybody but the platform: the
// settings page does not show an administrator the platform's keys, so the
// row recording a change to backup.directory must not either.
func auditRowFor(ev store.AuditEvent, id *auth.Identity) store.AuditEvent {
	action, guarded := auditReadActions[ev.TargetKind]
	if guarded && !auth.Allowed(id, action) {
		ev.Before, ev.After = "", ""
	}
	if ev.TargetKind == "settings" && (id == nil || !id.Role.AtLeast(store.RolePlatform)) {
		ev.Before, ev.After = withoutPlatformKeys(ev.Before), withoutPlatformKeys(ev.After)
	}
	if id == nil || !id.Role.AtLeast(store.RoleAdmin) {
		ev.IP = ""
	}
	return ev
}

// withoutPlatformKeys drops the platform's settings from a settings row's
// document. A document it cannot read is dropped whole: failing open here
// would be the leak this exists to close.
func withoutPlatformKeys(doc string) string {
	if doc == "" {
		return ""
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(doc), &keys); err != nil {
		return ""
	}
	for k := range keys {
		if st, ok := config.LookupSetting(k); ok && platformsOwn(st) {
			delete(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	out, err := json.Marshal(keys)
	if err != nil {
		return ""
	}
	return string(out)
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

package api

import (
	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/events"
)

// frameReads is, for every kind of frame the bus carries, the action whose GET
// route serves the same thing. A stream sends a frame only to a caller who
// could have fetched it.
//
// Before this, holding events.read was enough for every frame, and a few kinds
// were narrowed by role afterwards. That let a token scoped to events:read
// alone receive the audit log one row at a time, and it would have handed the
// same rows to a public view's visitors, who are refused GET /audit. The rule
// now has one shape: a frame is the resource's GET in miniature, so it asks the
// GET's question.
var frameReads = map[events.Kind]auth.Action{
	events.KindRunnerCreated:       auth.ActionRunnersRead,
	events.KindRunnerUpdated:       auth.ActionRunnersRead,
	events.KindRunnerDeleted:       auth.ActionRunnersRead,
	events.KindPoolCreated:         auth.ActionPoolsRead,
	events.KindPoolUpdated:         auth.ActionPoolsRead,
	events.KindPoolDeleted:         auth.ActionPoolsRead,
	events.KindJobUpdated:          auth.ActionJobsRead,
	events.KindHostUpdated:         auth.ActionHostsRead,
	events.KindHostDeleted:         auth.ActionHostsRead,
	events.KindScaling:             auth.ActionStatsRead,
	events.KindInstallation:        auth.ActionInstallationsRead,
	events.KindInstallationDeleted: auth.ActionInstallationsRead,
	events.KindProviderUpdated:     auth.ActionProvidersRead,
	events.KindProviderDeleted:     auth.ActionProvidersRead,
	events.KindMachineUpdated:      auth.ActionMachinesRead,
	events.KindMachineDeleted:      auth.ActionMachinesRead,
	events.KindKennelUpdated:       auth.ActionKennelRead,
	events.KindKennelDeleted:       auth.ActionKennelRead,
	events.KindKennelSummary:       auth.ActionKennelRead,
	events.KindProblems:            auth.ActionStatsRead,
	events.KindStats:               auth.ActionStatsRead,
	events.KindAudit:               auth.ActionAuditRead,
	events.KindWebhook:             auth.ActionWebhooksRead,
	events.KindUpdates:             auth.ActionUpdatesRead,
	events.KindTransfer:            auth.ActionBackupsRead,
	events.KindControllerMachine:   auth.ActionHostsRead,
}

// framesForEveryone are the kinds that describe the stream rather than the
// fleet: a keep-alive, and "what you hold may be stale".
var framesForEveryone = map[events.Kind]bool{
	events.KindHeartbeat: true,
	events.KindResync:    true,
}

// frameReadable reports whether id may be sent a frame of kind k. A kind
// nobody mapped is withheld: a new kind of frame reaches nobody until somebody
// decides who may read it, rather than everybody until somebody notices.
func frameReadable(id *auth.Identity, k events.Kind) bool {
	if framesForEveryone[k] {
		return true
	}
	a, ok := frameReads[k]
	return ok && auth.Allowed(id, a)
}

// visitorStreams is the event bus class a public view's visitors' streams are
// opened in, so limits.visitor_event_subscribers counts them and
// limits.event_subscribers does not.
const visitorStreams = "visitor"

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/store"
)

// The event stream carries every audit row as it is written, and GET /audit
// filters what a caller may read of one. The stream did not: a viewer with the
// Overview open was sent the whole document of a settings change, backup
// directory and all, the moment the platform made it. The browser-level
// audience test found it.
func TestAnAuditFrameIsFilteredAsTheAuditLogIs(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("watcher", store.RoleViewer)
	admin, _ := h.user("fleet-admin", store.RoleAdmin)
	plat, _ := h.user("operator-of-record", store.RolePlatform)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	asViewer, _ := h.openStream(t, ctx, "/api/v1/events", h.session(viewer), nil)
	asAdmin, _ := h.openStream(t, ctx, "/api/v1/events", h.session(admin), nil)
	asPlatform, _ := h.openStream(t, ctx, "/api/v1/events", h.session(plat), nil)
	for _, frames := range []<-chan sseFrame{asViewer, asAdmin, asPlatform} {
		await(t, frames, "the opening comment", func(f sseFrame) bool { return f.comment != "" })
	}

	const where = "/srv/the-platforms-backups"
	h.do(request{
		method: http.MethodPatch, path: "/api/v1/settings", cookie: h.session(plat),
		body: map[string]any{"backup.directory": where, "scheduler.interval": "12s"},
	}).mustStatus(t, http.StatusOK, "change a platform and a fleet setting together")

	isSettings := func(f sseFrame) bool {
		return f.event == string(events.KindAudit) && strings.Contains(f.data, `"target_kind":"settings"`)
	}
	if f := await(t, asViewer, "the viewer's audit frame", isSettings); strings.Contains(f.data, where) || strings.Contains(f.data, "12s") {
		t.Errorf("a viewer's audit frame carries the settings document: %s", f.data)
	}
	f := await(t, asAdmin, "the administrator's audit frame", isSettings)
	if strings.Contains(f.data, where) {
		t.Errorf("an administrator's audit frame carries the platform's backup directory: %s", f.data)
	}
	if !strings.Contains(f.data, "12s") {
		t.Errorf("an administrator's audit frame lost the fleet's own change: %s", f.data)
	}
	if f := await(t, asPlatform, "the platform's audit frame", isSettings); !strings.Contains(f.data, where) {
		t.Errorf("the platform's audit frame is missing its own change: %s", f.data)
	}

	// And the list says the same thing to the same people.
	listed := h.do(request{method: http.MethodGet, path: "/api/v1/audit", cookie: h.session(admin)})
	listed.mustStatus(t, http.StatusOK, "list the audit log as admin")
	if strings.Contains(string(listed.body), where) {
		t.Errorf("an administrator's audit log carries the platform's backup directory")
	}
}

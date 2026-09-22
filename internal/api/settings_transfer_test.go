package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// An export is the settings somebody chose, in a file that can be started
// from; it never carries a default, because those are computed on the host
// that reads them, and never a secret's value.
func TestAnExportCarriesWhatWasSetAndNeverASecret(t *testing.T) {
	h := newHarness(t)
	// Platform: this exercises keys on both sides of the audience split,
	// and its subject is the behaviour, not who may reach it.
	admin, _ := h.user("admin", store.RolePlatform)
	cookie := h.session(admin)
	patched := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: cookie,
		body: map[string]any{"retention.jobs": "96h", "capacity_demand.signing_secret": "hush-now-secret"}})
	patched.mustStatus(t, http.StatusOK, "patch")

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/settings/export", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "export")
	if cd := resp.header.Get("Content-Disposition"); !strings.Contains(cd, "zoomies-settings-") || !strings.HasSuffix(cd, `.json"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	var doc settingsExport
	resp.into(t, &doc)
	if doc.ExportVersion != exportVersion || doc.ExportedAt.IsZero() || doc.Version == "" {
		t.Errorf("the document does not say what it is: %+v", doc)
	}
	retention, _ := doc.Settings["retention"].(map[string]any)
	if retention["jobs"] != "96h" {
		t.Errorf("the changed setting is not in the export: %+v", doc.Settings)
	}
	if _, ok := retention["runners"]; ok {
		t.Error("a default is in the export")
	}
	if strings.Contains(string(resp.body), "hush-now-secret") {
		t.Fatal("the secret's value is in the export")
	}
	if len(doc.SecretsConfigured) != 1 || doc.SecretsConfigured[0] != "capacity_demand.signing_secret" {
		t.Errorf("secrets_configured = %v", doc.SecretsConfigured)
	}
	assertShape(t, loadSpec(t), "SettingsExport", resp.body)

	// The YAML form is the tree alone, with the facts as a comment, and the
	// harness's test configuration -- set in the struct, so read as a file
	// would be -- is in it too.
	y := h.do(request{method: http.MethodGet, path: "/api/v1/settings/export?format=yaml", cookie: cookie})
	y.mustStatus(t, http.StatusOK, "yaml export")
	if !strings.HasPrefix(string(y.body), "# Zoomies settings") || !strings.Contains(string(y.body), "jobs: 96h") {
		t.Errorf("yaml export:\n%s", y.body)
	}
	if strings.Contains(string(y.body), "hush-now-secret") || !strings.Contains(string(y.body), "capacity_demand.signing_secret") {
		t.Error("the yaml export carries the secret, or does not name it as work to do")
	}
	bad := h.do(request{method: http.MethodGet, path: "/api/v1/settings/export?format=toml", cookie: cookie})
	bad.mustStatus(t, http.StatusBadRequest, "an export format that is not one")
}

// The preview is the whole point of the import: it says what would happen,
// key by key, and writes nothing. Applying is then one change or none.
func TestAnImportIsPreviewedThenAppliedAsOneChange(t *testing.T) {
	h := newHarness(t)
	// Platform: this exercises keys on both sides of the audience split,
	// and its subject is the behaviour, not who may reach it.
	admin, _ := h.user("admin", store.RolePlatform)
	cookie := h.session(admin)
	before := h.ctrl.Config().Retention.Jobs

	document := "retention:\n  jobs: 48h\n  runners: 168h\nlog:\n  level: chatty\nnonsense:\n  key: 1\n"
	preview := h.do(request{method: http.MethodPost, path: "/api/v1/settings/import", cookie: cookie,
		body: map[string]any{"document": document, "dry_run": true}})
	preview.mustStatus(t, http.StatusOK, "dry run")
	assertShape(t, loadSpec(t), "SettingsImport", preview.body)
	var out importResponse
	preview.into(t, &out)
	if out.Applied {
		t.Fatal("a dry run claims to have applied")
	}
	if h.ctrl.Config().Retention.Jobs != before {
		t.Fatal("a dry run changed the running configuration")
	}
	actions := map[string]string{}
	reasons := map[string]string{}
	for _, ch := range out.Changes {
		actions[ch.Key] = ch.Action
		reasons[ch.Key] = ch.Reason
	}
	if actions["retention.jobs"] != importChangeAction || actions["retention.runners"] != importUnchangedAction {
		t.Errorf("actions = %v", actions)
	}
	if actions["log.level"] != importRefusedAction || !strings.Contains(reasons["log.level"], "not a log level") {
		t.Errorf("a bad value was not refused with the reason: %v %v", actions, reasons)
	}
	if actions["nonsense.key"] != importRefusedAction {
		t.Errorf("an unknown key was not refused: %v", actions)
	}
	if out.Summary.Change != 1 || out.Summary.Unchanged != 1 || out.Summary.Refused != 2 {
		t.Errorf("summary = %+v", out.Summary)
	}

	// Applying with a refusal in it is refused as a whole.
	refused := h.do(request{method: http.MethodPost, path: "/api/v1/settings/import", cookie: cookie,
		body: map[string]any{"document": document}})
	refused.mustStatus(t, http.StatusUnprocessableEntity, "apply with refused keys")
	if h.ctrl.Config().Retention.Jobs != before {
		t.Fatal("a refused import changed the running configuration")
	}

	// Skipping them, it goes through -- and only the changed key is written.
	applied := h.do(request{method: http.MethodPost, path: "/api/v1/settings/import", cookie: cookie,
		body: map[string]any{"document": document, "skip": []string{"log.level", "nonsense.key"}}})
	applied.mustStatus(t, http.StatusOK, "apply")
	applied.into(t, &out)
	if !out.Applied || out.Settings == nil || out.Summary.Skipped != 2 {
		t.Errorf("applied = %+v", out.Summary)
	}
	if got := h.ctrl.Config().Retention.Jobs.String(); got != "48h0m0s" {
		t.Errorf("retention.jobs = %s after the import", got)
	}
	rows, _ := h.st.ListInstanceSettings(h.ctx)
	for _, row := range rows {
		if row.Key == "retention.runners" {
			t.Error("an unchanged key was written as if it had changed")
		}
	}
	audit := h.do(request{method: http.MethodGet, path: "/api/v1/audit?action=settings.import", cookie: cookie})
	if !strings.Contains(string(audit.body), `"settings.import"`) {
		t.Error("the import was not audited")
	}
}

// An export round-trips: the JSON document an instance produced is a document
// the same route reads, with its secrets named as work to do by hand.
func TestAnExportImportsBackIncludingTheSecretsItCouldNotCarry(t *testing.T) {
	h := newHarness(t)
	// Platform: this exercises keys on both sides of the audience split,
	// and its subject is the behaviour, not who may reach it.
	admin, _ := h.user("admin", store.RolePlatform)
	cookie := h.session(admin)
	h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: cookie,
		body: map[string]any{"retention.jobs": "96h", "capacity_demand.signing_secret": "hush"}}).mustStatus(t, http.StatusOK, "patch")
	exported := h.do(request{method: http.MethodGet, path: "/api/v1/settings/export", cookie: cookie})
	exported.mustStatus(t, http.StatusOK, "export")

	// Put the setting back first, so the import has something to change.
	h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: cookie,
		body: map[string]any{"retention.jobs": nil}}).mustStatus(t, http.StatusOK, "reset")

	preview := h.do(request{method: http.MethodPost, path: "/api/v1/settings/import", cookie: cookie,
		body: map[string]any{"document": string(exported.body), "dry_run": true}})
	preview.mustStatus(t, http.StatusOK, "dry run of an export")
	var out importResponse
	preview.into(t, &out)
	var found bool
	for _, ch := range out.Changes {
		if ch.Key == "retention.jobs" && ch.Action == importChangeAction {
			found = true
		}
	}
	if !found || len(out.SecretsConfigured) != 1 {
		t.Errorf("preview = %+v", out)
	}

	// A document from the future is refused with a sentence, and rubbish is
	// refused as rubbish.
	future := h.do(request{method: http.MethodPost, path: "/api/v1/settings/import", cookie: cookie,
		body: map[string]any{"document": `{"export_version": 99, "settings": {}}`, "dry_run": true}})
	future.mustStatus(t, http.StatusUnprocessableEntity, "a newer export")
	if !strings.Contains(string(future.body), "version 99") {
		t.Errorf("the refusal does not name the version: %s", future.body)
	}
	garbage := h.do(request{method: http.MethodPost, path: "/api/v1/settings/import", cookie: cookie,
		body: map[string]any{"document": "{[not yaml", "dry_run": true}})
	garbage.mustStatus(t, http.StatusUnprocessableEntity, "not a document")
	empty := h.do(request{method: http.MethodPost, path: "/api/v1/settings/import", cookie: cookie,
		body: map[string]any{"document": "   "}})
	empty.mustStatus(t, http.StatusUnprocessableEntity, "an empty document")
}

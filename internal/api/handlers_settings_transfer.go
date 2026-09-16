package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/version"
)

// Moving a configuration between instances, or keeping a record of one.
//
// An export is every setting somebody has set -- not the defaults, which are
// computed on the host that reads them, and never a secret's value -- shaped
// like zoomies.yaml, so the file an operator downloads is a file the next
// controller could be started with. An import is the same document read back,
// planned key by key through the same checks a PATCH goes through, and shown
// as a preview before anything is written: which keys would change, which are
// already so, and which this instance refuses and why.

// exportVersion is the export document's own shape number. It moves when a
// field is removed or renamed, not when one is added.
const exportVersion = 1

// settingsExport is the JSON form of an export. The YAML form is the
// `settings` tree alone, with these facts as a comment above it.
type settingsExport struct {
	ExportVersion int       `json:"export_version"`
	ExportedAt    time.Time `json:"exported_at"`
	// ExportedFrom names the instance, by the address it answers on, so a
	// file found in a directory a year later says where it came from.
	ExportedFrom string `json:"exported_from,omitempty"`
	Version      string `json:"version"`
	// Settings is the nested tree, shaped like the file.
	Settings map[string]any `json:"settings"`
	// SecretsConfigured names the credentials that were set on the source
	// and are not in this document. An import shows them as work to do by
	// hand, because a document that could carry them would be a credential.
	SecretsConfigured []string `json:"secrets_configured"`
}

// handleExportSettings answers GET /api/v1/settings/export.
func (s *Server) handleExportSettings(w http.ResponseWriter, r *http.Request) {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "yaml" {
		badRequestField(w, "format", "the export is written as json or yaml")
		return
	}
	doc := s.exportSettings()
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "settings.export", "settings", "settings", map[string]any{
		"format": format, "keys": countKeys(doc.Settings),
	})

	stamp := doc.ExportedAt.Format("20060102-150405")
	w.Header().Set("Cache-Control", "no-store")
	if format == "yaml" {
		body, err := yaml.Marshal(doc.Settings)
		if err != nil {
			s.internal(w, r, "rendering the export", err)
			return
		}
		header := fmt.Sprintf("# Zoomies settings, exported %s from %s (Zoomies %s).\n"+
			"# Every setting somebody has set on that instance; defaults are left out. No secret is in here:\n"+
			"# %s\n"+
			"# This file is the shape zoomies.yaml takes, and can be imported on the Settings page.\n",
			doc.ExportedAt.Format(time.RFC3339), orPhrase(doc.ExportedFrom, "an unnamed instance"), doc.Version,
			secretsLine(doc.SecretsConfigured))
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="zoomies-settings-`+stamp+`.yaml"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(header))
		_, _ = w.Write(body)
		return
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		s.internal(w, r, "rendering the export", err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="zoomies-settings-`+stamp+`.json"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}

// exportSettings gathers every instance setting whose value somebody chose:
// stored here, set in the file, or pinned by the environment. Bootstrap and
// local settings are left out because they belong to a host rather than to a
// fleet, and a secret contributes its name to the list rather than its value.
func (s *Server) exportSettings() settingsExport {
	c := s.cfg()
	doc := settingsExport{
		ExportVersion:     exportVersion,
		ExportedAt:        s.ctrl.Now(),
		ExportedFrom:      c.Server.ExternalURL,
		Version:           version.Short(),
		Settings:          map[string]any{},
		SecretsConfigured: []string{},
	}
	for _, st := range config.StoredSettings() {
		if c.Source(st.Key) == config.SourceDefault {
			continue
		}
		v, err := c.Value(st.Key)
		if err != nil {
			continue
		}
		if st.Secret {
			if config.Text(st, v) != "" {
				doc.SecretsConfigured = append(doc.SecretsConfigured, st.Key)
			}
			continue
		}
		config.SetNested(doc.Settings, st.Key, v)
	}
	return doc
}

func secretsLine(keys []string) string {
	if len(keys) == 0 {
		return "no secret was set on the source."
	}
	return "set these by hand after importing: " + strings.Join(keys, ", ") + "."
}

func orPhrase(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func countKeys(tree map[string]any) int {
	flat := map[string]any{}
	flatten("", tree, flat)
	return len(flat)
}

// ---------------------------------------------------------------------------
// Import
// ---------------------------------------------------------------------------

// importRequest is what the page sends: the file's text, whether this is only
// a look, and the keys to leave out.
type importRequest struct {
	// Document is the export, or a zoomies.yaml, as text. YAML, of which JSON
	// is a subset, so either file works.
	Document string `json:"document"`
	// DryRun plans and reports without writing anything.
	DryRun bool `json:"dry_run"`
	// Skip names keys in the document to ignore -- the ones the preview
	// refused, or the ones the operator unticked.
	Skip []string `json:"skip"`
}

// importChange is one key's fate.
type importChange struct {
	Key     string `json:"key"`
	Label   string `json:"label,omitempty"`
	Section string `json:"section,omitempty"`
	// Current is the running value and Incoming the document's, both as the
	// settings page renders them; a secret's are never here.
	Current  any `json:"current"`
	Incoming any `json:"incoming"`
	// Action is what applying would do: change, unchanged, unset, or
	// refused. Live says a change is in force at once; false means it waits
	// for a restart.
	Action string `json:"action"`
	Live   bool   `json:"live"`
	Secret bool   `json:"secret"`
	// Reason says why it is refused, in a sentence.
	Reason string `json:"reason,omitempty"`
}

// The actions a planned key can have.
const (
	importChangeAction    = "change"
	importUnchangedAction = "unchanged"
	importUnsetAction     = "unset"
	importRefusedAction   = "refused"
)

// importResponse is the preview, or the result.
type importResponse struct {
	Applied bool           `json:"applied"`
	Changes []importChange `json:"changes"`
	Summary importSummary  `json:"summary"`
	// SecretsConfigured is carried through from an export document, so the
	// preview can say which credentials the operator has to set by hand.
	SecretsConfigured []string `json:"secrets_configured"`
	// Settings is the page after applying, so the client can repaint without
	// a second request. Absent on a dry run.
	Settings *settingsResponse `json:"settings,omitempty"`
}

type importSummary struct {
	Change    int `json:"change"`
	Unchanged int `json:"unchanged"`
	Unset     int `json:"unset"`
	Refused   int `json:"refused"`
	Skipped   int `json:"skipped"`
}

// handleImportSettings answers POST /api/v1/settings/import.
//
// A dry run plans and reports; a real run refuses the whole document if any
// key in it is refused, exactly as a PATCH does, so that an import is one
// change rather than the keys that happened to parse. The page sends the
// refused keys back in `skip` when the operator has decided to go on without
// them.
func (s *Server) handleImportSettings(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Document) == "" {
		unprocessable(w, "there is nothing to import", []fieldError{{"document", "paste or upload an export, or a zoomies.yaml"}})
		return
	}
	tree, secrets, err := parseImport(req.Document)
	if err != nil {
		unprocessable(w, "that is not a settings document", []fieldError{{"document", err.Error()}})
		return
	}
	flat := map[string]any{}
	flatten("", tree, flat)
	skipped := 0
	for _, key := range req.Skip {
		if _, ok := flat[key]; ok {
			delete(flat, key)
			skipped++
		}
	}

	current := s.cfg()
	candidate, staged, fields := s.planSettings(current, flat)
	out := importResponse{Changes: []importChange{}, SecretsConfigured: emptySlice(secrets), Summary: importSummary{Skipped: skipped}}
	refused := map[string]string{}
	for _, f := range fields {
		refused[f.Field] = f.Message
	}
	for _, key := range sortedKeys(flat) {
		ch := importChange{Key: key}
		if st, ok := config.LookupSetting(key); ok {
			ch.Label, ch.Section, ch.Live, ch.Secret = st.Label, st.Section(), st.Live, st.Secret
			if !st.Secret {
				ch.Current, _ = current.Value(key)
			}
		}
		if reason, bad := refused[key]; bad {
			ch.Action, ch.Reason = importRefusedAction, reason
			out.Summary.Refused++
			out.Changes = append(out.Changes, ch)
			continue
		}
		idx := slices.IndexFunc(staged, func(c change) bool { return c.setting.Key == key })
		if idx < 0 {
			continue
		}
		planned := staged[idx]
		if !planned.setting.Secret {
			ch.Incoming = planned.value
		}
		switch {
		case planned.unset:
			ch.Action = importUnsetAction
			out.Summary.Unset++
		case config.Text(planned.setting, planned.before) == config.Text(planned.setting, planned.value):
			ch.Action = importUnchangedAction
			out.Summary.Unchanged++
			// Not staged: writing a row that changes nothing would still
			// stamp it as changed by this import.
			staged = slices.Delete(staged, idx, idx+1)
		default:
			ch.Action = importChangeAction
			out.Summary.Change++
		}
		out.Changes = append(out.Changes, ch)
	}
	sort.SliceStable(out.Changes, func(i, j int) bool { return config.CompareKeys(out.Changes[i].Key, out.Changes[j].Key) < 0 })

	if len(staged) > 0 {
		// The validator's verdict belongs in the preview too: an import that
		// would leave a controller that will not start is refused as a
		// whole, and the row it blames is marked so the operator can skip it.
		for _, f := range validatePlan(current, candidate, staged) {
			out.Summary.Refused++
			if f.Field == "" {
				out.Changes = append(out.Changes, importChange{Key: "", Action: importRefusedAction, Reason: f.Message})
				continue
			}
			for i := range out.Changes {
				if out.Changes[i].Key == f.Field {
					out.Changes[i].Action, out.Changes[i].Reason = importRefusedAction, f.Message
					out.Summary.Change--
				}
			}
			staged = slices.DeleteFunc(staged, func(c change) bool { return c.setting.Key == f.Field })
		}
	}

	if req.DryRun {
		writeJSON(w, http.StatusOK, out)
		return
	}
	if out.Summary.Refused > 0 {
		var fields []fieldError
		for _, ch := range out.Changes {
			if ch.Action == importRefusedAction {
				fields = append(fields, fieldError{ch.Key, ch.Reason})
			}
		}
		unprocessable(w, "some of the document was refused; skip those keys or fix the document", fields)
		return
	}
	if len(staged) > 0 {
		if err := s.applyPlan(r.Context(), Identity(r.Context()), staged); err != nil {
			s.internal(w, r, "storing the imported settings", err)
			return
		}
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "settings.import", "settings", "settings", map[string]any{
		"changed": out.Summary.Change, "unset": out.Summary.Unset, "unchanged": out.Summary.Unchanged, "skipped": skipped,
	})
	out.Applied = true
	page, err := s.settingsPage(r)
	if err != nil {
		s.internal(w, r, "reading the settings back", err)
		return
	}
	out.Settings = &page
	writeJSON(w, http.StatusOK, out)
}

// parseImport reads an export document or a zoomies.yaml and returns the
// settings tree, and the secrets an export said were configured.
//
// YAML is the parser, because JSON is YAML and a file an operator wrote by
// hand is more likely to be the former. An export is recognised by its
// version field; anything else is taken to be the tree itself.
func parseImport(text string) (map[string]any, []string, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, nil, fmt.Errorf("it does not parse as YAML or JSON: %w", err)
	}
	if doc == nil {
		return nil, nil, fmt.Errorf("it is empty")
	}
	if v, ok := doc["export_version"]; ok {
		n, isNum := v.(int)
		if !isNum || n > exportVersion {
			return nil, nil, fmt.Errorf("it is an export of version %v, and this build reads version %d", v, exportVersion)
		}
		tree, ok := doc["settings"].(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("its settings are not an object")
		}
		var secrets []string
		if raw, ok := doc["secrets_configured"].([]any); ok {
			for _, item := range raw {
				if str, ok := item.(string); ok {
					secrets = append(secrets, str)
				}
			}
		}
		return tree, secrets, nil
	}
	return doc, nil, nil
}

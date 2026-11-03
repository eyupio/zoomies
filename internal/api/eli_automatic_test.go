package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

type automaticFixture struct {
	h        *harness
	inst     *store.Installation
	provider *store.AssistantProvider
	admin    string
	adminID  string
	auto     string
	asks     string
}

// newAutomaticFixture is an installation with one repository that repairs
// itself, one that asks first, and an installation provider to pay for the first.
func newAutomaticFixture(t *testing.T) automaticFixture {
	t.Helper()
	h := newHarness(t)
	inst := h.installation()
	user, admin := h.user("automatic-admin", store.RoleAdmin)
	provider := &store.AssistantProvider{Name: "Team provider", Kind: "openai", Model: "team-model", Enabled: true}
	if err := h.st.CreateAssistantProvider(h.ctx, provider); err != nil {
		t.Fatal(err)
	}
	f := automaticFixture{h: h, inst: inst, provider: provider, admin: admin, adminID: user.ID, auto: inst.Target + "/auto", asks: inst.Target + "/asks"}
	for _, p := range []store.EliRepairPolicy{
		{Repo: f.auto, InstallationID: inst.ID, ProviderID: provider.ID, Enabled: true, Automatic: true},
		{Repo: f.asks, InstallationID: inst.ID, Enabled: true, AskFirst: true},
	} {
		if err := h.st.SetEliRepairPolicy(h.ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f automaticFixture) stop(cookie string, body any) *response {
	f.h.t.Helper()
	return f.h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/automatic/stop", cookie: cookie, body: body})
}

func (f automaticFixture) policy(t *testing.T, repo string) store.EliRepairPolicy {
	t.Helper()
	policies, err := f.h.st.EliRepairPolicies(f.h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range policies {
		if p.Repo == repo {
			return p
		}
	}
	t.Fatalf("no policy for %s", repo)
	return store.EliRepairPolicy{}
}

// The overview names providers and counts what has been spent. It is an
// administrator's page, and the generated client reads it from the document.
func TestTheAutomaticOverviewIsAnAdministratorRouteAndTakesTheSpecsShape(t *testing.T) {
	f := newAutomaticFixture(t)
	// An attempt, so the latest repair is there to be checked against the shape.
	if _, err := f.h.st.EnqueueEliRepair(f.h.ctx, &store.EliRepair{DedupKey: "automatic:" + f.auto + ":abc", Repo: f.auto, InstallationID: f.inst.ID, Trigger: "automatic", PullNumber: 3, HeadSHA: "abc"}); err != nil {
		t.Fatal(err)
	}
	_, viewer := f.h.user("automatic-viewer", store.RoleViewer)
	_, operator := f.h.user("automatic-operator", store.RoleOperator)
	for name, cookie := range map[string]string{"viewer": viewer, "operator": operator} {
		f.h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/automatic", cookie: cookie}).
			mustStatus(t, http.StatusForbidden, "a "+name+" may not read what the automation has done")
	}

	response := f.h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/automatic", cookie: f.admin})
	response.mustStatus(t, http.StatusOK, "an administrator reads the overview")
	doc := loadSpec(t)
	assertShape(t, doc, "EliAutomaticRepairs", json.RawMessage(response.body))
	var answer struct {
		Repositories []json.RawMessage `json:"repositories"`
	}
	if err := json.Unmarshal(response.body, &answer); err != nil || len(answer.Repositories) != 1 {
		t.Fatalf("expected the one automatic repository, got %s (%v)", response.body, err)
	}
	assertShape(t, doc, "EliAutomaticRepository", answer.Repositories[0])
	assertShape(t, doc, "EliAutomaticLast", lift(t, answer.Repositories[0], "last"))
	var repo struct {
		Repo         string `json:"repo"`
		ProviderName string `json:"provider_name"`
		Running      int    `json:"running"`
		Problem      string `json:"problem"`
	}
	if err := json.Unmarshal(answer.Repositories[0], &repo); err != nil {
		t.Fatal(err)
	}
	if repo.Repo != f.auto || repo.ProviderName != "Team provider" || repo.Running != 1 || repo.Problem != "" {
		t.Errorf("the overview says %+v", repo)
	}
}

func TestStoppingAutomaticRepairsAnswersEachRepositoryAndWritesAnAuditRowForEachChange(t *testing.T) {
	f := newAutomaticFixture(t)
	missing := f.inst.Target + "/missing"

	response := f.stop(f.admin, map[string]any{"repos": []string{f.auto, f.asks, missing}, "then": "ask_first"})
	response.mustStatus(t, http.StatusOK, "a stop answers for each repository")
	var answer struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(response.body, &answer); err != nil || len(answer.Results) != 3 {
		t.Fatalf("expected three results, got %s (%v)", response.body, err)
	}
	doc := loadSpec(t)
	type result struct {
		Repo    string `json:"repo"`
		Changed bool   `json:"changed"`
		Error   string `json:"error"`
	}
	var got []result
	for _, raw := range answer.Results {
		assertShape(t, doc, "EliAutomaticStopResult", raw)
		var r result
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if got[0].Repo != f.auto || !got[0].Changed || got[0].Error != "" {
		t.Errorf("the automatic repository: %+v", got[0])
	}
	if got[1].Repo != f.asks || got[1].Changed || got[1].Error != "" {
		t.Errorf("a repository that was never automatic is changed or an error: %+v", got[1])
	}
	if got[2].Repo != missing || got[2].Changed || got[2].Error == "" {
		t.Errorf("a repository with no policy: %+v", got[2])
	}
	if p := f.policy(t, f.auto); p.Automatic || !p.AskFirst || !p.Enabled || p.ProviderID != f.provider.ID {
		t.Errorf("the policy after the stop: %+v", p)
	}

	rows, _, err := f.h.st.ListAudit(f.h.ctx, store.AuditFilter{Actions: []string{"assistant.repair_automatic_stop"}}, store.Page{Limit: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("audit rows = %d (%v), want one for the one repository that changed", len(rows), err)
	}
	if a := rows[0]; a.ActorID != f.adminID || a.TargetKind != "repository" || a.TargetID != f.auto || !strings.Contains(a.After, "ask_first") {
		t.Errorf("the audit row must name who stopped which repository and to what: %+v", a)
	}

	// A second press changes nothing and leaves no second row saying it did.
	f.stop(f.admin, map[string]any{"repos": []string{f.auto}, "then": "ask_first"}).mustStatus(t, http.StatusOK, "stopping it again")
	if again, _, _ := f.h.st.ListAudit(f.h.ctx, store.AuditFilter{Actions: []string{"assistant.repair_automatic_stop"}}, store.Page{Limit: 10}); len(again) != 1 {
		t.Errorf("a stop that changed nothing wrote an audit row: %d rows", len(again))
	}
}

func TestStoppingAutomaticRepairsRefusesWhatItCannotUnderstand(t *testing.T) {
	f := newAutomaticFixture(t)
	fifty := make([]string, 51)
	for i := range fifty {
		fifty[i] = fmt.Sprintf("acme/repo-%d", i)
	}
	for name, body := range map[string]map[string]any{
		"no repositories":      {"repos": []string{}, "then": "ask_first"},
		"too many":             {"repos": fifty, "then": "ask_first"},
		"a blank name":         {"repos": []string{f.auto, "  "}, "then": "ask_first"},
		"a choice not offered": {"repos": []string{f.auto}, "then": "pause"},
		"no choice":            {"repos": []string{f.auto}},
	} {
		f.stop(f.admin, body).mustStatus(t, http.StatusUnprocessableEntity, name)
	}
	if p := f.policy(t, f.auto); !p.Automatic || !p.Enabled {
		t.Errorf("a refused stop changed the policy: %+v", p)
	}
	_, viewer := f.h.user("automatic-viewer", store.RoleViewer)
	f.stop(viewer, map[string]any{"repos": []string{f.auto}, "then": "off"}).mustStatus(t, http.StatusForbidden, "a viewer stops nothing")
	if p := f.policy(t, f.auto); !p.Automatic || !p.Enabled {
		t.Errorf("a refused caller changed the policy: %+v", p)
	}
}

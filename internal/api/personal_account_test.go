package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// A personal account's App is not an organisation's with a different name on
// it. GitHub creates it under the person's own settings, and a person has no
// organisation permissions to grant, so the manifest posted to the
// organisation's page with the organisation's runner permission creates
// nothing a personal account can use.
func TestManifestForAPersonalAccountIsCreatedUnderThePersonAndAsksForAdministration(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/installations/manifest", cookie: h.session(admin),
		body: map[string]any{"target": "octocat", "target_type": "org", "account_type": "user", "kennel_settings": true}})
	resp.mustStatus(t, http.StatusOK, "building a manifest for a personal account")
	body := resp.json(t)

	if post, _ := body["post_url"].(string); strings.Contains(post, "/organizations/") || !strings.HasSuffix(post, "/settings/apps/new") {
		t.Errorf("post_url = %q, want the person's own App page", post)
	}
	var m struct {
		Permissions map[string]string `json:"default_permissions"`
	}
	if err := json.Unmarshal([]byte(body["manifest"].(string)), &m); err != nil {
		t.Fatalf("the manifest is not JSON: %v", err)
	}
	if m.Permissions["administration"] != "write" {
		t.Errorf("administration = %q, want write: it is what registers a runner on a repository", m.Permissions["administration"])
	}
	if got, asked := m.Permissions["organization_self_hosted_runners"]; asked {
		t.Errorf("a personal account's App asks for organization_self_hosted_runners:%s, which a person cannot grant", got)
	}
}

func TestManifestRefusesAnAccountTypeItDoesNotKnow(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/installations/manifest", cookie: h.session(admin),
		body: map[string]any{"target": "octocat", "target_type": "org", "account_type": "team"}})
	resp.mustStatus(t, http.StatusUnprocessableEntity, "an unknown account type")
}

// The tab GitHub returns the operator to knows the App and the installation and
// nothing else, so what the flow was told about the account has to come from
// the handshake, the same as the target does.
func TestAPersonalAccountIsRecordedFromTheManifestFlow(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	h.api.manifests.put(&pendingApp{
		state:         store.NewSecret(16),
		target:        "octocat",
		targetType:    store.TargetOrg,
		accountType:   store.AccountUser,
		apiBaseURL:    h.cfg.GitHub.APIBaseURL,
		appID:         4242,
		slug:          "zoomies-octocat",
		pem:           testPEM,
		webhookSecret: "from-github",
		createdAt:     h.ctrl.Now(),
	})

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/installations", cookie: h.session(admin),
		body: map[string]any{"app_id": 4242, "installation_id": 99}})
	resp.mustStatus(t, http.StatusCreated, "recording a personal account from the manifest flow")
	body := resp.json(t)
	if body["target_type"] != "org" || body["account_type"] != "user" {
		t.Errorf("target_type=%v account_type=%v, want org and user", body["target_type"], body["account_type"])
	}
	// The App's settings are under the person, not under /organizations/.
	if settings, _ := body["settings_url"].(string); strings.Contains(settings, "/organizations/") {
		t.Errorf("settings_url = %q, which GitHub answers with a 404 for a person", settings)
	}
}

// Both settings promise a runner that waits for whichever job comes next. On a
// personal account every runner is registered on one repository, so neither can
// be honoured, and accepting them would be a pool that says one thing and does
// another.
func TestAPoolOnAPersonalAccountMustBeEphemeralWithNoWarmRunners(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	if err := h.st.SetInstallationAccountType(h.ctx, inst.ID, store.AccountUser); err != nil {
		t.Fatalf("SetInstallationAccountType: %v", err)
	}
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	for field, mutate := range map[string]func(map[string]any){
		"min_runners": func(b map[string]any) { b["min_runners"] = 1 },
		"ephemeral":   func(b map[string]any) { b["ephemeral"] = false },
	} {
		body := poolBody(inst.ID)
		mutate(body)
		resp := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
		resp.mustStatus(t, http.StatusUnprocessableEntity, "a pool with "+field+" set on a personal account")
		var env errorEnvelope
		resp.into(t, &env)
		found := ""
		for _, fe := range env.Errors {
			if fe.Field == field {
				found = fe.Message
			}
		}
		if !strings.Contains(found, "personal account") {
			t.Errorf("the message on %s does not say why: %q (all: %+v)", field, found, env.Errors)
		}
	}

	resp := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: poolBody(inst.ID)})
	resp.mustStatus(t, http.StatusCreated, "an ephemeral pool with no warm runners on a personal account")
	// A person has no runner groups, so the pool is not put in Zoomies' own.
	if group, _ := resp.json(t)["runner_group"].(string); group != "" {
		t.Errorf("runner_group = %q, want none", group)
	}
}

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// The listing carries each installation's pool count, because "connected to
// GitHub" and "actually making runners" are different states and the
// Installations page has to tell them apart.
func TestListInstallationsCarriesPoolCounts(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	inst := h.installation()
	h.pool(inst, "zoomies-4vcpu")

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/installations", cookie: h.session(admin)})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp.body)
	}
	var out struct {
		Items []struct {
			ID        string `json:"id"`
			Target    string `json:"target"`
			PoolCount int    `json:"pool_count"`
			Healthy   bool   `json:"healthy"`
		} `json:"items"`
	}
	resp.into(t, &out)
	if len(out.Items) != 1 {
		t.Fatalf("listed %d installations, want 1", len(out.Items))
	}
	if out.Items[0].Target != "acme" || out.Items[0].PoolCount != 1 {
		t.Fatalf("installation = %+v, want acme with one pool", out.Items[0])
	}

	// The single-installation route renders the same shape, because the event
	// stream's installation.updated frames are fed from it.
	one := h.do(request{method: http.MethodGet, path: "/api/v1/installations/" + inst.ID, cookie: h.session(admin)})
	if one.status != http.StatusOK {
		t.Fatalf("status = %d: %s", one.status, one.body)
	}
	if got := one.json(t)["pool_count"]; got != float64(1) {
		t.Fatalf("pool_count = %v, want 1", got)
	}

	missing := h.do(request{method: http.MethodGet, path: "/api/v1/installations/inst_nope", cookie: h.session(admin)})
	if missing.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", missing.status)
	}
}

// Changing credentials is the thing this route exists for, and the private key
// must be sealed on the way in -- an operator pasting a .pem into a form is
// handing over the App itself.
func TestUpdateInstallationSealsANewPrivateKey(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	inst := h.installation()

	const fresh = "-----BEGIN RSA PRIVATE KEY-----\nrotated\n-----END RSA PRIVATE KEY-----"
	resp := h.do(request{method: http.MethodPatch, path: "/api/v1/installations/" + inst.ID,
		cookie: h.session(admin), body: map[string]any{
			"target":         "globex",
			"private_key":    fresh,
			"webhook_secret": "rotated-secret",
		}})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp.body)
	}

	stored, err := h.st.GetInstallation(h.ctx, inst.ID)
	if err != nil {
		t.Fatalf("GetInstallation: %v", err)
	}
	if stored.Target != "globex" {
		t.Fatalf("target = %q, want globex", stored.Target)
	}
	if strings.Contains(string(stored.PrivateKeyEnc), "rotated") {
		t.Fatal("the private key was stored in the clear")
	}
	if got, err := h.key.OpenString(stored.PrivateKeyEnc); err != nil || got != fresh {
		t.Fatalf("unsealed key = %q, %v", got, err)
	}
	if got, err := h.key.OpenString(stored.WebhookSecretEnc); err != nil || got != "rotated-secret" {
		t.Fatalf("unsealed webhook secret = %q, %v", got, err)
	}

	// The response must never carry either secret back out.
	body := string(resp.body)
	if strings.Contains(body, "rotated") {
		t.Fatalf("the response leaked a credential:\n%s", body)
	}
}

// A field the caller can fix is a 422 naming it, not a 500 with a request ID:
// "that is not a PEM key" is something an operator can act on.
func TestUpdateInstallationRefusesThingsTheOperatorCanFix(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	inst := h.installation()

	for _, tc := range []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"an empty target", map[string]any{"target": "   "}, "target"},
		{"a key that is not a key", map[string]any{"private_key": "just some text"}, "private_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(request{method: http.MethodPatch, path: "/api/v1/installations/" + inst.ID,
				cookie: h.session(admin), body: tc.body})
			if resp.status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422: %s", resp.status, resp.body)
			}
			if !strings.Contains(string(resp.body), tc.field) {
				t.Fatalf("the refusal must name %q:\n%s", tc.field, resp.body)
			}
		})
	}

	// A bare GHES hostname is normalised rather than refused: the docs promise
	// it works, here and in zoomies.yaml alike.
	ok := h.do(request{method: http.MethodPatch, path: "/api/v1/installations/" + inst.ID,
		cookie: h.session(admin), body: map[string]any{"api_base_url": "ghes.example.com"}})
	if ok.status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", ok.status, ok.body)
	}
	if got := ok.json(t)["api_base_url"]; got != "https://ghes.example.com/api/v3/" {
		t.Fatalf("api_base_url = %v, want the GHES endpoint it implies", got)
	}

	// None of the refused fields was written.
	stored, err := h.st.GetInstallation(h.ctx, inst.ID)
	if err != nil {
		t.Fatalf("GetInstallation: %v", err)
	}
	if stored.Target != "acme" {
		t.Fatalf("a refused request changed the target to %q", stored.Target)
	}
}

// Deleting an installation takes its pools and their runners with it, and the
// answer says how much that was -- an operator who deletes the wrong one needs
// to know immediately, not from the Runners page a minute later.
func TestDeleteInstallationSaysWhatWentWithIt(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	inst := h.installation()
	pool := h.pool(inst, "zoomies-4vcpu")
	host := h.host("vm-1")
	h.runner(pool, host, store.RunnerIdle)
	// A runner that is already gone is not affected by this, because there is
	// nothing left to take away.
	h.runner(pool, host, store.RunnerRemoved)

	resp := h.do(request{method: http.MethodDelete, path: "/api/v1/installations/" + inst.ID,
		cookie: h.session(admin)})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp.body)
	}
	body := resp.json(t)
	if body["pools_deleted"] != float64(1) {
		t.Fatalf("pools_deleted = %v, want 1", body["pools_deleted"])
	}
	if body["runners_affected"] != float64(1) {
		t.Fatalf("runners_affected = %v, want 1 (the terminal runner is already gone)", body["runners_affected"])
	}

	if _, err := h.st.GetInstallation(h.ctx, inst.ID); err == nil {
		t.Fatal("the installation survived its own deletion")
	}
	pools, err := h.st.ListPools(h.ctx)
	if err != nil {
		t.Fatalf("ListPools: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("%d pools outlived their installation", len(pools))
	}
}

func TestDeleteInstallationReportsAnUnknownID(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)

	resp := h.do(request{method: http.MethodDelete, path: "/api/v1/installations/inst_nope",
		cookie: h.session(admin)})
	if resp.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", resp.status, resp.body)
	}
}

// The pool wizard offers the target's runner groups, so it has to be able to
// read them -- and an installation that does not exist is a 404 rather than a
// 500 about a client it could not build.
func TestRunnerGroupsComeFromGitHub(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	inst := h.installation()

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/installations/" + inst.ID + "/runner-groups",
		cookie: h.session(admin)})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp.body)
	}
	var out struct {
		Items []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	resp.into(t, &out)
	if len(out.Items) == 0 {
		t.Fatal("no runner groups were offered to the wizard")
	}

	missing := h.do(request{method: http.MethodGet, path: "/api/v1/installations/inst_nope/runner-groups",
		cookie: h.session(admin)})
	if missing.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", missing.status, missing.body)
	}
}

// The remaining quota is what an operator checks when scaling has gone quiet,
// so it is a route of its own rather than a line in a log somewhere.
func TestRateLimitReportsTheInstallationsQuota(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	inst := h.installation()

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/installations/" + inst.ID + "/rate-limit",
		cookie: h.session(admin)})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp.body)
	}
	body := resp.json(t)
	for _, key := range []string{"limit", "remaining", "reset_at"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("the quota answer is missing %q:\n%s", key, resp.body)
		}
	}

	missing := h.do(request{method: http.MethodGet, path: "/api/v1/installations/inst_nope/rate-limit",
		cookie: h.session(admin)})
	if missing.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", missing.status, missing.body)
	}
}

// Installations are administrators' business: a viewer may read the fleet but
// must not be able to read, change or delete the credentials it runs on.
func TestInstallationWritesAreAdminOnly(t *testing.T) {
	h := newHarness(t)
	viewer, _ := h.user("viewer", store.RoleViewer)
	inst := h.installation()
	cookie := h.session(viewer)

	for _, req := range []request{
		{method: http.MethodPatch, path: "/api/v1/installations/" + inst.ID, cookie: cookie,
			body: map[string]any{"target": "globex"}},
		{method: http.MethodDelete, path: "/api/v1/installations/" + inst.ID, cookie: cookie},
		{method: http.MethodPost, path: "/api/v1/installations", cookie: cookie,
			body: map[string]any{"app_id": 1, "installation_id": 2}},
	} {
		resp := h.do(req)
		if resp.status != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403", req.method, req.path, resp.status)
		}
		// A 403 has to say which role is missing, or the operator is left
		// guessing at their own permissions.
		if !strings.Contains(strings.ToLower(resp.errorMessage(t)), "admin") {
			t.Errorf("%s %s: the refusal must name the role: %s", req.method, req.path, resp.body)
		}
	}
}

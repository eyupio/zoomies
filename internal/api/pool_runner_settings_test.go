package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
)

// A pool may override the fleet's runner timings, and the three states an
// operator can express all survive the round trip.
//
// The states are the point. Absent leaves what the pool had, so a PATCH that
// changes a priority does not clear an override nobody mentioned; null hands
// the setting back to the fleet; and a duration sets it. A shape that could
// not tell the first two apart would make "stop overriding this" impossible to
// say without also re-sending every other field.
func TestAPoolsRunnerSettingsSurviveTheRoundTrip(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	body := poolBody(inst.ID)
	body["runner_settings"] = map[string]any{
		"provision_timeout": "45m",
		"drain_timeout":     "2m",
	}
	res := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
	res.mustStatus(t, http.StatusCreated, "create")
	var created controller.PoolView
	res.into(t, &created)

	if created.RunnerSettings.ProvisionTimeout == nil ||
		created.RunnerSettings.ProvisionTimeout.Duration() != 45*time.Minute {
		t.Fatalf("provision timeout = %v, want 45m", created.RunnerSettings.ProvisionTimeout)
	}
	if created.RunnerSettings.DrainTimeout == nil ||
		created.RunnerSettings.DrainTimeout.Duration() != 2*time.Minute {
		t.Fatalf("drain timeout = %v, want 2m", created.RunnerSettings.DrainTimeout)
	}
	if created.RunnerSettings.MaxRunnerLifetime != nil {
		t.Errorf("a setting nobody mentioned was given a value: %v", created.RunnerSettings.MaxRunnerLifetime)
	}

	// An edit that says nothing about the overrides leaves them alone.
	res = h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + created.ID,
		cookie: cookie, body: map[string]any{"priority": 3}})
	res.mustStatus(t, http.StatusOK, "patch priority")
	var untouched controller.PoolView
	res.into(t, &untouched)
	if untouched.RunnerSettings.ProvisionTimeout == nil ||
		untouched.RunnerSettings.ProvisionTimeout.Duration() != 45*time.Minute {
		t.Fatalf("an unrelated edit cleared an override: %+v", untouched.RunnerSettings)
	}

	// An explicit null hands that one setting back to the fleet and leaves the
	// other where it was.
	res = h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + created.ID, cookie: cookie,
		body: map[string]any{"runner_settings": map[string]any{"provision_timeout": nil}}})
	res.mustStatus(t, http.StatusOK, "patch null")
	var cleared controller.PoolView
	res.into(t, &cleared)
	if cleared.RunnerSettings.ProvisionTimeout != nil {
		t.Errorf("an explicit null left the override in place: %v", cleared.RunnerSettings.ProvisionTimeout)
	}
	if cleared.RunnerSettings.DrainTimeout == nil ||
		cleared.RunnerSettings.DrainTimeout.Duration() != 2*time.Minute {
		t.Errorf("clearing one override cleared another: %+v", cleared.RunnerSettings)
	}
}

// Zero is a real answer, not an absence: a pool on a slow air-gapped registry
// means "never fail a runner of mine for taking too long to register".
func TestAnOverrideOfZeroIsKept(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	body := poolBody(inst.ID)
	body["runner_settings"] = map[string]any{"provision_timeout": "0s"}
	res := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
	res.mustStatus(t, http.StatusCreated, "create")
	var created controller.PoolView
	res.into(t, &created)
	if created.RunnerSettings.ProvisionTimeout == nil {
		t.Fatal("an override of zero was read as nothing having been said")
	}
	if created.RunnerSettings.ProvisionTimeout.Duration() != 0 {
		t.Errorf("provision timeout = %v, want 0", created.RunnerSettings.ProvisionTimeout)
	}
}

// A duration the parser cannot read is a field error naming the field that
// carried it, so the form can put the message under the input rather than
// showing a 400 about JSON.
func TestAnUnreadableOverrideNamesItsField(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	u, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(u)

	for _, tc := range []struct {
		name  string
		value any
		field string
	}{
		{"not a duration", "5 munutes", "runner_settings.provision_timeout"},
		{"negative", "-1m", "runner_settings.provision_timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := poolBody(inst.ID)
			body["runner_settings"] = map[string]any{"provision_timeout": tc.value}
			res := h.do(request{method: http.MethodPost, path: "/api/v1/pools", cookie: cookie, body: body})
			res.mustStatus(t, http.StatusUnprocessableEntity, "create")
			var env errorEnvelope
			res.into(t, &env)
			named := false
			for _, fe := range env.Errors {
				if fe.Field == tc.field {
					named = true
				}
			}
			if !named {
				t.Errorf("no error on %s: %s", tc.field, res.body)
			}
		})
	}
}

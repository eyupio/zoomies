package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// Every setting this API accepts is one a running controller can change
// without a restart, and each has a shape. A round trip through all of them is
// what stops one being added to the vocabulary with no checking behind it.
func TestEverySettableKeyRoundTrips(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(admin)

	settings := map[string]any{
		"log.level":                      "debug",
		"github.poll_interval":           "45s",
		"scheduler.interval":             "10s",
		"scheduler.scale_up_delay":       "0s",
		"scheduler.max_runner_lifetime":  "6h",
		"scheduler.provision_timeout":    "4m",
		"scheduler.max_creates_per_tick": 7,
		"retention.jobs":                 "720h",
		"retention.runners":              "168h",
		"retention.scaling_events":       "24h",
		"retention.samples":              "48h",
		"retention.webhooks":             "12h",
		"images.refresh_interval":        "30m",
		"updates.check_interval":         "24h",
	}

	resp := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: cookie, body: settings})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp.body)
	}

	cfg := h.ctrl.Config()
	if cfg.Log.Level != "debug" {
		t.Errorf("log level = %q, want debug", cfg.Log.Level)
	}
	if cfg.GitHub.PollInterval.String() != "45s" {
		t.Errorf("poll interval = %s, want 45s", cfg.GitHub.PollInterval)
	}
	if cfg.Scheduler.MaxCreatesPerTick != 7 {
		t.Errorf("max creates per tick = %d, want 7", cfg.Scheduler.MaxCreatesPerTick)
	}
	if cfg.Scheduler.MaxRunnerLifetime.String() != "6h0m0s" {
		t.Errorf("max runner lifetime = %s, want 6h", cfg.Scheduler.MaxRunnerLifetime)
	}
	if cfg.Retention.Webhooks.String() != "12h0m0s" {
		t.Errorf("webhook retention = %s, want 12h", cfg.Retention.Webhooks)
	}
	if cfg.Updates.CheckInterval.String() != "24h0m0s" {
		t.Errorf("update check interval = %s, want 24h", cfg.Updates.CheckInterval)
	}
}

// A request is refused as a whole before any part of it has taken effect. A
// patch that half-applied would leave the controller in a state the operator
// never asked for and cannot see.
func TestASettingsPatchIsAllOrNothing(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(admin)
	before := h.ctrl.Config().Log.Level

	resp := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: cookie,
		body: map[string]any{"log.level": "debug", "scheduler.interval": "not a duration"}})
	if resp.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", resp.status, resp.body)
	}
	if got := h.ctrl.Config().Log.Level; got != before {
		t.Fatalf("log level = %q after a refused patch, want %q untouched", got, before)
	}
}

// Each refusal names what is wrong with the value, because "invalid" leaves an
// operator guessing at which of the fourteen keys they got wrong and how.
func TestSettingsRefusalsSayWhatIsWrongWithTheValue(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)
	cookie := h.session(admin)

	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"an unknown key", map[string]any{"nonsense.key": "1s"}, "not a setting"},
		{"a log level that is not one", map[string]any{"log.level": "chatty"}, "is not a log level"},
		{"a level that is not even a string", map[string]any{"log.level": 3}, "expected a string"},
		{"a duration that is not one", map[string]any{"retention.jobs": "a while"}, "is not a duration"},
		{"a negative duration", map[string]any{"retention.jobs": "-1h"}, "cannot be negative"},
		// A poll interval of one millisecond is a denial of service against
		// GitHub, not a configuration choice.
		{"a poll interval below the floor", map[string]any{"github.poll_interval": "1ms"}, "too short"},
		{"a negative cap", map[string]any{"scheduler.max_creates_per_tick": -1}, "cannot be negative"},
		{"a fractional cap", map[string]any{"scheduler.max_creates_per_tick": 1.5}, "whole number"},
		{"a cap that is not a number at all", map[string]any{"scheduler.max_creates_per_tick": true}, "whole number"},
		{"a cap written as words", map[string]any{"scheduler.max_creates_per_tick": "many"}, "whole number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: cookie, body: tc.body})
			if resp.status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422: %s", resp.status, resp.body)
			}
			if !strings.Contains(string(resp.body), tc.want) {
				t.Fatalf("the refusal must say %q:\n%s", tc.want, resp.body)
			}
		})
	}
}

// A whole number sent as a string is what a script written by hand produces,
// and refusing it would be a distinction without a difference.
func TestASettingsCapMayBeWrittenAsAString(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)

	resp := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: h.session(admin),
		body: map[string]any{"scheduler.max_creates_per_tick": "12"}})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp.body)
	}
	if got := h.ctrl.Config().Scheduler.MaxCreatesPerTick; got != 12 {
		t.Fatalf("max creates per tick = %d, want 12", got)
	}
}

// Zero switches a duration off, and that is a real answer rather than a value
// below the floor -- "never expire these" is a supported choice.
func TestZeroSwitchesADurationOffRatherThanFailingTheFloor(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("admin", store.RoleAdmin)

	resp := h.do(request{method: http.MethodPatch, path: "/api/v1/settings", cookie: h.session(admin),
		body: map[string]any{"github.poll_interval": "0s", "scheduler.max_creates_per_tick": 0}})
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp.body)
	}
	cfg := h.ctrl.Config()
	if cfg.GitHub.PollInterval != 0 {
		t.Errorf("poll interval = %s, want it switched off", cfg.GitHub.PollInterval)
	}
	if cfg.Scheduler.MaxCreatesPerTick != 0 {
		t.Errorf("max creates per tick = %d, want no cap", cfg.Scheduler.MaxCreatesPerTick)
	}
}

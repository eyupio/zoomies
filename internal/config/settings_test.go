package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// The registry is the one list, so it has to actually cover the struct.
//
// The whole reason it exists is that five hand-maintained lists of the same
// fact had drifted. A sixth that could drift from the struct it describes would
// be no better than the five: a field with no row has no environment variable,
// no place on the settings page and no way into the database, and nothing would
// say so until somebody went looking for a setting that was never there.
func TestEverySettingIsRegistered(t *testing.T) {
	keys := SettingKeys()
	if len(keys) < 80 {
		t.Fatalf("the walk found %d keys, which is fewer than the configuration has", len(keys))
	}

	var unlisted []string
	for _, key := range keys {
		if _, ok := LookupSetting(key); !ok && !unregistered[key] {
			unlisted = append(unlisted, key)
		}
	}
	if len(unlisted) > 0 {
		t.Errorf("these fields of Config have no row in the settings registry:\n  %s\n"+
			"add one, or add the key to `unregistered` with the reason", strings.Join(unlisted, "\n  "))
	}

	// And the reverse: a row naming a field that no longer exists would render
	// a setting on the page that nothing reads.
	for _, s := range Settings() {
		if !slices.Contains(keys, s.Key) {
			t.Errorf("the registry has %s, but Config has no such field", s.Key)
		}
	}
}

// Two settings must never share an environment variable: whichever applyEnv
// reached second would silently win, and the other would be a setting that
// cannot be set from the environment at all.
func TestEverySettingHasItsOwnEnvironmentVariable(t *testing.T) {
	seen := map[string]string{}
	for _, s := range Settings() {
		if s.Env == "" {
			t.Errorf("%s has no environment variable, so there is no way past a bad stored value", s.Key)
			continue
		}
		if !strings.HasPrefix(s.Env, "ZOOMIES_") {
			t.Errorf("%s uses %s, which is not a ZOOMIES_* name", s.Key, s.Env)
		}
		if other, dup := seen[s.Env]; dup {
			t.Errorf("%s and %s both use %s", other, s.Key, s.Env)
		}
		seen[s.Env] = s.Key
	}
}

// Every value survives the trip it makes on every restart: out of the
// configuration as text, into a database row, and back.
//
// A value that does not round-trip is a setting that changes itself. The one
// that caught this was oidc.scopes, which reached the database as the Go
// rendering of a slice -- "[openid profile email]" -- and came back as a
// one-item list whose only item was the whole list.
func TestEverySettingRoundTripsThroughItsStoredText(t *testing.T) {
	c := Default()
	for _, s := range Settings() {
		before, err := c.Value(s.Key)
		if err != nil {
			t.Fatalf("%s: %v", s.Key, err)
		}
		text := Text(s, before)
		if _, err := c.SetValueString(s.Key, text); err != nil {
			t.Errorf("%s: the text it renders as (%q) does not parse back: %v", s.Key, text, err)
			continue
		}
		after, err := c.Value(s.Key)
		if err != nil {
			t.Fatalf("%s: %v", s.Key, err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Errorf("%s: %#v rendered as %q and came back as %#v", s.Key, before, text, after)
		}
	}
}

// Values that arrive as JSON are not the Go types the configuration holds: a
// list is []any and a number is float64. The settings API hands those straight
// through, so they have to be understood.
func TestAValueFromJSONIsUnderstood(t *testing.T) {
	c := Default()
	for _, tc := range []struct {
		key   string
		value any
		want  string
	}{
		{"oidc.scopes", []any{"openid", "profile"}, "openid,profile"},
		{"agent.capacity", float64(6), "6"},
		{"agent.labels", map[string]any{"tier": "build"}, "tier=build"},
		{"scheduler.interval", "45s", "45s"},
		{"security.cookie_secure", true, "true"},
		{"server.tls.mode", "Self-Signed", "self-signed"},
	} {
		s, ok := LookupSetting(tc.key)
		if !ok {
			t.Fatalf("no setting %s", tc.key)
		}
		if _, err := c.SetValue(tc.key, tc.value); err != nil {
			t.Errorf("%s = %#v: %v", tc.key, tc.value, err)
			continue
		}
		got, _ := c.Value(tc.key)
		if text := Text(s, got); text != tc.want {
			t.Errorf("%s = %#v stored as %q, want %q", tc.key, tc.value, text, tc.want)
		}
	}
}

// A duration is shown the way somebody writes it. Go's own rendering is exact
// and unreadable past an hour, and an operator who typed 720h and was shown
// 720h0m0s has to work out whether the product changed their mind.
func TestADurationIsRenderedTheWayItIsWritten(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{30 * time.Second, "30s"},
		{2 * time.Minute, "2m"},
		{720 * time.Hour, "720h"},
		{90 * time.Minute, "1h30m"},
		{time.Hour + 30*time.Second, "1h0m30s"},
	} {
		if got := TidyDuration(tc.in); got != tc.want {
			t.Errorf("TidyDuration(%s) = %q, want %q", tc.in, got, tc.want)
		}
		if _, err := time.ParseDuration(TidyDuration(tc.in)); err != nil {
			t.Errorf("TidyDuration(%s) does not parse back: %v", tc.in, err)
		}
	}
}

// The three keys that get the database open cannot live in it, and nothing
// else may quietly join them: a setting marked bootstrap is one an
// administrator cannot change from the settings page, so the list is a
// deliberate one rather than something that grows by accident.
func TestOnlyTheKeysThatOpenTheDatabaseAreBootstrap(t *testing.T) {
	var got []string
	for _, s := range Settings() {
		if s.Scope == ScopeBootstrap {
			got = append(got, s.Key)
		}
	}
	slices.Sort(got)
	want := []string{"database.path", "security.encryption_key", "security.encryption_key_file"}
	if !slices.Equal(got, want) {
		t.Errorf("bootstrap settings = %v, want %v", got, want)
	}
}

// A setting that says it applies at once has to be one the running controller
// actually re-reads. Saying "saved" about a value nothing is using is worse
// than refusing the edit, because there is nothing left to fix.
func TestALiveSettingGivesNoReasonItNeedsARestart(t *testing.T) {
	for _, s := range Settings() {
		if s.Live && s.RestartReason != "" {
			t.Errorf("%s is live and also explains why it needs a restart", s.Key)
		}
		if !s.Live && s.Stored() && s.RestartReason == "" {
			t.Errorf("%s waits for a restart and does not say why; the settings page has nothing to tell an operator", s.Key)
		}
	}
}

// Every setting has something to call it and something to say about it, so the
// settings page never has to fall back to the dotted key and an empty line.
func TestEverySettingHasALabelAndASummary(t *testing.T) {
	for _, s := range Settings() {
		if strings.TrimSpace(s.Label) == "" {
			t.Errorf("%s has no label", s.Key)
		}
		if strings.TrimSpace(s.Summary) == "" {
			t.Errorf("%s has no summary", s.Key)
		}
		if s.Kind == KindEnum && len(s.Choices) == 0 {
			t.Errorf("%s is an enum with no choices, so the page cannot offer a menu", s.Key)
		}
	}
}

// Settings are listed in the order the documentation lists them, not
// alphabetically: server before database before security, because that is the
// order somebody reads a configuration in.
func TestSettingsAreOrderedTheWayTheDocumentationOrdersThem(t *testing.T) {
	if CompareKeys("server.bind", "database.path") >= 0 {
		t.Error("server should sort before database")
	}
	if CompareKeys("retention.jobs", "retention.runners") >= 0 {
		t.Error("within a section, keys sort alphabetically")
	}
	for _, s := range Settings() {
		if !slices.Contains(SectionOrder, s.Section()) {
			t.Errorf("%s is in section %q, which has no place in SectionOrder", s.Key, s.Section())
		}
	}
}

// A refusal names the setting, says what it is for, and says what was wrong --
// because "5 munutes is not a duration" is a sentence somebody has to open the
// documentation to act on.
func TestARefusalSaysWhatTheSettingIsFor(t *testing.T) {
	c := Default()
	_, err := c.SetValueString("scheduler.provision_timeout", "5 munutes")
	if err == nil {
		t.Fatal("a nonsense duration was accepted")
	}
	for _, want := range []string{"scheduler.provision_timeout", "never finishes registering", "30s, 5m, 2h"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

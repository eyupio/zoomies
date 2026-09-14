package store

import (
	"context"
	"errors"
	"testing"
)

// An empty value is a value, not an absence.
//
// This is the whole reason instance settings have their own read rather than
// reusing GetSetting next door, which answers "" for a missing row. An operator
// can legitimately clear an external URL or empty a trusted-proxy list, and a
// caller that could not tell that from "never set" would overwrite the layer
// underneath with a blank every time it looked.
func TestAnEmptyInstanceSettingIsNotTheSameAsAnUnsetOne(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.GetInstanceSetting(ctx, "server.external_url"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unset key answered %v, want ErrNotFound", err)
	}

	if err := s.PutInstanceSettings(ctx, "root", []InstanceSetting{
		{Key: "server.external_url", Value: ""},
	}); err != nil {
		t.Fatalf("PutInstanceSettings: %v", err)
	}

	row, err := s.GetInstanceSetting(ctx, "server.external_url")
	if err != nil {
		t.Fatalf("a key set to the empty string answered %v", err)
	}
	if row.Value != "" || row.UpdatedBy != "root" {
		t.Errorf("row = %+v", row)
	}
}

// A batch is one change. Half a settings request applied is a fleet in a state
// nobody asked for, with an audit row for neither half.
func TestInstanceSettingsAreWrittenAsOneChange(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.PutInstanceSettings(ctx, "root", []InstanceSetting{
		{Key: "scheduler.interval", Value: "30s"},
		{Key: "retention.jobs", Value: "720h"},
	}); err != nil {
		t.Fatalf("PutInstanceSettings: %v", err)
	}

	rows, err := s.InstanceSettings(ctx)
	if err != nil {
		t.Fatalf("InstanceSettings: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("stored %d rows, want 2: %+v", len(rows), rows)
	}
	// Ordered by key, so two reads of an unchanged database are identical and
	// a caller diffing them sees nothing rather than a reshuffle.
	if rows[0].Key != "retention.jobs" || rows[1].Key != "scheduler.interval" {
		t.Errorf("rows are not ordered by key: %+v", rows)
	}
	for _, row := range rows {
		if row.UpdatedAt.IsZero() {
			t.Errorf("%s has no timestamp, so the page cannot say when it changed", row.Key)
		}
	}
}

// The list a handler renders from cannot carry a credential, whatever the
// handler then does with it.
func TestListingInstanceSettingsBlanksEverySecret(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.PutInstanceSettings(ctx, "root", []InstanceSetting{
		{Key: "oidc.client_secret", Value: "sealed-and-base64", Secret: true},
		{Key: "scheduler.interval", Value: "30s"},
	}); err != nil {
		t.Fatalf("PutInstanceSettings: %v", err)
	}

	rendered, err := s.ListInstanceSettings(ctx)
	if err != nil {
		t.Fatalf("ListInstanceSettings: %v", err)
	}
	for _, row := range rendered {
		if row.Secret && row.Value != "" {
			t.Errorf("%s came back with its value: %q", row.Key, row.Value)
		}
	}

	// The startup path still gets it, because it holds the key that opens it.
	raw, err := s.InstanceSettings(ctx)
	if err != nil {
		t.Fatalf("InstanceSettings: %v", err)
	}
	var found bool
	for _, row := range raw {
		if row.Key == "oidc.client_secret" {
			found = row.Value == "sealed-and-base64"
		}
	}
	if !found {
		t.Error("the sealed value did not reach the one caller that can read it")
	}
}

// Clearing a key is how a setting goes back to the layer underneath, and asking
// to clear one that was never set is not a mistake: the caller wanted it unset,
// and it is.
func TestClearingASettingThatWasNeverSetIsNotAnError(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.DeleteInstanceSettings(ctx, []string{"scheduler.interval"}); err != nil {
		t.Fatalf("clearing an unset key: %v", err)
	}

	if err := s.PutInstanceSettings(ctx, "root", []InstanceSetting{
		{Key: "scheduler.interval", Value: "30s"},
	}); err != nil {
		t.Fatalf("PutInstanceSettings: %v", err)
	}
	if err := s.DeleteInstanceSettings(ctx, []string{"scheduler.interval"}); err != nil {
		t.Fatalf("DeleteInstanceSettings: %v", err)
	}
	if _, err := s.GetInstanceSetting(ctx, "scheduler.interval"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the row survived being cleared: %v", err)
	}
}

// HasInstanceSettings is what tells a first run from a fleet somebody has
// configured, which is the question the file importer asks before it copies
// anything in.
func TestHasInstanceSettingsAnswersWhetherAnythingIsStored(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if has, err := s.HasInstanceSettings(ctx); err != nil || has {
		t.Fatalf("a fresh database reported %v, %v", has, err)
	}
	if err := s.PutInstanceSettings(ctx, "root", []InstanceSetting{{Key: "log.level", Value: "debug"}}); err != nil {
		t.Fatalf("PutInstanceSettings: %v", err)
	}
	if has, err := s.HasInstanceSettings(ctx); err != nil || !has {
		t.Errorf("after a write it reported %v, %v", has, err)
	}
}

// A sealed setting counts as a sealed secret, so a restore that brought the
// database back without its key is refused at startup rather than starting,
// looking healthy and failing inside the first request that needs one.
func TestASealedSettingCountsAsASealedSecret(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if sealed, err := s.HasSealedSecrets(ctx); err != nil || sealed {
		t.Fatalf("a fresh database reported %v, %v", sealed, err)
	}
	if err := s.PutInstanceSettings(ctx, "root", []InstanceSetting{
		{Key: "capacity_demand.signing_secret", Value: "sealed", Secret: true},
	}); err != nil {
		t.Fatalf("PutInstanceSettings: %v", err)
	}
	if sealed, err := s.HasSealedSecrets(ctx); err != nil || !sealed {
		t.Errorf("a sealed setting was not counted: %v, %v", sealed, err)
	}
}

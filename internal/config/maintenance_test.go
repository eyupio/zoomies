package config

import (
	"strings"
	"testing"
	"time"
)

// The controller tidies its own state directory on a schedule, and two settings
// say how: how many of the copies it took before a migration it keeps, and how
// often the pass runs. What these hold is the part a person relies on without
// reading the code: the defaults, that both can be changed without a restart,
// and that neither can be set to a value that deletes the only way back.
func TestTheMaintenanceSettingsHaveTheDefaultsAndLimitsTheyPromise(t *testing.T) {
	c := Default()
	if c.Backup.PreMigrationKeep != 2 {
		t.Errorf("backup.pre_migration_keep defaults to %d, want 2: the upgrade that just happened and the one before it", c.Backup.PreMigrationKeep)
	}
	if c.Maintenance.Interval != 24*time.Hour {
		t.Errorf("maintenance.interval defaults to %v, want 24h", c.Maintenance.Interval)
	}

	keep, ok := LookupSetting("backup.pre_migration_keep")
	if !ok || keep.Kind != KindInt || !keep.Live || !keep.Platform() {
		t.Errorf("backup.pre_migration_keep = %+v (found %v), want a live integer that is the platform's", keep, ok)
	}
	interval, ok := LookupSetting("maintenance.interval")
	if !ok || interval.Kind != KindDuration || !interval.Live || !interval.Platform() || interval.Floor != time.Minute {
		t.Errorf("maintenance.interval = %+v (found %v), want a live duration with a floor of a minute that is the platform's", interval, ok)
	}
}

// Zero switches the scheduled pass off, which is a real answer. Anything under a
// minute is a loop that reads the backup directory all day.
func TestTheMaintenanceIntervalIsRefusedBelowAMinuteAndZeroSwitchesItOff(t *testing.T) {
	for _, tc := range []struct {
		value   string
		wantErr string
	}{
		{"30s", "too short"},
		{"1m", ""},
		{"24h", ""},
		{"0", ""},
	} {
		c := Default()
		_, err := c.SetValueString("maintenance.interval", tc.value)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%q: unexpected error: %v", tc.value, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%q: error = %v, want one mentioning %q", tc.value, err, tc.wantErr)
		}
	}
}

// A copy taken before a migration is the one rollback there is, and once the
// upgrade has run nothing can take it again. A keep of none is therefore an
// error that stops a settings change, and names what to do instead.
func TestAPreMigrationKeepBelowOneIsAnErrorThatSaysWhatToSetInstead(t *testing.T) {
	for _, keep := range []int{0, -3} {
		c := Default()
		c.Backup.PreMigrationKeep = keep
		var found *Finding
		for _, f := range c.Validate().Errors() {
			if f.Code == "backup.pre_migration_keep" {
				found = &f
			}
		}
		if found == nil {
			t.Fatalf("a pre_migration_keep of %d raised no error", keep)
		}
		if found.Setting != "backup.pre_migration_keep" || !strings.Contains(found.Title, "at least 1") || found.Fix == "" {
			t.Errorf("finding = %+v, want it to name the setting, say at least 1 and give a fix", *found)
		}
	}
	for _, keep := range []int{1, 2, 10} {
		c := Default()
		c.Backup.PreMigrationKeep = keep
		for _, f := range c.Validate().Errors() {
			if f.Code == "backup.pre_migration_keep" {
				t.Errorf("a pre_migration_keep of %d raised %+v", keep, f)
			}
		}
	}
}

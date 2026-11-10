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

// The controller gives a database's free pages back to the disk, and two settings
// say whether and when. The default is to give them back for a database that can
// be given them back in slices, and to leave one that cannot alone: rewriting an
// existing database holds the writer for as long as it takes, so it is a choice
// somebody makes, not something an upgrade does.
func TestTheCompactionSettingsHaveTheDefaultsAndChoicesTheyPromise(t *testing.T) {
	c := Default()
	if c.Maintenance.Compact != "incremental" {
		t.Errorf("maintenance.compact defaults to %q, want incremental", c.Maintenance.Compact)
	}
	if c.Maintenance.CompactFreePercent != 20 {
		t.Errorf("maintenance.compact_free_percent defaults to %d, want 20", c.Maintenance.CompactFreePercent)
	}

	mode, ok := LookupSetting("maintenance.compact")
	if !ok || mode.Kind != KindEnum || !mode.Live || !mode.Platform() {
		t.Errorf("maintenance.compact = %+v (found %v), want a live choice that is the platform's", mode, ok)
	}
	if want := []string{"off", "incremental", "convert"}; !slicesEqual(mode.Choices, want) {
		t.Errorf("maintenance.compact offers %v, want %v", mode.Choices, want)
	}
	percent, ok := LookupSetting("maintenance.compact_free_percent")
	if !ok || percent.Kind != KindInt || !percent.Live || !percent.Platform() {
		t.Errorf("maintenance.compact_free_percent = %+v (found %v), want a live integer that is the platform's", percent, ok)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A mode nobody offered, and a share that is no share, are errors that stop a
// settings change and say what to set instead.
func TestACompactionSettingThatMeansNothingIsAnErrorThatSaysWhatToSetInstead(t *testing.T) {
	has := func(c *Config, code string) *Finding {
		for _, f := range c.Validate().Errors() {
			if f.Code == code {
				return &f
			}
		}
		return nil
	}

	c := Default()
	c.Maintenance.Compact = "sometimes"
	if f := has(c, "maintenance.compact"); f == nil || !strings.Contains(f.Fix, "off, incremental or convert") {
		t.Errorf("an unknown mode raised %+v, want an error that offers the choices", f)
	}
	for _, mode := range []string{"off", "incremental", "convert", "CONVERT", " incremental ", ""} {
		c := Default()
		c.Maintenance.Compact = mode
		if f := has(c, "maintenance.compact"); f != nil {
			t.Errorf("the mode %q raised %+v", mode, f)
		}
	}

	for _, percent := range []int{0, -5, 100, 250} {
		c := Default()
		c.Maintenance.CompactFreePercent = percent
		f := has(c, "maintenance.compact_percent")
		if f == nil || f.Setting != "maintenance.compact_free_percent" || f.Fix == "" {
			t.Errorf("a share of %d raised %+v, want an error that names the setting and says what to set", percent, f)
		}
	}
	for _, percent := range []int{1, 20, 99} {
		c := Default()
		c.Maintenance.CompactFreePercent = percent
		if f := has(c, "maintenance.compact_percent"); f != nil {
			t.Errorf("a share of %d raised %+v", percent, f)
		}
	}
}

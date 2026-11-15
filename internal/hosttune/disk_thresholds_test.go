package hosttune

import (
	"context"
	"strings"
	"testing"
)

func TestConfiguredDiskWarnings(t *testing.T) {
	for _, tc := range []struct {
		name       string
		thresholds *DiskThresholds
		want       Status
	}{
		{"defaults", nil, OK},
		{"percent", &DiskThresholds{WarnFreePercent: 30}, Warn},
		{"bytes", &DiskThresholds{WarnFreeGiB: 30}, Warn},
		{"boundary", &DiskThresholds{WarnFreePercent: 20, WarnFreeGiB: 20}, OK},
		{"disabled", &DiskThresholds{}, OK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, f := fixture()
			e.Disk = tc.thresholds
			f.commands[strings.Join([]string{"df", "-Pk", "--", "/work"}, " ")] = "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/sda1 104857600 83886080 20971520 80% /work"
			got := diskCheck(false)(context.Background(), e)
			if got.Status != tc.want {
				t.Fatalf("%+v", got)
			}
		})
	}
}

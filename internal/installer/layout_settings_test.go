package installer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// controllerFixture is a Compose controller deployment whose data volume holds
// a database with the given settings, and whose Compose file an older release
// wrote: no shared folder, no socket, no group.
func controllerFixture(t *testing.T, settings map[string]string) (UpgradeOptions, DeploymentRecord, string) {
	t.Helper()
	opts, rec := upgradeFixture(t, DeploymentCompose)
	rec.Mode = ModeController
	opts.Mode = ModeController
	if _, err := WriteDeploymentRecord(opts.ConfigDir, rec); err != nil {
		t.Fatal(err)
	}
	volume := filepath.Join(t.TempDir(), "volume")
	if err := os.MkdirAll(volume, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(volume, "zoomies.db")})
	if err != nil {
		t.Fatal(err)
	}
	var rows []store.InstanceSetting
	for k, v := range settings {
		rows = append(rows, store.InstanceSetting{Key: k, Value: v})
	}
	if len(rows) > 0 {
		if err := st.PutInstanceSettings(context.Background(), "test", rows); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.Close()

	old := "services:\n  zoomies:\n    image: ${ZOOMIES_IMAGE}\n    volumes:\n      - zoomies-data:/var/lib/zoomies\n    # an operator's own note\n    labels:\n      team: platform\n"
	if err := os.WriteFile(rec.ComposeFile(), []byte(old), 0o640); err != nil {
		t.Fatal(err)
	}
	opts.shared.dir = filepath.Join(t.TempDir(), "shared")
	return opts, rec, volume
}

// fakeVolume answers the question where the container's data is mounted
// from with the fixture's volume, and everything else as a healthy host
// would.
func fakeVolume(opts *UpgradeOptions, volume string, inspectErr error) {
	opts.run = func(_ context.Context, name string, args ...string) (string, error) {
		line := name + " " + strings.Join(args, " ")
		switch {
		case strings.Contains(line, "inspect --type container --format"):
			return volume, inspectErr
		case strings.Contains(line, "config --images"):
			return opts.Image, nil
		case name == "docker" && len(args) > 0 && args[0] == "inspect":
			return "true", nil
		}
		return "", nil
	}
}

// Whether a controller runs runners is a setting, and settings are kept in the
// database -- the settings page writes nowhere else. So the upgrade asks the
// deployment's database, and a controller whose embedded agent was turned off
// there is not offered a runner host's folder and mounts.
func TestTheDatabaseDecidesWhatAControllerIsOffered(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings map[string]string
		offered  bool
	}{
		{"embedded agent turned off on the settings page", map[string]string{"agent.embedded": "false"}, false},
		{"embedded agent turned on", map[string]string{"agent.embedded": "true"}, true},
		{"never set, so the default: on", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, _, volume := controllerFixture(t, tc.settings)
			fakeVolume(&opts, volume, nil)
			opts.socketGroup = func(string) int { return 998 }
			var out bytes.Buffer
			opts.Out, opts.Check = &out, true
			if err := Upgrade(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			got := strings.Contains(out.String(), "mount "+SharedHostDir)
			if got != tc.offered {
				t.Errorf("shared folder offered = %v, want %v:\n%s", got, tc.offered, out.String())
			}
			if strings.Contains(out.String(), "Could not read") {
				t.Errorf("the database was there to read:\n%s", out.String())
			}
		})
	}
}

// An older Compose file can lack more than the shared folder: the socket and
// its group were once optional too. Every mount this release would write for
// the host is offered, in one edit with one backup.
func TestAnOldComposeFileIsGivenEveryMountItLacks(t *testing.T) {
	opts, rec, volume := controllerFixture(t, map[string]string{"agent.embedded": "true"})
	fakeVolume(&opts, volume, nil)
	opts.socketGroup = func(string) int { return 998 }
	var out bytes.Buffer
	opts.Out, opts.AssumeYes = &out, true
	if err := Upgrade(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(rec.ComposeFile())
	if err != nil {
		t.Fatal(err)
	}
	have, err := composeServiceMounts(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{SharedHostDir, "/var/run/docker.sock", "/var/lib/zoomies"} {
		if !have.targets[target] {
			t.Errorf("%s is not mounted after the upgrade:\n%s", target, body)
		}
	}
	if !have.groups["998"] {
		t.Errorf("the socket's group was not added:\n%s", body)
	}
	for _, keep := range []string{"an operator's own note", "team: platform", "${ZOOMIES_IMAGE}"} {
		if !strings.Contains(string(body), keep) {
			t.Errorf("the edit lost %q:\n%s", keep, body)
		}
	}
	if backups, _ := filepath.Glob(rec.ComposeFile() + ".bak.*"); len(backups) != 1 {
		t.Errorf("backups = %v, want one", backups)
	}
}

// Docker Desktop keeps volumes in its own VM, and a volume can be anywhere a
// daemon puts it. When the database cannot be read the upgrade says so and
// offers what a host that runs runners needs: a question too many costs a
// controller nothing, a mount too few breaks a runner host's tool cache.
func TestAnUnreadableDatabaseOffersWhatARunnerHostNeeds(t *testing.T) {
	opts, _, _ := controllerFixture(t, map[string]string{"agent.embedded": "false"})
	fakeVolume(&opts, "", errors.New("no such volume"))
	opts.socketGroup = func(string) int { return 0 }
	var out bytes.Buffer
	opts.Out, opts.Check = &out, true
	if err := Upgrade(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Could not read this deployment's settings") || !strings.Contains(out.String(), "mount "+SharedHostDir) {
		t.Errorf("output:\n%s", out.String())
	}
}
